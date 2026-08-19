package mongo

import (
	"context"
	"errors"
	"fmt"

	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// ErrQuoteNotFound is returned when no document in the studio-quotes
// collection matches the requested quote id.
var ErrQuoteNotFound = errors.New("studio quote not found")

// StudioQuote is the read-only shape of a document in the studio-quotes
// collection. It carries the identity of the quote plus the config snapshot
// that the Studio Wizard produced when the quote was created. The config is
// what the charge flow (#71) translates into a pricing.Config to recalculate
// the exact price at charge time.
type StudioQuote struct {
	// ID is the stable public token (publicToken) that Payload CMS assigns to
	// a studio quote. Payload persists the quote with an ObjectId _id and no
	// "id" field; the public token is the only stable, externally-addressable
	// identity, so lookups key on it.
	ID     string `bson:"publicToken" json:"publicToken"`
	UserID string `bson:"userId" json:"userId"`
	// Config holds the Studio Wizard config snapshot (process, W, L, borders,
	// run_size, ...) as stored by Payload CMS. It is the input to the pricing
	// engine adapter.
	Config map[string]any `bson:"config" json:"config"`
}

// QuoteStore provides read-only access to studio quotes in Mongo.
type QuoteStore struct {
	client *Client
	coll   string
}

// NewQuoteStore creates a read-only store for the studio-quotes collection.
// It returns nil when the Mongo client is nil (Mongo disabled).
func NewQuoteStore(client *Client, cfg *configs.Config) *QuoteStore {
	if client == nil {
		return nil
	}
	return &QuoteStore{
		client: client,
		coll:   cfg.MongoStudioQuotesCollection,
	}
}

// GetByID returns the studio quote document matching the given public token
// (publicToken). It returns ErrQuoteNotFound when no such quote exists.
func (s *QuoteStore) GetByID(ctx context.Context, quoteID string) (*StudioQuote, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("mongo client is not configured")
	}
	if quoteID == "" {
		return nil, fmt.Errorf("quote id is required")
	}

	var raw bson.Raw
	err := s.client.Collection(s.coll).FindOne(ctx, bson.M{"publicToken": quoteID}).Decode(&raw)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrQuoteNotFound
		}
		return nil, fmt.Errorf("find studio quote %q: %w", quoteID, err)
	}

	var quote StudioQuote
	if err := bson.Unmarshal(raw, &quote); err != nil {
		return nil, fmt.Errorf("decode studio quote %q: %w", quoteID, err)
	}

	// Normalize the config map so downstream code receives plain Go types
	// (map[string]any / []interface{} / float64 / string) regardless of which
	// BSON named types the mongo driver produced.
	normalized, err := NormalizeBSONData(quote.Config)
	if err != nil {
		return nil, fmt.Errorf("normalize studio quote config: %w", err)
	}
	quote.Config = normalized

	return &quote, nil
}
