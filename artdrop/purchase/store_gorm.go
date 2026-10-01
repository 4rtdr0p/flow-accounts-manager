package purchase

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// GormStore is the GORM-backed implementation of Store.
type GormStore struct {
	db         *gorm.DB
	durability DurabilityOptions
}

func (s *GormStore) GetPurchaseCharge(ctx context.Context, purchaseID string) (*PurchaseCharge, error) {
	if s.db == nil {
		return nil, ErrChargeRecordFailed
	}
	var charge PurchaseCharge
	err := s.db.WithContext(ctx).Where("purchase_id = ?", purchaseID).First(&charge).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &charge, nil
}

func (s *GormStore) ClaimEscrowOpening(ctx context.Context, purchaseID, chipID string, nonce uint64, idempotencyKey string) (bool, error) {
	result := s.db.WithContext(ctx).Model(&PurchaseCharge{}).
		Where("purchase_id = ? AND status = ? AND (escrow_job_id = '' OR escrow_job_id IS NULL)", purchaseID, PurchaseStatusPaidPendingEscrow).
		Updates(map[string]interface{}{"status": PurchaseStatusEscrowOpening, "chip_id": chipID, "nonce": nonce, "escrow_idempotency_key": idempotencyKey, "updated_at": time.Now().UTC()})
	return result.RowsAffected == 1, result.Error
}

func (s *GormStore) SetEscrowJobID(ctx context.Context, purchaseID, jobID string) error {
	return s.db.WithContext(ctx).Model(&PurchaseCharge{}).Where("purchase_id = ? AND status = ?", purchaseID, PurchaseStatusEscrowOpening).
		Updates(map[string]interface{}{"escrow_job_id": jobID, "updated_at": time.Now().UTC()}).Error
}

func (s *GormStore) ResetEscrowOpening(ctx context.Context, purchaseID string) error {
	return s.db.WithContext(ctx).Model(&PurchaseCharge{}).Where("purchase_id = ? AND status = ? AND (escrow_job_id = '' OR escrow_job_id IS NULL)", purchaseID, PurchaseStatusEscrowOpening).
		Updates(map[string]interface{}{"status": PurchaseStatusPaidPendingEscrow, "escrow_idempotency_key": "", "updated_at": time.Now().UTC()}).Error
}

// ResetEscrowOpeningAfterFailedJob releases a claim held by a terminally
// failed wallet job. See Store.ResetEscrowOpeningAfterFailedJob for the
// contract; the job-id predicate is what makes concurrent retries safe — the
// second caller's UPDATE matches zero rows once the first has re-opened.
// chip_id and nonce are deliberately left in place: the next
// ClaimEscrowOpening overwrites both, and a crash between reset and re-claim
// must leave an ordinary PAID_PENDING_ESCROW row, not a half-cleared one.
func (s *GormStore) ResetEscrowOpeningAfterFailedJob(ctx context.Context, purchaseID, failedJobID string) (bool, error) {
	result := s.db.WithContext(ctx).Model(&PurchaseCharge{}).
		Where("purchase_id = ? AND status = ? AND escrow_job_id = ?", purchaseID, PurchaseStatusEscrowOpening, failedJobID).
		Updates(map[string]interface{}{"status": PurchaseStatusPaidPendingEscrow, "escrow_job_id": "", "escrow_idempotency_key": "", "updated_at": time.Now().UTC()})
	return result.RowsAffected == 1, result.Error
}

// NewGormStore creates a new GORM-backed Store.
func NewGormStore(db *gorm.DB, options ...DurabilityOptions) Store {
	o := DefaultDurabilityOptions()
	if len(options) > 0 {
		o = options[0]
	}
	return &GormStore{db: db, durability: o}
}

// CreatePurchaseCharge persists a purchase charge audit record.
func (s *GormStore) CreatePurchaseCharge(charge *PurchaseCharge) error {
	if charge.CreatedAt.IsZero() {
		charge.CreatedAt = time.Now().UTC()
	}
	if charge.UpdatedAt.IsZero() {
		charge.UpdatedAt = charge.CreatedAt
	}
	return s.db.Create(charge).Error
}
