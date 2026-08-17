package pricing

import (
	"context"
	"fmt"
	"math"
	"strconv"
)

// ChargeEngine recomputes the exact price for a Studio stock request at charge
// time (#71). It translates the quote's config snapshot (as stored in the
// studio-quotes Mongo document) into a Config, applies the requested quantity
// as the run size, and prices it with the active rates.
//
// It deliberately exposes a primitive-only method signature so it satisfies the
// studio.PriceEngine interface structurally, without the studio package having
// to import this package (which would create an import cycle through handlers).
type ChargeEngine struct {
	svc *QuoteService
}

// NewChargeEngine creates a charge engine backed by the given quote service.
func NewChargeEngine(svc *QuoteService) *ChargeEngine {
	return &ChargeEngine{svc: svc}
}

// Quote recomputes the price for the given quote config snapshot and run size,
// returning the server-computed amount in cents plus the pricing hash and
// engine version that produced it.
func (e *ChargeEngine) Quote(ctx context.Context, config map[string]any, runSize int) (amountCents int64, pricingHash string, engineVersion string, err error) {
	if e == nil || e.svc == nil {
		return 0, "", "", ErrPricingDisabled
	}

	cfg, err := toEngineConfig(config)
	if err != nil {
		return 0, "", "", fmt.Errorf("translate quote config: %w", err)
	}
	cfg.RunSize = runSize

	result, err := e.svc.Quote(ctx, cfg)
	if err != nil {
		return 0, "", "", err
	}

	// RunTotal is the total price for the run size (the requested quantity),
	// already floored to the minimum order.
	amountCents = int64(math.Round(result.RunTotal * 100))
	return amountCents, result.Hash, result.EngineVersion, nil
}

// ---------------------------------------------------------------------------
// Wizard -> engine adapter
//
// Ported 1:1 from Payload-Galaxy src/lib/studio/pricing-engine/adapter.ts
// (develop branch). The wizard exposes a simplified config (~12 high-level
// fields); the engine expects ~30 granular fields. This adapter translates and
// fills defaults for fields the wizard doesn't expose yet.
// ---------------------------------------------------------------------------

// PROCESS_MAP maps the wizard application/processKey to the engine process
// name.
var PROCESS_MAP = map[string]string{
	"textured":       "Textured Reproductions",
	"nextlevel":      "Textured Reproductions", // same process, different config in prototypes — engine maps recipe by name
	"bespoke":        "BeSpoke Custom",
	"droptix":        "Lenticular on Acrylic",
	"pyramid":        "Foil / Holographic",
	"holographic":    "Foil / Holographic",
	"metal":          "Metal Print",
	"acrylic":        "Acrylic Print",
	"canvas":         "Canvas Print",
	"heavy-archival": "Heavy Archival Print",
}

// MEDIA_MAP maps the wizard mediaKey to the engine media product name.
var MEDIA_MAP = map[string]string{
	"epson-luster":    "Epson Premium Luster",
	"hahn-photorag":   "Hahnemühle Photo Rag Baryta",
	"hahn-fineart":    "Hahnemühle FineArt Baryta",
	"hahn-metallic":   "Hahnemühle Photo Rag Metallic",
	"arches88":        "ARCHES 88",
	"somerset":        "Somerset Enhanced Velvet",
	"rives":           "Rives BFK",
	"lana":            "Lana Hot Press",
	"awagami-kozo":    "Awagami Kozo Thick Natural",
	"awagami-premio":  "Awagami Premio Kozo",
	"bc-satin":        "Breathing Color Satin Canvas",
	"aurora-linen":    "Aurora Linen Canvas",
	"aurora-metallic": "Aurora Metallic Canvas",
	"brilliance-holo": "Brilliance Rainbow Holographic",
	"mirri-rainbow":   "Mirri Digital Rainbow",
	"mirri-silver":    "Mirri Digital Silver",
	"maxmetal":        "MaxMetal ACM Panel",
	"dibond":          "DIBOND",
	"gesso":           "Gesso Panel",
	"birch":           "Birch Veneer",
	"cradle78":        "Cradled Panel 7/8in",
	"cradle15":        "Cradled Panel 1.5in",
	"acrylic3":        "Museum Acrylic",
	"acrylic5":        "Museum Acrylic",
}

// TEXTURE_PRESET_MAP maps the wizard texture to the engine preset name.
var TEXTURE_PRESET_MAP = map[string]string{
	"flat":   "Flat",
	"light":  "Light",
	"medium": "Medium",
	"heavy":  "Impasto",
}

// PRESENT_MAP maps the wizard presentation to the engine present name.
var PRESENT_MAP = map[string]string{
	"rolled":  "Media only",
	"stretch": "Stretched",
	"mounted": "Mounted",
	"hang":    "Back Frame",
	"frame":   "Float Frame",
}

// NFC_MAP maps the wizard addons.nfc to the engine nfc value.
var NFC_MAP = map[string]string{
	"std": "No",
	"eng": "Yes",
}

// BRUSH_APPLICATIONS are the applications that enable relief brushwork.
var BRUSH_APPLICATIONS = map[string]bool{
	"textured":  true,
	"nextlevel": true,
}

// MOUNT_PANEL_MAP maps the material family to the default mount panel.
var MOUNT_PANEL_MAP = map[string]string{
	"paper":   "MaxMetal ACM Panel",
	"canvas":  "MaxMetal ACM Panel",
	"foil":    "MaxMetal ACM Panel",
	"metal":   "MaxMetal ACM Panel",
	"wood":    "MaxMetal ACM Panel",
	"acrylic": "MaxMetal ACM Panel",
}

// BAR_TYPE_MAP maps the material family to the default bar type for stretching.
var BAR_TYPE_MAP = map[string]string{
	"paper":   "Stretcher Bar Gallery 1.5in",
	"canvas":  "Stretcher Bar Gallery 1.5in",
	"foil":    "Stretcher Bar Gallery 1.5in",
	"metal":   "Stretcher Bar Gallery 1.5in",
	"wood":    "Stretcher Bar Gallery 1.5in",
	"acrylic": "Stretcher Bar Gallery 1.5in",
}

// MOULDING_MAP maps the material family to the default moulding for frames.
var MOULDING_MAP = map[string]string{
	"paper":   "Floater Black 1.5in",
	"canvas":  "Floater Black 1.5in",
	"foil":    "Floater Black 1.5in",
	"metal":   "Floater Black 1.5in",
	"wood":    "Floater Black 1.5in",
	"acrylic": "Floater Black 1.5in",
}

// FAMILY_MEDIA_DEFAULTS maps the material family to a fallback media product.
var FAMILY_MEDIA_DEFAULTS = map[string]string{
	"paper":   "ARCHES 88",
	"canvas":  "Breathing Color Satin Canvas",
	"foil":    "Brilliance Rainbow Holographic",
	"metal":   "DIBOND",
	"wood":    "Birch Veneer",
	"acrylic": "Museum Acrylic",
}

// toEngineConfig converts a wizard-shaped quote config map (as stored in the
// studio-quotes Mongo document) into an engine Config. It is a 1:1 port of the
// Payload adapter's toEngineCfg.
func toEngineConfig(raw map[string]any) (Config, error) {
	if raw == nil {
		return Config{}, fmt.Errorf("quote config is nil")
	}

	application := str(raw["application"])
	processKey := str(raw["processKey"])
	materialFamily := str(raw["materialFamily"])
	mediaKey := str(raw["mediaKey"])
	texture := str(raw["texture"])
	presentation := str(raw["presentation"])
	shape := str(raw["shape"])

	// Legacy quotes may contain the old `textured-replica` metadata value.
	// Prefer the canonical processKey for new quotes, then fall back to
	// application for compatibility.
	key := processKey
	if key == "" || PROCESS_MAP[key] == "" {
		key = application
	}
	process := PROCESS_MAP[key]
	if process == "" {
		process = "Textured Reproductions"
	}

	isCircle := shape == "circle"

	size := numSlice(raw["sizeInches"])
	w := 0.0
	if len(size) > 0 {
		w = size[0]
	}
	l := w
	if !isCircle && len(size) > 1 {
		l = size[1]
	}

	// Texture preset + custom height.
	texturePreset := TEXTURE_PRESET_MAP[texture]
	if texture == "custom" {
		texturePreset = "Custom"
	}
	if texturePreset == "" {
		texturePreset = "Flat"
	}
	texMm := 0.0
	if texture == "custom" {
		texMm = math.Max(0, numAny(raw["textureMm"]))
	}

	present := PRESENT_MAP[presentation]
	if present == "" {
		present = "Media only"
	}
	// The prototype deliberately prices the print with a neutral package.
	// Packing is selected for order-time fulfillment and must not change the
	// edition print price.
	pack := "No package"

	nfc := "No"
	if addons, ok := raw["addons"].(map[string]any); ok {
		if v := NFC_MAP[str(addons["nfc"])]; v != "" {
			nfc = v
		}
	}

	// Brushwork is an optional relief pass. A textured-capable application
	// with a flat texture must not be charged for that pass; custom zero-height
	// texture is equivalent to flat for pricing purposes.
	hasBrushwork := BRUSH_APPLICATIONS[application] &&
		texture != "flat" &&
		(texture != "custom" || numAny(raw["textureMm"]) > 0)
	brush := "No"
	if hasBrushwork {
		brush = "Yes"
	}

	rush := "No"
	if b, ok := raw["rush"].(bool); ok && b {
		rush = "Yes"
	}

	return Config{
		Process:    process,
		Shape:      map[bool]string{true: "Circle", false: "Rectangle"}[isCircle],
		W:          w,
		L:          l,
		BordT:      numAny(raw["bordT"]),
		BordB:      numAny(raw["bordB"]),
		BordL:      numAny(raw["bordL"]),
		BordR:      numAny(raw["bordR"]),
		Media:      resolveMedia(mediaKey, materialFamily),
		Preset:     texturePreset,
		TexMM:      texMm,
		Brush:      brush,
		Varnish:    "Matte",
		Present:    present,
		MountPanel: lookup(MOUNT_PANEL_MAP, materialFamily, "MaxMetal ACM Panel"),
		BarType:    lookup(BAR_TYPE_MAP, materialFamily, "Stretcher Bar Gallery 1.5in"),
		Edge:       "Mirror",
		Moulding:   lookup(MOULDING_MAP, materialFamily, "Floater Black 1.5in"),
		Fulfill:    "Bulk to artist",
		Pack:       pack,
		NFC:        nfc,
		Gold:       "No",
		Photo:      "No",
		Video:      "No",
		Copy:       "No",
		ARVR:       "No",
		Twin:       "No",
		Mktg:       "No",
		Rush:       rush,
	}, nil
}

// resolveMedia resolves the engine media product name from the wizard mediaKey,
// with a fallback by material family and a last-resort default.
func resolveMedia(mediaKey, materialFamily string) string {
	if direct := MEDIA_MAP[mediaKey]; direct != "" {
		return direct
	}
	if familyDefault := FAMILY_MEDIA_DEFAULTS[materialFamily]; familyDefault != "" {
		return familyDefault
	}
	return "ARCHES 88"
}

// lookup returns m[key] or the given fallback when key is absent.
func lookup(m map[string]string, key, fallback string) string {
	if v := m[key]; v != "" {
		return v
	}
	return fallback
}

// str returns the string value of v, or "" when v is nil or not a string.
func str(v any) string {
	s, _ := v.(string)
	return s
}

// numAny returns the float64 value of v, or 0 when v is nil or not numeric. It
// accepts float64, int, int32, int64 (the normalized BSON forms) and numeric
// strings. It is named numAny to avoid clashing with the package-level num
// helper in pricing.go (which handles float64/int/json.Number).
func numAny(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	default:
		return 0
	}
}

// numSlice returns the float64 values of v when v is a slice of numbers (the
// normalized BSON form of sizeInches), or nil otherwise.
func numSlice(v any) []float64 {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]float64, 0, len(arr))
	for _, item := range arr {
		out = append(out, numAny(item))
	}
	return out
}
