/// is_artist.cdc
/// Returns true if `artist` has created at least one Original, as tracked by
/// ArtDropRegistry's ArtistIndex (published at ArtDropRegistry.ArtistPublicPath
/// on the ArtDrop account).
///
/// Uses IArtistIndexReader (read-only) instead of the full IArtistIndex,
/// since the public capability at ArtistPublicPath was narrowed to the
/// reader interface (artdrop-protocol issue #163 - the old wide interface
/// exposed register/unregister publicly).
import ArtDropRegistry from 0xec581a0282d99a1a

access(all)
fun main(artist: Address): Bool {
    let cap = getAccount(0xec581a0282d99a1a).capabilities
        .borrow<&{ArtDropRegistry.IArtistIndexReader}>(ArtDropRegistry.ArtistPublicPath)
    return cap?.isArtist(artist: artist) ?? false
}