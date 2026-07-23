package tests

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/gorilla/mux"
)

func Test_IdempotencyMiddleware(t *testing.T) {
	is := handlers.NewIdempotencyStoreLocal()
	callCount := 0

	// Dummy endpoint for testing
	testHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		callCount++
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusCreated)
		rw.Write([]byte(`{"ok":true}`)) // nolint
	})

	router := mux.NewRouter()
	router.Handle("/test", handlers.UseIdempotency(testHandler, handlers.IdempotencyHandlerOptions{
		Expiry:      5000 * time.Millisecond,
		IgnorePaths: []string{"/ignored"},
	}, is)).Methods(http.MethodPost)

	ik := "idempotency-key-test"

	t.Run("returns handler response with a fresh key", func(t *testing.T) {
		res := sendWithHeaders(router, http.MethodPost, "/test", bytes.NewBufferString(""), map[string]string{"Idempotency-Key": ik})
		assertStatusCode(t, res, http.StatusCreated)

		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != `{"ok":true}` {
			t.Fatalf("expected fresh response body to be replayable, got %q", string(body))
		}
	})

	t.Run("replays stored response with a used key", func(t *testing.T) {
		res := sendWithHeaders(router, http.MethodPost, "/test", bytes.NewBufferString(""), map[string]string{"Idempotency-Key": ik})
		assertStatusCode(t, res, http.StatusCreated)

		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != `{"ok":true}` {
			t.Fatalf("expected replayed response body, got %q", string(body))
		}
		if callCount != 1 {
			t.Fatalf("expected handler to run once, ran %d times", callCount)
		}
	})

	t.Run("returns 400 with missing header", func(t *testing.T) {
		res := send(router, http.MethodPost, "/test", bytes.NewBufferString(""))
		assertStatusCode(t, res, http.StatusBadRequest)
	})

}

func Test_IdempotencyMiddleware_ReplaysErrorResponses(t *testing.T) {
	is := handlers.NewIdempotencyStoreLocal()
	callCount := 0

	testHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		callCount++
		http.Error(rw, "upstream failed", http.StatusInternalServerError)
	})

	router := mux.NewRouter()
	router.Handle("/test-error", handlers.UseIdempotency(testHandler, handlers.IdempotencyHandlerOptions{
		Expiry: 5000 * time.Millisecond,
	}, is)).Methods(http.MethodPost)

	ik := "idempotency-key-error-test"

	res := sendWithHeaders(router, http.MethodPost, "/test-error", bytes.NewBufferString(""), map[string]string{"Idempotency-Key": ik})
	assertStatusCode(t, res, http.StatusInternalServerError)
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "upstream failed\n" {
		t.Fatalf("expected first error body to be captured, got %q", string(body))
	}

	res = sendWithHeaders(router, http.MethodPost, "/test-error", bytes.NewBufferString(""), map[string]string{"Idempotency-Key": ik})
	assertStatusCode(t, res, http.StatusInternalServerError)
	body, err = io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "upstream failed\n" {
		t.Fatalf("expected replayed error body, got %q", string(body))
	}
	if callCount != 1 {
		t.Fatalf("expected handler to run once for replayed error, ran %d times", callCount)
	}
}

// TODO: Move to test utils
func sendWithHeaders(router *mux.Router, method, path string, body io.Reader, headers map[string]string) *http.Response {
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("content-type", "application/json")

	for hk, hv := range headers {
		req.Header.Set(hk, hv)
	}

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr.Result()
}
