/// get_escrow_summary.cdc — Return the full ArtDropCore.EscrowSummary as a
/// flat `{String: AnyStruct}` dictionary for the wallet-api Go decoder.
///
/// Used to return only `summary.status.rawValue` (a bare UInt8): the
/// contract's EscrowSummary struct (ArtDropCore.cdc) has always carried
/// buyer, seller, editionId, chipId, unlockAt, nonce, certificateId,
/// releaseReason and claimed/claimedAt too — this was a limitation of this
/// script, not of the contract. See issue #98.
///
/// `status` and `releaseReason` are returned as their enum rawValue
/// (UInt8) — Go has no notion of a Cadence enum type, so callers decode
/// them the same way this file's sibling scripts already do for other
/// enum-typed fields.
import ArtDropCore from 0xec581a0282d99a1a

access(all) fun main(escrowId: UInt64): {String: AnyStruct}? {
    let s = ArtDropCore.getEscrowSummary(id: escrowId)
    if s == nil {
        return nil
    }
    let e = s!
    return {
        "id": e.id,
        "buyer": e.buyer,
        "seller": e.seller,
        "editionId": e.editionId,
        "chipId": e.chipId,
        "unlockAt": e.unlockAt,
        "nonce": e.nonce,
        "certificateId": e.certificateId,
        "status": e.status.rawValue,
        "releaseReason": e.releaseReason?.rawValue,
        "claimed": e.claimed,
        "claimedAt": e.claimedAt
    }
}
