# Plan — PoolPriceOracle (FLOW/USD desde pools de Flow EVM) · issue #121

Reemplaza el oráculo de precio del flujo de compra por una lectura on-chain de pools de Flow EVM. Drop-in detrás de la interfaz `PriceOracle` existente (cero cambios en service.go/deps.go).

## Decisiones del tech lead (firmes)
- **Pyth: ELIMINADO por completo** — borrar `artdrop/purchase/pyth.go` (PythClient), `FLOW_WALLET_PYTH_API_KEY`, la config Hermes (PythHermesBaseURL/FeedID/APIKey), y su rama de wiring. Sin fallback a Pyth.
- **Precio fijo (de Edgar #120): SE MANTIENE como override opt-in** — NO borrar `fixed.go`. Env con el MISMO nombre `FLOW_WALLET_DEV_FLOW_USD_PRICE`. Semántica: si está seteado (>0) Y la red NO es mainnet → usa FixedPriceOracle (para QA). En mainnet, si está seteado → rechazar al arranque (mantener el guard de Edgar).
- **Default en ambas redes = PoolPriceOracle.**
- **Precedencia de wiring:** (1) fixed price opt-in [non-mainnet + env>0] → (2) PoolPriceOracle.
- Resiliencia sin Pyth = failover de URLs de RPC + cache TTL. Aceptable.

## Hechos verificados on-chain (Flow EVM mainnet, chainId 747, RPC https://mainnet.evm.nodes.onflow.org)

Tokens (symbol/decimals confirmados):
- WFLOW `0xd3bf53dac106a0290b0483ecbc89d40fcc961f3e` (18)
- PYUSD0 `0x99af3eea856556646c98c8b9b2548fe815240750` (6)
- USDF `0x2aabea2058b5ac2d339b163c6ab6f2b6d53aabed` (6)
- stgUSDC `0xf1815bd50389c46847f0bda824ec8da914045d14` (6)

Pools verificados (default hardcodeado, todos ~$0.0262 dentro de 0.1%):
| addr | tipo | token0 | token1 | wflowEsToken0 | stableDec | TVL |
|---|---|---|---|---|---|---|
| `0x0fdba612fea7a7ad0256687eebf056d81ca63f63` | v3 | PYUSD0 | WFLOW | false | 6 | $742K |
| `0xfc18d92085fa9df01be5985e5d890b4a4d7edad9` | v2 | PYUSD0 | WFLOW | false | 6 | $304K |
| `0x17e96496212d06eb1ff10c6f853669cc9947a1e7` | v2 | USDF | WFLOW | false | 6 | $297K |
| `0xd21c58adaf1d1119fe40413b45a5f43d23d58df3` | v3 | USDF | WFLOW | false | 6 | $247K |
| `0xc0f7bd6485a30b743f5a704d3bf11070806ce073` | v3 | WFLOW | stgUSDC | **true** | 6 | $41K |

**OJO: el último pool tiene WFLOW como token0 (orientación invertida) — la orientación y los decimales del stable SON CONFIG POR-POOL, nunca inferidos.**

Selectores: `token0()=0x0dfe1681`, `token1()=0xd21220a7`, `decimals()=0x313ce567`, `getReserves()=0x0902f1ac` (v2), `slot0()=0x3850c7bd` (v3, primera palabra 32B = sqrtPriceX96), `balanceOf(addr)=0x70a08231`+addr padded 32B.

Hallazgos clave: (1) `balanceOf(pool)` sirve para TVL en v2 Y v3 (no hace falta math de ticks/liquidity de v3). (2) El RPC soporta batch JSON-RPC (array) → 1 round-trip por refresh.

## Diseño

**`artdrop/purchase/pooloracle.go`** implementa `PriceOracle` (interfaz existente, `Latest(ctx) (*PythPrice, error)` — devolver `&PythPrice{PriceUSD: final, PublishTime: readTime}`; mantener el tipo para no tocar el call site).

Pipeline `Latest()`:
1. **Cache + TTL** (default 45s): si `now-cachedAt < TTL` devolver cacheado. **Single-flight** (sync.Mutex o x/sync/singleflight) → una relectura por ráfaga. Reloj inyectable (`now func() time.Time`) para tests. Ante error de relectura: **fallar** (no servir vencido).
2. **Read** — un batch `eth_call`: por pool v2 `getReserves()`; por pool v3 `slot0()` + `balanceOf(token0)` + `balanceOf(token1)`. Un POST array con timeout corto. Failover: probar URLs en orden.
3. **Spot por pool** (stable = $1.00):
   - v2: `price = (reserveStable/10^stableDec)/(reserveWFLOW/10^18)`, orientado por wflowEsToken0. TVL = `stableUSD + wflowUSD·price` (de los reserves).
   - v3: de `sqrtPriceX96`: `pRaw=(sqrtP/2^96)^2` (token1-raw/token0-raw); ajustar decimales; invertir si WFLOW es token1 → USD/WFLOW. TVL = de los dos `balanceOf`. **Usar `math/big`** (sqrtP hasta 2^160, al cuadrado desborda float64).
4. **Rechazo por desviación:** exigir ≥`minPools` leídos (default 3) o ERROR. Calcular **mediana**. Descartar pools con `|precio−mediana|/mediana > maxDeviation` (default 200 bps=2%). Exigir ≥`minSurvivors` sobrevivientes (default 2) o ERROR.
5. **Ponderación por TVL:** `precioFinal = Σ(precio_i·tvl_i)/Σ(tvl_i)` sobre sobrevivientes. (Peso: usar el spot propio de cada pool para su TVL — circularidad despreciable.)
6. **Sanity bound:** rechazar si `precioFinal ∉ [sanityMin, sanityMax]` (default [$0.0005, $2.0]) → ERROR.

Filosofía: **ante la duda, fallar el charge** (un monto equivocado es peor que un charge fallido y reintentable).

**`artdrop/purchase/evmrpc.go`** — cliente JSON-RPC mínimo, sin go-ethereum directo (`net/http`+`encoding/json`+`math/big`):
- `evmClient{ urls []string; http *http.Client }`, `BatchCall(ctx, []call) ([]json.RawMessage, error)` (array JSON-RPC, 1 round-trip, failover de URLs).
- Transport como interfaz para inyectar respuestas canned en tests.
- Decode: quitar 0x, `big.Int.SetString(hex,16)`, partir en palabras de 32B (getReserves=3 palabras uint112/uint112/uint32; slot0=palabra[0]; balanceOf=uint256).
- Opcional al arranque: verificar `decimals()`/`token0/1()` contra la config.

## Config (`artdrop/config.go`, prefijo FLOW_WALLET_ARTDROP_)
- `FLOW_WALLET_ARTDROP_FLOW_EVM_RPC_URL` (default `https://mainnet.evm.nodes.onflow.org`, SIEMPRE mainnet aunque el wallet sea testnet; soportar lista separada por comas para failover)
- `FLOW_WALLET_ARTDROP_ORACLE_POOLS` (default vacío → set hardcodeado de 5; override "addr:v2|v3:wflowIsToken0:stableDec" CSV o JSON)
- `FLOW_WALLET_ARTDROP_ORACLE_TTL` (45s) · `_MAX_DEVIATION_BPS` (200) · `_MIN_POOLS` (3) · `_MIN_SURVIVORS` (2) · `_SANITY_MIN_USD` (0.0005) · `_SANITY_MAX_USD` (2.0) · `_RPC_TIMEOUT` (10s)
- Validar en normalizeAndValidate(): sanityMin<sanityMax, minSurvivors<=minPools, TTL>0.
- Funciona SIN config (defaults = los 5 pools verificados).

## Remover / mantener
- **REMOVER:** `pyth.go` + `pyth_test.go` (si existe), `FLOW_WALLET_PYTH_API_KEY` + PythHermes* de configs.go, y la rama Pyth en plugin.go.
- **MANTENER:** `fixed.go` + `fixed_test.go` (FixedPriceOracle), `FLOW_WALLET_DEV_FLOW_USD_PRICE` (mismo nombre) + su guard mainnet.
- **plugin.go wiring:** `if cfg.DevFlowUSDPrice > 0 && !mainnet { oracle = FixedPriceOracle } else { oracle = PoolPriceOracle }`. Sin Pyth.

## Tests
- Unit (`pooloracle_test.go`, transport falso con hex canned): happy (5 pools → ~$0.0262); decode v2 y v3 + **orientación invertida** (el pool stgUSDC); un outlier a $0.05 descartado; todos-outliers/muy-pocos → error; sanity reject ($3 → error); cache/TTL (2 Latest dentro de TTL = 1 read; avanzar reloj → relee); depeg de stable (pool 3% corrido → descartado por el gate 2%); minPools (mayoría falla → error).
- Integración en vivo (`//go:build integration`): RPC real mainnet, assert precio ∈ [$0.005,$0.20]. Gated para CI sin red.

## Fases (build order)
1. `evmrpc.go` + tests batch/decode.
2. Decoders puros precio+TVL por pool (v2/v3) + table tests.
3. Agregador (mediana/outlier/peso/sanity) función pura + tests.
4. Cache + `Latest()` implementando PriceOracle + tests TTL.
5. Config en artdrop/config.go + validación + tests.
6. Wiring en plugin.go (PoolOracle default; fixed-price override; SIN Pyth) + remover pyth.go/config Pyth.
7. Test de integración en vivo.

## Deploy/transición
Swap drop-in, neutro al storage → no dispara "storage change needs consult". Deploy **testnet primero** (gana precio real), smoke-test `POST /v1/purchases:charge`, luego mainnet. En el deploy: **dessetear `FLOW_WALLET_DEV_FLOW_USD_PRICE`** en testnet (para que use el oráculo por default) — dejarlo solo si se quiere fijar para una prueba puntual.
