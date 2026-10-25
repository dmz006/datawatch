# BL431 — Automation: multi-Automaton orchestration (design pass)

**Date:** 2026-10-14
**Version:** v9.0.75 at time of writing (`var Version = "9.0.75"` in `cmd/datawatch/main.go:116`). Implementation target TBD: the next minor after 9.0.75 at the earliest. Framed as a v10.0.0 architecture item.
**Status:** Design pass — needs_review, no implementation approved. No code, config default, `var Version` or CHANGELOG entry is changed by this doc.
_Re-verified: 2026-10-14 (attempt 3, content confirmed unchanged and correct; full verifier exit 0)._

**Terminology.** Operator-facing text says **Automaton / Automata**, following
AGENT.md's Terminology Rule. The internal Go type is `PRD` (`internal/autonomous`),
and this doc uses "PRD" only for code identifiers. **Automation** (capital A) is the
new coordinating object designed here: a named, graph-shaped group of member
Automata that the daemon advances on its own. It is never called a "Program" or a
"Queue".

**Numbering: why BL431 and not BL430.** The design-pass Automaton for this work is
stored in the daemon as record `c187962a`, titled "BL430 — Automation: multi-Automaton
orchestration design pass". That number is a **collision**. BL430 is already
registered (`docs/plans/README.md` item 27): it is the per-task / per-story /
per-Automaton verification-LLM override, PRD `4d0ad909`, shipped in v9.0.72. The
BL428 doc already flags the same collision
(`docs/plans/2026-10-14-concurrency-and-budget-design.md:9-13`). This doc uses
**BL431** throughout. It does **not** renumber or relabel the BL430 entry or any
file that references it, because changing the historical label of a feature that
has already shipped has no benefit. Only `c187962a`'s title carries the wrong
number. Whether to retitle that record is left to the operator (Open question 13).

---

## Introduction

datawatch can run one Automaton very well. It cannot run a **body of work**: an
ordered, partly conditional set of Automata that should follow one another without
a human or a chat session in the loop. This doc designs that missing layer as the
**v10.0.0 orchestration architecture**: a flexible, editable, cost-aware,
multi-model-aware way to sequence large bodies of work across many Automata.

The 2026-10-13/14 overnight queue stall is one motivating incident. It is not the
whole problem. During that stall the queue sat idle from 02:23 to 07:37, and the
approved PRD `180a01a0` sat idle from 00:33 until 07:49. The stall is recorded in
BL428's worked example (iii)
(`docs/plans/2026-10-14-concurrency-and-budget-design.md:685-720`). The same night
also showed that the work being sequenced kept changing while it ran (see
**Dynamic mid-flight editing**). It also showed that per-batch cost and model policy
had to be applied by hand to each Automaton.

**This doc absorbs and supersedes the "Automation coordinator" concept drafted as
section 7a of BL428** (PRD `a0669eb6`). BL428 keeps its narrower scope:
phase-overlap concurrency safety and usage-budget throttling mechanics.

How the sibling and prior items fit in:

| Item | What it contributes to BL431 |
|---|---|
| **BL427** (PRD `fe2e2110`, `docs/plans/2026-10-13-prd-lifecycle-events.md`) | The lifecycle event dispatcher. The Automation coordinator **subscribes** to it as an in-process Go callback (BL427 doc lines 259-270 and 510-516) instead of adding its own hook into the terminal-state write path. |
| **BL428** (PRD `a0669eb6`, `docs/plans/2026-10-14-concurrency-and-budget-design.md`) | The foundation this sits on. It provides the per-`project_dir` tree write lock (3a), the inference caps (3b) and the base usage-budget mechanics (section 4: the ceiling, meters, `admit` check and back-off). BL431's **BudgetPolicy** narrows that ceiling to one Automation (BL428 doc 4c, lines 432-435). |
| **BL422** (PRD `f6a38c29`, README item 21) | The model/effort tiering eval. Its tier recommendations are applied through the **Automation-level model default**, so a whole batch gets them at once. |
| **BL414** (README item 14) | Cost rollup (`Decision.CostUSD`, which has zero writers today, `internal/autonomous/models.go:57`) and the budget-guardrail idea. BudgetPolicy extends it and reuses its rollup for per-Automation attribution. BL428 recommends that BL428 absorb BL414's guardrail half (BL428 doc 4c). |
| **BL430** (PRD `4d0ad909`, README item 27, shipped v9.0.72) | The verification-LLM override. Its task → story → Automaton → global precedence chain (`internal/autonomous/verification_llm.go`) is the shape this doc extends with one Automation tier. |
| **BL117** (`internal/orchestrator/`, v4.0.0) | Prior art: a PRD-DAG "Graph". This doc recommends **superseding** it (see **Prior art**). |

### Evidence notes (what was and was not read)

- **Sibling docs exist and were read.** BL428's doc is
  `docs/plans/2026-10-14-concurrency-and-budget-design.md`. BL427's doc is
  `docs/plans/2026-10-13-prd-lifecycle-events.md`. The budget key names used here
  come from BL428 doc §4 and §7b. The dispatcher names come from BL427 doc
  "Recommendation". Both are **design names that are not yet implemented**. They are
  provisional until those implementation PRDs land, and they must be reconciled
  against the real code then.
- **Sibling PRD specs were read** via `datawatch autonomous prd-get`.
  - `a0669eb6` (BL428, status `completed`, spec of 9,541 chars) has **no**
    "Automation", "StepOrder", "coordinator" or "7a" text in its stored spec.
  - `fe2e2110` (BL427, `completed`) has no "Automation" or "BL428" text in its
    stored spec.
  - So the BL428 §7a draft shape quoted below
    (`Automation{ID, Name, Members, Mode}`,
    `AutomationMember{PRDID, StepOrder, AdvancePolicy}`,
    `auto_advance_on_completion`) comes from this task's brief. It is **not
    verified** against any stored text.
  - The landed BL428 doc's §7a (lines 724-740) is already a forward pointer to
    BL431.
- **Not verified.** It was not checked whether `30e6f153` was created *because of*
  something seen in BL422's eval. Its creation time, 2026-10-14 01:14:44, is
  verified. Its ordering relative to BL422 comes from the brief.
- **Re-verified on 2026-10-14 (retry pass).** The cited anchors were re-checked
  against the tree at `var Version = "9.0.75"` and still match:
  `Manager.Run` (`internal/autonomous/executor.go:124`),
  `resolveTaskBackendModel` (`executor.go:1607`), `ResolveVerificationLLM` and the
  `VerifierSource*` constants including `pool`
  (`internal/autonomous/verification_llm.go:18-25, 53`), `CancelStory`
  (`internal/autonomous/manager.go:1771`), `SetStoryLLM` (`manager.go:2360`),
  `API.SetStoryLLM` / `API.SetTaskLLM` (`internal/autonomous/api.go:602, 611`),
  `Decision.CostUSD` (`internal/autonomous/models.go:57`) and the `Effort` type
  (`models.go:105`). The previous attempt failed only because the verifier
  session timed out. No design content changed in this pass.

---

## Scope

**In scope (design only):**

1. **A graph data model.** Per-outcome edges (`OnComplete` / `OnBlocked` /
   `OnFailed`), not a flat `StepOrder`.
2. **Dynamic mid-flight editing.** Members can be inserted, spliced, reordered and
   removed while the Automation is active.
3. **A status lifecycle** with a transition table.
4. **Control operations** that follow the BL382 precedent.
5. **Cost:** an optional **BudgetPolicy** that narrows BL428's global ceiling.
6. **Multi-model defaults:** an Automation tier in both the execution and the
   verification precedence chains.
7. **Trigger:** a consumer of BL427's dispatcher, plus restart reconciliation and
   `autonomous_status` visibility.
8. **PWA:** an "Automation" tab, a list view, a pipeline-strip detail view and a v1
   create/edit flow.
9. **v10.0.0 framing,** the cross-references above, and an explicit decision on the
   BL117 prior art.

The doc also covers the parity surface, phases, the follow-up build list, the
changes needed in sibling PRDs, and open questions.

**Out of scope:**
- **Any code.** That includes Go/JS/HTML/CSS changes, config default changes and
  version bumps.
- **Nested Automations** (an "Automation of Automations", or an "Initiative"
  object). This is an open question and is **not decided either way** (Open
  question 2).
- **Multi-backend / open-model concurrency logic.** That is BL428's boundary (BL428
  doc §5). An Automation member is an ordinary Automaton as far as BL428's tree lock
  and inference caps are concerned.
- **Re-litigating BL422, BL423, BL424, BL425 or BL426.**
- **Creating, approving, running, editing or cancelling any Automaton.**

**Files the follow-up implementation would touch.** These were all read for this
doc:
- `internal/autonomous/`: `models.go`, `executor.go`, `verification_llm.go`,
  `api.go`, `manager.go`, `store.go`, `capacity.go`.
- `internal/orchestrator/`: `models.go`, `runner.go`, `store.go`, `api.go`.
- `internal/mcp/orchestrator.go` and `internal/mcp/autonomous.go`.
- `internal/server/autonomous.go`.
- `internal/server/web/index.html` and `internal/server/web/app.js`.
- `internal/alerts/store.go` and `internal/session/cost.go`.
- `cmd/datawatch/main.go`.

---

## What exists today (grounding)

### Nothing starts the next Automaton automatically

`Manager.Run` (`internal/autonomous/executor.go:124`) walks one PRD's stories and
tasks. It is only ever started through `API.Run`
(`internal/autonomous/api.go:291`). `API.Run` checks that the status is
approved/running, saves `running`, and launches `Manager.Run` in a goroutine keyed
in `runCancels` (`api.go:309-334`). It is called from these places:

- the REST `run` action, `internal/server/autonomous.go:426-438`;
- the REST `reset_task` action, `:1040` (to make sure an executor is alive);
- `API.Resume` (`api.go:371`);
- boot-resume (`resumeRunningPRDs`, `api.go:81-150`), which relaunches **the same**
  PRD when it was `running`, or when it was `failed` with every task killed by the
  daemon.

MCP `autonomous_prd_run` and the CLI go through the same server path. **No code
path starts a *different* Automaton when one finishes.** `Approve` does not run.
The `completed` / `failed` / `blocked` roll-ups in `Manager.Run` save and return
(BL427 doc, "PRD-level transition points"). The only automatic chaining is
`recurseChildPRD` (`executor.go:753-759`), and it runs a **child** Automaton inline
inside a parent task. That is a different relationship (parent/child), not
sequencing.

### The execution and verification LLM precedence chains

Execution backend and model, `resolveTaskBackendModel(t, story, prd, fallbackBackend)`
at `internal/autonomous/executor.go:1607-1633`. The v8.37.2 rule:

- The **first level that sets a backend wins**, in the order task → story → PRD.
- The model comes from that **same level**. A more specific level's model still
  applies to a backend inherited from a less specific level.
- A less specific level's model never cascades past a level that overrides the
  backend.
- When no level sets a backend, `fallbackBackend` (`m.cfg.ExecutionBackend`, called
  at `executor.go:830`) is used, paired with the first model found at
  task → story → PRD.

Execution effort resolves **independently** (`executor.go:831-836`): task, then
story, then PRD.

Verification, `ResolveVerificationLLM` / `resolveVerificationLLM` in
`internal/autonomous/verification_llm.go` (BL430, PRD `4d0ad909`):

- Precedence is "task → story (the story containing the task) → Automaton → global
  `cfg.VerificationBackend` / `VerificationModel` / `VerificationEffort`"
  (doc comment, lines 28-50).
- "Backend and model follow resolveTaskBackendModel's v8.37.2 rule exactly … the
  model is taken from the SAME level the backend was found at."
- "Effort resolves independently: first non-empty of task → story → Automaton →
  global."
- Provenance is recorded in `VerificationLLM.Source` using the `VerifierSource*`
  constants (lines 16-26): `task`, `story`, `prd`, `global`, and `pool`. `pool`
  marks a backend picked from the global `verification_backends` capacity pool
  rather than by the chain.

The per-unit setters are:
- `API.SetPRDLLM` (`api.go:618`), MCP `autonomous_prd_set_llm`;
- `API.SetStoryLLM` (`api.go:602`, which delegates to `Manager.SetStoryLLM`,
  `manager.go:2360`), MCP `autonomous_prd_set_story_llm`;
- `API.SetTaskLLM` (`api.go:611`), MCP `autonomous_prd_set_task_llm`;
- the verification variants at `api.go:643-657`.

### The BL382 control-op precedent

- `Manager.CancelStory` (`internal/autonomous/manager.go:1771-1822`):
  - refuses a terminal story with an error prefixed `"409: "`;
  - kills the story's task sessions;
  - sets them `cancelled`;
  - appends `Decision{Kind: "cancel_story", Actor, Note: "story=… cancelled: <reason>"}`;
  - saves.
- The REST handler (`internal/server/autonomous.go:1068-1103`):
  - takes `{story_id, actor?, reason?}`, defaulting the actor to `"operator"`;
  - maps the `409:` prefix to `http.StatusConflict`;
  - writes `s.audit(ctx, "cancel_story", "automaton", id, {story_id, reason})`
    (`internal/server/skills.go:443`).
- `cancel_task` (`manager.go:1826`) follows the same pattern.
- `requeue_task` is the forced form of `ResetTask` (`manager.go:1643`; the Decision
  kind is set at `:1757`).
- `API.Cancel` (`api.go:391-410`) kills sessions and appends `Kind: "cancel"`.
- `Pause` / `Resume` (`manager.go:2544`, `:2566`) only accept `running` / `paused`
  respectively. Pause lets in-progress tasks finish and dispatches no new ones
  (`models.go:33`).

### PRD status vocabulary

`internal/autonomous/models.go:20-34` defines these statuses:

| Group | Statuses |
|---|---|
| Before a run | `draft`, `planning`, `needs_review`, `approved`, `revisions_asked`, legacy `active` |
| During a run | `running`, `paused` |
| After a run | `completed`, `rejected`, `cancelled`, `archived`, `blocked`, `failed` |

`type Effort` is at `models.go:105`. `type Decision` is at `models.go:50-66`.

### Status, alerts and cost

- **Status.** `LoopStatus` (`models.go:809`) is what `autonomous_status` returns
  (`internal/mcp/autonomous.go:18-22`; REST `internal/server/autonomous.go:85`).
- **Alerts.**
  - `alerts.Store.Add` / `AddSystem` are in `internal/alerts/store.go:114` and
    `:144`.
  - The daemon's store is `<data_dir>/alerts.json` (`cmd/datawatch/main.go:1856`).
- **Cost.**
  - The per-session `EstCostUSD` is summed in `internal/session/cost.go:77-82`
    and accumulated at `:161`.
  - `Decision.CostUSD` exists but has no writers (BL414).

### Prior art: BL117 PRD-DAG orchestrator (`internal/orchestrator/`)

BL117 (v4.0.0) was designed in
`docs/plans/historical-plans/2026-04-20-bl117-prd-dag-orchestrator.md`. Its model,
in `internal/orchestrator/models.go`:

- A `Graph{ID, Title, ProjectDir, PRDIDs, Nodes, Status}` with statuses
  `draft | active | completed | blocked | cancelled`.
- Each `Node{Kind: prd|guardrail, PRDID, Guardrail, DependsOn []string, Status, Verdict}`.
- Persisted under `<data_dir>/orchestrator/` (`internal/orchestrator/store.go:24-25`).
- Exposed through the `orchestrator_graph_list/create/get/plan/run/cancel` MCP tools
  (`internal/mcp/orchestrator.go:57-158`).
- About 33 `orchestrator` references in `internal/server/web/app.js`. Its PWA lives
  in a Settings sub-tab, because BL238 removed its nav button (comment in
  `internal/server/web/index.html`, ~line 147).

What it actually does:
- `Runner.Run` (`internal/orchestrator/runner.go:138-197`) computes `topoSort` once,
  up front, and walks it.
- When any dependency is blocked, it marks the dependent node `cancelled`.
- It ends `completed` or `blocked`.
- `runPRD` (`runner.go:199-219`) calls the wired `PRDRunFn`. That function is a
  **REST loopback** `POST /api/autonomous/prds/{id}/run`
  (`cmd/datawatch/main.go:5593-5612`).
- `runPRD` then marks the node **`completed` as soon as that call returns 200**.

The loopback returns as soon as `API.Run` has *launched* the executor goroutine
(`api.go:324-345`; the handler replies `{"status":"running"}`). So **a BL117 PRD
node is recorded as completed when its Automaton starts, not when it finishes.**
Dependents then start immediately, against a tree the "completed" predecessor is
still writing. That is exactly the overlap BL428 example (i) documents. This was
confirmed by reading the code, not by a live run.

`internal/pipeline/` (`internal/pipeline/pipeline.go`) is a different layer. Its
DAG nodes are **sessions** ("a directed acyclic graph of tasks where each task is a
datawatch session"), with their own `pending/running/completed/failed` states. It
sits below Automata, not above them, and is not affected by this doc.

---

## Recommendation on BL117: supersede, with a one-shot migration

**Decision (recommended):** Automation **supersedes** the BL117 Graph. Do not
extend BL117 and do not keep both long-term. The reasons:

1. **Wrong completion semantics.** BL117 treats "run accepted" as "completed"
   (above). Fixing that means rewriting `runPRD` to wait for a terminal event, and
   that is exactly the BL427 subscriber this doc designs.
2. **Wrong edge model.** BL117 has an upfront `DependsOn` DAG executed in
   topological order computed once at `Run`. A failure only cancels downstream
   nodes. It cannot express "if this is blocked, go here instead".
   Automation has **per-outcome edges** (`OnComplete` / `OnBlocked` / `OnFailed`)
   followed one event at a time. That is what makes conditional routing and
   **mid-flight editing** possible: the next step is read at advance time, not
   frozen at plan time.
3. **No edit model.** BL117 has no insert, no audit of who added what, and no
   version check.
4. **Guardrail nodes are already redundant.** Per-Automaton guardrails now exist:
   `per_automaton_guardrails_set` and `resolveGuardrails`
   (`internal/autonomous/guardrail_registry.go`), plus ci-watch. A BL117 guardrail
   node maps onto the member Automaton's own guardrail list.
5. **No existing UI to save.** The PWA surface is already demoted to a Settings
   sub-tab, so there is no top-level UX to preserve.

**Fate of existing graphs:**
- Graphs stay readable, as read-only history in `<data_dir>/orchestrator/`.
- `orchestrator_graph_create` / `plan` / `run` and their REST/PWA equivalents return
  a deprecation notice for one minor release and are then removed. `list` / `get`
  stay.
- A one-shot `automation import-graph <graph_id>` converts a graph whose `prd`
  nodes form a **single chain**: each node with exactly one `prd` dependency
  becomes the predecessor's `OnComplete`.
- Guardrail nodes become the member's `per_prd_guardrails`.
- Graphs with fan-in or fan-out between `prd` nodes are reported as "not
  auto-migratable". The operator splits them into several concurrent Automations,
  which is how parallelism is expressed here (see **Data model**).
- Member status is read from the **real PRD status**, never from the graph node,
  because of the completion-semantics bug above.

Whether to keep BL117 alive in parallel instead is Open question 4.

---

## Data model

**Placement:** a new file pair in the `autonomous` package (`internal/autonomous`), `automation.go` (new) and
`automation_store.go`, persisted as `<data_dir>/autonomous/automations.jsonl` next
to `prds.jsonl` (`internal/autonomous/store.go:35`, `:625`). It belongs in the
`autonomous` package, not `internal/orchestrator`, for three reasons:

- the Automation tier has to be read **inside** `resolveTaskBackendModel` /
  `resolveVerificationLLM`;
- the PRD back-reference is checked under the Manager's mutex;
- advancing calls `API.Run` in-process.

A separate package would need a callback seam for each of those, or would create
an import cycle.

```go
// AutomationStatus — see "Status lifecycle".
type AutomationStatus string

const (
	AutomationDraft     AutomationStatus = "draft"
	AutomationActive    AutomationStatus = "active"
	AutomationPaused    AutomationStatus = "paused"
	AutomationCompleted AutomationStatus = "completed"
	AutomationBlocked   AutomationStatus = "blocked"
	AutomationCancelled AutomationStatus = "cancelled"
)

type MemberStatus string

const (
	MemberPending   MemberStatus = "pending"   // not started (UI derives "approved" from the PRD)
	MemberRunning   MemberStatus = "running"   // the cursor is here; PRD running/paused
	MemberCompleted MemberStatus = "completed" // PRD completed; OnComplete followed
	MemberBlocked   MemberStatus = "blocked"   // PRD blocked (or cancelled/reverted, see edge mapping)
	MemberFailed    MemberStatus = "failed"
	MemberSkipped   MemberStatus = "skipped"   // operator skip_member
	MemberCancelled MemberStatus = "cancelled" // Automation cancelled before it ran
)

type Automation struct {
	ID            string             `json:"id"`   // 8-hex, like PRD.ID
	Name          string             `json:"name"`
	Description   string             `json:"description,omitempty"`
	Status        AutomationStatus   `json:"status"`
	EntryMemberID string             `json:"entry_member_id"`
	CurrentMemberID string           `json:"current_member_id,omitempty"` // the single cursor
	Members       []AutomationMember `json:"members"`
	Version       int64              `json:"version"` // bumped on every write; optimistic concurrency

	BudgetPolicy *BudgetPolicy `json:"budget_policy,omitempty"`

	// Execution defaults — new tier between PRD and global.
	Backend string `json:"backend,omitempty"`
	Model   string `json:"model,omitempty"`
	Effort  Effort `json:"effort,omitempty"`
	// Verification defaults — new tier between Automaton (PRD) and global.
	VerificationBackend string `json:"verification_backend,omitempty"`
	VerificationModel   string `json:"verification_model,omitempty"`
	VerificationEffort  Effort `json:"verification_effort,omitempty"`

	ProjectDir string `json:"project_dir,omitempty"` // informational; BL428 3a keys on each member's own tree

	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	IdleSince   *time.Time `json:"idle_since,omitempty"` // set when a member finishes and no next member started
	Decisions   []Decision `json:"decisions,omitempty"` // same type as PRD.Decisions; same 200 trim
}

type AutomationMember struct {
	ID    string `json:"id"`     // 8-hex, stable across edits
	PRDID string `json:"prd_id"` // the member Automaton

	// Per-outcome edges: another member ID, or "" = stop (see edge semantics).
	OnComplete string `json:"on_complete,omitempty"`
	OnBlocked  string `json:"on_blocked,omitempty"`
	OnFailed   string `json:"on_failed,omitempty"`

	AddedAt     time.Time `json:"added_at"`
	AddedBy     string    `json:"added_by"`               // "operator" | "session:<id>" | "agent:<id>" | "import:bl117:<graph>"
	AddedReason string    `json:"added_reason,omitempty"` // required for inserts into a non-draft Automation

	Status     MemberStatus `json:"status"`
	StartedAt  *time.Time   `json:"started_at,omitempty"`
	FinishedAt *time.Time   `json:"finished_at,omitempty"`
	Outcome    string       `json:"outcome,omitempty"`  // PRD status observed at finish
	EdgeTaken  string       `json:"edge_taken,omitempty"` // on_complete|on_blocked|on_failed|skip

	BudgetPolicy *BudgetPolicy `json:"budget_policy,omitempty"` // explicit member override (narrows further)
}

// PRD gains one back-reference field (internal/autonomous/models.go, type PRD):
//   AutomationID string `json:"automation_id,omitempty"`
```

### Graph, not a flat ordinal

The BL428 §7a draft (from the task brief; see Evidence notes) modelled
`AutomationMember{PRDID, StepOrder, AdvancePolicy}` with a single **`StepOrder`**
integer. That is **explicitly rejected**: a flat ordinal cannot express "if X is
blocked, run the fix-up Automaton Y, otherwise continue with Z". Bolting branching
on later would mean a data migration and a second advance engine.

The **per-outcome edge graph is the day-one model.** It is not a "linear MVP now,
graph later" deferral. A **linear** Automation is simply the trivial case: every
member's `OnComplete` points to the next member, and `OnBlocked` / `OnFailed` are
empty. The create flow builds exactly that by default, so the simple case costs the
operator nothing. The draft's `Mode` / `AdvancePolicy` collapse into the edges.
"Gate for approval" does not need its own field, because it is the existing PRD
`approved` gate (see **Trigger mechanism**).

**One cursor per Automation.** Each outcome edge names at most one next member, so
an Automation has exactly one current member at a time. It is sequential, with
conditional branches. **Parallelism is expressed by running several Automations
concurrently**, not by fan-out inside one. That keeps the advance rule trivially
deterministic. A parallel "fork/join" inside one Automation was Open question 10 and is now Phase 3a. The single-cursor rule above holds for linear Automations only; Phase 3a replaces it with a set of active cursors (see the Phase 3a section).

### Mapping PRD statuses onto the three edges

| Member PRD reaches | Edge followed | Notes |
|---|---|---|
| `completed` | `OnComplete` | Empty `OnComplete` means this was a terminal member: the Automation becomes `completed` and an info alert is raised. |
| `blocked` (guardrail, rules review, ci-watch) | `OnBlocked` | Empty means the Automation becomes `blocked` and a warning alert is raised. |
| `failed` | `OnFailed` | Empty means the Automation becomes `blocked` and an alert is raised. Advancing waits until boot-resume's `PRDFailed` recovery pass has finished (`api.go:136-150`), so a daemon-killed member that gets recovered is not routed down `OnFailed`. |
| `cancelled` / `archived` / `rejected` (someone acted on the member directly) | **none** | The Automation becomes `blocked` with the reason "member cancelled outside the Automation". Cancellation is an operator decision, not an outcome, so it is never silently routed. The operator uses `skip_member` to choose an edge. |
| reverts to `approved` after a start (the `API.Run` goroutine error path, `api.go:337-341`) | **none** | Same as above: `blocked`, reason "member run errored". |
| `needs_review` / `revisions_asked` / `planning` / `draft` while current (re-decompose mid-run) | **none (wait)** | Not terminal. The member stays `running` in the Automation, and the idle timer applies. |
| `paused` | **none (wait)** | Shown as paused on the strip. |

### Validation (on every write, under the Automation lock)

- **Cycle detection:** a three-colour DFS over all three edge kinds, starting from
  `EntryMemberID`. Any back-edge is rejected with `400`. A member is one run of one
  Automaton, and a completed Automaton is not re-run by an edge, so a cycle could
  never finish. A deliberate retry loop is modelled as inserting a new fix-up
  member instead.
- Every non-empty edge must name an existing member.
- `EntryMemberID` must exist.
- Members unreachable from Entry are allowed in `draft` but produce a warning.
  Activating with unreachable members is `409`.
- **One active Automation per PRD.** A PRD may be a member of at most one
  Automation whose status is not `completed` or `cancelled`, and only once within
  it.
  - This is enforced through `PRD.AutomationID`, set on add and cleared on
    remove, on cancel, or when the Automation reaches a terminal status.
  - A store index rebuilt on load makes it cheap to check.
  - The rule prevents two coordinators from each believing they own the next
    `Run` of the same Automaton.
- Templates (`IsTemplate`) and child Automata (`ParentPRDID != ""`,
  `models.go:226`) cannot be members. Children are run by their parent task
  (`recurseChildPRD`).
- Removing or reordering is rejected (`409`) for members in `completed` / `failed`
  / `blocked` / `skipped` status, because history is **immutable** (see the next
  section).

### Concurrency between Automations

There is **no global flat queue.** Several Automations may be `active` at once, for
example a release sequence in `datawatch` and an unrelated research batch in
another project. Each has its own cursor. Cross-Automation safety is **not**
re-invented here. It comes from BL428:

- the per-`project_dir` write serialization (BL428 3a,
  `autonomous.project_write_serialization: per_project_dir`), so two Automations
  whose members share a tree serialize their *task execution* at `admit`;
- the `llm:` / `node:` inference caps (3b).

A member of an Automation is an ordinary Automaton to BL428 (BL428 doc §7a:
"no special-casing is needed"). If BL431 is built before BL428 Phase 2, two active
Automations in one tree would overlap exactly as in BL428 example (i). The
follow-up PRD therefore either lands after BL428 Phase 2, or refuses to activate a
second Automation whose members share a `project_dir` with an active one. That
choice is part of Open question 5.

---

## Dynamic mid-flight editing

### The real incidents (2026-10-13/14)

- **`30e6f153`** ("Autonomous task effort reaches claude-code --effort").
  - Created 2026-10-14 01:14:44, approved 07:37:58, started 07:38:00, completed
    07:48:06. These daemon timestamps were verified with `prd-get`.
  - According to the brief, it was discovered *while BL422's eval was running* and
    had to be **inserted ahead of BL422's planned resume**: the eval could not be
    trusted until effort actually reached the CLI.
  - The brief labels it "BL429". The README's registered **BL429** (item 26) is the
    one-shot re-paste fix, PRD `c5774d3f` (shipped v9.0.66), which was created
    00:31 the same night and was also discovered mid-flight. Both are examples of
    the same pattern. This doc does not relabel either one.
- **BL428's own scope** grew through several rounds of live operator iteration while
  other Automata were actively running. Its README entry records that BL429 and
  BL430 were "split out of" it during that work.

Neither case is an edge case. **Discovering work mid-flight is the normal
condition** for a long sequence. A model that only allows edits in `draft` or
`paused` would have forced the operator to cancel and rebuild the sequence, so
these rules allow editing **while the Automation is `active`**.

### Rules

1. **Insert anywhere ahead of the cursor, at any time** (`draft` / `active` /
   `paused` / `blocked`):
   - `insert_member{anchor_member_id, position: before|after, prd_ids: [...], reason}`.
   - A single `prd_id` inserts one member.
   - Several `prd_ids` **splice in a short side-sequence**, chained by `OnComplete`
     in the order given.
   - **After X:** the side-sequence's tail takes X's old `OnComplete`, and X's
     `OnComplete` points to the head.
   - **Before X** (X not started): every edge that pointed to X is re-pointed to the
     head, and the tail's `OnComplete` points to X. If X was `EntryMemberID`, the
     head becomes the entry.
2. **Insert before the current member.** The member already started, so this is
   only allowed when it is `paused` (the PRD is paused) or `blocked`. This is
   exactly the 30e6f153 case: BL422's eval was the paused current member.
   - The cursor moves to the inserted head.
   - The tail's `OnComplete` points back to the paused member.
   - When the coordinator advances to a member whose PRD is `paused`, it calls
     `API.Resume` (`api.go:359-373`) instead of `API.Run`.
3. **Every inserted member records** `AddedAt`, `AddedBy` (`operator`, or
   `session:<id>` / `agent:<id>` for an AI caller, taken from the authenticated
   caller rather than the request body when available) and `AddedReason`.
   `reason` is **required** for any insert into a non-`draft` Automation. It is also
   copied into the Automation's `Decisions` and the audit log.
4. **Completed members are immutable history.** A member in `completed`, `failed`,
   `blocked` (already routed), or `skipped` status cannot be removed or reordered,
   and its edges cannot be changed. The history of what ran, and which edge it
   took, never changes.
5. **Members that have not started can always be reordered or removed**:
   `remove_member` re-points incoming edges to the removed member's `OnComplete`
   target, and `reorder_edges` sets any of a pending member's three edges.
6. **The currently running member's own outgoing edges may be edited**, for example
   to add an `OnFailed` fix-up path while it runs. It **cannot be retroactively
   un-run**: it cannot be removed or moved, and a new member cannot be placed
   before it unless rule 2 applies.
7. **Activation re-validates** the whole graph (cycles, reachability, ownership),
   and every edit re-validates too. An edit that fails validation is rejected as a
   whole, so nothing is ever half-applied.

### Edits racing an advance

The coordinator's advance and every edit operation take the **same per-Automation
mutex** (`sync.Mutex` keyed by Automation ID inside the Automation store), and
every write bumps `Automation.Version`.

- The advance reads `CurrentMemberID` and that member's edges **under the lock**.
  An insert-after-current committed a millisecond before the terminal event is
  therefore honoured. One committed a millisecond after applies to the next
  advance, and the response shows the cursor has already moved.
- Edit requests carry an optional `expected_version`. A mismatch returns
  `409 Conflict` with the current version, mirroring the BL382 `"409: "`
  convention. The PWA always sends it, and AI callers are expected to send it, so
  a stale editor never overwrites a concurrent change.
- The advance itself is idempotent. It is a compare-and-set on
  `(CurrentMemberID == m.ID && m.Status == running)`, so a duplicate event (an
  event plus restart reconciliation, say) cannot advance twice.

---

## Status lifecycle

`draft → active → paused → completed / blocked / cancelled`, mirroring the PRD state
machine in `internal/autonomous/models.go:20-34`. Approval and decomposition of each
**member** stay on the member, and the Automation has no `approved` state of its
own. Activating is the operator's go-ahead for the sequence. Each member still
needs its own approval, because the existing gate is kept.

| From | To | Trigger | PRD-state analogue |
|---|---|---|---|
| — | `draft` | `create` | `PRDDraft` |
| `draft` | `active` | `activate` (validation passes). The coordinator starts the entry member if it is approved, otherwise the Automation goes straight to `blocked`. | `approved → running` |
| `active` | `paused` | `pause_automation`. Auto-advance is suspended, **and the current member is allowed to finish**. Its terminal event is recorded and `CurrentMemberID` moves to the next member, but that member is not started. | `PRDPaused` (in-progress tasks finish, no new dispatch) |
| `paused` | `active` | `resume_automation`. If the cursor is on a not-started member, start it now (same approval gate). | `paused → running` (`Manager.Resume`) |
| `active` | `completed` | A terminal member completes with an empty `OnComplete`. | `PRDCompleted` |
| `active` | `blocked` | Needs a judgment call: the member reached `blocked` / `failed` with an empty edge; the member was cancelled, archived or rejected outside the Automation; the next member is not approved (`needs_review`, `draft`, …); a member Run errored; or a **BudgetPolicy** breach when `on_breach: block`. | `PRDBlocked` |
| `blocked` | `active` | `skip_member` (choose an edge), `resume_automation` after the operator fixes the cause (e.g. approves the next member), or an `insert_member` that gives the cursor a runnable member. | operator action |
| `draft` / `active` / `paused` / `blocked` | `cancelled` | `cancel_automation` | `PRDCancelled` |
| `completed` / `cancelled` | — | terminal. Members' `PRD.AutomationID` is cleared. | — |

`Decisions` on the Automation record `activate`, `advance`, `pause`, `resume`,
`cancel`, `skip`, `insert`, `remove`, `reorder`, `budget_breach` and `blocked`,
each with `Actor` and `Note`. The member PRD also gets one Decision,
`Kind: "automation_advance"`, when the coordinator starts it, so the Automaton's own
timeline shows *why* it started.

---

## Control operations

These follow the BL382 `cancel_story` / `cancel_task` / `requeue_task` precedent
exactly:

- body `{…, actor?, reason?}`, with the actor defaulting to `"operator"`;
- a Decision entry is appended;
- an `"409: "`-prefixed error on an invalid state maps to `409 Conflict`;
- `s.audit(ctx, "<op>", "automation", id, {…})` is written.

| Operation | Valid from | Effect |
|---|---|---|
| `pause_automation` | `active` | Becomes `paused` (lifecycle above). It does **not** pause the member Automaton. Pausing the member as well is the separate existing per-Automaton pause, or `{pause_member: true}`. |
| `resume_automation` | `paused`, `blocked` | Becomes `active`. Starts or resumes the cursor member if it is approved, otherwise stays `blocked` and gives the reason. |
| `cancel_automation` | not terminal | See below. |
| `skip_member` | `blocked`, with the cursor on a `blocked` / `failed` / cancelled-outside member, or on a not-started member that is not approved | `{member_id, via: on_complete\|on_blocked\|on_failed, reason}` (reason required). Marks the member `skipped`, sets `EdgeTaken: "skip"`, follows the chosen edge and becomes `active`. It deliberately bypasses a stuck member. |
| `insert_member` | not terminal | Mid-flight rules 1-3. Reason required unless `draft`. |
| `remove_member` | not terminal; the member has not started | Rule 5. |
| `reorder_edges` | not terminal; the member is pending, or is the running member (its own outgoing edges only) | `{member_id, on_complete?, on_blocked?, on_failed?, reason}`. |

**`cancel_automation` semantics (unambiguous).** The default is to **halt future
auto-advance only**.

- The Automation becomes `cancelled`.
- Every pending member becomes `cancelled` as a member, and its `PRD.AutomationID`
  is cleared. The member Automata themselves are untouched and stay `approved` or
  wherever they were.
- **The in-flight member Automaton is NOT cancelled.** It keeps running to its own
  terminal state, as a now-standalone Automaton. Its terminal event no longer
  advances anything.

An explicit flag, **`cancel_in_flight: true`**, also calls the existing `API.Cancel`
on the in-flight member (`api.go:391`, which kills its sessions) and records both
Decisions.

The default is the non-destructive one for three reasons:
- an Automation is a coordinator, so stopping coordination should not throw away an
  hour of a member's work;
- it matches PRD `Pause`'s "in-progress tasks finish" philosophy;
- the destructive form is one explicit flag away.

Open question 3 asks the operator to confirm this.

---

## Local-model decision assistant (optional, operator-raised 2026-10-14)

Several of the decision points above, and a few elsewhere in this doc, share
a shape: today's design resolves them with one hardcoded default, or by
stopping and escalating to the operator, because the "right" answer needs
*some* judgment but not necessarily a human's. The operator's framing: a
configurable local open model (an existing `compute_node`/Ollama-style
backend — cheap, fast, no cloud cost, available even when the operator
isn't and even if a cloud API budget is exhausted) could make that judgment
call instead, **except where there is a genuinely hard requirement on the
operator** — which stays a hard stop, not something the assistant can
override.

This is named here as a cross-cutting **extensibility point**, not a fully
specified mechanism — the data model below is sized to let it be wired in
later without a rearchitect, which is deliberate: it does not need to be
built now to be designed for.

**Concrete hook points already named in this doc, all optional and
independently enable-able:**

1. **Empty-edge resolution** (open question 6). Today's default when
   `OnBlocked`/`OnFailed` is empty is a flat stop-and-alert. An assistant
   could read the blocked member's own failure/judgment evidence (the same
   `FailureReason`/`GuardrailVerdict` shape BL410's plan already defines)
   and recommend `skip_member` down a specific edge, `pause_automation`, or
   "this genuinely needs the operator" — the last of which still escalates
   exactly as today.
2. **`skip_member` pre-screening.** Instead of an operator manually
   deciding whether to bypass a stuck member, the assistant can pre-screen
   and recommend (audited as `AddedBy: "assistant:<model>"`, never silently
   auto-acting past a configured confidence threshold).
3. **Mid-flight `insert_member` triage** (open question 8, "may an agent
   insert members without operator approval"). The assistant is one
   concrete answer to what kind of "agent" that question means — it can
   recommend inserting a corrective member using the exact same
   `AddedBy`/`AddedReason` audit fields already specified for a session/agent
   insert, with the inserted Automaton still gated by its own normal
   `approved` requirement before the coordinator runs it.
4. **BudgetPolicy breach response** (open question 8's second half: pause
   vs. block). A single hardcoded default can't account for "90% done and
   $2 over" versus "10% done and already 3x over" — the assistant can read
   the Automation's own progress/spend state and recommend which, still
   bounded by the operator's own configured ceiling (it can only choose
   between the already-allowed responses, never raise the ceiling itself).

**What makes something a "hard requirement on the operator"** (not
delegable to the assistant, ever, regardless of configuration): anything
that destroys state or history (`cancel_in_flight: true`, `remove_member`
of a started member — already structurally disallowed above), raising a
`BudgetPolicy` ceiling, and any action a `guardrail profile`/quality gate
has itself marked as requiring human sign-off. The assistant's scope is
strictly "which already-allowed option to pick," never "whether to allow
an otherwise-disallowed action."

**Data model sizing for this (so it can be added later without a
rewrite):** `AutomationMember` and `Automation` should carry an optional
`DecisionAssistant` reference (empty = feature off, matching this doc's
general "ceilings only narrow, features only opt in" pattern) naming a
backend/model pair (reusing the existing LLM registry — no new backend
concept), a confidence threshold below which it must escalate instead of
deciding, and which of the four hook points above are enabled. Every
assistant-made decision is a normal `Decision` entry with
`actor: "assistant:<model>"`, so it reads identically to an operator or
agent decision in history — no new audit shape.

**Cross-reference BL437** (the recurring hardware-aware model/effort eval,
filed 2026-10-14): once this is built, the assistant's own judgment quality
at each hook point is exactly the kind of thing BL437's monthly sweep
should be testing — "is this model/effort combination actually good at
Automation-level triage decisions" is a real task shape, not just "is it
good at writing code." Name it as a candidate BL437 task-shape category in
that design doc, not duplicated here.

**A fifth hook point, with real evidence behind it (operator-raised and
live-tested 2026-10-15): task dispatch itself, for small/local-model
executors specifically.** Every "no output" failure investigated tonight
(BL427 Phase 2, BL431 Phase 1, BL464, three separate tasks) used the same
heavy standard task template — a multi-KB prompt carrying a full
CHECKPOINT PROTOCOL block, a "BEFORE YOU FINISH" block, and (on retry) a
prior-attempt-hint block, on top of the real instruction — plus opencode's
full default tool schema. A live, isolated side-by-side test (same model,
same real files, same real two-file edit task) showed this is not a minor
tuning knob: the heavy template produced a one-line plan followed by an
idle stall every time; a short, atomic, directive prompt (~700 bytes, no
boilerplate) with the tool permission set trimmed to only what the task's
own files/scope need (`permission` in opencode.json — deny-by-default,
allow only read/write/edit/bash/grep/glob) completed the identical task
correctly end-to-end — right line numbers, right edits, gofmt-clean,
verified, committed, and even self-flagged an unrelated file the model's
own `git add -A` had swept in — in about a minute, versus 90+ seconds just
to go idle on the heavy template, or several minutes to stall out
entirely. Speed and reliability moved together, not as a tradeoff — a
smaller prompt is cheaper to process *and* keeps the model from losing
the thread.

This should be a fifth assistant hook point: **on dispatch to a
small/local-model executor, decide the task's own shape** — (a) is this
task atomic/small enough to warrant the stripped template, or does it
genuinely need the standard scaffolding (long-running, needs mid-task
checkpointing)?, and (b) which tools does this specific task's declared
`files`/scope actually touch, so the dispatched `opencode.json` can trim
`permission` to just those instead of always sending the full schema.
**Refinement on (b), operator-raised 2026-10-15: decompose, not a
hardcoded list, should be the source of which tools a task needs.** A
hand-picked global allow/deny list risks blocking something a specific
task genuinely needs (the real opencode tool set — confirmed against
opencode's own config schema — is `read, edit, glob, grep, list, bash,
task, external_directory, todowrite, question, webfetch, websearch, lsp,
skill`; a naive guess missed `list` and `todowrite` when this was tried
live tonight). Decompose already has full context on what a task is
supposed to do — it should emit a per-task `tools_needed` list (or the
inverse, `tools_not_needed`) as part of its normal planning output, which
flows through to the dispatched `permissions` block. The interim ship
tonight does NOT do this — it only denies the 2 tools that are safe to
deny for *any* one-shot autonomous task regardless of content (`task`,
since recursive sub-agent spawning is pure risk/redundant with datawatch's
own orchestration; `external_directory`, which only reinforces the scope
boundary already stated in every task prompt). Nothing else is touched.
Decompose-emitted per-task tool lists are the real version of this hook
point; the interim narrow deny-list is deliberately conservative until
that exists.

Then, symmetrically, **live stall detection should reroute, not blindly
retry**: a session showing the "awaiting_input with zero tool calls"
signature (or the harness-crash signature from BL410's classifier) should
have its *next* attempt dispatched with a corrected shape — smaller/more
atomic prompt, trimmed tools, or escalated to a stronger model — rather
than retried with the identical oversized template a 2nd or 3rd time,
which is exactly what produced every failure tonight. This reuses the same
`DecisionAssistant`/confidence-threshold/audit shape as the other four
hook points; it just fires at dispatch and at retry-selection time instead
of at an edge/skip/insert/budget decision point.

**Not open-model-specific (operator-raised 2026-10-15).** Nothing about
this fix is tied to local/open models — the mechanism (task atomicity,
tool-scope trimming) applies to any smaller-tier executor, including a
Claude haiku executor (the Sunday post-reset plan already in Claude's
memory: adding haiku as a second parallel executor alongside qwen3.8:27b).
The dispatch-time hook point above should key off executor *tier*, not
backend family — "is this executor a smaller/cheaper model that benefits
from a trimmed task" applies identically whether that executor is local
opencode or Claude haiku.

**Upstream of dispatch: decompose itself needs the same awareness, and
existing queued PRDs are affected (operator-raised 2026-10-15).** The task
granularity and task-spec text produced by decompose today assumes the
full standard template regardless of which executor will run it — a task
planned for a small/local model gets the same multi-paragraph spec shape
as one planned for Claude. Once this dispatch-time trimming exists, the
better fix is upstream: when a PRD/story/task is known (or likely) to be
routed to a small/local-model executor, decompose should plan *smaller,
more atomic* tasks in the first place (one tool-call chain, one checkable
outcome per task — matching the "atomic step" framing BL405/this doc's
Model-tier defaults section already groups by role) rather than relying on
dispatch-time trimming alone to rescue an oversized task. Concretely: every
PRD/task currently queued or blocked on open-model execution (BL427 Phase
2, BL431 Phase 1, BL464, and any others in the Stage A/C queue pinned to a
local-GPU profile) was decomposed before this finding existed, under the
old assumption — once dispatch-time trimming and/or decompose-time
atomic-task planning actually ship, those PRDs are real candidates for
re-decomposition, not just a retry with the same task boundaries. Do not
re-decompose any of them proactively before that lands; this is a
forward-looking note for when it does.

**Two distinct routing layers, named explicitly so they don't get
conflated (operator-raised 2026-10-15).** Everything in this section so
far — dispatch-time trimming, stall-triggered reroute, decompose
granularity — is **intra-PRD**: routing and retry/retest *within* one
PRD's own stories/tasks, using that task's own verification feedback to
adjust and loop before anything escalates further. This is a different
layer from the rest of this document, which is about **inter-PRD**
routing: the Automation coordinator deciding which *member* (a whole PRD)
runs next, skips, gets inserted, or triggers a budget response. The two
need to compose, not merge into one mechanism:
- **Intra-PRD** (new scope, named here): a task/story that fails its own
  verification should be able to enter a bounded "adjust and retest" loop
  *inside* the same PRD — not just "retry the identical dispatch" (BL410's
  existing flat retry-count budget) but genuinely vary the approach each
  iteration (smaller sub-task, trimmed tools, a different hint derived
  from the specific verification failure) before exhausting and only then
  surfacing to the PRD/Automation level. The dispatch-time hook point
  above is the first concrete piece of this; it should grow into a real
  bounded loop construct, not stay a single reroute-on-retry.
- **Inter-PRD** (already this doc's main scope): once a PRD itself reaches
  a terminal state (including "intra-PRD loop exhausted"), the Automation
  coordinator's existing hook points (empty-edge resolution, skip_member,
  insert_member, budget response) decide what happens at the
  whole-PRD-member level.
A PRD that exhausts its own intra-PRD loop is exactly the signal the
inter-PRD layer's empty-edge/skip_member hooks already consume today —
no new signal shape needed at the boundary, just a clear scope line
between "retry smarter inside this PRD" and "route around this PRD
member."

This is intentionally not resolved into an implementation plan in this
design pass — see Open questions, new item 15.

---

## Cost / budget — BudgetPolicy

```go
type BudgetPolicy struct {
	CeilingType  string  `json:"ceiling_type"`  // "dollar" | "allocation_pct"
	CeilingValue float64 `json:"ceiling_value"` // USD, or percent (0-100) of the window's allocation
	Window       string  `json:"window"`        // "automation" (lifetime) | "day" | "week" | "month" — BL428 llm.<name>.budget windows + lifetime
	Pacing       string  `json:"pacing"`        // "none" | "even" — see below
	OnBreach     string  `json:"on_breach"`     // "pause" (default) | "block"
	Backend      string  `json:"backend,omitempty"` // optional: count only this backend's spend; default all
}
```

**What it narrows.** BL428 §4 defines the base mechanics:

- an API/dollar ceiling, `llm.<name>.budget` = `{window: day|week|month, ceiling_usd}`,
  enforced as a BL405-Phase-2-style windowed ledger pool `budget:<backend>`;
- the subscription meter, which cannot be read directly (§4a: "Remaining quota
  cannot be queried");
- `plan_tier` and `max_inflight` (`llm.<name>.plan_tier`);
- the reactive `backoff:<backend>` flag (`autonomous.usage_backoff`);
- the dispatch gate in the executor's `admit` (`internal/autonomous/capacity.go:127`),
  which turns a ceiling into a **wait** (`TaskWaitingCapacity`), not a failure.

BudgetPolicy does **not** add a second meter or a second admission path. It is an
extra, **narrower ceiling** checked at the same points, scoped to this Automation's
members. These names come from the BL428 **doc**. That doc exists, but the
mechanism is not implemented, so the names are design-level. Reconcile them with
the BL428 implementation PRD.

**Resolution precedence:** explicit member `BudgetPolicy` > Automation
`BudgetPolicy` > global BL428 ceilings (`llm.<name>.budget`, and allocation via
back-off). This generalizes BL428's per-backend precedence with an Automation tier.
Because these are ceilings, the effective limit is the **tightest** applicable one.
A member or Automation policy can only narrow the global ceiling, never widen it.

**Example.** "This overnight release sequence should cost ≤ $40, or ≤ 15% of this
week's allocation":

- `{ceiling_type: dollar, ceiling_value: 40, window: automation, pacing: none, on_breach: pause}`, or
- `{ceiling_type: allocation_pct, ceiling_value: 15, window: week, pacing: even}`.

**Federation forward pointer (operator-raised 2026-10-14, added to BL428's
doc §3c/§4e, not designed here).** BL428's global ceilings above assume one
daemon's own view of its usage is the whole picture. It is not, when this
daemon is federated with a peer that shares the same `claude-code` account
(the same subscription or API key) or the same physical compute hardware
under a different `compute_node` name — BL428 §4e/§3c design a keyed-hash
fingerprint exchange so peers can detect this *without* exchanging the
underlying secret or hardware identifier, confirmed by the operator, then
coordinated live (lease/back-off propagation) while sessions are actually
running. If BL428's `billing_group`/`SharesHardwareWith` linkage exists by
the time this PRD's BudgetPolicy ships, the precedence chain above should
read a federated peer's shared usage into the same `tightest-wins` ceiling
check — a $40 Automation ceiling means $40 across every daemon drawing on
that billing group, not $40 per daemon. This PRD does not design that
read-through; it is named here so the follow-up implementation PRD does not
have to rediscover the need for it.

With `pacing: even`, the coordinator does not *start* the next member while
spend-to-date exceeds `ceiling × elapsed_fraction(window)`. It waits and shows the
reason, so a burst early in the week cannot eat the whole share. `none` spends
freely up to the ceiling.

**Where it is enforced (two points, both existing gates):**

1. **Advance gate.** This lives in the coordinator, before `API.Run` of the next
   member. If the Automation is at or over its ceiling, the member is not started.
2. **Dispatch gate.** This is BL428's `admit`, for tasks of the running member.
   - When the member's Automation is over its ceiling, new tasks wait as
     `TaskWaitingCapacity` with the reason "automation budget reached".
   - Tasks already in progress finish. This is the same "let in-flight finish"
     rule as pause.

**On breach:**
- **Default `pause`:** the Automation becomes `paused`, a `budget_breach` Decision
  and an audit entry are written, and `alerts.Store.AddSystem(LevelWarn, …)` raises
  an alert (`internal/alerts/store.go:144`). It resumes automatically when a
  windowed policy rolls over, or by hand after the operator raises the ceiling.
- **`block`:** same, but the Automation is `blocked`. It never auto-resumes and
  needs an explicit operator decision.

Open question 8.

**Attribution (per-Automation spend).** Spend is the sum of member spend inside the
window. Member spend is the sum of `EstCostUSD` (`internal/session/cost.go:77-82`)
over every session that ran the member's tasks (`Task.SessionID`, including verify
sessions), plus decompose sessions. That is the **BL414** rollup, whose
`Decision.CostUSD` has zero writers today (`models.go:57`, README item 14). The
follow-up must use **one shared** `PRDCostUSD(prd, since)` helper, built as BL414's
rollup or as BL428 Phase 5's, rather than a third private join.

`allocation_pct` cannot be measured directly, because the subscription meter is
opaque (BL428 §4a). It is estimated as the members' token usage priced at API rates
(the same `EstCostUSD`), divided by an operator-declared notional allocation for
the backend's `plan_tier`. It is labelled **"estimated"** everywhere it appears. The
hard signal stays BL428's reactive `backoff:<backend>`. Open question 9.

---

## Multi-model / cost-tier defaults

Extend the **existing** chains with exactly one tier. The order becomes
**task > story > PRD (Automaton) > Automation > global**, for both:

- **Execution.** `resolveTaskBackendModel` (`internal/autonomous/executor.go:1607`)
  gains an `auto *Automation` parameter (nil when the PRD is not a member), checked
  after `prd.Backend` and before `fallbackBackend`:
  - `case auto != nil && auto.Backend != "": backend = auto.Backend; if model == "" { model = auto.Model }`;
  - in the `default` branch, `auto.Model` joins the model fallthrough after
    `prd.Model`.
  - Effort (`executor.go:831-836`) adds `auto.Effort` after `prd.Effort`.
  - The per-unit setters are unchanged: `autonomous_prd_set_task_llm`,
    `_set_story_llm`, `_set_llm` (`API.SetTaskLLM` / `SetStoryLLM` / `SetPRDLLM`,
    `api.go:602-625`).
  - A new `automation_set_llm` sets the Automation tier.
- **Verification.** `resolveVerificationLLM`
  (`internal/autonomous/verification_llm.go`, BL430 / `4d0ad909`) gains the same
  tier:
  - a new case after `prd.VerificationBackend`, with the source being a **new
    constant `VerifierSourceAutomation = "automation"`** next to
    `task|story|prd|global|pool`;
  - the model fallthrough and the effort chain each gain the Automation level after
    the PRD level.
  - The `pool` behaviour is unchanged: an explicit backend at any level, now
    including the Automation, bypasses the `verification_backends` pool, exactly
    as an explicit per-unit backend does today.

Both rules are kept verbatim. The **same-level backend+model rule** means an
Automation's model never pairs with a PRD-level backend. The **independent effort
rule** is unchanged. Task results already record `verifier_source`, so a verdict
shows `automation` when the Automation tier supplied the verifier. A matching
`executor_source` is added to the task's run record (Open question 12) so execution
provenance is just as visible.

**BL422 (`f6a38c29`).** The Automation-level default is where an operator applies
BL422's tier recommendation to **a whole batch at once**. For example, "every
member of this doc-heavy Automation executes on sonnet/medium and verifies on
haiku/low", while the individual tasks BL422 flags as hard keep task-level
overrides. That replaces the N × `set_llm` calls, or BL422's transient
global-config flip, used today. The decompose backend (`PRD.DecompositionModel`)
is not part of this chain and stays PRD-level. Whether to add an Automation tier
for decompose is Open question 11.

---

## Trigger mechanism

**The Automation coordinator is a consumer of BL427's event dispatch** (`fe2e2110`).
It uses BL427's simplest subscriber type, an **in-process Go callback**: no webhook,
no session-push transport, no persisted subscription. At startup it calls
`dispatcher.Subscribe(Filter{Kinds: terminal, Level: prd}, coord.onMemberTerminal)`.
That is the exact shape in the BL427 doc's Recommendation item 3, and the names are
provisional until BL427 is implemented. The terminal set is `prd.completed`,
`prd.blocked`, `prd.failed` and `prd.cancelled`. `prd.status_changed` is also
subscribed, filtered to members only, to catch the revert-to-`approved` and
`archived` cases in the edge table. The coordinator **does not add a second hook**
into the `PRDCompleted` / `PRDFailed` / `PRDBlocked` write path in
`executor.go:578-677`.

**Ordering fallback:**

- **If BL427 ships first** (the operator-confirmed order BL427 → BL428 → BL431,
  README "2026-10-14 sequencing addendum"), build on its dispatcher.
- **If BL431 ships first,** build a **minimal temporary hook**:
  - one `m.notifyTerminal(prd, old, new)` call next to the roll-up and cancel
    status assignments (`executor.go` roll-up, `api.go:399` `Cancel`);
  - it delivers to a single registered func **asynchronously** (`go fn(...)`), so
    the coordinator never runs inside the executor goroutine or under the Manager
    mutex;
  - it is marked in a code comment `// TEMPORARY(BL431): migrate to the BL427
    lifecycle dispatcher`, and its removal is a BL427 Phase 1 checklist item.

**On an advance** (`onMemberTerminal`, under the per-Automation lock):

1. Ignore events for PRDs whose `AutomationID` is empty, or whose member is not the
   cursor.
2. Record the member's `Status`, `FinishedAt`, `Outcome` and `EdgeTaken`. Pick the
   edge from the mapping table.
3. If the edge is empty: become `completed` or `blocked` (see the table) and alert.
4. If the Automation is `paused`: move the cursor, but do not start. Set
   `IdleSince`.
5. Check the BudgetPolicy advance gate.
6. **Start the next member only if it is cleanly `approved`** (or `paused`, for the
   rule-2 interject case: `API.Resume`).
   - Start it by calling **`API.Run(prdID)` in-process** (`api.go:291`). That is
     the one code path that launches `Manager.Run` with the spawn/verify callbacks
     and the `runCancels` idempotency. It is **never** a REST loopback like BL117's
     `prdRun` (`cmd/datawatch/main.go:5593`).
   - Otherwise (`needs_review`, `draft`, `planning`, `revisions_asked`), become
     `blocked` with the reason "next member <name> is <status>". Raise
     `alerts.Store.AddSystem` (`internal/alerts/store.go:144`; stored in
     `<data_dir>/alerts.json`, `cmd/datawatch/main.go:1856`), and BL427's comm
     fan-out once that exists.
7. Append Decisions to the Automation and to the started member, write audit, and
   emit a PWA update.

No LLM call and no chat session is involved anywhere in this path. That directly
removes the failure mode of the overnight stall.

**Daemon restart and resume.** `resumeRunningPRDs` (`api.go:81-150`) relaunches
`running` members and recovers daemon-killed `failed` ones. **After** it finishes,
the coordinator reconciles every `active` / `paused` Automation:

- If the cursor member's PRD is terminal but the member is still `running`, the
  member finished while the daemon was down. Apply the same advance logic. The
  compare-and-set makes this safe if a live event also arrives.
- If the cursor member's PRD is `running`, do nothing. Boot-resume owns it.
- If the cursor member is `pending` and the Automation is `active`, it was about to
  start when the daemon died. Retry the start through the approval gate.
- If the member was started (`StartedAt` set) but its PRD is `approved` and not
  running, become `blocked` with the reason "member run errored".

**`autonomous_status` visibility.** Extend `LoopStatus`
(`internal/autonomous/models.go:809`):

```go
Automations []AutomationStatusSummary `json:"automations,omitempty"`
// AutomationStatusSummary{ID, Name, Status, CurrentMemberID, CurrentPRDID,
//   CurrentPRDTitle, PendingMembers int, IdleSince *time.Time, IdleSeconds int64,
//   BlockedReason string, SpentUSD float64, CeilingDisplay string}
```

`IdleSeconds` is the time since a member finished with nothing started. The
02:23-07:37 stall would have shown up as an `active` Automation idle for 5 hours
with a reason. An optional `autonomous.automation.idle_alert_minutes` raises an
alert when an `active` Automation stays idle past that threshold.

---

## PWA

**Working assumption: a new top-level "Automation" nav tab, placed LEFT of the
existing Automata tab.** The existing tab is `navBtnAutonomous`,
`data-view="autonomous"`, label key `nav_automata`, in
`internal/server/web/index.html` (~lines 131-134). The bigger unit of work reads
first, left to right:
`Sessions | Automation | Automata | Alerts | Observer | Dashboard | Settings`.

- The new button is `navBtnAutomation`, `data-view="automation"`, `data-i18n="nav_automation"`.
- It is hidden (`display:none`) until `/api/autonomous/config` reports enabled,
  following the same v5.26.8 gating as `navBtnAutonomous`.

The operator is asked to confirm this ordering in Open question 1.

**(a) List view.** Same shape and conventions as `renderAutonomousView`
(`internal/server/web/app.js:21855`):

- an `output-tabs` bar;
- `automata-filter-badge` status filters, with active statuses `draft`, `active`,
  `paused`, `blocked` and `completed` always visible, and `cancelled` behind the
  history toggle;
- the Select / Filter / History action buttons (`automata-action-btn`).

Cards reuse `prd-row prd-card prd-card-status-<status>`, so the left-border colour
works unchanged, as in `renderAutomataCard` (`app.js:19150-19224`). A card shows:
- the name and status;
- progress, e.g. "3 / 7 members";
- the current member's Automaton title;
- the idle duration while idle;
- the blocked reason;
- a spend / ceiling bar when a BudgetPolicy is set.

**(b) Detail view: a pipeline strip.**

- **Layout.** A horizontal strip of member cards in edge order: start from
  `EntryMemberID` and follow `OnComplete`.
- **Member card.** Each card has the Automaton name, an 8-hex id chip and a
  **status-coloured dot**: pending (grey), approved (blue), running (green, pulsing),
  blocked (amber), completed (check), failed (red). "Approved" is derived for a
  pending member whose PRD is approved.
- **Active member.** It is highlighted with an accent ring.
- **Click-through.** A card opens that Automaton's existing detail page
  (`renderPRDDetailView(id)`, the same handler `renderAutomataCard` uses).
- **Branch points.** A member with **more than one non-empty outcome edge** is
  drawn distinctly from a straight line.
  - It gets a fork glyph (⑂) on the card's right edge.
  - Below the main line it gets a **stacked sub-row per extra edge**, labelled "if
    blocked →" / "if failed →", and that edge's target chain renders in the
    sub-row.
  - A straight segment is a plain connector arrow.
  - The edge actually taken is drawn solid. Edges not taken are drawn dashed and
    dimmed after the fact.
- **Inserted members.** They carry a small "inserted" marker, with
  `AddedBy` / `AddedReason` / `AddedAt` in the tooltip and the detail drawer.
- **Completed members.** They render read-only.
- **Narrow screens.** At phone width the strip becomes a vertical list, and the
  sub-rows become indented blocks.

**(c) Create/edit flow (v1).**

- A form takes the name and description.
- An Automaton picker searches `/api/autonomous/prds` by name or ID. It excludes
  templates, child Automata and members of another active Automation, and greys
  them out with the reason.
- Picking several Automata in order produces the default **linear** wiring.
- Each pending card has:
  - three edge selects (`on complete` / `on blocked` / `on failed`), each offering
    another member or "stop and alert";
  - **insert before** / **insert after** buttons that open the picker plus a
    **required reason** field (reason is optional only in draft);
  - remove.
- The optional BudgetPolicy fields are: type, value, window, pacing, on-breach.
- The optional execution and verification model-default rows reuse the existing
  Verification LLM row components from the Automaton detail page.
- Every edit sends `expected_version`. A `409` shows a stale-version prompt with
  the server's message and a reload button; nothing is retried, and the page
  reloads only when the user chooses to.

A **drag-and-drop graph canvas is explicitly NOT v1.** It is a possible v2
enhancement once the edge model has been proven in use.

**Locale-bundle keys** (all five bundles in `internal/server/web/locales/`):
- `nav_automation`;
- `automation_status_{draft,active,paused,completed,blocked,cancelled}`;
- `automation_member_status_{pending,approved,running,blocked,completed,failed,skipped,cancelled}`;
- `automation_edge_on_{complete,blocked,failed}`, `automation_edge_stop`;
- `automation_insert_before`, `automation_insert_after`,
  `automation_insert_reason_label`, `automation_inserted_by`;
- `automation_pause`, `automation_resume`, `automation_cancel`,
  `automation_cancel_in_flight`, `automation_skip_member`;
- `automation_budget_{ceiling,window,pacing,on_breach}`, `automation_budget_estimated`;
- `automation_idle_for`, `automation_blocked_reason`, `automation_new`.

Rules for these strings:
- **No BL ids in any user-facing string.** `scripts/check-no-internal-refs.sh`
  enforces this.
- "Automaton/Automata" stays as the per-locale forms in AGENT.md's table.
- AGENT.md's Terminology Rule currently says "never use 'Automation' (a different
  word)" for an *Automaton*. That rule still holds, and it needs an addendum
  defining "Automation" as this separate object, with per-locale translations.
- The existing FAB label "Launch Automation" (`app.js:2057`, key `fab_launch_auto`)
  launches an *Automaton*, so it will collide visibly. See Open question 7.

The BL117 Settings → Orchestrator sub-tab is retired along with BL117 (see
**Recommendation on BL117**).

---

## Observability: Dashboard and Observer integration (operator-raised 2026-10-14)

A live retrospective pulled 2026-10-14 (story/task counts, files touched,
wall-clock time per task, cost per session) across today's own PRDs turned
up real, immediately-useful numbers (e.g. release/doc tasks run 2-3x longer
than pure code tasks; files-touched tracks scope better than task count)
and, as a direct side effect of just looking, surfaced two silently-dropped
PRDs (BL433 blocked 6 hours, BL431 itself cancelled with one task left)
that nothing had flagged. The operator's own conclusion: this is exactly
dashboard material, and it should not live only in a new Automation tab —
it should extend the **existing** Dashboard and Observer systems, which
already have a real, extensible mechanism for this, not be a fourth
parallel thing.

**Existing extensibility, confirmed by reading the code, not assumed:**
- **Dashboard**: `internal/server/api_dashboard_cards.go`'s `dashCard{ID,
  CS, RS, System}` layout (`GET/POST/PUT/DELETE /api/dashboard/cards`) is
  already a generic, ID-keyed card registry — `defaultDashCards()` lists
  `tree`, `orbital`, `events`, `sparklines`, `gantt`, `heatmap`,
  `guardrails`, `ekg`, `smoke`, `memory-scope`. A new card is adding one ID
  to this list plus a PWA renderer for it — the same shape `memory-scope`
  used for BL387 Phase 3. No new card-layout mechanism is needed.
- **Observer**: `internal/observer/types.go`'s `Envelope{ID, Kind, Label,
  CPUPct, RSSBytes, GPUPct, GPUMemBytes, ...}` already rolls up resource
  usage by `EnvelopeKind` (`session | backend | container | system`). It
  has no `automation` kind and does not need one: an Automation's resource
  cost is the **sum of its member tasks' existing `session:<id>` envelopes**
  — a client-side or server-side rollup over data the observer already
  collects, not a new attribution mechanism inside the observer itself.

**Three surfaces, one shared data source — not three parallel
implementations:**
1. **A new Dashboard card**, e.g. `automation-summary` (or
   `automation-pipeline` for a compact per-Automation strip), added to
   `defaultDashCards()`. Shows the same retrospective numbers pulled live
   today: tasks/stories/files/time/cost per Automation, and per-member
   status. Pulls from the same task/story/PRD/Decision/`cost_summary`
   fields named in BL440's "already-available data" map (see
   docs/plans/2026-10-14-status-tab-session-detail-gap.md once it lands) —
   reuse that data path, don't build a second one.
2. **Observer rollup by Automation.** Given an Automation's member PRD
   IDs → their tasks' `SessionID`s → the matching `session:<id>` envelopes
   already in `/api/observer/envelopes`, sum CPU/RSS/GPU per Automation.
   This can be computed entirely from existing data (Automation membership
   + existing envelope IDs); it does not require teaching the observer
   itself about Automations. Surfaces as an optional grouping/filter on the
   existing Observer tab, not a new tab.
3. **BL431's own Automation tab** (the pipeline-strip detail view,
   described above under `## PWA`) embeds the SAME summary component the
   Dashboard card uses — stats bar above or beside the pipeline strip, not
   a separately-built view. One component, rendered in three places
   (Dashboard card, Observer rollup panel, Automation detail page).

**What this explicitly does NOT require:** a new `EnvelopeKind`, a new
dashboard-layout mechanism, or a second data-aggregation path. Every
number named above already exists somewhere in the daemon today (tasks,
decisions, verification results, session envelopes, cost summaries) — this
section is about wiring three presentation surfaces to data that already
exists, mirroring BL440's own "wire existing data before building new
collection" approach for the session Status tab.

**Staleness/alerting as a side benefit.** The BL433/BL431 drops found live
today were only caught because an operator happened to ask for a
retrospective. A dashboard card surfacing "last touched" / "idle duration"
per Automation and per ungrouped-but-stuck PRD would have caught both
automatically — this is a concrete, low-cost argument for building the
dashboard card early rather than treating it as cosmetic. Cross-reference
BL427 (event dispatch could push a staleness alert instead of requiring a
dashboard poll, once its later phases ship) and the existing
`autonomous_status`/`get_stats`/`datawatch stale` surfaces already used
manually this session for exactly this kind of check.

---

## Progress, estimates and the running dashboard (operator-raised 2026-10-21)

**Requirement.** Everything the supervisor prints in the `scripts/v10-status.py` table is a product
feature of the Automation, not a supervisor script. As the Automation builds, it keeps these numbers
current from the data it already owns, and as PRDs and tasks are added the averages and estimates
move on their own. The operator should see one running dashboard (PWA card, MCP, REST, CLI, comm
verb), not a report that someone regenerates. The script is the reference oracle: the server's output
for a fixture store must equal the script's.

**What it computes, per Automation and for the whole store (scope selectable: an Automation, a
label or tag, a date range, or "everything since date X")**

1. **Counts.** PRDs and tasks, each split by kind (feature, bug, other) and by state (done, running,
   waiting, blocked). Tasks done and tasks left. A cancelled PRD whose tasks all completed is merged
   work whose record was closed: it counts as done. Other cancelled, rejected and archived PRDs are
   not counted.
2. **Kind.** An explicit `kind` field on the PRD (feature, bug, other), set at create time and
   editable. The keyword heuristic in the script is only the migration default for old PRDs.
3. **Two clocks, kept apart (operator, 2026-10-21).**
   - **Lead time (state tracking):** created, approved, first task start, last task complete, merged
     or delivered. Per-stage durations (approval wait, queue wait, run time, merge wait) are shown for
     every PRD and as medians. This is useful as a state to track from created or approved to
     delivery, and it is where waiting and hand-offs show up.
   - **Queue time (the running queue):** only the start and stop of each task counts. Time to
     completion is estimated from task service time and real, observed completion rates, not from lead
     time. Lead time never feeds the completion estimate; waits are reported as their own stage.
   Details of the second clock:
   - Task time: `started_at` to `completed_at`. Per-PRD work is the sum of its tasks.
   - PRD time: first task start to last task completion. **Not** `approved_at` to `updated_at`:
     that includes weeks of waiting and every later edit, cancel or registry cleanup, and
     inflated the first version of the estimate about 15x.
   - Report medians next to means; a few outliers dominate the means.
4. **Throughput.** PRDs and tasks completed per day, taken from the completion timestamps of the
   last task (never from record update times), over windows of 24 hours, 3 days and the whole period.
5. **Utilization.** Worker-hours done per day divided by lane capacity (lanes x 24). This is the
   number that explains why the lanes' raw speed is not the limit.
6. **Remaining work, without double counting.**
   - Known: tasks still open in PRDs that exist and are decomposed (an exact count).
   - Estimated: PRDs not yet decomposed, and backlog items that have no PRD yet, each at the
     measured tasks-per-PRD for its kind (mean and median, with a spread).
   - The two are shown separately and never summed into one unlabelled number. A backlog item that
     already has a PRD is counted once, through the PRD.
   - Needs a machine-readable backlog: each backlog row carries an id, kind, linked PRD ids and a
     phase count, so the dashboard does not parse prose.
7. **Estimated time to completion as a range, with its method shown.** By PRD rate, by task rate
   (overall and last 24 hours) and by worker-hours at current utilization. The dashboard shows the
   range and which assumption moves it (utilization, mix of large PRDs, undecomposed backlog).
8. **History.** A snapshot is stored daily and on each milestone so the estimate has a trend line
   and the operator can see whether it is converging.

**Stage ledger: every stage is timed, not just the build (operator, 2026-10-21).** Each PRD and
each task carries start and end timestamps for every stage, so the dashboard can show where time
goes and the optimizer can act on it. Stages, with what exists today and what must be added:

| Stage | Start event | End event | Today |
|---|---|---|---|
| Created and spec written | `created_at` | decompose requested | `created_at`; request time not stored |
| Decompose | planning mark (status planning) | `decompose` decision | end only; add planning-start timestamp and session id |
| Plan validation | decompose done | `approve` decision | `plan_coverage_ack` and `approve` decisions exist; supervisor validation start/end and plan-review result must be added |
| Approval wait | plan ready (needs_review) | `approve` | derivable |
| Queue wait | `approve`/`run` | first task spawn | `run` decision and task `started_at` |
| Per task: spawn and delivery | session created | task text delivered | `session.json` has `task_delivered_at`; copy onto the task |
| Per task: work | delivered | worker turn ends | add: first output time, turn-end time, idle dwell after turn end |
| Per task: nudge or stall handling | turn end without completion marker | marker seen or nudged or killed | add: nudge count and time |
| Per task: verification | verifier spawn | `verification.verified_at` | end only; add verifier start and session id |
| Per task: retries | each failed attempt | next attempt | `retry_count`, `prior_attempt_count`; add per-attempt durations and failure reason |
| PRD completion review | last task done | PRD guardrail verdict | add timestamps |
| Merge wait and merge | PRD completed | branch merged | not in the store; add `merged_at` and merge duration from the merge event |
| CI and release | tag pushed | release workflow green | not in the store; add from `ci_watch` events |
| Deployed | release green | daemon running that version | add: daemon version change event |

| Operator wait (whole system) | an explicit `operator_wait_start` event: the supervisor or a PRD asks the operator and stops | `operator_wait_end` when the answer arrives | not recorded; the status script infers it from 3 h or longer gaps with no task activity (15 such gaps, 137 h, about half of calendar time since 2026-10-10) |

**Operator waits are tracked, not zeroed (operator, 2026-10-21).** Each wait carries a `kind`:
`design` (a design decision only the operator can make) or `build` (a build, release or merge
decision that is the supervisor's to make). Rates, throughput and utilization are computed on active
time, so one overnight wait does not make the lanes look slow. The dashboard also shows the waits
themselves, the share of calendar time they took, and a calendar estimate that includes that share.
A `build` wait is a supervisor defect and is reported as one; `design` waits are expected. Until the
events exist, inactive windows are inferred from task activity and labelled as inferred.

The dashboard shows each stage's median and p90 per kind, the share of lead time each stage takes,
and the slowest stage. Anything that cannot be timed yet is shown as "not measured", never as zero.

**Optimization inputs (so queue and lane decisions can be made from live data, not guesses).**
Per lane: busy, idle, waiting-on-nudge and blocked time, split by lane type. Per task: work time
against idle dwell after turn end, nudge count, retry count and wasted attempt time. Per PRD:
critical-path length against task count (potential parallel speedup), and which tasks are
independent. Queue: number of validated, approved PRDs waiting per lane type (decompose-ahead
depth), blocked-reason histogram, time from "completed" to "merged". Capacity: GPU engines in use
against the limit, model load events, Claude session and weekly usage against pace. Quality: first-
pass verification success rate per backend and model, retry rate per backend.

**Lessons recorded from the script (2026-10-21), to become tests**
- Measuring PRD time from approval to last update overstated it by an order of magnitude.
- Counting PRDs touched in the last 24 hours as throughput counted the cleanup actions themselves.
- Adding open backlog rows to PRDs that already exist double counted them (59 vs the real 78 total
  was wrong in both directions).
- Existing-but-undecomposed PRDs have no tasks yet; their size is an estimate and must be labelled so.

**Phasing.** The data comes from the Automation member list and the lifecycle events, so it follows
Phase 2 (coordinator). Compute and REST/MCP/CLI/comm in one phase, PWA dashboard card with the
Phase 6 PWA work, daily snapshots with Phase 5 BudgetPolicy (it shares the cost rollup). Filed as
BL553 (docs/plans/README.md row 134).

## Parity surface

Phase 2 (development build) has shipped part of this table: activate on REST, MCP,
CLI and comm channel; the `automations[]` array on `autonomous_status` (REST and MCP);
and the `autonomous.automation.idle_alert_minutes` key on every config surface including
the PWA Settings field. Phase 5 (development build) has shipped the budget slice: REST
`set_budget`, MCP `automation_set_budget`, CLI `automation set-budget`, comm `automation
set-budget` and the budget lines of `automation status`, and the `budget` view on the Automation
read and on the status rows; it added no config key (see the YAML/config row). The rest of the
table is still the plan for Phases 3-7, and the table below remains accurate as written, with the
corrections in the rows themselves. Every surface follows AGENT.md's
Configuration Accessibility Rule and the full parity-surface set.

| Surface | Included? | Concrete design |
|---|---|---|
| **REST** | Yes | `GET/POST /api/automations`; `GET/PUT/DELETE /api/automations/{id}` (delete only in `draft` / terminal); `POST /api/automations/{id}/{activate\|pause\|resume\|cancel\|skip_member\|insert_member\|remove_member\|reorder_edges\|set_llm\|set_verification_llm\|set_budget}`; `POST /api/automations/import-graph/{graph_id}`. Same `fedCap` gating as the autonomous handlers (`CapAutonomousRead` / `Write` / `Run`). `409` on invalid state or version mismatch. Documented in `openapi.yaml`. |
| **MCP** | Yes | `automation_list`, `automation_get`, `automation_create`, `automation_update`, `automation_activate`, `automation_pause`, `automation_resume`, `automation_cancel` (`cancel_in_flight` flag), `automation_skip_member`, `automation_insert_member`, `automation_remove_member`, `automation_reorder_edges`, `automation_set_llm`, `automation_set_verification_llm`, `automation_set_budget`, `automation_import_graph`. Each is added to `internal/federation/mcp_tool_caps.go`. `autonomous_status` is extended with `automations[]`. The `orchestrator_graph_*` tools are deprecated (see the BL117 recommendation). |
| **CLI** | Yes | `datawatch automation list\|get\|create\|activate\|pause\|resume\|cancel [--cancel-in-flight]\|skip <member> --via on_failed --reason …\|insert <id> --after <member> --prd <id>[,<id>…] --reason …\|remove\|edges\|set-llm\|set-verification-llm\|set-budget\|import-graph`. |
| **Comm channel** | Yes (control + status) | `automation list`, `automation status <id>`, `automation pause\|resume\|cancel <id> [reason]`, `automation skip <id> <member> <edge> <reason>`, `automation insert <id> after <member> <prd> <reason>`, `automation set-budget <id> key=value...` (built in Phase 5; `clear=true` removes the policy; the arguments are the REST fields). Create and edge rewiring are excluded from comm because they are graph editing and do not fit one line; use the PWA, CLI or MCP. Advance, blocked and budget-breach notifications go through BL427's comm fan-out, not a separate path. |
| **YAML/config** | Yes | Provisional keys under `autonomous.automation`: `enabled` (bool); `idle_alert_minutes` (int); `empty_edge_action` (`block`\|`pause`, Open question 6); `agent_insert_policy` (`allow`\|`require_operator`, Open question 8); ~~`default_budget_on_breach` (`pause`\|`block`)~~ (not built: Phase 5 added no config key, and `on_breach` defaults to `pause` on the policy itself). Each is in `handleGetConfig` / `handlePutConfig`, `docs/config-reference.yaml`, `configure <key>=<value>` and `datawatch config set`. Automation objects themselves are runtime data in `automations.jsonl`, not YAML. |
| **PWA** | Yes | The Automation tab left of Automata; list, pipeline-strip detail and create/edit (see **PWA**); the config fields in Settings → General (autonomous section); locale keys in all bundles. |
| **Android** | Yes, via a parity issue | A `datawatch-app` issue: Automation list + strip detail + pause/resume/cancel/skip; create/edit may lag one release. Filed in the companion app repo by the implementation PRD, not here. Phase 5 adds: parse the `budget_policy` fields, the `budget` view, the status `budget` / `spent_usd` / `ceiling_display` fields and the paused or blocked budget reason. **Not filed yet** (cannot be filed from the implementing session). Phase 7: the ready-to-file text is under `### Phase 7 companion apps` (Android issue); still **not filed**. Phase 8 (progress card and Automaton kind): the ready-to-file text is under `### Phase 8 companion apps` (Android issue); **not filed**. |
| **iPhone/iOS** | Yes, via a parity issue | Same scope as Android, in the companion app repo's iOS track; the Phase 5 budget fields above are not filed yet either. Advance and blocked push notifications ride on BL427's mobile topic sink (`PublishToTopic`), so APNs/FCM need no Automation-specific delivery. Phase 7: the ready-to-file text is under `### Phase 7 companion apps` (iOS issue); still **not filed**. Phase 8: the ready-to-file text is under `### Phase 8 companion apps` (iOS issue); **not filed**. |

---

## Phases

These phases are proposed for the follow-up implementation PRD. All are
**Planned**, and none is approved. Each phase's own checklist includes AGENT.md's
**Phase Completion Checklist**: functionality, unit tests, live smoke against a
real daemon, full `go test ./...` and `node --test`, the 8-item documentation
checklist, CHANGELOG, `docs/config-reference.yaml`, `docs/testing-tracker.md`,
howto, and the `datawatch-app` issue where applicable.

1. **Phase 1 — Data model, store, validation, CRUD.** Status: Done (development build 9.5.5; released with the first complete release, 9.6.0).
   - `Automation` / `AutomationMember` / `BudgetPolicy` and `PRD.AutomationID`.
   - `automations.jsonl` with the ownership index.
   - Cycle, reachability and ownership validation.
   - REST + MCP CRUD.
   - Audit of create, update and delete.
2. **Phase 2 — Coordinator.** Status: Done (development build; released with the next release).
   - The BL427 dispatcher subscriber, or the temporary hook.
   - The advance with compare-and-set, `API.Run` / `API.Resume` in-process, the
     approval gate and blocked alerts.
   - Restart reconciliation after `resumeRunningPRDs`.
   - The `autonomous_status` extension and idle alert.
3a. **Phase 3a — Multi-cursor: fork/join branches + nested Automations.** Status: Planned (full design in the **Phase 3a** section below; the build follows that design).
   - The multi-cursor data model (`ActiveCursors` replacing the single cursor, load-time migration).
   - Member kinds `prd` (default) / `automation` / `fork` / `join`; fork/join pairing validation, including the rule that branch members carry no outcome edges.
   - Per-join `on_branch_failure` rule (`all_must_succeed` default) and join firing/blocking.
   - Nested Automation members: ownership, cross-level cycle rejection, terminal-event routing.
   - The multi-cursor advance rule, mid-fork restart reconciliation, the `autonomous_status` extension (`active_cursors`, `join_wait`), and the new audit entries (`automation_fork`, `automation_join`, `automation_join_blocked`, `automation_nested_activate`).
   - Branch naming convention that Phase 3b control ops build on (member ID = the stable branch name).
3b. **Phase 3b — Control ops and mid-flight editing (branch-named).** Status: Planned.
   - The pause / resume / cancel (`cancel_in_flight` is **required, no default** — Open question 3) / skip / insert / remove / reorder ops.
   - Every op names the branch (member ID) it acts on, per the Phase 3a naming convention.
   - The shared per-Automation lock, the `Version` check and `409`s.
   - Decisions and audit.
4. **Phase 4 — Model-tier defaults.** Status: Done (development build; released with the next release).
   - The Automation tier in `resolveTaskBackendModel`, the executor effort chain
     and `resolveVerificationLLM`.
   - `VerifierSourceAutomation`.
   - `automation_set_llm` / `_set_verification_llm`.
   - Shipped notes:
     - `executor_source` (`task` | `story` | `prd` | `automation` | `global`) is
       recorded on the task run record beside `verifier_source`. It names the level
       that supplied the execution backend for the latest attempt, is overwritten by
       each later attempt, and `global` means no level named a backend. The session
       status block and the PWA show it (an **Executor** line).
     - `expected_version` is optional on both set operations (omitted or `0` skips
       the check, a stale positive value is a `409`), unlike the Phase 3b control
       operations where it is required. The fields are presence-aware (omitted keeps,
       empty clears), and the operations are allowed while the Automation is
       `draft`, `active`, `paused` or `blocked`.
     - The planning pair (`planning_backend`, `planning_model`) is used for decompose
       only: unset means the global planning backend, and it never changes the run
       or verify default (and the run and verify defaults never change who plans).
       `set_verification_llm` rejects the planning fields.
     - Operator-facing docs: the model-tier section of `docs/api/autonomous.md`, the
       Automation tools in `docs/mcp.md`, and the "Give an Automation its own model
       defaults" section of `docs/howto/automation-queue-advance.md`. The Automation
       defaults are runtime data on each Automation, not config keys, so
       `docs/config-reference.yaml` carries a comment block only.
     - The live smoke row is opt-in and still owed: see the Phase 4 section of
       `docs/testing-tracker.md`.
5. **Phase 5 — BudgetPolicy.** Status: Done (development build; released with the next release).
   - The shared `PRDCostUSD` rollup (with BL414 / BL428 Phase 5).
   - The advance gate and the `admit` hook.
   - Pacing, on-breach handling and the estimated `allocation_pct`.

   **Phase 5 status note.** Built on the existing per-LLM spend ledger (no second ledger).
   - Rollup and ledger: `internal/autonomous/automation_cost.go` (`PRDSessionIDs`, `PRDCostUSD`,
     `AutomationSpendUSD`, `SetCostSources`); `internal/autonomous/budget.go` attributes each
     ledger entry to its session (`SpendBySessions`); `cmd/datawatch/capacity_wiring.go` passes the
     session ID and wires the evaluator and cost sources.
   - Model: `internal/autonomous/automation.go` (`BudgetPolicy` gains `allocation_estimate`,
     `peer_window`, `reset_time`, `reset_weekday`, `reset_day`, `reset_zone`; `AutomationMember`
     gains `estimated_cost_usd`; `Automation.BudgetState` is the current breach episode).
   - Evaluator: `automation_budget.go` (`AutomationBudget.Evaluate`: tightest of the Automation and
     member policy, windows and anchors, even pacing, declared or prior-spend start estimate),
     `automation_budget_reset.go` (reset known for day/week/month windows and for a parsed
     rate-limit reset on the lifetime window), `peer_budget_spend.go` (`SpendForPeers`, opt-in
     peers only, fails open), `automation_budget_view.go` (the computed `budget` view).
   - Gate: `automation_budget_gate.go` and `automation_advance.go` / `automation_coordinator.go` /
     `automation_control.go` / `automation_reconcile.go` (advance, activation entry and fork gates;
     pause with automatic resume at a known reset; block at an unknown reset or `on_breach: block`;
     one alert and one audit record per episode; the periodic budget pass; the operator resume
     re-checks the ceiling and is refused with a 409 while it is reached).
   - Admit hook: `automation_budget_admit.go` (a new member task waits at the dollar ceiling; tasks
     in progress are never interrupted).
   - Surfaces: REST `POST /api/automations/{id}/set_budget` and the `budget` view on the Automation
     read and on each `automations[]` row of `GET /api/autonomous/status`
     (`internal/server/autonomous.go`, `internal/autonomous/automation_status.go`); MCP `automation_set_budget`
     (`internal/mcp/automation_tools.go`, `internal/federation/mcp_tool_caps.go`); CLI
     `datawatch automation set-budget` (`cmd/datawatch/cli_automation.go`); comm
     `automation set-budget` and the budget lines of `automation status`
     (`internal/router/automation_cmd.go`, text from `budget_format.go`); audit events
     `automation_set_budget`, `automation_budget_breach`, `automation_budget_resumed`
     (`internal/audit/events.go`, `cef.go`); `docs/api/openapi.yaml`.
   - Operator decisions applied: (1) on a breach the Automation pauses and resumes by itself at the
     window rollover only when the reset time is known (a day, week or month window the daemon
     defines itself, anchored at the declared reset or the calendar default, or a reset time parsed
     from a rate-limit message); when it is unknown it goes to blocked and waits for the operator;
     an alert is raised either way. (2) `allocation_pct` is an optional estimate, off by default,
     always labelled estimated, and never pauses or blocks. (3) Another daemon's usage counts only
     for peers explicitly opted in (`peer_window`), off by default; the resource fingerprint
     exchange that would confirm a shared account does not exist yet, so peer counting has no
     automatic confirmation and the status text says so.
   - No new daemon config key: the policy is Automation data. `default_budget_on_breach` (listed as
     provisional in the Parity surface table) was not built; `on_breach` defaults to `pause` on the
     policy itself. The `budget_policy` object is documented in `docs/config-reference.yaml`.
   - Not in this phase: the PWA display of the policy (Phase 6).
   - Docs: `docs/howto/automata-capacity.md` and `docs/operations.md` (`Automation spending limit`),
     `docs/commands.md`, `docs/api/autonomous.md`, `docs/api-mcp-mapping.md`, `docs/mcp.md`,
     `docs/testing-tracker.md` (`BL431 Phase 5: Automation BudgetPolicy`).
   - Live smoke against a real daemon: **No**, not run. Every Validated cell of the tracker section
     stays No until a sandbox daemon run exercises the pause, the automatic resume and the block.
   - `datawatch-app` parity: **not filed yet.** A companion-app issue is needed so Android and iOS
     parse the `budget_policy` fields, the `budget` view, the `budget`, `spent_usd` and
     `ceiling_display` status fields, and the paused or blocked budget reason. It cannot be filed
     from the implementing session; the supervisor files it, and this note is updated with the
     issue numbers when it is.
6. **Phase 6 — PWA.** Status: Planned.

6. **Phase 6 — PWA.** Status: Done (development build; released with the next release).
   - The tab, list, pipeline strip, controls and forms are built in `internal/server/web` with node tests.
   - The live browser smoke row is owed: see the Automation tab section of `docs/testing-tracker.md`.
   - The nav tab, list, pipeline strip and v1 create/edit.
   - Locale keys in all bundles.
   - The Terminology Rule addendum.
7. **Phase 7 — Remaining parity, BL117 migration, close-out.** Status: Done (development build; released with the next release).
   - The parity audit table and `TestAutomationParity`; the verb, web control, audit event and config key gaps are closed on every surface.
   - `import-graph` on REST, MCP, CLI and comm, with the converter that turns an orchestrator graph into a draft Automation.
   - Deprecation notices on `orchestrator_graph_create/plan/run` (REST, MCP, CLI and comm), with a once-per-process daemon warning.
   - The howto `docs/howto/automation-chains.md` (no BL ids).
   - The release-smoke section 78.
   - The `datawatch-app` Android and iOS issues are written as ready-to-file text (see Phase 7 companion apps); **not filed yet**.

### Phase 7 status note

   **Phase 7 status note.** What shipped:
   - Parity audit: the table is the `Phase 7 parity audit` section near the end of this file;
     `TestAutomationParity` (`cmd/datawatch/automation_parity_test.go`) enforces the REST, MCP, CLI and comm columns.
   - import-graph: REST `POST /api/automations/import-graph/{graph_id}` (`internal/server/automation_import_graph.go`,
     body `{"dry_run": bool}`; 422 carries the refusal reason); MCP tool `automation_import_graph`
     (`internal/mcp/automation_tools.go`); CLI `datawatch automation import-graph <graph-id> [--dry-run]`
     (`cmd/datawatch/cli_automation.go`); comm `automation import-graph <graph-id> [dry-run]`
     (`internal/router/automation_cmd.go`); audit event `automation_import_graph` on a real import.
   - Converter: `internal/autonomous/automation_import_graph.go` (`ImportGraphToInput`).
   - Deprecation helper: `internal/orchestrator/deprecation.go` (`DeprecationNotice`, `WarnDeprecatedOnce`) on the
     three tools `orchestrator_graph_create`, `orchestrator_graph_plan` and `orchestrator_graph_run` and their REST,
     CLI and comm equivalents; responses carry a `Deprecation: true` header and a `deprecation` field.
   - Howto: `docs/howto/automation-chains.md`.
   - Tracker section: `Automation parity, import-graph and deprecation — unreleased` in `docs/testing-tracker.md`.
   - Release smoke: section 78 of `scripts/release-smoke.sh`.

   Still owed:
   - The live smoke result: section 78 and the tracker section have not been run against a real daemon, so every
     Validated cell of the tracker section stays No until a sandbox run exercises them.
   - The mobile apps: no mobile code is in this phase; see Phase 7 companion apps.

### Phase 7 companion apps

   **`datawatch-app` parity: not filed yet.** The two issues below are ready to file in `dmz006/datawatch-app`.
   They cannot be filed from the implementing session; the supervisor files them and this note is updated
   with the issue numbers. No mobile code is in this phase.

   **Issue 1 — Android: Automation tab and Phase 7 fields**
   - Scope: an Automation tab with the list; the pipeline strip detail for one Automation; the controls pause,
     resume, cancel (with the `cancel_in_flight` option), skip and insert. Add the new verbs. Show the
     import-graph dry-run result (the preview input and the warnings) and, on a refusal (HTTP 422), the refusal
     reason. Render the `fork` and `join` member kinds in the strip. Parse the budget fields (`budget_policy`,
     the `budget` view, `spent_usd`, `ceiling_display`, and the paused or blocked budget reason) and the
     `deprecated` and `deprecation` fields now present on old orchestrator graph responses, showing the notice.
   - Acceptance criteria:
     1. The tab lists Automations and opens a pipeline strip detail with the current state of each member.
     2. Pause, resume, cancel (with and without cancel-in-flight), skip and insert each call the REST route and show the result or the error.
     3. import-graph with dry run shows the previewed Automation and warnings and stores nothing; a refused graph shows the reason text.
     4. Fork and join members draw as parallel branches without breaking the strip.
     5. Budget fields parse and display, and a response missing them does not crash the screen.
     6. A graph response with `deprecated` and `deprecation` shows the notice; one without them is unchanged.

   **Issue 2 — iOS: Automation tab and Phase 7 fields**
   - Scope: the same as the Android issue, in the companion app repo's iOS track: the Automation tab (list,
     pipeline strip detail, pause, resume, cancel with cancel-in-flight, skip, insert), the new verbs,
     import-graph (dry run result display and the refusal reason), the `fork` and `join` member kinds, the budget
     fields, and the `deprecated` and `deprecation` fields on old orchestrator graph responses.
   - Acceptance criteria: 1 to 6 of the Android issue, verified on iOS.
   - Advance and blocked push notifications ride on BL427's mobile topic sink, so no Automation-specific APNs work is needed.
8. **Phase 8 — Progress, estimates and running dashboard (BL553).** Status: Done (development build; released with the next release).
   - The metrics engine (counts by kind and state, correct time and throughput, utilization,
     remaining work split known vs estimated, completion range) with REST, MCP, CLI and comm verb.
   - The explicit PRD `kind` field and a machine-readable backlog.
   - Daily snapshots and trend; the PWA dashboard card; `scripts/v10-status.py` kept as the oracle
     and as a golden test.
   - Operator guide `docs/howto/automata-progress.md`, tracker section `Automata progress dashboard — unreleased`
     in `docs/testing-tracker.md`; the `datawatch-app` Android and iOS issues are written as ready-to-file text
     (see Phase 8 companion apps); **not filed yet**.

### Phase 8 status note

   **Phase 8 status note.** What shipped:
   - Engine: `internal/autonomous/progress_engine.go` (counts, throughput, utilization, lead time),
     `progress_time.go` (inactive windows, active hours, median), `progress_remaining.go` (known and estimated
     work, completion range and drivers), `progress_kind.go` (kind resolution), `progress_backlog.go` (backlog
     reader and the `{kind=...}` and `{phases=N}` markers), `progress_text.go` (the one text renderer).
   - Surfaces: REST `GET /api/autonomous/progress` and `GET /api/autonomous/progress/trend`
     (`internal/server/progress.go`, read capability); MCP `autonomous_progress` and `autonomous_progress_trend`
     (read-only) and `autonomous_prd_set_kind`; CLI `datawatch autonomous progress`, `progress-trend`,
     `prd-set-kind` and `prd-create --kind`; comm `progress`, `progress trend [days]` and `autonomous set-kind`;
     REST `POST /api/autonomous/prds/{id}/set_kind`.
   - Snapshots: `progress-snapshots.jsonl` next to the Automata store, one per UTC day plus one per completed
     Automaton, pruned at `autonomous.progress.snapshot_retention_days`; trend direction converging, widening,
     flat or unknown (10% tolerance on the width of the range).
   - Config: `autonomous.progress.lanes`, `inactive_gap_hours`, `backlog_path`, `since`, `snapshot_retention_days`
     on YAML, CLI, web UI, REST, MCP and comm, applied live.
   - PWA: the Progress card on the Automata tab (`progressCardHtml`, `loadProgressCard`), the *Kind* select in
     Automaton settings, the five settings fields; locale keys `progress_card_*` and `settings_progress_*` in
     all five bundles.
   - Oracle and golden test: `internal/autonomous/progress_golden_test.go` runs the engine and
     `scripts/v10-status.py` on the fixture in `internal/autonomous/testdata/progress/` and compares every number.
   - Howto: `docs/howto/automata-progress.md`. Tracker section: `Automata progress dashboard — unreleased`.

   The four documented oracle changes (made in `scripts/v10-status.py` and the engine together, so they still agree):
   - (a) Overlapping or touching inactive windows are merged before hours are summed, so no stretch is excluded twice.
   - (b) A multi-phase backlog row is read from its `{phases=N}` marker instead of the names of two particular
     rows: an unfiled row adds max(N, 1) new Automata, a filed row adds max(N minus its linked ids, 0).
   - (c) Tasks per Automaton divides by the Automata that have at least one task, not by every Automaton.
   - (d) Records whose `is_template` field is true are skipped.

   Still owed:
   - The live smoke result: the tracker rows are Tested by unit and golden tests with fakes and fixtures only, so
     every Validated cell stays No until a sandbox run covers the end-to-end story (real finished Automata, a real
     backlog and waits file, a day boundary for the snapshot, the card in a browser).
   - Deferred, not built: the stage ledger timestamps (lead time uses the existing created, approved and task
     start and completion times, so a stage that is not recorded reads not measured); per-lane optimization
     inputs (utilization is one figure against a configured lane count, not a per-lane or per-backend view);
     and critical-path figures (the completion range divides total work by a rate; it does not model task
     dependencies or which Automaton gates the finish).
   - The mobile apps: no mobile code is in this phase; see Phase 8 companion apps.

### Phase 8 companion apps

   **`datawatch-app` parity: not filed yet.** The two issues below are ready to file in `dmz006/datawatch-app`.
   They cannot be filed from the implementing session; the supervisor files them and this note is updated
   with the issue numbers. No mobile code is in this phase.

   **Issue 1 — Android: Automata progress card and Automaton kind**
   - Scope: an Automata progress card and a kind field. Read `GET /api/autonomous/progress` (optional
     `automation_id`, `tag_id`, `since`, `until`) and `GET /api/autonomous/progress/trend?days=N` (1 to 365,
     default 30). Parse the metrics object: `generated_at`, `since`, `scope`, `calendar_days`, `active_days`,
     `lanes`; `groups` (`feature`, `bug`, `other`, always all three) and `totals`, each with `prds`, `done`,
     `running`, `waiting`, `blocked`, `tasks`, `tasks_done`, `tasks_left`, `work_hours` and the nullable
     `median_prd_wall_hours`, `mean_prd_wall_hours`, `mean_task_hours`, `median_task_hours`; `throughput`;
     `utilization`; `lead_time` (every stage nullable); `inactive` (`windows` with `start`, `end`, `kind`,
     `reason`, `inferred`, plus `excluded_hours`, `share_of_calendar`, `calendar_estimate_days`); `remaining`
     (`known`, `estimated`, `combined`); `completion` (`low_days`, `high_days`, the four `by_*_days` figures,
     `calendar_days`, `drivers`, `method`); `unfiled_rows`; `backlog_available`; `not_measured`. From the trend
     object read `days`, `direction` and `points` (`date`, `reason`, `completion_low_days`,
     `completion_high_days`, `known_tasks`, `estimated_tasks`, `throughput_3d`, `utilization`,
     `completed_prds`). Treat every nullable number as not measured; never show a missing value as 0. Add a
     Kind picker (feature, bug, other, or derived) that calls `POST /api/autonomous/prds/{id}/set_kind` with
     `{"kind": ...}` (an empty kind derives it again), and send `kind` when creating an Automaton. Never show
     the `id` or `number` of an `unfiled_rows` entry; show its `item` text only.
   - Card contents, in this order: the kind table (one row each for feature, bug and, when non-zero, other, plus
     a total: Automata, done, running, waiting, blocked, tasks, tasks done, tasks left, work time, median per
     Automaton, mean per task); the work-remaining block with the known part (Automata not done, tasks still
     open, not decomposed yet) and the estimated part (new items not filed yet, Automata with no tasks yet,
     estimated tasks, tasks per Automaton mean and median) kept apart, then the combined estimate; the
     completion range with the four method lines and their sentences, the calendar view and the drivers; the
     throughput, utilization and inactive-windows summaries (inferred windows marked); the unfiled-row count
     and the trend direction. Show a stale notice when the last refresh failed or the data is older than five
     minutes, keep the last good data on screen, and show a retry control when the first load fails.
   - Locale keys: add all `progress_card_*` keys (title, loading, stale, failed, retry, not measured, inferred,
     the units, the `col_*` headings, the `kind_*` labels, the remaining, completion, method, driver,
     throughput, utilization, windows, backlog and trend keys) and the five `settings_progress_*` keys
     (`lanes`, `inactive_gap_hours`, `backlog_path`, `since`, `snapshot_retention_days`) from the five server
     locale bundles under `internal/server/web/locales/`, in the five languages (en, de, es, fr, ja). Map the
     API `drivers` values `utilization`, `large_prd_mix`, `undecomposed_backlog` and `sparse_history` to the
     `progress_card_driver_*` keys, and the `method` keys `prd_rate`, `task_rate`, `task_rate_24h` and
     `worker_hours` to the `progress_card_method_*` keys; an unknown value must not break the screen.
   - Acceptance criteria:
     1. The card lists a row per kind and a total, and the *other* row appears only when it has Automata.
     2. Known and estimated remaining work are shown separately and the combined figure is labelled an estimate.
     3. A null number shows the localized *not measured* text, never 0, and a response missing a field does not crash the screen.
     4. The completion range shows low to high active days, each method with its sentence, and the drivers as localized text.
     5. Inactive windows list with the inferred mark; the stale notice and the retry control behave as described.
     6. The trend direction shows converging, widening or flat, and nothing when it is unknown or no snapshot exists.
     7. Setting the kind calls the route, shows an error on a 400 or 404, and an empty kind clears it.
     8. No tracker id, internal term or `unfiled_rows` id appears anywhere on screen; hostile text in a row is shown as plain text.

   **Issue 2 — iOS: Automata progress card and Automaton kind**
   - Scope: the same as the Android issue, in the companion app repo's iOS track: the progress and trend reads,
     the metrics parsing with nullable numbers, the kind picker and create-time kind, the card contents and
     states, and the `progress_card_*` and `settings_progress_*` locale keys in the five languages.
   - Acceptance criteria: 1 to 8 of the Android issue, verified on iOS.
   - No push or APNs work is needed: the progress figures are read on demand and carry no notification.

---

## Phase 3a — multi-cursor: fork/join branches and nested Automations

Phase 3a extends the Automation graph from a single linear cursor to **parallel
branches** (fork/join members) and **nested Automations** (a member that is
another Automation), plus the multi-cursor machinery (advance rule, restart
reconciliation, status, audit) both need. It is the operator-decided 3a/3b
split from Open question 10: **3a designs and builds the multi-cursor
coordinator first; 3b then builds the control ops on top, and every control op
names the branch it acts on** using the branch naming fixed in this section.

Operator decision record (2026-10-22, recorded in this doc's Open questions
and in the task notes):

- **Nesting (OQ 2):** allowed (operator decision 2026-10-22) — an Automation may contain another
  Automation as a member, with cycle detection across levels and clear
  ownership.
- **Fork/join (OQ 10):** build it — a fork starts several member Automata at
  the same time; a join waits for all of them, then continues, with a
  per-join rule for what happens if a branch fails or is skipped.
- **Join failure rule (OQ 10):** per join — `on_branch_failure`:
  `all_must_succeed` (default: wait for the other branches, then the
  Automation goes `blocked` and alerts naming the failed branch),
  `any_one_is_enough`, or `continue_anyway`.

### Multi-cursor model

The single cursor becomes a **set of active cursors**:

- `Automation` gains `ActiveCursors []string` (member IDs currently in
  flight, kept sorted and unique for determinism). The existing
  `CurrentMemberID` keeps its JSON field (`current_member_id`) as a
  **deprecated input only for load-time migration**: on load, if
  `active_cursors` is absent or empty and `current_member_id` is non-empty,
  `ActiveCursors` is seeded from it. It is never written again after a 3a
  write. (The feature is new and unreleased; no operator data migration is
  needed beyond this load path.)
- A linear Automation is exactly the existing model: no `fork`/`join`
  members and at most one active cursor at a time. All existing advance,
  blocking, alerting and idle behaviour for linear Automations is unchanged.
- Every write (advance, fork fire, join fire, reconcile) still goes through
  the existing optimistic-concurrency `UpdateAutomation(id, expectedVersion, …)`
  payload; `ActiveCursors` is part of that payload, and a stale-version apply
  re-reads the record, re-evaluates, and no-ops if the state has already
  moved (the existing `coordinatorCASRetries` bounded retry applies per
  cursor). This is the stale-version rule the required test set exercises.

### Active cursor set and advance rule

On each terminal event for a member in `ActiveCursors` (a live PRD or child
Automation reaching a terminal status, or a reconcile replay of one):

1. Remove the member from `ActiveCursors`. Record its outcome on the member
   (`Outcome`, `EdgeTaken`, `FinishedAt`).
2. If the member is a **branch** (listed in a fork's `fork_members`), record its
   success or non-success outcome on the paired join and evaluate the join rule.
   Do **not** follow an edge: a branch has none.
3. Otherwise follow the member's single outcome edge, per the edge mapping for
   `prd` members and per the nested outcome mapping for `automation` members. An
   edge to a `prd` or `automation` member starts it through the start gate and
   adds it to `ActiveCursors`. An edge to a `fork` fires that fork. An empty edge
   completes or blocks the Automation as the mapping says.
4. A join that fires follows its `on_complete` exactly as step 3 does.
5. Persist once with `UpdateAutomation(id, expectedVersion, …)`. A stale version
   re-reads the record and re-evaluates, and it is a no-op if the state has
   already moved. The `coordinatorCASRetries` bound applies per cursor.
6. The Automation stays `active` while `ActiveCursors` is non-empty or a join is
   waiting. It completes when `ActiveCursors` is empty, no join is waiting, and
   every cursor's forward path ended in an empty `on_complete`. It blocks as
   described under **Fork/join semantics**.

A linear Automation takes steps 1, 3 and 5 with at most one cursor. Its behaviour
is unchanged.

### Member kinds (fork/join + nested)

`AutomationMember` gains a `kind` discriminator (JSON `kind`, omit defaults
to `prd`):

| `kind` | Meaning | New fields |
|---|---|---|
| `prd` (default) | Run one member Automaton (PRD), then follow an outcome edge | `prd_id` (as today), `on_complete/on_blocked/on_failed` |
| `automation` | Run another whole Automation, nested: start it and wait for its terminal state | `automation_id` (the child Automation); edges as today |
| `fork` | Control node reached like any other next member: on arrival it starts **all** its branch members at once | `fork_members: []string` — branch member IDs (≥ 2) |
| `join` | Control node: collects its paired fork's branches, applies the rule, then continues | `join_members: []string` — mirrors the paired fork's `fork_members`; `on_branch_failure`; `on_complete` |

New member fields (all omitempty on the wire; member-ID-named, matching the
existing validation error style):

- `kind` — `prd` | `automation` | `fork` | `join`; omit = `prd`.
- `automation_id` — `kind=automation`: the child Automation ID.
- `fork_members` — `kind=fork`: the branch member IDs to start simultaneously.
- `join_members` — `kind=join`: the branch member IDs this join collects.
- `on_branch_failure` — `kind=join` only: `all_must_succeed` (default when
  omit) | `any_one_is_enough` | `continue_anyway`.

Control-node bookkeeping: a fired `fork` member is recorded `completed` with
outcome `forked`; a fired `join` member is recorded `completed` with outcome
`joined`. Neither starts a PRD, so their start gate never runs.

**Nested outcome mapping** — the child Automation's terminal status maps to
the nested member's outcome and edge:

| Child ends | Nested member outcome | Edge taken |
|---|---|---|
| `completed` | success | `on_complete` |
| `blocked` | blocked | `on_blocked` |
| `cancelled` | failed | `on_failed` |

The parent's coordinator learns child terminal states through the existing
lifecycle dispatcher at a **new level** `LevelAutomation` (`"automation"`),
kinds `automation.completed` / `automation.blocked` /
`automation.cancelled`, emitted by the Manager on the child's transition to
that status. `LifecycleEvent.AutomationID` (already present) routes it, and
the coordinator's existing LevelPRD subscription shape is reused for
LevelAutomation; the filter validation in `lifecycle_subscription.go` gains
`LevelAutomation` as an accepted level. A `fork` branch of kind `automation`
therefore terminates via `automation.*` events into the join exactly like a
`prd` branch terminates via PRD events.

### Fork/join semantics

- A **fork** is reached as a next member (via an incoming outcome edge, or as
  the entry member — a `fork` may be the entry member). On arrival the
  coordinator marks it `completed` (outcome `forked`) and attempts each branch
  in deterministic (sorted) order, each under the standard per-cursor start
  gate (approval state, resume-vs-run, deferred-before-recovery, the
  `beginStart` in-flight guard). `ActiveCursors` becomes the set of branches
  that started. A branch that fails its gate or whose start errors is
  recorded immediately as a **failed branch** (decision + audit) — the
  fork/join contract treats it like a branch that ended in failure; it does
  not block sibling branches. This is the only place a single branch's start
  failure does not block the Automation: the join rule is the decider.
- **Branches carry no outcome edges; the fork/join pairing is the only link.**
  Every member listed in a fork's `fork_members` must be a `prd` or
  `automation` member (control nodes may not be branches), and its
  `on_complete`, `on_blocked` and `on_failed` must all be empty. The fork
  itself carries no outcome edges: its forward path is its paired join. A
  branch's terminal outcome reaches the join through the pairing (the join's
  `join_members` set equal to the fork's `fork_members`), not through an edge.
  So a join's only incoming link is its fork, and no branch can stall a join,
  because every branch's terminal outcome is always delivered to it.
- **Branch outcome mapping.** A branch's terminal status is recorded on the
  join as success or non-success: `completed` (or `automation.completed` for a
  nested branch) is success; `blocked`, `failed`, `cancelled` and `skipped` are
  non-success. The data-model edge table (Mapping PRD statuses onto the three
  edges) applies to linear members only. For a branch, the join rule, not an
  edge, decides whether the Automation blocks, so a cancelled branch is named
  in the join's block reason rather than blocking the Automation directly.
- A **join** fires when its rule is satisfied by the branches' terminal
  outcomes. Rule evaluation is derived from branch member statuses (not event
  order), so it is replay-safe:
  - `all_must_succeed` (default): fires the `on_complete` edge only when
    **every** branch ended successful. When all branches are terminal and
    any ended non-successfully, the Automation goes `blocked` with reason
    `join <id>: not all branches succeeded (non-success: <member IDs>)`,
    plus a decision and the `automation_join_blocked` audit entry (and the
    existing block alert).
  - `any_one_is_enough`: fires the moment **any** branch ends successful.
    Still-running branches are allowed to finish naturally; their late
    events are recorded as decisions and never re-fire the join. If every
    branch ends without one succeeding, the Automation goes `blocked` with
    reason `join <id>: no branch succeeded`.
  - `continue_anyway`: fires the moment **all** branches are terminal,
    whatever their outcomes; non-success branches are named in the join
    decision and audit.
- After a join fires: the join member is recorded `completed` (outcome
  `joined`) and its `on_complete` edge is followed exactly like any other
  advance (gate, CAS, audit — or complete the Automation if the edge is
  empty).
- The **Automation completes** when the last live cursor's forward path
  resolves to an empty `on_complete` (or join `on_complete`) and no live
  cursor remains. It goes **blocked** when the last live cursor's forward
  path is a non-success edge with empty target, a last remaining branch's
  start fails at the gate or in the start, or a join's rule is unsatisfied
  with all branches terminal. Late branches of an already-fired join never
  block completion (they finish naturally; their events are recorded).

### Nested Automation semantics

- **Ownership:** the child's PRDs belong to the **child Automation** under
  the existing single-ownership rule. The child Automation itself may be a
  member of **at most one other** non-terminal Automation — enforced at
  create/update of the referencing Automation by scanning non-terminal
  Automations' membership, the same mechanism PRD ownership validation uses.
  There is no parent pointer on the child; parentship lives only in the
  parent's member list (no second source of truth to keep in sync).
- **Start:** starting a `kind=automation` member activates the child (via
  `Manager.ActivateAutomation`'s existing semantics, which starts the
  child's entry member through the normal gate). The coordinator takes this
  through a **new interface** (`ActivateAutomation(id, actor string) error`,
  checked by type-assertion on the starter, so existing `AutomationStarter`
  test fakes keep compiling). Starting a child that is already active is the
  existing `409` path and is recorded as a failed start of that branch.
- **Cycle rejection across levels:** validation walks parent→child edges
  (from `kind=automation` members) and rejects any create/update where the
  parent can reach itself transitively — including the trivial self-reference
  (a member naming its own Automation) and the two-cycle (A ↔ B). A child may
  not be a member of two Automations (uniqueness above), and a PRD that is a
  member of the child may not also be a member of the parent (existing PRD
  ownership rule). Depth is therefore arbitrary but acyclic; typical nesting
  is one level.
- **Budgets:** the child keeps its own `BudgetPolicy`; a parent does not gate
  or cap the child's internal starts, and 3a does no cross-level budget
  rollup (OQ 9 stays as recorded).

### Validation addenda (on every create/update, under the same lock)

Existing invariants are unchanged (unique member IDs, edges naming existing
members, reachability from the entry member, no in-Automation cycles, single
PRD ownership). 3a adds:

1. `fork_members` ≥ 2, all exist, distinct, each a `prd` or `automation`
   member; no member appears in two forks' `fork_members`.
2. A `join` pairs with **exactly one** `fork` of the same Automation whose
   `fork_members` equal its `join_members` (set equality), and no second
   join may share that branch set.
3. Every branch's `on_complete`, `on_blocked` and `on_failed` are empty, and
   the fork itself carries no outcome edges (see **Fork/join semantics**). A
   `join` carries only `on_complete`; its `on_blocked` and `on_failed` must be
   empty, because a join's rule failure blocks the Automation rather than
   routing along an edge.
4. No outcome edge may name a `join`. A join is reached only through the
   pairing in rule 2, so its only incoming link is its fork. Outcome edges may
   name `fork`s. A `join` may not be the entry member; a `fork` may be.
5. `kind=automation`: the child exists, is not itself (no self-nesting), is
   not referenced by any other non-terminal Automation, and the parent does
   not reach itself through it (transitively).
6. `on_branch_failure` appears only on `join` members and is one of the
   three rule values. `kind` values are one of the four.
7. All errors are member-ID named, in the existing `validateAutomation`
   error style. Fork/join members may not carry `prd_id`; `prd` members may
   not carry `fork_members`/`join_members`/`on_branch_failure`.
8. **Cycle detection covers the pairing.** The three-colour DFS (see **Data
   model › Validation**) also walks the fork/join pairing: an edge from each
   fork to each of its branches, and from each branch to its paired join. A
   cycle that passes through a fork is therefore rejected with the member IDs
   named, even though the branches carry no outcome edges.

### Restart reconciliation (multi-cursor)

`reconcile()` generalizes from the single cursor to the cursor set, with the
existing guarantees (nil-safe, idempotent, runs once after boot-resume before
the coordinator becomes ready):

- For each active/paused Automation: for every member in `ActiveCursors`
  whose PRD (or child Automation) has reached a terminal state while the
  daemon was down → replay through the standard event path as a terminal
  event (outcome recorded, branch → join evaluation, edge or join rule
  applied). Existing behaviour, now per cursor.
- For every not-yet-fired `fork` whose branches are (partly) already in
  flight or terminal → re-fire the fork: started/terminal branches are
  untouched (the `beginStart` guard and PRD status checks make this
  idempotent), and each unstarted branch gets its one retry. This is the
  **restart-mid-fork** case the required test set covers: branches already
  running at crash time keep running; the rest start; the join still sees
  every branch exactly once.
- For every pending `join` whose branches are all already terminal →
  re-derive the rule and fire or block, idempotently (same inputs → same
  decision, so a replay never double-fires or double-blocks).
- For every pending `kind=automation` member whose child is still `draft`
  (its start never landed) → retry the child activation once, through the
  normal gate path.

### `autonomous_status` extension

Each `automations[]` entry gains two fields (existing scalar fields kept for
back-compat):

- `active_cursors: [{ member_id, prd_id?, automation_id?, status }]` — one
  entry per in-flight member; `prd_id` for kind `prd`, `automation_id` for
  kind `automation`; `status` the member status. Sorted by member ID.
- `join_wait: { join_member_id, rule, pending_branches: [member_id], failed_branches: [member_id] }` —
  present (and non-null) while a join is waiting on branches: the join
  member, the applied rule, the branches still live, the branches ended
  non-successfully. Absent otherwise.
- Back-compat: `current_member_id` / `current_prd_id` /
  `current_prd_title` are derived from the first active cursor (sorted);
  empty when none. Existing fields (`pending_members`, `idle_since`,
  `idle_seconds`, `blocked_reason`) keep their semantics, with
  `blocked_reason` additionally carrying the join-block reasons above.

### Audit entries (Audit Logging Rule)

New actions, all through the existing `s.audit(…, "automation", id, …)` /
`SetAuditFn` paths in JSON-lines and CEF, each carrying **actor** and
**reason** where applicable:

| Action | When | Payload includes |
|---|---|---|
| `automation_fork` | A fork fires | fork member ID, branch member IDs, per-branch start result (started / gate-denied / start-error) |
| `automation_join` | A join fires (rule satisfied) | join member ID, rule, per-branch outcomes, the edge taken |
| `automation_join_blocked` | A join's rule is unsatisfied and the Automation blocks | join member ID, rule, non-success branch IDs, the block reason |
| `automation_nested_activate` | A `kind=automation` member starts | parent Automation ID, parent member ID, child Automation ID, start result |

Existing `automation_advance` / `automation_blocked` /
`automation_activate` entries keep their shape; an advance whose target is a
`fork` or `join` member adds `fork_members` / `join_members` to the payload.

### Branch naming (what Phase 3b builds on)

Phase 3b control ops (pause / resume / cancel / skip / insert / remove /
reorder) address work by **stable member IDs** inside their Automation:

- A **branch** is named by its member ID: `automation <id> <op> member=<member_id> …`.
  Member IDs are stable for the Automation's lifetime (`8-hex, stable across
  edits`) and across daemon restarts, so a branch name is unambiguous within
  its Automation and survives restarts. In-flight cursors and join-waiting
  branches are both addressable this way.
- **Nested scope:** ops on a member inside a child name the nesting path
  `<parent_member_id>/<child_member_id>` (parent member = the
  `kind=automation` member); the REST address for 3b is
  `/api/automations/{parent}/members/{parent_member}/…` — the concrete 3b
  endpoint set is 3b's design, but this naming is fixed here so 3a and 3b
  cannot diverge.
- **Joins** are addressable by their member ID too (3b can pause at a join —
  hold its firing — or mark it consumed on skip).
- `cancel` inherits the OQ 3 decision: `cancel_in_flight` is a **required
  argument on every surface with no default**; 3a's per-branch semantics
  (a cancelled Automation stops future starts per branch; in-flight branches
  finish) is what 3b exposes.
- **Urgent insertion (OQ 17):** the cap — at most one urgent member at a
  time per Automation, a new urgent insert allowed only after the previous
  urgent member has finished — applies per Automation in 3a exactly as
  recorded, branch-aware in 3b.

### Operator open-question status (effect on this phase)

The decisions themselves are recorded verbatim in the Open questions section
(2026-10-22); this table is their status with respect to 3a/3b:

| OQ | Decision | Status w.r.t. 3a |
|---|---|---|
| 2 — nested Automations | **Allowed** (operator decision 2026-10-22) | **Implemented in 3a**: member kind `automation`, ownership + cross-level cycle rejection above; "same control ops" and PWA visible nested levels land in 3b / Phase 6 |
| 3 — `cancel_in_flight` | **No default — required, ask each time** | 3a fixes per-branch cancel semantics; **3b** makes the argument required on REST/MCP/CLI/comm and the PWA confirm dialog |
| 8 — agent insert + budget-on-breach | `agent_insert_policy` per Automation (default `allow`); pause only when a reset time is known, else block | **3b** (insert ops + budget `OnBreach`); 3a records the names and default so the branch-aware ops can key off them |
| 10 — fork/join + per-join rule | **Build now; `on_branch_failure` per join, default `all_must_succeed` wait-then-block** | **Core of 3a** — fully this phase (data model, semantics, rules, reconciliation, status, audit); PWA branch drawing in Phase 6 |
| 11 — planning setting | Separate optional per-Automation planning field, unset = global | Not 3a scope (Phase 4 tier work); a nested child carries its own field independently — no inheritance added by 3a |
| 12 — `executor_source` | Yes, run records gain `executor_source` | Not 3a scope (run-record schema); 3a audit entries already carry actor |
| 17 — advance gate + urgent lane | Diff-aware test check (BL413) is in scope; urgent lane per the recorded cap | 3a keeps the existing per-branch start gate unchanged (approval/defer/start-failure per cursor); the BL413 gate hook and the 1-urgent-member cap land with their own work and are branch-aware under the naming above |

### Phase 3a parity surface

The table below is the Phase 3a slice of the **Parity surface** table above,
for the surfaces the operator requires: REST, MCP, CLI, comm channel, PWA and
the companion app. "In 3a" means the build ships it; "deferred" names the phase
that does.

| Surface | In 3a? | What 3a ships |
|---|---|---|
| **REST** | Yes | The existing `/api/automations` CRUD accepts the new member fields; `GET /api/autonomous/status` returns `active_cursors` and `join_wait`. No new endpoints. |
| **MCP** | Yes | `automation_create` / `automation_update` forward the new member fields; `autonomous_status` carries the new fields. No new tools. |
| **CLI** | Yes | `datawatch automation create` / `update` accept member JSON; `list` / `get` print `active_cursors`. |
| **Comm channel** | Yes (read-only) | `automation list` and `automation get <id>`. Graph building stays off comm. |
| **PWA** | Deferred to Phase 6 | No PWA view in 3a. The pipeline strip must draw parallel branches and nested levels (Open question 10). |
| **datawatch-app (Android, iOS)** | Owed, filed at merge | Issue on `dmz006/datawatch-app`, filed when the 3a branch merges. Not filed in this build. |

### Phase 3a parity deltas

The Parity surface table above remains the plan of record. 3a's deltas:

- **REST:** no new endpoints — the existing `GET/POST /api/automations` and
  `GET/PUT/DELETE /api/automations/{id}` accept the new member fields
  (`kind`, `automation_id`, `fork_members`, `join_members`,
  `on_branch_failure`) with the validation addenda above, and
  `GET /api/autonomous/status` returns `active_cursors` / `join_wait`.
  `openapi.yaml` documents the new member fields. Invalid fork/join/nested
  shapes are `400` with member-ID-named messages; version conflicts stay
  `409`.
- **MCP:** `automation_create` / `automation_update` forward the new member
  fields through the unchanged `members_json` shape (their description text
  updated); `automations[]` on `autonomous_status` carries the new fields;
  no new tools. All follow the existing `internal/mcp` tool pattern.
- **CLI:** `datawatch automation create` / `update` (new subcommands, sibling
  of the existing `activate`, calling the same REST endpoints) accept the
  member JSON so an operator can build a fork/join or nested graph from the
  shell; `list` / `get` print `active_cursors` when present.
- **Comm channel:** `automation list` and `automation get <id>` verbs join
  the existing `activate` verb (graph building over one comm line is excluded
  from comm, per the table: create and edge rewiring stay on REST/MCP/CLI/PWA).
- **YAML/config:** no new keys in 3a (3b's `empty_edge_action` / insert
  policy keys are Phase 3b/5 territory with the table above).
- **PWA / datawatch-app (mobile parity rule):** 3a ships **no PWA view** —
  the pipeline strip must draw parallel branches and nested levels, which is
  Phase 6 scope per Open question 10. The `datawatch-app` parity issue for
  Automation (Android + iOS tracks) must explicitly include **parallel
  branches and nested levels in the pipeline strip**, the `active_cursors`
  / `join_wait` status fields, and no client-side assumptions about
  `current_member_id` alone. **Mobile parity: owed — file an issue on
  `dmz006/datawatch-app` when the 3a branch merges.** It is not filed in this
  build.

### Phase Completion Checklist (3a build)

Per AGENT.md's Phase Completion Checklist, the 3a build includes the functionality
and the **required test set** below, `go test ./...` green, a `docs/testing-tracker.md`
section for 3a, `docs/api/autonomous.md` and `docs/mcp.md` updates, and `openapi.yaml`
member fields. The datawatch-app issue is owed at merge (see **Phase 3a parity deltas**).
CHANGELOG and the version bump are release-pipeline work and are **out of scope for
this build** (the build must not edit CHANGELOG.md).

**Required test set (3a build).** Each item is a named test; each must pass in the build:

1. **Fork starts.** A fork fires every branch in sorted order. Each branch passes its own gate. A gate-denied or start-failed branch is recorded as a failed branch and does not block its siblings.
2. **Join rules.** One test per rule: `all_must_succeed` (also the default when `on_branch_failure` is omitted), `any_one_is_enough`, and `continue_anyway`. Each asserts the fire condition, the edge taken and the join decision.
3. **Join waits, then blocks.** With a non-success branch under `all_must_succeed`, the join does not fire while any branch is live. Once all branches are terminal it blocks with the named reason and emits `automation_join_blocked`.
4. **Nested outcome mapping.** A child that ends `completed` takes `on_complete`, `blocked` takes `on_blocked`, and `cancelled` takes `on_failed`.
5. **Nested cycle rejection.** Rejects direct self-reference, the two-cycle A → B → A, and a child that is already a member of another non-terminal Automation. Each error names the member ID.
6. **Restart mid-fork.** Branches already running keep running, unstarted branches start, and the join is fed exactly once.
7. **Restart mid-join.** Every branch is terminal before the crash. On replay the join fires or blocks exactly once, with the same decision as before the crash.
8. **Stale-version no-op.** A stale-version apply re-reads the record, re-evaluates, and changes nothing if the state has already moved.
9. **Status shape.** `active_cursors` lists in-flight members sorted by member ID. `join_wait` is present only while a join waits. The back-compat `current_member_id`, `current_prd_id` and `current_prd_title` derive from the first active cursor.
10. **Validation shape.** A branch carrying any outcome edge is rejected. A join carrying `on_blocked` or `on_failed` is rejected. A `join` that is not paired with exactly one fork is rejected. Each error names the member ID.

---

## Follow-up implementation PRD — what to build

- **Structs and store.**
  - A new `automation.go` in package `autonomous` holds the types (above) and the validation:
    `validateAutomation(a) error`, three-colour DFS, reachability, ownership.
  - `automation_store.go` handles JSONL load/save (same `loadJSONL` helper as
    `store.go:625`), an index `prdID → automationID`, and a per-ID mutex.
  - `PRD.AutomationID` is added in `internal/autonomous/models.go`.
- **Resolution-chain changes (exact functions).**
  - `resolveTaskBackendModel` (`executor.go:1607`) gains a param.
  - The effort block at `executor.go:831-836` changes.
  - `resolveVerificationLLM` / `ResolveVerificationLLM`
    (`verification_llm.go`) gains the param.
  - `VerifierSourceAutomation` is added.
  - Every caller is updated, including guardrail-adjacent code that calls these
    (`resolveGuardrailBackendModel` is **not** changed: guardrail routing has no
    Automation tier).
- **Coordinator subscriber.**
  - `Coordinator.onMemberTerminal` and `Coordinator.reconcile()`, called at the
    end of `resumeRunningPRDs`.
  - Registration on the BL427 dispatcher, or the `TEMPORARY(BL431)` hook.
  - The advance calls `API.Run` / `API.Resume`.
- **Control ops.** Manager methods return `"409: "` errors. REST handlers follow
  `internal/server/autonomous.go:1068-1103`, and the MCP and CLI wrappers mirror
  them.
- **BudgetPolicy enforcement hook points.**
  - The coordinator advance gate.
  - BL428's `admit` (`internal/autonomous/capacity.go:127`), which consults
    `automationCeilingFor(prd)`.
  - The shared `PRDCostUSD` rollup over `internal/session/cost.go`.
  - Window rollover handling for auto-resume.
- **`autonomous_status` extension.** `LoopStatus.Automations`, plus the idle-alert
  ticker.
- **Every parity surface** in the table above, including config keys in
  `handleGetConfig` / `handlePutConfig`, the PWA field arrays,
  `docs/config-reference.yaml`, `openapi.yaml` and `datawatch-app` issues.
- **Audit logging (AGENT.md Audit Logging Rule).** Every one of these events goes
  through `s.audit(…, "automation", id, …)` and the autonomous `SetAuditFn` mirror,
  in both JSON-lines and CEF:
  - `automation_create`, `automation_activate`, `automation_advance` (from →
    to member, edge, PRD outcome), `automation_skip_member`,
    `automation_insert_member` (AddedBy, AddedReason), `automation_remove_member`,
    `automation_reorder_edges`, `automation_pause`, `automation_resume`,
    `automation_cancel` (with `cancel_in_flight`), `automation_blocked` (reason),
    `automation_budget_breach` (spent, ceiling, window, action).
  - Each carries **actor** and **reason**.
  - Each gets a JSON round-trip test and a CEF escaping test with its
    `(signatureID, name, severity)` triple.
- **BL117 migration.** `ImportGraph(graphID)` handles single chains only and
  reports other shapes as not migratable. It reads real PRD status. Deprecation
  notices go on `orchestrator_graph_create/plan/run`. Graph history stays
  read-only.
- **Tests.**
  - Unit tests:
    - validation: a cycle via `OnFailed`, an unreachable member, a PRD in two
      active Automations, child and template rejection;
    - edge mapping for every PRD status in the table;
    - compare-and-set double-advance;
    - an insert-after-current racing an advance (`-race`);
    - `cancel_automation` default versus `cancel_in_flight`;
    - precedence tables for both chains with the Automation tier (same-level
      model, independent effort, `pool` bypass, source `automation`);
    - BudgetPolicy tightest-wins, pacing and on-breach;
    - **Phase 3a required set** (see the Phase 3a section): fork fires every
      branch (independent gates); the three join rules plus the default
      (`all_must_succeed`); join blocks with a named reason when its rule is
      unsatisfied; a fork branch that fails its start does not block siblings
      and counts as a failed branch; nested member starts the child and
      advances on `automation.completed` / blocks on `automation.blocked` /
      fails on `automation.cancelled`; cross-level cycle rejection (A → B → A
      and self-reference) and one-child-of-one-parent ownership; restart
      mid-fork (started branches untouched, unstarted branches started, join
      still fed once); restart mid-join (all branches terminal before the crash;
      the join fires or blocks exactly once on replay); a stale-version advance apply re-reads and no-ops;
      `active_cursors` and `join_wait` on the status shape, back-compat
      fields derived from the first active cursor; fork/join pairing and
      pairing validation (including branches that carry outcome edges) errors name the member.
  - E2E story ideas (sandbox daemon, per the E2E rules):
    - **(1) Advance with zero LLM or chat involvement.** Two trivial approved
      Automata in a linear Automation. Assert the second reaches `running` within
      one lifecycle event of the first's `completed`, with no MCP/REST call and no
      chat session in between. This is the stall regression test.
    - **(2) Insert while active.** While member A runs, `insert_member` after A
      with a reason. Assert the inserted member runs next, its
      `AddedBy`/`AddedReason` persist, and the original next member runs after it.
    - **(3)** A blocked member with an empty `OnBlocked` makes the Automation
      `blocked` and raises an alert, and `skip_member` resumes along the chosen
      edge.
    - **(4)** A restart while the member is mid-run, then a restart after the
      member completed while the daemon was down. Both reconcile correctly.
    - **(5)** A budget ceiling reached pauses the Automation and raises an alert.
    - **(6)** `cancel_automation` leaves the in-flight member running.

---

## Sibling PRD changes

- **BL428 (`a0669eb6`).** Its section 7a should be trimmed to a one-paragraph
  forward pointer to BL431. **Observed:** the landed BL428 doc already does this
  (`docs/plans/2026-10-14-concurrency-and-budget-design.md:724-740`, "7a. BL431
  pointer (Automation coordinator)"), and its stored PRD spec has no 7a text. The
  one remaining wording to reconcile: BL428 §7a says BudgetPolicy "narrows this
  doc's usage-budget ceiling", which matches this doc. The only stale wording is
  the qualifier "once it exists" after this doc's path
  (`docs/plans/2026-10-14-concurrency-and-budget-design.md:728`). This doc is now
  committed, so a later touch-up can drop that qualifier. Nothing else needs to
  change.
- **BL427 (`fe2e2110`).** It should change its cross-reference from "BL428's
  Automation coordinator" to **"BL431's Automation coordinator"**. **Observed:** the
  landed BL427 doc already says "BL431's Automation coordinator" (lines 69-88 and
  259). Its stored spec has no BL428 reference. One sentence is now stale: lines
  85-88 say BL431's doc "did not exist in the tree" and that the content is "not
  verified against BL431's doc text". That sentence can now point here.
- **This task does NOT edit either PRD's spec or doc.** Both docs exist in
  `docs/plans/`, so any wording touch-up, and the README registration of BL431,
  belong to a separate README-registration task.

---

## Open questions

1. **PWA tab ordering.** Should the Automation tab sit to the **left of**
   Automata (working assumption: larger scale first)? Or should it sit to the
   right, or be a sub-tab inside the Automata view (next to Automata / Templates)?
   **Decided by the operator 2026-10-22: left of Automata.**
2. **Nested Automations / an Initiative object.** Should an Automation be able to
   contain Automations (an "Automation of Automations", or a separate
   "Initiative")? This is **not decided either way**. Today's model allows only
   Automata as members, and adding a `MemberKind` later would be additive.
   **Decided by the operator 2026-10-22: allow nesting, in 10.0.0.** An Automation can contain another Automation as a member (a new `MemberKind`), with cycle detection across levels, clear ownership, and the same control ops. It is designed together with fork/join in Phase 3a, and the PWA pipeline strip must show nested levels.
3. **The cancel_automation default.** Recommended: halt future auto-advance only,
   and leave the in-flight member running; `cancel_in_flight: true` is opt-in.
   Should the default be reversed?
   **Decided by the operator 2026-10-22: ask each time.** There is no default. `cancel_in_flight` is a required argument on every surface (REST, MCP, CLI, comm channel); a call that omits it is rejected with a message naming the two choices, and the PWA shows a confirm dialog that asks the same thing.
4. **BL117 orchestrator.** Recommended: supersede, with a single-chain
   `import-graph`, read-only history, and the create/plan/run tools deprecated then
   removed after one minor. Alternatives: extend BL117's Graph in place, or keep
   both permanently.
   **Decided by the operator 2026-10-22: supersede.** Single-chain `import-graph`, read-only history, `orchestrator_graph_create/plan/run` deprecated then removed after one minor.
5. **One implementation PRD or several, and in what order.** Should BL427's
   dispatcher, BL428's mechanics and this coordinator be separate implementation
   PRDs in the confirmed order BL427 → BL428 → BL431? Or should BL431 Phases 1-3
   land before BL428 Phase 2, behind a "refuse to activate a second Automation in
   the same `project_dir`" interim guard? Should the Automation be used to run
   BL428's own implementation (dogfooding, per the README addendum)?
6. **The default for an empty edge.** An empty `OnBlocked` / `OnFailed` currently
   means stop and alert, with the Automation `blocked`. Should it be `paused`
   instead, which auto-resumes once the operator fixes the member? An empty
   `OnComplete` always completes the Automation.
7. **Terminology collision.** AGENT.md's Terminology Rule says "never use
   'Automation'" when translating Automaton. The PWA FAB "Launch Automation"
   (`fab_launch_auto`) launches an Automaton. Approve: (a) a Terminology Rule
   addendum that defines Automation as this object, with per-locale translations;
   (b) relabelling that FAB "Launch Automaton".
   **Decided by the operator 2026-10-22: define both and relabel the button.** AGENT.md's Terminology Rule gets an addendum defining Automation (the chain object) and Automaton (one PRD) with per-locale translations; the `fab_launch_auto` button is relabelled "Launch Automaton" in all locales.
8. **Agent and session inserts, and budget breach action.** May an agent or session
   (`AddedBy: session:<id>`) insert members without operator approval?
   Recommended: yes, because the inserted Automaton still has to pass its own
   `approved` gate before the coordinator will run it, and the insert is audited.
   The alternative is `agent_insert_policy: require_operator`. Separately, should a
   breached BudgetPolicy **pause** (recommended default; auto-resumes on window
   rollover) or **block** (needs an operator decision)?
   **Part one decided by the operator 2026-10-22: per-Automation setting.** Each Automation carries `agent_insert_policy` (`allow` | `require_operator`), with a global default of `allow` (config key, settable on every surface). An insert from an agent or session under `require_operator` is held pending until the operator approves it; under `allow` the inserted Automaton still passes its own `approved` gate and the insert is audited. **Part two decided by the operator 2026-10-22: pause only when the reset time is known, otherwise block.** A breached BudgetPolicy pauses and auto-resumes at the window rollover when datawatch knows the reset time (a dollar window it defines itself, anchored at the operator-declared reset, or a reset time parsed from Claude's own limit message); if the reset time is unknown the Automation goes `blocked` and waits for the operator. An alert is raised either way.
9. **`allocation_pct` estimation.** Is an "estimated" allocation share acceptable,
   given that the subscription meter cannot be read (BL428 §4a)? It would be token
   usage priced at API rates against an operator-declared notional allocation per
   `plan_tier`. The alternative is to ship `dollar` only and defer `allocation_pct`.
    **Decided by the operator 2026-10-22: ship both, estimate off by default.** The `dollar` ceiling is the real mechanism. `allocation_pct` also ships, opt-in per Automation, always labelled `estimated`, computed from token usage priced at API rates against an operator-declared notional allocation per `plan_tier`.
10. **Fork/join inside one Automation.** Parallel work is currently expressed as
    separate concurrent Automations. Is a true parallel fork with a join member
    needed inside one Automation? That would need a multi-cursor model.
    **Decided by the operator 2026-10-22: build fork/join now (in 10.0.0).** One Automation can contain a fork (start several member Automata at the same time) and a join (wait for all of them, with a per-join rule for what happens if one branch fails or is skipped) before continuing. This replaces the single cursor with a set of active cursors, so it lands as its own phase *before* the control operations: Phase 3a designs and builds the multi-cursor coordinator (data model, advance rule, restart reconciliation, `autonomous_status`, audit), then Phase 3b builds the control ops on top and every op names the branch it acts on. The PWA pipeline strip must draw parallel branches (Phase 6).
    **Join failure rule decided by the operator 2026-10-22: set per join.** Each join member carries `on_branch_failure`: `all_must_succeed`, `any_one_is_enough` or `continue_anyway`. The default for a join that does not set it is `all_must_succeed` with wait-then-block: the join waits for the other branches to finish, then the Automation goes `blocked`, alerts, and names the failed branch.
11. **Decompose tier.** Should the Automation-level model default also cover
    decomposition (`PRD.DecompositionModel` / the decompose backend), or only
    execution and verification?
    **Decided by the operator 2026-10-22: separate planning setting.** The Automation model default covers execution and verification. Planning has its own optional field on the Automation (unset = the global planning backend); it does not change the run/verify default. Exposed on every surface like the other tier fields.
12. **Execution provenance.** Should task run records gain an `executor_source`
    (`task|story|prd|automation|global`) mirroring `verifier_source`, so the
    Automation tier's effect is visible on execution too?
    **Decided by the operator 2026-10-22: yes.** Task run records gain `executor_source` (`task|story|prd|automation|global`) beside `verifier_source`.
13. **Record `c187962a`'s title.** It says "BL430 — Automation…" while the backlog
    number is BL431. Should it be retitled, or left alone with this doc and the
    BL428 doc noting the collision?
14. **Federation-aware BudgetPolicy (operator-raised 2026-10-14, see the
    forward pointer above and BL428 §3c/§4e).** Once BL428's fingerprint-sync
    confirms a federated peer shares a billing account or compute node,
    should `BudgetPolicy`'s ceiling check read that peer's usage into the
    same `tightest-wins` math by default, or stay per-daemon unless the
    operator explicitly opts an Automation into federation-wide ceilings?
    **Decided by the operator 2026-10-22: opt-in per peer.** A daemon counts only its own usage by default; shared counting is switched on per peer (and per Automation) after fingerprint-sync has confirmed the peer shares the billing account or node.
15. **Local-model decision assistant (operator-raised 2026-10-14, see the
    section above).** Is the four-hook-point scope (empty-edge resolution,
    skip_member pre-screening, mid-flight insert triage, budget-breach
    response) the right starting set, or should it be narrower for v1 (e.g.
    empty-edge resolution only, since it's the one with the clearest
    existing evidence shape via `FailureReason`)? Should a confidence
    threshold below which it must escalate be a single global number, or
    per-hook-point? Does this ship in BL431's own follow-up implementation
    PRD, or as its own later PRD once the Automation coordinator exists to
    attach it to?
    **Decided by the operator 2026-10-22:** all four hook points (empty-edge resolution, skip_member pre-screening, mid-flight insert triage, budget-breach response) are in scope for the first version; the escalation confidence threshold is set per hook point with a conservative (high) default, all editable; it ships as its own PRD after Phase 3a/3b exist, and must be in 10.0.0.
    **Status 2026-10-23:** the assistant is built: all four hook points, per-hook thresholds, shadow and live modes, the hard-requirement guard, and the REST, MCP, CLI, comm and web surfaces. See [automation-decision-assistant](../howto/automation-decision-assistant.md). Which model judges these decisions well is the BL437 evaluation sweep's question; that is out of scope here and no default model is chosen.
16. **Dashboard/Observer integration (operator-raised 2026-10-14, see the
    section above).** Should the `automation-summary` dashboard card and
    the Observer per-Automation rollup ship as part of BL431's own
    follow-up implementation PRD (Phase 1, alongside the CRUD surfaces,
    since both read-only views need no more than the data model to exist),
    or as a separate later PRD once an Automation actually has members to
    show? Should staleness/idle-duration alerting on the dashboard card be
    in scope for v1 given it would have caught today's two silently-dropped
    PRDs, or is that premature before BL427's event dispatch can push the
    alert instead of requiring a poll?
    **Decided by the operator 2026-10-22:** ship the `automation-summary` card and the Observer per-Automation rollup with the first Automation surfaces; idle/staleness alerting on the card is left until event push exists.
17. **The coordinator as a real quality/regression gate, not just a status
    poll (operator-raised 2026-10-15, live incident: two PRD tasks failed
    and sat silently blocked for ~50 minutes before the operator noticed,
    not the coordinator).** The advance decision (`onMemberTerminal`)
    should not just check "did this member reach a terminal status" — it
    should verify the terminal status is actually trustworthy before
    treating it as ready to advance:
    - **Test-coverage validation, not just a passing suite.** When a
      member's work adds new functionality, does a test exist for it
      specifically — not just "did `go test ./...` exit 0" (a new code
      path with zero new tests can pass the existing suite trivially).
      This is already an AGENT.md rule (the Phase Completion Checklist);
      the gap is that nothing currently *enforces* it mechanically before
      advancing. Candidate mechanism: reuse BL413's diff-aware
      project-rules work (once it exists) rather than building a second
      diff-coverage checker.
    - **Regression coverage for cross-cutting surfaces specifically** —
      comms, config, MCP, API, PWA. A change touching any of these needs
      the relevant E2E/smoke/unit/functional suite to have actually run
      and passed, not just the package-local unit tests. Which suites map
      to which surfaces needs a concrete table (this doc does not define
      one yet).
    - **Alerting is not optional instrumentation — it's how the operator
      finds out something needs them**, per the live incident above. The
      coordinator's own idle/stall detection (open question 16) and this
      gate are the same underlying need: don't let a bad terminal state
      sit silent.
    - **Dynamic reprioritization.** The Automation's own member order is
      feature/precedence-driven (this doc's §"Dynamic mid-flight editing"
      already covers insert/skip/reorder mechanically) — but should a
      *bug report arriving mid-run* be able to automatically jump ahead of
      the current queue position, or does that always require an explicit
      operator `insert_member` call? If automatic, what's the trigger
      (severity field? a dedicated "urgent" Automation lane?) and what
      stops a bug-reprioritization loop from starving the original queue?
    Open: does this gate live entirely in BL431's own coordinator, or does
    it also need pieces from BL410 (retry/failure classification — already
    exists), BL413 (diff-aware rules — not yet built), and a concrete
    surface→test-suite mapping that doesn't exist anywhere today? Scoping
    this precisely is itself real design work, not a one-line addition.
    **Decided by the operator 2026-10-22 (advance gate): add the diff-aware test check, in 10.0.0.** Before advancing, the coordinator verifies the member's terminal status is trustworthy: guardrails and verification passed, loud alerts on any blocked or failed member, and new functionality must come with a specific new test. That check is BL413 (diff-aware project-rules enforcement), which is now in the 10.0.0 scope and needs its own design pass first.
    **Decided by the operator 2026-10-22 (bug reprioritization), as recorded from the operator's free-text answer:** an arriving bug goes to a dedicated urgent lane (a separate Automation running alongside the main one) when that lane is available. If the urgent lane is not available and the bug is blocking the running Automation, it may be inserted ahead with `urgent` severity (audited). Otherwise it joins the regular queue, placed before the members it could block. Cap on automatic urgent inserts, decided by the operator 2026-10-22: only one urgent member at a time per Automation; a new urgent insert is allowed after the previous urgent member has finished.
18. **Tree-conflict assessment is BL428 §3a's scope, not this doc's — the
    coordinator just has to consume it faithfully (operator-raised
    2026-10-15, see BL428's own open question 14 for the full write-up).**
    Live judgment call made by hand while running PRDs tonight: a docs-only
    PRD was allowed to run concurrently with a code-change PRD on the same
    main tree, reasoned from actual file footprint rather than treating
    "same tree" as automatically exclusive. Whatever BL428 §3a lands on
    (binary lock vs. footprint-aware concurrent admission) is what
    `onMemberTerminal`/the advance decision must respect — this doc should
    not grow a second, competing conflict-assessment mechanism.

    **Update (operator-raised 2026-10-15, later same night): the simpler
    fix may just be the right one.** Three separate real incidents
    tonight — not hypothetical — had a PRD's own verification polluted by
    unrelated commits/uncommitted changes landing on the same shared main
    tree while it ran (BL452 twice, BL441 once). All three happened on
    PRDs created before worktree-isolation became the default pattern
    this session (omitting `project_dir` auto-isolates into
    `~/.datawatch/prd-worktrees/<id>/`). Given real evidence now exists
    that shared-tree concurrency causes real breakage, not just a
    theoretical risk, BL428 §3a's footprint-aware concurrent-admission
    design may be solving a harder problem than needed — defaulting every
    PRD to worktree isolation unconditionally (never the main tree unless
    explicitly opted in) is simpler and would have prevented all three
    incidents outright. Worth weighing against footprint-aware admission
    when BL428 §3a is actually decided, not assumed to need the more
    complex mechanism.
19. **Failure-triage decision tree + after-advance validation checklist
    (operator-raised 2026-10-15, codifying live judgment calls made by hand
    tonight rather than leaving them as folklore).** The interim manual
    monitoring loop (a 5-minute operator-approved poll — see
    `project_continuous_monitoring_stopgap` in Claude's memory) is already
    making real triage decisions this coordinator needs to make
    mechanically once it exists:
    - **On a newly-failed/blocked task, before any auto-action**, pull the
      task's own `FailureReason` and (when still available) the actual
      session output — never trust the stored status string alone.
      Classify with BL410's existing taxonomy (infra/content/scope/policy),
      then branch:
      - **infra** (the mechanism itself didn't finish — e.g. backend
        unreachable): safe to auto-retry within BL410's existing
        budget/escalation, no operator involvement needed.
      - **content, matching a signature an open research/fix PRD is
        already tracking** (worked example tonight: BL464's task failed
        with "verifier: no changes detected ... task produced no output" —
        the exact signature BL455 is already investigating, now a 3rd
        confirmed occurrence after BL427 Phase 2 and BL431 Phase 1). Do
        **not** retry blindly. Leave it blocked, record it as corroborating
        evidence on the tracking PRD (a `Decision`/`Learning` cross-link,
        not just a chat message), and auto-resume it only once the tracking
        PRD ships its fix — mirroring what the manual loop did by hand for
        all three PRDs tonight.
      - **content, NOT a known tracked signature** (a genuinely new failure
        shape): do not auto-fix silently either. Surface it immediately
        (alert/dashboard, not a buried log line) and hold for an operator
        or a higher-tier model's judgment — the coordinator should not
        "fix itself" on a novel failure with no human or higher-tier model
        in the loop, per the small-model-oversight finding already in
        Claude's memory (`project_small_model_retry_needs_higher_oversight`).
      - **scope** (the spec itself is structurally wrong): never retry the
        identical spec; either auto-`request_revision` naming the specific
        gap, or hold for an operator edit.
    - **On a task/story/PRD reaching a terminal `completed` state, before
      advancing past it**, run the same after-done checklist the operator
      has been applying by hand all session (`feedback_approval_bar` in
      Claude's memory): version-bump parity (`main.go` + `api.go` together),
      a CHANGELOG entry in plain operator language with no leaked tracker
      IDs, real tests for any new functionality (not just a passing
      pre-existing suite), full parity surface for anything touching
      comms/config/MCP/API/PWA (REST+MCP+CLI+comm+PWA, not REST alone),
      docs cross-references updated, and the PRD's own roadmap entry
      closed. Anything missing here is itself a content-category gap —
      route it through BL410 Phase 2's existing corrective-work synthesis
      rather than marking the PRD done and advancing past it.
    - **Regression-suite cadence should be automatic, not operator-recalled.**
      Trigger a scoped regression pass whenever a just-completed PRD
      touched a cross-cutting surface (comms/config/MCP/API/PWA — the same
      surface table open question 17 above already calls for), and
      otherwise on a coarse periodic cadence (e.g. once per N completed
      PRDs, or once per sustained idle window) so coverage doesn't depend
      on a human remembering to kick it off.
    - **Honest scope of the interim stopgap, until this ships**: the manual
      5-minute wakeup loop approximates the failure-triage half (classify,
      hold known signatures, flag idle GPUs) but does **not** currently
      re-run the after-done checklist on a newly-`completed` PRD, nor
      auto-trigger regression — those still require an operator or Claude
      noticing in a live conversation turn. That gap is this open
      question's direct motivation, not a hypothetical one.

20. **A fourth `content` branch for Open Question 19's decision tree:
    scope-too-broad, auto-split instead of blind-retry (operator-raised
    2026-10-16, live incident on BL465's own pilot run).** Running BL465
    itself through pi-prompt hit the exact failure shape this doc already
    warns about: one task ("wire the deterministic commit into 9
    `StateComplete` call sites, delete the old mechanism, and write a
    final audit") produced a real but incomplete diff and a second task
    ("write a bash smoke script and a Go test") produced none at all.
    Both were manually diagnosed, by hand, as **one task bundling
    multiple separable deliverables** — not wrong content, not a stall —
    and fixed by hand-splitting into smaller tasks and re-inserting them
    in the correct order. That hand process is exactly what Open Question
    19's `content` branch is missing a case for: today it only
    distinguishes "known tracked signature, wait" from "novel, escalate
    to a human or higher-tier model." Neither fits "the spec itself asks
    for too much in one unit" — that failure is self-correcting once
    recognized, and recognizing it does not need a human.

    **The concrete mechanism, composing the three pieces the operator
    asked about directly:**
    - **Decompose-time prevention (the "better decomposer" half) is the
      first line of defense**, not a replacement for this. The atomic-
      granularity default already discussed this session (a
      `task_granularity: atomic | standard` config switch, not yet
      built) means most PRDs never reach this failure at all. This open
      question's mechanism exists for the remainder: a task that still
      turns out too broad despite atomic decompose (decompose cannot be
      perfect), or any PRD decomposed before the switch existed.
    - **BL452 ("The V Method") is the classifier**, extended with one
      more closed-form question beyond the ones already in its table
      (verdict `ok`, `severity`, guardrail outcome): *"Does this task's
      own spec describe one coherent code change, or does it bundle
      multiple separable units (different files, languages, or
      call-sites that could each be its own task)?"* Same shape as
      BL452's existing rows — one forward pass, no free text to parse,
      cheap enough to run on every `content`-category failure, not just
      the ones already escalated to a human. Add it to BL452's own
      table as a new row (see that doc).
    - **BL410's `FailureReason` taxonomy gets one new signal**, set by
      that classifier call when a `content` failure fires: a
      `scope_too_broad` flag (or a fifth taxonomy value — BL452's own
      doc decides the exact shape when it implements the row above).
      This is additive to BL410's existing `infra | content | scope |
      policy` categories, not a replacement for any of them.
    - **The orchestrator's failure-triage tree (this question) gains the
      branch**: `content + scope_too_broad` → do not blind-retry and do
      not just escalate. Re-invoke decompose, scoped narrowly to *just
      this one task's own spec* (not the whole PRD), asking it to split
      into N atomic sub-tasks. Insert them **in place of the original
      task**, chained by `depends_on` in the order decompose returns
      them, and chain whatever already depended on the original task
      onto the **last** inserted sub-task. Mark the original task
      `skipped` with a cross-reference to its replacements (not
      `failed` — it was correctly diagnosed, not wrong), audited the
      same way `skip_member` already is at the Automation level.

    **The mechanical blocker this exposed, now fixed (2026-10-16, v9.1.24):**
    `AddTask` (REST `add_task`, MCP `autonomous_prd_add_task`, CLI
    `prd-add-task`) had no way to set `depends_on` on a newly-inserted
    task at all — the hand-fix above had to work around it twice tonight
    by removing and re-adding every later task in array order. This is
    now a real parameter at every layer (`internal/autonomous/store.go`'s
    `AddTask`, validated against the PRD's existing task IDs), which is
    the exact primitive the auto-split mechanism above needs to insert
    replacement tasks correctly — it did not exist until tonight, and
    without it this open question had no way to be implemented at all,
    by a human or by the orchestrator.

    **Scope note, same as the rest of this doc:** this is still
    **intra-PRD** (routing within one PRD's own task graph), distinct
    from the **inter-PRD** member-sequencing this doc's main body
    designs. It is the first concrete resolution of the "intra-PRD
    adjust-and-retest loop" named earlier (**Local-model decision
    assistant**, the "Two distinct routing layers" passage) — narrower
    than that passage's full scope (varying tools, hints, or model tier
    per retry), but a real, buildable first slice of it: one failure
    signature, one corrective action, reusing mechanisms (decompose,
    `AddTask`+`depends_on`, BL452's classifier shape, BL410's taxonomy)
    that already exist or are now unblocked.

    **Not yet decided:** whether this ships as part of BL431's own
    follow-up implementation PRD (same question Open Question 15 asks
    about the decision-assistant hook points generally) or as its own
    PRD once BL452's classifier row and the atomic-decompose switch
    exist to build on. Given it only needs `AddTask`+`depends_on` (now
    shipped) and a single new classifier question (small, additive to
    BL452's existing design), it is a reasonable **first** MVP slice of
    the decision-assistant concept — smaller in scope than any of the
    four original hook points, and backed by two real incidents in one
    night rather than a hypothetical.

## Example rule: one-shot session lifecycle (validate before commit)

Added 2026-10-17 after an ad-hoc one-shot pi session committed its own
unvalidated work. This is an example rule for any datawatch user's
automation, not a fixed policy: every value below is configurable.

**Rule.** A one-shot session that changes code must pass validation
before it may commit or complete. Validation failure restarts the
session (bounded), and only a pass commits and completes it.

**Lifecycle.**
1. **Start.** The session declares its task, its write scope (paths it
   may change), and one or more validation commands (for example
   `go build ./...` and a targeted `go test`).
2. **Work.** The session edits only inside its declared scope.
3. **Validate.** Datawatch runs the declared validation commands in the
   session's worktree, outside the worker's own shell. Results are
   recorded on the session.
4. **Pass.** Stage only the declared scope, commit with the session's
   message, then mark the session complete and close its tmux pane.
5. **Fail.** Restart the session with the failing output fed back as
   input, up to `max_attempts`. After that, stop, leave the changes
   uncommitted, and report. Never commit a failing tree.

**Configurable (defaults shown).**
- `validate_commands`: none by default, so a session with no commands
  must say so explicitly and is reported as unvalidated.
- `max_attempts`: 2.
- `auto_commit`: false for one-shot sessions started outside a PRD. A
  PRD task may auto-commit after its own verifier passes.
- `write_scope`: required for any auto-commit; no scope means no commit.

**Why.** The leak-fix one-shot (session `leak-fix-v3`) committed its
work through datawatch's completion path without running validation,
and a broken build went into history. Completion commits must be gated
by validation, not by the session finishing.

## Phase 7 parity audit

Audit of every Automation operation and every `autonomous.automation` config key
against each parity surface. `TestAutomationParity` in
`cmd/datawatch/automation_parity_test.go` enforces the REST, MCP, CLI and comm
columns; the web and audit columns are audited by hand. The REST operation list is
`AutomationRESTOperations()` in `internal/server/automation_ops.go`, and the control
router uses the same list. Create, update and delete are kept off the comm channel by
design (the test's `commExempt` map holds the reasons), so the comm cell for those is
`Exempt`.

### Operations

| Operation | REST | MCP | CLI | Comm | Web | Audit |
|---|---|---|---|---|---|---|
| list | Present: `internal/server/autonomous.go` | Present: `internal/mcp/automation_tools.go` | Present: `cmd/datawatch/cli_automation.go` | Present: `internal/router/automation_cmd.go` | Present: `app.js` automation list | Exempt: read-only, changes nothing, so no audit event |
| get | Present: `internal/server/autonomous.go` | Present: `automation_tools.go` | Present: `cli_automation.go` | Present: `automation_cmd.go` | Present: `app.js` member drawer | Exempt: read-only, changes nothing, so no audit event |
| create | Present: `autonomous.go` | Present: `automation_tools.go` | Present: `cli_automation.go` | Exempt: member list does not fit a chat message | Present: `app.js` create form | Present: `automation_create` |
| update | Present: `autonomous.go` | Present: `automation_tools.go` | Present: `cli_automation.go` (update) | Exempt: member list does not fit a chat message | Present: `app.js` edit form | Present: `automation_update` |
| delete | Present: `autonomous.go` | Present: `automation_tools.go` | Present: `cli_automation.go` (delete) | Exempt: destructive, not offered from chat | Present: `app.js` remove control | Present: `automation_delete` |
| activate | Present: `autonomous.go` | Present: `automation_tools.go` | Present: `cli_automation.go` | Present: `automation_cmd.go` | Present: `app.js` automation_activate | Present: `automation_activate` |
| pause | Present: `autonomous.go` | Present: `automation_tools.go` | Present: `cli_automation.go` | Present: `automation_cmd.go` | Present: `app.js` automation_pause | Present: `automation_pause` |
| resume | Present: `autonomous.go` | Present: `automation_tools.go` | Present: `cli_automation.go` | Present: `automation_cmd.go` | Present: `app.js` automation_resume | Present: `automation_resume` |
| cancel | Present: `autonomous.go` | Present: `automation_tools.go` | Present: `cli_automation.go` | Present: `automation_cmd.go` | Present: `app.js` automation_cancel | Present: `automation_cancel` |
| skip_member | Present: `autonomous.go` | Present: `automation_tools.go` | Present: `cli_automation.go` (skip) | Present: `automation_cmd.go` (skip) | Present: `app.js` automation_skip_member | Present: `automation_skip_member` |
| insert_member | Present: `autonomous.go` | Present: `automation_tools.go` | Present: `cli_automation.go` (insert) | Present: `automation_cmd.go` (insert) | Present: `app.js` insert dialog | Present: `automation_insert_member` |
| remove_member | Present: `autonomous.go` | Present: `automation_tools.go` | Present: `cli_automation.go` (remove) | Present: `automation_cmd.go` (remove) | Present: `app.js` automation_remove_member | Present: `automation_remove_member` |
| reorder_edges | Present: `autonomous.go` | Present: `automation_tools.go` | Present: `cli_automation.go` (edges) | Present: `automation_cmd.go` (edges) | Present: `app.js` automation_reorder_edges | Present: `automation_reorder_edges` |
| approve_insert | Present: `autonomous.go` | Present: `automation_tools.go` | Present: `cli_automation.go` (approve) | Present: `automation_cmd.go` (approve) | Present: `app.js` automation_approve_insert | Present: `automation_approve_insert` |
| set_llm | Present: `autonomous.go` | Present: `automation_tools.go` | Present: `cli_automation.go` (set-llm) | Present: `automation_cmd.go` (set-llm) | Present: `app.js` planning pair saved through set_llm | Present: `automation_set_llm` |
| set_verification_llm | Present: `autonomous.go` | Present: `automation_tools.go` | Present: `cli_automation.go` (set-verification-llm) | Present: `automation_cmd.go` (set-verification-llm) | Present: `app.js` openAutomationVerificationLLMDialog | Present: `automation_set_verification_llm` |
| set_budget | Present: `autonomous.go` | Present: `automation_tools.go` | Present: `cli_automation.go` (set-budget) | Present: `automation_cmd.go` (set-budget) | Present: `app.js` openAutomationBudgetDialog | Present: `automation_set_budget` |
| status | Present: `GET /api/autonomous/status` automations[] | Present: `internal/mcp/autonomous.go` (autonomous_status) | Present: `cli_automation.go` (status) | Present: `automation_cmd.go` (status) | Present: `app.js` loadAutomationLiveStatus | Exempt: read-only, changes nothing, so no audit event |
| import-graph | Gap: built by the import-graph tasks | Gap: built by the import-graph tasks | Gap: built by the import-graph tasks | Gap: built by the import-graph tasks | Gap: built by the import-graph tasks | Gap: built by the import-graph tasks |

### Config keys under `autonomous.automation`

| Key | Config file | REST config | MCP config | CLI `config set` | Comm `configure` | Web Settings field | en | de | es | fr | ja |
|---|---|---|---|---|---|---|---|---|---|---|---|
| idle_alert_minutes | Present: `internal/config/config.go`, `docs/config-reference.yaml` | Present: `internal/server/api.go` | Present: `autonomous_config_set` (automation_idle_alert_minutes) and `config_set` | Present: generic `datawatch config set` | Present: `internal/router/router.go` | Present: `app.js` settings_automation_idle_alert_minutes | Present: `locales/en.json` | Present: `locales/de.json` | Present: `locales/es.json` | Present: `locales/fr.json` | Present: `locales/ja.json` |
| agent_insert_policy | Present: `internal/config/config.go` | Present: `internal/server/api.go` | Present: `autonomous_config_set` (automation_agent_insert_policy) and `config_set` | Present: generic `datawatch config set` | Present: `internal/router/router.go` | Present: `app.js` settings_automation_agent_insert_policy | Present: `locales/en.json` | Present: `locales/de.json` | Present: `locales/es.json` | Present: `locales/fr.json` | Present: `locales/ja.json` |

### Gaps for the next tasks

The verb-surface and web gaps of the operations table are closed. The test's
`knownParityGaps` map now holds only the four `import-graph` entries, which the
import-graph tasks remove when they build that operation. The audit column found no
gap for the read operations (list, get, status): they change nothing, so they emit
no audit event. The config rows are all Present, and
`TestAutomationConfigKeysEverySurface` in `internal/server/automation_config_surfaces_test.go`
covers the REST and comm `configure` round trips for both keys.

The design's provisional keys `enabled` and `empty_edge_action` were not built; they are
not built, no surface owed.

## Supervisor duties the Automation coordinator must absorb (added 2026-10-24)

These came out of 2026-10-24 incidents, where the human supervisor loop missed them. Each is a requirement on the coordinator, written as behaviour; the numbers (times, counts) are configurable policy settings whose defaults the operator decides.

1. **Watch CI for every workflow, not only the release one.** After a push or a tag the coordinator lists the result of every workflow on the head of main and on the tag (release, security scan, docs sync, secret scan, CI summary). A failed or stuck run is read, classified and either fixed through a fix PRD or reported with its failing job and log excerpt. A hung step is rerun once. A red workflow is never shown as running. Failed runs are deleted after a confirmed green fix. (Security scan sat red for a day while the loop only watched the release workflow.)
2. **Security-failure exception.** A dependency or CVE failure from a security workflow for an advisory older than 72 hours that the project already suppresses is never fixed or documented by the coordinator: it stops and sends the operator the dependency, advisory id, publication date, allowlist entry and an upgrade-or-keep recommendation. Suppressions are written only to the accepted-risks registry, with the real first-added date and a real trace, never to the generated ignore file.
3. **Merge pending is visible and alerting.** A completed PRD whose commits are not on main, compared by commit content and not by commit count, shows as merge pending in the Automation view and raises an alert after a configurable time. A branch whose commits are all on main is reported as safe to delete. (A finished lane-fill PRD sat unmerged for five days and the feature was built a second time.)
4. **Search before create.** Creating a PRD warns, without blocking, when its title or spec overlaps an existing PRD of any status, a registry row, or an unmerged branch, and names what it found.
5. **Failed and blocked PRDs are triaged every poll.** A PRD that moves to failed or blocked is reported with the failed task, its error and the recorded session end reason, and the coordinator applies the retry policy before it waits for a person. (A PRD sat failed for hours with an idle lane behind it.)
6. **A lane whose PRDs all wait on verifiers is idle.** Lane fill counts a Claude lane as available when every PRD on it is waiting for a verifier slot, and starts another PRD there, under the operator's lane count and usage policy.
7. **Gate implementation on open design points.** A task that depends on an operator decision starts only when the decision is recorded; otherwise it stops and reports the missing decision.
8. **Gate on a task that mentions an error phrase.** Stall and failure detectors match the tool's own error line for the backend they were built for, never ordinary output text.

Open for the operator: the default alert times and the retry count for items 1, 3 and 5, and whether item 1 may rerun a hung step without asking.

## Operator decision channel: questions, decisions and design through datawatch (requested 2026-10-25)

**Operator request, quoted (2026-10-25 07:28):** "It would be good if supervisor communicated with datawatch in json or something and questions and decisions and design were through datawatch interface and not in claude terminal, stored up things, track input and quotations, add to design"

**Why now.** On 2026-10-24 a blocking multiple-choice question in the supervisor's terminal stopped the 30-minute ticks for 16 hours because the ticks only run while the session is idle; the lanes sat idle overnight. Decisions given in a terminal are also lost to everything else: they are recorded by hand in plan docs, the exact words of the operator are paraphrased, and nothing links an answer to the PRD, task or proposal that asked for it.

**What exists today (read, not assumed).**
- The Decision Interview Protocol in AGENT.md (context, options, recommendation, one question, wait): defined for the agent, answered in the terminal.
- A session that prints `DATAWATCH_NEEDS_INPUT: <question>` is detected and shown as needing input; the operator answers by sending text to the session.
- PRD records carry a `decisions` list (approve, run, coverage acknowledgements, set LLM, requeue) with actor, time and note.
- Lifecycle events (notify only) can tell a channel that a PRD needs review or is blocked; they carry no question and take no answer.
- Memory discussions (`memory_discussion_write`/`recall`, `reply_to_parent`) carry messages between sessions, federation-aware and audited.

**Requirements, as behaviour.**
1. **A structured question.** A supervisor or a worker raises a question as data, not terminal text: an id, who asked, the context (PRD, task, proposal, or none), the text, the options with their trade-offs, the recommendation, an optional deadline and a default that applies if none is given (never a silent choice), and a state (open, answered, withdrawn, expired).
2. **A question queue in datawatch.** Open questions are stored by the daemon, listed in the Automation view and the mobile apps, in the CLI, the comm channels and over REST and MCP, and surface as notifications. Many can be open at once; none blocks the asker's other work or the ticks.
3. **Answers are recorded with the operator's own words.** An answer stores the chosen option and the operator's free text verbatim, who gave it, from which surface and when. Answers are immutable; a changed mind is a new answer that supersedes the old one.
4. **Return path as data.** The asker receives the answer as structured data (a JSON message to the session, a polled endpoint, or a lifecycle event) and continues; a session that asked is not required to stay idle.
5. **Decisions link to what they decide.** An answer links to the PRD, task, proposal file or registry row, and a proposal file's `Confirmed:` line can be set from the answer instead of by hand. The PRD `decisions` list references the answer id.
6. **Design stays in datawatch.** A design question (options, mockups, recommendation) is a record the operator reviews in the interface; the accepted design is written back to the design document with the quotation and a link to the answer, so the design doc, the registry and the decision log agree.
7. **Search and audit.** All questions and answers are searchable, exported with the audit log, and kept across restarts. Every answer shows its quotation wherever the decision is cited.
8. **Parity.** Every surface in the Configuration Accessibility Rule and the Mobile-Parity Rule (config, REST, MCP, CLI, comm channel, PWA, mobile apps).

**Open for the operator (not decided here).** Where the queue is shown first (Automation view or a new tab); whether a question may carry a deadline and what happens at expiry; who may answer when several operators exist; whether the supervisor's own tick may keep working with a default for low-risk questions; the retention of answered questions; how the Decision Interview Protocol in AGENT.md changes (ask through the queue first, terminal only as fallback).

**Operator decisions, 2026-10-25 (quoted):** "Queues are a part of automation.  No expiring, unless operator says to slip out use supervisor decision,  low risk would be an option set in automation,  any other questions"

1. **The question queue belongs to Automation.** It is part of the Automation subsystem and its view, not a separate tab or a separate subsystem.
2. **Questions never expire.** An open question stays open until answered or withdrawn. The one exception is when the operator says to skip it ("slip out": read as skip); then the supervisor decides, and the record shows the answer as a supervisor decision made on the operator's skip, with its reasoning.
3. **Low-risk handling is an Automation setting.** Whether a low-risk question may be answered by the supervisor's default is an option set in Automation, not a built-in behaviour and not a per-question deadline.
4. The earlier open points about a deadline and its expiry behaviour are closed by items 2 and 3.

**Operator decisions, 2026-10-25 later (quoted):** "Skip would be specific or all, should be an option. We are not multi operator yet but any operator authorized for the automation.  All comms are parity so valid,  what else?"

5. **Skip is an option with two scopes:** skip one specific question, or skip all open questions. The operator picks when skipping.

**Operator clarification, 2026-10-25 (quoted):** "Skip is based on question,  not entire automation,  unless that was the option enable,  by default it is specific, but can apply wide."

5a. **Skip is per question by default.** A skip applies to the one question it is given on, never to the whole Automation. Skipping wide (all open questions) is available only when that option is enabled in Automation; it is off by default. This replaces the reading of item 5 as a free choice of scope at skip time.
6. **Who answers:** the system is not multi-operator yet; any operator authorized for the Automation may answer its questions.
7. **Every communication channel is valid for answering,** by the parity rule: PWA, mobile apps, CLI, REST, MCP and every messaging channel.


**Operator answers, one question at a time, 2026-10-25.**

8. **Low risk is defined by both a type list and a per-question level (quoted: "3 both,  sometimes a list is easier,  don't know what will be quantifiable low sometimes and that will help").** The operator keeps a list of question types in Automation that count as low risk, because a list is sometimes easier and not every low-risk case can be defined in advance. Each type gives a default risk level; the asker may raise the level of a single question and never lower it. The Automation setting that lets the supervisor answer by default applies to questions whose resulting level is low.

9. **A low-risk default never covers a design question (quoted: "1, maybe 3, keep track of that for future possibility").** Defaults cover operational questions only (retry counts, ordering, which lane, wording); design questions always wait for the operator or the operator's skip. **Future possibility, tracked and not built:** a repeated design question the operator has already answered may take the earlier answer as its default. The proposal must keep room for it: an answer record that can be matched to a later question by type and subject, with the match shown to the operator and never applied silently.

10. **What stays the operator's on a skip (answer: "1").** Three things stay with the operator even when a question is skipped: the 10.0 tag, releasing something the operator has not reviewed, and spending Claude usage beyond the operator's lane count. On a skip the supervisor decides everything else, and the record shows the answer as a supervisor decision made on the operator's skip, with its reasoning.

11. **Reminders and visibility (answer: "1", updated 2026-10-25: "But if we are putting details in datawatch it should be visible in automation and notifications").** An open question is mentioned once in the next report, with no repeated messages and no expiry. Because the details live in datawatch, an open question is also visible in the Automation view, with its full details and not only a count, and it raises a notification through the existing notification channels when it is asked and when it is answered or skipped.
12. **While a question is open (answer: "5 answer is 1").** The asker keeps working on everything else. Only the one task or PRD that depends on the answer waits, and it shows which question it is waiting on. The ticks and all other lanes carry on.

13. **Retention (answer: "1").** Answered questions and their quotations are decisions of record and are kept forever in the audit log, never deleted. The Automation view shows recent ones and searches older ones.

14. **Changing an answer (answer: "1").** An answer is never edited. A changed mind is a new answer that supersedes the old one; both stay on the record, each with its quotation, and the decision log shows which one is current.

15. **The AGENT.md Decision Interview Protocol (answer: "2").** The terminal protocol stays and the queue is added beside it. An agent may use either. Answers given in the terminal are copied into the queue afterwards by the supervisor, with the operator's quotation, so the decision record is complete either way. The proposal must say how that copy is made and checked.

16. **Question types (answer: "2 generic since we don't know what users of datawatch will use it for,  but default set based on 1").** There is one generic question record, with options, a recommendation, a risk level and links; the type is a label, not a different structure, because other users will use the queue for purposes we cannot predict. The default set of labels ships as decision, design review, approval and information (the four from the proposal), and a user may add or change labels. The low-risk type list from item 8 uses these labels.

17. **Proposal files (answer: "1").** When the operator answers a proposal's questions through the queue, the answers set the proposal file's `Confirmed:` line to yes and write the operator's quotations into the file's decisions section, automatically. The PRD's gated tasks then proceed without a manual edit. (Done by hand for two proposals on 2026-10-24 and 2026-10-25.)

**Status of the interview, 2026-10-25:** all ten open points are answered and recorded above (items 1 to 17). The proposal PRD (BL610) turns them into the data model, the order of PRD-sized steps and the parity surface; anything it finds that these answers do not cover goes back to the operator as a question.

18. **The question feature works with every LLM (operator, 2026-10-25: "also make sure question feature works with all llm").** Raising a question and receiving its answer must not depend on one backend's features. Every backend datawatch supports (claude-code, opencode, pi and pi-prompt, goose, gemini, aider, ollama, openwebui, shell, and any added later) can ask a question and get the answer back, by whichever route the backend supports. The proposal must give a route for each class of backend (those with MCP tools, those that only print text, and one-shot print-mode backends that have no input once started), and a conformance test with a fake backend of each class that raises a question, leaves it open while other work continues, and resumes with the answer.

19. **Marker route and one-shot backends (answers: "1 marker with options, 2 daemon asks on its behalf").** The text marker route carries options: a small structured block after the marker line gives the options, the recommendation and the risk label, with the same length caps and untrusted-text rules as free text. A one-shot print-mode backend never asks for itself: when its task ends needing a decision, the daemon raises the question on its behalf, with the task and the backend named as the asker, and holds the task until the answer arrives.

20. **Mobile work (proposal 6.g, answer: "1").** The Android and iOS work for questions is tracked as datawatch-app issues that carry the API contract, following the Mobile App Sync Rule. The issues are filed once the first two steps (storage with REST, then notifications and the return path) have shipped, because that is when the contract is stable. No native code is added to this repository.

21. **Secrets in an answer (proposal 6.h, answer: "1, encourage saving secrets on fs and pointing session to it or coordinate with datawatch secrets").** An answer whose text matches a secret pattern is not stored. The operator is told to resend it without the secret, and the refusal is audited without the text, so every stored quotation stays verbatim and no secret is ever stored. The refusal message encourages the two safe routes: put the secret in the datawatch secrets store and refer to it by name (`${secret:name}`), or save it on the filesystem and point the session to the file. The question flow must coordinate with the datawatch secrets store so the asker can be given the secret's name, never its value.

22. **Return path (proposal 6.j, answer: "1").** The answer reaches the asker as a lifecycle event on the existing event queue plus a polled GET endpoint, which needs no change to the asker's session. A question can opt in to a JSON line sent into the asking session, for askers that cannot poll, so answers stay out of the terminal unless asked for.

23. **Replying from a chat channel (proposal 6.k, answer: "1").** Each notification carries the question id and numbered options. A bare reply is accepted only when exactly one question is open for that channel; with several open it is refused and the list is sent back. Free text is accepted with the option, so the operator's exact words are kept as the quotation.

**Status 2026-10-25:** every open point in the decision channel proposal (6.a to 6.k) is now answered, except the proposal's own remaining parts noted in its sections. The next step is to turn it into the first implementation PRD (storage and REST), gated on the proposal's `Confirmed:` line, which the operator flips.

24. **Who writes a decision into the repository documents (proposal 6.i, answer: "2 but others may want to edit by hand so leave that as an option,  keep full editing as a future idea to examine").** The daemon records the link and the operator's exact words, and a docs-only change applies them through the normal repository path (the supervisor or the Automation makes it); the daemon never writes repository files on its own. Editing the document by hand stays available as an option for users who prefer it, and the record is the same either way. **Future idea to examine, not built:** letting the daemon edit and commit the documents itself (full editing), with its safety and review implications to be worked out first.

**Status 2026-10-25:** all points of the decision channel proposal (6.a to 6.k) are decided. The proposal's `Confirmed:` line is still `no` until the operator flips it; the first implementation PRD (storage and REST) can be filed and gated on it.

25. **Default alert times for the Automation duties (supervisor duties proposal Q1, answer: "1, 30 minutes").** One shared default of 30 minutes for CI failures, merge pending and failed or blocked PRDs, the same as the lane-fill idle alert; each value stays configurable.

26. **Retry count for the Automation duties (supervisor duties proposal Q2, answer: "2 existing but leave open for future examination").** The coordinator reuses the existing per-category retry budgets (infra, content, scope, policy) and adds no new count. **Kept open for later examination:** whether the coordinator needs its own retry count once it has run for a while.

27. **Rerunning a hung CI step (supervisor duties proposal Q3, answer: "1").** The coordinator may rerun a hung or infrastructure-classified CI run once without asking, and logs the rerun. It never reruns security workflow runs and never reruns a run whose judge verdict is policy.

28. **Surfaces for the Automation duties (supervisor duties proposal Q4, answer: "2").** Every one of the eight duties gets the full parity set: config, REST, MCP, CLI, comm channel, PWA, and a mobile parity issue wherever a REST or WebSocket contract changes. The proposal's recommended mix is not used.

This section is the requirement list; the first PRD writes the proposal and asks the operator the open points.
