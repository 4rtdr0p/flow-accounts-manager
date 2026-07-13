import "ArtDropCore"
import "MetadataViews"

access(all)
fun main(address: Address, id: UInt64): String {
    let collection = getAccount(address)
        .capabilities
        .borrow<&ArtDropCore.Collection>(ArtDropCore.CertCollectionPublicPath)
        ?? panic("missing certificate collection capability")

    let nft = collection.borrowNFT(id) as? &ArtDropCore.Certificate
        ?? panic("missing certificate")

    let view = nft.resolveView(Type<MetadataViews.Display>()) as? MetadataViews.Display
        ?? panic("missing display view")

    return view.name
}
