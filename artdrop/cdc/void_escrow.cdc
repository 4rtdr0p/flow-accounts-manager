/// void_escrow.cdc — Admin annulment of a Pending escrow (issue #185),
/// signed by the wallet-api's own admin account via a delegated capability
/// — no EscrowModule signing key on this account.
///
/// voidEscrow is OperationalAdmin-gated and off the public IEscrowLogic
/// interface, so create_escrow.cdc/re_escrow.cdc's usual
/// `getAccount(logicOwner).capabilities.borrow<&{EscrowModule.IEscrowLogic}>`
/// path can't reach it. Instead this account holds a
/// Capability<auth(ArtDropCore.OperationalAdmin) &EscrowModule.EscrowLogic>
/// claimed via inbox from logicOwner (see artdrop-protocol's
/// transactions/admin/grant_escrow_void_cap.cdc +
/// transactions/setup/claim_escrow_void_cap.cdc) and saved at
/// /storage/artdropEscrowVoidAdminCap — this transaction just reads that
/// capability from its own storage and calls through it.
import ArtDropCore from 0xec581a0282d99a1a
import EscrowModule from 0x1bfedfa0ec66c23e

transaction(escrowId: UInt64) {
    prepare(signer: auth(Storage) &Account) {
        let capabilityPath = StoragePath(identifier: "artdropEscrowVoidAdminCap")!

        let cap = signer.storage.copy<
            Capability<auth(ArtDropCore.OperationalAdmin) &EscrowModule.EscrowLogic>
        >(from: capabilityPath)
            ?? panic("void_escrow: no delegated void capability in storage — run claim_escrow_void_cap.cdc first")

        let logic = cap.borrow()
            ?? panic("void_escrow: capability borrow failed — grant may have been revoked or logicOwner's EscrowLogic moved")

        logic.voidEscrow(escrowId: escrowId, releaser: signer.address)
    }
}
