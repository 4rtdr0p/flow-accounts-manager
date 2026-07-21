import ArtDropCore from 0xec581a0282d99a1a

access(all)
fun main(id: UInt64): ArtDropCore.OriginalSummary? {
    return ArtDropCore.getOriginalSummary(id: id)
}
