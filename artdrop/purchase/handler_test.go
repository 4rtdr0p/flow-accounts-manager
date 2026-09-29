package purchase

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop/authguard"
	"github.com/flow-hydraulics/flow-wallet-api/handlers/middleware"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
)

// mockPurchaseService is a minimal in-memory implementation of Service for
// handler tests. It records the last input and how many times it was called so
// tests can assert the identity guard runs before the service.
type mockPurchaseService struct {
	calls     int
	lastIn    CreatePurchaseChargeInput
	openCalls int
	lastOpen  OpenEscrowInput
	err       error
}

func (m *mockPurchaseService) OpenEscrow(_ context.Context, in OpenEscrowInput) (*PurchaseCharge, error) {
	m.openCalls++
	m.lastOpen = in
	if m.err != nil {
		return nil, m.err
	}
	return &PurchaseCharge{PurchaseID: in.PurchaseID, Status: PurchaseStatusEscrowOpening, EscrowJobID: "job-1", ChipID: in.ChipID}, nil
}

func (m *mockPurchaseService) CreatePurchaseCharge(ctx context.Context, in CreatePurchaseChargeInput) (*PurchaseCharge, error) {
	m.calls++
	m.lastIn = in
	if m.err != nil {
		return nil, m.err
	}
	p := &PurchaseCharge{ID: 1, UserID: in.UserID, StripePaymentIntent: "pi_123"}
	p.responseBody, _ = json.Marshal(p)
	return p, nil
}

// withClaims attaches auth claims to the request context the same way the auth
// middleware does, so the identity guard sees an authenticated caller.
func withClaims(r *http.Request, subject, scope string) *http.Request {
	r.Header.Set("Idempotency-Key", validPurchaseInput().IdempotencyKey)
	claims := &middleware.AuthClaims{
		Scope:            scope,
		RegisteredClaims: jwt.RegisteredClaims{Subject: subject},
	}
	return r.WithContext(middleware.ContextWithClaims(r.Context(), claims))
}

// withStripeClaims is withClaims plus a stripe_customer_id claim, for tests of
// the Stripe-customer identity guard.
func withStripeClaims(r *http.Request, subject, scope, stripeCustomerID string) *http.Request {
	r.Header.Set("Idempotency-Key", validPurchaseInput().IdempotencyKey)
	claims := &middleware.AuthClaims{
		Scope:            scope,
		StripeCustomerID: stripeCustomerID,
		RegisteredClaims: jwt.RegisteredClaims{Subject: subject},
	}
	return r.WithContext(middleware.ContextWithClaims(r.Context(), claims))
}

const purchaseBody = `{"userId":"user-1","artworkKind":"edition","artworkId":"art-1","stripeCustomerId":"cus_123","buyer":"0x1","seller":"0x2","editionId":1}`

func TestCreatePurchaseChargeGuardSubjectMatches(t *testing.T) {
	svc := &mockPurchaseService{}
	h := NewHandler(svc)

	req := withClaims(httptest.NewRequest(http.MethodPost, "/v1/purchases:charge", bytes.NewBufferString(purchaseBody)), "user-1", "studio.charge.create")
	rr := httptest.NewRecorder()

	h.CreatePurchaseCharge().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 when subject matches userId, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreatePurchaseChargeGuardSubjectMismatchForbidden(t *testing.T) {
	svc := &mockPurchaseService{}
	h := NewHandler(svc)

	req := withClaims(httptest.NewRequest(http.MethodPost, "/v1/purchases:charge", bytes.NewBufferString(purchaseBody)), "user-2", "studio.charge.create")
	rr := httptest.NewRecorder()

	h.CreatePurchaseCharge().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when subject != userId, got %d: %s", rr.Code, rr.Body.String())
	}
	// The guard must run before the service: no charge should be attempted.
	if svc.calls != 0 {
		t.Fatalf("expected service not called on a blocked request, got %d calls", svc.calls)
	}
}

func TestCreatePurchaseChargeGuardOnBehalfBypass(t *testing.T) {
	svc := &mockPurchaseService{}
	h := NewHandler(svc)

	req := withClaims(httptest.NewRequest(http.MethodPost, "/v1/purchases:charge", bytes.NewBufferString(purchaseBody)), "operator-9", "studio.charge.create "+authguard.ScopeChargeOnBehalf)
	rr := httptest.NewRecorder()

	h.CreatePurchaseCharge().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 for on-behalf operator, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreatePurchaseChargeGuardEmptySubjectForbidden(t *testing.T) {
	svc := &mockPurchaseService{}
	h := NewHandler(svc)

	req := withClaims(httptest.NewRequest(http.MethodPost, "/v1/purchases:charge", bytes.NewBufferString(purchaseBody)), "", "studio.charge.create")
	rr := httptest.NewRecorder()

	h.CreatePurchaseCharge().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for empty subject, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreatePurchaseChargeGuardAuthOffFailsClosed(t *testing.T) {
	// Durable financial requests require claims, including with auth disabled.
	svc := &mockPurchaseService{}
	h := NewHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/v1/purchases:charge", bytes.NewBufferString(purchaseBody))
	rr := httptest.NewRecorder()

	h.CreatePurchaseCharge().ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized || svc.calls != 0 {
		t.Fatalf("expected 401 with auth off, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreatePurchaseChargeGuardStripeCustomerMismatchForbidden(t *testing.T) {
	svc := &mockPurchaseService{}
	h := NewHandler(svc)

	// Body requests cus_123 (see purchaseBody); token is bound to a different
	// customer.
	req := withStripeClaims(httptest.NewRequest(http.MethodPost, "/v1/purchases:charge", bytes.NewBufferString(purchaseBody)), "user-1", "studio.charge.create", "cus_999")
	rr := httptest.NewRecorder()

	h.CreatePurchaseCharge().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when stripe_customer_id claim != stripeCustomerId, got %d: %s", rr.Code, rr.Body.String())
	}
	if svc.calls != 0 {
		t.Fatalf("expected service not called on a blocked request, got %d calls", svc.calls)
	}
}

func TestCreatePurchaseChargeGuardStripeCustomerMatches(t *testing.T) {
	svc := &mockPurchaseService{}
	h := NewHandler(svc)

	req := withStripeClaims(httptest.NewRequest(http.MethodPost, "/v1/purchases:charge", bytes.NewBufferString(purchaseBody)), "user-1", "studio.charge.create", "cus_123")
	rr := httptest.NewRecorder()

	h.CreatePurchaseCharge().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 when stripe_customer_id claim matches stripeCustomerId, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreatePurchaseChargePaymentNotSucceededReturnsPaymentRequired(t *testing.T) {
	svc := &mockPurchaseService{err: ErrPaymentNotSucceeded}
	h := NewHandler(svc)
	req := withClaims(httptest.NewRequest(http.MethodPost, "/v1/purchases:charge", bytes.NewBufferString(purchaseBody)), "user-1", "studio.charge.create")
	rr := httptest.NewRecorder()

	h.CreatePurchaseCharge().ServeHTTP(rr, req)

	if rr.Code != http.StatusPaymentRequired {
		t.Fatalf("expected 402 for payment not succeeded, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestOpenEscrowRejectsFieldsOutsideAllowList(t *testing.T) {
	svc := &mockPurchaseService{}
	h := NewHandler(svc)
	req := httptest.NewRequest(http.MethodPost, "/v1/purchases/purchase-1:open-escrow", bytes.NewBufferString(`{"chipId":"chip-1","nonce":1,"amount":999}`))
	req.Header.Set("Idempotency-Key", "open-1")
	req = mux.SetURLVars(req, map[string]string{"purchaseId": "purchase-1"})
	rr := httptest.NewRecorder()
	h.OpenEscrow().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest || svc.openCalls != 0 {
		t.Fatalf("got status=%d calls=%d, want 400 and no service call", rr.Code, svc.openCalls)
	}
}

func TestOpenEscrowRequiresIdempotencyKey(t *testing.T) {
	svc := &mockPurchaseService{}
	h := NewHandler(svc)
	req := httptest.NewRequest(http.MethodPost, "/v1/purchases/purchase-1:open-escrow", bytes.NewBufferString(`{"chipId":"chip-1","nonce":1}`))
	req = mux.SetURLVars(req, map[string]string{"purchaseId": "purchase-1"})
	rr := httptest.NewRecorder()
	h.OpenEscrow().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestOpenEscrowForbiddenWithoutOperationsScope(t *testing.T) {
	svc := &mockPurchaseService{}
	h := NewHandler(svc)
	protected := middleware.AuthHandler(h.OpenEscrow(), middleware.AuthOptions{
		Enabled: true,
		Secret:  "test-secret",
		Rules: []middleware.AuthRule{
			middleware.NewAuthRule(http.MethodPost, "/v1/purchases/{purchaseId}:open-escrow", "account.artdrop.escrow.open-paid"),
		},
	})
	claims := middleware.AuthClaims{Scope: "studio.charge.create", RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/purchases/purchase-1:open-escrow", bytes.NewBufferString(`{"chipId":"chip-1","nonce":1}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", "open-1")
	rr := httptest.NewRecorder()
	protected.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden || svc.openCalls != 0 {
		t.Fatalf("got status=%d calls=%d, want 403 and no service call", rr.Code, svc.openCalls)
	}
}

func (m *mockPurchaseService) GetPurchaseRecovery(context.Context, string) (*PurchaseRecoveryResponse, error) {
	return nil, ErrPurchaseNotFound
}
func (m *mockPurchaseService) GetIntentRecovery(context.Context, string, string) (*PurchaseRecoveryResponse, error) {
	return nil, ErrPurchaseNotFound
}
