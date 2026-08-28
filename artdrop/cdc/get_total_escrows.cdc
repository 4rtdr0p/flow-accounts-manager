/// get_total_escrows.cdc — Returns ArtDropCore.getTotalEscrows(), the
/// monotonic count of every escrow ever created (via createEscrow or
/// createReEscrow). Used by the escrow projection's one-time backfill
/// (issue #102) to size the id range it walks with
/// get_all_escrow_summaries.cdc — see artdrop/service.go's
/// BackfillEscrowProjection.
import ArtDropCore from 0xec581a0282d99a1a

access(all) fun main(): UInt64 {
    return ArtDropCore.getTotalEscrows()
}
