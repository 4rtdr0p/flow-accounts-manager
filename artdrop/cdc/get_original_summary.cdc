import "ArtDropCore"

access(all)
fun main(id: UInt64): ArtDropCore.OriginalSummary? {
    return ArtDropCore.getOriginalSummary(id: id)
}
