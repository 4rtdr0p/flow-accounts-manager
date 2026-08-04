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
	if !strings.Contains(rr.Body.String(), "read active studio pricing configuration") {
		t.Fatalf("expected wrapped error in body, got %s", rr.Body.String())
	}
}
