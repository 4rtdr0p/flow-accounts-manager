package pricing

import (
	"context"
	"errors"
	"sync"
	"time"

	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
	log "github.com/sirupsen/logrus"
)

// ErrPricingDisabled is returned when the service has no Mongo reader
// configured (MONGO_URI empty -> pricing features disabled).
var ErrPricingDisabled = errors.New("studio pricing is disabled")

// ActiveConfigReader reads the active studio-printing pricing configuration.
// It is implemented by *datastoremongo.PricingStore.
type ActiveConfigReader interface {
	GetActive(ctx context.Context) (*datastoremongo.PricingConfiguration, error)
	GetActiveUpdatedAt(ctx context.Context) (time.Time, error)
}

// ActiveService serves the active studio-printing pricing configuration
// (GET /studio/pricing/active). It keeps the active row cached in process
// memory so requests don't hit Mongo every time. A stale cache is re-validated
// with a lightweight updatedAt projection and only re-fetched in full when the
// active row's updatedAt changed.
type ActiveService struct {
	reader ActiveConfigReader
	ttl    time.Duration
	now    func() time.Time // clock, overridable in tests

	mu      sync.RWMutex
	cached  *datastoremongo.PricingConfiguration
	fetched time.Time // last time the cache was validated against Mongo
}

// NewActiveService creates a cache-backed service for the active pricing
// configuration. A nil reader (Mongo disabled) makes Get return
// ErrPricingDisabled. ttl controls how long the cache is served before Mongo is
// re-checked; a non-positive ttl defaults to 60s.
func NewActiveService(reader ActiveConfigReader, ttl time.Duration) *ActiveService {
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	return &ActiveService{reader: reader, ttl: ttl, now: time.Now}
}

// Get returns the active pricing configuration, serving from the in-memory
// cache while it is fresh. Once the cache is stale, Mongo is re-checked with a
// lightweight updatedAt projection; the full configuration is re-fetched only
// when the active row's updatedAt changed. Cache reads take a read lock so
// concurrent requests never block on a Mongo refresh.
func (s *ActiveService) Get(ctx context.Context) (*datastoremongo.PricingConfiguration, error) {
	if s.reader == nil {
		return nil, ErrPricingDisabled
	}

	now := s.now()
	s.mu.RLock()
	cached := s.cached
	if cached != nil && now.Sub(s.fetched) < s.ttl {
		s.mu.RUnlock()
		return cached, nil
	}
	s.mu.RUnlock()

	// Cache is stale or empty: take the write lock, re-check under it, then
	// refresh. Concurrent stale requests collapse into a single refresh.
	s.mu.Lock()
	defer s.mu.Unlock()

	now = s.now()
	if s.cached != nil && now.Sub(s.fetched) < s.ttl {
		return s.cached, nil
	}

	// A cached row just needs its updatedAt re-validated before re-reading the
	// full (potentially large) configuration document.
	if s.cached != nil {
		updatedAt, err := s.reader.GetActiveUpdatedAt(ctx)
		if err != nil {
			if errors.Is(err, datastoremongo.ErrNoActivePricing) {
				// The active row no longer exists; stop serving the stale copy.
				s.cached = nil
			}
			return nil, err
		}
		if updatedAt.Equal(s.cached.UpdatedAt) {
			s.fetched = now
			return s.cached, nil
		}
	}

	fresh, err := s.reader.GetActive(ctx)
	if err != nil {
		if errors.Is(err, datastoremongo.ErrNoActivePricing) {
			s.cached = nil
		}
		return nil, err
	}

	s.cached = fresh
	s.fetched = now
	log.WithField("updatedAt", fresh.UpdatedAt).Debug("refreshed active studio pricing cache")
	return s.cached, nil
}
