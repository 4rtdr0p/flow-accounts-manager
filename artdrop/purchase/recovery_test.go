package purchase

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/flow-hydraulics/flow-wallet-api/handlers/middleware"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

func TestRecoveryAuthScopesOwnerAndSelector(t *testing.T) {
	db, svc := newPurchaseTestServiceWithDB(t, goodPrices(), &mockPriceOracle{}, &mockChargeClient{}, nil, 500)
	in := validPurchaseInput()
	in.ChipID = ""
	p, err := svc.CreatePurchaseCharge(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	// Recovery cannot rely on live dependencies or invoke any effects.
	svc = NewService(NewGormStore(db), nil, nil, nil, nil, nil, nil, nil, 0, 0)
	router := mux.NewRouter()
	h := NewHandler(svc)
	router.Handle("/v1/purchases/{purchaseId}", h.GetPurchase())
	router.Handle("/v1/purchase-intents/{intentId}", h.GetIntent())
	router.Handle("/v1/purchases:charge", h.CreatePurchaseCharge())
	purchasePath := "/v1/purchases/" + p.PurchaseID
	intentPath := "/v1/purchase-intents/" + in.IdempotencyKey
	for _, tc := range []struct {
		name, path, sub, scope string
		want                   int
	}{
		{"no claims", purchasePath, "", "", 401}, {"no intent claims", intentPath, "", "", 401},
		{"wrong scope", purchasePath, "user-1", "ops.read", 403}, {"missing read with capability", purchasePath, "ops", "purchase.read.any", 403},
		{"owner", purchasePath, "user-1", "purchase.read", 200}, {"intent owner", intentPath, "user-1", "purchase.read", 200},
		{"own selector", intentPath + "?userId=user-1", "user-1", "purchase.read", 200},
		{"other row", purchasePath, "other", "purchase.read", 404}, {"other intent", intentPath, "other", "purchase.read", 404},
		{"forbidden selector", intentPath + "?userId=user-1", "other", "purchase.read", 403},
		{"ops.read no bypass", purchasePath, "other", "purchase.read ops.read", 404},
		{"creation no bypass", purchasePath, "other", "purchase.read studio.charge.create.onbehalf", 404},
		{"ops purchase", purchasePath, "ops", "purchase.read purchase.read.any", 200},
		{"ops selector", intentPath + "?userId=user-1", "ops", "purchase.read purchase.read.any", 200},
		{"ops no global lookup", intentPath, "ops", "purchase.read purchase.read.any", 404},
		{"wildcard", purchasePath, "admin", "*", 200},
		{"invalid purchase", "/v1/purchases/bad", "user-1", "purchase.read", 400},
		{"invalid intent", "/v1/purchase-intents/legacy", "user-1", "purchase.read", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", tc.path, nil)
			if tc.sub != "" {
				r = withClaims(r, tc.sub, tc.scope)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tc.want || w.Header().Get("Cache-Control") != "no-store" || !json.Valid(w.Body.Bytes()) {
				t.Fatalf("status=%d cache=%s body=%s", w.Code, w.Header().Get("Cache-Control"), w.Body.String())
			}
		})
	}
	// POST replay checks authorization before looking at durable bytes.
	for _, tc := range []struct {
		sub, scope string
		want       int
	}{{"other", "studio.charge.create", 403}, {"ops", "studio.charge.create studio.charge.create.onbehalf", 201}} {
		body, _ := json.Marshal(createPurchaseChargeRequest{UserID: in.UserID, ArtworkKind: string(in.ArtworkKind), ArtworkID: in.ArtworkID, StripeCustomerID: in.StripeCustomerID, PaymentMethodID: in.PaymentMethodID, Buyer: in.Buyer, Seller: in.Seller, EditionID: in.EditionID, Nonce: in.Nonce})
		r := withClaims(httptest.NewRequest("POST", "/v1/purchases:charge", strings.NewReader(string(body))), tc.sub, tc.scope)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("replay auth: %d %s", w.Code, w.Body.String())
		}
	}
}
func TestRecoveryExpiredBearerAndScopeMiddleware(t *testing.T) {
	_, svc := newPurchaseTestServiceWithDB(t, nil, nil, nil, nil, 500)
	h := NewHandler(svc)
	protected := middleware.AuthHandler(h.GetIntent(), middleware.AuthOptions{Enabled: true, Secret: "secret", Rules: []middleware.AuthRule{middleware.NewAuthRule("GET", "/v1/purchase-intents/{intentId}", "purchase.read")}})
	for _, tc := range []struct {
		name, scope string
		expiry      time.Time
		want        int
	}{{"expired", "purchase.read", time.Now().Add(-time.Minute), 401}, {"scope", "ops.read", time.Now().Add(time.Minute), 403}} {
		t.Run(tc.name, func(t *testing.T) {
			claims := middleware.AuthClaims{Scope: tc.scope, RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1", ExpiresAt: jwt.NewNumericDate(tc.expiry)}}
			token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("secret"))
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("GET", "/v1/purchase-intents/"+validPurchaseInput().IdempotencyKey, nil)
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			protected.ServeHTTP(w, r)
			if w.Code != tc.want || !json.Valid(w.Body.Bytes()) || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
}
func TestRecoveryEvidenceLegacyAndObservedStates(t *testing.T) {
	for _, status := range []string{"succeeded", "processing", "requires_action"} {
		t.Run(status, func(t *testing.T) {
			now := time.Now().UTC()
			p := &PurchaseCharge{PurchaseID: uuid.NewString(), ChargeIntentID: handlers.Ptr(validPurchaseInput().IdempotencyKey), AmountCents: 10000, ShippingCents: 500, StripePaymentIntent: "pi", StripeCustomerID: handlers.Ptr("cus"), StripeStatusObserved: &status, StripeObservedAt: &now, Status: PurchaseStatusEscrowOpening, EscrowJobID: "job"}
			r := recoveryResponse(nil, p)
			if r.Purchase.Financial.Status != "unknown" || r.Purchase.Financial.VerifiedAt != nil || r.Purchase.Amounts.TotalCents != 10500 || r.Purchase.Amounts.PresentationCents != nil || r.Purchase.Escrow.Status != "opening" || r.Purchase.Escrow.EscrowID != nil {
				t.Fatalf("false evidence: %+v", r)
			}
		})
	}
	db, svc := newPurchaseTestServiceWithDB(t, nil, nil, nil, nil, 500)
	p := &PurchaseCharge{PurchaseID: uuid.NewString(), UserID: "legacy", StripePaymentIntent: "pi_legacy"}
	if err := db.Create(p).Error; err != nil {
		t.Fatal(err)
	}
	r, err := svc.GetPurchaseRecovery(context.Background(), p.PurchaseID)
	if err != nil {
		t.Fatal(err)
	}
	if r.IntentID != nil || r.Purchase.RequestHash != nil || r.Purchase.HashVersion != nil || r.Purchase.Financial.StripeCustomerID != nil || r.Purchase.Financial.EvidenceSource != "legacy_record" {
		t.Fatalf("invented legacy evidence: %+v", r)
	}
	_, err = svc.GetPurchaseRecovery(context.Background(), uuid.NewString())
	codeIs(t, err, "PURCHASE_NOT_FOUND")
	_, err = svc.GetIntentRecovery(context.Background(), "user-1", validPurchaseInput().IdempotencyKey)
	codeIs(t, err, "INTENT_NOT_FOUND")
	if err := db.Exec("DROP TABLE purchase_charges").Error; err != nil {
		t.Fatal(err)
	}
	_, err = svc.GetPurchaseRecovery(context.Background(), p.PurchaseID)
	codeIs(t, err, "LOOKUP_UNAVAILABLE")
	_, err = svc.GetIntentRecovery(context.Background(), "user-1", validPurchaseInput().IdempotencyKey)
	codeIs(t, err, "LOOKUP_UNAVAILABLE")
}
func TestRecoveryIndexFallbackAndEvidenceConflict(t *testing.T) {
	db, svc := newPurchaseTestServiceWithDB(t, goodPrices(), &mockPriceOracle{}, &mockChargeClient{}, nil, 500)
	in := validPurchaseInput()
	in.ChipID = ""
	p, err := svc.CreatePurchaseCharge(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	row := reserveRow(t, db, in)
	if err := db.Model(row).Update("purchase_id", nil).Error; err != nil {
		t.Fatal(err)
	}
	r, err := svc.GetIntentRecovery(context.Background(), in.UserID, in.IdempotencyKey)
	if err != nil || r.ExecutionStatus != "recorded" || r.Purchase.PurchaseID != p.PurchaseID {
		t.Fatalf("index fallback: %+v %v", r, err)
	}
	for _, column := range []string{"request_hash", "stripe_payment_intent_id"} {
		original := requestHash(in)
		if column == "stripe_payment_intent_id" {
			original = p.StripePaymentIntent
		}
		if err := db.Model(row).Update(column, "conflict").Error; err != nil {
			t.Fatal(err)
		}
		r, err := svc.GetIntentRecovery(context.Background(), in.UserID, in.IdempotencyKey)
		if err != nil || r.ExecutionStatus != "unknown" || r.ResolutionCode != "EVIDENCE_CONFLICT" {
			t.Fatalf("conflict hidden: %+v %v", r, err)
		}
		if err := db.Model(row).Update(column, original).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Delete(row).Error; err != nil {
		t.Fatal(err)
	}
	r, err = svc.GetIntentRecovery(context.Background(), in.UserID, in.IdempotencyKey)
	if err != nil || r.ExecutionStatus != "recorded" {
		t.Fatalf("missing reservation fallback: %+v %v", r, err)
	}
}

func TestRecoveryOwnerComparisonIsByteExact(t *testing.T) {
	db, svc := newPurchaseTestServiceWithDB(t, nil, nil, nil, nil, 500)
	if err := db.Migrator().DropTable(&PurchaseCharge{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE purchase_charges (id INTEGER PRIMARY KEY, user_id TEXT COLLATE NOCASE)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&PurchaseCharge{}); err != nil {
		t.Fatal(err)
	}
	in := validPurchaseInput()
	p := &PurchaseCharge{UserID: "User-1", PurchaseID: uuid.NewString(), StripePaymentIntent: "pi_case", ChargeIntentID: &in.IdempotencyKey, ChargeOperation: handlers.Ptr(handlers.PurchaseOperation)}
	if err := db.Create(p).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(p).Where("user_id = ?", "user-1").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("case-insensitive fixture: %d %v", count, err)
	}
	_, err := svc.GetIntentRecovery(context.Background(), "user-1", in.IdempotencyKey)
	codeIs(t, err, "INTENT_NOT_FOUND")
}
