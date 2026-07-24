/// create_original.cdc — Artist creates their own Original.
///
/// Signer: the artist's own custodial account (must already hold
/// ArtistDirect, claimed via setup_artist_direct_claim.cdc).
///
/// The artist identity is derived from the signer's own account
/// reference (artistCreateOriginal takes &Account, not Address), so it
/// cannot be forged by passing a different address.
///
/// Mirror of artdrop-protocol transactions/artist/create_original.cdc.
/// Verified against real testnet execution (tx b877e7a3..., post
/// security-hardening commit 2540289).
import ArtDropCore from 0xec581a0282d99a1a

transaction(name: String, description: String, prices: {String: UFix64}) {
    prepare(signer: auth(Storage) &Account) {
        let cap = signer.storage.copy<
            Capability<auth(ArtDropCore.ArtistDirect) &ArtDropCore.ProtocolAdmin>
        >(
            from: ArtDropCore.AdminStoragePath
        ) ?? panic("create_original: signer does not hold ArtistDirect on ProtocolAdmin at AdminStoragePath — run onboard_artist + claim first")
        let admin = cap.borrow()
            ?? panic("create_original: claimed capability did not borrow — ProtocolAdmin may have been moved")
        admin.artistCreateOriginal(
            artistAccount: signer,
            name: name,
            description: description,
            prices: prices
        )
    }
}
