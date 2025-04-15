import NonFungibleToken from "../contracts/NonFungibleToken.cdc"
import ExampleNFT from "../contracts/ExampleNFT.cdc"

transaction(recipient: Address, withdrawID: UInt64) {
    prepare(signer: auth(Storage) &Account) {
        let recipient = getAccount(recipient)
        
        let collectionRef = signer.storage
            .borrow<&ExampleNFT.Collection>(from: ExampleNFT.CollectionStoragePath)
            ?? panic("Could not borrow a reference to the owner's collection")

        let depositRef = recipient.capabilities
            .borrow<&{NonFungibleToken.CollectionPublic}>(ExampleNFT.CollectionPublicPath)
            ?? panic("Could not borrow a reference to the recipient's collection")
        
        let nft <- collectionRef.withdraw(withdrawID: withdrawID)
        
        depositRef.deposit(token: <-nft)
    }
}
