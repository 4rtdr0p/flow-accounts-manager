package middleware

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

func TestAuthHandler(t *testing.T) {
	secret := "test-secret"
	rules := []AuthRule{{
		Method:        http.MethodGet,
		PathPattern:   regexp.MustCompile(`^/v1/accounts$`),
		RequiredScope: "account.read",
	}}

	h := AuthHandler(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}), AuthOptions{Enabled: true, Secret: secret, Rules: rules})

	t.Run("returns 401 without bearer token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/accounts", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})

	t.Run("returns 401 with expired token", func(t *testing.T) {
		tok := signedToken(t, secret, "account.read", time.Now().Add(-1*time.Minute))
		req := httptest.NewRequest(http.MethodGet, "/v1/accounts", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})

	t.Run("returns 403 with wrong scope", func(t *testing.T) {
		tok := signedToken(t, secret, "account.create", time.Now().Add(5*time.Minute))
		req := httptest.NewRequest(http.MethodGet, "/v1/accounts", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})

	t.Run("returns 200 with valid scope", func(t *testing.T) {
		tok := signedToken(t, secret, "account.read", time.Now().Add(5*time.Minute))
		req := httptest.NewRequest(http.MethodGet, "/v1/accounts", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})
}

func signedToken(t *testing.T, secret string, scope string, exp time.Time) string {
	t.Helper()
	claims := AuthClaims{
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
