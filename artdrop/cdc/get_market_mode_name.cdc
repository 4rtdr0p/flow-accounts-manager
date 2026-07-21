/// get_market_mode_name.cdc
/// Returns the current MarketMode name as a String.
import ArtDropCore from 0xec581a0282d99a1a

access(all)
fun main(): String {
    return ArtDropCore.getMarketModeName()
}
