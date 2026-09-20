# Eval Sweep - API & Data Structures (suite x backend evaluation)

**Date:** 2026-09-13 · **Status:** Draft · **Companion:** `eval-backend-integration.md` (system integration), `enhancement-proposals.md` §1 (behaviour)

This document defines the **external contract** for evaluating model performance across suites and backends: the data structures, REST endpoints, and MCP tool signatures. The sibling integration doc answers *how it wires into existing machinery*; this one answers *what the API surface looks like*, so every layer (REST handlers, MCP proxies, CLI, comm router, PWA, mobile) can be built against one fixed shape.

Conventions:

- All endpoints are authenticated with the daemon bearer token (`Authorization: Bearer $TOKEN`), as with the rest of `/api/*`.
- Federation capability gates apply per endpoint (noted below) using the existing `federation.Cap*` set.
- JSON field names are the wire names (snake_case); Go types are shown alongside.
- Timestamps are RFC 3339 UTC.
- Errors return the daemon's existing error shape (`{"error":"<message>"}`) with the status code indicated per endpoint below.
- All new surface is **additive** - the four existing MCP tools and the five existing REST routes (`/api/evals`, `/api/evals/suites`, `/api/evals/run`, `/api/evals/runs`, `/api/evals/runs/{id}`) keep their current contracts byte-for-byte.

## 1. Data structures

All new and modified types live in `package evals` (`internal/evals`). Existing `Suite`, `Case`, `Grader`, `CaseResult`, `Mode`, `GraderType` are unchanged and reused throughout.

### 1.1 Modified: `Run` (one field added)

One additive field on the existing `Run` struct so sweep children can be grouped under a parent:

```go
type Run struct {
    // — existing fields (unchanged) —
    ID         string
    Suite      string
    Mode       Mode
    StartedAt  time.Time
    FinishedAt time.Time
    PassRate   float64
    Pass       bool
    Threshold  float64
    Results    []CaseResult
    // — new —
    ParentRunID string `json:"parent_run_id,omitempty"`
    // Empty = standalone run (existing behaviour).
    // Non-empty = this Run is a child of a RunSet (the value is the RunSet ID).
}
```

No new file format: children persist to the same `~/.datawatch/evals/runs/<id>.json` as standalone runs.

### 1.2 New: `SweepRequest`

The canonical input for a cross-backend evaluation. One shape is canonical across all surfaces (§6 lists them) — only the encoding differs.

```go
type SweepRequest struct {
    // Suite is the eval suite name (filename without extension under
    // ~/.datawatch/evals/). Required.
    Suite string `json:"suite"`
    // Backends is the list of LLM registry entry names to evaluate.
    // The single-element list ["*"] is a convenience expansion meaning
    // "every enabled inference-kind LLM in the registry" (session-backend
    // kinds excluded; disabled LLMs excluded).
    // An explicit name list bypasses the flag entirely.
    // Empty (omitted) falls back to daemon config evals.default_backends
    // (which itself defaults to "*").
    Backends []string `json:"backends"`
    // Models is an optional per-cell model override list.
    // If len(Models) == 1 the single model applies to every backend cell.
    // If len(Models) == len(Backends) each model pairs positionally with
    // the backend at the same index.
    // Any other length is a 400 error (see §5 error taxonomy).
    // Empty (omitted) = use each LLM's own Model/Models selection.
    Models []string `json:"models,omitempty"`
    // MaxParallel is the requested worker-pool size for cell execution.
    // 0 or omitted = inherit daemon config evals.max_parallel (default 3).
    // Clamped to evals.max_parallel at plan time; request values above
    // the cap are silently lowered, never rejected.
    MaxParallel int `json:"max_parallel,omitempty"`
}
```

### 1.3 New: `RunSet`

The parent object that groups one cross-backend evaluation. It is a *view over its children* — the per-cell `Run` rows — after all cells settle. Persisted as `~/.datawatch/evals/runs/<id>.json` with the literal discriminator `kind: "runset"` so existing `ListRuns`/`LoadRun` never mistake it for a standalone `Run`.

```go
type RunSet struct {
    // ID is a UUID, distinct from any child Run ID.
    ID string `json:"id"`
    // Kind is the literal string "runset" — the discriminator.
    Kind string `json:"kind"`
    // Suite is the suite name that was swept.
    Suite string `json:"suite"`
    // Mode mirrors the suite's mode (capability|regression), denormalized
    // so a RunSet row is self-describing without re-loading the suite.
    Mode Mode `json:"mode"`
    // Threshold mirrors the suite's pass threshold for the same reason.
    Threshold float64 `json:"threshold"`
    // Matrix is the resolved (post-expansion) suite x backend x model spec
    // that was actually executed. Useful for debugging wildcard expansion.
    Matrix MatrixSpec `json:"matrix"`
    // MaxParallel is the effective worker-pool size actually used.
    MaxParallel int `json:"max_parallel"`
    // Cells is the per-backend comparison table, one entry per cell,
    // in the original request order (not sorted by pass_rate).
    Cells []Cell `json:"cells"`
    // ReferenceCellIndex is the index into Cells used as the delta
    // baseline (always 0 = first backend in the request).
    ReferenceCellIndex int `json:"reference_cell_index"`
    // Deltas is a parallel slice to Cells; each entry expresses that
    // cell's metrics relative to the reference cell. Deltas[0] is the
    // zero value (the reference delta against itself).
    Deltas []Delta `json:"deltas"`
    // Best is a convenience summary of the top cell by PassRate,
    // tie-broken by lowest CostUSD, then shortest LatencyMs.
    // nil while status != complete.
    Best *CellSummary `json:"best,omitempty"`
    // Status reflects the aggregate RunSet state.
    Status RunSetStatus `json:"status"`
    // Incomplete is true when one or more cells failed to complete
    // (backend down, timeout, malformed grader, RBAC denial). When true,
    // the failed cells' Error is set and their metrics are zero-valued;
    // the operator should not read the aggregate as a fair comparison.
    Incomplete bool `json:"incomplete"`
    StartedAt  time.Time `json:"started_at"`
    FinishedAt time.Time `json:"finished_at,omitempty"`
}
```

### 1.4 New: `RunSetStatus`

```go
type RunSetStatus string

const (
    RunSetStatusPending  RunSetStatus = "pending"
    RunSetStatusRunning  RunSetStatus = "running"
    RunSetStatusComplete RunSetStatus = "complete"
    RunSetStatusFailed   RunSetStatus = "failed"
)
```

`pending` is the transient state between matrix creation and the first cell starting; `running` is active fan-out; `complete` is settled (with or without per-cell failures); `failed` is terminal for a plan-time error (unknown suite, malformed matrix) where no cell could run.

### 1.5 New: `MatrixSpec`

```go
type MatrixSpec struct {
    // Backends is the resolved list of LLM names actually evaluated
    // (post-wildcard expansion, post disabled-filter,
    //  post session-backend-kind rejection).
    Backends []string `json:"backends"`
    // Models is the per-cell model list. Same length as Backends; a
    // scalar request model is fanned out element-wise into this list;
    // an empty entry means "LLM row default".
    Models []string `json:"models"`
    // MaxParallel is the effective pool size actually applied.
    MaxParallel int `json:"max_parallel"`
}
```

### 1.6 New: `Cell`

One row of the comparison table — the per-backend evaluation of the suite.

```go
type Cell struct {
    // Backend is the LLM registry name.
    Backend string `json:"backend"`
    // Model is the effective model used for this cell (empty = the LLM
    // row's default Model/Models selection). Distinct from Backend so
    // operators can read the exact model tested at a glance.
    Model string `json:"model"`
    // Node is the ComputeNode that actually served the cell (empty for
    // cloud kinds, or when the cell failed before resolving a node).
    Node string `json:"node,omitempty"`
    // RunID is the ID of the child Run holding this cell's per-case
    // results. Empty for cells that failed before producing a Run.
    RunID string `json:"run_id,omitempty"`
    // Pass reflects whether the cell's PassRate met the suite Threshold.
    Pass bool `json:"pass"`
    // PassRate is the cell's pass_rate (0.0..1.0). Zero for failed cells.
    PassRate float64 `json:"pass_rate"`
    // Threshold is the suite's pass threshold, denormalized per-cell so
    // the table is self-describing against a single suite.
    Threshold float64 `json:"threshold"`
    // LatencyMs is the sum of inference call durations across all cases
    // in the cell (per-case inference.DurationMs + any llm_rubric
    // grading call). Zero for failed cells.
    LatencyMs int64 `json:"latency_ms"`
    // TokensIn is the sum of prompt tokens across all cases in the cell.
    // Zero when unknown (local kind did not report, or the cell failed).
    TokensIn int `json:"tokens_in"`
    // TokensOut is the sum of completion tokens. Same zero convention.
    TokensOut int `json:"tokens_out"`
    // CostUSD is the estimated cell cost in USD, computed via BL6
    // session.EstimateCost with the rate for the cell's backend family.
    // Zero for local kinds (default $0) and for cells with 0 tokens.
    CostUSD float64 `json:"cost_usd"`
    // Error is set only for cells that failed (backend down, timeout,
    // malformed grader, RBAC denial). Human-readable.
    Error string `json:"error,omitempty"`
    // CaseResults is the per-case grading detail for this cell — a copy
    // of the child Run's Results, promoted into the Cell so the
    // comparison table is self-contained and consumers do not need to
    // fetch each child Run. Shape is the existing CaseResult type, 1:1.
    CaseResults []CaseResult `json:"case_results"`
}
```

### 1.7 New: `Delta`

Each entry expresses one target cell's metrics **relative to the reference cell** (the first backend in the request). Sign convention: positive `PassRateDelta` = the target outperforms the reference on pass_rate; positive `LatencyDeltaMs` / `CostDeltaUSD` = the target is worse (slower / more expensive) than the reference.

```go
type Delta struct {
    // PassRateDelta is target.PassRate - reference.PassRate.
    PassRateDelta float64 `json:"pass_rate_delta"`
    // LatencyDeltaMs is target.LatencyMs - reference.LatencyMs.
    LatencyDeltaMs int64 `json:"latency_delta_ms"`
    // CostDeltaUSD is target.CostUSD - reference.CostUSD.
    CostDeltaUSD float64 `json:"cost_delta_usd"`
}
```

`RunSet.Deltas[i]` is a parallel slice to `RunSet.Cells[i]`. `Deltas[ReferenceCellIndex]` (i.e. `Deltas[0]`) is the zero value (the reference's delta against itself is trivially zero) and carries no meaning.

### 1.8 New: `CellSummary`

```go
type CellSummary struct {
    Backend   string  `json:"backend"`
    Model     string  `json:"model"`
    Node      string  `json:"node,omitempty"`
    PassRate  float64 `json:"pass_rate"`
    LatencyMs int64   `json:"latency_ms"`
    TokensIn  int     `json:"tokens_in,omitempty"`
    TokensOut int     `json:"tokens_out,omitempty"`
    CostUSD   float64 `json:"cost_usd,omitempty"`
}
```

### 1.9 Unchanged (referenced for context)

- `Suite`, `Case`, `Grader` — unchanged, existing `internal/evals` types.
- `CaseResult`, `Mode`, `GraderType` — unchanged, existing enums / result shape.
- `inference.Request` — unchanged: `{Prompt, SystemPrompt, ModelOverride, Consumer}`. The sweep sets `Consumer: "eval"` and `ModelOverride` per cell.
- `inference.Response` — the integration plan proposes adding `TokensIn`/`TokensOut` (P0 plumbing in the ollama / openwebui / opencode-api / claude adapters). Until that lands, cells report tokens as zero and cost as $0 for local kinds / `n/a` for SaaS kinds.

## 2. REST endpoints

New routes are registered in `internal/server/server.go` alongside the existing `/api/evals*` group (existing routes in `§9 Compatibility` are byte-for-byte unchanged). All new routes require the evals runner to be wired, otherwise they return `503 {"error":"evals disabled"}` (same as today's routes).

### 2.1 Endpoint summary

| Method | Path | Auth | FedCap | Purpose |
|---|---|---|---|---|
| POST | `/api/evals/sweep` | Bearer | `federation.CapAutonomousRun` | Execute a suite against the `SweepRequest` backend matrix, return the full `RunSet` after all cells settle |
| GET | `/api/evals/runsets` | Bearer | `federation.CapAutonomousList` | List past `RunSet` rows (most recent first), optionally filtered |
| GET | `/api/evals/runsets/{id}` | Bearer | `federation.CapAutonomousRead` | Fetch one `RunSet` by id |

`/api/evals/runsets` and `/api/evals/run` are deliberately separate routes so the single-run path keeps its existing request/response shape (`POST ?suite=<name>` → bare `Run`).

### 2.2 POST `/api/evals/sweep`

Request — body is the canonical `SweepRequest` JSON. Query-string form is also accepted for curl / comm-router compatibility (fields parsed into the same struct):

```http
POST /api/evals/sweep HTTP/1.1
Authorization: Bearer <token>
Content-Type: application/json

{
  "suite": "json-output",
  "backends": ["ollama", "claude"],
  "models": ["llama3.1:8b", "claude-3-5-sonnet"],
  "max_parallel": 2
}
```

Equivalent query-string form (all optional fields repeatable where applicable):

```
POST /api/evals/sweep?suite=json-output&backends=ollama&backends=claude&models=llama3.1:8b&models=claude-3-sonnet&max_parallel=2
```

Response `200` — the settled `RunSet` (JSON in `§1.3`):

```json
{
  "id": "9f2c4a1e-…",
  "kind": "runset",
  "suite": "json-output",
  "mode": "capability",
  "threshold": 0.7,
  "matrix": {
    "backends": ["ollama", "claude"],
    "models":   ["llama3.1:8b", "claude-3-5-sonnet"],
    "max_parallel": 2
  },
  "max_parallel": 2,
  "cells": [
    {
      "backend": "ollama",
      "model": "llama3.1:8b",
      "node": "local-ollama",
      "run_id": "11ab1c2d-…",
      "pass": false,
      "pass_rate": 0.58,
      "threshold": 0.7,
      "latency_ms": 4120,
      "tokens_in": 1840,
      "tokens_out": 912,
      "cost_usd": 0.0,
      "case_results": [ { "name": "valid-json", "pass": true, "score": 1.0, "feedback": "jq empty ok" } ]
    },
    {
      "backend": "claude",
      "model": "claude-3-5-sonnet",
      "run_id": "7e5f6a7b-…",
      "pass": true,
      "pass_rate": 0.92,
      "threshold": 0.7,
      "latency_ms": 2310,
      "tokens_in": 1210,
      "tokens_out": 402,
      "cost_usd": 0.00392,
      "case_results": [{ "name": "valid-json", "pass": true, "score": 1.0, "feedback": "jq empty ok" }]
    }
  ],
  "reference_cell_index": 0,
  "deltas": [
    { "pass_rate_delta": 0.0, "latency_delta_ms": 0, "cost_delta_usd": 0.0 },
    { "pass_rate_delta": 0.34, "latency_delta_ms": -1810, "cost_delta_usd": 0.00392 }
  ],
  "best": {
    "backend": "claude",
    "model": "claude-3-5-sonnet",
    "pass_rate": 0.92,
    "latency_ms": 2310,
    "tokens_in": 1210,
    "tokens_out": 402,
    "cost_usd": 0.00392
  },
  "status": "complete",
  "incomplete": false,
  "started_at": "2026-09-13T22:00:00Z",
  "finished_at": "2026-09-13T22:00:09Z"
}
```

Status codes:

| Code | Condition |
|---|---|
| 200 | Sweep ran to completion (per-cell failures are still 200 — see `incomplete`) |
| 400 | Malformed body / models length mismatch / unknown field / max_cells overflow |
| 404 | `suite` not found under `~/.datawatch/evals/` |
| 422 | A resolved backend is a session-backend kind (no inference adapter) — error lists the offending names |
| 503 | Eval runner not wired (daemon started without the evals subsystem) |
| 500 | Unexpected server-side error (plan, execute, persist) |

The audit entry for this endpoint is `action=evals_sweep`, `resource_type=eval_runset`, `resource_id=<runset-id>`, `details.suite=<name>` — written to the same audit log as `evals_run`.

### 2.3 GET `/api/evals/runsets`

| Query param | Type | Default | Notes |
|---|---|---|---|
| `suite` | string | — | Filter to one suite name |
| `limit` | int | 0 (unlimited) | Max rows returned, most recent first |

Response `200`:

```json
{
  "runsets": [
    { "id": "…", "suite": "json-output", "status": "complete", "incomplete": false, "cells_count": 3, "best_pass_rate": 0.92, "started_at": "…", "finished_at": "…" },
    { "id": "…", "suite": "summarization", "status": "complete", "incomplete": true, "cells_count": 2, "best_pass_rate": 0.71, "started_at": "…", "finished_at": "…" }
  ]
}
```

Each list entry is a *projection* of the RunSet (id, suite, status, incomplete, cells_count, best_pass_rate, started_at, finished_at) — cheap enough to return for a long list without materializing each cell. The full object is available via `GET /api/evals/runsets/{id}`.

### 2.4 GET `/api/evals/runsets/{id}`

Response `200` is the full `RunSet` object identical to the `POST /api/evals/sweep` response. `404` on unknown id (including when the id belongs to a standalone `Run` — the two stores are distinguished by the `kind` field; passing a Run id here is a 404, not an implicit lookup).

## 3. MCP tool signatures

Three new tools, all in `internal/mcp/evals.go` alongside the existing four (which are unchanged — §9). All three proxy to REST exactly as `handleEvalRun` does today (`s.proxyJSON` / `s.proxyGet`) and return the body as MCP text content via `textOK`.

### 3.1 Tool: `eval_sweep`

Executes a suite against one or more backends in parallel; returns the full `RunSet` JSON. This is the sweep surface of the existing `eval_run` — a single-backend `eval_sweep` with one backend and no model override is equivalent to `eval_run` + a RunSet wrapper.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `suite` | string | **yes** | Suite name (matches `~/.datawatch/evals/<name>.yaml`) |
| `backends` | string (CSV) | no | Comma-separated LLM registry names; `"*"` = every enabled inference-kind LLM; omitted = `evals.default_backends` |
| `models` | string (CSV) | no | Comma-separated model overrides — scalar (1) applies to all cells, or positional (one per backend); omitted = each LLM's own default |
| `max_parallel` | string (int) | no | Worker-pool size; clamped to `evals.max_parallel`; omitted = daemon config |

Signature (Go, mcpsdk):

```go
func (s *Server) toolEvalSweep() mcpsdk.Tool {
    return mcpsdk.NewTool("eval_sweep",
        mcpsdk.WithDescription("BL259-P2 — sweep one eval suite across one or more LLM backends in parallel and return a RunSet with per-cell pass_rate, latency_ms, tokens, cost_usd and cross-backend deltas. Backends accepts comma-separated LLM registry names or '*' for every enabled inference kind; models is an optional CSV of per-cell model overrides."),
        mcpsdk.WithString("suite", mcpsdk.Required(), mcpsdk.Description("suite name (matches ~/.datawatch/evals/<name>.yaml)")),
        mcpsdk.WithString("backends", mcpsdk.Description("comma-separated LLM names, or '*' for every enabled inference kind; default: daemon config evals.default_backends")),
        mcpsdk.WithString("models", mcpsdk.Description("optional CSV model overrides: 1 value = all cells, N values = positional per backends; empty = each LLM's own default")),
        mcpsdk.WithString("max_parallel", mcpsdk.Description("worker-pool size for cell execution; clamped to evals.max_parallel")),
    )
}
```

Handler maps args → `SweepRequest` → `POST /api/evals/sweep` (JSON body), returns the RunSet body as text.

Errors propagate from REST as `{"error":"…"}` text with the same status code the server would return — the MCP transport already reports non-2xx as a tool error via the daemon's proxy path.

### 3.2 Tool: `eval_list_runsets`

Lists past `RunSet` rows (most recent first). Thin projection of `GET /api/evals/runsets`.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `suite` | string | no | Filter to one suite name |
| `limit` | string (int) | no | Max rows |

```go
func (s *Server) toolEvalListRunsets() mcpsdk.Tool {
    return mcpsdk.NewTool("eval_list_runsets",
        mcpsdk.WithDescription("BL259-P2 — list past cross-backend eval sweeps (RunSets), most recent first. Returns a projection per row (id, suite, status, incomplete, cells_count, best_pass_rate, timestamps); use eval_get_runset for the full object."),
        mcpsdk.WithString("suite", mcpsdk.Description("filter by suite name (optional)")),
        mcpsdk.WithString("limit", mcpsdk.Description("max rows to return (default unlimited)")),
    )
}
```

### 3.3 Tool: `eval_get_runset`

Fetches one full `RunSet` by id (the object identical to the `POST /api/evals/sweep` response).

| Parameter | Type | Required | Description |
|---|---|---|---|
| `id` | string | **yes** | RunSet id (distinct from a child Run id — passing a child Run id returns the daemon 404) |

```go
func (s *Server) toolEvalGetRunset() mcpsdk.Tool {
    return mcpsdk.NewTool("eval_get_runset",
        mcpsdk.WithDescription("BL259-P2 — fetch one cross-backend eval sweep (RunSet) by id: full cells table, deltas vs reference cell, best-summary, per-case results."),
        mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("RunSet id (from eval_sweep or eval_list_runsets)")),
    )
}
```

### 3.4 Unchanged MCP tools

`eval_list_suites`, `eval_run`, `eval_list_runs`, `eval_get_run` — byte-for-byte identical to today; `eval_run` remains the single-backend single-run verb and is still the right call for the Algorithm Mode Measure phase (BL259 P2 v6.10.1) and for quick self-grade loops.

## 4. Metric semantics (normalized across backends)

The comparison table is only meaningful because every column has one definition regardless of backend kind:

| Column | Definition | Source |
|---|---|---|
| `pass_rate` | `count(CaseResult.pass) / len(Suite.Cases)` for that cell — identical to a standalone `Run.PassRate` for the same suite + grader set | `evals.Runner.Execute` per child |
| `pass` | `pass_rate >= Suite.PassThreshold` (the same threshold every cell uses, so pass/fail is comparable across backends) | suite YAML |
| `latency_ms` | `Σ inference.Response.DurationMs` over all case calls in the cell, inclusive of any `llm_rubric` grading calls | `inference.Dispatcher.Call` |
| `tokens_in` / `tokens_out` | `Σ inference.Response.TokensIn/Out` over the case calls; `0` when the adapter did not report (local kinds without usage in the protocol response) | adapter `usage` parsing (P0 in the integration plan) |
| `cost_usd` | `session.EstimateCost(rate, tokens_in, tokens_out)` per case, summed; rate is the cell backend family's row in `Manager.SetCostRates` / `DefaultCostRates`; local kinds default $0 | BL6 (`internal/session/cost.go`) |
| `deltas[i]` | `cell[i].metric − cell[reference].metric` per column, sign convention in `§1.7` | read-side aggregation |
| `best` | max `pass_rate`; tie-break min `cost_usd`; tie-break min `latency_ms` | read-side aggregation |

Two deliberate non-goals kept out of the table:

- **Token *efficiency*** (pass rate per dollar) would need a cost model with per-model accuracy curves; operators can derive it from `pass_rate` and `cost_usd` in their own tooling.
- **Per-case cross-backend joins** (which *case* one backend failed that another passed) — already available by aligning `CaseResults[i].name` across cells; no dedicated structure.

## 5. Error taxonomy

| Condition | HTTP | MCP / CLI surfacing |
|---|---|---|
| Unknown suite | 404 | error text `suite not found: <name>` |
| `models` length not 0/1/N (N=len(backends)) | 400 | `models length 2 does not match 0, 1, or backends length 3` |
| Session-backend kind in the resolved matrix (`claude-code`, `aider`, `goose`, `opencode-acp`, …) | 422 | `session-backend kind(s) have no inference adapter: <names>` — the planner rejects at matrix time, never at cell time |
| `*` expansion yielded zero backends (all disabled) | 422 | `no enabled inference-kind LLMs to sweep against` |
| Matrix exceeds `evals.max_cells` | 400 | `matrix size N exceeds evals.max_cells M` |
| A cell's inference call fails (backend down / timeout / RBAC denial) | — cell-level | `Cell.Error` set, `Cell.Pass=false`, `RunSet.Incomplete=true`; siblings unaffected |
| `max_parallel` above `evals.max_parallel` | — | silently clamped; `RunSet.MaxParallel` reflects the applied value |

## 6. Companion surfaces (same contract)

The canonical shapes above are served unchanged through:

- **CLI** (`cmd/datawatch/cli_evals.go`): `datawatch evals sweep <suite> --backends a,b [--models ...] [--max-parallel N]`, plus `evals runsets [--suite …] [--limit N]` and `evals get-runset <id>` — thin `daemonJSON`/`daemonGet` wrappers of §2.
- **Comm channel** (`internal/router/evals.go`): `evals sweep <suite> [backends]`, `evals runsets [suite]`, `evals get-runset <id>` — thin proxies of §2.

All four surfaces build the same `SweepRequest`; there is no surface-specific fork in the request.

## 7. Compatibility

- Existing routes `GET /api/evals`, `GET /api/evals/suites`, `POST /api/evals/run`, `GET /api/evals/runs`, `GET /api/evals/runs/{id}` are unchanged.
- Existing MCP tools `eval_list_suites` / `eval_run` / `eval_list_runs` / `eval_get_run` are unchanged.
- `Run` gains only `parent_run_id` (omitempty) — existing consumers (PWA EvalsCard, mobile, Algorithm Mode Measure) read the rest byte-for-byte.
- `ListRuns` / `LoadRun` must skip rows where `kind == "runset"` (a RunSet file on disk is not a Run) — a one-line filter; sweep children themselves remain ordinary `Run` rows and keep appearing in `evals runs`.

## 8. Open questions (carried from `eval-backend-integration.md` §7)

1. Does `*` include operator-disabled LLMs? Spec answer here: **no** — disabled is an operator signal.
2. `RunSet` retention: no extra GC; piggybacks the runs directory. `ListRuns` must filter `kind` forever.
3. `llm_rubric` grading-call tokens: counted into the cell's `tokens_in/out` (spec answer here, §4) and therefore into `cost_usd` — the rubric call is part of the cell's measurable cost.
