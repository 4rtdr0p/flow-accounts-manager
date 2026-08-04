package pricing

import (
	"context"
	"errors"
	"sync"
	"time"

	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
)

// ErrPricingDisabled is returned when the service has no Mongo reader
// configured (MONGO_URI empty -> pricing features disabled).
var ErrPricingDisabled = errors.New("studio pricing is disabled")

// ActiveConfigReader reads the active studio-printing pricing configuration.
// It is implemented by *datastoremongo.PricingStore.
type ActiveConfigReader interface {
	GetActive(ctx context.Context) (*datastoremongo.PricingConfiguration, error)
}

// ActiveService serves the active studio-printing pricing configuration
// (GET /studio/pricing/active). It keeps the active row cached in process
// memory so requests don't hit Mongo every time, and refreshes the cache only
// when the active row's updatedAt changed.
type ActiveService struct {
	reader ActiveConfigReader
	ttl    time.Duration
	now    func() time.Time // clock, overridable in tests

	mu      sync.Mutex
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
// cache while it is fresh. Once the cache is stale, Mongo is re-read and the
// cached value is replaced only when the active row's updatedAt changed, so an
// unchanged row keeps serving the cached copy.
func (s *ActiveService) Get(ctx context.Context) (*datastoremongo.PricingConfiguration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.reader == nil {
		return nil, ErrPricingDisabled
	}

	now := s.now()
	if s.cached != nil && now.Sub(s.fetched) < s.ttl {
		return s.cached, nil
	}

	fresh, err := s.reader.GetActive(ctx)
	if err != nil {
		if errors.Is(err, datastoremongo.ErrNoActivePricing) {
			// The active row no longer exists; stop serving the stale copy.
			s.cached = nil
		}
		return nil, err
	}

	if s.cached == nil || !fresh.UpdatedAt.Equal(s.cached.UpdatedAt) {
		s.cached = fresh
	}
	s.fetched = now

	return s.cached, nil
}
