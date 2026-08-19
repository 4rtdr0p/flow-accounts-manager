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

// contractAddresses maps each ArtDrop protocol contract name to the address
// it's deployed to in cfg. Contracts not in this map — FungibleToken,
// NonFungibleToken, MetadataViews — are the stable, non-ArtDrop-specific
// standard contracts called out in the task; substituteAddresses leaves
// their import lines untouched.
func contractAddresses(cfg Config) map[string]string {
	return map[string]string{
		"ArtDropCore":     cfg.ArtDropCoreAddress,
		"ArtDropRegistry": cfg.ArtDropRegistryAddress,
		"EscrowModule":    cfg.EscrowModuleAddress,
		"PaymentModule":   cfg.PaymentModuleAddress,
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
