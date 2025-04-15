/**

## The Flow Non-Fungible Token standard

## `NonFungibleToken` contract

The interface that all Non-Fungible Token contracts should conform to.
If a user wants to deploy a new NFT contract, their contract should implement
The types defined here

/// Contributors (please add to this list if you contribute!):
/// - Joshua Hannan - https://github.com/joshuahannan
/// - Bastian Müller - https://twitter.com/turbolent
/// - Dete Shirley - https://twitter.com/dete73
/// - Bjarte Karlsen - https://twitter.com/0xBjartek
/// - Austin Kline - https://twitter.com/austin_flowty
/// - Giovanni Sanchez - https://twitter.com/gio_incognito
/// - Deniz Edincik - https://twitter.com/bluesign
///
/// Repo reference: https://github.com/onflow/flow-nft

## `NFT` resource interface

The core resource type that represents an NFT in the smart contract.

## `Collection` Resource interface

The resource that stores a user's NFT collection.
It includes a few functions to allow the owner to easily
move tokens in and out of the collection.

## `Provider` and `Receiver` resource interfaces

These interfaces declare functions with some pre and post conditions
that require the Collection to follow certain naming and behavior standards.

They are separate because it gives developers the ability to define functions
that can use any type that implements these interfaces

By using resources and interfaces, users of NFT smart contracts can send
and receive tokens peer-to-peer, without having to interact with a central ledger
smart contract.

To send an NFT to another user, a user would simply withdraw the NFT
from their Collection, then call the deposit function on another user's
Collection to complete the transfer.

*/

import ViewResolver from "./ViewResolver.cdc"

/// The main NFT contract interface. Other NFT contracts will import
/// and implement this interface as well the interfaces defined in this interface
///
access(all) contract interface NonFungibleToken {

    /// An entitlement for allowing the withdrawal of tokens from a Vault
    access(all) entitlement Withdraw

    /// An entitlement for allowing updates and update events for an NFT
    access(all) entitlement Update

    /// Event that contracts should emit when the metadata of an NFT is updated
    /// It can only be emitted by calling the `emitNFTUpdated` function
    /// with an `Update` entitled reference to the NFT that was updated
    /// The entitlement prevents spammers from calling this from other users' collections
    /// because only code within a collection or that has special entitled access
    /// to the collections methods will be able to get the entitled reference
    /// 
    /// The event makes it so that third-party indexers can monitor the events
    /// and query the updated metadata from the owners' collections.
    ///
    access(all) event Updated(type: String, id: UInt64, uuid: UInt64, owner: Address?)
    access(all) view fun emitNFTUpdated(_ nftRef: auth(Update) &{NonFungibleToken.NFT})
    {
        emit Updated(type: nftRef.getType().identifier, id: nftRef.id, uuid: nftRef.uuid, owner: nftRef.owner?.address)
    }


    /// Event that is emitted when a token is withdrawn,
    /// indicating the type, id, uuid, the owner of the collection that it was withdrawn from,
    /// and the UUID of the resource it was withdrawn from, usually a collection.
    ///
    /// If the collection is not in an account's storage, `from` will be `nil`.
    ///
    access(all) event Withdrawn(type: String, id: UInt64, uuid: UInt64, from: Address?, providerUUID: UInt64)

    /// Event that emitted when a token is deposited to a collection.
    /// Indicates the type, id, uuid, the owner of the collection that it was deposited to,
    /// and the UUID of the collection it was deposited to
    ///
    /// If the collection is not in an account's storage, `from`, will be `nil`.
    ///
    access(all) event Deposited(type: String, id: UInt64, uuid: UInt64, to: Address?, collectionUUID: UInt64)

    /// Interface that the NFTs must conform to
    ///
    access(all) resource interface NFT: ViewResolver.Resolver {

        /// unique ID for the NFT
        access(all) let id: UInt64

        /// Event that is emitted automatically every time a resource is destroyed
        /// The type information is included in the metadata event so it is not needed as an argument
        access(all) event ResourceDestroyed(id: UInt64 = self.id, uuid: UInt64 = self.uuid)

        /// createEmptyCollection creates an empty Collection that is able to store the NFT
        /// and returns it to the caller so that they can own NFTs
        ///
        /// @return A an empty collection that can store this NFT
        ///
        access(all) fun createEmptyCollection(): @{Collection} {
            post {
                result.getLength() == 0: 
                    "NonFungibleToken.NFT.createEmptyCollection: Cannot create an empty collection! "
                    .concat("The created NonFungibleToken Collection has a non-zero length. ")
                    .concat(" A newly created collection must be empty!")
                result.isSupportedNFTType(type: self.getType()): 
                    "NonFungibleToken.NFT.createEmptyCollection: Cannot create an empty collection! "
                    .concat("The created NonFungibleToken Collection does not support NFTs of type <")
                    .concat(self.getType().identifier)
                    .concat(">. The collection must support NFTs of type <")
                    .concat(self.getType().identifier).concat(">.")
            }
        }

        /// Gets all the NFTs that this NFT directly owns
        ///
        /// @return A dictionary of all subNFTS keyed by type
        ///
        access(all) view fun getAvailableSubNFTS(): {Type: [UInt64]} {
            return {}
        }

        /// Get a reference to an NFT that this NFT owns
        /// Both arguments are optional to allow the NFT to choose
        /// how it returns sub NFTs depending on what arguments are provided
        /// For example, if `type` has a value, but `id` doesn't, the NFT 
        /// can choose which NFT of that type to return if there is a "default"
        /// If both are `nil`, then NFTs that only store a single NFT can just return
        /// that. This helps callers who aren't sure what they are looking for 
        ///
        /// @param type: The Type of the desired NFT
        /// @param id: The id of the NFT to borrow
        ///
        /// @return A structure representing the requested view.
        access(all) fun getSubNFT(type: Type, id: UInt64) : &{NonFungibleToken.NFT}? {
            return nil
        }
    }

    /// Interface to mediate withdrawals from a resource, usually a Collection
    ///
    access(all) resource interface Provider {

        // We emit withdraw events from the provider interface because conficting withdraw
        // events aren't as confusing to event listeners as conflicting deposit events

        /// withdraw removes an NFT from the collection and moves it to the caller
        /// It does not specify whether the ID is UUID or not
        ///
        /// @param withdrawID: The id of the NFT to withdraw from the collection
        /// @return @{NFT}: The NFT that was withdrawn
        ///
        access(Withdraw) fun withdraw(withdrawID: UInt64): @{NFT} {
            post {
                result.id == withdrawID: 
                    "NonFungibleToken.Provider.withdraw: Cannot withdraw NFT! "
                    .concat("The ID of the withdrawn NFT (")
                    .concat(result.id.toString())
                    .concat(") must be the same as the requested ID (")
                    .concat(withdrawID.toString())
                    .concat(").")
                emit Withdrawn(type: result.getType().identifier, id: result.id, uuid: result.uuid, from: self.owner?.address, providerUUID: self.uuid)
            }
        }
    }

    /// Interface to mediate deposits to the Collection
    ///
    access(all) resource interface Receiver {

        /// deposit takes an NFT as an argument and adds it to the Collection
        /// @param token: The NFT to deposit
        access(all) fun deposit(token: @{NFT})

        /// getSupportedNFTTypes returns a list of NFT types that this receiver accepts
        /// @return A dictionary of types mapped to booleans indicating if this
        ///         reciever supports it
        access(all) view fun getSupportedNFTTypes(): {Type: Bool}

        /// Returns whether or not the given type is accepted by the collection
        /// A collection that can accept any type should just return true by default
        /// @param type: An NFT type
        /// @return A boolean indicating if this receiver can recieve the desired NFT type
        access(all) view fun isSupportedNFTType(type: Type): Bool
    }

    /// Interface to enable a collection that stores NFTs
    access(all) resource interface CollectionPublic {
        access(all) view fun deposit(token: @{NonFungibleToken.NFT})
        access(all) view fun getIDs(): [UInt64]
        access(all) view fun borrowNFT(id: UInt64): &{NonFungibleToken.NFT}?
        access(all) view fun getSupportedNFTTypes(): {Type: Bool}
        access(all) view fun isSupportedNFTType(type: Type): Bool
    }

    /// Interface to enable a collection that stores NFTs
    access(all) resource interface Collection: Provider, Receiver, CollectionPublic {
        /// Returns the length of the collection
        access(all) view fun getLength(): Int
    }

    /// The total number of tokens of this type in existence
    access(all) view fun totalSupply(): UInt64

    /// The event that is emitted when a token is withdrawn from a Collection
    access(all) event Withdraw(id: UInt64, from: Address?)

    /// The event that is emitted when a token is deposited into a Collection
    access(all) event Deposit(id: UInt64, to: Address?)

    /// The Entitlements
    access(all) entitlement CollectionsGuardian
    access(all) entitlement ProxyWithdraw

    access(all) view fun resolveView(view: Type): AnyStruct?
}
