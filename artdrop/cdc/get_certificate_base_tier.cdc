import ArtDropCore from 0xec581a0282d99a1a

access(all)
fun main(address: Address, id: UInt64): UFix64? {
    let collection = getAccount(address)
        .capabilities
        .borrow<&ArtDropCore.Collection>(ArtDropCore.CertCollectionPublicPath)
    if collection == nil {
        return nil
    }

    let cert = collection!.borrowNFT(id) as? &ArtDropCore.Certificate
    if cert == nil {
        return nil
    }

    return cert!.baseTier
}
