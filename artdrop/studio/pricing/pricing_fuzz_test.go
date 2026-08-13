package pricing

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// FuzzCompute fuzzes the Compute(data, cfg) entry point against random
// Config inputs. The seed corpus draws from the 53 spreadsheet-verified
// cases in testdata/fixtures.json plus six explicit edge cases. The fuzzer
// receives each input as raw JSON bytes of a Config.
//
// Tier A only (per subtask scope). A non-nil error from Compute is treated
// as a legitimate outcome (e.g. unknown process has no recipe) and aborts
// the iteration without invariant checks; tier B validity-gated checks and
// tier C cost breakdowns are explicitly out of scope.
func FuzzCompute(f *testing.F) {
	data, err := DefaultData()
	if err != nil {
		f.Fatalf("DefaultData: %v", err)
	}

	// Seed corpus part 1: every existing spreadsheet fixture case.
	// pricing_parity_test.go's loadJSON/mustRead helpers are typed *testing.T
	// and *testing.F embeds testing.TB (not *testing.T), so we cannot pass
	// the *testing.F setup parameter to them; minimal inline loader mirrors
	// what those helpers do (os.ReadFile + json.Unmarshal into the same
	// parityFixtures struct used by TestComputeMatchesStudioPricingFixtures).
	raw, err := os.ReadFile(filepath.Join("testdata", "fixtures.json"))
	if err != nil {
		f.Fatalf("read fixtures.json: %v", err)
	}
	var fixtures parityFixtures
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		f.Fatalf("unmarshal fixtures.json: %v", err)
	}
	for _, tc := range fixtures.Cases {
		seedBytes, err := json.Marshal(tc.Config)
		if err != nil {
			f.Fatalf("marshal seed: %v", err)
		}
		f.Add(seedBytes)
	}

	// Seed corpus part 2: explicit edge cases.
	for _, edge := range []Config{
		{W: 0, L: 0, RunSize: 1},
		{W: -10, L: -30, RunSize: 10},
		{RunSize: 0},
		{RunSize: -5},
		{BordT: -1, BordB: -1, BordL: -1, BordR: -1},
		{Process: ""},
	} {
		seedBytes, _ := json.Marshal(edge)
		f.Add(seedBytes)
	}

	f.Fuzz(func(t *testing.T, cfgBytes []byte) {
		var cfg Config
		if err := json.Unmarshal(cfgBytes, &cfg); err != nil {
			t.Skip()
		}
		res, err := Compute(data, cfg)
		if err != nil {
			return
		}

		for _, fd := range []struct {
			name string
			val  float64
		}{
			{"GrandTotal1PC", res.GrandTotal1PC},
			{"BuildPrice", res.BuildPrice},
			{"RunTotal", res.RunTotal},
			{"CostPiece", res.CostPiece},
			{"JobRevenue", res.JobRevenue},
			{"JobCost", res.JobCost},
			{"JobProfit", res.JobProfit},
			{"JobMargin", res.JobMargin},
			{"PressDays", res.PressDays},
			{"ProfitPerHour", res.ProfitPerHour},
			{"RunPerPrint", res.RunPerPrint},
		} {
			if math.IsNaN(fd.val) || math.IsInf(fd.val, 0) {
				t.Errorf("invariant 1: %s = %v (NaN/Inf); cfg=%+v", fd.name, fd.val, cfg)
			}
		}

		if got := len(res.Volume); got != 8 {
			t.Errorf("invariant 2: len(Volume) = %d, want 8; cfg=%+v", got, cfg)
		}
		if len(res.Volume) > 0 && res.Volume[0].Units != 1 {
			t.Errorf("invariant 3: Volume[0].Units = %d, want 1; cfg=%+v", res.Volume[0].Units, cfg)
		}
		for i, v := range res.Volume {
			if v.Units <= 0 {
				t.Errorf("invariant 4: Volume[%d].Units = %d, want > 0; cfg=%+v", i, v.Units, cfg)
			}
		}
		for i := 1; i < len(res.Volume); i++ {
			if res.Volume[i].Units < res.Volume[i-1].Units {
				t.Errorf("invariant 5: Volume[%d].Units = %d < Volume[%d].Units = %d; cfg=%+v",
					i, res.Volume[i].Units, i-1, res.Volume[i-1].Units, cfg)
			}
		}
		if res.RunSize == 0 {
			t.Errorf("invariant 6: RunSize = 0 (cfg.RunSize <= 0 should coerce but did not); cfg=%+v", cfg)
		}

		// Tier B invariants: validity-gated (same conditions as
		// validateQuoteConfig in quote.go, which is package-private and
		// callable from this _test.go file in the same package).
		if validateQuoteConfig(cfg) != nil {
			return
		}
		minOrder := n(data.rp, "min_order")

		if v := res.GrandTotal1PC; v < minOrder {
			t.Errorf("invariant B1: GrandTotal1PC = %v, want >= minOrder = %v; cfg=%+v", v, minOrder, cfg)
		}
		if v := res.BuildPrice; v < 0 {
			t.Errorf("invariant B2: BuildPrice = %v, want >= 0; cfg=%+v", v, cfg)
		}
		if v := res.RunTotal; v < minOrder {
			t.Errorf("invariant B3: RunTotal = %v, want >= minOrder = %v; cfg=%+v", v, minOrder, cfg)
		}
		if cfg.RunSize <= 1 {
			if got, want := res.RunPerPrint, res.BuildPrice; got != want {
				t.Errorf("invariant B4: RunPerPrint = %v, want == BuildPrice = %v (cfg.RunSize=%d); cfg=%+v", got, want, cfg.RunSize, cfg)
			}
			if len(res.Volume) > 0 {
				if got, want := res.Volume[0].PerPrint, res.BuildPrice; got != want {
					t.Errorf("invariant B5: Volume[0].PerPrint = %v, want == BuildPrice = %v (cfg.RunSize=%d); cfg=%+v", got, want, cfg.RunSize, cfg)
				}
			}
		}
		if got := res.MountAdded; got != "Yes" && got != "No" {
			t.Errorf("invariant B6: MountAdded = %q, want \"Yes\" or \"No\"; cfg=%+v", got, cfg)
		}
	})
}
