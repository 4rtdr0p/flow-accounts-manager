/// create_escrow.cdc — Protocol crea un escrow en Pending status.
///
/// El buyer paga 100% off-chain (Stripe). El protocolo toma el 5%,
/// lo convierte a FLOW, y firma esta transacción para crear el escrow.
/// El seller recibe el Certificate cuando el buyer active el chip (D6).
///
/// chipPubKey is no longer a parameter here (chip-registry redesign,
/// 2026-08-19) — EscrowModule.createEscrow looks the chip's public key up
/// from ArtDropRegistry.ChipPublicKeyIndex by chipId, panicking loudly if
/// the chip hasn't been provisioned. Registration/replacement is a
/// separate, OperationalAdmin-gated, one-time-per-chip operation — not
/// something this transaction or the wallet API can do.

import FungibleToken from 0x9a0766d93b6608b7
import ArtDropCore from 0xec581a0282d99a1a
import EscrowModule from 0x1bfedfa0ec66c23e

transaction(
    logicOwner: Address,
    buyer: Address,
    seller: Address,
    editionId: UInt64,
    chipId: String,
    unlockAt: UFix64,
    nonce: UInt64,
    amount: UFix64,
    vaultIdentifier: String
) {
    prepare(signer: auth(BorrowValue, FungibleToken.Withdraw) &Account) {
        let vaultPath = StoragePath(identifier: vaultIdentifier)!
        let vault = signer.storage.borrow<auth(FungibleToken.Withdraw) &{FungibleToken.Vault}>(
            from: vaultPath
        ) ?? panic("create_escrow: vault not found at path")

        let payment <- vault.withdraw(amount: amount)

        let escrowLogic = getAccount(logicOwner)
            .capabilities
            .borrow<&{EscrowModule.IEscrowLogic}>(EscrowModule.PublicPath)
            ?? panic("create_escrow: EscrowModule capability missing")

        let escrowId = escrowLogic.createEscrow(
            buyer: buyer,
            seller: seller,
            editionId: editionId,
            chipId: chipId,
            unlockAt: unlockAt,
            nonce: nonce,
            payment: <-payment
        )

        log("Escrow created with id: ".concat(escrowId.toString()))
    }
}
