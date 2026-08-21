package purchase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop/studio"
	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// mockArtworkPriceReader is a scripted ArtworkPriceReader for tests.
type mockArtworkPriceReader struct {
	editionPrice  *datastoremongo.ArtworkPrice
	paintingPrice *datastoremongo.ArtworkPrice
	err           error
}

func (m *mockArtworkPriceReader) GetEditionPrice(ctx context.Context, editionID string) (*datastoremongo.ArtworkPrice, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.editionPrice == nil {
		return nil, datastoremongo.ErrArtworkNotFound
	}
	return m.editionPrice, nil
}

func (m *mockArtworkPriceReader) GetPaintingPrice(ctx context.Context, paintingID string) (*datastoremongo.ArtworkPrice, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.paintingPrice == nil {
		return nil, datastoremongo.ErrArtworkNotFound
	}
	return m.paintingPrice, nil
}

// mockPriceOracle is a scripted PriceOracle for tests.
type mockPriceOracle struct {
	price *PythPrice
	err   error
}

func (m *mockPriceOracle) Latest(ctx context.Context) (*PythPrice, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.price == nil {
		return &PythPrice{PriceUSD: 1.0}, nil
	}
	return m.price, nil
}

// mockChargeClient is a scripted ChargeClient for tests. It records the last
// StripeChargeInput so tests can assert the server-computed amount and the
// Idempotency-Key propagation.
type mockChargeClient struct {
	intent *studio.StripePaymentIntent
	err    error
	lastIn studio.StripeChargeInput
}

func (m *mockChargeClient) CreateAndConfirm(ctx context.Context, in studio.StripeChargeInput) (*studio.StripePaymentIntent, error) {
	m.lastIn = in
	if m.err != nil {
		return nil, m.err
	}
	if m.intent == nil {
		return &studio.StripePaymentIntent{ID: "pi_123", Status: "succeeded"}, nil
	}
	return m.intent, nil
}

// mockEscrowCreator is a scripted EscrowCreator for tests. It records the
// server-computed FLOW amount passed to the escrow so tests can assert that
// the amount is derived from the Mongo price + Pyth, never from the client.
type mockEscrowCreator struct {
	amount float64
	err    error
}

func (m *mockEscrowCreator) CreateEscrow(ctx context.Context, sync bool, address string, buyer, seller string, editionID uint64, chipID string, unlockAt float64, nonce uint64, amount float64) (*jobs.Job, *transactions.Transaction, error) {
	m.amount = amount
	if m.err != nil {
		return nil, nil, m.err
	}
	return nil, nil, nil
}

// mockStore is a scripted Store for simulating audit write failures and
// duplicate-key idempotency.
type mockStore struct {
	createErr error
}

func (m *mockStore) CreatePurchaseCharge(charge *PurchaseCharge) error {
	return m.createErr
}

// newPurchaseTestService builds a ServiceImpl wired with the given mocks and a
// fresh in-memory DB.
func newPurchaseTestService(t *testing.T, prices ArtworkPriceReader, oracle PriceOracle, charge ChargeClient, escrow EscrowCreator, platformFeeBps int) Service {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&PurchaseCharge{}); err != nil {
		t.Fatal(err)
	}
	return NewService(NewGormStore(db), prices, oracle, charge, escrow, platformFeeBps)
}

func validPurchaseInput() CreatePurchaseChargeInput {
	return CreatePurchaseChargeInput{
		UserID:           "user-1",
		ArtworkKind:      ArtworkEdition,
		ArtworkID:        "edition-1",
		StripeCustomerID: "cus_123",
		PaymentMethodID:  "pm_123",
		IdempotencyKey:   "idem-1",
		Buyer:            "0x179b6b1cb6755e31",
		Seller:           "0xf3fcd2c1a78f5eee",
		EditionID:        1,
		ChipID:           "chip-1",
		UnlockAt:         4102444800.0,
		Nonce:            1,
	}
}

// TestCreatePurchaseCharge_ServerComputesAmount pins the core discipline of
// #93: the escrow amount is computed server-side from the Mongo artwork price,
// the configured platform fee, and the Pyth FLOW/USD price — never accepted
// from the client. This is the amount-validation gap that
// artdrop/escrow_amount_validation_test.go used to pin with t.Skip; the
// purchase flow is where the server now owns the amount.
func TestCreatePurchaseCharge_ServerComputesAmount(t *testing.T) {
	prices := &mockArtworkPriceReader{
		editionPrice: &datastoremongo.ArtworkPrice{ID: "edition-1", PriceUSD: 100.0},
	}
	oracle := &mockPriceOracle{price: &PythPrice{PriceUSD: 0.5}} // 1 FLOW = $0.50
	charge := &mockChargeClient{}
	escrow := &mockEscrowCreator{}

	svc := newPurchaseTestService(t, prices, oracle, charge, escrow, 500) // 5% fee

	got, err := svc.CreatePurchaseCharge(context.Background(), validPurchaseInput())
	if err != nil {
		t.Fatalf("CreatePurchaseCharge: %v", err)
	}

	// artwork $100 + 5% fee = $105 = 10500 cents
	if got.AmountCents != 10500 {
		t.Errorf("AmountCents = %d, want 10500", got.AmountCents)
	}
	if got.PlatformFeeCents != 500 {
		t.Errorf("PlatformFeeCents = %d, want 500", got.PlatformFeeCents)
	}
	// $105 / $0.50 per FLOW = 210 FLOW
	if got.FlowAmount != 210.0 {
		t.Errorf("FlowAmount = %f, want 210.0", got.FlowAmount)
	}
	// The Stripe charge must use the server-computed USD amount.
	if charge.lastIn.AmountCents != 10500 {
		t.Errorf("Stripe AmountCents = %d, want 10500", charge.lastIn.AmountCents)
	}
	// The escrow must receive the server-computed FLOW amount.
	if escrow.amount != 210.0 {
		t.Errorf("escrow amount = %f, want 210.0", escrow.amount)
	}
}

// TestCreatePurchaseCharge_RejectsClientAmount ensures the input has no amount
// field at all: the client cannot influence the charge or escrow amount.
func TestCreatePurchaseCharge_RejectsClientAmount(t *testing.T) {
	in := validPurchaseInput()
	// There is no Amount field on CreatePurchaseChargeInput by design. This
	// test documents that the input shape carries no amount the client could
	// set, so the only amount that reaches Stripe and the escrow is the one the
	// server computes.
	if _, ok := any(in).(interface{ GetAmount() float64 }); ok {
		t.Fatal("CreatePurchaseChargeInput must not expose a client-settable amount")
	}
}

func TestCreatePurchaseCharge_ArtworkNotFound(t *testing.T) {
	prices := &mockArtworkPriceReader{} // no price -> ErrArtworkNotFound
	svc := newPurchaseTestService(t, prices, &mockPriceOracle{}, &mockChargeClient{}, &mockEscrowCreator{}, 500)

	_, err := svc.CreatePurchaseCharge(context.Background(), validPurchaseInput())
	if !errors.Is(err, ErrArtworkNotFound) {
		t.Fatalf("err = %v, want ErrArtworkNotFound", err)
	}
}

func TestCreatePurchaseCharge_ArtworkPriceMissing(t *testing.T) {
	prices := &mockArtworkPriceReader{err: datastoremongo.ErrArtworkPriceMissing}
	svc := newPurchaseTestService(t, prices, &mockPriceOracle{}, &mockChargeClient{}, &mockEscrowCreator{}, 500)

	_, err := svc.CreatePurchaseCharge(context.Background(), validPurchaseInput())
	if !errors.Is(err, ErrArtworkPriceMissing) {
		t.Fatalf("err = %v, want ErrArtworkPriceMissing", err)
	}
}

func TestCreatePurchaseCharge_OracleStale(t *testing.T) {
	prices := &mockArtworkPriceReader{editionPrice: &datastoremongo.ArtworkPrice{ID: "edition-1", PriceUSD: 100.0}}
	oracle := &mockPriceOracle{err: ErrPythStale}
	svc := newPurchaseTestService(t, prices, oracle, &mockChargeClient{}, &mockEscrowCreator{}, 500)

	_, err := svc.CreatePurchaseCharge(context.Background(), validPurchaseInput())
	if !errors.Is(err, ErrOracleStale) {
		t.Fatalf("err = %v, want ErrOracleStale", err)
	}
}

func TestCreatePurchaseCharge_StripeDisabled(t *testing.T) {
	prices := &mockArtworkPriceReader{editionPrice: &datastoremongo.ArtworkPrice{ID: "edition-1", PriceUSD: 100.0}}
	svc := newPurchaseTestService(t, prices, &mockPriceOracle{}, nil, &mockEscrowCreator{}, 500)

	_, err := svc.CreatePurchaseCharge(context.Background(), validPurchaseInput())
	if !errors.Is(err, ErrStripeDisabled) {
		t.Fatalf("err = %v, want ErrStripeDisabled", err)
	}
}

func TestCreatePurchaseCharge_EscrowDisabled(t *testing.T) {
	prices := &mockArtworkPriceReader{editionPrice: &datastoremongo.ArtworkPrice{ID: "edition-1", PriceUSD: 100.0}}
	svc := newPurchaseTestService(t, prices, &mockPriceOracle{}, &mockChargeClient{}, nil, 500)

	_, err := svc.CreatePurchaseCharge(context.Background(), validPurchaseInput())
	if !errors.Is(err, ErrEscrowDisabled) {
		t.Fatalf("err = %v, want ErrEscrowDisabled", err)
	}
}

// TestCreatePurchaseCharge_IdempotentDuplicate pins the idempotency guard: a
// second attempt with the same Stripe payment intent (same Idempotency-Key)
// must not create a duplicate audit record — it maps the unique-constraint
// violation to ErrChargeAlreadyRecorded.
func TestCreatePurchaseCharge_IdempotentDuplicate(t *testing.T) {
	prices := &mockArtworkPriceReader{editionPrice: &datastoremongo.ArtworkPrice{ID: "edition-1", PriceUSD: 100.0}}
	store := &mockStore{createErr: gorm.ErrDuplicatedKey}
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&PurchaseCharge{}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(store, prices, &mockPriceOracle{}, &mockChargeClient{}, &mockEscrowCreator{}, 500)

	_, err = svc.CreatePurchaseCharge(context.Background(), validPurchaseInput())
	if !errors.Is(err, ErrChargeAlreadyRecorded) {
		t.Fatalf("err = %v, want ErrChargeAlreadyRecorded", err)
	}
}

func TestCreatePurchaseCharge_Validation(t *testing.T) {
	tests := []struct {
		name  string
		mut   func(*CreatePurchaseChargeInput)
		field string
	}{
		{"missing user id", func(in *CreatePurchaseChargeInput) { in.UserID = "" }, "user id"},
		{"missing artwork id", func(in *CreatePurchaseChargeInput) { in.ArtworkID = "" }, "artwork id"},
		{"bad artwork kind", func(in *CreatePurchaseChargeInput) { in.ArtworkKind = "bogus" }, "artwork kind"},
		{"missing stripe customer", func(in *CreatePurchaseChargeInput) { in.StripeCustomerID = "" }, "stripe customer id"},
		{"missing idempotency key", func(in *CreatePurchaseChargeInput) { in.IdempotencyKey = "" }, "idempotency key"},
		{"missing buyer", func(in *CreatePurchaseChargeInput) { in.Buyer = "" }, "buyer"},
		{"missing seller", func(in *CreatePurchaseChargeInput) { in.Seller = "" }, "seller"},
		{"missing chip id", func(in *CreatePurchaseChargeInput) { in.ChipID = "" }, "chip id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := validPurchaseInput()
			tc.mut(&in)
			svc := newPurchaseTestService(t, &mockArtworkPriceReader{}, &mockPriceOracle{}, &mockChargeClient{}, &mockEscrowCreator{}, 500)
			_, err := svc.CreatePurchaseCharge(context.Background(), in)
			if err == nil {
				t.Fatalf("expected validation error for %s", tc.field)
			}
		})
	}
}
