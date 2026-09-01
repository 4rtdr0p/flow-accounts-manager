package purchase

import (
	"context"
	"time"
)

// FixedPriceOracle returns a configured FLOW/USD price for non-mainnet QA.
type FixedPriceOracle struct {
	PriceUSD float64
}

func (o FixedPriceOracle) Latest(_ context.Context) (*PythPrice, error) {
	return &PythPrice{PriceUSD: o.PriceUSD, PublishTime: time.Now()}, nil
}
