package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/gorilla/mux"

	"github.com/flow-hydraulics/flow-wallet-api/flow_helpers"
	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/tests/test"
	"github.com/onflow/flow-go-sdk"
)

// TestE2EAccountCreationStress (T2) fires 100 concurrent POST /accounts and
// asserts every one yields a distinct, valid address with no duplicates or
// deadlock. This is the concurrency guarantee the real service needs: many
// account creations landing at once must not collide on the admin proposal key
// or the DB.
//
// It is gated behind a Postgres DSN (FLOW_WALLET_TEST_POSTGRES_DSN) and skips
// when unset, because the wallet's SQLite test path serialises/deadlocks under
// this contention — Postgres is the supported concurrent backend, and running
// this against SQLite would test a configuration the service never runs in
// production. Provide e.g.:
//
//	FLOW_WALLET_TEST_POSTGRES_DSN="postgresql://wallet:wallet@localhost:5432/wallet_test" \
//	  go test . -run TestE2EAccountCreationStress
func TestE2EAccountCreationStress(t *testing.T) {
	dsn := os.Getenv("FLOW_WALLET_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set FLOW_WALLET_TEST_POSTGRES_DSN to run the concurrent account-creation stress test (SQLite deadlocks under this load)")
	}

	cfg := test.LoadConfig(t)
	// Point at the provided Postgres instead of the forced SQLite temp file.
	cfg.DatabaseType = "postgres"
	cfg.DatabaseDSN = dsn
	// Give the worker pool room to drain the burst of create jobs concurrently.
	cfg.WorkerCount = 10
	cfg.WorkerQueueCapacity = 1000

	app := test.GetServices(t, cfg)
	accHandler := handlers.NewAccounts(app.GetAccounts())
	jobSvc := app.GetJobs()

	router := mux.NewRouter()
	router.Handle("/accounts", accHandler.Create()).Methods(http.MethodPost)

	const n = 100

	// Fire n concurrent POST /accounts, collecting each returned job id.
	jobIDs := make([]string, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/accounts", nil)
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			if rr.Code != http.StatusCreated {
				t.Errorf("request %d: expected 201, got %d: %s", i, rr.Code, rr.Body.String())
				return
			}
			var jr jobs.JSONResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &jr); err != nil {
				t.Errorf("request %d: decode job response: %v", i, err)
				return
			}
			jobIDs[i] = jr.ID.String()
		}(i)
	}
	wg.Wait()
	if t.Failed() {
		t.Fatal("one or more concurrent POST /accounts requests failed")
	}

	// Wait for every job and collect the distinct addresses it produced.
	seen := make(map[string]int, n)
	for i, id := range jobIDs {
		if id == "" {
			t.Fatalf("request %d produced no job id", i)
		}
		job, err := test.WaitForJob(jobSvc, id)
		if err != nil {
			t.Fatalf("job %s (request %d) failed: %v", id, i, err)
		}
		addr := job.Result
		if _, err := flow_helpers.ValidateAddress(addr, flow.Emulator); err != nil {
			t.Fatalf("job %s produced invalid address %q: %v", id, addr, err)
		}
		if prev, dup := seen[addr]; dup {
			t.Fatalf("duplicate address %s from requests %d and %d", addr, prev, i)
		}
		seen[addr] = i
	}

	if len(seen) != n {
		t.Fatalf("expected %d distinct addresses, got %d", n, len(seen))
	}
	t.Logf("created %d distinct custodial accounts concurrently with no collisions", len(seen))
}
