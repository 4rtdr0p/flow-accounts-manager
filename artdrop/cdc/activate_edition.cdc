/// activate_edition.cdc — Transition an Edition from Draft to Active.
///
/// Signer: the wallet-api admin account (the one holding ProtocolAdmin with
/// the GovernanceAdmin entitlement at ArtDropCore.AdminStoragePath — the same
/// account onboard_artist.cdc uses for ArtistOnboarding).
///
/// Valid source state: Draft ONLY. After activation the Edition can mint
/// certificates via createEscrow (ArtDropCore requires Active or Locked).
///
/// Mirror of artdrop-protocol transactions/admin/activate_edition.cdc.
/// WARNING (protocol G-04): direct call is fine for test/dev; mainnet must
/// route activation through governance timelock instead.
import ArtDropCore from 0xec581a0282d99a1a

transaction(editionId: UInt64) {
    prepare(signer: auth(Storage) &Account) {
        let admin = signer.storage.borrow<auth(ArtDropCore.GovernanceAdmin) &ArtDropCore.ProtocolAdmin>(
            from: ArtDropCore.AdminStoragePath
        ) ?? panic("activate_edition: signer does not hold ProtocolAdmin at ArtDropCore.AdminStoragePath")

        admin.activateEdition(id: editionId)
    }
}
