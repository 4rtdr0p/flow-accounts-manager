/// setup_artist_direct_claim.cdc — Claim an ArtistDirect capability from inbox.
///
/// Signer: the artist's own custodial account (the inbox entry recipient).
///
/// Run after the artist has been onboarded via onboard_artist.cdc. The
/// inbox name is deterministic: "artist-direct-<artistAddress>".
///
/// The value stored at AdminStoragePath is a Capability (a struct value),
/// not a ProtocolAdmin resource — the idempotency check uses
/// storage.type(at:), not borrow<T>, or it would silently always be nil.
///
/// Mirror of artdrop-protocol transactions/setup/setup_artist_direct_claim.cdc.
/// Verified against real testnet execution (tx 9fd50b3d...).
import ArtDropCore from 0xec581a0282d99a1a

transaction(provider: Address, inboxName: String) {
    prepare(signer: auth(ClaimInboxCapability, Storage, SaveValue) &Account) {
        let storagePath = ArtDropCore.AdminStoragePath
        if signer.storage.type(at: storagePath) == nil {
            let capability = signer.inbox.claim<auth(ArtDropCore.ArtistDirect) &ArtDropCore.ProtocolAdmin>(
                inboxName,
                provider: provider
            ) ?? panic("setup_artist_direct_claim: capability not found in inbox — was the artist onboarded via onboard_artist?")
            signer.storage.save(capability, to: storagePath)
        }
    }
}
