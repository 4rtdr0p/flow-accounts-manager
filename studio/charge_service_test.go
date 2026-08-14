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
// primitive-only studio.PriceEngine signature.
type mockPriceEngine struct {
	amountCents   int64
	pricingHash   string
	engineVersion string
	err           error
}

func (m *mockPriceEngine) Quote(ctx context.Context, config map[string]any, runSize int) (int64, string, string, error) {
	if m.err != nil {
		return 0, "", "", m.err
	}
	return m.amountCents, m.pricingHash, m.engineVersion, nil
}

// mockChargeClient is a scripted ChargeClient for tests.
type mockChargeClient struct {
	intent *StripePaymentIntent
	err    error
}

func (m *mockChargeClient) CreateAndConfirm(ctx context.Context, in StripeChargeInput) (*StripePaymentIntent, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.intent == nil {
		return &StripePaymentIntent{ID: "pi_123", Status: "succeeded"}, nil
	}
	return m.intent, nil
}

// newChargeTestService builds a ServiceImpl wired with the given mocks and a
// fresh in-memory DB.
func newChargeTestService(t *testing.T, quotes QuoteReader, engine PriceEngine, charge ChargeClient) Service {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&ProductionCharge{}); err != nil {
		t.Fatal(err)
	}
	return NewChargeService(NewGormStore(db), quotes, engine, charge)
}

func validChargeInput() CreateStockRequestChargeInput {
	return CreateStockRequestChargeInput{
		UserID:           "user-1",
		QuoteID:          "quote-1",
		Quantity:         10,
		StripeCustomerID: "cus_123",
		PaymentMethodID:  "pm_123",
	}
}

func validQuoteConfig() map[string]any {
	return map[string]any{
		"process": "print",
		"W":       float64(40),
		"L":       float64(60),
		"bord_t":  float64(0),
		"bord_b":  float64(0),
		"bord_l":  float64(0),
		"bord_r":  float64(0),
		"run_size": float64(10),
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

func TestCreateStockRequestChargePricingDisabled(t *testing.T) {
	// nil engine -> pricing disabled.
	svc := newChargeTestService(t, &mockQuoteReader{quote: &datastoremongo.StudioQuote{ID: "quote-1", Config: validQuoteConfig()}}, nil, &mockChargeClient{})

	_, err := svc.CreateStockRequestCharge(context.Background(), validChargeInput())
	if !errors.Is(err, ErrPricingDisabled) {
		t.Fatalf("expected ErrPricingDisabled, got %v", err)
	}
}

func TestCreateStockRequestChargeStripeDisabled(t *testing.T) {
	svc := newChargeTestService(t,
		&mockQuoteReader{quote: &datastoremongo.StudioQuote{ID: "quote-1", Config: validQuoteConfig()}},
		&mockPriceEngine{amountCents: 2500},
		nil, // nil charge client -> stripe disabled
	)

	_, err := svc.CreateStockRequestCharge(context.Background(), validChargeInput())
	if !errors.Is(err, ErrStripeDisabled) {
		t.Fatalf("expected ErrStripeDisabled, got %v", err)
	}
}

func TestCreateStockRequestChargeInvalidQuoteConfig(t *testing.T) {
	// A config missing required numeric fields (W) must fail translation.
	quotes := &mockQuoteReader{quote: &datastoremongo.StudioQuote{
		ID:     "quote-1",
		Config: map[string]any{"process": "print"},
	}}
	svc := newChargeTestService(t, quotes, &mockPriceEngine{}, &mockChargeClient{})

	if _, err := svc.CreateStockRequestCharge(context.Background(), validChargeInput()); err == nil {
		t.Fatal("expected error for invalid quote config, got nil")
	}
}

func TestCreateStockRequestChargeEngineError(t *testing.T) {
	quotes := &mockQuoteReader{quote: &datastoremongo.StudioQuote{ID: "quote-1", Config: validQuoteConfig()}}
	engine := &mockPriceEngine{err: errors.New("engine boom")}
	svc := newChargeTestService(t, quotes, engine, &mockChargeClient{})

	if _, err := svc.CreateStockRequestCharge(context.Background(), validChargeInput()); err == nil {
		t.Fatal("expected error from engine, got nil")
	}
}

func TestCreateStockRequestChargeStripeError(t *testing.T) {
	quotes := &mockQuoteReader{quote: &datastoremongo.StudioQuote{ID: "quote-1", Config: validQuoteConfig()}}
	engine := &mockPriceEngine{amountCents: 2500}
	charge := &mockChargeClient{err: errors.New("stripe boom")}
	svc := newChargeTestService(t, quotes, engine, charge)

	if _, err := svc.CreateStockRequestCharge(context.Background(), validChargeInput()); err == nil {
		t.Fatal("expected error from stripe, got nil")
	}
}

func TestCreateStockRequestChargeIdempotentByPaymentIntent(t *testing.T) {
	quotes := &mockQuoteReader{quote: &datastoremongo.StudioQuote{ID: "quote-1", Config: validQuoteConfig()}}
	engine := &mockPriceEngine{amountCents: 2500}
	charge := &mockChargeClient{intent: &StripePaymentIntent{ID: "pi_123", Status: "succeeded"}}

	svc := newChargeTestService(t, quotes, engine, charge)

	if _, err := svc.CreateStockRequestCharge(context.Background(), validChargeInput()); err != nil {
		t.Fatalf("unexpected error on first charge: %v", err)
	}
	// A retry with the same quote/user must not create a duplicate audit row:
	// the audit layer's idempotency guard reports the charge as already
	// recorded.
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
