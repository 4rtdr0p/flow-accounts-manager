/// get_escrows_by_buyer.cdc — Returns the escrow ids recorded for a given
/// buyer via the O(1) ArtDropRegistry.EscrowsByBuyerIndex (escrow-lifecycle
/// redesign). Ported from artdrop-protocol/scripts/registry/get_escrows_by_buyer.cdc
/// for issue #98.
///
/// registryOwner is the account the capability is published on (the same
/// account ArtDropRegistry is deployed to) — taken as an explicit parameter
/// rather than hardcoded, following the is_artist.cdc pattern (see that
/// file's comment for why: substituteAddresses only rewrites import lines,
/// never a literal buried in a function body).
///
/// Returns an empty array both when the buyer has no escrows and when the
/// index itself isn't published yet (no reader capability found) — a caller
/// can't tell those two apart from this script alone.
import ArtDropRegistry from 0xec581a0282d99a1a

access(all) fun main(buyer: Address, registryOwner: Address): [UInt64] {
    let cap = getAccount(registryOwner).capabilities
        .borrow<&{ArtDropRegistry.IEscrowsByBuyerIndexReader}>(ArtDropRegistry.EscrowsByBuyerPublicPath())
    return cap?.getEscrowIds(buyer: buyer) ?? []
}
