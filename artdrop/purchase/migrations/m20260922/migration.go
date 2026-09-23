package m20260922

import (
	"time"

	"gorm.io/gorm"
)

const ID = "20260922_purchase_recovery"

type PurchaseCharge struct {
	UserID               string     `gorm:"column:user_id;index;uniqueIndex:idx_purchase_intent,priority:1"`
	ChargeIntentID       *string    `gorm:"column:charge_intent_id;size:128;uniqueIndex:idx_purchase_intent,priority:3"`
	ChargeOperation      *string    `gorm:"size:64;uniqueIndex:idx_purchase_intent,priority:2"`
	RequestHash          *string    `json:"requestHash" gorm:"size:71"`
	HashVersion          *int       `json:"hashVersion"`
	StripeCustomerID     *string    `json:"stripeCustomerId"`
	StripeStatusObserved *string    `json:"stripeStatusObserved"`
	StripeObservedAt     *time.Time `json:"stripeObservedAt"`
}

func (PurchaseCharge) TableName() string { return "purchase_charges" }
func Migrate(tx *gorm.DB) error          { return tx.AutoMigrate(&PurchaseCharge{}) }
func Rollback(tx *gorm.DB) error         { return nil }
