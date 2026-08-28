/// get_escrows_by_seller_expanded.cdc — Returns the full EscrowSummary (see
/// get_escrow_summary.cdc for the exact field shape) for every escrow open
/// against an edition created by the given seller/artist, walking three
/// existing public indices in one script execution (issue #100):
///
///   ArtDropRegistry.ArtistIndex        seller -> [originalId]
///   ArtDropCore.getEditionIdsByOriginal originalId -> [editionId]
///   ArtDropRegistry.EscrowsByEditionIndex editionId -> [escrowId]
///   ArtDropCore.getEscrowSummary        escrowId -> EscrowSummary?
///
/// There is no direct seller -> escrow index in the contract, and this
/// deliberately doesn't add one: composing the three existing indices
/// inside a single script has the same network cost — one access-node
/// round trip — as a dedicated index would, regardless of how many
/// originals/editions/escrows the seller has, since a script only pays for
/// round trips, not for internal looping. No contract or storage change.
///
/// registryOwner is the account ArtDropRegistry is deployed to — taken as
/// an explicit parameter rather than hardcoded, same reasoning as
/// get_escrows_by_buyer.cdc / is_artist.cdc.
///
/// Unlike get_escrows_by_buyer.cdc / get_escrows_by_edition.cdc, there is no
/// separate ids-only sibling script for this query: the expensive part is
/// the three-level walk itself, not resolving each id's summary, so
/// Service.ListEscrowsBySeller always calls this one script and derives the
/// plain id list from the summaries it returns when the caller didn't ask
/// for ?expand=summary.
import ArtDropRegistry from 0xec581a0282d99a1a
import ArtDropCore from 0xec581a0282d99a1a

access(all) fun main(seller: Address, registryOwner: Address): [{String: AnyStruct}] {
    let artistCap = getAccount(registryOwner).capabilities
        .borrow<&{ArtDropRegistry.IArtistIndexReader}>(ArtDropRegistry.ArtistPublicPath)
    let originalIds = artistCap?.getOriginals(artist: seller) ?? []

    let editionEscrowsCap = getAccount(registryOwner).capabilities
        .borrow<&{ArtDropRegistry.IEscrowsByEditionIndexReader}>(ArtDropRegistry.EscrowsByEditionPublicPath())

    let result: [{String: AnyStruct}] = []
    for originalId in originalIds {
        for editionId in ArtDropCore.getEditionIdsByOriginal(originalId: originalId) {
            let escrowIds = editionEscrowsCap?.getEscrowIds(editionId: editionId) ?? []
            for id in escrowIds {
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
        }
    }
    return result
}
