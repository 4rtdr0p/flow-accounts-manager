package pricing

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
)

// fakeReader is a scripted ActiveConfigReader for tests. GetActive consumes the
// next (config, error) pair in order; GetActiveUpdatedAt consumes the next
// (updatedAt, error) pair independently. delay optionally simulates a slow
// Mongo read.
type fakeReader struct {
	cfgs       []*datastoremongo.PricingConfiguration
	errs       []error
	updatedAts []time.Time
	lightErrs  []error
	delay      time.Duration

	fullCalls  int
	lightCalls int
}

func (f *fakeReader) GetActive(context.Context) (*datastoremongo.PricingConfiguration, error) {
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	i := f.fullCalls
	f.fullCalls++
	if i < len(f.errs) && f.errs[i] != nil {
		return nil, f.errs[i]
	}
	if i < len(f.cfgs) {
		return f.cfgs[i], nil
	}
	return nil, nil
}

func (f *fakeReader) GetActiveUpdatedAt(context.Context) (time.Time, error) {
	i := f.lightCalls
	f.lightCalls++
	if i < len(f.lightErrs) && f.lightErrs[i] != nil {
		return time.Time{}, f.lightErrs[i]
	}
	if i < len(f.updatedAts) {
		return f.updatedAts[i], nil
	}
	return time.Time{}, nil
}

func activeConfig(updatedAt time.Time) *datastoremongo.PricingConfiguration {
	return &datastoremongo.PricingConfiguration{
		Domain:        "studio-printing",
		Status:        "active",
		EffectiveFrom: updatedAt.Add(-time.Hour),
		UpdatedAt:     updatedAt,
		Data:          map[string]any{"paper_price": 1.25},
	}
}

func TestActiveServiceGetReturnsActiveConfig(t *testing.T) {
	reader := &fakeReader{cfgs: []*datastoremongo.PricingConfiguration{activeConfig(time.Now())}}
	svc := NewActiveService(reader, time.Minute)

	got, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || got.Domain != "studio-printing" {
		t.Fatalf("expected active studio-printing config, got %+v", got)
	}
	if reader.fullCalls != 1 {
		t.Fatalf("expected 1 full read, got %d", reader.fullCalls)
	}
}

func TestActiveServiceCacheServesWithinTTL(t *testing.T) {
	reader := &fakeReader{cfgs: []*datastoremongo.PricingConfiguration{activeConfig(time.Now())}}
	svc := NewActiveService(reader, time.Minute)

	if _, err := svc.Get(context.Background()); err != nil {
		t.Fatalf("first Get: %v", err)
	}
	if _, err := svc.Get(context.Background()); err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if reader.fullCalls != 1 || reader.lightCalls != 0 {
		t.Fatalf("expected cache to serve the second call (1 full read, 0 light), got full=%d light=%d", reader.fullCalls, reader.lightCalls)
	}
}

func TestActiveServiceRefreshesOnUpdatedAtChange(t *testing.T) {
	v1 := time.Now()
	v2 := v1.Add(time.Minute)
	reader := &fakeReader{
		cfgs:       []*datastoremongo.PricingConfiguration{activeConfig(v1), activeConfig(v2)},
		updatedAts: []time.Time{v2},
	}

	svc := NewActiveService(reader, time.Minute)
	clock := time.Now()
	svc.now = func() time.Time { return clock }

	first, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	if !first.UpdatedAt.Equal(v1) {
		t.Fatalf("expected first config with updatedAt %v, got %v", v1, first.UpdatedAt)
	}

	// Within TTL: same updatedAt served from cache, no re-read.
	second, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("cached Get: %v", err)
	}
	if !second.UpdatedAt.Equal(v1) {
		t.Fatalf("expected cached config, got updatedAt %v", second.UpdatedAt)
	}
	if reader.fullCalls != 1 || reader.lightCalls != 0 {
		t.Fatalf("expected 1 full read within TTL, got full=%d light=%d", reader.fullCalls, reader.lightCalls)
	}

	// Past TTL with a changed updatedAt: the light projection detects the
	// change and the cache is refreshed.
	clock = clock.Add(2 * time.Minute)
	third, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("refreshed Get: %v", err)
	}
	if !third.UpdatedAt.Equal(v2) {
		t.Fatalf("expected refreshed config with updatedAt %v, got %v", v2, third.UpdatedAt)
	}
	if reader.fullCalls != 2 || reader.lightCalls != 1 {
		t.Fatalf("expected 2 full reads + 1 light after change, got full=%d light=%d", reader.fullCalls, reader.lightCalls)
	}
}

func TestActiveServiceKeepsCacheWhenUpdatedAtUnchangedAfterTTL(t *testing.T) {
	updated := time.Now()
	reader := &fakeReader{
		cfgs:       []*datastoremongo.PricingConfiguration{activeConfig(updated)},
		updatedAts: []time.Time{updated},
	}

	svc := NewActiveService(reader, time.Minute)
	clock := time.Now()
	svc.now = func() time.Time { return clock }

	if _, err := svc.Get(context.Background()); err != nil {
		t.Fatalf("first Get: %v", err)
	}

	// Past TTL but the active row's updatedAt is unchanged: only the light
	// projection runs, the full document is not re-read and the cached copy is
	// kept.
	clock = clock.Add(2 * time.Minute)
	got, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("Get after TTL: %v", err)
	}
	if !got.UpdatedAt.Equal(updated) {
		t.Fatalf("expected cached updatedAt %v, got %v", updated, got.UpdatedAt)
	}
	if reader.fullCalls != 1 || reader.lightCalls != 1 {
		t.Fatalf("expected 1 full read + 1 light check (no churn), got full=%d light=%d", reader.fullCalls, reader.lightCalls)
	}
}

func TestActiveServiceNilReaderDisabled(t *testing.T) {
	svc := NewActiveService(nil, time.Minute)
	_, err := svc.Get(context.Background())
	if !errors.Is(err, ErrPricingDisabled) {
		t.Fatalf("expected ErrPricingDisabled, got %v", err)
	}
}

func TestActiveServiceNoActiveConfigCold(t *testing.T) {
	reader := &fakeReader{errs: []error{datastoremongo.ErrNoActivePricing}}
	svc := NewActiveService(reader, time.Minute)

	_, err := svc.Get(context.Background())
	if !errors.Is(err, datastoremongo.ErrNoActivePricing) {
		t.Fatalf("expected ErrNoActivePricing, got %v", err)
	}
}

func TestActiveServiceClearsCacheWhenActiveRowDisappears(t *testing.T) {
	v1 := time.Now()
	reader := &fakeReader{
		cfgs:      []*datastoremongo.PricingConfiguration{activeConfig(v1)},
		errs:      []error{nil, datastoremongo.ErrNoActivePricing},
		lightErrs: []error{datastoremongo.ErrNoActivePricing},
	}

	svc := NewActiveService(reader, time.Minute)
	clock := time.Now()
	svc.now = func() time.Time { return clock }

	if _, err := svc.Get(context.Background()); err != nil {
		t.Fatalf("first Get: %v", err)
	}
	if reader.fullCalls != 1 {
		t.Fatalf("expected 1 full read, got %d", reader.fullCalls)
	}

	// Past TTL the active row disappears: the light projection reports
	// ErrNoActivePricing and the stale cache is cleared.
	clock = clock.Add(2 * time.Minute)
	if _, err := svc.Get(context.Background()); !errors.Is(err, datastoremongo.ErrNoActivePricing) {
		t.Fatalf("expected ErrNoActivePricing after row disappears, got %v", err)
	}
	if reader.lightCalls != 1 {
		t.Fatalf("expected 1 light check, got %d", reader.lightCalls)
	}

	// A subsequent Get must re-query (cache was cleared), not serve a stale row.
	if _, err := svc.Get(context.Background()); !errors.Is(err, datastoremongo.ErrNoActivePricing) {
		t.Fatalf("expected ErrNoActivePricing on re-query, got %v", err)
	}
	if reader.fullCalls != 2 {
		t.Fatalf("expected cache cleared and a second full read, got %d", reader.fullCalls)
	}
}

func TestActiveServiceConcurrentStaleRequestsSingleRead(t *testing.T) {
	reader := &fakeReader{
		cfgs:  []*datastoremongo.PricingConfiguration{activeConfig(time.Now())},
		delay: 20 * time.Millisecond,
	}
	svc := NewActiveService(reader, time.Minute)

	const workers = 8
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cfg, err := svc.Get(context.Background())
			if err != nil {
				errCh <- err
				return
			}
			if cfg == nil || cfg.Domain != "studio-printing" {
				errCh <- errors.New("unexpected config returned")
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent Get failed: %v", err)
	}

	if reader.fullCalls != 1 {
		t.Fatalf("expected concurrent stale requests to collapse into 1 full read, got %d", reader.fullCalls)
	}
}
