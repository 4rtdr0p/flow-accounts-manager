package tests

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/flow-hydraulics/flow-wallet-api/handlers"
)

func TestW05GraduateToSelfCustodyAuth(t *testing.T) {
	secret := "w05-test-secret"
	rules := []handlers.AuthRule{
		handlers.NewAuthRule(http.MethodPost, "/{apiVersion}/accounts/{address}/graduate-to-self-custody", "account.key.graduate"),
	}

	okHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	router := mux.NewRouter()
	router.Handle("/v1/accounts/{address}/graduate-to-self-custody", handlers.UseAuth(okHandler, handlers.AuthOptions{
		Enabled: true,
		Secret:  secret,
		Rules:   rules,
	})).Methods(http.MethodPost)

	graduateURL := "/v1/accounts/0xf8d6e0586b0a20c7/graduate-to-self-custody"

	t.Run("returns 401 without bearer token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, graduateURL, nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
		}
	})

	t.Run("returns 403 with wrong scope", func(t *testing.T) {
		tok := w02SignedToken(t, secret, "account.sign", time.Now().Add(5*time.Minute))
		req := httptest.NewRequest(http.MethodPost, graduateURL, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})

	t.Run("returns 200 with valid scope", func(t *testing.T) {
		tok := w02SignedToken(t, secret, "account.key.graduate", time.Now().Add(5*time.Minute))
		req := httptest.NewRequest(http.MethodPost, graduateURL, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})
}
