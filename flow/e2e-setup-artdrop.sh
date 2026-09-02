#!/usr/bin/env bash
# e2e-setup-artdrop.sh — Stand up a Tier-2 ArtDrop emulator for the wallet-api's
# `//go:build artdrop_e2e` suite: one emulator whose service account IS the
# ArtDrop deployer AND the wallet-api admin, all the protocol contracts deployed
# and wired, and the two delegated capabilities (ChipAdmin + EscrowVoid) plus
# ArtistOnboarding granted to that account.
#
# This drives the REAL, working artdrop-protocol deploy path
# (scripts/deploy_emulator.sh + scripts/setup_emulator.sh) rather than
# re-inventing a fragile deploy — the EscrowModule's two init-arg capability
# dance (C-ii) lives entirely inside those scripts. We only add the wallet-api
# specific cap grants on top.
#
# The three cross-repo blockers this solves:
#   C-i   shared service key: the emulator is started with --service-priv-key K
#         where K is artdrop-protocol's emulator-account.pkey, so signing as
#         `emulator-account` (deploy) and FLOW_WALLET_ADMIN_PRIVATE_KEY=K (the
#         wallet admin) are the SAME account 0xf8d6e0586b0a20c7. Every grant is
#         therefore a self-grant — the simplest possible wiring.
#   C-ii  EscrowModule's 2 init args: handled by deploy_emulator.sh's
#         setup_escrow_module + deploy_escrow_module inbox dance.
#   C-iii FungibleToken/NonFungibleToken/MetadataViews addresses: written into
#         the env file below so the wallet rewrites the scripts' testnet import
#         addresses to the emulator's (see artdrop/cdc.go substituteAddresses).
#
# On success it leaves the emulator running (pidfile flow/e2e-emulator.pid) and
# writes flow/e2e-artdrop.env with the FLOW_WALLET_* the Go suite needs. The
# Makefile `test-artdrop-e2e` target sources that env and runs the tagged tests.
#
# Usage (from the flow-accounts-manager repo root):
#   bash flow/e2e-setup-artdrop.sh
#   set -a && . flow/e2e-artdrop.env && set +a
#   go test -tags artdrop_e2e . ./artdrop/... -p 1
# Or just: make test-artdrop-e2e
set -euo pipefail

# ── Paths / constants ─────────────────────────────────────────────────────────
WALLET_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"           # repo root
FLOW_DIR="$WALLET_DIR/flow"
PROTOCOL_DIR="${ARTDROP_PROTOCOL_DIR:-$WALLET_DIR/../artdrop-protocol}"
FLOW_BIN="${FLOW_BIN:-flow}"

# A == the single emulator service account (deployer == wallet admin).
A_ADDR="0xf8d6e0586b0a20c7"
# The protocol's emulator-account private key. deploy_emulator.sh signs with it
# (from its flow.json .pkey), so the running emulator's service key MUST match.
SERVICE_KEY_FILE="${ARTDROP_SERVICE_KEY_FILE:-/home/oydual3/emulator-account.pkey}"
PIDFILE="$FLOW_DIR/e2e-emulator.pid"
ENVFILE="$FLOW_DIR/e2e-artdrop.env"
EMU_LOG="${ARTDROP_EMU_LOG:-/tmp/artdrop-e2e-emu.log}"
PORT="${ARTDROP_EMU_PORT:-3569}"

# Emulator addresses of the standard Flow contracts (differ from testnet).
FT_ADDR="0xee82856bf20e2aa6"
NFT_ADDR="0xf8d6e0586b0a20c7"
MDV_ADDR="0xf8d6e0586b0a20c7"

log() { printf '\n=== %s ===\n' "$*"; }

if [[ ! -f "$SERVICE_KEY_FILE" ]]; then
  echo "FATAL: protocol service key not found at $SERVICE_KEY_FILE" >&2
  echo "       (set ARTDROP_SERVICE_KEY_FILE to artdrop-protocol's emulator-account.pkey)" >&2
  exit 1
fi
if [[ ! -x "$PROTOCOL_DIR/scripts/deploy_emulator.sh" && ! -f "$PROTOCOL_DIR/scripts/deploy_emulator.sh" ]]; then
  echo "FATAL: artdrop-protocol deploy scripts not found under $PROTOCOL_DIR" >&2
  echo "       (set ARTDROP_PROTOCOL_DIR to the sibling artdrop-protocol checkout)" >&2
  exit 1
fi

K="$(tr -d ' \n\r' < "$SERVICE_KEY_FILE")"
K="${K#0x}"

# ── C-i: fresh emulator with the shared service key ───────────────────────────
log "Starting emulator on :$PORT with the protocol service key (C-i)"
if [[ -f "$PIDFILE" ]]; then
  kill "$(cat "$PIDFILE")" 2>/dev/null || true
  rm -f "$PIDFILE"
  sleep 1
fi
# Start from the protocol dir so its flow.json networks/accounts are in scope for
# the deploy that follows.
( cd "$PROTOCOL_DIR" && nohup "$FLOW_BIN" emulator --port "$PORT" --service-priv-key "$K" > "$EMU_LOG" 2>&1 & echo $! > "$PIDFILE" )
echo "emulator pid $(cat "$PIDFILE"), log $EMU_LOG"

# Wait for the gRPC API to answer.
ready=0
for _ in $(seq 1 60); do
  if ( cd "$PROTOCOL_DIR" && "$FLOW_BIN" blocks get latest --network emulator >/dev/null 2>&1 ); then
    ready=1; break
  fi
  sleep 0.5
done
if [[ "$ready" != 1 ]]; then
  echo "FATAL: emulator did not become ready; log tail:" >&2
  tail -20 "$EMU_LOG" >&2 || true
  exit 1
fi

# ── C-ii: deploy + wire all protocol contracts (the protocol's own path) ──────
log "Deploying ArtDrop protocol contracts (artdrop-protocol/scripts/deploy_emulator.sh — C-ii)"
( cd "$PROTOCOL_DIR" && bash scripts/deploy_emulator.sh )

log "Wiring the protocol (artdrop-protocol/scripts/setup_emulator.sh)"
# Note: setup_emulator.sh's "step 2/5 setup_artist_direct (claim)" reverts on
# this single-account emulator (the deployer's AdminStoragePath already holds
# the real ProtocolAdmin resource, so the static WalletAPI-ArtistDirect claim
# has nowhere to store its capability). That's harmless here: this suite uses
# DYNAMIC artist onboarding (a separate artist account + the ArtistOnboarding
# cap granted below, stored at a different path), so the static claim is unused.
( cd "$PROTOCOL_DIR" && bash scripts/setup_emulator.sh )

# ── Wallet-api delegated capabilities (self-grants; A == deployer == wallet) ──
send() {
  ( cd "$PROTOCOL_DIR" && "$FLOW_BIN" transactions send "$1" "${@:2}" \
      --signer emulator-account --network emulator )
}

# setup_emulator.sh wires only the registry hub + chip OWNER index. The chip
# public-key / certificate indexes and the escrow query/counter indexes that
# createEscrow, voidEscrow and the escrow reads depend on are each a separate
# one-time setup — run the full set here.
log "Setting up the chip + escrow registry indexes"
for idx in \
  setup_chip_public_key_index \
  setup_chip_certificate_index \
  setup_escrows_by_certificate_index \
  setup_escrows_by_buyer_index \
  setup_escrows_by_edition_index \
  setup_escrow_status_counter_index; do
  send "transactions/registry/${idx}.cdc"
done

# The EscrowLogic needs a destination the released/voided 5% FLOW returns to.
# On this single-account emulator that's the service account's own FLOW
# receiver (the "ArtDrop vault").
log "Setting the escrow destination (released FLOW returns here)"
( cd "$PROTOCOL_DIR" && "$FLOW_BIN" transactions send transactions/admin/set_escrow_destination.cdc \
    --signer emulator-account --network emulator \
    --args-json "[{\"type\":\"Address\",\"value\":\"$A_ADDR\"},{\"type\":\"Path\",\"value\":{\"domain\":\"public\",\"identifier\":\"flowTokenReceiver\"}}]" )

log "Granting ArtistOnboarding to the wallet admin (dynamic artist onboarding)"
send transactions/setup/setup_artist_onboarding_cap.cdc "$A_ADDR"
send transactions/setup/setup_artist_onboarding_claim.cdc "$A_ADDR"

log "Granting + claiming the delegated ChipAdmin capability"
send transactions/admin/grant_chip_admin_cap.cdc "$A_ADDR"
send transactions/setup/claim_chip_admin_cap.cdc "$A_ADDR"

log "Granting + claiming the delegated EscrowVoid capability"
send transactions/admin/grant_escrow_void_cap.cdc "$A_ADDR"
send transactions/setup/claim_escrow_void_cap.cdc "$A_ADDR"

# ── Env for the Go suite ──────────────────────────────────────────────────────
log "Writing $ENVFILE"
cat > "$ENVFILE" <<EOF
# Generated by flow/e2e-setup-artdrop.sh — source before the artdrop_e2e suite.
# Admin == ArtDrop deployer == 0xf8d6e0586b0a20c7 (single-account emulator).
FLOW_WALLET_ADMIN_ADDRESS=$A_ADDR
FLOW_WALLET_ADMIN_PRIVATE_KEY=$K
FLOW_WALLET_ACCESS_API_HOST=127.0.0.1:$PORT
FLOW_WALLET_CHAIN_ID=flow-emulator

# All ArtDrop contracts live on the single service account.
FLOW_WALLET_ARTDROP_CORE_ADDRESS=$A_ADDR
FLOW_WALLET_ARTDROP_REGISTRY_ADDRESS=$A_ADDR
FLOW_WALLET_ARTDROP_ESCROW_MODULE_ADDRESS=$A_ADDR
FLOW_WALLET_ARTDROP_PAYMENT_MODULE_ADDRESS=$A_ADDR
FLOW_WALLET_ARTDROP_LOGIC_OWNER_ADDRESS=$A_ADDR

# Standard Flow contracts on the emulator (C-iii): the wallet rewrites the
# scripts' hardcoded testnet import addresses to these.
FLOW_WALLET_ARTDROP_FUNGIBLE_TOKEN_ADDRESS=$FT_ADDR
FLOW_WALLET_ARTDROP_NON_FUNGIBLE_TOKEN_ADDRESS=$NFT_ADDR
FLOW_WALLET_ARTDROP_METADATA_VIEWS_ADDRESS=$MDV_ADDR

# Ixkio runs in bypass on the emulator (no real tap / hardware needed).
FLOW_WALLET_ARTDROP_IXKIO_ENABLED=false
# Fixed FLOW/USD price so the purchase path uses the FixedPriceOracle instead of
# the mainnet Pyth RPC (only relevant if a test exercises purchases:charge).
FLOW_WALLET_DEV_FLOW_USD_PRICE=0.02
EOF

log "Tier-2 ArtDrop emulator ready"
echo "  emulator pidfile: $PIDFILE"
echo "  env file:         $ENVFILE"
echo "  stop with:        kill \$(cat $PIDFILE) && rm $PIDFILE"
