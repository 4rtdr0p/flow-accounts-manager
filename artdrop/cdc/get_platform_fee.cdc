/// get_platform_fee.cdc
/// Returns the current platform fee in basis points (0.0 = no fee).
import ArtDropCore from 0xe2f96cbbdfde8c9f

access(all)
fun main(): UFix64 {
    return ArtDropCore.getPlatformFee()
}
