package artdrop

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/flow-hydraulics/flow-wallet-api/errors"
	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/flow-hydraulics/flow-wallet-api/handlers/middleware"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	"github.com/gorilla/mux"
)

// requireArtistSubject rejects the request if the caller's token identifies a
// specific subject (artist) that doesn't match the artistAddress in the path.
// These endpoints let an artist create their own Original/Edition; the
// on-chain transaction already ties identity to the signer and can't be
// forged, but without this check any caller holding the right scope could
// ask the wallet-api to sign on behalf of a *different* artist's custodial
// account. Tokens without a subject claim (e.g. service/admin tokens) are
// left untouched by this check, since this repo has no existing convention
// for what `sub` carries — confirm with whoever issues artist tokens that
// `sub` is set to the artist's own address for this to be effective.
func requireArtistSubject(r *http.Request, artistAddress string) error {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || claims.Subject == "" {
		return nil
	}
	if !strings.EqualFold(claims.Subject, artistAddress) {
		return &errors.RequestError{
			StatusCode: http.StatusForbidden,
			Err:        fmt.Errorf("token subject does not match artistAddress"),
		}
	}
	return nil
}

// Handler exposes HTTP endpoints for the artdrop plugin.
type Handler struct {
	svc *Service
}

// NewHandler creates a handler backed by the given artdrop service.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Transfer() http.Handler {
	return handlers.UseJson(http.HandlerFunc(h.TransferFunc))
}

func (h *Handler) TransferFunc(rw http.ResponseWriter, r *http.Request) {
	if r.Body == nil || r.Body == http.NoBody {
		handlers.HandleError(rw, r, handlers.EmptyBodyError)
		return
	}

	var req TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlers.HandleError(rw, r, &errors.RequestError{
			StatusCode: http.StatusBadRequest,
			Err:        fmt.Errorf("invalid body: %w", err),
		})
		return
	}

	if req.To == "" {
		handlers.HandleError(rw, r, &errors.RequestError{
			StatusCode: http.StatusBadRequest,
			Err:        fmt.Errorf("field 'to' is required"),
		})
		return
	}

	if req.CertificateID == nil {
		handlers.HandleError(rw, r, &errors.RequestError{
			StatusCode: http.StatusBadRequest,
			Err:        fmt.Errorf("field 'certificateId' is required"),
		})
		return
	}

	sync := r.FormValue(handlers.SyncQueryParameter) != ""
	job, tx, err := h.svc.Transfer(r.Context(), sync, mux.Vars(r)["address"], req)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	var res interface{}
	if sync {
		res = tx.ToJSONResponse()
	} else {
		res = job.ToJSONResponse()
	}

	handlers.HandleJsonResponse(rw, http.StatusCreated, res)
}

func (h *Handler) Setup() http.Handler {
	return http.HandlerFunc(h.SetupFunc)
}

func (h *Handler) SetupFunc(rw http.ResponseWriter, r *http.Request) {
	sync := r.FormValue(handlers.SyncQueryParameter) != ""
	job, transaction, err := h.svc.Setup(r.Context(), sync, mux.Vars(r)["address"])
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	var res interface{}
	if sync {
		res = transaction.ToJSONResponse()
	} else {
		res = job.ToJSONResponse()
	}

	handlers.HandleJsonResponse(rw, http.StatusCreated, res)
}

func (h *Handler) SetupArtistDirect() http.Handler {
	return http.HandlerFunc(h.SetupArtistDirectFunc)
}

func (h *Handler) SetupArtistDirectFunc(rw http.ResponseWriter, r *http.Request) {
	artistAddress := mux.Vars(r)["artistAddress"]
	if err := requireArtistSubject(r, artistAddress); err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	sync := r.FormValue(handlers.SyncQueryParameter) != ""
	job, transaction, err := h.svc.SetupArtistDirect(r.Context(), sync, artistAddress)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	var res interface{}
	if sync {
		res = transaction.ToJSONResponse()
	} else {
		res = job.ToJSONResponse()
	}

	handlers.HandleJsonResponse(rw, http.StatusCreated, res)
}

func (h *Handler) CreateOriginal() http.Handler {
	return handlers.UseJson(http.HandlerFunc(h.CreateOriginalFunc))
}

func (h *Handler) CreateOriginalFunc(rw http.ResponseWriter, r *http.Request) {
	artistAddress := mux.Vars(r)["artistAddress"]
	if err := requireArtistSubject(r, artistAddress); err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	var req CreateOriginalRequest
	if !h.decodeBody(rw, r, &req) {
		return
	}

	sync := r.FormValue(handlers.SyncQueryParameter) != ""
	job, tx, err := h.svc.CreateOriginal(r.Context(), sync, artistAddress, req)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	h.handleTransactionResponse(rw, sync, job, tx)
}

func (h *Handler) CreateEdition() http.Handler {
	return handlers.UseJson(http.HandlerFunc(h.CreateEditionFunc))
}

func (h *Handler) CreateEditionFunc(rw http.ResponseWriter, r *http.Request) {
	artistAddress := mux.Vars(r)["artistAddress"]
	if err := requireArtistSubject(r, artistAddress); err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	var req CreateEditionRequest
	if !h.decodeBody(rw, r, &req) {
		return
	}

	originalID, err := strconv.ParseUint(mux.Vars(r)["originalId"], 10, 64)
	if err != nil {
		handlers.HandleError(rw, r, &errors.RequestError{
			StatusCode: http.StatusBadRequest,
			Err:        fmt.Errorf("invalid originalId: %w", err),
		})
		return
	}

	sync := r.FormValue(handlers.SyncQueryParameter) != ""
	job, tx, err := h.svc.CreateEdition(r.Context(), sync, artistAddress, originalID, req)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	h.handleTransactionResponse(rw, sync, job, tx)
}

func (h *Handler) CreateEscrow() http.Handler {
	return handlers.UseJson(http.HandlerFunc(h.CreateEscrowFunc))
}

func (h *Handler) CreateEscrowFunc(rw http.ResponseWriter, r *http.Request) {
	var req CreateEscrowRequest
	if !h.decodeBody(rw, r, &req) {
		return
	}

	sync := r.FormValue(handlers.SyncQueryParameter) != ""
	job, tx, err := h.svc.CreateEscrow(r.Context(), sync, mux.Vars(r)["address"], req)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	h.handleTransactionResponse(rw, sync, job, tx)
}

func (h *Handler) ReEscrow() http.Handler {
	return handlers.UseJson(http.HandlerFunc(h.ReEscrowFunc))
}

func (h *Handler) ReEscrowFunc(rw http.ResponseWriter, r *http.Request) {
	var req ReEscrowRequest
	if !h.decodeBody(rw, r, &req) {
		return
	}

	sync := r.FormValue(handlers.SyncQueryParameter) != ""
	job, tx, err := h.svc.ReEscrow(r.Context(), sync, mux.Vars(r)["address"], req)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	h.handleTransactionResponse(rw, sync, job, tx)
}

func (h *Handler) VoidEscrow() http.Handler {
	return http.HandlerFunc(h.VoidEscrowFunc)
}

func (h *Handler) VoidEscrowFunc(rw http.ResponseWriter, r *http.Request) {
	escrowId, ok := h.parseEscrowID(rw, r)
	if !ok {
		return
	}

	sync := r.FormValue(handlers.SyncQueryParameter) != ""
	job, tx, err := h.svc.VoidEscrow(r.Context(), sync, escrowId)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	h.handleTransactionResponse(rw, sync, job, tx)
}

func (h *Handler) ActivateChip() http.Handler {
	return handlers.UseJson(http.HandlerFunc(h.ActivateChipFunc))
}

func (h *Handler) ActivateChipFunc(rw http.ResponseWriter, r *http.Request) {
	var req ActivateChipRequest
	if !h.decodeBody(rw, r, &req) {
		return
	}

	escrowId, ok := h.parseEscrowID(rw, r)
	if !ok {
		return
	}

	sync := r.FormValue(handlers.SyncQueryParameter) != ""
	job, tx, err := h.svc.ActivateChip(r.Context(), sync, mux.Vars(r)["address"], escrowId, req)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	h.handleTransactionResponse(rw, sync, job, tx)
}

func (h *Handler) ListCertificates() http.Handler {
	return http.HandlerFunc(h.ListCertificatesFunc)
}

func (h *Handler) ListCertificatesFunc(rw http.ResponseWriter, r *http.Request) {
	address := mux.Vars(r)["address"]

	certs, err := h.svc.ListCertificates(r.Context(), address)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	handlers.HandleJsonResponse(rw, http.StatusOK, certs)
}

func (h *Handler) GetCertificateDetail() http.Handler {
	return http.HandlerFunc(h.GetCertificateDetailFunc)
}

func (h *Handler) GetCertificateDetailFunc(rw http.ResponseWriter, r *http.Request) {
	certId, err := strconv.ParseUint(mux.Vars(r)["certId"], 10, 64)
	if err != nil {
		handlers.HandleError(rw, r, &errors.RequestError{
			StatusCode: http.StatusBadRequest,
			Err:        fmt.Errorf("invalid certId: %w", err),
		})
		return
	}

	detail, err := h.svc.GetCertificateDetail(r.Context(), mux.Vars(r)["address"], certId)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}
	if detail == nil {
		handlers.HandleError(rw, r, &errors.RequestError{
			StatusCode: http.StatusNotFound,
			Err:        fmt.Errorf("certificate not found"),
		})
		return
	}

	handlers.HandleJsonResponse(rw, http.StatusOK, detail)
}

func (h *Handler) GetCollectionLength() http.Handler {
	return http.HandlerFunc(h.GetCollectionLengthFunc)
}

func (h *Handler) GetCollectionLengthFunc(rw http.ResponseWriter, r *http.Request) {
	address := mux.Vars(r)["address"]

	length, err := h.svc.GetCollectionLength(r.Context(), address)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	handlers.HandleJsonResponse(rw, http.StatusOK, length)
}

func (h *Handler) GetOriginalSummary() http.Handler {
	return http.HandlerFunc(h.GetOriginalSummaryFunc)
}

func (h *Handler) GetOriginalSummaryFunc(rw http.ResponseWriter, r *http.Request) {
	origId, err := strconv.ParseUint(mux.Vars(r)["origId"], 10, 64)
	if err != nil {
		handlers.HandleError(rw, r, &errors.RequestError{
			StatusCode: http.StatusBadRequest,
			Err:        fmt.Errorf("invalid origId: %w", err),
		})
		return
	}

	summary, err := h.svc.GetOriginalSummary(r.Context(), origId)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}
	if summary == nil {
		handlers.HandleError(rw, r, &errors.RequestError{
			StatusCode: http.StatusNotFound,
			Err:        fmt.Errorf("original not found"),
		})
		return
	}

	handlers.HandleJsonResponse(rw, http.StatusOK, summary)
}

func (h *Handler) GetEditionSummary() http.Handler {
	return http.HandlerFunc(h.GetEditionSummaryFunc)
}

func (h *Handler) GetEditionIDsByOriginal() http.Handler {
	return http.HandlerFunc(h.GetEditionIDsByOriginalFunc)
}

func (h *Handler) GetEditionSummaryFunc(rw http.ResponseWriter, r *http.Request) {
	edId, err := strconv.ParseUint(mux.Vars(r)["edId"], 10, 64)
	if err != nil {
		handlers.HandleError(rw, r, &errors.RequestError{
			StatusCode: http.StatusBadRequest,
			Err:        fmt.Errorf("invalid edId: %w", err),
		})
		return
	}

	summary, err := h.svc.GetEditionSummary(r.Context(), edId)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}
	if summary == nil {
		handlers.HandleError(rw, r, &errors.RequestError{
			StatusCode: http.StatusNotFound,
			Err:        fmt.Errorf("edition not found"),
		})
		return
	}

	handlers.HandleJsonResponse(rw, http.StatusOK, summary)
}

func (h *Handler) GetEditionIDsByOriginalFunc(rw http.ResponseWriter, r *http.Request) {
	origId, err := strconv.ParseUint(mux.Vars(r)["origId"], 10, 64)
	if err != nil {
		handlers.HandleError(rw, r, &errors.RequestError{
			StatusCode: http.StatusBadRequest,
			Err:        fmt.Errorf("invalid origId: %w", err),
		})
		return
	}

	editionIDs, err := h.svc.GetEditionIDsByOriginal(r.Context(), origId)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	handlers.HandleJsonResponse(rw, http.StatusOK, editionIDs)
}

func (h *Handler) GetPlatformFee() http.Handler {
	return http.HandlerFunc(h.GetPlatformFeeFunc)
}

func (h *Handler) GetPlatformFeeFunc(rw http.ResponseWriter, r *http.Request) {
	fee, err := h.svc.GetPlatformFee(r.Context())
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}
	handlers.HandleJsonResponse(rw, http.StatusOK, fee)
}

func (h *Handler) GetMarketMode() http.Handler {
	return http.HandlerFunc(h.GetMarketModeFunc)
}

func (h *Handler) GetMarketModeFunc(rw http.ResponseWriter, r *http.Request) {
	mode, err := h.svc.GetMarketMode(r.Context())
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}
	handlers.HandleJsonResponse(rw, http.StatusOK, mode)
}

func (h *Handler) IsArtist() http.Handler {
	return http.HandlerFunc(h.IsArtistFunc)
}

func (h *Handler) IsArtistFunc(rw http.ResponseWriter, r *http.Request) {
	address := mux.Vars(r)["address"]

	is, err := h.svc.IsArtist(r.Context(), address)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	handlers.HandleJsonResponse(rw, http.StatusOK, map[string]bool{"isArtist": is})
}

func (h *Handler) GetEscrow() http.Handler {
	return http.HandlerFunc(h.GetEscrowFunc)
}

func (h *Handler) GetEscrowFunc(rw http.ResponseWriter, r *http.Request) {
	escrowId, ok := h.parseEscrowID(rw, r)
	if !ok {
		return
	}

	// logic_owner used to be a required query param, validated against the
	// caller's value and otherwise unused (get_escrow_summary.cdc never took
	// it as an argument). The EscrowModule owner is now a server-side config
	// value (see Config.LogicOwner), so the param is no longer required; it's
	// simply ignored if a caller still sends it, to avoid breaking them
	// mid-flight.
	summary, err := h.svc.GetEscrow(r.Context(), escrowId)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	handlers.HandleJsonResponse(rw, http.StatusOK, summary)
}

func (h *Handler) ListEscrows() http.Handler {
	return http.HandlerFunc(h.ListEscrowsFunc)
}

// ListEscrowsFunc lists the escrow ids opened for the account in the path
// (as buyer), via ArtDropRegistry.EscrowsByBuyerIndex. Pass ?expand=summary
// to additionally resolve each id to its full GetEscrow summary — omitted by
// default to avoid an N+1 script call per id on every request.
func (h *Handler) ListEscrowsFunc(rw http.ResponseWriter, r *http.Request) {
	address := mux.Vars(r)["address"]
	expand := r.URL.Query().Get("expand") == "summary"

	res, err := h.svc.ListEscrowsByBuyer(r.Context(), address, expand)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	handlers.HandleJsonResponse(rw, http.StatusOK, res)
}

func (h *Handler) ListEscrowsByEdition() http.Handler {
	return http.HandlerFunc(h.ListEscrowsByEditionFunc)
}

// ListEscrowsByEditionFunc lists the escrow ids opened against the edition
// in the path, via ArtDropRegistry.EscrowsByEditionIndex. Pass
// ?expand=summary for the same combined-script summary expansion as
// ListEscrowsFunc. The {address} path segment is unused — this query isn't
// account-scoped — kept only so the route lives alongside the other escrow
// endpoints under /accounts/{address}/artdrop/escrows/..., matching
// GetEscrowFunc's precedent (see its logic_owner comment).
func (h *Handler) ListEscrowsByEditionFunc(rw http.ResponseWriter, r *http.Request) {
	editionId, err := strconv.ParseUint(mux.Vars(r)["editionId"], 10, 64)
	if err != nil {
		handlers.HandleError(rw, r, &errors.RequestError{
			StatusCode: http.StatusBadRequest,
			Err:        fmt.Errorf("invalid editionId: %w", err),
		})
		return
	}
	expand := r.URL.Query().Get("expand") == "summary"

	res, err := h.svc.ListEscrowsByEdition(r.Context(), editionId, expand)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	handlers.HandleJsonResponse(rw, http.StatusOK, res)
}

func (h *Handler) ListEscrowsBySeller() http.Handler {
	return http.HandlerFunc(h.ListEscrowsBySellerFunc)
}

// ListEscrowsBySellerFunc lists every escrow whose seller field is
// literally the address in the path — see Service.ListEscrowsBySeller's
// doc comment for why this is a different question from
// ListEscrowsByArtistFunc below, and why this one is projection-served
// with no chain fallback (issue #102). Pass ?expand=summary for full
// summaries; omitted, the response carries only escrow_ids.
func (h *Handler) ListEscrowsBySellerFunc(rw http.ResponseWriter, r *http.Request) {
	address := mux.Vars(r)["address"]
	expand := r.URL.Query().Get("expand") == "summary"

	res, err := h.svc.ListEscrowsBySeller(r.Context(), address, expand)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	handlers.HandleJsonResponse(rw, http.StatusOK, res)
}

func (h *Handler) ListEscrowsByArtist() http.Handler {
	return http.HandlerFunc(h.ListEscrowsByArtistFunc)
}

// ListEscrowsByArtistFunc lists every escrow open against an edition
// created by the address in the path AS AN ARTIST — see
// Service.ListEscrowsByArtist and get_escrows_by_seller_expanded.cdc for
// the three-index walk (unchanged from issue #100; only this endpoint's
// name is new, issue #102 — see ListEscrowsBySeller's doc comment for why
// it was split off). Pass ?expand=summary for full summaries; omitted, the
// response carries only escrow_ids.
func (h *Handler) ListEscrowsByArtistFunc(rw http.ResponseWriter, r *http.Request) {
	address := mux.Vars(r)["address"]
	expand := r.URL.Query().Get("expand") == "summary"

	res, err := h.svc.ListEscrowsByArtist(r.Context(), address, expand)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	handlers.HandleJsonResponse(rw, http.StatusOK, res)
}

func (h *Handler) decodeBody(rw http.ResponseWriter, r *http.Request, dst interface{}) bool {
	if r.Body == nil || r.Body == http.NoBody {
		handlers.HandleError(rw, r, handlers.EmptyBodyError)
		return false
	}
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		handlers.HandleError(rw, r, &errors.RequestError{
			StatusCode: http.StatusBadRequest,
			Err:        fmt.Errorf("invalid body: %w", err),
		})
		return false
	}
	return true
}

func (h *Handler) parseEscrowID(rw http.ResponseWriter, r *http.Request) (uint64, bool) {
	escrowId, err := strconv.ParseUint(mux.Vars(r)["escrowId"], 10, 64)
	if err != nil {
		handlers.HandleError(rw, r, &errors.RequestError{
			StatusCode: http.StatusBadRequest,
			Err:        fmt.Errorf("invalid escrowId: %w", err),
		})
		return 0, false
	}
	return escrowId, true
}

func (h *Handler) handleTransactionResponse(rw http.ResponseWriter, sync bool, job *jobs.Job, tx *transactions.Transaction) {
	var res interface{}
	if sync {
		res = tx.ToJSONResponse()
	} else {
		res = job.ToJSONResponse()
	}

	handlers.HandleJsonResponse(rw, http.StatusCreated, res)
}
