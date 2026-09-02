# Spec — Completar el suite E2E del wallet-api (#7) contra el emulador

Basado en el plan del e2e-planner (aterrizado, corrió todo contra el emulador). Rama: `feat/7-e2e-suite`. Repo: /home/oydual3/artdrop/flow-accounts-manager. Go: $HOME/.local/go/bin, flow: $HOME/.local/bin.

## PRIORIDAD (decisión del tech lead)
Lo VITAL es que funcionen los flujos que el FRONT va a cablear (issues Payload-Galaxy #284 auth, #497 chip, #495 escrow), más que la checklist vieja de #7. Entonces **Tier 2 (chip + escrow LIVE) es la prioridad**, no un "nice-to-have". Más el **E2E del auth-exchange (#284)**.

## DECISIONES ya tomadas (no preguntar)
1. **Graduate: borrar SOLO los tests** (`tests/w05_graduate_auth_test.go` + las aserciones de graduate en `tests/account_handler_test.go`). **DEJAR el endpoint** (removerlo tocaría openapi.yml → riesgo de crash-loop al arranque). 
2. **Item 3 de #7 (artist-activate/community-pool-enable): NO existen esos endpoints.** Re-scopear a **onboarding de artista via `SetupArtistDirect`** (que además es SETUP necesario para el ciclo de escrow — crear original/edición requiere un artista onboardeado).
3. **Stress test (T2):** si no hay Postgres fácil (entorno no-docker), gatearlo detrás de un DSN Postgres y SKIP si no está. No bloquear el goal por esto.

## RECIPE DE EMULADOR CONFIRMADO (Tier 1)
```
cd flow && nohup flow emulator --port 3569 > /tmp/emu.log 2>&1 &   # desde flow/, pidfile; NO pkill -f "flow emulator" (se auto-mata)
# esperar: flow blocks get latest --network emulator  (poll ~1s)
FLOW_WALLET_ACCESS_API_HOST=localhost:3569 FLOW_WALLET_CHAIN_ID=flow-emulator go test ./... -p 1
```
El harness Go auto-deploya los contratos estándar (FUSD/ExampleNFT via templates en main_test.go). Tier 1 NO necesita `flow project deploy`.

## FASE 0 — prereqs
- **Fix `make test`:** el `flow project deploy` falla por `import from FUSD could not be found: ./ViewResolver.cdc`. Agregar alias `ViewResolver` (y `MetadataViews`) para emulator = `0xf8d6e0586b0a20c7` en `flow/flow.json`. O simplificar el target `test` a start-emulator + run-tests (sin el `deploy` roto, ya que el harness auto-deploya). Recomendado: lo segundo, y dejar `deploy` como target aparte arreglado.
- **Borrar graduate tests** (decisión 1).
- **C-iii (source, prereq de Tier 2):** `artdrop/cdc.go` `substituteAddresses` reescribe solo ArtDropCore/Registry/EscrowModule/PaymentModule. `create_escrow.cdc`/`re_escrow.cdc` hardcodean `import FungibleToken from 0x9a0766d93b6608b7` (testnet) y nunca se reescribe → rompe en emulador (FungibleToken está en 0xee82856bf20e2aa6). **Extender `contractAddresses`/`substituteAddresses` para mapear FungibleToken y NonFungibleToken a la dirección de la cadena.** Cambio chico pero del que depende Tier 2.

## FASE 1 — Tier 1 (contratos estándar, bajo riesgo)
- **T1 — happy path encadenado (una cuenta):** POST /accounts → poll job → /setup → mint (ExampleNFT) → transfer FLOW a una 2da cuenta (assert vía chain-event listener) → rotate-key (assert old key revocada). Consolida el item 1 de #7.
- **T3 — auth edge cases:** completar lo que falta — missing Idempotency-Key → **400** (NO 409; 409 es solo duplicado in-flight), concurrent-dup → 409, expired-token → 401 en rutas que hoy solo testean wrong-scope. httptest puro.
- **AUTH-EXCHANGE E2E (#284) — VITAL:** generar una aserción RS256 de prueba (par de llaves de test) → `POST /v1/auth/token` → token scopeado → con `AUTH_ENABLED=true`, llamar un endpoint tipado y assert que el rol correcto pasa y el equivocado da **403**. Cubrir el mapeo rol→scopes (user no puede void; operations sí; admin tiene break-glass). Esto es emulator-testeable con contratos estándar (ej. gatear POST /accounts/{a}/setup).
- **T2 — stress:** 100 POST /accounts en paralelo, assert 100 direcciones distintas, sin dup/deadlock. Requiere Postgres (sqlite deadlockea — hay un skip existente que lo prueba). Gatear detrás de DSN Postgres; SKIP si no hay.

## FASE 2 — setup de Tier 2 (el make-or-break; los contratos ArtDrop viven en ../artdrop-protocol)
Un script `flow/e2e-setup-artdrop.sh` que:
- **C-i (service key):** arrancar UN emulador con service key compartida — más simple: `flow emulator --service-priv-key <K>` (o arrancarlo desde el repo protocol para que matchee su `.pkey`) Y setear `FLOW_WALLET_ADMIN_PRIVATE_KEY`=misma K, así el admin del wallet == el deployer ArtDrop == 0xf8d6e0586b0a20c7 (cada grant/claim = self-grant, el cableado más simple).
- **C-ii (EscrowModule 2 init args):** `flow project deploy` da "EscrowModule required arguments 2, but provided 0". **Confirmar en ../artdrop-protocol cuál es el deploy que SÍ produce un emulador funcional** (su `make deploy` es un `flow project deploy` pelado que no anduvo como está — hay que ver el deployment block con args o un deploy scripteado). Esto es territorio del repo protocol — apoyarse en SU path de deploy, no reinventarlo. **Si el deploy cross-repo no se puede correr limpio → PARAR y reportar, NO hand-rollear un deploy frágil.**
- Grant+claim de los caps delegados (ChipAdmin → /storage/artdropChipAdminCap; EscrowVoid OperationalAdmin → /storage/artdropEscrowVoidAdminCap). Self-grants si admin==deployer.
- Export env: `FLOW_WALLET_ARTDROP_CORE_ADDRESS/REGISTRY/ESCROW_MODULE/PAYMENT_MODULE/LOGIC_OWNER`=0xf8d6e0586b0a20c7; `ARTDROP_IXKIO_ENABLED=false`; `FLOW_WALLET_DEV_FLOW_USD_PRICE=0.02` (para que purchases:charge use FixedPriceOracle, no el RPC de mainnet).

## FASE 3 — Tier 2 tests (build tag `//go:build artdrop_e2e`, para que `go test ./...` normal siga verde/rápido)
- **T4 — chip provisioning (#497):** POST /v1/chips → assert cuenta P-256 creada + pubkey registrada on-chain (leerla de vuelta) + fila de mapeo en DB; re-POST idempotente = mismo mapeo, sin 2da llamada on-chain.
- **T5 — ciclo de escrow completo (#497/#495):** crear escrow (via `Service.CreateEscrow`/`EscrowCreator` directo para no arrastrar Mongo+Stripe del HTTP purchases:charge — mantenerlo hermético) → leer summary → **void** → **re-escrow** → **activate-and-settle** con Ixkio en bypass → assert Released + certificado transferido. Onboarding de artista + original + edición como setup (cubre el item 3 re-scopeado).
- **T6 — void aislado:** create → void → assert status=Voided (subconjunto de T5, caso propio).

## FASE 4 — make targets + corrida limpia
- `make test` → emulador (pidfile, desde flow/) + Tier 1. **Este es el "cualquiera lo corre y ve el happy path pasar".**
- `make test-artdrop-e2e` → `flow/e2e-setup-artdrop.sh` + `go test -tags artdrop_e2e ./...`. Tier 2. Documentar que depende del repo hermano artdrop-protocol.
- `make test-integration` → `go test -tags integration ./artdrop/purchase/...` (pool oracle live, mainnet). Separado.
- Correr `make test` limpio de punta a punta y capturar el output.

## KEEP SEPARADO
- T7 pool oracle live: ya existe (`purchase/pooloracle_live_test.go` `//go:build integration`, mainnet RPC). NO meter en el E2E de emulador.

## Realista para una noche
Si la Fase 2 (deploy cross-repo) resulta demasiado frágil, **shipear Fase 1 completa (Tier 1 + auth-exchange E2E) + dejar Tier 2 como script documentado**, y REPORTAR el bloqueo con detalle. La cobertura unit de artdrop ya prueba la lógica; el valor marginal de Tier 2 es la integración con contratos reales. Pero intentar Tier 2 en serio (es lo vital).
