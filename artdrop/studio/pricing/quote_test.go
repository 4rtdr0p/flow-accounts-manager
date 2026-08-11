package pricing

import (
	"context"
	"encoding/json"
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

	_, err := svc.Quote(context.Background(), Config{})
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

	_, err := svc.Quote(context.Background(), Config{})
	if err == nil {
		t.Fatal("expected error for invalid pricing data")
	}
}
