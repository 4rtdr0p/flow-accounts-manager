package mongo

import (
	"context"
	"errors"
	"fmt"

	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// ErrArtworkNotFound is returned when no document in the editions or
// paintings collection matches the requested artwork id.
var ErrArtworkNotFound = errors.New("artwork not found")

// ErrArtworkPriceMissing is returned when the artwork document exists but
// carries no usable price (editions.price / paintings.originalPrice missing,
// null, or not a number).
var ErrArtworkPriceMissing = errors.New("artwork price missing")

// ArtworkPrice is the server-computed price of an artwork read from Mongo.
// The price is always in whole dollars (USD) as stored by Payload CMS.
type ArtworkPrice struct {
	// ID is the stable id of the artwork document (edition id or painting id).
	ID string
	// PriceUSD is the artwork price in whole dollars.
	PriceUSD float64
}

// PurchaseStore provides read-only access to the artwork price data in Mongo
// (editions.price / paintings.originalPrice) that the purchase charge flow
// (#93) reads to compute the escrow amount server-side.
type PurchaseStore struct {
	client        *Client
	editionsColl  string
	paintingsColl string
}

// NewPurchaseStore creates a read-only store for the editions and paintings
// collections. It returns nil when the Mongo client is nil (Mongo disabled).
func NewPurchaseStore(client *Client, cfg *configs.Config) *PurchaseStore {
	if client == nil {
		return nil
	}
	return &PurchaseStore{
		client:        client,
		editionsColl:  cfg.MongoEditionsCollection,
		paintingsColl: cfg.MongoPaintingsCollection,
	}
}

// GetEditionPrice returns the price of an edition by its id, reading
// editions.price. It returns ErrArtworkNotFound when no such edition exists
// and ErrArtworkPriceMissing when the price field is absent or not a number.
func (s *PurchaseStore) GetEditionPrice(ctx context.Context, editionID string) (*ArtworkPrice, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("mongo client is not configured")
	}
	if editionID == "" {
		return nil, fmt.Errorf("edition id is required")
	}

	raw, err := s.findArtwork(ctx, s.editionsColl, editionID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrArtworkNotFound
		}
		return nil, fmt.Errorf("find edition %q: %w", editionID, err)
	}

	var doc struct {
		Price *float64 `bson:"price"`
	}
	if err := bson.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode edition %q: %w", editionID, err)
	}
	if doc.Price == nil || *doc.Price <= 0 {
		return nil, fmt.Errorf("%w: edition %q has no positive price", ErrArtworkPriceMissing, editionID)
	}

	return &ArtworkPrice{ID: editionID, PriceUSD: *doc.Price}, nil
}

// GetPaintingPrice returns the price of a painting by its id, reading
// paintings.originalPrice. It returns ErrArtworkNotFound when no such painting
// exists and ErrArtworkPriceMissing when the price field is absent or not a
// number.
func (s *PurchaseStore) GetPaintingPrice(ctx context.Context, paintingID string) (*ArtworkPrice, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("mongo client is not configured")
	}
	if paintingID == "" {
		return nil, fmt.Errorf("painting id is required")
	}

	raw, err := s.findArtwork(ctx, s.paintingsColl, paintingID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrArtworkNotFound
		}
		return nil, fmt.Errorf("find painting %q: %w", paintingID, err)
	}

	var doc struct {
		OriginalPrice *float64 `bson:"originalPrice"`
	}
	if err := bson.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode painting %q: %w", paintingID, err)
	}
	if doc.OriginalPrice == nil || *doc.OriginalPrice <= 0 {
		return nil, fmt.Errorf("%w: painting %q has no positive originalPrice", ErrArtworkPriceMissing, paintingID)
	}

	return &ArtworkPrice{ID: paintingID, PriceUSD: *doc.OriginalPrice}, nil
}

// findArtwork resolves Payload artwork ids across documents that store the
// public identifier in either Mongo's _id field or Payload's id field.
func (s *PurchaseStore) findArtwork(ctx context.Context, collection, artworkID string) (bson.Raw, error) {
	ids := []any{artworkID}
	if objectID, err := primitive.ObjectIDFromHex(artworkID); err == nil {
		ids = []any{objectID, artworkID}
	}

	for _, id := range ids {
		var raw bson.Raw
		err := s.client.Collection(collection).FindOne(ctx, bson.M{"_id": id}).Decode(&raw)
		if err == nil {
			return raw, nil
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return nil, err
		}
	}

	var raw bson.Raw
	if err := s.client.Collection(collection).FindOne(ctx, bson.M{"id": artworkID}).Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}
