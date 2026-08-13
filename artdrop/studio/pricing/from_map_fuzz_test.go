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
// Seeds: the canonical Mongo-shaped variables map (loaded via the embedded
// defaultVariablesJSON and translated to the Payload CMS key names that
// LoadDataFromMap expects, since mongoDataFromVariables(t) requires
// *testing.T) plus explicit variants with each required key deleted and a
// couple of value mutations. Non-parseable bytes are skipped.
// LoadDataFromMap returning a non-nil error is treated as a legitimate
// outcome (e.g. missing required key) and aborts the iteration with no
// further assertions.
func FuzzLoadDataFromMap(f *testing.F) {
	var variables map[string]any
	if err := json.Unmarshal(defaultVariablesJSON, &variables); err != nil {
		f.Fatalf("unmarshal defaultVariablesJSON: %v", err)
	}
	// Translate the internal engine key names (rates_printing, ...) to the
	// Payload CMS key names (printing, ...) that LoadDataFromMap expects, and
	// nest ink_markup under ink.markup.
	mongo := internalToMongoKeys(variables)
	canonical, err := json.Marshal(mongo)
	if err != nil {
		f.Fatalf("marshal canonical seed: %v", err)
	}
	f.Add(canonical)

	for _, key := range []string{
		"printing", "presentation", "cutting",
		"fulfillment", "package", "ink",
		"labor", "recipes", "processSetups",
	} {
		m := make(map[string]any, len(mongo))
		for k, v := range mongo {
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
		{"printing.bed_gap=0", []string{"printing", "bed_gap"}},
		{"printing.min_order=0", []string{"printing", "min_order"}},
	} {
		m := deepCloneMap(mongo)
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

// internalToMongoKeys translates a variables.json-shaped map (internal engine
// key names: rates_printing, ..., ink_markup, recipe, setups) into the Payload
// CMS key names that LoadDataFromMap expects (printing, ..., ink.markup,
// recipes, processSetups). It is the inverse of normalizeMongoData and is used
// to build fuzz seeds that match the real Mongo schema.
func internalToMongoKeys(m map[string]any) map[string]any {
	// Inverse of mongoKeyAliases.
	aliases := map[string]string{
		"rates_printing":     "printing",
		"rates_presentation": "presentation",
		"rates_cutting":      "cutting",
		"rates_fulfillment":  "fulfillment",
		"rates_package":      "package",
		"recipe":             "recipes",
		"setups":             "processSetups",
		"labor":              "labor",
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if k == "ink_markup" {
			out["ink"] = map[string]any{"markup": v}
			continue
		}
		if alias, ok := aliases[k]; ok {
			out[alias] = v
			continue
		}
		out[k] = v
	}
	return out
}
