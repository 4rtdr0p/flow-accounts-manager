package m20260620

import (
	"github.com/flow-hydraulics/flow-wallet-api/migrations"
	"gorm.io/gorm"
)

var ID = "20260620"

func Migrate(db *gorm.DB) error {
	return db.Exec(`ALTER TABLE transactions ALTER COLUMN transaction_type TYPE text USING transaction_type::text`).Error
}

func Rollback(db *gorm.DB) error {
	return db.Exec(`ALTER TABLE transactions ALTER COLUMN transaction_type TYPE bigint USING transaction_type::bigint`).Error
}

func init() {
	migrations.MustRegister(func(db *gorm.DB) error {
		return Migrate(db)
	})
}
