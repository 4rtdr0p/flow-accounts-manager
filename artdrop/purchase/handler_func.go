package purchase

import (
	"encoding/json"
	stdErrors "errors"
	"io"
	"net/http"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop/authguard"
	"github.com/flow-hydraulics/flow-wallet-api/errors"
	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/gorilla/mux"
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
	ShippingCents    int64  `json:"shippingCents,omitempty"`

	// Escrow fields. UnlockAt is not accepted here (issue #111): it is
	// computed server-side as now() + Config.EscrowClaimWindowSeconds, never
	// trusted from the client — see CreatePurchaseChargeInput's doc comment.
	// An "unlockAt" key in the request body is simply ignored by the JSON
	// decoder.
	Buyer     string `json:"buyer"`
	Seller    string `json:"seller"`
	EditionID uint64 `json:"editionId"`
	ChipID    string `json:"chipId"`
	Nonce     uint64 `json:"nonce"`

	// CertificateID selects the create-vs-reescrow branch (issue #107):
	// omit/zero for a fresh purchase (mint a new certificate against
	// editionId), non-zero to re-offer an existing certificate whose prior
	// escrow was Voided. It does not let the client set the amount — only
	// which certificate is re-escrowed; the amount stays server-computed.
	CertificateID uint64 `json:"certificateId,omitempty"`
}

type openEscrowRequest struct {
	ChipID string `json:"chipId"`
	Nonce  uint64 `json:"nonce"`
}

// OpenEscrowFunc opens escrow for an already-paid obligation. Strict decoding
// makes every financial and identity field an allow-list violation.
func (h *Handler) OpenEscrowFunc(rw http.ResponseWriter, r *http.Request) {
	if r.Body == nil || r.Body == http.NoBody {
		handlers.HandleError(rw, r, handlers.EmptyBodyError)
		return
	}
	if r.Header.Get("Idempotency-Key") == "" {
		handlers.HandleError(rw, r, &errors.RequestError{StatusCode: http.StatusBadRequest, Err: ErrInvalidOpenEscrowInput})
		return
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req openEscrowRequest
	if err := dec.Decode(&req); err != nil {
		handlers.HandleError(rw, r, handlers.InvalidBodyError)
		return
	}
	var extra interface{}
	if err := dec.Decode(&extra); err != io.EOF {
		handlers.HandleError(rw, r, handlers.InvalidBodyError)
		return
	}
	charge, err := h.service.OpenEscrow(r.Context(), OpenEscrowInput{PurchaseID: mux.Vars(r)["purchaseId"], ChipID: req.ChipID, Nonce: req.Nonce, IdempotencyKey: r.Header.Get("Idempotency-Key")})
	if err != nil {
		switch {
		case stdErrors.Is(err, ErrInvalidOpenEscrowInput):
			handlers.HandleError(rw, r, &errors.RequestError{StatusCode: http.StatusBadRequest, Err: err})
		case stdErrors.Is(err, ErrPurchaseNotFound):
			handlers.HandleError(rw, r, &errors.RequestError{StatusCode: http.StatusNotFound, Err: err})
		case stdErrors.Is(err, ErrPurchaseNotOpenable):
			handlers.HandleError(rw, r, &errors.RequestError{StatusCode: http.StatusConflict, Err: err})
		case stdErrors.Is(err, ErrChipNotProvisioned):
			handlers.HandleError(rw, r, &errors.RequestError{StatusCode: http.StatusUnprocessableEntity, Err: err})
		case stdErrors.Is(err, ErrEscrowDisabled), stdErrors.Is(err, ErrEscrowUnavailable), stdErrors.Is(err, ErrChipUnavailable), stdErrors.Is(err, ErrChargeRecordFailed):
			handlers.HandleError(rw, r, &errors.RequestError{StatusCode: http.StatusServiceUnavailable, Err: err})
		default:
			handlers.HandleError(rw, r, err)
		}
		return
	}
	handlers.HandleJsonResponse(rw, http.StatusCreated, struct {
		PurchaseID  string `json:"purchaseId"`
		Status      string `json:"status"`
		EscrowJobID string `json:"escrowJobId"`
		ChipID      string `json:"chipId"`
	}{charge.PurchaseID, charge.Status, charge.EscrowJobID, charge.ChipID})
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

	// Bind the userId in the body to the authenticated token subject: an end
	// user may only charge a purchase for their own account (an operator uses
	// the on-behalf scope). See authguard.RequireUserSubject.
	if err := authguard.RequireUserSubject(r, req.UserID); err != nil {
		handlers.HandleError(rw, r, err)
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
		ShippingCents:    req.ShippingCents,
		Buyer:            req.Buyer,
		Seller:           req.Seller,
		EditionID:        req.EditionID,
		ChipID:           req.ChipID,
		Nonce:            req.Nonce,
		CertificateID:    req.CertificateID,
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
