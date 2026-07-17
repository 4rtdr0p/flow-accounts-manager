import ArtDropCore from 0xe2f96cbbdfde8c9f

access(all)
fun main(address: Address): Bool {
    let collection = getAccount(address).capabilities
        .borrow<&ArtDropCore.Collection>(ArtDropCore.CertCollectionPublicPath)
    return collection != nil
}
