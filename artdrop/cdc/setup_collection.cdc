/// setup_collection.cdc — Initialize a CertificateNFT Collection for the signer.
/// Creates the collection in storage and publishes the public capability if not already present.

import "ArtDropCore"

transaction {
    prepare(signer: auth(SaveValue, IssueStorageCapabilityController, PublishCapability, BorrowValue) &Account) {
        if signer.storage.borrow<&ArtDropCore.Collection>(from: ArtDropCore.CertCollectionStoragePath) == nil {
            signer.storage.save(
                <-ArtDropCore.createEmptyCollection(nftType: Type<@ArtDropCore.Certificate>()),
                to: ArtDropCore.CertCollectionStoragePath
            )
        }

        if signer.capabilities.borrow<&ArtDropCore.Collection>(ArtDropCore.CertCollectionPublicPath) == nil {
            let cap = signer.capabilities.storage.issue<&ArtDropCore.Collection>(
                ArtDropCore.CertCollectionStoragePath
            )
            signer.capabilities.publish(cap, at: ArtDropCore.CertCollectionPublicPath)
        }
    }
}
