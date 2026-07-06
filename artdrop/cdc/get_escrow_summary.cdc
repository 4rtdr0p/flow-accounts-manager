import "EscrowModule"

access(all) fun main(escrowId: UInt64): UInt8 {
    let acct = getAccount(0xf73e0e516e336e9f)
    let cap = acct.capabilities.get<&{EscrowModule.IEscrowLogic}>(EscrowModule.PublicPath)
    let ref = cap.borrow()
        ?? panic("Could not borrow EscrowLogic reference")
    return ref.borrowEscrowReadOnly(escrowId: escrowId).status
}
