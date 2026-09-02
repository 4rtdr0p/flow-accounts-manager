//go:build artdrop_e2e

// Package artdrop's Tier-2 E2E suite runs the real chip/escrow flows against a
// live emulator with the ArtDrop protocol deployed and wired. It is gated
// behind the `artdrop_e2e` build tag so the default `go test ./...` stays fast
// and needs no cross-repo deploy. Stand the environment up with
// flow/e2e-setup-artdrop.sh (or `make test-artdrop-e2e`), which starts the
// emulator, deploys artdrop-protocol, grants the delegated caps, and writes the
// FLOW_WALLET_* env this suite reads.
package artdrop

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop/chips"
	"github.com/flow-hydraulics/flow-wallet-api/artdrop/ixkio"
	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/flow-hydraulics/flow-wallet-api/tests/test"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
)

// escrow status rawValues (see EscrowSummary.Status doc): 0=Pending, 2=Voided.
// escrowStatusReleased (1) is already defined in escrow_cache.go.
const (
	escrowStatusPending uint8 = 0
	escrowStatusVoided  uint8 = 2
)

// idField pulls a single uint64 id (e.g. "escrowId") out of an async job's
// JSON Result blob.
func idField(t *testing.T, resultJSON, key string) uint64 {
	t.Helper()
	m := map[string]uint64{}
	if err := json.Unmarshal([]byte(resultJSON), &m); err != nil {
		t.Fatalf("decode job result %q: %v", resultJSON, err)
	}
	v, ok := m[key]
	if !ok {
		t.Fatalf("job result %q has no %q", resultJSON, key)
	}
	return v
}

// futureUnlockAt is a safely-future unlock timestamp (seconds since epoch) for
// an escrow's claim window.
func futureUnlockAt() float64 { return float64(time.Now().Unix() + 100000) }

// uniqueSuffix returns a per-run-unique token so chip ids / names don't collide
// across reruns against the same persistent-ish emulator session.
func uniqueSuffix() int64 { return time.Now().UnixNano() }

// e2eEnv bundles a live artdrop Service wired to the emulator plus the base
// services and config the tests need.
type e2eEnv struct {
	t      *testing.T
	svc    *Service
	app    test.Services
	cfg    *configs.Config
	artcfg *Config
}

// newE2E boots the base wallet services against the emulator and constructs a
// real artdrop.Service on top, exactly as main() does — same accounts /
// transactions / tokens services, same DB, same env-driven config.
func newE2E(t *testing.T) *e2eEnv {
	t.Helper()

	cfg := configs.ParseTestConfig(t)
	app := test.GetServices(t, cfg)

	// The chips table isn't in the base harness's migration set (that's a
	// plugin migration only main() wires); create it here.
	if err := app.GetDB().AutoMigrate(&chips.Chip{}); err != nil {
		t.Fatalf("automigrate chips table: %v", err)
	}

	artcfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("load artdrop config from env: %v", err)
	}

	deps := plugins.PluginDeps{
		Accounts:     app.GetAccounts(),
		Tokens:       app.GetTokens(),
		Transactions: app.GetTransactions(),
		Config:       cfg,
		DB:           app.GetDB(),
	}
	svc, err := NewService(deps, artcfg)
	if err != nil {
		t.Fatalf("build artdrop service: %v", err)
	}

	return &e2eEnv{t: t, svc: svc, app: app, cfg: cfg, artcfg: artcfg}
}

// chipPublicKeyOnChain reads chipId's registered public key straight from
// ArtDropRegistry's published ChipPublicKeyIndex reader — the same index
// createEscrow looks the chip up in — and returns it as lowercase hex, or ""
// if the chip isn't registered.
func (e *e2eEnv) chipPublicKeyOnChain(t *testing.T, chipId string) string {
	t.Helper()
	code := fmt.Sprintf(`import ArtDropRegistry from %s

access(all) fun main(registryOwner: Address, chipId: String): [UInt8]? {
    let ref = getAccount(registryOwner)
        .capabilities
        .borrow<&{ArtDropRegistry.IChipPublicKeyIndexReader}>(ArtDropRegistry.ChipPublicKeyPublicPath())
        ?? panic("chip public key index reader not published")
    return ref.getPublicKey(chipId: chipId)
}`, e.artcfg.ArtDropRegistryAddress)

	val, err := e.app.GetTransactions().ExecuteScript(context.Background(), code, []transactions.Argument{
		cadence.NewAddress(flow.HexToAddress(e.artcfg.ArtDropRegistryAddress)),
		cadence.String(chipId),
	})
	if err != nil {
		t.Fatalf("read chip pubkey on-chain: %v", err)
	}

	opt, ok := val.(cadence.Optional)
	if !ok {
		t.Fatalf("expected Optional from chip pubkey script, got %T", val)
	}
	if opt.Value == nil {
		return ""
	}
	arr, ok := opt.Value.(cadence.Array)
	if !ok {
		t.Fatalf("expected [UInt8] inside Optional, got %T", opt.Value)
	}
	b := make([]byte, len(arr.Values))
	for i, v := range arr.Values {
		b[i] = byte(v.(cadence.UInt8))
	}
	return hex.EncodeToString(b)
}

// TestE2EChipProvisioning (T4, issue #497) provisions a chip end-to-end: the
// wallet creates a P-256 custodial account for it, registers that account's
// public key on-chain via the delegated ChipAdmin capability, and persists the
// chipId -> account mapping. Asserts the on-chain key matches, the DB mapping
// is readable, and a second provisioning is idempotent (same account, no new
// on-chain registration).
func TestE2EChipProvisioning(t *testing.T) {
	e := newE2E(t)
	ctx := context.Background()

	chipId := fmt.Sprintf("e2e-chip-%d", uniqueSuffix())

	info, err := e.svc.ProvisionChip(ctx, chipId)
	if err != nil {
		t.Fatalf("ProvisionChip: %v", err)
	}
	if info.ChipId != chipId {
		t.Fatalf("chip id mismatch: got %q want %q", info.ChipId, chipId)
	}
	if info.SigningMode != chipSigningModeCustodial {
		t.Fatalf("signing mode: got %q want %q", info.SigningMode, chipSigningModeCustodial)
	}
	if info.AccountAddress == "" {
		t.Fatal("provisioned chip has no account address")
	}
	if len(info.PublicKey) != chipPublicKeyLength*2 {
		t.Fatalf("chip public key hex length %d, want %d", len(info.PublicKey), chipPublicKeyLength*2)
	}

	// On-chain readback: the registry must hold exactly this key for this chip.
	onChain := e.chipPublicKeyOnChain(t, chipId)
	if onChain == "" {
		t.Fatal("chip public key was not registered on-chain")
	}
	if !strings.EqualFold(onChain, info.PublicKey) {
		t.Fatalf("on-chain chip key %s != provisioned key %s", onChain, info.PublicKey)
	}

	// DB mapping is readable and matches.
	got, err := e.svc.GetChip(ctx, chipId)
	if err != nil {
		t.Fatalf("GetChip: %v", err)
	}
	if got == nil {
		t.Fatal("GetChip returned nil for a provisioned chip")
	}
	if got.AccountAddress != info.AccountAddress || !strings.EqualFold(got.PublicKey, info.PublicKey) {
		t.Fatalf("GetChip mapping mismatch: got %+v want %+v", got, info)
	}

	// Idempotency: re-provisioning returns the SAME mapping without creating a
	// new account or re-registering on-chain.
	info2, err := e.svc.ProvisionChip(ctx, chipId)
	if err != nil {
		t.Fatalf("ProvisionChip (idempotent retry): %v", err)
	}
	if info2.AccountAddress != info.AccountAddress {
		t.Fatalf("idempotent re-provision created a new account: %s -> %s", info.AccountAddress, info2.AccountAddress)
	}
	if !strings.EqualFold(info2.PublicKey, info.PublicKey) {
		t.Fatalf("idempotent re-provision changed the public key: %s -> %s", info.PublicKey, info2.PublicKey)
	}

	t.Logf("chip %s provisioned to account %s, key registered on-chain", chipId, info.AccountAddress)
}

// await takes an async service call's (job, tx, err) return directly and runs
// the job to completion, returning the finished job (whose Result carries the
// created id JSON). It is a METHOD so the multi-value service call can be its
// sole argument — e.await(e.svc.CreateEscrow(...)) — which Go allows for a call
// whose arguments are exactly one multi-value call.
func (e *e2eEnv) await(job *jobs.Job, _ *transactions.Transaction, err error) *jobs.Job {
	e.t.Helper()
	if err != nil {
		e.t.Fatalf("service call: %v", err)
	}
	done, werr := test.WaitForJob(e.app.GetJobs(), job.ID.String())
	if werr != nil {
		e.t.Fatalf("job %s failed: %v", job.ID, werr)
	}
	return done
}

// createCustodial makes a fresh custodial account and returns its address.
func (e *e2eEnv) createCustodial(t *testing.T) string {
	t.Helper()
	_, acc, err := e.app.GetAccounts().Create(context.Background(), true)
	if err != nil {
		t.Fatalf("create custodial account: %v", err)
	}
	return acc.Address
}

// activateEdition drives the GovernanceAdmin-gated activation directly (the
// wallet has no ActivateEdition method — activation is a protocol-admin action,
// not a wallet-API responsibility). On the single-account emulator the admin
// holds ProtocolAdmin at AdminStoragePath, so it can borrow GovernanceAdmin.
func (e *e2eEnv) activateEdition(t *testing.T, editionId uint64) {
	t.Helper()
	code := fmt.Sprintf(`import ArtDropCore from %s

transaction(editionId: UInt64) {
    prepare(signer: auth(Storage) &Account) {
        let admin = signer.storage.borrow<auth(ArtDropCore.GovernanceAdmin) &ArtDropCore.ProtocolAdmin>(
            from: ArtDropCore.AdminStoragePath
        ) ?? panic("activate_edition: signer does not hold ProtocolAdmin at AdminStoragePath")
        admin.activateEdition(id: editionId)
    }
}`, e.artcfg.ArtDropCoreAddress)

	if _, _, err := e.app.GetTransactions().Create(
		context.Background(), true, e.cfg.AdminAddress, code,
		[]transactions.Argument{cadence.NewUInt64(editionId)}, transactions.General,
	); err != nil {
		t.Fatalf("activate edition %d: %v", editionId, err)
	}
}

// escrowParties is the fully-provisioned cast a Pending escrow needs.
type escrowParties struct {
	artist    string // seller in a primary sale; holds the minted cert until activation
	buyer     string
	editionId uint64
	chipId    string
}

// setupEscrowParties runs the whole pre-escrow onboarding once: onboard an
// artist, create an Original + Edition and activate it, set up the artist
// (seller) and a buyer with ArtDrop collections, and provision a chip. This is
// the re-scoped "item 3" setup (artist onboarding via SetupArtistDirect) plus
// everything a real escrow needs.
func (e *e2eEnv) setupEscrowParties(t *testing.T) escrowParties {
	t.Helper()
	ctx := context.Background()

	artist := e.createCustodial(t)
	if _, _, err := e.svc.SetupArtistDirect(ctx, true, artist); err != nil {
		t.Fatalf("SetupArtistDirect: %v", err)
	}

	// Artist (seller) and buyer both need an ArtDrop certificate collection +
	// provider: the cert mints into the seller's collection at createEscrow and
	// moves to the buyer at activation.
	if _, _, err := e.svc.Setup(ctx, true, artist); err != nil {
		t.Fatalf("setup artist collection: %v", err)
	}
	buyer := e.createCustodial(t)
	if _, _, err := e.svc.Setup(ctx, true, buyer); err != nil {
		t.Fatalf("setup buyer collection: %v", err)
	}

	origJob := e.await(e.svc.CreateOriginal(ctx, false, artist, CreateOriginalRequest{
		Name:        "E2E Original",
		Description: "artdrop_e2e lifecycle",
		Prices:      map[string]float64{"FLOW": 100.0},
	}))
	originalId := idField(t, origJob.Result, "originalId")

	// IsArtist becomes true only once the artist has created an Original (it's
	// tracked by the registry's ArtistIndex, written in artistCreateOriginal) —
	// so assert onboarding worked end-to-end here, after the first Original.
	if ok, err := e.svc.IsArtist(ctx, artist); err != nil || !ok {
		t.Fatalf("expected %s to be a registered artist after creating an Original (ok=%v err=%v)", artist, ok, err)
	}

	edJob := e.await(e.svc.CreateEdition(ctx, false, artist, originalId, CreateEditionRequest{
		ReprintLimit:      5,
		Prices:            map[string]float64{"FLOW": 100.0},
		ProfitSplit:       map[string]float64{"artist": 0.7, "platform": 0.15, "yield": 0.15},
		RarityCurve:       []uint64{5},
		MultiplierWeights: map[string]float64{"common": 1.0},
		RarityProfile:     1,
	}))
	editionId := idField(t, edJob.Result, "editionId")

	e.activateEdition(t, editionId)

	chipId := fmt.Sprintf("e2e-escrow-chip-%d", uniqueSuffix())
	if _, err := e.svc.ProvisionChip(ctx, chipId); err != nil {
		t.Fatalf("ProvisionChip for escrow: %v", err)
	}

	t.Logf("setup: artist/seller=%s buyer=%s edition=%d chip=%s", artist, buyer, editionId, chipId)
	return escrowParties{artist: artist, buyer: buyer, editionId: editionId, chipId: chipId}
}

// TestE2EEscrowVoidIsolated (T6) is the minimal void case: create a Pending
// escrow, void it, and confirm the on-chain status flips to Voided (2).
func TestE2EEscrowVoidIsolated(t *testing.T) {
	e := newE2E(t)
	ctx := context.Background()
	p := e.setupEscrowParties(t)

	createJob := e.await(e.svc.CreateEscrow(ctx, false, p.buyer, CreateEscrowRequest{
		Buyer:     p.buyer,
		Seller:    p.artist,
		EditionId: p.editionId,
		ChipId:    p.chipId,
		UnlockAt:  futureUnlockAt(),
		Nonce:     1,
		Amount:    1.0,
	}))
	escrowId := idField(t, createJob.Result, "escrowId")

	if s, err := e.svc.fetchEscrow(ctx, escrowId); err != nil || s == nil || s.Status != escrowStatusPending {
		t.Fatalf("expected fresh escrow %d to be Pending(0): summary=%+v err=%v", escrowId, s, err)
	}

	e.await(e.svc.VoidEscrow(ctx, false, escrowId))

	s, err := e.svc.fetchEscrow(ctx, escrowId)
	if err != nil {
		t.Fatalf("GetEscrow after void: %v", err)
	}
	if s.Status != escrowStatusVoided {
		t.Fatalf("expected escrow %d status Voided(%d), got %d", escrowId, escrowStatusVoided, s.Status)
	}
	t.Logf("T6: escrow %d voided (status=%d)", escrowId, s.Status)
}

// TestE2EEscrowLifecycle (T5) is the full lifecycle: create a Pending escrow,
// void it, re-escrow the now-voided certificate to a fresh escrow, then
// activate-and-settle that escrow (Ixkio in bypass) and confirm it Releases and
// the certificate lands in the buyer's collection.
func TestE2EEscrowLifecycle(t *testing.T) {
	e := newE2E(t)
	ctx := context.Background()
	p := e.setupEscrowParties(t)

	// 1. create
	createJob := e.await(e.svc.CreateEscrow(ctx, false, p.buyer, CreateEscrowRequest{
		Buyer:     p.buyer,
		Seller:    p.artist,
		EditionId: p.editionId,
		ChipId:    p.chipId,
		UnlockAt:  futureUnlockAt(),
		Nonce:     1,
		Amount:    1.0,
	}))
	escrowId := idField(t, createJob.Result, "escrowId")
	certificateId := idField(t, createJob.Result, "certificateId")
	t.Logf("T5: created escrow %d (certificate %d)", escrowId, certificateId)

	// 2. void (required before re-escrow — the contract demands Voided, never a
	//    mere timeout)
	e.await(e.svc.VoidEscrow(ctx, false, escrowId))
	if s, err := e.svc.fetchEscrow(ctx, escrowId); err != nil || s.Status != escrowStatusVoided {
		t.Fatalf("expected escrow %d Voided before re-escrow: %+v err=%v", escrowId, s, err)
	}

	// 3. re-escrow the same certificate to a fresh escrow (no re-mint).
	// NOTE: unlike CreateEscrow, the ReEscrow path (TxTypeReEscrow) has no
	// result extractor registered in NewService, so its async job.Result is
	// empty — the new escrow id can't be read from the job the way #495's
	// read-after-write expects. We recover it here from the monotonic total
	// escrow counter (this single-threaded test is the only writer).
	e.await(e.svc.ReEscrow(ctx, false, p.buyer, ReEscrowRequest{
		Buyer:         p.buyer,
		Seller:        p.artist,
		CertificateId: certificateId,
		ChipId:        p.chipId,
		UnlockAt:      futureUnlockAt(),
		Nonce:         2,
		Amount:        1.0,
	}))
	reEscrowId := e.totalEscrows(t) // newest escrow == the one just created
	t.Logf("T5: re-escrowed certificate %d to escrow %d", certificateId, reEscrowId)
	if s, err := e.svc.fetchEscrow(ctx, reEscrowId); err != nil || s.Status != escrowStatusPending {
		t.Fatalf("expected re-escrow %d to be Pending: %+v err=%v", reEscrowId, s, err)
	}

	// 4. activate-and-settle with an Ixkio tap (bypass mode: any tap Passes)
	e.await(e.svc.ActivateChip(ctx, false, p.buyer, reEscrowId, ActivateChipRequest{
		IxkioTap: ixkio.Tap{X: "dummy-x", N: "dummy-n", E: "dummy-e"},
	}))

	s, err := e.svc.fetchEscrow(ctx, reEscrowId)
	if err != nil {
		t.Fatalf("GetEscrow after activate: %v", err)
	}
	if s.Status != escrowStatusReleased {
		t.Fatalf("expected escrow %d Released(%d) after activate, got %d", reEscrowId, escrowStatusReleased, s.Status)
	}

	// 5. the certificate must now be in the buyer's collection.
	if !e.buyerHoldsCertificate(t, p.buyer, certificateId) {
		t.Fatalf("expected buyer %s to hold certificate %d after settlement", p.buyer, certificateId)
	}
	t.Logf("T5: escrow %d Released, certificate %d transferred to buyer %s", reEscrowId, certificateId, p.buyer)
}

// buyerHoldsCertificate returns whether address's certificate collection
// contains certificateId, via Service.ListCertificates.
func (e *e2eEnv) buyerHoldsCertificate(t *testing.T, address string, certificateId uint64) bool {
	t.Helper()
	certs, err := e.svc.ListCertificates(context.Background(), address)
	if err != nil {
		t.Fatalf("ListCertificates(%s): %v", address, err)
	}
	for _, c := range certs {
		if c.Id == certificateId {
			return true
		}
	}
	return false
}

// totalEscrows returns ArtDropCore.getTotalEscrows() — the monotonic count of
// every escrow ever created. In this single-threaded suite the newest escrow's
// id equals this count, which is how the re-escrow path (whose async job
// carries no result id) recovers the id it just created.
func (e *e2eEnv) totalEscrows(t *testing.T) uint64 {
	t.Helper()
	val, err := e.app.GetTransactions().ExecuteScript(context.Background(), e.svc.getTotalEscrowsCDC, nil)
	if err != nil {
		t.Fatalf("get_total_escrows: %v", err)
	}
	n, ok := val.(cadence.UInt64)
	if !ok {
		t.Fatalf("expected UInt64 from get_total_escrows, got %T", val)
	}
	return uint64(n)
}
