//go:build integration

package purchase

import (
	"context"
	"testing"
	"time"
)

// TestPoolOracleLive reads FLOW/USD from the real Flow EVM mainnet pools and
// asserts the price is in a sane band. Gated behind the `integration` build tag
// so it never runs in CI-without-network. Run:
//
//	go test -tags integration ./artdrop/purchase/ -run Live
func TestPoolOracleLive(t *testing.T) {
	o := NewPoolPriceOracle(PoolOracleConfig{
		RPCURLs:    []string{"https://mainnet.evm.nodes.onflow.org"},
		RPCTimeout: 15 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	price, err := o.Latest(ctx)
	if err != nil {
		t.Fatalf("live oracle read failed: %v", err)
	}
	t.Logf("live FLOW/USD = $%.6f (read at %s)", price.PriceUSD, price.PublishTime.UTC().Format(time.RFC3339))
	if price.PriceUSD < 0.005 || price.PriceUSD > 0.20 {
		t.Fatalf("live FLOW/USD $%.6f outside expected band [$0.005, $0.20]", price.PriceUSD)
	}
}
