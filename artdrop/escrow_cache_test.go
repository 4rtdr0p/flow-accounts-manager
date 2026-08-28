package artdrop

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
)

func TestEscrowCacheGetSetRoundTrip(t *testing.T) {
	c := newEscrowCache(10, time.Minute)

	if _, ok := c.get(1); ok {
		t.Fatal("expected miss on empty cache")
	}

	c.set(1, EscrowSummary{Id: 1, Status: 0})

	got, ok := c.get(1)
	if !ok {
		t.Fatal("expected hit after set")
	}
	if got.Id != 1 {
		t.Fatalf("unexpected cached summary: %+v", got)
	}
}

// TestEscrowCacheNonTerminalExpiresByTTL covers the 60s-style TTL path for
// a Pending (non-terminal) escrow: after the TTL elapses, the entry is
// treated as a miss and evicted.
func TestEscrowCacheNonTerminalExpiresByTTL(t *testing.T) {
	c := newEscrowCache(10, 20*time.Millisecond)

	c.set(1, EscrowSummary{Id: 1, Status: 0}) // 0 = Pending, non-terminal

	if _, ok := c.get(1); !ok {
		t.Fatal("expected hit immediately after set")
	}

	time.Sleep(40 * time.Millisecond)

	if _, ok := c.get(1); ok {
		t.Fatal("expected miss after TTL elapsed for a non-terminal entry")
	}
	if len(c.items) != 0 {
		t.Fatalf("expected expired entry to be evicted from items map, got %d entries", len(c.items))
	}
}

// TestEscrowCacheTerminalNeverExpiresByTTL covers the "indefinite" side of
// the design: a Released (terminal) escrow's summary never changes again,
// so it must still be a hit long after even a very short TTL would have
// elapsed.
func TestEscrowCacheTerminalNeverExpiresByTTL(t *testing.T) {
	c := newEscrowCache(10, time.Nanosecond)

	c.set(1, EscrowSummary{Id: 1, Status: escrowStatusReleased})

	time.Sleep(5 * time.Millisecond)

	got, ok := c.get(1)
	if !ok {
		t.Fatal("expected a terminal (Released) entry to survive past the TTL")
	}
	if got.Id != 1 {
		t.Fatalf("unexpected cached summary: %+v", got)
	}
}

// TestEscrowCacheEvictsLRUAtCapacity covers the bounded-size guarantee: once
// the cache is over capacity, the least-recently-used entry is evicted, not
// an arbitrary one — a get() that touches an entry keeps it alive.
func TestEscrowCacheEvictsLRUAtCapacity(t *testing.T) {
	c := newEscrowCache(2, time.Minute)

	c.set(1, EscrowSummary{Id: 1})
	c.set(2, EscrowSummary{Id: 2})

	// Touch id 1 so it becomes more-recently-used than id 2.
	if _, ok := c.get(1); !ok {
		t.Fatal("expected hit for id 1")
	}

	// Inserting a third entry should evict id 2 (least recently used), not id 1.
	c.set(3, EscrowSummary{Id: 3})

	if _, ok := c.get(2); ok {
		t.Fatal("expected id 2 to have been evicted as least-recently-used")
	}
	if _, ok := c.get(1); !ok {
		t.Fatal("expected id 1 to survive eviction (recently touched)")
	}
	if _, ok := c.get(3); !ok {
		t.Fatal("expected id 3 to survive (just inserted)")
	}
	if len(c.items) != 2 {
		t.Fatalf("expected cache to stay at capacity 2, got %d entries", len(c.items))
	}
}

// TestEscrowCacheGetOrFetchDoesNotCacheNilResult covers the deliberate
// "never cache a miss" rule: an escrow id that doesn't exist yet could be
// created later, so every call must re-fetch rather than serving a stale
// nil.
func TestEscrowCacheGetOrFetchDoesNotCacheNilResult(t *testing.T) {
	c := newEscrowCache(10, time.Minute)

	var fetchCalls int32
	fetch := func() (*EscrowSummary, error) {
		atomic.AddInt32(&fetchCalls, 1)
		return nil, nil
	}

	for i := 0; i < 3; i++ {
		summary, err := c.getOrFetch(1, fetch)
		if err != nil {
			t.Fatalf("getOrFetch returned error: %v", err)
		}
		if summary != nil {
			t.Fatalf("expected nil summary, got %+v", summary)
		}
	}

	if got := atomic.LoadInt32(&fetchCalls); got != 3 {
		t.Fatalf("expected fetch to run every time for an uncacheable nil result, got %d calls", got)
	}
}

// TestEscrowCacheGetOrFetchPropagatesError covers the fetch-error path: the
// error is returned as-is and nothing is cached, so a subsequent call
// retries.
func TestEscrowCacheGetOrFetchPropagatesError(t *testing.T) {
	c := newEscrowCache(10, time.Minute)
	wantErr := errors.New("boom")

	_, err := c.getOrFetch(1, func() (*EscrowSummary, error) {
		return nil, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wantErr, got %v", err)
	}
	if _, ok := c.get(1); ok {
		t.Fatal("expected nothing cached after a fetch error")
	}
}

// TestEscrowCacheGetOrFetchCoalescesConcurrentMisses is the singleflight
// coverage: N concurrent callers missing on the same id must trigger
// exactly one underlying fetch, and every caller gets that fetch's result.
func TestEscrowCacheGetOrFetchCoalescesConcurrentMisses(t *testing.T) {
	c := newEscrowCache(10, time.Minute)

	const n = 20
	var fetchCalls int32
	release := make(chan struct{})

	fetch := func() (*EscrowSummary, error) {
		atomic.AddInt32(&fetchCalls, 1)
		<-release // hold every concurrent caller here until they've all queued
		return &EscrowSummary{Id: 1, Status: 0}, nil
	}

	var wg sync.WaitGroup
	results := make([]*EscrowSummary, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = c.getOrFetch(1, fetch)
		}(i)
	}

	// Give every goroutine a chance to reach the singleflight call before
	// releasing the shared fetch.
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&fetchCalls); got != 1 {
		t.Fatalf("expected exactly 1 underlying fetch for %d concurrent callers, got %d", n, got)
	}
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("caller %d got error: %v", i, errs[i])
		}
		if results[i] == nil || results[i].Id != 1 {
			t.Fatalf("caller %d got unexpected result: %+v", i, results[i])
		}
	}
}

// TestGetEscrowUsesCacheOnRepeatLookup is the Service-level integration
// check: a second GetEscrow call for the same id must be served from cache,
// never reaching Transactions.ExecuteScript again.
func TestGetEscrowUsesCacheOnRepeatLookup(t *testing.T) {
	unlockAt, err := cadence.NewUFix64("4102444800.00000000")
	if err != nil {
		t.Fatal(err)
	}
	txSvc := &queryTxService{
		scriptResult: cadence.NewOptional(cadence.NewDictionary([]cadence.KeyValuePair{
			{Key: cadence.String("id"), Value: cadence.NewUInt64(42)},
			{Key: cadence.String("buyer"), Value: cadence.NewAddress(flow.HexToAddress("0x179b6b1cb6755e31"))},
			{Key: cadence.String("seller"), Value: cadence.NewAddress(flow.HexToAddress("0xf3fcd2c1a78f5eee"))},
			{Key: cadence.String("editionId"), Value: cadence.NewUInt64(7)},
			{Key: cadence.String("chipId"), Value: cadence.String("chip-1")},
			{Key: cadence.String("unlockAt"), Value: unlockAt},
			{Key: cadence.String("nonce"), Value: cadence.NewUInt64(1)},
			{Key: cadence.String("certificateId"), Value: cadence.NewUInt64(99)},
			{Key: cadence.String("status"), Value: cadence.NewUInt8(0)},
			{Key: cadence.String("releaseReason"), Value: cadence.NewOptional(nil)},
			{Key: cadence.String("claimed"), Value: cadence.NewBool(false)},
			{Key: cadence.String("claimedAt"), Value: cadence.NewOptional(nil)},
		})),
	}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	first, err := svc.GetEscrow(context.Background(), 42)
	if err != nil {
		t.Fatalf("first GetEscrow returned error: %v", err)
	}
	if first == nil || first.Id != 42 {
		t.Fatalf("unexpected first result: %+v", first)
	}

	second, err := svc.GetEscrow(context.Background(), 42)
	if err != nil {
		t.Fatalf("second GetEscrow returned error: %v", err)
	}
	if second == nil || second.Id != 42 {
		t.Fatalf("unexpected second result: %+v", second)
	}

	if len(txSvc.calls) != 1 {
		t.Fatalf("expected exactly 1 script call across 2 GetEscrow calls for the same id, got %d", len(txSvc.calls))
	}
}
