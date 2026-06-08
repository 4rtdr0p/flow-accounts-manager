// Example NFT implementation for ArtDrop testing (Cadence 1.0).

import NonFungibleToken from "./NonFungibleToken.cdc"
import ViewResolver from "./ViewResolver.cdc"

access(all) contract ExampleNFT: NonFungibleToken {

    access(all) var totalSupply: UInt64

    access(all) event ContractInitialized()
    access(all) event Withdraw(id: UInt64, from: Address?)
    access(all) event Deposit(id: UInt64, to: Address?)

    access(all) let CollectionStoragePath: StoragePath
    access(all) let CollectionPublicPath: PublicPath
    access(all) let MinterStoragePath: StoragePath

    access(all) view fun getContractViews(resourceType: Type?): [Type] {
        return []
    }

    access(all) fun resolveContractView(resourceType: Type?, viewType: Type): AnyStruct? {
        return nil
    }

    access(all) resource NFT: NonFungibleToken.NFT, ViewResolver.Resolver {
        access(all) let id: UInt64
        access(all) var metadata: {String: String}

        init(initID: UInt64) {
            self.id = initID
            self.metadata = {}
        }

        access(all) view fun getViews(): [Type] {
            return ExampleNFT.getContractViews(resourceType: Type<@ExampleNFT.NFT>())
        }

        access(all) fun resolveView(_ view: Type): AnyStruct? {
            return ExampleNFT.resolveContractView(resourceType: Type<@ExampleNFT.NFT>(), viewType: view)
        }

        access(all) fun createEmptyCollection(): @{NonFungibleToken.Collection} {
            return <-ExampleNFT.createEmptyCollection(nftType: Type<@ExampleNFT.NFT>())
        }

        access(all) view fun getAvailableSubNFTS(): {Type: [UInt64]} {
            return {}
        }

        access(all) fun getSubNFT(type: Type, id: UInt64): &{NonFungibleToken.NFT}? {
            return nil
        }
    }

    access(all) resource Collection: NonFungibleToken.Collection {
        access(all) var ownedNFTs: @{UInt64: {NonFungibleToken.NFT}}

        init () {
            self.ownedNFTs <- {}
        }

        access(NonFungibleToken.Withdraw) fun withdraw(withdrawID: UInt64): @{NonFungibleToken.NFT} {
            let token <- self.ownedNFTs.remove(key: withdrawID) ?? panic("missing NFT")
            emit Withdraw(id: token.id, from: self.owner?.address)
            return <-token
        }

        access(all) fun deposit(token: @{NonFungibleToken.NFT}) {
            let token <- token as! @ExampleNFT.NFT
            let id: UInt64 = token.id
            let oldToken <- self.ownedNFTs[id] <- token
            emit Deposit(id: id, to: self.owner?.address)
            destroy oldToken
        }

        access(all) view fun getIDs(): [UInt64] {
            return self.ownedNFTs.keys
        }

        access(all) view fun borrowNFT(_ id: UInt64): &{NonFungibleToken.NFT}? {
            return &self.ownedNFTs[id]
        }

        access(all) view fun getLength(): Int {
            return self.ownedNFTs.length
        }

        access(all) view fun getSupportedNFTTypes(): {Type: Bool} {
            return {Type<@ExampleNFT.NFT>(): true}
        }

        access(all) view fun isSupportedNFTType(type: Type): Bool {
            return type == Type<@ExampleNFT.NFT>()
        }

        access(all) fun createEmptyCollection(): @{NonFungibleToken.Collection} {
            return <-ExampleNFT.createEmptyCollection(nftType: Type<@ExampleNFT.NFT>())
        }
    }

    access(all) fun createEmptyCollection(nftType: Type): @{NonFungibleToken.Collection} {
        if nftType != Type<@ExampleNFT.NFT>() {
            panic("ExampleNFT.createEmptyCollection: unsupported nft type")
        }
        return <- create Collection()
    }

    access(all) resource NFTMinter {
        access(all) fun mintNFT(
            recipient: &{NonFungibleToken.CollectionPublic},
            name: String,
            description: String,
            thumbnail: String
        ) {
            var newNFT <- create NFT(initID: ExampleNFT.totalSupply)
            newNFT.metadata["name"] = name
            newNFT.metadata["description"] = description
            newNFT.metadata["thumbnail"] = thumbnail
            recipient.deposit(token: <-newNFT)
            ExampleNFT.totalSupply = ExampleNFT.totalSupply + UInt64(1)
        }
    }

    init() {
        self.CollectionStoragePath = /storage/exampleNFTCollection
        self.CollectionPublicPath = /public/exampleNFTCollection
        self.MinterStoragePath = /storage/exampleNFTMinter
        self.totalSupply = 0

        let collection <- create Collection()
        self.account.storage.save(<-collection, to: self.CollectionStoragePath)

        let cap = self.account.capabilities.storage.issue<&{NonFungibleToken.CollectionPublic}>(
            self.CollectionStoragePath
        )
        self.account.capabilities.publish(cap, at: self.CollectionPublicPath)

        let minter <- create NFTMinter()
        self.account.storage.save(<-minter, to: self.MinterStoragePath)

        emit ContractInitialized()
    }
}
