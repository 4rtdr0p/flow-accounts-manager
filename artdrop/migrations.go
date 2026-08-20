package artdrop

import (
	"github.com/flow-hydraulics/flow-wallet-api/artdrop/studio/migrations/m20260807"
	"github.com/flow-hydraulics/flow-wallet-api/artdrop/studio/migrations/m20260820"
	"github.com/go-gormigrate/gormigrate/v2"
)

// Migrations returns the artdrop plugin's database migrations, in order.
// Migrations are static (they need no runtime deps), so this can run before
// the plugin instance is constructed: main.go calls it alongside
// migrations.List() when opening the database (see datastore/gorm.New), and
// the plugin instance is built afterwards from the resulting *gorm.DB. This
// keeps every plugin migration in the same gormigrate table as the core
// migrations, so rollback and versioning stay coherent across the whole
// schema.
func Migrations() []*gormigrate.Migration {
	return []*gormigrate.Migration{
		{
			ID:       m20260807.ID,
			Migrate:  m20260807.Migrate,
			Rollback: m20260807.Rollback,
		},
		{
			ID:       m20260820.ID,
			Migrate:  m20260820.Migrate,
			Rollback: m20260820.Rollback,
		},
	}
}
