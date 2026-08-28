package artdrop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop/escrow_projection"
	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	"github.com/google/go-cmp/cmp"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
)

func TestListCertificatesReturnsIds(t *testing.T) {
	mustStr := func(s string) cadence.String {
		v, err := cadence.NewString(s)
		if err != nil {
			panic(err)
		}
		return v
	}
	txSvc := &queryTxService{
		scriptResult: cadence.NewArray([]cadence.Value{
			cadence.NewDictionary([]cadence.KeyValuePair{
				{Key: mustStr("id"), Value: cadence.NewUInt64(1)},
				{Key: mustStr("editionId"), Value: cadence.NewUInt64(7)},
				{Key: mustStr("serial"), Value: cadence.NewUInt64(1)},
				{Key: mustStr("isRevealed"), Value: cadence.NewBool(true)},
			}),
			cadence.NewDictionary([]cadence.KeyValuePair{
				{Key: mustStr("id"), Value: cadence.NewUInt64(42)},
				{Key: mustStr("editionId"), Value: cadence.NewUInt64(7)},
				{Key: mustStr("serial"), Value: cadence.NewUInt64(2)},
				{Key: mustStr("isRevealed"), Value: cadence.NewBool(false)},
			}),
			cadence.NewDictionary([]cadence.KeyValuePair{
				{Key: mustStr("id"), Value: cadence.NewUInt64(99)},
				{Key: mustStr("editionId"), Value: cadence.NewUInt64(7)},
				{Key: mustStr("serial"), Value: cadence.NewUInt64(3)},
				{Key: mustStr("isRevealed"), Value: cadence.NewBool(true)},
			}),
		}),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	certs, err := svc.ListCertificates(context.Background(), "0xf8d6e0586b0a20c7")
	if err != nil {
		t.Fatalf("ListCertificates returned error: %v", err)
	}
	if len(certs) != 3 {
		t.Fatalf("expected 3 certificates, got %d", len(certs))
	}
	if certs[0].Id != 1 || certs[1].Id != 42 || certs[2].Id != 99 {
		t.Fatalf("unexpected certificate ids: %+v", certs)
	}
	if certs[0].EditionId != 7 || certs[1].EditionId != 7 || certs[2].EditionId != 7 {
		t.Fatalf("expected editionId 7 on all, got %+v", certs)
	}
	if certs[0].Serial != 1 || certs[1].Serial != 2 || certs[2].Serial != 3 {
		t.Fatalf("expected serials 1/2/3, got %+v", certs)
	}
	if !certs[0].IsRevealed || certs[1].IsRevealed || !certs[2].IsRevealed {
		t.Fatalf("expected revealed=true/false/true, got %+v", certs)
	}
}

func TestListCertificatesReturnsEmpty(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewArray([]cadence.Value{}),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	certs, err := svc.ListCertificates(context.Background(), "0xf8d6e0586b0a20c7")
	if err != nil {
		t.Fatalf("ListCertificates returned error: %v", err)
	}
	if len(certs) != 0 {
		t.Fatalf("expected 0 certificates, got %d", len(certs))
	}
}

func TestListCertificatesPropagatesScriptError(t *testing.T) {
	txSvc := &queryTxService{err: errors.New("script execution failed")}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.ListCertificates(context.Background(), "0xf8d6e0586b0a20c7")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListCertificatesRejectsUnexpectedType(t *testing.T) {
	strVal, _ := cadence.NewString("not-an-array")
	txSvc := &queryTxService{
		scriptResult: strVal,
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.ListCertificates(context.Background(), "0xf8d6e0586b0a20c7")
	if err == nil {
		t.Fatal("expected error for unexpected script result type, got nil")
	}
}

// escrowSummaryDict builds the raw {String: AnyStruct} dictionary shared by
// get_escrow_summary.cdc's (optional) result and every
// get_escrows_by_*_expanded.cdc script's per-element (non-optional) result,
// matching the fields decoded by decodeEscrowSummary. Shared by the tests
// below.
func escrowSummaryDict(t *testing.T, id, editionId, nonce, certificateId uint64, status uint8, releaseReason *uint8, claimed bool, claimedAt *string) cadence.Dictionary {
	t.Helper()

	unlockAt, err := cadence.NewUFix64("4102444800.00000000")
	if err != nil {
		t.Fatal(err)
	}

	releaseReasonValue := cadence.NewOptional(nil)
	if releaseReason != nil {
		releaseReasonValue = cadence.NewOptional(cadence.NewUInt8(*releaseReason))
	}

	claimedAtValue := cadence.NewOptional(nil)
	if claimedAt != nil {
		ufix, err := cadence.NewUFix64(*claimedAt)
		if err != nil {
			t.Fatal(err)
		}
		claimedAtValue = cadence.NewOptional(ufix)
	}

	return cadence.NewDictionary([]cadence.KeyValuePair{
		{Key: cadence.String("id"), Value: cadence.NewUInt64(id)},
		{Key: cadence.String("buyer"), Value: cadence.NewAddress(flow.HexToAddress("0x179b6b1cb6755e31"))},
		{Key: cadence.String("seller"), Value: cadence.NewAddress(flow.HexToAddress("0xf3fcd2c1a78f5eee"))},
		{Key: cadence.String("editionId"), Value: cadence.NewUInt64(editionId)},
		{Key: cadence.String("chipId"), Value: cadence.String("chip-1")},
		{Key: cadence.String("unlockAt"), Value: unlockAt},
		{Key: cadence.String("nonce"), Value: cadence.NewUInt64(nonce)},
		{Key: cadence.String("certificateId"), Value: cadence.NewUInt64(certificateId)},
		{Key: cadence.String("status"), Value: cadence.NewUInt8(status)},
		{Key: cadence.String("releaseReason"), Value: releaseReasonValue},
		{Key: cadence.String("claimed"), Value: cadence.NewBool(claimed)},
		{Key: cadence.String("claimedAt"), Value: claimedAtValue},
	})
}

// escrowSummaryScriptResult wraps escrowSummaryDict as the `{String:
// AnyStruct}?` optional get_escrow_summary.cdc returns.
func escrowSummaryScriptResult(t *testing.T, id, editionId, nonce, certificateId uint64, status uint8, releaseReason *uint8, claimed bool, claimedAt *string) cadence.Value {
	t.Helper()
	return cadence.NewOptional(escrowSummaryDict(t, id, editionId, nonce, certificateId, status, releaseReason, claimed, claimedAt))
}

// escrowSummaryExpandedArrayResult builds the `[{String: AnyStruct}]` array
// a get_escrows_by_*_expanded.cdc script returns, one escrowSummaryDict per
// id — the shape decodeEscrowSummaryArray expects.
func escrowSummaryExpandedArrayResult(t *testing.T, dicts ...cadence.Dictionary) cadence.Value {
	t.Helper()
	values := make([]cadence.Value, 0, len(dicts))
	for _, d := range dicts {
		values = append(values, d)
	}
	return cadence.NewArray(values)
}

// TestGetEscrowReturnsFullSummary pins issue #98: get_escrow_summary.cdc now
// returns ArtDropCore.EscrowSummary's full field set (buyer, seller,
// editionId, chipId, unlockAt, nonce, certificateId, status, releaseReason,
// claimed, claimedAt), not just a bare status byte.
func TestGetEscrowReturnsFullSummary(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: escrowSummaryScriptResult(t, 7, 42, 1, 99, 3, nil, false, nil),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	summary, err := svc.GetEscrow(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetEscrow returned error: %v", err)
	}
	if summary.Id != 7 {
		t.Fatalf("expected escrow id 7, got %d", summary.Id)
	}
	if summary.Buyer != "0x179b6b1cb6755e31" {
		t.Fatalf("unexpected buyer: %s", summary.Buyer)
	}
	if summary.Seller != "0xf3fcd2c1a78f5eee" {
		t.Fatalf("unexpected seller: %s", summary.Seller)
	}
	if summary.EditionId != 42 {
		t.Fatalf("expected editionId 42, got %d", summary.EditionId)
	}
	if summary.ChipId != "chip-1" {
		t.Fatalf("unexpected chipId: %s", summary.ChipId)
	}
	if summary.UnlockAt != "4102444800.00000000" {
		t.Fatalf("unexpected unlockAt: %s", summary.UnlockAt)
	}
	if summary.Nonce != 1 {
		t.Fatalf("expected nonce 1, got %d", summary.Nonce)
	}
	if summary.CertificateId != 99 {
		t.Fatalf("expected certificateId 99, got %d", summary.CertificateId)
	}
	if summary.Status != 3 {
		t.Fatalf("expected status 3, got %d", summary.Status)
	}
	if summary.ReleaseReason != nil {
		t.Fatalf("expected nil releaseReason, got %v", *summary.ReleaseReason)
	}
	if summary.Claimed {
		t.Fatal("expected claimed=false")
	}
	if summary.ClaimedAt != nil {
		t.Fatalf("expected nil claimedAt, got %v", *summary.ClaimedAt)
	}
	if len(txSvc.args) != 1 {
		t.Fatalf("expected 1 script arg, got %d", len(txSvc.args))
	}
	if txSvc.args[0] != cadence.NewUInt64(7) {
		t.Fatalf("expected escrow id as arg, got %#v", txSvc.args[0])
	}
}

// TestGetEscrowReturnsReleasedFields covers the optional releaseReason/
// claimedAt fields once an escrow is Released and claimed.
func TestGetEscrowReturnsReleasedFields(t *testing.T) {
	reason := uint8(0) // ClaimedByBuyer
	claimedAt := "4102444900.00000000"
	txSvc := &queryTxService{
		scriptResult: escrowSummaryScriptResult(t, 7, 42, 1, 99, 1, &reason, true, &claimedAt),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	summary, err := svc.GetEscrow(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetEscrow returned error: %v", err)
	}
	if summary.ReleaseReason == nil || *summary.ReleaseReason != 0 {
		t.Fatalf("expected releaseReason 0, got %v", summary.ReleaseReason)
	}
	if !summary.Claimed {
		t.Fatal("expected claimed=true")
	}
	if summary.ClaimedAt == nil || *summary.ClaimedAt != claimedAt {
		t.Fatalf("expected claimedAt %s, got %v", claimedAt, summary.ClaimedAt)
	}
}

// TestGetEscrowReturnsNilWhenNotFound mirrors GetCertificateDetail's
// nil-means-404 convention: ArtDropCore.getEscrowSummary returns nil for an
// unknown escrow id, and get_escrow_summary.cdc passes that straight
// through as Optional(nil).
func TestGetEscrowReturnsNilWhenNotFound(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewOptional(nil),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	summary, err := svc.GetEscrow(context.Background(), 7)
	if err != nil {
		t.Fatalf("expected nil error when script returns Optional(nil), got %v", err)
	}
	if summary != nil {
		t.Fatalf("expected nil summary, got %+v", summary)
	}
}

func TestGetEscrowPropagatesScriptError(t *testing.T) {
	txSvc := &queryTxService{err: errors.New("script execution failed")}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.GetEscrow(context.Background(), 7)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetEscrowRejectsUnexpectedType(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewUInt64(42),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.GetEscrow(context.Background(), 7)
	if err == nil {
		t.Fatal("expected error for unexpected script result type, got nil")
	}
}

// TestListEscrowsByBuyerReturnsIds pins issue #98's listing endpoint: with
// expand=false, it returns exactly the escrow ids from
// ArtDropRegistry.EscrowsByBuyerIndex and makes a single script call — no
// N+1 GetEscrow lookups.
func TestListEscrowsByBuyerReturnsIds(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewArray([]cadence.Value{
			cadence.NewUInt64(7),
			cadence.NewUInt64(9),
		}),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	res, err := svc.ListEscrowsByBuyer(context.Background(), "0xf8d6e0586b0a20c7", false)
	if err != nil {
		t.Fatalf("ListEscrowsByBuyer returned error: %v", err)
	}
	if len(res.EscrowIds) != 2 || res.EscrowIds[0] != 7 || res.EscrowIds[1] != 9 {
		t.Fatalf("unexpected escrow ids: %+v", res.EscrowIds)
	}
	if res.Escrows != nil {
		t.Fatalf("expected no expanded escrows when expand=false, got %+v", res.Escrows)
	}
	if len(txSvc.calls) != 1 {
		t.Fatalf("expected 1 script call, got %d", len(txSvc.calls))
	}
	if len(txSvc.calls[0]) != 2 {
		t.Fatalf("expected 2 args (buyer, registryOwner), got %d", len(txSvc.calls[0]))
	}
	if txSvc.calls[0][0] != cadence.NewAddress(flow.HexToAddress("0xf8d6e0586b0a20c7")) {
		t.Fatalf("expected buyer as first arg, got %#v", txSvc.calls[0][0])
	}
}

// TestListEscrowsByBuyerReturnsEmpty covers both a buyer with no escrows and
// an unpublished EscrowsByBuyerIndex — the script can't tell those apart, so
// this is also the shape returned if the index was never set up on the
// configured registry account.
func TestListEscrowsByBuyerReturnsEmpty(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewArray([]cadence.Value{}),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	res, err := svc.ListEscrowsByBuyer(context.Background(), "0xf8d6e0586b0a20c7", false)
	if err != nil {
		t.Fatalf("ListEscrowsByBuyer returned error: %v", err)
	}
	if len(res.EscrowIds) != 0 {
		t.Fatalf("expected 0 escrow ids, got %d", len(res.EscrowIds))
	}
}

// TestListEscrowsByBuyerExpandsSummaries covers ?expand=summary: one script
// call for the id list, then exactly one further call to
// get_escrows_by_buyer_expanded.cdc that resolves every id's summary in a
// single execution — issue #100 replaced the previous per-id GetEscrow loop
// (one script call per escrow) with this combined call specifically because
// that loop rate-limited the public testnet access node at just 9 escrows.
func TestListEscrowsByBuyerExpandsSummaries(t *testing.T) {
	txSvc := &queryTxService{
		scriptResults: []cadence.Value{
			cadence.NewArray([]cadence.Value{
				cadence.NewUInt64(7),
				cadence.NewUInt64(9),
			}),
			escrowSummaryExpandedArrayResult(t,
				escrowSummaryDict(t, 7, 42, 1, 99, 0, nil, false, nil),
				escrowSummaryDict(t, 9, 43, 2, 100, 1, nil, true, nil),
			),
		},
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	res, err := svc.ListEscrowsByBuyer(context.Background(), "0xf8d6e0586b0a20c7", true)
	if err != nil {
		t.Fatalf("ListEscrowsByBuyer returned error: %v", err)
	}
	if len(res.EscrowIds) != 2 {
		t.Fatalf("expected 2 escrow ids, got %d", len(res.EscrowIds))
	}
	if len(res.Escrows) != 2 {
		t.Fatalf("expected 2 expanded escrows, got %d", len(res.Escrows))
	}
	if res.Escrows[0].Id != 7 || res.Escrows[0].EditionId != 42 {
		t.Fatalf("unexpected first expanded escrow: %+v", res.Escrows[0])
	}
	if res.Escrows[1].Id != 9 || res.Escrows[1].EditionId != 43 {
		t.Fatalf("unexpected second expanded escrow: %+v", res.Escrows[1])
	}
	if len(txSvc.calls) != 2 {
		t.Fatalf("expected 2 script calls (id list + combined expansion), got %d", len(txSvc.calls))
	}
}

// TestListEscrowsByEditionReturnsIds mirrors
// TestListEscrowsByBuyerReturnsIds for the by-edition listing added
// alongside it for issue #100.
func TestListEscrowsByEditionReturnsIds(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewArray([]cadence.Value{
			cadence.NewUInt64(11),
			cadence.NewUInt64(12),
		}),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	res, err := svc.ListEscrowsByEdition(context.Background(), 42, false)
	if err != nil {
		t.Fatalf("ListEscrowsByEdition returned error: %v", err)
	}
	if len(res.EscrowIds) != 2 || res.EscrowIds[0] != 11 || res.EscrowIds[1] != 12 {
		t.Fatalf("unexpected escrow ids: %+v", res.EscrowIds)
	}
	if res.Escrows != nil {
		t.Fatalf("expected no expanded escrows when expand=false, got %+v", res.Escrows)
	}
	if len(txSvc.calls) != 1 {
		t.Fatalf("expected 1 script call, got %d", len(txSvc.calls))
	}
	if txSvc.calls[0][0] != cadence.NewUInt64(42) {
		t.Fatalf("expected editionId as first arg, got %#v", txSvc.calls[0][0])
	}
}

// TestListEscrowsByEditionExpandsSummaries mirrors
// TestListEscrowsByBuyerExpandsSummaries: id-list call plus exactly one
// combined-expansion call, never a per-id loop.
func TestListEscrowsByEditionExpandsSummaries(t *testing.T) {
	txSvc := &queryTxService{
		scriptResults: []cadence.Value{
			cadence.NewArray([]cadence.Value{cadence.NewUInt64(11)}),
			escrowSummaryExpandedArrayResult(t,
				escrowSummaryDict(t, 11, 42, 1, 99, 0, nil, false, nil),
			),
		},
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	res, err := svc.ListEscrowsByEdition(context.Background(), 42, true)
	if err != nil {
		t.Fatalf("ListEscrowsByEdition returned error: %v", err)
	}
	if len(res.Escrows) != 1 || res.Escrows[0].Id != 11 {
		t.Fatalf("unexpected expanded escrows: %+v", res.Escrows)
	}
	if len(txSvc.calls) != 2 {
		t.Fatalf("expected 2 script calls (id list + combined expansion), got %d", len(txSvc.calls))
	}
}

// TestListEscrowsBySellerWalksThreeIndicesInOneCall covers the by-seller
// listing (issue #100): unlike buyer/edition there is no separate ids-only
// script, so even expand=false makes exactly one call to
// get_escrows_by_seller_expanded.cdc and derives EscrowIds from its result.
//
// This walk moved from ListEscrowsBySeller to ListEscrowsByArtist in issue
// #102 — "which editions did this address create as an artist" is a
// different question from the new ListEscrowsBySeller's literal
// WHERE-seller-field query (projection-served, tested separately below in
// TestListEscrowsBySeller*). See Service.ListEscrowsBySeller's doc comment
// for the full split rationale.
func TestListEscrowsByArtistWalksThreeIndicesInOneCall(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: escrowSummaryExpandedArrayResult(t,
			escrowSummaryDict(t, 21, 5, 1, 60, 0, nil, false, nil),
			escrowSummaryDict(t, 22, 6, 2, 61, 1, nil, true, nil),
		),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	res, err := svc.ListEscrowsByArtist(context.Background(), "0xf8d6e0586b0a20c7", false)
	if err != nil {
		t.Fatalf("ListEscrowsByArtist returned error: %v", err)
	}
	if len(res.EscrowIds) != 2 || res.EscrowIds[0] != 21 || res.EscrowIds[1] != 22 {
		t.Fatalf("unexpected escrow ids: %+v", res.EscrowIds)
	}
	if res.Escrows != nil {
		t.Fatalf("expected no expanded escrows when expand=false, got %+v", res.Escrows)
	}
	if len(txSvc.calls) != 1 {
		t.Fatalf("expected exactly 1 script call regardless of expand, got %d", len(txSvc.calls))
	}
}

// TestListEscrowsByArtistExpandsSummaries covers ?expand=summary for
// by-artist: same single call, but the response now also carries the full
// summaries.
func TestListEscrowsByArtistExpandsSummaries(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: escrowSummaryExpandedArrayResult(t,
			escrowSummaryDict(t, 21, 5, 1, 60, 0, nil, false, nil),
		),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	res, err := svc.ListEscrowsByArtist(context.Background(), "0xf8d6e0586b0a20c7", true)
	if err != nil {
		t.Fatalf("ListEscrowsByArtist returned error: %v", err)
	}
	if len(res.Escrows) != 1 || res.Escrows[0].Id != 21 || res.Escrows[0].EditionId != 5 {
		t.Fatalf("unexpected expanded escrows: %+v", res.Escrows)
	}
	if len(txSvc.calls) != 1 {
		t.Fatalf("expected exactly 1 script call, got %d", len(txSvc.calls))
	}
}

// TestListEscrowsByArtistPropagatesScriptError covers by-artist's script
// failure path (its own scripted TxService.err, distinct from
// TestListEscrowsByBuyerPropagatesScriptError below).
func TestListEscrowsByArtistPropagatesScriptError(t *testing.T) {
	txSvc := &queryTxService{err: errors.New("script execution failed")}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.ListEscrowsByArtist(context.Background(), "0xf8d6e0586b0a20c7", false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestListEscrowsBySeller_ServesFromProjectionOnly pins issue #102's split:
// ListEscrowsBySeller now answers "escrows where address is literally the
// seller field" from the local projection, never from
// get_escrows_by_seller_expanded.cdc's artist walk (that's
// ListEscrowsByArtist above, a different question).
func TestListEscrowsBySeller_ServesFromProjectionOnly(t *testing.T) {
	db := newProjectionTestDB(t)
	txSvc := &queryTxService{}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
		DB:           db,
	})

	seller := "0xf3fcd2c1a78f5eee"
	if err := svc.escrowStore.UpsertCreated(context.Background(), escrow_projection.CreateFields{
		EscrowID: 21, Buyer: "0x179b6b1cb6755e31", Seller: seller,
		EditionID: editionPtr(5), CertificateID: 60, SourceEvent: "EscrowCreated",
	}); err != nil {
		t.Fatalf("seed projection: %v", err)
	}

	res, err := svc.ListEscrowsBySeller(context.Background(), seller, true)
	if err != nil {
		t.Fatalf("ListEscrowsBySeller returned error: %v", err)
	}
	if len(res.EscrowIds) != 1 || res.EscrowIds[0] != 21 {
		t.Fatalf("unexpected escrow ids: %+v", res.EscrowIds)
	}
	if len(res.Escrows) != 1 || res.Escrows[0].Seller != seller {
		t.Fatalf("unexpected expanded escrows: %+v", res.Escrows)
	}
	if len(txSvc.calls) != 0 {
		t.Fatalf("expected zero chain calls — by-seller must never fall back to the artist walk, got %d calls", len(txSvc.calls))
	}
}

// TestListEscrowsBySeller_ReturnsEmptyNotChainDataWhenProjectionIsCold
// pins the explicit tech-lead decision: an unready projection returns an
// empty (never nil) result for by-seller — it must NEVER fall back to
// ListEscrowsByArtist's walk, which would silently answer a different
// question (e.g. a gallery reselling on an artist's behalf is a seller but
// not the artist).
func TestListEscrowsBySeller_ReturnsEmptyNotChainDataWhenProjectionIsCold(t *testing.T) {
	db := newProjectionTestDB(t) // migrated, but empty — projection not backfilled yet
	txSvc := &queryTxService{}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
		DB:           db,
	})

	res, err := svc.ListEscrowsBySeller(context.Background(), "0xf3fcd2c1a78f5eee", false)
	if err != nil {
		t.Fatalf("ListEscrowsBySeller returned error: %v", err)
	}
	if res.EscrowIds == nil || len(res.EscrowIds) != 0 {
		t.Fatalf("expected an empty (never nil) EscrowIds slice, got %+v", res.EscrowIds)
	}
	if len(txSvc.calls) != 0 {
		t.Fatalf("expected zero chain calls when the projection is cold, got %d", len(txSvc.calls))
	}
}

// TestListEscrowsBySeller_ReturnsEmptyWhenNoDBConfigured covers the
// deps.DB==nil case (no chain fallback ever, same reasoning as the cold
// projection case above).
func TestListEscrowsBySeller_ReturnsEmptyWhenNoDBConfigured(t *testing.T) {
	txSvc := &queryTxService{}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	res, err := svc.ListEscrowsBySeller(context.Background(), "0xf3fcd2c1a78f5eee", false)
	if err != nil {
		t.Fatalf("ListEscrowsBySeller returned error: %v", err)
	}
	if len(res.EscrowIds) != 0 {
		t.Fatalf("expected an empty EscrowIds slice, got %+v", res.EscrowIds)
	}
	if len(txSvc.calls) != 0 {
		t.Fatalf("expected zero chain calls, got %d", len(txSvc.calls))
	}
}

// TestListEscrowsByBuyerPropagatesScriptError covers the listing script's
// own failure path.
func TestListEscrowsByBuyerPropagatesScriptError(t *testing.T) {
	txSvc := &queryTxService{err: errors.New("script execution failed")}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.ListEscrowsByBuyer(context.Background(), "0xf8d6e0586b0a20c7", false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestListEscrowsByBuyerRejectsUnexpectedType covers a malformed script
// result for the id-list call.
func TestListEscrowsByBuyerRejectsUnexpectedType(t *testing.T) {
	strVal, _ := cadence.NewString("not-an-array")
	txSvc := &queryTxService{scriptResult: strVal}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.ListEscrowsByBuyer(context.Background(), "0xf8d6e0586b0a20c7", false)
	if err == nil {
		t.Fatal("expected error for unexpected script result type, got nil")
	}
}

func TestGetCertificateDetailReturnsConsolidatedMetadata(t *testing.T) {
	baseTier, err := cadence.NewUFix64("1.25000000")
	if err != nil {
		t.Fatal(err)
	}
	finalMultiplier, err := cadence.NewUFix64("2.50000000")
	if err != nil {
		t.Fatal(err)
	}
	displayName, err := cadence.NewString("Certificate #7")
	if err != nil {
		t.Fatal(err)
	}
	txSvc := &queryTxService{
		scriptResult: cadence.NewOptional(cadence.NewDictionary([]cadence.KeyValuePair{
			{Key: cadence.String("id"), Value: cadence.NewUInt64(7)},
			{Key: cadence.String("baseTier"), Value: cadence.NewOptional(baseTier)},
			{Key: cadence.String("finalMultiplier"), Value: cadence.NewOptional(finalMultiplier)},
			{Key: cadence.String("chipPubKey"), Value: cadence.NewArray([]cadence.Value{cadence.NewUInt8(1), cadence.NewUInt8(2), cadence.NewUInt8(3)})},
			{Key: cadence.String("isRevealed"), Value: cadence.NewBool(true)},
			{Key: cadence.String("displayName"), Value: cadence.NewOptional(displayName)},
		})),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	detail, err := svc.GetCertificateDetail(context.Background(), "0xf8d6e0586b0a20c7", 7)
	if err != nil {
		t.Fatalf("GetCertificateDetail returned error: %v", err)
	}
	if detail.Id != 7 {
		t.Fatalf("expected id 7, got %d", detail.Id)
	}
	if detail.BaseTier == nil || *detail.BaseTier != "1.25000000" {
		t.Fatalf("unexpected base tier: %+v", detail.BaseTier)
	}
	if string(detail.ChipPubKey) != string([]byte{1, 2, 3}) {
		t.Fatalf("unexpected chip pub key: %v", detail.ChipPubKey)
	}
	if !detail.IsRevealed {
		t.Fatal("expected certificate to be revealed")
	}
	if detail.FinalMultiplier == nil || *detail.FinalMultiplier != "2.50000000" {
		t.Fatalf("unexpected final multiplier: %+v", detail.FinalMultiplier)
	}
	if detail.DisplayName == nil || *detail.DisplayName != "Certificate #7" {
		t.Fatalf("unexpected display name: %+v", detail.DisplayName)
	}
	if len(txSvc.calls) != 1 {
		t.Fatalf("expected 1 script call, got %d", len(txSvc.calls))
	}
	if len(txSvc.calls[0]) != 2 {
		t.Fatalf("expected 2 args per script call, got %d", len(txSvc.calls[0]))
	}
	if txSvc.calls[0][0] != cadence.NewAddress(flow.HexToAddress("0xf8d6e0586b0a20c7")) {
		t.Fatalf("expected address as first arg, got %#v", txSvc.calls[0][0])
	}
	if txSvc.calls[0][1] != cadence.NewUInt64(7) {
		t.Fatalf("expected certificate id as second arg, got %#v", txSvc.calls[0][1])
	}
}

func TestGetCertificateDetailReturnsNilWhenScriptReturnsNil(t *testing.T) {
	// Mirrors the script's behaviour for: missing collection, wrong-type
	// capability, empty collection, missing cert id — all four collapse to
	// `Optional(nil)` and the service returns (nil, nil) so the handler
	// can answer 404.
	txSvc := &queryTxService{
		scriptResult: cadence.NewOptional(nil),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	detail, err := svc.GetCertificateDetail(context.Background(), "0xf8d6e0586b0a20c7", 7)
	if err != nil {
		t.Fatalf("expected nil error when script returns Optional(nil), got %v", err)
	}
	if detail != nil {
		t.Fatalf("expected nil detail when script returns Optional(nil), got %+v", detail)
	}
}

func TestGetCertificateDetailRejectsUnexpectedChipPubKeyType(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewOptional(cadence.NewDictionary([]cadence.KeyValuePair{
			{Key: cadence.String("id"), Value: cadence.NewUInt64(7)},
			{Key: cadence.String("baseTier"), Value: cadence.NewOptional(nil)},
			{Key: cadence.String("finalMultiplier"), Value: cadence.NewOptional(nil)},
			{Key: cadence.String("chipPubKey"), Value: cadence.NewUInt64(42)},
			{Key: cadence.String("isRevealed"), Value: cadence.NewBool(false)},
			{Key: cadence.String("displayName"), Value: cadence.NewOptional(nil)},
		})),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.GetCertificateDetail(context.Background(), "0xf8d6e0586b0a20c7", 7)
	if err == nil {
		t.Fatal("expected error for unexpected chip pub key result type, got nil")
	}
}

// TestIsArtistReturnsTrue also covers the registryOwner parameter added to
// is_artist.cdc/Service.IsArtist: registryOwner used to be a second,
// independent literal hardcoded inside the script body, invisible to
// substituteAddresses (which only rewrites import lines) — a redeploy could
// update the import correctly and silently leave that second literal
// pointing at a retired account, making the script resolve and run while
// returning false for every real artist. It's now a script argument built
// from config, so this asserts the arg the chain actually receives is the
// server's ArtDropRegistryAddress, not a literal baked into the .cdc file.
func TestIsArtistReturnsTrue(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewBool(true),
	}
	cfg := ParseTestConfig(t)
	svc, err := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}, cfg)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	is, err := svc.IsArtist(context.Background(), "0xf8d6e0586b0a20c7")
	if err != nil {
		t.Fatalf("IsArtist returned error: %v", err)
	}
	if !is {
		t.Fatalf("expected isArtist true, got false")
	}
	if len(txSvc.args) != 2 {
		t.Fatalf("expected 2 script args (artist, registryOwner), got %d", len(txSvc.args))
	}
	addr, ok := txSvc.args[0].(cadence.Address)
	if !ok {
		t.Fatalf("expected first script arg to be cadence.Address, got %T", txSvc.args[0])
	}
	if addr.Hex() != flow.HexToAddress("0xf8d6e0586b0a20c7").Hex() {
		t.Fatalf("expected first script arg address 0xf8d6e0586b0a20c7, got %s", addr.Hex())
	}
	registryOwner, ok := txSvc.args[1].(cadence.Address)
	if !ok {
		t.Fatalf("expected second script arg to be cadence.Address, got %T", txSvc.args[1])
	}
	if registryOwner.Hex() != flow.HexToAddress(cfg.ArtDropRegistryAddress).Hex() {
		t.Fatalf("expected second script arg to be server config's ArtDropRegistryAddress %q, got %s", cfg.ArtDropRegistryAddress, registryOwner.Hex())
	}
}

func TestIsArtistReturnsFalse(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewBool(false),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	is, err := svc.IsArtist(context.Background(), "0xf8d6e0586b0a20c7")
	if err != nil {
		t.Fatalf("IsArtist returned error: %v", err)
	}
	if is {
		t.Fatalf("expected isArtist false, got true")
	}
}

func TestIsArtistPropagatesScriptError(t *testing.T) {
	txSvc := &queryTxService{err: errors.New("script execution failed")}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.IsArtist(context.Background(), "0xf8d6e0586b0a20c7")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestIsArtistRejectsUnexpectedType(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewUInt64(1),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.IsArtist(context.Background(), "0xf8d6e0586b0a20c7")
	if err == nil {
		t.Fatal("expected error for unexpected script result type, got nil")
	}
}

func TestIsArtistRejectsInvalidAddress(t *testing.T) {
	txSvc := &queryTxService{}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.IsArtist(context.Background(), "not-an-address")
	if err == nil {
		t.Fatal("expected error for invalid address, got nil")
	}
}

func TestSummaryScriptsProjectAllContractFields(t *testing.T) {
	tests := []struct {
		name        string
		script      string
		projections []string
	}{
		{
			name:   "original",
			script: getOriginalExtendedSummaryCDC,
			projections: []string{
				`"id": orig.id`,
				`"artist": orig.artist`,
				`"name": orig.name`,
				`"prices": orig.prices`,
				`"createdAtBlock": orig.createdAtBlock`,
				`"schemaVersion": orig.schemaVersion`,
				`"editionCount": orig.editionCount`,
				`"totalMintedAcrossEditions": orig.totalMintedAcrossEditions`,
				`"displayName": orig.displayName`,
			},
		},
		{
			name:   "edition",
			script: getEditionSummaryCDC,
			projections: []string{
				`"id": ed.id`,
				`"originalId": ed.originalId`,
				`"artist": ed.artist`,
				`"shuffleSeedBlock": ed.shuffleSeedBlock`,
				`"reprintLimit": ed.reprintLimit`,
				`"prices": ed.prices`,
				`"profitSplit": ed.profitSplit`,
				`"rarityCurve": ed.rarityCurve`,
				`"multiplierWeights": ed.multiplierWeights`,
				`"createdAtBlock": ed.createdAtBlock`,
				`"schemaVersion": ed.schemaVersion`,
				`"state": stateRaw`,
				`"totalMinted": ed.totalMinted`,
				`"rarityProfile": ed.rarityProfile`,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, projection := range test.projections {
				if !strings.Contains(test.script, projection) {
					t.Errorf("summary script is missing projection %q", projection)
				}
			}
		})
	}
}

func TestGetOriginalSummaryMapsContractFields(t *testing.T) {
	primary, err := cadence.NewUFix64("10.00000000")
	if err != nil {
		t.Fatal(err)
	}
	displayName, err := cadence.NewString("Ariel Artist")
	if err != nil {
		t.Fatal(err)
	}
	txSvc := &queryTxService{
		scriptResult: cadence.NewOptional(cadence.NewDictionary([]cadence.KeyValuePair{
			{Key: cadence.String("id"), Value: cadence.NewUInt64(3)},
			{Key: cadence.String("artist"), Value: cadence.NewAddress(flow.HexToAddress("0xf8d6e0586b0a20c7"))},
			{Key: cadence.String("name"), Value: cadence.String("Original")},
			{Key: cadence.String("prices"), Value: cadence.NewDictionary([]cadence.KeyValuePair{{Key: cadence.String("primary"), Value: primary}})},
			{Key: cadence.String("createdAtBlock"), Value: cadence.NewUInt64(100)},
			{Key: cadence.String("schemaVersion"), Value: cadence.NewUInt8(2)},
			{Key: cadence.String("editionCount"), Value: cadence.NewUInt64(5)},
			{Key: cadence.String("totalMintedAcrossEditions"), Value: cadence.NewUInt64(42)},
			{Key: cadence.String("displayName"), Value: cadence.NewOptional(displayName)},
		})),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	summary, err := svc.GetOriginalSummary(context.Background(), 3)
	if err != nil {
		t.Fatalf("GetOriginalSummary returned error: %v", err)
	}
	if summary.Id != 3 || summary.Artist != "0xf8d6e0586b0a20c7" || summary.Name != "Original" || summary.Prices["primary"] != "10.00000000" || summary.CreatedAtBlock != 100 || summary.SchemaVersion != 2 || summary.EditionCount != 5 || summary.TotalMintedAcrossEditions != 42 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.DisplayName == nil || *summary.DisplayName != "Ariel Artist" {
		t.Fatalf("unexpected display name: %+v", summary.DisplayName)
	}
}

func TestGetOriginalSummaryAllowsMissingDisplayName(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewOptional(cadence.NewDictionary([]cadence.KeyValuePair{
			{Key: cadence.String("id"), Value: cadence.NewUInt64(3)},
			{Key: cadence.String("displayName"), Value: cadence.NewOptional(nil)},
		})),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	summary, err := svc.GetOriginalSummary(context.Background(), 3)
	if err != nil {
		t.Fatalf("GetOriginalSummary returned error: %v", err)
	}
	if summary.DisplayName != nil {
		t.Fatalf("expected nil display name, got %+v", summary.DisplayName)
	}
}

func TestGetOriginalSummaryRejectsUnexpectedDisplayNameType(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewOptional(cadence.NewDictionary([]cadence.KeyValuePair{
			{Key: cadence.String("displayName"), Value: cadence.NewOptional(cadence.NewUInt64(42))},
		})),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.GetOriginalSummary(context.Background(), 3)
	if err == nil {
		t.Fatal("expected error for unexpected displayName type, got nil")
	}
}

func TestGetEditionSummaryMapsContractFields(t *testing.T) {
	primary, err := cadence.NewUFix64("12.00000000")
	if err != nil {
		t.Fatal(err)
	}
	artistShare, err := cadence.NewUFix64("0.85000000")
	if err != nil {
		t.Fatal(err)
	}
	rareWeight, err := cadence.NewUFix64("0.25000000")
	if err != nil {
		t.Fatal(err)
	}
	txSvc := &queryTxService{
		scriptResult: cadence.NewOptional(cadence.NewDictionary([]cadence.KeyValuePair{
			{Key: cadence.String("id"), Value: cadence.NewUInt64(4)},
			{Key: cadence.String("originalId"), Value: cadence.NewUInt64(3)},
			{Key: cadence.String("artist"), Value: cadence.NewAddress(flow.HexToAddress("0xf8d6e0586b0a20c7"))},
			{Key: cadence.String("shuffleSeedBlock"), Value: cadence.NewUInt64(99)},
			{Key: cadence.String("reprintLimit"), Value: cadence.NewUInt64(500)},
			{Key: cadence.String("prices"), Value: cadence.NewDictionary([]cadence.KeyValuePair{{Key: cadence.String("primary"), Value: primary}})},
			{Key: cadence.String("profitSplit"), Value: cadence.NewDictionary([]cadence.KeyValuePair{{Key: cadence.String("artist"), Value: artistShare}})},
			{Key: cadence.String("rarityCurve"), Value: cadence.NewArray([]cadence.Value{cadence.NewUInt64(1), cadence.NewUInt64(2)})},
			{Key: cadence.String("multiplierWeights"), Value: cadence.NewDictionary([]cadence.KeyValuePair{{Key: cadence.String("rare"), Value: rareWeight}})},
			{Key: cadence.String("createdAtBlock"), Value: cadence.NewUInt64(101)},
			{Key: cadence.String("schemaVersion"), Value: cadence.NewUInt8(2)},
			{Key: cadence.String("state"), Value: cadence.NewUInt8(3)},
			{Key: cadence.String("totalMinted"), Value: cadence.NewUInt64(9)},
			{Key: cadence.String("rarityProfile"), Value: cadence.NewUInt8(1)},
		})),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	summary, err := svc.GetEditionSummary(context.Background(), 4)
	if err != nil {
		t.Fatalf("GetEditionSummary returned error: %v", err)
	}
	if summary.Id != 4 || summary.OriginalId != 3 || summary.Artist != "0xf8d6e0586b0a20c7" || summary.ShuffleSeedBlock != 99 || summary.ReprintLimit != 500 || summary.MaxSupply != 500 || summary.CreatedAtBlock != 101 || summary.SchemaVersion != 2 || summary.State != "3" || summary.TotalMinted != 9 || summary.RarityProfile != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.Prices["primary"] != "12.00000000" || summary.ProfitSplit["artist"] != "0.85000000" || summary.MultiplierWeights["rare"] != "0.25000000" {
		t.Fatalf("unexpected maps: %+v", summary)
	}
	if len(summary.RarityCurve) != 2 || summary.RarityCurve[0] != 1 || summary.RarityCurve[1] != 2 {
		t.Fatalf("unexpected rarity curve: %+v", summary.RarityCurve)
	}
}

func TestGetEditionIDsByOriginalMapsArray(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewArray([]cadence.Value{
			cadence.NewUInt64(11),
			cadence.NewUInt64(12),
			cadence.NewUInt64(13),
		}),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	editionIDs, err := svc.GetEditionIDsByOriginal(context.Background(), 3)
	if err != nil {
		t.Fatalf("GetEditionIDsByOriginal returned error: %v", err)
	}
	if diff := cmp.Diff([]uint64{11, 12, 13}, editionIDs); diff != "" {
		t.Fatalf("unexpected edition ids (-want +got):\n%s", diff)
	}
	if len(txSvc.args) != 1 {
		t.Fatalf("expected one script argument, got %d", len(txSvc.args))
	}
	idArg, ok := txSvc.args[0].(cadence.UInt64)
	if !ok || uint64(idArg) != 3 {
		t.Fatalf("expected originalId argument 3, got %#v", txSvc.args[0])
	}
}

func TestGetEditionIDsByOriginalAllowsEmptyArray(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewArray([]cadence.Value{}),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	editionIDs, err := svc.GetEditionIDsByOriginal(context.Background(), 3)
	if err != nil {
		t.Fatalf("GetEditionIDsByOriginal returned error: %v", err)
	}
	if editionIDs == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(editionIDs) != 0 {
		t.Fatalf("expected empty slice, got %+v", editionIDs)
	}
}

func TestGetEditionIDsByOriginalRejectsUnexpectedType(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewOptional(nil),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.GetEditionIDsByOriginal(context.Background(), 3)
	if err == nil {
		t.Fatal("expected error for unexpected result type, got nil")
	}
}

// ---- mock transaction service for read-only queries ----

type queryTxService struct {
	scriptResult  cadence.Value
	scriptResults []cadence.Value
	args          []transactions.Argument
	calls         [][]transactions.Argument
	err           error
}

func (s *queryTxService) Create(ctx context.Context, sync bool, proposerAddress string, code string, args []transactions.Argument, tType transactions.Type) (*jobs.Job, *transactions.Transaction, error) {
	panic("not used by queries")
}

func (s *queryTxService) Sign(ctx context.Context, proposerAddress string, code string, args []transactions.Argument) (*transactions.SignedTransaction, error) {
	panic("not used by queries")
}

func (s *queryTxService) List(limit, offset int) ([]transactions.Transaction, error) {
	panic("not used by queries")
}

func (s *queryTxService) ListForAccount(tType transactions.Type, address string, limit, offset int) ([]transactions.Transaction, error) {
	panic("not used by queries")
}

func (s *queryTxService) Details(ctx context.Context, transactionId string) (*transactions.Transaction, error) {
	panic("not used by queries")
}

func (s *queryTxService) DetailsForAccount(ctx context.Context, tType transactions.Type, address, transactionId string) (*transactions.Transaction, error) {
	panic("not used by queries")
}

func (s *queryTxService) ExecuteScript(ctx context.Context, code string, args []transactions.Argument) (cadence.Value, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.args = args
	copiedArgs := append([]transactions.Argument(nil), args...)
	s.calls = append(s.calls, copiedArgs)
	if len(s.scriptResults) > 0 {
		result := s.scriptResults[0]
		s.scriptResults = s.scriptResults[1:]
		return result, nil
	}
	return s.scriptResult, nil
}

func (s *queryTxService) UpdateTransaction(t *transactions.Transaction) error {
	panic("not used by queries")
}

func (s *queryTxService) GetOrCreateTransaction(transactionId string) *transactions.Transaction {
	panic("not used by queries")
}

func (s *queryTxService) RegisterResultExtractor(tType transactions.Type, fn transactions.ResultExtractorFunc) {
}
