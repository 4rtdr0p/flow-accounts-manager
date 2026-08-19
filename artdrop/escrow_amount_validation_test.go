package artdrop

import (
	"context"
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/onflow/flow-go-sdk"
)

// This file documents and pins the escrow `amount` validation gap first
// reported live against a real Flow emulator on 2026-08-06 (see
// flow-accounts-manager/INFORME_ESCROW_LIVE_TEST.md, sections 8-9),
// re-verified by static reading on 2026-08-18, and re-verified again on
// 2026-08-19 against the config-driven-addresses branch and the
// escrow-lifecycle redesign (new EscrowModule, cancel/refund deleted).
//
// Confirmed facts this test encodes, current as of the redesign:
//
//  1. CreateEscrow (artdrop/service.go) ALWAYS uses Config.AdminAddress as
//     the transaction proposer (and, per transactions/service.go, the
//     admin is also always the fee payer). The `address` path parameter is
//     validated but never used as proposer. The `amount` field is parsed
//     with newUFix64 and forwarded verbatim as a UFix64 argument with no
//     comparison against a sale price, a percentage, a balance, or any cap.
//     Still true today — nothing in the config-driven-addresses work or the
//     escrow redesign touched amount validation.
//
//  2. create_escrow.cdc withdraws `amount` from the SIGNER's own vault, and
//     the signer is always the admin (see 1) — so the vault actually
//     debited is the admin wallet's, not the buyer's, regardless of what
//     `buyer`/`address` say. VaultIdentifier used to be a client-supplied
//     storage path on that same admin account; it's now fixed server-side
//     to "flowTokenVault" (defaultVaultIdentifier), which narrows *which*
//     admin vault gets hit but doesn't touch the underlying gap — `amount`
//     is still forwarded unchecked, still debited from the admin's own
//     FLOW vault.
//
//  3. SEVERITY SHIFT (2026-08-19, escrow-lifecycle redesign): the original
//     audit chained #1/#2 into a theft: create an escrow naming yourself
//     buyer with an inflated `amount`, then Cancel (pre-unlock) or Refund
//     (post-unlock) to route the admin-debited funds back to a vault you
//     control — see the former TestEscrowCreateThenCancel_
//     RoundTripsToSelfDeclaredBuyerWithoutSalePriceCheck, deleted below.
//     `cancel` and `refund` no longer exist anywhere in EscrowModule or in
//     this API (see the artdrop-cdc-audit commits on this branch) — that
//     return path is gone, not just gated differently. activateChipAndSettle
//     never pays a customer either; released FLOW always lands in ArtDrop's
//     own vault. So an unvalidated `amount` can no longer be routed back to
//     an attacker — the money isn't stolen, it's stuck: it locks up
//     ArtDrop's own FLOW in an escrow that only comes back via the
//     OperationalAdmin-gated timeout release (which, separately, nothing in
//     this wallet API currently has a path to trigger — see the
//     escrow-lifecycle-redesign audit report). That makes this an
//     availability/accounting problem, not a theft — still worth fixing,
//     no longer urgent.

// TestCreateEscrow_RejectsAmountUnrelatedToAnySalePrice encodes the DESIRED
// behavior: the server must not accept an `amount` it cannot justify against
// a real sale. No `sale_price` field exists anywhere in CreateEscrowRequest
// today (that absence is itself part of the bug — see the audit report), so
// this test uses a conservative placeholder ceiling instead of a real
// percentage check. Whatever shape the eventual fix takes (sale_price field
// + 5% comparison, admin-configured cap, balance check, ...), it must cause
// this request to be rejected.
//
// This test documents a real gap but is SKIPPED rather than left red: a
// deliberately-failing test in a package CI is about to start running
// (./artdrop/... is on the list for the planned handlers/configs -> +artdrop
// +studio +datastore/mongo CI expansion) would turn that expansion red on
// day one, and the likely outcome is someone excludes the package again —
// which would hide the real, currently-uncovered bugs that expansion exists
// to catch (a broken quote lookup already shipped once because of that
// coverage gap). Un-skip this the moment amount validation lands.
func TestCreateEscrow_RejectsAmountUnrelatedToAnySalePrice(t *testing.T) {
	t.Skip("KNOWN GAP: CreateEscrow forwards `amount` to the chain with no " +
		"validation against any sale price, cap, or balance — a caller with " +
		"only account.transfer scope on their own account can set it to " +
		"anything up to the admin vault's balance (see the header comment on " +
		"this file for the full chain and the 2026-08-19 severity reassessment). " +
		"Fixing this needs a decision this test can't make on its own: where " +
		"the authoritative sale price comes from. Payload-Galaxy owns pricing " +
		"via Stripe today, so the fix is a cross-service contract (e.g. " +
		"wallet-api validating `amount` against a Payload-Galaxy-supplied " +
		"price/quote), not a local change to this package. Un-skip this test " +
		"the moment that decision lands and the validation is implemented.")

	txSvc := &setupTxService{}
	svc := mustNewService(t, plugins.PluginDeps{
		Transactions: txSvc,
		Config: &configs.Config{
			AdminAddress: "0xf8d6e0586b0a20c7",
			ChainID:      flow.Emulator,
		},
	})

	// Mirrors the live emulator test of 2026-08-06 (INFORME_ESCROW_LIVE_TEST.md
	// section 9): an "absurd" amount with no sale price behind it whatsoever.
	// LogicOwner and VaultIdentifier are gone from CreateEscrowRequest as of
	// the config-driven-addresses work (they're server-controlled now — see
	// Config.LogicOwner / defaultVaultIdentifier), so this construction has
	// fewer fields than the original audit's version.
	_, _, err := svc.CreateEscrow(context.Background(), true, "0x179b6b1cb6755e31", CreateEscrowRequest{
		Buyer:      "0x179b6b1cb6755e31",
		Seller:     "0xf3fcd2c1a78f5eee",
		EditionId:  1,
		ChipId:     "chip-test-1",
		ChipPubKey: make([]byte, 64),
		UnlockAt:   4102444800.0, // far future
		Nonce:      1,
		Amount:     999999.0, // 999,999 FLOW "5% fee" -- no sale this size exists
	})

	if err == nil {
		t.Fatal("VULNERABILITY: CreateEscrow accepted amount=999999.0 FLOW with no " +
			"sale_price, no cap, and no balance check to validate it against. " +
			"This amount is withdrawn from the ADMIN wallet's flowTokenVault " +
			"(see transactions/service.go, artdrop/service.go CreateEscrow), not " +
			"the buyer's. A caller with only account.transfer scope on their own " +
			"account can set this to anything up to the admin vault's balance. " +
			"It can no longer be routed back to the caller (cancel/refund are " +
			"gone — see the escrow-lifecycle redesign), so this is now an " +
			"availability/accounting bug (ArtDrop's own FLOW gets stuck) rather " +
			"than a theft, but it's still an unvalidated amount reaching the " +
			"chain and should still be rejected.")
	}
}

// TestEscrowCreateThenCancel_RoundTripsToSelfDeclaredBuyerWithoutSalePriceCheck
// (the drain half of the original audit — CreateEscrow's unvalidated amount
// routed back to a self-declared buyer via Cancel) is deliberately NOT
// ported. `Service.Cancel` and the `cancel` EscrowModule function it called
// were deleted by the escrow-lifecycle redesign (2026-08-19) — cancel/refund
// don't exist on-chain or in this API any more, so a caller has no way to
// pull escrowed funds back to an account they control. See the severity-shift
// note at the top of this file: the remaining #1/#2 gap is now a stuck-funds
// problem, not a drain chain. Do not re-add a Cancel/Refund-based version of
// this test without first confirming the redesign reintroduced one of those
// functions.
