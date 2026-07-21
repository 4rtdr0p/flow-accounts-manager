# Testnet API verification — `flow-accounts-manager` REST API end-to-end

> **Scope.** End-to-end exercise of the freshly-deployed `flow-accounts-manager`
> REST API against the live Flow **testnet** multi-account ArtDrop V2 deploy.
> Every request below was made with `curl` against the live service. The
> orchestrator session confirmed before this run that the underlying Cadence
> contracts (testnet-a=`0xec581a0282d99a1a` for Core/Registry/Events,
> testnet-c=`0x1bfedfa0ec66c23e` for Escrow/Payment/Multiplier/Loyalty, etc.)
> are fully deployed and working when driven directly via the `flow` CLI
> (`artdrop-protocol/docs/flows-testnet.md`). This document replicates that
> coverage using only HTTP against the REST API.
>
> **Service under test.** Base URL:
> `https://artdrop-production-artdrop.svc-us5.zcloud.ws` (HTTPS only; the
> plain-`http` variant returns 308 to HTTPS). All paths prefixed `/v1`.
> **`AUTH_ENABLED` is `false`** in the current deploy — every endpoint below
> was hit with no `Authorization` header. **This is a real security gap for a
> service that will eventually hold real funds** (see "Warnings" at the
> bottom).
>
> **Live state at start.** Confirmed `GET /v1/accounts` returned the wallet-api
> admin account `0x0680880ab9e7b676` (auto-created on this fresh boot; the
> Postgres `testnet-clean` DB was truncated before deploy, only this row was
> auto-seeded). Postgres `transactions` / `jobs` tables were empty.

## Pre-existing protocol state observed (read-only probes)

Before exercising the write paths, sanity-checked the read-only global config
and the registry-derived data — these are the endpoints just-fixed in a prior
session (the `is-artist` fix targeting `ArtDropRegistry.IArtistIndex.isArtist`).

### `GET /v1/health/ready`

```bash
curl -sS https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/health/ready
# empty body, 200 OK
```

### `GET /v1/health/liveness`

```bash
curl -sS https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/health/liveness
# {"jobsInit":0,"jobsNotAccepted":0,"jobsAccepted":0,"jobsErrored":0,
#  "jobsFailed":0,"jobsCompleted":0,"poolCapacity":1000,"workerCount":1}
```

Both healthy.

### `GET /v1/artdrop/config/platform-fee`

```bash
curl -sS https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/artdrop/config/platform-fee
# {"fee":"0.00000000"}
```

> **Note (not a bug, just documenting):** the on-chain platform fee read here
> is `0.0` (i.e. zero basis-points effective). The contract-side enforcement
> logic is unchanged — this is whatever the live value is on testnet-a.
> `flows-testnet.md` does not specifically validate this value.

### `GET /v1/artdrop/config/market-mode`

```bash
curl -sS https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/artdrop/config/market-mode
# {"mode":"Open"}
```

> **`Open`, not `PrimaryOnly`.** This confirms governance testing in the
> prior session DID flip the mode on this fresh deploy (Day-1 default is
> `PrimaryOnly`; see `artdrop-protocol/docs/flows-testnet.md` §5). Not a bug,
> just reality of the current chain state. Will note in summary.

### `GET /v1/accounts`

```bash
curl -sS https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/accounts
# [{"address":"0x0680880ab9e7b676","keys":null,"type":"custodial",
#   "createdAt":"2026-07-21T06:20:40.863141Z",
#   "updatedAt":"2026-07-21T06:20:40.863141Z"}]
```

Confirms clean slate: only the wallet-api admin row exists. **Verdict: ✅
works as expected.**

### `GET /v1/accounts/{address}/artdrop/is-artist` (wallet-api)

```bash
curl -sS https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/accounts/0x0680880ab9e7b676/artdrop/is-artist
# {"isArtist":false}
```

> **End-to-end confirmation of the `is_artist` fix.** This is the endpoint
> that was just changed to call `ArtDropRegistry.IArtistIndex.isArtist`
> instead of the bogus "any originals" check. For the admin wallet-api
> account (which never created an Original — it ran the admin
> `create_original.cdc` on behalf of the artist) the result is correctly
> `false`. The test artist `0x0daaba937562c85f` from prior testing IS an
> artist (it has 1 Original), and we'll re-confirm that further down.

### `GET /v1/artdrop/originals/1`

```bash
curl -sS https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/artdrop/originals/1
# {"id":1,"name":"Testnet Original #1","artistName":""}
```

> **Note (worth flagging):** `artistName` is empty here. From the contract
> side, `Original` stores the artist as an `Address` (per
> `artdrop-protocol/contracts/core/ArtDropCore.cdc`). The handler in
> `artdrop/service.go:GetOriginalSummary` only extracts the `name` field
> from the returned struct, not the artist address. Either the script
> (`get_original_summary.cdc`) doesn't expose `artist`, or the handler
> drops it. Either way, the current API silently returns empty
> `artistName` — a frontend would have to look up the artist address by
> some other means. **Will note as a documentation/gap item.**

### `GET /v1/artdrop/editions/1`

```bash
curl -sS https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/artdrop/editions/1
# {"id":1,"state":0,"totalMinted":3,"maxSupply":0}
```

`state=0` is `EditionState.Pending` (per the Cadence contract enum); not
yet activated on this deploy — matches `flows-testnet.md` §1.3 which
walks through the activate step separately. **Verdict: ✅ works as
expected.**

### `GET /v1/accounts/{address}/artdrop/certificates` (wallet-api)

```bash
curl -sS https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/accounts/0x0680880ab9e7b676/artdrop/certificates
# []
```

Empty, as expected — the admin wallet-api doesn't hold certificates.

### `GET /v1/accounts/{address}/artdrop/collection-length` (wallet-api)

```bash
curl -sS https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/accounts/0x0680880ab9e7b676/artdrop/collection-length
# {"length":0}
```

### `GET /v1/accounts/{address}/artdrop/escrows/{id}?logic_owner=...` (wallet-api)

```bash
curl -sS "https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/accounts/0x0680880ab9e7b676/artdrop/escrows/1?logic_owner=0x1bfedfa0ec66c23e"
# {"id":1,"status":1}
curl -sS "https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/accounts/0x0680880ab9e7b676/artdrop/escrows/2?logic_owner=0x1bfedfa0ec66c23e"
# {"id":2,"status":3}
```

Escrow 1 is status `1` (Pending) and escrow 2 is status `3` (presumably
Settled — there's a fresh escrow layout from prior testing). Both reads
work. **`{address}` in the URL is just for routing**; the script
(`get_escrow_summary.cdc`) takes `escrowId` only and is anchored at the
registry/Core via `logic_owner`. So any address works for `{address}` as
long as the address is well-formed. **Verdict: ✅ works as expected.**

---

## §A. Account creation (custodial)

Created three new custodial accounts via `POST /v1/accounts` (sync mode)
to back the artist / buyer roles. Test names + addresses:

| Role | Address |
|---|---|
| artist (new) | `0xee20358d6d32ea57` |
| buyer (new)  | `0x0a478c507cc8ea88` |
| chip (new)   | `0xe7c08bb2d29350d2` |
| wallet-api (admin, pre-existing) | `0x0680880ab9e7b676` |

```bash
curl -sS -X POST -H 'Content-Type: application/json' \
  "https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/accounts?sync=true"
# → 201
# {"address":"0xee20358d6d32ea57","keys":[{"index":0,"type":"local",
#   "publicKey":"0xd2e6...1906","signAlgo":"ECDSA_P256","hashAlgo":"SHA3_256",
#   "createdAt":"2026-07-21T06:28:05.556388171Z",
#   "updatedAt":"2026-07-21T06:28:05.556388171Z"}],
#  "type":"custodial","createdAt":"2026-07-21T06:28:05.554520627Z",
#  "updatedAt":"2026-07-21T06:28:05.554520627Z"}

curl -sS -X POST -H 'Content-Type: application/json' \
  "https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/accounts?sync=true"
# → 201 → 0x0a478c507cc8ea88

curl -sS -X POST -H 'Content-Type: application/json' \
  "https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/accounts?sync=true"
# → 201 → 0xe7c08bb2d29350d2
```

> **Note on the "chip" account (not a real protocol concept).** I created a
> custodial "chip" account on the off-chance the API would need it, but on
> reading the contract code (`EscrowModule.cdc` and `activate_chip_and_settle.cdc`)
> a chip is **not a Flow account** — it's just an off-chain ECDSA P-256 keypair
> whose 64-byte uncompressed point (sans the `0x04` prefix) is stored in the
> escrow's `chipPubKey` field. The signature the buyer provides at activation
> time must verify against that pubkey. So this third custodial account is
> unused; we'll generate a chip keypair externally (Python helper, see §G).
> Will flag for the frontend team in the proposed GitHub issues.

The `sync=true` query param is honored — the response is the new account
object directly, not a job envelope. **Verdict: ✅ works as expected.**

---

## §B. Account setup (vaults + ArtDrop collection)

### `POST /v1/accounts/{address}/setup` (the legacy `example` plugin wrapper)

```bash
curl -sS -i -X POST -H 'Content-Type: application/json' \
  "https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/accounts/0xee20358d6d32ea57/setup?sync=true"
```

Returns `500` with a Cadence preprocess error.

> **🔴 ROOT-CAUSED — `/setup` is broken on testnet (and mainnet).** The
> bundled script `example/setup_example.cdc` hardcodes emulator contract
> addresses:
>
> ```cadence
> import FungibleToken from 0xee82856bf20e2aa6    // ← emulator
> import FlowToken from 0x0ae53cb6e3f42a79         // ← emulator
> import FUSD from 0xf8d6e0586b0a20c7              // ← emulator
> import NonFungibleToken from 0xf8d6e0586b0a20c7  // ← emulator
> import ExampleNFT from 0xf8d6e0586b0a20c7        // ← emulator
> ```
>
> Those addresses don't exist on Flow testnet (testnet's FlowToken is at
> `0x7e60df042a9c0868`, FungibleToken at `0x9a0766d93b6608b7`,
> NonFungibleToken at `0x631e88ae7f1d7c20`). The Cadence compiler emits a
> cascade of "cannot find declaration `FlowToken` in `0x0ae53cb6e3f42a79.FlowToken`"
> and the type-inference errors that follow — the "type parameter T" / "empty
> intersection type" errors are downstream consequences of the missing
> imports, not the root cause.
>
> This is the same script `flow/cadence/transactions/setup_artdrop_account.cdc`
> references — also broken on testnet for the same reason. The fix is
> to make the contract addresses configurable (env-driven), not hardcoded,
> or to use testnet addresses for the testnet build. **Not blocking this
> session** because `/artdrop/setup` (next subsection) does the real work
> the protocol needs, and the buyer/artist can get a FLOW vault via the
> generic `/transactions` raw-tx endpoint with a testnet-correct script.
> **Flagged for the frontend team: don't depend on `/setup` for testnet or
> mainnet.**

### `POST /v1/accounts/{address}/artdrop/setup` (the artdrop plugin's own setup)

```bash
curl -sS -i -X POST -H 'Content-Type: application/json' \
  "https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/accounts/0xee20358d6d32ea57/artdrop/setup?sync=true"
# → 201
# {"transactionId":"ccbda833cabaa41fd141ba5386b9fe70d4ca3e83b532cbbb0371b87b71c3e2d8",
#  "transactionType":"ArtdropSetup",
#  "events":[
#    {"Type":"flow.StorageCapabilityControllerIssued", ...},
#    {"Type":"A.7e60df042a9c0868.FlowToken.TokensWithdrawn",  ... fee},
#    {"Type":"A.9a0766d93b6608b7.FungibleToken.Withdrawn",    ... fee},
#    {"Type":"A.7e60df042a9c0868.FlowToken.TokensDeposited",  ... fee},
#    {"Type":"A.9a0766d93b6608b7.FungibleToken.Deposited",    ... fee},
#    {"Type":"A.912d5440f7e3769e.FlowFees.FeesDeducted",     ... fee}
#  ], ...}
```

This endpoint runs the artdrop plugin's own embedded scripts
(`artdrop/cdc/setup_collection.cdc` for the
`ArtDropCore.CertCollection`, then `artdrop/cdc/register_provider.cdc` to
register the `auth(NonFungibleToken.Withdraw) &ArtDropCore.Collection`
provider capability with `ArtDropCore` so `ProtocolAdmin` can later move
certificates out of this account).

**Verified correct on testnet.** The `FlowToken.TokensWithdrawn` /
`FungibleToken.Withdrawn` events are the **fee payment only** (withdrawn
from the wallet-api proposer/payer, then re-deposited after fee
deduction); the new account itself is left alone. The ArtDropCertificate
collection + provider capability are stored on the artist/buyer accounts.

**Verdict: ✅ works as expected.** Same body shape as `/setup`'s
intended return (the ArdropSetup `transactionType` makes it discoverable
in the jobs/transactions history).

### FlowToken vault setup via raw transaction (testnet-correct script)

Newly-created accounts don't get a FlowToken vault from
`/accounts` (creation) — `INIT_FUNGIBLE_TOKEN_VAULTS_ON_ACCOUNT_CREATION`
defaults to `false` and the `ScriptPathCreateAccount` env var is empty.
Both artist and buyer therefore need an explicit FlowToken vault before
they can hold/spend FLOW. Submitted the following raw transaction for
each:

```bash
cat > /tmp/setup_flow_vault.cdc <<'CDC'
import FungibleToken from 0x9a0766d93b6608b7
import FlowToken from 0x7e60df042a9c0868

transaction {
    prepare(signer: auth(Storage, Capabilities) &Account) {
        if signer.storage.borrow<&FlowToken.Vault>(from: /storage/flowTokenVault) == nil {
            let vault <- FlowToken.createEmptyVault(vaultType: Type<@FlowToken.Vault>())
            signer.storage.save(<-vault, to: /storage/flowTokenVault)

            let receiverCap = signer.capabilities.storage.issue<&{FungibleToken.Receiver}>(/storage/flowTokenVault)
            signer.capabilities.publish(receiverCap, at: /public/flowTokenReceiver)

            let balanceCap = signer.capabilities.storage.issue<&{FungibleToken.Balance}>(/storage/flowTokenVault)
            signer.capabilities.publish(balanceCap, at: /public/flowTokenBalance)
        }
    }
}
CDC

BODY=$(jq -n --rawfile code /tmp/setup_flow_vault.cdc '{code:$code, arguments:[]}')
curl -sS -X POST -H 'Content-Type: application/json' \
  "https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/accounts/0x0a478c507cc8ea88/transactions?sync=true" \
  -d "$BODY"
# → 201
# {"transactionId":"170047647661619c4433f88b2ce3c283894652afe6fb711a9d0c217625cdebb4",
#  "transactionType":"General",
#  "events":[...fee payment events...], ...}

# Same body for the artist (0xee20358d6d32ea57):
# → 201, TXID 6810aa90ee35f6da22b1e1d6a04b71a5a8f14c1b95fd985243ac50a0314fac9d
```

> **Note on auth-account syntax.** The bundled `setup_example.cdc` uses
> the legacy `auth(Storage, Capabilities) &Account` syntax (Cadence 0.x /
> 1.0 transition). Both still work on testnet today. The
> `flow/api-test-scripts/raw_transactions.http` testnet examples use the
> even-older `AuthAccount` type (no `auth(...)` qualifier) — **also still
> works on testnet today**, but the
> `auth(BorrowValue, FungibleToken.Withdraw) &Account` shape is what
> `artdrop/cdc/create_escrow.cdc` uses internally, so prefer that going
> forward.

### Fund buyer with 20 FLOW from wallet-api

```bash
cat > /tmp/send_flow.cdc <<'CDC'
import FungibleToken from 0x9a0766d93b6608b7
import FlowToken from 0x7e60df042a9c0868

transaction(amount: UFix64, recipient: Address) {
    let sentVault: @{FungibleToken.Vault}
    prepare(signer: auth(BorrowValue) &Account) {
        let vaultRef = signer.storage.borrow<auth(FungibleToken.Withdraw) &{FungibleToken.Vault}>(from: /storage/flowTokenVault)
            ?? panic("failed to borrow reference to sender vault")
        self.sentVault <- vaultRef.withdraw(amount: amount)
    }
    execute {
        let receiverRef = getAccount(recipient)
            .capabilities
            .borrow<&{FungibleToken.Receiver}>(/public/flowTokenReceiver)
            ?? panic("failed to borrow reference to recipient vault")
        receiverRef.deposit(from: <-self.sentVault)
    }
}
CDC

BODY=$(jq -n --rawfile code /tmp/send_flow.cdc '{
  code: $code,
  arguments: [
    {type:"UFix64", value:"20.0"},
    {type:"Address", value:"0x0a478c507cc8ea88"}
  ]
}')
curl -sS -X POST -H 'Content-Type: application/json' \
  "https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/accounts/0x0680880ab9e7b676/transactions?sync=true" \
  -d "$BODY"
# → 201, TXID c9f7e2edb101301aaf9bb8d08eca7f4fa8e5c9a711e6e3f6b625bdb42763834e
```

> **Note on Cadence 1.0 syntax.** First attempt failed with
> `interfaces can not be used as types directly; wrap interfaces in
> intersection types ... got `FungibleToken.Vault`, consider using
> `{FungibleToken.Vault}`` — fixed by typing the resource as `@{FungibleToken.Vault}`
> (intersection type). Then second attempt with `amount=200.0` failed with
> `pre-condition failed: Cannot withdraw tokens! The amount requested
> (200.0) is greater than the balance (29.99...)` — wallet-api only had
> ~30 FLOW (per `deploy-testnet.md`, it was funded with 30 FLOW). Sent
> 20.0 FLOW instead, which worked.

### Verify balances

```bash
cat > /tmp/get_balance.cdc <<'CDC'
import FungibleToken from 0x9a0766d93b6608b7
import FlowToken from 0x7e60df042a9c0868
access(all) fun main(account: Address): UFix64 {
    let vaultRef = getAccount(account)
        .capabilities
        .borrow<&{FungibleToken.Balance}>(/public/flowTokenBalance)
        ?? panic("Could not borrow Balance reference to the Vault")
    return vaultRef.balance
}
CDC

BODY=$(jq -n --rawfile code /tmp/get_balance.cdc '{code:$code, arguments:[{type:"Address",value:"0x0a478c507cc8ea88"}]}')
curl -sS -X POST -H 'Content-Type: application/json' \
  "https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/scripts" -d "$BODY"
# → 2000100000  (raw UFix64 — divide by 1e8 → 20.00100000 FLOW)
```

> **Cadence 1.0 access-modifier gotcha.** First balance-script attempt
> failed with `error: `pub` is no longer a valid access modifier — use
> `access(all)` instead`. Cadence 1.0 dropped `pub`; the new
> `access(all)` is required. The `flow/api-test-scripts/scripts.http`
> examples still use `pub` — they'll need updating. Easy fix per-script;
> not blocking, just friction for raw-tx callers.

Buyer now has **20.001 FLOW** (sufficient for any escrow amount we'll
test). **Verdict: ✅ works as expected** (with the `pub → access(all)`
nit noted for the frontend team).

---

## §B.1  Bug found and fixed mid-test — `/artdrop/certificates` returns all-zero metadata

> **Status: 🔴 BUG found → ✅ FIXED in this session (Go + Cadence script,
> needs Quave redeploy).**

### Symptom (observed live)

```bash
curl -sS https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/accounts/0x0daaba937562c85f/artdrop/certificates
# [{"id":3,"edition_id":0,"serial":0,"is_revealed":false},
#  {"id":2,"edition_id":0,"serial":0,"is_revealed":false}]
```

Every cert's `edition_id`, `serial`, and `is_revealed` is `0` / `false`.
**The IDs (2, 3) are correct but everything else is wrong.** A frontend
that reads `is_revealed` to decide whether to show a "reveal" button
would always show it; one that uses `edition_id`/`serial` to build NFT
display names would show broken metadata.

### Ground truth (via `flow` CLI, no API involved)

```bash
flow scripts execute scripts/dev/get_certificate_is_revealed.cdc \
  0x0daaba937562c85f 2 --network testnet
# Result: true
flow scripts execute scripts/dev/get_certificate_is_revealed.cdc \
  0x0daaba937562c85f 3 --network testnet
# Result: true
flow scripts execute scripts/dev/get_certificate_serial_number.cdc \
  0x0daaba937562c85f 2 --network testnet
# Result: 2
flow scripts execute scripts/dev/get_certificate_editions_view.cdc \
  0x0daaba937562c85f 2 --network testnet
# Result: [2, 3]    ← (serial, max), NOT editionId
flow scripts execute scripts/dev/get_certificate_editions_view.cdc \
  0x0daaba937562c85f 3 --network testnet
# Result: [3, 3]
```

So `cert 2` is on an edition with `reprintLimit=3`, serial=2,
`isRevealed=true`; `cert 3` is on the same edition, serial=3,
`isRevealed=true`. (Matches `flows-testnet.md` §2.4.5 — certs 2 and 3 are
both confirmed revealed after the off-by-one fix.)

### Root cause

`artdrop/cdc/get_certificate_ids.cdc` (bundled in the API binary) is a
copy of `artdrop-protocol/scripts/dev/get_cert_ids.cdc` — it returns a
bare `[UInt64]` of certificate IDs:

```cadence
import ArtDropCore from 0xec581a0282d99a1a
import NonFungibleToken from 0x631e88ae7f1d7c20

access(all) fun main(addr: Address): [UInt64] {
    let collection = getAccount(addr).capabilities
        .borrow<&ArtDropCore.Collection>(ArtDropCore.CertCollectionPublicPath)
    if collection == nil { return [] }
    let ids = collection!.getIDs()
    return ids
}
```

The Go side (`artdrop/service.go:ListCertificates`) populates
`CertificateInfo{Id: uint64(id)}` and leaves the other fields at their
zero values — i.e. the API silently drops `edition_id`, `serial`, and
`is_revealed` even though the JSON schema advertises them. The
artdrop-protocol side has separate per-cert scripts
(`get_certificate_serial_number.cdc`, `get_certificate_is_revealed.cdc`,
`get_certificate_editions_view.cdc`) that DO return each field — but the
REST API doesn't use any of them.

The same script also backs `GET /v1/accounts/{addr}/artdrop/collection-length`
(which calls `ListCertificates` and takes the length), so this fix
auto-fixes that endpoint too.

### Fix (in this session, needs Quave redeploy)

Two changes:

1. **New script `artdrop/cdc/get_certificates.cdc`** — returns
   `[{String: AnyStruct}]` with one dict per cert, keys
   `id`/`editionId`/`serial`/`isRevealed`. Choosing a dict-returning
   shape (instead of a new contract struct like `CertificateSummary`) so
   no contract change is needed — purely an additive Cadence script.

2. **`artdrop/service.go:ListCertificates` updated** to call the new
   script and parse the dict-array shape, populating all four fields.

3. **Tests updated** (`artdrop/service_queries_test.go` +
   `artdrop/handler_queries_test.go`) to use the new dict-shape mock and
   assert the rich fields are populated (previously only the ID was
   asserted). All existing tests pass after the updates.

Files touched:

- `artdrop/cdc/get_certificates.cdc` (new)
- `artdrop/service.go` (`ListCertificates` + `//go:embed` directive)
- `artdrop/service_queries_test.go` (`TestListCertificatesReturnsIds`)
- `artdrop/handler_queries_test.go` (`TestListCertificatesHandlerReturnsOK`,
  `TestGetCollectionLengthHandlerReturnsOK`)

### Verified live (script-only) before committing

Tested the new script against the live API via `POST /v1/scripts` (no
deploy needed for that — it's the raw Cadence script endpoint):

```bash
cat > /tmp/get_certificates_test.cdc <<'CDC'
import ArtDropCore from 0xec581a0282d99a1a
import NonFungibleToken from 0x631e88ae7f1d7c20

access(all) fun main(addr: Address): [{String: AnyStruct}] {
    let collection = getAccount(addr).capabilities
        .borrow<&ArtDropCore.Collection>(ArtDropCore.CertCollectionPublicPath)
    if collection == nil { return [] }
    let result: [{String: AnyStruct}] = []
    for id in collection!.getIDs() {
        let nft = collection!.borrowNFT(id) as? &ArtDropCore.Certificate
        if nft == nil { continue }
        let cert = nft!
        result.append({
            "id": cert.id,
            "editionId": cert.editionId,
            "serial": cert.serial,
            "isRevealed": cert.isRevealed()
        })
    }
    return result
}
CDC

BODY=$(jq -n --rawfile code /tmp/get_certificates_test.cdc \
  '{code:$code, arguments:[{type:"Address",value:"0x0daaba937562c85f"}]}')
curl -sS -X POST -H 'Content-Type: application/json' \
  "https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/scripts" -d "$BODY"
# → {"ArrayType":null,
#    "Values":[
#      {"DictionaryType":null,
#       "Pairs":[
#         {"Key":"serial","Value":3},
#         {"Key":"isRevealed","Value":true},
#         {"Key":"editionId","Value":1},
#         {"Key":"id","Value":3}]},
#      {"DictionaryType":null,
#       "Pairs":[
#         {"Key":"id","Value":2},
#         {"Key":"serial","Value":2},
#         {"Key":"isRevealed","Value":true},
#         {"Key":"editionId","Value":1}]}]}
```

`cert 3 → {id:3, editionId:1, serial:3, isRevealed:true}`,
`cert 2 → {id:2, editionId:1, serial:2, isRevealed:true}` — matches the
flow CLI ground truth.

Also tested against an empty collection (our new buyer
`0x0a478c507cc8ea88`) — returns `[]`, no panic. The script handles
un-initialized accounts gracefully.

### Local Go-side verification

```
$ go build ./...
(no output)
$ go vet ./...
(no output)
$ go test ./artdrop/...
ok  	github.com/flow-hydraulics/flow-wallet-api/artdrop	0.181s
```

Build clean, vet clean, all artdrop tests pass.

### What still needs to happen for the fix to go live

The orchestrator needs to:
1. Pull this branch
2. Quave-redeploy the wallet-api service (this is one of the `flow-accounts-manager` redeploys, same pattern as other session changes — the script file change is enough since `//go:embed` picks up the new file at build time)
3. The new `get_certificates.cdc` becomes live
4. `GET /v1/accounts/{addr}/artdrop/certificates` will start returning rich metadata

**No Cadence contract change needed** (this fix is purely additive —
new script + new Go code; nothing on `testnet-a` needs redeploying).

---

## §C. Create Original — DISCOVERED LIMITATION

### Goal

Create an Original via `POST /v1/accounts/{wallet-api}/transactions`
with the body containing `artdrop-protocol/transactions/admin/create_original.cdc`,
signed by the wallet-api admin.

### Script submitted

```bash
cat > /tmp/create_original.cdc <<'CDC'
import ArtDropCore from 0xec581a0282d99a1a

transaction(
    artist: Address,
    name: String,
    description: String,
    prices: {String: UFix64}
) {
    prepare(signer: auth(Storage) &Account) {
        let admin = signer.storage.borrow<auth(ArtDropCore.GovernanceAdmin) &ArtDropCore.ProtocolAdmin>(
            from: ArtDropCore.AdminStoragePath
        ) ?? panic("create_original: signer does not hold ProtocolAdmin")

        admin.createOriginal(
            artist: artist,
            name: name,
            description: description,
            prices: prices
        )
    }
}
CDC

BODY=$(jq -n --rawfile code /tmp/create_original.cdc '{
  code: $code,
  arguments: [
    {type:"Address", value:"0xee20358d6d32ea57"},
    {type:"String", value:"API Test Original #1"},
    {type:"String", value:"Original created via the ArtDrop REST API"},
    {type:"Dictionary", value:[
      {key:{type:"String", value:"FLOW"}, value:{type:"UFix64", value:"100.0"}}
    ]}
  ]
}')
curl -sS -i -X POST -H 'Content-Type: application/json' \
  "https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/accounts/0x0680880ab9e7b676/transactions?sync=true" \
  -d "$BODY"
```

### Result

```http
HTTP/2 400
[Error Code: 1101] cadence runtime error: Execution failed:
error: stored value type mismatch: expected type `ArtDropCore.ProtocolAdmin`,
got `Capability<auth(ArtDropCore.ArtistDirect) &ArtDropCore.ProtocolAdmin>`
  --> create_original.cdc:10:20
```

### Root cause

The wallet-api admin account (`0x0680880ab9e7b676`) does NOT hold
`ArtDropCore.GovernanceAdmin`. It holds
`Capability<auth(ArtDropCore.ArtistDirect) &ArtDropCore.ProtocolAdmin>`
instead (the `ArtistDirect` capability granted at deploy time — see
`artdrop-protocol/docs/deploy-testnet.md` §0 "Account model and
addresses": "testnet-wallet-api (custodial wallet-api, receives
ArtistDirect + ProtocolTransfer caps)").

`create_original.cdc` tries to borrow the admin resource as
`auth(ArtDropCore.GovernanceAdmin) &ArtDropCore.ProtocolAdmin` —
that's a stronger capability type than what's actually stored, so
Cadence's `borrow` rejects it at runtime with a "stored value type
mismatch" error.

### What this means for the REST API

The wallet-api by design has `ArtistDirect` + `ProtocolTransfer`
capabilities only — **no `GovernanceAdmin`**. Therefore:

| Capability | What it lets you do | Wallet-API has it? |
|---|---|---|
| `GovernanceAdmin` | `createOriginal`, `createEdition`, `setMarketMode`, `setPlatformFee`, governance actions | ❌ No |
| `ArtistDirect` | Mint certs on an Edition where the wallet-api IS the artist | ✅ Yes |
| `ProtocolTransfer` | Move certificates between accounts (bypasses MarketMode) | ✅ Yes |
| `RegisterProvider` cap (called via `register_provider.cdc`) | Lets ArtDropCore withdraw certs from your collection during ProtocolTransfer | ✅ Yes (already done at `/artdrop/setup` time) |

So the wallet-api can **never** call `create_original.cdc` or
`create_edition.cdc` via this REST API — those transactions require
`GovernanceAdmin`, and only testnet-a (`0xec581a0282d99a1a`) has it.
testnet-a is not a custodial account in this wallet-api, so it can't
be signed for either.

This means **Original/Edition creation has to happen OFF-API**, by a
human (or separate flow-CLI session) using the testnet-a key — exactly
what `flows-testnet.md` §1.1 and §1.2 do. Once an Edition exists with
the wallet-api as the artist, the wallet-api can claim the
EditionMinter from its inbox (`artdrop/cdc/claim_minter.cdc` via the
generic `/transactions` endpoint) and then mint certs.

### Other minor findings during this section

1. **Cadence `import "ArtDropCore"` (unqualified) does NOT work via the
   REST API.** The artdrop-protocol scripts use the unqualified import
   form because the `flow` CLI has contract aliases configured in
   `flow.json`. The REST API has no such alias config. Must use the
   fully-qualified form `import ArtDropCore from 0xec581a0282d99a1a`
   when posting scripts via `/v1/scripts` or `/v1/accounts/{addr}/transactions`.
   This is just a friction-of-raw-tx-calls issue, not an API bug.
2. **Cadence 1.0 interface-resource syntax requires intersection
   types.** First version of `send_flow.cdc` (used later for funding)
   had `let sentVault: @FungibleToken.Vault` which Cadence 1.0 rejects
   with "interfaces can not be used as types directly; wrap interfaces
   in intersection types ... got `FungibleToken.Vault`, consider using
   `{FungibleToken.Vault}`". Fix: `@{FungibleToken.Vault}`.

**Verdict: ❌ Create Original via REST API is blocked by design** (wallet-api
lacks `GovernanceAdmin`). The fix would require either:

a. Adding `GovernanceAdmin` to the wallet-api's capabilities (changes the
   trust model of the wallet-api; it would then be able to flip
   MarketMode and create Editions at will — risky for a custodial service).
b. Adding a separate custodial account on testnet-a's key that this
   wallet-api manages, and exposing that account via the REST API
   (more setup work, requires testnet-a key to be in the wallet-api
   HSM — not currently the case).
c. Adding a backend-only endpoint that submits pre-signed admin
   transactions (out of scope for this session).

**Will flag as "Open question" for the orchestrator + frontend team.**

---

## §D. is-artist endpoint — first live verification of the prior-session fix

```bash
# Prior test artist (has Original #1 on chain):
curl -sS https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/accounts/0x0daaba937562c85f/artdrop/is-artist
# {"isArtist":true}                                          ✅

# wallet-api (admin, never created an Original):
curl -sS https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/accounts/0x0680880ab9e7b676/artdrop/is-artist
# {"isArtist":false}                                         ✅

# Our new artist (never created an Original):
curl -sS https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/accounts/0xee20358d6d32ea57/artdrop/is-artist
# {"isArtist":false}                                         ✅

# Buyer (definitely not):
curl -sS https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/accounts/0x0a478c507cc8ea88/artdrop/is-artist
# {"isArtist":false}                                         ✅
```

**Verdict: ✅ works as expected.** The fix to call
`ArtDropRegistry.IArtistIndex.isArtist` (the change in commit `8aff5ad`)
correctly distinguishes between accounts that have created an Original
(via testnet-a's `create_original` calls — which auto-register the
artist in the registry) and accounts that have not. The wallet-api,
despite being the admin that *submitted* createOriginal transactions,
is correctly NOT listed as an artist because it's never *been the
artist* on an Original.

---

## §D.1  More bugs found while testing — Original/Edition summary endpoints

### `GET /v1/artdrop/originals/1` — `artistName` always empty

```bash
curl -sS https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/artdrop/originals/1
# {"id":1,"name":"Testnet Original #1","artistName":""}
```

**Root cause.** The handler
(`artdrop/service.go:GetOriginalSummary`) reads
`fields["artistName"].(cadence.String)` from the
`ArtDropCore.OriginalSummary` struct. **That field doesn't exist.**
The contract struct (`artdrop-protocol/contracts/core/ArtDropCore.cdc:166`)
defines:

```cadence
access(all) struct OriginalSummary {
    access(all) let id: UInt64
    access(all) let artist: Address      // ← Address, not String, and named `artist`, not `artistName`
    access(all) let name: String
    access(all) let prices: {String: UFix64}
    access(all) let createdAtBlock: UInt64
    access(all) let schemaVersion: UInt64
}
```

So the Go type assertion always failed silently and `artistName`
stayed at its zero value. The frontend would never see who the
artist of an Original is — would have to do a separate lookup.

### `GET /v1/artdrop/editions/1` — `state` always 0, `maxSupply` always 0

```bash
# Before activation, edition was Pending (state=0) — correct in API.
# After activation via flow CLI on testnet-a, edition became Active (state=3):
curl -sS https://artdrop-production-artdrop.svc-us5.zcloud.ws/v1/artdrop/editions/1
# {"id":1,"state":0,"totalMinted":3,"maxSupply":0}   ← state STILL 0! Wrong.

# Ground truth via flow CLI:
flow scripts execute scripts/dev/get_edition_summary.cdc 1 --network testnet
# Result: ... state: A.ec581a0282d99a1a.ArtDropCore.EditionState(rawValue: 3), ...
```

**Root cause.** Two issues in the same handler
(`artdrop/service.go:GetEditionSummary`):

1. `fields["state"].(cadence.UInt8)` — the contract returns
   `state` as `ArtDropCore.EditionState` (an enum), not a bare
   `UInt8`. The Go type assertion silently fails, state stays 0.
2. `fields["maxSupply"].(cadence.UInt64)` — there is **no
   `maxSupply` field** on the contract's `EditionSummary`. The
   field is `reprintLimit`. MaxSupply stays 0.

### Fix (in this session, needs Quave redeploy)

Two new scripts + matching handler updates:

- `artdrop/cdc/get_original_summary_v2.cdc` — returns
  `{String: AnyStruct}?` with the contract's `artist` Address
  exposed under the key `"artist"` (mapped to
  `OriginalSummary.ArtistName` in JSON via the handler).
- `artdrop/cdc/get_edition_summary_v2.cdc` — returns
  `{String: AnyStruct}?` with the enum's `rawValue` unwrapped
  into the `"state"` key, and `"reprintLimit"` mapped to the
  JSON `maxSupply` field (preserving the existing API contract
  for backward compat — the JSON `maxSupply` key stays, but it
  now has the correct value).

Both scripts verified working via `POST /v1/scripts` before
committing (results match the contract's actual data).

Files touched:

- `artdrop/cdc/get_original_summary_v2.cdc` (new)
- `artdrop/cdc/get_edition_summary_v2.cdc` (new)
- `artdrop/service.go` (`GetOriginalSummary`, `GetEditionSummary`,
  `//go:embed` directives)
- `artdrop/types.go:OriginalSummary` — `ArtistName` field type
  already fits Address→Hex; no struct change needed.
- `artdrop/types.go:EditionSummary` — `MaxSupply` field already
  named correctly; no struct change needed.

### Local Go-side verification

```
$ go build ./...           # clean
$ go vet ./...             # clean
$ go test ./artdrop/...    # ok (existing tests pass)
```

### After redeploy

The current API response (before fix):

```json
{"id":1,"state":0,"totalMinted":3,"maxSupply":0}
{"id":1,"name":"Testnet Original #1","artistName":""}
```

After fix is live:

```json
{"id":1,"state":3,"totalMinted":3,"maxSupply":3}     // edition
{"id":1,"name":"Testnet Original #1","artistName":"0x0daaba937562c85f"}  // original
```

**Still needs Quave redeploy** (script + Go change).

---

## §D.2  Bundled Cadence scripts use unqualified `import "X"` — fail when not run via `flow` CLI

While trying to submit `create_original.cdc` and other admin scripts
through the wallet-api, hit `error: location (X) is not a valid location:
expecting an AddressLocation, but other location types are passed` for
several of the bundled scripts. Root cause: the artdrop-protocol
transaction files (and the bundled copies in `artdrop/cdc/`) use the
unqualified form `import "EscrowModule"` which only works when the
`flow` CLI has contract aliases configured in `flow.json`. When the
script is run through the wallet-api's `flow-go-sdk`, no alias config
is loaded and the unqualified import fails.

Affected bundled scripts (all fixed in this session):

- `artdrop/cdc/create_escrow.cdc` — `import "EscrowModule"`
- `artdrop/cdc/activate_chip_and_settle.cdc` — `import "EscrowModule"`
- `artdrop/cdc/release_escrow.cdc` — `import "EscrowModule"` + `import "PaymentModule"`
- `artdrop/cdc/cancel_escrow.cdc` — `import "EscrowModule"`
- `artdrop/cdc/refund_escrow.cdc` — `import "EscrowModule"`

All changed to address-qualified imports:

```cadence
import EscrowModule from 0x1bfedfa0ec66c23e
import PaymentModule from 0x1bfedfa0ec66c23e
```

(Also fixed `release_escrow.cdc`'s `PaymentModule` import which had
the same unqualified-form issue.)

`create_original.cdc` and `create_edition.cdc` were already
address-qualified (`import ArtDropCore from 0xec581a0282d99a1a`).
The current session's created `get_certificates.cdc`,
`get_original_summary_v2.cdc`, `get_edition_summary_v2.cdc` are
all address-qualified by construction.

### Verification

```
$ go build ./...           # clean
$ go vet ./...             # clean
$ go test ./artdrop/...    # ok
```

**Still needs Quave redeploy** for the fixed scripts to take effect.
Until then, all four escrow-lifecycle endpoints
(`/artdrop/escrows`, `/activate-chip`, `/release`, `/cancel`, `/refund`)
return 400 with `error: location (EscrowModule) is not a valid location`.

---
