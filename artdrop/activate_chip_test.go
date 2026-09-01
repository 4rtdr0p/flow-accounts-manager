package artdrop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop/chips"
	"github.com/flow-hydraulics/flow-wallet-api/artdrop/ixkio"
	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
)

// Tests for the issue #117 phase 3 rework of ActivateChip: the wallet-api
// now produces the chip's activation signature itself (server-built
// challenge, Ixkio-gated SignChipChallenge) instead of relaying a
// client-supplied {challenge, signature}. These use svc.escrowReader/
// svc.ixkioVerifier/svc.chipStore fakes (see service.go's field doc
// comments) rather than a real chain/projection/Ixkio/DB stack.

const activateTestBuyer = "0xf8d6e0586b0a20c7"

// fakeEscrowReader returns a fixed (*EscrowSummary, error) pair regardless of
// escrowId, and records whether it was called.
type fakeEscrowReader struct {
	summary *EscrowSummary
	err     error
	calls   int
}

func (f *fakeEscrowReader) read(_ context.Context, escrowId uint64) (*EscrowSummary, error) {
	f.calls++
	return f.summary, f.err
}

// fakeActivateVerifier is a minimal ixkio.Verifier fake — see ixkio.Verifier's
// doc comment for why any Verify implementation can stand in for the real
// client.
type fakeActivateVerifier struct {
	xuid  string
	pass  bool
	err   error
	calls []ixkio.Tap
}

func (f *fakeActivateVerifier) Verify(_ context.Context, tap ixkio.Tap) (string, bool, error) {
	f.calls = append(f.calls, tap)
	return f.xuid, f.pass, f.err
}

// fakeChipStore is a minimal chips.Store fake.
type fakeChipStore struct {
	chip      *chips.Chip
	err       error
	gotChipID string
	calls     int
}

func (f *fakeChipStore) UpsertChip(_ context.Context, _ chips.Chip) error {
	panic("not used")
}

func (f *fakeChipStore) GetChip(_ context.Context, chipID string) (*chips.Chip, error) {
	f.calls++
	f.gotChipID = chipID
	return f.chip, f.err
}

// activateTxCall pins one Create call the same way setupTxCall does.
type activateTxCall struct {
	sync            bool
	proposerAddress string
	code            string
	args            []transactions.Argument
	txType          transactions.Type
}

type signChipCall struct {
	address string
	payload []byte
}

// activateChipTxService is a transactions.Service fake dedicated to
// ActivateChip's tests: it records both Create calls and SignChipChallenge
// calls (setupTxService's SignChipChallenge just panics, since none of that
// file's tests exercise chip signing).
type activateChipTxService struct {
	createCalls []activateTxCall
	createErr   error

	signCalls []signChipCall
	signSig   []byte
	signErr   error
}

func (s *activateChipTxService) Create(ctx context.Context, sync bool, proposerAddress string, code string, args []transactions.Argument, tType transactions.Type) (*jobs.Job, *transactions.Transaction, error) {
	s.createCalls = append(s.createCalls, activateTxCall{sync, proposerAddress, code, args, tType})
	if s.createErr != nil {
		return nil, nil, s.createErr
	}
	return &jobs.Job{Type: string(tType), TransactionID: "tx-activate"},
		&transactions.Transaction{TransactionId: "tx-activate", TransactionType: tType, ProposerAddress: proposerAddress},
		nil
}

func (s *activateChipTxService) SignChipChallenge(_ context.Context, address string, payload []byte) ([]byte, error) {
	s.signCalls = append(s.signCalls, signChipCall{address, payload})
	if s.signErr != nil {
		return nil, s.signErr
	}
	if s.signSig != nil {
		return s.signSig, nil
	}
	return []byte{1, 2, 3, 4}, nil
}

func (s *activateChipTxService) Sign(ctx context.Context, proposerAddress string, code string, args []transactions.Argument) (*transactions.SignedTransaction, error) {
	panic("not used")
}
func (s *activateChipTxService) List(limit, offset int) ([]transactions.Transaction, error) {
	panic("not used")
}
func (s *activateChipTxService) ListForAccount(tType transactions.Type, address string, limit, offset int) ([]transactions.Transaction, error) {
	panic("not used")
}
func (s *activateChipTxService) Details(ctx context.Context, transactionId string) (*transactions.Transaction, error) {
	panic("not used")
}
func (s *activateChipTxService) DetailsForAccount(ctx context.Context, tType transactions.Type, address, transactionId string) (*transactions.Transaction, error) {
	panic("not used")
}
func (s *activateChipTxService) ExecuteScript(ctx context.Context, code string, args []transactions.Argument) (cadence.Value, error) {
	panic("not used — activate_chip_test.go swaps out svc.escrowReader instead of exercising the chain read path")
}
func (s *activateChipTxService) UpdateTransaction(t *transactions.Transaction) error {
	panic("not used")
}
func (s *activateChipTxService) GetOrCreateTransaction(transactionId string) *transactions.Transaction {
	panic("not used")
}
func (s *activateChipTxService) RegisterResultExtractor(tType transactions.Type, fn transactions.ResultExtractorFunc) {
}

// activateChipFixtures bundles a Service wired with fakes for escrowReader/
// ixkioVerifier/chipStore/Transactions, plus handles to each fake so a test
// can assert on calls or tweak behavior before calling ActivateChip.
type activateChipFixtures struct {
	svc      *Service
	cfg      *Config
	txSvc    *activateChipTxService
	escrow   *fakeEscrowReader
	verifier *fakeActivateVerifier
	chipSvc  *fakeChipStore
}

// newActivateChipFixtures wires a Service for ActivateChip's tests with a
// happy-path escrow (buyer=activateTestBuyer, nonce=7, chipId="chip-1"), a
// passing Ixkio verifier echoing xuid="chip-1", and a provisioned chip
// pointing at custodial account "0xaaaaaaaaaaaaaaaa". Callers override
// individual fakes' fields for the scenario under test. configure, if
// non-nil, is applied to the Config BEFORE NewService runs — needed for
// fields like IxkioEnabled that NewService copies into svc.cfg by value
// (mutating the returned *Config afterwards would have no effect on the
// already-built Service).
func newActivateChipFixtures(t *testing.T, configure func(*Config)) *activateChipFixtures {
	t.Helper()

	txSvc := &activateChipTxService{}
	cfg := ParseTestConfig(t)
	if configure != nil {
		configure(cfg)
	}
	svc, err := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}, cfg)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	escrow := &fakeEscrowReader{summary: &EscrowSummary{
		Id:     55,
		Buyer:  activateTestBuyer,
		ChipId: "chip-1",
		Nonce:  7,
	}}
	verifier := &fakeActivateVerifier{xuid: "chip-1", pass: true}
	chipSvc := &fakeChipStore{chip: &chips.Chip{
		ChipID:         "chip-1",
		AccountAddress: "0xaaaaaaaaaaaaaaaa",
	}}

	svc.escrowReader = escrow.read
	svc.ixkioVerifier = verifier
	svc.chipStore = chipSvc

	return &activateChipFixtures{svc: svc, cfg: cfg, txSvc: txSvc, escrow: escrow, verifier: verifier, chipSvc: chipSvc}
}

// TestServiceActivateChipUsesPathAddressAndServerLogicOwner is the happy
// path: it pins the args reaching activate_chip_and_settle.cdc (logicOwner,
// escrowId, challenge, signature — 4 args, no certificateId/certificateOwner,
// same shape as before the rework) and that the challenge/signature are now
// wallet-produced, not client-supplied (ActivateChipRequest no longer has a
// Challenge/Signature field to supply them from).
func TestServiceActivateChipUsesPathAddressAndServerLogicOwner(t *testing.T) {
	f := newActivateChipFixtures(t, nil)
	f.txSvc.signSig = []byte{9, 8, 7, 6}

	_, _, err := f.svc.ActivateChip(context.Background(), true, activateTestBuyer, 55, ActivateChipRequest{
		IxkioTap: ixkio.Tap{X: "chip-1", N: "1a", E: "deadbeef"},
	})
	if err != nil {
		t.Fatalf("ActivateChip returned error: %v", err)
	}

	if len(f.txSvc.createCalls) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(f.txSvc.createCalls))
	}
	call := f.txSvc.createCalls[0]
	if call.proposerAddress != activateTestBuyer {
		t.Fatalf("expected path proposer, got %q", call.proposerAddress)
	}
	if call.txType != TxTypeActivateChip {
		t.Fatalf("expected type %q, got %q", TxTypeActivateChip, call.txType)
	}
	if !strings.Contains(call.code, "activateChipAndSettle") {
		t.Fatal("expected activate-chip-and-settle CDC")
	}
	if len(call.args) != 4 {
		t.Fatalf("expected 4 args (logicOwner, escrowId, challenge, signature), got %d", len(call.args))
	}
	if got := call.args[0]; got != cadence.NewAddress(flow.HexToAddress(f.cfg.LogicOwner)) {
		t.Fatalf("expected logicOwner arg to be server config's %q, got %#v", f.cfg.LogicOwner, got)
	}
	if got := call.args[1]; got != cadence.UInt64(55) {
		t.Fatalf("expected path escrow id arg 55, got %#v", got)
	}
	wantChallenge := "7:" + activateTestBuyer + ":55"
	if got := call.args[2]; got != cadence.String(wantChallenge) {
		t.Fatalf("expected challenge arg %q, got %#v", wantChallenge, got)
	}
	wantSig := newUInt8Array([]byte{9, 8, 7, 6})
	if got, ok := call.args[3].(cadence.Array); !ok || got.String() != wantSig.String() {
		t.Fatalf("expected wallet-produced signature arg %#v, got %#v", wantSig, call.args[3])
	}
}

// TestServiceActivateChipBuildsChallengeMatchingEscrowModuleBuildChallenge
// pins the exact "{nonce}:{buyer}:{escrowId}" format independently of the
// args-shape assertions above — confirmed against
// artdrop-protocol/contracts/implementations/EscrowModule.cdc's
// buildChallenge (nonce.toString():buyer.toString():escrowId.toString()).
func TestServiceActivateChipBuildsChallengeMatchingEscrowModuleBuildChallenge(t *testing.T) {
	f := newActivateChipFixtures(t, nil)
	f.escrow.summary = &EscrowSummary{Id: 123, Buyer: activateTestBuyer, ChipId: "chip-1", Nonce: 4242}

	_, _, err := f.svc.ActivateChip(context.Background(), true, activateTestBuyer, 123, ActivateChipRequest{
		IxkioTap: ixkio.Tap{X: "chip-1"},
	})
	if err != nil {
		t.Fatalf("ActivateChip returned error: %v", err)
	}

	if len(f.txSvc.signCalls) != 1 {
		t.Fatalf("expected 1 SignChipChallenge call, got %d", len(f.txSvc.signCalls))
	}
	wantChallenge := "4242:" + activateTestBuyer + ":123"
	if got := string(f.txSvc.signCalls[0].payload); got != wantChallenge {
		t.Fatalf("expected challenge payload %q, got %q", wantChallenge, got)
	}
}

// TestServiceActivateChipSignsWithResolvedChipAccount confirms
// SignChipChallenge is called with the chip's resolved custodial account
// address (from chipStore.GetChip), not the buyer's address.
func TestServiceActivateChipSignsWithResolvedChipAccount(t *testing.T) {
	f := newActivateChipFixtures(t, nil)
	f.chipSvc.chip = &chips.Chip{ChipID: "chip-1", AccountAddress: "0xcccccccccccccccc"}

	_, _, err := f.svc.ActivateChip(context.Background(), true, activateTestBuyer, 55, ActivateChipRequest{
		IxkioTap: ixkio.Tap{X: "chip-1"},
	})
	if err != nil {
		t.Fatalf("ActivateChip returned error: %v", err)
	}

	if len(f.txSvc.signCalls) != 1 {
		t.Fatalf("expected 1 SignChipChallenge call, got %d", len(f.txSvc.signCalls))
	}
	if got := f.txSvc.signCalls[0].address; got != "0xcccccccccccccccc" {
		t.Fatalf("expected SignChipChallenge to target the chip account, got %q", got)
	}
	if f.chipSvc.gotChipID != "chip-1" {
		t.Fatalf("expected chip lookup by escrow's chipId %q, got %q", "chip-1", f.chipSvc.gotChipID)
	}
}

// TestServiceActivateChipRejectsFailedIxkioTap is the security-critical
// short-circuit: an Ixkio Fail must stop ActivateChip BEFORE any signing or
// submission happens — the chip key is never wielded on an unconfirmed tap.
func TestServiceActivateChipRejectsFailedIxkioTap(t *testing.T) {
	f := newActivateChipFixtures(t, nil)
	f.verifier.pass = false

	_, _, err := f.svc.ActivateChip(context.Background(), true, activateTestBuyer, 55, ActivateChipRequest{
		IxkioTap: ixkio.Tap{X: "chip-1"},
	})
	if err == nil {
		t.Fatal("expected ActivateChip to reject a failed Ixkio tap")
	}
	if len(f.txSvc.signCalls) != 0 {
		t.Fatalf("expected NO SignChipChallenge call on a failed tap, got %d", len(f.txSvc.signCalls))
	}
	if len(f.txSvc.createCalls) != 0 {
		t.Fatalf("expected NO transaction submission on a failed tap, got %d", len(f.txSvc.createCalls))
	}
}

// TestServiceActivateChipRejectsIxkioVerifyError covers a hard Ixkio error
// (e.g. the documented error-body shape) the same way as a Fail — never
// treated as a Pass, and short-circuits before signing.
func TestServiceActivateChipRejectsIxkioVerifyError(t *testing.T) {
	f := newActivateChipFixtures(t, nil)
	f.verifier.pass = true // even if pass were somehow true, err must win
	f.verifier.err = errors.New("ixkio: batch_inactive")

	_, _, err := f.svc.ActivateChip(context.Background(), true, activateTestBuyer, 55, ActivateChipRequest{
		IxkioTap: ixkio.Tap{X: "chip-1"},
	})
	if err == nil {
		t.Fatal("expected ActivateChip to reject an Ixkio verify error")
	}
	if len(f.txSvc.signCalls) != 0 {
		t.Fatalf("expected NO SignChipChallenge call on an Ixkio error, got %d", len(f.txSvc.signCalls))
	}
}

// TestServiceActivateChipRejectsBuyerMismatch confirms the path address must
// equal the escrow's buyer — failing fast (before even calling Ixkio) rather
// than letting a mismatched activator reach the chain, where the contract
// would revert anyway.
func TestServiceActivateChipRejectsBuyerMismatch(t *testing.T) {
	f := newActivateChipFixtures(t, nil)
	f.escrow.summary.Buyer = "0x0ae53cb6e3f42a79" // different from activateTestBuyer

	_, _, err := f.svc.ActivateChip(context.Background(), true, activateTestBuyer, 55, ActivateChipRequest{
		IxkioTap: ixkio.Tap{X: "chip-1"},
	})
	if err == nil {
		t.Fatal("expected ActivateChip to reject a buyer/path-address mismatch")
	}
	if len(f.verifier.calls) != 0 {
		t.Fatalf("expected Ixkio to never be called on a buyer mismatch, got %d calls", len(f.verifier.calls))
	}
	if len(f.txSvc.createCalls) != 0 {
		t.Fatalf("expected no transaction submission on a buyer mismatch, got %d", len(f.txSvc.createCalls))
	}
}

// TestServiceActivateChipErrorsWhenEscrowNotFound covers escrowReader
// returning (nil, nil) — the GetEscrow nil-means-404 convention.
func TestServiceActivateChipErrorsWhenEscrowNotFound(t *testing.T) {
	f := newActivateChipFixtures(t, nil)
	f.escrow.summary = nil

	_, _, err := f.svc.ActivateChip(context.Background(), true, activateTestBuyer, 55, ActivateChipRequest{
		IxkioTap: ixkio.Tap{X: "chip-1"},
	})
	if err == nil {
		t.Fatal("expected ActivateChip to error when the escrow is not found")
	}
}

// TestServiceActivateChipErrorsWhenChipNotProvisioned covers chipStore
// returning (nil, nil) for the escrow's chipId — the chip must be
// provisioned (issue #117 phase 2) before its escrow can be activated.
func TestServiceActivateChipErrorsWhenChipNotProvisioned(t *testing.T) {
	f := newActivateChipFixtures(t, nil)
	f.chipSvc.chip = nil

	_, _, err := f.svc.ActivateChip(context.Background(), true, activateTestBuyer, 55, ActivateChipRequest{
		IxkioTap: ixkio.Tap{X: "chip-1"},
	})
	if err == nil {
		t.Fatal("expected ActivateChip to error when the escrow's chip isn't provisioned")
	}
	if len(f.txSvc.signCalls) != 0 {
		t.Fatalf("expected no SignChipChallenge call when the chip isn't provisioned, got %d", len(f.txSvc.signCalls))
	}
}

// TestServiceActivateChipEnforcesXuidChipBindingWhenIxkioEnabled covers the
// design doc §1/§6 identity binding: with real Ixkio (Config.IxkioEnabled),
// a tap whose verified xuid doesn't match the escrow's own chipId must be
// rejected — a Pass for chip A must never activate chip B's escrow.
func TestServiceActivateChipEnforcesXuidChipBindingWhenIxkioEnabled(t *testing.T) {
	f := newActivateChipFixtures(t, func(c *Config) { c.IxkioEnabled = true })
	f.verifier.xuid = "chip-OTHER" // tap belongs to a different chip than the escrow's

	_, _, err := f.svc.ActivateChip(context.Background(), true, activateTestBuyer, 55, ActivateChipRequest{
		IxkioTap: ixkio.Tap{X: "chip-OTHER"},
	})
	if err == nil {
		t.Fatal("expected ActivateChip to reject a xuid/chipId mismatch under real Ixkio")
	}
	if len(f.txSvc.signCalls) != 0 {
		t.Fatalf("expected NO SignChipChallenge call on a xuid/chipId mismatch, got %d", len(f.txSvc.signCalls))
	}
}

// TestServiceActivateChipSkipsXuidBindingUnderBypass covers the
// ixkio.BypassVerifier case (Config.IxkioEnabled=false, the default): there
// is no real tap to bind, so a xuid that doesn't match the escrow's chipId
// must NOT block activation — the escrow's own chipId is used directly, as
// documented on BypassVerifier and ActivateChip.
func TestServiceActivateChipSkipsXuidBindingUnderBypass(t *testing.T) {
	f := newActivateChipFixtures(t, nil) // IxkioEnabled defaults to false (bypass)
	f.verifier.xuid = "chip-OTHER"       // would fail the binding check if it were enforced

	_, _, err := f.svc.ActivateChip(context.Background(), true, activateTestBuyer, 55, ActivateChipRequest{
		IxkioTap: ixkio.Tap{X: "chip-OTHER"},
	})
	if err != nil {
		t.Fatalf("expected bypass mode to skip the xuid/chipId binding check, got error: %v", err)
	}
	if len(f.txSvc.signCalls) != 1 {
		t.Fatalf("expected activation to proceed under bypass, got %d SignChipChallenge calls", len(f.txSvc.signCalls))
	}
}

// TestServiceActivateChipPropagatesSignChipChallengeError confirms a
// SignChipChallenge failure (e.g. a KMS-backed or non-P256 chip account,
// see transactions.SignChipChallenge's doc comment) aborts before any
// transaction submission.
func TestServiceActivateChipPropagatesSignChipChallengeError(t *testing.T) {
	f := newActivateChipFixtures(t, nil)
	f.txSvc.signErr = errors.New("chip signing requires ECDSA_P256")

	_, _, err := f.svc.ActivateChip(context.Background(), true, activateTestBuyer, 55, ActivateChipRequest{
		IxkioTap: ixkio.Tap{X: "chip-1"},
	})
	if err == nil {
		t.Fatal("expected ActivateChip to propagate a SignChipChallenge error")
	}
	if len(f.txSvc.createCalls) != 0 {
		t.Fatalf("expected no transaction submission when signing fails, got %d", len(f.txSvc.createCalls))
	}
}
