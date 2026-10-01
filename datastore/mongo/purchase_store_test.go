package mongo

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/event"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"
)

func TestPurchaseStoreGetEditionPriceByObjectID(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("finds an edition by ObjectID", func(mt *mtest.T) {
		id := primitive.NewObjectID()
		mt.AddMockResponses(mtest.CreateCursorResponse(1, "payload.editions", mtest.FirstBatch,
			bson.D{{Key: "_id", Value: id}, {Key: "price", Value: 12.5}}))

		price, err := newPurchaseStoreForTest(mt, "editions").GetEditionPrice(context.Background(), id.Hex())
		if err != nil {
			mt.Fatalf("GetEditionPrice: %v", err)
		}
		if price.PriceUSD != 12.5 {
			mt.Fatalf("price = %v, want 12.5", price.PriceUSD)
		}
		assertArtworkLookup(mt, "_id")
	})
}

func TestPurchaseStoreGetEditionPriceByStringID(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("finds an edition by string _id", func(mt *mtest.T) {
		mt.AddMockResponses(mtest.CreateCursorResponse(1, "payload.editions", mtest.FirstBatch,
			bson.D{{Key: "_id", Value: "edition-1"}, {Key: "price", Value: 12.5}}))

		price, err := newPurchaseStoreForTest(mt, "editions").GetEditionPrice(context.Background(), "edition-1")
		if err != nil {
			mt.Fatalf("GetEditionPrice: %v", err)
		}
		if price.PriceUSD != 12.5 {
			mt.Fatalf("price = %v, want 12.5", price.PriceUSD)
		}
		assertArtworkLookup(mt, "_id")
	})
}

func TestPurchaseStoreGetEditionPriceFallsBackToID(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("falls back to id", func(mt *mtest.T) {
		mt.AddMockResponses(
			mtest.CreateCursorResponse(0, "payload.editions", mtest.FirstBatch),
			mtest.CreateCursorResponse(1, "payload.editions", mtest.FirstBatch,
				bson.D{{Key: "id", Value: "edition-1"}, {Key: "price", Value: 12.5}}),
		)

		price, err := newPurchaseStoreForTest(mt, "editions").GetEditionPrice(context.Background(), "edition-1")
		if err != nil {
			mt.Fatalf("GetEditionPrice: %v", err)
		}
		if price.PriceUSD != 12.5 {
			mt.Fatalf("price = %v, want 12.5", price.PriceUSD)
		}
		assertArtworkLookups(mt, "_id", "id")
	})
}

func TestPurchaseStoreGetPaintingPriceByObjectID(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("finds a painting by ObjectID", func(mt *mtest.T) {
		id := primitive.NewObjectID()
		mt.AddMockResponses(mtest.CreateCursorResponse(1, "payload.paintings", mtest.FirstBatch,
			bson.D{{Key: "_id", Value: id}, {Key: "originalPrice", Value: 25.0}}))

		price, err := newPurchaseStoreForTest(mt, "paintings").GetPaintingPrice(context.Background(), id.Hex())
		if err != nil {
			mt.Fatalf("GetPaintingPrice: %v", err)
		}
		if price.PriceUSD != 25.0 {
			mt.Fatalf("price = %v, want 25", price.PriceUSD)
		}
		assertArtworkLookup(mt, "_id")
	})
}

func TestPurchaseStoreGetPaintingPriceFallsBackToID(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("falls back to id", func(mt *mtest.T) {
		mt.AddMockResponses(
			mtest.CreateCursorResponse(0, "payload.paintings", mtest.FirstBatch),
			mtest.CreateCursorResponse(1, "payload.paintings", mtest.FirstBatch,
				bson.D{{Key: "id", Value: "painting-1"}, {Key: "originalPrice", Value: 25.0}}),
		)

		price, err := newPurchaseStoreForTest(mt, "paintings").GetPaintingPrice(context.Background(), "painting-1")
		if err != nil {
			mt.Fatalf("GetPaintingPrice: %v", err)
		}
		if price.PriceUSD != 25.0 {
			mt.Fatalf("price = %v, want 25", price.PriceUSD)
		}
		assertArtworkLookups(mt, "_id", "id")
	})
}

func TestPurchaseStoreArtworkNotFound(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("returns not found after both lookups", func(mt *mtest.T) {
		mt.AddMockResponses(
			mtest.CreateCursorResponse(0, "payload.editions", mtest.FirstBatch),
			mtest.CreateCursorResponse(0, "payload.editions", mtest.FirstBatch),
		)

		_, err := newPurchaseStoreForTest(mt, "editions").GetEditionPrice(context.Background(), "missing")
		assertArtworkLookups(mt, "_id", "id")
		if !errors.Is(err, ErrArtworkNotFound) {
			mt.Fatalf("error = %v, want ErrArtworkNotFound", err)
		}
	})
}

func TestPurchaseStoreArtworkPriceMissing(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("returns price missing without fallback", func(mt *mtest.T) {
		mt.AddMockResponses(mtest.CreateCursorResponse(1, "payload.editions", mtest.FirstBatch,
			bson.D{{Key: "_id", Value: "edition-1"}}))

		_, err := newPurchaseStoreForTest(mt, "editions").GetEditionPrice(context.Background(), "edition-1")
		assertArtworkLookup(mt, "_id")
		if !errors.Is(err, ErrArtworkPriceMissing) {
			mt.Fatalf("error = %v, want ErrArtworkPriceMissing", err)
		}
	})
}

func TestPurchaseStoreDoesNotFallbackOnMongoError(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("propagates non-not-found errors", func(mt *mtest.T) {
		mt.AddMockResponses(mtest.CreateCommandErrorResponse(mtest.CommandError{
			Code:    13,
			Message: "unauthorized",
		}))

		_, err := newPurchaseStoreForTest(mt, "editions").GetEditionPrice(context.Background(), "edition-1")
		assertArtworkLookup(mt, "_id")
		if err == nil || !strings.Contains(err.Error(), "unauthorized") {
			mt.Fatalf("error = %v, want the Mongo error", err)
		}
	})
}

func newPurchaseStoreForTest(mt *mtest.T, collection string) *PurchaseStore {
	return &PurchaseStore{
		client:        &Client{client: mt.Client, db: mt.Client.Database("payload")},
		editionsColl:  collection,
		paintingsColl: collection,
		mirrorColl:    "blockchain-editions",
	}
}

// #135 keys fresh-purchase price lookups by the numeric on-chain edition id,
// but the editions collection is keyed by Payload ObjectIds. The lookup must
// resolve the numeric id through the blockchain-editions mirror instead of
// failing with ErrArtworkNotFound.
func TestPurchaseStoreGetEditionPriceByChainIDMirror(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
	mt.Run("resolves the numeric chain id through the mirror", func(mt *mtest.T) {
		id := primitive.NewObjectID()
		mt.AddMockResponses(
			// editions _id:"16" (string) → miss
			mtest.CreateCursorResponse(0, "payload.editions", mtest.FirstBatch),
			// editions id:"16" → miss
			mtest.CreateCursorResponse(0, "payload.editions", mtest.FirstBatch),
			// mirror {blockchainEditionId: 16, editionId: <hex>} → hit
			mtest.CreateCursorResponse(1, "payload.blockchain-editions", mtest.FirstBatch,
				bson.D{{Key: "blockchainEditionId", Value: int64(16)}, {Key: "editionId", Value: id.Hex()}}),
			// ack for the mirror cursor teardown (killCursors)
			mtest.CreateSuccessResponse(),
			// editions _id:ObjectID(<hex>) → hit with price
			mtest.CreateCursorResponse(1, "payload.editions", mtest.FirstBatch,
				bson.D{{Key: "_id", Value: id}, {Key: "price", Value: 12.5}}),
		)

		price, err := newPurchaseStoreForTest(mt, "editions").GetEditionPrice(context.Background(), "16")
		if err != nil {
			mt.Fatalf("GetEditionPrice: %v", err)
		}
		if price.PriceUSD != 12.5 {
			mt.Fatalf("price = %v, want 12.5", price.PriceUSD)
		}
		if price.ID != id.Hex() {
			mt.Fatalf("price.ID = %q, want the resolved Payload id %q", price.ID, id.Hex())
		}
		// Four finds: editions(_id), editions(id), mirror(blockchainEditionId),
		// editions(_id resolved).
		finds := 0
		for _, e := range mt.GetAllStartedEvents() {
			if e.CommandName == "find" {
				finds++
			}
		}
		if finds != 4 {
			mt.Fatalf("find count = %d, want 4", finds)
		}
	})

	mt.Run("keeps ErrArtworkNotFound when the mirror misses too", func(mt *mtest.T) {
		mt.AddMockResponses(
			// editions _id:"16" (string) → miss
			mtest.CreateCursorResponse(0, "payload.editions", mtest.FirstBatch),
			// editions id:"16" → miss
			mtest.CreateCursorResponse(0, "payload.editions", mtest.FirstBatch),
			// mirror → miss
			mtest.CreateCursorResponse(0, "payload.blockchain-editions", mtest.FirstBatch),
			// resolved editions _id:ObjectID → miss
			mtest.CreateCursorResponse(0, "payload.editions", mtest.FirstBatch),
			// resolved editions _id:<hex string> → miss
			mtest.CreateCursorResponse(0, "payload.editions", mtest.FirstBatch),
			// resolved editions id:<hex> → miss
			mtest.CreateCursorResponse(0, "payload.editions", mtest.FirstBatch),
		)

		_, err := newPurchaseStoreForTest(mt, "editions").GetEditionPrice(context.Background(), "16")
		if !errors.Is(err, ErrArtworkNotFound) {
			mt.Fatalf("error = %v, want ErrArtworkNotFound", err)
		}
	})
}

func assertArtworkLookup(t *mtest.T, field string) {
	assertArtworkLookups(t, field)
}

func assertArtworkLookups(t *mtest.T, fields ...string) {
	t.Helper()
	events := make([]*event.CommandStartedEvent, 0)
	for _, candidate := range t.GetAllStartedEvents() {
		if candidate.CommandName == "find" {
			events = append(events, candidate)
		}
	}
	if len(events) != len(fields) {
		t.Fatalf("lookup count = %d, want %d", len(events), len(fields))
	}
	for i, field := range fields {
		filter, ok := events[i].Command.Lookup("filter").DocumentOK()
		if !ok {
			t.Fatalf("lookup %d has no filter: %s", i, events[i].Command)
		}
		if _, ok := filter.Lookup(field).StringValueOK(); !ok {
			if field == "_id" {
				if _, ok := filter.Lookup(field).ObjectIDOK(); ok {
					continue
				}
			}
			t.Fatalf("lookup %d filter = %s, want field %q", i, filter, field)
		}
	}
}
