package tests

import (
	"bytes"
	"database/sql"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/flow-hydraulics/flow-wallet-api/system"
	"github.com/flow-hydraulics/flow-wallet-api/tests/test"
	"github.com/gorilla/mux"
)

// resetSystemSettings puts the shared system-settings row back to its baseline
// (not paused, not in maintenance) so a test that asserts the baseline is not
// affected by whatever a sibling test left behind.
func resetSystemSettings(t *testing.T, svc system.Service) {
	t.Helper()
	settings, err := svc.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.MaintenanceMode = false
	settings.PausedSince = sql.NullTime{}
	if err := svc.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsE2E(t *testing.T) {
	cfg := test.LoadConfig(t)
	svcs := test.GetServices(t, cfg)

	sysHandler := handlers.NewSystem(svcs.GetSystem())

	router := mux.NewRouter()
	router.Handle("/settings", sysHandler.GetSettings()).Methods(http.MethodGet)
	router.Handle("/settings", sysHandler.SetSettings()).Methods(http.MethodPost)

	var steps = []struct {
		body           io.Reader
		path           string
		method         string
		expectedStatus int
		expectedBody   string
	}{
		{
			body:           nil,
			path:           "/settings",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK,
			expectedBody:   `{"maintenanceMode":false}`,
		},
		{
			body:           bytes.NewBufferString("{\"maintenanceMode\": true}"),
			path:           "/settings",
			method:         http.MethodPost,
			expectedStatus: http.StatusOK,
			expectedBody:   `{"maintenanceMode":true}`,
		},
		{
			body:           nil,
			path:           "/settings",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK,
			expectedBody:   `{"maintenanceMode":true}`,
		},
	}

	for _, tt := range steps {
		res := send(router, tt.method, tt.path, tt.body)
		assertStatusCode(t, res, tt.expectedStatus)
		if bs, err := io.ReadAll(res.Body); err != nil || strings.TrimSpace(string(bs)) != tt.expectedBody {
			if err != nil {
				t.Error(err)
			} else {
				t.Errorf("expected response body to equal '%v', got '%v'\n", tt.expectedBody, strings.TrimSpace(string(bs)))
			}
		}
	}
}

func TestIsMaintenanceMode(t *testing.T) {
	cfg := test.LoadConfig(t)
	svcs := test.GetServices(t, cfg)

	sysService := svcs.GetSystem()

	// The system settings are a single shared row; sibling tests (and the
	// handler table-test above) mutate it. Reset to a known baseline so this
	// test is self-contained regardless of run order, and restore it after.
	resetSystemSettings(t, sysService)
	t.Cleanup(func() { resetSystemSettings(t, sysService) })

	settings, err := sysService.GetSettings()
	if err != nil {
		t.Fatal(err)
	}

	if settings.IsMaintenanceMode() {
		t.Error("expected system not to be in maintenance mode")
	}

	settings.MaintenanceMode = true

	if err := sysService.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}

	settings, err = sysService.GetSettings()
	if err != nil {
		t.Fatal(err)
	}

	if !settings.IsMaintenanceMode() {
		t.Error("expected system to be in maintenance mode")
	}
}

func TestIsPaused(t *testing.T) {
	cfg := test.LoadConfig(t)
	svcs := test.GetServices(t, cfg)

	sysService := svcs.GetSystem()

	// The system settings are a single shared row; reset to a known baseline so
	// this test is self-contained regardless of run order, and restore it after.
	resetSystemSettings(t, sysService)
	t.Cleanup(func() { resetSystemSettings(t, sysService) })

	settings, err := sysService.GetSettings()
	if err != nil {
		t.Fatal(err)
	}

	if settings.IsPaused(time.Minute) {
		t.Error("expected system not to be paused")
	}

	settings.PausedSince = sql.NullTime{Time: time.Now(), Valid: true}

	if err := sysService.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}

	settings, err = sysService.GetSettings()
	if err != nil {
		t.Fatal(err)
	}

	if !settings.IsPaused(time.Minute) {
		t.Error("expected system to be paused")
	}
}

func TestPausing(t *testing.T) {
	cfg := test.LoadConfig(t)
	svcs := test.GetServices(t, cfg)

	sysService := svcs.GetSystem()

	if halted, err := sysService.IsHalted(); err != nil || halted {
		t.Error("expected system not to be halted")
	}

	if err := sysService.Pause(); err != nil {
		t.Fatal(err)
	}

	if halted, err := sysService.IsHalted(); err != nil || !halted {
		t.Error("expected system to be halted")
	}
}
