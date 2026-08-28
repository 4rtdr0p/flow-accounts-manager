package escrow_projection

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

// createColumns is the DoUpdates column list for UpsertCreated — see
// Escrow's doc comment for why this is scoped to exactly the columns
// EscrowCreated/CertificateReEscrowed own, never status/release_reason/
// claimed/claimed_at.
var createColumns = []string{
	"buyer", "seller", "edition_id", "chip_id", "unlock_at",
	"certificate_id", "source_event", "last_event_height", "updated_at", "incomplete",
}

func (s *GormStore) UpsertCreated(ctx context.Context, f CreateFields) error {
	row := Escrow{
		EscrowID:        f.EscrowID,
		Buyer:           f.Buyer,
		Seller:          f.Seller,
		EditionID:       f.EditionID,
		ChipID:          f.ChipID,
		UnlockAt:        f.UnlockAt,
		CertificateID:   f.CertificateID,
		SourceEvent:     f.SourceEvent,
		LastEventHeight: f.LastEventHeight,
		// Incomplete tracks whether EditionID could be resolved — always
		// true (known) for EscrowCreated (the event carries editionId
		// directly), only sometimes unresolved for CertificateReEscrowed
		// (see EditionIDForCertificate). Status/Claimed are left at their Go
		// zero values (0=Pending, false) — correct defaults for a brand new
		// row, and never touched by this upsert's DoUpdates on conflict, so
		// an existing Released/Claimed stub is preserved.
		Incomplete: f.EditionID == nil,
	}

	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "escrow_id"}},
		DoUpdates: clause.AssignmentColumns(createColumns),
	}).Create(&row).Error
}

func (s *GormStore) UpsertReleased(ctx context.Context, escrowID uint64, status uint8, releaseReason uint8, height uint64) error {
	row := Escrow{
		EscrowID:        escrowID,
		Status:          status,
		ReleaseReason:   &releaseReason,
		LastEventHeight: height,
		// Incomplete=true only takes effect on INSERT (no prior row, i.e.
		// this Released event beat its own Created/CertificateReEscrowed) —
		// "incomplete" is deliberately absent from DoUpdates below, so an
		// existing row's value (already false once Created has landed) is
		// never touched here.
		Incomplete: true,
	}

	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "escrow_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"status", "release_reason", "last_event_height", "updated_at"}),
	}).Create(&row).Error
}

func (s *GormStore) UpsertClaimed(ctx context.Context, escrowID uint64, claimed bool, claimedAt *string) error {
	row := Escrow{
		EscrowID:  escrowID,
		Claimed:   claimed,
		ClaimedAt: claimedAt,
		// See UpsertReleased — same stub-only-on-insert reasoning.
		Incomplete: true,
	}

	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "escrow_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"claimed", "claimed_at", "updated_at"}),
	}).Create(&row).Error
}

// UpsertBackfillRow inserts row only if no row for its EscrowID exists yet
// — DoNothing on conflict, since the live listener is always authoritative
// once it has seen an id (see Store.UpsertBackfillRow's doc comment).
func (s *GormStore) UpsertBackfillRow(ctx context.Context, row Escrow) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "escrow_id"}},
		DoNothing: true,
	}).Create(&row).Error
}

func (s *GormStore) EditionIDForCertificate(ctx context.Context, certificateID uint64) (*uint64, error) {
	var row Escrow
	err := s.db.WithContext(ctx).
		Where("certificate_id = ? AND edition_id IS NOT NULL", certificateID).
		Order("escrow_id ASC").
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row.EditionID, nil
}

func (s *GormStore) GetByID(ctx context.Context, escrowID uint64) (*Escrow, error) {
	var row Escrow
	err := s.db.WithContext(ctx).Where("escrow_id = ?", escrowID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *GormStore) ListByBuyer(ctx context.Context, buyer string) ([]Escrow, error) {
	var rows []Escrow
	err := s.db.WithContext(ctx).Where("buyer = ?", buyer).Order("escrow_id ASC").Find(&rows).Error
	return rows, err
}

func (s *GormStore) ListByEdition(ctx context.Context, editionID uint64) ([]Escrow, error) {
	var rows []Escrow
	err := s.db.WithContext(ctx).Where("edition_id = ?", editionID).Order("escrow_id ASC").Find(&rows).Error
	return rows, err
}

func (s *GormStore) ListBySeller(ctx context.Context, seller string) ([]Escrow, error) {
	var rows []Escrow
	err := s.db.WithContext(ctx).Where("seller = ?", seller).Order("escrow_id ASC").Find(&rows).Error
	return rows, err
}

func (s *GormStore) Count(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&Escrow{}).Count(&count).Error
	return count, err
}
