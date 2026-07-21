package artdrop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/flow-hydraulics/flow-wallet-api/errors"
	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	"github.com/gorilla/mux"
)

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

func (h *Handler) Release() http.Handler {
	return handlers.UseJson(http.HandlerFunc(h.ReleaseFunc))
}

func (h *Handler) ReleaseFunc(rw http.ResponseWriter, r *http.Request) {
	h.handleEscrowAction(rw, r, h.svc.Release)
}

func (h *Handler) Cancel() http.Handler {
	return handlers.UseJson(http.HandlerFunc(h.CancelFunc))
}

func (h *Handler) CancelFunc(rw http.ResponseWriter, r *http.Request) {
	h.handleEscrowAction(rw, r, h.svc.Cancel)
}

func (h *Handler) Refund() http.Handler {
	return handlers.UseJson(http.HandlerFunc(h.RefundFunc))
}

func (h *Handler) RefundFunc(rw http.ResponseWriter, r *http.Request) {
	h.handleEscrowAction(rw, r, h.svc.Refund)
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

func (h *Handler) GetOriginalExtendedSummary() http.Handler {
	return http.HandlerFunc(h.GetOriginalExtendedSummaryFunc)
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

func (h *Handler) GetOriginalExtendedSummaryFunc(rw http.ResponseWriter, r *http.Request) {
	origId, err := strconv.ParseUint(mux.Vars(r)["origId"], 10, 64)
	if err != nil {
		handlers.HandleError(rw, r, &errors.RequestError{
			StatusCode: http.StatusBadRequest,
			Err:        fmt.Errorf("invalid origId: %w", err),
		})
		return
	}

	summary, err := h.svc.GetOriginalExtendedSummary(r.Context(), origId)
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
	logicOwner := r.URL.Query().Get("logic_owner")
	if logicOwner == "" {
		handlers.HandleError(rw, r, &errors.RequestError{
			StatusCode: http.StatusBadRequest,
			Err:        fmt.Errorf("field 'logic_owner' is required"),
		})
		return
	}

	summary, err := h.svc.GetEscrow(r.Context(), logicOwner, escrowId)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	handlers.HandleJsonResponse(rw, http.StatusOK, summary)
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

func (h *Handler) handleEscrowAction(
	rw http.ResponseWriter,
	r *http.Request,
	action func(context.Context, bool, string, uint64, EscrowActionRequest) (*jobs.Job, *transactions.Transaction, error),
) {
	var req EscrowActionRequest
	if !h.decodeBody(rw, r, &req) {
		return
	}

	escrowId, ok := h.parseEscrowID(rw, r)
	if !ok {
		return
	}

	sync := r.FormValue(handlers.SyncQueryParameter) != ""
	job, tx, err := action(r.Context(), sync, mux.Vars(r)["address"], escrowId, req)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	h.handleTransactionResponse(rw, sync, job, tx)
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
