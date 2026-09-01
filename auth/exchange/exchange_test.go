package exchange

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
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

func b64PEM(pem string) string {
	return base64.StdEncoding.EncodeToString([]byte(pem))
}

func testExchanger(pubPEM string) *Exchanger {
	return New(Config{
		PayloadPublicKeyB64:       b64PEM(pubPEM),
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
	// The user role carries the typed end-user actions...
	for _, want := range []string{scopeScriptExecute, scopeAccountTransfer, scopeEscrowActivate} {
		if !hasScopeInClaim(gotClaims.Scope, want) {
			t.Fatalf("minted user scope claim %q missing %q", gotClaims.Scope, want)
		}
	}
	// ...but NOT the break-glass account.sign god-scope (removed from user).
	if hasScopeInClaim(gotClaims.Scope, scopeAccountSign) {
		t.Fatalf("user token must not carry break-glass %q, got %q", scopeAccountSign, gotClaims.Scope)
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
	ex2 := New(Config{PayloadPublicKeyB64: b64PEM(pubPEM), AccessTokenSecret: "", AccessTokenTTL: 600 * time.Second})
	if ex2.Configured() {
		t.Fatal("exchanger with empty secret should not be configured")
	}

	// Non-base64 garbage ⇒ not configured, no panic.
	ex3 := New(Config{PayloadPublicKeyB64: "!!!not-base64!!!", AccessTokenSecret: testSecret, AccessTokenTTL: 600 * time.Second})
	if ex3.Configured() {
		t.Fatal("exchanger with invalid base64 key should not be configured")
	}

	// Valid base64 but not an RSA PEM ⇒ not configured, no panic.
	ex4 := New(Config{PayloadPublicKeyB64: base64.StdEncoding.EncodeToString([]byte("hello world")), AccessTokenSecret: testSecret, AccessTokenTTL: 600 * time.Second})
	if ex4.Configured() {
		t.Fatal("exchanger with non-RSA payload should not be configured")
	}

	// Sanity: a correctly base64-encoded PEM with a secret IS configured.
	_, pubPEM2 := newTestKeypair(t)
	ex5 := New(Config{PayloadPublicKeyB64: b64PEM(pubPEM2), AccessTokenSecret: testSecret, AccessTokenTTL: 600 * time.Second})
	if !ex5.Configured() {
		t.Fatal("exchanger with valid base64 PEM and secret should be configured")
	}
}

func TestScopesForRole(t *testing.T) {
	if _, ok := ScopesForRole("nope"); ok {
		t.Fatal("unknown role should not resolve")
	}

	userScopes, _ := ScopesForRole("user")
	opsScopes, ok := ScopesForRole("operations")
	if !ok {
		t.Fatal("operations role should resolve")
	}
	adminScopes, ok := ScopesForRole("admin")
	if !ok {
		t.Fatal("admin role should resolve")
	}

	userSet := toSet(userScopes)
	opsSet := toSet(opsScopes)
	adminSet := toSet(adminScopes)

	// user must have the typed actions and NOT any break-glass scope.
	for _, s := range []string{scopeScriptExecute, scopeAccountTransfer, scopeEscrowActivate, scopeAccountCreate, scopeAccountSetup, scopeStudioChargeCreate} {
		if _, ok := userSet[s]; !ok {
			t.Fatalf("user missing expected scope %q", s)
		}
	}
	for _, s := range []string{scopeAccountSign, scopeTransactionCreate, scopeTokenWrite, scopeEscrowVoid, scopeSystemWrite} {
		if _, ok := userSet[s]; ok {
			t.Fatalf("user must NOT have scope %q", s)
		}
	}

	// operations is a strict superset of user, plus operator actions, but NOT
	// the break-glass scopes.
	for s := range userSet {
		if _, ok := opsSet[s]; !ok {
			t.Fatalf("operations missing user scope %q", s)
		}
	}
	for _, s := range []string{scopeEscrowVoid, scopeEscrowReescrow, scopeOriginalCreate, scopeEditionCreate, scopeArtistOnboard, scopeAccountGraduate, scopeAccountKeySync, scopeWatchlistWrite, scopeOpsRun, scopeSystemWrite} {
		if _, ok := opsSet[s]; !ok {
			t.Fatalf("operations missing expected scope %q", s)
		}
	}
	for _, s := range []string{scopeAccountSign, scopeTransactionCreate, scopeTokenWrite} {
		if _, ok := opsSet[s]; ok {
			t.Fatalf("operations must NOT have break-glass scope %q", s)
		}
	}

	// admin is a strict superset of operations, plus exactly the 3 break-glass
	// scopes. It must NOT merely mirror operations.
	if len(adminScopes) == len(opsScopes) {
		t.Fatal("admin must be a strict superset of operations, not an alias")
	}
	for s := range opsSet {
		if _, ok := adminSet[s]; !ok {
			t.Fatalf("admin missing operations scope %q", s)
		}
	}
	for _, s := range []string{scopeAccountSign, scopeTransactionCreate, scopeTokenWrite} {
		if _, ok := adminSet[s]; !ok {
			t.Fatalf("admin missing break-glass scope %q", s)
		}
	}
	if len(adminScopes) != len(opsScopes)+3 {
		t.Fatalf("admin should be operations + 3 break-glass scopes, got %d vs %d", len(adminScopes), len(opsScopes))
	}
}

// TestAdminCoversAllNonExemptScopes guards the invariant that every scope in
// openapi.yml is granted by some role EXCEPT auth.token (the exchange route
// itself, which is auth-exempt). Since admin is the top superset, admin must
// contain exactly the full non-exempt scope universe.
func TestAdminCoversAllNonExemptScopes(t *testing.T) {
	allNonExempt := []string{
		// reads
		"account.read", "job.read", "ops.read", "pricing.read", "studio.charge.read",
		"system.read", "token.read", "transaction.read", "health.read",
		// user actions
		"script.execute", "account.create", "account.setup", "studio.charge.create",
		"account.transfer", "account.artdrop.escrow.activate",
		// operations actions
		"account.artdrop.escrow.void", "account.artdrop.escrow.reescrow",
		"account.artdrop.original.create", "account.artdrop.edition.create",
		"account.artdrop.artist.onboard", "account.key.graduate", "account.key.sync",
		"watchlist.write", "ops.run", "system.write",
		// admin break-glass
		"account.sign", "transaction.create", "token.write",
	}
	adminScopes, _ := ScopesForRole("admin")
	adminSet := toSet(adminScopes)
	for _, s := range allNonExempt {
		if _, ok := adminSet[s]; !ok {
			t.Errorf("admin (superset) missing openapi scope %q", s)
		}
	}
	if len(adminScopes) != len(allNonExempt) {
		t.Fatalf("admin scope count = %d, want %d (all non-exempt openapi scopes); auth.token must stay exempt", len(adminScopes), len(allNonExempt))
	}
}

func toSet(ss []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		m[s] = struct{}{}
	}
	return m
}

func hasScopeInClaim(scopeClaim, want string) bool {
	for _, s := range strings.Fields(scopeClaim) {
		if s == want {
			return true
		}
	}
	return false
}
