package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockStudioService is a minimal in-memory implementation of Service
// for handler tests.
type mockStudioService struct {
	charges []ProductionCharge
	err     error
	lastIn  CreateStockRequestChargeInput
}

func (m *mockStudioService) RecordProductionCharge(in CreateProductionChargeInput) (*ProductionCharge, error) {
	if m.err != nil {
		return nil, m.err
	}
	c := &ProductionCharge{
		ID:                  uint(len(m.charges) + 1),
		UserID:              in.UserID,
		QuoteID:             in.QuoteID,
		AmountCents:         in.AmountCents,
		Currency:            in.Currency,
		StripePaymentIntent: in.StripePaymentIntent,
		PricingHash:         in.PricingHash,
		EngineVersion:       in.EngineVersion,
		Metadata:            in.Metadata,
	}
	m.charges = append(m.charges, *c)
	return c, nil
}

func (m *mockStudioService) CreateStockRequestCharge(ctx context.Context, in CreateStockRequestChargeInput) (*ProductionCharge, error) {
	m.lastIn = in
	if m.err != nil {
		return nil, m.err
	}
	c := &ProductionCharge{
		ID:                  uint(len(m.charges) + 1),
		UserID:              in.UserID,
		QuoteID:             in.QuoteID,
		AmountCents:         2500,
		Currency:            "usd",
		StripePaymentIntent: "pi_123",
		PricingHash:         "hash-abc",
		EngineVersion:       "v1",
		Metadata:            in.Metadata,
	}
	m.charges = append(m.charges, *c)
	return c, nil
}

func (m *mockStudioService) ListProductionChargesByUser(userID string) ([]ProductionCharge, error) {
	if m.err != nil {
		return nil, m.err
	}
	var out []ProductionCharge
	for _, c := range m.charges {
		if c.UserID == userID {
			out = append(out, c)
		}
	}
	return out, nil
}

func newStudioHandler(svc Service) *Handler {
	return NewHandler(svc)
}

func TestCreateStockRequestHappyPath(t *testing.T) {
	svc := &mockStudioService{}
	h := newStudioHandler(svc)

	body := `{"userId":"user-1","quoteId":"quote-1","quantityRequested":10,"stripeCustomerId":"cus_123","paymentMethodId":"pm_123"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/stock-requests:create", bytes.NewBufferString(body))
	req.Header.Set("Idempotency-Key", "idem-abc")
	rr := httptest.NewRecorder()

	h.CreateStockRequest().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp ProductionCharge
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}
	if resp.StripePaymentIntent != "pi_123" {
		t.Fatalf("expected payment intent pi_123, got %s", resp.StripePaymentIntent)
	}
	// The Idempotency-Key header must be propagated to the service so it can
	// be forwarded to Stripe.
	if svc.lastIn.IdempotencyKey != "idem-abc" {
		t.Fatalf("expected Idempotency-Key idem-abc propagated to service, got %q", svc.lastIn.IdempotencyKey)
	}
}

func TestCreateStockRequestConflictOnDuplicate(t *testing.T) {
	h := newStudioHandler(&mockStudioService{err: ErrChargeAlreadyRecorded})

	body := `{"userId":"user-1","quoteId":"quote-1","quantityRequested":10,"stripeCustomerId":"cus_123"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/stock-requests:create", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()

	h.CreateStockRequest().ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
}

// A failed audit write after Stripe charged must respond 500, so the
// idempotency middleware releases the key instead of caching the failure.
func TestCreateStockRequestRecordFailureIsServerError(t *testing.T) {
	h := newStudioHandler(&mockStudioService{err: ErrChargeRecordFailed})

	body := `{"userId":"user-1","quoteId":"quote-1","quantityRequested":10,"stripeCustomerId":"cus_123"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/stock-requests:create", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()

	h.CreateStockRequest().ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateStockRequestEmptyBody(t *testing.T) {
	h := newStudioHandler(&mockStudioService{})

	req := httptest.NewRequest(http.MethodPost, "/v1/stock-requests:create", nil)
	rr := httptest.NewRecorder()

	h.CreateStockRequest().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestCreateStockRequestInvalidBody(t *testing.T) {
	h := newStudioHandler(&mockStudioService{})

	req := httptest.NewRequest(http.MethodPost, "/v1/stock-requests:create", bytes.NewBufferString("not-json"))
	rr := httptest.NewRecorder()

	h.CreateStockRequest().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestListChargesHappyPath(t *testing.T) {
	svc := &mockStudioService{}
	svc.charges = append(svc.charges, ProductionCharge{
		ID:                  1,
		UserID:              "user-1",
		AmountCents:         2500,
		StripePaymentIntent: "pi_123",
	})
	h := newStudioHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/v1/studio/charges?userId=user-1", nil)
	rr := httptest.NewRecorder()

	h.ListCharges().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp []ProductionCharge
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 charge, got %d", len(resp))
	}
}

func TestListChargesMissingUserID(t *testing.T) {
	h := newStudioHandler(&mockStudioService{})

	req := httptest.NewRequest(http.MethodGet, "/v1/studio/charges", nil)
	rr := httptest.NewRecorder()

	h.ListCharges().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestListChargesServiceError(t *testing.T) {
	h := newStudioHandler(&mockStudioService{err: errors.New("boom")})

	req := httptest.NewRequest(http.MethodGet, "/v1/studio/charges?userId=user-1", nil)
	rr := httptest.NewRecorder()

	h.ListCharges().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}
