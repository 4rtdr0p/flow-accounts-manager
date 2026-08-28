package m20260829

import (
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newTestDB follows the same per-test-name in-memory DSN convention as
// artdrop/purchase/service_test.go's newPurchaseTestService, so parallel
// test runs never share a database.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMigrate_CreatesEscrowsTable(t *testing.T) {
	db := newTestDB(t)

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if !db.Migrator().HasTable(&Escrow{}) {
		t.Fatal("expected escrows table to exist after Migrate")
	}

	for _, col := range []string{
		"escrow_id", "buyer", "seller", "edition_id", "chip_id", "unlock_at",
		"certificate_id", "source_event", "status", "release_reason",
		"claimed", "claimed_at", "last_event_height", "incomplete", "updated_at",
	} {
		if !db.Migrator().HasColumn(&Escrow{}, col) {
			t.Fatalf("expected column %q to exist after Migrate", col)
		}
	}

	// Insert + read round trip, sanity-checking the shape is actually usable.
	row := Escrow{EscrowID: 7, Buyer: "0x179b6b1cb6755e31", Status: 0}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("insert after migrate: %v", err)
	}

	var got Escrow
	if err := db.First(&got, "escrow_id = ?", 7).Error; err != nil {
		t.Fatalf("read back after migrate: %v", err)
	}
	if got.Buyer != "0x179b6b1cb6755e31" {
		t.Fatalf("unexpected buyer: %s", got.Buyer)
	}
}

func TestMigrateThenRollback_DropsEscrowsTable(t *testing.T) {
	db := newTestDB(t)

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := Rollback(db); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	if db.Migrator().HasTable(&Escrow{}) {
		t.Fatal("expected escrows table to be dropped after Rollback")
	}
}
