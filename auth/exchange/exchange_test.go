package exchange

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/handlers/middleware"
	jwt "github.com/golang-jwt/jwt/v5"
)

const (
	testSecret            = "test-hs256-secret"
	testAccessIssuer      = "wallet-api"
	testAccessAudience    = "wallet-clients"
	testAssertionIssuer   = "payload"
	testAssertionAudience = "wallet-api"
)

func newTestKeypair(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	return key, string(pemBytes)
}

func signAssertion(t *testing.T, key *rsa.PrivateKey, role, sub string, exp time.Time, iss, aud string) string {
	t.Helper()
	claims := assertionClaims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   sub,
			Issuer:    iss,
			Audience:  jwt.ClaimStrings{aud},
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}
	return signed
}

func testExchanger(pubPEM string) *Exchanger {
	return New(Config{
		PayloadPublicKeyPEM:       pubPEM,
		AccessTokenSecret:         testSecret,
		AccessTokenIssuer:         testAccessIssuer,
		AccessTokenAudience:       testAccessAudience,
		AccessTokenTTL:            600 * time.Second,
		ExpectedAssertionIssuer:   testAssertionIssuer,
		ExpectedAssertionAudience: testAssertionAudience,
	})
}

// TestExchangeMintsTokenValidatedByRealMiddleware is the loop-closing test: it
// mints an access token via Exchange and feeds it through the REAL wallet auth
// middleware (with AUTH enabled), asserting the mapped scope is recognized and
// the request is allowed through with the expected claims.
func TestExchangeMintsTokenValidatedByRealMiddleware(t *testing.T) {
	key, pubPEM := newTestKeypair(t)
	ex := testExchanger(pubPEM)

	assertion := signAssertion(t, key, "user", "user-123", time.Now().Add(time.Minute), testAssertionIssuer, testAssertionAudience)
	result, err := ex.Exchange(assertion)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if result.ExpiresIn != 600 {
		t.Fatalf("expires_in = %d, want 600", result.ExpiresIn)
	}

	// A protected route requiring a scope the "user" role maps to.
	rule := middleware.NewAuthRule(http.MethodGet, "/{apiVersion}/accounts", scopeAccountRead)

	var (
		reached   bool
		gotClaims *middleware.AuthClaims
		claimsOK  bool
	)
	next := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		reached = true
		gotClaims, claimsOK = middleware.ClaimsFromContext(r.Context())
		rw.WriteHeader(http.StatusOK)
	})

	handler := middleware.AuthHandler(next, middleware.AuthOptions{
		Enabled:  true,
		Secret:   testSecret,
		Issuer:   testAccessIssuer,
		Audience: testAccessAudience,
		Rules:    []middleware.AuthRule{rule},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+result.Token)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("middleware rejected minted token: status %d body %q", rw.Code, rw.Body.String())
	}
	if !reached {
		t.Fatal("protected handler was not reached")
	}
	if !claimsOK || gotClaims == nil {
		t.Fatal("claims not found in context after validation")
	}
	if gotClaims.Subject != "user-123" {
		t.Fatalf("subject = %q, want user-123", gotClaims.Subject)
	}
	if !hasScopeInClaim(gotClaims.Scope, scopeAccountRead) {
		t.Fatalf("minted scope claim %q missing %q", gotClaims.Scope, scopeAccountRead)
	}
	// The user role must also carry the transitional god-scope.
	if !hasScopeInClaim(gotClaims.Scope, scopeAccountSign) {
		t.Fatalf("minted scope claim %q missing transitional %q", gotClaims.Scope, scopeAccountSign)
	}
}

// TestOperationsRoleGetsEscrowScopes confirms an operations assertion mints a
// token accepted for an escrow-ops-scoped route.
func TestOperationsRoleGetsEscrowScopes(t *testing.T) {
	key, pubPEM := newTestKeypair(t)
	ex := testExchanger(pubPEM)

	assertion := signAssertion(t, key, "operations", "op-1", time.Now().Add(time.Minute), testAssertionIssuer, testAssertionAudience)
	result, err := ex.Exchange(assertion)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	rule := middleware.NewAuthRule(http.MethodPost, "/{apiVersion}/accounts/{address}/escrow/void", scopeEscrowVoid)
	next := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { rw.WriteHeader(http.StatusOK) })
	handler := middleware.AuthHandler(next, middleware.AuthOptions{
		Enabled: true, Secret: testSecret, Issuer: testAccessIssuer, Audience: testAccessAudience,
		Rules: []middleware.AuthRule{rule},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/accounts/0xabc/escrow/void", nil)
	req.Header.Set("Authorization", "Bearer "+result.Token)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("operations token rejected for escrow.void: status %d body %q", rw.Code, rw.Body.String())
	}
}

// TestUserRoleDeniedEscrowScope confirms the policy is actually restrictive: a
// user token must NOT satisfy an operations-only scope.
func TestUserRoleDeniedEscrowScope(t *testing.T) {
	key, pubPEM := newTestKeypair(t)
	ex := testExchanger(pubPEM)

	assertion := signAssertion(t, key, "user", "user-1", time.Now().Add(time.Minute), testAssertionIssuer, testAssertionAudience)
	result, err := ex.Exchange(assertion)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	rule := middleware.NewAuthRule(http.MethodPost, "/{apiVersion}/accounts/{address}/escrow/void", scopeEscrowVoid)
	next := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { rw.WriteHeader(http.StatusOK) })
	handler := middleware.AuthHandler(next, middleware.AuthOptions{
		Enabled: true, Secret: testSecret, Issuer: testAccessIssuer, Audience: testAccessAudience,
		Rules: []middleware.AuthRule{rule},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/accounts/0xabc/escrow/void", nil)
	req.Header.Set("Authorization", "Bearer "+result.Token)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)
	if rw.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for user token on escrow.void, got %d", rw.Code)
	}
}

func TestExchangeErrors(t *testing.T) {
	key, pubPEM := newTestKeypair(t)
	otherKey, _ := newTestKeypair(t)
	ex := testExchanger(pubPEM)

	cases := []struct {
		name      string
		assertion string
		wantErr   error
	}{
		{
			name:      "expired",
			assertion: signAssertion(t, key, "user", "u", time.Now().Add(-time.Minute), testAssertionIssuer, testAssertionAudience),
			wantErr:   ErrExpired,
		},
		{
			name:      "bad signature",
			assertion: signAssertion(t, otherKey, "user", "u", time.Now().Add(time.Minute), testAssertionIssuer, testAssertionAudience),
			wantErr:   ErrBadSignature,
		},
		{
			name:      "unknown role",
			assertion: signAssertion(t, key, "superuser", "u", time.Now().Add(time.Minute), testAssertionIssuer, testAssertionAudience),
			wantErr:   ErrUnknownRole,
		},
		{
			name:      "wrong issuer",
			assertion: signAssertion(t, key, "user", "u", time.Now().Add(time.Minute), "attacker", testAssertionAudience),
			wantErr:   ErrInvalidIssuer,
		},
		{
			name:      "wrong audience",
			assertion: signAssertion(t, key, "user", "u", time.Now().Add(time.Minute), testAssertionIssuer, "someone-else"),
			wantErr:   ErrInvalidAudience,
		},
		{
			name:      "missing subject",
			assertion: signAssertion(t, key, "user", "", time.Now().Add(time.Minute), testAssertionIssuer, testAssertionAudience),
			wantErr:   ErrMissingSubject,
		},
		{
			name:      "malformed",
			assertion: "not-a-jwt",
			wantErr:   ErrMalformedAssertion,
		},
		{
			name:      "empty",
			assertion: "   ",
			wantErr:   ErrMalformedAssertion,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ex.Exchange(tc.assertion)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Exchange error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestExchangeNotConfigured(t *testing.T) {
	// Empty public key ⇒ unconfigured ⇒ ErrNotConfigured, and the service still
	// constructs fine (no panic, New returns a usable value).
	ex := New(Config{AccessTokenSecret: testSecret, AccessTokenTTL: 600 * time.Second})
	if ex.Configured() {
		t.Fatal("exchanger with empty public key should not be configured")
	}
	_, err := ex.Exchange("anything")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Exchange error = %v, want ErrNotConfigured", err)
	}

	// Valid key but empty secret ⇒ also not configured (token would be
	// unvalidatable).
	_, pubPEM := newTestKeypair(t)
	ex2 := New(Config{PayloadPublicKeyPEM: pubPEM, AccessTokenTTL: 600 * time.Second})
	if ex2.Configured() {
		t.Fatal("exchanger with empty secret should not be configured")
	}
}

func TestScopesForRole(t *testing.T) {
	if _, ok := ScopesForRole("nope"); ok {
		t.Fatal("unknown role should not resolve")
	}
	adminScopes, ok := ScopesForRole("admin")
	if !ok {
		t.Fatal("admin role should resolve")
	}
	opsScopes, _ := ScopesForRole("operations")
	if strings.Join(adminScopes, " ") != strings.Join(opsScopes, " ") {
		t.Fatal("admin should currently mirror operations exactly")
	}
}

func hasScopeInClaim(scopeClaim, want string) bool {
	for _, s := range strings.Fields(scopeClaim) {
		if s == want {
			return true
		}
	}
	return false
}
