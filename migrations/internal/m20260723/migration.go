// m20260723 adds stored HTTP response data for idempotency replay.
package m20260723

import "gorm.io/gorm"

const ID = "20260723"

type IdempotencyStoreGormItem struct {
	Key         string `gorm:"column:key;primary_key"`
	StatusCode  int    `gorm:"column:status_code"`
	ContentType string `gorm:"column:content_type"`
	Body        []byte `gorm:"column:body"`
	Completed   bool   `gorm:"column:completed"`
}

func (IdempotencyStoreGormItem) TableName() string {
	return "idempotency_keys"
}

func Migrate(tx *gorm.DB) error {
	return tx.AutoMigrate(&IdempotencyStoreGormItem{})
}

func Rollback(tx *gorm.DB) error {
	if err := tx.Migrator().DropColumn(&IdempotencyStoreGormItem{}, "status_code"); err != nil {
		return err
	}
	if err := tx.Migrator().DropColumn(&IdempotencyStoreGormItem{}, "content_type"); err != nil {
		return err
	}
	if err := tx.Migrator().DropColumn(&IdempotencyStoreGormItem{}, "body"); err != nil {
		return err
	}
	if err := tx.Migrator().DropColumn(&IdempotencyStoreGormItem{}, "completed"); err != nil {
		return err
	}

	return nil
}
