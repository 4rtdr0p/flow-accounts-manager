import NonFungibleToken from "../contracts/NonFungibleToken.cdc"
import ExampleNFT from "../contracts/ExampleNFT.cdc"

access(all)
view fun main(account: Address): [UInt64] {
    let receiver = getAccount(account)
        .capabilities
        .borrow<&{NonFungibleToken.CollectionPublic}>(ExampleNFT.CollectionPublicPath)
        ?? panic("Could not borrow a reference to the collection")

    return receiver.getIDs()
}
