package purchase

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

// CreatePurchaseCharge persists a purchase charge audit record.
func (s *GormStore) CreatePurchaseCharge(charge *PurchaseCharge) error {
	if charge.CreatedAt.IsZero() {
		charge.CreatedAt = time.Now().UTC()
	}
	return s.db.Create(charge).Error
}
