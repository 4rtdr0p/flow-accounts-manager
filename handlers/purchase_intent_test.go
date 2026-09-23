package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPurchaseIdentitySurvivesGenericCacheMaintenance(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "cache.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&PurchaseIntent{}); err != nil {
		t.Fatal(err)
	}
	row, won, err := ReservePurchaseIntent(context.Background(), db, "owner", "actor", "intent", "hash", "cus", 2*time.Minute)
	if err != nil || !won {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := db.Model(row).Updates(map[string]interface{}{"expiry_date": old, "response_expires_at": old, "body": []byte("old body")}).Error; err != nil {
		t.Fatal(err)
	}
	cache := NewIdempotencyStoreGorm(db)
	if _, found, err := cache.Get(row.Key); err != nil || found {
		t.Fatalf("generic read found purchase: %v %v", found, err)
	}
	for _, op := range []func() error{cache.Prune, func() error { return cache.Release(row.Key) }, func() error {
		return cache.SetResponse(row.Key, IdempotencyRecord{Body: []byte("poison"), StatusCode: 400}, time.Hour)
	}} {
		if err := op(); err != nil {
			t.Fatal(err)
		}
	}
	if err := PurgePurchaseResponseBodies(context.Background(), db, time.Now()); err != nil {
		t.Fatal(err)
	}
	actual, err := ReadPurchaseIntent(context.Background(), db, "owner", "intent")
	if err != nil || actual == nil {
		t.Fatal("permanent identity deleted", err)
	}
	if Value(actual.ExecutionState) != "reserved" || actual.Completed || actual.StatusCode != 0 || len(actual.Body) != 0 {
		t.Fatalf("generic cache altered purchase: %+v", actual)
	}
	_, won, err = ReservePurchaseIntent(context.Background(), db, "owner", "actor", "intent", "hash", "cus", 2*time.Minute)
	if err != nil || won {
		t.Fatalf("expired identity recycled: won=%v err=%v", won, err)
	}
	// Other POSTs retain ordinary cache expiry/release semantics.
	if err := cache.SetResponse("generic", IdempotencyRecord{Body: []byte("response"), StatusCode: 201}, -time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := cache.Prune(); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&IdempotencyRecord{}).Where("key = ?", "generic").Count(&count)
	if count != 0 {
		t.Fatal("generic cache no longer prunes")
	}
}
func TestExactPurchaseIdempotencyExclusion(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{"POST", "/v1/purchases:charge", 201}, {"POST", "/v1/purchases:charge/extra", 400},
		{"POST", "/v1/purchases/id:open-escrow", 400}, {"POST", "/v2/purchases:charge", 400},
		{"GET", "/v1/purchases:charge", 201},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			h := IdempotencyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201) }), IdempotencyHandlerOptions{Expiry: time.Hour, IgnoreOperations: []IdempotencyOperation{{Method: "POST", Path: "/v1/purchases:charge"}}}, NewIdempotencyStoreLocal())
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.want {
				t.Fatalf("got %d want %d", w.Code, tc.want)
			}
		})
	}
}

func TestPurchaseReadRequiresExactIdentityEvenWithCaseInsensitiveCollation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// Emulate a deployment where SQL text comparison folds case. Digest lookup
	// must still prevent reading another owner's financial reservation.
	if err := db.Exec("CREATE TABLE idempotency_keys (key TEXT PRIMARY KEY, owner_user_id TEXT COLLATE NOCASE, operation TEXT COLLATE NOCASE, intent_id TEXT COLLATE NOCASE)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&PurchaseIntent{}); err != nil {
		t.Fatal(err)
	}
	_, won, err := ReservePurchaseIntent(context.Background(), db, "User-1", "User-1", "intent", "hash", "cus", 2*time.Minute)
	if err != nil || !won {
		t.Fatal(err)
	}
	var matches int64
	if err := db.Table("idempotency_keys").Where("owner_user_id = ?", "user-1").Count(&matches).Error; err != nil || matches != 1 {
		t.Fatalf("case-insensitive fixture missing: %d %v", matches, err)
	}
	row, err := ReadPurchaseIntent(context.Background(), db, "user-1", "intent")
	if err != nil || row != nil {
		t.Fatalf("other case owner leaked: row=%v err=%v", row, err)
	}
}
