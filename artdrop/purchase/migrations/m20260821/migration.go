// m20260821 adds the purchase_charges audit table.
//
// This table records every real buyer purchase charge: the USD amount actually
// charged to Stripe, the FLOW amount the escrow was opened with, the artwork
// and parties involved, and the Pyth price that converted between them. It
// lets us answer "how much was this buyer charged, in what, and at what
// exchange rate" without having to go back to Mongo or the oracle.
package m20260821

import (
	"time"

	"gorm.io/gorm"
)

const ID = "20260821"

// PurchaseCharge is the audit record for a single buyer purchase charge.
type PurchaseCharge struct {
	ID                  uint      `gorm:"column:id;primary_key;autoIncrement"`
	UserID              string    `gorm:"column:user_id;index"`
	ArtworkKind         string    `gorm:"column:artwork_kind;size:16"`
	ArtworkID           string    `gorm:"column:artwork_id;index"`
	AmountCents         int64     `gorm:"column:amount_cents"`
	PlatformFeeCents    int64     `gorm:"column:platform_fee_cents"`
	Currency            string    `gorm:"column:currency;size:3;default:usd"`
	FlowAmount          float64   `gorm:"column:flow_amount"`
	FlowPriceUSD        float64   `gorm:"column:flow_price_usd"`
	StripePaymentIntent string    `gorm:"column:stripe_payment_intent_id;uniqueIndex"`
	Buyer               string    `gorm:"column:buyer"`
	Seller              string    `gorm:"column:seller"`
	EditionID           uint64    `gorm:"column:edition_id"`
	ChipID              string    `gorm:"column:chip_id"`
	UnlockAt            float64   `gorm:"column:unlock_at"`
	Nonce               uint64    `gorm:"column:nonce"`
	Metadata            string    `gorm:"column:metadata;type:text"`
	CreatedAt           time.Time `gorm:"column:created_at"`
}

func (PurchaseCharge) TableName() string {
	return "purchase_charges"
}

func Migrate(tx *gorm.DB) error {
	return tx.AutoMigrate(&PurchaseCharge{})
}

func Rollback(tx *gorm.DB) error {
	return tx.Migrator().DropTable(&PurchaseCharge{})
}
