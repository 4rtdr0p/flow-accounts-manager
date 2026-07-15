package artdrop

import (
	"net/http"

	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/gorilla/mux"
)

// Plugin is the artdrop plugin entry point.
type Plugin struct {
	svc *Service
}

// NewPlugin creates the artdrop plugin using the shared application dependencies.
func NewPlugin(deps plugins.PluginDeps) plugins.Plugin {
	return &Plugin{svc: NewService(deps)}
}

// Name returns the plugin name.
func (p *Plugin) Name() string {
	return "artdrop"
}

// RegisterRoutes adds the artdrop plugin routes to the API router.
func (p *Plugin) RegisterRoutes(router *mux.Router, deps plugins.PluginDeps) {
	h := NewHandler(p.svc)

	router.Handle("/accounts/{address}/transfer", h.Transfer()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/setup", h.Setup()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/escrows", h.CreateEscrow()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/escrows/{escrowId}/activate", h.ActivateChip()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/escrows/{escrowId}/activate-chip", h.ActivateChip()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/escrows/{escrowId}/activate-and-settle", h.ActivateChip()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/escrows/{escrowId}/release", h.Release()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/escrows/{escrowId}/cancel", h.Cancel()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/escrows/{escrowId}/refund", h.Refund()).Methods(http.MethodPost)
	router.Handle("/accounts/{address}/artdrop/certificates", h.ListCertificates()).Methods(http.MethodGet)
	router.Handle("/accounts/{address}/artdrop/collection-length", h.GetCollectionLength()).Methods(http.MethodGet)
	router.Handle("/accounts/{address}/artdrop/escrows/{escrowId}", h.GetEscrow()).Methods(http.MethodGet)
	router.Handle("/artdrop/originals/{origId}", h.GetOriginalSummary()).Methods(http.MethodGet)
	router.Handle("/artdrop/editions/{edId}", h.GetEditionSummary()).Methods(http.MethodGet)
	router.Handle("/artdrop/config/platform-fee", h.GetPlatformFee()).Methods(http.MethodGet)
	router.Handle("/artdrop/config/market-mode", h.GetMarketMode()).Methods(http.MethodGet)
	router.Handle("/accounts/{address}/artdrop/is-artist", h.HasCollection()).Methods(http.MethodGet)
}
