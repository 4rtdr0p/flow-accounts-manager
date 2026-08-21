package purchase

import (
	"net/http"
)

// Handler is the HTTP server for buyer purchase charge auditing.
type Handler struct {
	service Service
}

// NewHandler initiates a new purchase handler.
func NewHandler(service Service) *Handler {
	return &Handler{service}
}

// CreatePurchaseCharge handles POST /v1/purchases:charge. It charges a buyer's
// purchase and opens the escrow end-to-end: the server reads the artwork price
// from Mongo, applies the configured platform fee, converts the total to FLOW
// via the Pyth oracle, creates and confirms a Stripe PaymentIntent, opens the
// on-chain escrow with the server-computed FLOW amount, and persists the audit
// record. Every amount is computed server-side; the client only identifies the
// artwork, the parties and the payment details.
func (h *Handler) CreatePurchaseCharge() http.Handler {
	return http.HandlerFunc(h.CreatePurchaseChargeFunc)
}
