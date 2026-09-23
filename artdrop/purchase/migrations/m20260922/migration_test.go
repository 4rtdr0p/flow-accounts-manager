package m20260922

import (
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop/purchase/migrations/m20260821"
	"github.com/flow-hydraulics/flow-wallet-api/artdrop/purchase/migrations/m20260828"
	"github.com/flow-hydraulics/flow-wallet-api/artdrop/purchase/migrations/m20260917"
	"github.com/flow-hydraulics/flow-wallet-api/artdrop/purchase/migrations/m20260918"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrationFromLegacyPurchasesWithData(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []func(*gorm.DB) error{m20260821.Migrate, m20260828.Migrate, m20260917.Migrate, m20260918.Migrate} {
		if err := m(db); err != nil {
			t.Fatal(err)
		}
	}
	for _, pi := range []string{"pi_old1", "pi_old2"} {
		if err := db.Exec("INSERT INTO purchase_charges (user_id,purchase_id,stripe_payment_intent_id,amount_cents,metadata) VALUES (?,?,?,?,?)", "same-owner", pi, pi, 12500, "offer").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		UserID, Metadata                                               string
		AmountCents                                                    int64
		ChargeIntentID, ChargeOperation, RequestHash, StripeCustomerID *string
		HashVersion                                                    *int
	}
	if err := db.Table("purchase_charges").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatal("lost legacy rows")
	}
	for _, row := range rows {
		if row.ChargeIntentID != nil || row.ChargeOperation != nil || row.RequestHash != nil || row.HashVersion != nil || row.StripeCustomerID != nil || row.UserID != "same-owner" || row.AmountCents != 12500 || row.Metadata != "offer" {
			t.Fatalf("backfilled legacy data: %+v", row)
		}
	}
	insert := func(pi, owner, intent string) error {
		return db.Exec("INSERT INTO purchase_charges (user_id,purchase_id,stripe_payment_intent_id,charge_operation,charge_intent_id) VALUES (?,?,?,?,?)", owner, pi, pi, "purchases.charge.v2", intent).Error
	}
	if err := insert("pi_new", "same-owner", "intent"); err != nil {
		t.Fatal(err)
	}
	if err := insert("pi_duplicate", "same-owner", "intent"); err == nil {
		t.Fatal("missing unique intent index")
	}
	if err := insert("pi_other", "another-owner", "intent"); err != nil {
		t.Fatal(err)
	}
	if err := Rollback(db); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Table("purchase_charges").Count(&count)
	if count != 4 {
		t.Fatal("rollback deleted financial evidence")
	}
}
