package pricing

import (
	"math"
	"testing"
)

func TestGeometryGuardsAgainstZeroDenominator(t *testing.T) {
	data, err := DefaultData()
	if err != nil {
		t.Fatalf("DefaultData: %v", err)
	}
	data.rp["bed_gap"] = 0.0

	cfg := Config{
		Process: "Metal Print",
		Shape:   "Rectangle",
		W:       0,
		L:       0,
		RunSize: 1,
	}

	res, err := Compute(data, cfg)
	if err != nil {
		t.Fatalf("Compute returned error: %v", err)
	}

	if res.Geometry.EN != 1 {
		t.Fatalf("expected EN=1 with zero denominator, got %d", res.Geometry.EN)
	}
	if res.Geometry.EMaxRows != 1 {
		t.Fatalf("expected EMaxRows=1 with zero denominator, got %d", res.Geometry.EMaxRows)
	}
	if math.IsNaN(res.GrandTotal1PC) || math.IsInf(res.GrandTotal1PC, 0) {
		t.Fatalf("GrandTotal1PC must be finite, got %v", res.GrandTotal1PC)
	}
	if res.GrandTotal1PC <= 0 {
		t.Fatalf("expected positive GrandTotal1PC, got %v", res.GrandTotal1PC)
	}
}
