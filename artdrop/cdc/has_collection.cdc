import ArtDropCore from 0x050dd2bfe6cd6421

access(all)
fun main(address: Address): Bool {
    let collection = getAccount(address).capabilities
        .borrow<&ArtDropCore.Collection>(ArtDropCore.CertCollectionPublicPath)
    return collection != nil
}
