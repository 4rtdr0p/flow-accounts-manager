package artdrop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	"github.com/gorilla/mux"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
)

func TestTransferAcceptsCertificateIDZero(t *testing.T) {
	scriptFile, err := os.CreateTemp(t.TempDir(), "protocol_transfer_*.cdc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scriptFile.WriteString("transaction {}"); err != nil {
		t.Fatal(err)
	}
	if err := scriptFile.Close(); err != nil {
		t.Fatal(err)
	}

	txSvc := &captureTransactionService{}
	handler := NewHandler(NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config: &configs.Config{
			ChainID:                    flow.Emulator,
			ScriptPathProtocolTransfer: scriptFile.Name(),
		},
	}))

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/accounts/0xf8d6e0586b0a20c7/transfer",
		strings.NewReader(`{"certificateId":0,"to":"0xf8d6e0586b0a20c7"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req = mux.SetURLVars(req, map[string]string{"address": "0xf8d6e0586b0a20c7"})
	rw := httptest.NewRecorder()

	handler.Transfer().ServeHTTP(rw, req)

	if rw.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", rw.Code, rw.Body.String())
	}
	if len(txSvc.args) != 3 {
		t.Fatalf("expected 3 Cadence args, got %d", len(txSvc.args))
	}
	certificateID, ok := txSvc.args[0].(cadence.UInt64)
	if !ok {
		t.Fatalf("expected first arg to be cadence.UInt64, got %T", txSvc.args[0])
	}
	if certificateID != cadence.UInt64(0) {
		t.Fatalf("expected certificateId 0, got %s", certificateID)
	}
}

func TestTransferRequiresCertificateID(t *testing.T) {
	handler := NewHandler(nil)
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/accounts/0xf8d6e0586b0a20c7/transfer",
		strings.NewReader(`{"to":"0xf8d6e0586b0a20c7"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()

	handler.Transfer().ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rw.Code, rw.Body.String())
	}
}

type captureTransactionService struct {
	args []transactions.Argument
}

func (s *captureTransactionService) Create(ctx context.Context, sync bool, proposerAddress string, code string, args []transactions.Argument, tType transactions.Type) (*jobs.Job, *transactions.Transaction, error) {
	s.args = args
	return &jobs.Job{Type: string(tType), State: jobs.Init}, &transactions.Transaction{TransactionType: tType}, nil
}

func (s *captureTransactionService) Sign(ctx context.Context, proposerAddress string, code string, args []transactions.Argument) (*transactions.SignedTransaction, error) {
	return nil, nil
}

func (s *captureTransactionService) List(limit, offset int) ([]transactions.Transaction, error) {
	return nil, nil
}

func (s *captureTransactionService) ListForAccount(tType transactions.Type, address string, limit, offset int) ([]transactions.Transaction, error) {
	return nil, nil
}

func (s *captureTransactionService) Details(ctx context.Context, transactionId string) (*transactions.Transaction, error) {
	return nil, nil
}

func (s *captureTransactionService) DetailsForAccount(ctx context.Context, tType transactions.Type, address, transactionId string) (*transactions.Transaction, error) {
	return nil, nil
}

func (s *captureTransactionService) ExecuteScript(ctx context.Context, code string, args []transactions.Argument) (cadence.Value, error) {
	return nil, nil
}

func (s *captureTransactionService) UpdateTransaction(t *transactions.Transaction) error {
	return nil
}

func (s *captureTransactionService) GetOrCreateTransaction(transactionId string) *transactions.Transaction {
	return nil
}
