package pricing

import (
	"encoding/json"
	"math"
	"testing"
)

// FuzzLoadDataFromMap fuzzes the new Mongo->engine bridge (LoadDataFromMap
// in from_map.go) against random map[string]any inputs, then composes the
// result through Compute() with the same baseline Config used by
// TestQuoteServiceComputesPriceWithHash and the LoadDataFromMap regression
// test. It only checks that the chain composes safely; the full Tier A
// invariant list already lives in FuzzCompute in pricing_fuzz_test.go.
//
// Seeds: the canonical Mongo-shaped variables map (loaded via the
// embedded defaultVariablesJSON since mongoDataFromVariables(t) requires
// *testing.T) plus explicit variants with each required key deleted
// and a couple of value mutations. Non-parseable bytes are skipped.
// LoadDataFromMap returning a non-nil error is treated as a legitimate
// outcome (e.g. missing required key) and aborts the iteration with no
// further assertions.
func FuzzLoadDataFromMap(f *testing.F) {
	var variables map[string]any
	if err := json.Unmarshal(defaultVariablesJSON, &variables); err != nil {
		f.Fatalf("unmarshal defaultVariablesJSON: %v", err)
	}
	canonical, err := json.Marshal(variables)
	if err != nil {
		f.Fatalf("marshal canonical seed: %v", err)
	}
	f.Add(canonical)

	for _, key := range []string{
		"rates_printing", "rates_presentation", "rates_cutting",
		"rates_fulfillment", "rates_package", "ink_markup",
		"labor", "recipe", "setups",
	} {
		m := make(map[string]any, len(variables))
		for k, v := range variables {
			if k == key {
				continue
			}
			m[k] = v
		}
		b, err := json.Marshal(m)
		if err != nil {
			f.Fatalf("marshal seed (delete %s): %v", key, err)
		}
		f.Add(b)
	}

	for _, mutation := range []struct {
		name string
		path []string
	}{
		{"rates_printing.bed_gap=0", []string{"rates_printing", "bed_gap"}},
		{"rates_printing.min_order=0", []string{"rates_printing", "min_order"}},
	} {
		m := deepCloneMap(variables)
		cur := m
		for i, p := range mutation.path {
			if i == len(mutation.path)-1 {
				cur[p] = 0.0
			} else {
				next, ok := cur[p].(map[string]any)
				if !ok {
					f.Fatalf("mutation %q: path %v not navigable at %q", mutation.name, mutation.path, p)
				}
				cur = next
			}
		}
		b, err := json.Marshal(m)
		if err != nil {
			f.Fatalf("marshal seed (%s): %v", mutation.name, err)
		}
		f.Add(b)
	}

	f.Fuzz(func(t *testing.T, mBytes []byte) {
		var m map[string]any
		if err := json.Unmarshal(mBytes, &m); err != nil {
			t.Skip()
		}

		data, err := LoadDataFromMap(m)
		if err != nil {
			return
		}

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

		res, err := Compute(data, cfg)
		if err != nil {
			t.Errorf("Compute returned error after LoadDataFromMap succeeded: %v; m=%+v", err, m)
			return
		}

		if math.IsNaN(res.GrandTotal1PC) || math.IsInf(res.GrandTotal1PC, 0) {
			t.Errorf("Compute().GrandTotal1PC = %v (NaN/Inf) after LoadDataFromMap; m=%+v", res.GrandTotal1PC, m)
		}
	})
}

func deepCloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if sub, ok := v.(map[string]any); ok {
			out[k] = deepCloneMap(sub)
		} else {
			out[k] = v
		}
	}
	return out
}
