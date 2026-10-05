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
