package mongo

import (
	"context"
	"fmt"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// PricingConfiguration is the minimal read-only shape of a document in the
// pricing-configurations collection. Only the fields needed for the test query
// and for the downstream pricing issues are mapped; unknown fields are ignored.
type PricingConfiguration struct {
	ID            primitive.ObjectID `bson:"_id,omitempty"`
	Domain        string             `bson:"domain"`
	Status        string             `bson:"status"`
	EffectiveFrom time.Time `bson:"effectiveFrom"`
	UpdatedAt     time.Time `bson:"updatedAt"`
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

	var cfg PricingConfiguration
	err := s.client.Collection(s.coll).FindOne(ctx, filter).Decode(&cfg)
	if err != nil {
		return nil, fmt.Errorf("find active pricing-configuration: %w", err)
	}
	return &cfg, nil
}
