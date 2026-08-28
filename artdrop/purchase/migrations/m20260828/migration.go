// m20260828 adds escrow_job_id and escrow_id to purchase_charges (issue
// #98), so a purchase audit record can be traced to the async escrow-
// creation job it triggered, and — once that job's Result has been
// resolved — to the on-chain escrow itself.
//
// escrow_job_id is populated at charge time by
// purchase.ServiceImpl.CreatePurchaseCharge (the wallet-api's job UUID for
// the CreateEscrow transaction). escrow_id is left NULL by this migration
// and by that flow: resolving it from the job's Result (see
// extractEscrowCreatedResult in the artdrop package, registered for
// TxTypeCreateEscrow) and backfilling this column is a separate
// reconciliation step, not built here.
package m20260828

import (
	"time"

	"gorm.io/gorm"
)

const ID = "20260828"

// PurchaseCharge is the purchase_charges model as of this migration: the
// m20260821 shape plus escrow_job_id/escrow_id.
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
	EscrowJobID         string    `gorm:"column:escrow_job_id;index"`
	EscrowID            *uint64   `gorm:"column:escrow_id"`
	CreatedAt           time.Time `gorm:"column:created_at"`
}

func (PurchaseCharge) TableName() string {
	return "purchase_charges"
}

func Migrate(tx *gorm.DB) error {
	return tx.AutoMigrate(&PurchaseCharge{})
}

func Rollback(tx *gorm.DB) error {
	if err := tx.Migrator().DropColumn(&PurchaseCharge{}, "escrow_job_id"); err != nil {
		return err
	}
	return tx.Migrator().DropColumn(&PurchaseCharge{}, "escrow_id")
}
