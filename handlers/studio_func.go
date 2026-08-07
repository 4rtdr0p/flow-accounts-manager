package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/flow-hydraulics/flow-wallet-api/errors"
	"github.com/flow-hydraulics/flow-wallet-api/studio"
)

// createStockRequestRequest is the body of POST /v1/stock-requests:create.
// It carries the identity of the user and quote, plus the charge details that
// the pricing engine (#70) produced at charge time.
type createStockRequestRequest struct {
	UserID              string `json:"userId"`
	QuoteID             string `json:"quoteId"`
	AmountCents         int64  `json:"amountCents"`
	Currency            string `json:"currency"`
	StripePaymentIntent string `json:"stripePaymentIntentId"`
	PricingHash         string `json:"pricingHash"`
	EngineVersion       string `json:"engineVersion"`
	Metadata            string `json:"metadata"`
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

	charge, err := s.service.RecordProductionCharge(studio.CreateProductionChargeInput{
		UserID:              req.UserID,
		QuoteID:             req.QuoteID,
		AmountCents:         req.AmountCents,
		Currency:            req.Currency,
		StripePaymentIntent: req.StripePaymentIntent,
		PricingHash:         req.PricingHash,
		EngineVersion:       req.EngineVersion,
		Metadata:            req.Metadata,
	})
	if err != nil {
		// A duplicate charge is a client error: the same payment intent was
		// already recorded, so the request is not new work.
		if err == studio.ErrChargeAlreadyRecorded {
			handleError(rw, r, &errors.RequestError{StatusCode: http.StatusConflict, Err: err})
			return
		}
		handleError(rw, r, err)
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
