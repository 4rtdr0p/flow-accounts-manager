package artdrop

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/flow-hydraulics/flow-wallet-api/errors"
	"github.com/flow-hydraulics/flow-wallet-api/handlers"
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

	if req.CertificateID == 0 {
		handlers.HandleError(rw, r, &errors.RequestError{
			StatusCode: http.StatusBadRequest,
			Err:        fmt.Errorf("field 'certificateId' must be a non-zero positive integer"),
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
	http.Error(rw, "artdrop: not implemented", http.StatusNotImplemented)
}

func (h *Handler) CreateEscrow() http.Handler {
	return http.HandlerFunc(h.CreateEscrowFunc)
}

func (h *Handler) CreateEscrowFunc(rw http.ResponseWriter, r *http.Request) {
	http.Error(rw, "artdrop: not implemented", http.StatusNotImplemented)
}

func (h *Handler) ActivateChip() http.Handler {
	return http.HandlerFunc(h.ActivateChipFunc)
}

func (h *Handler) ActivateChipFunc(rw http.ResponseWriter, r *http.Request) {
	http.Error(rw, "artdrop: not implemented", http.StatusNotImplemented)
}

func (h *Handler) Release() http.Handler {
	return http.HandlerFunc(h.ReleaseFunc)
}

func (h *Handler) ReleaseFunc(rw http.ResponseWriter, r *http.Request) {
	http.Error(rw, "artdrop: not implemented", http.StatusNotImplemented)
}

func (h *Handler) Cancel() http.Handler {
	return http.HandlerFunc(h.CancelFunc)
}

func (h *Handler) CancelFunc(rw http.ResponseWriter, r *http.Request) {
	http.Error(rw, "artdrop: not implemented", http.StatusNotImplemented)
}

func (h *Handler) Refund() http.Handler {
	return http.HandlerFunc(h.RefundFunc)
}

func (h *Handler) RefundFunc(rw http.ResponseWriter, r *http.Request) {
	http.Error(rw, "artdrop: not implemented", http.StatusNotImplemented)
}

func (h *Handler) ListCertificates() http.Handler {
	return http.HandlerFunc(h.ListCertificatesFunc)
}

func (h *Handler) ListCertificatesFunc(rw http.ResponseWriter, r *http.Request) {
	http.Error(rw, "artdrop: not implemented", http.StatusNotImplemented)
}

func (h *Handler) GetEscrow() http.Handler {
	return http.HandlerFunc(h.GetEscrowFunc)
}

func (h *Handler) GetEscrowFunc(rw http.ResponseWriter, r *http.Request) {
	http.Error(rw, "artdrop: not implemented", http.StatusNotImplemented)
}
