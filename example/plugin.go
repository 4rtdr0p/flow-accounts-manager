package example

import (
	"net/http"

	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/gorilla/mux"
)

// Plugin is the example plugin entry point.
type Plugin struct {
	service *Service
}

// NewPlugin creates the example plugin using the shared application dependencies.
func NewPlugin(deps plugins.PluginDeps) plugins.Plugin {
	return &Plugin{service: NewService(deps)}
}

// Name returns the plugin name.
func (p *Plugin) Name() string {
	return "example"
}

// RegisterRoutes adds the example plugin routes to the API router.
func (p *Plugin) RegisterRoutes(router *mux.Router, deps plugins.PluginDeps) {
	h := NewHandler(p.service)
	router.Handle("/accounts/{address}/setup-example", h.Setup()).Methods(http.MethodPost)
}
