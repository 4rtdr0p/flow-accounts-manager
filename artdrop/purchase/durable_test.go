package purchase

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop/studio"
	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type chargeFunc func(context.Context, studio.StripeChargeInput) (*studio.StripePaymentIntent, error)

func (f chargeFunc) CreateAndConfirm(ctx context.Context, in studio.StripeChargeInput) (*studio.StripePaymentIntent, error) {
	return f(ctx, in)
}
func goodPrices() *mockArtworkPriceReader {
	return &mockArtworkPriceReader{editionPrice: &datastoremongo.ArtworkPrice{PriceUSD: 100}, paintingPrice: &datastoremongo.ArtworkPrice{PriceUSD: 100}}
}
func serviceOn(db *gorm.DB, stripe ChargeClient, escrow EscrowCreator) *ServiceImpl {
	return NewService(NewGormStore(db), goodPrices(), &mockPriceOracle{}, stripe, escrow, nil, defaultTestEditionArtists(), nil, 500, testClaimWindowSeconds).(*ServiceImpl)
}
func codeIs(t *testing.T, err error, code string) {
	t.Helper()
	var e *RecoveryError
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("got %v, want %s", err, code)
	}
}
func reserveRow(t *testing.T, db *gorm.DB, in CreatePurchaseChargeInput) *handlers.PurchaseIntent {
	t.Helper()
	row, err := handlers.ReadPurchaseIntent(context.Background(), db, in.UserID, in.IdempotencyKey)
	if err != nil || row == nil {
		t.Fatalf("reservation=%v err=%v", row, err)
	}
	return row
}

// Independent handles and services share only SQL. A barrier keeps the winner
// inside Stripe while the second instance attempts the same financial identity.
func concurrentRecovery(t *testing.T, db1, db2 *gorm.DB) {
	t.Helper()
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	stripe := chargeFunc(func(context.Context, studio.StripeChargeInput) (*studio.StripePaymentIntent, error) {
		calls.Add(1)
		close(entered)
		<-release
		return &studio.StripePaymentIntent{ID: "pi_" + uuid.NewString(), Status: "succeeded"}, nil
	})
	first, second := serviceOn(db1, stripe, nil), serviceOn(db2, stripe, nil)
	in := validPurchaseInput()
	in.IdempotencyKey = "purchase-v2:" + uuid.NewString()
	in.ChipID = ""
	done := make(chan error, 1)
	go func() { _, err := first.CreatePurchaseCharge(context.Background(), in); done <- err }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("winner failed: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("winner timed out")
	}
	_, err := second.CreatePurchaseCharge(context.Background(), in)
	close(release)
	codeIs(t, err, "INTENT_IN_PROGRESS")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// Reconstruct the service: replay must not need prices, oracle, Stripe or escrow.
	restarted := NewService(NewGormStore(db2), nil, nil, nil, nil, nil, nil, nil, 0, 0)
	replay, err := restarted.CreatePurchaseCharge(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	byIntent, err := restarted.GetIntentRecovery(context.Background(), in.UserID, in.IdempotencyKey)
	if err != nil {
		t.Fatal(err)
	}
	byID, err := restarted.GetPurchaseRecovery(context.Background(), replay.PurchaseID)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || byIntent.Purchase.PurchaseID != replay.PurchaseID || byID.Purchase.Financial.StripePaymentIntentID == nil || *byID.Purchase.Financial.StripePaymentIntentID != replay.StripePaymentIntent {
		t.Fatalf("calls=%d recovery=%+v", calls.Load(), byIntent)
	}
	var count int64
	if err := db1.Model(&PurchaseCharge{}).Where("user_id = ? AND charge_intent_id = ?", in.UserID, in.IdempotencyKey).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("rows=%d err=%v", count, err)
	}
	row := reserveRow(t, db2, in)
	if !bytes.Equal(row.Body, replay.responseBody) || row.ExpiryDate != nil {
		t.Fatal("replay bytes or permanent identity changed")
	}
}
func TestDurableTwoSQLiteConnectionsAndRestart(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "purchase.db") + "?_busy_timeout=5000&_journal_mode=WAL"
	open := func() *gorm.DB {
		db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		sql, _ := db.DB()
		t.Cleanup(func() { _ = sql.Close() })
		return db
	}
	db1, db2 := open(), open()
	if err := db1.AutoMigrate(&PurchaseCharge{}, &handlers.PurchaseIntent{}); err != nil {
		t.Fatal(err)
	}
	concurrentRecovery(t, db1, db2)
}
func TestDurableTwoPostgresConnections(t *testing.T) {
	dsn := os.Getenv("PURCHASE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires isolated PostgreSQL test database: PURCHASE_TEST_POSTGRES_DSN")
	}
	open := func() *gorm.DB {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		sql, _ := db.DB()
		t.Cleanup(func() { _ = sql.Close() })
		return db
	}
	db1, db2 := open(), open()
	if err := db1.AutoMigrate(&PurchaseCharge{}, &handlers.PurchaseIntent{}); err != nil {
		t.Fatal(err)
	}
	concurrentRecovery(t, db1, db2)
}
func TestDurableTermsConflictBeforeDependenciesAndAfterBodyExpiry(t *testing.T) {
	changes := []struct {
		name   string
		change func(*CreatePurchaseChargeInput)
	}{
		{"customer", func(i *CreatePurchaseChargeInput) { i.StripeCustomerID = "other" }}, {"paymentMethod", func(i *CreatePurchaseChargeInput) { i.PaymentMethodID = "other" }},
		{"metadata Offer", func(i *CreatePurchaseChargeInput) { i.Metadata = `{"offerId":"other"}` }}, {"metadata exact bytes", func(i *CreatePurchaseChargeInput) { i.Metadata = " " }},
		{"nonce uint64", func(i *CreatePurchaseChargeInput) { i.Nonce = 9007199254740993 }}, {"shipping", func(i *CreatePurchaseChargeInput) { i.ShippingCents = 1 }},
		{"artwork", func(i *CreatePurchaseChargeInput) { i.ArtworkID = "other" }}, {"kind", func(i *CreatePurchaseChargeInput) { i.ArtworkKind = ArtworkPainting }},
		{"buyer", func(i *CreatePurchaseChargeInput) { i.Buyer = "other" }}, {"seller", func(i *CreatePurchaseChargeInput) { i.Seller = "other" }},
		{"edition", func(i *CreatePurchaseChargeInput) { i.EditionID = 2 }}, {"chip", func(i *CreatePurchaseChargeInput) { i.ChipID = "other" }},
		{"certificate", func(i *CreatePurchaseChargeInput) { i.CertificateID = 7 }},
	}
	db, svc := newPurchaseTestServiceWithDB(t, goodPrices(), &mockPriceOracle{}, &mockChargeClient{}, nil, 500)
	in := validPurchaseInput()
	in.ChipID = ""
	if _, err := svc.CreatePurchaseCharge(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if err := handlers.PurgePurchaseResponseBodies(context.Background(), db, time.Now().Add(31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	restarted := NewService(NewGormStore(db), nil, nil, nil, nil, nil, nil, nil, 0, 0)
	for _, tc := range changes {
		t.Run(tc.name, func(t *testing.T) {
			changed := in
			tc.change(&changed)
			_, err := restarted.CreatePurchaseCharge(context.Background(), changed)
			codeIs(t, err, "IDEMPOTENCY_TERMS_CONFLICT")
		})
	}
	_, err := restarted.CreatePurchaseCharge(context.Background(), in)
	codeIs(t, err, "RESULT_BODY_EXPIRED")
	result, err := restarted.GetIntentRecovery(context.Background(), in.UserID, in.IdempotencyKey)
	if err != nil || result.ExecutionStatus != "recorded" {
		t.Fatalf("%+v %v", result, err)
	}
}
func TestDurableOwnerAndActorIdentity(t *testing.T) {
	var keys []string
	stripe := chargeFunc(func(_ context.Context, in studio.StripeChargeInput) (*studio.StripePaymentIntent, error) {
		keys = append(keys, in.IdempotencyKey)
		return &studio.StripePaymentIntent{ID: fmt.Sprintf("pi_%d", len(keys)), Status: "succeeded"}, nil
	})
	db, svc := newPurchaseTestServiceWithDB(t, goodPrices(), &mockPriceOracle{}, stripe, nil, 500)
	in := validPurchaseInput()
	in.ChipID = ""
	in.ActorSubject = in.UserID
	first, err := svc.CreatePurchaseCharge(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	in.ActorSubject = "ops"
	replay, err := svc.CreatePurchaseCharge(context.Background(), in)
	if err != nil || replay.PurchaseID != first.PurchaseID {
		t.Fatalf("%v", err)
	}
	in.UserID = "other-owner"
	if _, err := svc.CreatePurchaseCharge(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0] == keys[1] || keys[0] == in.IdempotencyKey {
		t.Fatalf("provider identity not scoped: %v", keys)
	}
	if handlers.Value(reserveRow(t, db, in).ActorSubject) != "ops" {
		t.Fatal("actor not recorded")
	}
}

// SQL triggers fail at real transaction boundaries. This tests rollback of a
// purchase insert when the terminal CAS fails, not just a mocked store method.
func TestDurableSQLFailures(t *testing.T) {
	for _, stage := range []string{"reserve", "effect", "observe", "commit", "after_escrow", "unknown_save", "pre_effect_save"} {
		t.Run(stage, func(t *testing.T) {
			stripe := &mockChargeClient{}
			escrow := &mockEscrowCreator{}
			prices := goodPrices()
			db, svc := newPurchaseTestServiceWithDB(t, prices, &mockPriceOracle{}, stripe, escrow, 500)
			trigger := ""
			wantStripe := true
			wantEscrow := false
			switch stage {
			case "reserve":
				trigger = "CREATE TRIGGER fail_reserve BEFORE INSERT ON idempotency_keys BEGIN SELECT RAISE(FAIL, 'reserve unavailable'); END"
				wantStripe = false
			case "effect":
				trigger = "CREATE TRIGGER fail_effect BEFORE UPDATE OF effect_may_have_started ON idempotency_keys BEGIN SELECT RAISE(FAIL, 'effect unavailable'); END"
				wantStripe = false
			case "observe":
				trigger = "CREATE TRIGGER fail_observe BEFORE UPDATE OF stripe_payment_intent_id ON idempotency_keys BEGIN SELECT RAISE(FAIL, 'observe unavailable'); END"
			case "commit":
				trigger = "CREATE TRIGGER fail_commit BEFORE UPDATE OF purchase_id ON idempotency_keys BEGIN SELECT RAISE(FAIL, 'commit unavailable'); END"
			case "after_escrow":
				trigger = "CREATE TRIGGER fail_purchase BEFORE INSERT ON purchase_charges BEGIN SELECT RAISE(FAIL, 'purchase unavailable'); END"
				wantEscrow = true
			case "unknown_save":
				trigger = "CREATE TRIGGER fail_unknown BEFORE UPDATE ON idempotency_keys WHEN NEW.execution_state = 'unknown' BEGIN SELECT RAISE(FAIL, 'unknown unavailable'); END"
				stripe.err = errors.New("response lost after charging")
			case "pre_effect_save":
				trigger = "CREATE TRIGGER fail_result BEFORE UPDATE ON idempotency_keys WHEN NEW.execution_state = 'not_charged' BEGIN SELECT RAISE(FAIL, 'result unavailable'); END"
				prices.err = errors.New("price unavailable")
				wantStripe = false
			}
			if err := db.Exec(trigger).Error; err != nil {
				t.Fatal(err)
			}
			in := validPurchaseInput()
			if !wantEscrow {
				in.ChipID = ""
			}
			_, err := svc.CreatePurchaseCharge(context.Background(), in)
			if err == nil {
				t.Fatal("expected failure")
			}
			if stripe.called != wantStripe || escrow.called != wantEscrow {
				t.Fatalf("stripe=%v escrow=%v", stripe.called, escrow.called)
			}
			var count int64
			db.Model(&PurchaseCharge{}).Count(&count)
			if count != 0 {
				t.Fatal("partial purchase commit")
			}
			if stage == "reserve" {
				return
			}
			row := reserveRow(t, db, in)
			if stage == "commit" || stage == "after_escrow" {
				if handlers.Value(row.StripePaymentIntentID) != "pi_123" {
					t.Fatal("lost persisted PI")
				}
			}
			// A stale lease cannot authorize takeover even when saving unknown failed.
			if err := db.Model(&handlers.PurchaseIntent{}).Where("key = ?", row.Key).Update("lease_until", time.Now().Add(-time.Hour)).Error; err != nil {
				t.Fatal(err)
			}
			stripe.called, escrow.called = false, false
			_, err = svc.CreatePurchaseCharge(context.Background(), in)
			codeIs(t, err, "RECONCILIATION_REQUIRED")
			if stripe.called || escrow.called {
				t.Fatal("reexecuted uncertain effects")
			}
			got, err := svc.GetIntentRecovery(context.Background(), in.UserID, in.IdempotencyKey)
			if err != nil || got.ExecutionStatus != "unknown" {
				t.Fatalf("got=%+v err=%v", got, err)
			}
		})
	}
}
func TestDurableProviderErrorsAndCancellation(t *testing.T) {
	for _, problem := range []error{errors.New("HTTP 400"), errors.New("invalid JSON response"), context.DeadlineExceeded, context.Canceled} {
		t.Run(problem.Error(), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls int
			stripe := chargeFunc(func(context.Context, studio.StripeChargeInput) (*studio.StripePaymentIntent, error) {
				calls++
				cancel()
				return nil, problem
			})
			db, svc := newPurchaseTestServiceWithDB(t, goodPrices(), &mockPriceOracle{}, stripe, nil, 500)
			in := validPurchaseInput()
			in.ChipID = ""
			_, err := svc.CreatePurchaseCharge(ctx, in)
			codeIs(t, err, "RECONCILIATION_REQUIRED")
			row := reserveRow(t, db, in)
			if handlers.Value(row.ExecutionState) != "unknown" || !handlers.Value(row.EffectMayHaveStarted) {
				t.Fatal("uncertainty not durable")
			}
			_, err = svc.CreatePurchaseCharge(context.Background(), in)
			codeIs(t, err, "RECONCILIATION_REQUIRED")
			if calls != 1 {
				t.Fatal("second charge")
			}
		})
	}
}
func TestDurableNotChargedAndLeaseLateCommit(t *testing.T) {
	db, svc := newPurchaseTestServiceWithDB(t, nil, nil, nil, nil, 500)
	in := validPurchaseInput()
	_, err := svc.CreatePurchaseCharge(context.Background(), in)
	codeIs(t, err, "NOT_CHARGED")
	got, err := svc.GetIntentRecovery(context.Background(), in.UserID, in.IdempotencyKey)
	if err != nil || got.ExecutionStatus != "not_charged" || got.Financial.Status != "not_charged" {
		t.Fatalf("%+v %v", got, err)
	}
	_, err = svc.CreatePurchaseCharge(context.Background(), in)
	codeIs(t, err, "NOT_CHARGED")
	in.IdempotencyKey = "purchase-v2:" + uuid.NewString()
	store := NewGormStore(db).(*GormStore)
	row, won, err := store.Reserve(context.Background(), in, requestHash(in))
	if err != nil || !won {
		t.Fatal(err)
	}
	if err := db.Model(row).Update("lease_until", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	got, err = svc.GetIntentRecovery(context.Background(), in.UserID, in.IdempotencyKey)
	if err != nil || got.ExecutionStatus != "unknown" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := store.MarkEffect(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	p := &PurchaseCharge{PurchaseID: uuid.NewString(), UserID: in.UserID, StripePaymentIntent: "pi_late", ChargeIntentID: &in.IdempotencyKey, ChargeOperation: handlers.Ptr(handlers.PurchaseOperation), RequestHash: handlers.Ptr(requestHash(in)), HashVersion: handlers.Ptr(1)}
	if err := store.CompleteCharge(context.Background(), row, p); err != nil {
		t.Fatal(err)
	}
	if err := store.FailIntent(context.Background(), row, true, recoveryError(503, "RECONCILIATION_REQUIRED", in.IdempotencyKey)); !errors.Is(err, handlers.ErrIntentCAS) {
		t.Fatalf("terminal result overwritten: %v", err)
	}
}
func TestDurableDuplicatePIStopsBeforeEscrow(t *testing.T) {
	for _, legacy := range []bool{true, false} {
		t.Run(fmt.Sprint(legacy), func(t *testing.T) {
			escrow := &mockEscrowCreator{}
			db, svc := newPurchaseTestServiceWithDB(t, goodPrices(), &mockPriceOracle{}, &mockChargeClient{}, escrow, 500)
			old := &PurchaseCharge{PurchaseID: uuid.NewString(), UserID: "other-user", StripePaymentIntent: "pi_123"}
			if !legacy {
				old.ChargeIntentID = handlers.Ptr("purchase-v2:" + uuid.NewString())
				old.RequestHash = handlers.Ptr("other")
				old.HashVersion = handlers.Ptr(1)
			}
			if err := db.Create(old).Error; err != nil {
				t.Fatal(err)
			}
			_, err := svc.CreatePurchaseCharge(context.Background(), validPurchaseInput())
			codeIs(t, err, "PAYMENT_INTENT_CONFLICT")
			if escrow.called {
				t.Fatal("duplicate PI enqueued escrow")
			}
		})
	}
}
func TestDurableHTTPReplayCanonicalJSONAndLostResponse(t *testing.T) {
	db, svc := newPurchaseTestServiceWithDB(t, goodPrices(), &mockPriceOracle{}, &mockChargeClient{}, nil, 500, map[uint64]string{1: "0x2"})
	h := NewHandler(svc)
	bodies := []string{purchaseBody, `{"editionId":1,"seller":"0x2","buyer":"0x1","stripeCustomerId":"cus_123","artworkId":"art-1","artworkKind":"edition","userId":"user-1","paymentMethodId":"","shippingCents":0,"unlockAt":123}`}
	var original []byte
	for _, body := range bodies {
		r := withClaims(httptest.NewRequest("POST", "/v1/purchases:charge", strings.NewReader(body)), "user-1", "studio.charge.create")
		w := httptest.NewRecorder()
		h.CreatePurchaseCharge().ServeHTTP(w, r)
		if w.Code != 201 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		if original == nil {
			original = append([]byte(nil), w.Body.Bytes()...)
		} else if !bytes.Equal(original, w.Body.Bytes()) {
			t.Fatal("replay changed")
		}
	}
	var p PurchaseCharge
	if err := json.Unmarshal(original, &p); err != nil {
		t.Fatal(err)
	}
	// Discard the response entirely and recreate service; GET still recovers it.
	restarted := NewService(NewGormStore(db), nil, nil, nil, nil, nil, nil, nil, 0, 0)
	got, err := restarted.GetIntentRecovery(context.Background(), "user-1", validPurchaseInput().IdempotencyKey)
	if err != nil || got.Purchase.PurchaseID != p.PurchaseID {
		t.Fatalf("%v", err)
	}
	for _, body := range []string{strings.TrimSuffix(purchaseBody, "}") + `,"unknownTerm":1}`, purchaseBody + " {}"} {
		r := withClaims(httptest.NewRequest(http.MethodPost, "/v1/purchases:charge", strings.NewReader(body)), "user-1", "studio.charge.create")
		w := httptest.NewRecorder()
		h.CreatePurchaseCharge().ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("unexpected body accepted: %d", w.Code)
		}
	}
}

func TestDurableHTTPTimeoutRetainsUnknown(t *testing.T) {
	var calls atomic.Int32
	stripe := chargeFunc(func(ctx context.Context, _ studio.StripeChargeInput) (*studio.StripePaymentIntent, error) {
		calls.Add(1)
		<-ctx.Done()
		return &studio.StripePaymentIntent{ID: "pi_after_timeout", Status: "succeeded"}, nil
	})
	db, svc := newPurchaseTestServiceWithDB(t, goodPrices(), &mockPriceOracle{}, stripe, nil, 500, map[uint64]string{1: "0x2"})
	finished := make(chan struct{})
	h := http.TimeoutHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		NewHandler(svc).CreatePurchaseCharge().ServeHTTP(w, r)
	}), 20*time.Millisecond, "timed out")
	r := withClaims(httptest.NewRequest("POST", "/v1/purchases:charge", strings.NewReader(purchaseBody)), "user-1", "studio.charge.create")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatalf("%d", w.Code)
	}
	select {
	case <-finished:
	case <-time.After(6 * time.Second):
		t.Fatal("handler cleanup did not finish")
	}
	row := reserveRow(t, db, validPurchaseInput())
	if handlers.Value(row.ExecutionState) != "unknown" {
		t.Fatalf("state=%s", handlers.Value(row.ExecutionState))
	}
	r = withClaims(httptest.NewRequest("POST", "/v1/purchases:charge", strings.NewReader(purchaseBody)), "user-1", "studio.charge.create")
	w = httptest.NewRecorder()
	NewHandler(svc).CreatePurchaseCharge().ServeHTTP(w, r)
	if w.Code != 409 || calls.Load() != 1 {
		t.Fatalf("timeout replay status=%d calls=%d", w.Code, calls.Load())
	}
}

func TestDurableExistingPurchaseWithoutReservationNeverCharges(t *testing.T) {
	stripe := &mockChargeClient{}
	db, svc := newPurchaseTestServiceWithDB(t, goodPrices(), &mockPriceOracle{}, stripe, nil, 500)
	in := validPurchaseInput()
	in.ChipID = ""
	p, err := svc.CreatePurchaseCharge(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	row := reserveRow(t, db, in)
	if err := db.Delete(row).Error; err != nil {
		t.Fatal(err)
	}
	stripe.called = false
	_, err = svc.CreatePurchaseCharge(context.Background(), in)
	codeIs(t, err, "RESULT_BODY_EXPIRED")
	if stripe.called {
		t.Fatal("positive purchase evidence caused another charge")
	}
	r, err := svc.GetIntentRecovery(context.Background(), in.UserID, in.IdempotencyKey)
	if err != nil || r.Purchase.PurchaseID != p.PurchaseID || r.ExecutionStatus != "recorded" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestDurablePIIsFencedBeforeEscrowAcrossIntents(t *testing.T) {
	entered, release := make(chan struct{}, 2), make(chan struct{})
	stripe := chargeFunc(func(context.Context, studio.StripeChargeInput) (*studio.StripePaymentIntent, error) {
		entered <- struct{}{}
		<-release
		return &studio.StripePaymentIntent{ID: "pi_collision", Status: "succeeded"}, nil
	})
	db, svc := newPurchaseTestServiceWithDB(t, goodPrices(), &mockPriceOracle{}, stripe, &mockEscrowCreator{}, 500)
	// SQLite serializes writes in this test; the CAS/unique constraints still
	// decide ownership, not a Go mutex or the provider mock.
	sql, _ := db.DB()
	sql.SetMaxOpenConns(1)
	in1, in2 := validPurchaseInput(), validPurchaseInput()
	in2.IdempotencyKey = "purchase-v2:" + uuid.NewString()
	done := make(chan error, 2)
	for _, in := range []CreatePurchaseChargeInput{in1, in2} {
		go func(in CreatePurchaseChargeInput) {
			_, err := svc.CreatePurchaseCharge(context.Background(), in)
			done <- err
		}(in)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case err := <-done:
			t.Fatalf("failed before barrier: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("barrier timeout")
		}
	}
	close(release)
	errs := []error{<-done, <-done}
	successes, conflicts := 0, 0
	for _, err := range errs {
		if err == nil {
			successes++
		} else {
			codeIs(t, err, "PAYMENT_INTENT_CONFLICT")
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("results=%v", errs)
	}
	var count int64
	db.Model(&PurchaseCharge{}).Count(&count)
	if count != 1 {
		t.Fatalf("obligations=%d", count)
	}
}

func TestDurableMatchingPIRecoversPriorPurchaseWithoutEscrow(t *testing.T) {
	var db *gorm.DB
	in := validPurchaseInput()
	stripe := chargeFunc(func(context.Context, studio.StripeChargeInput) (*studio.StripePaymentIntent, error) {
		p := &PurchaseCharge{PurchaseID: uuid.NewString(), UserID: in.UserID, StripePaymentIntent: "pi_same", ChargeIntentID: &in.IdempotencyKey, ChargeOperation: handlers.Ptr(handlers.PurchaseOperation), RequestHash: handlers.Ptr(requestHash(in)), HashVersion: handlers.Ptr(1), Status: PurchaseStatusPaidPendingEscrow}
		return &studio.StripePaymentIntent{ID: "pi_same", Status: "succeeded"}, db.Create(p).Error
	})
	escrow := &mockEscrowCreator{}
	var svc Service
	db, svc = newPurchaseTestServiceWithDB(t, goodPrices(), &mockPriceOracle{}, stripe, escrow, 500)
	p, err := svc.CreatePurchaseCharge(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if escrow.called || p.StripePaymentIntent != "pi_same" || len(p.responseBody) == 0 {
		t.Fatal("prior matching PI not recovered safely")
	}
	var count int64
	db.Model(&PurchaseCharge{}).Count(&count)
	if count != 1 {
		t.Fatal("duplicated obligation")
	}
}

func TestCanonicalHashPreservesUint64AndIgnoresActor(t *testing.T) {
	a := validPurchaseInput()
	a.Nonce = 9007199254740992
	b := a
	b.Nonce++
	if requestHash(a) == requestHash(b) {
		t.Fatal("nonce rounded through float64")
	}
	b = a
	b.ActorSubject = "ops"
	b.IdempotencyKey = "purchase-v2:" + uuid.NewString()
	if requestHash(a) != requestHash(b) {
		t.Fatal("actor or identity changed commercial hash")
	}
}
func TestDurablePreEffectHTTPResultIsExactReplay(t *testing.T) {
	_, svc := newPurchaseTestServiceWithDB(t, nil, nil, nil, nil, 500)
	var first []byte
	for i := 0; i < 2; i++ {
		r := withClaims(httptest.NewRequest("POST", "/v1/purchases:charge", strings.NewReader(purchaseBody)), "user-1", "studio.charge.create")
		w := httptest.NewRecorder()
		NewHandler(svc).CreatePurchaseCharge().ServeHTTP(w, r)
		if w.Code != 503 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		if i == 0 {
			first = append([]byte(nil), w.Body.Bytes()...)
		} else if !bytes.Equal(first, w.Body.Bytes()) {
			t.Fatal("not_charged replay changed bytes")
		}
	}
}
func TestDurableInvalidConfigurationNeverCharges(t *testing.T) {
	stripe := &mockChargeClient{}
	db, _ := newPurchaseTestServiceWithDB(t, nil, nil, nil, nil, 500)
	svc := NewService(NewGormStore(db, DurabilityOptions{}), goodPrices(), &mockPriceOracle{}, stripe, nil, nil, nil, nil, 500, 0)
	_, err := svc.CreatePurchaseCharge(context.Background(), validPurchaseInput())
	codeIs(t, err, "RESERVATION_UNAVAILABLE")
	if stripe.called {
		t.Fatal("invalid config allowed charge")
	}
}

type commitAcknowledgementLost struct{ *GormStore }

func (s commitAcknowledgementLost) CompleteCharge(ctx context.Context, row *handlers.PurchaseIntent, p *PurchaseCharge) error {
	if err := s.GormStore.CompleteCharge(ctx, row, p); err != nil {
		return err
	}
	return errors.New("SQL commit acknowledged too late")
}
func TestDurableCommittedResultSurvivesReportedCommitFailure(t *testing.T) {
	stripe := &mockChargeClient{}
	db, _ := newPurchaseTestServiceWithDB(t, nil, nil, nil, nil, 500)
	store := commitAcknowledgementLost{NewGormStore(db).(*GormStore)}
	svc := NewService(store, goodPrices(), &mockPriceOracle{}, stripe, nil, nil, defaultTestEditionArtists(), nil, 500, testClaimWindowSeconds)
	in := validPurchaseInput()
	in.ChipID = ""
	_, err := svc.CreatePurchaseCharge(context.Background(), in)
	codeIs(t, err, "RECONCILIATION_REQUIRED")
	row := reserveRow(t, db, in)
	if handlers.Value(row.ExecutionState) != "recorded" {
		t.Fatal("late failure overwrote committed result")
	}
	stripe.called = false
	p, err := svc.CreatePurchaseCharge(context.Background(), in)
	if err != nil || p == nil || stripe.called {
		t.Fatalf("replay err=%v stripe=%v", err, stripe.called)
	}
	r, err := svc.GetIntentRecovery(context.Background(), in.UserID, in.IdempotencyKey)
	if err != nil || r.ExecutionStatus != "recorded" || r.Purchase.PurchaseID != p.PurchaseID {
		t.Fatalf("%+v %v", r, err)
	}
}
