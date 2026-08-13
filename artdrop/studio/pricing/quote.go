package pricing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// EngineVersion identifies the version of the ported pricing engine used to
// compute a quote. It is included in the quote hash so a client can prove which
// engine version produced a given price snapshot. Bump it whenever the compute
// logic in this package changes in a way that can alter results.
const EngineVersion = "1.0.0"

// QuoteResult is the price snapshot returned by POST /studio/quotes:price. It
// embeds the engine Result plus proof metadata: a hash over the active rates
// and engine version, and the updatedAt of the active configuration.
type QuoteResult struct {
	Result        `json:",inline"`
	Hash          string    `json:"hash"`
	EngineVersion string    `json:"engine_version"`
	RatesUpdated  time.Time `json:"rates_updated_at"`
}

// QuoteService computes a studio quote from the active pricing configuration.
// It is the single place that knows how to price a job: it reads the active
// rates from the cached Mongo configuration (#69), feeds them into the ported
// engine (#68), and returns the snapshot with a proof hash.
type QuoteService struct {
	active *ActiveService
}

// NewQuoteService creates a quote service backed by the active-config service.
func NewQuoteService(active *ActiveService) *QuoteService {
	return &QuoteService{active: active}
}

// ErrInvalidQuoteConfig is returned by QuoteService.Quote when the supplied
// Studio config fails validation (negative or zero dimensions, non-positive
// run_size, or negative borders). QuoteHandler maps this to HTTP 400.
var ErrInvalidQuoteConfig = errors.New("invalid quote config")

func validateQuoteConfig(cfg Config) error {
	if cfg.RunSize <= 0 {
		return fmt.Errorf("%w: run_size must be > 0 (got %d)", ErrInvalidQuoteConfig, cfg.RunSize)
	}
	if cfg.W <= 0 {
		return fmt.Errorf("%w: W must be > 0 (got %g)", ErrInvalidQuoteConfig, cfg.W)
	}
	if cfg.L <= 0 {
		return fmt.Errorf("%w: L must be > 0 (got %g)", ErrInvalidQuoteConfig, cfg.L)
	}
	if cfg.BordT < 0 || cfg.BordB < 0 || cfg.BordL < 0 || cfg.BordR < 0 {
		return fmt.Errorf("%w: borders must be >= 0 (got bord_t=%g bord_b=%g bord_l=%g bord_r=%g)",
			ErrInvalidQuoteConfig, cfg.BordT, cfg.BordB, cfg.BordL, cfg.BordR)
	}
	return nil
}

// Quote computes the price for the given Studio Wizard config using the active
// pricing configuration. It returns the price snapshot plus a deterministic
// hash over the active rates and the engine version.
func (s *QuoteService) Quote(ctx context.Context, cfg Config) (QuoteResult, error) {
	if s.active == nil {
		return QuoteResult{}, ErrPricingDisabled
	}

	if err := validateQuoteConfig(cfg); err != nil {
		return QuoteResult{}, err
	}

	active, err := s.active.Get(ctx)
	if err != nil {
		return QuoteResult{}, err
	}

	data, err := LoadDataFromMap(active.Data)
	if err != nil {
		return QuoteResult{}, fmt.Errorf("build engine data from active pricing: %w", err)
	}

	res, err := Compute(data, cfg)
	if err != nil {
		return QuoteResult{}, fmt.Errorf("compute quote: %w", err)
	}

	hash, err := quoteHash(active.Data)
	if err != nil {
		return QuoteResult{}, fmt.Errorf("compute quote hash: %w", err)
	}

	return QuoteResult{
		Result:        res,
		Hash:          hash,
		EngineVersion: EngineVersion,
		RatesUpdated:  active.UpdatedAt,
	}, nil
}

// quoteHash returns a deterministic SHA-256 hex digest over the active rates
// (the Mongo Data map) and the engine version. The Data map is serialized with
// sorted keys so the same rates always produce the same hash regardless of map
// iteration order, making the hash a stable proof of which rates and engine
// version produced a snapshot.
func quoteHash(data map[string]any) (string, error) {
	canonical, err := canonicalJSON(data)
	if err != nil {
		return "", err
	}

	h := sha256.New()
	h.Write([]byte(EngineVersion))
	h.Write([]byte{0})
	h.Write(canonical)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// canonicalJSON serializes a value to JSON with object keys sorted
// lexicographically at every level, so the output is stable across map
// iteration orders. Numbers are emitted without lossy float formatting.
func canonicalJSON(v any) ([]byte, error) {
	normalized := normalize(v)
	return json.Marshal(normalized)
}

// normalize recursively converts map[string]any into a sorted-key structure so
// json.Marshal emits keys in a stable order. Arrays and scalars pass through.
func normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([]any, 0, len(x))
		for _, k := range keys {
			out = append(out, []any{k, normalize(x[k])})
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalize(item)
		}
		return out
	default:
		return x
	}
}
