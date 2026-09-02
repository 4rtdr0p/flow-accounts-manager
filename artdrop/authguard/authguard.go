// Package authguard holds identity guards shared by the ArtDrop HTTP handlers
// that accept a userId in the request (Studio stock-request charges, Studio
// charge listing, and buyer purchase charges).
//
// It lives in its own package rather than in the top-level artdrop package
// because artdrop imports studio and purchase, so those packages cannot import
// artdrop back without a cycle. authguard depends only on the auth middleware
// and the errors package, so both studio and purchase can import it freely.
package authguard

import (
	"fmt"
	"net/http"

	"github.com/flow-hydraulics/flow-wallet-api/errors"
	"github.com/flow-hydraulics/flow-wallet-api/handlers/middleware"
)

// ScopeChargeOnBehalf is the scope an operator token carries to create a charge
// for a userId other than its own token subject. It mirrors
// scopeStudioChargeOnBehalf in auth/exchange/policy.go (operations+ only, never
// granted to end users) and is duplicated here as a literal rather than
// imported because that constant is unexported in the exchange package. The two
// must stay in sync.
const ScopeChargeOnBehalf = "studio.charge.create.onbehalf"

// RequireUserSubject binds the userId a request acts on to the authenticated
// token's subject, closing an IDOR on the endpoints that take userId from the
// request body or query. The token subject (sub) is the caller's Payload user
// id — the same value the front sends as userId — so an end user may only
// charge or list their own records.
//
// Behavior:
//   - No claims in context: auth is disabled (the middleware did not run, e.g.
//     local dev), so this is a passthrough and the pre-existing behavior is
//     unchanged. When auth is enabled the middleware always attaches claims
//     before the handler runs.
//   - Token carries ScopeChargeOnBehalf: an operator acting for another user;
//     the guard is bypassed. (middleware.HasScope also honors the "*" wildcard,
//     so admin-scoped tokens bypass too.)
//   - Otherwise fail-closed: the subject must be non-empty and an EXACT match
//     for userID. Payload user ids are case-sensitive, so this is a byte-for-
//     byte comparison — unlike requireArtistSubject's EqualFold on Flow
//     addresses, matching on case here would let "User-1" charge "user-1".
func RequireUserSubject(r *http.Request, userID string) error {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return nil
	}
	if middleware.HasScope(claims.Scope, ScopeChargeOnBehalf) {
		return nil
	}
	if claims.Subject == "" || claims.Subject != userID {
		return &errors.RequestError{
			StatusCode: http.StatusForbidden,
			Err:        fmt.Errorf("token subject does not match userId"),
		}
	}
	return nil
}
