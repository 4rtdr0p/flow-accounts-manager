/// get_certificates.cdc — List the certificates owned by an account, with
/// per-cert metadata: id, editionId, serial, isRevealed.
///
/// Returns `[{String: AnyStruct}]` so the script doesn't need a
/// Cadence contract change (no need to add a CertificateSummary struct
/// to ArtDropCore). Each dict has the keys: id, editionId, serial,
/// isRevealed — matching the fields exposed by
/// ArtDropCore.Certificate (see contracts/core/ArtDropCore.cdc).
///
/// Returns an empty array if the account has no Collection capability
/// at ArtDropCore.CertCollectionPublicPath (i.e. has not run
/// setup_collection.cdc).

import ArtDropCore from 0xec581a0282d99a1a
import NonFungibleToken from 0x631e88ae7f1d7c20

access(all) fun main(addr: Address): [{String: AnyStruct}] {
    let collection = getAccount(addr).capabilities
        .borrow<&ArtDropCore.Collection>(ArtDropCore.CertCollectionPublicPath)
    if collection == nil {
        return []
    }

    let result: [{String: AnyStruct}] = []
    for id in collection!.getIDs() {
        let nft = collection!.borrowNFT(id) as? &ArtDropCore.Certificate
        if nft == nil {
            continue
        }
        let cert = nft!
        result.append({
            "id": cert.id,
            "editionId": cert.editionId,
            "serial": cert.serial,
            "isRevealed": cert.isRevealed()
        })
    }
    return result
}
