# Eval Sweep — API Design

**Date:** 2026-09-15 · **Status:** Draft (API-surface view)
**Scope:** External contract only — REST endpoints, MCP tool signatures, request/response wire structures. No implementation, no sequencing, no backend wiring.
**Companions (source of truth on behaviour & Go types):**
- `eval-sweep-technical-spec.md` — behaviour contract (authoritative where they conflict)
- `eval-sweep-spec.md` — Go-level type definitions & JSON tags
- `eval-sweep-outline.md` — consolidated one-page view
- `eval-backend-integration.md` — registry / dispatcher / config deltas

> **Normative note.** The *behavioural* contract (matrix expansion, grading, backend compatibility) is authoritative in `eval-sweep-technical-spec.md`. This document renders that same contract as an **API-surface reference**: REST endpoints as a table, MCP tool signatures as code blocks, and every request/response structure in **YAML** (the daemon's on-disk / wire encoding). Where this document and a companion disagree, the companion wins.

---

## Conventions

Inherited from the daemon; apply to every surface below.

- **Auth:** `Authorization: Bearer <token>` on every `/api/evals/*` route (or `?token=` fallback) — the same `fedAuthMiddleware` as the existing five evals routes.
- **Federation gates:** per-endpoint, reusing the `federation.CapAutonomous*` set (same `s.fedCap` guard). The bearer cap for the `POST` sweep route is `federation.CapAutonomousRun`.
- **Wire encoding:** `snake_case` JSON field names; YAML shown below is the *structural* view (field names are byte-identical to the wire). Success responses are bare JSON (`writeJSONOK`); errors are `{"error":"<msg>"}` (`jsonError`) with the status code named per endpoint.
- **Timestamps:** RFC 3339 UTC.
- **Additive:** nothing a v6 consumer already reads is removed or reshaped. All new surface is additive; `eval_run` and the five existing routes keep their exact contract.
- **Runner-not-wired guard:** every new route returns `503 {"error":"evals disabled"}` when the sweep runner is unset — identical to today's evals routes.

---

## 1. REST endpoints

Registered in `internal/server/server.go` alongside the existing `/api/evals*` group. The existing five routes are **byte-for-byte unchanged** (listed in §6 for reference).

| # | Method | Path | Auth | FedCap | Purpose |
|---|--------|------|------|--------|---------|
| R1 | `POST` | `/api/evals/sweep` | Bearer | `CapAutonomousRun` | Run one suite against a `SweepRequest` backend × model matrix; block until all cells settle; return the full `RunSet` |
| R2 | `GET`  | `/api/evals/runsets` | Bearer | `CapAutonomousList` | List past `RunSet`s (most recent first), optional `suite`/`limit` filters; returns cheap projections |
| R3 | `GET`  | `/api/evals/runsets/{id}` | Bearer | `CapAutonomousRead` | Fetch one full `RunSet` by id (identical object to the R1 response) |

**Routing invariants**

- `/api/evals/sweep` is deliberately distinct from the existing `/api/evals/run` so the single-run path keeps its bare-`Run` shape. A one-backend `SweepRequest` is equivalent to `eval_run` wrapped in a `RunSet` — the two are not interchangeable at the wire level.
- `{id}` is a path segment; a child-`Run` id passed here returns `404` (the two stores share the `runs/` directory but are distinguished by the `kind` field — no implicit lookup).
- All three routes 503 when `s.evalsRunner` is nil, identically to the existing five.

### 1.1 `POST /api/evals/sweep` (R1)

**Request** — body is the canonical `SweepRequest` (§2.1). A query-string form is also accepted for curl / comm-router parity (`suite=` and repeatable `backends=`/`models=`); both map to the same struct.

```http
POST /api/evals/sweep HTTP/1.1
Authorization: Bearer <token>
Content-Type: application/json

{
  "suite": "json-output",
  "backends": ["ollama", "claude"],
  "models":   ["llama3.1:8b", "claude-3-5-sonnet"],
  "max_parallel": 2
}
```

**Response `200`** — the settled `RunSet` (§2.2). Per-cell failures still return `200`; read `incomplete` and `cells[i].error` to judge fairness.

**Status codes**

| Code | Condition |
|------|-----------|
| `200` | Sweep settled (even with per-cell failures — see `incomplete`) |
| `400` | Malformed body · `models` length ∉ {0, 1, len(backends)} · matrix size exceeds `evals.max_cells` · unknown body field |
| `404` | `suite` not found under `~/.datawatch/evals/` |
| `422` | A resolved backend is a session-backend kind (no inference adapter) · `*` expanded to zero backends (all disabled) |
| `503` | Eval/sweep runner not wired |
| `500` | Unexpected plan / execute / persist error |

**Audit** — one entry per sweep: `action=evals_sweep`, `resource_type=eval_runset`, `resource_id=<runset-id>`, `details.suite=<name>`. Same audit log as the existing `evals_run`, so the sweep is visible to `audit_query` / `get_alerts` with no new plumbing.

### 1.2 `GET /api/evals/runsets` (R2)

Query parameters:

| Param | Type | Default | Notes |
|-------|------|---------|-------|
| `suite` | string | — | Filter to one suite name |
| `limit` | int | `0` (unlimited) | Max rows returned, most recent first |

**Response `200`** — `{"runsets":[ <RunSetSummary>, … ]}`. Each row is the cheap projection (§2.4); the full object is available via R3.

### 1.3 `GET /api/evals/runsets/{id}` (R3)

**Response `200`** — the full `RunSet` object, identical to the R1 response body. `404` on unknown id — including when the id actually belongs to a standalone `Run` (the two stores are distinguished by the `kind` field; there is no implicit lookup).

---

## 2. Request/response structures (YAML)

All structures below are the *wire shapes* — the field names and nesting are byte-identical to the JSON the route emits, rendered in YAML for readability. Types live in `package evals` (`internal/evals`); existing types `Suite` / `Case` / `Grader` / `CaseResult` / `Mode` / `GraderType` are reused unchanged and shown only where they are embedded.

### 2.1 `SweepRequest` — request (R1 body)

The canonical input for a cross-backend evaluation. One shape across all surfaces — only the encoding differs (JSON body / query string / CLI flags / comm arg string). The MCP encoder flattens `backends` and `models` to CSV.

```yaml
# --- SweepRequest (request-only, never persisted) ---
suite: "json-output"             # REQUIRED — suite name (filename stem under ~/.datawatch/evals/)
backends:                        # LLM registry names; ["*"] = every enabled inference kind
  - "ollama"
  - "claude"
  # omitted → daemon config evals.default_backends (which itself defaults to "*")
models:                          # OPTIONAL per-cell model override
  - "llama3.1:8b"                #   len(models)==0 → each LLM's own Model/Models
  - "claude-3-5-sonnet"          #   len(models)==1 → broadcast to all cells
  #                              #   len(models)==len(backends) → positional pairing
  #                              #   any other length → 400
max_parallel: 2                  # OPTIONAL worker-pool size; 0/omitted → evals.max_parallel (default 3);
                                 # clamped to the cap, never rejected
```

**`backends` expansion rules** (planner order, authoritative in the technical spec §1.3):

1. Load the suite once; fail-fast `404` if unknown.
2. Wildcard `["*"]` → every LLM where `IsSessionBackendKind == false`, `Disabled == false`, and an adapter is registered. An explicit name list bypasses the wildcard; unknown names → `400`.
3. Explicit list containing a disabled LLM → `422` listing the name (disabled is an operator signal, not a silent skip).
4. Explicit list containing a session-backend kind (`claude-code`, `aider`, `goose`, `gemini`-CLI, `shell`, `opencode-acp`, `opencode-prompt`) → `422` listing all offending names. Rejected at plan time, never at cell time.
5. `*` expansion yielding zero backends (all disabled) → `422` `no enabled inference-kind LLMs to sweep against`.
6. `models` length must be 0, 1, or `len(backends)` — else `400` `models length N does not match 0, 1, or backends length M`.
7. `max_parallel` is clamped to `evals.max_parallel` (values above are silently lowered; `RunSet.max_parallel` records the applied value).
8. Cell count above `evals.max_cells` (default 64) → `400` `matrix size N exceeds evals.max_cells M` (DoS guard).

### 2.2 `RunSet` — response (R1 body · R3 body · persisted parent)

The parent object that groups one cross-backend evaluation. A **view over its children** (the per-cell `Run` rows) after all cells settle. Persisted to `~/.datawatch/evals/runs/<id>.json` with the literal discriminator `kind: "runset"` so existing `ListRuns` / `LoadRun` never mistake it for a standalone `Run`.

```yaml
# --- RunSet (parent aggregate) ---
id: "9f2c4a1e-…"                 # UUID, distinct from any child Run ID
kind: "runset"                   # LITERAL discriminator (required on every persisted row)
suite: "json-output"             # the suite name that was swept
mode: "capability"               # denormalized from suite (capability | regression)
threshold: 0.7                   # denormalized from suite (same value for every cell)

# --- resolved, post-expansion spec actually executed ---
matrix:
  backends: ["ollama", "claude"]   # resolved LLM names (post-wildcard, post-filter)
  models:   ["llama3.1:8b", "claude-3-5-sonnet"]   # same length as backends; "" = LLM default
  max_parallel: 2                   # effective pool size actually applied
max_parallel: 2                    # echo of matrix.max_parallel at top level

# --- per-cell comparison table (one entry per cell, request order) ---
cells: [ <Cell>, … ]               # see §2.3
reference_cell_index: 0            # INVARIANT — always 0 (first backend in the request)

# --- parallel slice to cells; each entry is that cell's delta vs the reference ---
deltas: [ <Delta>, … ]             # see §2.3; deltas[0] is the zero value

# --- best cell (argmax pass_rate; tie-break min cost, then min latency) ---
best:                              # nil while status != "complete"
  backend: "claude"
  model: "claude-3-5-sonnet"
  node: ""                         # omit for SaaS / failed-before-node cells
  pass_rate: 0.92
  latency_ms: 2310
  tokens_in: 1210
  tokens_out: 402
  cost_usd: 0.00392

# --- lifecycle ---
status: "complete"                 # pending | running | complete | failed
incomplete: false                  # true iff one or more cells failed (see cell.error)
started_at: "2026-09-13T22:00:00Z"
finished_at: "2026-09-13T22:00:09Z"   # omit while status != "complete"
```

**Status semantics**

| Status | Meaning | Terminal? |
|--------|---------|-----------|
| `pending` | Between matrix creation and the first cell starting | transient |
| `running` | Active fan-out (one or more cells in flight) | transient |
| `complete` | All cells terminal (success **or** per-cell error). The operator judges from `incomplete` + `cells[i].error` what the table means. | **terminal** |
| `failed` | Plan-time error where no cell could start (unknown suite, malformed matrix). `cells == []`. | **terminal** |

**Invariants**

- `reference_cell_index` is always `0`. The reference cell is *the first backend in the request*; cells preserve request order, not pass_rate order.
- `deltas` is a parallel slice to `cells`; `len(deltas) == len(cells)`.
- `deltas[0]` is the zero value (reference's delta against itself) and is **not** semantically meaningful — consumers should skip index 0.
- `best` is `nil` while `status != "complete"`.
- `incomplete` is `true` iff at least one `cell.error` is non-empty.

### 2.3 `Cell` + `Delta` — elements

**`Cell`** — one row of the comparison table: the per-backend evaluation of the suite.

```yaml
# --- Cell (one row of RunSet.cells) ---
backend: "ollama"                  # LLM registry name
model: "llama3.1:8b"               # effective model used for this cell ("" = LLM row default)
node: "local-ollama"               # ComputeNode that served the cell; omit for SaaS / failed-pre-node
run_id: "11ab1c2d-…"               # child Run ID holding this cell's per-case results; omit if cell failed before producing a Run
pass: false                        # pass_rate >= threshold
pass_rate: 0.58                    # 0.0..1.0; 0.0 for failed cells
threshold: 0.7                     # denormalized per-cell (same as RunSet.threshold)
latency_ms: 4120                   # Σ inference.DurationMs across all case calls in the cell (incl. rubric grading call); 0 for failed cells
tokens_in: 1840                    # Σ prompt tokens; 0 when adapter did not report (local kind / cell failed)
tokens_out: 912                    # Σ completion tokens; same zero convention
cost_usd: 0.0                      # Σ session.EstimateCost via BL6 with the cell backend's rate; 0 for local kinds / 0 tokens
error: ""                          # set ONLY for failed cells (backend down / timeout / RBAC denial); human-readable
case_results:                      # copy of the child Run.Results (the existing CaseResult shape), promoted into the Cell
  - name: "valid-json"
    pass: true
    score: 1.0
    feedback: "jq empty ok"
    # actual: "..."                # optional; the backend's response text for this case
```

**`Delta`** — one entry of `RunSet.deltas`, parallel to `cells`. Expresses that cell's metrics **relative to the reference cell** (`cells[0]`). Sign convention:

- `pass_rate_delta > 0` → target outperforms the reference on pass_rate (better).
- `latency_delta_ms > 0` → target is *slower* than the reference (worse).
- `cost_delta_usd > 0` → target is *more expensive* than the reference (worse).

```yaml
# --- Delta (relative to reference cell cells[0]) ---
pass_rate_delta: 0.34              # target.pass_rate - reference.pass_rate
latency_delta_ms: -1810            # target.latency_ms - reference.latency_ms  (negative = faster)
cost_delta_usd: 0.00392            # target.cost_usd - reference.cost_usd      (positive = more expensive)
```

**`best` (the winner `Cell`)** — a `CellSummary`, populated from the winning cell:

```yaml
# --- CellSummary (the RunSet.best winner) ---
backend: "claude"
model: "claude-3-5-sonnet"
node: ""                           # omit for SaaS / failed-pre-node
pass_rate: 0.92
latency_ms: 2310
tokens_in: 1210
tokens_out: 402
cost_usd: 0.00392
```

**Metric semantics** (one definition per column, regardless of backend kind):

| Column | Definition | Source |
|--------|------------|--------|
| `pass_rate` | `count(case_results.pass) / len(suite.cases)` for that cell — identical to a standalone `Run.pass_rate` for the same suite + grader set | `evals.Runner.Execute` per child |
| `pass` | `pass_rate >= suite.pass_threshold` (the *same* threshold every cell uses, so pass/fail is directly comparable across backends) | suite YAML |
| `latency_ms` | `Σ inference.Response.DurationMs` over all case calls in the cell, inclusive of any `llm_rubric` grading calls | `inference.Dispatcher.Call` |
| `tokens_in` / `tokens_out` | `Σ inference.Response.TokensIn/Out` over the case calls; `0` when the adapter did not report (local kinds without usage in the protocol response) | adapter `usage` parsing |
| `cost_usd` | `Σ session.EstimateCost(rate, in, out)` per case; rate is the cell backend's row in the BL6 rate table (`Manager.SetCostRates` / `DefaultCostRates`); local kinds default $0 | BL6 (`internal/session/cost.go`) |
| `deltas[i]` | `cell[i].metric − cell[0].metric` per column (sign convention above) | read-side aggregation |
| `best` | max `pass_rate`; tie-break min `cost_usd`; tie-break min `latency_ms` | read-side aggregation |

**Deliberate non-goals** (kept out of the table):

- *Token efficiency* (pass-rate-per-dollar) would need a cost model with per-model accuracy curves; operators can derive it from `pass_rate` × `cost_usd` in their own tooling.
- *Per-case cross-backend joins* (which *case* one backend failed that another passed) — already available by aligning `case_results[i].name` across cells; no dedicated structure.

### 2.4 `RunSetSummary` — list projection (R2 body element)

A cheap projection returned by `GET /api/evals/runsets` — sufficient for a long list without materializing each cell. The full object is available via R3 / the MCP `eval_get_runset`.

```yaml
# --- RunSetSummary (one element of R2's runsets[] array) ---
id: "9f2c4a1e-…"                   # RunSet ID
suite: "json-output"
status: "complete"                 # pending | running | complete | failed
incomplete: false                  # true iff one or more cells failed
cells_count: 2                     # len(cells) — cheap to include in a projection
best_pass_rate: 0.92               # best.pass_rate (0.0 / null while status != complete)
started_at: "2026-09-13T22:00:00Z"
finished_at: "2026-09-13T22:00:09Z"
```

**R2 full response shape:**

```yaml
# --- GET /api/evals/runsets → 200 ---
runsets:
  - id: "9f2c4a1e-…"
    suite: "json-output"
    status: "complete"
    incomplete: false
    cells_count: 2
    best_pass_rate: 0.92
    started_at: "2026-09-13T22:00:00Z"
    finished_at: "2026-09-13T22:00:09Z"
  - id: "77ab8c9d-…"
    suite: "summarization"
    status: "complete"
    incomplete: true               # one cell failed
    cells_count: 2
    best_pass_rate: 0.71
    started_at: "2026-09-13T21:40:00Z"
    finished_at: "2026-09-13T21:40:07Z"
```

### 2.5 Full `RunSet` example (R1 / R3 response body)

The complete object a single `POST /api/evals/sweep` returns after all cells settle — every section of §2.2 populated, both cells present, `best` set, deltas computed.

```yaml
# --- POST /api/evals/sweep → 200  (full RunSet) ---
id: "9f2c4a1e-…"
kind: "runset"
suite: "json-output"
mode: "capability"
threshold: 0.7
matrix:
  backends: ["ollama", "claude"]
  models:   ["llama3.1:8b", "claude-3-5-sonnet"]
  max_parallel: 2
max_parallel: 2
cells:
  - backend: "ollama"
    model: "llama3.1:8b"
    node: "local-ollama"
    run_id: "11ab1c2d-…"
    pass: false
    pass_rate: 0.58
    threshold: 0.7
    latency_ms: 4120
    tokens_in: 1840
    tokens_out: 912
    cost_usd: 0.0
    # error: ""                        # not set — this cell completed (just didn't pass the threshold)
    case_results:
      - name: "valid-json"
        pass: true
        score: 1.0
        feedback: "jq empty ok"
      - name: "roundtrip"
        pass: false
        score: 0.0
        feedback: "expected 'ok' not found"
  - backend: "claude"
    model: "claude-3-5-sonnet"
    run_id: "7e5f6a7b-…"
    pass: true
    pass_rate: 0.92
    threshold: 0.7
    latency_ms: 2310
    tokens_in: 1210
    tokens_out: 402
    cost_usd: 0.00392
    # node: ""                         # SaaS kind → no ComputeNode → omitted
    case_results:
      - name: "valid-json"
        pass: true
        score: 1.0
        feedback: "jq empty ok"
      - name: "roundtrip"
        pass: true
        score: 1.0
        feedback: "match"
      # ... remaining cases elided for brevity; len(case_results) == len(suite.cases)
reference_cell_index: 0
deltas:
  - pass_rate_delta: 0.0              # Deltas[0] — zero value (reference vs itself)
    latency_delta_ms: 0
    cost_delta_usd: 0.0
  - pass_rate_delta: 0.34             # 0.92 - 0.58
    latency_delta_ms: -1810           # 2310 - 4120  (claude is faster)
    cost_delta_usd: 0.00392           # 0.00392 - 0.0 (claude is more expensive)
best:
  backend: "claude"
  model: "claude-3-5-sonnet"
  pass_rate: 0.92
  latency_ms: 2310
  tokens_in: 1210
  tokens_out: 402
  cost_usd: 0.00392
status: "complete"
incomplete: false
started_at: "2026-09-13T22:00:00Z"
finished_at: "2026-09-13T22:00:09Z"
```

**A `failed` cell** (one of two cells in a 2-cell sweep) looks like this inside `cells`:

```yaml
# --- a cell that failed before producing a Run ---
- backend: "gemini-api"
  model: "gemini-2.5-flash"
  # node: ""                            # failed before node resolution → omitted
  # run_id: ""                          # failed before producing a Run → omitted
  pass: false
  pass_rate: 0.0
  threshold: 0.7
  latency_ms: 0
  tokens_in: 0
  tokens_out: 0
  cost_usd: 0.0
  error: "no backend available: gemini-api (all ComputeNodes denied consumer 'eval')"
  case_results: []                        # empty — no cell results were produced
```

When such a cell is present, `incomplete: true` on the `RunSet`, and the *sibling* cells in `cells[]` still carry their full metrics — the sweep does not abort siblings.

### 2.6 Unchanged (referenced for context)

- `Suite`, `Case`, `Grader`, `Mode`, `GraderType` — unchanged, existing `internal/evals` types.
- `CaseResult` — unchanged: `{name, pass, score, feedback, actual?}`.
- `Run` — **one additive field** `parent_run_id` (`omitempty`, `""` = standalone). Standalone runs serialize byte-identically to today; sweep children carry `parent_run_id = <runset-id>`. Persisted to the same `runs/<id>.json` layout, surfaced by the existing `GET /api/evals/runs`.
- `Run.ParentRunID` is the *only* change to an existing type. The two additive int fields on `inference.Response.TokensIn/Out` are plumbing (not a wire structure on a route) and are documented in the companion integration doc, not here.

---

## 3. MCP tool signatures

Three new tools, all in `internal/mcp/evals.go` alongside the existing four (which are unchanged — §3.4). All three proxy to REST exactly as `handleEvalRun` does today (`proxyJSON` / `proxyGet`) and return the body as MCP text content via `textOK`. CSV strings are the wire encoding for list parameters (MCP args here are flat strings; the handler maps CSV → `[]string` before building the `SweepRequest`).

### 3.1 `eval_sweep`

Executes a suite against one or more backends in parallel; returns the full `RunSet` JSON (the object from §2.2). The sweep surface of `eval_run`: a single-backend `eval_sweep` with one backend and no model override is equivalent to `eval_run` wrapped in a `RunSet`.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `suite` | string | **yes** | Suite name (matches `~/.datawatch/evals/<name>.yaml`) |
| `backends` | string (CSV) | no | Comma-separated LLM registry names; `"*"` = every enabled inference-kind LLM; omitted = `evals.default_backends` |
| `models` | string (CSV) | no | Comma-separated model overrides — scalar (1) applies to all cells, or positional (one per `backends`); omitted = each LLM's own default |
| `max_parallel` | string (int) | no | Worker-pool size; clamped to `evals.max_parallel`; omitted = daemon config |

Signature (Go, `mcpsdk`):

```go
func (s *Server) toolEvalSweep() mcpsdk.Tool {
    return mcpsdk.NewTool("eval_sweep",
        mcpsdk.WithDescription(
            "BL259-P2 — sweep one eval suite across one or more LLM backends in "+
            "parallel and return a RunSet with per-cell pass_rate, latency_ms, "+
            "tokens, cost_usd and cross-backend deltas. Backends accepts "+
            "comma-separated LLM registry names or '*' for every enabled "+
            "inference kind; models is an optional CSV of per-cell model overrides."),
        mcpsdk.WithString("suite",
            mcpsdk.Required(),
            mcpsdk.Description("suite name (matches ~/.datawatch/evals/<name>.yaml)")),
        mcpsdk.WithString("backends",
            mcpsdk.Description("comma-separated LLM names, or '*' for every "+
                "enabled inference kind; default: daemon config evals.default_backends")),
        mcpsdk.WithString("models",
            mcpsdk.Description("optional CSV model overrides: 1 value = all cells, "+
                "N values = positional per backends; empty = each LLM's own default")),
        mcpsdk.WithString("max_parallel",
            mcpsdk.Description("worker-pool size for cell execution; clamped to "+
                "evals.max_parallel")),
    )
}
```

**Handler contract:** map CSV args → `SweepRequest` → `POST /api/evals/sweep` (JSON body) → return the `RunSet` body as MCP text. Errors propagate from REST as `{"error":"…"}` text with the same status code the server would return — the MCP transport already reports non-2xx as a tool error via the daemon's proxy path, so this doc does not re-specify the error surface.

### 3.2 `eval_list_runsets`

Lists past `RunSet` rows (most recent first). Thin projection of R2.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `suite` | string | no | Filter to one suite name |
| `limit` | string (int) | no | Max rows (default unlimited) |

Signature (Go, `mcpsdk`):

```go
func (s *Server) toolEvalListRunsets() mcpsdk.Tool {
    return mcpsdk.NewTool("eval_list_runsets",
        mcpsdk.WithDescription(
            "BL259-P2 — list past cross-backend eval sweeps (RunSets), most recent "+
            "first. Returns a projection per row (id, suite, status, incomplete, "+
            "cells_count, best_pass_rate, timestamps); use eval_get_runset for "+
            "the full object."),
        mcpsdk.WithString("suite", mcpsdk.Description("filter by suite name (optional)")),
        mcpsdk.WithString("limit", mcpsdk.Description("max rows to return (default unlimited)")),
    )
}
```

**Handler contract:** map args → `GET /api/evals/runsets?suite=&limit=` → return the `{"runsets":[…]}` body as MCP text.

### 3.3 `eval_get_runset`

Fetches one full `RunSet` by id (the object from §2.2, identical to the R1 response).

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `id` | string | **yes** | RunSet id (a child-`Run` id passed here returns the daemon's `404`) |

Signature (Go, `mcpsdk`):

```go
func (s *Server) toolEvalGetRunset() mcpsdk.Tool {
    return mcpsdk.NewTool("eval_get_runset",
        mcpsdk.WithDescription(
            "BL259-P2 — fetch one cross-backend eval sweep (RunSet) by id: full "+
            "cells table, deltas vs reference cell, best-summary, per-case results."),
        mcpsdk.WithString("id",
            mcpsdk.Required(),
            mcpsdk.Description("RunSet id (from eval_sweep or eval_list_runsets)")),
    )
}
```

**Handler contract:** `GET /api/evals/runsets/<id>` → return the full `RunSet` body as MCP text. `404` on unknown id (including a standalone-`Run` id).

### 3.4 Unchanged MCP tools

`eval_list_suites` · `eval_run` · `eval_list_runs` · `eval_get_run` — byte-for-byte identical to today (see `internal/mcp/evals.go`). `eval_run` remains the single-backend, single-run verb and is still the right call for the Algorithm Mode Measure phase (BL259 P2 v6.10.1) and for quick self-grade loops. The three new tools are purely additive; no existing tool's signature, description, or handler changes.

---

## 4. Cross-surface contract (one canonical shape)

The same `SweepRequest` / `RunSet` shapes are served unchanged through four surfaces. **None forks the request** — they all build one `SweepRequest` and hit the same REST route, so behaviour is identical regardless of entry point. The encoding differs (JSON body / CSV string / CLI flag / comm arg string) but the wire shape is single.

| Surface | Verb | Maps to |
|---------|------|---------|
| **REST** | `POST /api/evals/sweep` (body = `SweepRequest`) · `GET /api/evals/runsets` · `GET /api/evals/runsets/{id}` | the canonical route |
| **MCP** | `eval_sweep` (CSV `backends`/`models`) · `eval_list_runsets` · `eval_get_runset` | proxy to REST |
| **CLI** | `datawatch evals sweep <suite> --backends a,b [--models …] [--max-parallel N]` · `evals runsets [--suite …] [--limit N]` · `evals get-runset <id>` | `daemonJSON` / `daemonGet` of REST |
| **Comm** | `evals sweep <suite> [backends]` · `evals runsets [suite]` · `evals get-runset <id>` | thin proxy of REST |

There is no surface-specific fork in the request or response. A `SweepRequest` from any surface is byte-identical to the same `SweepRequest` from any other surface, and the `RunSet` returned by any surface is byte-identical to the `RunSet` returned by any other.

---

## 5. Error taxonomy (REST → MCP / CLI / comm)

| Condition | HTTP | MCP / CLI / comm surfacing |
|-----------|------|----------------------------|
| Unknown suite | `404` | error text `suite not found: <name>` |
| `models` length ∉ {0, 1, len(backends)} | `400` | `models length 2 does not match 0, 1, or backends length 3` |
| Session-backend kind in the resolved matrix | `422` | `session-backend kind(s) have no inference adapter: <names>` — rejected at matrix time, never at cell time |
| `*` expansion yielded zero backends (all disabled) | `422` | `no enabled inference-kind LLMs to sweep against` |
| Matrix size exceeds `evals.max_cells` | `400` | `matrix size N exceeds evals.max_cells M` |
| A cell's inference call fails (backend down / timeout / RBAC denial) | — (cell-level, sweep still `200`) | `cell.error` set, `cell.pass=false`, `RunSet.incomplete=true`; siblings unaffected |
| `max_parallel` above `evals.max_parallel` | — (clamped) | silently clamped; `RunSet.max_parallel` reflects the applied value |
| Malformed request body | `400` | `invalid request: <field>` |
| Eval/sweep runner not wired | `503` | `evals disabled` |

The MCP transport already reports non-2xx as a tool error, so MCP / CLI / comm surfaces do not add their own error taxonomy — they surface the daemon's `{"error":"<msg>"}` text verbatim.

---

## 6. Compatibility

**Unchanged (byte-for-byte, v6 contract preserved):**

- Existing routes: `GET /api/evals` · `GET /api/evals/suites` · `POST /api/evals/run` · `GET /api/evals/runs` · `GET /api/evals/runs/{id}`.
- Existing MCP tools: `eval_list_suites` · `eval_run` · `eval_list_runs` · `eval_get_run`.
- `Suite`, `Case`, `Grader`, `CaseResult`, `Mode`, `GraderType` — unchanged, existing `internal/evals` types.

**Additive (new surface):**

- Three new routes (R1/R2/R3), three new MCP tools, the `SweepRequest` / `RunSet` / `RunSetStatus` / `MatrixSpec` / `Cell` / `Delta` / `CellSummary` / `RunSetSummary` types, and the `Run.parent_run_id` field (`omitempty`).
- `Run.parent_run_id` is the **only** change to an existing type. Standalone runs serialize identically to today; sweep children carry the field set.
- `ListRuns` / `LoadRun` must skip rows where `kind == "runset"` (a one-line filter; sweep parents are views, not runs). Sweep children remain ordinary `Run` rows and continue to surface in `evals runs`, tagged by `parent_run_id`.
- No new database, no migration, no new retention policy, no new daemon flag required to enable the routes.
- Federation caps and the audit log are the *existing* mechanisms extended to the new routes — no new auth or audit machinery.

**New consumer name (behavioural, documented in the integration doc, not an API surface):**

- `Consumer:"eval"` (and the second name `Consumer:"eval_rubric"` for llm_rubric grading calls) is the one behavioural interaction with existing machinery. It is purely additive: default-all ComputeNodes accept it with no config change; pinned nodes must add it. `denied_consumers: ["eval"]` excludes a node from sweep cells explicitly.

---

## 7. Open questions (carried from companion docs)

1. **Does `*` include operator-disabled LLMs?** → **No.** Disabled is an operator signal; a disabled backend must not silently appear in a wildcard sweep. An explicit list that includes a disabled LLM is a `422`, so intent is visible and unambiguous.
2. **`RunSet` retention** → piggyback the `runs/` directory; no extra GC. The one permanent obligation: `ListRuns` / `LoadRun` must filter `kind == "runset"` forever.
3. **`llm_rubric` grading-call tokens/cost** → the grading call **is** part of the cell; its `TokensIn/Out` and cost are folded into the cell's totals (so the rubric call is part of the cell's measurable cost, per the metric-semantics table in §2.3).
4. **Session-backend kinds** → excluded by design. A "grade an agent against a suite" feature, if wanted, ships as a separate session-prodded runner and never folds into this `RunSet`.
5. **Whole-sweep timeout** → bounded per cell by `inference.ResolveTimeout(llm)` per case (existing). A whole-sweep timeout is **not** specified here; the operator sets per-LLM `eval_timeout_seconds` or `TimeoutSeconds` to cap it.

---

*End of API design document.* This document is the API-surface view of Eval Sweep — REST endpoints (table), MCP tool signatures (code blocks), and request/response structures (YAML). Behavioural contracts are authoritative in the companion specs; where this document and a companion disagree, the companion wins.
