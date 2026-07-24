/// create_edition.cdc — Artist creates an Edition for their own Original.
///
/// Signer: the artist's own custodial account (must already hold
/// ArtistDirect, claimed via setup_artist_direct_claim.cdc).
///
/// The artist identity is derived from the signer's own account
/// reference (artistCreateEdition takes &Account, not Address). The
/// contract validates original.artist == artistAccount.address — fails
/// if the signer does not own the Original being edited.
///
/// Mirror of artdrop-protocol transactions/artist/create_edition.cdc.
/// Verified against real testnet execution (tx 6e7795ef..., post
/// security-hardening commit 2540289).
import ArtDropCore from 0xec581a0282d99a1a

transaction(
    originalId: UInt64,
    reprintLimit: UInt64,
    prices: {String: UFix64},
    profitSplit: {String: UFix64},
    rarityCurve: [UInt64],
    multiplierWeights: {String: UFix64},
    rarityProfile: UInt8
) {
    prepare(signer: auth(Storage) &Account) {
        let cap = signer.storage.copy<
            Capability<auth(ArtDropCore.ArtistDirect) &ArtDropCore.ProtocolAdmin>
        >(
            from: ArtDropCore.AdminStoragePath
        ) ?? panic("create_edition: signer does not hold ArtistDirect on ProtocolAdmin at AdminStoragePath — run onboard_artist + claim first")
        let admin = cap.borrow()
            ?? panic("create_edition: claimed capability did not borrow — ProtocolAdmin may have been moved")
        let editionId = admin.artistCreateEdition(
            artistAccount: signer,
            originalId: originalId,
            reprintLimit: reprintLimit,
            prices: prices,
            profitSplit: profitSplit,
            rarityCurve: rarityCurve,
            multiplierWeights: multiplierWeights,
            rarityProfile: rarityProfile
        )
    }
}
