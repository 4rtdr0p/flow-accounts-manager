package artdrop

import (
	"testing"

	purchasemigration "github.com/flow-hydraulics/flow-wallet-api/artdrop/purchase/migrations/m20260922"
	"github.com/flow-hydraulics/flow-wallet-api/migrations"
)

func TestPurchaseRecoveryMigrationIsLastAndUnique(t *testing.T) {
	list := Migrations()
	if list[len(list)-1].ID != purchasemigration.ID {
		t.Fatal("purchase recovery migration must be appended")
	}
	seen := map[string]bool{}
	for _, m := range append(migrations.List(), list...) {
		if seen[m.ID] {
			t.Fatalf("duplicate global migration ID %s", m.ID)
		}
		seen[m.ID] = true
	}
}
