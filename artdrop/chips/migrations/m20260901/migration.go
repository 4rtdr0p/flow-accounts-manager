// m20260901 adds the artdrop_chips table (issue #117, phase 2): the
// wallet-api's own chipId -> custodial-account mapping. Purely additive — no
// existing table or column is touched, and nothing on-chain changes; see
// docs/CHIP-SIGNING-DESIGN.md §2.3.
//
// This migration defines its own snapshot of the row shape (rather than
// importing chips.Chip), following the same convention as
// escrow_projection/migrations/m20260829 and the purchase/studio migrations:
// a migration's struct is a point-in-time snapshot for AutoMigrate, decoupled
// from however the live package's struct evolves afterwards.
package m20260901

import (
	"time"

	"gorm.io/gorm"
)

const ID = "20260901"

// Chip is the artdrop_chips table shape as of this migration.
type Chip struct {
	ChipID            string    `gorm:"column:chip_id;primary_key"`
	AccountAddress    string    `gorm:"column:account_address;index"`
	PublicKey         string    `gorm:"column:public_key"`
	SigningMode       string    `gorm:"column:signing_mode;default:custodial"`
	RegisteredAtBlock *uint64   `gorm:"column:registered_at_block"`
	CreatedAt         time.Time `gorm:"column:created_at"`
	UpdatedAt         time.Time `gorm:"column:updated_at"`
}

func (Chip) TableName() string {
	return "artdrop_chips"
}

func Migrate(tx *gorm.DB) error {
	return tx.AutoMigrate(&Chip{})
}

func Rollback(tx *gorm.DB) error {
	return tx.Migrator().DropTable(&Chip{})
}
