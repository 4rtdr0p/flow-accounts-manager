package tests

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"

	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/flow-hydraulics/flow-wallet-api/handlers/middleware"
)

// TestW06TransferAuth verifies that POST /accounts/{address}/transfer
// requires scope "account.transfer" and rejects all other cases.
func TestW06TransferAuth(t *testing.T) {
	secret := "w06-test-secret"
	rules := []handlers.AuthRule{
		handlers.NewAuthRule(http.MethodPost, "/{apiVersion}/accounts/{address}/transfer", "account.transfer"),
	}

	okHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusCreated)
	})

	router := mux.NewRouter()
	router.Handle("/v1/accounts/{address}/transfer", handlers.UseAuth(okHandler, handlers.AuthOptions{
		Enabled: true,
		Secret:  secret,
		Rules:   rules,
	})).Methods(http.MethodPost)

	transferURL := "/v1/accounts/0xf8d6e0586b0a20c7/transfer"

	t.Run("returns 401 without bearer token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, transferURL, nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})

	t.Run("returns 403 with wrong scope (account.sign)", func(t *testing.T) {
		tok := w06SignedToken(t, secret, "account.sign", time.Now().Add(5*time.Minute))
		req := httptest.NewRequest(http.MethodPost, transferURL, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})

	t.Run("returns 201 with valid scope (account.transfer)", func(t *testing.T) {
		tok := w06SignedToken(t, secret, "account.transfer", time.Now().Add(5*time.Minute))
		req := httptest.NewRequest(http.MethodPost, transferURL, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("expected %d, got %d", http.StatusCreated, rr.Code)
		}
	})
}

// TestW06SignAuth verifies that POST /accounts/{address}/sign
// requires scope "account.sign" and rejects all other cases.
func TestW06SignAuth(t *testing.T) {
	secret := "w06-test-secret"
	rules := []handlers.AuthRule{
		handlers.NewAuthRule(http.MethodPost, "/{apiVersion}/accounts/{address}/sign", "account.sign"),
	}

	okHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusCreated)
	})

	router := mux.NewRouter()
	router.Handle("/v1/accounts/{address}/sign", handlers.UseAuth(okHandler, handlers.AuthOptions{
		Enabled: true,
		Secret:  secret,
		Rules:   rules,
	})).Methods(http.MethodPost)

	signURL := "/v1/accounts/0xf8d6e0586b0a20c7/sign"

	t.Run("returns 401 without bearer token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, signURL, nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})

	t.Run("returns 403 with wrong scope (account.transfer)", func(t *testing.T) {
		tok := w06SignedToken(t, secret, "account.transfer", time.Now().Add(5*time.Minute))
		req := httptest.NewRequest(http.MethodPost, signURL, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})

	t.Run("returns 201 with valid scope (account.sign)", func(t *testing.T) {
		tok := w06SignedToken(t, secret, "account.sign", time.Now().Add(5*time.Minute))
		req := httptest.NewRequest(http.MethodPost, signURL, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("expected %d, got %d", http.StatusCreated, rr.Code)
		}
	})
}

// TestW06CrossScopeIsolation verifies that a router exposing both /transfer
// and /sign enforces separate scopes — a token with account.transfer cannot
// call /sign and vice-versa.
func TestW06CrossScopeIsolation(t *testing.T) {
	secret := "w06-test-secret"
	rules := []handlers.AuthRule{
		handlers.NewAuthRule(http.MethodPost, "/{apiVersion}/accounts/{address}/transfer", "account.transfer"),
		handlers.NewAuthRule(http.MethodPost, "/{apiVersion}/accounts/{address}/sign", "account.sign"),
	}

	okHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusCreated)
	})

	router := mux.NewRouter()
	router.Handle("/v1/accounts/{address}/transfer", handlers.UseAuth(okHandler, handlers.AuthOptions{
		Enabled: true,
		Secret:  secret,
		Rules:   rules,
	})).Methods(http.MethodPost)
	router.Handle("/v1/accounts/{address}/sign", handlers.UseAuth(okHandler, handlers.AuthOptions{
		Enabled: true,
		Secret:  secret,
		Rules:   rules,
	})).Methods(http.MethodPost)

	addr := "/v1/accounts/0xf8d6e0586b0a20c7"

	t.Run("account.transfer token cannot call /sign", func(t *testing.T) {
		tok := w06SignedToken(t, secret, "account.transfer", time.Now().Add(5*time.Minute))
		req := httptest.NewRequest(http.MethodPost, addr+"/sign", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})

	t.Run("account.sign token cannot call /transfer", func(t *testing.T) {
		tok := w06SignedToken(t, secret, "account.sign", time.Now().Add(5*time.Minute))
		req := httptest.NewRequest(http.MethodPost, addr+"/transfer", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})
}

func w06SignedToken(t *testing.T, secret string, scope string, exp time.Time) string {
	t.Helper()
	claims := middleware.AuthClaims{
		Scope: scope,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}
