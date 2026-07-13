import ArtDropCore from 0x050dd2bfe6cd6421

access(all) fun main(escrowId: UInt64): UInt8 {
    let summary = ArtDropCore.getEscrowSummary(id: escrowId)
        ?? panic("get_escrow_status: escrow not found")
    return summary.status.rawValue
}
