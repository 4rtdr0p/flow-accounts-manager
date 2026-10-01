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
	// ResetEscrowOpeningAfterFailedJob releases an escrow claim whose wallet
	// job terminally FAILED (atomic on-chain revert — no escrow or certificate
	// was created). Conditional on the purchase still holding exactly
	// failedJobID, so a concurrent fresh claim can never be cleared; a false
	// result means the row moved on and the caller must not re-open.
	ResetEscrowOpeningAfterFailedJob(ctx context.Context, purchaseID, failedJobID string) (bool, error)
}
