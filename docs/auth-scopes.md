# Wallet API Auth Scopes (W-01)

This document defines the operation-level scope model for the Wallet API.

## Bearer Token Model

- Auth scheme: `Authorization: Bearer <jwt>`
- Token format: JWT (HS256)
- Required claims:
  - `exp` (token must be unexpired)
  - `scope` (space-separated scopes)
- Optional claims:
  - `iss` (checked when configured)
  - `aud` (checked when configured)

## Scope Matrix

All `/v1/*` endpoints require a Bearer token.

| Method | Path | Required scope |
|---|---|---|
| GET | `/debug` | `system.read` |
| GET | `/health/ready` | `health.read` |
| GET | `/health/liveness` | `health.read` |
| GET | `/system/settings` | `system.read` |
| POST | `/system/settings` | `system.write` |
| POST | `/system/sync-account-key-count` | `account.key.sync` |
| GET | `/jobs` | `job.read` |
| GET | `/jobs/{jobId}` | `job.read` |
| GET | `/tokens` | `token.read` |
| POST | `/tokens` | `token.write` |
| GET | `/tokens/{id_or_name}` | `token.read` |
| DELETE | `/tokens/{id}` | `token.write` |
| GET | `/fungible-tokens` | `token.read` |
| GET | `/non-fungible-tokens` | `token.read` |
| GET | `/transactions` | `transaction.read` |
| GET | `/transactions/{transactionId}` | `transaction.read` |
| GET | `/accounts` | `account.read` |
| POST | `/accounts` | `account.create` |
| GET | `/accounts/{address}` | `account.read` |
| POST | `/accounts/{address}/sign` | `account.sign` |
| GET | `/accounts/{address}/transactions` | `transaction.read` |
| POST | `/accounts/{address}/transactions` | `transaction.create` |
| GET | `/accounts/{address}/transactions/{transactionId}` | `transaction.read` |
| POST | `/watchlist/accounts` | `watchlist.write` |
| DELETE | `/watchlist/accounts/{address}` | `watchlist.write` |
| POST | `/scripts` | `script.execute` |
| GET | `/accounts/{address}/fungible-tokens` | `account.setup` |
| GET | `/accounts/{address}/fungible-tokens/{tokenName}` | `account.setup` |
| POST | `/accounts/{address}/fungible-tokens/{tokenName}` | `account.setup` |
| GET | `/accounts/{address}/fungible-tokens/{tokenName}/withdrawals` | `account.transfer` |
| POST | `/accounts/{address}/fungible-tokens/{tokenName}/withdrawals` | `account.transfer` |
| GET | `/accounts/{address}/fungible-tokens/{tokenName}/withdrawals/{transactionId}` | `account.transfer` |
| GET | `/accounts/{address}/fungible-tokens/{tokenName}/deposits` | `account.transfer` |
| GET | `/accounts/{address}/fungible-tokens/{tokenName}/deposits/{transactionId}` | `account.transfer` |
| GET | `/accounts/{address}/non-fungible-tokens` | `account.setup` |
| GET | `/accounts/{address}/non-fungible-tokens/{tokenName}` | `account.setup` |
| POST | `/accounts/{address}/non-fungible-tokens/{tokenName}` | `account.setup` |
| GET | `/accounts/{address}/non-fungible-tokens/{tokenName}/withdrawals` | `account.transfer` |
| POST | `/accounts/{address}/non-fungible-tokens/{tokenName}/withdrawals` | `account.transfer` |
| GET | `/accounts/{address}/non-fungible-tokens/{tokenName}/withdrawals/{transactionId}` | `account.transfer` |
| GET | `/accounts/{address}/non-fungible-tokens/{tokenName}/deposits` | `account.transfer` |
| GET | `/accounts/{address}/non-fungible-tokens/{tokenName}/deposits/{transactionId}` | `account.transfer` |
| GET | `/ops/missing-fungible-token-vaults/start` | `ops.run` |
| GET | `/ops/missing-fungible-token-vaults/stats` | `ops.read` |

## Token Issuance Flow

1. Auth service validates machine/user identity.
2. Auth service issues short-lived JWT with least-privilege `scope`.
3. Wallet API verifies signature and claims (`exp`, optional `iss`, optional `aud`).
4. Wallet API authorizes per-endpoint scope and logs failed auth events.

Recommended controls:

- Keep token TTL short (for example, <= 15 minutes for write scopes).
- Separate read and write clients.
- Rotate JWT signing secret regularly and support overlapping keys during rotation.
- Never include private key material in token claims or logs.
