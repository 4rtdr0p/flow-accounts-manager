// Package studio provides the audit trail for Studio production charges.
//
// It records every real charge made against a Studio user: the amount
// actually charged, the Stripe payment intent that settled it, and a hash of
// the pricing config / rate / engine version that produced the amount. This
// lets us answer "how much was user X charged and with what data" without
// having to go back to Mongo.
package studio

import "time"

// FulfillmentMethod is how a Studio stock request's prints reach the artist:
// picked up at the studio (no shipping charge) or delivered (a flat
// per-unit shipping charge is added). It is client-supplied metadata about
// the order, not a trusted amount: the server always computes the shipping
// total itself from the method and the requested quantity.
type FulfillmentMethod string

const (
	FulfillmentPickup   FulfillmentMethod = "pickup"
	FulfillmentDelivery FulfillmentMethod = "delivery"
)

// ProductionCharge is the audit record for a single Studio production charge.
// AmountCents is the total actually charged; ProductionAmountCents and
// ShippingAmountCents split it into the two components ops reconciles
// against. ShippingAmountCents is 0 for a pickup order.
type ProductionCharge struct {
	ID                    uint      `json:"id" gorm:"column:id;primary_key;autoIncrement"`
	UserID                string    `json:"userId" gorm:"column:user_id;index"`
	QuoteID               string    `json:"quoteId" gorm:"column:quote_id;index"`
	AmountCents           int64     `json:"amountCents" gorm:"column:amount_cents"`
	ProductionAmountCents int64     `json:"productionAmountCents" gorm:"column:production_amount_cents"`
	ShippingAmountCents   int64     `json:"shippingAmountCents" gorm:"column:shipping_amount_cents"`
	Currency              string    `json:"currency" gorm:"column:currency;size:3;default:usd"`
	StripePaymentIntent   string    `json:"stripePaymentIntentId" gorm:"column:stripe_payment_intent_id;uniqueIndex"`
	PricingHash           string    `json:"pricingHash" gorm:"column:pricing_hash"`
	EngineVersion         string    `json:"engineVersion" gorm:"column:engine_version"`
	Metadata              string    `json:"metadata" gorm:"column:metadata;type:text"`
	CreatedAt             time.Time `json:"createdAt" gorm:"column:created_at"`
}

// TableName returns the table name for the audit record.
func (ProductionCharge) TableName() string {
	return "studio_production_charges"
}

// CreateProductionChargeInput is the data needed to record a charge. The
// PricingHash is a fingerprint of the config/rate/engine version used to
// compute the amount at charge time (produced by the pricing engine, #70).
// AmountCents must equal ProductionAmountCents + ShippingAmountCents.
type CreateProductionChargeInput struct {
	UserID                string
	QuoteID               string
	AmountCents           int64
	ProductionAmountCents int64
	ShippingAmountCents   int64
	Currency              string
	StripePaymentIntent   string
	PricingHash           string
	EngineVersion         string
	Metadata              string
}

// CreateStockRequestChargeInput is the data needed to charge a Studio stock
// request. The server recomputes the exact price from the quote's config
// snapshot and the active pricing rates at charge time; the client only
// identifies the user, the quote and the quantity, plus the Stripe payment
// details. No amount, hash or engine version is trusted from the client.
//
// FulfillmentMethod controls whether a shipping charge is added; the rate
// itself is server-configured (see Config.StudioShippingRatePerUnitUSD), so
// only whether shipping applies is client-supplied. A caller could
// understate shipping by claiming pickup when the order is actually
// delivered — that trust boundary is weaker than the amount, which is
// entirely server-computed. It defaults to pickup so existing callers that
// don't send it keep paying nothing extra, matching today's behavior.
//
// IdempotencyKey is the client-supplied Idempotency-Key header. It is
// propagated to Stripe so that an HTTP replay of the same logical purchase
// maps to the same PaymentIntent, while a genuinely new purchase (a new key)
// is allowed to charge again.
type CreateStockRequestChargeInput struct {
	UserID            string
	QuoteID           string
	Quantity          int
	FulfillmentMethod FulfillmentMethod
	StripeCustomerID  string
	PaymentMethodID   string
	IdempotencyKey    string
	Metadata          string
}
