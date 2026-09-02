package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop"
	"github.com/flow-hydraulics/flow-wallet-api/auth/exchange"
	"github.com/flow-hydraulics/flow-wallet-api/auth/openapi"
	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
)

// TestAuthExchangeE2E is the full-stack proof of issue #284: a Payload identity
// assertion (short-lived RS256, sub + role only) is exchanged at the REAL
// /v1/auth/token endpoint for a wallet access token, and that token is gated by
// the REAL auth middleware whose rules come from the REAL router + the REAL
// openapi.yml x-required-scopes — the exact openapi→scope wiring main() builds.
//
// The per-unit pieces (assertion → Exchange → middleware, and the role→scope
// policy) are already covered in auth/exchange/exchange_test.go; this test adds
// the end-to-end wiring assertion those can't make: that the scope openapi
// actually binds to POST .../void is the scope the wallet grants `operations`
// but not `user`. It gates sentinel handlers with the real rules — auth runs
// before any handler, so a sentinel is all the wiring assertion needs, and it
// keeps the test hermetic (no services, no emulator, no contract deploy).
func TestAuthExchangeE2E(t *testing.T) {
	// A test keypair standing in for Payload's signing key.
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	// The wallet ingests the public key as single-line base64 of the PEM (Quave
	// env vars can't carry a multi-line PEM) — mirror that exactly.
	pubB64 := base64.StdEncoding.EncodeToString(pubPEM)

	const (
		accessSecret    = "auth-exchange-e2e-access-secret"
		assertionIssuer = "payload"
		assertionAud    = "wallet-api"
		anAccount       = "0xf8d6e0586b0a20c7"
		anEscrow        = "42"
	)

	exchanger := exchange.New(exchange.Config{
		PayloadPublicKeyB64:       pubB64,
		AccessTokenSecret:         accessSecret,
		AccessTokenTTL:            10 * time.Minute,
		ExpectedAssertionIssuer:   assertionIssuer,
		ExpectedAssertionAudience: assertionAud,
	})
	if !exchanger.Configured() {
		t.Fatal("exchanger should be Configured with a valid public key + secret")
	}
	authHandler := handlers.NewAuthExchange(exchanger)

	// Build the REAL router so its registered routes + openapi.yml's
	// x-required-scopes yield the REAL auth rules — exactly as main() does.
	// Service handlers are nil (route registration never calls them); the
	// exchange handler is real so /v1/auth/token can mint tokens.
	deps := plugins.PluginDeps{}
	registeredPlugins, err := registerPlugins(nil, artdrop.ParseTestConfig(t), deps)
	if err != nil {
		t.Fatalf("registerPlugins: %v", err)
	}
	realRouter := buildRouter(routeOptions{}, routeHandlers{
		System:           handlers.NewSystem(nil),
		Templates:        handlers.NewTemplates(nil),
		Jobs:             handlers.NewJobs(nil),
		Accounts:         handlers.NewAccounts(nil),
		Transactions:     handlers.NewTransactions(nil, nil),
		Tokens:           handlers.NewTokens(nil),
		Ops:              handlers.NewOps(nil),
		Auth:             authHandler,
		DebugURL:         "debug-url",
		DebugSHA:         "debug-sha",
		DebugBuildTime:   "debug-build-time",
		WorkerPoolStatus: func() (interface{}, error) { return nil, nil },
	}, registeredPlugins, deps)

	spec, err := os.ReadFile("openapi.yml")
	if err != nil {
		t.Fatalf("read openapi.yml: %v", err)
	}
	scopeIndex, err := openapi.LoadScopeIndex(spec)
	if err != nil {
		t.Fatalf("LoadScopeIndex: %v", err)
	}
	authRules, err := openapi.AuthRulesFromRouter(realRouter, scopeIndex)
	if err != nil {
		t.Fatalf("AuthRulesFromRouter: %v", err)
	}

	// The server under test: sentinel handlers gated by the REAL rules, with
	// the REAL exchange endpoint (auth-exempt) in front. This is main()'s
	// UseAuth(router, {Rules: authRules}) wrapping, minus the business logic.
	sentinel := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})
	tr := mux.NewRouter()
	tr.Handle("/v1/auth/token", authHandler.TokenExchange()).Methods(http.MethodPost)
	tr.PathPrefix("/").Handler(sentinel)
	server := handlers.UseAuth(tr, handlers.AuthOptions{
		Enabled: true,
		Secret:  accessSecret,
		Rules:   authRules,
	})

	// mintAssertion signs a Payload-style RS256 identity assertion. Overridable
	// iss/aud/exp/role so we can exercise the rejection paths too.
	mintAssertion := func(t *testing.T, role, iss, aud string, exp time.Time) string {
		t.Helper()
		claims := jwt.MapClaims{
			"sub":  "user-123",
			"role": role,
			"iss":  iss,
			"aud":  aud,
			"iat":  time.Now().Unix(),
			"exp":  exp.Unix(),
		}
		signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(priv)
		if err != nil {
			t.Fatalf("sign assertion: %v", err)
		}
		return signed
	}

	// Real openapi-mapped endpoints (verified scopes):
	//   GET  .../artdrop/escrows            account.read                 (user)
	//   GET  .../artdrop/escrows/by-seller  account.read                 (user)
	//   POST .../artdrop/escrows/{id}/void  account.artdrop.escrow.void  (operations)
	//   POST .../sign                       account.sign                 (admin)
	escrowsURL := "/v1/accounts/" + anAccount + "/artdrop/escrows"
	bySellerURL := "/v1/accounts/" + anAccount + "/artdrop/escrows/by-seller"
	voidURL := "/v1/accounts/" + anAccount + "/artdrop/escrows/" + anEscrow + "/void"
	signURL := "/v1/accounts/" + anAccount + "/sign"

	exchangeToken := func(t *testing.T, assertion string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"assertion": assertion})
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/token", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("token exchange: expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Token     string `json:"token"`
			TokenType string `json:"token_type"`
			ExpiresIn int    `json:"expires_in"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode token response: %v", err)
		}
		if resp.TokenType != "Bearer" || resp.ExpiresIn <= 0 || resp.Token == "" {
			t.Fatalf("bad token response: %+v", resp)
		}
		return resp.Token
	}

	call := func(t *testing.T, method, url, token string) int {
		t.Helper()
		req := httptest.NewRequest(method, url, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, req)
		return rr.Code
	}

	future := time.Now().Add(5 * time.Minute)

	t.Run("user role: reads allowed, escrow.void and sign FORBIDDEN", func(t *testing.T) {
		token := exchangeToken(t, mintAssertion(t, "user", assertionIssuer, assertionAud, future))
		if got := call(t, http.MethodGet, escrowsURL, token); got != http.StatusOK {
			t.Fatalf("user GET escrows: expected 200, got %d", got)
		}
		if got := call(t, http.MethodGet, bySellerURL, token); got != http.StatusOK {
			t.Fatalf("user GET escrows/by-seller: expected 200, got %d", got)
		}
		// The crux of #284: a buyer's valid token can never void an escrow.
		if got := call(t, http.MethodPost, voidURL, token); got != http.StatusForbidden {
			t.Fatalf("user POST void: expected 403, got %d", got)
		}
		if got := call(t, http.MethodPost, signURL, token); got != http.StatusForbidden {
			t.Fatalf("user POST sign: expected 403, got %d", got)
		}
	})

	t.Run("operations role: escrow.void allowed, sign FORBIDDEN", func(t *testing.T) {
		token := exchangeToken(t, mintAssertion(t, "operations", assertionIssuer, assertionAud, future))
		if got := call(t, http.MethodPost, voidURL, token); got != http.StatusOK {
			t.Fatalf("operations POST void: expected 200, got %d", got)
		}
		if got := call(t, http.MethodGet, escrowsURL, token); got != http.StatusOK {
			t.Fatalf("operations GET escrows: expected 200, got %d", got)
		}
		if got := call(t, http.MethodPost, signURL, token); got != http.StatusForbidden {
			t.Fatalf("operations POST sign: expected 403 (admin break-glass only), got %d", got)
		}
	})

	t.Run("admin role: break-glass sign allowed", func(t *testing.T) {
		token := exchangeToken(t, mintAssertion(t, "admin", assertionIssuer, assertionAud, future))
		if got := call(t, http.MethodPost, signURL, token); got != http.StatusOK {
			t.Fatalf("admin POST sign: expected 200, got %d", got)
		}
	})

	t.Run("no token is 401", func(t *testing.T) {
		if got := call(t, http.MethodPost, voidURL, ""); got != http.StatusUnauthorized {
			t.Fatalf("missing token: expected 401, got %d", got)
		}
	})

	// The exchange endpoint's own rejection paths (all 401).
	assertExchange401 := func(t *testing.T, assertion string) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"assertion": assertion})
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/token", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 from exchange, got %d: %s", rr.Code, rr.Body.String())
		}
	}

	t.Run("expired assertion is rejected 401", func(t *testing.T) {
		assertExchange401(t, mintAssertion(t, "user", assertionIssuer, assertionAud, time.Now().Add(-1*time.Minute)))
	})
	t.Run("wrong issuer is rejected 401", func(t *testing.T) {
		assertExchange401(t, mintAssertion(t, "user", "not-payload", assertionAud, future))
	})
	t.Run("wrong audience is rejected 401", func(t *testing.T) {
		assertExchange401(t, mintAssertion(t, "user", assertionIssuer, "not-wallet-api", future))
	})
	t.Run("unknown role is rejected 401", func(t *testing.T) {
		assertExchange401(t, mintAssertion(t, "superuser", assertionIssuer, assertionAud, future))
	})
	t.Run("assertion signed by a different key is rejected 401", func(t *testing.T) {
		other, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate other RSA key: %v", err)
		}
		claims := jwt.MapClaims{
			"sub": "user-123", "role": "user", "iss": assertionIssuer,
			"aud": assertionAud, "iat": time.Now().Unix(), "exp": future.Unix(),
		}
		signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(other)
		if err != nil {
			t.Fatalf("sign with other key: %v", err)
		}
		assertExchange401(t, signed)
	})
}
