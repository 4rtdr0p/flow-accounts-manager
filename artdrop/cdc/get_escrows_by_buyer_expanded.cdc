/// get_escrows_by_buyer_expanded.cdc — Returns the full EscrowSummary (see
/// get_escrow_summary.cdc for the exact field shape) for every escrow
/// recorded for a given buyer via ArtDropRegistry.EscrowsByBuyerIndex.
///
/// This replaces the N+1 pattern of calling get_escrow_summary.cdc once per
/// id from Go: that pattern issued one ExecuteScript per escrow and was
/// observed rate-limiting the public testnet access node at just 9 escrows
/// (issue #100). A Cadence script pays only one access-node round trip no
/// matter how much it loops internally, so walking the index and resolving
/// every summary inside a single script execution collapses N calls to 1
/// regardless of how many escrows the buyer has. No contract or storage
/// change — ArtDropCore.getEscrowSummary was already a public view
/// function; this only composes it with the existing buyer index in one
/// call instead of many.
///
/// registryOwner is the account the capability is published on (the same
/// account ArtDropRegistry is deployed to) — taken as an explicit parameter
/// rather than hardcoded, following get_escrows_by_buyer.cdc / is_artist.cdc
/// (substituteAddresses only ever rewrites import lines, never a literal
/// buried in a function body).
///
/// An id present in the index whose summary has since become unavailable
/// (getEscrowSummary returns nil) is silently skipped rather than erroring
/// the whole call — Service.ListEscrowsByBuyer still gets the authoritative
/// id list from get_escrows_by_buyer.cdc separately, so a caller can tell
/// the two apart if that ever happens.
import ArtDropRegistry from 0xec581a0282d99a1a
import ArtDropCore from 0xec581a0282d99a1a

access(all) fun main(buyer: Address, registryOwner: Address): [{String: AnyStruct}] {
    let cap = getAccount(registryOwner).capabilities
        .borrow<&{ArtDropRegistry.IEscrowsByBuyerIndexReader}>(ArtDropRegistry.EscrowsByBuyerPublicPath())
    let ids = cap?.getEscrowIds(buyer: buyer) ?? []

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
