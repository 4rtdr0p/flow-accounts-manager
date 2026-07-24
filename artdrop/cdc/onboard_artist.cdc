/// onboard_artist.cdc — Dynamic artist onboarding via ArtistOnboarding.
///
/// Signer: an ArtistOnboarding holder (the wallet-api's own custodial account).
///
/// Publishes an ArtistDirect capability to the artist's inbox without
/// requiring the central governance key to sign anything. The artist then
/// runs setup_artist_direct_claim.cdc to claim the cap.
///
/// The inbox name is deterministic: "artist-direct-<artistAddress>",
/// set by ArtDropCore.issueArtistDirectCapability at the contract level.
///
/// Mirror of artdrop-protocol transactions/setup/onboard_artist.cdc.
/// Verified against real testnet execution (tx 0d64107e...).
import ArtDropCore from 0xec581a0282d99a1a

transaction(artist: Address) {
    prepare(signer: auth(Storage) &Account) {
        let cap = signer.storage.copy<
            Capability<auth(ArtDropCore.ArtistOnboarding) &ArtDropCore.ProtocolAdmin>
        >(
            from: ArtDropCore.ArtistOnboardingStoragePath()
        ) ?? panic("onboard_artist: signer does not hold ArtistOnboarding on ProtocolAdmin at ArtistOnboardingStoragePath — run setup_artist_onboarding_cap + claim first")
        let admin = cap.borrow()
            ?? panic("onboard_artist: claimed capability did not borrow — ProtocolAdmin may have been moved")
        ArtDropCore.issueArtistDirectCapability(admin: admin, artist: artist)
    }
}
