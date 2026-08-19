package artdrop

import (
	"regexp"
	"strings"
	"testing"
)

// embeddedCDCScripts returns every .cdc source actually go:embed'd into the
// binary (see the //go:embed directives in service.go), keyed by filename.
// Shared by tests that need to walk the full embedded set.
func embeddedCDCScripts() map[string]string {
	return map[string]string{
		"setup_collection.cdc":              setupCollectionCDC,
		"register_provider.cdc":             registerProviderCDC,
		"get_certificate_detail.cdc":        getCertificateDetailCDC,
		"get_certificates.cdc":              getCertificatesCDC,
		"get_escrow_summary.cdc":            getEscrowSummaryCDC,
		"create_escrow.cdc":                 createEscrowCDC,
		"activate_chip_and_settle.cdc":      activateChipAndSettleCDC,
		"get_original_extended_summary.cdc": getOriginalExtendedSummaryCDC,
		"get_edition_summary.cdc":           getEditionSummaryCDC,
		"get_edition_ids_by_original.cdc":   getEditionIDsByOriginalCDC,
		"get_platform_fee.cdc":              getPlatformFeeCDC,
		"get_market_mode_name.cdc":          getMarketModeNameCDC,
		"is_artist.cdc":                     isArtistCDC,
		"onboard_artist.cdc":                onboardArtistCDC,
		"setup_artist_direct_claim.cdc":     setupArtistDirectClaimCDC,
		"create_original.cdc":               createOriginalCDC,
		"create_edition.cdc":                createEditionCDC,
	}
}

func testAddressConfig() Config {
	return Config{
		LogicOwner:             "0x0000000000000001",
		ArtDropCoreAddress:     "0x0000000000000002",
		ArtDropRegistryAddress: "0x0000000000000003",
		EscrowModuleAddress:    "0x0000000000000004",
		PaymentModuleAddress:   "0x0000000000000005",
	}
}

// TestSubstituteAddressesRewritesArtDropImports covers the substitution
// mechanism itself: every "import <ArtDropContract> from 0x..." line must
// end up with cfg's address, regardless of what address was originally
// written in the embedded source.
func TestSubstituteAddressesRewritesArtDropImports(t *testing.T) {
	cfg := testAddressConfig()

	script := `import FungibleToken from 0x9a0766d93b6608b7
import ArtDropCore from 0xec581a0282d99a1a
import ArtDropRegistry from 0xec581a0282d99a1a
import EscrowModule from 0x1bfedfa0ec66c23e
import PaymentModule from 0x1bfedfa0ec66c23e

access(all) fun main() {}
`

	got := substituteAddresses(script, cfg)

	want := []string{
		"import ArtDropCore from " + cfg.ArtDropCoreAddress,
		"import ArtDropRegistry from " + cfg.ArtDropRegistryAddress,
		"import EscrowModule from " + cfg.EscrowModuleAddress,
		"import PaymentModule from " + cfg.PaymentModuleAddress,
	}
	for _, line := range want {
		if !strings.Contains(got, line) {
			t.Fatalf("expected substituted script to contain %q, got:\n%s", line, got)
		}
	}
}

// TestSubstituteAddressesLeavesNonArtDropImportsAlone covers the standard
// contracts (FungibleToken, NonFungibleToken, MetadataViews) called out in
// the task as explicitly out of scope: their import lines must survive
// substitution byte-for-byte.
func TestSubstituteAddressesLeavesNonArtDropImportsAlone(t *testing.T) {
	cfg := testAddressConfig()

	script := `import FungibleToken from 0x9a0766d93b6608b7
import NonFungibleToken from 0x631e88ae7f1d7c20
import MetadataViews from 0x631e88ae7f1d7c20
import ArtDropCore from 0xec581a0282d99a1a
`

	got := substituteAddresses(script, cfg)

	for _, line := range []string{
		"import FungibleToken from 0x9a0766d93b6608b7",
		"import NonFungibleToken from 0x631e88ae7f1d7c20",
		"import MetadataViews from 0x631e88ae7f1d7c20",
	} {
		if !strings.Contains(got, line) {
			t.Fatalf("expected stable standard-contract import to survive unchanged: %q, got:\n%s", line, got)
		}
	}
	if strings.Contains(got, "0xec581a0282d99a1a") {
		t.Fatalf("expected ArtDropCore's old address to be gone, got:\n%s", got)
	}
}

// TestSubstituteAddressesKeepsScriptOtherwiseIntact makes sure substitution
// only touches import lines — comments, transaction bodies and blank lines
// pass through unchanged, and the result stays syntactically plausible
// Cadence (no placeholder tokens left behind).
func TestSubstituteAddressesKeepsScriptOtherwiseIntact(t *testing.T) {
	cfg := testAddressConfig()

	script := "/// a comment mentioning import ArtDropCore from 0xec581a0282d99a1a in prose\n" +
		"import ArtDropCore from 0xec581a0282d99a1a\n" +
		"\n" +
		"transaction() {\n" +
		"    prepare(signer: &Account) {}\n" +
		"}\n"

	got := substituteAddresses(script, cfg)

	if !strings.Contains(got, "/// a comment mentioning import ArtDropCore from 0xec581a0282d99a1a in prose") {
		t.Fatalf("expected comment line to survive unchanged, got:\n%s", got)
	}
	if !strings.Contains(got, "transaction() {\n    prepare(signer: &Account) {}\n}") {
		t.Fatalf("expected transaction body to survive unchanged, got:\n%s", got)
	}
	if strings.Contains(got, "{{") || strings.Contains(got, "}}") {
		t.Fatalf("expected no template placeholder syntax in substituted output, got:\n%s", got)
	}
}

// TestEmbeddedCDCScriptsSubstituteCleanly is a light regression check that
// every .cdc file actually embedded into the binary (via NewService) still
// contains the exact "import <Contract> from 0x..." shape
// substituteAddresses expects, for each of the four ArtDrop contracts it
// references. It doesn't re-verify the full go:embed file list against the
// task's audit (18 ArtDropCore / 2 EscrowModule / 0 PaymentModule / 1
// ArtDropRegistry, post escrow-lifecycle redesign — release_escrow.cdc,
// cancel_escrow.cdc and refund_escrow.cdc were deleted, taking the only
// PaymentModule import and three of the five EscrowModule imports with
// them) — that was done by hand — but it would catch a future .cdc edit
// that silently breaks substitution (e.g. an import written with an alias
// or on a wrapped line).
func TestEmbeddedCDCScriptsSubstituteCleanly(t *testing.T) {
	cfg := ParseTestConfig(t)

	for name, script := range embeddedCDCScripts() {
		t.Run(name, func(t *testing.T) {
			got := substituteAddresses(script, *cfg)
			for contract, addr := range contractAddresses(*cfg) {
				if strings.Contains(script, "import "+contract+" from") && !strings.Contains(got, "import "+contract+" from "+addr) {
					t.Fatalf("%s imports %s but substitution didn't rewrite it to %s, got:\n%s", name, contract, addr, got)
				}
			}
		})
	}
}

// addressLiteralRE matches a bare Flow address literal anywhere in a line —
// deliberately broader than importLineRE, which only matches the specific
// "import X from 0x..." shape.
var addressLiteralRE = regexp.MustCompile(`0x[0-9A-Fa-f]{16}`)

// TestEmbeddedCDCScriptsHaveNoAddressLiteralsOutsideImportLines guards
// against exactly the bug found auditing is_artist.cdc against the live
// deployed contracts (2026-08-19): it hardcoded the ArtDropRegistry address
// twice — once on its import line, which substituteAddresses correctly
// rewrites, and once again inline in a getAccount(0x...) call in the
// function body, which substituteAddresses can never see by construction
// (it only ever matches import lines). A redeploy updated the import and
// silently left the body literal pointing at the retired account — the
// script still type-checked, still executed, and returned false for every
// real artist. No build ever caught it; it shipped to production.
//
// This scans every embedded script for any 16-hex-char address literal
// that isn't on an import line and fails the build if it finds one, so the
// next script written with this shape fails a test instead of shipping.
// The fix is always the same: take the address as a script/transaction
// parameter built from Config (see is_artist.cdc's registryOwner and
// create_escrow.cdc's logicOwner), never hardcode it a second time.
func TestEmbeddedCDCScriptsHaveNoAddressLiteralsOutsideImportLines(t *testing.T) {
	for name, script := range embeddedCDCScripts() {
		t.Run(name, func(t *testing.T) {
			for i, line := range strings.Split(script, "\n") {
				if importLineRE.MatchString(line) {
					continue
				}
				if addr := addressLiteralRE.FindString(line); addr != "" {
					t.Fatalf("%s:%d: address literal %s outside an import line — substituteAddresses can't rewrite this; take it as a parameter instead:\n  %s", name, i+1, addr, line)
				}
			}
		})
	}
}
