/// has_artist_display_name.cdc
/// Returns whether an artist has a display name set in
/// ArtDropRegistry's ArtistDisplayNameIndex. False if the artist has none,
/// or if the index isn't set up.
import ArtDropRegistry from 0xec581a0282d99a1a

access(all) fun main(artist: Address): Bool {
    let cap = getAccount(0xec581a0282d99a1a).capabilities
        .borrow<&{ArtDropRegistry.IArtistDisplayNameIndex}>(
            ArtDropRegistry.ArtistDisplayNamePublicPath()
        )
    return cap?.contains(artist: artist) ?? false
}
