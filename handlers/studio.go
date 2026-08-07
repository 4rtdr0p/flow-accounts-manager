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

// CreateStockRequest handles POST /v1/stock-requests:create. It records a
// production charge in the audit table. The actual price is computed by the
// pricing engine (#70) at charge time; this handler persists the resulting
// amount and its pricing hash.
func (s *Studio) CreateStockRequest() http.Handler {
	return http.HandlerFunc(s.CreateStockRequestFunc)
}

// ListCharges handles GET /v1/studio/charges. It returns the audit records
// for a user.
func (s *Studio) ListCharges() http.Handler {
	return http.HandlerFunc(s.ListChargesFunc)
}
