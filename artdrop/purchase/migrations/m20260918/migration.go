// m20260918 records the Stripe shipping component separately from the artwork
// and FLOW escrow amounts. The migration is additive and preserves all rows.
package m20260918

import "gorm.io/gorm"

const ID = "20260918"

type PurchaseCharge struct {
	ShippingCents int64 `gorm:"column:shipping_cents;default:0"`
}

func (PurchaseCharge) TableName() string { return "purchase_charges" }
func Migrate(tx *gorm.DB) error          { return tx.AutoMigrate(&PurchaseCharge{}) }
func Rollback(tx *gorm.DB) error {
	return tx.Migrator().DropColumn(&PurchaseCharge{}, "shipping_cents")
}
