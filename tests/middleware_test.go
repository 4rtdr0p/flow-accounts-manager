package tests

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/gomodule/redigo/redis"
	"github.com/gorilla/mux"
	upstreamgorm "gorm.io/gorm"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
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

func Test_IdempotencyMiddleware_ConcurrentRequests(t *testing.T) {
	is := handlers.NewIdempotencyStoreLocal()
	var callCount atomic.Int32

	testHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		time.Sleep(25 * time.Millisecond)
		rw.WriteHeader(http.StatusCreated)
		_, _ = rw.Write([]byte("ok"))
	})

	server := httptest.NewServer(handlers.UseIdempotency(testHandler, handlers.IdempotencyHandlerOptions{
		Expiry: time.Minute,
	}, is))
	defer server.Close()

	const requests = 200
	start := make(chan struct{})
	var wg sync.WaitGroup
	client := &http.Client{}
	statuses := make(chan int, requests)
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req, err := http.NewRequest(http.MethodPost, server.URL, nil)
			if err != nil {
				t.Error(err)
				return
			}
			req.Header.Set("Idempotency-Key", "concurrent-idempotency-key")
			res, err := client.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			res.Body.Close()
			statuses <- res.StatusCode
		}()
	}
	close(start)
	wg.Wait()
	close(statuses)

	if got := callCount.Load(); got != 1 {
		t.Fatalf("expected handler to run once, ran %d times", got)
	}

	created := 0
	for status := range statuses {
		switch status {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
		default:
			t.Fatalf("unexpected response status %d", status)
		}
	}
	if created == 0 {
		t.Fatal("expected at least one successful response")
	}
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
	if callCount != 2 {
		t.Fatalf("expected handler to re-run after a 5xx reservation release, ran %d times", callCount)
	}
}

func Test_IdempotencyMiddleware_ReleasesOn5xxAcrossBackends(t *testing.T) {
	cases := newBackendCases(t)
	for _, backend := range cases {
		backend := backend
		t.Run(backend.name, func(t *testing.T) {
			defer backend.cleanup()

			is := backend.store
			var callCount atomic.Int32

			handlerStatus := atomic.Int32{}
			handlerStatus.Store(http.StatusInternalServerError)

			testHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				callCount.Add(1)
				status := int(handlerStatus.Load())
				http.Error(rw, "upstream failed", status)
			})

			router := mux.NewRouter()
			router.Handle("/test-error", handlers.UseIdempotency(testHandler, handlers.IdempotencyHandlerOptions{
				Expiry: 5 * time.Second,
			}, is)).Methods(http.MethodPost)

			ik := backend.key("releases-on-5xx")

			res := sendWithHeaders(router, http.MethodPost, "/test-error", bytes.NewBufferString(""), map[string]string{"Idempotency-Key": ik})
			assertStatusCode(t, res, http.StatusInternalServerError)
			res.Body.Close()

			handlerStatus.Store(http.StatusOK)
			testHandler = http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				callCount.Add(1)
				rw.Header().Set("Content-Type", "application/json")
				rw.WriteHeader(http.StatusOK)
				_, _ = rw.Write([]byte(`{"ok":true}`))
			})
			router = mux.NewRouter()
			router.Handle("/test-error", handlers.UseIdempotency(testHandler, handlers.IdempotencyHandlerOptions{
				Expiry: 5 * time.Second,
			}, is)).Methods(http.MethodPost)

			res = sendWithHeaders(router, http.MethodPost, "/test-error", bytes.NewBufferString(""), map[string]string{"Idempotency-Key": ik})
			assertStatusCode(t, res, http.StatusOK)
			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if string(body) != `{"ok":true}` {
				t.Fatalf("expected the post-release retry to execute the handler, got body %q", string(body))
			}
			if got := callCount.Load(); got != 2 {
				t.Fatalf("expected handler to run twice (initial 5xx + post-release retry), ran %d times", got)
			}
		})
	}
}

func Test_IdempotencyMiddleware_ReplaysSuccessAcrossBackends(t *testing.T) {
	cases := newBackendCases(t)
	for _, backend := range cases {
		backend := backend
		t.Run(backend.name, func(t *testing.T) {
			defer backend.cleanup()

			is := backend.store
			var callCount atomic.Int32

			testHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				callCount.Add(1)
				rw.Header().Set("Content-Type", "application/json")
				rw.WriteHeader(http.StatusCreated)
				_, _ = rw.Write([]byte(`{"ok":true}`))
			})

			router := mux.NewRouter()
			router.Handle("/test-success", handlers.UseIdempotency(testHandler, handlers.IdempotencyHandlerOptions{
				Expiry: 5 * time.Second,
			}, is)).Methods(http.MethodPost)

			ik := backend.key("replays-success")

			res := sendWithHeaders(router, http.MethodPost, "/test-success", bytes.NewBufferString(""), map[string]string{"Idempotency-Key": ik})
			assertStatusCode(t, res, http.StatusCreated)
			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if string(body) != `{"ok":true}` {
				t.Fatalf("expected fresh success body, got %q", string(body))
			}

			res = sendWithHeaders(router, http.MethodPost, "/test-success", bytes.NewBufferString(""), map[string]string{"Idempotency-Key": ik})
			assertStatusCode(t, res, http.StatusCreated)
			body, err = io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			res.Body.Close()
			if string(body) != `{"ok":true}` {
				t.Fatalf("expected replayed success body, got %q", string(body))
			}
			if got := callCount.Load(); got != 1 {
				t.Fatalf("expected handler to run once (second request must be a replay), ran %d times", got)
			}
		})
	}
}

func Test_IdempotencyMiddleware_ConcurrentRequestsAcrossBackends(t *testing.T) {
	cases := newBackendCases(t)
	for _, backend := range cases {
		backend := backend
		t.Run(backend.name, func(t *testing.T) {
			defer backend.cleanup()

			is := backend.store
			var callCount atomic.Int32
			release := make(chan struct{})

			testHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				callCount.Add(1)
				<-release
				rw.Header().Set("Content-Type", "application/json")
				rw.WriteHeader(http.StatusCreated)
				_, _ = rw.Write([]byte("ok"))
			})

			router := mux.NewRouter()
			router.Handle("/test-concurrent", handlers.UseIdempotency(testHandler, handlers.IdempotencyHandlerOptions{
				Expiry: 5 * time.Second,
			}, is)).Methods(http.MethodPost)

			const racers = 16
			var wg sync.WaitGroup
			statuses := make(chan int, racers)
			start := make(chan struct{})
			key := backend.key("concurrent-requests")

			for i := 0; i < racers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					res := sendWithHeaders(router, http.MethodPost, "/test-concurrent", bytes.NewBufferString(""), map[string]string{"Idempotency-Key": key})
					statuses <- res.StatusCode
					res.Body.Close()
				}()
			}

			close(start)
			time.Sleep(20 * time.Millisecond)
			close(release)
			wg.Wait()
			close(statuses)

			if got := callCount.Load(); got != 1 {
				t.Fatalf("expected handler to run once, ran %d times", got)
			}

			created, conflicts := 0, 0
			for status := range statuses {
				switch status {
				case http.StatusCreated:
					created++
				case http.StatusConflict:
					conflicts++
				default:
					t.Fatalf("unexpected response status %d", status)
				}
			}
			if created != 1 {
				t.Fatalf("expected exactly one 201 (the owner), got %d", created)
			}
			if conflicts != racers-1 {
				t.Fatalf("expected %d 409 responses, got %d", racers-1, conflicts)
			}
		})
	}
}

func Test_IdempotencyMiddleware_PersistsResponseAfterTransientFailures(t *testing.T) {
	store := newFlakyStore(handlers.NewIdempotencyStoreLocal(), 2)
	callCount := 0

	testHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		callCount++
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusCreated)
		_, _ = rw.Write([]byte(`{"ok":true}`))
	})

	router := mux.NewRouter()
	router.Handle("/test-flaky", handlers.UseIdempotency(testHandler, handlers.IdempotencyHandlerOptions{
		Expiry: 5 * time.Second,
	}, store)).Methods(http.MethodPost)

	ik := "flaky-store-key"

	res := sendWithHeaders(router, http.MethodPost, "/test-flaky", bytes.NewBufferString(""), map[string]string{"Idempotency-Key": ik})
	assertStatusCode(t, res, http.StatusCreated)
	res.Body.Close()

	res = sendWithHeaders(router, http.MethodPost, "/test-flaky", bytes.NewBufferString(""), map[string]string{"Idempotency-Key": ik})
	assertStatusCode(t, res, http.StatusCreated)
	res.Body.Close()
	if callCount != 1 {
		t.Fatalf("expected handler to run once after flaky SetResponse succeeded on retry, ran %d times", callCount)
	}
}

func Test_IdempotencyMiddleware_FailsLoudWhenResponseCannotBePersisted(t *testing.T) {
	store := newFlakyStore(handlers.NewIdempotencyStoreLocal(), 99)
	callCount := 0

	testHandler := http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		callCount++
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusCreated)
		_, _ = rw.Write([]byte(`{"ok":true}`))
	})

	router := mux.NewRouter()
	router.Handle("/test-unwritable", handlers.UseIdempotency(testHandler, handlers.IdempotencyHandlerOptions{
		Expiry: 5 * time.Second,
	}, store)).Methods(http.MethodPost)

	ik := "unwritable-store-key"

	res := sendWithHeaders(router, http.MethodPost, "/test-unwritable", bytes.NewBufferString(""), map[string]string{"Idempotency-Key": ik})
	assertStatusCode(t, res, http.StatusCreated)
	res.Body.Close()

	res = sendWithHeaders(router, http.MethodPost, "/test-unwritable", bytes.NewBufferString(""), map[string]string{"Idempotency-Key": ik})
	assertStatusCode(t, res, http.StatusCreated)
	res.Body.Close()
	if callCount != 2 {
		t.Fatalf("expected handler to run twice (once per request) when persistence never succeeds, ran %d times", callCount)
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

// backendCase represents an idempotency store backend plus the cleanup hooks
// needed to keep tests isolated. The local backend is always available; the
// real backends are skipped when the matching service isn't reachable.
type backendCase struct {
	name    string
	store   handlers.IdempotencyStore
	key     func(label string) string
	cleanup func()
}

// newBackendCases returns the backends enabled for this test run. The
// Postgres and Redis backends are opt-in via environment variables so the
// suite stays green in environments that don't have those services running.
func newBackendCases(t *testing.T) []backendCase {
	t.Helper()

	cases := []backendCase{
		{
			name: "local",
			store: handlers.NewIdempotencyStoreLocal(),
			key: func(label string) string {
				return "local-" + label
			},
			cleanup: func() {},
		},
	}

	if dsn := os.Getenv("FLOW_WALLET_IDEMPOTENCY_TEST_POSTGRES_DSN"); dsn != "" {
		db, err := upstreamgorm.Open(postgres.Open(dsn), &upstreamgorm.Config{})
		if err != nil {
			t.Logf("skipping postgres idempotency backend: %v", err)
		} else {
			if err := db.AutoMigrate(&handlers.IdempotencyRecord{}); err != nil {
				t.Logf("skipping postgres idempotency backend: %v", err)
			} else {
				cases = append(cases, backendCase{
					name: "postgres",
					store: handlers.NewIdempotencyStoreGorm(db),
					key: func(label string) string {
						return "pg-" + label
					},
					cleanup: func() {
						_ = db.Where("1 = 1").Delete(&handlers.IdempotencyRecord{}).Error
						sqlDB, _ := db.DB()
						if sqlDB != nil {
							_ = sqlDB.Close()
						}
					},
				})
			}
		}
	}

	if url := os.Getenv("FLOW_WALLET_IDEMPOTENCY_TEST_REDIS_URL"); url != "" {
		conn, err := redis.DialURL(url)
		if err != nil {
			t.Logf("skipping redis idempotency backend: %v", err)
		} else {
			cases = append(cases, backendCase{
				name: "redis",
				store: handlers.NewIdempotencyStoreRedis(conn),
				key: func(label string) string {
					return "redis-" + label
				},
				cleanup: func() {
					_, _ = conn.Do("FLUSHDB")
					_ = conn.Close()
				},
			})
		}
	}

	return cases
}

// flakyIdempotencyStore fails SetResponse the first failuresLeft times it is
// invoked, then behaves like the wrapped store. It is used to verify that
// the handler retries SetResponse before giving up.
type flakyIdempotencyStore struct {
	inner       handlers.IdempotencyStore
	failuresLeft int
	mu          sync.Mutex
}

func newFlakyStore(inner handlers.IdempotencyStore, failuresLeft int) *flakyIdempotencyStore {
	return &flakyIdempotencyStore{inner: inner, failuresLeft: failuresLeft}
}

func (f *flakyIdempotencyStore) TryReserve(key string, expiry time.Duration) (bool, *handlers.IdempotencyRecord, error) {
	return f.inner.TryReserve(key, expiry)
}

func (f *flakyIdempotencyStore) SetResponse(key string, record handlers.IdempotencyRecord, expiry time.Duration) error {
	f.mu.Lock()
	if f.failuresLeft > 0 {
		f.failuresLeft--
		f.mu.Unlock()
		return errors.New("simulated transient SetResponse failure")
	}
	f.mu.Unlock()
	return f.inner.SetResponse(key, record, expiry)
}

func (f *flakyIdempotencyStore) Release(key string) error {
	return f.inner.Release(key)
}

// openTestSQLiteDB is a tiny helper used by the sqlite-only fallback if the
// suite is run without a Postgres DSN. It mirrors the engine used by the
// Gorm idempotency store so the test exercises the same code path.
func openTestSQLiteDB(t *testing.T) *upstreamgorm.DB {
	t.Helper()
	db, err := upstreamgorm.Open(sqlite.Open(":memory:"), &upstreamgorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&handlers.IdempotencyRecord{}); err != nil {
		t.Fatalf("auto-migrate sqlite: %v", err)
	}
	return db
}
