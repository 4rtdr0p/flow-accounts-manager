/// get_all_escrow_summaries.cdc — Returns every EscrowSummary whose id
/// falls in [startId, endId] (inclusive), skipping ids that don't resolve
/// (e.g. gaps, none expected today but tolerated the same way
/// get_escrows_by_buyer_expanded.cdc skips a nil summary). One access-node
/// round trip regardless of the range size.
///
/// Used ONLY by the escrow projection's one-time backfill (issue #102) —
/// see artdrop/service.go's BackfillEscrowProjection, which sizes the range
/// with get_total_escrows.cdc and calls this in chunks. Not part of any
/// per-request read path, so it doesn't reintroduce the rate-limit problem
/// the projection exists to fix.
///
/// Same flat-dictionary shape as get_escrow_summary.cdc /
/// get_escrows_by_*_expanded.cdc, so the Go side reuses the same
/// decodeEscrowSummaryArray decoder.
import ArtDropCore from 0xec581a0282d99a1a

access(all) fun main(startId: UInt64, endId: UInt64): [{String: AnyStruct}] {
    let result: [{String: AnyStruct}] = []
    if endId < startId {
        return result
    }

    var id = startId
    while id <= endId {
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
        id = id + 1
    }
    return result
}
