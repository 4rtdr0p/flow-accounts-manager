package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const PurchaseOperation = "purchases.charge.v2"

type IdempotencyOperation struct{ Method, Path string }

// All added fields are nullable: old cache rows carry no financial identity.
type PurchaseIntentFields struct {
	RecordKind            *string `gorm:"size:32"`
	OwnerUserID           *string `gorm:"size:191;uniqueIndex:idx_idempotency_purchase_intent,priority:1"`
	ActorSubject          *string
	Operation             *string `gorm:"size:64;uniqueIndex:idx_idempotency_purchase_intent,priority:2"`
	IntentID              *string `gorm:"size:128;uniqueIndex:idx_idempotency_purchase_intent,priority:3"`
	RequestHash           *string `gorm:"size:71"`
	HashVersion           *int
	ProviderKey           *string
	ExecutionState        *string
	ExecutionToken        *string
	LeaseUntil            *time.Time
	CreatedAt             *time.Time
	UpdatedAt             *time.Time
	EffectMayHaveStarted  *bool
	PurchaseID            *string
	StripePaymentIntentID *string `gorm:"size:191;uniqueIndex:idx_purchase_provider_intent"`
	StripeStatusObserved  *string
	StripeObservedAt      *time.Time
	StripeCustomerID      *string
	ErrorCode             *string
	ResponseExpiresAt     *time.Time
}

// PurchaseIntent shares the cache table, but never expires or releases identity.
// Do not use generic Get/TryReserve for these records.
type PurchaseIntent struct {
	Key         string `gorm:"primaryKey"`
	ExpiryDate  *time.Time
	StatusCode  int
	ContentType string
	Body        []byte
	Completed   bool
	PurchaseIntentFields
}

func (PurchaseIntent) TableName() string { return "idempotency_keys" }
func Ptr[T any](v T) *T                  { return &v }
func Value[T any](v *T) (zero T) {
	if v != nil {
		return *v
	}
	return
}

func PurchaseIntentKey(owner, intent string) string {
	b, _ := json.Marshal([]string{"wallet.purchase.identity.v1", owner, PurchaseOperation, intent})
	sum := sha256.Sum256(b)
	return "purchase-v2:" + hex.EncodeToString(sum[:])
}

var ErrIntentCAS = errors.New("purchase intent executor no longer owns active reservation")

func ReadPurchaseIntent(ctx context.Context, db *gorm.DB, owner, intent string) (*PurchaseIntent, error) {
	var row PurchaseIntent
	err := db.WithContext(ctx).Where("key = ? AND record_kind = ? AND owner_user_id = ? AND operation = ? AND intent_id = ?", PurchaseIntentKey(owner, intent), "purchase", owner, PurchaseOperation, intent).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err == nil && (Value(row.OwnerUserID) != owner || Value(row.Operation) != PurchaseOperation || Value(row.IntentID) != intent || Value(row.RecordKind) != "purchase") {
		return nil, ErrIntentCAS
	}
	return &row, err
}

// ReservePurchaseIntent commits an insert before any external effects. Ownership
// is the random token read back from SQL, not dialect-dependent RowsAffected.
func ReservePurchaseIntent(ctx context.Context, db *gorm.DB, owner, actor, intent, hash, customer string, lease time.Duration) (*PurchaseIntent, bool, error) {
	now := time.Now().UTC()
	token := uuid.NewString()
	row := PurchaseIntent{Key: PurchaseIntentKey(owner, intent), PurchaseIntentFields: PurchaseIntentFields{
		RecordKind: Ptr("purchase"), OwnerUserID: Ptr(owner), ActorSubject: Ptr(actor), Operation: Ptr(PurchaseOperation),
		IntentID: Ptr(intent), RequestHash: Ptr(hash), HashVersion: Ptr(1), ProviderKey: Ptr(PurchaseIntentKey(owner, intent)),
		ExecutionState: Ptr("reserved"), ExecutionToken: Ptr(token), LeaseUntil: Ptr(now.Add(lease)),
		CreatedAt: &now, UpdatedAt: &now, EffectMayHaveStarted: Ptr(false), StripeCustomerID: Ptr(customer),
	}}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
	})
	if err != nil {
		return nil, false, err
	}
	existing, err := ReadPurchaseIntent(ctx, db, owner, intent)
	if err != nil {
		return nil, false, err
	}
	if existing == nil {
		return nil, false, ErrIntentCAS
	}
	return existing, Value(existing.ExecutionToken) == token, nil
}

// CASPurchaseIntent never updates terminal results. Expired leases do not permit
// takeover; the original token may still commit a late result.
func CASPurchaseIntent(ctx context.Context, db *gorm.DB, row *PurchaseIntent, from []string, changes map[string]interface{}) error {
	changes["updated_at"] = time.Now().UTC()
	result := db.WithContext(ctx).Model(&PurchaseIntent{}).
		Where("key = ? AND record_kind = 'purchase' AND execution_token = ? AND execution_state IN ?", row.Key, Value(row.ExecutionToken), from).Updates(changes)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrIntentCAS
	}
	return nil
}

// PurgePurchaseResponseBodies removes only replay bytes, never the dedupe marker.
func PurgePurchaseResponseBodies(ctx context.Context, db *gorm.DB, now time.Time) error {
	return db.WithContext(ctx).Model(&PurchaseIntent{}).Where("record_kind = 'purchase' AND response_expires_at <= ? AND body IS NOT NULL", now).
		UpdateColumn("body", nil).Error
}
