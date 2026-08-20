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
	svc := mustNewService(t, plugins.PluginDeps{
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
	svc := mustNewService(t, plugins.PluginDeps{
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
	svc := mustNewService(t, plugins.PluginDeps{
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

// TestServiceSetupArtistDirectRunsOnboardThenClaim also pins the inbox
// provider fix: ArtDropCore.issueArtistDirectCapability publishes the claim
// to `self.account.inbox` — the ArtDropCore contract account, not the
// wallet-api's admin account. Those two are separate accounts (they only
// coincided in a single-account deployment), so the claim's `provider`
// argument must be Config.ArtDropCoreAddress, never AdminAddress — asserted
// here against two deliberately different addresses so the test would fail
// if they were ever confused for one another again.
func TestServiceSetupArtistDirectRunsOnboardThenClaim(t *testing.T) {
	txSvc := &setupTxService{}
	cfg := ParseTestConfig(t)
	svc, err := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config: &configs.Config{
			AdminAddress: "0xf8d6e0586b0a20c7",
			ChainID:      flow.Emulator,
		},
	}, cfg)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	_, tx, err := svc.SetupArtistDirect(context.Background(), true, "0x0ae53cb6e3f42a79")
	if err != nil {
		t.Fatalf("SetupArtistDirect returned error: %v", err)
	}
	if tx == nil {
		t.Fatal("expected returned transaction")
	}
	if len(txSvc.calls) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(txSvc.calls))
	}
	if txSvc.calls[0].proposerAddress != "0xf8d6e0586b0a20c7" {
		t.Fatalf("expected admin proposer on onboard call, got %q", txSvc.calls[0].proposerAddress)
	}
	if txSvc.calls[1].proposerAddress != "0x0ae53cb6e3f42a79" {
		t.Fatalf("expected artist proposer on claim call, got %q", txSvc.calls[1].proposerAddress)
	}
	if !txSvc.calls[0].sync || !txSvc.calls[1].sync {
		t.Fatal("expected sync setup to execute both transactions synchronously")
	}
	if txSvc.calls[0].txType != TxTypeSetupArtistDirect || txSvc.calls[1].txType != TxTypeSetupArtistDirect {
		t.Fatalf("expected tx type %q on both calls", TxTypeSetupArtistDirect)
	}
	if got := txSvc.calls[0].args[0]; got != cadence.NewAddress(flow.HexToAddress("0x0ae53cb6e3f42a79")) {
		t.Fatalf("expected onboard arg artist address, got %#v", got)
	}
	// cfg.ArtDropCoreAddress ("0xd97d6774544fcd9c" by default) and AdminAddress
	// ("0xf8d6e0586b0a20c7" here) are deliberately different values, so this
	// assertion alone fails if the two are ever confused for one another
	// again.
	if got := txSvc.calls[1].args[0]; got != cadence.NewAddress(flow.HexToAddress(cfg.ArtDropCoreAddress)) {
		t.Fatalf("expected claim arg provider to be the ArtDropCore account %q, got %#v", cfg.ArtDropCoreAddress, got)
	}
	if got := txSvc.calls[1].args[1]; got != cadence.String("artist-direct-0x0ae53cb6e3f42a79") {
		t.Fatalf("expected inbox name arg, got %#v", got)
	}
}

func TestServiceSetupArtistDirectAsyncKeepsOnboardSync(t *testing.T) {
	txSvc := &setupTxService{}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config: &configs.Config{
			AdminAddress: "0xf8d6e0586b0a20c7",
			ChainID:      flow.Emulator,
		},
	})

	job, tx, err := svc.SetupArtistDirect(context.Background(), false, "0x0ae53cb6e3f42a79")
	if err != nil {
		t.Fatalf("SetupArtistDirect returned error: %v", err)
	}
	if job == nil || tx == nil {
		t.Fatal("expected async setup to return claim job and transaction")
	}
	if len(txSvc.calls) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(txSvc.calls))
	}
	if !txSvc.calls[0].sync {
		t.Fatal("expected onboard transaction to run synchronously")
	}
	if txSvc.calls[1].sync {
		t.Fatal("expected claim transaction to honor async request")
	}
}

func TestServiceSetupArtistDirectStopsWhenOnboardFails(t *testing.T) {
	txSvc := &setupTxService{err: errors.New("onboard failed")}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config: &configs.Config{
			AdminAddress: "0xf8d6e0586b0a20c7",
			ChainID:      flow.Emulator,
		},
	})

	_, _, err := svc.SetupArtistDirect(context.Background(), true, "0x0ae53cb6e3f42a79")
	if err == nil {
		t.Fatal("expected error")
	}
	if len(txSvc.calls) != 1 {
		t.Fatalf("expected only onboard transaction, got %d calls", len(txSvc.calls))
	}
}

func TestSetupFuncReturnsCreatedTransaction(t *testing.T) {
	txSvc := &setupTxService{}
	h := NewHandler(mustNewService(t, plugins.PluginDeps{
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

func TestServiceCreateOriginalUsesArtistProposerAndCadenceArgs(t *testing.T) {
	txSvc := &setupTxService{}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, _, err := svc.CreateOriginal(context.Background(), true, "0xf8d6e0586b0a20c7", CreateOriginalRequest{
		Name:        "Original 1",
		Description: "Artist drop",
		Prices:      map[string]float64{"primary": 10.5},
	})
	if err != nil {
		t.Fatalf("CreateOriginal returned error: %v", err)
	}
	if len(txSvc.calls) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(txSvc.calls))
	}
	call := txSvc.calls[0]
	if call.proposerAddress != "0xf8d6e0586b0a20c7" {
		t.Fatalf("expected artist proposer, got %q", call.proposerAddress)
	}
	if call.txType != TxTypeCreateOriginal {
		t.Fatalf("expected type %q, got %q", TxTypeCreateOriginal, call.txType)
	}
	if len(call.args) != 3 {
		t.Fatalf("expected 3 args, got %d", len(call.args))
	}
	if _, ok := call.args[2].(cadence.Dictionary); !ok {
		t.Fatalf("expected prices cadence dictionary, got %T", call.args[2])
	}
}

func TestServiceCreateEditionUsesArtistProposerAndCadenceArgs(t *testing.T) {
	txSvc := &setupTxService{}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, _, err := svc.CreateEdition(context.Background(), true, "0xf8d6e0586b0a20c7", 77, CreateEditionRequest{
		ReprintLimit:      500,
		Prices:            map[string]float64{"primary": 12},
		ProfitSplit:       map[string]float64{"artist": 0.85},
		RarityCurve:       []uint64{1, 2, 3},
		MultiplierWeights: map[string]float64{"rare": 0.25},
		RarityProfile:     1,
	})
	if err != nil {
		t.Fatalf("CreateEdition returned error: %v", err)
	}
	if len(txSvc.calls) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(txSvc.calls))
	}
	call := txSvc.calls[0]
	if call.proposerAddress != "0xf8d6e0586b0a20c7" {
		t.Fatalf("expected artist proposer, got %q", call.proposerAddress)
	}
	if call.txType != TxTypeCreateEdition {
		t.Fatalf("expected type %q, got %q", TxTypeCreateEdition, call.txType)
	}
	if len(call.args) != 7 {
		t.Fatalf("expected 7 args, got %d", len(call.args))
	}
	if got := call.args[0]; got != cadence.NewUInt64(77) {
		t.Fatalf("expected original id arg 77, got %#v", got)
	}
	if _, ok := call.args[2].(cadence.Dictionary); !ok {
		t.Fatalf("expected prices cadence dictionary, got %T", call.args[2])
	}
	if _, ok := call.args[3].(cadence.Dictionary); !ok {
		t.Fatalf("expected profit split cadence dictionary, got %T", call.args[3])
	}
	if _, ok := call.args[4].(cadence.Array); !ok {
		t.Fatalf("expected rarity curve cadence array, got %T", call.args[4])
	}
	if _, ok := call.args[5].(cadence.Dictionary); !ok {
		t.Fatalf("expected multiplier weights cadence dictionary, got %T", call.args[5])
	}
}

// TestServiceCreateEscrowUsesAdminProposerAndCadenceArgs also covers the
// removal of CreateEscrowRequest.LogicOwner, .VaultIdentifier and
// .ChipPubKey: none of the three exist on the request anymore (there's
// nothing for a caller to override, and nothing for the chip-registry
// redesign's createEscrow to accept even if there were), so this asserts
// directly that the logicOwner and vaultIdentifier args reaching the chain
// are the server's own config and defaultVaultIdentifier, and that the
// call carries exactly the 9 args the new createEscrow signature takes —
// full stop, no chip public key anywhere in the argument list.
func TestServiceCreateEscrowUsesAdminProposerAndCadenceArgs(t *testing.T) {
	txSvc := &setupTxService{}
	cfg := ParseTestConfig(t)
	svc, err := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config: &configs.Config{
			AdminAddress: "0xf8d6e0586b0a20c7",
			ChainID:      flow.Emulator,
		},
	}, cfg)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	_, _, err = svc.CreateEscrow(context.Background(), true, "0xf8d6e0586b0a20c7", CreateEscrowRequest{
		Buyer:     "0xf8d6e0586b0a20c7",
		Seller:    "0x0ae53cb6e3f42a79",
		EditionId: 42,
		ChipId:    "chip-1",
		UnlockAt:  123.45,
		Nonce:     7,
		Amount:    10.5,
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
	if len(call.args) != 9 {
		t.Fatalf("expected 9 args (no chip public key), got %d", len(call.args))
	}
	if got := call.args[0]; got != cadence.NewAddress(flow.HexToAddress(cfg.LogicOwner)) {
		t.Fatalf("expected logicOwner arg to be server config's %q, got %#v", cfg.LogicOwner, got)
	}
	if got := call.args[4]; got != cadence.String("chip-1") {
		t.Fatalf("expected chipId arg, got %#v", got)
	}
	if got := call.args[8]; got != cadence.String(defaultVaultIdentifier) {
		t.Fatalf("expected vaultIdentifier arg %q, got %#v", defaultVaultIdentifier, got)
	}
}

// TestServiceCreateEscrowSendsNoChipPublicKey pins the chip-registry
// redesign (2026-08-19) directly: EscrowModule.createEscrow no longer
// accepts a chip public key argument at all — the contract looks it up
// from ArtDropRegistry.ChipPublicKeyIndex by chipId instead, panicking
// loudly if the chip hasn't been provisioned. A raw chip public key is the
// only []byte-shaped argument this call ever sent, so asserting no arg is a
// cadence.Array is a direct, single-purpose pin — independent of the
// general arg-count/shape assertions in
// TestServiceCreateEscrowUsesAdminProposerAndCadenceArgs — that would fail
// immediately if a chip key (or any other byte array) ever got wired back
// into this call.
func TestServiceCreateEscrowSendsNoChipPublicKey(t *testing.T) {
	txSvc := &setupTxService{}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config: &configs.Config{
			AdminAddress: "0xf8d6e0586b0a20c7",
			ChainID:      flow.Emulator,
		},
	})

	_, _, err := svc.CreateEscrow(context.Background(), true, "0xf8d6e0586b0a20c7", CreateEscrowRequest{
		Buyer:     "0xf8d6e0586b0a20c7",
		Seller:    "0x0ae53cb6e3f42a79",
		EditionId: 42,
		ChipId:    "chip-1",
		UnlockAt:  123.45,
		Nonce:     7,
		Amount:    10.5,
	})
	if err != nil {
		t.Fatalf("CreateEscrow returned error: %v", err)
	}

	if len(txSvc.calls) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(txSvc.calls))
	}
	for i, arg := range txSvc.calls[0].args {
		if _, ok := arg.(cadence.Array); ok {
			t.Fatalf("expected no cadence.Array argument (a chip public key would be the only one) — found one at index %d: %#v", i, arg)
		}
	}
}

// TestServiceReEscrowUsesAdminProposerAndCadenceArgs pins the request shape
// for ReEscrow the same way TestServiceCreateEscrowUsesAdminProposerAndCadenceArgs
// pins CreateEscrow's — server-controlled logicOwner/vaultIdentifier,
// certificateId (not editionId) at the position the contract expects, so a
// future edit can't silently reintroduce a client-supplied logicOwner,
// vaultIdentifier, or editionId field.
func TestServiceReEscrowUsesAdminProposerAndCadenceArgs(t *testing.T) {
	txSvc := &setupTxService{}
	cfg := ParseTestConfig(t)
	svc, err := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config: &configs.Config{
			AdminAddress: "0xf8d6e0586b0a20c7",
			ChainID:      flow.Emulator,
		},
	}, cfg)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	_, _, err = svc.ReEscrow(context.Background(), true, "0xf8d6e0586b0a20c7", ReEscrowRequest{
		Buyer:         "0xf8d6e0586b0a20c7",
		Seller:        "0x0ae53cb6e3f42a79",
		CertificateId: 7,
		ChipId:        "chip-1",
		UnlockAt:      123.45,
		Nonce:         7,
		Amount:        10.5,
	})
	if err != nil {
		t.Fatalf("ReEscrow returned error: %v", err)
	}

	if len(txSvc.calls) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(txSvc.calls))
	}
	call := txSvc.calls[0]
	if call.proposerAddress != "0xf8d6e0586b0a20c7" {
		t.Fatalf("expected admin proposer, got %q", call.proposerAddress)
	}
	if call.txType != TxTypeReEscrow {
		t.Fatalf("expected type %q, got %q", TxTypeReEscrow, call.txType)
	}
	if !strings.Contains(call.code, "createReEscrow") {
		t.Fatal("expected re-escrow CDC")
	}
	if len(call.args) != 9 {
		t.Fatalf("expected 9 args, got %d", len(call.args))
	}
	if got := call.args[0]; got != cadence.NewAddress(flow.HexToAddress(cfg.LogicOwner)) {
		t.Fatalf("expected logicOwner arg to be server config's %q, got %#v", cfg.LogicOwner, got)
	}
	if got := call.args[3]; got != cadence.NewUInt64(7) {
		t.Fatalf("expected certificateId arg 7, got %#v", got)
	}
	if got := call.args[4]; got != cadence.String("chip-1") {
		t.Fatalf("expected chipId arg, got %#v", got)
	}
	if got := call.args[8]; got != cadence.String(defaultVaultIdentifier) {
		t.Fatalf("expected vaultIdentifier arg %q, got %#v", defaultVaultIdentifier, got)
	}
}

// TestServiceActivateChipUsesPathAddressAndServerLogicOwner also covers the
// removal of ActivateChipRequest.LogicOwner, .CertificateId and
// .CertificateOwner (escrow-lifecycle redesign, 2026-08 — the contract now
// derives certificate id/owner from the escrow's own state, closing a
// theft vector where a caller could pass arbitrary values): with none of
// those fields left on the request, the args reaching the chain can only
// ever be the path address, the server's config LogicOwner, and whatever
// the request actually still carries (escrowId, challenge, signature).
//
// Release/Cancel/Refund and their EscrowActionRequest/TxTypeRelease/
// TxTypeCancel/TxTypeRefund covered here previously were deleted along
// with the Service methods themselves — the underlying EscrowModule
// functions (releaseEscrow, cancel, refund) no longer exist on chain, so
// there's no behavior left for those cases to assert.
func TestServiceActivateChipUsesPathAddressAndServerLogicOwner(t *testing.T) {
	txSvc := &setupTxService{}
	cfg := ParseTestConfig(t)
	svc, err := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}, cfg)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	_, _, err = svc.ActivateChip(context.Background(), true, "0xf8d6e0586b0a20c7", 55, ActivateChipRequest{
		EscrowId:  99,
		Challenge: "challenge",
		Signature: []byte{9, 8, 7},
	})
	if err != nil {
		t.Fatalf("ActivateChip returned error: %v", err)
	}

	if len(txSvc.calls) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(txSvc.calls))
	}
	call := txSvc.calls[0]
	if call.proposerAddress != "0xf8d6e0586b0a20c7" {
		t.Fatalf("expected path proposer, got %q", call.proposerAddress)
	}
	if call.txType != TxTypeActivateChip {
		t.Fatalf("expected type %q, got %q", TxTypeActivateChip, call.txType)
	}
	if !strings.Contains(call.code, "activateChipAndSettle") {
		t.Fatal("expected activate-chip-and-settle CDC")
	}
	if len(call.args) != 4 {
		t.Fatalf("expected 4 args (logicOwner, escrowId, challenge, signature) — no certificateId/certificateOwner, got %d", len(call.args))
	}
	if got := call.args[0]; got != cadence.NewAddress(flow.HexToAddress(cfg.LogicOwner)) {
		t.Fatalf("expected logicOwner arg to be server config's %q, got %#v", cfg.LogicOwner, got)
	}
	if got := call.args[1]; got != cadence.UInt64(55) {
		t.Fatalf("expected path escrow id arg 55, got %#v", got)
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
