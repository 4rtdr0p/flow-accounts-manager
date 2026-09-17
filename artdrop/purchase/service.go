package purchase

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop/studio"
	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// ErrChargeAlreadyRecorded is returned when a charge with the same Stripe
// payment intent has already been recorded. This is the idempotency guard at
// the audit layer: a double-click or a network retry must not create a
// duplicate charge record.
var ErrChargeAlreadyRecorded = errors.New("charge already recorded")

// ErrArtworkNotFound is returned when the requested artwork does not exist in
// Mongo (no matching edition or painting).
var ErrArtworkNotFound = errors.New("artwork not found")

// ErrArtworkPriceMissing is returned when the artwork exists but carries no
// usable price.
var ErrArtworkPriceMissing = errors.New("artwork price missing")

// ErrPricingDisabled is returned when the artwork price store is not
// configured (Mongo disabled).
var ErrPricingDisabled = errors.New("purchase pricing is disabled")

// ErrOracleDisabled is returned when the Pyth oracle is not configured.
var ErrOracleDisabled = errors.New("pyth oracle is disabled")

// ErrOracleStale is returned when the Pyth price is older than the configured
// maximum age.
var ErrOracleStale = errors.New("pyth price is stale")

// ErrStripeDisabled is returned when the Stripe client is not configured.
var ErrStripeDisabled = errors.New("stripe is disabled")

// ErrEscrowDisabled is returned when the escrow creator is not configured.
var ErrEscrowDisabled = errors.New("escrow creator is disabled")

// ErrChargeRecordFailed is returned when the audit record could not be
// persisted for a reason other than a duplicate payment intent. Callers must
// map it to a 5xx so the idempotency middleware releases the key reservation.
var ErrChargeRecordFailed = errors.New("failed to record charge")
var ErrPurchaseNotFound = errors.New("paid purchase not found")
var ErrPurchaseNotOpenable = errors.New("purchase is not awaiting escrow")
var ErrChipNotProvisioned = errors.New("chip is not provisioned")
var ErrChipUnavailable = errors.New("chip lookup is unavailable")
var ErrInvalidOpenEscrowInput = errors.New("invalid open escrow request")
var ErrEscrowUnavailable = errors.New("escrow queue is unavailable")

// Service lists all functionality provided by the purchase service.
type Service interface {
	// CreatePurchaseCharge charges a buyer's purchase and opens the escrow:
	// it reads the artwork price from Mongo, applies the configured platform
	// fee (the ArtDrop share of that price, not a surcharge on it), charges
	// the buyer the full artwork price via a Stripe PaymentIntent, converts
	// only the platform fee to FLOW via the Pyth oracle, opens the on-chain
	// escrow with that FLOW amount as a gas reserve, and persists the audit
	// record. Every amount is computed server-side; the client only
	// identifies the artwork, the parties and the payment details.
	CreatePurchaseCharge(ctx context.Context, in CreatePurchaseChargeInput) (*PurchaseCharge, error)
	OpenEscrow(ctx context.Context, in OpenEscrowInput) (*PurchaseCharge, error)
}

// ServiceImpl implements the purchase Service.
type ServiceImpl struct {
	store              Store
	prices             ArtworkPriceReader
	oracle             PriceOracle
	charge             ChargeClient
	escrow             EscrowCreator
	chips              ChipReader
	platformFeeBps     int
	shippingRatePerUSD float64

	// claimWindowSeconds is the buyer's on-chain claim deadline window (issue
	// #111): the escrow's unlock_at is computed as now() + claimWindowSeconds,
	// never accepted from the client. See Config.EscrowClaimWindowSeconds.
	claimWindowSeconds float64
	// now is the current-time source, overridable in tests so unlock_at can be
	// pinned to a known value; defaults to time.Now.
	now func() time.Time
}

// NewService initiates a new purchase service wired for the full charge flow.
// Any of the optional deps may be nil; the corresponding step reports its
// disabled error. platformFeeBps is the platform fee in basis points, applied
// as ArtDrop's share of the artwork price rather than a surcharge added on top
// of it (see Config.PurchasePlatformFeeBasisPoints). claimWindowSeconds is the
// server-computed unlock_at window (issue #111; see
// Config.EscrowClaimWindowSeconds) — the buyer's on-chain claim deadline is
// now() + claimWindowSeconds, never client-supplied.
func NewService(store Store, prices ArtworkPriceReader, oracle PriceOracle, charge ChargeClient, escrow EscrowCreator, chips ChipReader, platformFeeBps int, claimWindowSeconds float64) Service {
	return &ServiceImpl{
		store:              store,
		prices:             prices,
		oracle:             oracle,
		charge:             charge,
		escrow:             escrow,
		chips:              chips,
		platformFeeBps:     platformFeeBps,
		claimWindowSeconds: claimWindowSeconds,
		now:                time.Now,
	}
}

// CreatePurchaseCharge charges a buyer's purchase and opens the escrow
// end-to-end. The amount is never trusted from the client: it is computed from
// the artwork price in Mongo, the configured platform fee, and the current
// FLOW/USD price from the Pyth oracle. The buyer is charged the full artwork
// price in USD; the escrow carries only the platform fee, converted to FLOW.
func (s *ServiceImpl) CreatePurchaseCharge(ctx context.Context, in CreatePurchaseChargeInput) (*PurchaseCharge, error) {
	if in.UserID == "" {
		return nil, fmt.Errorf("user id is required")
	}
	if in.ArtworkID == "" {
		return nil, fmt.Errorf("artwork id is required")
	}
	if in.ArtworkKind != ArtworkEdition && in.ArtworkKind != ArtworkPainting {
		return nil, fmt.Errorf("artwork kind must be %q or %q", ArtworkEdition, ArtworkPainting)
	}
	if in.StripeCustomerID == "" {
		return nil, fmt.Errorf("stripe customer id is required")
	}
	if in.IdempotencyKey == "" {
		return nil, fmt.Errorf("idempotency key is required")
	}
	if in.Buyer == "" {
		return nil, fmt.Errorf("buyer is required")
	}
	if in.Seller == "" {
		return nil, fmt.Errorf("seller is required")
	}

	// 1. Read the artwork price from Mongo. The price is in whole dollars
	// (USD) as stored by Payload CMS.
	if s.prices == nil {
		return nil, ErrPricingDisabled
	}
	artworkPrice, err := s.readArtworkPrice(ctx, in)
	if err != nil {
		return nil, err
	}

	// 2. Compute the configured platform fee (in basis points) as ArtDrop's
	// share of the artwork price, not a surcharge added on top of it. The fee
	// is server-configured, never client-supplied. The buyer is charged the
	// full artwork price; the fee is the portion of that price ArtDrop keeps.
	feeBps := s.platformFeeBps
	if feeBps <= 0 {
		feeBps = 0
	}
	artworkCents := int64(math.Round(artworkPrice.PriceUSD * 100))
	feeCents := int64(math.Round(float64(artworkCents) * float64(feeBps) / 10000))
	if artworkCents <= 0 {
		return nil, fmt.Errorf("computed charge amount must be positive (got %d cents)", artworkCents)
	}
	// The escrow's FLOW amount is derived from feeCents alone (see step 3),
	// so a zero fee — misconfigured platformFeeBps, or rounding down to zero
	// on a very cheap artwork — would open a zero-FLOW escrow. On chain that
	// aborts the escrow transaction (it requires a positive payment), and by
	// then the buyer would already have been charged via Stripe. Reject it
	// here, before Stripe or Pyth are touched, so nothing is charged.
	if feeCents <= 0 {
		return nil, fmt.Errorf("computed platform fee must be positive (artwork %d cents, fee %d bps -> %d cents)", artworkCents, feeBps, feeCents)
	}

	// 3. Convert only the platform fee to FLOW using the current Pyth oracle
	// price. The escrow does not hold the sale proceeds — it is a gas
	// reserve funded from the admin vault and returned to the ArtDrop vault,
	// sized to the 5% fee, not to the full artwork price.
	if s.oracle == nil {
		return nil, ErrOracleDisabled
	}
	pyth, err := s.oracle.Latest(ctx)
	if err != nil {
		if errors.Is(err, ErrPythStale) {
			return nil, ErrOracleStale
		}
		return nil, fmt.Errorf("read pyth price: %w", err)
	}
	if pyth.PriceUSD <= 0 {
		return nil, fmt.Errorf("pyth returned non-positive FLOW/USD price %f", pyth.PriceUSD)
	}
	flowAmount := float64(feeCents) / 100.0 / pyth.PriceUSD

	// 4. Create and confirm the Stripe PaymentIntent for the full artwork
	// price in USD. The buyer pays 100% of the artwork price; the platform
	// fee is ArtDrop's share of that amount, not an addition to it.
	if s.charge == nil {
		return nil, ErrStripeDisabled
	}
	intent, err := s.charge.CreateAndConfirm(ctx, studio.StripeChargeInput{
		AmountCents:     artworkCents,
		Currency:        "usd",
		CustomerID:      in.StripeCustomerID,
		PaymentMethodID: in.PaymentMethodID,
		IdempotencyKey:  in.IdempotencyKey,
		Metadata:        in.Metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("create stripe payment intent: %w", err)
	}

	// 5. The deployed legacy caller supplies a chip and keeps the original
	// charge+open behavior. A chipless caller creates a paid obligation; it is
	// opened later by operations once production assigns a provisioned chip.
	// fee-only gas reserve). The escrow is opened by the artdrop service
	// (via the EscrowCreator adapter), which owns the transaction submission
	// and the server-controlled escrow arguments.
	//
	// Branch on in.CertificateID (issue #107): a fresh purchase (zero) opens a
	// new escrow that mints a new certificate against EditionID; re-offering an
	// existing certificate whose prior escrow was Voided (non-zero, protocol
	// #185) re-escrows that certificate with no re-mint — the contract derives
	// the edition from the certificate itself. Both receive the identical
	// server-computed flowAmount above; only the escrow call and its
	// certificate-vs-edition argument differ.
	// sync=false: this only schedules the on-chain transaction, it does not
	// wait for it to confirm (unchanged behavior). job.ID is the wallet-api's
	// own async job UUID, available immediately regardless of whether the
	// transaction has run yet — it's what lets the audit record be traced
	// forward to the escrow the job eventually creates (see
	// PurchaseCharge.EscrowJobID; resolving the on-chain escrowId from the
	// job's Result is a separate step, not done here).
	// unlock_at, like amount, is computed server-side (issue #111), not
	// trusted from the client: it is the buyer's on-chain claim deadline, and
	// once now > unlock_at, releaseOnTimeout becomes permissionless and yanks
	// the escrow reserve to the ArtDrop vault. A client-set past/zero value
	// would close the buyer's claim window before they ever activate their
	// chip.
	serverUnlockAt := float64(s.now().Unix()) + s.claimWindowSeconds

	var escrowJobID string
	status := PurchaseStatusPaidPendingEscrow
	if in.ChipID != "" {
		if s.escrow == nil {
			return nil, ErrEscrowDisabled
		}
		var job *jobs.Job
		if in.CertificateID != 0 {
			job, _, err = s.escrow.ReEscrow(ctx, false, in.Buyer, in.Buyer, in.Seller, in.CertificateID, in.ChipID, serverUnlockAt, in.Nonce, flowAmount)
		} else {
			job, _, err = s.escrow.CreateEscrow(ctx, false, in.Buyer, in.Buyer, in.Seller, in.EditionID, in.ChipID, serverUnlockAt, in.Nonce, flowAmount)
		}
		if err != nil {
			return nil, fmt.Errorf("create escrow: %w", err)
		}
		if job != nil {
			escrowJobID = job.ID.String()
		}
		status = PurchaseStatusEscrowOpening
	}

	// 6. Persist the audit record with the server-computed values.
	charge := &PurchaseCharge{
		PurchaseID:          uuid.NewString(),
		Status:              status,
		UserID:              in.UserID,
		ArtworkKind:         string(in.ArtworkKind),
		ArtworkID:           in.ArtworkID,
		AmountCents:         artworkCents,
		PlatformFeeCents:    feeCents,
		Currency:            "usd",
		FlowAmount:          flowAmount,
		FlowPriceUSD:        pyth.PriceUSD,
		StripePaymentIntent: intent.ID,
		Buyer:               in.Buyer,
		Seller:              in.Seller,
		EditionID:           in.EditionID,
		ChipID:              in.ChipID,
		UnlockAt:            serverUnlockAt,
		Nonce:               in.Nonce,
		Metadata:            in.Metadata,
		EscrowJobID:         escrowJobID,
	}
	if err := s.store.CreatePurchaseCharge(charge); err != nil {
		if isDuplicateKeyError(err) {
			return nil, ErrChargeAlreadyRecorded
		}
		log.WithFields(log.Fields{
			"userId":              in.UserID,
			"artworkKind":         in.ArtworkKind,
			"artworkId":           in.ArtworkID,
			"stripePaymentIntent": intent.ID,
			"error":               err,
		}).Error("failed to record purchase charge")
		return nil, ErrChargeRecordFailed
	}

	return charge, nil
}

// OpenEscrow binds a provisioned chip to exactly one already-paid obligation
// and enqueues CreateEscrow. ClaimEscrowOpening is a conditional SQL update,
// so concurrent operators cannot submit two jobs for the same purchase.
func (s *ServiceImpl) OpenEscrow(ctx context.Context, in OpenEscrowInput) (*PurchaseCharge, error) {
	if in.PurchaseID == "" || in.ChipID == "" || in.IdempotencyKey == "" {
		return nil, fmt.Errorf("%w: purchase id, chip id and idempotency key are required", ErrInvalidOpenEscrowInput)
	}
	if s.store == nil {
		return nil, ErrChargeRecordFailed
	}
	charge, err := s.store.GetPurchaseCharge(ctx, in.PurchaseID)
	if err != nil {
		return nil, fmt.Errorf("read paid purchase: %w", err)
	}
	if charge == nil {
		return nil, ErrPurchaseNotFound
	}
	if charge.Status == PurchaseStatusEscrowOpening && charge.EscrowIdempotencyKey == in.IdempotencyKey && charge.EscrowJobID != "" {
		return charge, nil
	}
	if charge.Status != PurchaseStatusPaidPendingEscrow || charge.EscrowJobID != "" {
		return nil, ErrPurchaseNotOpenable
	}
	if s.chips == nil {
		return nil, ErrEscrowDisabled
	}
	ok, err := s.chips.IsProvisioned(ctx, in.ChipID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrChipUnavailable, err)
	}
	if !ok {
		return nil, ErrChipNotProvisioned
	}
	claimed, err := s.store.ClaimEscrowOpening(ctx, in.PurchaseID, in.ChipID, in.Nonce, in.IdempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("claim paid purchase: %w", err)
	}
	if !claimed {
		return nil, ErrPurchaseNotOpenable
	}
	if s.escrow == nil {
		_ = s.store.ResetEscrowOpening(ctx, in.PurchaseID)
		return nil, ErrEscrowDisabled
	}
	job, _, err := s.escrow.CreateEscrow(ctx, false, charge.Buyer, charge.Buyer, charge.Seller, charge.EditionID, in.ChipID, charge.UnlockAt, in.Nonce, charge.FlowAmount)
	if err != nil {
		// The adapter may have accepted the asynchronous job before returning an
		// error. Keep the durable claim fail-closed rather than clearing it and
		// risking a second CreateEscrow/certificate on retry.
		return nil, fmt.Errorf("%w: %v", ErrEscrowUnavailable, err)
	}
	if job == nil {
		return nil, fmt.Errorf("%w: create escrow returned no job", ErrEscrowUnavailable)
	}
	if err := s.store.SetEscrowJobID(ctx, in.PurchaseID, job.ID.String()); err != nil {
		return nil, ErrChargeRecordFailed
	}
	charge.ChipID, charge.Nonce, charge.EscrowJobID, charge.Status = in.ChipID, in.Nonce, job.ID.String(), PurchaseStatusEscrowOpening
	return charge, nil
}

// readArtworkPrice reads the server-side price of the artwork from Mongo,
// selecting the collection by artwork kind.
func (s *ServiceImpl) readArtworkPrice(ctx context.Context, in CreatePurchaseChargeInput) (*datastoremongo.ArtworkPrice, error) {
	var (
		price *datastoremongo.ArtworkPrice
		err   error
	)
	switch in.ArtworkKind {
	case ArtworkEdition:
		price, err = s.prices.GetEditionPrice(ctx, in.ArtworkID)
	case ArtworkPainting:
		price, err = s.prices.GetPaintingPrice(ctx, in.ArtworkID)
	default:
		return nil, fmt.Errorf("unsupported artwork kind %q", in.ArtworkKind)
	}
	if err != nil {
		if errors.Is(err, datastoremongo.ErrArtworkNotFound) {
			return nil, ErrArtworkNotFound
		}
		if errors.Is(err, datastoremongo.ErrArtworkPriceMissing) {
			return nil, ErrArtworkPriceMissing
		}
		return nil, fmt.Errorf("read artwork price: %w", err)
	}
	return price, nil
}

// isDuplicateKeyError reports whether err is a unique-constraint violation.
// GORM translates driver errors to gorm.ErrDuplicatedKey on some drivers, but
// the SQLite driver used in tests surfaces the raw "UNIQUE constraint failed"
// message, so we match both.
func isDuplicateKeyError(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "duplicate key")
}
