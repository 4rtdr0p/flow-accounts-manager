package purchase

import (
	"context"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop/studio"
	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
)

// ArtworkPriceReader reads the server-side price of an artwork from Mongo. It
// is implemented by *datastoremongo.PurchaseStore.
type ArtworkPriceReader interface {
	GetEditionPrice(ctx context.Context, editionID string) (*datastoremongo.ArtworkPrice, error)
	GetPaintingPrice(ctx context.Context, paintingID string) (*datastoremongo.ArtworkPrice, error)
}

// PriceOracle reads the current FLOW/USD price from the Pyth Hermes oracle. It
// is implemented by *PythClient.
type PriceOracle interface {
	Latest(ctx context.Context) (*PythPrice, error)
}

// ChargeClient creates and confirms a Stripe PaymentIntent. It is implemented
// by *studio.StripeClient.
type ChargeClient interface {
	CreateAndConfirm(ctx context.Context, in studio.StripeChargeInput) (*studio.StripePaymentIntent, error)
}

// EscrowCreator opens an on-chain escrow for the purchase. It is implemented
// by an adapter over *artdrop.Service (see the artdrop plugin wiring), which
// owns the transaction submission and the server-controlled escrow arguments
// (LogicOwner, vault identifier). The amount passed in is the server-computed
// FLOW amount.
//
// The interface deliberately uses only primitive types so the artdrop package
// can satisfy it structurally without importing this package, keeping the
// dependency graph acyclic.
type EscrowCreator interface {
	CreateEscrow(ctx context.Context, sync bool, address string, buyer, seller string, editionID uint64, chipID string, unlockAt float64, nonce uint64, amount float64) (*jobs.Job, *transactions.Transaction, error)
}
