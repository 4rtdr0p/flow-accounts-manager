package artdrop

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/gorilla/mux"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
)

func TestListCertificatesHandlerReturnsOK(t *testing.T) {
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
				{Key: mustStr("id"), Value: cadence.NewUInt64(7)},
				{Key: mustStr("editionId"), Value: cadence.NewUInt64(1)},
				{Key: mustStr("serial"), Value: cadence.NewUInt64(1)},
				{Key: mustStr("isRevealed"), Value: cadence.NewBool(true)},
			}),
			cadence.NewDictionary([]cadence.KeyValuePair{
				{Key: mustStr("id"), Value: cadence.NewUInt64(13)},
				{Key: mustStr("editionId"), Value: cadence.NewUInt64(1)},
				{Key: mustStr("serial"), Value: cadence.NewUInt64(2)},
				{Key: mustStr("isRevealed"), Value: cadence.NewBool(false)},
			}),
		}),
	}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/accounts/0xf8d6e0586b0a20c7/artdrop/certificates", nil)
	req = mux.SetURLVars(req, map[string]string{"address": "0xf8d6e0586b0a20c7"})
	rw := httptest.NewRecorder()

	handler.ListCertificates().ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"id":7`) {
		t.Fatalf("expected response to contain certificate id 7, got %s", rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"id":13`) {
		t.Fatalf("expected response to contain certificate id 13, got %s", rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"is_revealed":true`) {
		t.Fatalf("expected response to contain is_revealed:true, got %s", rw.Body.String())
	}
}

func TestGetCertificateDetailHandlerReturnsOK(t *testing.T) {
	displayName, err := cadence.NewString("Certificate #7")
	if err != nil {
		t.Fatal(err)
	}
	txSvc := &queryTxService{
		scriptResult: cadence.NewOptional(cadence.NewDictionary([]cadence.KeyValuePair{
			{Key: cadence.String("id"), Value: cadence.NewUInt64(7)},
			{Key: cadence.String("baseTier"), Value: cadence.NewOptional(nil)},
			{Key: cadence.String("finalMultiplier"), Value: cadence.NewOptional(nil)},
			{Key: cadence.String("chipPubKey"), Value: cadence.NewArray([]cadence.Value{cadence.NewUInt8(1), cadence.NewUInt8(2)})},
			{Key: cadence.String("isRevealed"), Value: cadence.NewBool(false)},
			{Key: cadence.String("displayName"), Value: cadence.NewOptional(displayName)},
		})),
	}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/accounts/0xf8d6e0586b0a20c7/artdrop/certificates/7", nil)
	req = mux.SetURLVars(req, map[string]string{
		"address": "0xf8d6e0586b0a20c7",
		"certId":  "7",
	})
	rw := httptest.NewRecorder()

	handler.GetCertificateDetail().ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"id":7`) {
		t.Fatalf("expected response to contain certificate id 7, got %s", rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"displayName":"Certificate #7"`) {
		t.Fatalf("expected response to contain display name, got %s", rw.Body.String())
	}
}

func TestGetCertificateDetailHandlerReturnsNotFound(t *testing.T) {
	// Mirrors the live-testnet verification (issue #53): when the script
	// returns Optional(nil) — missing collection / wrong capability type /
	// empty collection / unknown cert id — the service returns (nil, nil)
	// and the handler answers 404. This is the deliberate behavior change
	// flagged in the commit message and the service doc-comment.
	txSvc := &queryTxService{scriptResult: cadence.NewOptional(nil)}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/accounts/0xf8d6e0586b0a20c7/artdrop/certificates/7", nil)
	req = mux.SetURLVars(req, map[string]string{
		"address": "0xf8d6e0586b0a20c7",
		"certId":  "7",
	})
	rw := httptest.NewRecorder()

	handler.GetCertificateDetail().ServeHTTP(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", rw.Code, rw.Body.String())
	}
}

func TestGetCertificateDetailHandlerRejectsInvalidCertId(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/accounts/0xf8d6e0586b0a20c7/artdrop/certificates/not-a-number", nil)
	req = mux.SetURLVars(req, map[string]string{
		"address": "0xf8d6e0586b0a20c7",
		"certId":  "not-a-number",
	})
	rw := httptest.NewRecorder()

	handler.GetCertificateDetail().ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for invalid certId, got %d: %s", rw.Code, rw.Body.String())
	}
}

func TestGetCertificateDetailHandlerRejectsInvalidAddress(t *testing.T) {
	txSvc := &queryTxService{}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/accounts/not-an-address/artdrop/certificates/7", nil)
	req = mux.SetURLVars(req, map[string]string{
		"address": "not-an-address",
		"certId":  "7",
	})
	rw := httptest.NewRecorder()

	handler.GetCertificateDetail().ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for invalid address, got %d: %s", rw.Code, rw.Body.String())
	}
}

func TestGetCertificateDetailHandlerPropagatesScriptError(t *testing.T) {
	txSvc := &queryTxService{err: errors.New("boom")}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/accounts/0xf8d6e0586b0a20c7/artdrop/certificates/7", nil)
	req = mux.SetURLVars(req, map[string]string{
		"address": "0xf8d6e0586b0a20c7",
		"certId":  "7",
	})
	rw := httptest.NewRecorder()

	handler.GetCertificateDetail().ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for script error, got %d: %s", rw.Code, rw.Body.String())
	}
}

func TestGetCollectionLengthHandlerReturnsOK(t *testing.T) {
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
				{Key: mustStr("id"), Value: cadence.NewUInt64(7)},
				{Key: mustStr("editionId"), Value: cadence.NewUInt64(1)},
				{Key: mustStr("serial"), Value: cadence.NewUInt64(1)},
				{Key: mustStr("isRevealed"), Value: cadence.NewBool(true)},
			}),
			cadence.NewDictionary([]cadence.KeyValuePair{
				{Key: mustStr("id"), Value: cadence.NewUInt64(13)},
				{Key: mustStr("editionId"), Value: cadence.NewUInt64(1)},
				{Key: mustStr("serial"), Value: cadence.NewUInt64(2)},
				{Key: mustStr("isRevealed"), Value: cadence.NewBool(false)},
			}),
		}),
	}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/accounts/0xf8d6e0586b0a20c7/artdrop/collection-length", nil)
	req = mux.SetURLVars(req, map[string]string{"address": "0xf8d6e0586b0a20c7"})
	rw := httptest.NewRecorder()

	handler.GetCollectionLength().ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"length":2`) {
		t.Fatalf("expected response to contain length 2, got %s", rw.Body.String())
	}
}

func TestGetCollectionLengthHandlerReturnsZero(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewArray([]cadence.Value{}),
	}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/accounts/0xf8d6e0586b0a20c7/artdrop/collection-length", nil)
	req = mux.SetURLVars(req, map[string]string{"address": "0xf8d6e0586b0a20c7"})
	rw := httptest.NewRecorder()

	handler.GetCollectionLength().ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"length":0`) {
		t.Fatalf("expected response to contain length 0, got %s", rw.Body.String())
	}
}

func TestGetCollectionLengthHandlerRejectsInvalidAddress(t *testing.T) {
	txSvc := &queryTxService{}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/accounts/not-an-address/artdrop/collection-length", nil)
	req = mux.SetURLVars(req, map[string]string{"address": "not-an-address"})
	rw := httptest.NewRecorder()

	handler.GetCollectionLength().ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rw.Code, rw.Body.String())
	}
}

func TestIsArtistHandlerReturnsTrue(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewBool(true),
	}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/accounts/0xf8d6e0586b0a20c7/artdrop/is-artist", nil)
	req = mux.SetURLVars(req, map[string]string{"address": "0xf8d6e0586b0a20c7"})
	rw := httptest.NewRecorder()

	handler.IsArtist().ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"isArtist":true`) {
		t.Fatalf("expected response to contain isArtist:true, got %s", rw.Body.String())
	}
}

func TestIsArtistHandlerReturnsFalse(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewBool(false),
	}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/accounts/0xf8d6e0586b0a20c7/artdrop/is-artist", nil)
	req = mux.SetURLVars(req, map[string]string{"address": "0xf8d6e0586b0a20c7"})
	rw := httptest.NewRecorder()

	handler.IsArtist().ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"isArtist":false`) {
		t.Fatalf("expected response to contain isArtist:false, got %s", rw.Body.String())
	}
}

func TestIsArtistHandlerRejectsInvalidAddress(t *testing.T) {
	txSvc := &queryTxService{}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/accounts/not-an-address/artdrop/is-artist", nil)
	req = mux.SetURLVars(req, map[string]string{"address": "not-an-address"})
	rw := httptest.NewRecorder()

	handler.IsArtist().ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rw.Code, rw.Body.String())
	}
}

// TestGetEscrowHandlerReturnsOK also covers the removal of the logic_owner
// query param: it used to be required (validated, but never actually used
// to build the script call — get_escrow_summary.cdc never took it as an
// argument), and is gone now. This request carries no query string at all.
// TestGetEscrowHandlerReturnsOK also covers issue #98: the response now
// carries the full EscrowSummary field set, not just id/status.
func TestGetEscrowHandlerReturnsOK(t *testing.T) {
	unlockAt, err := cadence.NewUFix64("4102444800.00000000")
	if err != nil {
		t.Fatal(err)
	}
	txSvc := &queryTxService{
		scriptResult: cadence.NewOptional(cadence.NewDictionary([]cadence.KeyValuePair{
			{Key: cadence.String("id"), Value: cadence.NewUInt64(42)},
			{Key: cadence.String("buyer"), Value: cadence.NewAddress(flow.HexToAddress("0x179b6b1cb6755e31"))},
			{Key: cadence.String("seller"), Value: cadence.NewAddress(flow.HexToAddress("0xf3fcd2c1a78f5eee"))},
			{Key: cadence.String("editionId"), Value: cadence.NewUInt64(7)},
			{Key: cadence.String("chipId"), Value: cadence.String("chip-1")},
			{Key: cadence.String("unlockAt"), Value: unlockAt},
			{Key: cadence.String("nonce"), Value: cadence.NewUInt64(1)},
			{Key: cadence.String("certificateId"), Value: cadence.NewUInt64(99)},
			{Key: cadence.String("status"), Value: cadence.NewUInt8(2)},
			{Key: cadence.String("releaseReason"), Value: cadence.NewOptional(nil)},
			{Key: cadence.String("claimed"), Value: cadence.NewBool(false)},
			{Key: cadence.String("claimedAt"), Value: cadence.NewOptional(nil)},
		})),
	}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/accounts/0xf8d6e0586b0a20c7/artdrop/escrows/42", nil)
	req = mux.SetURLVars(req, map[string]string{
		"address":  "0xf8d6e0586b0a20c7",
		"escrowId": "42",
	})
	rw := httptest.NewRecorder()

	handler.GetEscrow().ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"id":42`) {
		t.Fatalf("expected response to contain escrow id 42, got %s", rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"status":2`) {
		t.Fatalf("expected response to contain status 2, got %s", rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"buyer":"0x179b6b1cb6755e31"`) {
		t.Fatalf("expected response to contain buyer, got %s", rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"certificate_id":99`) {
		t.Fatalf("expected response to contain certificate_id 99, got %s", rw.Body.String())
	}
}

func TestGetEscrowHandlerRejectsInvalidEscrowId(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/accounts/0xf8d6e0586b0a20c7/artdrop/escrows/abc", nil)
	req = mux.SetURLVars(req, map[string]string{
		"address":  "0xf8d6e0586b0a20c7",
		"escrowId": "abc",
	})
	rw := httptest.NewRecorder()

	handler.GetEscrow().ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for invalid escrowId, got %d: %s", rw.Code, rw.Body.String())
	}
}

// TestListEscrowsHandlerReturnsIds covers issue #98's listing endpoint
// without ?expand — the response carries only escrow_ids.
func TestListEscrowsHandlerReturnsIds(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewArray([]cadence.Value{
			cadence.NewUInt64(7),
			cadence.NewUInt64(9),
		}),
	}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/accounts/0xf8d6e0586b0a20c7/artdrop/escrows", nil)
	req = mux.SetURLVars(req, map[string]string{"address": "0xf8d6e0586b0a20c7"})
	rw := httptest.NewRecorder()

	handler.ListEscrows().ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"escrow_ids":[7,9]`) {
		t.Fatalf("expected response to contain escrow_ids [7,9], got %s", rw.Body.String())
	}
	if strings.Contains(rw.Body.String(), `"escrows"`) {
		t.Fatalf("expected no expanded escrows without ?expand=summary, got %s", rw.Body.String())
	}
}

// TestListEscrowsHandlerExpandsSummaries covers ?expand=summary. The second
// script call returns get_escrows_by_buyer_expanded.cdc's shape — a
// non-optional array of summary dicts, resolved in one combined call
// instead of one GetEscrow-shaped call per id (issue #100).
func TestListEscrowsHandlerExpandsSummaries(t *testing.T) {
	unlockAt, err := cadence.NewUFix64("4102444800.00000000")
	if err != nil {
		t.Fatal(err)
	}
	summaryDict := cadence.NewDictionary([]cadence.KeyValuePair{
		{Key: cadence.String("id"), Value: cadence.NewUInt64(7)},
		{Key: cadence.String("buyer"), Value: cadence.NewAddress(flow.HexToAddress("0x179b6b1cb6755e31"))},
		{Key: cadence.String("seller"), Value: cadence.NewAddress(flow.HexToAddress("0xf3fcd2c1a78f5eee"))},
		{Key: cadence.String("editionId"), Value: cadence.NewUInt64(42)},
		{Key: cadence.String("chipId"), Value: cadence.String("chip-1")},
		{Key: cadence.String("unlockAt"), Value: unlockAt},
		{Key: cadence.String("nonce"), Value: cadence.NewUInt64(1)},
		{Key: cadence.String("certificateId"), Value: cadence.NewUInt64(99)},
		{Key: cadence.String("status"), Value: cadence.NewUInt8(0)},
		{Key: cadence.String("releaseReason"), Value: cadence.NewOptional(nil)},
		{Key: cadence.String("claimed"), Value: cadence.NewBool(false)},
		{Key: cadence.String("claimedAt"), Value: cadence.NewOptional(nil)},
	})
	txSvc := &queryTxService{
		scriptResults: []cadence.Value{
			cadence.NewArray([]cadence.Value{cadence.NewUInt64(7)}),
			cadence.NewArray([]cadence.Value{summaryDict}),
		},
	}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/accounts/0xf8d6e0586b0a20c7/artdrop/escrows?expand=summary", nil)
	req = mux.SetURLVars(req, map[string]string{"address": "0xf8d6e0586b0a20c7"})
	rw := httptest.NewRecorder()

	handler.ListEscrows().ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"escrows"`) {
		t.Fatalf("expected expanded escrows with ?expand=summary, got %s", rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"certificate_id":99`) {
		t.Fatalf("expected expanded escrow to contain certificate_id 99, got %s", rw.Body.String())
	}
}

// TestListEscrowsByEditionHandlerReturnsIds covers the by-edition listing
// endpoint added for issue #100, without ?expand.
func TestListEscrowsByEditionHandlerReturnsIds(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewArray([]cadence.Value{
			cadence.NewUInt64(11),
			cadence.NewUInt64(12),
		}),
	}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/accounts/0xf8d6e0586b0a20c7/artdrop/escrows/by-edition/42", nil)
	req = mux.SetURLVars(req, map[string]string{
		"address":   "0xf8d6e0586b0a20c7",
		"editionId": "42",
	})
	rw := httptest.NewRecorder()

	handler.ListEscrowsByEdition().ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"escrow_ids":[11,12]`) {
		t.Fatalf("expected response to contain escrow_ids [11,12], got %s", rw.Body.String())
	}
}

// TestListEscrowsByEditionHandlerRejectsInvalidEditionId mirrors
// TestGetEscrowHandlerRejectsInvalidEscrowId for the editionId path param.
func TestListEscrowsByEditionHandlerRejectsInvalidEditionId(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/accounts/0xf8d6e0586b0a20c7/artdrop/escrows/by-edition/abc", nil)
	req = mux.SetURLVars(req, map[string]string{
		"address":   "0xf8d6e0586b0a20c7",
		"editionId": "abc",
	})
	rw := httptest.NewRecorder()

	handler.ListEscrowsByEdition().ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for invalid editionId, got %d: %s", rw.Code, rw.Body.String())
	}
}

// TestListEscrowsBySellerHandlerReturnsIds covers the by-seller listing
// endpoint added for issue #100. Unlike by-edition/by-buyer, a single
// script call always resolves full summaries; without ?expand the handler
// still reports only escrow_ids.
func TestListEscrowsBySellerHandlerReturnsIds(t *testing.T) {
	unlockAt, err := cadence.NewUFix64("4102444800.00000000")
	if err != nil {
		t.Fatal(err)
	}
	txSvc := &queryTxService{
		scriptResult: cadence.NewArray([]cadence.Value{
			cadence.NewDictionary([]cadence.KeyValuePair{
				{Key: cadence.String("id"), Value: cadence.NewUInt64(21)},
				{Key: cadence.String("buyer"), Value: cadence.NewAddress(flow.HexToAddress("0x179b6b1cb6755e31"))},
				{Key: cadence.String("seller"), Value: cadence.NewAddress(flow.HexToAddress("0xf3fcd2c1a78f5eee"))},
				{Key: cadence.String("editionId"), Value: cadence.NewUInt64(5)},
				{Key: cadence.String("chipId"), Value: cadence.String("chip-1")},
				{Key: cadence.String("unlockAt"), Value: unlockAt},
				{Key: cadence.String("nonce"), Value: cadence.NewUInt64(1)},
				{Key: cadence.String("certificateId"), Value: cadence.NewUInt64(60)},
				{Key: cadence.String("status"), Value: cadence.NewUInt8(0)},
				{Key: cadence.String("releaseReason"), Value: cadence.NewOptional(nil)},
				{Key: cadence.String("claimed"), Value: cadence.NewBool(false)},
				{Key: cadence.String("claimedAt"), Value: cadence.NewOptional(nil)},
			}),
		}),
	}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/accounts/0xf8d6e0586b0a20c7/artdrop/escrows/by-seller", nil)
	req = mux.SetURLVars(req, map[string]string{"address": "0xf8d6e0586b0a20c7"})
	rw := httptest.NewRecorder()

	handler.ListEscrowsBySeller().ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"escrow_ids":[21]`) {
		t.Fatalf("expected response to contain escrow_ids [21], got %s", rw.Body.String())
	}
	if strings.Contains(rw.Body.String(), `"escrows"`) {
		t.Fatalf("expected no expanded escrows without ?expand=summary, got %s", rw.Body.String())
	}
}

func TestGetOriginalSummaryHandlerReturnsOK(t *testing.T) {
	displayName, err := cadence.NewString("Ariel Artist")
	if err != nil {
		t.Fatal(err)
	}
	txSvc := &queryTxService{
		scriptResult: cadence.NewOptional(cadence.NewDictionary([]cadence.KeyValuePair{
			{Key: cadence.String("id"), Value: cadence.NewUInt64(3)},
			{Key: cadence.String("artist"), Value: cadence.NewAddress(flow.HexToAddress("0xf8d6e0586b0a20c7"))},
			{Key: cadence.String("name"), Value: cadence.String("Original")},
			{Key: cadence.String("createdAtBlock"), Value: cadence.NewUInt64(100)},
			{Key: cadence.String("schemaVersion"), Value: cadence.NewUInt8(2)},
			{Key: cadence.String("editionCount"), Value: cadence.NewUInt64(5)},
			{Key: cadence.String("totalMintedAcrossEditions"), Value: cadence.NewUInt64(42)},
			{Key: cadence.String("displayName"), Value: cadence.NewOptional(displayName)},
		})),
	}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/artdrop/originals/3", nil)
	req = mux.SetURLVars(req, map[string]string{"origId": "3"})
	rw := httptest.NewRecorder()

	handler.GetOriginalSummary().ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"editionCount":5`) {
		t.Fatalf("expected response to contain editionCount 5, got %s", rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"totalMintedAcrossEditions":42`) {
		t.Fatalf("expected response to contain total minted 42, got %s", rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"displayName":"Ariel Artist"`) {
		t.Fatalf("expected response to contain displayName, got %s", rw.Body.String())
	}
}

func TestGetOriginalSummaryHandlerRejectsInvalidOriginalId(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/artdrop/originals/not-a-number", nil)
	req = mux.SetURLVars(req, map[string]string{"origId": "not-a-number"})
	rw := httptest.NewRecorder()

	handler.GetOriginalSummary().ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for invalid origId, got %d: %s", rw.Code, rw.Body.String())
	}
}

func TestGetOriginalSummaryHandlerReturnsNotFound(t *testing.T) {
	txSvc := &queryTxService{scriptResult: cadence.NewOptional(nil)}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/artdrop/originals/3", nil)
	req = mux.SetURLVars(req, map[string]string{"origId": "3"})
	rw := httptest.NewRecorder()

	handler.GetOriginalSummary().ServeHTTP(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", rw.Code, rw.Body.String())
	}
}

func TestGetEditionIDsByOriginalHandlerReturnsOK(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewArray([]cadence.Value{
			cadence.NewUInt64(11),
			cadence.NewUInt64(12),
		}),
	}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/artdrop/originals/3/edition-ids", nil)
	req = mux.SetURLVars(req, map[string]string{"origId": "3"})
	rw := httptest.NewRecorder()

	handler.GetEditionIDsByOriginal().ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if got := strings.TrimSpace(rw.Body.String()); got != "[11,12]" {
		t.Fatalf("expected [11,12], got %s", got)
	}
}

func TestGetEditionIDsByOriginalHandlerRejectsInvalidOriginalId(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/artdrop/originals/not-a-number/edition-ids", nil)
	req = mux.SetURLVars(req, map[string]string{"origId": "not-a-number"})
	rw := httptest.NewRecorder()

	handler.GetEditionIDsByOriginal().ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for invalid origId, got %d: %s", rw.Code, rw.Body.String())
	}
}

func TestGetEditionIDsByOriginalHandlerReturnsEmptyArray(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewArray([]cadence.Value{}),
	}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/artdrop/originals/3/edition-ids", nil)
	req = mux.SetURLVars(req, map[string]string{"origId": "3"})
	rw := httptest.NewRecorder()

	handler.GetEditionIDsByOriginal().ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if got := strings.TrimSpace(rw.Body.String()); got != "[]" {
		t.Fatalf("expected [], got %s", got)
	}
}

func TestGetEditionSummaryHandlerReturnsOK(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewOptional(cadence.NewDictionary([]cadence.KeyValuePair{
			{Key: cadence.String("id"), Value: cadence.NewUInt64(4)},
			{Key: cadence.String("originalId"), Value: cadence.NewUInt64(3)},
			{Key: cadence.String("artist"), Value: cadence.NewAddress(flow.HexToAddress("0xf8d6e0586b0a20c7"))},
			{Key: cadence.String("shuffleSeedBlock"), Value: cadence.NewUInt64(99)},
			{Key: cadence.String("reprintLimit"), Value: cadence.NewUInt64(500)},
			{Key: cadence.String("maxSupply"), Value: cadence.NewUInt64(500)},
			{Key: cadence.String("createdAtBlock"), Value: cadence.NewUInt64(101)},
			{Key: cadence.String("schemaVersion"), Value: cadence.NewUInt8(2)},
			{Key: cadence.String("state"), Value: cadence.NewUInt8(3)},
			{Key: cadence.String("totalMinted"), Value: cadence.NewUInt64(9)},
		})),
	}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/artdrop/editions/4", nil)
	req = mux.SetURLVars(req, map[string]string{"edId": "4"})
	rw := httptest.NewRecorder()

	handler.GetEditionSummary().ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"id":4`) {
		t.Fatalf("expected response to contain id 4, got %s", rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"state":"3"`) {
		t.Fatalf("expected response to contain mapped state, got %s", rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"totalMinted":9`) {
		t.Fatalf("expected response to contain totalMinted 9, got %s", rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"maxSupply":500`) {
		t.Fatalf("expected response to contain maxSupply 500, got %s", rw.Body.String())
	}
}

func TestGetEditionSummaryHandlerRejectsInvalidEditionId(t *testing.T) {
	handler := NewHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/artdrop/editions/not-a-number", nil)
	req = mux.SetURLVars(req, map[string]string{"edId": "not-a-number"})
	rw := httptest.NewRecorder()

	handler.GetEditionSummary().ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 for invalid edId, got %d: %s", rw.Code, rw.Body.String())
	}
}

func TestGetEditionSummaryHandlerReturnsNotFound(t *testing.T) {
	txSvc := &queryTxService{scriptResult: cadence.NewOptional(nil)}
	handler := NewHandler(mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	}))

	req := httptest.NewRequest(http.MethodGet, "/artdrop/editions/4", nil)
	req = mux.SetURLVars(req, map[string]string{"edId": "4"})
	rw := httptest.NewRecorder()

	handler.GetEditionSummary().ServeHTTP(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", rw.Code, rw.Body.String())
	}
}
