// Package purchase provides the buyer purchase charge + escrow flow (#93).
//
// It charges a buyer for an artwork (edition or painting) and opens the
// on-chain escrow that funds the sale, with every amount computed server-side:
// the artwork price is read from Mongo (editions.price /
// paintings.originalPrice), a platform fee is applied, the total is converted
// from USD to FLOW using the Pyth Hermes oracle, and the resulting FLOW amount
// is what both the Stripe charge and the on-chain escrow use. The client only
// identifies the artwork, the parties and the payment details — never an
// amount.
package purchase

import "time"

// ArtworkKind is which kind of artwork is being purchased: an edition or a
// painting. It selects which Mongo collection the price is read from.
type ArtworkKind string

const (
	// ArtworkEdition is a limited edition of an original.
	ArtworkEdition ArtworkKind = "edition"
	// ArtworkPainting is an original painting.
	ArtworkPainting ArtworkKind = "painting"
)

// PurchaseCharge is the audit record for a single buyer purchase charge. It
// records the USD amount actually charged to Stripe, the FLOW amount the
// escrow was opened with, the artwork and parties involved, and the Pyth price
// that converted between them. This lets ops answer "how much was this buyer
// charged, in what, and at what exchange rate" without going back to Mongo or
// the oracle.
type PurchaseCharge struct {
	ID                  uint      `json:"id" gorm:"column:id;primary_key;autoIncrement"`
	UserID              string    `json:"userId" gorm:"column:user_id;index"`
	ArtworkKind         string    `json:"artworkKind" gorm:"column:artwork_kind;size:16"`
	ArtworkID           string    `json:"artworkId" gorm:"column:artwork_id;index"`
	AmountCents         int64     `json:"amountCents" gorm:"column:amount_cents"`
	PlatformFeeCents    int64     `json:"platformFeeCents" gorm:"column:platform_fee_cents"`
	Currency            string    `json:"currency" gorm:"column:currency;size:3;default:usd"`
	FlowAmount          float64   `json:"flowAmount" gorm:"column:flow_amount"`
	FlowPriceUSD        float64   `json:"flowPriceUsd" gorm:"column:flow_price_usd"`
	StripePaymentIntent string    `json:"stripePaymentIntentId" gorm:"column:stripe_payment_intent_id;uniqueIndex"`
	Buyer               string    `json:"buyer" gorm:"column:buyer"`
	Seller              string    `json:"seller" gorm:"column:seller"`
	EditionID           uint64    `json:"editionId" gorm:"column:edition_id"`
	ChipID              string    `json:"chipId" gorm:"column:chip_id"`
	UnlockAt            float64   `json:"unlockAt" gorm:"column:unlock_at"`
	Nonce               uint64    `json:"nonce" gorm:"column:nonce"`
	Metadata            string    `json:"metadata" gorm:"column:metadata;type:text"`
	CreatedAt           time.Time `json:"createdAt" gorm:"column:created_at"`
}

// TableName returns the table name for the audit record.
func (PurchaseCharge) TableName() string {
	return "purchase_charges"
}

// CreatePurchaseChargeInput is the data needed to charge a buyer's purchase
// and open the escrow. The server computes every amount: it reads the artwork
// price from Mongo, applies the configured platform fee, converts the total to
// FLOW via the Pyth oracle, and uses that FLOW amount for both the Stripe
// charge and the on-chain escrow. The client only identifies the artwork, the
// parties and the payment details — no amount, fee or exchange rate is trusted
// from the client.
//
// IdempotencyKey is the client-supplied Idempotency-Key header. It is
// propagated to Stripe so that an HTTP replay of the same logical purchase
// maps to the same PaymentIntent, while a genuinely new purchase (a new key)
// is allowed to charge again.
type CreatePurchaseChargeInput struct {
	UserID           string
	ArtworkKind      ArtworkKind
	ArtworkID        string
	StripeCustomerID string
	PaymentMethodID  string
	IdempotencyKey   string
	Metadata         string

	// Escrow fields. Buyer and Seller are Flow addresses; EditionID, ChipID,
	// UnlockAt and Nonce are the escrow parameters. Amount is NOT accepted
	// here — it is computed server-side.
	Buyer     string
	Seller    string
	EditionID uint64
	ChipID    string
	UnlockAt  float64
	Nonce     uint64
}
