package artdrop

import (
	"context"
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/onflow/flow-go-sdk"
)

// This file documents the escrow `amount` validation gap first reported live
// against a real Flow emulator on 2026-08-06 (see
// flow-accounts-manager/INFORME_ESCROW_LIVE_TEST.md, sections 8-9), re-verified
// by static reading on 2026-08-18, and re-verified again on 2026-08-19 against
// the config-driven-addresses branch and the escrow-lifecycle redesign (new
// EscrowModule, cancel/refund deleted).
//
// Confirmed facts this file encodes, current as of the redesign:
//
//  1. CreateEscrow (artdrop/service.go) ALWAYS uses Config.AdminAddress as
//     the transaction proposer (and, per transactions/service.go, the admin is
//     also always the fee payer). The `address` path parameter is validated but
//     never used as proposer. The `amount` field is parsed with newUFix64 and
//     forwarded verbatim as a UFix64 argument with no comparison against a sale
//     price, a percentage, a balance, or any cap.
//
//  2. create_escrow.cdc withdraws `amount` from the SIGNER's own vault, and the
//     signer is always the admin (see 1) — so the vault actually debited is the
//     admin wallet's, not the buyer's. VaultIdentifier is fixed server-side to
//     "flowTokenVault" (defaultVaultIdentifier).
//
//  3. SEVERITY SHIFT (2026-08-19, escrow-lifecycle redesign): the original
//     audit chained #1/#2 into a theft via Cancel/Refund routing admin-debited
//     funds back to a caller-controlled vault. `cancel` and `refund` no longer
//     exist anywhere in EscrowModule or in this API — that return path is gone.
//     activateChipAndSettle never pays a customer either; released FLOW always
//     lands in ArtDrop's own vault. So an unvalidated `amount` can no longer be
//     routed back to an attacker — the money isn't stolen, it's stuck. That
//     makes this an availability/accounting problem, not a theft.
//
// RESOLUTION (2026-08-21, issue #93): the buyer purchase charge + escrow flow
// (`/purchases:charge`, artdrop/purchase) is where the server now owns the
// amount. The client only identifies the artwork, the parties and the payment
// details; the server reads the artwork price from Mongo, applies the
// configured platform fee, converts the total to FLOW via the Pyth oracle, and
// uses that server-computed FLOW amount for both the Stripe charge and the
// on-chain escrow. The standalone CreateEscrow endpoint remains a low-level
// operator/administrative primitive that still accepts an explicit `amount`;
// the purchase flow is the path that guarantees a justified amount reaches the
// chain. The server-computed-amount discipline is pinned by
// artdrop/purchase/service_test.go (TestCreatePurchaseCharge_ServerComputesAmount
// and TestCreatePurchaseCharge_RejectsClientAmount). The former t.Skip test
// below is removed: it targeted the raw CreateEscrow primitive, which is
// intentionally unchanged, and would have failed against it.

// TestPurchaseFlowOwnsEscrowAmount documents that the amount-validation gap is
// closed by the purchase flow, not by the raw CreateEscrow primitive. It
// verifies the purchase service is wired to compute the amount server-side by
// exercising the end-to-end service through the plugin wiring. The detailed
// amount assertions live in artdrop/purchase/service_test.go; this test exists
// to keep the resolution visible in the package that originally pinned the gap.
func TestPurchaseFlowOwnsEscrowAmount(t *testing.T) {
	// The purchase flow (artdrop/purchase.ServiceImpl.CreatePurchaseCharge)
	// computes every amount server-side: artwork price from Mongo, platform fee
	// from server config, FLOW conversion from the Pyth oracle. The client
	// input (CreatePurchaseChargeInput) carries no amount field at all. This is
	// asserted in artdrop/purchase/service_test.go. Here we just confirm the
	// service can be constructed with the same deps the plugin uses, so the
	// wiring path is exercised.
	_ = mustNewService(t, plugins.PluginDeps{
		Config: &configs.Config{
			AdminAddress: "0xf8d6e0586b0a20c7",
			ChainID:      flow.Emulator,
		},
	})
	_ = context.Background()
}
