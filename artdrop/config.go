package artdrop

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/caarlos0/env/v6"
	"github.com/flow-hydraulics/flow-wallet-api/flow_helpers"
	"github.com/onflow/flow-go-sdk"
)

// Config holds artdrop-specific configuration settings: the accounts the
// ArtDrop protocol contracts are deployed to. These addresses used to be
// hardcoded into the import lines of 22 embedded .cdc files; LoadConfig
// reads them from FLOW_WALLET_-prefixed environment variables (the same
// convention as configs.Parse — see configs/configs.go) and Service
// substitutes them into the embedded scripts at construction time (see
// cdc.go). Redeploying the protocol to new addresses is now an env var
// change instead of a 22-file edit.
type Config struct {
	// LogicOwner is the Flow address hosting the EscrowModule.IEscrowLogic
	// capability that every escrow transaction borrows (create/activate/
	// release/cancel/refund escrow — see artdrop/cdc/*_escrow.cdc). In this
	// deployment it is always the same account EscrowModule itself is
	// deployed to, so it defaults to EscrowModuleAddress when left unset;
	// it exists as its own setting only so it could be pointed elsewhere
	// without a code change if that ever stopped being true.
	//
	// This value is server-controlled and must never be taken from client
	// request bodies: CreateEscrowRequest.LogicOwner, ActivateChipRequest.
	// LogicOwner and EscrowActionRequest.LogicOwner are still accepted for
	// backwards compatibility with in-flight callers, but their values are
	// now ignored in favor of this config.
	LogicOwner string `env:"ARTDROP_LOGIC_OWNER_ADDRESS"`

	// ArtDropCoreAddress is the account ArtDropCore is deployed to.
	ArtDropCoreAddress string `env:"ARTDROP_CORE_ADDRESS,notEmpty" envDefault:"0xec581a0282d99a1a"`

	// ArtDropRegistryAddress is the account ArtDropRegistry is deployed to.
	ArtDropRegistryAddress string `env:"ARTDROP_REGISTRY_ADDRESS,notEmpty" envDefault:"0xec581a0282d99a1a"`

	// EscrowModuleAddress is the account EscrowModule is deployed to.
	EscrowModuleAddress string `env:"ARTDROP_ESCROW_MODULE_ADDRESS,notEmpty" envDefault:"0x1bfedfa0ec66c23e"`

	// PaymentModuleAddress is the account PaymentModule is deployed to.
	PaymentModuleAddress string `env:"ARTDROP_PAYMENT_MODULE_ADDRESS,notEmpty" envDefault:"0x1bfedfa0ec66c23e"`
}

// LoadConfig parses the artdrop plugin's contract-address configuration from
// the environment (FLOW_WALLET_ prefix, matching configs.Parse) and
// validates every address up front, so a bad deploy fails loudly at startup
// instead of silently submitting transactions that import from a malformed
// or zero address. The defaults match the addresses that were previously
// hardcoded in the .cdc files, so an operator who sets none of the new env
// vars gets today's behavior unchanged.
func LoadConfig() (*Config, error) {
	cfg := Config{}
	if err := env.Parse(&cfg, env.Options{Prefix: "FLOW_WALLET_"}); err != nil {
		return nil, fmt.Errorf("parse artdrop config: %w", err)
	}
	if err := cfg.normalizeAndValidate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ParseTestConfig returns a validated Config for tests, following the same
// pattern as configs.ParseTestConfig: it parses whatever FLOW_WALLET_
// ARTDROP_* environment variables are already set (normally none, so every
// field falls back to its envDefault) and fails the test immediately if the
// result doesn't validate.
func ParseTestConfig(t *testing.T) *Config {
	t.Helper()

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// normalizeAndValidate defaults LogicOwner from EscrowModuleAddress when
// unset, then validates and canonicalizes every address field in place.
func (c *Config) normalizeAndValidate() error {
	if c.LogicOwner == "" {
		c.LogicOwner = c.EscrowModuleAddress
	}

	fields := []struct {
		name  string
		value *string
	}{
		{"ARTDROP_LOGIC_OWNER_ADDRESS", &c.LogicOwner},
		{"ARTDROP_CORE_ADDRESS", &c.ArtDropCoreAddress},
		{"ARTDROP_REGISTRY_ADDRESS", &c.ArtDropRegistryAddress},
		{"ARTDROP_ESCROW_MODULE_ADDRESS", &c.EscrowModuleAddress},
		{"ARTDROP_PAYMENT_MODULE_ADDRESS", &c.PaymentModuleAddress},
	}
	for _, f := range fields {
		normalized, err := validateContractAddress(*f.value)
		if err != nil {
			return fmt.Errorf("%s: %w", f.name, err)
		}
		*f.value = normalized
	}
	return nil
}

// validateContractAddress checks that value is a well-formed, non-zero Flow
// address and returns it in canonical "0x"+16-lowercase-hex-chars form.
//
// It deliberately does not check the address against a chain ID's checksum
// the way flow_helpers.ValidateAddress does for request-supplied addresses:
// these are contract addresses substituted into Cadence import lines, not
// wallet-api account addresses being used as transaction signers/args, and
// tying their validity to the wallet-api's own configured ChainID breaks
// local/emulator runs — the real testnet contract addresses this repo
// defaults to are not valid "flow-emulator" addresses, so emulator-mode
// tests and dev runs would fail to start for a reason that has nothing to
// do with their actual configuration.
func validateContractAddress(value string) (string, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(value), "0x")
	if len(trimmed) != 16 {
		return "", fmt.Errorf("%q is not a valid Flow address (want 16 hex chars, got %d)", value, len(trimmed))
	}
	if _, err := hex.DecodeString(trimmed); err != nil {
		return "", fmt.Errorf("%q is not valid hex: %w", value, err)
	}
	addr := flow.HexToAddress(trimmed)
	if addr == flow.EmptyAddress {
		return "", fmt.Errorf("%q is the zero address", value)
	}
	return flow_helpers.FormatAddress(addr), nil
}
