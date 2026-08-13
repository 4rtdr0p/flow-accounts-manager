package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
)

// mongoDataFromVariables builds a Mongo-style flat Data map from the embedded
// variables.json. This mirrors what PricingStore.GetActive produces: the rates
// as top-level keys of the document.
func mongoDataFromVariables(t *testing.T) map[string]any {
	t.Helper()
	var variables map[string]any
	if err := json.Unmarshal(defaultVariablesJSON, &variables); err != nil {
		t.Fatalf("unmarshal variables.json: %v", err)
	}
	return variables
}

func quoteActiveConfig(updatedAt time.Time, data map[string]any) *datastoremongo.PricingConfiguration {
	return &datastoremongo.PricingConfiguration{
		Domain:        "studio-printing",
		Status:        "active",
		EffectiveFrom: updatedAt.Add(-time.Hour),
		UpdatedAt:     updatedAt,
		Data:          data,
	}
}

func TestQuoteServiceComputesPriceWithHash(t *testing.T) {
	reader := &fakeReader{cfgs: []*datastoremongo.PricingConfiguration{
		quoteActiveConfig(time.Now(), mongoDataFromVariables(t)),
	}}
	active := NewActiveService(reader, time.Minute)
	svc := NewQuoteService(active)

	cfg := Config{
		Process:    "Metal Print",
		Shape:      "Rectangle",
		W:          20,
		L:          30,
		Matcat:     "Canvas",
		Media:      "Aurora Linen Canvas",
		Preset:     "Flat",
		Varnish:    "Matte",
		Present:    "Media only",
		MountPanel: "MaxMetal ACM Panel",
		BarType:    "Stretcher Bar Gallery 1.5in",
		Edge:       "Mirror",
		Moulding:   "Floater Black 1.5in",
		Fulfill:    "Bulk to artist",
		Pack:       "Flat Pack",
		Rush:       "No",
		RunSize:    10,
	}
	res, err := svc.Quote(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Quote returned error: %v", err)
	}

	if res.Hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if res.EngineVersion != EngineVersion {
		t.Fatalf("expected engine version %q, got %q", EngineVersion, res.EngineVersion)
	}
	if res.RatesUpdated.IsZero() {
		t.Fatal("expected rates_updated_at to be set")
	}
	if res.GrandTotal1PC <= 0 {
		t.Fatalf("expected positive grand_total_1pc, got %v", res.GrandTotal1PC)
	}
}

func TestQuoteHashIsDeterministic(t *testing.T) {
	data := mongoDataFromVariables(t)

	h1, err := quoteHash(data)
	if err != nil {
		t.Fatalf("quoteHash returned error: %v", err)
	}
	h2, err := quoteHash(data)
	if err != nil {
		t.Fatalf("quoteHash returned error: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("expected deterministic hash, got %q and %q", h1, h2)
	}
}

func TestQuoteHashChangesWhenRatesChange(t *testing.T) {
	data := mongoDataFromVariables(t)
	base, err := quoteHash(data)
	if err != nil {
		t.Fatalf("quoteHash returned error: %v", err)
	}

	// Mutate a rate and confirm the hash changes.
	rates := data["rates_printing"].(map[string]any)
	rates["mach_cmin"] = 9.999

	changed, err := quoteHash(data)
	if err != nil {
		t.Fatalf("quoteHash returned error: %v", err)
	}
	if changed == base {
		t.Fatal("expected hash to change when rates change")
	}
}

func TestQuoteServiceReturnsErrPricingDisabled(t *testing.T) {
	svc := NewQuoteService(nil)
	_, err := svc.Quote(context.Background(), Config{})
	if err != ErrPricingDisabled {
		t.Fatalf("expected ErrPricingDisabled, got %v", err)
	}
}

func TestQuoteServiceReturnsErrNoActivePricing(t *testing.T) {
	reader := &fakeReader{errs: []error{datastoremongo.ErrNoActivePricing}}
	active := NewActiveService(reader, time.Minute)
	svc := NewQuoteService(active)

	_, err := svc.Quote(context.Background(), Config{
		Process: "Metal Print",
		W:       20,
		L:       30,
		RunSize: 10,
	})
	if err != datastoremongo.ErrNoActivePricing {
		t.Fatalf("expected ErrNoActivePricing, got %v", err)
	}
}

func TestQuoteServiceRejectsInvalidData(t *testing.T) {
	// Missing required rate keys -> LoadDataFromMap must fail.
	reader := &fakeReader{cfgs: []*datastoremongo.PricingConfiguration{
		quoteActiveConfig(time.Now(), map[string]any{"paper_price": 1.25}),
	}}
	active := NewActiveService(reader, time.Minute)
	svc := NewQuoteService(active)

	_, err := svc.Quote(context.Background(), Config{
		Process: "Metal Print",
		W:       20,
		L:       30,
		RunSize: 10,
	})
	if err == nil {
		t.Fatal("expected error for invalid pricing data")
	}
}

func TestQuoteServiceRejectsInvalidConfig(t *testing.T) {
	reader := &fakeReader{cfgs: []*datastoremongo.PricingConfiguration{
		quoteActiveConfig(time.Now(), mongoDataFromVariables(t)),
	}}
	active := NewActiveService(reader, time.Minute)
	svc := NewQuoteService(active)

	valid := Config{
		Process:    "Metal Print",
		Shape:      "Rectangle",
		W:          20,
		L:          30,
		Matcat:     "Canvas",
		Media:      "Aurora Linen Canvas",
		Preset:     "Flat",
		Varnish:    "Matte",
		Present:    "Media only",
		MountPanel: "MaxMetal ACM Panel",
		BarType:    "Stretcher Bar Gallery 1.5in",
		Edge:       "Mirror",
		Moulding:   "Floater Black 1.5in",
		Fulfill:    "Bulk to artist",
		Pack:       "Flat Pack",
		Rush:       "No",
		RunSize:    10,
	}

	tests := []struct {
		name string
		mod  func(c *Config)
	}{
		{"run_size=0", func(c *Config) { c.RunSize = 0 }},
		{"run_size=-5", func(c *Config) { c.RunSize = -5 }},
		{"W=-10", func(c *Config) { c.W = -10 }},
		{"L=-10", func(c *Config) { c.L = -10 }},
		{"BordT=-1", func(c *Config) { c.BordT = -1 }},
		{"BordB=-1", func(c *Config) { c.BordB = -1 }},
		{"BordL=-1", func(c *Config) { c.BordL = -1 }},
		{"BordR=-1", func(c *Config) { c.BordR = -1 }},
		{"baseline (valid) still succeeds", func(c *Config) {}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := valid
			tc.mod(&c)
			_, err := svc.Quote(context.Background(), c)
			if tc.name == "baseline (valid) still succeeds" {
				if err != nil {
					t.Fatalf("expected no error for valid baseline, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected ErrInvalidQuoteConfig, got nil")
			}
			if !errors.Is(err, ErrInvalidQuoteConfig) {
				t.Fatalf("expected error wrapping ErrInvalidQuoteConfig, got %v", err)
			}
		})
	}
}

// TestQuoteServiceFromMapMatchesSpreadsheetGroundTruth exercises the new
// LoadDataFromMap Mongo->engine bridge end-to-end (not LoadData + DefaultData,
// which is what pricing_parity_test.go covers). The Config matches the
// "spreadsheet ground truth" entry in testdata/fixtures.json (Metal Print,
// 20x30, run_size=10) and asserts GrandTotal1PC == 250.0616296302498 within
// centTolerance -- the same tolerance used by pricing_parity_test.go.
// This is the single test that catches subtle map->Data bugs in from_map.go
// (key swaps, unit-conversion mistakes, ordering bugs) that LoadData() can't
// catch because the parity suite never goes through LoadDataFromMap.
func TestQuoteServiceFromMapMatchesSpreadsheetGroundTruth(t *testing.T) {
	reader := &fakeReader{cfgs: []*datastoremongo.PricingConfiguration{
		quoteActiveConfig(time.Now(), mongoDataFromVariables(t)),
	}}
	active := NewActiveService(reader, time.Minute)
	svc := NewQuoteService(active)

	cfg := Config{
		Process:    "Metal Print",
		Shape:      "Rectangle",
		W:          20,
		L:          30,
		Matcat:     "Canvas",
		Media:      "Aurora Linen Canvas",
		Preset:     "Flat",
		Varnish:    "Matte",
		Present:    "Media only",
		MountPanel: "MaxMetal ACM Panel",
		BarType:    "Stretcher Bar Gallery 1.5in",
		Edge:       "Mirror",
		Moulding:   "Floater Black 1.5in",
		Fulfill:    "Bulk to artist",
		Pack:       "Flat Pack",
		Rush:       "No",
		RunSize:    10,
	}

	res, err := svc.Quote(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Quote returned error: %v", err)
	}

	money(t, "grand_total_1pc", res.GrandTotal1PC, 250.0616296302498)
}
