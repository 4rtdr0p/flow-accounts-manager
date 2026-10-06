# Wallet delegated edition activation

`POST /v1/artdrop/editions/{id}/activate` now submits
`activate_edition_via_delegated_cap.cdc` using the wallet service's normal
`AdminAddress`. The transaction copies and borrows the private
`OperationalAdmin` capability at `/storage/artdropOperationalAdminCap`, then
calls `activateEdition`.

The endpoint keeps its on-chain idempotency pre-check: editions that are no
longer Draft return the existing `200 {"alreadyActive":true}` response without
submitting a transaction. `ARTDROP_PROTOCOL_ADMIN_ADDRESS` remains an optional,
validated configuration field for compatibility, but is not used by this
activation path.

## Import form matters (2026-10-06 testnet incident)

The transaction was initially copied from artdrop-protocol with its flow-CLI
alias import (`import "ArtDropCore"`) intact. That form only resolves through
a local `flow.json` aliases block — the Access Node rejects it at preprocess
time with `[Error Code: 1054] location (ArtDropCore) is not a valid location`,
and `substituteAddresses` (cdc.go) can never rewrite it because its regex only
anchors the explicit `import <Contract> from 0x...` shape. Every embedded
script must therefore use the explicit form (the source address is a placeholder
— construction rewrites it to `Config.ArtDropCoreAddress`);
`TestEmbeddedCDCScriptsHaveNoBareStringImports` fails the build on any bare
string import.

Before this endpoint can succeed, the wallet's `AdminAddress` must also have
claimed the delegated capability: `grant_operational_admin_cap.cdc` (signed by
the Core/Governance account) publishes `artdropOperationalAdmin` to the
operator inbox, and `claim_operational_admin_cap.cdc` (signed by the wallet
admin) stores it at `/storage/artdropOperationalAdminCap`. Without it the
transaction panics with "no delegated operational admin capability in
storage".
