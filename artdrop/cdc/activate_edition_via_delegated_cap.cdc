/// Activates a Draft edition through the signer's private OperationalAdmin
/// capability stored by claim_operational_admin_cap.cdc.
///
/// The import must use the explicit "from 0x..." form so substituteAddresses
/// (cdc.go) can rewrite it to Config.ArtDropCoreAddress at construction — a
/// bare `import "ArtDropCore"` (flow-CLI alias form) is left untouched and
/// fails at the Access Node with "[Error Code: 1054] location (ArtDropCore)
/// is not a valid location" because there is no flow.json alias resolution
/// server-side.
import ArtDropCore from 0xec581a0282d99a1a

transaction(editionId: UInt64) {
    prepare(signer: auth(CopyValue) &Account) {
        let cap = signer.storage.copy<
            Capability<auth(ArtDropCore.OperationalAdmin) &ArtDropCore.ProtocolAdmin>
        >(from: /storage/artdropOperationalAdminCap)
            ?? panic("activate_edition_via_delegated_cap: no delegated operational admin capability in storage")

        let admin = cap.borrow()
            ?? panic("activate_edition_via_delegated_cap: capability borrow failed")
        admin.activateEdition(id: editionId)
    }
}
