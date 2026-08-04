package pricing

import (
	stdErrs "errors"
	"fmt"
	"net/http"

	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
	"github.com/flow-hydraulics/flow-wallet-api/errors"
	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	log "github.com/sirupsen/logrus"
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
		case stdErrs.Is(err, ErrPricingDisabled):
			handlers.HandleError(rw, r, &errors.RequestError{
				StatusCode: http.StatusServiceUnavailable,
				Err:        fmt.Errorf("studio pricing is disabled"),
			})
		case stdErrs.Is(err, datastoremongo.ErrNoActivePricing):
			handlers.HandleError(rw, r, &errors.RequestError{
				StatusCode: http.StatusNotFound,
				Err:        fmt.Errorf("active studio pricing configuration not found"),
			})
		default:
			// Log the underlying failure but never leak driver internals to the
			// client.
			log.WithFields(log.Fields{"error": err}).Warn("failed to read active studio pricing configuration")
			http.Error(rw, "internal server error", http.StatusInternalServerError)
		}
		return
	}

	handlers.HandleJsonResponse(rw, http.StatusOK, cfg)
}
