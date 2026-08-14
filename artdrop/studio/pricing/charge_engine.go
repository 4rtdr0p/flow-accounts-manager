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

// toEngineConfig converts a raw config map into a Config. Numeric fields are
// coerced from float64 (the normalized BSON form) or string; a missing or
// non-numeric value for a required numeric field is an error.
func toEngineConfig(raw map[string]any) (Config, error) {
	if raw == nil {
		return Config{}, fmt.Errorf("quote config is nil")
	}

	cfg := Config{
		Process:    str(raw["process"]),
		Shape:      str(raw["shape"]),
		Matcat:     str(raw["matcat"]),
		Media:      str(raw["media"]),
		Preset:     str(raw["preset"]),
		Brush:      str(raw["brush"]),
		Varnish:    str(raw["varnish"]),
		Present:    str(raw["present"]),
		MountPanel: str(raw["mountpanel"]),
		BarType:    str(raw["bartype"]),
		Edge:       str(raw["edge"]),
		Moulding:   str(raw["moulding"]),
		Fulfill:    str(raw["fulfill"]),
		Pack:       str(raw["pack"]),
		NFC:        str(raw["nfc"]),
		Gold:       str(raw["gold"]),
		Photo:      str(raw["photo"]),
		Video:      str(raw["video"]),
		Copy:       str(raw["copy"]),
		ARVR:       str(raw["arvr"]),
		Twin:       str(raw["twin"]),
		Mktg:       str(raw["mktg"]),
		Rush:       str(raw["rush"]),
	}

	var err error
	if cfg.W, err = coerceNum(raw["W"]); err != nil {
		return Config{}, fmt.Errorf("quote config W: %w", err)
	}
	if cfg.L, err = coerceNum(raw["L"]); err != nil {
		return Config{}, fmt.Errorf("quote config L: %w", err)
	}
	if cfg.BordT, err = coerceNum(raw["bord_t"]); err != nil {
		return Config{}, fmt.Errorf("quote config bord_t: %w", err)
	}
	if cfg.BordB, err = coerceNum(raw["bord_b"]); err != nil {
		return Config{}, fmt.Errorf("quote config bord_b: %w", err)
	}
	if cfg.BordL, err = coerceNum(raw["bord_l"]); err != nil {
		return Config{}, fmt.Errorf("quote config bord_l: %w", err)
	}
	if cfg.BordR, err = coerceNum(raw["bord_r"]); err != nil {
		return Config{}, fmt.Errorf("quote config bord_r: %w", err)
	}
	if cfg.TexMM, err = coerceNum(raw["tex_mm"]); err != nil {
		return Config{}, fmt.Errorf("quote config tex_mm: %w", err)
	}
	if cfg.RunSize, err = intNum(raw["run_size"]); err != nil {
		return Config{}, fmt.Errorf("quote config run_size: %w", err)
	}

	return cfg, nil
}

// str returns the string value of v, or "" when v is nil or not a string.
func str(v any) string {
	s, _ := v.(string)
	return s
}

// coerceNum returns the float64 value of v. It accepts float64, int, int32,
// int64 (the normalized BSON forms) and numeric strings.
func coerceNum(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, fmt.Errorf("expected number, got %q", n)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("expected number, got %T", v)
	}
}

// intNum returns the int value of v, coercing float64 (the normalized BSON
// form) and numeric strings.
func intNum(v any) (int, error) {
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case float32:
		return int(n), nil
	case int:
		return n, nil
	case int32:
		return int(n), nil
	case int64:
		return int(n), nil
	case string:
		i, err := strconv.Atoi(n)
		if err != nil {
			return 0, fmt.Errorf("expected integer, got %q", n)
		}
		return i, nil
	default:
		return 0, fmt.Errorf("expected integer, got %T", v)
	}
}
