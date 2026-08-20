/// re_escrow.cdc — Protocol crea un nuevo escrow sobre un Certificate YA
/// minteado ([SECURITY] issue #175 / diseño de re-escrow sin huérfanos).
///
/// Cierra el hueco del timeout normal: cuando el buyer nunca reclama, el
/// escrow expira, el 5% vuelve, y el certificado queda varado con el
/// seller para siempre — la única forma de re-vender esa pieza era
/// create_escrow.cdc, que siempre mintea un certificado NUEVO. Este
/// transaction reutiliza el certificado existente sin mintear nada.
///
/// certificateId reemplaza editionId — el contrato deriva editionId del
/// certificado mismo, nunca del caller.

import FungibleToken from 0x9a0766d93b6608b7
import ArtDropCore from 0xec581a0282d99a1a
import EscrowModule from 0x1bfedfa0ec66c23e

transaction(
    logicOwner: Address,
    buyer: Address,
    seller: Address,
    certificateId: UInt64,
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
        ) ?? panic("re_escrow: vault not found at path")

        let payment <- vault.withdraw(amount: amount)

        let escrowLogic = getAccount(logicOwner)
            .capabilities
            .borrow<&{EscrowModule.IEscrowLogic}>(EscrowModule.PublicPath)
            ?? panic("re_escrow: EscrowModule capability missing")

        let escrowId = escrowLogic.createReEscrow(
            buyer: buyer,
            seller: seller,
            certificateId: certificateId,
            chipId: chipId,
            unlockAt: unlockAt,
            nonce: nonce,
            payment: <-payment
        )

        log("Re-escrow created with id: ".concat(escrowId.toString()))
    }
}
