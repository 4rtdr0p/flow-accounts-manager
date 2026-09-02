package purchase

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop/authguard"
	"github.com/flow-hydraulics/flow-wallet-api/handlers/middleware"
	jwt "github.com/golang-jwt/jwt/v5"
)

// mockPurchaseService is a minimal in-memory implementation of Service for
// handler tests. It records the last input and how many times it was called so
// tests can assert the identity guard runs before the service.
type mockPurchaseService struct {
	calls  int
	lastIn CreatePurchaseChargeInput
	err    error
}

func (m *mockPurchaseService) CreatePurchaseCharge(ctx context.Context, in CreatePurchaseChargeInput) (*PurchaseCharge, error) {
	m.calls++
	m.lastIn = in
	if m.err != nil {
		return nil, m.err
	}
	return &PurchaseCharge{
		ID:                  1,
		UserID:              in.UserID,
		StripePaymentIntent: "pi_123",
	}, nil
}

// withClaims attaches auth claims to the request context the same way the auth
// middleware does, so the identity guard sees an authenticated caller.
func withClaims(r *http.Request, subject, scope string) *http.Request {
	claims := &middleware.AuthClaims{
		Scope:            scope,
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

	req := withClaims(httptest.NewRequest(http.MethodPost, "/v1/purchases:charge", bytes.NewBufferString(purchaseBody)), "operator-9", authguard.ScopeChargeOnBehalf)
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

func TestCreatePurchaseChargeGuardAuthOffPassthrough(t *testing.T) {
	// No claims in context (auth disabled): the request must pass unchanged.
	svc := &mockPurchaseService{}
	h := NewHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/v1/purchases:charge", bytes.NewBufferString(purchaseBody))
	rr := httptest.NewRecorder()

	h.CreatePurchaseCharge().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 with auth off, got %d: %s", rr.Code, rr.Body.String())
	}
}
