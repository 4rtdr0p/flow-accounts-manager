import ArtDropCore from "../contracts/ArtDropCore.cdc"
import NonFungibleToken from "../contracts/NonFungibleToken.cdc"

/// Executes an ArtDrop protocol transfer using the ProtocolTransfer entitlement.
transaction(certificateId: UInt64, to: Address) {
    let collection: auth(ArtDropCore.ProtocolTransfer) &ArtDropCore.Collection

    prepare(signer: auth(Storage) &Account) {
        self.collection = signer.storage
            .borrow<auth(ArtDropCore.ProtocolTransfer) &ArtDropCore.Collection>(
                from: ArtDropCore.CollectionStoragePath
            ) ?? panic("Could not borrow ProtocolTransfer-entitled collection from signer storage")
    }

    execute {
        let recipient = getAccount(to)
        let receiverRef = recipient.capabilities
            .borrow<&{NonFungibleToken.CollectionPublic}>(ArtDropCore.CollectionPublicPath)
            ?? panic("Could not borrow recipient's ArtDropCore collection")

        let nft <- self.collection.protocolTransfer(withdrawID: certificateId)
        receiverRef.deposit(token: <-nft)
    }
}
