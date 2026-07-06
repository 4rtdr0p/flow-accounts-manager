import "ArtDropCore"
import "NonFungibleToken"

access(all) fun main(addr: Address): [UInt64] {
    let collection = getAccount(addr).capabilities
        .borrow<&ArtDropCore.Collection>(ArtDropCore.CertCollectionPublicPath)
    if collection == nil { return [] }
    let ids = collection!.getIDs()
    return ids
}
