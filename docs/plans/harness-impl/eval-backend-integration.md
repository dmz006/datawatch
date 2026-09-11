# Eval Sweep — Backend System Integration

**Date:** 2026-09-11 · **Status:** Draft · **Source:** `docs/plans/harness-research/enhancement-proposals.md` §1 (Eval Sweep — suite × backend matrix)

This document specifies how the eval sweep feature wires into DataWatch's existing backend machinery. It is the sibling of `eval-sweep-spec.md` (behavioural spec) and `eval-dag-design.md` (orchestrator eval nodes). Here we answer three questions:

1. **Data flow** — how a sweep request flows from the surface (MCP / REST / CLI) through a per-backend runner to result aggregation.
2. **Registry integration** — the exact existing LLM / backend APIs the sweep runner calls and how cells are dispatched.
3. **Config deltas** — the new fields required on backend rows (LLM registry entries and ComputeNodes) and the companion daemon config.

---

## 1. Problem recap

`eval_run` (BL259) executes one suite against a single backend and emits one `Run`. There is no side-by-side across LLMs/ComputeNodes, and no aggregate comparison. Proposal §1 asks for a sweep verb that accepts a matrix (suite × list of LLM refs, optionally × model overrides), fans the suite out per cell in parallel under the existing max-parallel plumbing, records per-cell results as children of a parent `RunSet` row, normalizes metrics (pass_rate, latency, tokens, cost via BL6), and renders a comparison table.

### Existing land — what we build on

| System | Where | What the sweep reuses |
|---|---|---|
| Evals framework | `internal/evals` (`Runner`, `Suite`, `Case`, `Run`, `Grade`) | Suite loading, per-case grading, run persistence (`~/.datawatch/evals/runs/<id>.json`) |
| Evals REST surface | `internal/server/evals.go` (`evalsRunner` interface) | Route wiring, audit hook, federation capability gates |
| Evals CLI / MCP | `internal/router/evals.go`, `internal/mcp/evals.go` | `evals run` verb shape, `eval_run` MCP tool |
| LLM registry | `internal/inference/llm.go` (`Registry`, `LLM`) | Named LLM resolution, kind/model/ComputeNode lookup, enabled/disabled state |
| LLM dispatcher | `internal/inference/dispatcher.go` (`Dispatcher.Call`) | Kind-agnostic one-shot inference with ordered ComputeNode failover + RBAC |
| Cost accounting (BL6) | `internal/session/cost.go` (`CostRate`, `EstimateCost`, `DefaultCostRates`) | USD-per-cell normalization |
| Max-parallel plumbing | `autonomous.max_parallel_tasks`, `orchestrator.max_parallel_prds` | Cap on cells running concurrently |

The current `evals.Runner.Execute` is backend-agnostic: it grades the case's `Input` directly and never touches the LLM. The sweep introduces a **per-backend runner** that sends each case through a backend and grades the *response*. `llm_rubric` (currently a stub) becomes a genuine call in that path.

---

## 2. Data flow: sweep request → per-backend runner → aggregation

### 2.1 Entry points

Three surfaces accept a sweep, mirroring the existing `eval_run` surface:

- **MCP:** extend `eval_run` (`internal/mcp/evals.go`) with `backends` (comma-separated LLM refs), `models` (comma-separated model overrides, one per backend or scalar), and `max_parallel`. `eval_run --sweep` returns the `RunSet` table.
- **REST:** `POST /api/evals/sweep?suite=<name>&backends=a,b&models=...&max_parallel=N` in `internal/server/evals.go`. Kept separate from `/api/evals/run` so single-run behaviour is unchanged; both share the audit/federation gates (`federation.CapAutonomousRun`).
- **CLI:** `evals sweep <suite> --backends a,b [--models ...] [--max-parallel N]` in `internal/router/evals.go`, wrapping the same REST proxy the `evals run` verb uses.

### 2.2 Stages

```
 sweep request ──► Stage 1: parse + matrix expansion
                   Stage 2: fan-out (RunSet parent row) + parallel cell scheduling
                   Stage 3: per-backend runner (per cell)
                   Stage 4: aggregation (comparison table)
```

**Stage 1 — Parse & matrix expansion** (`handleEvalsSweep` → `SweepPlanner`)

1. Load the suite once via the existing `evalsRunner.LoadSuite`; fail fast if the suite is unknown (`404`).
2. Resolve the backend list against the **inference registry**, not the legacy `internal/llm` coding-agent registry:
   - Each entry may be an LLM name from `inference.Registry.List()`/`Get()`, or the convenience value `*` = "every enabled inference kind" (all LLMs where `IsSessionBackendKind(kind) == false` and `Disabled == false` and an adapter is registered).
   - Session-backend kinds (`claude-code`, `aider`, `goose`, `gemini`, `shell`, `opencode-acp`, `opencode-prompt`) have **no inference adapter** (`inference.IsSessionBackendKind`) — the planner rejects them with a clear error instead of letting `Dispatcher.Call` fail per cell.
   - Skips LLMs where `entry.Disabled == true` (inverse-enabled toggle, `LLM.Enabled()`).
3. Apply `ModelOverride` per cell (scalar model = all cells, or one per backend positionally). Empty model = the LLM row's own `Model`/`Models` selection.
4. Produce the **matrix**: `cells = suite × backends` (× any requested model_override cells). Cap cells at a hard ceiling (`evals.max_cells`).
5. Create the parent **`RunSet`** row: id, suite name, matrix spec (backend × model), `max_parallel`, timestamps, status `pending`.

**Stage 2 — Fan-out & scheduling** (new `SweepRunner`, sits next to `evals.Runner`)

- For each cell create a child `Run` row carrying `parent_run_id = <runset id>` (the single new field on `Run`; no new store — children persist under the same `~/.datawatch/evals/runs/`).
- Run cells under a worker-pool sized `min(requested max_parallel, autonomous.max_parallel_tasks fallback, evals.max_parallel config)`, keyed off the same max-parallel plumbing the orchestrator/pipeline runners use. Cells of the same suite may reuse one *loaded* `Suite` (read-only); the per-cell `Run` is distinct.
- Each cell is independent — one cell failing (backend down, timeout, malformed grader) does not abort siblings; it records a `failed` cell with `error` in the child `Run`.

**Stage 3 — Per-backend runner** (`cellRunner.run`)

```
for each case in suite.Cases:
    req = inference.Request{
        Prompt:        case.Input,
        SystemPrompt:  grader-specific rubric preamble,   // llm_rubric only
        ModelOverride: cell.ModelOverride,
        Consumer:      "eval",
    }
    resp = inferenceDisp.Call(ctx, cell.LLMName, req)     // registry → ComputeNode → adapter
    grade = Grade(case with Actual=resp.Text)             // existing CaseResult logic
    record: pass, score, feedback, latency (resp.DurationMs),
            tokens (resp.TokensIn/Out), cost (BL6 EstimateCost)
```

Grader behaviour per cell:

- `string_match` / `regex_match` / `binary_test` — unchanged graders, but now applied to the backend's **response text** (`resp.Text`) instead of the case's raw `Input`. `Input` remains the prompt.
- `llm_rubric` — un-stubbed in this path: `case.Grader.Rubric` becomes `SystemPrompt`, the candidate answer is `case.Expected` (or a prior cell's `Actual`), and a fresh `Dispatcher.Call` grades it. `case.Grader.Model` overrides the cell model for the rubric grade.

Per-cell timeout uses `inference.ResolveTimeout(llm)` (explicit `timeout_seconds`, else effort-scaled default) so a hang on one backend caps cell duration.

**Stage 4 — Aggregation** (read-side, no new collection path)

- The parent `RunSet` is a *view over its children*: after all cells finish, the coordinator re-reads the child rows (or collects them in memory before persist) and computes per-cell:

  - `pass_rate`, `pass` (vs the suite's `pass_threshold`)
  - `latency_ms` (sum of `DurationMs`)
  - `tokens_in`, `tokens_out` (new fields, aggregated from `resp`)
  - `cost_usd` via `session.EstimateCost(rate, in, out)` with the rate for the cell's backend family from `Manager.SetCostRates` / `DefaultCostRates` (local kinds cost $0 by default; SaaS kinds — `claude`, `gemini-api` — use the LLM row's `CostPer1kTokens*` when present, else BL6 rates)

- The comparison table = suite × backend(matrix) with per-cell pass/∅/fail and the normalized metrics, plus a **delta row** against a reference cell (first backend) so "is the new model a regression?" is one scan.
- `RunSet` is persisted as `~/.datawatch/evals/runs/<runset-id>.json` with a `kind: "runset"` discriminator so existing `ListRuns`/`LoadRun` never mistake it for a plain `Run`. Listing is unchanged: children still surface via the normal run list, tagged by `parent_run_id`.

### 2.3 Interface delta on the server runner

The `evalsRunner` interface in `internal/server/evals.go` gains sweep methods while keeping the single-run surface intact:

```go
type evalsRunner interface {
    // existing
    ListSuites() ([]string, error)
    LoadSuite(name string) (*evals.Suite, error)
    Execute(s *evals.Suite) (*evals.Run, error)
    ListRuns(suite string, limit int) ([]*evals.Run, error)
    LoadRun(id string) (*evals.Run, error)
    // new
    Sweep(req evals.SweepRequest) (*evals.RunSet, error)      // sync until done
    LoadRunSet(id string) (*evals.RunSet, error)              // read-side view
}
```

The runtime concrete type is a `*evals.SweepRunner` (new) that composes the existing `*evals.Runner` (loading/grading/persistence) and is constructed with the **inference registry + dispatcher + cost-rate provider** — see §3. `/api/evals/run` and the backlog-compat `GET /api/evals` handler only touch the existing methods, so v6 behavior is preserved byte-for-byte.

---

## 3. How the sweep runner calls existing LLM / backend registry APIs

The sweep runner **reuses** the `internal/inference` dispatcher — it does not reinvent backend calling, and it does **not** use the `internal/llm` package (that registry is for tmux-session coding agents, not one-shot inference).

| Call | API | Purpose |
|---|---|---|
| Enumerate candidates | `inference.Registry.List()` / `Get(name)` | Resolve matrix backends, honor `Disabled`, detect session-backend kinds |
| Kind sanity | `inference.IsSessionBackendKind(kind)` | Reject non-inference rows up front |
| Dispatc h one case | `inference.Dispatcher.Call(ctx, llmName, inference.Request{Prompt, SystemPrompt, ModelOverride, Consumer:"eval"})` | Full path: LLM row → ordered ComputeNode failover → adapter → `Response{Text, UsedNode, UsedModel, Backend, DurationMs}` |
| Model selection | `inference.ResolveModel(llm, req)` | Effective model when no override |
| Timeout | `inference.ResolveTimeout(llm)` | Per-cell deadline (existing `TimeoutSeconds` / effort scaling) |
| ComputeNode RBAC | `compute.Node.AllowsConsumer("eval")` | `Consumer:"eval"` routes through the same allow/deny matrix as `ask` / `council` / `session_spawn` |
| Cost | `session.EstimateCost(rate, in, out)`, `session.DefaultCostRates` / `Manager.SetCostRates` | Per-cell USD; local kinds default $0 |
| Persistence | existing `evals.Runner` disk layout | Child runs + `RunSet` under `~/.datawatch/evals/runs/` |

The dispatcher call already handles everything the sweep needs across kinds:

- **Local kinds** (`ollama`, `openwebui`, `opencode`, `gemini-api`, `opencode-api`) — walk the LLM's ordered `ComputeNodes`, skip maintenance/disabled/denied nodes, fail over on transient errors, return `ErrNoBackend` when exhausted.
- **Cloud kinds** (`claude`) — dispatched directly; ComputeNode list walked for RBAC/permissions accounting only. Sweep cells against `claude` therefore cost real money and are fine to include because BL6 rates (or the LLM row's `CostPer1kTokens*`) let the table show it.
- `datawatch-proxy` and `docker-network` compute routing are transparent to the sweep — the dispatcher resolves peers/containers before the adapter call.

### Session-backend kinds are out of scope

`claude-code`, `aider`, `goose`, `gemini` (CLI), `shell`, `opencode-acp`, `opencode-prompt` have no inference adapter — `Dispatcher.Call` would return "no adapter for kind". The planner rejects them at matrix-expansion time (one error listing the offending names) so a sweep never burns a cell on a mis-target. If a future story needs eval-against-a-coding-agent, it ships as a separate session-prodded runner, not inside this sweep.

### Consumer name addition

Today `internal/compute/node.go` documents consumers `council | agent_spawn | ask | session_spawn`. The sweep introduces a **new consumer name `eval`**. This is the one behavioural change to existing machinery and it is purely additive:

- Empty `AllowedConsumers` (the common case) means "all consumers" → existing nodes accept `eval` with no config change.
- Operators pinning consumers must add `eval` to `allowed_consumers` (or rely on default-all); `denied_consumers` with `eval` excludes a node from sweeps explicitly.

### Token accounting gap

`inference.Response` currently carries no token counts, so "tokens" and "cost via BL6" cannot be sourced today. The integration plan requires an additive extension to `Response`:

```go
type Response struct {
    Text       string
    UsedNode   string
    UsedModel  string
    Backend    Kind
    DurationMs int64
    TokensIn   int   // new — adapter-parsed prompt token count (0 = unknown/local)
    TokensOut  int   // new — adapter-parsed completion token count
}
```

Adapters that already receive usage in their protocol responses (openwebui / opencode-api OpenAI-style `usage`, claude message usage) populate these; ollama can parse `prompt_eval_count`/`eval_count`. When zero, the aggregator reports tokens/cost as `n/a` (local kinds default $0 anyway). BL6 `CostRate`/`EstimateCost` are unchanged.

---

## 4. New configuration fields

### 4.1 On LLM registry rows (`internal/inference/llm.go` — `LLM` struct)

New, all `omitempty` so existing persisted registry JSON reads as "configured by defaults":

| Field | yaml/json | Meaning | Default |
|---|---|---|---|
| `EvalEnabled` | `eval_enabled` | Opt-in to be a sweep target when the matrix is `*`; explicit `backends=a,b` bypasses this flag | logical OR: `true` when set, else `true` for inference kinds (so `*` = every inference kind) |
| `EvalDefaultModel` | `eval_default_model` | Model override applied when a cell specifies the LLM name but no model | empty = LLM row `Models`/`Model` |
| `EvalTimeoutSeconds` | `eval_timeout_seconds` | Per-cell timeout override for this LLM's cells; beats `ResolveTimeout` | 0 = `ResolveTimeout(llm)` as today |
| `EvalMaxParallel` | `eval_max_parallel` | Per-LLM cap on concurrent cells (for multi-model setups) | 0 = inherit global `evals.max_parallel` |

Reuse (already present, no change): `TimeoutSeconds`, `Disabled`, `Models`/`Model`, `CostPer1kTokensInput/Output`, `ComputeNodes`.

### 4.2 On ComputeNode rows (`internal/compute/node.go` — `Permissions`)

| Field | Meaning |
|---|---|
| `AllowedConsumers` [+] `"eval"` | Allows the node to serve sweep cells (default-all nodes unaffected) |
| `DeniedConsumers` [+] `"eval"` | Excludes the node from sweeps; wins over allow |

No other ComputeNode change: the dispatcher's maintenance/disabled/consumer machinery already governs sweep cells once `Consumer:"eval"` is passed.

### 4.3 Companion daemon config (`internal/config/config.go` — new `EvalsConfig`)

Sweep defaults live in a small `evals` section, additive and optional:

| key | Meaning | Default |
|---|---|---|
| `evals.max_parallel` | Global sweep fan-out cap | 3 (mirrors `autonomous.max_parallel_tasks`) |
| `evals.max_cells` | Hard ceiling on matrix size (DoS guard) | 64 |
| `evals.default_backends` | Backend list when the request omits `backends` | `*` (all enabled inference kinds) |
| `evals.cost_rates` | Optional per-backend override; falls back to `session.cost_rates` / `DefaultCostRates` | empty |

Cost rates are intentionally *not* duplicated on the evals struct: the sweep reads the same rate table the session manager owns (`Session.CostRates` → `Manager.SetCostRates`), so one operator edit re-prices both sessions and sweeps.

### 4.4 Validation

- LLM rows: `LLM.Validate()` gains a no-op check (fields are advisory); matrix expansion is where `EvalEnabled`/kind filters apply.
- A cell that resolves to a session-backend kind is a matrix-time error, never a runtime one.
- `max_parallel` request values clamp to `evals.max_parallel`; `max_cells` overflow returns `400`.

---

## 5. ASCII sequence diagram

```
 Operator / MCP / CLI                Server                   SweepRunner                  inference.Registry        inference.Dispatcher        ComputeNode(s)       evals.Runner
      │  eval_run --sweep             │                            │                              │                            │                       │              │
      │  suite=X backends=ollama,claude│                            │                              │                            │                       │              │
      │───────────────────────────────▶│                            │                              │                            │                       │              │
      │  POST /api/evals/sweep         │                            │                              │                            │                       │              │
      │                               │  1 Plan matrix            │                              │                            │                       │              │
      │                               │── LoadSuite(X) ─────────────────────────────────────────────────────────────────────────────────────────────────▶│
      │                               │<──────────────────────────  Suite ───────────────────────────────────────────────────────────────────────────────┤
      │                               │   Resolve backends ▶------▶ Registry.List() / Get / IsSessionBackendKind
      │                               │<────────────────────── [llm rows + kinds] ────────────────────────────────────────────────────────────────────────┤
      │                               │  build cells s×b, create RunSet (parent) ─────────────▶
      │                               │─────────────────────────▶ Sweep(RunSet)                 │                              │                       │              │
      │                               │                            │ for each cell (≤ max_parallel)                           │                       │              │
      │                               │                            │   create child Run (parent_run_id=…)                     │                       │              │
      │                               │                            │                          │  Dispatcher.Call("ollama", req{Consumer:"eval"}) │              │
      │                               │                            │─────────────────────────▶│                              │                       │              │
      │                               │                            │                          │  │ resolve LLM row           │                       │              │
      │                               │                            │                          │  │ walk ComputeNodes ──────▶│ AllowsConsumer("eval")  │              │
      │                               │                            │                          │  │ _________________________│  adapter.Infer ─────────▶│ (LLM/HTTP)  │
      │                               │                            │                          │◀─────────────────────────────┴── Response{Text, DurationMs, Tokens}──┘
      │                               │                            │   apply grader (string_match / regex_match / binary_test / llm_rubric)
      │                               │                            │   repeat llm_rubric as a second Dispatcher.Call with rubric as SystemPrompt
      │                               │                            │  persist child Run ────────────────────────────────────────────────────────────────▶
      │                               │                            │  agg tokens/cost (BL6 EstimateCost, rate by backend family)
      │                               │                            │ (fan-out repeats per cell; per-cell failure records error, siblings continue)
      │                               │                            │ fan-out done
      │                               │◀───────────────────────── RunSet (view over child runs + per-cell table + deltas)
      │                               │  audit evals_run → RunSet  │
      │◀──────────────────────────────│ 200 {runset, cells:[{backend, model, pass_rate, pass, latency_ms, tokens, cost_usd}], delta_vs_reference}
      │                               │                            │                              │                            │                       │              │
```

---

## 6. Sequencing & verification

1. **P0 — plumbing:** extend `inference.Response` with `TokensIn/Out`; parse usage in the ollama / openwebui / opencode-api / claude adapters; no behaviour change elsewhere.
2. **P1 — sweep core:** `SweepRequest`/`RunSet` types, `parent_run_id` on `Run`, `SweepRunner` with worker-pool fan-out, REST `POST /api/evals/sweep`, CLI + MCP verb. Wire `sweepRunner` at the same boot site as `evals.NewRunner` (`cmd/datawatch/main.go:1095`) and pass `s.inferenceReg` / `s.inferenceDisp` / `s.sessionMgr` into it — the same handles `handleAsk` already uses.
3. **P2 — cost + config:** `evals` config section, `eval_*` LLM fields, `Consumer:"eval"` plumbing, comparison-table deltas. Document the `allowed_consumers`/`denied_consumers` addition in the PWA compute UI.
4. **P3 — llm_rubric:** real rubric grading through `Dispatcher.Call`; suite fixtures gain rubric cases.

Verification reuses the existing evals test harness (`internal/evals/evals_test.go` style): table-driven matrix runs against a fake dispatcher (return canned `Response` per llm name), a golden child/parent JSON fixture, and a max-parallel contention test proving cells <= cap. Cross-backend independence is guaranteed by construction — cells never share adapter state.

---

## 7. Open questions

- Should `*` matrix also include **disabled** LLMs (operator toggled off)? Proposal intent says no — disabled is operator-signal. Flagged for the spec.
- `RunSet` retention: children already age with normal run listing; the `RunSet` parent row carries no extra GC, but `ListRuns` must keep filtering out `kind:"runset"` rows forever (they are views, not runs).
- Whether `llm_rubric`'s grader call should increment the same per-cell token counters or be reported separately — spec decision, affects the table columns.