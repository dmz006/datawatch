# Eval Nodes in the PRD-DAG Orchestrator — Technical Specification

**Date:** 2026-09-16 · **Status:** Draft (v1.0)
**Scope:** Node-kind extension · Verdict adapter · Parity surfaces
**Proposes:** `enhancement-proposals.md` §2 — "Eval nodes in the PRD-DAG orchestrator"
**Gap source:** `feature-themes.md` §1 — the DAG-verification gap vs. LangGraph/DSPy: *"do not run downstream PRD B until suite X passes on the artifact PRD A produced"*
**Companion (sweep behaviour):** `eval-sweep-technical-spec.md` — **source of truth** for sweep matrix expansion, grading semantics, `RunSet`/`Cell` shapes, and the `evals.*` config section. This spec references it and never restates its behavioural contracts.
**Not a code change.** This document defines the contract; it creates and modifies no `.go` files.

---

## 1. Overview + problem statement

The BL117 orchestrator (`internal/orchestrator`, package doc `internal/orchestrator/models.go:1-5`,
design doc `docs/plans/2026-04-20-bl117-prd-dag-orchestrator.md` — the path the
`models.go:5` comment points at; the file now lives at
`docs/plans/historical-plans/2026-04-20-bl117-prd-dag-orchestrator.md`) composes BL24 autonomous
PRDs into a dependency graph and runs them under a **guardrail-attestation model**:

> `// NodeKind separates PRD execution nodes from guardrail attestation nodes.`
> — `internal/orchestrator/models.go:11`
>
> `// Verdict is a guardrail's pass/warn/block judgment. A node-level`
> `// `block` halts the graph and requires operator intervention.`
> — `internal/orchestrator/models.go:41-42`

Today a node is either `prd` (wrap a BL24 PRD run) or `guardrail` (one LLM-rule attestation
of a *specific* PRD's summary — see `Node` comment at `internal/orchestrator/models.go:52-54`
and `GuardrailRequest.PRDID` at `internal/orchestrator/runner.go:28`, "the PRD this
guardrail is attesting"). There is no node kind that *measures behaviour*. The closest
existing mechanisms do not close the gap:

- **Guardrail nodes** grade a PRD's text summary against an LLM rule set
  (`rules`, `security`, `release-readiness`, `docs-diagrams-architecture` —
  `DefaultConfig()` at `internal/orchestrator/runner.go:50-57`). They cannot execute a
  suite, a binary probe, or a rubric against the project's running state.
- **BL367 quality gates** (`autonomous quality_gates_*` config) block a single PRD on a
  `test_command` regression check. They are per-PRD, not graph-scoped, and emit no
  orchestrator `Verdict` — nothing the DAG can order against.
- **BL259 `eval_run`** executes a suite against one backend with a threshold pass/fail.
  It is an out-of-band verb: the operator must remember to run it between PRDs.

Consequence: in a multi-PRD graph, PRD B always starts as soon as PRD A's attestation
nodes pass, even when the *behavioural* acceptance criteria (eval suite) fail. The
"DAG-verification gap vs. LangGraph/DSPy" (feature-themes.md §1) is exactly this: the DAG
expresses ordering but not *verification* of the state ordering produces.

**Proposed feature.** Add a third node kind, `eval`: it executes a named eval suite
(optionally with sweep parameters from Proposal 1) against the graph's project directory
state, records a `Verdict` struct exactly like a guardrail verdict, and its `block`
outcome halts the graph exactly like a guardrail `block`. The dependency-walk and
verdict-appending machinery in `runner.go` is unchanged; only the node-type dispatch
gains a branch and the planner gains a node source (enhancement-proposals.md §2,
rated feasibility 5/5 · impact 4/5).

**Design goals.**

| ID | Requirement | Priority |
|---|---|---|
| E1 | `eval` node kind planned and dispatched exactly like `guardrail` | Must |
| E2 | Verdict reuses the existing `Verdict` struct — byte-identical json tags — so `orchestrator_verdicts`, the graph GET, and PWA all render eval verdicts with zero consumer changes | Must |
| E3 | `block` outcome halts the graph; `warn`/`pass` proceed; `warn` is non-blocking identical to guardrail semantics | Must |
| E4 | Additive only: `prd`/`guardrail` planning, dispatch, persistence, and every existing REST/MCP/CLI/comm surface unchanged | Must |
| E5 | Optional sweep consumption (backend matrix) reuses `eval-sweep-technical-spec.md` verbatim, gated behind sweep landing | Should |
| E6 | Dependency-walk (`topoSort`/`depBlocked`) and verdict-appending code unchanged | Must |

---

## 2. Node design

### 2.1 New `NodeKind` value

Proposed addition to the const set at `internal/orchestrator/models.go:14-17`:

```go
const (
    NodeKindPRD       NodeKind = "prd"
    NodeKindGuardrail NodeKind = "guardrail"
    NodeKindEval      NodeKind = "eval"      // new — suite-verification gate
)
```

`NodeStatus`, `GraphStatus`, and the JSON round-trip at the store
(`internal/orchestrator/store.go:120-124`, plain `json.Unmarshal` of `Graph`) absorb the
new kind with no change: unknown/absent kinds are simply not emitted by the planner,
and existing persisted graphs contain only `prd`/`guardrail` rows, so loading them after
the change is unaffected.

### 2.2 New fields on `Node`

The `Node` struct (`internal/orchestrator/models.go:55-77`) today carries kind-specific
payload in two optional fields: `PRDID` ("required when Kind==prd", `:59`) and
`Guardrail` ("name, when Kind==guardrail", `:60`). The eval node follows the same
additive pattern:

```go
type Node struct {
    // ...existing fields unchanged (ID, GraphID, Kind, PRDID, Guardrail,
    // DependsOn, Status, Verdict, Error, StartedAt, FinishedAt, CreatedAt,
    // UpdatedAt, ObserverSummary) — models.go:56-76

    // New (both omitempty; absent on prd/guardrail rows, so their JSON
    // serializes byte-identically to today):
    EvalSuite    string   `json:"eval_suite,omitempty"`    // required when Kind==eval; suite name under ~/.datawatch/evals/
    EvalBackends []string `json:"eval_backends,omitempty"` // optional sweep params (Proposal 1); empty = single-backend run
}
```

Field semantics:

| Field | Meaning | Notes |
|---|---|---|
| `EvalSuite` | suite name to execute via BL259 `Runner.LoadSuite` (see `eval-sweep-technical-spec.md` §3.1 for the unchanged `Suite` object) | Required when `Kind == NodeKindEval`. Planner rejects a row with empty suite. |
| `EvalBackends` | optional list of LLM registry names (or `["*"]`) | When non-empty and sweep is available, the node executes as a sweep (`eval-sweep-technical-spec.md` §1) and the verdict is derived from the `RunSet` (pass iff all cells pass, or best-cell — see §2.4). Empty = single-backend run using `evals.default_backends` per `eval-sweep-technical-spec.md` §4.7. |

`Node.Guardrail` is a scalar name because one guardrail is one attestation. One eval
suite is one gate; the optional backend matrix is *within* the gate, hence a single
suite name + optional backends list rather than one node per backend.

### 2.3 `Verdict` structural contract (adapter must match exactly)

The adapter's only output is the existing struct, `internal/orchestrator/models.go:43-50`,
with its exact field types and json tags:

```go
type Verdict struct {
    Outcome     string    `json:"outcome"`               // pass | warn | block
    Severity    string    `json:"severity,omitempty"`    // info | low | medium | high | critical
    Summary     string    `json:"summary"`
    Issues      []string  `json:"issues,omitempty"`
    VerdictAt   time.Time `json:"verdict_at"`
    ValidatorID string    `json:"validator_id,omitempty"` // session ID of the validator worker, if any
}
```

An implementer building the eval adapter fills it as follows:

| `Verdict` field ← | eval source |
|---|---|
| `Outcome` | exactly per the outcome-mapping tables below: `pass` / `warn` / `block` (the `pass_rate < PassThreshold` comparison is the same one a single `Run` uses — BL259 threshold gate: `Run.Pass` at `internal/evals/evals.go:100`, computed at `:196`) |
| `Severity` | `high` for threshold breach of a `mode: regression` suite, `medium` for `mode: capability` breach (modes per `eval-sweep-technical-spec.md` §3.5); `info` otherwise |
| `Summary` | one line: `suite <name>: pass_rate 0.85 vs threshold 0.99 (N cases, M failed)` — mirrors the PRD-node convention of stashing the run output in `Summary` (`runner.go:210-216`) |
| `Issues` | one entry per failed case: `"<case name>: <feedback>"` (from `CaseResult.Feedback`, `eval-sweep-technical-spec.md` §3.1) |
| `VerdictAt` | `time.Now()` at settle |
| `ValidatorID` | the eval child `Run` ID (or sweep `RunSet` ID) so the operator can open `evals get-run <id>` for the full per-case table — the exact analogue of a guardrail's validator-session ID |

**Outcome mapping (single-backend phase, E5 off):**

| Eval result | `Outcome` | Blocking? |
|---|---|---|
| `Run.Pass == true` (pass_rate ≥ threshold) | `pass` | no |
| `Run.Pass == false` and operator config `orchestrator.eval_block_on_fail` is `true` (default) | `block` | **yes — halts graph** |
| `Run.Pass == false` and `eval_block_on_fail` is `false` | `warn` | no |
| suite missing / runner error | node `status=failed`, `node.Error` set, `Verdict == nil` | yes — `depBlocked` already treats `NodeFailed` as blocking (`runner.go:297-306`) |

**Outcome mapping (sweep phase, E5 on):** run via the sweep surface with
`SweepRequest{Suite: n.EvalSuite, Backends: n.EvalBackends}` (`eval-sweep-technical-spec.md`
§6.2). Pass iff `RunSet` is non-incomplete and every `Cell.Pass == true`; otherwise
`block` (or `warn` per `eval_block_on_fail`). `ValidatorID` = the `RunSet` ID; `Issues`
carries the failing `cells[i].backend/cell.error` entries.

### 2.4 Why not a new verdict type

`Store.ListVerdicts` flattens every node's `*Verdict` across all graphs
(`internal/orchestrator/store.go:93-106`) and serves it at
`GET /api/orchestrator/verdicts` (`internal/server/orchestrator.go:201`, MCP
`orchestrator_verdicts` at `internal/mcp/orchestrator.go:172-183`). Reusing the same
struct is what makes the parity goal E2 cost-free: every existing consumer — CLI
`datawatch orchestrator verdicts` (`cmd/datawatch/cli_orchestrator.go:152-157`), comm
`orchestrator verdicts` (`internal/router/bl220_comm_commands.go:62-67`), PWA — renders eval verdicts with zero changes. The PWA routes the orchestrator view into
the Automata tab as a card (see the BL247 view routing at
`internal/server/web/app.js:1877-1878`) and renders verdicts generically from the same
verdict list.

---

## 3. Runner integration

### 3.1 The existing node-type dispatch (what plugs in where)

`Runner.Run` (`internal/orchestrator/runner.go:138-197`) walks `topoSort` output and,
per node, dispatches on kind at `runner.go:169-189`:

```go
if node.Kind == NodeKindPRD {
    if err := r.runPRD(ctx, g, node); err != nil { ... blocked = true ... }   // :169-176
} else {
    if err := r.runGuardrail(ctx, g, node); err != nil { ... }                // :177-183
    if node.Verdict != nil && node.Verdict.Outcome == "block" {               // :184-188
        node.Status = NodeBlocked
        blocked = true
        _ = saveNode(r.store, g, node)
    }
}
```

Note the asymmetry: a `prd` failure sets `blocked=true` directly; a non-`prd` node
completes unless it returns a `block` verdict. **Proposed change:** widen the second
branch's entry condition from "not PRD" to "guardrail **or** eval", adding one
node-type check and one call:

```go
switch node.Kind {
case NodeKindPRD:
    // unchanged — runner.go:169-176
case NodeKindEval:
    // NEW — mirrors the guardrail branch exactly, per E4/E6:
    if err := r.runEval(ctx, g, node); err != nil {
        node.Status = NodeFailed; node.Error = err.Error()
        _ = saveNode(r.store, g, node); continue
    }
    if node.Verdict != nil && node.Verdict.Outcome == "block" {   // identical to :184-188
        node.Status = NodeBlocked; blocked = true
        _ = saveNode(r.store, g, node)
    }
default: // NodeKindGuardrail
    // unchanged — runner.go:177-188
}
```

The `default` case keeps guardrail behaviour byte-identical (E4). The eval branch
*deliberately duplicates* guardrail semantics — same error path, same block-check —
so that `TestRun_BlockVerdictHaltsGraph`-style tests carry over verbatim (§5).

### 3.2 `runEval` behind the same function-interface style

The runner keeps its cycle-free, test-friendly fn indirection (comment at
`runner.go:1-8`; fns at `runner.go:20-22` `PRDRunFn` and `runner.go:34-37`
`GuardrailFn`, wired in `main.go`; tests inject fakes —
`orchestrator_test.go:12-19` `newTest(t, run, guard)`). The eval node adds one
more of the same shape:

```go
// EvalRequest is the input to an eval gate. Mirrors GuardrailRequest
// (runner.go:25-32) with the suite identity and optional sweep backends.
type EvalRequest struct {
    GraphID     string
    NodeID      string
    PRDID       string    // empty — the gate attests the project state, not one PRD's summary
    Suite       string    // n.EvalSuite
    Backends    []string  // n.EvalBackends (may be nil → single-backend run)
    ProjectDir  string    // g.ProjectDir — the artifact PRD A produced lives here
    Summary     string    // upstream PRD summaries, same context channel as GuardrailRequest.Summary (runner.go:30, 245-252)
}

// EvalFn executes one suite gate and returns its Verdict. Wired at boot to the
// BL259 runner (or the sweep runner once E5 lands); tests inject fakes exactly
// as newTest does for GuardrailFn.
type EvalFn func(ctx context.Context, req EvalRequest) (Verdict, error)
```

`Runner` struct and constructor gain the parallel field — same pattern as
`Runner{run, guard}` (`runner.go:60-70`):

```go
type Runner struct {
    mu    sync.Mutex
    cfg   Config
    store *Store
    run   PRDRunFn
    guard GuardrailFn
    eval  EvalFn          // new; nil = eval nodes degrade like guardrails do without GuardrailFn
}

func NewRunner(store *Store, cfg Config, run PRDRunFn, guard GuardrailFn, eval EvalFn) *Runner
```

**Degradation rule (mirrors `runner.go:232-238`):** `runGuardrail` with
`r.guard == nil` records `Verdict{Outcome: "pass", Summary: "no guardrail fn
configured"}` and completes the node (`TestRun_NoGuardrailFn_StillProgressesAsPass`,
`orchestrator_test.go:182-197`). `runEval` does the same with
`"no eval fn configured"` — this keeps dev/test environments progressing (E4) and
gives the §5 unit tests a deterministic no-fn baseline.

**`runEval` body sketch** (mirrors `runGuardrail`, `runner.go:221-262`):
`markStart` → timeout from `cfg.EvalTimeoutMs` (new `Config` field, default 0 →
cell timeouts bounded via the existing `ResolveTimeout`, the same mechanism the
sweep spec pins in design goal G9 (`eval-sweep-technical-spec.md` §0.5); a plain
`eval_run` needs no per-cell timeout) → call `r.eval(cctx, EvalRequest{...})` →
`n.Verdict = &v` → `markDone(n, NodeCompleted)` unless outcome is `block` →
`saveNode`. The PRD-summary pull loop (`runner.go:224-231`) is not copied: eval
gates attest project state, so `Summary` is assembled from upstream PRD nodes as
context only, never as the object under test.

### 3.3 Explicitly unchanged code (E6)

Documenting what must NOT change, with file:line evidence:

| Code | Location | Why untouched |
|---|---|---|
| `topoSort` (cycle detection, stable order) | `runner.go:308-353` | kind-agnostic — operates on `DependsOn` only |
| `depBlocked` (blocked/failed deps cancel downstream) | `runner.go:297-306` | already treats `NodeBlocked` — which a blocked eval node becomes — as a blocking status |
| verdict append (`n.Verdict = ...` + `saveNode`) | `runner.go:256`, `runner.go:276-286` | `saveNode` replaces the node in the slice and calls `store.SaveGraph`; no kind knowledge |
| `block` → `NodeBlocked` + `blocked=true` | `runner.go:184-188` (reused by the new `case NodeKindEval`) | already the halt mechanism |
| graph status settle `GraphBlocked`/`GraphCompleted` | `runner.go:191-196` | unchanged |
| store persistence, `CreateGraph`, `ListVerdicts` | `store.go:38-58`, `store.go:93-106` | `[]Node` JSON round-trip is kind-agnostic |
| API adapter surface | `api.go:36-81` (incl. `PlanGraph` `:70-72`, `RunGraph` `:66-68`, `ListVerdicts` `:74-81`) | `any`-typed adapter — new kinds flow through without touching this file's signatures |

The **only** files inside the package that the node design + runner integration
touch are `models.go` (const + 2 `Node` fields), `runner.go` (Config field, `EvalRequest`,
`EvalFn`, `Runner` field, `NewRunner` signature, `Run` dispatch, `runEval`), and
`orchestrator_test.go` (new tests per §5). `api.go` and `store.go` are expected to
compile unchanged; if the boot wiring in `main.go` must pass a third fn, that call
site updates — no package-internal surface does.

### 3.4 Planner integration

`Plan` (`runner.go:91-134`) currently emits PRD nodes then one guardrail node per
(PRD × `cfg.DefaultGuardrails`) with `DependsOn: [prdNodeID[pid]]`
(`runner.go:116-127`). Eval nodes are **explicit operator input**, not a
per-PRD auto-fanout like guardrails (a suite gate is chosen deliberately per graph).
`Plan` gains a third node source from the graph-create request:

- The create request (REST body, `internal/server/orchestrator.go:102-107`) gains an
  optional `evals` entry: `[{suite, backends?, depends_on_prd}]`.
- `Plan` emits, per entry, one `NodeKindEval` node with
  `DependsOn: [prdNodeID[entry.DependsOnPRD]]` (a PRD that must complete first) and
  `EvalSuite`/`EvalBackends` set. PRD B that must not run before the gate lists
  the eval node ID in its deps — expressed either as an explicit node-id dep or, in
  the simple form, a PRD-level dep on the PRD the gate follows (§4.1 planner shape).
- `Plan` remains idempotent (`runner.go:90`, "calling Plan twice replaces the node
  set"), so re-planning a graph with/without gates is lossless.

If a gate is declared for PRD A but A's node id is not yet known at Plan time, the
planner resolves it from `prdNodeID` the same way guardrail rows do
(`runner.go:122`).

---

## 4. API / interface design

All surfaces are **additive**; every existing route/tool/verb compiles and behaves
unchanged (E4). The table in §4.7 is the normative parity enumeration.

### 4.1 Graph planner input shape (additive fields on the existing create/plan body)

Existing REST create body (`internal/server/orchestrator.go:101-107`):
`{title, project_dir, prd_ids, deps}`. Additive:

```json
POST /api/orchestrator/graphs
{
  "title": "payments-rewrite",
  "project_dir": "/srv/payments",
  "prd_ids": ["pA", "pB"],
  "deps": {"pB": ["pA"]},
  "evals": [                                  // NEW — optional; absent = today's planner
    {
      "suite": "payments-regression",
      "backends": ["*"],                      // optional; absent = single-backend eval_run
      "attests": "pA",                        // PRD whose artifact is under test (eval node depends on pA's node)
      "blocks": ["pB"]                        // PRDs that must not run until this gate settles
    }
  ]
}
```

Planner expansion: one `NodeKindEval` row per `evals[]` entry with
`DependsOn=[node(pA)]`, and each `blocks[]` PRD gets the eval node appended to its
`DependsOn` (same resolution path as `deps` — `runner.go:106-112` / `:122`).
`blocks` is sugar for the operator-facing sentence *"do not run pB until the suite
passes on pA's artifact"*; the graph JSON stored after `Plan` then contains the
eval row with `kind:"eval"`, `eval_suite`, `eval_backends`, `depends_on`, and `verdict`
exactly like every other row (node shape per §2.2, verdict per §2.3).

`POST /api/orchestrator/graphs/{id}/plan` (`:171-185`) accepts the same `evals` body
for re-planning; absent = existing guardrail-only plan (E4).

### 4.2 Verdict stream — zero new surface

`GET /api/orchestrator/verdicts` (`internal/server/orchestrator.go:201-213`) already
returns `ListVerdicts()` over all nodes (`store.go:93-106`); eval verdicts appear
there automatically because they are `*Verdict` on a `Node`. Graph detail
(`GET /api/orchestrator/graphs/{id}`, `internal/server/orchestrator.go:150-161`)
returns the node with its verdict. MCP `orchestrator_verdicts`
(`internal/mcp/orchestrator.go:172-183`) and CLI `datawatch orchestrator verdicts`
(`cmd/datawatch/cli_orchestrator.go:152-157`) likewise. **No new verdict endpoint
exists or is required** — stated explicitly to prevent scope creep.

### 4.3 MCP tool additions

Existing tool surface in `internal/mcp/orchestrator.go` (all proxy to REST):

| Tool | Definition | Handler | REST target |
|---|---|---|---|
| `orchestrator_graph_create` | `internal/mcp/orchestrator.go:69-77` | `handleOrchestratorGraphCreate` `:78-102` | `POST /api/orchestrator/graphs` |
| `orchestrator_graph_plan` | `:118-124` | `handleOrchestratorGraphPlan` `:125-140` | `POST /api/orchestrator/graphs/{id}/plan` |
| `orchestrator_graph_run` | `:142-147` | `handleOrchestratorGraphRun` `:148-155` | `POST /api/orchestrator/graphs/{id}/run` |
| `orchestrator_verdicts` | `:172-176` | `:177-183` | `GET /api/orchestrator/verdicts` |

Proposed additions (all additive strings on the two planner tools, mirroring the
existing `deps` string-JSON pattern at `:75` and `:122`):

1. `orchestrator_graph_create` gains `mcpsdk.WithString("evals", Description("Optional JSON array of eval gates: [{suite, backends?, attests, blocks}]; absent = guardrail-only plan"))` and the handler pipes it through `body["evals"]` after JSON validation (same unmarshal-error handling as `prd_ids`, `:79-82`).
2. `orchestrator_graph_plan` gains the same `evals` parameter and forwards it (`:125-140` pattern).
3. **No new tool for verdicts** — `orchestrator_verdicts` already streams eval
   verdicts (§4.2). Documented in the tool description:
   *"Lists guardrail AND eval verdicts across all graphs."* (description text at `:174`).

### 4.4 REST endpoints

Verified existing routes, registration at `internal/server/server.go:370-373`
(Sprint S8 block):

```go
apiMux.HandleFunc("/api/orchestrator/config", api.handleOrchestratorConfig)
apiMux.HandleFunc("/api/orchestrator/graphs", api.handleOrchestratorGraphs)
apiMux.HandleFunc("/api/orchestrator/graphs/", api.handleOrchestratorGraphs)
apiMux.HandleFunc("/api/orchestrator/verdicts", api.handleOrchestratorVerdicts)
```

Handlers at `internal/server/orchestrator.go`: `handleOrchestratorConfig` `:42`,
`handleOrchestratorGraphs` `:76` (create `:101-135`, get/delete `:148-170`,
`plan` `:171-185`, `run` `:186-195`), `handleOrchestratorVerdicts` `:201`.
OpenAPI block: `internal/server/web/openapi.yaml:973-1059`.

**New REST surface: none.** The `evals` field rides on the two existing create/plan
bodies above. The OpenAPI spec gains the additive `evals` property on those two
request schemas only — documented additively against
`internal/server/web/openapi.yaml:990-1048`, otherwise unchanged.

### 4.5 CLI verb shape

Existing subcommand set at `cmd/datawatch/cli_orchestrator.go:32-42`
(`config-get`, `config-set`, `graph-list`, `graph-create`, `graph-get`, `graph-plan`,
`graph-run`, `graph-cancel`, `verdicts`). `newOrchestratorGraphCreateCmd`
(`:74-102`) already parses an optional third JSON arg (`deps_json`) — the additive
pattern:

```
datawatch orchestrator graph-create <title> <prd_ids_json> [deps_json] [evals_json]
    --project-dir <dir>
    # evals_json: [{"suite":"payments-regression","backends":["*"],"attests":"pA","blocks":["pB"]}]
datawatch orchestrator graph-plan <id> [deps_json] [evals_json]   # newOrchestratorGraphPlanCmd :114-130
```

No new verb required: `graph-run <id>` (`newOrchestratorGraphRunCmd`,
`cmd/datawatch/cli_orchestrator.go:132-140`) and `verdicts` (`:152-157`) work on
gate-carrying graphs unmodified because the runner and verdict stream are
kind-agnostic (§3.3). The `Short` help strings at `:77`/`:116` gain the optional
arg — cosmetic.

### 4.6 Comm-channel trigger

Existing verb set documented at `internal/router/bl220_comm_commands.go:29-38`:

```
orchestrator [config|verdicts|get <id>|create <title> <proj>|plan <id>|run <id>|delete <id>]
```

with the run verb handled at `:104-114` (POST `/api/orchestrator/graphs/<id>/run`)
and the parser proven by `TestBL220_Parse_Orchestrator_Run`
(`internal/router/bl220_comm_commands_test.go:33-38`) alongside
`TestBL220_Parse_Orchestrator_Bare/Config/Get/Verdicts` (`:12-45`).

**Additive verbs:**

```
orchestrator create <title> <project> <prd_ids_json> [evals_json]   # extends bl220_comm_commands.go:79-92
orchestrator plan <id> [evals_json]                                  # extends :93-103
```

`run`/`get`/`verdicts`/`config`/`delete` are unchanged — a gate-carrying graph is
triggered exactly like today (`orchestrator run <id>`), which is the parity
requirement: *the operator's mental model for starting a graph must not know about
gates*. Loopback-safety tests (`TestBL220_Loopback_Orchestrator`,
`bl220_comm_commands_test.go:235-240`) are extended with the new `create`/`plan`
variants only.

### 4.7 Parity surface

Per AGENT.md "Parity surface section" rule (AGENT.md:151) and the full parity-surface
set (AGENT.md:462-463): `REST`, `MCP`, `CLI`, `comm channel`, `YAML/config`, `PWA`,
`Android`, `iPhone`.

| Surface | Status | Detail |
|---|---|---|
| `REST` | additively extended | `evals` field on `POST /api/orchestrator/graphs` and `.../plan` only; verdicts/graph GET/DELETE unchanged (§4.1, §4.4) |
| `MCP` | additively extended | `evals` string param on `orchestrator_graph_create`/`orchestrator_graph_plan` (`internal/mcp/orchestrator.go:69`, `:118`); description update on `orchestrator_verdicts` `:172`; no new tools |
| `CLI` | additively extended | optional 4th arg `graph-create`, optional 2nd arg `graph-plan` (§4.5); `graph-run`/`verdicts` unchanged |
| `comm channel` | additively extended | `create`/`plan` gain `evals_json` (§4.6); `run`/`get`/`verdicts` unchanged |
| `YAML/config` | extended | new keys below + `datawatch config` apply-patch cases alongside the existing `orchestrator.*` cases at `internal/server/api.go:5891-5900` |
| `PWA` | **no change required (capability parity)** | the Automata-tab orchestrator card renders `graph_get` nodes + verdicts generically (`internal/server/web/app.js:1877-1878`); eval rows render as other node rows and their verdicts appear in the same verdict list. If a node-kind chip/badge distinction is added later, that is a PWA-side polish change needing a datawatch-app issue per the Mobile-Parity Rule (AGENT.md:455-460) — file it, do not block on it |
| `Android` | excluded | no client-surface work in v1 — gates are created via the other five operator surfaces; Android parity tracked via the same datawatch-app issue as the PWA polish. Reason: capability parity is met (full REST available in-app); no new screens required for v1 |
| `iPhone/iOS` | excluded | same reason as Android; parity standard PWA==Android==iOS applies to any later UI addition only (AGENT.md:457-460) |
| Locales | N/A | no new user-facing strings in v1 (only JSON-schema fields and help-text arg lists); if a PWA badge ships, run the Localization Rule (AGENT.md:415-441) at that time |
| Federation caps | unchanged | `orchestrator_verdicts` already uses `federation.CapCouncilList` (`internal/server/orchestrator.go:202`); no new routes, no new caps |

**New `orchestrator` config keys** (additive on the existing `Config` struct,
`runner.go:40-48`, surfaced by `orchestrator_config_get` / `orchestrator_config_set`
and the `datawatch config` apply-patch cases at `internal/server/api.go:5891-5900`):

| Key | Default | Notes |
|---|---|---|
| `orchestrator.eval_enabled` | `true` (gates only exist if the operator plans them — the flag is a kill switch) | `false` → planner rejects `evals` rows with a clear error; runner degrades eval nodes as §3.2 no-fn rule |
| `orchestrator.eval_block_on_fail` | `true` | §2.3 outcome mapping: `false` turns suite failures into `warn` (non-blocking) |
| `orchestrator.eval_timeout_ms` | `120000` | per-gate timeout, same shape as `guardrail_timeout_ms` (`runner.go:43`) |
| `orchestrator.eval_backend` | `""` | LLM ref for `llm_rubric` suites in gate context; empty = `evals.rubric_backend` (`eval-sweep-technical-spec.md` §4.7); single-backend `eval_run` ignores it |

Sweep parameters on a gate (`evals[].backends`, `evals[].models`) are owned by
`eval-sweep-technical-spec.md` §4.7 (`evals.*` keys) and §6.2 (`SweepRequest`) —
this spec does not duplicate them (constraint: "name the companion
eval-sweep-technical-spec.md as the source of truth for sweep behaviour").

---

## 5. Test plan

All tests follow the existing in-package style (`internal/orchestrator/orchestrator_test.go`):
fresh `Runner` against a `t.TempDir()` store via `newTest(t, run, guard)`
(`orchestrator_test.go:12-19`) — extended to `newTest(t, run, guard, eval)` — fake
fns, no network, no live LLM. Deterministic.

### 5.1 Unit (`internal/orchestrator`)

| Test | Mirrors | Asserts |
|---|---|---|
| `TestPlan_EvalNodeWithSuiteAndDeps` | `TestPlan_GeneratesPRDAndGuardrailNodesWithDeps` (`:47-87`) | `Plan` with an `evals` entry emits exactly one `NodeKindEval` row; `EvalSuite` set; `DependsOn` = the attested PRD's node id; each `blocks[]` PRD's node lists the eval node; PRD/guardrail node counts unchanged |
| `TestPlan_NoEvalEntries_Undertouched` | same | `Plan` with an empty/absent `evals` field produces a node set byte-identical to today's (E4 guard) |
| `TestRun_EvalDispatch_ReceivesRequest` | `TestRun_PRDSummaryReachesGuardrail` (`:118-147`) | fake `EvalFn` receives `EvalRequest{Suite, ProjectDir, GraphID, NodeID}`; upstream PRD summary reached `Summary` |
| `TestRun_EvalPass_CompletesGraph` | `TestRun_NoGuardrailFn_StillProgressesAsPass` (`:182-197`) | `Outcome:"pass"` → node `NodeCompleted`, graph `GraphCompleted`, zero block verdicts |
| `TestRun_EvalWarn_NonBlocking` | — | `Outcome:"warn"` → node completes, graph completes (eval-branch copy of the `:184` check skips non-block) |
| `TestRun_EvalBlockHaltsGraph` | `TestRun_BlockVerdictHaltsGraph` (`:149-180`) | `Outcome:"block"` → node `NodeBlocked`, graph `GraphBlocked`, exactly one block verdict, every downstream node `NodeCancelled` (via `depBlocked`, `runner.go:297-306`) |
| `TestRun_NoEvalFn_DegradesToPass` | `TestRun_NoGuardrailFn_...` (`:182-197`) | `eval == nil` → verdict `pass`, `Summary == "no eval fn configured"` (E4 degradation, §3.2) |
| `TestRun_EvalFnError_FailsNode` | `TestRun_BlockVerdictHaltsGraph`'s error path | fn returns error → node `NodeFailed` + `Error` set; `TestTopoSort_CycleDetection` (`:91-99`) carries over unmodified as the cycle guard |
| `TestStore_EvalNodeRoundTrip` | `TestStore_CreateGraph_RoundTrip` (`:23-36`) | save graph with an eval row → reload → `EvalSuite`/`EvalBackends` intact; a graph saved *before* the field existed still loads (additive-field back-compat) |
| `TestAPI_ListVerdicts_IncludesEval` | `TestAPI_SetConfig_UnmarshalsRawJSON` (`:201-212`) | `ListVerdicts()` (`api.go:74-81`) contains the eval verdict alongside guardrail ones — the §4.2 zero-new-surface property pinned in Go |

### 5.2 Integration (`internal/server` test harness style)

1. **Ordering: PRD A → eval gate → PRD B.** Create a two-PRD graph with
   `evals:[{suite, attests:"pA", blocks:["pB"]}]`; stub `PRDRunFn` + `EvalFn`; assert
   node execution order from timestamps (`StartedAt`) is pA → eval → pB, and that
   `pB` never starts before the eval settles.
2. **Gate blocks the successor.** Same graph, `EvalFn` returns `block`; assert pB is
   `NodeCancelled`, graph `GraphBlocked` (mirrors `TestRun_BlockVerdictHaltsGraph`
   end-to-end through the REST handler `handleOrchestratorGraphs`,
   `internal/server/orchestrator.go:76`).
3. **Sweep-backed gate (E5 phase).** `EvalFn` wired to the sweep runner
   (`eval-sweep-technical-spec.md` §7 P1 landing): `evals:[{suite, backends:["a","b"]}]`;
   assert the node's `ValidatorID` is a `RunSet` id, `Issues` lists failing cells, and
   a single failing cell blocks the graph (mapping per §2.3). Skipped while the
   sweep is unlanded (see §6 gating).
4. **REST/MCP/CLI smoke** — one live-daemon pass per surface with `orchestrator
   graph_create/plan/run/verdicts` (CLI `cmd/datawatch/cli_orchestrator.go:74-157`),
   MCP `orchestrator_graph_create` + `orchestrator_verdicts`
   (`internal/mcp/orchestrator.go:69-77`, `:172-183`), and comm `orchestrator run <id>
   verdicts` (`internal/router/bl220_comm_commands.go:62-67`), plus the loopback
   safety assertions already exercised at
   `internal/router/bl220_comm_commands_test.go:235-240`.

### 5.3 Smoke

One-PRD graph with a **single eval node** and no guardrail entries
(`DefaultGuardrails` cleared, as the tests do at `orchestrator_test.go:130` and
`:159`). Operator workflow: create → `graph-run` → poll `graph-get` until the eval
row shows `verdict.outcome` → confirm the same verdict object appears in
`datawatch orchestrator verdicts`, the MCP `orchestrator_verdicts` output, the comm
`orchestrator verdicts` reply, and the PWA orchestrator card — i.e. *identical
placement to a guardrail verdict* (goal E2), and the graph settles `blocked` (failing
suite) or `completed` (passing suite) with the operator unblocked to read the per-case
table via `evals get-run <validator_id>`.

**Acceptance (whole spec):** all five existing `orchestrator_test.go` tests green
unmodified; §5.1 table green; one integration order test green; smoke pass.

---

## 6. Sequencing & dependencies

### 6.1 Dependency chain

```
Phase A  (this spec, minimal)  ──depends on──▶  nothing beyond BL259 eval_run (v6.10.x, landed)
Phase B  (sweep-backed gates) ──depends on──▶  eval-sweep-technical-spec.md P1 landing (SweepRunner + /api/evals/sweep)
Phase C  (parity polish)      ──depends on──▶  Phase A; locale/mobile work ONLY if a PWA/Android badge is added
```

The `eval` node is **explicitly viable without the sweep** (Phase A): `EvalBackends`
empty → single-backend `eval_run` semantics (suite × threshold), which is the
enhancement-proposals.md §2 minimal shape ("executes a named suite … records a verdict
record exactly like a guardrail verdict"). Sweep params on a gate are a follow-on
concern (Phase B) and must not be planned in — the node field exists from day one
(§2.2) so Phase B is wiring, not schema.

### 6.2 Phase → repo-file map

| Phase | Scope | Files touched (actual) |
|---|---|---|
| **A1 — node contract** | `NodeKindEval` const; `EvalSuite`/`EvalBackends` on `Node`; adapter contract pinned by tests | `internal/orchestrator/models.go` |
| **A2 — dispatch + fn** | `EvalRequest`/`EvalFn`; `Runner.eval`; `NewRunner` 5th param; `Run` switch; `runEval`; `Config` keys (§4.7); boot wiring of the BL259-backed `EvalFn` | `internal/orchestrator/runner.go`; `cmd/datawatch/main.go:4759` (the `orchestratorpkg.NewRunner(ostore, ocfg, prdRun, guard)` call site gains the 5th argument); `internal/config` for the four `orchestrator.eval_*` keys |
| **A3 — planner gates** | `evals` body parsing in the create/plan handlers; `Plan` eval-node emission; OpenAPI property additions | `internal/server/orchestrator.go` (handler bodies + request struct at `:102-107`); `internal/server/web/openapi.yaml:990-1059` (BL117 block); `internal/server/api.go:5891-5900` (config-patch cases) |
| **A4 — parity surfaces** | MCP params + description updates; CLI arg positions; comm `create`/`plan` verbs + loopback tests | `internal/mcp/orchestrator.go` (`:69-77`, `:118-140`, `:172-183`); `cmd/datawatch/cli_orchestrator.go` (`:74-102`, `:114-130`); `internal/router/bl220_comm_commands.go` (`:29-38` doc, `:79-103` verbs), `internal/router/bl220_comm_commands_test.go` |
| **A5 — tests** | §5.1 (all), §5.2 items 1–2 | `internal/orchestrator/orchestrator_test.go`; `internal/server/orchestrator_enrich_test.go`-style handler tests |
| **B1 — sweep wiring** | `EvalFn` gains a sweep path behind `EvalBackends`; `ValidatorID` = RunSet id; `Issues` from failing cells; `orchestrator.eval_backend` honoured | `internal/orchestrator/runner.go` (adapter, not package surface beyond A2); `internal/server/evals.go` interface extension per `eval-sweep-technical-spec.md` §3.6 (`Sweep` method already specified there); §5.2 item 3 test |
| **C1 — optional polish** | PWA kind-chip / `locale-guard` / datawatch-app issue | `internal/server/web/app.js`, locale bundles — **only if** a distinct eval badge ships; otherwise closed with no client change |

`store.go`, `api.go`, and the existing `Run`/`topoSort`/`depBlocked`/`saveNode`
spans in `runner.go` (§3.3 unchanged-list) are **not in any phase's change set** —
they compile untouched, which is the acceptance proof of E4/E6.

### 6.3 Sequencing vs the Eval Sweep story (Proposal 1)

Per `enhancement-proposals.md` — "Sequencing (per synthesis.md recommendation)" section (line 79), item 1
("Proposal 1 + 2 first — both reuse BL259/BL117 with minimal new machinery"):

1. **Phase A ships standalone**, immediately (or in parallel with sweep P0–P1): it
   consumes only `eval_run` semantics, which already exist. This alone closes the
   DAG-verification gap for the single-backend case.
2. **Phase B is gated on sweep P1** (`eval-sweep-technical-spec.md` §7: P1 lands
   `SweepRunner` + `POST /api/evals/sweep` + the `evalsRunner.Sweep` interface).
   Until then, `evals[].backends` non-empty is rejected with
   `400 sweep not available on this daemon (evals P1 not landed)` — a planner error,
   mirroring how the sweep spec returns `400` on malformed input
   (`eval-sweep-technical-spec.md` §5.1).
3. **Phase C is cosmetic** and may land in any release after A.

No other harness-impl doc is a prerequisite for reading or implementing this spec —
where sweep behaviour is relied on, `eval-sweep-technical-spec.md` is the source of
truth and is named at every such point.

---

*End of specification.* This document is the contract for the eval-node (DAG)
half of the Eval-orchestration theme; `eval-sweep-technical-spec.md` remains the
contract for sweep behaviour. Together they are the Proposal 1 + 2 pair.
