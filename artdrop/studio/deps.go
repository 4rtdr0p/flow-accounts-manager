package studio

import (
	"context"

	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
)

// QuoteReader reads a studio quote by id from Mongo. It is implemented by
// *datastoremongo.QuoteStore.
type QuoteReader interface {
	GetByID(ctx context.Context, quoteID string) (*datastoremongo.StudioQuote, error)
}

// PriceEngine recomputes the price for a Studio stock request from the quote's
// config snapshot and the active pricing rates. It returns the server-computed
// amount in cents, the pricing hash and engine version that produced it, and
// maxQuantity: the largest production tier the engine itself produces for this
// config (the same authoritative source as the price), so the caller can
// reject a requested quantity above it.
//
// The interface deliberately uses only primitive types so the engine
// implementation (in the pricing package) can satisfy it structurally without
// importing this package, keeping the dependency graph acyclic.
type PriceEngine interface {
	Quote(ctx context.Context, config map[string]any, runSize int) (amountCents int64, pricingHash string, engineVersion string, maxQuantity int, err error)
}

// ChargeClient creates and confirms a Stripe PaymentIntent. It is implemented
// by *StripeClient.
type ChargeClient interface {
	CreateAndConfirm(ctx context.Context, in StripeChargeInput) (*StripePaymentIntent, error)
}
