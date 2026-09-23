package migrations

import (
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/migrations/internal/m20260922"
)

func TestPurchaseMigrationIsLastAndUnique(t *testing.T) {
	list := List()
	if list[len(list)-1].ID != m20260922.ID {
		t.Fatal("purchase migration must be appended")
	}
	seen := map[string]bool{}
	for _, m := range list {
		if seen[m.ID] {
			t.Fatalf("duplicate migration ID %s", m.ID)
		}
		seen[m.ID] = true
	}
}
