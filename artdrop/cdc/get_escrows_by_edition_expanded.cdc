/// get_escrows_by_edition_expanded.cdc — Returns the full EscrowSummary
/// (see get_escrow_summary.cdc for the exact field shape) for every escrow
/// recorded for a given edition via ArtDropRegistry.EscrowsByEditionIndex,
/// all inside a single script execution — same N+1-avoidance rationale as
/// get_escrows_by_buyer_expanded.cdc (issue #100).
///
/// registryOwner is the account the capability is published on (the same
/// account ArtDropRegistry is deployed to) — see get_escrows_by_edition.cdc.
import ArtDropRegistry from 0xec581a0282d99a1a
import ArtDropCore from 0xec581a0282d99a1a

access(all) fun main(editionId: UInt64, registryOwner: Address): [{String: AnyStruct}] {
    let cap = getAccount(registryOwner).capabilities
        .borrow<&{ArtDropRegistry.IEscrowsByEditionIndexReader}>(ArtDropRegistry.EscrowsByEditionPublicPath())
    let ids = cap?.getEscrowIds(editionId: editionId) ?? []

    let result: [{String: AnyStruct}] = []
    for id in ids {
        if let s = ArtDropCore.getEscrowSummary(id: id) {
            result.append({
                "id": s.id,
                "buyer": s.buyer,
                "seller": s.seller,
                "editionId": s.editionId,
                "chipId": s.chipId,
                "unlockAt": s.unlockAt,
                "nonce": s.nonce,
                "certificateId": s.certificateId,
                "status": s.status.rawValue,
                "releaseReason": s.releaseReason?.rawValue,
                "claimed": s.claimed,
                "claimedAt": s.claimedAt
            })
        }
    }
    return result
}
