package pricing

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

const centTolerance = 0.005

type parityFixtures struct {
	Cases []parityCase `json:"cases"`
}

type parityCase struct {
	Name   string         `json:"name"`
	Config Config         `json:"cfg"`
	Expect expectedResult `json:"expect"`
}

type expectedResult struct {
	GrandTotal1PC float64          `json:"grand_total_1pc"`
	BuildPrice    float64          `json:"build_price"`
	BuildPriceRow float64          `json:"build_price_row"`
	SetupFee      float64          `json:"setup_fee"`
	RunPerPrint   float64          `json:"run_perprint"`
	RunTotal      float64          `json:"run_total"`
	Printing1Off  float64          `json:"printing_1off"`
	MediaUnit     float64          `json:"media_unit"`
	CuttingPrice  float64          `json:"cutting_price"`
	Presentation  float64          `json:"presentation_price"`
	Package       float64          `json:"package_price"`
	Fulfillment   float64          `json:"fulfill_price"`
	AddonsPP      float64          `json:"addons_pp_price"`
	AddonsOnce    float64          `json:"addons_once_price"`
	Tex1Off       float64          `json:"tex_1off"`
	Brush1Off     float64          `json:"brush_1off"`
	EN            int              `json:"e_N"`
	EMaxRows      int              `json:"e_maxrows"`
	MountAdded    string           `json:"mount_added"`
	Volume        []expectedVolume `json:"volume"`
}

type expectedVolume struct {
	Units    int     `json:"units"`
	PerPrint float64 `json:"per_print"`
	JobTotal float64 `json:"job_total"`
}

func TestComputeMatchesStudioPricingFixtures(t *testing.T) {
	data := loadParityData(t)
	fixtures := loadJSON[parityFixtures](t, "fixtures.json")

	if len(fixtures.Cases) == 0 {
		t.Fatal("expected at least one pricing parity case")
	}

	for _, tc := range fixtures.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			got, err := Compute(data, tc.Config)
			if err != nil {
				t.Fatalf("Compute returned error: %v", err)
			}

			assertResultMatches(t, got, tc.Expect)
		})
	}
}

func TestDefaultDataMatchesStudioPricingFixtures(t *testing.T) {
	data, err := DefaultData()
	if err != nil {
		t.Fatalf("DefaultData returned error: %v", err)
	}
	fixtures := loadJSON[parityFixtures](t, "fixtures.json")
	if len(fixtures.Cases) == 0 {
		t.Fatal("expected at least one pricing parity case")
	}

	got, err := Compute(data, fixtures.Cases[0].Config)
	if err != nil {
		t.Fatalf("Compute returned error: %v", err)
	}
	assertResultMatches(t, got, fixtures.Cases[0].Expect)
}

func loadParityData(t *testing.T) Data {
	t.Helper()

	data, err := DefaultData()
	if err != nil {
		t.Fatalf("DefaultData returned error: %v", err)
	}
	return data
}

func loadJSON[T any](t *testing.T, parts ...string) T {
	t.Helper()

	var out T
	if err := json.Unmarshal(mustRead(t, parts...), &out); err != nil {
		t.Fatalf("unmarshal %s: %v", filepath.Join(parts...), err)
	}
	return out
}

func mustRead(t *testing.T, parts ...string) []byte {
	t.Helper()

	path := filepath.Join(append([]string{"testdata"}, parts...)...)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

func money(t *testing.T, name string, got, want float64) {
	t.Helper()

	if math.Abs(got-want) >= centTolerance {
		t.Fatalf("%s: got %.12f, want %.12f (delta %.12f)", name, got, want, math.Abs(got-want))
	}
}

func assertResultMatches(t *testing.T, got Result, want expectedResult) {
	t.Helper()

	money(t, "grand_total_1pc", got.GrandTotal1PC, want.GrandTotal1PC)
	money(t, "build_price", got.BuildPrice, want.BuildPrice)
	money(t, "build_price_row", got.BuildPriceRow, want.BuildPriceRow)
	money(t, "setup_fee", got.SetupFee, want.SetupFee)
	money(t, "run_perprint", got.RunPerPrint, want.RunPerPrint)
	money(t, "run_total", got.RunTotal, want.RunTotal)
	money(t, "printing_1off", got.Printing1Off, want.Printing1Off)
	money(t, "media_unit", got.MediaUnit, want.MediaUnit)
	money(t, "cutting_price", got.Cutting.Price, want.CuttingPrice)
	money(t, "presentation_price", got.Presentation.Price, want.Presentation)
	money(t, "package_price", got.Package.Price, want.Package)
	money(t, "fulfill_price", got.Fulfillment.Price, want.Fulfillment)
	money(t, "addons_pp_price", got.Addons.PPPrice, want.AddonsPP)
	money(t, "addons_once_price", got.Addons.OncePrice, want.AddonsOnce)
	money(t, "tex_1off", got.Printing.Tex1Off, want.Tex1Off)
	money(t, "brush_1off", got.Printing.Brush1Off, want.Brush1Off)

	if got.Geometry.EN != want.EN {
		t.Fatalf("e_N: got %d, want %d", got.Geometry.EN, want.EN)
	}
	if got.Geometry.EMaxRows != want.EMaxRows {
		t.Fatalf("e_maxrows: got %d, want %d", got.Geometry.EMaxRows, want.EMaxRows)
	}
	if got.MountAdded != want.MountAdded {
		t.Fatalf("mount_added: got %q, want %q", got.MountAdded, want.MountAdded)
	}
	if len(got.Volume) != len(want.Volume) {
		t.Fatalf("volume length: got %d, want %d", len(got.Volume), len(want.Volume))
	}
	for i := range want.Volume {
		if got.Volume[i].Units != want.Volume[i].Units {
			t.Fatalf("volume[%d].units: got %d, want %d", i, got.Volume[i].Units, want.Volume[i].Units)
		}
		money(t, "volume.per_print", got.Volume[i].PerPrint, want.Volume[i].PerPrint)
		money(t, "volume.job_total", got.Volume[i].JobTotal, want.Volume[i].JobTotal)
	}
}
