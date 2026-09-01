package purchase

import (
	"context"
	"math"
	"testing"
)

func TestFixedPriceOracleLatest(t *testing.T) {
	price, err := (FixedPriceOracle{PriceUSD: 0.026}).Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(price.PriceUSD-0.026) > 1e-12 {
		t.Fatalf("PriceUSD = %v, want 0.026", price.PriceUSD)
	}
	if price.PublishTime.IsZero() {
		t.Fatal("PublishTime is zero")
	}
}
