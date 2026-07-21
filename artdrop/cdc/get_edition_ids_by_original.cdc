/// get_edition_ids_by_original.cdc
/// Returns the list of Edition IDs belonging to the given Original, as
/// tracked by ArtDropRegistry.EditionsPerOriginalIndex (populated at
/// Edition-creation time; backfilled once for pre-existing Originals —
/// see artdrop-protocol docs/W12-summary-fields-design.md).
/// Returns an empty array if the index isn't set up or the Original has
/// no Editions.
import ArtDropCore from 0xec581a0282d99a1a

access(all) fun main(originalId: UInt64): [UInt64] {
    return ArtDropCore.getEditionIdsByOriginal(originalId: originalId)
}
