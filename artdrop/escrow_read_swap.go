package artdrop

import (
	"context"
	"fmt"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop/escrow_projection"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	"github.com/onflow/cadence"
	log "github.com/sirupsen/logrus"
)

// escrowBackfillChunkSize bounds how many escrow ids BackfillEscrowProjection
// asks get_all_escrow_summaries.cdc to resolve in one script execution. Not
// load-bearing at today's scale (testnet has ~17 escrows total) — this
// exists so backfill stays correct once that stops being true, without
// needing a second script pass.
const escrowBackfillChunkSize = 500

// getEscrowFromProjection is GetEscrow's projection-first read (issue
// #102). It returns (nil, false) — never an error — whenever the
// projection can't serve this id right now, for any reason: no store
// wired, the lookup errored, the row hasn't been projected yet, or the row
// is Incomplete (see escrow_projection.Escrow's doc comment). The caller
// (GetEscrow) always has the chain fallback for those cases.
func (s *Service) getEscrowFromProjection(ctx context.Context, escrowId uint64) (*EscrowSummary, bool) {
	if s.escrowStore == nil {
		return nil, false
	}

	row, err := s.escrowStore.GetByID(ctx, escrowId)
	if err != nil {
		log.WithError(err).WithField("escrowId", escrowId).Warn("escrow projection: lookup failed, falling back to chain")
		return nil, false
	}
	if row == nil || row.Incomplete {
		return nil, false
	}

	return projectionRowToSummary(row), true
}

// listEscrowsFromProjection is the shared projection-first path for
// ListEscrowsByBuyer/ByEdition/BySeller. fetchRows is the caller's
// store.ListBy*(ctx, ...) call.
//
// Consistency note: an empty per-filter result from the projection is only
// trustworthy once the projection as a whole has seen at least one row —
// Count()==0 doesn't distinguish "this buyer/edition/seller genuinely has
// no escrows" from "the projection hasn't been backfilled or hasn't
// processed any events yet", so that case (and any store error) falls back
// to the chain for the WHOLE request, not just this filter. This mirrors
// the same ambiguity the pre-#102 chain scripts already had for a
// not-yet-published index (see get_escrows_by_buyer.cdc's doc comment) —
// #102 doesn't make that ambiguity worse, it only adds one more condition
// (an empty projection) that resolves it the same way: fall back.
//
// If any matching row is Incomplete, the whole request falls back too,
// rather than silently omitting that row or serving a partial summary for
// it — callers of these list endpoints get either a fully trustworthy
// projection answer or the authoritative chain answer, never a mix.
func (s *Service) listEscrowsFromProjection(ctx context.Context, expand bool, fetchRows func() ([]escrow_projection.Escrow, error)) (*EscrowListResponse, bool) {
	if s.escrowStore == nil {
		return nil, false
	}

	count, err := s.escrowStore.Count(ctx)
	if err != nil {
		log.WithError(err).Warn("escrow projection: readiness check failed, falling back to chain")
		return nil, false
	}
	if count == 0 {
		return nil, false
	}

	rows, err := fetchRows()
	if err != nil {
		log.WithError(err).Warn("escrow projection: list query failed, falling back to chain")
		return nil, false
	}

	for _, row := range rows {
		if row.Incomplete {
			return nil, false
		}
	}

	ids := make([]uint64, 0, len(rows))
	var summaries []EscrowSummary
	if expand {
		summaries = make([]EscrowSummary, 0, len(rows))
	}
	for _, row := range rows {
		row := row
		ids = append(ids, row.EscrowID)
		if expand {
			summaries = append(summaries, *projectionRowToSummary(&row))
		}
	}

	return &EscrowListResponse{EscrowIds: ids, Escrows: summaries}, true
}

// projectionRowToSummary converts a projected row into the same
// EscrowSummary shape the chain-backed path returns (issue #100's type,
// see types.go) — the read-swap is transparent to every caller, same JSON
// response either way.
//
// Nonce is always left at its zero value: none of the four escrow
// lifecycle events carries nonce (see escrow_projection/handler.go's event
// catalog doc comment), so the projection has no source for it. A caller
// that needs the real nonce value is not served by the projection path —
// this is a disclosed, accepted gap (see the #102 design report point 1),
// not a silent data error.
func projectionRowToSummary(row *escrow_projection.Escrow) *EscrowSummary {
	summary := &EscrowSummary{
		Id:            row.EscrowID,
		Buyer:         row.Buyer,
		Seller:        row.Seller,
		ChipId:        row.ChipID,
		UnlockAt:      row.UnlockAt,
		CertificateId: row.CertificateID,
		Status:        row.Status,
		ReleaseReason: row.ReleaseReason,
		Claimed:       row.Claimed,
		ClaimedAt:     row.ClaimedAt,
	}
	if row.EditionID != nil {
		summary.EditionId = *row.EditionID
	}
	return summary
}

// BackfillEscrowProjection populates the escrows projection from current
// on-chain state, one time, self-healing: it is a no-op unless the
// projection table is still completely empty, so it is safe to call on
// every process boot (see main.go) — it only ever does real work the very
// first time it finds an empty table. Returns the number of rows written.
//
// It sizes the id range with get_total_escrows.cdc, then walks it in
// escrowBackfillChunkSize chunks via get_all_escrow_summaries.cdc, reusing
// decodeEscrowSummaryArray — the same decoder the #100 combined scripts
// already use, since this script returns the identical flat-dictionary
// shape. Every row is written with escrow_projection.Store.UpsertBackfillRow,
// which never overwrites a row the live listener has already written, so
// this is safe to run concurrently with (or after) the listener starting.
func (s *Service) BackfillEscrowProjection(ctx context.Context) (int, error) {
	if s.escrowStore == nil {
		return 0, nil
	}

	count, err := s.escrowStore.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("check escrow projection readiness: %w", err)
	}
	if count > 0 {
		return 0, nil
	}

	totalVal, err := s.deps.Transactions.ExecuteScript(ctx, s.getTotalEscrowsCDC, nil)
	if err != nil {
		return 0, fmt.Errorf("execute get_total_escrows script: %w", err)
	}
	total, ok := totalVal.(cadence.UInt64)
	if !ok {
		return 0, fmt.Errorf("unexpected script result type %T, expected cadence.UInt64", totalVal)
	}

	written := 0
	for start := uint64(1); start <= uint64(total); start += escrowBackfillChunkSize {
		end := start + escrowBackfillChunkSize - 1
		if end > uint64(total) {
			end = uint64(total)
		}

		args := []transactions.Argument{
			cadence.NewUInt64(start),
			cadence.NewUInt64(end),
		}
		val, err := s.deps.Transactions.ExecuteScript(ctx, s.getAllEscrowSummariesCDC, args)
		if err != nil {
			return written, fmt.Errorf("execute get_all_escrow_summaries script (ids %d-%d): %w", start, end, err)
		}

		summaries, err := decodeEscrowSummaryArray(val)
		if err != nil {
			return written, fmt.Errorf("decode get_all_escrow_summaries result (ids %d-%d): %w", start, end, err)
		}

		for _, summary := range summaries {
			if err := s.escrowStore.UpsertBackfillRow(ctx, escrowSummaryToBackfillRow(summary)); err != nil {
				return written, fmt.Errorf("upsert backfilled escrow %d: %w", summary.Id, err)
			}
			written++
		}
	}

	log.WithFields(log.Fields{"total": total, "written": written}).Info("escrow projection: backfill complete")
	return written, nil
}

// escrowSummaryToBackfillRow converts a chain-decoded EscrowSummary into a
// full, complete projection row. Backfill always has the authoritative
// editionId — ArtDropCore.getEscrowSummary resolves it from the Escrow
// resource itself, not from an event — so Incomplete is always false here,
// unlike the live CertificateReEscrowed gap (see handler.go).
func escrowSummaryToBackfillRow(summary EscrowSummary) escrow_projection.Escrow {
	editionId := summary.EditionId
	return escrow_projection.Escrow{
		EscrowID:      summary.Id,
		Buyer:         summary.Buyer,
		Seller:        summary.Seller,
		EditionID:     &editionId,
		ChipID:        summary.ChipId,
		UnlockAt:      summary.UnlockAt,
		CertificateID: summary.CertificateId,
		SourceEvent:   "backfill",
		Status:        summary.Status,
		ReleaseReason: summary.ReleaseReason,
		Claimed:       summary.Claimed,
		ClaimedAt:     summary.ClaimedAt,
		Incomplete:    false,
	}
}
