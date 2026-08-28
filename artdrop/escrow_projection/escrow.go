// Package escrow_projection is a local Postgres read-model of ArtDropCore
// escrows (issue #102), kept in sync off the shared chain_events listener
// instead of reading Flow on every GetEscrow/ListEscrowsBy* request.
//
// Design background (see the #102 design report for the full rationale,
// citations and alternatives considered): reading escrow state via
// ExecuteScript against the public testnet access node rate-limits under
// concurrent load on distinct ids/editions/buyers — issue #100 already
// collapsed each listing's N+1 into a single combined script and added a
// TTL cache for repeat single-id lookups, but neither helps when 20
// concurrent users are each asking about a DIFFERENT escrow: every one of
// those is still a fresh access-node round trip. This package removes that
// per-request chain read entirely for the steady-state case by projecting
// the four escrow lifecycle events into a local table, with the existing
// #100 scripts kept as the fallback path for whatever the projection
// hasn't caught up to yet (see artdrop/service.go's read-swap).
package escrow_projection

import "time"

// Escrow is the local read-model projection of one on-chain ArtDropCore
// escrow (ArtDropCore.EscrowSummary — see artdrop-protocol/contracts/core/
// ArtDropCore.cdc lines ~1315-1356).
//
// COLUMN OWNERSHIP: each column group below is written by exactly one
// on-chain event, and only that event's handler (see handler.go) is
// allowed to touch it in the upsert it issues. This is what makes the
// projection idempotent and robust to out-of-order delivery without a
// staging table or a monotonic-id guard per row: the current
// chain_events dispatch mechanism gives no ordering guarantee at all
// between event types processed in the same poll batch (event.go's
// Trigger spawns one goroutine per event; listener.go's run() fetches and
// concatenates events type-by-type, not by block height) — so a
// Released/Claimed event for an escrow can genuinely arrive before that
// escrow's own Created event. Every writer here is also a strict "at most
// once per escrow" event on-chain (enforced by the contract's own
// preconditions — see EscrowCreated/CertificateReEscrowed being the sole
// originators of an id, and Escrow.markReleased/markClaimed's
// status==Pending / !claimed preconditions), so within a column group
// there is never a real write race, only the "child arrived before
// parent" ordering hazard — which the Incomplete flag and the handler's
// scoped ON CONFLICT DO UPDATE column lists resolve; see handler.go.
type Escrow struct {
	// EscrowID is ArtDropCore.Escrow.id — the primary key. Never reused
	// on-chain (assigned as ArtDropCore.totalEscrows+1), so no soft-delete
	// or tombstone concern.
	EscrowID uint64 `gorm:"column:escrow_id;primaryKey;autoIncrement:false"`

	// --- owned by EscrowCreated / CertificateReEscrowed (create-time, set once) ---

	Buyer  string `gorm:"column:buyer;size:18;index:idx_escrows_buyer"`
	Seller string `gorm:"column:seller;size:18;index:idx_escrows_seller"`
	// EditionID is nullable: CertificateReEscrowed does not carry editionId
	// in its event payload (see ArtDropCore.cdc's CertificateReEscrowed
	// event vs. createReEscrow reading it off the certificate itself). The
	// handler resolves it locally by joining on CertificateID against any
	// prior row for the same certificate — see
	// GormStore.EditionIDForCertificate — and only falls back to NULL (with
	// Incomplete=true) in the vanishingly rare case where that prior row
	// hasn't been projected yet either.
	EditionID *uint64 `gorm:"column:edition_id;index:idx_escrows_edition"`
	ChipID    string  `gorm:"column:chip_id;index:idx_escrows_chip"`
	// UnlockAt is the UFix64 unlock timestamp rendered as a decimal string,
	// matching EscrowSummary.UnlockAt's convention in artdrop/types.go
	// (every UFix64 field in the API is a string, not a float, to avoid
	// precision loss).
	UnlockAt      string `gorm:"column:unlock_at"`
	CertificateID uint64 `gorm:"column:certificate_id;index:idx_escrows_certificate"`
	// SourceEvent records which event created this row ("EscrowCreated" or
	// "CertificateReEscrowed") — debugging/traceability only, not read by
	// any query.
	SourceEvent string `gorm:"column:source_event;size:24"`

	// --- owned exclusively by EscrowReleased ---

	// Status mirrors ArtDropCore.EscrowStatus's rawValue: 0=Pending,
	// 1=Released. Defaults to 0 (Pending) for a row inserted by
	// EscrowCreated/CertificateReEscrowed, which is always correct: an
	// escrow is always Pending at creation.
	Status uint8 `gorm:"column:status;default:0"`
	// ReleaseReason mirrors ArtDropCore.ReleaseReason's rawValue
	// (0=ClaimedByBuyer, 1=WindowExpired), nil while Pending.
	ReleaseReason *uint8 `gorm:"column:release_reason"`

	// --- owned exclusively by EscrowClaimed ---

	Claimed bool `gorm:"column:claimed;default:false"`
	// ClaimedAt: EscrowClaimed carries no envelope and no timestamp field
	// (see ArtDropCore.cdc's EscrowClaimed event — the one escrow event
	// without an ArtDropEventEnvelope), so the live listener path sets
	// this to the event-processing wall-clock time, NOT the real on-chain
	// claim timestamp — a disclosed, accepted gap (see the #102 design
	// report point 3). The one-time backfill (backfill.go, run from
	// artdrop.Service) DOES get the authoritative on-chain value via
	// ArtDropCore.getEscrowSummary, so historical rows populated by
	// backfill are exact; only claims processed live afterwards carry the
	// approximation.
	ClaimedAt *string `gorm:"column:claimed_at"`

	// --- projection bookkeeping — not owned by any single on-chain event ---

	// LastEventHeight is the block height (from the event's
	// ArtDropEventEnvelope, when present) of the most recent
	// EscrowCreated/CertificateReEscrowed/EscrowReleased event applied to
	// this row. Observability only (staleness/consistency reporting, see
	// the #102 design report point 6) — never used as an ordering guard,
	// since EscrowClaimed carries no height and every writer is at-most-
	// once per escrow already (see the type doc comment above). Left at 0
	// (its Go zero value) for rows seeded purely by backfill, meaning "not
	// yet confirmed by the live listener".
	LastEventHeight uint64 `gorm:"column:last_event_height"`
	// Incomplete is true while some create-time field is still unknown:
	// either the row was stub-inserted by EscrowReleased/EscrowClaimed
	// before its EscrowCreated/CertificateReEscrowed arrived, or (rarer) a
	// CertificateReEscrowed landed before EditionID could be resolved.
	// Consumers should not trust a row with Incomplete=true as a full
	// EscrowSummary — the read-swap in artdrop/service.go falls back to
	// the on-chain script for it instead of serving it as-is.
	Incomplete bool      `gorm:"column:incomplete;default:false"`
	UpdatedAt  time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

// TableName pins the table name explicitly, following the same convention
// as chain_events.ListenerStatus / tokens.AccountToken.
func (Escrow) TableName() string {
	return "escrows"
}
