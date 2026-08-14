package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/flow-hydraulics/flow-wallet-api/errors"
	"github.com/flow-hydraulics/flow-wallet-api/studio"
)

// createStockRequestRequest is the body of POST /v1/stock-requests:create. It
// identifies the user, the quote and the requested quantity, plus the Stripe
// payment details. The amount is NOT accepted from the client: the server
// recomputes the exact price from the quote's config snapshot and the active
// pricing rates at charge time.
type createStockRequestRequest struct {
	UserID           string `json:"userId"`
	QuoteID          string `json:"quoteId"`
	QuantityRequested int    `json:"quantityRequested"`
	StripeCustomerID string `json:"stripeCustomerId"`
	PaymentMethodID  string `json:"paymentMethodId,omitempty"`
	Metadata         string `json:"metadata,omitempty"`
}

// CreateStockRequestFunc handles POST /v1/stock-requests:create.
func (s *Studio) CreateStockRequestFunc(rw http.ResponseWriter, r *http.Request) {
	if err := checkNonEmptyBody(r); err != nil {
		handleError(rw, r, err)
		return
	}

	var req createStockRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handleError(rw, r, InvalidBodyError)
		return
	}

	charge, err := s.service.CreateStockRequestCharge(r.Context(), studio.CreateStockRequestChargeInput{
		UserID:           req.UserID,
		QuoteID:          req.QuoteID,
		Quantity:         req.QuantityRequested,
		StripeCustomerID: req.StripeCustomerID,
		PaymentMethodID:  req.PaymentMethodID,
		IdempotencyKey:   r.Header.Get("Idempotency-Key"),
		Metadata:         req.Metadata,
	})
	if err != nil {
		switch err {
		case studio.ErrChargeAlreadyRecorded:
			handleError(rw, r, &errors.RequestError{StatusCode: http.StatusConflict, Err: err})
		case studio.ErrQuoteNotFound:
			handleError(rw, r, &errors.RequestError{StatusCode: http.StatusNotFound, Err: err})
		case studio.ErrPricingDisabled, studio.ErrStripeDisabled:
			handleError(rw, r, &errors.RequestError{StatusCode: http.StatusServiceUnavailable, Err: err})
		default:
			handleError(rw, r, err)
		}
		return
	}

	handleJsonResponse(rw, http.StatusCreated, charge)
}

// ListChargesFunc handles GET /v1/studio/charges?userId=...
func (s *Studio) ListChargesFunc(rw http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		handleError(rw, r, &errors.RequestError{StatusCode: http.StatusBadRequest, Err: errMissingUserID})
		return
	}

	charges, err := s.service.ListProductionChargesByUser(userID)
	if err != nil {
		handleError(rw, r, err)
		return
	}

	handleJsonResponse(rw, http.StatusOK, charges)
}
