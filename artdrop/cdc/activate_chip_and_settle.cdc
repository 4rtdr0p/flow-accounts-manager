/// activate_chip_and_settle.cdc — E-01 (D6): atomic chip activation + escrow settlement + certificate transfer.
///
/// The signer is the buyer (chip tapper). Requires:
/// - Escrow in Pending status, owned by the signer (caller == buyer)
/// - Valid challenge + signature against the registered chipPubKey
///
/// certificateId and its current owner are no longer transaction inputs —
/// escrow-lifecycle redesign (2026-08) derives both from the escrow's own
/// on-chain state (EscrowModule.runActivateChipAndSettle uses
/// escrowRead.certificateId / escrowRead.seller / escrowRead.buyer). That
/// was the fix for a certificate-theft vulnerability: a caller used to be
/// able to pass an arbitrary certificateId/certificateOwner here.
///
/// Atomic: signature verify + protocol transfer of the NFT to the buyer +
/// markEscrowClaimed (and, if still Pending, the fund release) happen
/// together or the whole transaction reverts.

import ArtDropCore from 0xec581a0282d99a1a
import EscrowModule from 0x1bfedfa0ec66c23e

transaction(
    logicOwner: Address,
    escrowId: UInt64,
    challenge: String,
    signature: [UInt8]
) {
    prepare(signer: &Account) {
        let escrowLogic = getAccount(logicOwner)
            .capabilities
            .borrow<&{EscrowModule.IEscrowLogic}>(EscrowModule.PublicPath)
            ?? panic("activate_chip_and_settle: EscrowModule capability missing")

        escrowLogic.activateChipAndSettle(
            escrowId: escrowId,
            challenge: challenge,
            signature: signature,
            activator: signer.address
        )
    }
}
