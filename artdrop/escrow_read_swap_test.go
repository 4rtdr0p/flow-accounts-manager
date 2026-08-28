package artdrop

import (
	"context"
	"strings"
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop/escrow_projection"
	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newProjectionTestDB opens a fresh in-memory sqlite DB migrated for the
// escrows table, following the same per-test-name DSN convention used
// throughout this repo's tests (see artdrop/purchase/service_test.go).
func newProjectionTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&escrow_projection.Escrow{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func editionPtr(v uint64) *uint64 { return &v }

// --- GetEscrow read-swap ---

func TestGetEscrow_ServesFromProjectionWithoutTouchingChain(t *testing.T) {
	db := newProjectionTestDB(t)
	// Deliberately zero-value: if the projection path has a bug and falls
	// through to the chain anyway, ExecuteScript records the call (so the
	// len(txSvc.calls) assertions below catch it) and returns a nil
	// cadence.Value, which the decoder rejects — either way the test fails
	// loudly instead of silently passing.
	txSvc := &queryTxService{}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
		DB:           db,
	})

	if err := svc.escrowStore.UpsertCreated(context.Background(), escrow_projection.CreateFields{
		EscrowID:      7,
		Buyer:         "0x179b6b1cb6755e31",
		Seller:        "0xf3fcd2c1a78f5eee",
		EditionID:     editionPtr(42),
		ChipID:        "chip-1",
		UnlockAt:      "4102444800.00000000",
		CertificateID: 99,
		SourceEvent:   "EscrowCreated",
	}); err != nil {
		t.Fatalf("seed projection: %v", err)
	}

	summary, err := svc.GetEscrow(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetEscrow: %v", err)
	}
	if summary == nil {
		t.Fatal("expected a summary, got nil")
	}
	if summary.Buyer != "0x179b6b1cb6755e31" || summary.EditionId != 42 || summary.CertificateId != 99 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if len(txSvc.calls) != 0 {
		t.Fatalf("expected zero chain calls when the projection has the row, got %d", len(txSvc.calls))
	}
}

func TestGetEscrow_FallsBackToChainWhenRowMissing(t *testing.T) {
	db := newProjectionTestDB(t)
	txSvc := &queryTxService{
		scriptResult: escrowSummaryScriptResult(t, 7, 42, 1, 99, 0, nil, false, nil),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
		DB:           db,
	})

	summary, err := svc.GetEscrow(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetEscrow: %v", err)
	}
	if summary == nil || summary.Id != 7 {
		t.Fatalf("expected the chain-backed summary, got %+v", summary)
	}
	if len(txSvc.calls) != 1 {
		t.Fatalf("expected exactly 1 chain call (the fallback), got %d", len(txSvc.calls))
	}
}

func TestGetEscrow_FallsBackToChainWhenRowIsIncomplete(t *testing.T) {
	db := newProjectionTestDB(t)
	txSvc := &queryTxService{
		scriptResult: escrowSummaryScriptResult(t, 7, 42, 1, 99, 1, nil, false, nil),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
		DB:           db,
	})

	// A Released event arrived before its own EscrowCreated — a stub,
	// Incomplete row (see escrow_projection's out-of-order handling).
	reason := uint8(1)
	if err := svc.escrowStore.UpsertReleased(context.Background(), 7, 1, reason, 100); err != nil {
		t.Fatalf("seed stub: %v", err)
	}

	summary, err := svc.GetEscrow(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetEscrow: %v", err)
	}
	if summary == nil || summary.Id != 7 {
		t.Fatalf("expected the chain-backed summary, got %+v", summary)
	}
	if len(txSvc.calls) != 1 {
		t.Fatalf("expected the Incomplete row to be rejected and the chain consulted, got %d calls", len(txSvc.calls))
	}
}

// --- ListEscrowsByBuyer read-swap (representative of ByEdition/BySeller too — same helper) ---

func TestListEscrowsByBuyer_ServesFromProjectionWhenReady(t *testing.T) {
	db := newProjectionTestDB(t)
	// Deliberately zero-value: if the projection path has a bug and falls
	// through to the chain anyway, ExecuteScript records the call (so the
	// len(txSvc.calls) assertions below catch it) and returns a nil
	// cadence.Value, which the decoder rejects — either way the test fails
	// loudly instead of silently passing.
	txSvc := &queryTxService{}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
		DB:           db,
	})

	ctx := context.Background()
	if err := svc.escrowStore.UpsertCreated(ctx, escrow_projection.CreateFields{
		EscrowID: 1, Buyer: "0x179b6b1cb6755e31", Seller: "0xf3fcd2c1a78f5eee",
		EditionID: editionPtr(1), CertificateID: 10, SourceEvent: "EscrowCreated",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := svc.escrowStore.UpsertCreated(ctx, escrow_projection.CreateFields{
		EscrowID: 2, Buyer: "0x179b6b1cb6755e31", Seller: "0xf3fcd2c1a78f5eee",
		EditionID: editionPtr(2), CertificateID: 11, SourceEvent: "EscrowCreated",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res, err := svc.ListEscrowsByBuyer(ctx, "0x179b6b1cb6755e31", true)
	if err != nil {
		t.Fatalf("ListEscrowsByBuyer: %v", err)
	}
	if len(res.EscrowIds) != 2 {
		t.Fatalf("expected 2 escrow ids, got %+v", res.EscrowIds)
	}
	if len(res.Escrows) != 2 {
		t.Fatalf("expected 2 expanded summaries, got %+v", res.Escrows)
	}
	if len(txSvc.calls) != 0 {
		t.Fatalf("expected zero chain calls when the projection is ready, got %d", len(txSvc.calls))
	}
}

func TestListEscrowsByBuyer_TrustsAGenuinelyEmptyResultOnceProjectionIsReady(t *testing.T) {
	db := newProjectionTestDB(t)
	// Deliberately zero-value: if the projection path has a bug and falls
	// through to the chain anyway, ExecuteScript records the call (so the
	// len(txSvc.calls) assertions below catch it) and returns a nil
	// cadence.Value, which the decoder rejects — either way the test fails
	// loudly instead of silently passing.
	txSvc := &queryTxService{}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
		DB:           db,
	})

	ctx := context.Background()
	// Seed a row for a DIFFERENT buyer so Count() > 0 (projection is
	// "ready"), while the queried buyer genuinely has none.
	if err := svc.escrowStore.UpsertCreated(ctx, escrow_projection.CreateFields{
		EscrowID: 1, Buyer: "0xf3fcd2c1a78f5eee", SourceEvent: "EscrowCreated",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res, err := svc.ListEscrowsByBuyer(ctx, "0x179b6b1cb6755e31", false)
	if err != nil {
		t.Fatalf("ListEscrowsByBuyer: %v", err)
	}
	if len(res.EscrowIds) != 0 {
		t.Fatalf("expected a genuinely empty result, got %+v", res.EscrowIds)
	}
	if len(txSvc.calls) != 0 {
		t.Fatalf("expected zero chain calls (empty result trusted once ready), got %d", len(txSvc.calls))
	}
}

func TestListEscrowsByBuyer_FallsBackToChainWhenProjectionIsEmpty(t *testing.T) {
	db := newProjectionTestDB(t)
	txSvc := &queryTxService{
		scriptResults: []cadence.Value{
			cadence.NewArray([]cadence.Value{cadence.NewUInt64(7)}), // get_escrows_by_buyer ids
		},
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
		DB:           db,
	})

	res, err := svc.ListEscrowsByBuyer(context.Background(), "0x179b6b1cb6755e31", false)
	if err != nil {
		t.Fatalf("ListEscrowsByBuyer: %v", err)
	}
	if len(res.EscrowIds) != 1 || res.EscrowIds[0] != 7 {
		t.Fatalf("expected the chain-backed ids [7], got %+v", res.EscrowIds)
	}
	if len(txSvc.calls) != 1 {
		t.Fatalf("expected exactly 1 chain call (the fallback), got %d", len(txSvc.calls))
	}
}

// TestListEscrowsByBuyer_NilDBBehavesExactlyAsBeforeThisFeature pins that
// #102 changes nothing for the deps.DB==nil case (some test/dev
// constructions never wire a DB) — same chain-only behavior as before.
func TestListEscrowsByBuyer_NilDBBehavesExactlyAsBeforeThisFeature(t *testing.T) {
	txSvc := &queryTxService{
		scriptResults: []cadence.Value{
			cadence.NewArray([]cadence.Value{cadence.NewUInt64(7)}),
		},
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	if svc.escrowStore != nil {
		t.Fatal("expected escrowStore to be nil when deps.DB is nil")
	}

	res, err := svc.ListEscrowsByBuyer(context.Background(), "0x179b6b1cb6755e31", false)
	if err != nil {
		t.Fatalf("ListEscrowsByBuyer: %v", err)
	}
	if len(res.EscrowIds) != 1 || res.EscrowIds[0] != 7 {
		t.Fatalf("expected the chain-backed ids [7], got %+v", res.EscrowIds)
	}
}

// --- Backfill ---

func TestBackfillEscrowProjection_PopulatesFromChainThenIsANoOp(t *testing.T) {
	db := newProjectionTestDB(t)
	dict1 := escrowSummaryDict(t, 1, 10, 1, 100, 1, nil, true, nil)
	dict2 := escrowSummaryDict(t, 2, 11, 2, 101, 0, nil, false, nil)
	txSvc := &queryTxService{
		scriptResults: []cadence.Value{
			cadence.NewUInt64(2), // get_total_escrows
			escrowSummaryExpandedArrayResult(t, dict1, dict2), // get_all_escrow_summaries
		},
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
		DB:           db,
	})

	written, err := svc.BackfillEscrowProjection(context.Background())
	if err != nil {
		t.Fatalf("BackfillEscrowProjection: %v", err)
	}
	if written != 2 {
		t.Fatalf("expected 2 rows written, got %d", written)
	}

	count, err := svc.escrowStore.Count(context.Background())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 rows in the projection, got %d", count)
	}

	row, err := svc.escrowStore.GetByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if row == nil || row.Incomplete {
		t.Fatalf("expected a complete backfilled row, got %+v", row)
	}
	if row.Status != 1 || !row.Claimed {
		t.Fatalf("expected status=1 claimed=true from backfill, got %+v", row)
	}

	// Second call: table is no longer empty, so it must be a self-healing
	// no-op — no further chain calls.
	written, err = svc.BackfillEscrowProjection(context.Background())
	if err != nil {
		t.Fatalf("second BackfillEscrowProjection: %v", err)
	}
	if written != 0 {
		t.Fatalf("expected the second backfill call to write 0 rows, got %d", written)
	}
	if len(txSvc.calls) != 2 {
		t.Fatalf("expected exactly 2 chain calls total (total + one range, none on the second run), got %d", len(txSvc.calls))
	}
}

func TestBackfillEscrowProjection_NeverOverwritesRowsTheListenerAlreadyWrote(t *testing.T) {
	db := newProjectionTestDB(t)
	txSvc := &queryTxService{
		scriptResults: []cadence.Value{
			cadence.NewUInt64(1),
			escrowSummaryExpandedArrayResult(t, escrowSummaryDict(t, 1, 999, 1, 999, 1, nil, true, nil)),
		},
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
		DB:           db,
	})

	// Table starts non-empty (as if the live listener had already written a
	// row) — Count()>0 must make backfill a no-op even though it's the
	// first time BackfillEscrowProjection itself is called.
	if err := svc.escrowStore.UpsertCreated(context.Background(), escrow_projection.CreateFields{
		EscrowID: 1, Buyer: "0xlivebuyer00000000", SourceEvent: "EscrowCreated",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	written, err := svc.BackfillEscrowProjection(context.Background())
	if err != nil {
		t.Fatalf("BackfillEscrowProjection: %v", err)
	}
	if written != 0 {
		t.Fatalf("expected a no-op (0 written), got %d", written)
	}
	if len(txSvc.calls) != 0 {
		t.Fatalf("expected zero chain calls when the table is already non-empty, got %d", len(txSvc.calls))
	}

	row, err := svc.escrowStore.GetByID(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if row.Buyer != "0xlivebuyer00000000" {
		t.Fatalf("expected the live row to survive untouched, got buyer=%q", row.Buyer)
	}
}

func TestBackfillEscrowProjection_NoOpWhenDBIsNil(t *testing.T) {
	// Deliberately zero-value: if the projection path has a bug and falls
	// through to the chain anyway, ExecuteScript records the call (so the
	// len(txSvc.calls) assertions below catch it) and returns a nil
	// cadence.Value, which the decoder rejects — either way the test fails
	// loudly instead of silently passing.
	txSvc := &queryTxService{}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	written, err := svc.BackfillEscrowProjection(context.Background())
	if err != nil {
		t.Fatalf("BackfillEscrowProjection: %v", err)
	}
	if written != 0 {
		t.Fatalf("expected 0 written when escrowStore is nil, got %d", written)
	}
}
