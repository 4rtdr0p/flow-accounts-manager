/// cancel_escrow.cdc — Buyer cancela escrow antes de unlockAt.
///
/// El buyer cancela el escrow antes de que expire el timelock.
/// El vault se devuelve al buyer.

import FungibleToken from 0x9a0766d93b6608b7
import EscrowModule from 0x1bfedfa0ec66c23e

transaction(
    logicOwner: Address,
    escrowId: UInt64
) {
    prepare(signer: auth(BorrowValue) &Account) {
        let vault = signer.storage.borrow<&{FungibleToken.Receiver}>(
            from: /storage/flowTokenVault
        ) ?? panic("cancel_escrow: flowTokenVault not found")

        let escrowLogic = getAccount(logicOwner)
            .capabilities
            .borrow<&{EscrowModule.IEscrowLogic}>(EscrowModule.PublicPath)
            ?? panic("cancel_escrow: EscrowModule capability missing")

        escrowLogic.cancel(
            escrowId: escrowId,
            buyerVault: vault,
            buyer: signer.address
        )
    }
}
