/// get_artist_display_name.cdc
/// Returns an artist's display name from ArtDropRegistry's
/// ArtistDisplayNameIndex (published at
/// ArtDropRegistry.ArtistDisplayNamePublicPath() on the ArtDrop account).
/// Returns nil if the artist hasn't set one yet (no writer transaction is
/// live yet — see artdrop-protocol docs/W12-summary-fields-design.md §open
/// questions for the pending self-service/admin-curated policy decision).
import ArtDropRegistry from 0xec581a0282d99a1a

access(all) fun main(artist: Address): String? {
    let cap = getAccount(0xec581a0282d99a1a).capabilities
        .borrow<&{ArtDropRegistry.IArtistDisplayNameIndex}>(
            ArtDropRegistry.ArtistDisplayNamePublicPath()
        )
    return cap?.get(artist: artist) ?? nil
}
