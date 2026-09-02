package artdrop

import (
	"fmt"
	"regexp"
)

// importLineRE matches a Cadence "import <Contract> from 0x<address>" line.
// It's deliberately anchored to that exact shape (one import per line, no
// aliasing) — that's the only form used across artdrop/cdc/*.cdc today; a
// script written some other way is left untouched rather than guessed at.
var importLineRE = regexp.MustCompile(`(?m)^import (\w+) from 0x[0-9A-Fa-f]+$`)

// contractAddresses maps each contract name substituteAddresses rewrites to
// the address it lives on in cfg. This covers the four ArtDrop protocol
// contracts plus the three standard Flow contracts whose import lines several
// scripts hardcode to a specific network's address (FungibleToken in
// create_escrow.cdc/re_escrow.cdc, NonFungibleToken in register_provider.cdc,
// MetadataViews in get_certificate_detail.cdc). The standard contracts are the
// same well-known system contracts on any network, but their address differs
// per network — testnet's 0x9a0766d93b6608b7 FungibleToken is the emulator's
// 0xee82856bf20e2aa6 — so a script written for testnet won't run on the
// emulator unless these are rewritten too. Their cfg values default to the
// testnet addresses that were hardcoded before, so production is unchanged.
func contractAddresses(cfg Config) map[string]string {
	return map[string]string{
		"ArtDropCore":      cfg.ArtDropCoreAddress,
		"ArtDropRegistry":  cfg.ArtDropRegistryAddress,
		"EscrowModule":     cfg.EscrowModuleAddress,
		"PaymentModule":    cfg.PaymentModuleAddress,
		"FungibleToken":    cfg.FungibleTokenAddress,
		"NonFungibleToken": cfg.NonFungibleTokenAddress,
		"MetadataViews":    cfg.MetadataViewsAddress,
	}
}

// substituteAddresses rewrites the "import <Contract> from 0x..." lines of a
// Cadence script to use the addresses configured in cfg, regardless of what
// address is currently written in the source. Imports of contracts outside
// the ArtDrop suite are returned unchanged.
//
// cfg is assumed to already be validated (Config.normalizeAndValidate) —
// this function trusts every address it's given and never produces an
// import with an empty or malformed address; NewService is what enforces
// that trust by validating cfg before calling this.
func substituteAddresses(script string, cfg Config) string {
	addresses := contractAddresses(cfg)
	return importLineRE.ReplaceAllStringFunc(script, func(line string) string {
		m := importLineRE.FindStringSubmatch(line)
		contract := m[1]
		addr, ok := addresses[contract]
		if !ok {
			return line
		}
		return fmt.Sprintf("import %s from %s", contract, addr)
	})
}
