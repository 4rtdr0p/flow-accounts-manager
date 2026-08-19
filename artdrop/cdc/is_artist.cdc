/// is_artist.cdc
/// Returns true if `artist` has created at least one Original, as tracked by
/// ArtDropRegistry's ArtistIndex (published at ArtDropRegistry.ArtistPublicPath
/// on the ArtDrop account).
///
/// Uses IArtistIndexReader (read-only) instead of the full IArtistIndex,
/// since the public capability at ArtistPublicPath was narrowed to the
/// reader interface (artdrop-protocol issue #163 - the old wide interface
/// exposed register/unregister publicly).
///
/// registryOwner is the account the capability is published on (the same
/// account ArtDropRegistry is deployed to). It used to be a second,
/// independent hardcoded literal here — invisible to substituteAddresses,
/// which only ever rewrites import lines — so a redeploy correctly updated
/// the import above but silently left this one pointing at the retired
/// account, making the script resolve and run while returning false for
/// every real artist. Taking it as a parameter (Service.IsArtist passes
/// Config.ArtDropRegistryAddress) makes that drift structurally
/// impossible instead of merely detectable.
import ArtDropRegistry from 0xec581a0282d99a1a

access(all)
fun main(artist: Address, registryOwner: Address): Bool {
    let cap = getAccount(registryOwner).capabilities
        .borrow<&{ArtDropRegistry.IArtistIndexReader}>(ArtDropRegistry.ArtistPublicPath)
    return cap?.isArtist(artist: artist) ?? false
}