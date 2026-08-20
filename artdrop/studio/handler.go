package studio

import (
	"net/http"
)

// Handler is the HTTP server for Studio production charge auditing.
type Handler struct {
	service Service
}

// NewHandler initiates a new studio handler.
func NewHandler(service Service) *Handler {
	return &Handler{service}
}

// CreateStockRequest handles POST /v1/stock-requests:create. It charges a
// Studio stock request end-to-end: the server reads the quote's config
// snapshot from Mongo, recomputes the exact price with the active pricing
// rates, creates and confirms a Stripe PaymentIntent, and persists the audit
// record. The amount is never trusted from the client.
func (h *Handler) CreateStockRequest() http.Handler {
	return http.HandlerFunc(h.CreateStockRequestFunc)
}

// ListCharges handles GET /v1/studio/charges. It returns the audit records
// for a user.
func (h *Handler) ListCharges() http.Handler {
	return http.HandlerFunc(h.ListChargesFunc)
}
