package artdrop

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestServiceSetupCreatesCollectionThenRegistersProvider(t *testing.T) {
	txSvc := &setupTxService{}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, tx, err := svc.Setup(context.Background(), true, "0xf8d6e0586b0a20c7")
	if err != nil {
		t.Fatalf("Setup returned error: %v", err)
	}

	if tx == nil {
		t.Fatal("expected returned transaction")
	}

	if len(txSvc.calls) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(txSvc.calls))
	}

	if !strings.Contains(txSvc.calls[0].code, "createEmptyCollection") {
		t.Fatalf("first transaction should setup collection, got code: %s", txSvc.calls[0].code)
	}
	if !strings.Contains(txSvc.calls[1].code, "registerProviderCap") {
		t.Fatalf("second transaction should register provider, got code: %s", txSvc.calls[1].code)
	}

	for _, call := range txSvc.calls {
		if call.proposerAddress != "0xf8d6e0586b0a20c7" {
			t.Fatalf("expected normalized proposer address, got %q", call.proposerAddress)
		}
		if call.txType != TxTypeSetup {
			t.Fatalf("expected transaction type %q, got %q", TxTypeSetup, call.txType)
		}
	}

	if !txSvc.calls[0].sync || !txSvc.calls[1].sync {
		t.Fatal("expected sync setup to execute both transactions synchronously")
	}
}

func TestServiceSetupAsyncRunsCollectionBeforeSchedulingProvider(t *testing.T) {
	txSvc := &setupTxService{}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	job, tx, err := svc.Setup(context.Background(), false, "0xf8d6e0586b0a20c7")
	if err != nil {
		t.Fatalf("Setup returned error: %v", err)
	}
	if job == nil || tx == nil {
		t.Fatal("expected async setup to return provider job and transaction")
	}

	if len(txSvc.calls) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(txSvc.calls))
	}
	if !txSvc.calls[0].sync {
		t.Fatal("expected collection setup to run synchronously before provider registration")
	}
	if txSvc.calls[1].sync {
		t.Fatal("expected provider registration to honor async request")
	}
}

func TestServiceSetupStopsWhenCollectionSetupFails(t *testing.T) {
	txSvc := &setupTxService{err: errors.New("collection failed")}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, _, err := svc.Setup(context.Background(), true, "0xf8d6e0586b0a20c7")
	if err == nil {
		t.Fatal("expected error")
	}
	if len(txSvc.calls) != 1 {
		t.Fatalf("expected only collection transaction, got %d calls", len(txSvc.calls))
	}
}

func TestSetupFuncReturnsCreatedTransaction(t *testing.T) {
	txSvc := &setupTxService{}
	h := NewHandler(NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodPost, "/accounts/0xf8d6e0586b0a20c7/artdrop/setup?sync=true", nil)
	req = mux.SetURLVars(req, map[string]string{"address": "0xf8d6e0586b0a20c7"})
	rr := httptest.NewRecorder()

	h.SetupFunc(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"transactionType":"ArtdropSetup"`) {
		t.Fatalf("expected setup transaction response, got %s", rr.Body.String())
	}
}

func TestServiceCreateEscrowUsesAdminProposerAndCadenceArgs(t *testing.T) {
	txSvc := &setupTxService{}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config: &configs.Config{
			AdminAddress: "0xf8d6e0586b0a20c7",
			ChainID:      flow.Emulator,
		},
	})

	_, _, err := svc.CreateEscrow(context.Background(), true, "0xf8d6e0586b0a20c7", CreateEscrowRequest{
		LogicOwner:      "0xf8d6e0586b0a20c7",
		Buyer:           "0xf8d6e0586b0a20c7",
		Seller:          "0x0ae53cb6e3f42a79",
		EditionId:       42,
		ChipId:          "chip-1",
		ChipPubKey:      []byte{1, 2, 3},
		UnlockAt:        123.45,
		Nonce:           7,
		Amount:          10.5,
		VaultIdentifier: "flowTokenVault",
	})
	if err != nil {
		t.Fatalf("CreateEscrow returned error: %v", err)
	}

	if len(txSvc.calls) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(txSvc.calls))
	}
	call := txSvc.calls[0]
	if call.proposerAddress != "0xf8d6e0586b0a20c7" {
		t.Fatalf("expected admin proposer, got %q", call.proposerAddress)
	}
	if call.txType != TxTypeCreateEscrow {
		t.Fatalf("expected type %q, got %q", TxTypeCreateEscrow, call.txType)
	}
	if !strings.Contains(call.code, "createEscrow") {
		t.Fatal("expected create escrow CDC")
	}
	if len(call.args) != 10 {
		t.Fatalf("expected 10 args, got %d", len(call.args))
	}
	if _, ok := call.args[5].(cadence.Array); !ok {
		t.Fatalf("expected chip public key cadence array, got %T", call.args[5])
	}
}

func TestServiceEscrowActionsUsePathAddressAndPathEscrowID(t *testing.T) {
	tests := []struct {
		name   string
		call   func(*Service) (*jobs.Job, *transactions.Transaction, error)
		txType transactions.Type
		code   string
	}{
		{
			name: "activate",
			call: func(svc *Service) (*jobs.Job, *transactions.Transaction, error) {
				return svc.ActivateChip(context.Background(), true, "0xf8d6e0586b0a20c7", 55, ActivateChipRequest{
					LogicOwner:       "0xf8d6e0586b0a20c7",
					EscrowId:         99,
					Challenge:        "challenge",
					Signature:        []byte{9, 8, 7},
					CertificateId:    123,
					CertificateOwner: "0x0ae53cb6e3f42a79",
				})
			},
			txType: TxTypeActivateChip,
			code:   "activateChipAndSettle",
		},
		{
			name: "release",
			call: func(svc *Service) (*jobs.Job, *transactions.Transaction, error) {
				return svc.Release(context.Background(), true, "0xf8d6e0586b0a20c7", 55, EscrowActionRequest{LogicOwner: "0xf8d6e0586b0a20c7"})
			},
			txType: TxTypeRelease,
			code:   "releaseEscrow",
		},
		{
			name: "cancel",
			call: func(svc *Service) (*jobs.Job, *transactions.Transaction, error) {
				return svc.Cancel(context.Background(), true, "0xf8d6e0586b0a20c7", 55, EscrowActionRequest{LogicOwner: "0xf8d6e0586b0a20c7"})
			},
			txType: TxTypeCancel,
			code:   "cancel",
		},
		{
			name: "refund",
			call: func(svc *Service) (*jobs.Job, *transactions.Transaction, error) {
				return svc.Refund(context.Background(), true, "0xf8d6e0586b0a20c7", 55, EscrowActionRequest{LogicOwner: "0xf8d6e0586b0a20c7"})
			},
			txType: TxTypeRefund,
			code:   "refund",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txSvc := &setupTxService{}
			svc := NewService(plugins.PluginDeps{
				Transactions: txSvc,
				Config:       &configs.Config{ChainID: flow.Emulator},
			})

			_, _, err := tt.call(svc)
			if err != nil {
				t.Fatalf("action returned error: %v", err)
			}

			if len(txSvc.calls) != 1 {
				t.Fatalf("expected 1 transaction, got %d", len(txSvc.calls))
			}
			call := txSvc.calls[0]
			if call.proposerAddress != "0xf8d6e0586b0a20c7" {
				t.Fatalf("expected path proposer, got %q", call.proposerAddress)
			}
			if call.txType != tt.txType {
				t.Fatalf("expected type %q, got %q", tt.txType, call.txType)
			}
			if !strings.Contains(call.code, tt.code) {
				t.Fatalf("expected CDC containing %q", tt.code)
			}
			if got := call.args[1]; got != cadence.UInt64(55) {
				t.Fatalf("expected path escrow id arg 55, got %#v", got)
			}
		})
	}
}

type setupTxCall struct {
	sync            bool
	proposerAddress string
	code            string
	args            []transactions.Argument
	txType          transactions.Type
}

type setupTxService struct {
	calls []setupTxCall
	err   error
}

func (s *setupTxService) Create(ctx context.Context, sync bool, proposerAddress string, code string, args []transactions.Argument, tType transactions.Type) (*jobs.Job, *transactions.Transaction, error) {
	s.calls = append(s.calls, setupTxCall{
		sync:            sync,
		proposerAddress: proposerAddress,
		code:            code,
		args:            args,
		txType:          tType,
	})
	if s.err != nil {
		return nil, nil, s.err
	}

	id := "tx-setup-collection"
	if len(s.calls) == 2 {
		id = "tx-register-provider"
	}

	return &jobs.Job{
			Type:          string(tType),
			TransactionID: id,
		}, &transactions.Transaction{
			TransactionId:   id,
			TransactionType: tType,
			ProposerAddress: proposerAddress,
		}, nil
}

func (s *setupTxService) Sign(ctx context.Context, proposerAddress string, code string, args []transactions.Argument) (*transactions.SignedTransaction, error) {
	panic("not used")
}

func (s *setupTxService) List(limit, offset int) ([]transactions.Transaction, error) {
	panic("not used")
}

func (s *setupTxService) ListForAccount(tType transactions.Type, address string, limit, offset int) ([]transactions.Transaction, error) {
	panic("not used")
}

func (s *setupTxService) Details(ctx context.Context, transactionId string) (*transactions.Transaction, error) {
	panic("not used")
}

func (s *setupTxService) DetailsForAccount(ctx context.Context, tType transactions.Type, address, transactionId string) (*transactions.Transaction, error) {
	panic("not used")
}

func (s *setupTxService) ExecuteScript(ctx context.Context, code string, args []transactions.Argument) (cadence.Value, error) {
	panic("not used")
}

func (s *setupTxService) UpdateTransaction(t *transactions.Transaction) error {
	panic("not used")
}

func (s *setupTxService) GetOrCreateTransaction(transactionId string) *transactions.Transaction {
	panic("not used")
}
