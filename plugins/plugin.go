package plugins

import (
	"github.com/flow-hydraulics/flow-wallet-api/accounts"
	"github.com/flow-hydraulics/flow-wallet-api/configs"
	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/tokens"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	"github.com/gorilla/mux"
)

// Plugin defines the extension point for adding route groups to the API.
type Plugin interface {
	Name() string
	RegisterRoutes(router *mux.Router, deps PluginDeps)
}

// PluginDeps contains the shared application services available to plugins.
type PluginDeps struct {
	Accounts     accounts.Service
	Tokens       tokens.Service
	Transactions transactions.Service
	Config       *configs.Config
	WorkerPool   jobs.WorkerPool
	// Mongo is the read-only Mongo client for Payload pricing data. It is nil
	// when Mongo is not configured (MONGO_URI empty).
	Mongo *datastoremongo.Client
}
