package pricing

import (
	"context"
	"errors"
	"testing"
	"time"

	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
)

// fakeReader is a scripted ActiveConfigReader for tests. Each call consumes the
// next queued (config, error) pair; if none remain it returns the last one.
type fakeReader struct {
	cfgs  []*datastoremongo.PricingConfiguration
	errs  []error
	calls int
}

func (f *fakeReader) GetActive(context.Context) (*datastoremongo.PricingConfiguration, error) {
	i := f.calls
	f.calls++
	if i < len(f.errs) && f.errs[i] != nil {
		return nil, f.errs[i]
	}
	if i < len(f.cfgs) {
		return f.cfgs[i], nil
	}
	return nil, nil
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
	if reader.calls != 1 {
		t.Fatalf("expected 1 read, got %d", reader.calls)
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
	if reader.calls != 1 {
		t.Fatalf("expected cache to serve the second call (1 read), got %d reads", reader.calls)
	}
}

func TestActiveServiceRefreshesOnUpdatedAtChange(t *testing.T) {
	v1 := time.Now()
	v2 := v1.Add(time.Minute)
	reader := &fakeReader{cfgs: []*datastoremongo.PricingConfiguration{
		activeConfig(v1),
		activeConfig(v2),
	}}

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
	if reader.calls != 1 {
		t.Fatalf("expected 1 read within TTL, got %d", reader.calls)
	}

	// Past TTL with a changed updatedAt: the cache is refreshed.
	clock = clock.Add(2 * time.Minute)
	third, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("refreshed Get: %v", err)
	}
	if !third.UpdatedAt.Equal(v2) {
		t.Fatalf("expected refreshed config with updatedAt %v, got %v", v2, third.UpdatedAt)
	}
	if reader.calls != 2 {
		t.Fatalf("expected a re-read after TTL, got %d reads", reader.calls)
	}
}

func TestActiveServiceKeepsCacheWhenUpdatedAtUnchangedAfterTTL(t *testing.T) {
	updated := time.Now()
	reader := &fakeReader{cfgs: []*datastoremongo.PricingConfiguration{
		activeConfig(updated),
		activeConfig(updated),
	}}

	svc := NewActiveService(reader, time.Minute)
	clock := time.Now()
	svc.now = func() time.Time { return clock }

	if _, err := svc.Get(context.Background()); err != nil {
		t.Fatalf("first Get: %v", err)
	}

	// Past TTL but the active row's updatedAt is unchanged: no churn, the
	// cached copy keeps serving and the row is still fetched exactly once.
	clock = clock.Add(2 * time.Minute)
	got, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("Get after TTL: %v", err)
	}
	if !got.UpdatedAt.Equal(updated) {
		t.Fatalf("expected cached updatedAt %v, got %v", updated, got.UpdatedAt)
	}
	if reader.calls != 2 {
		t.Fatalf("expected a re-check after TTL (2 reads), got %d", reader.calls)
	}
}

func TestActiveServiceNilReaderDisabled(t *testing.T) {
	svc := NewActiveService(nil, time.Minute)
	_, err := svc.Get(context.Background())
	if !errors.Is(err, ErrPricingDisabled) {
		t.Fatalf("expected ErrPricingDisabled, got %v", err)
	}
}

func TestActiveServiceNoActiveConfig(t *testing.T) {
	reader := &fakeReader{errs: []error{datastoremongo.ErrNoActivePricing}}
	svc := NewActiveService(reader, time.Minute)

	_, err := svc.Get(context.Background())
	if !errors.Is(err, datastoremongo.ErrNoActivePricing) {
		t.Fatalf("expected ErrNoActivePricing, got %v", err)
	}
}
