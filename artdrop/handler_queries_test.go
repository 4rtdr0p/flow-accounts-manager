package artdrop

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/gorilla/mux"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
)

func TestListCertificatesHandlerReturnsOK(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewArray([]cadence.Value{
			cadence.NewUInt64(7),
			cadence.NewUInt64(13),
		}),
	}
	handler := NewHandler(NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/accounts/0xf8d6e0586b0a20c7/artdrop/certificates", nil)
	req = mux.SetURLVars(req, map[string]string{"address": "0xf8d6e0586b0a20c7"})
	rw := httptest.NewRecorder()

	handler.ListCertificates().ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"id":7`) {
		t.Fatalf("expected response to contain certificate id 7, got %s", rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"id":13`) {
		t.Fatalf("expected response to contain certificate id 13, got %s", rw.Body.String())
	}
}

func TestGetEscrowHandlerReturnsOK(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewUInt8(2),
	}
	handler := NewHandler(NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/accounts/0xf8d6e0586b0a20c7/artdrop/escrows/42?logic_owner=0xf8d6e0586b0a20c7", nil)
	req = mux.SetURLVars(req, map[string]string{
		"address":  "0xf8d6e0586b0a20c7",
		"escrowId": "42",
	})
	rw := httptest.NewRecorder()

	handler.GetEscrow().ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"id":42`) {
		t.Fatalf("expected response to contain escrow id 42, got %s", rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"status":2`) {
		t.Fatalf("expected response to contain status 2, got %s", rw.Body.String())
	}
}

func TestGetEscrowHandlerRejectsInvalidEscrowId(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/accounts/0xf8d6e0586b0a20c7/artdrop/escrows/abc", nil)
	req = mux.SetURLVars(req, map[string]string{
		"address":  "0xf8d6e0586b0a20c7",
		"escrowId": "abc",
	})
	rw := httptest.NewRecorder()

	handler.GetEscrow().ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for invalid escrowId, got %d: %s", rw.Code, rw.Body.String())
	}
}

func TestGetEscrowHandlerRequiresLogicOwner(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/accounts/0xf8d6e0586b0a20c7/artdrop/escrows/42", nil)
	req = mux.SetURLVars(req, map[string]string{
		"address":  "0xf8d6e0586b0a20c7",
		"escrowId": "42",
	})
	rw := httptest.NewRecorder()

	handler.GetEscrow().ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for missing logic_owner, got %d: %s", rw.Code, rw.Body.String())
	}
}
