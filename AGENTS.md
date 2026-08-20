# AGENTS.md — flow-accounts-manager (Wallet API)

Instructions for AI agents (and humans). Assume every executor is an AI agent. Read before coding.

## Context

Custodial Wallet API for ArtDrop (Go, Phoenix fork). Tasks here are **W-01..W-07** of the ArtDrop V2
plan. Master plan: `plan/tasks.yaml` (canonical copy in `4rtdr0p/artdrop-protocol`).
Tracking model + multi-repo map: see `artdrop-protocol/docs/OPERATIONS.md`.

## Golden rule

One task = one issue = one branch = one PR. Merge is the only "done".
Branch `task/<ID>-<slug>` (e.g. `task/W-02-account-endpoints`), commits `<ID>: <imperative>`,
PR body ends with `Closes #<n>`.

## Code order (this stack)

- **Keys never leave the HSM/KMS.** Never log, return, or persist key material. `/sign` returns a
  signature, never the key.
- **Every endpoint is scoped by Bearer token** (operation-level scope). 401 without token, 403 on wrong scope.
- Account creation is **idempotent** (retries must not create duplicates). Key rotation and
  graduation are **atomic** (add new key + revoke old in one tx — never leave an account keyless).
- Go: `gofmt`/`go vet` clean, wrap errors with context, table-driven tests.
- **Tests mandatory**: happy path **and** error paths — auth (expired/wrong scope), idempotency, and the
  atomic rotation/graduation invariants. Run `go test ./...`.
- **Any change that adds or edits a route (new endpoint, changed path, changed method) must also run
  `go test -run 'Auth|Route|Scope' .` from repo root** — `TestOpenAPIScopeIndexCoversFullRouter` lives
  there and is what actually catches a route registered without a matching `x-required-scopes` entry
  in `openapi.yml`. The service validates this at startup and exits fatally if it's missing, so a
  gap here doesn't fail in CI — it crash-loops the deployed pod instead (PR #83 shipped exactly this
  gap once; `go build`, `go vet`, `gofmt`, and `go test ./artdrop/...` all pass without the fix, since
  none of them touch the root package's route/scope index). Scoping test runs to a subpackage
  (`./artdrop/...`) is not equivalent to `./...` from root, and neither one substitutes for the
  targeted `-run 'Auth|Route|Scope' .` — run it explicitly whenever a route changes.

## Reporting protocol (MANDATORY — comment on YOUR issue)

1. **On claim** — `🤖 <agent> started` · one-line plan · branch name.
2. **On each check** — paste the **verdict** of `go test ./...` (pass/fail counts), lint/build, CI run link.
3. **On PR open** — comment the PR URL.
4. **On done** — final comment: what changed, final `go test` output, PR `#`.
5. **If blocked** — `⛔ blocked: <reason>`; cross-repo dep → link `owner/repo#n`.

The issue thread is the auditable trail; the lead reviews from there.

## Don'ts

- No work on `main`. No force-push. No secrets/keys committed (use env/KMS).
- Don't edit task **status** in `plan/tasks.yaml` (status lives in the issue/PR).
- W-* tasks are self-contained (depend only on each other); `unblock.yml` reopens them as deps close.
