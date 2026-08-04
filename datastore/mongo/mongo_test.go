package mongo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

// newTestConfig returns a config with Mongo settings populated.
func newTestConfig(uri string) *configs.Config {
	return &configs.Config{
		MongoURI:                             uri,
		MongoDatabase:                        "payload",
		MongoPricingConfigurationsCollection: "pricing-configurations",
		MongoConnectTimeout:                  2 * time.Second,
	}
}

func TestNewEmptyURI(t *testing.T) {
	// When MONGO_URI is empty, Mongo is disabled and New returns (nil, nil).
	cfg := newTestConfig("")
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("expected no error for empty URI, got %v", err)
	}
	if c != nil {
		t.Fatalf("expected nil client for empty URI, got %v", c)
	}
}

func TestNewInvalidURI(t *testing.T) {
	// An unreachable/invalid URI must produce an error, not a silent success.
	cfg := newTestConfig("mongodb://127.0.0.1:1")
	cfg.MongoConnectTimeout = 100 * time.Millisecond
	c, err := New(cfg)
	if err == nil {
		if c != nil {
			c.Close()
		}
		t.Fatal("expected error for unreachable URI, got nil")
	}
}

func TestCloseNilReceiver(t *testing.T) {
	// Close must be safe to call on a nil receiver.
	var c *Client
	c.Close()
}

func TestNewPricingStoreNilClient(t *testing.T) {
	cfg := newTestConfig("")
	s := NewPricingStore(nil, cfg)
	if s != nil {
		t.Fatalf("expected nil store when client is nil, got %v", s)
	}
}

func TestPricingStoreNilClientErrors(t *testing.T) {
	// A store with a nil client must return a clear error, not panic.
	cfg := newTestConfig("")
	s := NewPricingStore(nil, cfg)

	if _, err := s.TestQuery(context.Background()); err == nil {
		t.Fatal("expected error from TestQuery with nil client, got nil")
	}
	if _, err := s.GetActive(context.Background()); err == nil {
		t.Fatal("expected error from GetActive with nil client, got nil")
	}
}

func TestPricingStoreTestQuery(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("counts active studio-printing configs", func(mt *mtest.T) {
		// CountDocuments returns the count in the first batch of a cursor.
		mt.AddMockResponses(mtest.CreateCursorResponse(1, "payload.pricing-configurations", mtest.FirstBatch,
			bson.D{{Key: "n", Value: int64(3)}}))

		client := &Client{
			client: mt.Client,
			db:     mt.Client.Database("payload"),
		}
		cfg := newTestConfig("mongodb://mock")
		s := NewPricingStore(client, cfg)

		count, err := s.TestQuery(context.Background())
		if err != nil {
			mt.Fatalf("unexpected error: %v", err)
		}
		if count != 3 {
			mt.Fatalf("expected count 3, got %d", count)
		}
	})
}

func TestPricingStoreGetActive(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("returns active config with full data", func(mt *mtest.T) {
		doc := bson.D{
			{Key: "_id", Value: "cfg-1"},
			{Key: "domain", Value: "studio-printing"},
			{Key: "status", Value: "active"},
			{Key: "effectiveFrom", Value: time.Now().Add(-time.Hour)},
			{Key: "updatedAt", Value: time.Now()},
			{Key: "paper_price", Value: 1.25},
		}
		mt.AddMockResponses(mtest.CreateCursorResponse(1, "payload.pricing-configurations", mtest.FirstBatch, doc))

		client := &Client{
			client: mt.Client,
			db:     mt.Client.Database("payload"),
		}
		cfg := newTestConfig("mongodb://mock")
		s := NewPricingStore(client, cfg)

		got, err := s.GetActive(context.Background())
		if err != nil {
			mt.Fatalf("unexpected error: %v", err)
		}
		if got == nil {
			mt.Fatal("expected a config, got nil")
		}
		if got.Domain != "studio-printing" {
			mt.Fatalf("expected domain studio-printing, got %s", got.Domain)
		}
		if got.Data == nil {
			mt.Fatal("expected Data to hold the full document, got nil")
		}
		if price, ok := got.Data["paper_price"].(float64); !ok || price != 1.25 {
			mt.Fatalf("expected paper_price 1.25 in Data, got %#v", got.Data["paper_price"])
		}
	})

	mt.Run("returns error when no active config", func(mt *mtest.T) {
		// A real server signals a no-match FindOne with an empty cursor, which
		// the driver translates to mongo.ErrNoDocuments.
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "payload.pricing-configurations", mtest.FirstBatch))

		client := &Client{
			client: mt.Client,
			db:     mt.Client.Database("payload"),
		}
		cfg := newTestConfig("mongodb://mock")
		s := NewPricingStore(client, cfg)

		if _, err := s.GetActive(context.Background()); !errors.Is(err, ErrNoActivePricing) {
			mt.Fatalf("expected ErrNoActivePricing, got %v", err)
		}
	})
}

func TestPricingStoreGetActiveUpdatedAt(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("returns updatedAt via projection", func(mt *mtest.T) {
		// BSON stores dates as millisecond precision, so the expected value is
		// truncated to survive the mock round-trip.
		updated := time.Now().UTC().Truncate(time.Millisecond)
		doc := bson.D{
			{Key: "updatedAt", Value: updated},
		}
		mt.AddMockResponses(mtest.CreateCursorResponse(1, "payload.pricing-configurations", mtest.FirstBatch, doc))

		client := &Client{
			client: mt.Client,
			db:     mt.Client.Database("payload"),
		}
		cfg := newTestConfig("mongodb://mock")
		s := NewPricingStore(client, cfg)

		got, err := s.GetActiveUpdatedAt(context.Background())
		if err != nil {
			mt.Fatalf("unexpected error: %v", err)
		}
		if !got.Equal(updated) {
			mt.Fatalf("expected updatedAt %v, got %v", updated, got)
		}
	})

	mt.Run("returns ErrNoActivePricing when no active config", func(mt *mtest.T) {
		mt.AddMockResponses(mtest.CreateCursorResponse(0, "payload.pricing-configurations", mtest.FirstBatch))

		client := &Client{
			client: mt.Client,
			db:     mt.Client.Database("payload"),
		}
		cfg := newTestConfig("mongodb://mock")
		s := NewPricingStore(client, cfg)

		if _, err := s.GetActiveUpdatedAt(context.Background()); !errors.Is(err, ErrNoActivePricing) {
			mt.Fatalf("expected ErrNoActivePricing, got %v", err)
		}
	})
}
