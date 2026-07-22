import ArtDropCore from 0xec581a0282d99a1a

access(all)
fun main(address: Address, id: UInt64): [UInt8]? {
    let collection = getAccount(address)
        .capabilities
        .borrow<&ArtDropCore.Collection>(ArtDropCore.CertCollectionPublicPath)
    if collection == nil {
        return nil
    }

    let nft = collection!.borrowNFT(id) as? &ArtDropCore.Certificate
    if nft == nil {
        return nil
    }

    return nft!.getChipPubKeyBytes()
}
