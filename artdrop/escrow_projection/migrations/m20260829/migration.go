// m20260829 adds the escrows table (issue #102): a local read-model
// projection of ArtDropCore escrows, kept in sync by
// escrow_projection.ArtDropEscrowEventHandler off the shared chain_events
// listener. Purely additive — no existing table or column is touched, and
// nothing on-chain changes; see the #102 design report for the full
// rationale.
//
// This migration defines its own snapshot of the row shape (rather than
// importing escrow_projection.Escrow), following the same convention as
// artdrop/purchase/migrations/m20260821 and m20260828: a migration's struct
// is a point-in-time snapshot for AutoMigrate, decoupled from however the
// live package's struct evolves afterwards.
package m20260829

import (
	"time"

	"gorm.io/gorm"
)

const ID = "20260829"

// Escrow is the escrows table shape as of this migration.
type Escrow struct {
	EscrowID        uint64    `gorm:"column:escrow_id;primary_key;autoIncrement:false"`
	Buyer           string    `gorm:"column:buyer;size:18;index:idx_escrows_buyer"`
	Seller          string    `gorm:"column:seller;size:18;index:idx_escrows_seller"`
	EditionID       *uint64   `gorm:"column:edition_id;index:idx_escrows_edition"`
	ChipID          string    `gorm:"column:chip_id;index:idx_escrows_chip"`
	UnlockAt        string    `gorm:"column:unlock_at"`
	CertificateID   uint64    `gorm:"column:certificate_id;index:idx_escrows_certificate"`
	SourceEvent     string    `gorm:"column:source_event;size:24"`
	Status          uint8     `gorm:"column:status;default:0"`
	ReleaseReason   *uint8    `gorm:"column:release_reason"`
	Claimed         bool      `gorm:"column:claimed;default:false"`
	ClaimedAt       *string   `gorm:"column:claimed_at"`
	LastEventHeight uint64    `gorm:"column:last_event_height"`
	Incomplete      bool      `gorm:"column:incomplete;default:false"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
}

func (Escrow) TableName() string {
	return "escrows"
}

func Migrate(tx *gorm.DB) error {
	return tx.AutoMigrate(&Escrow{})
}

func Rollback(tx *gorm.DB) error {
	return tx.Migrator().DropTable(&Escrow{})
}
