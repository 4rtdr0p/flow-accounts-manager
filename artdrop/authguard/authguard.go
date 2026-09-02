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
	"strings"

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

// ScopeArtistOnBehalf is the scope an operator token carries to create an
// Original/Edition (or onboard an artist) for an artist other than its own
// flow_address claim. It mirrors scopeArtistOnBehalf in
// auth/exchange/policy.go (operations+ only, never granted to the artist role)
// and is duplicated here as a literal rather than imported because that
// constant is unexported in the exchange package. The two must stay in sync.
const ScopeArtistOnBehalf = "account.artdrop.artist.onbehalf"

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

// RequireArtistSubject binds the artistAddress a request acts on to the
// authenticated token's flow_address claim, so a self-service artist token can
// only create Originals/Editions (or onboard) for its OWN custodial account.
// The on-chain transaction already ties identity to the signer, but without
// this check any caller holding original.create could ask the wallet-api to
// sign on behalf of a *different* artist's custodial account.
//
// It binds on flow_address (the on-chain identity), NOT on the token subject:
// under the Payload token-exchange contract sub is a Payload user id, not a
// Flow address, so requireArtistSubject's old subject==address comparison could
// never match and failed open. Flow hex addresses are case-insensitive, so the
// comparison is EqualFold — unlike RequireUserSubject, which matches Payload
// user ids byte-for-byte.
//
// Behavior:
//   - No claims in context: auth is disabled (middleware did not run, e.g.
//     local dev), so this is a passthrough — pre-existing behavior unchanged.
//   - Token carries ScopeArtistOnBehalf: an operator acting for another artist;
//     the guard is bypassed. (middleware.HasScope also honors the "*" wildcard,
//     so admin-scoped tokens bypass too.)
//   - Otherwise fail-closed: flow_address must be non-empty and EqualFold-match
//     artistAddress, else 403.
func RequireArtistSubject(r *http.Request, artistAddress string) error {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return nil
	}
	if middleware.HasScope(claims.Scope, ScopeArtistOnBehalf) {
		return nil
	}
	if claims.FlowAddress == "" || !strings.EqualFold(claims.FlowAddress, artistAddress) {
		return &errors.RequestError{
			StatusCode: http.StatusForbidden,
			Err:        fmt.Errorf("token flow_address does not match artistAddress"),
		}
	}
	return nil
}
