package artdrop

import (
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/plugins"
)

// mustNewService builds a Service for tests using ParseTestConfig, failing
// the test immediately on error. In practice NewService only errors when
// its Config fails to validate, and ParseTestConfig's defaults always
// validate, so this should never actually fire outside of a test that
// deliberately breaks its own config.
func mustNewService(t *testing.T, deps plugins.PluginDeps) *Service {
	t.Helper()

	svc, err := NewService(deps, ParseTestConfig(t))
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}
