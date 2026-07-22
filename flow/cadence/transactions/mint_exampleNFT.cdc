import NonFungibleToken from "../contracts/NonFungibleToken.cdc"
import ExampleNFT from "../contracts/ExampleNFT.cdc"

transaction(recipient: Address) {
    
    let minter: &ExampleNFT.NFTMinter
    
    prepare(signer: auth(Storage) &Account) {
        self.minter = signer.storage
            .borrow<&ExampleNFT.NFTMinter>(from: ExampleNFT.MinterStoragePath)
            ?? panic("Could not borrow a reference to the NFT minter")
    }

    execute {
        let recipientAccount = getAccount(recipient)
        
        let recipientCollection = recipientAccount
            .capabilities
            .borrow<&{NonFungibleToken.CollectionPublic}>(ExampleNFT.CollectionPublicPath)
            ?? panic("Could not get receiver reference to the NFT Collection")
        
        self.minter.mintNFT(
            recipient: recipientCollection,
            name: "",
            description: "",
            thumbnail: ""
        )
    }
}
