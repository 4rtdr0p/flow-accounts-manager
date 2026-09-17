package purchase

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// GormStore is the GORM-backed implementation of Store.
type GormStore struct {
	db *gorm.DB
}

func (s *GormStore) GetPurchaseCharge(ctx context.Context, purchaseID string) (*PurchaseCharge, error) {
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

// NewGormStore creates a new GORM-backed Store.
func NewGormStore(db *gorm.DB) Store {
	return &GormStore{db}
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
