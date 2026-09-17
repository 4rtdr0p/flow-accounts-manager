package purchase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop/studio"
	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// testClaimWindowSeconds is an arbitrary, easy-to-check claim window used by
// tests that don't care about the exact unlock_at value, only that it's
// server-computed (issue #111).
const testClaimWindowSeconds = 604800

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
	called bool
}

func (m *mockChargeClient) CreateAndConfirm(ctx context.Context, in studio.StripeChargeInput) (*studio.StripePaymentIntent, error) {
	m.called = true
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
// It returns a *jobs.Job with a real (random) ID by default, mirroring what
// Service.CreateEscrow always returns for sync=false on success — see
// transactions.ServiceImpl.Create — so tests exercise the same job.ID
// capture path CreatePurchaseCharge uses in production (issue #98).
// returnNilJob forces a (nil, nil, nil) return instead, for
// TestCreatePurchaseCharge_SurvivesNilEscrowJob — the EscrowCreator
// interface doesn't itself guarantee a non-nil job on success.
type mockEscrowCreator struct {
	amount       float64
	unlockAt     float64
	err          error
	called       bool
	job          *jobs.Job
	returnNilJob bool

	// reEscrowCalled/certificateID record the issue #107 re-escrow branch: a
	// non-zero CreatePurchaseChargeInput.CertificateID must route to ReEscrow
	// (not CreateEscrow) with the same server-computed amount.
	reEscrowCalled bool
	certificateID  uint64
}

type mockChipReader struct {
	provisioned bool
	err         error
}

func (m *mockChipReader) IsProvisioned(_ context.Context, _ string) (bool, error) {
	return m.provisioned, m.err
}

func (m *mockEscrowCreator) CreateEscrow(ctx context.Context, sync bool, address string, buyer, seller string, editionID uint64, chipID string, unlockAt float64, nonce uint64, amount float64) (*jobs.Job, *transactions.Transaction, error) {
	m.called = true
	m.amount = amount
	m.unlockAt = unlockAt
	if m.err != nil {
		return nil, nil, m.err
	}
	if m.returnNilJob {
		return nil, nil, nil
	}
	if m.job == nil {
		m.job = &jobs.Job{ID: uuid.New()}
	}
	return m.job, nil, nil
}

func (m *mockEscrowCreator) ReEscrow(ctx context.Context, sync bool, address string, buyer, seller string, certificateID uint64, chipID string, unlockAt float64, nonce uint64, amount float64) (*jobs.Job, *transactions.Transaction, error) {
	m.reEscrowCalled = true
	m.certificateID = certificateID
	m.amount = amount
	m.unlockAt = unlockAt
	if m.err != nil {
		return nil, nil, m.err
	}
	if m.returnNilJob {
		return nil, nil, nil
	}
	if m.job == nil {
		m.job = &jobs.Job{ID: uuid.New()}
	}
	return m.job, nil, nil
}

// mockStore is a scripted Store for simulating audit write failures and
// duplicate-key idempotency.
type mockStore struct {
	createErr error
}

func (m *mockStore) CreatePurchaseCharge(charge *PurchaseCharge) error {
	return m.createErr
}

func (m *mockStore) GetPurchaseCharge(context.Context, string) (*PurchaseCharge, error) {
	return nil, nil
}
func (m *mockStore) ClaimEscrowOpening(context.Context, string, string, uint64, string) (bool, error) {
	return false, nil
}
func (m *mockStore) SetEscrowJobID(context.Context, string, string) error { return nil }
func (m *mockStore) ResetEscrowOpening(context.Context, string) error     { return nil }

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
	return NewService(NewGormStore(db), prices, oracle, charge, escrow, &mockChipReader{provisioned: true}, platformFeeBps, testClaimWindowSeconds)
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

	// The buyer is charged the full artwork price: $100 = 10000 cents.
	if got.AmountCents != 10000 {
		t.Errorf("AmountCents = %d, want 10000", got.AmountCents)
	}
	// The platform fee is ArtDrop's 5% share of that $100, not a surcharge:
	// $5 = 500 cents.
	if got.PlatformFeeCents != 500 {
		t.Errorf("PlatformFeeCents = %d, want 500", got.PlatformFeeCents)
	}
	// The escrow carries only the fee as a gas reserve: $5 / $0.50 per FLOW
	// = 10 FLOW.
	if got.FlowAmount != 10.0 {
		t.Errorf("FlowAmount = %f, want 10.0", got.FlowAmount)
	}
	// The Stripe charge must use the full artwork price, not price + fee.
	if charge.lastIn.AmountCents != 10000 {
		t.Errorf("Stripe AmountCents = %d, want 10000", charge.lastIn.AmountCents)
	}
	// The escrow must receive only the fee-only FLOW amount.
	if escrow.amount != 10.0 {
		t.Errorf("escrow amount = %f, want 10.0", escrow.amount)
	}
	// The audit record must capture the async escrow-creation job's id
	// (issue #98) — sync stays false; only the job return value changes
	// from discarded to captured.
	if got.EscrowJobID == "" {
		t.Error("expected EscrowJobID to be captured from the escrow-creation job")
	}
	if escrow.job == nil || got.EscrowJobID != escrow.job.ID.String() {
		t.Errorf("EscrowJobID = %q, want %v", got.EscrowJobID, escrow.job)
	}
	// EscrowID is intentionally not resolved by this flow — see #98.
	if got.EscrowID != nil {
		t.Errorf("expected EscrowID to stay nil (resolved separately), got %v", *got.EscrowID)
	}
}

func TestCreatePurchaseCharge_AddsShippingToStripeOnly(t *testing.T) {
	prices := &mockArtworkPriceReader{editionPrice: &datastoremongo.ArtworkPrice{ID: "edition-1", PriceUSD: 100}}
	stripe := &mockChargeClient{}
	escrow := &mockEscrowCreator{}
	svc := newPurchaseTestService(t, prices, &mockPriceOracle{price: &PythPrice{PriceUSD: 0.5}}, stripe, escrow, 500)
	in := validPurchaseInput()
	in.ShippingCents = 1299

	got, err := svc.CreatePurchaseCharge(context.Background(), in)
	if err != nil {
		t.Fatalf("CreatePurchaseCharge: %v", err)
	}
	if stripe.lastIn.AmountCents != 11299 {
		t.Errorf("Stripe AmountCents = %d, want 11299", stripe.lastIn.AmountCents)
	}
	if got.ShippingCents != 1299 || got.AmountCents != 10000 {
		t.Errorf("audit amounts = artwork %d shipping %d, want 10000 and 1299", got.AmountCents, got.ShippingCents)
	}
	if got.FlowAmount != 10 || escrow.amount != 10 {
		t.Errorf("FLOW escrow = record %f call %f, want 10 (shipping excluded)", got.FlowAmount, escrow.amount)
	}
}

func TestCreatePurchaseCharge_ZeroShippingPreservesLegacyStripeAmount(t *testing.T) {
	prices := &mockArtworkPriceReader{editionPrice: &datastoremongo.ArtworkPrice{ID: "edition-1", PriceUSD: 100}}
	stripe := &mockChargeClient{}
	svc := newPurchaseTestService(t, prices, &mockPriceOracle{price: &PythPrice{PriceUSD: 0.5}}, stripe, &mockEscrowCreator{}, 500)

	if _, err := svc.CreatePurchaseCharge(context.Background(), validPurchaseInput()); err != nil {
		t.Fatalf("CreatePurchaseCharge: %v", err)
	}
	if stripe.lastIn.AmountCents != 10000 {
		t.Errorf("Stripe AmountCents = %d, want legacy 10000", stripe.lastIn.AmountCents)
	}
}

func TestCreatePurchaseCharge_WithoutChipCreatesPaidObligation(t *testing.T) {
	prices := &mockArtworkPriceReader{editionPrice: &datastoremongo.ArtworkPrice{ID: "edition-1", PriceUSD: 100}}
	stripe := &mockChargeClient{}
	escrow := &mockEscrowCreator{}
	svc := newPurchaseTestService(t, prices, &mockPriceOracle{price: &PythPrice{PriceUSD: 0.5}}, stripe, escrow, 500)

	in := validPurchaseInput()
	in.ChipID = ""
	got, err := svc.CreatePurchaseCharge(context.Background(), in)
	if err != nil {
		t.Fatalf("CreatePurchaseCharge without chip: %v", err)
	}
	if got.PurchaseID == "" || got.Status != PurchaseStatusPaidPendingEscrow {
		t.Fatalf("paid obligation = %#v, want public id and %s", got, PurchaseStatusPaidPendingEscrow)
	}
	if got.EscrowJobID != "" || escrow.called || escrow.reEscrowCalled {
		t.Fatal("chipless charge must not enqueue escrow")
	}
	if !stripe.called || got.FlowAmount != 10 {
		t.Fatalf("chipless charge must charge and freeze FLOW amount, got stripe=%v flow=%v", stripe.called, got.FlowAmount)
	}
}

func TestOpenEscrow_QueuesOnceAndReplaysDurableResult(t *testing.T) {
	prices := &mockArtworkPriceReader{editionPrice: &datastoremongo.ArtworkPrice{ID: "edition-1", PriceUSD: 100}}
	escrow := &mockEscrowCreator{}
	svc := newPurchaseTestService(t, prices, &mockPriceOracle{price: &PythPrice{PriceUSD: 0.5}}, &mockChargeClient{}, escrow, 500)
	in := validPurchaseInput()
	in.ChipID = ""
	paid, err := svc.CreatePurchaseCharge(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}

	open := OpenEscrowInput{PurchaseID: paid.PurchaseID, ChipID: "chip-9", Nonce: 19, IdempotencyKey: "open-1"}
	got, err := svc.OpenEscrow(context.Background(), open)
	if err != nil {
		t.Fatalf("OpenEscrow: %v", err)
	}
	if got.Status != PurchaseStatusEscrowOpening || got.EscrowJobID == "" || got.ChipID != open.ChipID {
		t.Fatalf("open result = %#v", got)
	}
	if !escrow.called || escrow.amount != paid.FlowAmount || escrow.unlockAt != paid.UnlockAt {
		t.Fatal("open escrow did not use the frozen paid obligation values")
	}
	replay, err := svc.OpenEscrow(context.Background(), open)
	if err != nil || replay.EscrowJobID != got.EscrowJobID || escrow.job == nil {
		t.Fatalf("idempotent replay = %#v, %v", replay, err)
	}
	if _, err := svc.OpenEscrow(context.Background(), OpenEscrowInput{PurchaseID: paid.PurchaseID, ChipID: "chip-9", Nonce: 19, IdempotencyKey: "different-key"}); !errors.Is(err, ErrPurchaseNotOpenable) {
		t.Fatalf("second open err = %v, want ErrPurchaseNotOpenable", err)
	}
}

func TestOpenEscrow_RejectsUnknownChip(t *testing.T) {
	prices := &mockArtworkPriceReader{editionPrice: &datastoremongo.ArtworkPrice{ID: "edition-1", PriceUSD: 100}}
	svc := newPurchaseTestService(t, prices, &mockPriceOracle{price: &PythPrice{PriceUSD: 0.5}}, &mockChargeClient{}, &mockEscrowCreator{}, 500)
	impl := svc.(*ServiceImpl)
	impl.chips = &mockChipReader{provisioned: false}
	in := validPurchaseInput()
	in.ChipID = ""
	paid, err := svc.CreatePurchaseCharge(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.OpenEscrow(context.Background(), OpenEscrowInput{PurchaseID: paid.PurchaseID, ChipID: "missing", IdempotencyKey: "open-1"})
	if !errors.Is(err, ErrChipNotProvisioned) {
		t.Fatalf("err = %v, want ErrChipNotProvisioned", err)
	}
}

// TestCreatePurchaseCharge_ServerComputesUnlockAt pins issue #111: unlock_at
// is computed server-side as now() + Config.EscrowClaimWindowSeconds, never
// accepted from the client — CreatePurchaseChargeInput has no UnlockAt field
// at all. It injects a fixed `now` and a known claim window directly on the
// ServiceImpl and asserts the exact value reaches the escrow creator.
func TestCreatePurchaseCharge_ServerComputesUnlockAt(t *testing.T) {
	prices := &mockArtworkPriceReader{
		editionPrice: &datastoremongo.ArtworkPrice{ID: "edition-1", PriceUSD: 100.0},
	}
	oracle := &mockPriceOracle{price: &PythPrice{PriceUSD: 0.5}}
	charge := &mockChargeClient{}
	escrow := &mockEscrowCreator{}

	const claimWindowSeconds = 12345.0
	fixedNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	svcIface := newPurchaseTestService(t, prices, oracle, charge, escrow, 500)
	svc, ok := svcIface.(*ServiceImpl)
	if !ok {
		t.Fatalf("newPurchaseTestService returned %T, want *ServiceImpl", svcIface)
	}
	svc.claimWindowSeconds = claimWindowSeconds
	svc.now = func() time.Time { return fixedNow }

	want := float64(fixedNow.Unix()) + claimWindowSeconds

	got, err := svc.CreatePurchaseCharge(context.Background(), validPurchaseInput())
	if err != nil {
		t.Fatalf("CreatePurchaseCharge: %v", err)
	}

	// The escrow creator must receive the server-computed unlock_at.
	if escrow.unlockAt != want {
		t.Errorf("escrow unlockAt = %f, want %f", escrow.unlockAt, want)
	}
	// The audit record must persist the same server-computed value.
	if got.UnlockAt != want {
		t.Errorf("PurchaseCharge.UnlockAt = %f, want %f", got.UnlockAt, want)
	}
	// CreatePurchaseChargeInput has no UnlockAt field to derive it from — this
	// documents that the value cannot have come from the client.
	if _, ok := any(validPurchaseInput()).(interface{ GetUnlockAt() float64 }); ok {
		t.Fatal("CreatePurchaseChargeInput must not expose a client-settable unlock_at")
	}
}

// TestCreatePurchaseCharge_ReEscrowBranchServerComputesAmount pins issue #107:
// a non-zero CertificateID re-offers an EXISTING certificate via ReEscrow (no
// re-mint), and the FLOW amount it re-escrows with is the SAME server-computed
// value a fresh purchase would use — Mongo artwork price + platform fee + Pyth,
// never a client amount. It must call ReEscrow, not CreateEscrow, and pass the
// certificate id through unchanged.
func TestCreatePurchaseCharge_ReEscrowBranchServerComputesAmount(t *testing.T) {
	prices := &mockArtworkPriceReader{
		editionPrice: &datastoremongo.ArtworkPrice{ID: "edition-1", PriceUSD: 100.0},
	}
	oracle := &mockPriceOracle{price: &PythPrice{PriceUSD: 0.5}} // 1 FLOW = $0.50
	charge := &mockChargeClient{}
	escrow := &mockEscrowCreator{}

	svc := newPurchaseTestService(t, prices, oracle, charge, escrow, 500) // 5% fee

	in := validPurchaseInput()
	in.CertificateID = 77 // re-offer an existing certificate

	got, err := svc.CreatePurchaseCharge(context.Background(), in)
	if err != nil {
		t.Fatalf("CreatePurchaseCharge: %v", err)
	}

	// Must route to ReEscrow, not CreateEscrow.
	if !escrow.reEscrowCalled {
		t.Error("expected ReEscrow to be called for a non-zero CertificateID")
	}
	if escrow.called {
		t.Error("CreateEscrow must not be called when re-escrowing an existing certificate")
	}
	// The certificate id must pass through untouched.
	if escrow.certificateID != 77 {
		t.Errorf("re-escrow certificate id = %d, want 77", escrow.certificateID)
	}
	// The re-escrow amount is the same server-computed fee-only FLOW reserve as
	// a fresh purchase: $5 / $0.50 per FLOW = 10 FLOW. The client never set it.
	if escrow.amount != 10.0 {
		t.Errorf("re-escrow amount = %f, want 10.0", escrow.amount)
	}
	// Stripe is still charged the full artwork price, exactly like a fresh
	// purchase — re-escrow only changes the escrow call, not the charge.
	if charge.lastIn.AmountCents != 10000 {
		t.Errorf("Stripe AmountCents = %d, want 10000", charge.lastIn.AmountCents)
	}
	if got.FlowAmount != 10.0 {
		t.Errorf("FlowAmount = %f, want 10.0", got.FlowAmount)
	}
	// The audit record still captures the async job id (issue #98), regardless
	// of which escrow call produced it.
	if got.EscrowJobID == "" {
		t.Error("expected EscrowJobID to be captured from the re-escrow job")
	}
}

// TestCreatePurchaseCharge_SurvivesNilEscrowJob covers an EscrowCreator that
// returns a nil job alongside a nil error (not what production code does —
// transactions.ServiceImpl.Create always returns a non-nil job for
// sync=false on success — but not guaranteed by the EscrowCreator interface
// itself). CreatePurchaseCharge must still record the charge, with an empty
// EscrowJobID, rather than panicking on a nil dereference.
func TestCreatePurchaseCharge_SurvivesNilEscrowJob(t *testing.T) {
	prices := &mockArtworkPriceReader{
		editionPrice: &datastoremongo.ArtworkPrice{ID: "edition-1", PriceUSD: 100.0},
	}
	oracle := &mockPriceOracle{price: &PythPrice{PriceUSD: 0.5}}
	charge := &mockChargeClient{}
	escrow := &mockEscrowCreator{returnNilJob: true}
	svc := newPurchaseTestService(t, prices, oracle, charge, escrow, 500)

	got, err := svc.CreatePurchaseCharge(context.Background(), validPurchaseInput())
	if err != nil {
		t.Fatalf("CreatePurchaseCharge: %v", err)
	}
	if got.EscrowJobID != "" {
		t.Errorf("expected empty EscrowJobID when the escrow creator returns a nil job, got %q", got.EscrowJobID)
	}
}

// TestCreatePurchaseCharge_ZeroFeeCheapArtwork pins the fix for the zero-FLOW
// escrow bug: an artwork cheap enough that the 5% fee rounds down to zero
// cents must be rejected before Stripe or the escrow are touched. Without this
// guard the buyer would be charged via Stripe and the escrow would then abort
// on chain (it requires a positive payment), failing silently after the
// charge already succeeded.
func TestCreatePurchaseCharge_ZeroFeeCheapArtwork(t *testing.T) {
	prices := &mockArtworkPriceReader{
		editionPrice: &datastoremongo.ArtworkPrice{ID: "edition-1", PriceUSD: 0.09}, // 9 cents * 5% rounds to 0
	}
	charge := &mockChargeClient{}
	escrow := &mockEscrowCreator{}
	svc := newPurchaseTestService(t, prices, &mockPriceOracle{price: &PythPrice{PriceUSD: 0.5}}, charge, escrow, 500)

	_, err := svc.CreatePurchaseCharge(context.Background(), validPurchaseInput())
	if err == nil {
		t.Fatal("expected error for a zero-cent platform fee, got nil")
	}
	if charge.called {
		t.Error("Stripe must not be charged when the computed fee is zero")
	}
	if escrow.called {
		t.Error("escrow must not be opened when the computed fee is zero")
	}
}

// TestCreatePurchaseCharge_ZeroFeeBps pins the same guard for a misconfigured
// platformFeeBps of zero: with no fee configured there is nothing to reserve
// on chain, so the request must fail before Stripe or the escrow are touched.
func TestCreatePurchaseCharge_ZeroFeeBps(t *testing.T) {
	prices := &mockArtworkPriceReader{
		editionPrice: &datastoremongo.ArtworkPrice{ID: "edition-1", PriceUSD: 100.0},
	}
	charge := &mockChargeClient{}
	escrow := &mockEscrowCreator{}
	svc := newPurchaseTestService(t, prices, &mockPriceOracle{price: &PythPrice{PriceUSD: 0.5}}, charge, escrow, 0)

	_, err := svc.CreatePurchaseCharge(context.Background(), validPurchaseInput())
	if err == nil {
		t.Fatal("expected error for a zero platform fee bps, got nil")
	}
	if charge.called {
		t.Error("Stripe must not be charged when platformFeeBps is zero")
	}
	if escrow.called {
		t.Error("escrow must not be opened when platformFeeBps is zero")
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
	svc := NewService(store, prices, &mockPriceOracle{}, &mockChargeClient{}, &mockEscrowCreator{}, &mockChipReader{provisioned: true}, 500, testClaimWindowSeconds)

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
