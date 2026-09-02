package artdrop

import (
	"context"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop/purchase"
	"github.com/flow-hydraulics/flow-wallet-api/artdrop/studio"
	"github.com/flow-hydraulics/flow-wallet-api/artdrop/studio/pricing"
	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	"github.com/gorilla/mux"
	"github.com/onflow/flow-go-sdk"
	log "github.com/sirupsen/logrus"
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

// ReEscrow adapts *Service.ReEscrow for the purchase flow's re-escrow branch
// (issue #107): it re-offers an EXISTING certificate (identified by
// certificateID, no re-mint) with the server-computed amount, instead of
// minting a new one. Same server-controlled logicOwner/vault discipline as
// CreateEscrow — those come from Service, never this call.
func (a purchaseEscrowCreator) ReEscrow(ctx context.Context, sync bool, address string, buyer, seller string, certificateID uint64, chipID string, unlockAt float64, nonce uint64, amount float64) (*jobs.Job, *transactions.Transaction, error) {
	return a.svc.ReEscrow(ctx, sync, address, ReEscrowRequest{
		Buyer:         buyer,
		Seller:        seller,
		CertificateId: certificateID,
		ChipId:        chipID,
		UnlockAt:      unlockAt,
		Nonce:         nonce,
		Amount:        amount,
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
	// reads the artwork price from Mongo, charges that full price via Stripe,
	// applies the configured platform fee and converts only that fee to FLOW
	// via the FLOW/USD oracle as the escrow's gas reserve, opens the escrow, and
	// persists the audit record.
	// It reuses the studio Stripe client and the artdrop escrow creator.
	var purchasePlatformFeeBps int
	var devFlowUSDPrice float64
	var mainnet bool
	if deps.Config != nil {
		purchasePlatformFeeBps = deps.Config.PurchasePlatformFeeBasisPoints
		devFlowUSDPrice = deps.Config.DevFlowUSDPrice
		mainnet = deps.Config.ChainID == flow.Mainnet
	}
	purchaseStore := datastoremongo.NewPurchaseStore(deps.Mongo, deps.Config)
	// Oracle precedence (issue #121): the fixed-price override wins only for
	// non-mainnet QA (Parse already rejects a fixed price on mainnet); otherwise
	// the on-chain pool oracle is the default on every network. No Pyth.
	var oracle purchase.PriceOracle
	if devFlowUSDPrice > 0 && !mainnet {
		oracle = purchase.FixedPriceOracle{PriceUSD: devFlowUSDPrice}
		log.Warn("artdrop purchase: using fixed FLOW/USD price for non-mainnet QA")
	} else {
		acfg := p.svc.cfg
		oracle = purchase.NewPoolPriceOracle(purchase.PoolOracleConfig{
			RPCURLs:         strings.Split(acfg.FlowEVMRPCURL, ","),
			Pools:           acfg.OraclePoolsParsed,
			TTL:             acfg.OracleTTL,
			MaxDeviationBps: acfg.OracleMaxDeviationBps,
			MinPools:        acfg.OracleMinPools,
			MinSurvivors:    acfg.OracleMinSurvivors,
			SanityMinUSD:    acfg.OracleSanityMinUSD,
			SanityMaxUSD:    acfg.OracleSanityMaxUSD,
			RPCTimeout:      acfg.OracleRPCTimeout,
		})
	}
	purchaseService := purchase.NewService(
		purchase.NewGormStore(deps.DB),
		purchaseStore,
		oracle,
		stripeClient,
		purchaseEscrowCreator{svc: p.svc},
		purchasePlatformFeeBps,
		p.svc.cfg.EscrowClaimWindowSeconds,
	)
	purchaseHandler := purchase.NewHandler(purchaseService)
	router.Handle("/purchases:charge", purchaseHandler.CreatePurchaseCharge()).Methods(http.MethodPost)

	// Chip provisioning (issue #117, phase 2): create a chip's custodial
	// account, register its P-256 pubkey on-chain, and persist the
	// chipId -> account mapping. See docs/CHIP-SIGNING-DESIGN.md §2.
	router.Handle("/chips", h.ProvisionChip()).Methods(http.MethodPost)
	router.Handle("/chips/{chipId}", h.GetChip()).Methods(http.MethodGet)

	router.Handle("/accounts/{address}/transfer", h.Transfer()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/setup", h.Setup()).Methods(http.MethodPost)
	router.Handle("/accounts/{artistAddress}/artdrop/artist-direct/setup", h.SetupArtistDirect()).Methods(http.MethodPost)
	router.Handle("/accounts/{artistAddress}/artdrop/originals", h.CreateOriginal()).Methods(http.MethodPost)
	router.Handle("/accounts/{artistAddress}/artdrop/originals/{originalId}/editions", h.CreateEdition()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/escrows", h.ListEscrows()).Methods(http.MethodGet)
	router.Handle("/accounts/{address}/artdrop/escrows/re-escrow", h.ReEscrow()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/escrows/{escrowId}/activate", h.ActivateChip()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/escrows/{escrowId}/activate-chip", h.ActivateChip()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/escrows/{escrowId}/activate-and-settle", h.ActivateChip()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/escrows/{escrowId}/void", h.VoidEscrow()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/certificates", h.ListCertificates()).Methods(http.MethodGet)
	router.Handle("/accounts/{address}/artdrop/certificates/{certId}", h.GetCertificateDetail()).Methods(http.MethodGet)
	router.Handle("/accounts/{address}/artdrop/collection-length", h.GetCollectionLength()).Methods(http.MethodGet)
	// by-edition, by-seller and by-artist (#100/#102) MUST be registered
	// before the /escrows/{escrowId} catch-all below: gorilla/mux matches
	// routes in registration order, and "/escrows/by-edition/..." (extra
	// path segment, no real conflict) and especially "/escrows/by-seller"
	// / "/escrows/by-artist" (same single-segment shape as {escrowId})
	// would otherwise be swallowed by GetEscrowFunc with
	// escrowId="by-seller"/"by-artist", failing ParseUint with a 400
	// instead of ever reaching the intended handler.
	router.Handle("/accounts/{address}/artdrop/escrows/by-edition/{editionId}", h.ListEscrowsByEdition()).Methods(http.MethodGet)
	// by-seller = literal "escrows where {address} is the seller"
	// (projection-served, WHERE seller = address) vs. by-artist = "escrows
	// on editions {address} created as an artist" (chain-served three-index
	// walk) — split for issue #102, see Service.ListEscrowsBySeller's doc
	// comment for the full reasoning; both share the EscrowSummary shape.
	router.Handle("/accounts/{address}/artdrop/escrows/by-seller", h.ListEscrowsBySeller()).Methods(http.MethodGet)
	router.Handle("/accounts/{address}/artdrop/escrows/by-artist", h.ListEscrowsByArtist()).Methods(http.MethodGet)
	router.Handle("/accounts/{address}/artdrop/escrows/{escrowId}", h.GetEscrow()).Methods(http.MethodGet)
	router.Handle("/artdrop/originals/{origId}", h.GetOriginalSummary()).Methods(http.MethodGet)
	router.Handle("/artdrop/originals/{origId}/edition-ids", h.GetEditionIDsByOriginal()).Methods(http.MethodGet)
	router.Handle("/artdrop/editions/{edId}", h.GetEditionSummary()).Methods(http.MethodGet)
	router.Handle("/artdrop/config/platform-fee", h.GetPlatformFee()).Methods(http.MethodGet)
	router.Handle("/artdrop/config/market-mode", h.GetMarketMode()).Methods(http.MethodGet)
	router.Handle("/accounts/{address}/artdrop/is-artist", h.IsArtist()).Methods(http.MethodGet)

	// One-time escrow projection backfill (issue #102) — self-healing: a
	// no-op unless the escrows table is still completely empty (see
	// Service.BackfillEscrowProjection), so it's safe to trigger on every
	// boot; RegisterRoutes only ever runs once per process, same as this
	// plugin's other one-time setup above. Runs in a goroutine so a slow or
	// unavailable access node never delays server startup — until it
	// completes, GetEscrow/ListEscrowsBy* keep falling back to the chain
	// exactly as they did before #102. Skipped when the chain listener
	// itself is disabled (same flag main.go gates the listener with): with
	// no listener keeping the projection current afterwards, seeding it
	// here would be pointless and would still cost a chain call in setups
	// that deliberately asked for none.
	if deps.Config == nil || !deps.Config.DisableChainEvents {
		// resync is gated by its own flag (see Config.EscrowProjectionResync):
		// the backfill runs every boot (self-healing no-op once seeded), but
		// the resync re-reads the chain and rewrites existing rows' status, so
		// it only runs when an operator explicitly asks for it — typically for
		// one boot after deploying the EscrowVoided handler (issue #109).
		resync := p.svc.cfg.EscrowProjectionResync
		go func() {
			written, err := p.svc.BackfillEscrowProjection(context.Background())
			if err != nil {
				log.WithError(err).Warn("escrow projection: backfill failed, will retry next boot")
			} else if written > 0 {
				log.WithField("written", written).Info("escrow projection: backfill wrote rows")
			}

			if !resync {
				return
			}
			// Run after the backfill so a first-ever boot (empty table) seeds
			// then reconciles in one pass. On an already-seeded table the
			// backfill was a no-op and this repairs the stale rows (e.g. the
			// escrows voided before #109 subscribed to EscrowVoided).
			updated, err := p.svc.ResyncEscrowProjection(context.Background())
			if err != nil {
				log.WithError(err).Warn("escrow projection: resync failed, will retry next boot")
				return
			}
			if updated > 0 {
				log.WithField("updated", updated).Info("escrow projection: resync updated stale rows")
			}
		}()
	}
}
