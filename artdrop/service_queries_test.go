package artdrop

import (
	"context"
	"errors"
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
)

func TestListCertificatesReturnsIds(t *testing.T) {
	mustStr := func(s string) cadence.String {
		v, err := cadence.NewString(s)
		if err != nil {
			panic(err)
		}
		return v
	}
	txSvc := &queryTxService{
		scriptResult: cadence.NewArray([]cadence.Value{
			cadence.NewDictionary([]cadence.KeyValuePair{
				{Key: mustStr("id"), Value: cadence.NewUInt64(1)},
				{Key: mustStr("editionId"), Value: cadence.NewUInt64(7)},
				{Key: mustStr("serial"), Value: cadence.NewUInt64(1)},
				{Key: mustStr("isRevealed"), Value: cadence.NewBool(true)},
			}),
			cadence.NewDictionary([]cadence.KeyValuePair{
				{Key: mustStr("id"), Value: cadence.NewUInt64(42)},
				{Key: mustStr("editionId"), Value: cadence.NewUInt64(7)},
				{Key: mustStr("serial"), Value: cadence.NewUInt64(2)},
				{Key: mustStr("isRevealed"), Value: cadence.NewBool(false)},
			}),
			cadence.NewDictionary([]cadence.KeyValuePair{
				{Key: mustStr("id"), Value: cadence.NewUInt64(99)},
				{Key: mustStr("editionId"), Value: cadence.NewUInt64(7)},
				{Key: mustStr("serial"), Value: cadence.NewUInt64(3)},
				{Key: mustStr("isRevealed"), Value: cadence.NewBool(true)},
			}),
		}),
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	certs, err := svc.ListCertificates(context.Background(), "0xf8d6e0586b0a20c7")
	if err != nil {
		t.Fatalf("ListCertificates returned error: %v", err)
	}
	if len(certs) != 3 {
		t.Fatalf("expected 3 certificates, got %d", len(certs))
	}
	if certs[0].Id != 1 || certs[1].Id != 42 || certs[2].Id != 99 {
		t.Fatalf("unexpected certificate ids: %+v", certs)
	}
	if certs[0].EditionId != 7 || certs[1].EditionId != 7 || certs[2].EditionId != 7 {
		t.Fatalf("expected editionId 7 on all, got %+v", certs)
	}
	if certs[0].Serial != 1 || certs[1].Serial != 2 || certs[2].Serial != 3 {
		t.Fatalf("expected serials 1/2/3, got %+v", certs)
	}
	if !certs[0].IsRevealed || certs[1].IsRevealed || !certs[2].IsRevealed {
		t.Fatalf("expected revealed=true/false/true, got %+v", certs)
	}
}

func TestListCertificatesReturnsEmpty(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewArray([]cadence.Value{}),
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	certs, err := svc.ListCertificates(context.Background(), "0xf8d6e0586b0a20c7")
	if err != nil {
		t.Fatalf("ListCertificates returned error: %v", err)
	}
	if len(certs) != 0 {
		t.Fatalf("expected 0 certificates, got %d", len(certs))
	}
}

func TestListCertificatesPropagatesScriptError(t *testing.T) {
	txSvc := &queryTxService{err: errors.New("script execution failed")}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.ListCertificates(context.Background(), "0xf8d6e0586b0a20c7")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListCertificatesRejectsUnexpectedType(t *testing.T) {
	strVal, _ := cadence.NewString("not-an-array")
	txSvc := &queryTxService{
		scriptResult: strVal,
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.ListCertificates(context.Background(), "0xf8d6e0586b0a20c7")
	if err == nil {
		t.Fatal("expected error for unexpected script result type, got nil")
	}
}

func TestGetEscrowReturnsStatus(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewUInt8(3),
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	summary, err := svc.GetEscrow(context.Background(), "0xf8d6e0586b0a20c7", 7)
	if err != nil {
		t.Fatalf("GetEscrow returned error: %v", err)
	}
	if summary.Id != 7 {
		t.Fatalf("expected escrow id 7, got %d", summary.Id)
	}
	if summary.Status != 3 {
		t.Fatalf("expected status 3, got %d", summary.Status)
	}
	if len(txSvc.args) != 1 {
		t.Fatalf("expected 1 script arg, got %d", len(txSvc.args))
	}
	if txSvc.args[0] != cadence.NewUInt64(7) {
		t.Fatalf("expected escrow id as arg, got %#v", txSvc.args[0])
	}
}

func TestGetEscrowPropagatesScriptError(t *testing.T) {
	txSvc := &queryTxService{err: errors.New("script execution failed")}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.GetEscrow(context.Background(), "0xf8d6e0586b0a20c7", 7)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetEscrowRejectsUnexpectedType(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewUInt64(42),
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.GetEscrow(context.Background(), "0xf8d6e0586b0a20c7", 7)
	if err == nil {
		t.Fatal("expected error for unexpected script result type, got nil")
	}
}

func TestIsArtistReturnsTrue(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewBool(true),
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	is, err := svc.IsArtist(context.Background(), "0xf8d6e0586b0a20c7")
	if err != nil {
		t.Fatalf("IsArtist returned error: %v", err)
	}
	if !is {
		t.Fatalf("expected isArtist true, got false")
	}
	if len(txSvc.args) != 1 {
		t.Fatalf("expected 1 script arg, got %d", len(txSvc.args))
	}
	addr, ok := txSvc.args[0].(cadence.Address)
	if !ok {
		t.Fatalf("expected script arg to be cadence.Address, got %T", txSvc.args[0])
	}
	if addr.Hex() != flow.HexToAddress("0xf8d6e0586b0a20c7").Hex() {
		t.Fatalf("expected script arg address 0xf8d6e0586b0a20c7, got %s", addr.Hex())
	}
}

func TestIsArtistReturnsFalse(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewBool(false),
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	is, err := svc.IsArtist(context.Background(), "0xf8d6e0586b0a20c7")
	if err != nil {
		t.Fatalf("IsArtist returned error: %v", err)
	}
	if is {
		t.Fatalf("expected isArtist false, got true")
	}
}

func TestIsArtistPropagatesScriptError(t *testing.T) {
	txSvc := &queryTxService{err: errors.New("script execution failed")}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.IsArtist(context.Background(), "0xf8d6e0586b0a20c7")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestIsArtistRejectsUnexpectedType(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewUInt64(1),
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.IsArtist(context.Background(), "0xf8d6e0586b0a20c7")
	if err == nil {
		t.Fatal("expected error for unexpected script result type, got nil")
	}
}

func TestIsArtistRejectsInvalidAddress(t *testing.T) {
	txSvc := &queryTxService{}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.IsArtist(context.Background(), "not-an-address")
	if err == nil {
		t.Fatal("expected error for invalid address, got nil")
	}
}

// ---- mock transaction service for read-only queries ----

type queryTxService struct {
	scriptResult cadence.Value
	args         []transactions.Argument
	err          error
}

func (s *queryTxService) Create(ctx context.Context, sync bool, proposerAddress string, code string, args []transactions.Argument, tType transactions.Type) (*jobs.Job, *transactions.Transaction, error) {
	panic("not used by queries")
}

func (s *queryTxService) Sign(ctx context.Context, proposerAddress string, code string, args []transactions.Argument) (*transactions.SignedTransaction, error) {
	panic("not used by queries")
}

func (s *queryTxService) List(limit, offset int) ([]transactions.Transaction, error) {
	panic("not used by queries")
}

func (s *queryTxService) ListForAccount(tType transactions.Type, address string, limit, offset int) ([]transactions.Transaction, error) {
	panic("not used by queries")
}

func (s *queryTxService) Details(ctx context.Context, transactionId string) (*transactions.Transaction, error) {
	panic("not used by queries")
}

func (s *queryTxService) DetailsForAccount(ctx context.Context, tType transactions.Type, address, transactionId string) (*transactions.Transaction, error) {
	panic("not used by queries")
}

func (s *queryTxService) ExecuteScript(ctx context.Context, code string, args []transactions.Argument) (cadence.Value, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.args = args
	return s.scriptResult, nil
}

func (s *queryTxService) UpdateTransaction(t *transactions.Transaction) error {
	panic("not used by queries")
}

func (s *queryTxService) GetOrCreateTransaction(transactionId string) *transactions.Transaction {
	panic("not used by queries")
}
