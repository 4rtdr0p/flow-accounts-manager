/// release_escrow.cdc — Buyer releases escrow vault al treasury del protocolo.
///
/// Despues de activate_chip_and_settle (D6), el escrow queda Settled pero el
/// vault queda lockeado. El buyer llama esto para distribuir el vault de vuelta
/// al protocolo y marcar el escrow como Released.

import FungibleToken from 0x9a0766d93b6608b7
import "EscrowModule"
import "PaymentModule"

transaction(
    logicOwner: Address,
    escrowId: UInt64
) {
    prepare(signer: &Account) {
        let treasury = getAccount(logicOwner)
            .capabilities
            .borrow<&{FungibleToken.Receiver}>(EscrowModule.artDropVaultPublicPath)
            ?? panic("release_escrow: treasury receiver missing")

        let ctx = PaymentModule.DistributionContext(
            artistVault: treasury,
            platformVault: treasury,
            yieldVault: treasury,
            communityVault: nil
        )

        let escrowLogic = getAccount(logicOwner)
            .capabilities
            .borrow<&{EscrowModule.IEscrowLogic}>(EscrowModule.PublicPath)
            ?? panic("release_escrow: EscrowModule capability missing")

        escrowLogic.releaseEscrow(
            escrowId: escrowId,
            ctx: ctx,
            releaser: signer.address
        )
    }
}
