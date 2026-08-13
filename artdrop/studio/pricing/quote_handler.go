package pricing

import (
	"encoding/json"
	stdErrs "errors"
	"fmt"
	"net/http"

	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
	"github.com/flow-hydraulics/flow-wallet-api/errors"
	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	log "github.com/sirupsen/logrus"
)

// QuoteHandler exposes the studio quote endpoint (POST /studio/quotes:price).
type QuoteHandler struct {
	svc *QuoteService
}

// NewQuoteHandler creates a handler backed by the given quote service.
func NewQuoteHandler(svc *QuoteService) *QuoteHandler {
	return &QuoteHandler{svc: svc}
}

// Quote returns the price snapshot for a Studio Wizard config.
func (h *QuoteHandler) Quote() http.Handler {
	return http.HandlerFunc(h.QuoteFunc)
}

func (h *QuoteHandler) QuoteFunc(rw http.ResponseWriter, r *http.Request) {
	var cfg Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		handlers.HandleError(rw, r, &errors.RequestError{
			StatusCode: http.StatusBadRequest,
			Err:        fmt.Errorf("invalid quote config: %w", err),
		})
		return
	}

	result, err := h.svc.Quote(r.Context(), cfg)
	if err != nil {
		switch {
		case stdErrs.Is(err, ErrInvalidQuoteConfig):
			handlers.HandleError(rw, r, &errors.RequestError{
				StatusCode: http.StatusBadRequest,
				Err:        err,
			})
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
			log.WithFields(log.Fields{"error": err}).Warn("failed to compute studio quote")
			http.Error(rw, "internal server error", http.StatusInternalServerError)
		}
		return
	}

	handlers.HandleJsonResponse(rw, http.StatusOK, result)
}
