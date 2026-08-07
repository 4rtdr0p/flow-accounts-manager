// m20260807 adds the studio_production_charges audit table.
//
// This table records every real Studio production charge: the amount actually
// charged, the Stripe payment intent that settled it, and a hash of the
// pricing config / rate / engine version that produced the amount. It lets us
// answer "how much was user X charged and with what data" without having to
// go back to Mongo.
package m20260807

import "gorm.io/gorm"

const ID = "20260807"

// StudioProductionCharge is the audit record for a single Studio production
// charge. The pricing_hash is a fingerprint of the config/rate/engine version
// used to compute the amount at charge time.
type StudioProductionCharge struct {
	ID                  uint    `gorm:"column:id;primary_key;autoIncrement"`
	UserID              string  `gorm:"column:user_id;index"`
	QuoteID             string  `gorm:"column:quote_id;index"`
	AmountCents         int64   `gorm:"column:amount_cents"`
	Currency            string  `gorm:"column:currency;size:3;default:usd"`
	StripePaymentIntent string  `gorm:"column:stripe_payment_intent_id;uniqueIndex"`
	PricingHash         string  `gorm:"column:pricing_hash"`
	EngineVersion       string  `gorm:"column:engine_version"`
	Metadata            string  `gorm:"column:metadata;type:text"`
	CreatedAt           *string `gorm:"column:created_at"`
}

func (StudioProductionCharge) TableName() string {
	return "studio_production_charges"
}

func Migrate(tx *gorm.DB) error {
	return tx.AutoMigrate(&StudioProductionCharge{})
}

func Rollback(tx *gorm.DB) error {
	return tx.Migrator().DropTable(&StudioProductionCharge{})
}
