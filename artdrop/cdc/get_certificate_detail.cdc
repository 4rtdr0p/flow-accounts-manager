/// get_certificate_detail.cdc — Return consolidated certificate metadata as
/// a flat `{String: AnyStruct}?` dictionary.
///
/// Single round-trip replacement for the five per-field scripts
/// (`get_certificate_base_tier.cdc`, `get_certificate_chip_pubkey.cdc`,
/// `get_certificate_is_revealed.cdc`, `get_certificate_final_multiplier.cdc`,
/// `get_certificate_display_name.cdc`) that used to be executed one after
/// the other by `GetCertificateDetail` (`artdrop/service.go`).
///
/// Returns `nil` instead of `panic` when the account has not set up the
/// collection at `ArtDropCore.CertCollectionPublicPath`, when the
/// capability exists but points at the wrong type, or when the certificate
/// id does not exist in the collection — all three failure modes now map
/// to HTTP 404 (issue #49, #53), matching the behaviour of
/// `get_original_summary.cdc` / `get_edition_summary.cdc`. The wallet-api
/// Go cadence decoder does not extract `cadence.Optional<UFix64>`
/// (baseTier / finalMultiplier) cleanly out of a struct field, so the
/// script returns the explicit primitive values inside a flat dict — same
/// rationale as `get_original_summary.cdc` and `get_edition_summary.cdc`.
///
/// Fields exposed (matches `artdrop.CertificateDetail` JSON shape):
///   - id: UInt64
///   - baseTier: UFix64?             (nil until revealed)
///   - finalMultiplier: UFix64?      (nil until revealed)
///   - chipPubKey: [UInt8]           (defensive copy via getChipPubKeyBytes)
///   - isRevealed: Bool
///   - displayName: String?          (Optional; from MetadataViews.Display.name)

import ArtDropCore from 0xec581a0282d99a1a
import "MetadataViews"

access(all) fun main(address: Address, id: UInt64): {String: AnyStruct}? {
    let collection = getAccount(address).capabilities
        .borrow<&ArtDropCore.Collection>(ArtDropCore.CertCollectionPublicPath)
    if collection == nil {
        return nil
    }

    let nft = collection!.borrowNFT(id) as? &ArtDropCore.Certificate
    if nft == nil {
        return nil
    }
    let cert = nft!

    let view = cert.resolveView(Type<MetadataViews.Display>()) as? MetadataViews.Display

    return {
        "id": cert.id,
        "baseTier": cert.baseTier,
        "finalMultiplier": cert.finalMultiplier,
        "chipPubKey": cert.getChipPubKeyBytes(),
        "isRevealed": cert.isRevealed(),
        "displayName": view?.name
    }
}
