import ArtDropCore from 0x050dd2bfe6cd6421

access(all)
fun main(id: UInt64): ArtDropCore.EditionSummary? {
    return ArtDropCore.getEditionSummary(id: id)
}
