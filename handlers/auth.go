package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/flow-hydraulics/flow-wallet-api/auth/exchange"
)

// AuthExchange serves the token-exchange endpoint (POST /v1/auth/token). It is
// auth-exempt (see middleware.authExemptPaths): its authentication IS the
// Payload assertion in the request body, not a wallet bearer token.
type AuthExchange struct {
	exchanger *exchange.Exchanger
}

func NewAuthExchange(exchanger *exchange.Exchanger) *AuthExchange {
	return &AuthExchange{exchanger: exchanger}
}

type tokenExchangeRequest struct {
	Assertion string `json:"assertion"`
}

type tokenExchangeResponse struct {
	Token     string `json:"token"`
	TokenType string `json:"token_type"`
	ExpiresIn int    `json:"expires_in"`
}

// TokenExchange verifies a Payload identity assertion and returns a wallet
// access token carrying the scopes the wallet policy grants the asserted role.
func (h *AuthExchange) TokenExchange() http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		// Report "not configured" before touching the body: a 503 here must not
		// depend on the caller sending a well-formed assertion request.
		if !h.exchanger.Configured() {
			http.Error(rw, exchange.ErrNotConfigured.Error(), http.StatusServiceUnavailable)
			return
		}

		var req tokenExchangeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(rw, "invalid request body", http.StatusBadRequest)
			return
		}

		result, err := h.exchanger.Exchange(req.Assertion)
		if err != nil {
			writeExchangeError(rw, err)
			return
		}

		HandleJsonResponse(rw, http.StatusOK, tokenExchangeResponse{
			Token:     result.Token,
			TokenType: "Bearer",
			ExpiresIn: result.ExpiresIn,
		})
	})
}

// writeExchangeError maps exchange errors to HTTP status codes: 503 when the
// endpoint is not configured, 401 for any assertion verification failure.
func writeExchangeError(rw http.ResponseWriter, err error) {
	if errors.Is(err, exchange.ErrNotConfigured) {
		http.Error(rw, exchange.ErrNotConfigured.Error(), http.StatusServiceUnavailable)
		return
	}
	http.Error(rw, err.Error(), http.StatusUnauthorized)
}
