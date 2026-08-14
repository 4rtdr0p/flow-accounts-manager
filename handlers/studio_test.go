package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/studio"
)

// mockStudioService is a minimal in-memory implementation of studio.Service
// for handler tests.
type mockStudioService struct {
	charges []studio.ProductionCharge
	err     error
}

func (m *mockStudioService) RecordProductionCharge(in studio.CreateProductionChargeInput) (*studio.ProductionCharge, error) {
	if m.err != nil {
		return nil, m.err
	}
	c := &studio.ProductionCharge{
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

func (m *mockStudioService) CreateStockRequestCharge(ctx context.Context, in studio.CreateStockRequestChargeInput) (*studio.ProductionCharge, error) {
	if m.err != nil {
		return nil, m.err
	}
	c := &studio.ProductionCharge{
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

func (m *mockStudioService) ListProductionChargesByUser(userID string) ([]studio.ProductionCharge, error) {
	if m.err != nil {
		return nil, m.err
	}
	var out []studio.ProductionCharge
	for _, c := range m.charges {
		if c.UserID == userID {
			out = append(out, c)
		}
	}
	return out, nil
}

func newStudioHandler(svc studio.Service) *Studio {
	return NewStudio(svc)
}

func TestCreateStockRequestHappyPath(t *testing.T) {
	h := newStudioHandler(&mockStudioService{})

	body := `{"userId":"user-1","quoteId":"quote-1","quantityRequested":10,"stripeCustomerId":"cus_123","paymentMethodId":"pm_123"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/stock-requests:create", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()

	h.CreateStockRequest().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp studio.ProductionCharge
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}
	if resp.StripePaymentIntent != "pi_123" {
		t.Fatalf("expected payment intent pi_123, got %s", resp.StripePaymentIntent)
	}
}

func TestCreateStockRequestConflictOnDuplicate(t *testing.T) {
	h := newStudioHandler(&mockStudioService{err: studio.ErrChargeAlreadyRecorded})

	body := `{"userId":"user-1","quoteId":"quote-1","quantityRequested":10,"stripeCustomerId":"cus_123"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/stock-requests:create", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()

	h.CreateStockRequest().ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
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
	svc.charges = append(svc.charges, studio.ProductionCharge{
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

	var resp []studio.ProductionCharge
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
