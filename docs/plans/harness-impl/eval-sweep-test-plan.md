# Eval Sweep — Test Plan

**Date:** 2026-09-16 · **Status:** Draft (v1.0)
**Feature:** Eval Sweep — one suite × N-backends matrix run with a normalized, comparable `RunSet` result (Proposal 1 in `docs/plans/harness-research/enhancement-proposals.md` §1).

**Behavioural source of truth:** `docs/plans/harness-impl/eval-sweep-technical-spec.md` (the "technical spec"). Where this plan and any other doc disagree, the technical spec wins. Backend-wiring context: `docs/plans/harness-impl/eval-backend-integration.md` §3.

This document is self-contained: every code reference is verified against the current tree, and no reader is assumed to have seen the other harness-impl docs.

---

## 0. Test strategy

| Aspect | Decision |
|---|---|
| Levels | Unit (planner math, grading, aggregation) → Integration (REST/MCP/CLI/comm + stub backend) → Smoke (daemon end-to-end, human-rendered table) |
| Backend isolation | All non-smoke tests use a **stub dispatcher** (`inference.Dispatcher` fake) — no live LLM calls, deterministic |
| Harness style | Reuse the existing style in `internal/evals/evals_test.go`: `t.TempDir()` + `NewRunner(dir)` (evals.go:114), fixtures written as YAML into `r.SuitesDir()`, plain `t.Errorf` assertions, no testify |
| Existing patterns to extend | Grader tests call the exported `Grade(Case{...})` directly (evals_test.go:10–99); runner tests use `NewRunner(t.TempDir())` + `filepath.Join(r.RunsDir(), run.ID+".json")` persistence assertions (evals_test.go:103–128); cost math is pinned by `internal/session/bl6_cost_test.go` |
| Determinism | Fixed timestamps (injectable clock or fixed `StartedAt`); no sleeps except the §2 concurrency test |
| Additive-only gate | Every test of the standalone path must still pass unchanged (technical spec §9): `eval_run`, `evals runs`, `ListRuns`, `LoadRun` byte-identical behaviour |

### 0.1 Verified existing code this plan hooks into

| Item | Location |
|---|---|
| `Suite`, `Case`, `Grader`, `CaseResult`, `Run`, `Runner` types; `LoadSuite` (threshold coercion at evals.go:138–144); `Execute` pass-rate math (evals.go:193–196); `Grade` (evals.go:271); `llm_rubric` stub `"manual review needed"` (evals.go:279–286) | `internal/evals/evals.go` |
| Grader test helpers (string/regex/binary/rubric/unknown) | `internal/evals/evals_test.go` |
| `EstimateCost(rate CostRate, tokensIn, tokensOut int) float64`; `CostRate{InPerK, OutPerK}`; `DefaultCostRates()` | `internal/session/cost.go:48`, :24, :31 |
| Existing BL6 cost tests to mirror | `internal/session/bl6_cost_test.go` |
| CLI verb shape (`evals list` / `run <suite>` / `runs --suite --limit` / `get-run <id>`) and `daemonGet`/`daemonJSON` helpers | `cmd/datawatch/cli_evals.go:17–90`, `cmd/datawatch/cli_sx_parity.go:116,138` |
| REST handlers + 503-when-runner-nil pattern + `CapAutonomousRun` gate | `internal/server/evals.go:36–155`, gate at :81 (run) |
| `evalsRunner` interface (the surface the sweep extends) | `internal/server/evals.go:25–31` |
| Route registration | `internal/server/server.go:306–310` |
| MCP tools `eval_list_suites` / `eval_run` / `eval_list_runs` / `eval_get_run` (REST proxy via `proxyGet`/`proxyJSON`) | `internal/mcp/evals.go:19–78` |
| Comm verbs (`evals run <suite>`, `evals runs [suite]`, `evals get-run <id>`) over `commGet`/`commJSON` | `internal/router/evals.go:23–76`, helpers at `internal/router/sx2_parity.go:26,47` |
| Federation cap `CapAutonomousRun = "autonomous:run"` (+ siblings) | `internal/federation/capabilities.go:68–71` |
| `inference.Registry.List`, `IsSessionBackendKind`, `ResolveTimeout`, `Dispatcher.Call` with `Request.Consumer`, `node.AllowsConsumer` | `internal/inference/llm.go:274`, `dispatcher.go:265,285,120`, `compute/node.go:424` |
| Mobile-Parity Rule (parity-surface set incl. PWA/Android/iOS) | `AGENT.md:455–472` |

---

## 1. Unit test cases

All in `internal/evals` (new `*_test.go` files), deterministic, no network. Technical spec sections cited per row.

### 1.1 Suite-load edge cases

| # | Case | Setup | Expected | Spec ref |
|---|---|---|---|---|
| U1.1 | Empty suite dir | `NewRunner(t.TempDir())`; no YAML files | `ListSuites()` returns empty slice, no error (mirrors `TestRunnerListSuitesEmptyDir`) | spec §1.3 rule 1 |
| U1.2 | Unknown suite dir / name | `LoadSuite("does-not-exist")` | error → planner maps to REST `404` `suite not found: <name>` | spec §1.3 rule 1, §5.1 |
| U1.3 | Empty `Cases` | suite YAML with `cases: []` | loads successfully; `Execute` yields `PassRate=0`, `Pass` false for any threshold > 0 | spec §3.5 |
| U1.4 | `PassThreshold: 0` (absent) — capability mode | fixture with `mode: capability`, no threshold | coerced to **0.70** (evals.go:138–144) — pin as golden, mirroring `TestCapabilityThresholdDefault` (evals_test.go:239) | spec §3.5 |
| U1.5 | `PassThreshold: 0` — regression mode | same, `mode: regression` | coerced to **0.99**, mirroring `TestRegressionThresholdDefault` (evals_test.go:216) | spec §3.5 |
| U1.6 | Explicit `PassThreshold: 1` | fixture with `pass_threshold: 1` | retained (no coercion — coercion only fires at 0); suite **passes only if every case passes** (`PassRate >= 1.0`, evals.go:196); with 3/3 pass verdict = pass, 2/3 = fail | spec §3.5 |
| U1.7 | Explicit `PassThreshold: 0` passed directly to `Execute` (bypassing `LoadSuite`) | construct `Suite{PassThreshold:0, ...}` in-code | trivially passes (`PassRate >= 0`); documented boundary — sweep tests must go through `LoadSuite`, never direct struct | spec §3.5 (coercion is a `LoadSuite` behaviour) |
| U1.8 | Malformed YAML in suite file | corrupt fixture | `LoadSuite` returns error → `404`/plan-fail path (no panic) | spec §1.8 (`failed` RunSet) |

### 1.2 Matrix expansion (planner table)

| # | Case | Expected | Spec ref |
|---|---|---|---|
| U1.9 | 1 explicit backend | 1 cell; `MatrixSpec.Backends == [name]` | spec §1.3 rule 2 |
| U1.10 | N explicit backends (e.g. 3) | N cells, **request order preserved** in `Cells` | spec §1.5 ordering |
| U1.11 | Unknown name in explicit list | `400`, offending name in the error message | spec §1.3 rule 2 |
| U1.12 | Session-backend kind (e.g. `aider`, `goose`) in explicit list | `422`, **all** offending names listed | spec §1.3 rule 4, §4.6, §4.8 |
| U1.13 | Disabled LLM in explicit list | `422`, name listed (no silent skip) | spec §1.3 rule 3, §10.1 |
| U1.14 | `*` wildcard, ≥1 enabled inference-kind LLM | expands exactly to enabled + non-session + adapter-registered set | spec §1.3 rule 2(a–c) |
| U1.15 | `*` wildcard, **zero** expansion (all inference kind disabled / none present) | `422` `no enabled inference-kind LLMs to sweep against` | spec §1.3 (post-rule-2), §4.8 |
| U1.16 | Per-cell model override, `len(models) == len(backends)` | positionally paired; cell `Model` = override | spec §1.3 rule 5 |
| U1.17 | Single model broadcast, `len(models) == 1` | every cell gets the same `Model` | spec §1.3 rule 5 |
| U1.18 | `len(models) == 0` | each cell uses LLM default (`Model`/`Models`) | spec §1.3 rule 5 |
| U1.19 | `len(models) == 2` with 3 backends (neither 0, 1, nor N) | `400` `models length 2 does not match 0, 1, or backends length 3` | spec §1.3 rule 5 |
| U1.20 | `max_parallel` above `evals.max_parallel` (default 3) | **silently clamped**, never rejected; `RunSet.MaxParallel` records applied value | spec §1.3 rule 6, §8.2 |
| U1.21 | Cell count above `evals.max_cells` (default 64) | `400` `matrix size N exceeds evals.max_cells M` | spec §1.3 rule 7 |
| U1.22 | Per-LLM `eval_*` fields: `eval_enabled=false` excluded from `*`; `eval_default_model` used when cell has no model; `eval_max_parallel` caps that LLM's concurrency | three sub-assertions in one planner table | spec §4.7 LLM-row fields |

### 1.3 Normalization math

| # | Case | Expected | Spec ref |
|---|---|---|---|
| U1.23 | `pass_rate` | `Σ pass(cases)/len(cases)` per cell; identical formula to standalone `Execute` (evals.go:193–195) — same threshold applied to every cell (U1.4/U1.5 carry through) | spec §1.4, §3.5 |
| U1.24 | `latency_ms` | `Σ DurationMs` over the cell's cases; stub `Response.DurationMs` per case, assert exact sum | spec §1.4 |
| U1.25 | `tokens_in` / `tokens_out` | `Σ Response.TokensIn/Out`; adapter-unknown adapters report **0 = legal** and the cell sums honestly (no `n/a` masking) | spec §4.3 |
| U1.26 | `cost_usd` via BL6 | `Σ session.EstimateCost(rate, tokensIn, tokensOut)` (`internal/session/cost.go:48`) per case: `tokensIn/1000*InPerK + tokensOut/1000*OutPerK`. Pin the math with known triples (e.g. rate `{0.01, 0.03}`, 2000/500 tokens → `0.02+0.015 = 0.035`), mirroring `TestBL6_EstimateCost_BasicMath` (bl6_cost_test.go:10) | spec §4.4 |
| U1.27 | Cost resolution order | (a) `evals.cost_rates[llm]` override wins, (b) LLM row `CostPer1kTokens*` next, (c) daemon session rate table, (d) `CostRate{}` → **$0** for local kinds. Assert first-available-wins for a 3-LLM matrix | spec §4.4, §8.9 |
| U1.28 | `llm_rubric` tokens/cost folded into cell | stub grader call returns usage; assert cell totals include both the case call and the rubric call | spec §3.3, §10.3 |

### 1.4 Verdict & aggregation

| # | Case | Expected | Spec ref |
|---|---|---|---|
| U1.29 | All-pass | every cell `pass=true`; `RunSet.Incomplete=false`; `best` set (not nil); `deltas[0]` zero value | spec §1.6, §1.8 |
| U1.30 | Mixed (one cell below threshold) | failed cell `pass=false`, siblings unaffected; `Incomplete` reflects only **cell-level errors**, not threshold misses (`RunSet.Incomplete = any cell.Error != ""`) | spec §1.8 (status transition note) |
| U1.31 | All-fail with cell **errors** (dispatcher down for every backend) | `Status=complete` (settled), `Incomplete=true`, every cell `Error` non-empty, `best=nil` | spec §1.8 ("complete means settled") |
| U1.32 | Plan-time failure (unknown suite / malformed matrix) | `Status=failed`, `cells == []`, error on the RunSet itself (not a cell) | spec §1.8 terminal states |
| U1.33 | Delta math | `deltas[i] = cell[i] − cell[0]` for each column; sign convention: positive `latency_delta_ms`/`cost_delta_usd` = worse; `deltas[0]` zero | spec §1.6 |
| U1.34 | `best` tie-breaks | argmax `pass_rate`; tie → min `cost_usd`; tie → min `latency_ms`. Three-way golden fixture exercises both tie-breaks | spec §1.6, §8.5 |
| U1.35 | `reference_cell_index == 0` invariant, and cell order = request order (not pass_rate order) | assert against a fixture where the passing cell is not first | spec §1.5, §1.6 |
| U1.36 | `GradeActual` back-compat golden | for every grader type (`string_match`, `regex_match`, `binary_test`, `llm_rubric`, unknown), `Grade(c)` (evals.go:271) and `GradeActual(c, c.Input)` return identical `CaseResult` — extends existing per-grader tests (evals_test.go:10–99) | spec §3.2, §8.1 |
| U1.37 | Per-grader sweep semantics | `string_match`: grades `resp.Text` vs `Expected` (strict = full equality, non-strict = case-insensitive contains); `regex_match`: `Pattern` else `Expected` fallback (mirrors `TestGradeRegexMissPattern`); `binary_test`: `INPUT` env = **backend response** (not `c.Input`), `sh -c` exit 0 = pass (mirrors `TestGradeBinaryTestUsesInputEnv`, evals_test.go:75); `llm_rubric` pre-P3: all-fail `"manual review needed"` (mirrors `TestGradeLLMRubricStubbed`, evals_test.go:86), sweep still settles `complete`, never crashes | spec §3.2.1, §3.3, §3.4, §8.10 |

### 1.5 `parent_run_id` grouping & persistence

| # | Case | Expected | Spec ref |
|---|---|---|---|
| U1.38 | Child Run rows | each child persists to `runs/<child_id>.json` with `parent_run_id == <runset_id>`; standalone run keeps `ParentRunID == ""` and serializes byte-identically to today (omitempty — no key in JSON) | spec §1.7, §6.1, §9 |
| U1.39 | RunSet parent row | persists to `runs/<runset_id>.json` with `kind: "runset"` discriminator | spec §1.7 |
| U1.40 | `ListRuns` filtering | returns **children only**, never `kind:"runset"` rows; children still appear under their suite filter (extends `TestRunnerListRunsFilteredAndLimited`, evals_test.go:193) | spec §1.7, §3.6, §10.2 |
| U1.41 | `LoadRun(<runset-id>)` | not found (404) — RunSet is a view, not a run | spec §8.6 |
| U1.42 | `LoadRunSet(id)` round-trip | returns parent with `Cells`, `Deltas`, `Best` intact from disk (extends `TestRunnerLoadRun` pattern, evals_test.go:176) | spec §8.6 |
| U1.43 | `ListRunSets(suite, limit)` | newest-first, correct `limit`, projection fields (`ID`, `Suite`, `Status`, `Incomplete`, `CellsCount`, `BestPassRate`, timestamps) — no `Cells` payload | spec §6.9 |
| U1.44 | Two sequential sweeps in one `t.TempDir()` | two RunSets + all children listed independently; no cross-contamination of `parent_run_id` | spec §8.6 |

---

## 2. Integration test cases

Daemon-level: real HTTP routes, stub LLM/dispatcher backend, real config. Mirrors the handler patterns at `internal/server/evals.go` (503 at :38/:74/:105, cap gate at :81) and the MCP/comm proxies.

### 2.1 REST

| # | Case | Request | Expected | Spec ref |
|---|---|---|---|---|
| I2.1 | 200 happy path | `POST /api/evals/sweep` `{suite, backends:[a,b]}` against stub dispatcher | `200` + settled `RunSet` JSON: `status=complete`, `incomplete=false`, 2 cells in request order, valid `deltas`/`best` | spec §5, §5.1 |
| I2.2 | Per-cell failure → still 200 | stub errors only for backend `a` | `200` (not 5xx); cell `a` has `error`, cells `b` complete with metrics; `status=complete`, `incomplete=true`; audit entry written | spec §1.5 independence, §1.8, §5.1 |
| I2.3 | Runner not wired | server started without `SetEvalsRunner` | `503` `evals disabled` — same pattern as existing routes (evals.go:37–40) | spec §7 boot wiring |
| I2.4 | Unknown suite | `POST ... sweep` bad suite name | `404` `suite not found: <name>` | spec §5.1 |
| I2.5 | Planner rejections via REST | unknown backend name → `400`; session backend → `422`; zero `*` expansion → `422`; bad `models` length → `400` (one test per mapping, table-driven) | exact status + message shape `{"error":"..."}` per the daemon convention | spec §5.1, §0.6 |
| I2.6 | Bearer auth | request without/with wrong token, correct token | 401 / 401 / 200 — same auth as the existing five `/api/evals/*` routes | spec §0.6 |
| I2.7 | Federation capability gate | caller presenting capabilities **without** `CapAutonomousRun` (`federation/capabilities.go:71`) vs with it | `403`/denied vs `200` — same gate as `handleEvalsRun` (evals.go:81). Read endpoints (`GET /runsets`) use the read-cap gate as at evals.go:45 | spec §0.6, §5 |
| I2.8 | `GET /api/evals/runsets` | after I2.1 | `200` list of `RunSetSummary` (newest first); `?suite=` and `limit` filters honoured | spec §5, §6.9 |
| I2.9 | `GET /api/evals/runsets/{id}` | id from I2.1 | `200` full `RunSet`; unknown id → `404` (same split as `handleEvalsRuns` list-vs-detail, evals.go:108–145) | spec §5 |
| I2.10 | RunSet never leaks into existing list | `GET /api/evals/runs?suite=X` after a sweep | only child runs; `kind:"runset"` row absent | spec §1.7, §3.6 |
| I2.11 | Audit entry | after I2.1 | one entry `action=evals_sweep`, `resource_type=eval_runset`, `resource_id=<runset-id>`, visible via existing audit query | spec §5.2 |

### 2.2 MCP

| # | Case | Expected | Spec ref |
|---|---|---|---|
| I2.12 | `eval_run` with sweep params (`backends` CSV, optional `models` CSV) | returns the **RunSet table** (cells + deltas + best), not a bare `Run`; identical JSON to the REST I2.1 response | spec §5 (MCP row) |
| I2.13 | `eval_run` without sweep params | byte-identical single-`Run` response to today's `eval_run` (proxy at mcp/evals.go:52) — additive-only gate | spec §5, §9 |
| I2.14 | `eval_list_runsets` / `eval_get_runset` | mirror I2.8/I2.9 via `proxyGet` (mcp/evals.go:60,78 pattern) | spec §5 |
| I2.15 | Error pass-through | 404/422/503 from REST surface in the MCP tool result with the daemon error message | spec §5.1 |

### 2.3 CLI

Verb shape follows the existing four (cli_evals.go:17–90):

| # | Case | Command | Expected | Spec ref |
|---|---|---|---|---|
| I2.16 | sweep | `datawatch evals sweep <suite> --backends a,b [--models ...] [--max-parallel N]` | hits `POST /api/evals/sweep` (via `daemonJSON`, cli_sx_parity.go:138); prints the RunSet table/JSON | spec §5 CLI row |
| I2.17 | runsets list | `datawatch evals runsets [--suite S] [--limit N]` | hits `GET /api/evals/runsets` — same flag convention as existing `evals runs --suite --limit` (cli_evals.go:75–76) | spec §5 |
| I2.18 | get-runset | `datawatch evals get-runset <id>` | hits `GET /api/evals/runsets/<id>` (mirrors `get-run`, cli_evals.go:80–86) | spec §5 |
| I2.19 | existing verbs unchanged | `evals list` / `evals run <suite>` / `evals runs` / `evals get-run` | still work exactly as registered (cli_evals.go:28–31) | spec §9 |

### 2.4 Comm channel + parity triggers

| # | Case | Command | Expected | Spec ref |
|---|---|---|---|---|
| I2.20 | comm sweep | `evals sweep <suite> [backends]` | thin REST proxy (helper `commJSON`, router/evals.go pattern + sx2_parity.go:47); reply rendered like existing `evals run` (router/evals.go:49) | spec §5 comm row |
| I2.21 | comm read | `evals runsets [suite]` · `evals get-runset <id>` | proxy the two GETs; `prettyJSON` output | spec §5 |
| I2.22 | Parity trigger matrix | each of I2.1/I2.12/I2.16/I2.20 against the **same fixture**, same token | identical `RunSet` semantics: same `cells` order, same `incomplete`, same `best` — assert on the RunSet fields, not on transport-specific formatting | spec §5 (single canonical contract) |

### 2.5 Dispatch plumbing (with stub dispatcher + registry)

| # | Case | Expected | Spec ref |
|---|---|---|---|
| I2.23 | `Consumer:"eval"` round-trip per inference kind | one test per kind in the §4.2 table (`ollama`, `openwebui`, `opencode`, `claude`, `gemini-api`, `opencode-api`) using an `adapters/<kind>` fixture | spec §4.8 checklist |
| I2.24 | Node failover | LLM with two stub nodes, first always `ErrTransient` | cell still succeeds, `UsedNode` = second node | spec §4.5 |
| I2.25 | RBAC allow/deny | node with `allowed_consumers: ["ask"]` **skipped** for an eval cell; default-all node used (extends `compute/registry_test.go:190`); `denied_consumers: ["eval"]` excludes from failover walk | RBAC: `compute/node.go:424` | spec §4.5, §8.7 |
| I2.26 | Concurrency ceiling | stub dispatcher blocks briefly; atomic in-flight counter | never more than `evals.max_parallel` concurrent cells (spec §8.8) |
| I2.27 | Token plumbing per adapter | stubbed protocol responses (`prompt_eval_count`/`eval_count`; OpenAI `usage`; message `usage`; `usageMetadata`) | `Response.TokensIn/Out` populated per the §4.3 table; zero for unreported | spec §4.3, §4.8 |

---

## 3. Smoke test cases

End-to-end against a running daemon (real `datawatch` binary). Manual/CI smoke — acceptable to be slower and less hermetic than §2, but each must leave a verifiable artefact.

| # | Case | Steps | Pass criterion |
|---|---|---|---|
| S3.1 | Single-backend sweep renders | 1. daemon start 2. register one local stub/ollama LLM in the registry 3. `POST /api/evals/sweep` (or `datawatch evals sweep <suite> --backends local-1`) | side-by-side table (CLI/PWA read) shows: suite, threshold, 1 cell with pass_rate/latency/tokens/cost, reference marker on cell 0, `best` row — matches `GET /api/evals/runsets/<id>` JSON field-for-field |
| S3.2 | Two-backend sweep, one unreachable | 1. LLM `local-1` healthy (stub port), LLM `down` points at a dead port 2. sweep `--backends local-1,down` | `200` + table renders **both** rows: healthy row fully populated; `down` row shows `error`, zero/`n/a` metrics; `incomplete=true`; overall status `complete`; **no** hang — sweep settles within `ResolveTimeout(llm)` for the dead cell (`internal/inference/dispatcher.go:285`) |
| S3.3 | PWA / comm read after sweep | 1. run S3.1 2. open PWA evals surface / `evals runsets` + `evals get-runset <id>` over comm | PWA lists the RunSet under the suite with status/incomplete/best; detail view renders cells, deltas, best — identical data to the REST `GET`; PWA never shows `kind:"runset"` rows in the plain runs list (spec §3.6 filter) |
| S3.4 | Existing surfaces unregressed | after S3.1: `evals run <suite>` (single), PWA runs list, `GET /api/evals/runs` | all behave as pre-sweep; standalone `Run` JSON has **no** `parent_run_id` key (spec §9) |
| S3.5 | 503 surface | daemon started with evals runner not wired (fresh minimal config) | all five legacy routes **and** the three new routes return `503 evals disabled` consistently; PWA shows the disabled state, doesn't crash |

---

## 4. Parity surface checklist

Per the Mobile-Parity Rule (AGENT.md:455–472), the parity-surface set is **REST, MCP, CLI, comm channel, YAML/config, PWA, Android, iPhone/iOS**; capability parity is required, implementation parity is not (a surface may be read-only or absent, but the feature must not be invisible on a surface it already serves).

| Surface | Status | Plan |
|---|---|---|
| REST | ✅ full parity | `POST /api/evals/sweep`, `GET /api/evals/runsets`, `GET /api/evals/runsets/{id}` — canonical routes (I2.1–I2.11) |
| MCP | ✅ full parity | `eval_run` gains `backends`/`models` params (I2.12); `eval_list_runsets`, `eval_get_runset` (I2.13–I2.15) |
| CLI | ✅ full parity | `evals sweep`, `evals runsets`, `evals get-runset` (I2.16–I2.18) |
| Comm channel | ✅ full parity | `evals sweep [backends]`, `evals runsets`, `evals get-runset` (I2.20–I2.21) |
| YAML/config | ✅ full parity | `evals` section: `max_parallel`, `max_cells`, `default_backends`, `cost_rates`, `rubric_backend` (spec §4.7) + per-LLM `eval_*` fields; tested via planner tables U1.20–U1.22, U1.27 and a config-load test (today **no** `evals` section exists in `internal/config/config.go` — new, additive) |
| PWA | ✅ read parity required | RunSet list + detail render (S3.1, S3.3). Sweep *launch* from PWA required if the existing single-run launch exists there; otherwise document the gap |
| Android (push/comm client) | ⏸ pending | Verify whether the Android surface renders comm replies generically; if it renders `evals` replies, `evals runsets` text must not overflow — add one smoke line, or record exclusion reason here |
| iPhone/iOS (push/comm client) | ⏸ pending | Same check as Android; record outcome (pass + reply sample, or exclusion reason) in this table before GA |

---

## 5. Sequencing with implementation phases

Tests land **with** each implementation phase from technical spec §7, so each milestone's ship gate is green tests, not just code:

| Phase (spec §7) | Tests that land with it |
|---|---|
| **P0 — token plumbing** | U1.25, U1.26 (tokens/cost math), I2.27 (per-adapter token population), plus the `inference.Response` additive-field compile-gate on existing tests |
| **P1 — sweep core** | U1.1–U1.22 (planner + suite-load), U1.23–U1.24, U1.29–U1.39 (verdict + persistence), I2.1–I2.11 (REST), I2.12–I2.15 (MCP), I2.16–I2.19 (CLI), I2.20–I2.22 (comm + parity), S3.1, S3.4, S3.5 |
| **P2 — cost + config** | U1.27–U1.28 (cost resolution order, rubric cost), U1.22, I2.23–I2.26 (consumer RBAC, failover, concurrency ceiling), §4 YAML/config row tests, S3.2 (unreachable-backend smoke) |
| **P3 — llm_rubric** | U1.36–U1.37 (back-compat golden + rubric graders), I2.23 extension for rubric LLM call (`Consumer:"eval_rubric"`), S3.1 re-run with a rubric-case suite |

**Gate rule:** a phase is not shippable while any row above it is red, and the additive-only gate (spec §9) is re-asserted at every phase boundary by re-running the pre-existing suites in `internal/evals/evals_test.go` and `internal/session/bl6_cost_test.go` unchanged.

---

*This plan is the test contract for Eval Sweep. Behaviour cited as "spec §…" is authoritative in `eval-sweep-technical-spec.md`; where a case and the spec conflict, the spec wins and this plan must be amended.*
