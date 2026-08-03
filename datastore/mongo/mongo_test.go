package mongo

import (
	"context"
	"testing"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

// newTestConfig returns a config with Mongo settings populated.
func newTestConfig(uri string) *configs.Config {
	return &configs.Config{
		MongoURI:                            uri,
		MongoDatabase:                       "payload",
		MongoPricingConfigurationsCollection: "pricing-configurations",
		MongoConnectTimeout:                 2 * time.Second,
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

	mt.Run("returns active config", func(mt *mtest.T) {
		id := primitive.NewObjectID()
		doc := bson.D{
			{Key: "_id", Value: id},
			{Key: "domain", Value: "studio-printing"},
			{Key: "status", Value: "active"},
			{Key: "effectiveFrom", Value: time.Now().Add(-time.Hour)},
			{Key: "updatedAt", Value: time.Now()},
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
		if got.ID != id {
			mt.Fatalf("expected ID %s, got %s", id.Hex(), got.ID.Hex())
		}
		if got.Domain != "studio-printing" {
			mt.Fatalf("expected domain studio-printing, got %s", got.Domain)
		}
	})

	mt.Run("returns error when no active config", func(mt *mtest.T) {
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{
			Code:    66,
			Message: "no documents in result",
		}))

		client := &Client{
			client: mt.Client,
			db:     mt.Client.Database("payload"),
		}
		cfg := newTestConfig("mongodb://mock")
		s := NewPricingStore(client, cfg)

		if _, err := s.GetActive(context.Background()); err == nil {
			mt.Fatal("expected error when no active config, got nil")
		}
	})
}
