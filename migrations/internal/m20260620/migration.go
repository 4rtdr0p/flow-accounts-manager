package m20260620

import "gorm.io/gorm"

const ID = "20260620"

type TransactionWithTextType struct {
	TransactionType string `gorm:"column:transaction_type;index;type:text"`
}

func (TransactionWithTextType) TableName() string {
	return "transactions"
}

func Migrate(tx *gorm.DB) error {
	return tx.Migrator().AlterColumn(&TransactionWithTextType{}, "TransactionType")
}

func Rollback(tx *gorm.DB) error {
	return nil
}
