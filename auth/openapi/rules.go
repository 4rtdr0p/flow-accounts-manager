package openapi

import (
	"fmt"
	"sort"

	"github.com/flow-hydraulics/flow-wallet-api/handlers/middleware"
	"github.com/gorilla/mux"
)

// CollectRouteKeys returns registered mux route keys as "METHOD pathTemplate".
func CollectRouteKeys(router *mux.Router) (map[string]struct{}, error) {
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
			keys[routeKey(method, pathTemplate)] = struct{}{}
		}
		return nil
	})
	return keys, err
}

// AuthRulesFromRouter builds auth rules for each registered route using scopes from the OpenAPI index.
func AuthRulesFromRouter(router *mux.Router, index map[string]string) ([]middleware.AuthRule, error) {
	routeKeys, err := CollectRouteKeys(router)
	if err != nil {
		return nil, fmt.Errorf("collect route keys: %w", err)
	}

	rules := make([]middleware.AuthRule, 0, len(routeKeys))
	var missing []string

	for key := range routeKeys {
		scope, ok := index[key]
		if !ok {
			missing = append(missing, key)
			continue
		}

		method, pathTemplate, err := splitRouteKey(key)
		if err != nil {
			return nil, err
		}

		rules = append(rules, middleware.NewAuthRule(method, pathTemplate, scope))
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("openapi scope missing for routes: %v", missing)
	}

	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Key() < rules[j].Key()
	})

	return rules, nil
}

func splitRouteKey(key string) (method string, pathTemplate string, err error) {
	space := -1
	for i := 0; i < len(key); i++ {
		if key[i] == ' ' {
			space = i
			break
		}
	}
	if space <= 0 || space >= len(key)-1 {
		return "", "", fmt.Errorf("invalid route key %q", key)
	}
	return key[:space], key[space+1:], nil
}
