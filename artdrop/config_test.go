package artdrop

import (
	"strings"
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/plugins"
)

// TestLoadConfigDefaultsMatchPreMigrationAddresses locks in the "leave the
// defaults as the current values so nothing breaks today" requirement: with
// no FLOW_WALLET_ARTDROP_* env vars set, LoadConfig must resolve to exactly
// the addresses that were previously hardcoded across the 22 .cdc files.
func TestLoadConfigDefaultsMatchPreMigrationAddresses(t *testing.T) {
	cfg := ParseTestConfig(t)

	if cfg.ArtDropCoreAddress != "0xec581a0282d99a1a" {
		t.Fatalf("ArtDropCoreAddress default changed: got %q", cfg.ArtDropCoreAddress)
	}
	if cfg.ArtDropRegistryAddress != "0xec581a0282d99a1a" {
		t.Fatalf("ArtDropRegistryAddress default changed: got %q", cfg.ArtDropRegistryAddress)
	}
	if cfg.EscrowModuleAddress != "0x1bfedfa0ec66c23e" {
		t.Fatalf("EscrowModuleAddress default changed: got %q", cfg.EscrowModuleAddress)
	}
	if cfg.PaymentModuleAddress != "0x1bfedfa0ec66c23e" {
		t.Fatalf("PaymentModuleAddress default changed: got %q", cfg.PaymentModuleAddress)
	}
	// LogicOwner has no envDefault of its own — it must fall back to
	// EscrowModuleAddress, since that's the account that actually hosts the
	// EscrowModule.IEscrowLogic capability every escrow transaction borrows.
	if cfg.LogicOwner != cfg.EscrowModuleAddress {
		t.Fatalf("expected LogicOwner to default to EscrowModuleAddress %q, got %q", cfg.EscrowModuleAddress, cfg.LogicOwner)
	}
}

// TestConfigNormalizeAndValidateRejectsMalformedAddresses covers "fail
// loudly on a missing or malformed address rather than silently submitting
// a script that imports from the zero address": every address field must
// be individually validated, and the zero address in particular must be
// rejected even though it's a syntactically well-formed 16-hex-char value.
func TestConfigNormalizeAndValidateRejectsMalformedAddresses(t *testing.T) {
	base := func() Config {
		return Config{
			ArtDropCoreAddress:     "0xec581a0282d99a1a",
			ArtDropRegistryAddress: "0xec581a0282d99a1a",
			EscrowModuleAddress:    "0x1bfedfa0ec66c23e",
			PaymentModuleAddress:   "0x1bfedfa0ec66c23e",
		}
	}

	tests := []struct {
		name   string
		modify func(*Config)
	}{
		{"empty ArtDropCoreAddress", func(c *Config) { c.ArtDropCoreAddress = "" }},
		{"empty ArtDropRegistryAddress", func(c *Config) { c.ArtDropRegistryAddress = "" }},
		{"empty EscrowModuleAddress", func(c *Config) { c.EscrowModuleAddress = "" }},
		{"empty PaymentModuleAddress", func(c *Config) { c.PaymentModuleAddress = "" }},
		{"zero ArtDropCoreAddress", func(c *Config) { c.ArtDropCoreAddress = "0x0000000000000000" }},
		{"too-short address", func(c *Config) { c.ArtDropCoreAddress = "0x1234" }},
		{"non-hex address", func(c *Config) { c.ArtDropCoreAddress = "0xzzzzzzzzzzzzzzzz" }},
		{"explicit zero LogicOwner override", func(c *Config) { c.LogicOwner = "0x0000000000000000" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base()
			tt.modify(&cfg)
			if err := cfg.normalizeAndValidate(); err == nil {
				t.Fatalf("expected normalizeAndValidate to reject config, got nil error (cfg: %+v)", cfg)
			}
		})
	}
}

// TestConfigNormalizeAndValidateCanonicalizesAddresses confirms the "0x"
// prefix and casing get normalized, and that NewService (which reuses this
// validation) would refuse to build a Service around an invalid config
// rather than substituting a malformed address into a live script.
func TestConfigNormalizeAndValidateCanonicalizesAddresses(t *testing.T) {
	cfg := Config{
		ArtDropCoreAddress:     "EC581A0282D99A1A", // no "0x", uppercase
		ArtDropRegistryAddress: "0xec581a0282d99a1a",
		EscrowModuleAddress:    "0x1bfedfa0ec66c23e",
		PaymentModuleAddress:   "0x1bfedfa0ec66c23e",
	}

	if err := cfg.normalizeAndValidate(); err != nil {
		t.Fatalf("normalizeAndValidate returned error: %v", err)
	}
	if cfg.ArtDropCoreAddress != "0xec581a0282d99a1a" {
		t.Fatalf("expected canonicalized address, got %q", cfg.ArtDropCoreAddress)
	}
}

// TestNewServiceRejectsInvalidConfig confirms the failure surfaces where a
// caller would actually see it: NewService (and therefore NewPlugin, called
// from main() at startup) must return an error instead of silently building
// a Service with a bad address baked into its scripts.
func TestNewServiceRejectsInvalidConfig(t *testing.T) {
	if _, err := NewService(plugins.PluginDeps{}, nil); err == nil {
		t.Fatal("expected NewService to reject a nil config")
	}

	bad := &Config{
		ArtDropCoreAddress:     "",
		ArtDropRegistryAddress: "0xec581a0282d99a1a",
		EscrowModuleAddress:    "0x1bfedfa0ec66c23e",
		PaymentModuleAddress:   "0x1bfedfa0ec66c23e",
	}
	svc, err := NewService(plugins.PluginDeps{}, bad)
	if err == nil {
		t.Fatal("expected NewService to reject a config with a missing address")
	}
	if svc != nil {
		t.Fatal("expected NewService to return a nil Service alongside the error")
	}
	if !strings.Contains(err.Error(), "ARTDROP_CORE_ADDRESS") {
		t.Fatalf("expected error to name the offending field, got: %v", err)
	}
}
