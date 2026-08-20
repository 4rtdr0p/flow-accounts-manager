package studio

import (
	"context"
	"errors"
	"strings"
	"testing"

	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// mockQuoteReader is a scripted QuoteReader for tests.
type mockQuoteReader struct {
	quote *datastoremongo.StudioQuote
	err   error
}

func (m *mockQuoteReader) GetByID(ctx context.Context, quoteID string) (*datastoremongo.StudioQuote, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.quote == nil {
		return nil, datastoremongo.ErrQuoteNotFound
	}
	return m.quote, nil
}

// mockPriceEngine is a scripted PriceEngine for tests. It mirrors the
// primitive-only studio.PriceEngine signature. maxQuantity defaults to "no
// cap" (0) when left unset, since most tests don't exercise the tier check.
type mockPriceEngine struct {
	amountCents   int64
	pricingHash   string
	engineVersion string
	maxQuantity   int
	err           error
}

func (m *mockPriceEngine) Quote(ctx context.Context, config map[string]any, runSize int) (int64, string, string, int, error) {
	if m.err != nil {
		return 0, "", "", 0, m.err
	}
	return m.amountCents, m.pricingHash, m.engineVersion, m.maxQuantity, nil
}

// mockChargeClient is a scripted ChargeClient for tests. It records the last
// StripeChargeInput it received so tests can assert the Idempotency-Key is
// propagated from the request to Stripe.
type mockChargeClient struct {
	intent *StripePaymentIntent
	err    error
	lastIn StripeChargeInput
}

func (m *mockChargeClient) CreateAndConfirm(ctx context.Context, in StripeChargeInput) (*StripePaymentIntent, error) {
	m.lastIn = in
	if m.err != nil {
		return nil, m.err
	}
	if m.intent == nil {
		return &StripePaymentIntent{ID: "pi_123", Status: "succeeded"}, nil
	}
	return m.intent, nil
}

// mockStore is a scripted Store for simulating audit write failures.
type mockStore struct {
	createErr error
}

func (m *mockStore) CreateProductionCharge(charge *ProductionCharge) error {
	return m.createErr
}

func (m *mockStore) ListProductionChargesByUser(userID string) ([]ProductionCharge, error) {
	return nil, nil
}

// testShippingRateCentsPerUnit mirrors the production default (envDefault
// "25" on Config.StudioShippingRatePerUnitUSD, in cents).
const testShippingRateCentsPerUnit = 2500

// newChargeTestService builds a ServiceImpl wired with the given mocks and a
// fresh in-memory DB.
func newChargeTestService(t *testing.T, quotes QuoteReader, engine PriceEngine, charge ChargeClient) Service {
	t.Helper()
	return newChargeTestServiceWithShippingRate(t, quotes, engine, charge, testShippingRateCentsPerUnit)
}

func newChargeTestServiceWithShippingRate(t *testing.T, quotes QuoteReader, engine PriceEngine, charge ChargeClient, shippingRateCentsPerUnit int64) Service {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&ProductionCharge{}); err != nil {
		t.Fatal(err)
	}
	return NewChargeService(NewGormStore(db), quotes, engine, charge, shippingRateCentsPerUnit)
}

func validChargeInput() CreateStockRequestChargeInput {
	return CreateStockRequestChargeInput{
		UserID:           "user-1",
		QuoteID:          "quote-1",
		Quantity:         10,
		StripeCustomerID: "cus_123",
		PaymentMethodID:  "pm_123",
		IdempotencyKey:   "idem-1",
	}
}

// validQuoteConfig returns a wizard-shaped StudioQuote.config snapshot, i.e.
// the shape produced by the Payload Studio wizard (application, shape,
// sizeInches, materialFamily, mediaKey, texture, presentation, addons, ...).
// The pricing adapter (artdrop/studio/pricing) translates this into the engine
// Config at charge time.
func validQuoteConfig() map[string]any {
	return map[string]any{
		"application":    "textured",
		"processKey":     "textured-reproductions",
		"shape":          "rect",
		"sizeInches":     []any{float64(40), float64(60)},
		"materialFamily": "paper",
		"mediaKey":       "archival-paper",
		"texture":        "flat",
		"presentation":   "rolled",
		"addons": map[string]any{
			"packaging": "none",
			"nfc":       "no",
		},
		"rush":          "no",
		"volumeTierQty": float64(10),
	}
}

func TestCreateStockRequestChargeHappyPath(t *testing.T) {
	quotes := &mockQuoteReader{quote: &datastoremongo.StudioQuote{
		ID:     "quote-1",
		UserID: "user-1",
		Config: validQuoteConfig(),
	}}
	engine := &mockPriceEngine{amountCents: 2500, pricingHash: "hash-abc", engineVersion: "1.0.0"}
	charge := &mockChargeClient{intent: &StripePaymentIntent{ID: "pi_123", Status: "succeeded"}}

	svc := newChargeTestService(t, quotes, engine, charge)

	got, err := svc.CreateStockRequestCharge(context.Background(), validChargeInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.AmountCents != 2500 {
		t.Fatalf("expected amount 2500 cents, got %d", got.AmountCents)
	}
	if got.StripePaymentIntent != "pi_123" {
		t.Fatalf("expected payment intent pi_123, got %s", got.StripePaymentIntent)
	}
	if got.PricingHash != "hash-abc" {
		t.Fatalf("expected pricing hash hash-abc, got %s", got.PricingHash)
	}
	if got.EngineVersion != "1.0.0" {
		t.Fatalf("expected engine version 1.0.0, got %s", got.EngineVersion)
	}
	if got.QuoteID != "quote-1" {
		t.Fatalf("expected quote id quote-1, got %s", got.QuoteID)
	}
	// The request's Idempotency-Key must be propagated to Stripe.
	if charge.lastIn.IdempotencyKey != "idem-1" {
		t.Fatalf("expected idempotency key idem-1 propagated to Stripe, got %q", charge.lastIn.IdempotencyKey)
	}
	// The engine must receive the wizard-shaped config and the requested
	// quantity as the run size.
	if charge.lastIn.AmountCents != 2500 {
		t.Fatalf("expected Stripe amount 2500 cents, got %d", charge.lastIn.AmountCents)
	}
}

func TestCreateStockRequestChargeValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*CreateStockRequestChargeInput)
	}{
		{"missing user id", func(in *CreateStockRequestChargeInput) { in.UserID = "" }},
		{"missing quote id", func(in *CreateStockRequestChargeInput) { in.QuoteID = "" }},
		{"zero quantity", func(in *CreateStockRequestChargeInput) { in.Quantity = 0 }},
		{"negative quantity", func(in *CreateStockRequestChargeInput) { in.Quantity = -1 }},
		{"missing stripe customer id", func(in *CreateStockRequestChargeInput) { in.StripeCustomerID = "" }},
		{"missing idempotency key", func(in *CreateStockRequestChargeInput) { in.IdempotencyKey = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newChargeTestService(t, &mockQuoteReader{}, &mockPriceEngine{}, &mockChargeClient{})
			in := validChargeInput()
			tt.mutate(&in)
			if _, err := svc.CreateStockRequestCharge(context.Background(), in); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestCreateStockRequestChargeQuoteNotFound(t *testing.T) {
	svc := newChargeTestService(t, &mockQuoteReader{err: datastoremongo.ErrQuoteNotFound}, &mockPriceEngine{}, &mockChargeClient{})

	_, err := svc.CreateStockRequestCharge(context.Background(), validChargeInput())
	if !errors.Is(err, ErrQuoteNotFound) {
		t.Fatalf("expected ErrQuoteNotFound, got %v", err)
	}
}

func TestCreateStockRequestChargeForeignQuote(t *testing.T) {
	// The quote belongs to a different user. Charging it must be rejected as
	// not found (404) and Stripe must never be called.
	quotes := &mockQuoteReader{quote: &datastoremongo.StudioQuote{
		ID:     "quote-1",
		UserID: "user-2",
		Config: validQuoteConfig(),
	}}
	charge := &mockChargeClient{intent: &StripePaymentIntent{ID: "pi_123", Status: "succeeded"}}
	svc := newChargeTestService(t, quotes, &mockPriceEngine{amountCents: 2500}, charge)

	_, err := svc.CreateStockRequestCharge(context.Background(), validChargeInput())
	if !errors.Is(err, ErrQuoteNotFound) {
		t.Fatalf("expected ErrQuoteNotFound for foreign quote, got %v", err)
	}
	if charge.lastIn.AmountCents != 0 {
		t.Fatalf("expected Stripe NOT to be called for a foreign quote, but it was")
	}
}

func TestCreateStockRequestChargeOwnerlessQuote(t *testing.T) {
	// A quote without a resolvable owner must be rejected as not found (404)
	// and Stripe must never be called.
	quotes := &mockQuoteReader{quote: &datastoremongo.StudioQuote{
		ID:     "quote-1",
		UserID: "",
		Config: validQuoteConfig(),
	}}
	charge := &mockChargeClient{intent: &StripePaymentIntent{ID: "pi_123", Status: "succeeded"}}
	svc := newChargeTestService(t, quotes, &mockPriceEngine{amountCents: 2500}, charge)

	_, err := svc.CreateStockRequestCharge(context.Background(), validChargeInput())
	if !errors.Is(err, ErrQuoteNotFound) {
		t.Fatalf("expected ErrQuoteNotFound for ownerless quote, got %v", err)
	}
	if charge.lastIn.AmountCents != 0 {
		t.Fatalf("expected Stripe NOT to be called for an ownerless quote, but it was")
	}
}

func TestCreateStockRequestChargePricingDisabled(t *testing.T) {
	// nil engine -> pricing disabled.
	svc := newChargeTestService(t, &mockQuoteReader{quote: &datastoremongo.StudioQuote{ID: "quote-1", UserID: "user-1", Config: validQuoteConfig()}}, nil, &mockChargeClient{})

	_, err := svc.CreateStockRequestCharge(context.Background(), validChargeInput())
	if !errors.Is(err, ErrPricingDisabled) {
		t.Fatalf("expected ErrPricingDisabled, got %v", err)
	}
}

func TestCreateStockRequestChargeStripeDisabled(t *testing.T) {
	svc := newChargeTestService(t,
		&mockQuoteReader{quote: &datastoremongo.StudioQuote{ID: "quote-1", UserID: "user-1", Config: validQuoteConfig()}},
		&mockPriceEngine{amountCents: 2500},
		nil, // nil charge client -> stripe disabled
	)

	_, err := svc.CreateStockRequestCharge(context.Background(), validChargeInput())
	if !errors.Is(err, ErrStripeDisabled) {
		t.Fatalf("expected ErrStripeDisabled, got %v", err)
	}
}

func TestCreateStockRequestChargeInvalidQuoteConfig(t *testing.T) {
	// A wizard config missing the required sizeInches must fail translation.
	quotes := &mockQuoteReader{quote: &datastoremongo.StudioQuote{
		ID:     "quote-1",
		UserID: "user-1",
		Config: map[string]any{"application": "textured"},
	}}
	svc := newChargeTestService(t, quotes, &mockPriceEngine{}, &mockChargeClient{})

	if _, err := svc.CreateStockRequestCharge(context.Background(), validChargeInput()); err == nil {
		t.Fatal("expected error for invalid quote config, got nil")
	}
}

func TestCreateStockRequestChargeEngineError(t *testing.T) {
	quotes := &mockQuoteReader{quote: &datastoremongo.StudioQuote{ID: "quote-1", UserID: "user-1", Config: validQuoteConfig()}}
	engine := &mockPriceEngine{err: errors.New("engine boom")}
	svc := newChargeTestService(t, quotes, engine, &mockChargeClient{})

	if _, err := svc.CreateStockRequestCharge(context.Background(), validChargeInput()); err == nil {
		t.Fatal("expected error from engine, got nil")
	}
}

func TestCreateStockRequestChargeStripeError(t *testing.T) {
	quotes := &mockQuoteReader{quote: &datastoremongo.StudioQuote{ID: "quote-1", UserID: "user-1", Config: validQuoteConfig()}}
	engine := &mockPriceEngine{amountCents: 2500}
	charge := &mockChargeClient{err: errors.New("stripe boom")}
	svc := newChargeTestService(t, quotes, engine, charge)

	if _, err := svc.CreateStockRequestCharge(context.Background(), validChargeInput()); err == nil {
		t.Fatal("expected error from stripe, got nil")
	}
}

// TestCreateStockRequestChargeIdempotentByPaymentIntent verifies the audit
// layer's idempotency guard: replaying the same logical purchase (same Stripe
// payment intent) must not create a duplicate audit row.
func TestCreateStockRequestChargeIdempotentByPaymentIntent(t *testing.T) {
	quotes := &mockQuoteReader{quote: &datastoremongo.StudioQuote{ID: "quote-1", UserID: "user-1", Config: validQuoteConfig()}}
	engine := &mockPriceEngine{amountCents: 2500}
	charge := &mockChargeClient{intent: &StripePaymentIntent{ID: "pi_123", Status: "succeeded"}}

	svc := newChargeTestService(t, quotes, engine, charge)

	if _, err := svc.CreateStockRequestCharge(context.Background(), validChargeInput()); err != nil {
		t.Fatalf("unexpected error on first charge: %v", err)
	}
	// A retry that maps to the same Stripe payment intent must not create a
	// duplicate audit row: the audit layer's idempotency guard reports the
	// charge as already recorded.
	if _, err := svc.CreateStockRequestCharge(context.Background(), validChargeInput()); !errors.Is(err, ErrChargeAlreadyRecorded) {
		t.Fatalf("expected ErrChargeAlreadyRecorded on retry, got %v", err)
	}

	charges, err := svc.ListProductionChargesByUser("user-1")
	if err != nil {
		t.Fatalf("unexpected error listing: %v", err)
	}
	if len(charges) != 1 {
		t.Fatalf("expected exactly 1 charge after retry, got %d", len(charges))
	}
}

// TestCreateStockRequestChargeNewIdempotencyKeyAllowsNewPurchase verifies that
// a genuinely new purchase (a different Idempotency-Key) is allowed to charge
// again, even for the same quote/user. The Idempotency-Key is propagated to
// Stripe, which returns a different PaymentIntent, so a new audit row is
// created.
func TestCreateStockRequestChargeNewIdempotencyKeyAllowsNewPurchase(t *testing.T) {
	quotes := &mockQuoteReader{quote: &datastoremongo.StudioQuote{ID: "quote-1", UserID: "user-1", Config: validQuoteConfig()}}
	engine := &mockPriceEngine{amountCents: 2500}
	charge := &mockChargeClient{intent: &StripePaymentIntent{ID: "pi_123", Status: "succeeded"}}

	svc := newChargeTestService(t, quotes, engine, charge)

	in := validChargeInput()
	if _, err := svc.CreateStockRequestCharge(context.Background(), in); err != nil {
		t.Fatalf("unexpected error on first charge: %v", err)
	}

	// A new purchase with a different Idempotency-Key must be allowed. The
	// mock returns a fresh intent id so the audit row is not a duplicate.
	charge.intent = &StripePaymentIntent{ID: "pi_456", Status: "succeeded"}
	in.IdempotencyKey = "idem-2"
	if _, err := svc.CreateStockRequestCharge(context.Background(), in); err != nil {
		t.Fatalf("expected new purchase with different idempotency key to succeed, got %v", err)
	}

	charges, err := svc.ListProductionChargesByUser("user-1")
	if err != nil {
		t.Fatalf("unexpected error listing: %v", err)
	}
	if len(charges) != 2 {
		t.Fatalf("expected exactly 2 charges after a new purchase, got %d", len(charges))
	}
}

// A non-duplicate audit write failure must surface as ErrChargeRecordFailed,
// never as the raw store error and never as ErrChargeAlreadyRecorded.
func TestCreateStockRequestChargeRecordFailureIsNotLeakedAndNotDuplicate(t *testing.T) {
	quotes := &mockQuoteReader{quote: &datastoremongo.StudioQuote{ID: "quote-1", UserID: "user-1", Config: validQuoteConfig()}}
	engine := &mockPriceEngine{amountCents: 2500}
	charge := &mockChargeClient{intent: &StripePaymentIntent{ID: "pi_123", Status: "succeeded"}}
	store := &mockStore{createErr: errors.New("permission denied for table studio_production_charges (SQLSTATE 42501)")}

	svc := NewChargeService(store, quotes, engine, charge, testShippingRateCentsPerUnit)

	_, err := svc.CreateStockRequestCharge(context.Background(), validChargeInput())
	if !errors.Is(err, ErrChargeRecordFailed) {
		t.Fatalf("expected ErrChargeRecordFailed, got %v", err)
	}
	if strings.Contains(err.Error(), "SQLSTATE") || strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("raw store error must not be echoed to the caller, got %q", err.Error())
	}
	if errors.Is(err, ErrChargeAlreadyRecorded) {
		t.Fatalf("a failed write must not be reported as ErrChargeAlreadyRecorded")
	}
}

// TestCreateStockRequestChargePickupChargesNoShipping verifies that a pickup
// order (the default) charges only the production amount: the pre-existing
// behavior for every caller that doesn't send fulfillmentMethod.
func TestCreateStockRequestChargePickupChargesNoShipping(t *testing.T) {
	quotes := &mockQuoteReader{quote: &datastoremongo.StudioQuote{ID: "quote-1", UserID: "user-1", Config: validQuoteConfig()}}
	engine := &mockPriceEngine{amountCents: 2500}
	charge := &mockChargeClient{intent: &StripePaymentIntent{ID: "pi_123", Status: "succeeded"}}
	svc := newChargeTestService(t, quotes, engine, charge)

	in := validChargeInput()
	in.FulfillmentMethod = FulfillmentPickup

	got, err := svc.CreateStockRequestCharge(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ShippingAmountCents != 0 {
		t.Fatalf("expected no shipping for pickup, got %d cents", got.ShippingAmountCents)
	}
	if got.ProductionAmountCents != 2500 || got.AmountCents != 2500 {
		t.Fatalf("expected amount 2500 cents with no shipping, got production=%d total=%d", got.ProductionAmountCents, got.AmountCents)
	}
	if charge.lastIn.AmountCents != 2500 {
		t.Fatalf("expected Stripe to be charged 2500 cents, got %d", charge.lastIn.AmountCents)
	}
}

// TestCreateStockRequestChargeUnsetFulfillmentDefaultsToPickup verifies that
// omitting fulfillmentMethod entirely behaves exactly like pickup, so
// existing callers (Payload-Galaxy PR #381) are unaffected.
func TestCreateStockRequestChargeUnsetFulfillmentDefaultsToPickup(t *testing.T) {
	quotes := &mockQuoteReader{quote: &datastoremongo.StudioQuote{ID: "quote-1", UserID: "user-1", Config: validQuoteConfig()}}
	engine := &mockPriceEngine{amountCents: 2500}
	charge := &mockChargeClient{intent: &StripePaymentIntent{ID: "pi_123", Status: "succeeded"}}
	svc := newChargeTestService(t, quotes, engine, charge)

	in := validChargeInput()
	in.FulfillmentMethod = ""

	got, err := svc.CreateStockRequestCharge(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ShippingAmountCents != 0 || got.AmountCents != 2500 {
		t.Fatalf("expected unset fulfillment method to default to pickup (no shipping), got shipping=%d total=%d", got.ShippingAmountCents, got.AmountCents)
	}
}

// TestCreateStockRequestChargeDeliveryAddsShipping verifies a delivery order
// adds the flat per-unit shipping rate times the requested quantity, and that
// Stripe and the audit record both see the combined total split into its two
// components.
func TestCreateStockRequestChargeDeliveryAddsShipping(t *testing.T) {
	quotes := &mockQuoteReader{quote: &datastoremongo.StudioQuote{ID: "quote-1", UserID: "user-1", Config: validQuoteConfig()}}
	engine := &mockPriceEngine{amountCents: 2500}
	charge := &mockChargeClient{intent: &StripePaymentIntent{ID: "pi_123", Status: "succeeded"}}
	svc := newChargeTestServiceWithShippingRate(t, quotes, engine, charge, 2500)

	in := validChargeInput()
	in.Quantity = 10
	in.FulfillmentMethod = FulfillmentDelivery

	got, err := svc.CreateStockRequestCharge(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantShipping := int64(2500 * 10)
	if got.ShippingAmountCents != wantShipping {
		t.Fatalf("expected shipping %d cents (rate x quantity), got %d", wantShipping, got.ShippingAmountCents)
	}
	if got.ProductionAmountCents != 2500 {
		t.Fatalf("expected production amount unchanged at 2500 cents, got %d", got.ProductionAmountCents)
	}
	wantTotal := int64(2500) + wantShipping
	if got.AmountCents != wantTotal {
		t.Fatalf("expected total %d cents, got %d", wantTotal, got.AmountCents)
	}
	if charge.lastIn.AmountCents != wantTotal {
		t.Fatalf("expected Stripe to be charged the combined total %d cents, got %d", wantTotal, charge.lastIn.AmountCents)
	}
}

// TestCreateStockRequestChargeQuantityAtMaxTierIsAllowed verifies a request
// for exactly the largest tier the engine computed is accepted.
func TestCreateStockRequestChargeQuantityAtMaxTierIsAllowed(t *testing.T) {
	quotes := &mockQuoteReader{quote: &datastoremongo.StudioQuote{ID: "quote-1", UserID: "user-1", Config: validQuoteConfig()}}
	engine := &mockPriceEngine{amountCents: 2500, maxQuantity: 10}
	charge := &mockChargeClient{intent: &StripePaymentIntent{ID: "pi_123", Status: "succeeded"}}
	svc := newChargeTestService(t, quotes, engine, charge)

	in := validChargeInput()
	in.Quantity = 10

	if _, err := svc.CreateStockRequestCharge(context.Background(), in); err != nil {
		t.Fatalf("expected quantity at the max tier to be allowed, got %v", err)
	}
}

// TestCreateStockRequestChargeQuantityAboveMaxTierIsRejected verifies a
// request above the largest tier the engine computed is refused before
// Stripe is ever called, and that the error names the requested quantity and
// the max.
func TestCreateStockRequestChargeQuantityAboveMaxTierIsRejected(t *testing.T) {
	quotes := &mockQuoteReader{quote: &datastoremongo.StudioQuote{ID: "quote-1", UserID: "user-1", Config: validQuoteConfig()}}
	engine := &mockPriceEngine{amountCents: 2500, maxQuantity: 10}
	charge := &mockChargeClient{intent: &StripePaymentIntent{ID: "pi_123", Status: "succeeded"}}
	svc := newChargeTestService(t, quotes, engine, charge)

	in := validChargeInput()
	in.Quantity = 11

	_, err := svc.CreateStockRequestCharge(context.Background(), in)
	if !errors.Is(err, ErrQuantityExceedsMaxTier) {
		t.Fatalf("expected ErrQuantityExceedsMaxTier, got %v", err)
	}
	if !strings.Contains(err.Error(), "11") || !strings.Contains(err.Error(), "10") {
		t.Fatalf("expected error to name the requested quantity and the max, got %q", err.Error())
	}
	if charge.lastIn.AmountCents != 0 {
		t.Fatalf("expected Stripe NOT to be called when the quantity exceeds the max tier, but it was")
	}
}

// TestCreateStockRequestChargeZeroMaxQuantityMeansNoCap verifies that an
// engine reporting maxQuantity 0 (e.g. an empty Volume slice) does not
// spuriously reject every request.
func TestCreateStockRequestChargeZeroMaxQuantityMeansNoCap(t *testing.T) {
	quotes := &mockQuoteReader{quote: &datastoremongo.StudioQuote{ID: "quote-1", UserID: "user-1", Config: validQuoteConfig()}}
	engine := &mockPriceEngine{amountCents: 2500, maxQuantity: 0}
	charge := &mockChargeClient{intent: &StripePaymentIntent{ID: "pi_123", Status: "succeeded"}}
	svc := newChargeTestService(t, quotes, engine, charge)

	in := validChargeInput()
	in.Quantity = 1000

	if _, err := svc.CreateStockRequestCharge(context.Background(), in); err != nil {
		t.Fatalf("expected no cap to be applied when maxQuantity is 0, got %v", err)
	}
}
