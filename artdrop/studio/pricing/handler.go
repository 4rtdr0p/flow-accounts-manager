package pricing

import (
	"fmt"
	"net/http"

	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
	"github.com/flow-hydraulics/flow-wallet-api/errors"
	"github.com/flow-hydraulics/flow-wallet-api/handlers"
)

// Handler exposes HTTP endpoints for studio pricing.
type Handler struct {
	svc *ActiveService
}

// NewHandler creates a handler backed by the given studio pricing service.
func NewHandler(svc *ActiveService) *Handler {
	return &Handler{svc: svc}
}

// GetActive returns the active studio-printing pricing configuration.
func (h *Handler) GetActive() http.Handler {
	return http.HandlerFunc(h.GetActiveFunc)
}

func (h *Handler) GetActiveFunc(rw http.ResponseWriter, r *http.Request) {
	cfg, err := h.svc.Get(r.Context())
	if err != nil {
		switch {
		case err == ErrPricingDisabled:
			handlers.HandleError(rw, r, &errors.RequestError{
				StatusCode: http.StatusServiceUnavailable,
				Err:        fmt.Errorf("studio pricing is disabled: %w", err),
			})
		case err == datastoremongo.ErrNoActivePricing:
			handlers.HandleError(rw, r, &errors.RequestError{
				StatusCode: http.StatusNotFound,
				Err:        fmt.Errorf("active studio pricing configuration not found"),
			})
		default:
			handlers.HandleError(rw, r, &errors.RequestError{
				StatusCode: http.StatusInternalServerError,
				Err:        fmt.Errorf("read active studio pricing configuration: %w", err),
			})
		}
		return
	}

	handlers.HandleJsonResponse(rw, http.StatusOK, cfg)
}
