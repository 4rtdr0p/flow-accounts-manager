package openapi

import (
	"net/http"
	"testing"

	"github.com/gorilla/mux"
)

func TestAuthRulesFromRouter(t *testing.T) {
	index := map[string]string{
		"GET /{apiVersion}/health/ready": "health.read",
		"GET /{apiVersion}/accounts":     "account.read",
	}

	r := mux.NewRouter()
	rv := r.PathPrefix("/{apiVersion}").Subrouter()
	rv.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {}).Methods(http.MethodGet)
	rv.HandleFunc("/accounts", func(w http.ResponseWriter, r *http.Request) {}).Methods(http.MethodGet)

	rules, err := AuthRulesFromRouter(r, index)
	if err != nil {
		t.Fatalf("AuthRulesFromRouter: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
}

func TestAuthRulesFromRouterMissingScope(t *testing.T) {
	index := map[string]string{
		"GET /{apiVersion}/health/ready": "health.read",
	}

	r := mux.NewRouter()
	rv := r.PathPrefix("/{apiVersion}").Subrouter()
	rv.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {}).Methods(http.MethodGet)
	rv.HandleFunc("/accounts", func(w http.ResponseWriter, r *http.Request) {}).Methods(http.MethodGet)

	_, err := AuthRulesFromRouter(r, index)
	if err == nil {
		t.Fatal("expected error for missing openapi scope")
	}
}
