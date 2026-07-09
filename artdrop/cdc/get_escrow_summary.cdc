import "EscrowModule"

access(all) fun main(logicOwner: Address, escrowId: UInt64): UInt8 {
    let acct = getAccount(logicOwner)
    let cap = acct.capabilities.get<&{EscrowModule.IEscrowLogic}>(EscrowModule.PublicPath)
    let ref = cap.borrow()
        ?? panic("Could not borrow EscrowLogic reference")
    return ref.borrowEscrowReadOnly(escrowId: escrowId).status
}
