// m20260820 splits studio_production_charges.amount_cents into its
// production and shipping components, so ops can reconcile each against the
// pricing engine and the flat shipping rate separately instead of only
// seeing a single total.
package m20260820

import (
	"time"

	"gorm.io/gorm"
)

const ID = "20260820"

// StudioProductionCharge is the studio_production_charges model as of this
// migration: the m20260807 shape plus the production/shipping split.
type StudioProductionCharge struct {
	ID                    uint      `gorm:"column:id;primary_key;autoIncrement"`
	UserID                string    `gorm:"column:user_id;index"`
	QuoteID               string    `gorm:"column:quote_id;index"`
	AmountCents           int64     `gorm:"column:amount_cents"`
	ProductionAmountCents int64     `gorm:"column:production_amount_cents"`
	ShippingAmountCents   int64     `gorm:"column:shipping_amount_cents"`
	Currency              string    `gorm:"column:currency;size:3;default:usd"`
	StripePaymentIntent   string    `gorm:"column:stripe_payment_intent_id;uniqueIndex"`
	PricingHash           string    `gorm:"column:pricing_hash"`
	EngineVersion         string    `gorm:"column:engine_version"`
	Metadata              string    `gorm:"column:metadata;type:text"`
	CreatedAt             time.Time `gorm:"column:created_at"`
}

func (StudioProductionCharge) TableName() string {
	return "studio_production_charges"
}

func Migrate(tx *gorm.DB) error {
	return tx.AutoMigrate(&StudioProductionCharge{})
}

func Rollback(tx *gorm.DB) error {
	if err := tx.Migrator().DropColumn(&StudioProductionCharge{}, "production_amount_cents"); err != nil {
		return err
	}
	return tx.Migrator().DropColumn(&StudioProductionCharge{}, "shipping_amount_cents")
}
