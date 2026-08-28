package escrow_projection

import (
	"context"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newTestStore builds a GormStore backed by a fresh in-memory sqlite DB,
// following the same pattern as artdrop/purchase/service_test.go's
// newPurchaseTestService.
func newTestStore(t *testing.T) *GormStore {
	t.Helper()

	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Escrow{}); err != nil {
		t.Fatal(err)
	}
	return NewGormStore(db)
}

func u64(v uint64) *uint64 { return &v }
func str(v string) *string { return &v }

func TestUpsertCreated_ThenGetByID(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	err := store.UpsertCreated(ctx, CreateFields{
		EscrowID:        7,
		Buyer:           "0x179b6b1cb6755e31",
		Seller:          "0xf3fcd2c1a78f5eee",
		EditionID:       u64(42),
		ChipID:          "chip-1",
		UnlockAt:        "4102444800.00000000",
		CertificateID:   99,
		SourceEvent:     "EscrowCreated",
		LastEventHeight: 100,
	})
	if err != nil {
		t.Fatalf("UpsertCreated: %v", err)
	}

	row, err := store.GetByID(ctx, 7)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if row == nil {
		t.Fatal("expected row, got nil")
	}
	if row.Incomplete {
		t.Fatal("expected Incomplete=false for a fully-formed EscrowCreated")
	}
	if row.Status != 0 {
		t.Fatalf("expected default status 0 (Pending), got %d", row.Status)
	}
	if row.Buyer != "0x179b6b1cb6755e31" || row.Seller != "0xf3fcd2c1a78f5eee" {
		t.Fatalf("unexpected buyer/seller: %s / %s", row.Buyer, row.Seller)
	}
	if row.EditionID == nil || *row.EditionID != 42 {
		t.Fatalf("unexpected editionId: %v", row.EditionID)
	}
}

func TestGetByID_ReturnsNilWhenAbsent(t *testing.T) {
	store := newTestStore(t)

	row, err := store.GetByID(context.Background(), 999)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if row != nil {
		t.Fatalf("expected nil row, got %+v", row)
	}
}

// TestUpsertReleased_BeforeCreated_ThenCreatedCompletesWithoutClobberingStatus
// pins the out-of-order case the #102 design report calls out explicitly:
// chain_events' dispatch gives no ordering guarantee between event types in
// the same poll batch (event.go's Trigger spawns one goroutine per event;
// listener.go's run() fetches per type, not per height), so a Released
// event can legitimately reach the handler before its own Created event.
func TestUpsertReleased_BeforeCreated_ThenCreatedCompletesWithoutClobberingStatus(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	reason := uint8(1) // WindowExpired

	// EscrowReleased arrives first — no row exists yet.
	if err := store.UpsertReleased(ctx, 7, 1, reason, 200); err != nil {
		t.Fatalf("UpsertReleased: %v", err)
	}

	stub, err := store.GetByID(ctx, 7)
	if err != nil {
		t.Fatalf("GetByID after stub insert: %v", err)
	}
	if stub == nil {
		t.Fatal("expected a stub row after an out-of-order Released")
	}
	if !stub.Incomplete {
		t.Fatal("expected stub row to be Incomplete")
	}
	if stub.Status != 1 {
		t.Fatalf("expected stub status 1 (Released), got %d", stub.Status)
	}
	if stub.ReleaseReason == nil || *stub.ReleaseReason != reason {
		t.Fatalf("expected stub releaseReason %d, got %v", reason, stub.ReleaseReason)
	}
	if stub.Buyer != "" {
		t.Fatalf("expected empty buyer on stub row, got %q", stub.Buyer)
	}

	// EscrowCreated arrives second, for the same escrow id.
	if err := store.UpsertCreated(ctx, CreateFields{
		EscrowID:        7,
		Buyer:           "0x179b6b1cb6755e31",
		Seller:          "0xf3fcd2c1a78f5eee",
		EditionID:       u64(42),
		ChipID:          "chip-1",
		UnlockAt:        "4102444800.00000000",
		CertificateID:   99,
		SourceEvent:     "EscrowCreated",
		LastEventHeight: 100,
	}); err != nil {
		t.Fatalf("UpsertCreated: %v", err)
	}

	completed, err := store.GetByID(ctx, 7)
	if err != nil {
		t.Fatalf("GetByID after completion: %v", err)
	}
	if completed.Incomplete {
		t.Fatal("expected row to be complete once EscrowCreated has been applied")
	}
	if completed.Buyer != "0x179b6b1cb6755e31" || completed.Seller != "0xf3fcd2c1a78f5eee" {
		t.Fatalf("expected create-owned fields to be filled in, got buyer=%q seller=%q", completed.Buyer, completed.Seller)
	}
	// The critical assertion: EscrowCreated's upsert must NOT have clobbered
	// the status/releaseReason that EscrowReleased already wrote.
	if completed.Status != 1 {
		t.Fatalf("expected status to remain 1 (Released) after EscrowCreated landed, got %d", completed.Status)
	}
	if completed.ReleaseReason == nil || *completed.ReleaseReason != reason {
		t.Fatalf("expected releaseReason to remain %d after EscrowCreated landed, got %v", reason, completed.ReleaseReason)
	}
}

// TestUpsertClaimed_BeforeCreated_ThenCreatedCompletesWithoutClobberingClaimed
// mirrors the Released case for EscrowClaimed, the other stub-owning event.
func TestUpsertClaimed_BeforeCreated_ThenCreatedCompletesWithoutClobberingClaimed(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.UpsertClaimed(ctx, 7, true, str("2026-08-28T00:00:00Z")); err != nil {
		t.Fatalf("UpsertClaimed: %v", err)
	}

	stub, err := store.GetByID(ctx, 7)
	if err != nil {
		t.Fatalf("GetByID after stub insert: %v", err)
	}
	if stub == nil || !stub.Incomplete || !stub.Claimed {
		t.Fatalf("expected an Incomplete, Claimed stub row, got %+v", stub)
	}

	if err := store.UpsertCreated(ctx, CreateFields{
		EscrowID:      7,
		Buyer:         "0x179b6b1cb6755e31",
		Seller:        "0xf3fcd2c1a78f5eee",
		EditionID:     u64(42),
		ChipID:        "chip-1",
		UnlockAt:      "4102444800.00000000",
		CertificateID: 99,
		SourceEvent:   "EscrowCreated",
	}); err != nil {
		t.Fatalf("UpsertCreated: %v", err)
	}

	completed, err := store.GetByID(ctx, 7)
	if err != nil {
		t.Fatalf("GetByID after completion: %v", err)
	}
	if completed.Incomplete {
		t.Fatal("expected row to be complete once EscrowCreated has landed")
	}
	if !completed.Claimed {
		t.Fatal("expected claimed=true to survive EscrowCreated's upsert")
	}
	if completed.ClaimedAt == nil || *completed.ClaimedAt != "2026-08-28T00:00:00Z" {
		t.Fatalf("expected claimedAt to survive EscrowCreated's upsert, got %v", completed.ClaimedAt)
	}
}

// TestUpsertCreated_IsIdempotent pins reprocessing safety: chain_events is
// at-least-once (a restart resumes from the last confirmed height, which
// can replay a batch), so applying the same EscrowCreated event twice must
// be a no-op, not a duplicate row or an error.
func TestUpsertCreated_IsIdempotent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	f := CreateFields{
		EscrowID:      7,
		Buyer:         "0x179b6b1cb6755e31",
		Seller:        "0xf3fcd2c1a78f5eee",
		EditionID:     u64(42),
		ChipID:        "chip-1",
		UnlockAt:      "4102444800.00000000",
		CertificateID: 99,
		SourceEvent:   "EscrowCreated",
	}

	if err := store.UpsertCreated(ctx, f); err != nil {
		t.Fatalf("first UpsertCreated: %v", err)
	}
	if err := store.UpsertCreated(ctx, f); err != nil {
		t.Fatalf("second UpsertCreated: %v", err)
	}

	count, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 row after reprocessing the same event, got %d", count)
	}
}

func TestUpsertReleased_IsIdempotent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if err := store.UpsertReleased(ctx, 7, 1, 0, 200); err != nil {
			t.Fatalf("UpsertReleased call %d: %v", i, err)
		}
	}

	row, err := store.GetByID(ctx, 7)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if row.Status != 1 {
		t.Fatalf("expected status 1, got %d", row.Status)
	}

	count, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 row, got %d", count)
	}
}

func TestEditionIDForCertificate_ResolvesFromExistingRow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.UpsertCreated(ctx, CreateFields{
		EscrowID:      7,
		Buyer:         "0x179b6b1cb6755e31",
		Seller:        "0xf3fcd2c1a78f5eee",
		EditionID:     u64(42),
		ChipID:        "chip-1",
		CertificateID: 99,
		SourceEvent:   "EscrowCreated",
	}); err != nil {
		t.Fatalf("UpsertCreated: %v", err)
	}

	editionID, err := store.EditionIDForCertificate(ctx, 99)
	if err != nil {
		t.Fatalf("EditionIDForCertificate: %v", err)
	}
	if editionID == nil || *editionID != 42 {
		t.Fatalf("expected editionId 42, got %v", editionID)
	}
}

func TestEditionIDForCertificate_ReturnsNilWhenUnprojected(t *testing.T) {
	store := newTestStore(t)

	editionID, err := store.EditionIDForCertificate(context.Background(), 99)
	if err != nil {
		t.Fatalf("EditionIDForCertificate: %v", err)
	}
	if editionID != nil {
		t.Fatalf("expected nil, got %v", editionID)
	}
}

// TestUpsertBackfillRow_NeverOverwritesExistingRow pins the backfill safety
// property: the live listener path is always authoritative, so backfill
// must never clobber a row it already wrote.
func TestUpsertBackfillRow_NeverOverwritesExistingRow(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.UpsertCreated(ctx, CreateFields{
		EscrowID:      7,
		Buyer:         "0xlivebuyer00000000",
		Seller:        "0xliveseller0000000",
		EditionID:     u64(42),
		CertificateID: 99,
		SourceEvent:   "EscrowCreated",
	}); err != nil {
		t.Fatalf("UpsertCreated: %v", err)
	}

	// Backfill sees the same id with (hypothetically) different data — e.g.
	// a stale read — and must not overwrite the live row.
	if err := store.UpsertBackfillRow(ctx, Escrow{
		EscrowID: 7,
		Buyer:    "0xbackfillbuyer00000",
		Seller:   "0xbackfillseller0000",
	}); err != nil {
		t.Fatalf("UpsertBackfillRow: %v", err)
	}

	row, err := store.GetByID(ctx, 7)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if row.Buyer != "0xlivebuyer00000000" {
		t.Fatalf("expected live row to survive backfill untouched, got buyer=%q", row.Buyer)
	}
}

func TestUpsertBackfillRow_FillsGapsTheListenerHasntReachedYet(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	editionID := uint64(1)
	if err := store.UpsertBackfillRow(ctx, Escrow{
		EscrowID:      8,
		Buyer:         "0x179b6b1cb6755e31",
		Seller:        "0xf3fcd2c1a78f5eee",
		EditionID:     &editionID,
		CertificateID: 10,
		Status:        1,
		SourceEvent:   "backfill",
	}); err != nil {
		t.Fatalf("UpsertBackfillRow: %v", err)
	}

	row, err := store.GetByID(ctx, 8)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if row == nil || row.Buyer != "0x179b6b1cb6755e31" || row.Status != 1 {
		t.Fatalf("expected backfilled row to be present and complete, got %+v", row)
	}
}

func TestListByBuyerEditionSeller(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	rows := []CreateFields{
		{EscrowID: 1, Buyer: "0xbuyerA", Seller: "0xsellerA", EditionID: u64(1), CertificateID: 1, SourceEvent: "EscrowCreated"},
		{EscrowID: 2, Buyer: "0xbuyerA", Seller: "0xsellerB", EditionID: u64(2), CertificateID: 2, SourceEvent: "EscrowCreated"},
		{EscrowID: 3, Buyer: "0xbuyerB", Seller: "0xsellerA", EditionID: u64(1), CertificateID: 3, SourceEvent: "EscrowCreated"},
	}
	for _, f := range rows {
		if err := store.UpsertCreated(ctx, f); err != nil {
			t.Fatalf("UpsertCreated(%d): %v", f.EscrowID, err)
		}
	}

	byBuyer, err := store.ListByBuyer(ctx, "0xbuyerA")
	if err != nil {
		t.Fatalf("ListByBuyer: %v", err)
	}
	if len(byBuyer) != 2 {
		t.Fatalf("expected 2 escrows for buyerA, got %d", len(byBuyer))
	}

	byEdition, err := store.ListByEdition(ctx, 1)
	if err != nil {
		t.Fatalf("ListByEdition: %v", err)
	}
	if len(byEdition) != 2 {
		t.Fatalf("expected 2 escrows for edition 1, got %d", len(byEdition))
	}

	bySeller, err := store.ListBySeller(ctx, "0xsellerA")
	if err != nil {
		t.Fatalf("ListBySeller: %v", err)
	}
	if len(bySeller) != 2 {
		t.Fatalf("expected 2 escrows for sellerA, got %d", len(bySeller))
	}
}

func TestCount(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	count, err := store.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected empty store, got count %d", count)
	}

	if err := store.UpsertCreated(ctx, CreateFields{EscrowID: 1, SourceEvent: "EscrowCreated"}); err != nil {
		t.Fatalf("UpsertCreated: %v", err)
	}

	count, err = store.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}
}
