package m20260922

import (
	"time"

	"gorm.io/gorm"
)

const ID = "20260922_purchase_intents"

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

type IntentMigration struct {
	Key        string `gorm:"primaryKey"`
	ExpiryDate *time.Time
	PurchaseIntentFields
}

func (IntentMigration) TableName() string { return "idempotency_keys" }
func Migrate(tx *gorm.DB) error           { return tx.AutoMigrate(&IntentMigration{}) }

// Rollback preserves financial identities; reverting the binary must suspend charges.
func Rollback(tx *gorm.DB) error { return nil }
