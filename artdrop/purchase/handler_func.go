package purchase

import (
	"encoding/json"
	stdErrors "errors"
	"net/http"

	"github.com/flow-hydraulics/flow-wallet-api/errors"
	"github.com/flow-hydraulics/flow-wallet-api/handlers"
)

// createPurchaseChargeRequest is the body of POST /v1/purchases:charge. It
// identifies the artwork, the parties and the Stripe payment details. The
// amount is NOT accepted from the client: the server reads the artwork price
// from Mongo, applies the configured platform fee, and converts the total to
// FLOW via the Pyth oracle.
type createPurchaseChargeRequest struct {
	UserID           string `json:"userId"`
	ArtworkKind      string `json:"artworkKind"`
	ArtworkID        string `json:"artworkId"`
	StripeCustomerID string `json:"stripeCustomerId"`
	PaymentMethodID  string `json:"paymentMethodId,omitempty"`
	Metadata         string `json:"metadata,omitempty"`

	// Escrow fields.
	Buyer     string  `json:"buyer"`
	Seller    string  `json:"seller"`
	EditionID uint64  `json:"editionId"`
	ChipID    string  `json:"chipId"`
	UnlockAt  float64 `json:"unlockAt"`
	Nonce     uint64  `json:"nonce"`
}

// CreatePurchaseChargeFunc handles POST /v1/purchases:charge.
func (h *Handler) CreatePurchaseChargeFunc(rw http.ResponseWriter, r *http.Request) {
	if r.Body == nil || r.Body == http.NoBody {
		handlers.HandleError(rw, r, handlers.EmptyBodyError)
		return
	}

	var req createPurchaseChargeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlers.HandleError(rw, r, handlers.InvalidBodyError)
		return
	}

	charge, err := h.service.CreatePurchaseCharge(r.Context(), CreatePurchaseChargeInput{
		UserID:           req.UserID,
		ArtworkKind:      ArtworkKind(req.ArtworkKind),
		ArtworkID:        req.ArtworkID,
		StripeCustomerID: req.StripeCustomerID,
		PaymentMethodID:  req.PaymentMethodID,
		IdempotencyKey:   r.Header.Get("Idempotency-Key"),
		Metadata:         req.Metadata,
		Buyer:            req.Buyer,
		Seller:           req.Seller,
		EditionID:        req.EditionID,
		ChipID:           req.ChipID,
		UnlockAt:         req.UnlockAt,
		Nonce:            req.Nonce,
	})
	if err != nil {
		switch {
		case stdErrors.Is(err, ErrChargeAlreadyRecorded):
			handlers.HandleError(rw, r, &errors.RequestError{StatusCode: http.StatusConflict, Err: err})
		case stdErrors.Is(err, ErrArtworkNotFound):
			handlers.HandleError(rw, r, &errors.RequestError{StatusCode: http.StatusNotFound, Err: err})
		case stdErrors.Is(err, ErrArtworkPriceMissing):
			handlers.HandleError(rw, r, &errors.RequestError{StatusCode: http.StatusUnprocessableEntity, Err: err})
		case stdErrors.Is(err, ErrPricingDisabled), stdErrors.Is(err, ErrOracleDisabled), stdErrors.Is(err, ErrStripeDisabled), stdErrors.Is(err, ErrEscrowDisabled):
			handlers.HandleError(rw, r, &errors.RequestError{StatusCode: http.StatusServiceUnavailable, Err: err})
		case stdErrors.Is(err, ErrOracleStale):
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
