// This is an example implementation of a Flow Non-Fungible Token
// It is not part of the official standard but it assumed to be
// very similar to how many NFTs would implement the core functionality.

import NonFungibleToken from "./NonFungibleToken.cdc"
import ViewResolver from "./ViewResolver.cdc"

access(all) contract ExampleNFT: NonFungibleToken {

    access(all) var totalSupply: UInt64

    access(all) event ContractInitialized()
    access(all) event Withdraw(id: UInt64, from: Address?)
    access(all) event Deposit(id: UInt64, to: Address?)

    // Named Paths
    //
    access(all) let CollectionStoragePath: StoragePath
    access(all) let CollectionPublicPath: PublicPath
    access(all) let MinterStoragePath: StoragePath

    access(all) resource NFT: NonFungibleToken.NFT, ViewResolver.Resolver {
        access(all) let id: UInt64

        access(all) var metadata: {String: String}

        init(initID: UInt64) {
            self.id = initID
            self.metadata = {}
        }

        // Implement ViewResolver.Resolver interface
        access(all) view fun getViews(): [Type] {
            return []
        }

        access(all) view fun resolveView(_ view: Type): AnyStruct? {
            return nil
        }

        // Implement NonFungibleToken.NFT interface
        access(all) fun createEmptyCollection(): @{NonFungibleToken.Collection} {
            return <-ExampleNFT.createEmptyCollection()
        }

        access(all) view fun getAvailableSubNFTS(): {Type: [UInt64]} {
            return {}
        }

        access(all) fun getSubNFT(type: Type, id: UInt64): &{NonFungibleToken.NFT}? {
            return nil
        }
    }

    access(all) resource Collection: NonFungibleToken.Collection {
        // dictionary of NFT conforming tokens
        // NFT is a resource type with an `UInt64` ID field
        access(all) var ownedNFTs: @{UInt64: NonFungibleToken.NFT}

        init () {
            self.ownedNFTs <- {}
        }

        // withdraw removes an NFT from the collection and moves it to the caller
        access(NonFungibleToken.Withdraw) fun withdraw(withdrawID: UInt64): @{NonFungibleToken.NFT} {
            let token <- self.ownedNFTs.remove(key: withdrawID) ?? panic("missing NFT")

            emit Withdraw(id: token.id, from: self.owner?.address)

            return <-token
        }

        // deposit takes a NFT and adds it to the collections dictionary
        // and adds the ID to the id array
        access(all) fun deposit(token: @{NonFungibleToken.NFT}) {
            let token <- token as! @ExampleNFT.NFT

            let id: UInt64 = token.id

            // add the new token to the dictionary which removes the old one
            let oldToken <- self.ownedNFTs[id] <- token

            emit Deposit(id: id, to: self.owner?.address)

            destroy oldToken
        }

        // getIDs returns an array of the IDs that are in the collection
        access(all) view fun getIDs(): [UInt64] {
            return self.ownedNFTs.keys
        }

        // borrowNFT gets a reference to an NFT in the collection
        // so that the caller can read its metadata and call its methods
        access(all) view fun borrowNFT(id: UInt64): &{NonFungibleToken.NFT}? {
            return &self.ownedNFTs[id] as &{NonFungibleToken.NFT}
        }

        // Implement required functions from NonFungibleToken.Collection
        access(all) view fun getLength(): Int {
            return self.ownedNFTs.length
        }

        access(all) view fun getSupportedNFTTypes(): {Type: Bool} {
            return {Type<@ExampleNFT.NFT>(): true}
        }

        access(all) view fun isSupportedNFTType(type: Type): Bool {
            return type == Type<@ExampleNFT.NFT>()
        }
    }

    // Function that anyone can call to create a new empty collection
    access(all) fun createEmptyCollection(): @{NonFungibleToken.Collection} {
        return <- create Collection()
    }

    // Resource that an admin or something similar would own to be
    // able to mint new NFTs
    //
    access(all) resource NFTMinter {

        // mintNFT mints a new NFT with a new ID
        // and deposit it in the recipients collection using their collection reference
        access(all) fun mintNFT(
            recipient: &{NonFungibleToken.CollectionPublic},
            name: String,
            description: String,
            thumbnail: String
        ) {
            // create a new NFT
            var newNFT <- create NFT(initID: ExampleNFT.totalSupply)
            
            // Set metadata
            newNFT.metadata["name"] = name
            newNFT.metadata["description"] = description
            newNFT.metadata["thumbnail"] = thumbnail

            // deposit it in the recipient's account using their reference
            recipient.deposit(token: <-newNFT)

            ExampleNFT.totalSupply = ExampleNFT.totalSupply + UInt64(1)
        }
    }

    init() {
        // Set our named paths
        self.CollectionStoragePath = /storage/exampleNFTCollection
        self.CollectionPublicPath = /public/exampleNFTCollection
        self.MinterStoragePath = /storage/exampleNFTMinter

        // Initialize the total supply
        self.totalSupply = 0

        // Create a Collection resource and save it to storage
        let collection <- create Collection()
        self.account.storage.save(<-collection, to: self.CollectionStoragePath)

        // create a public capability for the collection
        let cap = self.account.capabilities.storage.issue<&{NonFungibleToken.CollectionPublic}>(
            self.CollectionStoragePath
        )
        self.account.capabilities.publish(cap, at: self.CollectionPublicPath)

        // Create a Minter resource and save it to storage
        let minter <- create NFTMinter()
        self.account.storage.save(<-minter, to: self.MinterStoragePath)

        emit ContractInitialized()
    }
}
