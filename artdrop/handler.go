package artdrop

import (
	"net/http"
)

// Handler exposes HTTP endpoints for the artdrop plugin.
type Handler struct {
	svc *Service
}

// NewHandler creates a handler backed by the given artdrop service.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
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
