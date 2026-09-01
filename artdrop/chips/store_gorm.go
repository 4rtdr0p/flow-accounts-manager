package chips

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormStore is the Postgres-backed (also sqlite-compatible, for tests)
// implementation of Store.
type GormStore struct {
	db *gorm.DB
}

func NewGormStore(db *gorm.DB) *GormStore {
	return &GormStore{db: db}
}

var upsertColumns = []string{
	"account_address", "public_key", "signing_mode", "registered_at_block", "updated_at",
}

func (s *GormStore) UpsertChip(ctx context.Context, c Chip) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "chip_id"}},
		DoUpdates: clause.AssignmentColumns(upsertColumns),
	}).Create(&c).Error
}

func (s *GormStore) GetChip(ctx context.Context, chipID string) (*Chip, error) {
	var row Chip
	err := s.db.WithContext(ctx).Where("chip_id = ?", chipID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}
