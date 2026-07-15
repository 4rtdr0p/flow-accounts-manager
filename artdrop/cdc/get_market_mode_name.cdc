/// get_market_mode_name.cdc
/// Returns the current MarketMode name as a String.
import ArtDropCore from 0x050dd2bfe6cd6421

access(all)
fun main(): String {
    return ArtDropCore.getMarketModeName()
}
