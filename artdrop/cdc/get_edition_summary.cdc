import "ArtDropCore"

access(all)
fun main(id: UInt64): ArtDropCore.EditionSummary? {
    return ArtDropCore.getEditionSummary(id: id)
}
