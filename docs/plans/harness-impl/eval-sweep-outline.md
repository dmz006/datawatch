# Eval Sweep — Outline (REST endpoints · data structures · MCP tool signatures)

**Date:** 2026-09-14 · **Status:** Outline (consolidated view)
**Canonical specs:** `eval-sweep-spec.md` (normative API & data shapes) · `eval-sweep-technical-spec.md` (behaviour contract)
**Scope:** a single consolidated outline of the three pillars requested — REST endpoints, data structures, MCP tool signatures. Field-level definitions below come from the specs; implementation does not yet exist in `internal/`.

Sweep in one line: one suite fans out over a matrix of LLM backends (optional per-cell model override), each cell runs in parallel under a worker pool, each cell becomes a child `Run`, and a parent `RunSet` aggregates a normalized, delta-annotated comparison table.

Conventions (inherited from the daemon):
- Auth: `Authorization: Bearer <token>` on every `/api/evals/*` route (or `?token=` fallback) — same `fedAuthMiddleware` as the existing five routes.
- Per-endpoint federation capability gates (`federation.CapAutonomous*`) — same `s.fedCap` guard as today.
- Wire JSON is snake_case; success is bare JSON (`writeJSONOK`); errors are `{"error":"<msg>"}` (`jsonError`) with the noted status code.
- RFC 3339 UTC timestamps. All new surface is **additive** — existing routes/MCP tools/`Run` keep their exact contract.
- Bearer cap on `POST` routes = `federation.CapAutonomousRun`.

---

## 1. REST endpoints

Register in `internal/server/server.go` alongside the existing `/api/evals*` group. All return `503 {"error":"evals disabled"}` if the sweep runner is not wired (matches today's evals routes).

| Method | Path | FedCap | Purpose |
|---|---|---|---|
| POST | `/api/evals/sweep` | `CapAutonomousRun` | Run one suite against a backend matrix; return the settled `RunSet` |
| GET | `/api/evals/runsets` | `CapAutonomousList` | List past `RunSet`s (most recent first), optional filters |
| GET | `/api/evals/runsets/{id}` | `CapAutonomousRead` | Fetch one `RunSet` by id (full object) |

### 1.1 `POST /api/evals/sweep`

- **Request body:** the canonical `SweepRequest` (JSON). Query-string form also accepted for curl / comm-router parity (`suite=&backends=a&backends=b&models=x&models=y&max_parallel=2`).
- **Response `200`:** the settled `RunSet` (see §2.3). Per-cell failures are still `200`; read `incomplete` + `cells[i].error`.
- **Status codes:**

  | Code | Condition |
  |---|---|
  | 200 | Sweep settled (even with per-cell failures — see `incomplete`) |
  | 400 | Malformed body · `models` length ∉ {0,1,len(backends)} · `max_cells` overflow |
  | 404 | `suite` not found under `~/.datawatch/evals/` |
  | 422 | A resolved backend is a session-backend kind (no inference adapter) · `*` expanded to zero backends |
  | 503 | Eval/sweep runner not wired |
  | 500 | Unexpected plan / execute / persist error |

- **Audit:** `action=evals_sweep`, `resource_type=eval_runset`, `resource_id=<runset-id>`, `details.suite=<name>` — same audit log as `evals_run`.

### 1.2 `GET /api/evals/runsets`

Query params:

| Param | Type | Default | Notes |
|---|---|---|---|
| `suite` | string | — | filter to one suite name |
| `limit` | int | 0 (unlimited) | max rows, most recent first |

Response `200`: `{"runsets":[ <RunSetSummary>, … ]}` where each row is the cheap projection (§2.9). Full object via `GET /api/evals/runsets/{id}`.

### 1.3 `GET /api/evals/runsets/{id}`

Response `200` = the full `RunSet` object (identical to the `POST /api/evals/sweep` response). `404` on unknown id — including when the id is actually a standalone `Run` (two stores distinguished by the `kind` field; no implicit lookup).

### 1.4 Unchanged routes (byte-for-byte)

`GET /api/evals` · `GET /api/evals/suites` · `POST /api/evals/run` · `GET /api/evals/runs` · `GET /api/evals/runs/{id}`. One required internal change: `ListRuns`/`LoadRun` must skip rows where `kind == "runset"`.

---

## 2. Data structures

All typed in `package evals` (`internal/evals`). Existing `Suite`, `Case`, `Grader`, `CaseResult`, `Mode`, `GraderType` are reused unchanged.

### 2.1 `Run` — modified (+1 field)

```go
type Run struct {
    ID, Suite string; Mode Mode
    StartedAt, FinishedAt time.Time
    PassRate float64; Pass bool; Threshold float64
    Results []CaseResult
    ParentRunID string `json:"parent_run_id,omitempty"` // "" = standalone; else = owner RunSet id
}
```
Same `runs/<id>.json` layout; `omitempty` keeps standalone runs byte-identical.

### 2.2 `SweepRequest` — new (request-only)

```go
type SweepRequest struct {
    Suite       string   `json:"suite"`                // required; filename stem
    Backends    []string `json:"backends"`             // LLM names, or ["*"] = every enabled inference kind
    Models      []string `json:"models,omitempty"`     // 0 (defaults) | 1 (broadcast) | N (positional per backend)
    MaxParallel int      `json:"max_parallel,omitempty"` // 0 = inherit evals.max_parallel; clamped, never rejected
}
```

### 2.3 `RunSet` — new (parent aggregate, persisted)

```go
type RunSet struct {
    ID                 string       `json:"id"`
    Kind               string       `json:"kind"`     // literal "runset" — discriminator
    Suite              string       `json:"suite"`
    Mode               Mode         `json:"mode"`     // denormalized
    Threshold          float64      `json:"threshold"`// denormalized
    Matrix             MatrixSpec   `json:"matrix"`   // resolved, post-expansion spec
    MaxParallel        int          `json:"max_parallel"`
    Cells              []Cell       `json:"cells"`     // one per backend, request order
    ReferenceCellIndex int          `json:"reference_cell_index"` // always 0
    Deltas             []Delta      `json:"deltas"`    // parallel to Cells
    Best               *CellSummary `json:"best,omitempty"`       // nil until status==complete
    Status             RunSetStatus `json:"status"`
    Incomplete         bool         `json:"incomplete"`          // true if any cell failed
    StartedAt          time.Time    `json:"started_at"`
    FinishedAt         time.Time    `json:"finished_at,omitempty"`
}
```
Persisted to `~/.datawatch/evals/runs/<id>.json` with `kind:"runset"`.

### 2.4 `RunSetStatus` — new enum

`pending` → `running` → `complete` (settled, with or without per-cell failures) · `failed` (terminal, plan-time error, `cells == []`).

### 2.5 `MatrixSpec` — new (embedded)

```go
type MatrixSpec struct {
    Backends    []string `json:"backends"`     // resolved LLM names (post-expansion, post-filter)
    Models      []string `json:"models"`       // same len as Backends; "" = LLM row default
    MaxParallel int      `json:"max_parallel"` // effective pool size applied
}
```

### 2.6 `Cell` — new (one row of the comparison table)

```go
type Cell struct {
    Backend     string       `json:"backend"`
    Model       string       `json:"model"`      // empty = LLM default
    Node        string       `json:"node,omitempty"`
    RunID       string       `json:"run_id,omitempty"`
    Pass        bool         `json:"pass"`
    PassRate    float64      `json:"pass_rate"`
    Threshold   float64      `json:"threshold"`
    LatencyMs   int64        `json:"latency_ms"`
    TokensIn    int          `json:"tokens_in"`
    TokensOut   int          `json:"tokens_out"`
    CostUSD     float64      `json:"cost_usd"`
    Error       string       `json:"error,omitempty"`     // set only on cell failure
    CaseResults []CaseResult `json:"case_results"`        // copy of child Run.Results
}
```

### 2.7 `Delta` — new (vs reference cell; sign convention)

`PassRateDelta > 0` = target better; `LatencyDeltaMs > 0` / `CostDeltaUSD > 0` = target worse. `Deltas[0]` is the zero value.

```go
type Delta struct {
    PassRateDelta  float64 `json:"pass_rate_delta"`
    LatencyDeltaMs int64   `json:"latency_delta_ms"`
    CostDeltaUSD   float64 `json:"cost_delta_usd"`
}
```

### 2.8 `CellSummary` — new (the `Best` winner)

```go
type CellSummary struct {
    Backend string  `json:"backend"`
    Model   string  `json:"model"`
    Node    string  `json:"node,omitempty"`
    PassRate float64 `json:"pass_rate"`
    LatencyMs int64  `json:"latency_ms"`
    TokensIn  int    `json:"tokens_in,omitempty"`
    TokensOut int    `json:"tokens_out,omitempty"`
    CostUSD   float64 `json:"cost_usd,omitempty"`
}
```

### 2.9 `RunSetSummary` — new (list projection)

`ID`, `Suite`, `Status`, `Incomplete`, `CellsCount`, `BestPassRate`, `StartedAt`, `FinishedAt`.

### 2.10 `inference.Response` — modified (+2 fields, P0 plumbing)

`inference.Response` is in `package inference` (`internal/inference`). Add two fields:

```go
type Response struct {
    Text       string
    UsedNode   string
    UsedModel  string
    Backend    Kind
    DurationMs int64
    TokensIn   int // new — prompt tokens; 0 = adapter did not report / local-kind unknown
    TokensOut  int // new — completion tokens; same zero convention
}
```
Existing callers compile unchanged; zero is legal and means "not reported".

### 2.11 Config surface (additive `evals` section)

```yaml
evals:
  max_parallel: 3        # global fan-out cap (request clamps to this)
  max_cells: 64          # hard matrix-size ceiling (overflow → 400)
  default_backends: "*"  # fallback when a request omits backends
  cost_rates: {}         # per-LLM BL6 rate override (highest priority)
  rubric_backend: ""     # llm_rubric grading LLM (empty = cell's own backend)
```

---

## 3. MCP tool signatures

Three new tools in `internal/mcp/evals.go` alongside the existing four (unchanged). All proxy to REST exactly as `handleEvalRun` does today (`proxyJSON` / `proxyGet`), returning the body as MCP text via `textOK`. CSV strings are the wire encoding for list parameters (MCP args are flat strings here).

### 3.1 `eval_sweep`

| Param | Type | Required | Description |
|---|---|---|---|
| `suite` | string | **yes** | suite name (`~/.datawatch/evals/<name>.yaml`) |
| `backends` | string (CSV) | no | LLM names, or `*` = every enabled inference kind; default `evals.default_backends` |
| `models` | string (CSV) | no | 1 value = all cells, N values = positional per `backends`; empty = each LLM's default |
| `max_parallel` | string (int) | no | pool size; clamped to `evals.max_parallel` |

```go
func (s *Server) toolEvalSweep() mcpsdk.Tool {
    return mcpsdk.NewTool("eval_sweep",
        mcpsdk.WithDescription("Sweep one eval suite across one or more LLM backends in parallel and return a RunSet with per-cell pass_rate, latency_ms, tokens, cost_usd and cross-backend deltas. Backends accepts comma-separated LLM registry names or '*' for every enabled inference kind; models is an optional CSV of per-cell model overrides."),
        mcpsdk.WithString("suite", mcpsdk.Required(), mcpsdk.Description("suite name (matches ~/.datawatch/evals/<name>.yaml)")),
        mcpsdk.WithString("backends", mcpsdk.Description("comma-separated LLM names, or '*' for every enabled inference kind; default: daemon config evals.default_backends")),
        mcpsdk.WithString("models", mcpsdk.Description("optional CSV model overrides: 1 value = all cells, N values = positional per backends; empty = each LLM's own default")),
        mcpsdk.WithString("max_parallel", mcpsdk.Description("worker-pool size for cell execution; clamped to evals.max_parallel")),
    )
}
```

Handler: map args → `SweepRequest` → `POST /api/evals/sweep` (JSON body) → return the `RunSet` body text.

### 3.2 `eval_list_runsets`

| Param | Type | Required | Description |
|---|---|---|---|
| `suite` | string | no | filter to one suite name |
| `limit` | string (int) | no | max rows (default unlimited) |

```go
func (s *Server) toolEvalListRunsets() mcpsdk.Tool {
    return mcpsdk.NewTool("eval_list_runsets",
        mcpsdk.WithDescription("List past cross-backend eval sweeps (RunSets), most recent first. Returns a projection per row (id, suite, status, incomplete, cells_count, best_pass_rate, timestamps); use eval_get_runset for the full object."),
        mcpsdk.WithString("suite", mcpsdk.Description("filter by suite name (optional)")),
        mcpsdk.WithString("limit", mcpsdk.Description("max rows to return (default unlimited)")),
    )
}
```

### 3.3 `eval_get_runset`

| Param | Type | Required | Description |
|---|---|---|---|
| `id` | string | **yes** | RunSet id (a child-Run id here returns 404) |

```go
func (s *Server) toolEvalGetRunset() mcpsdk.Tool {
    return mcpsdk.NewTool("eval_get_runset",
        mcpsdk.WithDescription("Fetch one cross-backend eval sweep (RunSet) by id: full cells table, deltas vs reference cell, best-summary, per-case results."),
        mcpsdk.WithString("id", mcpsdk.Required(), mcpsdk.Description("RunSet id (from eval_sweep or eval_list_runsets)")),
    )
}
```

### 3.4 Unchanged MCP tools

`eval_list_suites` · `eval_run` · `eval_list_runs` · `eval_get_run` — unchanged. `eval_run` remains the single-backend single-run verb (still the right call for Algorithm Mode Measure and quick self-grade loops).

---

## 4. Cross-surface contract (one canonical shape)

The same `SweepRequest` / `RunSet` shapes are served unchanged through four surfaces; none forks the request:

| Surface | Verb |
|---|---|
| REST | `POST /api/evals/sweep` · `GET /api/evals/runsets` · `GET /api/evals/runsets/{id}` |
| MCP | `eval_sweep` · `eval_list_runsets` · `eval_get_runset` |
| CLI | `datawatch evals sweep <suite> --backends a,b [--models …] [--max-parallel N]` · `evals runsets [--suite …] [--limit N]` · `evals get-runset <id>` |
| Comm | `evals sweep <suite> [backends]` · `evals runsets [suite]` · `evals get-runset <id>` |

## 5. Wiring / ownership (where it lands)

- Types 6.2–6.9 → `package evals`; `Run.ParentRunID` → `internal/evals`; `inference.Response.TokensIn/Out` → `package inference` (P0).
- `SweepRunner` (plan → fan-out → per-cell → settle) composes the existing `*evals.Runner` + the daemon's `inference.Registry`/`Dispatcher` + cost-rate provider; exposed via `HTTPServer.SetEvalsRunner` at the same boot site as `evals.NewRunner` (`cmd/datawatch/main.go`).
- Sequencing: P0 token plumbing → P1 sweep core (types, routes, MCP/CLI/comm) → P2 cost + config + RBAC `Consumer:"eval"` → P3 `llm_rubric` un-stub.

*This outline is a consolidated view; the field-level JSON in `eval-sweep-spec.md` §1–§3 is normative and this document defers to it on any conflict.*
