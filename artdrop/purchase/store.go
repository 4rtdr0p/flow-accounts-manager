package purchase

import "context"

// Store defines what purchase needs from the database.
type Store interface {
	// CreatePurchaseCharge persists a purchase charge audit record.
	CreatePurchaseCharge(charge *PurchaseCharge) error
	GetPurchaseCharge(ctx context.Context, purchaseID string) (*PurchaseCharge, error)
	// ClaimEscrowOpening atomically reserves a paid obligation for one escrow
	// submission. A false result means it was already opened or is not payable.
	ClaimEscrowOpening(ctx context.Context, purchaseID, chipID string, nonce uint64, idempotencyKey string) (bool, error)
	SetEscrowJobID(ctx context.Context, purchaseID, jobID string) error
	ResetEscrowOpening(ctx context.Context, purchaseID string) error
}
