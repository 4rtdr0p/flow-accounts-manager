/// protocol_transfer.cdc - ADMIN: protocol-initiated certificate transfer.
///
/// Bypasses MarketMode to move a Certificate between accounts. The signer
/// must hold auth(ProtocolTransfer) &ProtocolTransferAuthority.
///
/// The sender must have registered an auth(NonFungibleToken.Withdraw)
/// capability via ArtDropCore.registerProviderCap().
/// The receiver must have a Collection at ArtDropCore.CertCollectionPublicPath.
///
/// Cross-account: the wallet-api account claims this capability (issued by
/// the ArtDropCore deployer via inbox) and stores it at the custom path
/// "WalletAPIProtocolTransfer" -- see transactions/setup/claim_protocol_transfer_cap.cdc
/// in artdrop-protocol. ArtDropCore.ProtocolTransferStoragePath is occupied
/// by the deployer's own native ProtocolTransferAuthority resource, so a
/// non-deployer signer (like wallet-api) cannot use that path.

import ArtDropCore from 0xec581a0282d99a1a

transaction(certificateId: UInt64, from: Address, to: Address) {
    prepare(signer: auth(BorrowValue) &Account) {
        let protoTransferPath = StoragePath(identifier: "WalletAPIProtocolTransfer")!
        let protoTransfer = signer.storage
            .borrow<auth(ArtDropCore.ProtocolTransfer) &ArtDropCore.ProtocolTransferAuthority>(
                from: protoTransferPath
            ) ?? panic("protocol_transfer: ProtocolTransferAuthority not found at 'WalletAPIProtocolTransfer' - run claim_protocol_transfer_cap.cdc first")

        protoTransfer.protocolTransfer(
            certificateId: certificateId,
            from: from,
            to: to
        )
    }
}
