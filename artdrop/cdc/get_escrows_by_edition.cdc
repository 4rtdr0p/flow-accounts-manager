/// get_escrows_by_edition.cdc — Returns the escrow ids recorded for a given
/// edition via the O(1) ArtDropRegistry.EscrowsByEditionIndex
/// (escrow-lifecycle redesign). Ported from
/// artdrop-protocol/scripts/registry/get_escrows_by_edition.cdc for issue
/// #100, mirroring get_escrows_by_buyer.cdc's parameter shape (registryOwner
/// as an explicit arg rather than a hardcoded literal — see that file's
/// comment for why).
///
/// Returns an empty array both when the edition has no escrows and when the
/// index itself isn't published yet — a caller can't tell those two apart
/// from this script alone.
import ArtDropRegistry from 0xec581a0282d99a1a

access(all) fun main(editionId: UInt64, registryOwner: Address): [UInt64] {
    let cap = getAccount(registryOwner).capabilities
        .borrow<&{ArtDropRegistry.IEscrowsByEditionIndexReader}>(ArtDropRegistry.EscrowsByEditionPublicPath())
    return cap?.getEscrowIds(editionId: editionId) ?? []
}
