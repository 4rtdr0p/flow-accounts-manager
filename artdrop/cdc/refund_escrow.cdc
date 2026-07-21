/// refund_escrow.cdc — Buyer refund despues de unlockAt.
///
/// El buyer recupera el vault del escrow cuando el timelock expiro.
/// Solo funciona si unlockAt ya paso y el escrow sigue en Pending.

import FungibleToken from 0x9a0766d93b6608b7
import EscrowModule from 0x1bfedfa0ec66c23e

transaction(
    logicOwner: Address,
    escrowId: UInt64
) {
    prepare(signer: auth(BorrowValue) &Account) {
        let vault = signer.storage.borrow<&{FungibleToken.Receiver}>(
            from: /storage/flowTokenVault
        ) ?? panic("refund_escrow: flowTokenVault not found")

        let escrowLogic = getAccount(logicOwner)
            .capabilities
            .borrow<&{EscrowModule.IEscrowLogic}>(EscrowModule.PublicPath)
            ?? panic("refund_escrow: EscrowModule capability missing")

        escrowLogic.refund(
            escrowId: escrowId,
            buyerVault: vault,
            caller: signer.address
        )
    }
}
