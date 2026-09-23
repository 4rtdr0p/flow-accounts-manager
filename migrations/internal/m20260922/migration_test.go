package m20260922

import (
	"testing"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/flow-hydraulics/flow-wallet-api/migrations/internal/m20211202"
	"github.com/flow-hydraulics/flow-wallet-api/migrations/internal/m20260723"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrationPreservesOldCacheRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []func(*gorm.DB) error{m20211202.Migrate, m20260723.Migrate} {
		if err := m(db); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().UTC().Add(time.Hour)
	for _, key := range []string{"one", "two"} {
		if err := db.Exec("INSERT INTO idempotency_keys (key,expiry_date,status_code,body,completed) VALUES (?,?,?,?,?)", key, old, 201, []byte("response"), true).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var rows []handlers.PurchaseIntent
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("lost old rows: %d", len(rows))
	}
	for _, row := range rows {
		if row.IntentID != nil || row.RecordKind != nil || row.RequestHash != nil || row.ExecutionState != nil || row.ExpiryDate == nil || !row.ExpiryDate.Equal(old) || string(row.Body) != "response" || !row.Completed {
			t.Fatalf("old row altered: %+v", row)
		}
	}
	row := handlers.PurchaseIntent{Key: "durable", PurchaseIntentFields: handlers.PurchaseIntentFields{OwnerUserID: handlers.Ptr("owner"), Operation: handlers.Ptr(handlers.PurchaseOperation), IntentID: handlers.Ptr("intent")}}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	row.Key = "different-key"
	if err := db.Create(&row).Error; err == nil {
		t.Fatal("missing financial tuple uniqueness")
	}
	if err := Rollback(db); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&handlers.PurchaseIntent{}).Count(&count)
	if count != 3 {
		t.Fatal("rollback deleted identities")
	}
}
