package artdrop

import (
	"context"
	"math"
	"net/http"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop/purchase"
	"github.com/flow-hydraulics/flow-wallet-api/artdrop/studio"
	"github.com/flow-hydraulics/flow-wallet-api/artdrop/studio/pricing"
	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	"github.com/gorilla/mux"
)

// Plugin is the artdrop plugin entry point.
type Plugin struct {
	svc *Service
}

// purchaseEscrowCreator adapts *Service.CreateEscrow (which takes an
// artdrop.CreateEscrowRequest) to the primitive-based purchase.EscrowCreator
// interface. It lives in the artdrop package because the purchase package must
// not import artdrop (plugin.go imports purchase, so the reverse would be an
// import cycle). The amount passed in is the server-computed FLOW amount.
type purchaseEscrowCreator struct {
	svc *Service
}

func (a purchaseEscrowCreator) CreateEscrow(ctx context.Context, sync bool, address string, buyer, seller string, editionID uint64, chipID string, unlockAt float64, nonce uint64, amount float64) (*jobs.Job, *transactions.Transaction, error) {
	return a.svc.CreateEscrow(ctx, sync, address, CreateEscrowRequest{
		Buyer:     buyer,
		Seller:    seller,
		EditionId: editionID,
		ChipId:    chipID,
		UnlockAt:  unlockAt,
		Nonce:     nonce,
		Amount:    amount,
	})
}

// NewPlugin creates the artdrop plugin using the shared application
// dependencies and the artdrop contract-address config (see LoadConfig).
// Returns an error if cfg fails to validate — see NewService.
func NewPlugin(deps plugins.PluginDeps, cfg *Config) (plugins.Plugin, error) {
	svc, err := NewService(deps, cfg)
	if err != nil {
		return nil, err
	}
	return &Plugin{svc: svc}, nil
}

// Name returns the plugin name.
func (p *Plugin) Name() string {
	return "artdrop"
}

// RegisterRoutes adds the artdrop plugin routes to the API router.
func (p *Plugin) RegisterRoutes(router *mux.Router, deps plugins.PluginDeps) {
	h := NewHandler(p.svc)

	// Studio pricing: expose the active pricing-configurations row from Mongo
	// behind an in-memory cache invalidated when the active row's updatedAt
	// changes. When Mongo is not configured the store is nil and the endpoint
	// reports studio pricing as disabled (503).
	var pricingCacheTTL time.Duration
	var stripeSecretKey string
	var shippingRateCentsPerUnit int64
	if deps.Config != nil {
		pricingCacheTTL = deps.Config.StudioPricingCacheTTL
		stripeSecretKey = deps.Config.StripeSecretKey
		shippingRateCentsPerUnit = int64(math.Round(deps.Config.StudioShippingRatePerUnitUSD * 100))
	}
	pricingSvc := pricing.NewActiveService(datastoremongo.NewPricingStore(deps.Mongo, deps.Config), pricingCacheTTL)
	pricingHandler := pricing.NewHandler(pricingSvc)
	router.Handle("/studio/pricing/active", pricingHandler.GetActive()).Methods(http.MethodGet)

	// Studio quotes: compute a price snapshot from the active rates (#69) using
	// the ported engine (#68). Returns the price plus a hash proving which rates
	// and engine version were used.
	quoteSvc := pricing.NewQuoteService(pricingSvc)
	quoteHandler := pricing.NewQuoteHandler(quoteSvc)
	router.Handle("/studio/quotes:price", quoteHandler.Quote()).Methods(http.MethodPost)

	// Studio production charge auditing. The create endpoint is idempotent per
	// Stripe payment intent (see studio.Service.RecordProductionCharge). It
	// reuses the pricing/quote services above to recompute the exact price at
	// charge time rather than trusting the client.
	chargeEngine := pricing.NewChargeEngine(quoteSvc)
	stripeClient := studio.NewStripeClient(stripeSecretKey, "")
	quoteStore := datastoremongo.NewQuoteStore(deps.Mongo, deps.Config)
	studioService := studio.NewChargeService(studio.NewGormStore(deps.DB), quoteStore, chargeEngine, stripeClient, shippingRateCentsPerUnit)
	studioHandler := studio.NewHandler(studioService)
	router.Handle("/stock-requests:create", studioHandler.CreateStockRequest()).Methods(http.MethodPost)
	router.Handle("/studio/charges", studioHandler.ListCharges()).Methods(http.MethodGet)

	// Buyer purchase charge + escrow (#93). The endpoint charges the buyer's
	// purchase and opens the on-chain escrow with a server-computed amount: it
	// reads the artwork price from Mongo, applies the configured platform fee,
	// converts the total to FLOW via the Pyth oracle, creates and confirms a
	// Stripe PaymentIntent, opens the escrow, and persists the audit record.
	// It reuses the studio Stripe client and the artdrop escrow creator.
	var purchasePlatformFeeBps int
	var pythMaxAge time.Duration
	var pythBaseURL, pythFeedID string
	if deps.Config != nil {
		purchasePlatformFeeBps = deps.Config.PurchasePlatformFeeBasisPoints
		pythMaxAge = deps.Config.PythMaxAge
		pythBaseURL = deps.Config.PythHermesBaseURL
		pythFeedID = deps.Config.PythHermesFeedID
	}
	purchaseStore := datastoremongo.NewPurchaseStore(deps.Mongo, deps.Config)
	pythClient := purchase.NewPythClient(pythBaseURL, pythFeedID, pythMaxAge)
	purchaseService := purchase.NewService(
		purchase.NewGormStore(deps.DB),
		purchaseStore,
		pythClient,
		stripeClient,
		purchaseEscrowCreator{svc: p.svc},
		purchasePlatformFeeBps,
	)
	purchaseHandler := purchase.NewHandler(purchaseService)
	router.Handle("/purchases:charge", purchaseHandler.CreatePurchaseCharge()).Methods(http.MethodPost)

	router.Handle("/accounts/{address}/transfer", h.Transfer()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/setup", h.Setup()).Methods(http.MethodPost)
	router.Handle("/accounts/{artistAddress}/artdrop/artist-direct/setup", h.SetupArtistDirect()).Methods(http.MethodPost)
	router.Handle("/accounts/{artistAddress}/artdrop/originals", h.CreateOriginal()).Methods(http.MethodPost)
	router.Handle("/accounts/{artistAddress}/artdrop/originals/{originalId}/editions", h.CreateEdition()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/escrows", h.CreateEscrow()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/escrows/re-escrow", h.ReEscrow()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/escrows/{escrowId}/activate", h.ActivateChip()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/escrows/{escrowId}/activate-chip", h.ActivateChip()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/escrows/{escrowId}/activate-and-settle", h.ActivateChip()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/certificates", h.ListCertificates()).Methods(http.MethodGet)
	router.Handle("/accounts/{address}/artdrop/certificates/{certId}", h.GetCertificateDetail()).Methods(http.MethodGet)
	router.Handle("/accounts/{address}/artdrop/collection-length", h.GetCollectionLength()).Methods(http.MethodGet)
	router.Handle("/accounts/{address}/artdrop/escrows/{escrowId}", h.GetEscrow()).Methods(http.MethodGet)
	router.Handle("/artdrop/originals/{origId}", h.GetOriginalSummary()).Methods(http.MethodGet)
	router.Handle("/artdrop/originals/{origId}/edition-ids", h.GetEditionIDsByOriginal()).Methods(http.MethodGet)
	router.Handle("/artdrop/editions/{edId}", h.GetEditionSummary()).Methods(http.MethodGet)
	router.Handle("/artdrop/config/platform-fee", h.GetPlatformFee()).Methods(http.MethodGet)
	router.Handle("/artdrop/config/market-mode", h.GetMarketMode()).Methods(http.MethodGet)
	router.Handle("/accounts/{address}/artdrop/is-artist", h.IsArtist()).Methods(http.MethodGet)
}
