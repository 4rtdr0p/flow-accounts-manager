/// get_original_extended_summary.cdc — Return the W12-extended Original
/// metadata as a flat `{String: AnyStruct}` dictionary, same rationale as
/// get_original_summary.cdc (the wallet-api Go cadence decoder does not
/// extract enum/Address/struct fields reliably from `cadence.Struct`).
///
/// Backed by `ArtDropCore.getOriginalExtendedSummary`, added in
/// artdrop-protocol commit f80473f (W12). Adds `editionCount`,
/// `totalMintedAcrossEditions` and `displayName` on top of the Original
/// fields. `displayName` is nil until an artist sets one via
/// ArtDropRegistry.ArtistDisplayNameIndex (no writer transaction is live
/// yet — see docs/W12-summary-fields-design.md in artdrop-protocol).
import ArtDropCore from 0xec581a0282d99a1a

access(all) fun main(id: UInt64): {String: AnyStruct}? {
    let s = ArtDropCore.getOriginalExtendedSummary(id: id)
    if s == nil {
        return nil
    }
    let orig = s!
    return {
        "id": orig.id,
        "artist": orig.artist,
        "name": orig.name,
        "prices": orig.prices,
        "createdAtBlock": orig.createdAtBlock,
        "schemaVersion": orig.schemaVersion,
        "editionCount": orig.editionCount,
        "totalMintedAcrossEditions": orig.totalMintedAcrossEditions,
        "displayName": orig.displayName
    }
}
