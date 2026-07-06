package example

import (
	"net/http"

	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/gorilla/mux"
)

// Handler exposes HTTP endpoints for the example plugin.
type Handler struct {
	service *Service
}

// NewHandler creates a handler backed by the given example service.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// NewSetupHandler is a convenience constructor that creates a handler from plugin deps.
// It is used by main.go to keep the legacy /accounts/{address}/setup route working.
func NewSetupHandler(deps plugins.PluginDeps) http.Handler {
	return NewHandler(NewService(deps)).Setup()
}

// Setup returns the HTTP handler for POST /accounts/{address}/setup-example.
func (h *Handler) Setup() http.Handler {
	return http.HandlerFunc(h.SetupFunc)
}

// SetupFunc handles the example account setup request.
func (h *Handler) SetupFunc(rw http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	address := vars["address"]

	sync := r.FormValue(handlers.SyncQueryParameter) != ""
	job, transaction, err := h.service.SetupExampleAccount(r.Context(), sync, address)

	if err != nil {
		handlers.HandleError(rw, r, err)
		return
	}

	var res interface{}
	if sync {
		res = transaction.ToJSONResponse()
	} else {
		res = job.ToJSONResponse()
	}

	handlers.HandleJsonResponse(rw, http.StatusCreated, res)
}
