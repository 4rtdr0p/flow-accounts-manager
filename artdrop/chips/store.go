package chips

import "context"

// Store is the persistence interface for chip provisioning records.
// GormStore is the only production implementation; the interface exists so
// artdrop.Service's tests can use a fake, following escrow_projection.Store's
// convention.
type Store interface {
	// UpsertChip inserts a chip's mapping, keyed by ChipID. Provisioning
	// (Service.ProvisionChip) only ever inserts a brand new chipId — it
	// checks GetChip first and short-circuits on an existing row — but this
	// is still an upsert (ON CONFLICT DO UPDATE), not a bare insert, so a
	// future re-key path can reuse it without a second method.
	UpsertChip(ctx context.Context, c Chip) error

	// GetChip returns the chip's mapping, or (nil, nil) if chipId has not
	// been provisioned.
	GetChip(ctx context.Context, chipID string) (*Chip, error)
}
