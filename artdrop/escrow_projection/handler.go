package escrow_projection

import (
	"context"
	"strings"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/flow_helpers"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	log "github.com/sirupsen/logrus"
)

// Event catalog (see artdrop-protocol/contracts/core/ArtDropCore.cdc for the
// authoritative definitions):
//
//   - EscrowCreated (~L2140-2149): envelope, escrowId, buyer, seller,
//     editionId, chipId, unlockAt, certificateId.
//   - CertificateReEscrowed (~L2165-2173): envelope, escrowId,
//     certificateId, chipId, buyer, seller, unlockAt. No editionId — see
//     EditionIDForCertificate.
//   - EscrowReleased (~L2177-2182): envelope, escrowId, releaser,
//     releaseReason.
//   - EscrowClaimed (~L2153-2157): escrowId, certificateId, claimant. The
//     only one of the four with NO envelope — no blockHeight, no
//     timestamp. See Escrow.ClaimedAt's doc comment for how that gap is
//     handled.
//
// Deliberately NOT subscribed: EscrowSettled / EscrowForceRefunded /
// EscrowForceCancelled, defined in artdrop-protocol/contracts/core/
// ArtDropEvents.cdc §§6-7 but never emitted by any live code path — the
// Phase 3 escrow redesign removed buyer-initiated cancel/refund and the
// admin force-refund/force-cancel overrides (see EscrowModule.cdc's
// top-of-file note and docs/escrow-redesign-plan.md). Subscribing to them
// would only add GetEventsForHeightRange calls per poll tick with no
// events ever returned.
const (
	eventSuffixEscrowCreated         = ".ArtDropCore.EscrowCreated"
	eventSuffixCertificateReEscrowed = ".ArtDropCore.CertificateReEscrowed"
	eventSuffixEscrowReleased        = ".ArtDropCore.EscrowReleased"
	eventSuffixEscrowClaimed         = ".ArtDropCore.EscrowClaimed"
)

// EventTypes returns the fully-qualified Flow event types the chain_events
// listener must subscribe to for this projection, given the address
// ArtDropCore is deployed to (with or without a leading "0x"). Meant to be
// appended to the existing getTypes() closure in main.go alongside the
// token deposit event types — see the #102 design report point 4 for why
// this is safe to share the same listener/cursor as tokens.
func EventTypes(coreAddress string) []string {
	addr := strings.TrimPrefix(coreAddress, "0x")
	qualify := func(suffix string) string {
		// suffix already starts with ".ArtDropCore...."; the qualified type
		// is "A.<address>" + suffix, matching templates.EventType's
		// "A.%s.%s.%s" shape (templates/events.go) without importing that
		// package just for one format string.
		return "A." + addr + suffix
	}
	return []string{
		qualify(eventSuffixEscrowCreated),
		qualify(eventSuffixCertificateReEscrowed),
		qualify(eventSuffixEscrowReleased),
		qualify(eventSuffixEscrowClaimed),
	}
}

// ArtDropEscrowEventHandler is the chain_events handler that keeps the
// escrows projection in sync — the escrow-projection analogue of
// tokens.ChainEventHandler (tokens/chain_events.go). Register it with
// chain_events.ChainEvent.Register alongside the existing tokens handler.
//
// It satisfies chain_events' unexported chainEventHandler interface
// structurally (Handle(context.Context, flow.Event)) — no import of the
// chain_events package is needed here, same as this package needs no
// import of artdrop itself.
type ArtDropEscrowEventHandler struct {
	Store Store
}

func (h *ArtDropEscrowEventHandler) Handle(ctx context.Context, event flow.Event) {
	switch {
	case strings.HasSuffix(event.Type, eventSuffixEscrowCreated):
		h.handleCreated(ctx, event)
	case strings.HasSuffix(event.Type, eventSuffixCertificateReEscrowed):
		h.handleReEscrowed(ctx, event)
	case strings.HasSuffix(event.Type, eventSuffixEscrowReleased):
		h.handleReleased(ctx, event)
	case strings.HasSuffix(event.Type, eventSuffixEscrowClaimed):
		h.handleClaimed(ctx, event)
	}
}

func (h *ArtDropEscrowEventHandler) handleCreated(ctx context.Context, event flow.Event) {
	escrowID, ok := fieldUInt64(event.Value.SearchFieldByName("escrowId"))
	if !ok {
		log.WithField("eventType", event.Type).Warn("escrow_projection: EscrowCreated missing escrowId")
		return
	}
	buyer, _ := fieldAddressHex(event.Value.SearchFieldByName("buyer"))
	seller, _ := fieldAddressHex(event.Value.SearchFieldByName("seller"))
	editionID, editionOK := fieldUInt64(event.Value.SearchFieldByName("editionId"))
	chipID, _ := fieldString(event.Value.SearchFieldByName("chipId"))
	unlockAt, _ := fieldUFix64String(event.Value.SearchFieldByName("unlockAt"))
	certificateID, _ := fieldUInt64(event.Value.SearchFieldByName("certificateId"))

	f := CreateFields{
		EscrowID:        escrowID,
		Buyer:           buyer,
		Seller:          seller,
		ChipID:          chipID,
		UnlockAt:        unlockAt,
		CertificateID:   certificateID,
		SourceEvent:     "EscrowCreated",
		LastEventHeight: envelopeBlockHeight(event),
	}
	if editionOK {
		f.EditionID = &editionID
	}

	if err := h.Store.UpsertCreated(ctx, f); err != nil {
		log.WithError(err).WithField("escrowId", escrowID).Warn("escrow_projection: failed to apply EscrowCreated")
	}
}

// handleReEscrowed applies CertificateReEscrowed. Unlike EscrowCreated,
// this event carries no editionId (createReEscrow reads it off the
// certificate itself rather than accepting it as a parameter — see
// ArtDropCore.cdc's createReEscrow doc comment). Since createReEscrow
// requires the certificate to have already been minted by a prior
// EscrowCreated (ArtDropCore.cdc's certificateId existence check), that
// prior escrow's row — already carrying the real editionId — is normally
// already projected; EditionIDForCertificate resolves it with a local
// join, no chain call needed. In the rare case that row isn't projected
// yet either, EditionID is left nil and the row is marked Incomplete.
func (h *ArtDropEscrowEventHandler) handleReEscrowed(ctx context.Context, event flow.Event) {
	escrowID, ok := fieldUInt64(event.Value.SearchFieldByName("escrowId"))
	if !ok {
		log.WithField("eventType", event.Type).Warn("escrow_projection: CertificateReEscrowed missing escrowId")
		return
	}
	certificateID, _ := fieldUInt64(event.Value.SearchFieldByName("certificateId"))
	chipID, _ := fieldString(event.Value.SearchFieldByName("chipId"))
	buyer, _ := fieldAddressHex(event.Value.SearchFieldByName("buyer"))
	seller, _ := fieldAddressHex(event.Value.SearchFieldByName("seller"))
	unlockAt, _ := fieldUFix64String(event.Value.SearchFieldByName("unlockAt"))

	editionID, err := h.Store.EditionIDForCertificate(ctx, certificateID)
	if err != nil {
		log.WithError(err).WithField("certificateId", certificateID).Warn("escrow_projection: edition lookup for re-escrow failed")
	}

	f := CreateFields{
		EscrowID:        escrowID,
		Buyer:           buyer,
		Seller:          seller,
		EditionID:       editionID,
		ChipID:          chipID,
		UnlockAt:        unlockAt,
		CertificateID:   certificateID,
		SourceEvent:     "CertificateReEscrowed",
		LastEventHeight: envelopeBlockHeight(event),
	}

	if err := h.Store.UpsertCreated(ctx, f); err != nil {
		log.WithError(err).WithField("escrowId", escrowID).Warn("escrow_projection: failed to apply CertificateReEscrowed")
	}
}

func (h *ArtDropEscrowEventHandler) handleReleased(ctx context.Context, event flow.Event) {
	escrowID, ok := fieldUInt64(event.Value.SearchFieldByName("escrowId"))
	if !ok {
		log.WithField("eventType", event.Type).Warn("escrow_projection: EscrowReleased missing escrowId")
		return
	}
	releaseReason, _ := fieldUInt8(event.Value.SearchFieldByName("releaseReason"))
	const statusReleased = uint8(1) // ArtDropCore.EscrowStatus.Released.rawValue

	if err := h.Store.UpsertReleased(ctx, escrowID, statusReleased, releaseReason, envelopeBlockHeight(event)); err != nil {
		log.WithError(err).WithField("escrowId", escrowID).Warn("escrow_projection: failed to apply EscrowReleased")
	}
}

// handleClaimed applies EscrowClaimed. This event has no envelope and no
// timestamp field (see the event catalog doc comment above), so ClaimedAt
// is set to the event-processing wall-clock time rather than the real
// on-chain claim timestamp — a disclosed, accepted approximation (see
// Escrow.ClaimedAt's doc comment). The one-time backfill gets the exact
// value from ArtDropCore.getEscrowSummary for historical claims.
func (h *ArtDropEscrowEventHandler) handleClaimed(ctx context.Context, event flow.Event) {
	escrowID, ok := fieldUInt64(event.Value.SearchFieldByName("escrowId"))
	if !ok {
		log.WithField("eventType", event.Type).Warn("escrow_projection: EscrowClaimed missing escrowId")
		return
	}
	claimedAt := time.Now().UTC().Format(time.RFC3339)

	if err := h.Store.UpsertClaimed(ctx, escrowID, true, &claimedAt); err != nil {
		log.WithError(err).WithField("escrowId", escrowID).Warn("escrow_projection: failed to apply EscrowClaimed")
	}
}

// --- cadence.Value field decoding helpers ---

func fieldUInt64(v cadence.Value) (uint64, bool) {
	u, ok := v.(cadence.UInt64)
	if !ok {
		return 0, false
	}
	return uint64(u), true
}

func fieldUInt8(v cadence.Value) (uint8, bool) {
	u, ok := v.(cadence.UInt8)
	if !ok {
		return 0, false
	}
	return uint8(u), true
}

func fieldString(v cadence.Value) (string, bool) {
	s, ok := v.(cadence.String)
	if !ok {
		return "", false
	}
	return string(s), true
}

func fieldUFix64String(v cadence.Value) (string, bool) {
	u, ok := v.(cadence.UFix64)
	if !ok {
		return "", false
	}
	return u.String(), true
}

func fieldAddressHex(v cadence.Value) (string, bool) {
	a, ok := v.(cadence.Address)
	if !ok {
		return "", false
	}
	return flow_helpers.FormatAddress(flow.BytesToAddress(a.Bytes())), true
}

// envelopeBlockHeight extracts ArtDropEventEnvelope.blockHeight from an
// event's "envelope" field, or 0 if the event has no such field (e.g.
// EscrowClaimed) or it isn't the expected shape.
func envelopeBlockHeight(event flow.Event) uint64 {
	envelope, ok := event.Value.SearchFieldByName("envelope").(cadence.Struct)
	if !ok {
		return 0
	}
	height, ok := fieldUInt64(envelope.SearchFieldByName("blockHeight"))
	if !ok {
		return 0
	}
	return height
}
