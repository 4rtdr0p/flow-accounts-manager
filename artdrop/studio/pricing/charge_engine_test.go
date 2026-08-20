package pricing

import (
	"testing"
)

// TestToEngineConfigRect verifies the wizard->engine translation for a
// rectangular textured print with a flat texture (no brushwork, no rush).
func TestToEngineConfigRect(t *testing.T) {
	raw := map[string]any{
		"application":    "textured",
		"processKey":     "textured-reproductions",
		"shape":          "rect",
		"sizeInches":     []any{float64(40), float64(60)},
		"materialFamily": "paper",
		"mediaKey":       "arches88",
		"texture":        "flat",
		"presentation":   "rolled",
		"addons": map[string]any{
			"packaging": "none",
			"nfc":       "no",
		},
		"rush":          "no",
		"volumeTierQty": float64(10),
	}

	cfg, err := toEngineConfig(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Process != "Textured Reproductions" {
		t.Errorf("expected process Textured Reproductions, got %q", cfg.Process)
	}
	if cfg.Shape != "Rectangle" {
		t.Errorf("expected shape Rectangle, got %q", cfg.Shape)
	}
	if cfg.W != 40 || cfg.L != 60 {
		t.Errorf("expected W=40 L=60, got W=%v L=%v", cfg.W, cfg.L)
	}
	if cfg.Media != "ARCHES 88" {
		t.Errorf("expected media ARCHES 88, got %q", cfg.Media)
	}
	if cfg.Preset != "Flat" {
		t.Errorf("expected preset Flat, got %q", cfg.Preset)
	}
	if cfg.Brush != "No" {
		t.Errorf("expected brush No for flat texture, got %q", cfg.Brush)
	}
	if cfg.Present != "Media only" {
		t.Errorf("expected present Media only, got %q", cfg.Present)
	}
	if cfg.NFC != "No" {
		t.Errorf("expected nfc No, got %q", cfg.NFC)
	}
	if cfg.Rush != "No" {
		t.Errorf("expected rush No, got %q", cfg.Rush)
	}
	if cfg.Pack != "No package" {
		t.Errorf("expected pack No package, got %q", cfg.Pack)
	}
	if cfg.Varnish != "Matte" {
		t.Errorf("expected varnish Matte, got %q", cfg.Varnish)
	}
	if cfg.Edge != "Mirror" {
		t.Errorf("expected edge Mirror, got %q", cfg.Edge)
	}
	if cfg.Fulfill != "Bulk to artist" {
		t.Errorf("expected fulfill Bulk to artist, got %q", cfg.Fulfill)
	}
}

// TestToEngineConfigCircle verifies a circular print uses the single size for
// both W and L.
func TestToEngineConfigCircle(t *testing.T) {
	raw := map[string]any{
		"application":    "acrylic",
		"shape":          "circle",
		"sizeInches":     []any{float64(30)},
		"materialFamily": "acrylic",
		"mediaKey":       "acrylic3",
		"texture":        "flat",
		"presentation":   "mounted",
		"addons": map[string]any{
			"nfc": "eng",
		},
	}

	cfg, err := toEngineConfig(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Shape != "Circle" {
		t.Errorf("expected shape Circle, got %q", cfg.Shape)
	}
	if cfg.W != 30 || cfg.L != 30 {
		t.Errorf("expected W=30 L=30 for circle, got W=%v L=%v", cfg.W, cfg.L)
	}
	if cfg.Process != "Acrylic Print" {
		t.Errorf("expected process Acrylic Print, got %q", cfg.Process)
	}
	if cfg.Media != "Museum Acrylic" {
		t.Errorf("expected media Museum Acrylic, got %q", cfg.Media)
	}
	if cfg.NFC != "Yes" {
		t.Errorf("expected nfc Yes, got %q", cfg.NFC)
	}
	if cfg.Present != "Mounted" {
		t.Errorf("expected present Mounted, got %q", cfg.Present)
	}
}

// TestToEngineConfigBrushwork verifies that a textured application with a
// non-flat texture enables the brushwork pass.
func TestToEngineConfigBrushwork(t *testing.T) {
	raw := map[string]any{
		"application":    "textured",
		"shape":          "rect",
		"sizeInches":     []any{float64(40), float64(60)},
		"materialFamily": "paper",
		"mediaKey":       "arches88",
		"texture":        "medium",
		"presentation":   "rolled",
	}

	cfg, err := toEngineConfig(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Preset != "Medium" {
		t.Errorf("expected preset Medium, got %q", cfg.Preset)
	}
	if cfg.Brush != "Yes" {
		t.Errorf("expected brush Yes for medium texture, got %q", cfg.Brush)
	}
}

// TestToEngineConfigCustomTexture verifies custom texture maps to the Custom
// preset and carries the textureMm height.
func TestToEngineConfigCustomTexture(t *testing.T) {
	raw := map[string]any{
		"application":    "textured",
		"shape":          "rect",
		"sizeInches":     []any{float64(40), float64(60)},
		"materialFamily": "paper",
		"mediaKey":       "arches88",
		"texture":        "custom",
		"textureMm":      float64(3),
		"presentation":   "rolled",
	}

	cfg, err := toEngineConfig(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Preset != "Custom" {
		t.Errorf("expected preset Custom, got %q", cfg.Preset)
	}
	if cfg.TexMM != 3 {
		t.Errorf("expected tex_mm 3, got %v", cfg.TexMM)
	}
	if cfg.Brush != "Yes" {
		t.Errorf("expected brush Yes for custom texture with height, got %q", cfg.Brush)
	}
}

// TestToEngineConfigRush verifies the rush boolean maps to Yes/No.
func TestToEngineConfigRush(t *testing.T) {
	raw := map[string]any{
		"application":    "textured",
		"shape":          "rect",
		"sizeInches":     []any{float64(40), float64(60)},
		"materialFamily": "paper",
		"mediaKey":       "arches88",
		"texture":        "flat",
		"presentation":   "rolled",
		"rush":           true,
	}

	cfg, err := toEngineConfig(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Rush != "Yes" {
		t.Errorf("expected rush Yes, got %q", cfg.Rush)
	}
}

// TestToEngineConfigFallbacks verifies the adapter fills sensible defaults when
// the wizard config is sparse (legacy quote).
func TestToEngineConfigFallbacks(t *testing.T) {
	raw := map[string]any{
		"application": "bespoke",
		"shape":       "rect",
		"sizeInches":  []any{float64(20), float64(30)},
	}

	cfg, err := toEngineConfig(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Process != "BeSpoke Custom" {
		t.Errorf("expected process BeSpoke Custom, got %q", cfg.Process)
	}
	// No mediaKey and no materialFamily -> last-resort default.
	if cfg.Media != "ARCHES 88" {
		t.Errorf("expected media fallback ARCHES 88, got %q", cfg.Media)
	}
	if cfg.Preset != "Flat" {
		t.Errorf("expected preset fallback Flat, got %q", cfg.Preset)
	}
	if cfg.Present != "Media only" {
		t.Errorf("expected present fallback Media only, got %q", cfg.Present)
	}
	if cfg.NFC != "No" {
		t.Errorf("expected nfc fallback No, got %q", cfg.NFC)
	}
	if cfg.Brush != "No" {
		t.Errorf("expected brush No for bespoke, got %q", cfg.Brush)
	}
}

// TestToEngineConfigNil verifies a nil config is rejected.
func TestToEngineConfigNil(t *testing.T) {
	if _, err := toEngineConfig(nil); err == nil {
		t.Fatal("expected error for nil config, got nil")
	}
}

// TestMaxRunSize verifies the quantity cap is the maximum Units across the
// batches Compute produces, the same authoritative source that backs the
// price, and not an independently maintained maximum.
func TestMaxRunSize(t *testing.T) {
	volume := []VolumePrice{
		{Label: "1 print", Units: 1},
		{Label: "1 row", Units: 4},
		{Label: "Full bed", Units: 8},
		{Label: "12 beds", Units: 96},
	}
	if got := maxRunSize(volume); got != 96 {
		t.Errorf("expected max run size 96, got %d", got)
	}
}

// TestMaxRunSizeDoesNotDependOnOrdering pins that the cap is computed as a
// true maximum, not by reading the last element: Volume happens to be built
// ascending today, but this is a security check, so it must not silently go
// wrong if a future change reorders batches or inserts a tier out of order.
func TestMaxRunSizeDoesNotDependOnOrdering(t *testing.T) {
	volume := []VolumePrice{
		{Label: "Full bed", Units: 8},
		{Label: "12 beds", Units: 96},
		{Label: "1 print", Units: 1},
		{Label: "1 row", Units: 4},
	}
	if got := maxRunSize(volume); got != 96 {
		t.Errorf("expected max run size 96 regardless of slice order, got %d", got)
	}
}

// TestMaxRunSizeEmpty verifies an empty batches slice reports no cap (0)
// rather than panicking.
func TestMaxRunSizeEmpty(t *testing.T) {
	if got := maxRunSize(nil); got != 0 {
		t.Errorf("expected 0 for an empty batches slice, got %d", got)
	}
}
