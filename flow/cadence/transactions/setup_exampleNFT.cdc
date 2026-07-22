import NonFungibleToken from "../contracts/NonFungibleToken.cdc"
import ExampleNFT from "../contracts/ExampleNFT.cdc"

transaction {
    prepare(signer: auth(Storage, Capabilities) &Account) {
        // Return early if the account already has a collection
        if signer.storage.borrow<&ExampleNFT.Collection>(from: ExampleNFT.CollectionStoragePath) != nil {
            return
        }

        // create a new empty collection
        let collection <- ExampleNFT.createEmptyCollection(
            nftType: Type<@ExampleNFT.NFT>()
        )

        // save it to the account
        signer.storage.save(<-collection, to: ExampleNFT.CollectionStoragePath)

        // create a public capability for the collection
        let cap = signer.capabilities.storage.issue<&{NonFungibleToken.Collection}>(
            ExampleNFT.CollectionStoragePath
        )
        signer.capabilities.publish(cap, at: ExampleNFT.CollectionPublicPath)
    }
}
