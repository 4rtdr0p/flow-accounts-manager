package escrow_projection

import (
	"context"
	"strings"
	"testing"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
)

// newEnvelopeStruct builds a minimal cadence.Struct standing in for
// ArtDropEvents.ArtDropEventEnvelope, carrying only blockHeight — the only
// envelope field handler.go reads (see envelopeBlockHeight).
func newEnvelopeStruct(blockHeight uint64) cadence.Value {
	structType := cadence.NewStructType(
		nil,
		"ArtDropEvents.ArtDropEventEnvelope",
		[]cadence.Field{cadence.NewField("blockHeight", cadence.UInt64Type)},
		nil,
	)
	return cadence.NewStruct([]cadence.Value{cadence.NewUInt64(blockHeight)}).WithType(structType)
}

// newTestEvent builds a synthetic flow.Event whose qualified type is
// "A.<addr>.ArtDropCore.<name>" and whose cadence.Event.Value carries
// fields in the given order — same pattern as
// artdrop/event_extractors_test.go's newTestEvent (unexported there, so
// duplicated here rather than shared across packages).
func newTestEvent(name string, fields map[string]cadence.Value, order []string) flow.Event {
	cadenceFields := make([]cadence.Field, 0, len(order))
	values := make([]cadence.Value, 0, len(order))
	for _, k := range order {
		cadenceFields = append(cadenceFields, cadence.NewField(k, cadence.UInt64Type))
		values = append(values, fields[k])
	}

	eventType := cadence.NewEventType(nil, "ArtDropCore."+name, cadenceFields, nil)
	evt := cadence.NewEvent(values).WithType(eventType)

	return flow.Event{
		Type:  "A.ec581a0282d99a1a.ArtDropCore." + name,
		Value: evt,
	}
}

func testAddress(hex string) cadence.Address {
	addr := flow.HexToAddress(hex)
	return cadence.BytesToAddress(addr.Bytes())
}

// fakeStore is an in-memory Store double for handler tests — it records
// every call so tests can assert on exactly what the handler decoded and
// passed through, independent of the real upsert/ownership SQL semantics
// (covered separately in store_gorm_test.go).
type fakeStore struct {
	createCalls   []CreateFields
	releaseCalls  []releaseCall
	claimCalls    []claimCall
	editionByCert map[uint64]*uint64
}

type releaseCall struct {
	escrowID      uint64
	status        uint8
	releaseReason uint8
	height        uint64
}

type claimCall struct {
	escrowID  uint64
	claimed   bool
	claimedAt *string
}

func (f *fakeStore) UpsertCreated(ctx context.Context, fields CreateFields) error {
	f.createCalls = append(f.createCalls, fields)
	return nil
}
func (f *fakeStore) UpsertReleased(ctx context.Context, escrowID uint64, status, releaseReason uint8, height uint64) error {
	f.releaseCalls = append(f.releaseCalls, releaseCall{escrowID, status, releaseReason, height})
	return nil
}
func (f *fakeStore) UpsertClaimed(ctx context.Context, escrowID uint64, claimed bool, claimedAt *string) error {
	f.claimCalls = append(f.claimCalls, claimCall{escrowID, claimed, claimedAt})
	return nil
}
func (f *fakeStore) UpsertBackfillRow(ctx context.Context, row Escrow) error { return nil }
func (f *fakeStore) EditionIDForCertificate(ctx context.Context, certificateID uint64) (*uint64, error) {
	return f.editionByCert[certificateID], nil
}
func (f *fakeStore) GetByID(ctx context.Context, escrowID uint64) (*Escrow, error)   { return nil, nil }
func (f *fakeStore) ListByBuyer(ctx context.Context, buyer string) ([]Escrow, error) { return nil, nil }
func (f *fakeStore) ListByEdition(ctx context.Context, editionID uint64) ([]Escrow, error) {
	return nil, nil
}
func (f *fakeStore) ListBySeller(ctx context.Context, seller string) ([]Escrow, error) {
	return nil, nil
}
func (f *fakeStore) Count(ctx context.Context) (int64, error) { return 0, nil }

var _ Store = (*fakeStore)(nil)

func TestHandleEscrowCreated_DecodesAllFieldsAndAppliesToStore(t *testing.T) {
	store := &fakeStore{}
	h := &ArtDropEscrowEventHandler{Store: store}

	event := newTestEvent("EscrowCreated", map[string]cadence.Value{
		"envelope":      newEnvelopeStruct(12345),
		"escrowId":      cadence.NewUInt64(7),
		"buyer":         testAddress("0x179b6b1cb6755e31"),
		"seller":        testAddress("0xf3fcd2c1a78f5eee"),
		"editionId":     cadence.NewUInt64(42),
		"chipId":        cadence.String("chip-1"),
		"unlockAt":      cadence.UFix64(410244480000000000), // formatted by cadence.UFix64.String()
		"certificateId": cadence.NewUInt64(99),
	}, []string{"envelope", "escrowId", "buyer", "seller", "editionId", "chipId", "unlockAt", "certificateId"})

	h.Handle(context.Background(), event)

	if len(store.createCalls) != 1 {
		t.Fatalf("expected 1 UpsertCreated call, got %d", len(store.createCalls))
	}
	got := store.createCalls[0]
	if got.EscrowID != 7 {
		t.Fatalf("expected escrowId 7, got %d", got.EscrowID)
	}
	if got.Buyer != "0x179b6b1cb6755e31" || got.Seller != "0xf3fcd2c1a78f5eee" {
		t.Fatalf("unexpected buyer/seller: %s / %s", got.Buyer, got.Seller)
	}
	if got.EditionID == nil || *got.EditionID != 42 {
		t.Fatalf("expected editionId 42, got %v", got.EditionID)
	}
	if got.ChipID != "chip-1" {
		t.Fatalf("unexpected chipId: %s", got.ChipID)
	}
	if got.CertificateID != 99 {
		t.Fatalf("expected certificateId 99, got %d", got.CertificateID)
	}
	if got.SourceEvent != "EscrowCreated" {
		t.Fatalf("expected sourceEvent EscrowCreated, got %s", got.SourceEvent)
	}
	if got.LastEventHeight != 12345 {
		t.Fatalf("expected height 12345 from envelope, got %d", got.LastEventHeight)
	}
}

// TestHandleCertificateReEscrowed_ResolvesEditionIDLocally pins the
// re-escrow editionId gap (CertificateReEscrowed carries no editionId —
// see ArtDropCore.cdc): the handler must resolve it via
// EditionIDForCertificate rather than leaving it permanently nil.
func TestHandleCertificateReEscrowed_ResolvesEditionIDLocally(t *testing.T) {
	editionID := uint64(42)
	store := &fakeStore{editionByCert: map[uint64]*uint64{99: &editionID}}
	h := &ArtDropEscrowEventHandler{Store: store}

	event := newTestEvent("CertificateReEscrowed", map[string]cadence.Value{
		"envelope":      newEnvelopeStruct(999),
		"escrowId":      cadence.NewUInt64(20),
		"certificateId": cadence.NewUInt64(99),
		"chipId":        cadence.String("chip-1"),
		"buyer":         testAddress("0x179b6b1cb6755e31"),
		"seller":        testAddress("0xf3fcd2c1a78f5eee"),
		"unlockAt":      cadence.UFix64(1),
	}, []string{"envelope", "escrowId", "certificateId", "chipId", "buyer", "seller", "unlockAt"})

	h.Handle(context.Background(), event)

	if len(store.createCalls) != 1 {
		t.Fatalf("expected 1 UpsertCreated call, got %d", len(store.createCalls))
	}
	got := store.createCalls[0]
	if got.EscrowID != 20 || got.CertificateID != 99 {
		t.Fatalf("unexpected escrowId/certificateId: %d / %d", got.EscrowID, got.CertificateID)
	}
	if got.EditionID == nil || *got.EditionID != 42 {
		t.Fatalf("expected the locally-resolved editionId 42, got %v", got.EditionID)
	}
	if got.SourceEvent != "CertificateReEscrowed" {
		t.Fatalf("expected sourceEvent CertificateReEscrowed, got %s", got.SourceEvent)
	}
}

// TestHandleCertificateReEscrowed_LeavesEditionNilWhenUnresolvable covers
// the rare case documented in handler.go: no prior row for the
// certificate is projected yet.
func TestHandleCertificateReEscrowed_LeavesEditionNilWhenUnresolvable(t *testing.T) {
	store := &fakeStore{}
	h := &ArtDropEscrowEventHandler{Store: store}

	event := newTestEvent("CertificateReEscrowed", map[string]cadence.Value{
		"escrowId":      cadence.NewUInt64(20),
		"certificateId": cadence.NewUInt64(99),
	}, []string{"escrowId", "certificateId"})

	h.Handle(context.Background(), event)

	if len(store.createCalls) != 1 {
		t.Fatalf("expected 1 UpsertCreated call, got %d", len(store.createCalls))
	}
	if store.createCalls[0].EditionID != nil {
		t.Fatalf("expected nil editionId when unresolvable, got %v", store.createCalls[0].EditionID)
	}
}

func TestHandleEscrowReleased_Decodes(t *testing.T) {
	store := &fakeStore{}
	h := &ArtDropEscrowEventHandler{Store: store}

	event := newTestEvent("EscrowReleased", map[string]cadence.Value{
		"envelope":      newEnvelopeStruct(555),
		"escrowId":      cadence.NewUInt64(7),
		"releaser":      testAddress("0x179b6b1cb6755e31"),
		"releaseReason": cadence.UInt8(1), // WindowExpired
	}, []string{"envelope", "escrowId", "releaser", "releaseReason"})

	h.Handle(context.Background(), event)

	if len(store.releaseCalls) != 1 {
		t.Fatalf("expected 1 UpsertReleased call, got %d", len(store.releaseCalls))
	}
	got := store.releaseCalls[0]
	if got.escrowID != 7 {
		t.Fatalf("expected escrowId 7, got %d", got.escrowID)
	}
	if got.status != 1 {
		t.Fatalf("expected status 1 (Released), got %d", got.status)
	}
	if got.releaseReason != 1 {
		t.Fatalf("expected releaseReason 1, got %d", got.releaseReason)
	}
	if got.height != 555 {
		t.Fatalf("expected height 555, got %d", got.height)
	}
}

// TestHandleEscrowClaimed_HasNoEnvelope pins that EscrowClaimed is the one
// escrow event without an envelope (see ArtDropCore.cdc) — the handler
// must still apply claimed=true without a blockHeight.
func TestHandleEscrowClaimed_HasNoEnvelope(t *testing.T) {
	store := &fakeStore{}
	h := &ArtDropEscrowEventHandler{Store: store}

	event := newTestEvent("EscrowClaimed", map[string]cadence.Value{
		"escrowId":      cadence.NewUInt64(7),
		"certificateId": cadence.NewUInt64(99),
		"claimant":      testAddress("0x179b6b1cb6755e31"),
	}, []string{"escrowId", "certificateId", "claimant"})

	h.Handle(context.Background(), event)

	if len(store.claimCalls) != 1 {
		t.Fatalf("expected 1 UpsertClaimed call, got %d", len(store.claimCalls))
	}
	got := store.claimCalls[0]
	if got.escrowID != 7 {
		t.Fatalf("expected escrowId 7, got %d", got.escrowID)
	}
	if !got.claimed {
		t.Fatal("expected claimed=true")
	}
	if got.claimedAt == nil || *got.claimedAt == "" {
		t.Fatal("expected a non-empty claimedAt (wall-clock approximation)")
	}
}

func TestHandle_IgnoresUnrelatedEventType(t *testing.T) {
	store := &fakeStore{}
	h := &ArtDropEscrowEventHandler{Store: store}

	event := newTestEvent("CertificateMinted", map[string]cadence.Value{
		"certificateId": cadence.NewUInt64(1),
	}, []string{"certificateId"})

	h.Handle(context.Background(), event)

	if len(store.createCalls)+len(store.releaseCalls)+len(store.claimCalls) != 0 {
		t.Fatal("expected no store calls for an unrelated event type")
	}
}

func TestHandle_MissingEscrowIdIsANoOp(t *testing.T) {
	store := &fakeStore{}
	h := &ArtDropEscrowEventHandler{Store: store}

	// EscrowCreated with no escrowId field at all.
	event := newTestEvent("EscrowCreated", map[string]cadence.Value{
		"buyer": testAddress("0x179b6b1cb6755e31"),
	}, []string{"buyer"})

	h.Handle(context.Background(), event)

	if len(store.createCalls) != 0 {
		t.Fatal("expected no UpsertCreated call when escrowId is missing")
	}
}

func TestEventTypes_ReturnsTheFourLiveEventsOnly(t *testing.T) {
	types := EventTypes("0xec581a0282d99a1a")

	want := map[string]bool{
		"A.ec581a0282d99a1a.ArtDropCore.EscrowCreated":         false,
		"A.ec581a0282d99a1a.ArtDropCore.CertificateReEscrowed": false,
		"A.ec581a0282d99a1a.ArtDropCore.EscrowReleased":        false,
		"A.ec581a0282d99a1a.ArtDropCore.EscrowClaimed":         false,
	}
	if len(types) != 4 {
		t.Fatalf("expected exactly 4 event types, got %d: %v", len(types), types)
	}
	for _, ty := range types {
		if _, ok := want[ty]; !ok {
			t.Fatalf("unexpected event type %q", ty)
		}
		want[ty] = true
		// None of the dead ArtDropEvents.cdc events (EscrowSettled,
		// EscrowForceRefunded, EscrowForceCancelled) should ever appear.
		if strings.Contains(ty, "Settled") || strings.Contains(ty, "ForceRefunded") || strings.Contains(ty, "ForceCancelled") {
			t.Fatalf("event type %q should never be subscribed — it is never emitted (Phase 3 redesign)", ty)
		}
	}
	for ty, seen := range want {
		if !seen {
			t.Fatalf("expected event type %q to be present", ty)
		}
	}
}

func TestEventTypes_StripsLeading0x(t *testing.T) {
	withPrefix := EventTypes("0xec581a0282d99a1a")
	withoutPrefix := EventTypes("ec581a0282d99a1a")

	if len(withPrefix) != len(withoutPrefix) {
		t.Fatalf("expected same length regardless of 0x prefix")
	}
	for i := range withPrefix {
		if withPrefix[i] != withoutPrefix[i] {
			t.Fatalf("expected identical event types with/without 0x prefix, got %q vs %q", withPrefix[i], withoutPrefix[i])
		}
	}
}
