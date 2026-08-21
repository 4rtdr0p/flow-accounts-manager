package purchase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ErrPythDisabled is returned when the Pyth client is not configured.
var ErrPythDisabled = errors.New("pyth oracle is disabled")

// ErrPythStale is returned when the Pyth price is older than the configured
// maximum age, so a stale oracle can't lock an escrow at a wrong amount.
var ErrPythStale = errors.New("pyth price is stale")

// PythPrice is the FLOW/USD price read from the Pyth Hermes HTTP API.
type PythPrice struct {
	// PriceUSD is the USD price of one FLOW token.
	PriceUSD float64
	// PublishTime is when the price was published by the oracle.
	PublishTime time.Time
}

// PythClient reads the FLOW/USD price from the Pyth Hermes HTTP API. It is a
// minimal net/http client; the feed is free and needs no API key.
type PythClient struct {
	baseURL string
	feedID  string
	maxAge  time.Duration
	http    *http.Client
}

// NewPythClient creates a Pyth Hermes client. When baseURL is empty the client
// is disabled (Latest returns ErrPythDisabled). maxAge is the maximum age a
// price is accepted for; a price older than that is rejected.
func NewPythClient(baseURL, feedID string, maxAge time.Duration) *PythClient {
	if baseURL == "" {
		baseURL = "https://hermes.pyth.network"
	}
	return &PythClient{
		baseURL: baseURL,
		feedID:  feedID,
		maxAge:  maxAge,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// Latest returns the current FLOW/USD price from the Hermes API, rejecting it
// if it is older than the configured maximum age.
func (c *PythClient) Latest(ctx context.Context) (*PythPrice, error) {
	if c == nil || c.baseURL == "" {
		return nil, ErrPythDisabled
	}
	if c.feedID == "" {
		return nil, fmt.Errorf("pyth feed id is required")
	}

	q := url.Values{}
	q.Set("ids[]", c.feedID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v2/updates/price/latest?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build pyth request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pyth request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read pyth response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pyth API error (status %d): %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Parsed []struct {
			Price struct {
				Price       string `json:"price"`
				Expo        int32  `json:"expo"`
				PublishTime int64  `json:"publish_time"`
			} `json:"price"`
		} `json:"parsed"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode pyth response: %w", err)
	}
	if len(parsed.Parsed) == 0 {
		return nil, fmt.Errorf("pyth returned no price for feed %s", c.feedID)
	}

	price := parsed.Parsed[0].Price
	priceUSD, err := pythPriceToFloat(price.Price, price.Expo)
	if err != nil {
		return nil, err
	}
	if priceUSD <= 0 {
		return nil, fmt.Errorf("pyth returned non-positive FLOW/USD price %f", priceUSD)
	}

	publishTime := time.Unix(price.PublishTime, 0)
	if c.maxAge > 0 && time.Since(publishTime) > c.maxAge {
		return nil, fmt.Errorf("%w: published %s (age %s, max %s)",
			ErrPythStale, publishTime.UTC().Format(time.RFC3339), time.Since(publishTime).Round(time.Second), c.maxAge)
	}

	return &PythPrice{PriceUSD: priceUSD, PublishTime: publishTime}, nil
}

// pythPriceToFloat converts a Pyth price (an integer scaled by 10^expo) to a
// float64. Pyth prices are signed integers; expo is typically -8 for
// USD-denominated feeds.
func pythPriceToFloat(price string, expo int32) (float64, error) {
	var raw int64
	if _, err := fmt.Sscanf(price, "%d", &raw); err != nil {
		return 0, fmt.Errorf("parse pyth price %q: %w", price, err)
	}
	return float64(raw) * pow10(expo), nil
}

func pow10(expo int32) float64 {
	result := 1.0
	if expo >= 0 {
		for i := int32(0); i < expo; i++ {
			result *= 10
		}
		return result
	}
	for i := int32(0); i < -expo; i++ {
		result /= 10
	}
	return result
}
