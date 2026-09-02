package purchase

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math"
	"math/big"
	"strings"
	"testing"
	"time"
)

// --- test helpers: build canned on-chain values from human numbers ---

func toRaw(human float64, decimals int) *big.Int {
	f := new(big.Float).SetPrec(256).SetFloat64(human)
	f.Mul(f, new(big.Float).SetInt(pow10Int(decimals)))
	out, _ := f.Int(nil)
	return out
}

func word32(x *big.Int) string {
	b := x.Bytes()
	if len(b) > 32 {
		b = b[len(b)-32:]
	}
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return hex.EncodeToString(out)
}

func hexResult(words ...*big.Int) json.RawMessage {
	var sb strings.Builder
	sb.WriteString(`"0x`)
	for _, w := range words {
		sb.WriteString(word32(w))
	}
	sb.WriteString(`"`)
	return json.RawMessage(sb.String())
}

// sqrtPriceX96For inverts spotFromSqrtPriceX96 to produce the sqrtPriceX96 a
// v3 pool at the given USD/WFLOW price would report, for the given orientation.
func sqrtPriceX96For(priceUSD float64, wflowIsToken0 bool, stableDec int) *big.Int {
	scale := math.Pow10(wflowDecimals - stableDec)
	var priceRaw float64
	if wflowIsToken0 {
		priceRaw = priceUSD / scale // token1(stable)/token0(WFLOW), raw
	} else {
		priceRaw = scale / priceUSD // token1(WFLOW)/token0(stable), raw
	}
	sqrtF := new(big.Float).SetPrec(256).SetFloat64(math.Sqrt(priceRaw))
	twoPow96 := new(big.Float).SetPrec(256).SetInt(new(big.Int).Lsh(big.NewInt(1), 96))
	sqrtF.Mul(sqrtF, twoPow96)
	out, _ := sqrtF.Int(nil)
	return out
}

// --- fake transport ---

type fakeTransport struct {
	responses map[string]json.RawMessage // key: to|data (lowercased)
	posts     int
	// failFirstURLs, when > 0, makes the first N Post calls fail (URL failover).
	failFirstURLs int
}

func fkey(to, data string) string {
	return strings.ToLower(to) + "|" + strings.ToLower(data)
}

func (f *fakeTransport) Post(_ context.Context, _ string, body []byte) ([]byte, error) {
	f.posts++
	if f.failFirstURLs > 0 {
		f.failFirstURLs--
		return nil, context.DeadlineExceeded
	}
	var reqs []struct {
		ID     int               `json:"id"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(body, &reqs); err != nil {
		return nil, err
	}
	type respT struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Result  json.RawMessage `json:"result,omitempty"`
		Error   *jsonRPCError   `json:"error,omitempty"`
	}
	out := make([]respT, len(reqs))
	for i, r := range reqs {
		var p ethCallParams
		_ = json.Unmarshal(r.Params[0], &p)
		res, ok := f.responses[fkey(p.To, p.Data)]
		if !ok {
			out[i] = respT{JSONRPC: "2.0", ID: r.ID, Error: &jsonRPCError{Code: -32000, Message: "execution reverted"}}
			continue
		}
		out[i] = respT{JSONRPC: "2.0", ID: r.ID, Result: res}
	}
	return json.Marshal(out)
}

// registerPool adds the canned response(s) for one pool priced at price with a
// WFLOW-leg USD weight of weightUSD.
func (f *fakeTransport) registerPool(p PoolConfig, price, weightUSD float64) {
	wflowHuman := weightUSD / price // WFLOW tokens held (weight = wflowHuman*price)
	switch p.Kind {
	case PoolV2:
		reserveWFLOW := toRaw(wflowHuman, wflowDecimals)
		reserveStable := toRaw(weightUSD, p.StableDecimals) // stable leg ~= weight for a balanced pool
		var r0, r1 *big.Int
		if p.WFLOWIsToken0 {
			r0, r1 = reserveWFLOW, reserveStable
		} else {
			r0, r1 = reserveStable, reserveWFLOW
		}
		f.responses[fkey(p.Address, selGetReserves)] = hexResult(r0, r1, big.NewInt(1700000000))
	case PoolV3:
		f.responses[fkey(p.Address, selSlot0)] = hexResult(sqrtPriceX96For(price, p.WFLOWIsToken0, p.StableDecimals))
		f.responses[fkey(wflowAddress, balanceOfData(p.Address))] = hexResult(toRaw(wflowHuman, wflowDecimals))
	}
}

func newTestOracle(pools []PoolConfig, tr rpcTransport, clock func() time.Time) *PoolPriceOracle {
	o := NewPoolPriceOracle(PoolOracleConfig{Pools: pools, RPCURLs: []string{"http://a", "http://b"}})
	o.client = &evmClient{urls: []string{"http://a", "http://b"}, tr: tr}
	if clock != nil {
		o.now = clock
	}
	return o
}

func newFake() *fakeTransport {
	return &fakeTransport{responses: map[string]json.RawMessage{}}
}

const relTol = 1e-4

func approx(t *testing.T, got, want, tol float64, msg string) {
	t.Helper()
	if math.Abs(got-want)/want > tol {
		t.Fatalf("%s: got %.8f, want ~%.8f (tol %.4f%%)", msg, got, want, tol*100)
	}
}

// --- tests ---

func TestPoolOracleHappyPath(t *testing.T) {
	pools := DefaultPools()
	fake := newFake()
	// weights loosely mirror the plan's per-pool TVL; all price at $0.0262.
	weights := []float64{371000, 152000, 148500, 123500, 20500}
	for i, p := range pools {
		fake.registerPool(p, 0.0262, weights[i])
	}
	o := newTestOracle(pools, fake, nil)

	price, err := o.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	approx(t, price.PriceUSD, 0.0262, 1e-3, "happy-path final price")
	if fake.posts != 1 {
		t.Fatalf("expected 1 RPC round-trip, got %d", fake.posts)
	}
	if price.PublishTime.IsZero() {
		t.Fatal("PublishTime is zero")
	}
}

func TestComputeSpotOrientation(t *testing.T) {
	cases := []struct {
		name string
		pool PoolConfig
	}{
		{"v2 stable=token0", PoolConfig{Address: "0xaa", Kind: PoolV2, WFLOWIsToken0: false, StableDecimals: 6}},
		{"v2 WFLOW=token0", PoolConfig{Address: "0xbb", Kind: PoolV2, WFLOWIsToken0: true, StableDecimals: 6}},
		{"v3 stable=token0", PoolConfig{Address: "0xcc", Kind: PoolV3, WFLOWIsToken0: false, StableDecimals: 6}},
		{"v3 WFLOW=token0 (inverted, stgUSDC)", PoolConfig{Address: "0xdd", Kind: PoolV3, WFLOWIsToken0: true, StableDecimals: 6}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := newFake()
			fake.registerPool(c.pool, 0.0262, 100000)
			calls, reads := (&PoolPriceOracle{pools: []PoolConfig{c.pool}}).buildCalls()
			results, err := (&evmClient{urls: []string{"http://x"}, tr: fake}).BatchCall(context.Background(), calls)
			if err != nil {
				t.Fatal(err)
			}
			spot, ok := computeSpot(c.pool, results[reads[0].callStart:reads[0].callStart+reads[0].callCount])
			if !ok {
				t.Fatal("computeSpot returned ok=false")
			}
			approx(t, spot.price, 0.0262, relTol, "spot price")
			approx(t, spot.tvl, 100000, 1e-3, "tvl weight")
		})
	}
}

func TestPoolOracleOutlierDropped(t *testing.T) {
	pools := DefaultPools()
	fake := newFake()
	weights := []float64{371000, 152000, 148500, 123500, 20500}
	for i, p := range pools {
		price := 0.0262
		if i == 4 {
			price = 0.05 // outlier well beyond 2% of the median
		}
		fake.registerPool(p, price, weights[i])
	}
	o := newTestOracle(pools, fake, nil)

	price, err := o.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	approx(t, price.PriceUSD, 0.0262, 1e-3, "final price with outlier dropped")
}

func TestPoolOracleStableDepegDropped(t *testing.T) {
	// One pool's stable trades 3% rich -> its FLOW/USD spot is ~3% off, beyond
	// the 2% gate, so it is dropped and does not move the final price.
	pools := DefaultPools()
	fake := newFake()
	weights := []float64{371000, 152000, 148500, 123500, 20500}
	for i, p := range pools {
		price := 0.0262
		if i == 0 {
			price = 0.0262 * 1.03
		}
		fake.registerPool(p, price, weights[i])
	}
	o := newTestOracle(pools, fake, nil)

	price, err := o.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	approx(t, price.PriceUSD, 0.0262, 2e-3, "final price with depegged pool dropped")
}

func TestPoolOracleTooFewSurvivors(t *testing.T) {
	pools := DefaultPools()
	fake := newFake()
	// Prices all disagree; only the median pool survives the 2% gate.
	prices := []float64{0.01, 0.02, 0.0262, 0.04, 0.05}
	for i, p := range pools {
		fake.registerPool(p, prices[i], 100000)
	}
	o := newTestOracle(pools, fake, nil)

	if _, err := o.Latest(context.Background()); err == nil {
		t.Fatal("expected error when too few pools survive outlier rejection")
	} else if !strings.Contains(err.Error(), "within") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPoolOracleMinPools(t *testing.T) {
	pools := DefaultPools()
	fake := newFake()
	// Only 2 of 5 pools respond; the rest revert -> below minPools (3).
	for i, p := range pools {
		if i < 2 {
			fake.registerPool(p, 0.0262, 100000)
		}
	}
	o := newTestOracle(pools, fake, nil)

	if _, err := o.Latest(context.Background()); err == nil {
		t.Fatal("expected error when fewer than minPools respond")
	} else if !strings.Contains(err.Error(), "need >=") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPoolOracleSanityReject(t *testing.T) {
	pools := DefaultPools()
	fake := newFake()
	for _, p := range pools {
		fake.registerPool(p, 3.0, 100000) // above sanity max ($2)
	}
	o := newTestOracle(pools, fake, nil)

	if _, err := o.Latest(context.Background()); err == nil {
		t.Fatal("expected sanity-bound rejection")
	} else if !strings.Contains(err.Error(), "sanity") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPoolOracleCacheTTL(t *testing.T) {
	pools := DefaultPools()
	fake := newFake()
	weights := []float64{371000, 152000, 148500, 123500, 20500}
	for i, p := range pools {
		fake.registerPool(p, 0.0262, weights[i])
	}
	now := time.Unix(1700000000, 0)
	o := newTestOracle(pools, fake, func() time.Time { return now })
	o.ttl = 45 * time.Second

	if _, err := o.Latest(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Second call within TTL: served from cache, no new read.
	now = now.Add(30 * time.Second)
	if _, err := o.Latest(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fake.posts != 1 {
		t.Fatalf("expected 1 read within TTL, got %d", fake.posts)
	}
	// Advance past TTL: a fresh read.
	now = now.Add(30 * time.Second)
	if _, err := o.Latest(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fake.posts != 2 {
		t.Fatalf("expected 2 reads after TTL expiry, got %d", fake.posts)
	}
}

func TestPoolOracleFailsHardNoStale(t *testing.T) {
	pools := DefaultPools()
	fake := newFake()
	weights := []float64{371000, 152000, 148500, 123500, 20500}
	for i, p := range pools {
		fake.registerPool(p, 0.0262, weights[i])
	}
	now := time.Unix(1700000000, 0)
	o := newTestOracle(pools, fake, func() time.Time { return now })
	o.ttl = 45 * time.Second

	if _, err := o.Latest(context.Background()); err != nil {
		t.Fatal(err)
	}
	// After TTL, drop all pool responses so the refresh cannot meet minPools.
	fake.responses = map[string]json.RawMessage{}
	now = now.Add(time.Minute)
	if _, err := o.Latest(context.Background()); err == nil {
		t.Fatal("expected refresh to fail hard rather than serve the stale cache")
	}
}

func TestParsePoolsOverride(t *testing.T) {
	pools, err := ParsePools("0xabc0000000000000000000000000000000000001:v3:true:6,0xabc0000000000000000000000000000000000002:v2:false:18")
	if err != nil {
		t.Fatal(err)
	}
	if len(pools) != 2 {
		t.Fatalf("want 2 pools, got %d", len(pools))
	}
	if pools[0].Kind != PoolV3 || !pools[0].WFLOWIsToken0 || pools[0].StableDecimals != 6 {
		t.Fatalf("pool0 parsed wrong: %+v", pools[0])
	}
	if pools[1].Kind != PoolV2 || pools[1].WFLOWIsToken0 || pools[1].StableDecimals != 18 {
		t.Fatalf("pool1 parsed wrong: %+v", pools[1])
	}
	if _, err := ParsePools(""); err != nil {
		t.Fatalf("empty override should use defaults, got %v", err)
	}
	if _, err := ParsePools("bad:format"); err == nil {
		t.Fatal("expected parse error for malformed pool")
	}
}
