package artdrop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"

	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
)

// activateTxService extends the setup fake with the script read ActivateEdition
// composes (GetEditionSummary). stateRaw mirrors what get_edition_summary.cdc
// reports: the EditionState rawValue (0=Draft, 1=Active, …). A nil stateRaw
// makes the script return nil — the edition does not exist on-chain.
type activateTxService struct {
	setupTxService
	stateRaw *uint8
}

func (s *activateTxService) ExecuteScript(_ context.Context, _ string, _ []transactions.Argument) (cadence.Value, error) {
	if s.stateRaw == nil {
		return cadence.NewOptional(nil), nil
	}
	return cadence.NewOptional(cadence.NewDictionary([]cadence.KeyValuePair{
		{Key: cadence.String("state"), Value: cadence.UInt8(*s.stateRaw)},
	})), nil
}

func newActivateHandler(t *testing.T, txSvc *activateTxService) *Handler {
	t.Helper()
	return NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config: &configs.Config{
			AdminAddress: "0xf8d6e0586b0a20c7",
			ChainID:      flow.Emulator,
		},
	}))
}

func TestActivateEditionSubmitsForDraftEdition(t *testing.T) {
	draft := uint8(0)
	txSvc := &activateTxService{stateRaw: &draft}
	handler := newActivateHandler(t, txSvc)

	req := httptest.NewRequest(http.MethodPost, "/v1/artdrop/editions/18/activate?sync=true", nil)
	req.Header.Set("Content-Type", "application/json")
	req = mux.SetURLVars(req, map[string]string{"edId": "18"})
	rw := httptest.NewRecorder()

	handler.ActivateEdition().ServeHTTP(rw, req)

	if rw.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"transactionType":"ArtdropActivateEdition"`) {
		t.Fatalf("expected activate edition transaction response, got %s", rw.Body.String())
	}
	if len(txSvc.calls) != 1 {
		t.Fatalf("expected exactly one tx submission, got %d", len(txSvc.calls))
	}
	// The ProtocolAdmin holder (the configured admin account) must propose —
	// artist accounts cannot activate (GovernanceAdmin-gated).
	if txSvc.calls[0].proposerAddress != "0xf8d6e0586b0a20c7" {
		t.Fatalf("expected the admin proposer, got %q", txSvc.calls[0].proposerAddress)
	}
}

func TestActivateEditionIsIdempotentNoOpWhenActive(t *testing.T) {
	active := uint8(1)
	txSvc := &activateTxService{stateRaw: &active}
	handler := newActivateHandler(t, txSvc)

	req := httptest.NewRequest(http.MethodPost, "/v1/artdrop/editions/18/activate", nil)
	req.Header.Set("Content-Type", "application/json")
	req = mux.SetURLVars(req, map[string]string{"edId": "18"})
	rw := httptest.NewRecorder()

	handler.ActivateEdition().ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200 no-op, got %d: %s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"alreadyActive":true`) {
		t.Fatalf("expected alreadyActive marker, got %s", rw.Body.String())
	}
	if len(txSvc.calls) != 0 {
		t.Fatalf("expected NO tx submission for an already-active edition, got %d", len(txSvc.calls))
	}
}

func TestActivateEditionRejectsUnknownEdition(t *testing.T) {
	txSvc := &activateTxService{stateRaw: nil} // script returns nil optional
	handler := newActivateHandler(t, txSvc)

	req := httptest.NewRequest(http.MethodPost, "/v1/artdrop/editions/999/activate", nil)
	req.Header.Set("Content-Type", "application/json")
	req = mux.SetURLVars(req, map[string]string{"edId": "999"})
	rw := httptest.NewRecorder()

	handler.ActivateEdition().ServeHTTP(rw, req)

	if rw.Code == http.StatusOK || rw.Code == http.StatusCreated {
		t.Fatalf("expected an error status for unknown edition, got %d: %s", rw.Code, rw.Body.String())
	}
	if len(txSvc.calls) != 0 {
		t.Fatalf("expected NO tx submission, got %d", len(txSvc.calls))
	}
}

func TestActivateEditionRejectsInvalidEditionID(t *testing.T) {
	txSvc := &activateTxService{}
	handler := newActivateHandler(t, txSvc)

	req := httptest.NewRequest(http.MethodPost, "/v1/artdrop/editions/not-a-number/activate", nil)
	req.Header.Set("Content-Type", "application/json")
	req = mux.SetURLVars(req, map[string]string{"edId": "not-a-number"})
	rw := httptest.NewRecorder()

	handler.ActivateEdition().ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rw.Code, rw.Body.String())
	}
}
