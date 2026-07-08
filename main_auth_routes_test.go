package main

import (
	"net/http"
	"os"
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/artdrop"
	"github.com/flow-hydraulics/flow-wallet-api/auth/openapi"
	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/gorilla/mux"
)

func TestWalletAuthRulesMatchRegisteredRoutes(t *testing.T) {
	spec, err := os.ReadFile("openapi.yml")
	if err != nil {
		t.Fatalf("read openapi.yml: %v", err)
	}
	scopeIndex, err := openapi.LoadScopeIndex(spec)
	if err != nil {
		t.Fatalf("LoadScopeIndex: %v", err)
	}

	testCases := []struct {
		name string
		opts routeOptions
	}{
		{name: "all features enabled"},
		{name: "raw transactions disabled", opts: routeOptions{DisableRawTransactions: true}},
		{name: "fungible tokens disabled", opts: routeOptions{DisableFungibleTokens: true}},
		{name: "non fungible tokens disabled", opts: routeOptions{DisableNonFungibleTokens: true}},
		{name: "all optional route groups disabled", opts: routeOptions{
			DisableRawTransactions:   true,
			DisableFungibleTokens:    true,
			DisableNonFungibleTokens: true,
		}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			deps := plugins.PluginDeps{}
			router := buildRouter(tc.opts, routeHandlers{
				System:           handlers.NewSystem(nil),
				Templates:        handlers.NewTemplates(nil),
				Jobs:             handlers.NewJobs(nil),
				Accounts:         handlers.NewAccounts(nil),
				Transactions:     handlers.NewTransactions(nil, nil),
				Tokens:           handlers.NewTokens(nil),
				Ops:              handlers.NewOps(nil),
				DebugURL:         "debug-url",
				DebugSHA:         "debug-sha",
				DebugBuildTime:   "debug-build-time",
				WorkerPoolStatus: func() (interface{}, error) { return nil, nil },
			}, registerPlugins(nil, deps), deps)

			rules, err := openapi.AuthRulesFromRouter(router, scopeIndex)
			if err != nil {
				t.Fatalf("AuthRulesFromRouter: %v", err)
			}

			routeKeys, err := openapi.CollectRouteKeys(router)
			if err != nil {
				t.Fatalf("collect route keys: %v", err)
			}

			ruleKeys := make(map[string]struct{}, len(rules))
			for _, rule := range rules {
				ruleKeys[rule.Key()] = struct{}{}
			}

			missing := diffKeys(routeKeys, ruleKeys)
			extra := diffKeys(ruleKeys, routeKeys)
			if len(missing) > 0 || len(extra) > 0 {
				t.Fatalf("route/auth rule parity mismatch\nmissing rules: %v\nextra rules: %v", missing, extra)
			}
		})
	}
}

func diffKeys(left map[string]struct{}, right map[string]struct{}) []string {
	var diff []string
	for key := range left {
		if _, ok := right[key]; !ok {
			diff = append(diff, key)
		}
	}
	return diff
}

func TestWalletAuthRuleMatchesVersionedPath(t *testing.T) {
	rule := handlers.NewAuthRule(http.MethodGet, "/{apiVersion}/accounts/{address}", "account.read")
	if !rule.PathPattern.MatchString("/v1/accounts/0xf8d6e0586b0a20c7") {
		t.Fatal("expected versioned path rule to match concrete request path")
	}
}

func TestOpenAPIScopeIndexCoversFullRouter(t *testing.T) {
	spec, err := os.ReadFile("openapi.yml")
	if err != nil {
		t.Fatalf("read openapi.yml: %v", err)
	}
	scopeIndex, err := openapi.LoadScopeIndex(spec)
	if err != nil {
		t.Fatalf("LoadScopeIndex: %v", err)
	}

	deps := plugins.PluginDeps{}
	router := buildRouter(routeOptions{}, routeHandlers{
		System:           handlers.NewSystem(nil),
		Templates:        handlers.NewTemplates(nil),
		Jobs:             handlers.NewJobs(nil),
		Accounts:         handlers.NewAccounts(nil),
		Transactions:     handlers.NewTransactions(nil, nil),
		Tokens:           handlers.NewTokens(nil),
		Ops:              handlers.NewOps(nil),
		DebugURL:         "debug-url",
		DebugSHA:         "debug-sha",
		DebugBuildTime:   "debug-build-time",
		WorkerPoolStatus: func() (interface{}, error) { return nil, nil },
	}, registerPlugins(nil, deps), deps)

	rules, err := openapi.AuthRulesFromRouter(router, scopeIndex)
	if err != nil {
		t.Fatalf("AuthRulesFromRouter: %v", err)
	}

	ruleScopes := make(map[string]string, len(rules))
	for _, rule := range rules {
		ruleScopes[rule.Key()] = rule.RequiredScope
	}

	missing := diffScopeMap(scopeIndex, ruleScopes)
	extra := diffScopeMap(ruleScopes, scopeIndex)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("openapi/runtime scope mismatch\nmissing in runtime rules: %v\nextra in runtime rules: %v", missing, extra)
	}
}

func TestTransferRouteBelongsToArtDropPlugin(t *testing.T) {
	handlers := routeHandlers{
		System:           handlers.NewSystem(nil),
		Templates:        handlers.NewTemplates(nil),
		Jobs:             handlers.NewJobs(nil),
		Accounts:         handlers.NewAccounts(nil),
		Transactions:     handlers.NewTransactions(nil),
		Tokens:           handlers.NewTokens(nil),
		Ops:              handlers.NewOps(nil),
		DebugURL:         "debug-url",
		DebugSHA:         "debug-sha",
		DebugBuildTime:   "debug-build-time",
		WorkerPoolStatus: func() (interface{}, error) { return nil, nil },
	}

	coreRouter := buildRouter(routeOptions{}, handlers, nil, plugins.PluginDeps{})
	if routeExists(t, coreRouter, http.MethodPost, "/v1/accounts/0xf8d6e0586b0a20c7/transfer") {
		t.Fatal("expected /transfer to be registered by the artdrop plugin, not the core router")
	}

	spec, err := os.ReadFile("openapi.yml")
	if err != nil {
		t.Fatalf("read openapi.yml: %v", err)
	}
	scopeIndex, err := openapi.LoadScopeIndex(spec)
	if err != nil {
		t.Fatalf("LoadScopeIndex: %v", err)
	}

	deps := plugins.PluginDeps{}
	pluginRouter := buildRouter(routeOptions{}, handlers, []plugins.Plugin{artdrop.NewPlugin(deps)}, deps)
	if !routeExists(t, pluginRouter, http.MethodPost, "/v1/accounts/0xf8d6e0586b0a20c7/transfer") {
		t.Fatal("expected artdrop plugin to register POST /accounts/{address}/transfer")
	}

	rules, err := openapi.AuthRulesFromRouter(pluginRouter, scopeIndex)
	if err != nil {
		t.Fatalf("AuthRulesFromRouter: %v", err)
	}
	for _, rule := range rules {
		if rule.Key() == "POST /{apiVersion}/accounts/{address}/transfer" && rule.RequiredScope == "account.transfer" {
			return
		}
	}
	t.Fatal("expected artdrop transfer route to require account.transfer")
}

func routeExists(t *testing.T, router *mux.Router, method string, path string) bool {
	t.Helper()

	var match mux.RouteMatch
	req, err := http.NewRequest(method, path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return router.Match(req, &match)
}

func diffScopeMap(left map[string]string, right map[string]string) []string {
	var diff []string
	for key, leftScope := range left {
		rightScope, ok := right[key]
		if !ok {
			diff = append(diff, key+" (missing)")
			continue
		}
		if leftScope != rightScope {
			diff = append(diff, key+" ("+leftScope+" != "+rightScope+")")
		}
	}
	return diff
}
