/// get_market_mode_name.cdc
/// Returns the current MarketMode name as a String.
import "ArtDropCore"

access(all)
fun main(): String {
    return ArtDropCore.getMarketModeName()
}
