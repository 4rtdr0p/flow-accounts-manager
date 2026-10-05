/// Activates a Draft edition through the signer's private OperationalAdmin
/// capability stored by claim_operational_admin_cap.cdc.
import "ArtDropCore"

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
