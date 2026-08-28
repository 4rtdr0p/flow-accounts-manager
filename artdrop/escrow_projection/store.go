package escrow_projection

import "context"

// CreateFields is the set of columns EscrowCreated / CertificateReEscrowed
// own — see Escrow's doc comment for the full column-ownership scheme.
type CreateFields struct {
	EscrowID        uint64
	Buyer           string
	Seller          string
	EditionID       *uint64 // nil when CertificateReEscrowed couldn't resolve it locally (see EditionIDForCertificate)
	ChipID          string
	UnlockAt        string
	CertificateID   uint64
	SourceEvent     string
	LastEventHeight uint64
}

// Store is the persistence interface for the escrow projection. GormStore
// is the only production implementation; the interface exists so
// artdrop.Service and the handler/backfill tests can use a fake.
type Store interface {
	// UpsertCreated applies an EscrowCreated or CertificateReEscrowed event:
	// it writes exactly the CreateFields columns, leaving status/
	// release_reason/claimed/claimed_at untouched if the row already exists
	// (e.g. a Released/Claimed stub arrived first) and defaulting them to
	// Pending/unclaimed on first insert.
	UpsertCreated(ctx context.Context, f CreateFields) error

	// UpsertReleased applies an EscrowReleased event: writes status,
	// release_reason and last_event_height only. If no row exists yet, it
	// inserts a stub row (Incomplete=true) that UpsertCreated later
	// completes.
	UpsertReleased(ctx context.Context, escrowID uint64, status uint8, releaseReason uint8, height uint64) error

	// UpsertClaimed applies an EscrowClaimed event: writes claimed/
	// claimed_at only. Same stub-insert behavior as UpsertReleased when the
	// row doesn't exist yet.
	UpsertClaimed(ctx context.Context, escrowID uint64, claimed bool, claimedAt *string) error

	// UpsertBackfillRow inserts a full row from the one-time backfill
	// (backfill.go) if and only if no row for that escrow_id exists yet —
	// the live listener path is always authoritative, so backfill never
	// overwrites an existing row (ON CONFLICT DO NOTHING).
	UpsertBackfillRow(ctx context.Context, row Escrow) error

	// EditionIDForCertificate returns the edition id of any existing
	// projected row for certificateID (there is always at most one on
	// values seen by CertificateReEscrowed, since a certificate's edition
	// never changes across re-escrows), or nil if no such row is projected
	// yet.
	EditionIDForCertificate(ctx context.Context, certificateID uint64) (*uint64, error)

	// GetByID returns the projected row, or (nil, nil) if absent.
	GetByID(ctx context.Context, escrowID uint64) (*Escrow, error)
	ListByBuyer(ctx context.Context, buyer string) ([]Escrow, error)
	ListByEdition(ctx context.Context, editionID uint64) ([]Escrow, error)
	ListBySeller(ctx context.Context, seller string) ([]Escrow, error)

	// Count returns the total number of projected rows — used both to
	// decide whether the one-time backfill still needs to run (0 rows) and
	// by the read-swap to decide whether an empty per-filter listing result
	// can be trusted yet (see the #102 design report point 6).
	Count(ctx context.Context) (int64, error)
}
