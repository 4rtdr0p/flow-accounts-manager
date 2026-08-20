package studio

import (
	"encoding/json"
	stdErrors "errors"
	"fmt"
	"net/http"

	"github.com/flow-hydraulics/flow-wallet-api/errors"
	"github.com/flow-hydraulics/flow-wallet-api/handlers"
)

var errMissingUserID = fmt.Errorf("missing userId query parameter")

// createStockRequestRequest is the body of POST /v1/stock-requests:create. It
// identifies the user, the quote and the requested quantity, plus the Stripe
// payment details. The amount is NOT accepted from the client: the server
// recomputes the exact price from the quote's config snapshot and the active
// pricing rates at charge time.
type createStockRequestRequest struct {
	UserID            string `json:"userId"`
	QuoteID           string `json:"quoteId"`
	QuantityRequested int    `json:"quantityRequested"`
	StripeCustomerID  string `json:"stripeCustomerId"`
	PaymentMethodID   string `json:"paymentMethodId,omitempty"`
	Metadata          string `json:"metadata,omitempty"`
}

// CreateStockRequestFunc handles POST /v1/stock-requests:create.
func (h *Handler) CreateStockRequestFunc(rw http.ResponseWriter, r *http.Request) {
	if r.Body == nil || r.Body == http.NoBody {
		handlers.HandleError(rw, r, handlers.EmptyBodyError)
		return
	}

	var req createStockRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlers.HandleError(rw, r, handlers.InvalidBodyError)
		return
	}

	charge, err := h.service.CreateStockRequestCharge(r.Context(), CreateStockRequestChargeInput{
		UserID:           req.UserID,
		QuoteID:          req.QuoteID,
		Quantity:         req.QuantityRequested,
		StripeCustomerID: req.StripeCustomerID,
		PaymentMethodID:  req.PaymentMethodID,
		IdempotencyKey:   r.Header.Get("Idempotency-Key"),
		Metadata:         req.Metadata,
	})
	if err != nil {
		switch {
		case stdErrors.Is(err, ErrChargeAlreadyRecorded):
			handlers.HandleError(rw, r, &errors.RequestError{StatusCode: http.StatusConflict, Err: err})
		case stdErrors.Is(err, ErrQuoteNotFound):
			handlers.HandleError(rw, r, &errors.RequestError{StatusCode: http.StatusNotFound, Err: err})
		case stdErrors.Is(err, ErrPricingDisabled), stdErrors.Is(err, ErrStripeDisabled):
			handlers.HandleError(rw, r, &errors.RequestError{StatusCode: http.StatusServiceUnavailable, Err: err})
		case stdErrors.Is(err, ErrChargeRecordFailed):
			handlers.HandleError(rw, r, &errors.RequestError{StatusCode: http.StatusInternalServerError, Err: err})
		default:
			handlers.HandleError(rw, r, err)
		}
		return
	}

	handlers.HandleJsonResponse(rw, http.StatusCreated, charge)
}

// ListChargesFunc handles GET /v1/studio/charges?userId=...
func (h *Handler) ListChargesFunc(rw http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		handlers.HandleError(rw, r, &errors.RequestError{StatusCode: http.StatusBadRequest, Err: errMissingUserID})
		return
	}

	charges, err := h.service.ListProductionChargesByUser(userID)
	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	handlers.HandleJsonResponse(rw, http.StatusOK, charges)
}
