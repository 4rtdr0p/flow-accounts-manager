import ArtDropCore from 0xe2f96cbbdfde8c9f
import NonFungibleToken from 0x631e88ae7f1d7c20

access(all) fun main(addr: Address): [UInt64] {
    let collection = getAccount(addr).capabilities
        .borrow<&ArtDropCore.Collection>(ArtDropCore.CertCollectionPublicPath)
    if collection == nil { return [] }
    let ids = collection!.getIDs()
    return ids
}
