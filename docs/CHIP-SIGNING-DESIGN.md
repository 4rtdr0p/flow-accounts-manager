# Design — General "chip can sign anything" capability (wallet-api, model B)

| | |
|---|---|
| **Status** | Design only — not implemented. Direction ("model B") is decided; this designs toward it. |
| **Author** | chip-signing-design agent |
| **Date** | 2026-09-01 |
| **Scope** | `flow-accounts-manager` (`artdrop` plugin) primarily; one delegated-capability grant needed from `artdrop-protocol`; front changes are removals + a thinner request body. |
| **Grounded in** | `artdrop-protocol/docs/CHIP-AUTH.md`; `EscrowModule.cdc`; `ArtDropCore.cdc` (entitlements §1, `registerChipPublicKey`/`replaceChipPublicKey`, `ManageEscrow`); `docs/DEPLOY.md` (§10.2 `ArtistOnboarding` precedent); `register_chip.cdc`; `activate_chip_and_settle.cdc`; `flow-accounts-manager` `artdrop/`, `keys/`, `transactions/`, `openapi.yml`; `payload-galaxy-front` `ixkio/`, `Chips`, `chipSignature.ts`; [Ixkio Flex API docs](https://docs.ixkio.com/flex-api/flex-api-getting-started/flex-api-ntag424-authentication). |

---

## 0. TL;DR for the tech lead

- **Two guiding constraints from the tech lead, and how this design meets them:**
  (a) **It must work now.** The concrete, working feature is escrow activation: a config bypass flag (`ARTDROP_IXKIO_ENABLED=false`) lets the whole create-escrow → activate → settle flow be tested end to end on testnet with no physical chip and no Ixkio dependency (see §8 Test strategy). Provisioning + activation are the only pieces that ship now; nothing else is load-bearing for "works now."
  (b) **Dropping Ixkio later must be easy.** The `ixkio.Client` sits behind a `Verify(tap) (id, pass)` interface — swapping verifiers is a Go-level change with no Cadence and no on-chain change. The per-chip `signing_mode` flag (§7) makes the eventual move to asymmetric self-signing chips (no verifier needed at all) a config flip, not a rewrite. The on-chain interface (`verifyChipSignature`) never changes for either transition.
- **Decision (a) — who calls Ixkio: the wallet-api should call Ixkio itself.** The wallet-api holds the chip private key; it must independently confirm the tap before wielding that key. If the front just passes a `Pass` boolean, the gate is forgeable by anyone with API access and provides no real security. The coupling is off-chain only, which CHIP-AUTH.md explicitly permits. Front forwards the raw tap params `{x, n, e}`; wallet-api calls Ixkio `/v1/t`, maps `xuid → chipId`, then signs. **Single-use constraint:** Ixkio enforces one `Pass` per scan-count `n` — so the front must forward the tap unverified, never call Ixkio and then also forward it (that would burn the single use and the wallet's own verify would fail). See §4a.
- **Decision (b) — can flow-wallet-api sign arbitrary data today: yes at the SDK level, but NOT in a form the on-chain check accepts — a small, well-scoped addition is required.** `crypto.Signer.Sign([]byte)` is generic and already the basis of all signing. But the two ready-made paths don't match the chip verify: (i) the account's stored signer is pinned to **SHA3_256**, while the on-chain check uses **SHA2_256**; (ii) `flow.SignUserMessage` prepends the **`"FLOW-V0.0-user"`** domain tag, while the on-chain check uses an **empty** domain tag. The minimal addition is a raw-sign primitive that hashes with SHA2_256 and prepends no domain tag, producing a 64-byte `r||s` signature over the raw UTF-8 challenge — exactly what a hardware P-256 chip (and the model-A browser code) produces. For local ECDSA_P256 keys this is one thin method; see §4b and §3.
- **What ships now vs. later:** the general signing *primitive* (`signChipChallenge` + the Ixkio gate, §3–§4) is built now — that's the future-proofing. But the only wired *consumer* is escrow activation (§5). The public `POST /v1/chips/{chipId}/sign` HTTP endpoint is **deferred** (future/optional, Phase 3-or-later in §9) until a real non-escrow consumer exists; shipping an unused public signing endpoint isn't needed to meet either guiding constraint. `POST /v1/chips` (provision) and `GET /v1/chips/{chipId}` (mapping/status) ship now — activation needs provisioning.
- **Ixkio API mode — resolved.** Ixkio's machine-readable Pass/Fail API mode exists and is documented: `GET https://api.ixkio.com/v1/t?x=<xuid>&n=<scan-count-hex>&e=<auth-code>&r=<response-API-token>` → `{"xuid":"…","response":"Pass"|"Fail"}` (or `{"xuid":"…","error":"…"}`). Source: [Ixkio Flex API — NTAG424 authentication](https://docs.ixkio.com/flex-api/flex-api-getting-started/flex-api-ntag424-authentication). See §4a — this was the top open question and is no longer open. It is a config/dashboard dependency (the wallet needs its own `r` response-API token provisioned for API mode), not a code unknown.
- **Delegated-cap breadth — resolved: narrow it now, not later.** Investigated whether a purpose-built `ChipAdmin` entitlement (instead of reusing broad `OperationalAdmin`) is update-safe in place. It is — see §2.2 for the full reasoning. Recommendation: add `ChipAdmin` now and re-gate `registerChipPublicKey`/`replaceChipPublicKey` to it. No protocol-issue-for-later needed; this closes the debt at zero redeploy cost.
- **Proposed endpoint surface (now):** `POST /v1/chips` (provision: create chip account + register pubkey on-chain, ops-scoped), `GET /v1/chips/{chipId}` (mapping/status, read-scoped). `POST /v1/chips/{chipId}/sign` (general Ixkio-gated signing, action-scoped) is **deferred** — see above. Escrow activation becomes an in-process consumer of the internal signing primitive; its request body loses the client `signature` and `challenge`, carrying only the raw Ixkio tap instead.
- **Chip = full Flow account — RESOLVED (accepted).** One custodial Flow account per chip; the account-creation fee is funded by the per-piece 5% gas reserve already charged (that reserve exists precisely to absorb per-piece on-chain costs). See §10 Q4.
- **The identity binding (the security crux):** `xuid ↔ chipId ↔ chip account ↔ chipPubKey` must be one unbroken chain, and the wallet must assert the tap's `chipId` equals the escrow's `chipId` (a Pass for piece A can never make piece B's account sign). Recommendation: make our `chipId` **be** the Ixkio identifier (its `xuid`, or `cuid`) so the mapping is identity rather than a lookup table that can drift — pin `xuid`-vs-`cuid` at provisioning. See §1 (join key) and §3.2.

---

## 1. The model & vocabulary

### The two keys — never conflate them

| Key | Curve / algo | Held by | Used for | On-chain? |
|---|---|---|---|---|
| **Chip AES tap key** | AES-128 symmetric (NTAG424 SUN/CMAC) | chip secure element + Ixkio holds the copy | Ixkio authenticates the physical tap (off-chain, symmetric) | No |
| **Chip-account key** | **ECDSA P-256** | **wallet-api custody** (chip's custodial Flow account) | wallet-api signs challenges *as the chip*; its pubkey is the on-chain identity | **Yes — the pubkey is the trust anchor** |

The AES key is Ixkio's business and never touches Cadence. The **chip-account key is the P-256 keypair whose public key is registered on-chain as the chip's identity** (`chipPubKey`). Under model B the wallet-api custodies that private key and signs on the chip's behalf, gated by an Ixkio `Pass`.

### The join key is `chipId` (a String)

```
chipId (String)
  ├── wallet-api:   chipId → chip custodial Flow account (address) → P-256 private key (custody)
  ├── on-chain:     chipId → ArtDropRegistry.ChipPublicKeyIndex → chipPubKey (64-byte raw P-256)
  ├── escrow:       Escrow.chipId (String) + Escrow.chipPubKey (frozen at creation) → verify at activation
  └── Ixkio:        tap → xuid → chipId   (the wallet-api maps xuid to chipId)
```

Key facts pinned from the code:

- On-chain the chip's identity is **just a 64-byte raw ECDSA P-256 public key** (`x||y`, no `0x04` prefix) — `EscrowModule.verifyChipSignature` enforces `chipPubKey.length == 64` and constructs `PublicKey(publicKey:, signatureAlgorithm: ECDSA_P256)` (`EscrowModule.cdc:162-180`). The chip does **not** need to be a Flow account for the check to pass — the account framing is a *key-custody* convenience, not an on-chain requirement.
- `ChipPublicKeyIndex` is keyed by **`String` chipId** (`{String: [UInt8]}`, `ArtDropRegistry.cdc:786-858`), matching `Certificate.chipId` / `Escrow.chipId`. The older `ChipIndex` (owner map) is keyed by `UInt64` and is a separate legacy concern (`register_chip.cdc` writes both; the `UInt64` one is unrelated to signature verification).
- At **escrow creation** the pubkey is looked up once from the registry by `chipId` and **frozen onto the Escrow resource** (`Escrow.chipPubKey`, a `let`). Activation verifies against the frozen key, never re-queries the registry (`ArtDropCore.cdc:1219-1341`, `EscrowModule.runActivateChipAndSettle`). Consequence: re-keying a chip after an escrow exists does not affect that escrow.

### On-chain vs off-chain (unchanged from CHIP-AUTH.md)

- **On-chain (stable interface):** a per-chip/per-cert `chipPubKey` + `PublicKey.verify(signature, challenge, "", SHA2_256)`. The chain knows nothing about Ixkio, the chip technology, or who holds the private key.
- **Off-chain (swappable):** tap intake, Ixkio verification, and signing with the matching private key. Model B puts the signer inside the wallet-api. Everything here can change without a Cadence change.

---

## 2. Provisioning — creating a chip account and registering its pubkey

### 2.1 What "provision a chip" means

For a `chipId`, the wallet-api:

1. **Creates a custodial Flow account** (reusing the existing `POST /v1/accounts` account-creation path — an ECDSA_P256 / SHA3_256 key by default; the account never needs FLOW or to transact, it is purely a key-custody vehicle). The account's raw 64-byte P-256 pubkey is the chip's on-chain identity.
2. **Persists the `chipId → account` mapping** (see §2.3).
3. **Registers the pubkey on-chain** by submitting a `register_chip`-style transaction that calls `ArtDropCore.ProtocolAdmin.registerChipPublicKey(chipId, publicKey)` (or `replaceChipPublicKey` on re-key).

### 2.2 The authority gap and how the wallet-api gets it

`registerChipPublicKey`/`replaceChipPublicKey` are gated by `access(OperationalAdmin)` (`ArtDropCore.cdc:2756`, `:2778`). `register_chip.cdc` does a **same-account direct storage borrow** of `auth(ArtDropCore.OperationalAdmin) &ArtDropCore.ProtocolAdmin` from `ArtDropCore.AdminStoragePath` — that requires the `ProtocolAdmin` resource to physically live in the signer's storage. It lives only on the Core account (A3, `0xd97d6774544fcd9c`). The wallet-api custodial account holds only `ArtistDirect`, `ArtistOnboarding`, and `ProtocolTransfer` capabilities — **no `OperationalAdmin`**. **So the wallet-api cannot register chips today.**

**Recommended path: reuse the proven delegated-capability pattern** already shipped for escrow voiding (`grant_escrow_void_cap.cdc` / `claim_escrow_void_cap.cdc`, referenced by `artdrop/service.go:552-569` `VoidEscrow`, saved at `/storage/artdropEscrowVoidAdminCap`, exercised by `void_escrow_via_delegated_cap.cdc`) — but issued against a **new, narrow `ChipAdmin` entitlement**, not the broad `OperationalAdmin`. Concretely:

1. **Protocol change (one-time, A3 signs):**
   - Add `access(all) entitlement ChipAdmin` to `ArtDropCore`'s §1 entitlement block (`contracts/core/ArtDropCore.cdc:64-91`), alongside the existing `GovernanceAdmin`/`OperationalAdmin`/`ArtistDirect`/`ArtistOnboarding`/`Owner`/`ProtocolTransfer`/`ManageEscrow`.
   - Re-gate `registerChipPublicKey` (`ArtDropCore.cdc:2756`) and `replaceChipPublicKey` (`ArtDropCore.cdc:2778`) from `access(OperationalAdmin)` to `access(ChipAdmin)`.
   - A new `grant_chip_admin_cap.cdc` that does
     `signer.capabilities.storage.issue<auth(ArtDropCore.ChipAdmin) &ArtDropCore.ProtocolAdmin>(ArtDropCore.AdminStoragePath)` and `inbox.publish(...)` to the wallet-api account.
2. **Wallet-api one-time setup:** a `claim_chip_admin_cap.cdc` that claims from the inbox and saves the capability at, e.g., `/storage/artdropChipAdminCap`.
3. **New provisioning transaction** `register_chip_via_delegated_cap.cdc` (mirrors `void_escrow_via_delegated_cap.cdc`): `copy`/`borrow` the stored capability, then call `admin.registerChipPublicKey(...)` / `.replaceChipPublicKey(...)`. This transaction is signed by the wallet-api account and needs **no** `GovernanceAdmin`, and — with `ChipAdmin` — no `OperationalAdmin` either.

**Investigation: is adding `ChipAdmin` and re-gating these two functions update-safe in place (no redeploy)? — Yes.** Three independent lines of evidence converge:

1. **The contract says so about itself.** `ArtDropCore.cdc:55-59` states plainly: *"Entitlements are code-layer only (not serialized with data) — valid to change access modifier after deployment. However, widening entitlement scope effectively breaks the access model. Treat as stable commitments. Never widen after first mainnet deploy without a security review."* Entitlements gate access at the type-checker level; they are not part of a resource's serialized storage layout, so the update validator (which checks storage-format compatibility) has no reason to reject either adding a new entitlement declaration or changing which entitlement gates an existing function. The caution in that comment is about *widening* (a security regression), which doesn't apply here — narrowing two functions from a broad entitlement to a purpose-built one is the opposite of widening.
2. **This was already empirically established on this project**, not just inferred from docs — [[cadence-deploy-constraints]] (12 days old, from the 2026-08-19 escrow redesign, where each constraint was verified by actually attempting it on testnet, not by reading Cadence docs) lists as **"Update-safe, confirmed working in place"**: *"function-body changes, precondition changes, **access-modifier changes**, adding functions, **adding new declarations (structs/resources/enums)**, adding a member to an interface the contract itself defines."* Adding `entitlement ChipAdmin` is "adding a new declaration"; re-gating `access(OperationalAdmin)` → `access(ChipAdmin)` on the two functions is exactly "access-modifier changes." Both are already on the confirmed-safe list. (What forces a redeploy is a *storage-format* change — a shrunk enum, a new field on a stored resource, a conformed-to contract's address moving — none of which apply here.)
3. **There's already a working precedent for exactly this pattern in this same contract.** `ManageEscrow` (`ArtDropCore.cdc:91`) is a purpose-scoped entitlement, narrower than `OperationalAdmin`/`GovernanceAdmin`, gating only the escrow-lifecycle functions and held only by the `EscrowLogic` capability. `ArtistOnboarding` (`ArtDropCore.cdc:73-81`, live on testnet per `DEPLOY.md` §10.2) is another purpose-built narrow entitlement carved out specifically so the wallet-api doesn't need full `GovernanceAdmin` to onboard artists. `ChipAdmin` is the same idiom applied to chip registration — not a novel pattern for this codebase.

No genuine uncertainty remains here to justify the empirical negative-control check the tech lead flagged as a fallback (the kind used to verify the `Voided` enum append) — that technique is for cases where the update-checker's behavior is unconfirmed; here it's confirmed twice over (the contract's own comment + the project's own prior testnet attempt). A cheap non-blocking sanity check is still worth doing before it touches production: dry-run the two-line entitlement addition + re-gate against a throwaway copy of the deployed `ArtDropCore` with `flow accounts update-contract --network testnet` (or emulator, since this class of change doesn't depend on the emulator-vs-real-network removal-allowlist gap) before A3 signs for real.

**Recommendation: add `ChipAdmin` now.** It closes the "wallet-api holds all OperationalAdmin powers" debt at zero redeploy cost, matches the existing `ManageEscrow`/`ArtistOnboarding` narrow-entitlement idiom already used in this contract, and needs no protocol issue deferring it — there's no reason to accept the broader grant when the narrower one is equally cheap to ship.

**Alternative (rejected for the general capability):** keep provisioning off-API — a human runs `register_chip.cdc` with the A3 key. Simpler and zero new authority, but it does not meet the "wallet-api provisions chips" goal and does not scale to the ops chip-assignment flow. Fine as a stopgap for the very first chips.

### 2.3 Where the mapping is persisted

New table in the artdrop plugin (GORM), e.g. `artdrop_chips`:

| column | type | notes |
|---|---|---|
| `chip_id` | text, PK | the String chipId (join key) |
| `account_address` | text | the chip's custodial Flow account |
| `public_key` | bytea/hex | 64-byte raw P-256 (denormalized for convenience; source of truth is the account key) |
| `signing_mode` | text | `custodial` (wallet-api signs) or `self` (asymmetric chip self-signs) — see §7 |
| `registered_at_block` | int8, nullable | set after on-chain registration confirms |
| `ixkio_xuid` | text, nullable | optional, if we bind xuid→chipId here instead of in Payload |
| `created_at` / `updated_at` | timestamptz | |

The chip account's private key itself is stored exactly like every other custodial key — encrypted in `storable_keys` (`keys/keys.go:49-61`), indexed by `account_address`. No new key-storage mechanism.

### 2.4 Tie to the ops chip-assignment flow

In `payload-galaxy-front`, chips live in the Payload `Chips` collection (`chipId` unique/required, `technology` ∈ {nfc,rfid,qr}, `printId → prints`, `isActive`). Certificates/escrows are one hop away via `Prints` (`blockchainCertificateId`, `blockchainTransactionId`). Provisioning should be **triggered from the ops side**: when a chip is created/activated in Payload (or assigned to a print), a Payload hook calls `POST /v1/chips { chipId }`. The `chipId` is the same String used everywhere on-chain. (Payload has no `escrows` collection — escrow state is on-chain / in the wallet-api only.)

---

## 3. The general signing capability

A verifier-agnostic primitive: **given a `chipId` and a payload, produce a 64-byte ECDSA-P256 signature over that payload with the chip account's key, gated on an Ixkio `Pass`.**

### 3.1 The signing convention (must match the on-chain verify exactly)

The on-chain check (`EscrowModule.verifyChipSignature`, `EscrowModule.cdc:162-180`) is:

```
PublicKey(chipPubKey, ECDSA_P256).verify(
    signature:            <64-byte r||s>,
    signedData:           challenge.utf8,          // raw UTF-8 bytes, NOT a hash
    domainSeparationTag:  "",                       // EMPTY
    hashAlgorithm:        SHA2_256                   // SHA-256, not SHA3
)
```

So the wallet-api primitive **must**: sign the **raw UTF-8 bytes** of the payload, with **no domain-separation tag**, hashing with **SHA2_256**, emitting a **64-byte raw `r||s`** signature (not DER). This is deliberately the *hardware-chip* convention (it is exactly what the model-A browser code `chipSignature.ts` produces: `ECDSA-P256-SHA256` over UTF-8, DER→raw) — which is why the future asymmetric self-signing chip is a drop-in.

> This is the crux of decision (b): it is **not** `flow.SignUserMessage` (that prepends `"FLOW-V0.0-user"`), and **not** the account's default transaction signer (that uses SHA3_256 and the transaction domain tag). See §4b for the mechanics.

### 3.2 Request / response (HTTP surface) — deferred, future/optional

The internal primitive (§4b) ships now, wired to escrow activation (§3.4, §5). The public HTTP route below is **not** part of what ships now — there is no non-escrow consumer today, and building an unused public signing endpoint doesn't serve either guiding constraint (§0). Deferred to Phase 3-or-later (§9), to be built when a real consumer needs it. Documented here so the shape is settled in advance and the primitive is designed generally enough to support it without rework.

`POST /v1/chips/{chipId}/sign` *(future)*

```jsonc
// request
{
  "payload": "…",                 // the exact string to be signed (UTF-8)
  "ixkio_tap": { "x": "…", "n": "…", "e": "…" }   // tap evidence; wallet-api verifies with Ixkio
}
// response
{
  "chipId": "chip-…",
  "publicKey": "…64 bytes hex…",
  "signature": [ /* 64 ints */ ],  // raw r||s, the shape the Cadence tx wants
  "signAlgo": "ECDSA_P256",
  "hashAlgo": "SHA2_256",
  "domainTag": ""
}
```

Flow inside the handler:

1. Verify the Ixkio tap (`ixkio_tap`) → `{xuid, Pass}` (see §4a). Reject on `Fail`.
2. Map `xuid → chipId` and assert it equals the `{chipId}` path param (a tap for chip A cannot sign for chip B).
3. Look up `chipId → account_address` in `artdrop_chips`.
4. Call the raw-sign primitive (§4b) over `payload` with that account's key.
5. Return the signature.

### 3.3 Anti-replay & payload shape

The primitive is payload-agnostic, so **anti-replay is a property of the payload, not the signer.** Two layers:

- **Ixkio single-use:** Ixkio enforces one `Pass` per tap counter (`n`/`e`), so a given tap cannot be reused to mint a second signature — off-chain.
- **Payload binding:** every consumer must put a nonce / unique context into the payload it asks the chip to sign. For escrow activation the payload is `"{nonce}:{buyer}:{escrowId}"`, and the chain independently re-derives and checks that binding *and* marks the escrow consumed (see §5). For a *new* future consumer, the wallet-api should require the payload to carry a nonce or be bound to the single-use Ixkio tap; a raw free-form payload with no nonce is replayable within one tap's validity and should be discouraged in the endpoint docs.

### 3.4 Escrow activation as the only wired consumer

Escrow activation is expressed as an **in-process** caller of the same primitive (not an HTTP round-trip to `/chips/{chipId}/sign`): the escrow-activation service resolves the escrow → `chipId`, builds the canonical challenge, calls the internal signing function with the Ixkio tap, and embeds the resulting signature into the activation transaction. This is the **only** consumer that ships now (§0). The HTTP `/sign` endpoint (§3.2) is deferred until a real non-escrow consumer exists.

**This is a separate tap from the consumer-facing certificate-lookup flow, and that flow is untouched.** ArtDrop already has a different, unrelated tap→redirect flow for consumers (`auth.artdrop.me` → `/api/ixkio/auth` in `payload-galaxy-front`, using Ixkio's *redirect* mode and its own token, per `src/lib/ixkio/auth-bridge.ts`) that lets a buyer tap their chip and see the certificate. That flow is not part of this design, is not modified by it, and uses a physically different tap event than the one that activates an escrow — see §4a for why the two must never share a tap.

---

## 4. The two open decisions — resolved

### (a) Does the wallet-api call Ixkio itself? — **Yes. Recommended.**

**Recommendation: the wallet-api calls Ixkio.** The wallet-api holds the chip private key. The whole security of a custodial model rests on the signer refusing to sign unless the tap is real. If the front verifies Ixkio and passes a `Pass` boolean (or an unverifiable "I checked" flag), then anyone who can reach the wallet-api API can assert `Pass` and make the chip sign anything — the gate is decorative. Having the wallet-api call Ixkio makes the *signer itself* confirm the tap, which is the only arrangement that actually binds "a real person tapped the real chip" to "the chip key produced a signature." Per CHIP-AUTH.md this couples only off-chain, and Ixkio stays swappable.

**Ixkio API mode — confirmed, no longer an open question.** Source: [Ixkio Flex API — NTAG424 authentication](https://docs.ixkio.com/flex-api/flex-api-getting-started/flex-api-ntag424-authentication). The machine-readable Pass/Fail "API mode" exists as documented:

```
GET https://api.ixkio.com/v1/t?x=<xuid>&n=<scan-count-hex>&e=<auth-code>&r=<response-API-token>
    (optional: a=<accountId>, c=<cuid> for CUID requests)
```

Responses (verbatim shape):

```jsonc
// success
{ "xuid": "q8w3sbcz", "response": "Pass" }
// fail
{ "xuid": "q8w3sbcz", "response": "Fail" }
// cuid variant
{ "xuid": "q8w3sbcz", "cuid": "yourcuid", "response": "Pass" }
// error (e.g. inactive batch)
{ "xuid": "q8w3sbcz", "error": "batch_inactive" }
```

So the client is just this `GET` plus `response == "Pass"` — no inference from a redirect needed. **The front today uses a *different* Ixkio mode (redirect, for the separate consumer certificate-lookup flow — see §3.4), so this is a dashboard/config setup dependency for the wallet (provisioning its own "response API token" for API mode), not a code unknown.**

**Design of the wallet-api Ixkio client:**

- New config: `ARTDROP_IXKIO_API_URL` (default `https://api.ixkio.com/v1/t`), `ARTDROP_IXKIO_RESPONSE_TOKEN` (the `r` token — distinct from the front's redirect-mode `IXKIO_RESPONSE_API_TOKEN`; do not reuse the front's token, it's provisioned for the wrong mode), `ARTDROP_IXKIO_ENABLED` (bool, to allow a dev/testnet bypass — see §8 Test strategy).
- New `ixkio.Client` in the artdrop plugin: `Verify(ctx, tap {x,n,e}) (xuid string, pass bool, err error)`. It does the `GET /v1/t?x=&n=&e=&r=<token>` above with `cache: no-store`, parses the `{xuid, response}` / `{xuid, error}` shapes, returns `pass = (response == "Pass")`, surfaces `error` as a hard failure (never treated as `Pass`).

**Single-use tap — the architecture fix this resolves.** Ixkio enforces single-use per scan-count `n` (anti-replay): a given `{x,n,e}` can only be verified once. This means **the front must never call Ixkio itself and then also forward the tap to the wallet** — that would consume the single use, and the wallet's own verify would then see it as already-spent and fail. So for escrow activation, the front's role is purely to **forward the raw `{x,n,e}` tap params to the wallet-api unverified**; the wallet-api performs the *one* Ixkio verification for that tap. This is already how §3.4/§5 describe the flow — stated here explicitly because it's the reason decision (a) has to be "wallet calls Ixkio," not "front calls Ixkio and passes the result": either the front verifies (weak, forgeable) or the wallet verifies (strong), but never both against the same tap.

**xuid → chipId mapping is our data, not Ixkio's.** Ixkio returns `xuid` (its tag identifier); it has no notion of `chipId`. The `xuid ↔ chipId` mapping lives in data we control — the new `artdrop_chips` table (§2.3, which already has an `ixkio_xuid` column) and/or the Payload `Chips` collection (which already has `chipId` and `cuid`). It's an implementation detail pinned at build time (e.g., whether `xuid == chipId` or `cuid == chipId` for a given deployment), not something blocked on Ixkio.

**The rejected alternative (front passes Pass):** simpler (no Ixkio client in the wallet-api, no tap forwarding), but weaker — the wallet-api can't distinguish a real Pass from a forged one, so the chip key is effectively ungated behind the API scope alone. Only acceptable if the wallet-api API itself is a fully trusted, non-public, mTLS-only backend surface — a posture we should not assume for a service that will hold funds.

### (b) Can flow-wallet-api sign arbitrary data with a custodial key today? — **Yes at the SDK level; a small addition is needed to match the chip verify.**

**What exists.** All signing goes through the flow-go-sdk `crypto.Signer` interface, whose method is `Sign(message []byte) ([]byte, error)` — already generic over arbitrary bytes (`flow-go-sdk crypto.go:88-94`). `keys.Manager.UserAuthorizer(ctx, address)` (`keys/keys.go:35`) returns a `keys.Authorizer{Address, Key, Signer}` (`keys/keys.go:90-94`) carrying a live `crypto.Signer` for any custodial address, uniformly across the local / Google-KMS / AWS-KMS backends (`keys/basic/keys.go:159-250`). Transaction signing is just one caller of this (`transactions/service.go:281,287` via `SignPayload`/`SignEnvelope`). The SDK even ships `flow.SignUserMessage(signer, message)` (`sign.go:60-67`) for on-chain-verifiable raw messages.

**What's missing / the two mismatches.** Neither ready-made path matches `verifyChipSignature`:

1. **Hash algorithm.** The account's stored signer is built with the key's registered hash algo — **SHA3_256** by default (`DEFAULT_HASH_ALGO=SHA3_256`, `configs/configs.go:62`; hashing happens *inside* the signer, e.g. `InMemorySigner.Sign` calls `PrivateKey.Sign(message, s.Hasher)`). The on-chain check uses **SHA2_256**. Signing with the default account signer would produce a SHA3-based signature the SHA2 verify rejects.
2. **Domain separation tag.** `flow.SignUserMessage` prepends `"FLOW-V0.0-user"`; `SignPayload`/`SignEnvelope` prepend the transaction tag. The on-chain check uses an **empty** tag. So no existing helper produces the right pre-image.

Note the hash algo is **not** part of the keypair — the same P-256 private key can be signed with a SHA2_256 hasher and its (unchanged) pubkey still verifies. So re-hashing with SHA2_256 is legitimate and does not require a different key.

**The minimal addition.** A new service method, e.g. `signChipChallenge(ctx, chipAddress, payload []byte) ([]byte, error)`, that:

- loads the account's decrypted key via the existing `keys.Manager` path (same as `UserAuthorizer`), and
- **for local ECDSA_P256 keys** (testnet + default), constructs a one-off signer with a **SHA2_256** hasher from the same private key — `crypto.NewInMemorySigner(privKey, crypto.SHA2_256)` — and calls `.Sign(payload)` with **no domain tag prepended**, then ensures a 64-byte `r||s` output (the SDK's ECDSA signer already emits raw `r||s`, so no DER→raw step is needed on the Go side).

This reuses all existing key-loading, decryption, and storage; it adds no new key material handling. It should be routed through the same `accounts.CustodialSigningGuard` that gates transaction signing (`transactions/service.go:233-237`) plus a new scope (§6), since it is a second privileged use of custodial key material.

**KMS caveat to flag.** The KMS backends need care: the **AWS** signer path uses **secp256k1** + SHA3_256 (`keys/aws/aws.go`) — wrong curve *and* wrong hash for a P-256 chip; AWS-backed keys cannot be chip accounts as-is. **Google KMS** and **local** can hold P-256 keys, but the hasher must be SHA2_256 for the signature to verify. So a production constraint is: **chip accounts must be provisioned as ECDSA_P256 keys, and the raw-sign path must force SHA2_256** regardless of the account's registered account-key hash algo. Worth an explicit guard that rejects provisioning a chip account on a non-P256 key type.

---

## 5. Escrow activation, reworked

### Today

`artdrop/service.go:590-607` `ActivateChip` relays a **client-supplied** `{challenge, signature}` straight into the transaction:

```go
// ActivateChipRequest (types.go:117-121): { EscrowId, Challenge string, Signature []byte }
args := { logicOwner, escrowId, cadence.String(req.Challenge), newUInt8Array(req.Signature) }
Transactions.Create(ctx, sync, address /* the buyer path param */, activateChipAndSettleCDC, args, …)
```

The tx (`cdc/activate_chip_and_settle.cdc`) is signed by the buyer (path `{address}`), whose address becomes `activator`; the chain requires `activator == escrow.buyer` and re-derives+checks the challenge binding before verifying the signature against the frozen `chipPubKey`.

### Under model B

The wallet-api **produces** the signature instead of relaying it:

1. Request body drops `challenge` and `signature`; it carries only the Ixkio tap: `ActivateChipRequest { ixkio_tap {x,n,e} }`. (`EscrowId` was already dead — the real id comes from the URL path.)
2. `ActivateChip`:
   - resolves the escrow (`logicOwner` from config, `escrowId` from path) to read its `buyer`, `nonce`, and `chipId` (a read script, or reuse the existing escrow-summary read);
   - builds the canonical challenge **server-side**: `"{nonce}:{buyer}:{escrowId}"` (matches `EscrowModule.buildChallenge`, `EscrowModule.cdc:136-160`);
   - calls the internal signing primitive (§4b) — `signChipChallenge(chipAccountForChipId, challenge)` — **gated on the Ixkio tap** (§4a) for that chipId;
   - submits `activate_chip_and_settle` with `challenge` + the wallet-produced `signature`, **signed by the buyer's custodial account** (same as today — the buyer is custodied by the wallet-api, and `activator` must equal `escrow.buyer`).

**Two keys, both custodied by the wallet-api, in one activation:** the **buyer account key** signs the Flow transaction; the **chip account key** produced the challenge signature carried inside it.

### Code deltas (wallet-api)

- `artdrop/types.go`: `ActivateChipRequest` loses `Challenge` and `Signature`, gains `IxkioTap`. (The existing doc comment already warns against re-adding client-controlled cert fields — this continues that direction by also removing the client-controlled signature.)
- `artdrop/service.go` `ActivateChip`: add the escrow read → challenge build → Ixkio-gated internal sign → submit. The `cdc/activate_chip_and_settle.cdc` transaction is **unchanged** (it still takes `challenge` + `signature` args; only their *source* changed).
- `artdrop/handler.go` `ActivateChipFunc`: parse the new body; unchanged routing (the three aliases at `plugin.go:155-157`).
- No **on-chain** change to activation — the Cadence interface is untouched, per CHIP-AUTH.md's stable-interface principle.

---

## 6. Security model

- **Custodial, not proof-of-possession.** The chain's guarantee is only "this activation carries a valid signature from the registered pubkey." Under model B that pubkey belongs to a key the wallet-api holds, so verifying it proves the wallet-api signed — **you are trusting ArtDrop's backend + Ixkio**, exactly as CHIP-AUTH.md states. This must be documented as custodial, never as cryptographic proof the physical chip signed.
- **The chip private key never leaves the wallet-api.** Stored encrypted in `storable_keys` (AES-GCM or KMS-wrapped per `keys/encryption`), decrypted only transiently in memory during signing. Prefer Google-KMS-backed P-256 keys in production so the raw key never materializes outside the HSM (with a SHA2_256 signing config).
- **The trust boundary / who can make a chip sign:** the *conjunction* of (1) a valid Ixkio `Pass` for that chip's tap and (2) the API scope. Ixkio is the "a real tap happened" gate; the scope is the "this caller is allowed to ask" gate. Losing either weakens to the other alone — which is why decision (a) puts the Ixkio check inside the signer.
- **Anti-replay:** Ixkio single-use tap (`n`/`e`) off-chain; challenge bound to `nonce+buyer+escrowId` and escrow marked consumed on-chain (`runActivateChipAndSettle` re-derives the binding and flips escrow status). General `/sign` callers must supply a nonce in the payload (§3.3).
- **Scopes (openapi `x-required-scopes`, mapped by `auth/exchange/policy.go`):**
  - **Provisioning** `POST /v1/chips` → a new **operations**-bucket scope, e.g. `chip.provision` (same class as `account.artdrop.escrow.reescrow` / artist onboard).
  - **Signing** `POST /v1/chips/{chipId}/sign` *(deferred, not shipped now — §3.2, §9)* → when built, a new **action** scope, e.g. `chip.sign`. Because Ixkio is the real gate, this scope can be an action scope, but given it wields custodial keys, treat it as tightly held (operations-adjacent), not broadly granted to end users.
  - **Escrow activation** keeps its existing `account.artdrop.escrow.activate` scope.
  - Every new route needs its `x-required-scopes` entry in `openapi.yml` or the service **crash-loops at startup** (`auth/openapi/loader.go:36-49` requires exactly one per operation — this is the known wallet-api footgun; see the `wallet-api-route-needs-openapi-entry` memory).

---

## 7. Future migration (asymmetric self-signing chip)

The whole point of the generic on-chain interface is that this is an **off-chain-only** change:

- **On-chain: no-op.** An asymmetric chip (Arx HaLo / Kong, ECDSA P-256) has its *own* keypair. Provisioning simply registers the chip's own pubkey via `registerChipPublicKey` / `replaceChipPublicKey` (or a re-key on an existing chip). `verifyChipSignature` is identical — it just verifies against whatever pubkey is registered. No Cadence change.
- **Off-chain: a per-chip mode flag.** The `artdrop_chips.signing_mode` column (§2.3) selects behavior:
  - `custodial` → wallet-api signs (today's model B); Ixkio-gated.
  - `self` → the chip signs directly; the front POSTs the chip-produced `{challenge, signature}` and the wallet-api **does not** sign and **does not** need Ixkio (the signature *is* the proof of possession — trustless). For those chips, escrow activation reverts to relaying a client signature (the model-A shape), but now legitimately, because the signer is real hardware.
- **Dropping Ixkio entirely:** for `custodial` chips, swap the `ixkio.Client` for another verifier behind the same `Verify(tap) (id, pass)` interface — no on-chain change, no signing change. For `self` chips, no verifier is needed at all.

This mirrors CHIP-AUTH.md's "mark each certificate with which model it uses (custodial-proxy vs chip-self-signed) so old and new chips coexist." Keep the flag per-chip (and thus effectively per-certificate, since chip↔cert is 1:1 via the registry's `ChipCertificateIndex`).

---

## 8. Test strategy

Layered from deterministic/no-dependency to real-hardware, so the design is fully testable before any physical chip or live Ixkio account exists. Serves both guiding priorities from §0: the bypass flag (layer 4) is what makes "works now" actually verifiable end to end; the `Verify` interface + `signing_mode` flag (layers 2 and 5, and §7) are what make "dropping Ixkio later is easy" a testable claim, not just an architecture diagram.

1. **Raw-sign primitive — the load-bearing test.** Two parts:
   - Go unit: generate a P-256 key, sign a challenge with SHA2_256 + empty domain tag (§4b), verify the 64-byte `r||s` off-chain with Go `crypto/ecdsa`.
   - The real proof: `flow scripts execute` against the actual deployed `EscrowModule.verifyChipSignature` on testnet/emulator with `{pubkey, challenge, signature}` from the step above, asserting `true`. This is what actually proves the Go signing output matches the on-chain verify's expectations (SHA2_256, empty tag, raw `r||s`) — per [[verify-in-protocol-then-mirror-to-wallet-api]], test the real running verify, not only a Go-side unit test.
2. **Ixkio client.** Stub an HTTP server returning the documented JSON shapes from §4a (`{...,"response":"Pass"}`, `{...,"response":"Fail"}`, `{...,"error":"..."}`); assert `ixkio.Client.Verify` maps each correctly. A fake verifier implementing the same `Verify` interface exercises that the signing primitive is swappable, independent of the real Ixkio client.
3. **Provisioning.** Create a chip account → register its pubkey via the delegated `ChipAdmin` cap (§2.2) → read it back on-chain → assert the `artdrop_chips` mapping persists correctly.
4. **Activation end to end — the key to "works now."** A config flag, `ARTDROP_IXKIO_ENABLED=false`, that on testnet bypasses the Ixkio call and treats every tap as `Pass`. With it: create escrow → activate (wallet signs the challenge with the chip account) → assert the escrow settles and the certificate transfers on-chain — the **whole flow**, with **no physical chip and no live Ixkio dependency**. Plus the root-package `Auth`/`Route`/`Scope` tests for the new routes (the `openapi.yml` startup-crash guard — see [[wallet-api-route-needs-openapi-entry]]).
5. **One manual physical-tap test before mainnet.** Confirms the real Ixkio API mode end to end and the real `xuid → chipId` mapping for a physical chip. This one step cannot be automated — everything else in this list can.

---

## 9. Phased implementation plan

Ordered so each phase is independently buildable/deployable. Pure wallet-api unless marked **[protocol]**.

**Phase 0 — raw-sign primitive (no on-chain dependency).**
- Add `signChipChallenge(ctx, address, payload)` to the artdrop/transactions service (§4b): load key via `keys.Manager`, build a SHA2_256 InMemorySigner for local P-256 keys, sign raw bytes, return 64-byte `r||s`. Guard with `CustodialSigningGuard`.
- Tests: §8 layer 1 (Go unit + `flow scripts execute` against `EscrowModule.verifyChipSignature`).

**Phase 1 — Ixkio client.**
- `ixkio.Client.Verify(tap)`; config (`ARTDROP_IXKIO_*`); the `ARTDROP_IXKIO_ENABLED=false` testnet bypass flag. Tests: §8 layer 2 (stubbed HTTP server).

**Phase 2 — chip provisioning [protocol + wallet-api].**
- **[protocol]** Add `ChipAdmin` entitlement + re-gate `registerChipPublicKey`/`replaceChipPublicKey` to it (§2.2 — confirmed update-safe in place, no redeploy); `grant_chip_admin_cap.cdc` (A3), `claim_chip_admin_cap.cdc` (wallet-api), `register_chip_via_delegated_cap.cdc`.
- `artdrop_chips` table + migration; `POST /v1/chips` (create account → persist mapping → submit registration), `GET /v1/chips/{chipId}`.
- `openapi.yml` entries + `chip.provision` scope in `policy.go`. Tests: §8 layer 3, plus the root-package Auth|Route|Scope test (the startup-crash guard).

**Phase 3 — escrow activation rework.**
- `ActivateChipRequest` slims to the Ixkio tap; `ActivateChip` reads escrow → builds challenge → Ixkio-gated internal sign → submits (buyer-signed). No Cadence change. Update `service.go`, `handler.go`, tests; keep the three route aliases.
- Front: drop `chipSignature.ts` (model A) and its test; thin the activate-chip request body (§10) to forward the raw tap.
- Tests: §8 layer 4 — this is the phase where the bypass-flag end-to-end test proves the feature works now.

**Phase 4 — general signing endpoint (future / optional).**
- `POST /v1/chips/{chipId}/sign` wiring Phase 0 + Phase 1 (Ixkio-gate → xuid→chipId → sign). `openapi.yml` + `chip.sign` scope. Build only once a real non-escrow consumer exists (§0, §3.2).

**Phase 5 — future-proofing.**
- `signing_mode` column honored end-to-end; `self`-mode path (relay client signature, skip Ixkio). Ships whenever asymmetric chips arrive; no on-chain work.

**Before mainnet:** §8 layer 5, the one manual physical-tap test.

**What needs a protocol change:** only the `ChipAdmin` entitlement addition + the three delegated-cap transactions in Phase 2 — confirmed update-safe in place (§2.2), no redeploy, no filed follow-up issue needed. Everything else is pure wallet-api (Go + bundled Cadence transactions/scripts) plus front removals.

---

## 10. Conflicts & open questions for the tech lead

1. **Front model-A scaffolding is dropped.** `payload-galaxy-front/src/lib/blockchain/chipSignature.ts` generates the P-256 keypair **in the browser** (WebCrypto, extractable `CryptoKey`) and signs client-side — that is model A, and model B replaces it. Confirmed it has **zero production callers** (only `tests/unit/blockchain-chip-signature.spec.ts` and a stale prose reference in `activate-chip/route.ts`). Removing it means deleting the module + its test and rewriting the doc comment in `src/app/api/blockchain/escrows/[escrowId]/activate-chip/route.ts`. The wire client `src/lib/blockchain/escrow.ts` and the API route currently POST `{challenge, signature}`; under model B the front stops producing a signature and instead forwards the **raw Ixkio tap unverified** (§4a's single-use constraint) — the request body and the wire-contract test change accordingly. The separate consumer-facing redirect-mode tap flow (§3.4) is untouched.
2. ~~Ixkio endpoint mode~~ — **resolved, see §0 and §4a.** The remaining sub-question — how `xuid` maps to `chipId` — is also resolved as "our data, pinned at build time" (§4a); no further tech-lead input needed unless the team wants to choose `xuid` vs `cuid` as the pinned identifier explicitly.
3. ~~Breadth of the delegated cap~~ — **resolved, see §0 and §2.2.** Recommendation is `ChipAdmin` now, not broad `OperationalAdmin` + a deferred issue — it's update-safe in place, so there's no cost trade-off to accept.
4. **Chip = full Flow account — RESOLVED (accepted).** A full custodial Flow account per chip (not a lighter managed keypair). The existing key store is account-indexed, so the account reuses all existing infrastructure. The account-creation fee is not a concern: the per-piece **5% gas reserve** (the escrow reserve funded from ArtDrop's vault) is precisely the budget that absorbs the on-chain costs per piece — chip-account creation, escrow, and activation. So "an account per chip" is the intended design, funded by the 5% already charged per work.
5. **KMS curve/hash (§4b).** Production chip accounts must be **ECDSA_P256** with **SHA2_256** signing. AWS-KMS keys (secp256k1) cannot back chip accounts. Confirm the target key backend (local vs Google KMS P-256) for production chip custody.
6. **Re-key semantics (§1).** Because `Escrow.chipPubKey` is frozen at creation, `replaceChipPublicKey` does not affect existing escrows. Confirm that is the intended operational behavior (it matches the contract's design and the `ChipCertificateIndex` "lookup yes, authority no" note), and that re-key is only expected before an escrow exists for a chip.
7. **General `/sign` anti-replay policy (§3.3), when Phase 4 is eventually built.** For future non-escrow consumers, do we mandate a nonce-in-payload convention, or bind every signature to a single-use Ixkio tap? Recommend requiring a caller-supplied nonce and documenting it as a hard requirement of the endpoint.
