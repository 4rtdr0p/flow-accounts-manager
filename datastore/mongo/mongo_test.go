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
		assertFindSortsByEffectiveFromDesc(mt)
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
		assertFindSortsByEffectiveFromDesc(mt)
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

func assertFindSortsByEffectiveFromDesc(t *mtest.T) {
	t.Helper()

	event := t.GetStartedEvent()
	if event == nil {
		t.Fatal("expected a Mongo command event")
	}

	sort, ok := event.Command.Lookup("sort").DocumentOK()
	if !ok {
		t.Fatalf("expected find command to include sort, got %s", event.Command)
	}

	effectiveFrom := sort.Lookup("effectiveFrom")
	if got := effectiveFrom.Int32(); got != -1 {
		t.Fatalf("expected sort.effectiveFrom -1, got %d in command %s", got, event.Command)
	}
}

// TestNormalizeBSONDataProducesPlainGoTypes reproduces the type-mismatch bug
// found live against a real Mongo-backed QuoteService. The mongo driver
// decodes nested objects as primitive.M (a named type distinct from
// map[string]interface{} even though the same underlying type), arrays as
// primitive.A, and integers as int32/int64. The pricing helpers
// (object/array/num in pricing.go) and normalizeMongoData (in from_map.go,
// added in PR #79) type-switch on plain Go types only, so a raw primitive.M
// would have silently degraded to zeros (except for the loud ink.type check
// in normalizeMongoData). NormalizeBSONData flattens this to genuine plain
// Go types via a json.Marshal / json.Unmarshal round-trip.
func TestNormalizeBSONDataProducesPlainGoTypes(t *testing.T) {
	raw := bson.M{
		"obj":   bson.M{"markup": int32(2)},
		"arr":   bson.A{int32(1), int32(2), int32(3)},
		"plain": int32(42),
	}

	got, err := NormalizeBSONData(raw)
	if err != nil {
		t.Fatalf("NormalizeBSONData: %v", err)
	}

	// Top-level type must be map[string]any (the named type bson.M becomes the
	// underlying map type via JSON round-trip).
	if _, ok := got["obj"].(map[string]any); !ok {
		t.Fatalf("got[obj] type = %T, want map[string]any", got["obj"])
	}

	// Nested int32 → float64 (pricing.go's num() helper returns float64).
	nestedMap, ok := got["obj"].(map[string]any)
	if !ok {
		t.Fatalf("got[obj] not map[string]any")
	}
	v, ok := nestedMap["markup"].(float64)
	if !ok || v != 2 {
		t.Fatalf("got[obj][markup] = %v (%T), want float64 2", nestedMap["markup"], nestedMap["markup"])
	}

	// primitive.A → []interface{} (NOT primitive.A; pricing.go's array()
	// helper only matches []interface{}).
	if _, ok := got["arr"].([]interface{}); !ok {
		t.Fatalf("got[arr] type = %T, want []interface{}", got["arr"])
	}

	// Plain int32 → float64 (matching pricing.go's num() float64-only contract).
	if v, ok := got["plain"].(float64); !ok || v != 42 {
		t.Fatalf("got[plain] = %v (%T), want float64 42", got["plain"], got["plain"])
	}
}
