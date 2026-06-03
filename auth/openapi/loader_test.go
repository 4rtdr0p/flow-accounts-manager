package openapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadScopeIndex(t *testing.T) {
	spec := []byte(`
paths:
  /health/ready:
    get:
      x-required-scopes:
        - health.read
  /accounts:
    get:
      x-required-scopes:
        - account.read
    post:
      x-required-scopes:
        - account.create
`)

	index, err := LoadScopeIndex(spec)
	if err != nil {
		t.Fatalf("LoadScopeIndex: %v", err)
	}

	if index["GET /{apiVersion}/health/ready"] != "health.read" {
		t.Fatalf("unexpected health scope: %#v", index)
	}
	if index["POST /{apiVersion}/accounts"] != "account.create" {
		t.Fatalf("unexpected create scope: %#v", index)
	}
}

func TestLoadScopeIndexRejectsMissingScope(t *testing.T) {
	spec := []byte(`
paths:
  /accounts:
    get:
      summary: no scope
`)

	_, err := LoadScopeIndex(spec)
	if err == nil || !strings.Contains(err.Error(), "x-required-scopes") {
		t.Fatalf("expected missing scope error, got %v", err)
	}
}

func TestLoadScopeIndexFromRepoOpenAPI(t *testing.T) {
	specPath := filepath.Join("..", "..", "openapi.yml")
	spec, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read openapi.yml: %v", err)
	}

	index, err := LoadScopeIndex(spec)
	if err != nil {
		t.Fatalf("LoadScopeIndex: %v", err)
	}

	if len(index) < 20 {
		t.Fatalf("expected many scoped operations, got %d", len(index))
	}
}
