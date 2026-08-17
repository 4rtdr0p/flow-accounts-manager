package handlers

import (
	"net/http"

	"github.com/flow-hydraulics/flow-wallet-api/studio"
)

// Studio is the HTTP server for Studio production charge auditing.
type Studio struct {
	service studio.Service
}

// NewStudio initiates a new studio server.
func NewStudio(service studio.Service) *Studio {
	return &Studio{service}
}

// CreateStockRequest handles POST /v1/stock-requests:create. It charges a
// Studio stock request end-to-end: the server reads the quote's config
// snapshot from Mongo, recomputes the exact price with the active pricing
// rates, creates and confirms a Stripe PaymentIntent, and persists the audit
// record. The amount is never trusted from the client.
func (s *Studio) CreateStockRequest() http.Handler {
	return http.HandlerFunc(s.CreateStockRequestFunc)
}

// ListCharges handles GET /v1/studio/charges. It returns the audit records
// for a user.
func (s *Studio) ListCharges() http.Handler {
	return http.HandlerFunc(s.ListChargesFunc)
}
