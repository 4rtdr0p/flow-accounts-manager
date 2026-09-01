/// register_chip_via_delegated_cap.cdc — Register or re-key a chip's public
/// key (ChipPublicKeyIndex only — NOT ChipIndex, the separate UInt64 owner
/// map register_chip.cdc also writes; the wallet doesn't need that map for
/// signature verification) purely via a delegated ChipAdmin capability
/// claimed by this account ahead of time (one-time deploy-time setup, see
/// artdrop-protocol's transactions/admin/grant_chip_admin_cap.cdc +
/// transactions/setup/claim_chip_admin_cap.cdc) — no Core account (A3)
/// signing key held by this account.
///
/// Signer: wallet-api admin account. Mirrors void_escrow.cdc's copy/borrow/
/// panic structure: copy the stored `Capability<auth(ArtDropCore.ChipAdmin)
/// &ArtDropCore.ProtocolAdmin>` from /storage/artdropChipAdminCap, borrow
/// it, then call the matching admin function.
///
/// `logicOwner` here is the Core account (A3) that issued the capability —
/// used as a defense-in-depth check that the copied capability actually
/// points back at the expected provider before it's trusted, on top of
/// `.check()`. This is ArtDropCoreAddress (see Config), NOT the EscrowModule
/// account CreateEscrow/ActivateChip call LogicOwner.
///
/// `isReplace` selects registerChipPublicKey (initial provisioning) vs
/// replaceChipPublicKey (re-key) — the caller (artdrop.Service.ProvisionChip)
/// only ever passes false today; re-keying is not yet wired.
import ArtDropCore from 0xd97d6774544fcd9c

transaction(logicOwner: Address, chipId: String, publicKey: [UInt8], isReplace: Bool) {
    prepare(signer: auth(Storage) &Account) {
        let capabilityPath = StoragePath(identifier: "artdropChipAdminCap")!

        let cap = signer.storage.copy<
            Capability<auth(ArtDropCore.ChipAdmin) &ArtDropCore.ProtocolAdmin>
        >(from: capabilityPath)
            ?? panic("register_chip_via_delegated_cap: no delegated chip admin capability in storage — run claim_chip_admin_cap.cdc first")

        if cap.address != logicOwner {
            panic("register_chip_via_delegated_cap: capability provider does not match logicOwner")
        }

        let admin = cap.borrow()
            ?? panic("register_chip_via_delegated_cap: capability borrow failed — grant may have been revoked or logicOwner's ProtocolAdmin moved")

        if isReplace {
            admin.replaceChipPublicKey(chipId: chipId, newPublicKey: publicKey)
        } else {
            admin.registerChipPublicKey(chipId: chipId, publicKey: publicKey)
        }
    }
}
