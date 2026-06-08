package openapi

import (
	"fmt"
	"net/http"
	"strings"

	"gopkg.in/yaml.v3"
)

const apiVersionPrefix = "/{apiVersion}"

// LoadScopeIndex parses an OpenAPI document and returns a map of route key to required scope.
// Route keys use the format "METHOD /{apiVersion}/path" matching mux path templates.
func LoadScopeIndex(spec []byte) (map[string]string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(spec, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal openapi spec: %w", err)
	}

	pathsNode := findMappingValue(&doc, "paths")
	if pathsNode == nil {
		return nil, fmt.Errorf("openapi spec missing paths")
	}

	scopes := make(map[string]string)
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
				return nil, fmt.Errorf("operation %s %s missing x-required-scopes", method, openAPIPath)
			}
			if len(requiredScopes) != 1 {
				return nil, fmt.Errorf("operation %s %s must have exactly one x-required-scopes entry", method, openAPIPath)
			}

			pathTemplate := apiVersionPrefix + openAPIPath
			key := routeKey(method, pathTemplate)
			if _, exists := scopes[key]; exists {
				return nil, fmt.Errorf("duplicate scope key %q", key)
			}
			scopes[key] = requiredScopes[0]
		}
	}

	if len(scopes) == 0 {
		return nil, fmt.Errorf("openapi spec has no operations with x-required-scopes")
	}

	return scopes, nil
}

func routeKey(method, pathTemplate string) string {
	return method + " " + pathTemplate
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
