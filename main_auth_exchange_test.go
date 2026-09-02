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
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"

	"github.com/flow-hydraulics/flow-wallet-api/auth/exchange"
	"github.com/flow-hydraulics/flow-wallet-api/handlers"
)

// TestAuthExchangeE2E is the end-to-end proof of issue #284: Payload proves
// identity (a short-lived RS256 assertion carrying only sub + role), the wallet
// is the authorization authority (it maps role → scopes with its own policy and
// mints the access token), and the minted token gates the typed endpoints so a
// buyer (`user`) can never reach an operator-only action even with a valid
// token. It runs the REAL exchange handler and the REAL auth middleware — the
// only test double is a throwaway RSA keypair standing in for Payload's.
//
// Pure httptest: the exchange + gating logic is entirely chain-independent, so
// this is deterministic and needs no emulator.
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
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		signed, err := tok.SignedString(priv)
		if err != nil {
			t.Fatalf("sign assertion: %v", err)
		}
		return signed
	}

	// The wallet router: the exchange endpoint plus three typed routes, each
	// gated at the scope its role tier owns.
	//   account.setup                    → user  (buyer can set up an account)
	//   account.artdrop.escrow.void      → operations (buyer must NOT reach it)
	//   account.sign                     → admin break-glass only
	authHandler := handlers.NewAuthExchange(exchanger)
	protected := func(scope string) http.Handler {
		ok := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusOK)
		})
		return handlers.UseAuth(ok, handlers.AuthOptions{
			Enabled: true,
			Secret:  accessSecret,
			Rules: []handlers.AuthRule{
				handlers.NewAuthRule(http.MethodPost, "/{apiVersion}/accounts/{address}/setup", "account.setup"),
				handlers.NewAuthRule(http.MethodPost, "/{apiVersion}/accounts/{address}/artdrop/escrows/{escrowId}/void", "account.artdrop.escrow.void"),
				handlers.NewAuthRule(http.MethodPost, "/{apiVersion}/accounts/{address}/sign", "account.sign"),
			},
		})
	}

	router := mux.NewRouter()
	router.Handle("/v1/auth/token", authHandler.TokenExchange()).Methods(http.MethodPost)
	router.Handle("/v1/accounts/{address}/setup", protected("account.setup")).Methods(http.MethodPost)
	router.Handle("/v1/accounts/{address}/artdrop/escrows/{escrowId}/void", protected("account.artdrop.escrow.void")).Methods(http.MethodPost)
	router.Handle("/v1/accounts/{address}/sign", protected("account.sign")).Methods(http.MethodPost)

	setupURL := "/v1/accounts/" + anAccount + "/setup"
	voidURL := "/v1/accounts/" + anAccount + "/artdrop/escrows/" + anEscrow + "/void"
	signURL := "/v1/accounts/" + anAccount + "/sign"

	// exchange trades a Payload assertion for a wallet access token, asserting
	// the HTTP contract (200, token_type Bearer, positive expires_in).
	exchangeToken := func(t *testing.T, assertion string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"assertion": assertion})
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/token", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
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
		if resp.TokenType != "Bearer" {
			t.Fatalf("expected token_type Bearer, got %q", resp.TokenType)
		}
		if resp.ExpiresIn <= 0 {
			t.Fatalf("expected positive expires_in, got %d", resp.ExpiresIn)
		}
		if resp.Token == "" {
			t.Fatal("expected a non-empty token")
		}
		return resp.Token
	}

	callWithToken := func(t *testing.T, url, token string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, url, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		return rr.Code
	}

	future := time.Now().Add(5 * time.Minute)

	t.Run("user role: passes account.setup, is FORBIDDEN from escrow.void", func(t *testing.T) {
		token := exchangeToken(t, mintAssertion(t, "user", assertionIssuer, assertionAud, future))

		if got := callWithToken(t, setupURL, token); got != http.StatusOK {
			t.Fatalf("user should pass account.setup: expected 200, got %d", got)
		}
		// The whole point of #284: a buyer's token can never void an escrow,
		// even though it's a perfectly valid token.
		if got := callWithToken(t, voidURL, token); got != http.StatusForbidden {
			t.Fatalf("user must be forbidden from escrow.void: expected 403, got %d", got)
		}
		if got := callWithToken(t, signURL, token); got != http.StatusForbidden {
			t.Fatalf("user must be forbidden from account.sign: expected 403, got %d", got)
		}
	})

	t.Run("operations role: passes escrow.void and account.setup, FORBIDDEN from account.sign", func(t *testing.T) {
		token := exchangeToken(t, mintAssertion(t, "operations", assertionIssuer, assertionAud, future))

		if got := callWithToken(t, voidURL, token); got != http.StatusOK {
			t.Fatalf("operations should pass escrow.void: expected 200, got %d", got)
		}
		if got := callWithToken(t, setupURL, token); got != http.StatusOK {
			t.Fatalf("operations should also pass account.setup: expected 200, got %d", got)
		}
		// account.sign is admin break-glass only.
		if got := callWithToken(t, signURL, token); got != http.StatusForbidden {
			t.Fatalf("operations must be forbidden from account.sign: expected 403, got %d", got)
		}
	})

	t.Run("admin role: break-glass account.sign passes", func(t *testing.T) {
		token := exchangeToken(t, mintAssertion(t, "admin", assertionIssuer, assertionAud, future))
		if got := callWithToken(t, signURL, token); got != http.StatusOK {
			t.Fatalf("admin should pass account.sign break-glass: expected 200, got %d", got)
		}
	})

	t.Run("no token is 401, not 403", func(t *testing.T) {
		if got := callWithToken(t, setupURL, ""); got != http.StatusUnauthorized {
			t.Fatalf("missing token: expected 401, got %d", got)
		}
	})

	// The exchange endpoint's own rejection paths (all 401): expired assertion,
	// wrong issuer/audience, unknown role, tampered signature.
	assertExchange401 := func(t *testing.T, assertion string) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"assertion": assertion})
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/token", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
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
