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

func TestW04RotateKeyAuth(t *testing.T) {
	secret := "w04-test-secret"
	rules := []handlers.AuthRule{
		handlers.NewAuthRule(http.MethodPost, "/{apiVersion}/accounts/{address}/rotate-key", "account.rotate"),
	}

	okHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusCreated)
	})

	router := mux.NewRouter()
	router.Handle("/v1/accounts/{address}/rotate-key", handlers.UseAuth(okHandler, handlers.AuthOptions{
		Enabled: true,
		Secret:  secret,
		Rules:   rules,
	})).Methods(http.MethodPost)

	t.Run("returns 401 without bearer token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/accounts/0xf8d6e0586b0a20c7/rotate-key", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})

	t.Run("returns 403 with wrong scope", func(t *testing.T) {
		tok := w04SignedToken(t, secret, "account.read", time.Now().Add(5*time.Minute))
		req := httptest.NewRequest(http.MethodPost, "/v1/accounts/0xf8d6e0586b0a20c7/rotate-key", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})

	t.Run("returns 201 with valid scope", func(t *testing.T) {
		tok := w04SignedToken(t, secret, "account.rotate", time.Now().Add(5*time.Minute))
		req := httptest.NewRequest(http.MethodPost, "/v1/accounts/0xf8d6e0586b0a20c7/rotate-key", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("expected %d, got %d", http.StatusCreated, rr.Code)
		}
	})
}

func w04SignedToken(t *testing.T, secret string, scope string, exp time.Time) string {
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
