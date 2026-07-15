import ArtDropCore from 0x050dd2bfe6cd6421

access(all)
fun main(address: Address, id: UInt64): UFix64? {
    let collection = getAccount(address)
        .capabilities
        .borrow<&ArtDropCore.Collection>(ArtDropCore.CertCollectionPublicPath)
        ?? panic("missing certificate collection capability")

    let cert = collection.borrowNFT(id) as? &ArtDropCore.Certificate
        ?? panic("missing certificate")

    return cert.finalMultiplier
}
