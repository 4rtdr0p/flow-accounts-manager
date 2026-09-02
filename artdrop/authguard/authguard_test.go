package authguard

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/errors"
	"github.com/flow-hydraulics/flow-wallet-api/handlers/middleware"
	jwt "github.com/golang-jwt/jwt/v5"
)

// reqWithClaims builds a request carrying the given claims in its context the
// same way the auth middleware does. A nil claims means "auth disabled" (no
// claims attached).
func reqWithClaims(claims *middleware.AuthClaims) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/stock-requests:create", nil)
	if claims != nil {
		r = r.WithContext(middleware.ContextWithClaims(r.Context(), claims))
	}
	return r
}

func claimsWith(subject, scope string) *middleware.AuthClaims {
	return &middleware.AuthClaims{
		Scope:            scope,
		RegisteredClaims: jwt.RegisteredClaims{Subject: subject},
	}
}

func claimsWithFlowAddress(flowAddress, scope string) *middleware.AuthClaims {
	return &middleware.AuthClaims{
		Scope:       scope,
		FlowAddress: flowAddress,
		// sub carries a Payload user id under the new contract, not an address;
		// set it to prove RequireArtistSubject binds on flow_address, not sub.
		RegisteredClaims: jwt.RegisteredClaims{Subject: "payload-user-1"},
	}
}

func statusOf(err error) int {
	if err == nil {
		return 0
	}
	if re, ok := err.(*errors.RequestError); ok {
		return re.StatusCode
	}
	return -1
}

func TestRequireUserSubject_NoClaimsPassthrough(t *testing.T) {
	// Auth disabled (middleware never ran): the guard must not block, keeping
	// the pre-existing behavior for local/dev deployments.
	if err := RequireUserSubject(reqWithClaims(nil), "user-1"); err != nil {
		t.Fatalf("expected passthrough with no claims, got %v", err)
	}
}

func TestRequireUserSubject_SubjectMatches(t *testing.T) {
	err := RequireUserSubject(reqWithClaims(claimsWith("user-1", "studio.charge.create")), "user-1")
	if err != nil {
		t.Fatalf("expected pass for matching subject, got %v", err)
	}
}

func TestRequireUserSubject_SubjectMismatchForbidden(t *testing.T) {
	err := RequireUserSubject(reqWithClaims(claimsWith("user-2", "studio.charge.create")), "user-1")
	if got := statusOf(err); got != http.StatusForbidden {
		t.Fatalf("expected 403 for subject mismatch, got status %d (err %v)", got, err)
	}
}

func TestRequireUserSubject_OnBehalfBypass(t *testing.T) {
	// An operator token with the on-behalf scope may act for another user.
	err := RequireUserSubject(reqWithClaims(claimsWith("operator-9", ScopeChargeOnBehalf)), "user-1")
	if err != nil {
		t.Fatalf("expected on-behalf bypass, got %v", err)
	}
}

func TestRequireUserSubject_WildcardScopeBypass(t *testing.T) {
	// A "*"-scoped admin token bypasses too (HasScope honors the wildcard).
	err := RequireUserSubject(reqWithClaims(claimsWith("admin", "*")), "user-1")
	if err != nil {
		t.Fatalf("expected wildcard-scope bypass, got %v", err)
	}
}

func TestRequireUserSubject_EmptySubjectForbidden(t *testing.T) {
	// Fail-closed: an authenticated token with no subject and no bypass scope
	// must not be able to act on an arbitrary userId.
	err := RequireUserSubject(reqWithClaims(claimsWith("", "studio.charge.create")), "user-1")
	if got := statusOf(err); got != http.StatusForbidden {
		t.Fatalf("expected 403 for empty subject, got status %d (err %v)", got, err)
	}
}

func TestRequireUserSubject_CaseSensitive(t *testing.T) {
	// Payload user ids are case-sensitive: a differing case must NOT match.
	err := RequireUserSubject(reqWithClaims(claimsWith("User-1", "studio.charge.create")), "user-1")
	if got := statusOf(err); got != http.StatusForbidden {
		t.Fatalf("expected 403 for case-differing subject, got status %d (err %v)", got, err)
	}
}

const artistAddr = "0xf8d6e0586b0a20c7"

func TestRequireArtistSubject_NoClaimsPassthrough(t *testing.T) {
	// Auth disabled (middleware never ran): the guard must not block.
	if err := RequireArtistSubject(reqWithClaims(nil), artistAddr); err != nil {
		t.Fatalf("expected passthrough with no claims, got %v", err)
	}
}

func TestRequireArtistSubject_FlowAddressMatches(t *testing.T) {
	err := RequireArtistSubject(reqWithClaims(claimsWithFlowAddress(artistAddr, "account.artdrop.original.create")), artistAddr)
	if err != nil {
		t.Fatalf("expected pass for matching flow_address, got %v", err)
	}
}

func TestRequireArtistSubject_FlowAddressMatchesCaseInsensitively(t *testing.T) {
	// Flow hex addresses are case-insensitive: a differing case must still match
	// (EqualFold), unlike RequireUserSubject on Payload user ids.
	err := RequireArtistSubject(reqWithClaims(claimsWithFlowAddress("0xF8D6E0586B0A20C7", "account.artdrop.original.create")), artistAddr)
	if err != nil {
		t.Fatalf("expected pass for case-differing flow_address, got %v", err)
	}
}

func TestRequireArtistSubject_FlowAddressMismatchForbidden(t *testing.T) {
	err := RequireArtistSubject(reqWithClaims(claimsWithFlowAddress("0xother00000000000", "account.artdrop.original.create")), artistAddr)
	if got := statusOf(err); got != http.StatusForbidden {
		t.Fatalf("expected 403 for flow_address mismatch, got status %d (err %v)", got, err)
	}
}

func TestRequireArtistSubject_OnBehalfBypass(t *testing.T) {
	// An operator token with the on-behalf scope may act for another artist even
	// when its flow_address differs (or is absent).
	err := RequireArtistSubject(reqWithClaims(claimsWithFlowAddress("0xoperator00000000", ScopeArtistOnBehalf)), artistAddr)
	if err != nil {
		t.Fatalf("expected on-behalf bypass, got %v", err)
	}
}

func TestRequireArtistSubject_WildcardScopeBypass(t *testing.T) {
	// A "*"-scoped admin token bypasses too (HasScope honors the wildcard).
	err := RequireArtistSubject(reqWithClaims(claimsWithFlowAddress("0xoperator00000000", "*")), artistAddr)
	if err != nil {
		t.Fatalf("expected wildcard-scope bypass, got %v", err)
	}
}

func TestRequireArtistSubject_EmptyFlowAddressForbidden(t *testing.T) {
	// Fail-closed: an authenticated token with no flow_address and no bypass
	// scope must not be able to act on an arbitrary artist's account.
	err := RequireArtistSubject(reqWithClaims(claimsWithFlowAddress("", "account.artdrop.original.create")), artistAddr)
	if got := statusOf(err); got != http.StatusForbidden {
		t.Fatalf("expected 403 for empty flow_address, got status %d (err %v)", got, err)
	}
}
