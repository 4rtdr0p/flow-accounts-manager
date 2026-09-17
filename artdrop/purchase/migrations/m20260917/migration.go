// m20260917 adds the additive paid-escrow obligation fields for issue #520.
package m20260917

import (
	"gorm.io/gorm"
	"time"
)

const ID = "20260917"

type PurchaseCharge struct {
	ID                   uint      `gorm:"column:id;primary_key;autoIncrement"`
	PurchaseID           string    `gorm:"column:purchase_id;uniqueIndex"`
	Status               string    `gorm:"column:status;index"`
	EscrowIdempotencyKey string    `gorm:"column:escrow_idempotency_key"`
	CertificateID        *uint64   `gorm:"column:certificate_id"`
	UpdatedAt            time.Time `gorm:"column:updated_at"`
}

func (PurchaseCharge) TableName() string { return "purchase_charges" }
func Migrate(tx *gorm.DB) error          { return tx.AutoMigrate(&PurchaseCharge{}) }
func Rollback(tx *gorm.DB) error {
	for _, c := range []string{"updated_at", "certificate_id", "escrow_idempotency_key", "status", "purchase_id"} {
		if err := tx.Migrator().DropColumn(&PurchaseCharge{}, c); err != nil {
			return err
		}
	}
	return nil
}
