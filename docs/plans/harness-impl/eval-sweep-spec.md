# Eval Sweep — suite × backend comparison spec

**Date:** 2026-09-27 · **Status:** Draft · **Grounding:** Proposal #1 in `[enhancement-proposals.md](../harness-research/enhancement-proposals.md)` (feasibility 5/5, impact 4/5) and Theme 1 in `[feature-themes.md](../harness-research/feature-themes.md)` ("Eval harness integration & suite × backend sweeps").

**Hard constraints honored by this document:**

- No internal tracking-number identifiers (BL/XX style) in the prose — subsystems are named descriptively (the Evals framework, the LLM registry, the compute-node registry, the cost counters, the comm channel) and every code path cited was read in the working tree on the date above.
- Markdown only: pseudocode and JSON examples, no real Go source, no new `.go` files.
- Additive-only: the standalone `Run` shape gains exactly one omitempty field and everything else on the evals surface stays byte-identical.

---

## 1. Overview + problem statement

The Evals framework (package `evals`, `internal/evals/evals.go`) implements structured, rubric-based suite scoring: `Grader` / `Case` / `Suite` / `CaseResult` / `Run` types, a `Runner` that loads suite YAMLs from the daemon data dir, executes them through `Grade()`, and persists one JSON file per run under the runs directory (`internal/evals/evals.go:105-213`). The REST surface (`internal/server/evals.go`) exposes five routes; the MCP toolset (`internal/mcp/evals.go`) exposes four tools, all proxying that REST; the CLI family lives in `cmd/datawatch/cli_evals.go`; the comm channel joins the `evals` verb family in `internal/router/commands.go:237-241` (verb declared at `internal/router/commands.go:241`, parsed at `internal/router/commands.go:1346-1351`).

The problem, quoted from Proposal #1 in the research doc:

> `eval_run` executes one suite against a single backend and emits a single pass/fail. Choosing between LLMs/ComputeNodes (or comparing a before/after prompt) requires hand-running the suite once per backend and eyeballing two `Run` rows — no side-by-side, no aggregate comparison.

Theme 1 states the user needs in `feature-themes.md:11-15`:

- "Run the same behavioral suite against multiple LLMs / ComputeNodes in one invocation, with a side-by-side (pass rate, latency, tokens, cost) comparison — the promptfoo / lm-evaluation-harness UX."
- "Model-upgrade and model-selection decisions answered by data, not by hand-running the suite per backend and eyeballing two `Run` rows."

And its DataWatch-value bullets (`feature-themes.md:16-18`):

- "Cheapest, largest-gap win in the set: … YAML suites, threshold gates, per-case results … a `sweep` verb (suite × backend matrix, `parent_run_id` children) … reuses ~all existing machinery."
- "Closes the one category DataWatch is missing vs. catalog peers, and makes the eval verdict a first-class citizen of the PRD pipeline alongside guardrails."

This spec defines that sweep: a one-command **suite × backend** fan-out whose per-cell results are persisted exactly like standalone runs, aggregated under a new `RunSet` parent, and rendered as a normalized comparison table on every operator surface.

---

## 2. Design goals

| ID | Requirement | Priority |
|----|-------------|----------|
| G-01 | One-command suite × backend fan-out: a single sweep invocation on any surface (REST, MCP, CLI, comm channel) executes the suite against an explicit list of LLM registry entries or a wildcard over the enabled inference-kind entries | P0 |
| G-02 | Side-by-side comparison table with **normalized** metrics per cell: `pass_rate` vs the suite threshold, `pass` (per-cell threshold check), `latency_ms`, `tokens_in`, `tokens_out`, `cost_usd` | P0 |
| G-03 | Additive-only backward compatibility: `Run` gains exactly one omitempty field (`parent_run_id`); a standalone `Run` JSON without that field deserializes identically to today, and all four existing MCP tools + five existing REST routes stay byte-identical | P0 |
| G-04 | Bounded parallelism: cells execute concurrently under a config-capped worker pool (`evals.max_parallel`, default 2); a single cell failing or timing out must never abort its siblings | P0 |
| G-05 | Deterministic settlement: the `RunSet` settles when all cells complete or the resolve timeout elapses; any failed/timed-out cell settles the `RunSet` as `incomplete` rather than `settled` | P0 |
| G-06 | Reuse, not fork: per-cell execution reuses the existing per-suite executor verbatim through the existing runner interface; the sweep adds a loop, a new parent object, and matrix expansion — no new execution engine | P1 |
| G-07 | Cost comparability: per-cell `cost_usd` is derived through the existing cost counters / rate table so a cell's cost only appears when the inference path reports usage (prerequisite tracked in §9 P1) | P1 |
| G-08 | Federation-safe: every new REST route carries an explicit capability gate reusing the existing autonomous capability pattern | P1 |
| G-09 | Config is additive with safe defaults: an existing daemon config file that has no `evals:` section produces unchanged behavior everywhere | P1 |
| G-10 | Parity surface complete: REST, MCP, CLI, comm channel, YAML/config, PWA, and — where required — Android / iPhone parity all named with a status for each (§7) | P1 |

Out of scope (explicitly deferred):

- Per-case cross-backend joins (which *case* one backend failed that another passed) — derivable by aligning case names across the persisted child runs; no dedicated structure.
- Rubric-based (LLM-judged) cell scoring — the `llm_rubric` grader is a stub today (§10).
- Trend / drift analysis over sweep history — a follow-on read-side feature (Theme 2 territory).
- Eval nodes inside the DAG orchestrator — a separate proposal (#2 in the research doc); this sweep is its prerequisite data source.

---

## 3. Data structures

The existing types — `Grader`, `Case`, `Suite`, `CaseResult`, `Run`, `Mode`, `GraderType` — are **unchanged** (`internal/evals/evals.go:38-103`). Below are the one modified shape and the new shapes, in markdown pseudocode (JSON wire names shown, not Go source).

### 3.1 Modified: `Run` — one additive field

```
Run (existing fields unchanged — id, suite, mode, started_at, finished_at,
     pass_rate, pass, threshold, results)
+  parent_run_id : string   // omitempty; empty = standalone run (today's behavior)
                    non-empty = the RunSet id this cell belongs to
```

Wire compatibility: `parent_run_id` is omitted from JSON when empty (`omitempty` convention), so a standalone run serializes **byte-identically** to today, and a legacy run JSON file without the key still deserializes with `parent_run_id == ""`. No new store, no new directory, no new file format: children persist to the same `<data-dir>/evals/runs/<id>.json` the runner already uses (`internal/evals/evals.go:203-213`). The only reader-side obligation: `ListRuns` (`internal/evals/evals.go:231-260`) must skip files whose root object carries `kind == "runset"` — a one-line filter, since `RunSet` files live in the same directory (see §3.3).

### 3.2 New: `RunSet`

```
RunSet {
  id            : string            // new UUID; distinct from every child Run id
  kind          : "runset"          // literal discriminator — lets ListRuns skip these files
  suite         : string            // suite name that was swept
  threshold     : float64           // snapshot of the suite's pass_threshold at sweep time
  cells         : [ Cell ]          // one entry per matrix cell, in request order
  created_at    : string            // RFC3339 UTC, matrix settled and execution started
  settled_at    : string            // RFC3339 UTC, all cells done or resolve timeout elapsed
  status        : "pending" | "settled" | "incomplete"
}
```

`status` semantics:

- **pending** — assigned between acceptance and the first cell start (transient; may not survive to disk).
- **settled** — every cell completed and produced a pass/fail verdict.
- **incomplete** — at least one cell failed, timed out, or errored before producing a result. The `RunSet` is still fully readable; failed cells carry `error` and zero-valued metrics, and the operator must not read the aggregate as a fair comparison.

Persistence: one JSON file per `RunSet`, alongside the child runs in the runs directory, written atomically at settlement (rename-over-temp) so readers never observe a half-written matrix.

### 3.3 New: `Cell`

```
Cell {
  run_id      : string      // id of the child Run persisted for this cell
  llm_name    : string      // LLM registry entry name that was evaluated
  model       : string      // optional per-cell model override ("" = the LLM's own default)
  compute_node: string      // optional ComputeNode that served the cell ("" = cloud kind / unknown)
  pass        : bool        // cell pass_rate >= the RunSet threshold (per-cell threshold check)
  pass_rate   : float64     // 0.0..1.0, identical definition to a standalone Run.pass_rate
  threshold   : float64     // denormalized per cell so rows are self-describing
  latency_ms  : int64       // total inference duration across the cell's cases
  tokens_in   : int         // total prompt tokens; 0 when the backend did not report usage
  tokens_out  : int         // total completion tokens; 0 when not reported
  cost_usd    : float64     // from the cost counters at this cell's rate row; 0 when unknown
  error       : string      // only for cells that failed before producing a Run
}
```

### 3.4 Comparison / normalized metric block

The read-side comparison table is the `cells[]` array rendered with one row per cell. The normalized per-cell metric set, with one definition regardless of backend kind:

| Column | Definition | Source |
|--------|------------|--------|
| `pass / pass_rate` | `count(passing cases) / len(suite.cases)` — identical to a standalone run's `pass_rate` for the same suite; `pass` is `pass_rate >= threshold` (the *same* suite threshold for every cell, so pass/fail is comparable across backends) | per-cell executor (existing `Runner.Execute`) |
| `latency_ms` | wall-time of the cell's execution (Σ per-case inference durations once the inference path reports them; until then, cell wall-clock) | the sweep loop's per-cell timer |
| `tokens_in` / `tokens_out` | Σ reported token usage across the cell's inference calls; `0` when the adapter does not report usage (see prerequisite below) | the inference path → cost counters |
| `cost_usd` | rate-table lookup for the cell's backend × (tokens_in, tokens_out), summed over cases; `0` for local kinds by default (the default rate table assigns them $/1K of 0, `internal/session/cost.go:31-46`) and `0` whenever tokens are unreported | the cost counters (`internal/session/cost.go`) |

**Deliberate non-goals kept out of the table:**

- Token *efficiency* (pass rate per dollar) — derivable by the operator from `pass_rate` and `cost_usd`.
- Per-case cross-backend joins — already available by aligning case names across the child runs.

#### Prerequisite: per-cell cost requires usage report

The current inference `Response` carries `Text`, `UsedNode`, `UsedModel`, `Backend`, `DurationMs` (`internal/inference/dispatcher.go:53-59`) — **no token counts** — and the evals grader loop grades each case in-process with no LLM inference at all (only the `llm_rubric` branch is even nominally LLM-judged, and it is a fixed stub, `internal/evals/evals.go:279-286`). Consequently, until the inference path is taught to report per-call token usage (tracked as P1 in §9):

- `tokens_in` / `tokens_out` will be **zero** for every cell, and `cost_usd` will be **zero** for local-kind backends (their default rate rows are $0 anyway, `internal/session/cost.go:31-46`).
- `latency_ms` falls back to the cell's wall-clock execution time, which is still a comparable column.
- The cost table renders the zero values honestly (`"tokens_in":0, "cost_usd":0`), never a fabricated estimate.

Once P1 lands (each adapter records usage into the request-scoped cost counters via the same helper that `cost_usage` / `cost_summary` already use, `internal/mcp/sx_parity.go:357-381`), the sweep's settlement step reads those counters back into the cell. No schema change is needed — the `Cell` shape above already carries the fields.

---

## 4. Data flow

End-to-end, in the order the operator experiences it:

1. **Operator invokes the sweep.** Any of the surfaces in §6 (`POST /api/evals/sweep`, `eval_sweep`, `datawatch evals sweep ...`, `evals sweep <suite> [backends]`, or the PWA panel) submits: a `suite` (required), an optional `backends` list, an optional `models` list (per-cell override), and optional `max_parallel` / `resolve_timeout` overrides.

2. **Matrix expansion.** The planner resolves the backend list:
   - **Explicit list** → use as-is, in order. Unknown names are a plan-time 400 (listed by name); duplicate names are collapsed to one cell (first occurrence wins).
   - **Wildcard / omitted** → expand over the LLM registry (`Registry.List()`, `internal/inference/llm.go`), keeping entries whose `Kind` is an inference kind (per the kind taxonomy at `internal/inference/llm.go:44-80`) **and** whose `Enabled()` is true (`internal/inference/llm.go`), excluding the session-backend kinds that the dispatcher classifies as having no inference adapter (`internal/inference/dispatcher.go:265-274`). Zero enabled inference-kind entries → plan-time 400.
   - **models list** (optional): one model → applied to every cell; N models → paired positionally with N backends; any other length is a plan-time 400. Empty → each LLM's own default.

   The expanded matrix is capped at `evals.max_cells` (plan-time 400 if exceeded).

3. **Per-cell execution.** For each cell, the sweep calls the **existing per-suite executor verbatim** — the same `Execute` the REST `POST /api/evals/run` path uses (`internal/server/evals.go:94`) — with the cell's model override passed to the underlying inference call (`inference.Request.ModelOverride`, `internal/inference/dispatcher.go:40-51`). Cells run in a worker pool sized by the effective `max_parallel` (request value clamped to the config cap). A cell that errors (backend down, failover exhausted, timeout) records `Cell.error` and **does not abort siblings**.

4. **Persistence, cell by cell.** Each child `Run` persists exactly like a standalone run — same directory, same one-JSON-file-per-run writer (`internal/evals/evals.go:203-213`) — with `parent_run_id` set to the RunSet id. Order: persist the child run first, then record its metrics in the cell; the sweep never holds un-persisted results.

5. **RunSet settlement.** The RunSet settles when **all cells have reported** (pass, fail, or error) or the **resolve timeout** elapses — whichever is first. A cell that timed out is finalized as `error: "resolve timeout"`; if any cell is not a clean pass/fail, the RunSet's `status` is **`incomplete`**, else **`settled`**. Settlement writes the RunSet JSON atomically (temp + rename) to the runs directory. Sibling cells are never cancelled by a sibling's failure — the timeout is the only global stop.

6. **Read-side aggregation.** `eval_get_runset` / `GET /api/evals/runsets/{id}` / `datawatch evals runset <id>` / the PWA panel render the comparison table from `cells[]`. The read side performs **no re-grading**: it only denormalizes (per-cell threshold check already stored) and sorts/orders for display. This keeps the write path (the sweep loop) and the read path (rendering) decoupled.

### Where it maps to existing code vs. what is new

| Step | Existing code (reused verbatim) | New |
|------|--------------------------------|-----|
| Per-cell execution | `Runner.Execute`, `internal/evals/evals.go:173-201`; runner interface `internal/server/evals.go:25-31`; REST `handleEvalsRun`, `internal/server/evals.go:72-101` | the **sweep loop** (worker pool + per-cell error capture) |
| Wildcard expansion | `Registry.List()`, `LLM.Enabled()`, the inference/session kind split (`internal/inference/llm.go`, `internal/inference/dispatcher.go:265-274`) | the **matrix expansion** planner (explicit/wildcard/dedupe/cap) |
| Persistence | `persistRun`, `internal/evals/evals.go:203-213`; `ListRuns` (with the one-line `kind=="runset"` skip) | **RunSet persistence** (atomic temp+rename write + settlement state machine) |
| REST | route registration pattern in `internal/server/server.go:307-311`; `fedCap` gate pattern `internal/server/evals.go:45` | 3 new routes (§6) |
| MCP | proxy pattern (`proxyGet` / `proxyJSON`) in `internal/mcp/evals.go:45-84` | 3 new tools (§6) |
| CLI | `daemonGet` / `daemonJSON` wrappers, `cmd/datawatch/cli_evals.go:35-87` | 3 new subcommands (§6) |
| Comm | the `evals` verb family, `internal/router/commands.go:237-241` + dispatch `internal/router/router.go:1136` | new verbs under the existing `evals` family (§6) |
| Cost | `internal/session/cost.go` rate table + usage helper (`internal/mcp/sx_parity.go:357-381`) | per-cell usage read-back (the P1 plumbing prerequisite, §9) |

---

## 5. Config

A new subsection sits **next to** the existing `autonomous` and `detection` subsections in the daemon config (where `internal/config/config.go:301` declares `detection` and `internal/config/config.go:380` declares `autonomous`). It is a new `evals:` section:

```yaml
evals:
  max_parallel: 2            # worker-pool size for concurrent cells (default 2)
  max_cells: 8               # hard cap on the expanded matrix size (default 8)
  default_backends: "*"      # wildcard = every enabled inference-kind LLM (default)
                             # or an explicit list, e.g. ["ollama", "claude"]
  resolve_timeout_seconds: 120  # global settlement window for a RunSet (default 120)
```

| Key | Type | Default | Meaning |
|-----|------|---------|---------|
| `max_parallel` | int | `2` | Max concurrent cells in the sweep worker pool. Request values above the cap are clamped down, never rejected. |
| `max_cells` | int | `8` | Cap on `len(matrix)` after expansion. Plan-time 400 when exceeded. |
| `default_backends` | string or list | `"*"` | Backends used when a request omits the list. `"*"` = all enabled inference-kind LLMs. |
| `resolve_timeout_seconds` | int | `120` | Seconds before a RunSet force-settles (remaining cells mark `timeout` → `status=incomplete`). |

**Additive guarantee (G-09):** every key is optional and has a safe default; the section may be absent entirely. A daemon config file with **no** `evals:` block produces byte-identical behavior on every existing route, tool, CLI subcommand, and comm verb — the new sweep surface is the only thing that reads this section, and it falls back to the defaults above. All four knobs are reachable on every config channel (CLI flag, YAML, config REST round-trip) per the Configuration Accessibility Rule.

---

## 6. API / interface design

The canonical request is one shape across all surfaces; only the encoding differs.

Sweep request (REST body / MCP args / CLI flags all map onto this):

```
{
  "suite"     : "json-output",                       // required
  "backends"  : ["ollama", "claude"],                // optional list; omitted = default_backends
  "models"    : ["llama3.1:8b", "claude-3-5-sonnet"],// optional; 1 (all) or N (positional)
  "max_parallel"    : 2,                              // optional; clamped to evals.max_parallel
  "resolve_timeout": 120                              // optional seconds; default resolve_timeout_seconds
}
```

### 6a. MCP tools

Three new tools in `internal/mcp/evals.go`, all proxying the new REST routes exactly the way the existing four tools proxy today (`proxyGet` / `proxyJSON` + `textOK`, `internal/mcp/evals.go:45-84`). Each returns the body as MCP text content.

#### `eval_sweep`

| Param | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `suite` | string | **yes** | — | Suite name (matches `<data-dir>/evals/<name>.yaml`) |
| `backends` | string (CSV) | no | `default_backends` / `"*"` | Comma-separated LLM registry names; `"*"` = every enabled inference-kind LLM |
| `models` | string (CSV) | no | each LLM's default | Per-cell model override: 1 value = all cells, N values = positional |
| `max_parallel` | string (int) | no | `evals.max_parallel` | Worker-pool size; clamped to the config cap |
| `resolve_timeout` | string (int) | no | `evals.resolve_timeout_seconds` | Settlement window in seconds |

Example request → response (response is the full `RunSet`, §3.2):

```json
{ "suite": "json-output", "backends": "ollama,claude", "models": "llama3.1:8b,claude-3-5-sonnet" }
```

```json
{
  "id": "runset-9f2c4a1e",
  "kind": "runset",
  "suite": "json-output",
  "threshold": 0.7,
  "created_at": "2026-09-27T08:00:00Z",
  "settled_at": "2026-09-27T08:00:09Z",
  "status": "settled",
  "cells": [
    { "run_id": "11ab1c2d", "llm_name": "ollama", "model": "llama3.1:8b",
      "compute_node": "local-ollama", "pass": false, "pass_rate": 0.58, "threshold": 0.7,
      "latency_ms": 4120, "tokens_in": 0, "tokens_out": 0, "cost_usd": 0.0 },
    { "run_id": "7e5f6a7b", "llm_name": "claude", "model": "claude-3-5-sonnet",
      "pass": true, "pass_rate": 0.92, "threshold": 0.7,
      "latency_ms": 2310, "tokens_in": 0, "tokens_out": 0, "cost_usd": 0.0 }
  ]
}
```

*(Token and cost columns shown as 0 — see the usage-report prerequisite in §3.4; they populate once P1 plumbing lands.)*

Errors propagate from REST as `{"error":"…"}` text with the same status code the server would return — the MCP transport already surfaces non-2xx as a tool error via the daemon's proxy path.

**Existing tools — unchanged.** `eval_list_suites`, `eval_run`, `eval_list_runs`, `eval_get_run` (`internal/mcp/evals.go:18-41`) keep byte-identical signatures and bodies. `eval_run` remains the single-backend single-run verb — still correct for the Algorithm Mode Measure phase and quick self-grade loops.

#### `eval_list_runsets`

| Param | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `limit` | string (int) | no | unlimited | Max runsets to return, most recent first |

Thin projection of `GET /api/evals/runsets` — per row: id, suite, status, cell count, best pass_rate, settled_at.

```json
{ "limit": "5" }
```

```json
[
  { "id": "runset-9f2c4a1e", "suite": "json-output", "status": "settled",    "cells": 2, "best_pass_rate": 0.92, "settled_at": "2026-09-27T08:00:09Z" },
  { "id": "runset-5d81c002", "suite": "summarize",   "status": "incomplete", "cells": 3, "best_pass_rate": 0.71, "settled_at": "2026-09-26T19:12:44Z" }
]
```

#### `eval_get_runset`

| Param | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `id` | string | **yes** | — | RunSet id (a child Run id here is a 404 — the two are distinguished by the `kind` field) |

Returns the full `RunSet` object (identical to the `eval_sweep` response above).


### 6b. REST endpoints

Registered alongside the existing `/api/evals*` routes (`internal/server/server.go:307-311`). The existing five routes (`/api/evals`, `/api/evals/suites`, `/api/evals/run`, `/api/evals/runs`, `/api/evals/runs/{id}`) remain **byte-identical**. All new routes return `503 {"error":"evals disabled"}` when no runner is wired — the exact pattern already used by the existing handlers (`internal/server/evals.go:37-39`).

| Method | Path | Capability gate (federation) | Purpose |
|--------|------|------------------------------|---------|
| POST | `/api/evals/sweep` | `autonomous:run` (same pattern as `handleEvalsRun`, `internal/server/evals.go:81`) | Execute a suite against the backend matrix; 201 + settled `RunSet`; 400 unknown suite / bad matrix / models-length mismatch; 503 runner not wired |
| GET | `/api/evals/runsets` | `autonomous:list` | List past runsets, most recent first (`?limit=N` optional) |
| GET | `/api/evals/runsets/{id}` | `autonomous:read` | Fetch one runset by id; 404 on unknown id (including a child-Run id) |

Capability gates reuse the autonomous capability constants already used by the evals routes (`internal/federation/capabilities.go:68-71`).

`POST /api/evals/sweep` — request body = the sweep request from §6; response `201`:

```json
{
  "id": "runset-9f2c4a1e",
  "kind": "runset",
  "suite": "json-output",
  "threshold": 0.7,
  "created_at": "2026-09-27T08:00:00Z",
  "settled_at": "2026-09-27T08:00:09Z",
  "status": "settled",
  "cells": [
    { "run_id": "7e5f6a7b", "llm_name": "claude", "model": "claude-3-5-sonnet",
      "pass": true, "pass_rate": 0.92, "threshold": 0.7,
      "latency_ms": 2310, "tokens_in": 0, "tokens_out": 0, "cost_usd": 0.0 }
  ]
}
```

An audit entry is written like the existing `evals_run` action (`internal/server/evals.go:99`, `auditEvals` at `internal/server/evals.go:183-196`), with action `evals_sweep` and `resource_type: eval_runset`.

### 6c. CLI

Three new subcommands in the existing `evals` family (`cmd/datawatch/cli_evals.go`), thin `daemonJSON` / `daemonGet` wrappers of §6b:

```
datawatch evals sweep <suite> [--backends a,b] [--models m1,m2] [--max-parallel N] [--timeout seconds]
datawatch evals runsets [--limit N]
datawatch evals runset <id>
```

`sweep` renders the `cells[]` array as an aligned comparison table (columns: backend, model, node, pass, pass_rate, threshold, latency_ms, tokens, cost_usd) rather than raw JSON — the JSON shape remains available with the daemon's existing raw-output flag. `runsets` lists rows; `runset <id>` prints the table for one runset. Errors map 1:1 from the REST status codes (400 → exit non-zero with the server error text, same as `evals run` today).

### 6d. Comm channel

Joins the existing `evals` verb family — the verb is already parsed in the router's command table (declared at `internal/router/commands.go:237-241`, parsed at `internal/router/commands.go:1346-1351`, dispatched at `internal/router/router.go:1136`) and handled by the existing `evals` handler file next to it. New verbs, same encoding style as the rest of the family (plain text tokens):

```
evals sweep <suite> [backend1,backend2,...]   → execute; return the comparison table as text
evals runsets [N]                             → list the last N runsets (default 10)
evals runset <id>                             → one runset's table as text
```

The handler calls the same REST routes over the daemon's internal HTTP path — no second REST client is introduced; the table renderer is shared with the CLI so a single-backend sweep prints identical rows on both surfaces (smoke test, §8).

### 6e. YAML / config

The `evals:` subsection from §5 (max_parallel, max_cells, default_backends, resolve_timeout_seconds) is reachable through:

- The daemon config file (`internal/config`, next to the `detection` section declared at `internal/config/config.go:301` and the `autonomous` section at `internal/config/config.go:380`),
- `GET`/`PUT /api/config` round-trip — no new endpoint; the new section is a field on the existing config object, so the existing config round-trip surfaces it automatically.

MCP exposure: **decision logged** — the sweep's own MCP tools (6a) are the operator surface for running sweeps, and the config is already settable through the existing config channel; adding a dedicated autonomous-config-style MCP tool for *this* section is **out of scope** (rationale: the autonomous-config tool exists because the autonomous loop is daemon-wide runtime state; the sweep knobs are static defaults that the request itself can always override, so a dedicated MCP setter would duplicate the config round-trip with no new capability).

### 6f. Operator clients (PWA and mobile)

- **PWA:** a read-only **Evals panel** section: a list of runsets (from `GET /api/evals/runsets`) and, on selection, the comparison table (from `GET /api/evals/runsets/{id}`) — plus a single "sweep" affordance (suite + backends) that POSTs to `/api/evals/sweep`. No new data model on the client; it renders the wire shape from §3.
- **Android / iPhone-iOS:** the **Mobile-Parity Rule** (AGENT.md) requires either native parity or a documented, reason-logged exclusion. Decision: the sweep panel is **PWA-first in this spec**; the native Android and iOS clients consume the exact same three REST routes (platform-neutral per the rule's API note), so the parity path is the standard "mobile clients parse the same endpoints" path rather than a feature-by-feature port. A native-parity issue entry for the Evals sweep panel is the required tracked follow-up; this spec's status row for Android / iPhone is therefore **shipped-in-spec (REST-backed), client work tracked under the parity rule** — not an exclusion.

### 6g. Federation

The three new REST routes each carry an explicit capability gate as in §6b (`autonomous:run` / `autonomous:list` / `autonomous:read` — the same autonomous capability trio the existing evals routes already consume, `internal/federation/capabilities.go:68-71`). Any peer granted `autonomous:run` can therefore invoke a sweep; read-only peers (`autonomous:read`/`list`) can enumerate and inspect runsets. No new capability string is introduced — reusing the existing trio is the deliberate choice, matching how `POST /api/evals/run` is already gated.

---

## 7. Parity surface

Required by the parity rule in AGENT.md (the full surface set: REST, MCP, CLI, comm channel, YAML/config, PWA, Android, iPhone/iOS); the rows below extend it to the documentation trio this feature also touches. This section is what Phase 0 of the roadmap gates on.

| Surface | Status |
|---------|--------|
| REST | shipped-in-spec — 3 new routes, capability-gated, §6b |
| MCP | shipped-in-spec — `eval_sweep`, `eval_list_runsets`, `eval_get_runset`, §6a; existing 4 tools unchanged |
| CLI | shipped-in-spec — `evals sweep`, `evals runsets`, `evals runset`, §6c |
| Comm channel | shipped-in-spec — `evals sweep / runsets / runset` verbs, §6d |
| YAML/config | shipped-in-spec — additive `evals:` section with safe defaults, §5; config round-trip surfaces it |
| PWA | shipped-in-spec — read-only Evals panel (runsets + table + sweep affordance), §6f |
| Android | shipped-in-spec via the platform-neutral REST routes; native panel work tracked under the Mobile-Parity Rule (not an exclusion — §6f) |
| iPhone/iOS | shipped-in-spec via the platform-neutral REST routes; native panel work tracked under the Mobile-Parity Rule (not an exclusion — §6f) |
| Documentation | shipped-in-spec — this file; the docs suite gains a "how to compare backends on a suite" section and the howto for running evals (`docs/howto/evals.md`) gains the sweep steps; the definitions file (`docs/datawatch-definitions.md`) gains RunSet/Cell entries in the implementation phase |
| MCP-API mapping | shipped-in-spec — `docs/api-mcp-mapping.md` updated with the three new tools → routes mapping in the implementation phase |
| Howtos (docs_search corpus) | shipped-in-spec — `docs/howto/evals.md` extended with `eval_sweep` exec steps so the howto-driven surface can drive a sweep |

No excluded surfaces. Every surface either ships in the spec or is an explicit non-goal listed in §2 with a rationale.

---

## 8. Test plan

### 8.1 Unit

1. **Matrix expansion** — a table-driven test against the planner:
   - explicit list → cells in request order;
   - wildcard / omitted → only enabled inference-kind LLMs (assert session-backend kinds are excluded by the kind predicate, `internal/inference/dispatcher.go:265-274`);
   - duplicate names → collapsed to one cell;
   - unknown backend name → plan error enumerating the offending names;
   - `models` length 0 / 1 / N / mismatch → accept / accept / accept / plan error;
   - matrix exceeding `max_cells` → plan error.
2. **RunSet settlement logic** — with a stubbed per-cell executor:
   - all cells pass → `status: settled`;
   - one cell fails (executor returns error) → `status: incomplete`, failed cell has `error` set, siblings completed;
   - one cell exceeds the resolve timeout → `status: incomplete`, timed-out cell `error: "resolve timeout"`, siblings untouched;
   - two concurrent failures → still `incomplete`, both errors recorded;
   - settlement writes exactly one RunSet file and N child Run files.
3. **Backward compatibility** — a fixture `Run` JSON without `parent_run_id` deserializes to an identical struct (all fields equal) and re-serializes byte-identically (this is the additive-only regression check for §2/G-
03) — the existing `LoadRun` path, `internal/evals/evals.go:216-227`, must not change behavior for such files;
   - a Run JSON **with** `parent_run_id` set loads through the same path and carries it.
4. **Per-cell threshold comparison** — cell with `pass_rate == threshold` is `pass: true` (>=, matching `Runner.Execute`'s `run.Pass = run.PassRate >= run.Threshold`, `internal/evals/evals.go:196`); cell one epsilon below is `pass: false`; the threshold is the same value in every cell of the RunSet (snapshot at plan time).
5. **Cost read-back** — with stub usage counters, `cost_usd == rate table lookup × tokens` for a cloud kind and `0` for a local kind (rate rows at `internal/session/cost.go:31-46`); zero tokens → zero cost.

### 8.2 Integration

1. A sweep against N backends executes N cells **in parallel** (assert wall-time ≈ max-of-cells, not sum-of-cells, with `max_parallel >= N` and a deliberately slow stubbed backend — or assert the pool size via the effective value on the RunSet); all N child runs persist with `parent_run_id == <runset-id>` and are individually loadable through the existing single-run route.
2. When the inference path reports usage, each cell's `tokens_in`/`tokens_out`/`cost_usd` are populated from the cost counters and sum-check against the recorded per-call usage.
3. **Field-for-field identity**: `eval_sweep` (MCP) and `POST /api/
evals/sweep` return RunSet objects that are identical field-for-field (same id, same cells, same metric values) — the MCP tool must be a pure proxy, not a re-run.
4. `ListRuns` after a sweep skips the RunSet file (`kind == "runset"`) but still lists the N child runs; `GET /api/evals/runsets` lists the runset row.
5. The existing suite listing, run execution, run listing, and single-run routes behave byte-identically after the sweep code is in place (additive-only regression gate).

### 8.3 Smoke (manual / scripted)

1. **Single-backend sweep** renders **identical rows** on the CLI table and the PWA panel (same columns, same values, same pass/fail) — proves the shared renderer.
2. **Two-backend sweep with one backend unreachable** (point one LLM at a dead address): the sweep settles `incomplete` within `resolve_timeout_seconds`, the reachable cell is fully populated, the dead cell carries `error`, and no worker is left hanging.
3. **Additive-only regression**: `eval_run` / `eval_list_suites` / `eval_list_runs` / `eval_get_run` and the five existing routes are unchanged — run the existing evals test file (`internal/evals/evals_test.go`) against a tree with the sweep merged; all green, no test edited.
EOF339
echo done; wc -l docs/plans/harness-impl/eval-sweep-spec.md
