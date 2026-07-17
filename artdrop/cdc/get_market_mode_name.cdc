/// get_market_mode_name.cdc
/// Returns the current MarketMode name as a String.
import ArtDropCore from 0xe2f96cbbdfde8c9f

access(all)
fun main(): String {
    return ArtDropCore.getMarketModeName()
}
