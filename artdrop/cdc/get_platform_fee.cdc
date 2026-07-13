/// get_platform_fee.cdc
/// Returns the current platform fee in basis points (0.0 = no fee).
import "ArtDropCore"

access(all)
fun main(): UFix64 {
    return ArtDropCore.getPlatformFee()
}
