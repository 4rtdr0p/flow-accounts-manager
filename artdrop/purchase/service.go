package purchase

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop/studio"
	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
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

// Service lists all functionality provided by the purchase service.
type Service interface {
	// CreatePurchaseCharge charges a buyer's purchase and opens the escrow:
	// it reads the artwork price from Mongo, applies the configured platform
	// fee, converts the total to FLOW via the Pyth oracle, creates and
	// confirms a Stripe PaymentIntent, opens the on-chain escrow with the
	// server-computed FLOW amount, and persists the audit record. Every
	// amount is computed server-side; the client only identifies the artwork,
	// the parties and the payment details.
	CreatePurchaseCharge(ctx context.Context, in CreatePurchaseChargeInput) (*PurchaseCharge, error)
}

// ServiceImpl implements the purchase Service.
type ServiceImpl struct {
	store              Store
	prices             ArtworkPriceReader
	oracle             PriceOracle
	charge             ChargeClient
	escrow             EscrowCreator
	platformFeeBps     int
	shippingRatePerUSD float64
}

// NewService initiates a new purchase service wired for the full charge flow.
// Any of the optional deps may be nil; the corresponding step reports its
// disabled error. platformFeeBps is the platform fee in basis points applied
// on top of the artwork price (see Config.PurchasePlatformFeeBasisPoints).
func NewService(store Store, prices ArtworkPriceReader, oracle PriceOracle, charge ChargeClient, escrow EscrowCreator, platformFeeBps int) Service {
	return &ServiceImpl{
		store:          store,
		prices:         prices,
		oracle:         oracle,
		charge:         charge,
		escrow:         escrow,
		platformFeeBps: platformFeeBps,
	}
}

// CreatePurchaseCharge charges a buyer's purchase and opens the escrow
// end-to-end. The amount is never trusted from the client: it is computed from
// the artwork price in Mongo, the configured platform fee, and the current
// FLOW/USD price from the Pyth oracle.
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
	if in.ChipID == "" {
		return nil, fmt.Errorf("chip id is required")
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

	// 2. Apply the configured platform fee (in basis points) on top of the
	// artwork price. The fee is server-configured, never client-supplied.
	feeBps := s.platformFeeBps
	if feeBps <= 0 {
		feeBps = 0
	}
	artworkCents := int64(math.Round(artworkPrice.PriceUSD * 100))
	feeCents := int64(math.Round(float64(artworkCents) * float64(feeBps) / 10000))
	amountCents := artworkCents + feeCents
	if amountCents <= 0 {
		return nil, fmt.Errorf("computed charge amount must be positive (got %d cents)", amountCents)
	}

	// 3. Convert the total USD to FLOW using the current Pyth oracle price.
	// The escrow is funded in FLOW, so the on-chain amount must be derived
	// from the same total the buyer is charged in USD.
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
	flowAmount := float64(amountCents) / 100.0 / pyth.PriceUSD

	// 4. Create and confirm the Stripe PaymentIntent for the USD amount.
	if s.charge == nil {
		return nil, ErrStripeDisabled
	}
	intent, err := s.charge.CreateAndConfirm(ctx, studio.StripeChargeInput{
		AmountCents:     amountCents,
		Currency:        "usd",
		CustomerID:      in.StripeCustomerID,
		PaymentMethodID: in.PaymentMethodID,
		IdempotencyKey:  in.IdempotencyKey,
		Metadata:        in.Metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("create stripe payment intent: %w", err)
	}

	// 5. Open the on-chain escrow with the server-computed FLOW amount. The
	// escrow is created by the artdrop service (via the EscrowCreator
	// adapter), which owns the transaction submission and the
	// server-controlled escrow arguments.
	if s.escrow == nil {
		return nil, ErrEscrowDisabled
	}
	_, _, err = s.escrow.CreateEscrow(ctx, false, in.Buyer, in.Buyer, in.Seller, in.EditionID, in.ChipID, in.UnlockAt, in.Nonce, flowAmount)
	if err != nil {
		return nil, fmt.Errorf("create escrow: %w", err)
	}

	// 6. Persist the audit record with the server-computed values.
	charge := &PurchaseCharge{
		UserID:              in.UserID,
		ArtworkKind:         string(in.ArtworkKind),
		ArtworkID:           in.ArtworkID,
		AmountCents:         amountCents,
		PlatformFeeCents:    feeCents,
		Currency:            "usd",
		FlowAmount:          flowAmount,
		FlowPriceUSD:        pyth.PriceUSD,
		StripePaymentIntent: intent.ID,
		Buyer:               in.Buyer,
		Seller:              in.Seller,
		EditionID:           in.EditionID,
		ChipID:              in.ChipID,
		UnlockAt:            in.UnlockAt,
		Nonce:               in.Nonce,
		Metadata:            in.Metadata,
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
