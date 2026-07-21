/// get_platform_fee.cdc
/// Returns the current platform fee in basis points (0.0 = no fee).
import ArtDropCore from 0xec581a0282d99a1a

access(all)
fun main(): UFix64 {
    return ArtDropCore.getPlatformFee()
}
