package pricing

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
)

func doGetActive(svc *ActiveService) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/v1/studio/pricing/active", nil)
	rr := httptest.NewRecorder()
	NewHandler(svc).GetActive().ServeHTTP(rr, req)
	return rr
}

func TestGetActiveHandlerOK(t *testing.T) {
	reader := &fakeReader{cfgs: []*datastoremongo.PricingConfiguration{activeConfig(time.Now())}}
	rr := doGetActive(NewActiveService(reader, time.Minute))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"domain":"studio-printing"`) {
		t.Fatalf("expected domain in response body, got %s", rr.Body.String())
	}
}

func TestGetActiveHandlerDisabled(t *testing.T) {
	rr := doGetActive(NewActiveService(nil, time.Minute))

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestGetActiveHandlerNotFound(t *testing.T) {
	reader := &fakeReader{errs: []error{datastoremongo.ErrNoActivePricing}}
	rr := doGetActive(NewActiveService(reader, time.Minute))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestGetActiveHandlerInternalError(t *testing.T) {
	reader := &fakeReader{errs: []error{errors.New("mongo down")}}
	rr := doGetActive(NewActiveService(reader, time.Minute))

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rr.Code, rr.Body.String())
	}
	// The internal driver error must be logged, never leaked to the client.
	if strings.Contains(rr.Body.String(), "mongo down") {
		t.Fatalf("expected generic 500 body without internal error, got %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "internal server error") {
		t.Fatalf("expected generic message in body, got %s", rr.Body.String())
	}
}

func TestQuoteHandlerRejectsInvalidConfig(t *testing.T) {
	reader := &fakeReader{cfgs: []*datastoremongo.PricingConfiguration{
		quoteActiveConfig(time.Now(), mongoDataFromVariables(t)),
	}}
	active := NewActiveService(reader, time.Minute)
	svc := NewQuoteService(active)
	handler := NewQuoteHandler(svc)

	body := strings.NewReader(`{"process":"Metal Print","W":-10,"L":30,"run_size":10}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/studio/quotes:price", body)
	rr := httptest.NewRecorder()
	handler.Quote().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "W must be > 0") {
		t.Fatalf("expected validation message in body, got %s", rr.Body.String())
	}
}
