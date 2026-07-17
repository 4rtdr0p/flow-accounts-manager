/// register_provider.cdc — Register a provider capability for protocol transfer.
///
/// Registers a Capability<auth(NonFungibleToken.Withdraw) &ArtDropCore.Collection>
/// in ArtDropCore so that ProtocolAdmin can transfer certificates FROM this account
/// via protocol_transfer.cdc.
///
/// Required before ProtocolAdmin can move a Certificate out of this account.
/// One-time setup per account.

import ArtDropCore from 0xe2f96cbbdfde8c9f
import NonFungibleToken from 0x631e88ae7f1d7c20

transaction {
    prepare(signer: auth(IssueStorageCapabilityController) &Account) {
        let cap = signer.capabilities.storage.issue<auth(NonFungibleToken.Withdraw) &ArtDropCore.Collection>(
            ArtDropCore.CertCollectionStoragePath
        )

        ArtDropCore.registerProviderCap(cap: cap, address: signer.address)
    }
}
