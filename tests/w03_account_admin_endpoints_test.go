package tests

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/flow-hydraulics/flow-wallet-api/handlers/middleware"
	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
)

func TestW03ArtistActivateAuth(t *testing.T) {
	secret := "w03-test-secret"
	rules := []handlers.AuthRule{
		handlers.NewAuthRule(http.MethodPost, "/{apiVersion}/accounts/{address}/artist-activate", "account.artist.activate"),
	}

	okHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	router := mux.NewRouter()
	router.Handle("/v1/accounts/{address}/artist-activate", handlers.UseAuth(okHandler, handlers.AuthOptions{
		Enabled: true,
		Secret:  secret,
		Rules:   rules,
	})).Methods(http.MethodPost)

	accountURL := "/v1/accounts/0xf8d6e0586b0a20c7/artist-activate"

	t.Run("returns 401 without bearer token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, accountURL, nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})

	t.Run("returns 403 with wrong scope", func(t *testing.T) {
		tok := w03SignedToken(t, secret, "account.read", time.Now().Add(5*time.Minute))
		req := httptest.NewRequest(http.MethodPost, accountURL, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})

	t.Run("returns 200 with valid scope", func(t *testing.T) {
		tok := w03SignedToken(t, secret, "account.artist.activate", time.Now().Add(5*time.Minute))
		req := httptest.NewRequest(http.MethodPost, accountURL, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})
}

func TestW03CommunityPoolEnableAuth(t *testing.T) {
	secret := "w03-test-secret"
	rules := []handlers.AuthRule{
		handlers.NewAuthRule(http.MethodPost, "/{apiVersion}/accounts/{address}/community-pool-enable", "account.community_pool.enable"),
	}

	okHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	router := mux.NewRouter()
	router.Handle("/v1/accounts/{address}/community-pool-enable", handlers.UseAuth(okHandler, handlers.AuthOptions{
		Enabled: true,
		Secret:  secret,
		Rules:   rules,
	})).Methods(http.MethodPost)

	accountURL := "/v1/accounts/0xf8d6e0586b0a20c7/community-pool-enable"

	t.Run("returns 401 without bearer token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, accountURL, nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})

	t.Run("returns 403 with wrong scope", func(t *testing.T) {
		tok := w03SignedToken(t, secret, "account.artist.activate", time.Now().Add(5*time.Minute))
		req := httptest.NewRequest(http.MethodPost, accountURL, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})

	t.Run("returns 200 with valid scope", func(t *testing.T) {
		tok := w03SignedToken(t, secret, "account.community_pool.enable", time.Now().Add(5*time.Minute))
		req := httptest.NewRequest(http.MethodPost, accountURL, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})
}

func w03SignedToken(t *testing.T, secret string, scope string, exp time.Time) string {
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
