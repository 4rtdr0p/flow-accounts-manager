/// get_edition_summary.cdc — Return Edition metadata as a flat
/// `{String: AnyStruct}` dictionary with explicit primitive types.
///
/// Two contract-side fields differ from what the Go handler previously
/// expected:
///
/// - `state` is `ArtDropCore.EditionState` (an enum with a UInt8 rawValue),
///   not a bare UInt8 — the handler's `fields["state"].(cadence.UInt8)`
///   type assertion silently failed and `state` stayed at 0.
/// - There is NO `maxSupply` field on `EditionSummary`; the field is
///   `reprintLimit`. `maxSupply` stayed at 0.
///
/// This script unwraps the enum and exposes the relevant fields under
/// names that match the handler's expectations.
import ArtDropCore from 0xec581a0282d99a1a

access(all) fun main(id: UInt64): {String: AnyStruct}? {
    let s = ArtDropCore.getEditionSummary(id: id)
    if s == nil {
        return nil
    }
    let ed = s!

    var stateRaw: UInt8 = 0
    if let e = ed.state as? ArtDropCore.EditionState {
        stateRaw = e.rawValue
    }

    return {
        "id": ed.id,
        "originalId": ed.originalId,
        "artist": ed.artist,
        "shuffleSeedBlock": ed.shuffleSeedBlock,
        "reprintLimit": ed.reprintLimit,
        "prices": ed.prices,
        "profitSplit": ed.profitSplit,
        "rarityCurve": ed.rarityCurve,
        "multiplierWeights": ed.multiplierWeights,
        "createdAtBlock": ed.createdAtBlock,
        "schemaVersion": ed.schemaVersion,
        "state": stateRaw,
        "totalMinted": ed.totalMinted,
        "rarityProfile": ed.rarityProfile
    }
}
