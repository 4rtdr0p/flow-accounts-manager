package studio

import (
	"time"

	"gorm.io/gorm"
)

// GormStore is the GORM-backed implementation of Store.
type GormStore struct {
	db *gorm.DB
}

// NewGormStore creates a new GORM-backed Store.
func NewGormStore(db *gorm.DB) Store {
	return &GormStore{db}
}

// CreateProductionCharge persists a charge audit record.
func (s *GormStore) CreateProductionCharge(charge *ProductionCharge) error {
	if charge.CreatedAt.IsZero() {
		charge.CreatedAt = time.Now().UTC()
	}
	return s.db.Create(charge).Error
}

// ListProductionChargesByUser returns the audit records for a user, most
// recent first.
func (s *GormStore) ListProductionChargesByUser(userID string) ([]ProductionCharge, error) {
	var charges []ProductionCharge
	err := s.db.
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Find(&charges).Error
	return charges, err
}
