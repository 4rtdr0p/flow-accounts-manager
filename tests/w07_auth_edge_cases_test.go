package tests

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/flow-hydraulics/flow-wallet-api/handlers"
)

// TestW07AuthEdgeCases (T3) completes the auth edge cases the per-route auth
// tests (w02/w04) don't cover: the idempotency middleware's 400 (missing
// Idempotency-Key) and 409 (a concurrent in-flight duplicate — distinct from a
// completed replay), and the auth middleware's 401 on an *expired* bearer token
// (the existing tests only exercise wrong-scope 403 and missing-token 401).
// All pure httptest — no emulator needed.
func TestW07AuthEdgeCases(t *testing.T) {
	t.Run("missing Idempotency-Key returns 400", func(t *testing.T) {
		ok := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusCreated)
		})
		store := handlers.NewIdempotencyStoreLocal()
		router := mux.NewRouter()
		router.Handle("/accounts", handlers.UseIdempotency(
			ok,
			handlers.IdempotencyHandlerOptions{Expiry: time.Hour},
			store,
		)).Methods(http.MethodPost)

		// POST with no Idempotency-Key header at all.
		req := httptest.NewRequest(http.MethodPost, "/accounts", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected %d for missing Idempotency-Key, got %d: %s",
				http.StatusBadRequest, rr.Code, rr.Body.String())
		}
	})

	t.Run("concurrent in-flight duplicate returns 409", func(t *testing.T) {
		// The first request reserves the key and then blocks inside the
		// handler (never completing); a second request with the SAME key,
		// arriving while the reservation is held but not yet completed, must
		// get 409 — NOT a replay (there's no stored response yet) and NOT a
		// fresh 201.
		release := make(chan struct{})
		entered := make(chan struct{}, 1)
		slow := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			entered <- struct{}{}
			<-release
			rw.WriteHeader(http.StatusCreated)
		})

		store := handlers.NewIdempotencyStoreLocal()
		router := mux.NewRouter()
		router.Handle("/accounts", handlers.UseIdempotency(
			slow,
			handlers.IdempotencyHandlerOptions{Expiry: time.Hour},
			store,
		)).Methods(http.MethodPost)

		const key = "w07-concurrent-dup-key"

		var wg sync.WaitGroup
		wg.Add(1)
		firstStatus := make(chan int, 1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/accounts", nil)
			req.Header.Set("Idempotency-Key", key)
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			firstStatus <- rr.Code
		}()

		// Wait until the first request is inside the handler (key reserved).
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			close(release)
			t.Fatal("first request never entered the handler")
		}

		// Fire the duplicate while the first is still in-flight.
		req := httptest.NewRequest(http.MethodPost, "/accounts", nil)
		req.Header.Set("Idempotency-Key", key)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusConflict {
			close(release)
			t.Fatalf("expected %d for concurrent in-flight duplicate, got %d: %s",
				http.StatusConflict, rr.Code, rr.Body.String())
		}

		// Let the first request finish and confirm it succeeded (201).
		close(release)
		wg.Wait()
		if got := <-firstStatus; got != http.StatusCreated {
			t.Fatalf("expected first (in-flight) request to complete with %d, got %d",
				http.StatusCreated, got)
		}
	})

	t.Run("expired bearer token returns 401", func(t *testing.T) {
		secret := "w07-test-secret"
		rules := []handlers.AuthRule{
			handlers.NewAuthRule(http.MethodGet, "/{apiVersion}/jobs/{jobId}", "job.read"),
		}
		ok := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusOK)
		})
		router := mux.NewRouter()
		router.Handle("/v1/jobs/{jobId}", handlers.UseAuth(ok, handlers.AuthOptions{
			Enabled: true,
			Secret:  secret,
			Rules:   rules,
		})).Methods(http.MethodGet)

		url := "/v1/jobs/abc"

		// A token with the RIGHT scope but expired in the past must still 401.
		expired := w04SignedToken(t, secret, "job.read", time.Now().Add(-1*time.Minute))
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+expired)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected %d for expired token, got %d: %s",
				http.StatusUnauthorized, rr.Code, rr.Body.String())
		}

		// Sanity: the same scope, unexpired, passes — proving the 401 above is
		// the expiry, not the scope.
		valid := w04SignedToken(t, secret, "job.read", time.Now().Add(5*time.Minute))
		req = httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+valid)
		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d for valid unexpired token, got %d: %s",
				http.StatusOK, rr.Code, rr.Body.String())
		}
	})
}
