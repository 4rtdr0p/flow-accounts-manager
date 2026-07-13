package m20260620

import "gorm.io/gorm"

const ID = "20260620"

func Migrate(tx *gorm.DB) error {
	return tx.Exec(`ALTER TABLE transactions ALTER COLUMN transaction_type TYPE text USING transaction_type::text`).Error
}

func Rollback(tx *gorm.DB) error {
	return tx.Exec(`ALTER TABLE transactions ALTER COLUMN transaction_type TYPE bigint USING transaction_type::bigint`).Error
}
