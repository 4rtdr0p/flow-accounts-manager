// Bundled example account setup (placeholder contracts: FlowToken, FUSD, ExampleNFT).
// Each component is idempotent: skips initialization when already present.
import FungibleToken from 0xee82856bf20e2aa6
import FlowToken from 0x0ae53cb6e3f42a79
import FUSD from 0xf8d6e0586b0a20c7
import NonFungibleToken from 0xf8d6e0586b0a20c7
import ExampleNFT from 0xf8d6e0586b0a20c7

transaction {
    prepare(signer: auth(Storage, Capabilities) &Account) {
        // ExampleNFT collection (placeholder ArtDrop collection)
        if signer.storage.borrow<&ExampleNFT.Collection>(from: ExampleNFT.CollectionStoragePath) == nil {
            let collection <- ExampleNFT.createEmptyCollection(nftType: Type<@ExampleNFT.NFT>())
            signer.storage.save(<-collection, to: ExampleNFT.CollectionStoragePath)

            let nftCap = signer.capabilities.storage.issue<&{NonFungibleToken.Collection}>(
                ExampleNFT.CollectionStoragePath
            )
            signer.capabilities.publish(nftCap, at: ExampleNFT.CollectionPublicPath)
        }

        // FlowToken vault
        if signer.storage.borrow<&FlowToken.Vault>(from: /storage/flowTokenVault) == nil {
            let vault <- FlowToken.createEmptyVault(vaultType: Type<@FlowToken.Vault>())
            signer.storage.save(<-vault, to: /storage/flowTokenVault)

            let receiverCap = signer.capabilities.storage.issue<&{FungibleToken.Receiver}>(
                /storage/flowTokenVault
            )
            signer.capabilities.publish(receiverCap, at: /public/flowTokenReceiver)

            let balanceCap = signer.capabilities.storage.issue<&{FungibleToken.Balance}>(
                /storage/flowTokenVault
            )
            signer.capabilities.publish(balanceCap, at: /public/flowTokenBalance)
        }

        // FUSD vault
        if signer.storage.borrow<&FUSD.Vault>(from: /storage/fusdVault) == nil {
            let vault <- FUSD.createEmptyVault(vaultType: Type<@FUSD.Vault>())
            signer.storage.save(<-vault, to: /storage/fusdVault)

            let receiverCap = signer.capabilities.storage.issue<&{FungibleToken.Receiver}>(
                /storage/fusdVault
            )
            signer.capabilities.publish(receiverCap, at: /public/fusdReceiver)

            let balanceCap = signer.capabilities.storage.issue<&{FungibleToken.Balance}>(
                /storage/fusdVault
            )
            signer.capabilities.publish(balanceCap, at: /public/fusdBalance)
        }
    }
}
