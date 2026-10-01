package purchase

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop/studio"
	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/google/uuid"
	"github.com/onflow/flow-go-sdk"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// ErrChargeAlreadyRecorded is returned when a charge with the same Stripe
// payment intent has already been recorded. This is the idempotency guard at
// the audit layer: a double-click or a network retry must not create a
// duplicate charge record.
var ErrChargeAlreadyRecorded = errors.New("charge already recorded")

// ErrPaymentNotSucceeded is returned when Stripe has not completed the
// automatic off-session PaymentIntent. No purchase obligation is recorded in
// that case.
var ErrPaymentNotSucceeded = errors.New("payment not succeeded")

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
// retain the durable reservation: a failed local write cannot undo Stripe.
var ErrChargeRecordFailed = errors.New("failed to record charge")
var ErrPurchaseNotFound = errors.New("paid purchase not found")
var ErrPurchaseNotOpenable = errors.New("purchase is not awaiting escrow")
var ErrChipNotProvisioned = errors.New("chip is not provisioned")
var ErrChipUnavailable = errors.New("chip lookup is unavailable")
var ErrInvalidOpenEscrowInput = errors.New("invalid open escrow request")
var ErrEscrowUnavailable = errors.New("escrow queue is unavailable")

// ErrEditionNotFound is returned when a fresh purchase's EditionID does not
// exist on-chain (issue #135's seller/edition ownership check).
var ErrEditionNotFound = errors.New("edition not found")

// ErrSellerMismatch is returned when a fresh purchase's client-supplied
// Seller does not match EditionID's on-chain artist (issue #135). Nothing on
// the contract side rejects an arbitrary seller for a fresh CreateEscrow
// call, so this must be enforced here, before the buyer is charged.
var ErrSellerMismatch = errors.New("seller does not match the edition's artist")

// ErrEditionArtistUnavailable is returned when the edition-artist lookup
// required by issue #135's seller check is not configured. This fails
// closed: a fresh purchase's seller is never trusted without being verified
// against the edition's on-chain artist.
var ErrEditionArtistUnavailable = errors.New("edition artist lookup is unavailable")

// Service lists all functionality provided by the purchase service.
type Service interface {
	GetPurchaseRecovery(context.Context, string) (*PurchaseRecoveryResponse, error)
	GetIntentRecovery(context.Context, string, string) (*PurchaseRecoveryResponse, error)
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
	store  Store
	prices ArtworkPriceReader
	oracle PriceOracle
	charge ChargeClient
	escrow EscrowCreator
	chips  ChipReader
	// editions resolves EditionID's on-chain artist for the fresh-mint seller
	// check (issue #135). See EditionArtistReader's doc comment.
	editions       EditionArtistReader
	platformFeeBps int
	// jobStates reads a wallet job's terminal state for OpenEscrow's
	// failed-claim release (see JobStateReader). Nil keeps the historical
	// fail-closed behavior: an ESCROW_OPENING purchase is never re-openable.
	jobStates JobStateReader

	// claimWindowSeconds is the buyer's on-chain claim deadline window (issue
	// #111): the escrow's unlock_at is computed as now() + claimWindowSeconds,
	// never accepted from the client. See Config.EscrowClaimWindowSeconds.
	claimWindowSeconds float64
	// now is the current-time source, overridable in tests so unlock_at can be
	// pinned to a known value; defaults to time.Now.
	now func() time.Time
}

// FlowAmountForArtworkPrice computes the FLOW reserve from the artwork price
// and configured fee. Shipping is intentionally absent: it is a Stripe-only
// delivery charge and cannot affect a certificate's reserve.
func FlowAmountForArtworkPrice(priceUSD float64, platformFeeBps int, oracle PriceOracle) (float64, error) {
	artworkCents := int64(math.Round(priceUSD * 100))
	feeCents := int64(math.Round(float64(artworkCents) * float64(max(platformFeeBps, 0)) / 10000))
	if artworkCents <= 0 || feeCents <= 0 {
		return 0, fmt.Errorf("computed platform fee must be positive")
	}
	if oracle == nil {
		return 0, ErrOracleDisabled
	}
	pyth, err := oracle.Latest(context.Background())
	if err != nil {
		return 0, err
	}
	if pyth.PriceUSD <= 0 {
		return 0, fmt.Errorf("FLOW/USD price must be positive")
	}
	return float64(feeCents) / 100.0 / pyth.PriceUSD, nil
}

// NewService initiates a new purchase service wired for the full charge flow.
// Any of the optional deps may be nil; the corresponding step reports its
// disabled error — editions included: a nil EditionArtistReader fails a fresh
// purchase closed with ErrEditionArtistUnavailable rather than skip the
// seller/edition-artist check (issue #135). platformFeeBps is the platform fee in basis points, applied
// as ArtDrop's share of the artwork price rather than a surcharge added on top
// of it (see Config.PurchasePlatformFeeBasisPoints). claimWindowSeconds is the
// server-computed unlock_at window (issue #111; see
// Config.EscrowClaimWindowSeconds) — the buyer's on-chain claim deadline is
// now() + claimWindowSeconds, never client-supplied. jobStates may be nil;
// that only disables the terminally-failed-claim release in OpenEscrow, which
// then keeps its historical fail-closed ErrPurchaseNotOpenable.
func NewService(store Store, prices ArtworkPriceReader, oracle PriceOracle, charge ChargeClient, escrow EscrowCreator, chips ChipReader, editions EditionArtistReader, jobStates JobStateReader, platformFeeBps int, claimWindowSeconds float64) Service {
	return &ServiceImpl{
		store:              store,
		prices:             prices,
		oracle:             oracle,
		charge:             charge,
		escrow:             escrow,
		chips:              chips,
		editions:           editions,
		jobStates:          jobStates,
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
func (s *ServiceImpl) CreatePurchaseCharge(ctx context.Context, in CreatePurchaseChargeInput) (result *PurchaseCharge, resultErr error) {
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
	if in.ShippingCents < 0 {
		return nil, fmt.Errorf("shipping cents must not be negative")
	}
	if in.Buyer == "" {
		return nil, fmt.Errorf("buyer is required")
	}
	if in.Seller == "" {
		return nil, fmt.Errorf("seller is required")
	}

	if !validIntent(in.IdempotencyKey) {
		return nil, recoveryError(409, "LEGACY_INTENT_REQUIRES_REVIEW", in.IdempotencyKey)
	}
	durable, ok := s.store.(DurableStore)
	if !ok {
		return nil, recoveryError(503, "RESERVATION_UNAVAILABLE", in.IdempotencyKey)
	}
	hash := requestHash(in)
	reservation, won, err := durable.Reserve(ctx, in, hash)
	if err != nil {
		return nil, recoveryError(503, "RESERVATION_UNAVAILABLE", in.IdempotencyKey)
	}
	if !won {
		return replayIntent(reservation, hash)
	}
	// Consult the indexed obligation too: losing a reservation link must not
	// turn positive purchase evidence into permission to call Stripe again.
	_, existing, lookupErr := durable.ReadRecovery(ctx, in.UserID, in.IdempotencyKey)
	if lookupErr != nil || existing != nil {
		e := recoveryError(503, "LOOKUP_UNAVAILABLE", in.IdempotencyKey)
		if existing != nil {
			e.Status, e.Code, e.PurchaseID = 409, "RESULT_BODY_EXPIRED", existing.PurchaseID
			if handlers.Value(existing.RequestHash) != hash || handlers.Value(existing.HashVersion) != 1 {
				e.Code = "IDEMPOTENCY_TERMS_CONFLICT"
			}
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = durable.FailIntent(cleanup, reservation, true, e)
		return nil, e
	}
	effect := false
	defer func() {
		if resultErr == nil {
			return
		}
		e := recoveryError(503, "RECONCILIATION_REQUIRED", in.IdempotencyKey)
		e.cause = resultErr
		if !effect {
			e.Code = "NOT_CHARGED"
			e.Message = "This execution stopped before calling the payment provider."
		}
		var specific *RecoveryError
		if effect && errors.As(resultErr, &specific) && specific.Code == "PAYMENT_INTENT_CONFLICT" {
			e.Code, e.Status = specific.Code, specific.Status
		}
		// Persist uncertainty even when the HTTP request was cancelled. Bounded and
		// independent of that cancellation; failure never frees the reservation.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := durable.FailIntent(cleanup, reservation, effect, e); err != nil {
			e.Code, e.Message = "RECONCILIATION_REQUIRED", "Result persistence unavailable; keep this identity for review."
			_ = durable.FailIntent(cleanup, reservation, true, e)
		}
		resultErr = e
	}()

	// 1. Read the artwork price from Mongo. The price is in whole dollars
	// (USD) as stored by Payload CMS.
	if s.prices == nil {
		return nil, ErrPricingDisabled
	}
	artworkPrice, err := s.readArtworkPrice(ctx, in)
	if err != nil {
		return nil, err
	}

	// 1b. A fresh purchase (CertificateID == 0) is about to mint a brand-new
	// certificate against EditionID and pay its escrow reserve to Seller —
	// both client-supplied, separately from the ArtworkID priced above, and
	// neither is checked against the artwork actually charged anywhere else
	// (issue #135). The re-escrow branch below (CertificateID != 0) is not
	// touched here: it re-offers an EXISTING certificate, and the artdrop
	// plugin's re-escrow amount resolver already verifies on-chain that the
	// certificate belongs to Seller before this method is ever reached.
	//
	// Confirm that Seller is really EditionID's on-chain artist. This closes
	// the "otro seller" half of #135: create_escrow.cdc takes seller as a
	// free, contract-unvalidated argument, so nothing on-chain stops a
	// mismatched seller from receiving a freshly-minted certificate's escrow.
	if in.CertificateID == 0 {
		if s.editions == nil {
			return nil, ErrEditionArtistUnavailable
		}
		artist, err := s.editions.GetEditionArtist(ctx, in.EditionID)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrEditionNotFound, err)
		}
		if artist == "" || !sameFlowAddress(artist, in.Seller) {
			return nil, ErrSellerMismatch
		}
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
	if artworkCents <= 0 || in.ShippingCents > math.MaxInt64-artworkCents {
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
	if pyth == nil || pyth.PriceUSD <= 0 || math.IsNaN(pyth.PriceUSD) || math.IsInf(pyth.PriceUSD, 0) {
		return nil, fmt.Errorf("pyth returned invalid FLOW/USD price")
	}
	flowAmount := float64(feeCents) / 100.0 / pyth.PriceUSD
	if flowAmount <= 0 || math.IsInf(flowAmount, 0) || math.IsNaN(flowAmount) {
		return nil, fmt.Errorf("invalid FLOW reserve")
	}

	// 4. Create and confirm one Stripe PaymentIntent for the artwork plus the
	// carrier-calculated shipping amount. The platform fee and FLOW reserve
	// above remain based on artworkCents alone.
	if s.charge == nil {
		return nil, ErrStripeDisabled
	}
	// Set the conservative local flag before CAS: an ambiguous SQL acknowledgement
	// must never be reported as proof of not_charged.
	effect = true
	if err := durable.MarkEffect(ctx, reservation); err != nil {
		return nil, err
	}
	intent, err := s.charge.CreateAndConfirm(ctx, studio.StripeChargeInput{
		AmountCents:     artworkCents + in.ShippingCents,
		Currency:        "usd",
		CustomerID:      in.StripeCustomerID,
		PaymentMethodID: in.PaymentMethodID,
		IdempotencyKey:  handlers.Value(reservation.ProviderKey),
		Metadata:        in.Metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("create stripe payment intent: %w", err)
	}

	if intent == nil || intent.ID == "" {
		return nil, fmt.Errorf("stripe returned no payment intent identity")
	}
	observedAt := time.Now().UTC()
	if err := durable.ObservePayment(ctx, reservation, intent.ID, intent.Status, observedAt); err != nil {
		if isDuplicateKeyError(err) {
			return nil, recoveryError(409, "PAYMENT_INTENT_CONFLICT", in.IdempotencyKey)
		}
		return nil, err
	}
	previous, err := durable.PurchaseByPI(ctx, intent.ID)
	if err != nil {
		return nil, err
	}
	if previous != nil {
		// Normal replay never reaches Stripe. Unexpected PI reuse is accepted only
		// with complete matching owner/identity/hash evidence, before any escrow.
		if previous.UserID != in.UserID || handlers.Value(previous.ChargeIntentID) != in.IdempotencyKey ||
			handlers.Value(previous.ChargeOperation) != handlers.PurchaseOperation || handlers.Value(previous.RequestHash) != hash || handlers.Value(previous.HashVersion) != 1 {
			return nil, recoveryError(409, "PAYMENT_INTENT_CONFLICT", in.IdempotencyKey)
		}
		if err := durable.CompleteCharge(ctx, reservation, previous); err != nil {
			return nil, err
		}
		return previous, nil
	}
	if intent.Status != "succeeded" {
		return nil, fmt.Errorf("%w: observed status %q", ErrPaymentNotSucceeded, intent.Status)
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
		PurchaseID:     uuid.NewString(),
		ChargeIntentID: handlers.Ptr(in.IdempotencyKey), ChargeOperation: handlers.Ptr(handlers.PurchaseOperation),
		RequestHash: handlers.Ptr(hash), HashVersion: handlers.Ptr(1), StripeCustomerID: handlers.Ptr(in.StripeCustomerID),
		StripeStatusObserved: handlers.Ptr(intent.Status), StripeObservedAt: &observedAt,
		Status:              status,
		UserID:              in.UserID,
		ArtworkKind:         string(in.ArtworkKind),
		ArtworkID:           in.ArtworkID,
		AmountCents:         artworkCents,
		ShippingCents:       in.ShippingCents,
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
	if in.CertificateID != 0 {
		charge.CertificateID = handlers.Ptr(in.CertificateID)
	}
	if err := durable.CompleteCharge(ctx, reservation, charge); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrChargeRecordFailed, err)
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
		// A terminally FAILED escrow job reverted atomically — nothing was
		// created on-chain — so with no escrowId ever recorded the claim is
		// dead weight that would keep this purchase 409-locked forever (a
		// Flow transaction expiry strands exactly this state). Release it and
		// fall through to a fresh claim; every other state stays fail-closed.
		released, err := s.releaseFailedEscrowClaim(ctx, charge)
		if err != nil {
			return nil, err
		}
		if !released {
			return nil, ErrPurchaseNotOpenable
		}
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

// releaseFailedEscrowClaim releases a purchase's ESCROW_OPENING claim when the
// wallet job that holds it terminally FAILED. A FAILED Cadence job reverts
// atomically — no escrow, no certificate exists on-chain — so the claim is
// unrecoverable: resubmitting the dead job can never succeed, yet without this
// release the purchase answers ErrPurchaseNotOpenable forever.
//
// Fail-closed everywhere else:
//   - only an ESCROW_OPENING claim with a recorded job can be released (a
//     claim with no job may have a live in-flight submission);
//   - only a terminal FAILED state releases (pending, accepted or errored
//     jobs may still seal, and a second CreateEscrow would double-mint);
//   - a job-state read error is returned (wrapped in ErrEscrowUnavailable so
//     callers see a retryable 503, not a misleading permanent 409);
//   - the store reset is conditional on the row still holding that exact job
//     id, so a concurrent retry that already re-opened is never clobbered.
func (s *ServiceImpl) releaseFailedEscrowClaim(ctx context.Context, charge *PurchaseCharge) (bool, error) {
	if charge.Status != PurchaseStatusEscrowOpening || charge.EscrowJobID == "" || s.jobStates == nil {
		return false, nil
	}
	failed, err := s.jobStates.IsJobFailed(ctx, charge.EscrowJobID)
	if err != nil {
		return false, fmt.Errorf("%w: read escrow job %s: %v", ErrEscrowUnavailable, charge.EscrowJobID, err)
	}
	if !failed {
		return false, nil
	}
	released, err := s.store.ResetEscrowOpeningAfterFailedJob(ctx, charge.PurchaseID, charge.EscrowJobID)
	if err != nil {
		return false, fmt.Errorf("reset failed escrow claim for purchase %s: %w", charge.PurchaseID, err)
	}
	if released {
		log.WithFields(log.Fields{
			"purchaseId":  charge.PurchaseID,
			"failedJobId": charge.EscrowJobID,
			"chipId":      charge.ChipID,
		}).Warn("released terminally failed escrow claim; obligation is openable again")
	}
	return released, nil
}

// readArtworkPrice reads the server-side price of the artwork from Mongo,
// selecting the collection by artwork kind.
//
// For a fresh edition purchase (CertificateID == 0, issue #135), the Mongo
// lookup key is the escrowed EditionID's decimal string, not the client's
// ArtworkID: editionId, buyer and seller arrive separately from the client,
// and nothing else ties the price actually charged to the edition a fresh
// CreateEscrow call mints a certificate against. Payload's editions
// collection is keyed by that same decimal edition id — see the artdrop
// plugin's re-escrow amount resolver, which derives an edition's price the
// same way (purchaseStore.GetEditionPrice(strconv.FormatUint(editionID,
// 10))) from a certificate's on-chain edition id, with no artwork id in
// play at all. Deriving the key here closes the mismatch by construction:
// the buyer is always charged the price of the edition that gets escrowed,
// regardless of what ArtworkID the client separately sends. ArtworkID is
// still recorded on the audit row for display/reconciliation, and the
// re-escrow branch (CertificateID != 0) is intentionally left on the
// client's ArtworkID — that branch never uses EditionID for the escrow call
// and its certificate is already ownership-checked on-chain before this
// method runs (see CreatePurchaseCharge's 1b).
//
// Painting purchases keep using ArtworkID as before: no equivalent
// EditionID<->Payload-id convention for paintings is established anywhere
// in this codebase to derive from (see issue #135's report).
func (s *ServiceImpl) readArtworkPrice(ctx context.Context, in CreatePurchaseChargeInput) (*datastoremongo.ArtworkPrice, error) {
	var (
		price *datastoremongo.ArtworkPrice
		err   error
	)
	switch in.ArtworkKind {
	case ArtworkEdition:
		editionArtworkID := in.ArtworkID
		if in.CertificateID == 0 {
			editionArtworkID = strconv.FormatUint(in.EditionID, 10)
		}
		price, err = s.prices.GetEditionPrice(ctx, editionArtworkID)
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

// sameFlowAddress compares two Flow address strings for equality regardless
// of "0x" prefix, case, or missing leading zeros — flow.Address.Hex() always
// normalizes to the same lowercase, zero-padded form, so this avoids
// rejecting a legitimately-matching seller over formatting alone (issue
// #135's seller check must not be defeated NOR falsely tripped by format
// differences between the on-chain artist and the client's Seller string).
func sameFlowAddress(a, b string) bool {
	return flow.HexToAddress(a).Hex() == flow.HexToAddress(b).Hex()
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
	return strings.Contains(msg, "unique constraint") || strings.Contains(msg, "duplicate key") || strings.Contains(msg, "duplicate entry")
}
