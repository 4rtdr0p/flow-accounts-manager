import ArtDropCore from 0xe2f96cbbdfde8c9f

access(all)
fun main(id: UInt64): ArtDropCore.EditionSummary? {
    return ArtDropCore.getEditionSummary(id: id)
}
