/// is_artist.cdc
/// Returns true if `artist` has created at least one Original, as tracked by
/// ArtDropRegistry's ArtistIndex (published at ArtDropRegistry.ArtistPublicPath
/// on the ArtDrop account).
import ArtDropRegistry from 0xec581a0282d99a1a

access(all)
fun main(artist: Address): Bool {
    let cap = getAccount(0xec581a0282d99a1a).capabilities
        .borrow<&{ArtDropRegistry.IArtistIndex}>(ArtDropRegistry.ArtistPublicPath)
    return cap?.isArtist(artist: artist) ?? false
}