package artdrop

import (
	"container/list"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// escrowCacheCapacity and escrowCacheTTL size the in-process cache in front
// of Service.GetEscrow (issue #100). The service runs as a single Quave
// container with no autoscaling, so an in-process cache has no cross-replica
// coherence problem and gets full benefit — no Redis, no extra hop.
//
// 5000 entries is a deliberately generous cap given measured headroom
// (~479MB free out of 512MB at the time this was sized): each EscrowSummary
// is a few hundred bytes, so the whole cache costs a few MB. The cap exists
// for boundedness, not because memory is tight.
const (
	escrowCacheCapacity = 5000
	escrowCacheTTL      = 60 * time.Second
)

// escrowStatusReleased mirrors ArtDropCore.EscrowStatus.Released's rawValue
// (see EscrowSummary.Status's doc comment in types.go: "Status
// 0=Pending/1=Released"). It's the only terminal escrow status — once
// released, a summary never changes again — so a cached entry at this
// status is kept without a TTL instead of being re-fetched every 60s.
const escrowStatusReleased = uint8(1)

// escrowCacheEntry is the cached value for one escrow id. expires is the
// zero time.Time for a terminal (Released) entry, meaning "never expires by
// TTL" — it can still be evicted by the LRU cap.
type escrowCacheEntry struct {
	id       uint64
	summary  EscrowSummary
	terminal bool
	expires  time.Time
}

// escrowCache is a small in-process, TTL + LRU cache for EscrowSummary
// lookups, plus request coalescing for concurrent misses on the same id.
//
// Deliberately not a general-purpose caching package: no external
// dependency was added for this (golang.org/x/sync/singleflight was already
// an indirect dependency via go.mod before this file made it direct), and
// the eviction structure is the textbook container/list + map LRU rather
// than a vendored library, since the whole cache is only ever a few
// thousand small structs.
//
// Deliberately does NOT cache "not found" (a nil summary): an escrow id
// that doesn't exist yet could be created later, so caching a miss risks
// serving a stale nil for an id that has since become real.
type escrowCache struct {
	mu    sync.Mutex
	cap   int
	ttl   time.Duration
	items map[uint64]*list.Element // value is *escrowCacheEntry
	order *list.List               // front = most recently used

	group singleflight.Group
}

func newEscrowCache(capacity int, ttl time.Duration) *escrowCache {
	return &escrowCache{
		cap:   capacity,
		ttl:   ttl,
		items: make(map[uint64]*list.Element),
		order: list.New(),
	}
}

// get returns a copy of the cached summary for id, and whether it was
// found (and not expired). An expired non-terminal entry is evicted here.
func (c *escrowCache) get(id uint64) (*EscrowSummary, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[id]
	if !ok {
		return nil, false
	}
	entry := el.Value.(*escrowCacheEntry)
	if !entry.terminal && time.Now().After(entry.expires) {
		c.order.Remove(el)
		delete(c.items, id)
		return nil, false
	}

	c.order.MoveToFront(el)
	summary := entry.summary
	return &summary, true
}

// set stores summary for id, replacing any existing entry, and evicts the
// least-recently-used entry (or entries — cap changes aren't expected at
// runtime, but the loop is defensive) once the cache is over capacity.
func (c *escrowCache) set(id uint64, summary EscrowSummary) {
	c.mu.Lock()
	defer c.mu.Unlock()

	terminal := summary.Status == escrowStatusReleased
	entry := &escrowCacheEntry{id: id, summary: summary, terminal: terminal}
	if !terminal {
		entry.expires = time.Now().Add(c.ttl)
	}

	if el, ok := c.items[id]; ok {
		el.Value = entry
		c.order.MoveToFront(el)
		return
	}

	el := c.order.PushFront(entry)
	c.items[id] = el

	for c.order.Len() > c.cap {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*escrowCacheEntry).id)
	}
}

// getOrFetch returns the cached summary for id if present and unexpired;
// otherwise it calls fetch exactly once even under concurrent callers for
// the same id (singleflight), caches a non-nil result, and returns it.
//
// The cache is checked again inside the singleflight callback: a caller
// that loses the race to be first only waits for the winner's fetch, but a
// caller that starts a *new* singleflight call after the winner's Do has
// already returned (and cleared the in-flight entry) would otherwise
// re-fetch even though the result is now cached — rechecking closes that
// gap.
func (c *escrowCache) getOrFetch(id uint64, fetch func() (*EscrowSummary, error)) (*EscrowSummary, error) {
	if summary, ok := c.get(id); ok {
		return summary, nil
	}

	key := strconv.FormatUint(id, 10)
	v, err, _ := c.group.Do(key, func() (interface{}, error) {
		if summary, ok := c.get(id); ok {
			return summary, nil
		}

		summary, err := fetch()
		if err != nil {
			return nil, err
		}
		if summary != nil {
			c.set(id, *summary)
		}
		return summary, nil
	})
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, nil
	}
	return v.(*EscrowSummary), nil
}
