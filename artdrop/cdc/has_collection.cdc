import ArtDropCore from 0xec581a0282d99a1a

access(all)
fun main(address: Address): Bool {
    let collection = getAccount(address).capabilities
        .borrow<&ArtDropCore.Collection>(ArtDropCore.CertCollectionPublicPath)
    return collection != nil
}
