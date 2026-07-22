/// get_original_summary.cdc — Return Original metadata as a flat
/// `{String: AnyStruct}` dictionary with explicit primitive types,
/// avoiding the contract struct's Address field which the wallet-api
/// Go cadence decoder does not extract reliably from `cadence.Struct`.
///
/// The contract's `artist` field is an Address, not a String; the previous
/// handler looked for an `artistName` field that does not exist.
import ArtDropCore from 0xec581a0282d99a1a

access(all) fun main(id: UInt64): {String: AnyStruct}? {
    let s = ArtDropCore.getOriginalSummary(id: id)
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
        "schemaVersion": orig.schemaVersion
    }
}
