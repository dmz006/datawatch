# Eval Sweep — Technical Specification

**Date:** 2026-09-14 · **Status:** Draft (v1.0)
**Scope:** Evaluation matrix · Suite integration · Backend compatibility
**Companions:** `eval-sweep-spec.md` (external API & data structures) · `eval-backend-integration.md` (backend wiring) · `enhancement-proposals.md` §1 (behaviour rationale)
**Source of truth:** this document supersedes the three companion docs where they conflict; it is the single technical contract an implementer builds against.

---

## 0. Overview

### 0.1 Problem

`eval_run` (BL259) executes one eval suite against one backend and returns one
`Run`. To compare models — pick a provider, validate a model upgrade, or prove a
new prompt is not a regression — an operator must hand-run the same suite once
per backend and eyeball multiple `Run` rows. There is no side-by-side, no
normalized metric set, and no aggregate "which one is best / cheapest / fastest"
verdict.

### 0.2 Feature

Add a **sweep** verb to the evals surface: a single request fans a suite out
against a **matrix of LLM backends** (optionally with a per-cell model
override), runs every cell in parallel under the existing max-parallel
plumbing, records each cell as a child `Run`, and returns a **`RunSet`** — a
normalized, comparable, delta-annotated table spanning every cell.

### 0.3 The three pillars

This spec is organized around the three pillars requested, each self-contained
and traceable to existing code:

| Pillar | Question it answers | Primary section |
|---|---|---|
| **Evaluation matrix** | *what* is being run — the suite × backend(×model) grid, cell model, fan-out, aggregation, persistence | §1, §2 |
| **Suite integration** | *how* the existing `internal/evals` framework (suites, cases, graders, grading) plugs into the sweep, and how graders behave against live backend responses | §3 |
| **Backend compatibility** | *against what* the sweep runs — the inference LLM registry, per-kind adapter support, token/cost normalization, ComputeNode RBAC, config | §4 |

Follow-on sections cover the API surface (§5), the full data-structure catalog
(§6), implementation sequencing (§7), verification (§8), compatibility
guarantees (§9), and open questions (§10).

### 0.4 What this spec is *not*

- **Not a code change.** It defines the contract; implementation is tracked in §7 (P0–P3).
- **Not a new eval framework.** It reuses `Suite`/`Case`/`Grader`/`CaseResult` and `Grade` byte-for-byte.
- **Not session-backend coverage.** Coding-agent kinds (`claude-code`, `aider`, `goose`, …) are *excluded* by design — see §4.6. If "evaluate an agent under test" is wanted, that is a separate, future story (session-prodded runner).
- **Not a store change.** Children and the `RunSet` parent reuse `~/.datawatch/evals/runs/`; no new database, no migration.

### 0.5 Design goals & requirements

| ID | Requirement | Priority |
|---|---|---|
| G1 | One request, N backends, single call | Must |
| G2 | Apples-to-apples normalized metrics (pass_rate, latency, tokens, cost) | Must |
| G3 | Per-cell independence: one backend down ≠ whole sweep failed | Must |
| G4 | Reuse existing LLM registry + dispatcher; no new backend plumbing | Must |
| G5 | Additive only: existing REST routes / MCP tools / `Run` shape unchanged | Must |
| G6 | Cross-backend cost via BL6 so SaaS-vs-local is comparable | Should |
| G7 | `*` wildcard = "every enabled inference kind" | Should |
| G8 | Delta-vs-reference row + best-cell summary | Should |
| G9 | Cell timeout bounded via existing `ResolveTimeout` | Should |
| G10 | `RunSet` retention piggybacks existing run GC; no new store | Could |

### 0.6 Conventions

- **Auth:** Bearer token on all new `/api/evals/*` routes, as with the existing five.
- **Federation caps:** per-endpoint, reusing the `federation.CapAutonomous*` set.
- **JSON wire names** are `snake_case`; Go types shown alongside.
- **Timestamps** are RFC 3339 UTC.
- **Errors** use the daemon's `{"error":"<msg>"}` shape.
- **Additive:** nothing that a v6 consumer already reads is removed or reshaped. Non-2xx statuses and message text are new surface only.

---

## 1. Pillar 1 — Evaluation Matrix

### 1.1 Definition

A **matrix** is the set of `(backend, model)` pairs that a sweep will execute
against a *single* eval suite. Formally:

```
matrix = suite × backends × model_overrides
```

where `backends` is a list of LLM registry names (inferred or wildcard-expanded)
and `model_overrides` is an optional per-cell model pin. The result is a
flat list of **cells**, each of which is one independent evaluation run.

A **cell** is the atomic unit of a sweep. One cell = one `inference.Dispatcher.Call`
per suite case + one `Grade` per case, wrapped in a child `Run`.

### 1.2 Matrix spec (the canonical input shape)

```go
// In internal/evals (new).
type MatrixSpec struct {
    Suite   string   // suite name (filename stem under ~/.datawatch/evals/)
    Backends []string  // resolved LLM names, post-expansion, post-filter
    Models   []string  // per-cell model overrides (same length as Backends)
    MaxParallel int    // effective worker-pool size
}
```

`MatrixSpec` is **immutable** once produced by the planner and attached to the
parent `RunSet`. It is the single source of truth for what was actually
executed — the operator can always re-derive the cells from it.

### 1.3 Expansion rules

The planner applies, in order, on the raw `SweepRequest`:

1. **Load suite** — `Runner.LoadSuite(name)`. Fail-fast `404` if not found.
2. **Wildcard expansion** — the sentinel `backends: ["*"]` means:
   - every LLM in `inference.Registry.List()` that is
   - (a) *not* a session-backend kind (`inference.IsSessionBackendKind(kind) == false`),
   - (b) enabled (`llm.Disabled == false`), and
   - (c) has at least one registered adapter for its kind.
   An *explicit* backend list bypasses the wildcard — each name is resolved
   individually; unknown names are a `400`.
3. **Disabled filter** — if an explicit list contains a disabled LLM, it is
   *rejected* with `422` listing the offending name (disabled is an operator
   signal, not a silent skip).
4. **Session-backend rejection** — any name whose kind is session-backend
   (`claude-code`, `aider`, `goose`, `gemini`, `shell`, `opencode-acp`,
   `opencode-prompt`) is a `422` at plan time (not at cell time). The error
   lists all offending names so the operator can fix the request in one
   shot instead of failing the first cell.
5. **Model override pairing**:
   - `len(models) == 0` → each cell uses the LLM's own `Model`/`Models`
   - `len(models) == 1` → broadcast the single model to every cell
   - `len(models) == len(backends)` → positionally pair
   - Any other length → `400` (`models length N does not match 0, 1, or backends length M`)
6. **Parallelism** — `max_parallel` is clamped to `evals.max_parallel`
   (default 3). Values above the cap are silently lowered, never rejected;
   `RunSet.MaxParallel` records the applied value for audit.
7. **Cell count cap** — if the resulting cell count exceeds
   `evals.max_cells` (default 64), the request is `400`
   (`matrix size N exceeds evals.max_cells M`). DoS guard.

After all seven rules, the planner emits a `MatrixSpec`. If step 2 yields zero
backends (every inference-kind LLM disabled), the request is `422`:
`no enabled inference-kind LLMs to sweep against`.

### 1.4 Cell model

Each cell is described by the following fields (the full shape is in §6.6
`Cell`):

| Field | Source | Notes |
|---|---|---|
| `backend` | matrix | LLM registry name |
| `model` | matrix (override) or LLM default | Empty = LLM's `Model`/`Models` |
| `node` | `Response.UsedNode` | Empty for SaaS or if cell failed pre-node-resolution |
| `run_id` | child `Run` | Empty if cell failed before producing a `Run` |
| `pass` / `pass_rate` | child `Run` | vs suite threshold |
| `threshold` | suite | denormalized per cell |
| `latency_ms` | Σ `Response.DurationMs` | per case |
| `tokens_in` / `tokens_out` | Σ `Response.TokensIn/Out` | 0 when adapter did not report |
| `cost_usd` | `session.EstimateCost` | local kinds default $0 |
| `error` | cell-level | set only on cell failure |
| `case_results` | child `Run.Results` | shape = existing `CaseResult` |

### 1.5 Fan-out & scheduling

**Concurrency.** Cells run under a worker-pool sized `min(requested
max_parallel, evals.max_parallel)` (the planner clamps). The pool is
independent of `autonomous.max_parallel_tasks` and
`orchestrator.max_parallel_prds`; each has its own worker pool for its own
runners.

**Independence.** A cell failure (backend down, timeout, RBAC denial,
malformed grader output) records the failure in the cell and **does not
abort siblings**. `RunSet.Incomplete` is set to `true` when one or more
cells failed; individual cells carry their own `Error` string.

**Ordering.** `Cells` in the response preserve request order (the order the
backends appear in `MatrixSpec.Backends`), not pass_rate order. This makes
the reference cell unambiguous — see §1.6.

**Isolation.** Cells do not share adapter state. Each cell calls
`Dispatcher.Call` directly; the dispatcher resolves the LLM row, walks
ComputeNodes, and dispatches to the kind adapter. No cross-cell cache, no
shared connection pool — failover is per-LLM, not per-cell.

### 1.6 Aggregation

The `RunSet` is a **view over its children** — read-side aggregation
computed after all cells settle. No separate collection path, no event
stream.

| Aggregate | Definition | Source |
|---|---|---|
| `cells[i]` | per-cell metrics | child `Run` + `Response` |
| `deltas[i]` | `cell[i].metric − cell[0].metric` for each metric column | read-side |
| `best` | argmax `pass_rate`; tie-break min `cost_usd`; tie-break min `latency_ms` | read-side |
| `reference_cell_index` | 0 (first backend in the request) | invariant |

**Delta sign convention.**

- `pass_rate_delta > 0` → cell outperforms reference on pass_rate.
- `latency_delta_ms > 0` → cell is slower than reference (worse).
- `cost_delta_usd > 0` → cell is more expensive than reference (worse).

`Deltas[0]` is the zero value (the reference's delta against itself) and is
not semantically meaningful; consumers should skip index 0.

**Best summary** is a `CellSummary` (§6.8) populated from the winning cell.
While `status != complete`, `best` is nil.

### 1.7 Persistence

- **Children** persist to `~/.datawatch/evals/runs/<child_run_id>.json` —
  the same layout as a standalone `Run`. They carry `parent_run_id =
  <runset_id>` and show up in `evals runs` (filtered by `suites`, as today).
- **Parent** (`RunSet`) persists to `~/.datawatch/evals/runs/<runset_id>.json`
  with a `kind: "runset"` discriminator. `ListRuns` and `LoadRun` must skip
  rows where `kind == "runset"` (they are views, not runs). One-line filter;
  the children themselves remain ordinary `Run` rows.
- **Retention**: the `RunSet` is a child of the same `runs/` directory; it
  ages out with the same GC as standalone runs. No new retention policy.

### 1.8 Lifecycle & status

```
        ┌───────────────────────────────────────────────┐
        │  Plan (Stage 1)                               │
        │  LoadSuite → expand matrix → validate         │
        │  Create RunSet row (status=pending)           │
        └───────────────────────────────────────────────┘
                                │
                                ▼
        ┌───────────────────────────────────────────────┐
        │  Fan-out (Stage 2)                            │
        │  RunSet.status = running                      │
        │  For each cell (≤ max_parallel concurrent):   │
        │    create child Run (parent_run_id = runset)  │
        │    Stage 3: per-cell inference + grade        │
        │    persist child Run                          │
        └───────────────────────────────────────────────┘
                                │
                                ▼
        ┌───────────────────────────────────────────────┐
        │  Settle (Stage 4)                             │
        │  All cells terminal (success OR error)        │
        │  Compute deltas, best                         │
        │  RunSet.status = complete                     │
        │  RunSet.Incomplete = (any cell.Error != "")   │
        │  persist RunSet row                           │
        └───────────────────────────────────────────────┘
```

**Status transitions:**

- `pending → running` — first cell scheduled.
- `running → complete` — all cells terminal. *Even if some failed.* The
  operator decides from `incomplete` + `cell.error` what the table means.
- `running → failed` — **only** for plan-time errors where no cell could
  start (unknown suite, malformed matrix). After fan-out begins, status
  settles to `complete` with `incomplete=true`. There is no
  `completed-with-errors` — "complete" means "settled"; `incomplete`
  carries the error flag.

**Terminal states:** `complete`, `failed`. `pending` and `running` are
transient. A `failed` RunSet has an `error` on the `RunSet` itself (not a
cell) and `cells == []`.

---

## 2. Data flow (four stages)

```
 sweep request
      │
      ▼
 ┌─────────────────────────────────────────────────────────────┐
 │ Stage 1 — parse & matrix expansion          (SweepPlanner)   │
 │   load suite once (404 if unknown)                              │
 │   resolve backends vs inference.Registry (wildcard/explicit)   │
 │   apply model overrides (0/1/N rule)                           │
 │   clamp max_parallel; enforce max_cells                        │
 │   emit MatrixSpec; create RunSet(status=pending)              │
 └─────────────────────────────────────────────────────────────┘
      │
      ▼
 ┌─────────────────────────────────────────────────────────────┐
 │ Stage 2 — fan-out & scheduling        (SweepRunner)          │
 │   RunSet.status=running                                             │
 │   spawn worker-pool (size = effective max_parallel)             │
 │   for each cell: new child Run, parent_run_id=<runset>          │
 │   independent cells — one failure does not abort siblings       │
 └─────────────────────────────────────────────────────────────┘
      │
      ▼
 ┌─────────────────────────────────────────────────────────────┐
 │ Stage 3 — per-backend runner             (cellRunner)         │
 │   for each case in suite.Cases:                                    │
 │     inference.Dispatcher.Call(llm, {Prompt, SystemPrompt,        │
 │         ModelOverride, Consumer:"eval"})                          │
 │     Grade(case with Actual = resp.Text)                           │
 │     record pass, score, feedback, latency, tokens, cost(BL6)      │
 │   timeout per ResolveTimeout(llm)                                 │
 │   persist child Run → runs/<child>.json                           │
 └─────────────────────────────────────────────────────────────┘
      │
      ▼
 ┌─────────────────────────────────────────────────────────────┐
 │ Stage 4 — aggregation (read-side, no new collection path)      │
 │   collect all terminal cells                                       │
 │   normalize pass_rate/latency/tokens/cost per cell              │
 │   compute deltas vs cell[0]; best (max pass_rate,               │
 │     tie-break min cost, then min latency)                        │
 │   RunSet → runs/<runset>.json, status=complete                 │
 └─────────────────────────────────────────────────────────────┘
```

---

## 3. Pillar 2 — Suite Integration

This pillar answers: how does the existing `internal/evals` framework
(suites, cases, graders, `Grade`) plug into a sweep that grades **live backend
responses** instead of the case's static `Input`?

### 3.1 What is reused, byte-for-byte

The sweep does **not** redefine any suite-side type. These are imported as-is
from `package evals` (`internal/evals/evals.go`):

| Type | Used by the sweep | Notes |
|---|---|---|
| `Suite` | the object being swept | `Name`, `Mode`, `PassThreshold`, `Cases` |
| `Case` | one row of the suite | `Input`, `Expected`, `Grader` |
| `Grader` | per-case grader spec | `Type`, `Pattern`, `Strict`, `Command`, `Rubric`, `Model` |
| `CaseResult` | per-cell per-case result | `Name`, `Pass`, `Score`, `Feedback`, `Actual` |
| `Mode` / `GraderType` | enums | unchanged |
| `Runner.LoadSuite` / `ListSuites` | suite discovery | unchanged |
| `Runner.ListRuns` / `LoadRun` | child read-side | unchanged contract; must skip `kind:"runset"` (see §3.6) |

**One new field** is added to the *persistable* `Run` struct — `ParentRunID`
— so sweep children group under their parent. It is additive (`omitempty`);
a standalone run keeps `ParentRunID == ""` and serializes identically to
today.

### 3.2 The grading contract: from "grade the Input" to "grade the actual"

Today `Grade(c Case)` treats `c.Input` as the LLM-emitted answer and grades
*that* (v6.10.0 semantics). For a sweep that is backwards: `c.Input` is the
**prompt** we send to the backend, and we must grade the **response**.

The fix is a single contract change with full back-compat:

```go
// New: grades an arbitrary "actual" string against the case's Expected.
// This is the ONLY grader change; every grader type is unchanged in
// shape — only the source string flips from c.Input to `actual`.
func GradeActual(c Case, actual string) CaseResult

// Back-compat shim — the standalone eval_run path (Grade(c)) is now
// a one-liner over GradeActual, preserving v6.10.0 semantics exactly.
func Grade(c Case) CaseResult { return GradeActual(c, c.Input) }
```

The sweep's per-cell runner calls `GradeActual(case, resp.Text)`. The
standalone `eval_run` path keeps calling `Grade(case)` and is therefore
**byte-for-byte identical to today**.

#### 3.2.1 Per-grader behaviour (sweep path)

| Grader | `input` (prompt) | `actual` (graded) | `expected` | Pass logic |
|---|---|---|---|---|
| `string_match` | sent to backend | `resp.Text` | `c.Expected` | `strict ? actual==expected : contains(lower(actual), lower(expected))` |
| `regex_match` | sent to backend | `resp.Text` | `c.Grader.Pattern` (else `c.Expected`) | `re.MatchString(actual)` |
| `binary_test` | sent to backend | $INPUT | c.Expected → $EXPECTED | `sh -c c.Grader.Command` exits 0 |
| `llm_rubric` | sent to backend | `resp.Text` | `c.Expected` | LLM grader call returns `pass` (see §3.3) |

For `binary_test` in the sweep the `INPUT` env var is the **backend response**
(the actual under test); `EXPECTED` is unchanged. This is the one place the
env contract visibly differs from the standalone path (`INPUT = c.Input`),
and it is intentional: the sweep tests what the backend produced.

### 3.3 `llm_rubric` — un-stubbed (P3)

Today `llm_rubric` returns a fixed `Pass=false, "manual review needed"`. The
sweep path un-stubs it for *any* backend cell (this is what makes
free-response / open-ended suites comparable across models):

```
grader_prompt = c.Grader.Rubric            # the rubric/instructions
candidate     = resp.Text                  # the backend answer being graded
reference     = c.Expected                 # optional reference answer

resp2 = inference.Dispatcher.Call(ctx, rubricLLM, Request{
    Prompt:  "RUBRIC:\n" + grader_prompt +
            "\n\nREFERENCE (if any):\n" + reference +
            "\n\nCANDIDATE:\n" + candidate +
            "\n\nRespond with JSON: {\"pass\":bool,\"score\":0..1,\"feedback\":string}",
    ModelOverride: c.Grader.Model,          # per-grader model pin (if set)
    Consumer:      "eval_rubric",
})

parse resp2.Text as JSON → CaseResult{Pass, Score, Feedback}
```

- **Rubric LLM selection** — `c.Grader.Model` if non-empty (resolved via the
  inference registry); else the configured `evals.rubric_backend` (§4.4);
  else the *cell's own* backend (the LLM grades its own answer). Order:
  explicit grade-model > default rubric backend > cell backend.
- **Tokens/cost of the rubric call** count into the cell's
  `tokens_in/out` and `cost_usd` (the rubric call is part of the cell's
  measurable cost). See §10 (open question 3) — this is a *decision* made
  here, not a TBD.
- **Parse failure** → `Pass=false`, `Feedback="llm_rubric: unparseable
  grader output: <snippet>"`. Never a silent pass.

### 3.4 Grader compatibility matrix

A backend cell is **gradeable** for a grader type if and only if:

| Grader | Needs | Sweep-compatible |
|---|---|---|
| `string_match` | actual text | ✅ any inference kind |
| `regex_match` | actual text | ✅ any inference kind |
| `binary_test` | `sh -c` + `$INPUT` | ✅ (host must allow the command) |
| `llm_rubric` | a reachable grading LLM | ✅ (needs P3 un-stub) |

A suite whose *entire* grader set is `llm_rubric` before P3 will therefore
read as "all-fail, manual review" across every cell — visible, not masked.

### 3.5 Suite-mode semantics in a sweep

- `mode: capability` (default threshold 0.70) and `mode: regression`
  (default threshold 0.99) carry unchanged meaning: the *same*
  `PassThreshold` is applied to **every cell**, so a "pass" in one cell is
  directly comparable to a "pass" in another.
- `pass` per cell = `pass_rate >= threshold`. The sweep does *not* loosen or
  tighten the threshold per backend.
- A sweep is a fair comparison **iff every cell used the same suite + same
  grader set + same threshold** — which is guaranteed because the matrix
  shares one `Suite`.

### 3.6 Runner interface delta

The `evalsRunner` interface in `internal/server/evals.go` gains two methods
while keeping the single-run surface intact:

```go
type evalsRunner interface {
    // existing — unchanged
    ListSuites() ([]string, error)
    LoadSuite(name string) (*evals.Suite, error)
    Execute(s *evals.Suite) (*evals.Run, error)
    ListRuns(suite string, limit int) ([]*evals.Run, error)
    LoadRun(id string) (*evals.Run, error)

    // new — sweep surface
    Sweep(req evals.SweepRequest) (*evals.RunSet, error) // sync until all cells settle
    ListRunSets(suite string, limit int) ([]*evals.RunSetSummary, error)
    LoadRunSet(id string) (*evals.RunSet, error)
}
```

The concrete runtime type is `*evals.SweepRunner` (new). It composes the
existing `*evals.Runner` (loading/grading/persistence of the suite) and adds:
the inference registry + dispatcher (backend calls), the cost-rate provider
(BL6), and the worker-pool. It is constructed at boot alongside `evals.NewRunner`
(`cmd/datawatch/main.go`), receiving the **same** `inference.Registry` /
`inference.Dispatcher` / `session.Manager` handles the daemon already wires
for `handleAsk` and Council.

**Filtering rule (required):** `ListRuns` and `LoadRun` must treat
`kind == "runset"` rows as *not* runs (skip them). Without this filter, every
sweep parent would surface as a bogus "run" with `suite` but no coherent
`results`. The children remain ordinary `Run` rows and continue to appear in
`evals runs`.

---

## 4. Pillar 3 — Backend Compatibility

This pillar answers: *what* is the sweep allowed to run against, how do the
heterogeneous inference adapters normalize into one comparable table, and what
config surfaces the operator uses to steer it.

### 4.1 Registry of record

The sweep targets the **inference LLM registry** (`internal/inference`,
`Registry`/`LLM`/`Dispatcher`) — *not* the legacy `internal/llm` coding-agent
registry. The two coexist:

| Package | Purpose | Sweep uses it? |
|---|---|---|
| `internal/inference` | one-shot LLM inference: adapter per kind, ComputeNode failover, RBAC | ✅ exclusively |
| `internal/llm` | tmux coding agents (claude-code, aider sessions) | ❌ — session-backend kinds are rejected at plan time |

The sweep never opens a tmux session. A cell is a `Dispatcher.Call`, which
resolves the LLM row → walks ordered ComputeNodes → invokes the kind adapter →
returns `{Text, UsedNode, UsedModel, Backend, DurationMs}`.

### 4.2 Kind-by-kind compatibility table

The sweep is **exhaustive over inference kinds** and **explicitly excludes
session-backend kinds**:

| Kind | Adapter | Sweep target? | Token usage in protocol? | Notes |
|---|---|---|---|---|
| `ollama` | `adapters/ollama.go` | ✅ | ✅ `prompt_eval_count`/`eval_count` | local; cost defaults $0 |
| `openwebui` | `adapters/openwebui.go` | ✅ | ✅ OpenAI-style `usage` | local/SaaS |
| `opencode` | `adapters/opencode.go` | ✅ | ✅ (ollama-protocol) | wraps ollama |
| `claude` | `adapters/claude.go` | ✅ | ✅ message `usage` | SaaS; real money — table shows it via BL6/LLM-row rates |
| `gemini-api` | `adapters/gemini.go` | ✅ | ✅ `usageMetadata` | SaaS |
| `opencode-api` | `adapters/opencode_api.go` | ✅ | ✅ OpenAI-compat `usage` | HTTP inference API |
| `claude-code` | — (session) | ❌ | — | `IsSessionBackendKind==true` → `422` at plan time |
| `aider` | — (session) | ❌ | — | same |
| `goose` | — (session) | ❌ | — | same |
| `gemini` | — (session) | ❌ | — | same (distinct from `gemini-api`) |
| `shell` | — (session) | ❌ | — | same |
| `opencode-acp` | — (session) | ❌ | — | same |
| `opencode-prompt` | — (session) | ❌ | — | same |

Guarantee: the dispatch path is *kind-agnostic* — the same `Request`/`Response`
contract, so adding a new inference kind (e.g. `vllm`) later is "register an
adapter + list it for `*`" with no sweep-side change.

### 4.3 Token accounting (P0 plumbing)

**Gap:** `inference.Response` currently carries `{Text, UsedNode, UsedModel,
Backend, DurationMs}` — **no token counts**. Without tokens, "tokens" and
"cost via BL6" cannot be sourced, so the table cannot do what G6 requires.

**Fix (additive):**

```go
type Response struct {
    Text       string
    UsedNode   string
    UsedModel  string
    Backend    Kind
    DurationMs int64
    TokensIn   int  // new — adapter-parsed prompt token count; 0 = unknown/local
    TokensOut  int  // new — adapter-parsed completion token count; 0 = unknown/local
}
```

Per-adapter parsing:

| Adapter | Source of `TokensIn` | Source of `TokensOut` |
|---|---|---|
| ollama | `prompt_eval_count` | `eval_count` |
| openwebui | `usage.prompt_tokens` | `usage.completion_tokens` |
| opencode (ollama-protocol) | `prompt_eval_count` | `eval_count` |
| claude | `usage.input_tokens` (incl. cache read) | `usage.output_tokens` |
| gemini-api | `usageMetadata.promptTokenCount` | `usageMetadata.candidatesTokenCount` |
| opencode-api | `usage.prompt_tokens` | `usage.completion_tokens` |

**Zero is legal** and means "unknown": the aggregator reports the cell's tokens
as the sum (may be 0) and cost as BL6-estimated (may be $0). Operators reading
a 0-token SaaS cell should treat it as "adapter did not report", not
"free".

### 4.4 Cost normalization (BL6)

`cost_usd` per cell = Σ over cases of `session.EstimateCost(rate, in, out)`
(`internal/session/cost.go`, unchanged). Rate resolution order:

1. `evals.cost_rates[llm_name]` (per-LLM override, §4.7) — highest priority.
2. `LLM.CostPer1kTokensInput/Output` when present (the LLM row's own rate).
3. The daemon's session rate table (`session.Manager.costRates`, seeded from
   `session.DefaultCostRates()` and operator overrides via
   `session.cost_rates`).
4. `CostRate{}` (zero) — i.e. the cell is free (all local kinds).

Deliberate *non*-goal: a per-cell "efficiency" column (pass-per-dollar) is not
in the table; operators can derive it from `pass_rate` × `cost_usd` in their
own tooling.

**LLM-row cost fields** (`CostPer1kTokensInput`/`CostPer1kTokensOutput`) are
already present on the `LLM` struct and are read as-is by the sweep. The sweep
does not write them.

### 4.5 ComputeNode RBAC & failover

The sweep passes `Consumer: "eval"` on every `Dispatcher.Call`. The dispatcher
already enforces the full matrix:

- **Consumer allow/deny** — `node.AllowsConsumer("eval")`: a node with
  `allowed_consumers` pinned must *add* `eval` (or stay default-all).
  `denied_consumers` containing `eval` excludes a node from sweeps
  explicitly. A new node added with no permissions config (default-all)
  is sweep-eligible with zero operator action.
- **Maintenance** — `node.InMaintenance` skips the node for sweep cells
  exactly as for `ask`/`council`.
- **Ordered failover** — per-LLM `ComputeNodes` list is walked
  left-to-right; `ErrTransient` triggers failover to the next node; all
  exhausted → cell error (`ErrNoBackend` shape), siblings unaffected.
- **Routing variants** — `direct`, `docker-network`, `datawatch-proxy` are
  transparent: the dispatcher resolves peers/containers before the adapter
  call. A cell reports `UsedNode = <node name>` on the response.
- **SaaS kinds** (`claude`, `gemini-api`) — dispatched directly (no
  ComputeNode); `UsedNode` is empty; cost is real (see §4.4).

**New consumer name `eval`** is the *only* behavioural interaction with
existing machinery. It is purely additive: default-all nodes accept it with no
config change; pinned nodes must add it. `eval_rubric` (the §3.3 rubric
call) is a second consumer name, handled the same way.

### 4.6 Why session-backend kinds are excluded

`claude-code`, `aider`, `goose`, `gemini` (CLI), `shell`, `opencode-acp`,
`opencode-prompt` have **no inference adapter**: `Dispatcher.Call` for them
returns `no adapter for kind <k>`. If a sweep allowed them, the first cell
would fail with a confusing message and the operator would have to learn the
limitation by error.

The planner rejects them **at matrix time** with a single error listing all
offending names:

```
422: session-backend kind(s) have no inference adapter: ["aider","goose"]
```

If the future wants "grade a coding agent against a suite", the right shape
is a **session-prodded runner** — a separate `internal/evals/agent_runner.go`
that opens a tmux session per cell and grades its transcript — deliberately
*out of scope* for this spec.

### 4.7 Config surface

New daemon config section `evals` (in `internal/config/config.go`, additive
and optional — defaults mirror the spec):

```yaml
evals:
  max_parallel: 3         # global sweep fan-out cap (mirrors autonomous.max_parallel_tasks)
  max_cells: 64           # hard ceiling on matrix size (DoS guard)
  default_backends: "*"   # "*" | [names...] — the fallback when a request omits backends
  cost_rates: {}          # optional per-LLM rate override (map of LLM name → {in_per_k, out_per_k})
  rubric_backend: ""      # LLM name for llm_rubric grading (empty = cell's own backend)
```

| Key | Default | Notes |
|---|---|---|
| `evals.max_parallel` | `3` | global cap; request values clamp to it (never rejected) |
| `evals.max_cells` | `64` | hard cap; overflow → `400` |
| `evals.default_backends` | `"*"` | `*` = every *enabled* inference-kind LLM. Explicit backend list in a request bypasses this entirely |
| `evals.cost_rates` | `{}` | per-LLM BL6 rate override; highest-priority rate source for that LLM's cells. Empty = inherit session cost table |
| `evals.rubric_backend` | `""` | empty = rubric uses the cell's own backend; set = a fixed grader LLM across all cells (keeps the table fair even when a cheap model is being evaluated) |

Per-LLM opt-outs (on `internal/inference/llm.go` `LLM`, all `omitempty`):

| LLM field | Meaning | Default |
|---|---|---|
| `eval_enabled` | opt in/out-of `*` expansion (explicit backend list bypasses) | `true` (inference kinds are sweep-eligible by default) |
| `eval_default_model` | model override when a cell names the LLM without a model | empty = LLM `Model`/`Models` |
| `eval_timeout_seconds` | per-cell timeout override for this LLM's cells; beats `ResolveTimeout` | 0 = `ResolveTimeout(llm)` |
| `eval_max_parallel` | per-LLM cap on concurrent cells (multi-model setups) | 0 = inherit `evals.max_parallel` |

**Cost rates are deliberately *not* duplicated on the evals struct apart
from `evals.cost_rates`.** The session manager's `costRates` table (BL6) is
the canonical operator-editable place for session-wide rates; `evals.cost_rates`
exists only for per-sweep overrides (e.g. a test run at an internal price).

### 4.8 Backend compatibility checklist (acceptance)

Before declaring the sweep "compatible with backend X", each checklist item
must be green:

- [X] `Dispatcher.Call` round-trips with `Consumer="eval"` against a live LLM
      row of kind `X` (regression test per kind, using the existing
      `adapters/<kind>` fixture).
- [X] `Response.TokensIn` / `Response.TokensOut` are populated for a
      single-case suite run with a stubbed or live LLM (P0 test).
- [X] `cost_usd` for the cell is `> 0` iff (a) tokens are non-zero and (b)
      a non-zero rate is resolvable via §4.4's order (unit test per kind).
- [X] Node failure (one ComputeNode down) falls over to the next node
      without aborting the cell (existing dispatcher test, extended for
      `Consumer="eval"`).
- [X] `denied_consumers: ["eval"]` excludes a node from the cell's failover
      walk (RBAC test, new).
- [X] `*` expansion excludes session-backend kinds and disabled LLMs; a
      wildcard on an all-disabled daemon returns `422` (planner test).
- [X] A suite with only `llm_rubric` cases, run pre-P3, reads as
      all-fail-with-"manual review" and does *not* crash the sweep
      (regression test in §8).

---

## 5. Surfaces (one canonical contract)

The same `SweepRequest` / `RunSet` shapes are served through four surfaces.
None forks the request — they all build one `SweepRequest` and hit the same
REST route, so behaviour is identical regardless of entry point.

| Surface | Verb | Maps to |
|---|---|---|
| **REST** | `POST /api/evals/sweep` (body = `SweepRequest`) · `GET /api/evals/runsets` · `GET /api/evals/runsets/{id}` | the canonical route |
| **MCP** | `eval_sweep` (CSV `backends`/`models`) · `eval_list_runsets` · `eval_get_runset` | proxy to REST |
| **CLI** | `datawatch evals sweep <suite> --backends a,b [--models ...] [--max-parallel N]` · `evals runsets [--suite …] [--limit N]` · `evals get-runset <id>` | `daemonJSON`/`daemonGet` of REST |
| **Comm** | `evals sweep <suite> [backends]` · `evals runsets [suite]` · `evals get-runset <id>` | thin proxy of REST |

The existing four MCP tools (`eval_list_suites`, `eval_run`, `eval_list_runs`,
`eval_get_run`) and five REST routes are **unchanged**. `eval_run` remains the
single-backend single-run verb and stays the right call for the Algorithm
Mode Measure phase and quick self-grade loops.

Full parameter tables, mcpsdk Go signatures, REST request/response JSON
examples, the status-code table, and the audit action are specified in the
companion `eval-sweep-spec.md` (§2 and §3) and are normative here by
reference. The one place this spec is authoritative over that doc is the
*behavioural* contract: matrix expansion rules (§1.3), grading semantics
(§3.2), and the backend-compatibility table (§4.2).

### 5.1 Request/acceptance summary

| Check (planner order) | Status |
|---|---|
| `suite` present + loadable | else `404` `suite not found: <name>` |
| `backends` resolved (wildcard or explicit) | unknown name → `400`; session-kind/disabled → `422` |
| `*` expanded to ≥1 backend | else `422` `no enabled inference-kind LLMs to sweep against` |
| `models` length ∈ {0,1,len(backends)} | else `400` `models length N does not match 0, 1, or backends length M` |
| cell count ≤ `evals.max_cells` | else `400` `matrix size N exceeds evals.max_cells M` |
| all checks pass | `200` settled `RunSet` (per-cell failures are still `200`; see `incomplete`) |

### 5.2 Audit

Each sweep writes one audit entry:
`action=evals_sweep`, `resource_type=eval_runset`, `resource_id=<runset-id>`,
`details.suite=<name>` — the same audit log `evals_run` uses, so the sweep is
visible to `audit_query`/`get_alerts` with no new audit plumbing.

---

## 6. Data structures (catalog)

All new/modified types live in `package evals` (`internal/evals`). Existing
`Suite`, `Case`, `Grader`, `CaseResult`, `Mode`, `GraderType` are unchanged
and reused throughout. Full field-level Go definitions with JSON tags are in
`eval-sweep-spec.md` §1; this section is the **index + ownership** catalog.

| # | Type | Status | Key fields | Persisted as |
|---|---|---|---|---|
| 6.1 | `Run` | **modified** (+1 field) | existing fields + `ParentRunID` (`parent_run_id,omitempty`; `""` = standalone) | `runs/<id>.json` (unchanged layout) |
| 6.2 | `SweepRequest` | new | `Suite`, `Backends`, `Models`, `MaxParallel` | — (request-only) |
| 6.3 | `RunSet` | new | `ID`, `Kind:"runset"` (discriminator), `Suite`, `Mode`, `Threshold`, `Matrix`, `MaxParallel`, `Cells`, `ReferenceCellIndex`, `Deltas`, `Best`, `Status`, `Incomplete`, `StartedAt`, `FinishedAt` | `runs/<id>.json` with `kind:"runset"` |
| 6.4 | `RunSetStatus` | new | `pending` · `running` · `complete` · `failed` | — (enum) |
| 6.5 | `MatrixSpec` | new | `Backends`, `Models`, `MaxParallel` (resolved, post-expansion) | embedded in `RunSet` |
| 6.6 | `Cell` | new | `Backend`, `Model`, `Node`, `RunID`, `Pass`, `PassRate`, `Threshold`, `LatencyMs`, `TokensIn`, `TokensOut`, `CostUSD`, `Error`, `CaseResults` | element of `RunSet.Cells` |
| 6.7 | `Delta` | new | `PassRateDelta`, `LatencyDeltaMs`, `CostDeltaUSD` (vs reference cell; sign convention §1.6) | element of `RunSet.Deltas`, parallel to `Cells` |
| 6.8 | `CellSummary` | new | `Backend`, `Model`, `Node`, `PassRate`, `LatencyMs`, `TokensIn`, `TokensOut`, `CostUSD` (the `Best` winner) | `RunSet.Best` |
| 6.9 | `RunSetSummary` | new | `ID`, `Suite`, `Status`, `Incomplete`, `CellsCount`, `BestPassRate`, `StartedAt`, `FinishedAt` (projection for list endpoints — cheap, no cells) | — (read-side projection) |
| 6.10 | `inference.Response` | **modified** (+2 fields) | existing 5 + `TokensIn`, `TokensOut` | — (wired in P0) |

**Ownership:** types 6.2–6.9 → `package evals`. 6.10 → `package inference`
(P0). No type crosses the boundary except via those two field additions, so
existing consumers compile unchanged.

---

## 7. Implementation sequencing

Work is ordered so each milestone is independently shippable and testable.
Each phase lands behind the additive-only guarantee (§9).

| Phase | Scope | Touches | Gates the next |
|---|---|---|---|
| **P0 — token plumbing** | Add `TokensIn/Out` to `inference.Response`; parse usage in ollama / openwebui / opencode-api / claude (+ gemini-api once its adapter lands) adapters | `internal/inference/dispatcher.go`, `adapters/*` | — |
| **P1 — sweep core** | `SweepRequest`/`RunSet`/`Cell`/`Delta`/`MatrixSpec` types; `ParentRunID` on `Run`; `SweepRunner` (plan → fan-out → per-cell → settle); `POST /api/evals/sweep` + `GET /api/evals/runsets[{,/{id}}]`; MCP `eval_sweep`/`eval_list_runsets`/`eval_get_runset`; CLI + comm verbs; wire `SweepRunner` at the same boot site as `evals.NewRunner` | `internal/evals`, `internal/server/evals.go`, `internal/mcp/evals.go`, `cmd/datawatch`, `internal/router/evals.go` | P0 (so tokens are non-zero) |
| **P2 — cost + config** | `evals` config section (§4.7); `eval_*` LLM row fields; `Consumer:"eval"`/`"eval_rubric"` plumbing through the ComputeNode RBAC path; cost resolution order (§4.4); delta-vs-reference + best-summary in settlement | `internal/config`, `internal/inference/llm.go`, `internal/compute/node.go`, `internal/evals` | P1 |
| **P3 — llm_rubric** | Un-stub `llm_rubric` via `Dispatcher.Call` (rubric as system prompt, candidate = `resp.Text`, `c.Grader.Model` pin); suite fixtures gain rubric cases | `internal/evals`, suite YAML fixtures | P2 (cost already wired) |

**Boot wiring:** `cmd/datawatch/main.go` constructs `evals.NewRunner(dataDir)`
today; the sweep composes that same runner with the daemon's existing
`inference.Registry` / `inference.Dispatcher` / `session.Manager` handles (the
same set `handleAsk` already receives) and exposes the resulting
`*evals.SweepRunner` via `HTTPServer.SetEvalsRunner`. No new daemon flag
required to enable — the routes 503 until the runner is set, matching the
existing evals routes.

---

## 8. Verification

Reuse the existing evals test harness (`internal/evals/evals_test.go` style)
plus targeted tables. All are deterministic (fake dispatcher; no live calls).

1. **`GradeActual` is back-compat for the standalone path.** For every
   grader type, `Grade(c)` (existing) and `GradeActual(c, c.Input)` (new)
   return the *identical* `CaseResult`. Pin this as a golden fixture so the
   v6.10.0 semantics can never drift when the sweep path diverges.
2. **Matrix expansion (planner table).** Cases:
   - explicit list with one unknown name → `400`, offending name in the error.
   - explicit list with a session-backend kind → `422`, all offending names listed.
   - explicit list with a disabled LLM → `422`.
   - `*` with ≥1 enabled inference-kind LLM → expands to that set.
   - `*` with zero enabled inference-kind LLMs → `422`.
   - `models` length 0 / 1 / N / bad → only the first three accepted.
   - cell count above `max_cells` → `400`.
   - `max_parallel` above `evals.max_parallel` → silently clamped;
     `RunSet.MaxParallel` reflects the applied value.
3. **Per-cell independence.** Fake dispatcher errors on backend `A` only;
   assert: cell `A.Error` is set, siblings `B`, `C` complete and carry
   non-zero metrics, `RunSet.Incomplete == true`, `RunSet.Status == "complete"`.
4. **Cross-backend metric normalization.** A stub dispatcher returns known
   `Response{...DurationMs, TokensIn, TokensOut}` per llm name; a golden
   fixture pins `cells[i].latency_ms/tokens_in/tokens_out/cost_usd`
   exactly for 3 backends.
5. **Delta + best.** Assert `deltas[0]` is the zero value; `deltas[i] =
   cell[i] - cell[0]` per column; `best` is the argmax with the two
   documented tie-breaks (min cost, then min latency).
6. **Persistence / read-side.** Two sweeps written to `runs/`; `ListRuns`
   returns the *children only* (each child has `parent_run_id == <runset>`)
   and **never** the `kind:"runset"` rows. `LoadRunSet(id)` returns the
   parent with its `Cells` intact. `LoadRun(<runset-id>)` → not found (404).
   A child run loaded via `LoadRun` carries `parent_run_id` set.
7. **RBAC.** A pinned node (`allowed_consumers: ["ask"]`) is skipped for a
   `Consumer:"eval"` cell; a default-all node is used. `denied_consumers:
   ["eval"]` excludes a node from failover.
8. **Concurrency ceiling.** A fake dispatcher that blocks briefly on each
   call; assert at most `evals.max_parallel` calls are in flight concurrently
   (tracked via an atomic counter in the fake).
9. **Cost resolution (§4.4 order).** Three LLMs: (a) `evals.cost_rates`
   override present, (b) LLM-row `CostPer1kTokens*` present, (c) neither —
   assert the sweep picks the first available source, in that order, and
   $0 for (c).
10. **llm_rubric pre-P3 behaviour.** A suite of *only* `llm_rubric` cases,
    run pre-P3, reads as all-fail with the "manual review" feedback and the
    sweep still settles to `complete` (it does *not* crash). Post-P3 the
    same suite, with a stubbed grader LLM, reads `pass` for the golden case.

**Cross-backend independence** is guaranteed by construction — cells never
share adapter state and each goes through its own `Dispatcher.Call`; the tests
above just pin that property.

---

## 9. Compatibility guarantees

- `GET /api/evals`, `GET /api/evals/suites`, `POST /api/evals/run`,
  `GET /api/evals/runs`, `GET /api/evals/runs/{id}` — **unchanged**.
- MCP tools `eval_list_suites` / `eval_run` / `eval_list_runs` /
  `eval_get_run` — **unchanged**.
- `Run` serializes **identically** for standalone runs: `ParentRunID` is
  `omitempty`, so existing rows and readers see byte-identical JSON.
- `Score`/`Feedback`/`Actual` on `CaseResult` are untouched; `GradeActual`
  only changes *which string* is graded, not the result shape (§8.1 pins the
  back-compat boundary).
- `inference.Response` gains two **additive** int fields; existing callers
  of `DurationMs` etc. compile unchanged. Zero value = "not reported", never
  changes a previously-received field's meaning.
- The `internal/inference` dispatcher, registry, ComputeNode RBAC, and adapter
  set are all *reused*, not forked or replaced — the sweep is a new consumer
  (`Consumer:"eval"`/`"eval_rubric"`) of the existing path.
- No database, no migration, no new retention policy, no new daemon flag.
- Federation caps and the audit log are the *existing* mechanisms extended to
  the new routes — no new auth or audit machinery.

---

## 10. Open questions

1. **Does `*` include operator-disabled LLMs?** → **No.** Disabled is an
   operator signal; a disabled backend must not silently appear in a
   wildcard sweep. An *explicit* list that includes a disabled LLM is a
   `422`, so intent is visible and unambiguous.
2. **RunSet retention** → piggyback `runs/`; no extra GC. The one permanent
   obligation: `ListRuns`/`LoadRun` must filter `kind == "runset"` forever.
3. **`llm_rubric` token/cost** → the grading call *is* part of the cell; its
   `TokensIn/Out` and cost are folded into the cell's totals (decided in
   §3.3). The alternative (separate `rubric_*` columns) is deferred —
   revisit only if operators want to read grading cost apart.
4. **Session-backend kinds** → excluded by design (§4.6). A "grade an agent
   against a suite" feature, wanted as-is, is out of scope; it would ship as
   a separate session-prodded runner and never fold into this `RunSet`.
5. **Sweep timeout** → bounded per case by `ResolveTimeout(llm)` (§1.5, G9).
   A whole-swall timeout is *not* specified; the operator sets per-LLM
   `eval_timeout_seconds` or `TimeoutSeconds` to cap it.

---

*End of specification.* This document is the single technical contract for
Eval Sweep. Behavioural contracts (§1, §3, §4) are authoritative; API and
data-structure *shape* (field names, exact JSON) is normative via
`eval-sweep-spec.md` §1–§3, which this spec supersedes on any conflict.
