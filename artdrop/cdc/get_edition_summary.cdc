import ArtDropCore from 0xec581a0282d99a1a

access(all)
fun main(id: UInt64): ArtDropCore.EditionSummary? {
    return ArtDropCore.getEditionSummary(id: id)
}
