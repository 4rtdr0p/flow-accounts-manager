package artdrop

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/caarlos0/env/v6"
	"github.com/flow-hydraulics/flow-wallet-api/artdrop/ixkio"
	"github.com/flow-hydraulics/flow-wallet-api/artdrop/purchase"
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
	// This value is server-controlled and is never taken from client request
	// bodies — CreateEscrowRequest, ActivateChipRequest and
	// EscrowActionRequest have no LogicOwner field at all.
	LogicOwner string `env:"ARTDROP_LOGIC_OWNER_ADDRESS"`

	// ArtDropCoreAddress is the account ArtDropCore is deployed to.
	ArtDropCoreAddress string `env:"ARTDROP_CORE_ADDRESS,notEmpty" envDefault:"0xd97d6774544fcd9c"`

	// ArtDropRegistryAddress is the account ArtDropRegistry is deployed to.
	ArtDropRegistryAddress string `env:"ARTDROP_REGISTRY_ADDRESS,notEmpty" envDefault:"0xd97d6774544fcd9c"`

	// EscrowModuleAddress is the account EscrowModule is deployed to.
	EscrowModuleAddress string `env:"ARTDROP_ESCROW_MODULE_ADDRESS,notEmpty" envDefault:"0x2edba2d63af095b8"`

	// PaymentModuleAddress is the account PaymentModule is deployed to.
	PaymentModuleAddress string `env:"ARTDROP_PAYMENT_MODULE_ADDRESS,notEmpty" envDefault:"0x2edba2d63af095b8"`

	// FungibleTokenAddress, NonFungibleTokenAddress and MetadataViewsAddress
	// are the accounts the *standard* Flow contracts live on for this chain.
	// Unlike the ArtDrop contracts above, these are the same well-known system
	// contracts on any given network — but their address still differs per
	// network (e.g. FungibleToken is 0x9a0766d93b6608b7 on testnet but
	// 0xee82856bf20e2aa6 on the emulator). Several write-path scripts
	// (create_escrow.cdc, re_escrow.cdc: FungibleToken; register_provider.cdc:
	// NonFungibleToken) and read scripts (get_certificate_detail.cdc:
	// MetadataViews) hardcode the testnet address in their import lines, so
	// substituteAddresses now rewrites these too (see cdc.go). Defaults are the
	// testnet addresses that were previously hardcoded, so production behavior
	// is unchanged; the emulator E2E setup overrides them via env.
	FungibleTokenAddress string `env:"ARTDROP_FUNGIBLE_TOKEN_ADDRESS,notEmpty" envDefault:"0x9a0766d93b6608b7"`

	// NonFungibleTokenAddress — see FungibleTokenAddress.
	NonFungibleTokenAddress string `env:"ARTDROP_NON_FUNGIBLE_TOKEN_ADDRESS,notEmpty" envDefault:"0x631e88ae7f1d7c20"`

	// MetadataViewsAddress — see FungibleTokenAddress.
	MetadataViewsAddress string `env:"ARTDROP_METADATA_VIEWS_ADDRESS,notEmpty" envDefault:"0x631e88ae7f1d7c20"`

	// EscrowMaxAmountFlow is the server-side anti-garbage ceiling (issue #105,
	// re-scoped in #107) on the `amount` (whole FLOW, not centiFLOW) the raw
	// ReEscrow ops path is allowed to lock, enforced in Service.ReEscrow.
	//
	// As of #107 CreateEscrow no longer enforces this: it is unreachable over
	// HTTP (its raw route was removed in #105) and its only caller is the
	// purchase flow (#93), which computes `amount` server-side from the Mongo
	// artwork price + platform fee + Pyth FLOW/USD — a trusted value that a
	// cap only got in the way of. The live buyer re-escrow path (#107) routes
	// through that same purchase flow, so the ONLY untrusted `amount` left is
	// the raw ops /re-escrow endpoint (behind account.artdrop.escrow.reescrow).
	//
	// IMPORTANT: this is a pure anti-overflow/anti-garbage guard, not a pricing
	// control. `amount` is the escrow's FLOW gas reserve (the ~5% platform fee
	// converted via Pyth), never the artwork's sale price. FLOW's USD price can
	// be very low (e.g. ~$0.004), which inflates the FLOW reserve for a given
	// USD fee: at $0.004/FLOW a $50k artwork's 5% fee ($2.5k) already reserves
	// ~625000 FLOW, so the previous 500000 ceiling rejected legitimate large
	// artworks flowing through the server-computed purchase path. The default
	// is now 50000000 FLOW — well above any plausible server-computed reserve
	// while still catching an absurd/malformed/overflow amount on the raw ops
	// path. Tunable per deployment; the purchase flow's server-computed amount
	// remains the real guarantee.
	EscrowMaxAmountFlow float64 `env:"ARTDROP_ESCROW_MAX_AMOUNT_FLOW" envDefault:"50000000"`

	// EscrowProjectionResync, when true, runs a one-time on-boot
	// reconciliation of the escrows projection's terminal state against
	// on-chain state (issue #109) — see Service.ResyncEscrowProjection. It
	// re-reads on-chain escrow summaries and updates the status (and other
	// terminal fields) of EXISTING projected rows only; it never inserts
	// (backfill owns seeding). Its purpose is to repair drift the live
	// listener missed, chiefly escrows voided via EscrowVoided before the
	// projection subscribed to that event (added in #109).
	//
	// Off by default. Operational usage: set
	// FLOW_WALLET_ARTDROP_ESCROW_PROJECTION_RESYNC=true for ONE boot after
	// deploying the EscrowVoided handler, confirm the "escrow projection:
	// resync complete" log line, then unset it — leaving it on just re-reads
	// the chain and writes nothing new on every subsequent boot. Gated
	// together with the backfill on DISABLE_CHAIN_EVENTS (see plugin.go): with
	// no listener there is no live projection to keep reconciled.
	EscrowProjectionResync bool `env:"ARTDROP_ESCROW_PROJECTION_RESYNC" envDefault:"false"`

	// EscrowClaimWindowSeconds is the buyer's on-chain claim deadline (issue
	// #111), server-computed in the purchase flow as now + this window rather
	// than trusted from the client request. unlock_at is the point after which
	// releaseOnTimeout becomes permissionless and yanks the escrow reserve to
	// the ArtDrop vault — a client-set past/zero unlock_at would close the
	// buyer's claim window before they ever activate their chip, so like
	// EscrowMaxAmountFlow (#105/#107) it is now a server-controlled setting,
	// not a request field, for the purchase flow (#93). Raw ops endpoints
	// (CreateEscrowRequest/ReEscrowRequest) are unaffected: they remain
	// OperationalAdmin-scoped, trusted tooling with a client-supplied
	// UnlockAt, the same scoping decision #107 made for amount. Default is
	// 604800 seconds (7 days).
	EscrowClaimWindowSeconds float64 `env:"ARTDROP_ESCROW_CLAIM_WINDOW_SECONDS" envDefault:"604800"`

	// IxkioAPIURL is the Ixkio Flex API "API mode" tap-verification endpoint
	// (design doc CHIP-SIGNING-DESIGN.md §4a). It is NOT the front's
	// redirect-mode endpoint (payload-galaxy-front's auth-bridge.ts) — this is
	// a separate, unrelated tap flow (design doc §3.4).
	IxkioAPIURL string `env:"ARTDROP_IXKIO_API_URL" envDefault:"https://api.ixkio.com/v1/t"`

	// IxkioResponseToken is Ixkio's "r" API-mode response token for the
	// wallet-api's own Ixkio account. It is NOT the same token as the front's
	// redirect-mode IXKIO_RESPONSE_API_TOKEN — that token is provisioned for
	// a different Ixkio mode and must not be reused here (design doc §4a).
	// Empty is allowed: the client omits "r" from the request rather than
	// failing to start, so a deployment can come up before Ixkio provisions
	// the token; real Verify calls will fail against Ixkio's own API until
	// it's set.
	IxkioResponseToken string `env:"ARTDROP_IXKIO_RESPONSE_TOKEN" envDefault:""`

	// IxkioEnabled selects between the real Ixkio client and the
	// TESTNET-ONLY bypass (ixkio.BypassVerifier), which makes every tap
	// "Pass" without ever calling Ixkio. Defaults to false (bypass) so the
	// create-escrow -> activate -> settle flow can be tested end to end with
	// no physical chip and no live Ixkio dependency (design doc §0, §8 layer
	// 4). MUST be true in production — see NewIxkioVerifier and
	// ixkio.BypassVerifier's doc comment.
	IxkioEnabled bool `env:"ARTDROP_IXKIO_ENABLED" envDefault:"false"`

	// -- FLOW/USD pool oracle (issue #121) --
	// These configure the PoolPriceOracle that reads FLOW/USD from Flow EVM
	// pools (replacing Pyth). See docs/POOL-ORACLE-PLAN.md and
	// artdrop/purchase/pooloracle.go. Every field has a working default, so the
	// oracle runs with no configuration at all.

	// FlowEVMRPCURL is the Flow EVM JSON-RPC endpoint(s) the oracle reads pools
	// from. It is ALWAYS mainnet even when the wallet itself runs on testnet —
	// FLOW's USD price is a mainnet fact. A comma-separated list enables
	// failover (URLs are tried in order per read).
	FlowEVMRPCURL string `env:"ARTDROP_FLOW_EVM_RPC_URL" envDefault:"https://mainnet.evm.nodes.onflow.org"`

	// OraclePools overrides the hardcoded default pool set. Empty uses the five
	// verified pools (purchase.DefaultPools). Format: a CSV of
	// "addr:v2|v3:wflowIsToken0:stableDec" entries.
	OraclePools string `env:"ARTDROP_ORACLE_POOLS" envDefault:""`

	// OracleTTL is how long a read is cached before the next refresh.
	OracleTTL time.Duration `env:"ARTDROP_ORACLE_TTL" envDefault:"45s"`

	// OracleMaxDeviationBps rejects a pool whose spot deviates more than this
	// (in basis points) from the median of all read pools. Default 200 = 2%.
	OracleMaxDeviationBps int `env:"ARTDROP_ORACLE_MAX_DEVIATION_BPS" envDefault:"200"`

	// OracleMinPools is the minimum number of pools that must be read
	// successfully, or the refresh fails. Default 3.
	OracleMinPools int `env:"ARTDROP_ORACLE_MIN_POOLS" envDefault:"3"`

	// OracleMinSurvivors is the minimum number of pools that must survive
	// outlier rejection, or the refresh fails. Default 2.
	OracleMinSurvivors int `env:"ARTDROP_ORACLE_MIN_SURVIVORS" envDefault:"2"`

	// OracleSanityMinUSD / OracleSanityMaxUSD bound the accepted final price;
	// outside the band the refresh fails rather than locking a wrong amount.
	OracleSanityMinUSD float64 `env:"ARTDROP_ORACLE_SANITY_MIN_USD" envDefault:"0.0005"`
	OracleSanityMaxUSD float64 `env:"ARTDROP_ORACLE_SANITY_MAX_USD" envDefault:"2.0"`

	// OracleRPCTimeout is the per-request HTTP timeout for a pool read batch.
	OracleRPCTimeout time.Duration `env:"ARTDROP_ORACLE_RPC_TIMEOUT" envDefault:"10s"`

	// OraclePoolsParsed is the resolved pool set (from OraclePools or the
	// default), populated by normalizeAndValidate so a malformed OraclePools
	// override fails loudly at startup rather than in RegisterRoutes. It is not
	// an env field.
	OraclePoolsParsed []purchase.PoolConfig `env:"-"`
}

// NewIxkioVerifier builds the ixkio.Verifier this Config selects: the real
// Ixkio client when IxkioEnabled is true, or the TESTNET-ONLY bypass when
// false. This is the constructor future consumers (escrow activation, design
// doc §3.4/§5) call to get a verifier without caring which mode a deployment
// is in.
func (c *Config) NewIxkioVerifier() ixkio.Verifier {
	return ixkio.NewVerifier(c.IxkioEnabled, c.IxkioAPIURL, c.IxkioResponseToken, http.DefaultClient)
}

// defaultEscrowMaxAmountFlow mirrors the envDefault above and is the fallback
// normalizeAndValidate applies when EscrowMaxAmountFlow is left at its zero
// value — e.g. a Config literal built directly (bypassing env.Parse's own
// envDefault handling), the same situation LogicOwner's empty-string fallback
// below handles for the address fields.
const defaultEscrowMaxAmountFlow = 50000000

// defaultEscrowClaimWindowSeconds mirrors the envDefault above and is the
// fallback normalizeAndValidate applies when EscrowClaimWindowSeconds is left
// at its zero value, same rationale as defaultEscrowMaxAmountFlow.
const defaultEscrowClaimWindowSeconds = 604800

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

// normalizeAndValidate defaults LogicOwner from EscrowModuleAddress,
// EscrowMaxAmountFlow to defaultEscrowMaxAmountFlow, and
// EscrowClaimWindowSeconds to defaultEscrowClaimWindowSeconds when unset,
// then validates and canonicalizes every address field in place.
func (c *Config) normalizeAndValidate() error {
	if c.LogicOwner == "" {
		c.LogicOwner = c.EscrowModuleAddress
	}
	if c.EscrowMaxAmountFlow <= 0 {
		c.EscrowMaxAmountFlow = defaultEscrowMaxAmountFlow
	}
	if c.EscrowClaimWindowSeconds <= 0 {
		c.EscrowClaimWindowSeconds = defaultEscrowClaimWindowSeconds
	}

	// Oracle knobs: default the zero values a Config literal (bypassing
	// env.Parse's envDefault) would leave, then validate the invariants.
	if c.FlowEVMRPCURL == "" {
		c.FlowEVMRPCURL = "https://mainnet.evm.nodes.onflow.org"
	}
	if c.OracleTTL <= 0 {
		c.OracleTTL = 45 * time.Second
	}
	if c.OracleMaxDeviationBps <= 0 {
		c.OracleMaxDeviationBps = 200
	}
	if c.OracleMinPools <= 0 {
		c.OracleMinPools = 3
	}
	if c.OracleMinSurvivors <= 0 {
		c.OracleMinSurvivors = 2
	}
	if c.OracleSanityMinUSD <= 0 {
		c.OracleSanityMinUSD = 0.0005
	}
	if c.OracleSanityMaxUSD <= 0 {
		c.OracleSanityMaxUSD = 2.0
	}
	if c.OracleRPCTimeout <= 0 {
		c.OracleRPCTimeout = 10 * time.Second
	}
	if c.OracleSanityMinUSD >= c.OracleSanityMaxUSD {
		return fmt.Errorf("ARTDROP_ORACLE_SANITY_MIN_USD (%g) must be < ARTDROP_ORACLE_SANITY_MAX_USD (%g)", c.OracleSanityMinUSD, c.OracleSanityMaxUSD)
	}
	if c.OracleMinSurvivors > c.OracleMinPools {
		return fmt.Errorf("ARTDROP_ORACLE_MIN_SURVIVORS (%d) must be <= ARTDROP_ORACLE_MIN_POOLS (%d)", c.OracleMinSurvivors, c.OracleMinPools)
	}
	pools, err := purchase.ParsePools(c.OraclePools)
	if err != nil {
		return fmt.Errorf("ARTDROP_ORACLE_POOLS: %w", err)
	}
	c.OraclePoolsParsed = pools

	fields := []struct {
		name  string
		value *string
	}{
		{"ARTDROP_LOGIC_OWNER_ADDRESS", &c.LogicOwner},
		{"ARTDROP_CORE_ADDRESS", &c.ArtDropCoreAddress},
		{"ARTDROP_REGISTRY_ADDRESS", &c.ArtDropRegistryAddress},
		{"ARTDROP_ESCROW_MODULE_ADDRESS", &c.EscrowModuleAddress},
		{"ARTDROP_PAYMENT_MODULE_ADDRESS", &c.PaymentModuleAddress},
		{"ARTDROP_FUNGIBLE_TOKEN_ADDRESS", &c.FungibleTokenAddress},
		{"ARTDROP_NON_FUNGIBLE_TOKEN_ADDRESS", &c.NonFungibleTokenAddress},
		{"ARTDROP_METADATA_VIEWS_ADDRESS", &c.MetadataViewsAddress},
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
