// Package chips is the wallet-api's own record of which custodial Flow
// account holds a chip's ECDSA P-256 signing key (issue #117, design doc
// docs/CHIP-SIGNING-DESIGN.md §2.3).
//
// Unlike escrow_projection, this is NOT an event-driven projection of
// on-chain state — there is no "ChipRegistered" event to listen for, and no
// chain fallback exists for it. It is written directly by
// Service.ProvisionChip once account creation AND on-chain pubkey
// registration have both succeeded, and it is the ONLY place that knows
// "chipId X's private key lives in custodial account Y" — the on-chain
// ArtDropRegistry.ChipPublicKeyIndex only ever holds the public key.
package chips

import "time"

// Chip is one chipId -> custodial account mapping — the join key described
// in the design doc's §1 diagram:
//
//	chipId (String)
//	  ├── wallet-api:   chipId -> chip custodial Flow account -> P-256 private key (custody)
//	  ├── on-chain:     chipId -> ArtDropRegistry.ChipPublicKeyIndex -> chipPubKey
//	  └── Ixkio:        tap -> xuid -> chipId
type Chip struct {
	// ChipID is the String join key used everywhere else in the system
	// (Escrow.chipId, ArtDropRegistry.ChipPublicKeyIndex's key).
	ChipID string `gorm:"column:chip_id;primaryKey"`

	// AccountAddress is the chip's custodial Flow account — a normal
	// accounts.Account row whose stored key IS the chip's private key
	// (custodied exactly like any other account key, in storable_keys).
	AccountAddress string `gorm:"column:account_address;index"`

	// PublicKey is the chip's on-chain identity: the 64-byte raw ECDSA
	// P-256 public key (x||y, no 0x04 prefix), hex-encoded without a "0x"
	// prefix. Denormalized for convenience — the source of truth is the
	// account's own stored key — so a chip lookup never needs a join
	// against storable_keys.
	PublicKey string `gorm:"column:public_key"`

	// SigningMode selects who signs on this chip's behalf: "custodial"
	// (wallet-api signs, Ixkio-gated — model B, the only mode this phase
	// provisions) or "self" (a future asymmetric self-signing chip, design
	// doc §7 — not wired anywhere yet). Always "custodial" today.
	SigningMode string `gorm:"column:signing_mode;default:custodial"`

	// RegisteredAtBlock is the block height the confirming
	// registerChipPublicKey/replaceChipPublicKey transaction sealed at.
	// Nullable: this phase does not yet extract it from the transaction
	// result, so it is left unset (nil) at provisioning time.
	RegisteredAtBlock *uint64 `gorm:"column:registered_at_block"`

	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

// TableName pins the table name explicitly, following the same convention
// as escrow_projection.Escrow / chain_events.ListenerStatus.
func (Chip) TableName() string {
	return "artdrop_chips"
}
