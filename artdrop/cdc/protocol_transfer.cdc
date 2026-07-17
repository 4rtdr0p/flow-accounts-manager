/// protocol_transfer.cdc - ADMIN: protocol-initiated certificate transfer.
///
/// Bypasses MarketMode to move a Certificate between accounts. The signer
/// must hold auth(ProtocolTransfer) &ProtocolTransferAuthority (stored at
/// ArtDropCore.ProtocolTransferStoragePath in the deployer account).
///
/// The sender must have registered an auth(NonFungibleToken.Withdraw)
/// capability via ArtDropCore.registerProviderCap().
/// The receiver must have a Collection at ArtDropCore.CertCollectionPublicPath.
///
/// Day-1 mono-account: the deployer signs.
/// Cross-account (future): issue the capability via inbox first.

import ArtDropCore from 0xe2f96cbbdfde8c9f

transaction(certificateId: UInt64, from: Address, to: Address) {
    prepare(signer: auth(BorrowValue) &Account) {
        let protoTransfer = signer.storage
            .borrow<auth(ArtDropCore.ProtocolTransfer) &ArtDropCore.ProtocolTransferAuthority>(
                from: ArtDropCore.ProtocolTransferStoragePath
            ) ?? panic("protocol_transfer: ProtocolTransferAuthority not found - must be called from the ArtDropCore deployer account")

        protoTransfer.protocolTransfer(
            certificateId: certificateId,
            from: from,
            to: to
        )
    }
}
