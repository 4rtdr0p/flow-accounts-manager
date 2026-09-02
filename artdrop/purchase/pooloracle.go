package purchase

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Method selectors (first 4 bytes of keccak256(signature)).
const (
	selGetReserves = "0x0902f1ac" // getReserves()            (UniswapV2)
	selSlot0       = "0x3850c7bd" // slot0()                  (UniswapV3)
	selBalanceOf   = "0x70a08231" // balanceOf(address)       (ERC20)
)

// wflowAddress is WFLOW on Flow EVM mainnet (18 decimals). It is a constant of
// the deployment, so the WFLOW leg of every pool's TVL can be read with
// WFLOW.balanceOf(pool) without discovering token addresses at runtime.
const (
	wflowAddress  = "0xd3bf53dac106a0290b0483ecbc89d40fcc961f3e"
	wflowDecimals = 18
)

// PoolKind is a pool's AMM type, which selects the spot-price read + decode.
type PoolKind string

const (
	PoolV2 PoolKind = "v2" // UniswapV2-style: getReserves()
	PoolV3 PoolKind = "v3" // UniswapV3-style: slot0().sqrtPriceX96
)

// PoolConfig describes one FLOW/stable pool to read. Orientation and the
// stable's decimals are ALWAYS explicit config, never inferred from the chain:
// a pool can list WFLOW as token0 or token1 (WFLOWIsToken0), and different
// stables have different decimals (StableDecimals).
type PoolConfig struct {
	Address        string   // 0x-prefixed pool contract address
	Kind           PoolKind // v2 | v3
	WFLOWIsToken0  bool     // true when WFLOW is token0 (else token1)
	StableDecimals int      // decimals of the stable leg (e.g. 6 for USDC-likes)
}

// DefaultPools is the hardcoded, on-chain-verified set used when no override is
// configured (issue #121 plan). All five price WFLOW at ~$0.0262 within 0.1%.
func DefaultPools() []PoolConfig {
	return []PoolConfig{
		{Address: "0x0fdba612fea7a7ad0256687eebf056d81ca63f63", Kind: PoolV3, WFLOWIsToken0: false, StableDecimals: 6}, // PYUSD0/WFLOW
		{Address: "0xfc18d92085fa9df01be5985e5d890b4a4d7edad9", Kind: PoolV2, WFLOWIsToken0: false, StableDecimals: 6}, // PYUSD0/WFLOW
		{Address: "0x17e96496212d06eb1ff10c6f853669cc9947a1e7", Kind: PoolV2, WFLOWIsToken0: false, StableDecimals: 6}, // USDF/WFLOW
		{Address: "0xd21c58adaf1d1119fe40413b45a5f43d23d58df3", Kind: PoolV3, WFLOWIsToken0: false, StableDecimals: 6}, // USDF/WFLOW
		{Address: "0xc0f7bd6485a30b743f5a704d3bf11070806ce073", Kind: PoolV3, WFLOWIsToken0: true, StableDecimals: 6},  // WFLOW/stgUSDC (inverted)
	}
}

// ParsePools parses the FLOW_WALLET_ARTDROP_ORACLE_POOLS override. It accepts
// either a CSV of "addr:v2|v3:wflowIsToken0:stableDec" entries (separated by
// commas) or leaves the default set when empty. Each field is required.
func ParsePools(raw string) ([]PoolConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultPools(), nil
	}
	entries := strings.Split(raw, ",")
	pools := make([]PoolConfig, 0, len(entries))
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		parts := strings.Split(e, ":")
		if len(parts) != 4 {
			return nil, fmt.Errorf("pool %q: want addr:v2|v3:wflowIsToken0:stableDec (4 fields), got %d", e, len(parts))
		}
		addr := strings.TrimSpace(parts[0])
		if !strings.HasPrefix(addr, "0x") || len(addr) != 42 {
			return nil, fmt.Errorf("pool %q: address must be 0x + 40 hex chars", e)
		}
		kind := PoolKind(strings.TrimSpace(parts[1]))
		if kind != PoolV2 && kind != PoolV3 {
			return nil, fmt.Errorf("pool %q: kind must be v2 or v3", e)
		}
		wflowIsToken0, err := strconv.ParseBool(strings.TrimSpace(parts[2]))
		if err != nil {
			return nil, fmt.Errorf("pool %q: wflowIsToken0 must be true/false: %w", e, err)
		}
		stableDec, err := strconv.Atoi(strings.TrimSpace(parts[3]))
		if err != nil || stableDec < 0 || stableDec > 36 {
			return nil, fmt.Errorf("pool %q: stableDec must be 0..36", e)
		}
		pools = append(pools, PoolConfig{Address: addr, Kind: kind, WFLOWIsToken0: wflowIsToken0, StableDecimals: stableDec})
	}
	if len(pools) == 0 {
		return nil, fmt.Errorf("no pools parsed from %q", raw)
	}
	return pools, nil
}

// PoolOracleConfig configures a PoolPriceOracle.
type PoolOracleConfig struct {
	RPCURLs         []string      // failover list, tried in order
	Pools           []PoolConfig  // pools to read (defaults applied by caller)
	TTL             time.Duration // cache lifetime
	MaxDeviationBps int           // reject a pool whose price deviates > this from the median
	MinPools        int           // minimum pools that must be read, else error
	MinSurvivors    int           // minimum pools that must survive outlier rejection, else error
	SanityMinUSD    float64       // reject a final price below this
	SanityMaxUSD    float64       // reject a final price above this
	RPCTimeout      time.Duration // per-request HTTP timeout
}

// PoolPriceOracle implements PriceOracle by reading FLOW/USD from a set of Flow
// EVM pools. See docs/POOL-ORACLE-PLAN.md for the full pipeline: cached +
// single-flighted batched read -> per-pool spot -> median outlier rejection ->
// TVL-weighted mean -> sanity bound. On any refresh failure it returns an error
// (it never serves a stale price — a wrong escrow amount is worse than a
// retryable failed charge).
type PoolPriceOracle struct {
	client       *evmClient
	pools        []PoolConfig
	ttl          time.Duration
	maxDeviation float64 // fraction (bps/10000)
	minPools     int
	minSurvivors int
	sanityMin    float64
	sanityMax    float64

	// now is the clock, injectable so cache/TTL is testable.
	now func() time.Time

	mu       sync.Mutex
	cached   *PythPrice
	cachedAt time.Time
	group    singleflight.Group
}

// NewPoolPriceOracle builds a PoolPriceOracle. Missing knobs fall back to the
// plan defaults so a zero-ish config still works.
func NewPoolPriceOracle(cfg PoolOracleConfig) *PoolPriceOracle {
	pools := cfg.Pools
	if len(pools) == 0 {
		pools = DefaultPools()
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = 45 * time.Second
	}
	maxDevBps := cfg.MaxDeviationBps
	if maxDevBps <= 0 {
		maxDevBps = 200
	}
	minPools := cfg.MinPools
	if minPools <= 0 {
		minPools = 3
	}
	minSurvivors := cfg.MinSurvivors
	if minSurvivors <= 0 {
		minSurvivors = 2
	}
	sanityMin := cfg.SanityMinUSD
	if sanityMin <= 0 {
		sanityMin = 0.0005
	}
	sanityMax := cfg.SanityMaxUSD
	if sanityMax <= 0 {
		sanityMax = 2.0
	}
	timeout := cfg.RPCTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	urls := cfg.RPCURLs
	if len(urls) == 0 {
		urls = []string{"https://mainnet.evm.nodes.onflow.org"}
	}
	return &PoolPriceOracle{
		client:       newEVMClient(urls, timeout),
		pools:        pools,
		ttl:          ttl,
		maxDeviation: float64(maxDevBps) / 10000.0,
		minPools:     minPools,
		minSurvivors: minSurvivors,
		sanityMin:    sanityMin,
		sanityMax:    sanityMax,
		now:          time.Now,
	}
}

// Latest returns the cached price when it is within TTL, otherwise refreshes it
// under a single-flight so a burst of callers triggers one read. A refresh
// error is returned to the caller (no stale fallback).
func (o *PoolPriceOracle) Latest(ctx context.Context) (*PythPrice, error) {
	o.mu.Lock()
	if o.cached != nil && o.now().Sub(o.cachedAt) < o.ttl {
		p := o.cached
		o.mu.Unlock()
		return p, nil
	}
	o.mu.Unlock()

	v, err, _ := o.group.Do("refresh", func() (interface{}, error) {
		// Re-check the cache inside the single-flight: a caller that queued
		// behind the in-flight refresh must not trigger a second read.
		o.mu.Lock()
		if o.cached != nil && o.now().Sub(o.cachedAt) < o.ttl {
			p := o.cached
			o.mu.Unlock()
			return p, nil
		}
		o.mu.Unlock()

		price, err := o.refresh(ctx)
		if err != nil {
			return nil, err
		}
		o.mu.Lock()
		o.cached = price
		o.cachedAt = o.now()
		o.mu.Unlock()
		return price, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*PythPrice), nil
}

// poolRead records where a pool's calls landed in the batch, so the results can
// be routed back per pool.
type poolRead struct {
	pool      PoolConfig
	callStart int
	callCount int
}

// poolSpot is one pool's computed FLOW/USD spot price and its WFLOW-leg USD
// value, used as the TVL weight.
type poolSpot struct {
	price float64
	tvl   float64
}

// refresh performs one batched read and aggregates it into a price.
func (o *PoolPriceOracle) refresh(ctx context.Context) (*PythPrice, error) {
	calls, reads := o.buildCalls()
	results, err := o.client.BatchCall(ctx, calls)
	if err != nil {
		return nil, fmt.Errorf("oracle rpc read: %w", err)
	}

	spots := make([]poolSpot, 0, len(reads))
	for _, r := range reads {
		spot, ok := computeSpot(r.pool, results[r.callStart:r.callStart+r.callCount])
		if !ok {
			continue
		}
		spots = append(spots, spot)
	}

	final, err := aggregate(spots, o.minPools, o.minSurvivors, o.maxDeviation, o.sanityMin, o.sanityMax)
	if err != nil {
		return nil, err
	}
	return &PythPrice{PriceUSD: final, PublishTime: o.now()}, nil
}

// buildCalls lays out the batch: v2 pools read getReserves() (which gives both
// spot and TVL); v3 pools read slot0() (spot) + WFLOW.balanceOf(pool) (the
// WFLOW-leg TVL). Returns the calls and, per pool, the slice of the results it
// owns.
func (o *PoolPriceOracle) buildCalls() ([]evmCall, []poolRead) {
	var calls []evmCall
	reads := make([]poolRead, 0, len(o.pools))
	for _, p := range o.pools {
		start := len(calls)
		switch p.Kind {
		case PoolV2:
			calls = append(calls, evmCall{To: p.Address, Data: selGetReserves})
		case PoolV3:
			calls = append(calls,
				evmCall{To: p.Address, Data: selSlot0},
				evmCall{To: wflowAddress, Data: balanceOfData(p.Address)},
			)
		default:
			continue
		}
		reads = append(reads, poolRead{pool: p, callStart: start, callCount: len(calls) - start})
	}
	return calls, reads
}

// balanceOfData builds balanceOf(holder) calldata: selector + the 20-byte
// holder address left-padded to a 32-byte word.
func balanceOfData(holder string) string {
	h := strings.ToLower(strings.TrimPrefix(holder, "0x"))
	return selBalanceOf + strings.Repeat("0", 64-len(h)) + h
}

// computeSpot decodes one pool's results into its spot price and WFLOW-leg TVL.
// It returns ok=false when the pool's calls reverted or decoded badly, so the
// pool is simply dropped from the read set.
func computeSpot(p PoolConfig, results []json.RawMessage) (poolSpot, bool) {
	switch p.Kind {
	case PoolV2:
		if len(results) < 1 {
			return poolSpot{}, false
		}
		r0, r1, err := decodeReserves(results[0])
		if err != nil {
			return poolSpot{}, false
		}
		reserveWFLOW, reserveStable := r1, r0
		if p.WFLOWIsToken0 {
			reserveWFLOW, reserveStable = r0, r1
		}
		if reserveWFLOW.Sign() <= 0 || reserveStable.Sign() <= 0 {
			return poolSpot{}, false
		}
		price := spotFromReserves(reserveStable, reserveWFLOW, p.StableDecimals)
		if price <= 0 {
			return poolSpot{}, false
		}
		// TVL weight = WFLOW-leg USD value = (reserveWFLOW/1e18) * price.
		tvl := bigToFloat(reserveWFLOW, wflowDecimals) * price
		return poolSpot{price: price, tvl: tvl}, true

	case PoolV3:
		if len(results) < 2 {
			return poolSpot{}, false
		}
		sqrtP, err := decodeSlot0(results[0])
		if err != nil || sqrtP.Sign() <= 0 {
			return poolSpot{}, false
		}
		price := spotFromSqrtPriceX96(sqrtP, p.WFLOWIsToken0, p.StableDecimals)
		if price <= 0 {
			return poolSpot{}, false
		}
		wflowBal, err := decodeUint256(results[1])
		if err != nil || wflowBal.Sign() < 0 {
			return poolSpot{}, false
		}
		tvl := bigToFloat(wflowBal, wflowDecimals) * price
		return poolSpot{price: price, tvl: tvl}, true

	default:
		return poolSpot{}, false
	}
}

// spotFromReserves computes USD-per-WFLOW from v2 reserves (stable = $1):
// price = (reserveStable/10^stableDec) / (reserveWFLOW/10^18). Computed with
// big.Rat then narrowed to float64 to avoid intermediate float overflow.
func spotFromReserves(reserveStable, reserveWFLOW *big.Int, stableDec int) float64 {
	// price = reserveStable * 10^18 / (reserveWFLOW * 10^stableDec)
	num := new(big.Int).Mul(reserveStable, pow10Int(wflowDecimals))
	den := new(big.Int).Mul(reserveWFLOW, pow10Int(stableDec))
	if den.Sign() == 0 {
		return 0
	}
	f, _ := new(big.Rat).SetFrac(num, den).Float64()
	return f
}

// spotFromSqrtPriceX96 computes USD-per-WFLOW from a v3 sqrtPriceX96 using
// big.Int/big.Rat (sqrtP can be up to 2^160; its square overflows float64).
//
// priceRaw = (sqrtP/2^96)^2 = raw token1 per raw token0. Converting to
// human-readable token1-per-token0 multiplies by 10^(dec0-dec1). Then:
//   - WFLOWIsToken0: token0=WFLOW(18), token1=stable(S). token1/token0 human
//     is stable-per-WFLOW = USD/WFLOW directly = priceRaw * 10^(18-S).
//   - else: token0=stable(S), token1=WFLOW(18). token1/token0 human is
//     WFLOW-per-stable; USD/WFLOW is its reciprocal = 10^(18-S) / priceRaw.
func spotFromSqrtPriceX96(sqrtP *big.Int, wflowIsToken0 bool, stableDec int) float64 {
	sq := new(big.Int).Mul(sqrtP, sqrtP)             // sqrtP^2
	q192 := new(big.Int).Lsh(big.NewInt(1), 192)     // 2^192
	priceRaw := new(big.Rat).SetFrac(sq, q192)       // raw token1 / raw token0
	scale := ratPow10(wflowDecimals - stableDec)     // 10^(18-S)

	var priceRat *big.Rat
	if wflowIsToken0 {
		priceRat = new(big.Rat).Mul(priceRaw, scale)
	} else {
		if priceRaw.Sign() == 0 {
			return 0
		}
		priceRat = new(big.Rat).Quo(scale, priceRaw)
	}
	f, _ := priceRat.Float64()
	return f
}

// aggregate turns per-pool spots into the final price: require >= minPools
// read, drop pools beyond maxDeviation of the median, require >= minSurvivors,
// take the TVL-weighted mean of survivors, then enforce the sanity bound.
func aggregate(spots []poolSpot, minPools, minSurvivors int, maxDeviation, sanityMin, sanityMax float64) (float64, error) {
	if len(spots) < minPools {
		return 0, fmt.Errorf("oracle read only %d pools, need >= %d", len(spots), minPools)
	}
	prices := make([]float64, len(spots))
	for i, s := range spots {
		prices[i] = s.price
	}
	med := median(prices)
	if med <= 0 {
		return 0, fmt.Errorf("oracle median price is non-positive (%g)", med)
	}

	survivors := make([]poolSpot, 0, len(spots))
	for _, s := range spots {
		if abs(s.price-med)/med <= maxDeviation {
			survivors = append(survivors, s)
		}
	}
	if len(survivors) < minSurvivors {
		return 0, fmt.Errorf("only %d pools within %.2f%% of median $%.6f, need >= %d",
			len(survivors), maxDeviation*100, med, minSurvivors)
	}

	var wSum, tvlSum float64
	for _, s := range survivors {
		wSum += s.price * s.tvl
		tvlSum += s.tvl
	}
	if tvlSum <= 0 {
		return 0, fmt.Errorf("oracle survivors have non-positive total TVL")
	}
	final := wSum / tvlSum
	if final < sanityMin || final > sanityMax {
		return 0, fmt.Errorf("oracle price $%.6f outside sanity bound [$%.6f, $%.6f]", final, sanityMin, sanityMax)
	}
	return final, nil
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// bigToFloat converts a raw integer amount with `decimals` decimals to a
// float64 human amount (amount / 10^decimals).
func bigToFloat(amount *big.Int, decimals int) float64 {
	f, _ := new(big.Rat).SetFrac(amount, pow10Int(decimals)).Float64()
	return f
}

// pow10Int returns 10^n as a big.Int for n >= 0.
func pow10Int(n int) *big.Int {
	if n <= 0 {
		return big.NewInt(1)
	}
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}

// ratPow10 returns 10^n as a big.Rat, for positive or negative n.
func ratPow10(n int) *big.Rat {
	if n >= 0 {
		return new(big.Rat).SetInt(pow10Int(n))
	}
	return new(big.Rat).SetFrac(big.NewInt(1), pow10Int(-n))
}
