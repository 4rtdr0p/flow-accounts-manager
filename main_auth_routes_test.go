package main

import (
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/handlers"
	"github.com/gorilla/mux"
	"gopkg.in/yaml.v3"
)

func TestWalletAuthRulesMatchRegisteredRoutes(t *testing.T) {
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
			router := buildRouter(tc.opts, routeHandlers{
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
			})

			routeKeys, err := collectRouteKeys(router)
			if err != nil {
				t.Fatalf("collect route keys: %v", err)
			}

			ruleKeys := make(map[string]struct{}, len(walletAuthRules(tc.opts)))
			for _, rule := range walletAuthRules(tc.opts) {
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

func collectRouteKeys(router *mux.Router) (map[string]struct{}, error) {
	keys := map[string]struct{}{}
	err := router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		pathTemplate, err := route.GetPathTemplate()
		if err != nil {
			return nil
		}

		methods, err := route.GetMethods()
		if err != nil {
			return nil
		}

		for _, method := range methods {
			keys[method+" "+pathTemplate] = struct{}{}
		}
		return nil
	})
	return keys, err
}

func diffKeys(left map[string]struct{}, right map[string]struct{}) []string {
	var diff []string
	for key := range left {
		if _, ok := right[key]; !ok {
			diff = append(diff, key)
		}
	}
	sort.Strings(diff)
	return diff
}

func TestWalletAuthRuleMatchesVersionedPath(t *testing.T) {
	rule := handlers.NewAuthRule(http.MethodGet, "/{apiVersion}/accounts/{address}", "account.read")
	if !rule.PathPattern.MatchString("/v1/accounts/0xf8d6e0586b0a20c7") {
		t.Fatal("expected versioned path rule to match concrete request path")
	}
}

func TestWalletAuthRulesMatchOpenAPIRequiredScopes(t *testing.T) {
	openAPIScopes, err := collectOpenAPIRequiredScopes("openapi.yml")
	if err != nil {
		t.Fatalf("collect openapi scopes: %v", err)
	}

	ruleScopes := make(map[string]string, len(walletAuthRules(routeOptions{})))
	for _, rule := range walletAuthRules(routeOptions{}) {
		ruleScopes[rule.Key()] = rule.RequiredScope
	}

	missing := diffScopeMap(openAPIScopes, ruleScopes)
	extra := diffScopeMap(ruleScopes, openAPIScopes)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("openapi/auth rule scope mismatch\nmissing in code: %v\nextra in code: %v", missing, extra)
	}
}

func collectOpenAPIRequiredScopes(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	pathsNode := findMappingValue(&doc, "paths")
	if pathsNode == nil {
		return nil, nil
	}

	scopes := map[string]string{}
	for i := 0; i < len(pathsNode.Content); i += 2 {
		openAPIPath := pathsNode.Content[i].Value
		operationsNode := pathsNode.Content[i+1]
		for j := 0; j < len(operationsNode.Content); j += 2 {
			method := strings.ToUpper(operationsNode.Content[j].Value)
			if !isHTTPMethod(method) {
				continue
			}

			requiredScopes := findStringListField(operationsNode.Content[j+1], "x-required-scopes")
			if len(requiredScopes) == 0 {
				continue
			}

			pathTemplate := "/{apiVersion}" + openAPIPath
			key := method + " " + pathTemplate
			scopes[key] = requiredScopes[0]
		}
	}

	return scopes, nil
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
	sort.Strings(diff)
	return diff
}

func findStringListField(node *yaml.Node, field string) []string {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value != field {
			continue
		}

		valueNode := node.Content[i+1]
		if valueNode.Kind != yaml.SequenceNode {
			return nil
		}

		values := make([]string, 0, len(valueNode.Content))
		for _, item := range valueNode.Content {
			values = append(values, item.Value)
		}
		return values
	}

	return nil
}

func findMappingValue(node *yaml.Node, field string) *yaml.Node {
	if node == nil {
		return nil
	}

	current := node
	if current.Kind == yaml.DocumentNode && len(current.Content) > 0 {
		current = current.Content[0]
	}

	if current.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i < len(current.Content); i += 2 {
		if current.Content[i].Value == field {
			return current.Content[i+1]
		}
	}

	return nil
}

func isHTTPMethod(value string) bool {
	switch value {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions, http.MethodHead:
		return true
	default:
		return false
	}
}
