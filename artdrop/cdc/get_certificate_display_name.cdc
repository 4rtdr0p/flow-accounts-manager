import ArtDropCore from 0xec581a0282d99a1a
import "MetadataViews"

access(all)
fun main(address: Address, id: UInt64): String? {
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

    let view = nft!.resolveView(Type<MetadataViews.Display>()) as? MetadataViews.Display
    if view == nil {
        return nil
    }

    return view!.name
}
