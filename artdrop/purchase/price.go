package purchase

import (
	"errors"
	"time"
)

// PythPrice is the FLOW/USD price the purchase flow reads to size the escrow's
// FLOW gas reserve. The name is historical — the price now comes from the
// on-chain pool oracle (issue #121), not Pyth — but the type is deliberately
// kept so the PriceOracle interface, service.go and the audit record (which
// all refer to it) are untouched by the oracle swap.
type PythPrice struct {
	// PriceUSD is the USD price of one FLOW token.
	PriceUSD float64
	// PublishTime is when the price was read.
	PublishTime time.Time
}

// ErrPythStale is retained for the purchase service's stale-price mapping
// branch (service.go maps it to ErrOracleStale). The pool oracle never returns
// it — on a failed refresh it fails hard rather than serving a stale price —
// but the symbol is kept so service.go compiles unchanged.
var ErrPythStale = errors.New("flow/usd price is stale")
