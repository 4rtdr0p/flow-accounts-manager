package mongo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrNoActivePricing is returned when no row in pricing-configurations matches
// the active Studio printing filter (domain studio-printing, status active,
// effectiveFrom <= now).
var ErrNoActivePricing = errors.New("no active pricing configuration")

// PricingConfiguration is the read-only shape of a document in the
// pricing-configurations collection. The metadata fields used by the active-row
// filter are typed; Data carries the remaining fields (rates, tiers, addons,
// ...) of the document so pricing endpoints can return the full configuration.
type PricingConfiguration struct {
	Domain        string    `bson:"domain" json:"domain"`
	Status        string    `bson:"status" json:"status"`
	EffectiveFrom time.Time `bson:"effectiveFrom" json:"effectiveFrom"`
	UpdatedAt     time.Time `bson:"updatedAt" json:"updatedAt"`
	// Data holds the pricing fields of the document beyond the typed metadata
	// above. It is nil when the document carries only the metadata fields.
	Data map[string]any `bson:"-" json:"data,omitempty"`
}

// PricingStore provides read-only access to pricing data in Mongo.
type PricingStore struct {
	client *Client
	coll   string
}

// NewPricingStore creates a read-only store for the pricing-configurations
// collection. It returns nil when the Mongo client is nil (Mongo disabled).
func NewPricingStore(client *Client, cfg *configs.Config) *PricingStore {
	if client == nil {
		return nil
	}
	return &PricingStore{
		client: client,
		coll:   cfg.MongoPricingConfigurationsCollection,
	}
}

// TestQuery performs a read-only probe query against the pricing-configurations
// collection. It returns the count of documents matching the active Studio
// printing configuration. This is the wiring test requested by the issue: it
// proves the service can connect to Mongo and read pricing data.
func (s *PricingStore) TestQuery(ctx context.Context) (int64, error) {
	if s == nil || s.client == nil {
		return 0, fmt.Errorf("mongo client is not configured")
	}

	filter := bson.M{
		"domain": "studio-printing",
		"status": "active",
	}

	count, err := s.client.Collection(s.coll).CountDocuments(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("count pricing-configurations: %w", err)
	}
	return count, nil
}

// GetActive returns the active Studio printing pricing configuration, if any.
// It is the read primitive that the pricing endpoints (#69, #70) will build on.
func (s *PricingStore) GetActive(ctx context.Context) (*PricingConfiguration, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("mongo client is not configured")
	}

	filter := bson.M{
		"domain":        "studio-printing",
		"status":        "active",
		"effectiveFrom": bson.M{"$lte": time.Now()},
	}

	// Decode the raw document first so both the typed metadata fields and the
	// full document (Data) can be populated from a single query.
	var raw bson.Raw
	opts := options.FindOne().SetSort(bson.D{{Key: "effectiveFrom", Value: -1}})
	err := s.client.Collection(s.coll).FindOne(ctx, filter, opts).Decode(&raw)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNoActivePricing
		}
		return nil, fmt.Errorf("find active pricing-configuration: %w", err)
	}

	var cfg PricingConfiguration
	if err := bson.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("decode active pricing-configuration: %w", err)
	}

	// Preserve the full document so pricing endpoints can return the complete
	// active configuration. The metadata keys are already typed on the struct
	// and are removed from Data to avoid duplication.
	var full bson.M
	if err := bson.Unmarshal(raw, &full); err != nil {
		return nil, fmt.Errorf("decode active pricing-configuration data: %w", err)
	}
	for _, key := range []string{"_id", "domain", "status", "effectiveFrom", "updatedAt"} {
		delete(full, key)
	}
	cfg.Data = full

	return &cfg, nil
}

// GetActiveUpdatedAt returns the updatedAt of the active Studio printing
// configuration using a projection, without loading the full document. It is
// the lightweight read the cache invalidation uses to detect an updatedAt
// change before deciding to re-fetch the full configuration.
func (s *PricingStore) GetActiveUpdatedAt(ctx context.Context) (time.Time, error) {
	if s == nil || s.client == nil {
		return time.Time{}, fmt.Errorf("mongo client is not configured")
	}

	filter := bson.M{
		"domain":        "studio-printing",
		"status":        "active",
		"effectiveFrom": bson.M{"$lte": time.Now()},
	}

	var result struct {
		UpdatedAt time.Time `bson:"updatedAt"`
	}
	opts := options.FindOne().
		SetProjection(bson.M{"updatedAt": 1}).
		SetSort(bson.D{{Key: "effectiveFrom", Value: -1}})
	err := s.client.Collection(s.coll).FindOne(ctx, filter, opts).Decode(&result)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return time.Time{}, ErrNoActivePricing
		}
		return time.Time{}, fmt.Errorf("find active pricing-configuration updatedAt: %w", err)
	}

	return result.UpdatedAt, nil
}
