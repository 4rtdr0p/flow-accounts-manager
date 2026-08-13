package tests

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/flow-hydraulics/flow-wallet-api/handlers"
)

func TestStudioQuotesPriceAuth(t *testing.T) {
	secret := "w70-test-secret"
	rules := []handlers.AuthRule{
		handlers.NewAuthRule(http.MethodPost, "/{apiVersion}/studio/quotes:price", "pricing.read"),
	}

	okHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	router := mux.NewRouter()
	router.Handle("/v1/studio/quotes:price", handlers.UseAuth(okHandler, handlers.AuthOptions{
		Enabled: true,
		Secret:  secret,
		Rules:   rules,
	})).Methods(http.MethodPost)

	url := "/v1/studio/quotes:price"
	body := strings.NewReader(`{"process":"Metal Print","W":20,"L":30}`)

	t.Run("returns 401 without bearer token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, url, body)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})

	t.Run("returns 403 with wrong scope", func(t *testing.T) {
		tok := w02SignedToken(t, secret, "account.read", time.Now().Add(5*time.Minute))
		req := httptest.NewRequest(http.MethodPost, url, body)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})

	t.Run("returns 401 with expired pricing.read token", func(t *testing.T) {
		tok := w02SignedToken(t, secret, "pricing.read", time.Now().Add(-1*time.Minute))
		req := httptest.NewRequest(http.MethodPost, url, body)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})

	t.Run("returns 200 with pricing.read scope", func(t *testing.T) {
		tok := w02SignedToken(t, secret, "pricing.read", time.Now().Add(5*time.Minute))
		req := httptest.NewRequest(http.MethodPost, url, body)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})
}
