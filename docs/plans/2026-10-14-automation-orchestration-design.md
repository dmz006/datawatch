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
deterministic. A parallel "fork/join" inside one Automation is Open question 10.

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
- Every edit sends `expected_version`. A `409` reloads the strip and shows a
  "changed by <actor>" toast.

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

## Parity surface

None of these is implemented in this design phase. Every surface follows AGENT.md's
Configuration Accessibility Rule and the full parity-surface set.

| Surface | Included? | Concrete design |
|---|---|---|
| **REST** | Yes | `GET/POST /api/automations`; `GET/PUT/DELETE /api/automations/{id}` (delete only in `draft` / terminal); `POST /api/automations/{id}/{activate\|pause\|resume\|cancel\|skip_member\|insert_member\|remove_member\|reorder_edges\|set_llm\|set_verification_llm\|set_budget}`; `POST /api/automations/import-graph/{graph_id}`. Same `fedCap` gating as the autonomous handlers (`CapAutonomousRead` / `Write` / `Run`). `409` on invalid state or version mismatch. Documented in `openapi.yaml`. |
| **MCP** | Yes | `automation_list`, `automation_get`, `automation_create`, `automation_update`, `automation_activate`, `automation_pause`, `automation_resume`, `automation_cancel` (`cancel_in_flight` flag), `automation_skip_member`, `automation_insert_member`, `automation_remove_member`, `automation_reorder_edges`, `automation_set_llm`, `automation_set_verification_llm`, `automation_set_budget`, `automation_import_graph`. Each is added to `internal/federation/mcp_tool_caps.go`. `autonomous_status` is extended with `automations[]`. The `orchestrator_graph_*` tools are deprecated (see the BL117 recommendation). |
| **CLI** | Yes | `datawatch automation list\|get\|create\|activate\|pause\|resume\|cancel [--cancel-in-flight]\|skip <member> --via on_failed --reason …\|insert <id> --after <member> --prd <id>[,<id>…] --reason …\|remove\|edges\|set-llm\|set-verification-llm\|set-budget\|import-graph`. |
| **Comm channel** | Yes (control + status) | `automation list`, `automation status <id>`, `automation pause\|resume\|cancel <id> [reason]`, `automation skip <id> <member> <edge> <reason>`, `automation insert <id> after <member> <prd> <reason>`. Create and edge rewiring are excluded from comm because they are graph editing and do not fit one line; use the PWA, CLI or MCP. Advance, blocked and budget-breach notifications go through BL427's comm fan-out, not a separate path. |
| **YAML/config** | Yes | Provisional keys under `autonomous.automation`: `enabled` (bool); `idle_alert_minutes` (int); `empty_edge_action` (`block`\|`pause`, Open question 6); `agent_insert_policy` (`allow`\|`require_operator`, Open question 8); `default_budget_on_breach` (`pause`\|`block`). Each is in `handleGetConfig` / `handlePutConfig`, `docs/config-reference.yaml`, `configure <key>=<value>` and `datawatch config set`. Automation objects themselves are runtime data in `automations.jsonl`, not YAML. |
| **PWA** | Yes | The Automation tab left of Automata; list, pipeline-strip detail and create/edit (see **PWA**); the config fields in Settings → General (autonomous section); locale keys in all bundles. |
| **Android** | Yes, via a parity issue | A `datawatch-app` issue: Automation list + strip detail + pause/resume/cancel/skip; create/edit may lag one release. Filed in the companion app repo by the implementation PRD, not here. |
| **iPhone/iOS** | Yes, via a parity issue | Same scope as Android, in the companion app repo's iOS track. Advance and blocked push notifications ride on BL427's mobile topic sink (`PublishToTopic`), so APNs/FCM need no Automation-specific delivery. |

---

## Phases

These phases are proposed for the follow-up implementation PRD. All are
**Planned**, and none is approved. Each phase's own checklist includes AGENT.md's
**Phase Completion Checklist**: functionality, unit tests, live smoke against a
real daemon, full `go test ./...` and `node --test`, the 8-item documentation
checklist, CHANGELOG, `docs/config-reference.yaml`, `docs/testing-tracker.md`,
howto, and the `datawatch-app` issue where applicable.

1. **Phase 1 — Data model, store, validation, CRUD.** Status: Planned.
   - `Automation` / `AutomationMember` / `BudgetPolicy` and `PRD.AutomationID`.
   - `automations.jsonl` with the ownership index.
   - Cycle, reachability and ownership validation.
   - REST + MCP CRUD.
   - Audit of create, update and delete.
2. **Phase 2 — Coordinator.** Status: Planned.
   - The BL427 dispatcher subscriber, or the temporary hook.
   - The advance with compare-and-set, `API.Run` / `API.Resume` in-process, the
     approval gate and blocked alerts.
   - Restart reconciliation after `resumeRunningPRDs`.
   - The `autonomous_status` extension and idle alert.
3. **Phase 3 — Control ops and mid-flight editing.** Status: Planned.
   - The pause / resume / cancel (+`cancel_in_flight`) / skip / insert / remove /
     reorder ops.
   - The shared per-Automation lock, the `Version` check and `409`s.
   - Decisions and audit.
4. **Phase 4 — Model-tier defaults.** Status: Planned.
   - The Automation tier in `resolveTaskBackendModel`, the executor effort chain
     and `resolveVerificationLLM`.
   - `VerifierSourceAutomation`.
   - `automation_set_llm` / `_set_verification_llm`.
5. **Phase 5 — BudgetPolicy.** Status: Planned.
   - The shared `PRDCostUSD` rollup (with BL414 / BL428 Phase 5).
   - The advance gate and the `admit` hook.
   - Pacing, on-breach handling and the estimated `allocation_pct`.
6. **Phase 6 — PWA.** Status: Planned.
   - The nav tab, list, pipeline strip and v1 create/edit.
   - Locale keys in all bundles.
   - The Terminology Rule addendum.
7. **Phase 7 — Remaining parity, BL117 migration, close-out.** Status: Planned.
   - CLI and comm verbs, plus config keys on every surface.
   - `import-graph`, and deprecation notices on `orchestrator_graph_create/plan/run`.
   - The howto (`docs/howto/automation-*.md`, no BL ids).
   - The release-smoke section.
   - The `datawatch-app` Android and iOS issues.

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
    - BudgetPolicy tightest-wins, pacing and on-breach.
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
2. **Nested Automations / an Initiative object.** Should an Automation be able to
   contain Automations (an "Automation of Automations", or a separate
   "Initiative")? This is **not decided either way**. Today's model allows only
   Automata as members, and adding a `MemberKind` later would be additive.
3. **The cancel_automation default.** Recommended: halt future auto-advance only,
   and leave the in-flight member running; `cancel_in_flight: true` is opt-in.
   Should the default be reversed?
4. **BL117 orchestrator.** Recommended: supersede, with a single-chain
   `import-graph`, read-only history, and the create/plan/run tools deprecated then
   removed after one minor. Alternatives: extend BL117's Graph in place, or keep
   both permanently.
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
8. **Agent and session inserts, and budget breach action.** May an agent or session
   (`AddedBy: session:<id>`) insert members without operator approval?
   Recommended: yes, because the inserted Automaton still has to pass its own
   `approved` gate before the coordinator will run it, and the insert is audited.
   The alternative is `agent_insert_policy: require_operator`. Separately, should a
   breached BudgetPolicy **pause** (recommended default; auto-resumes on window
   rollover) or **block** (needs an operator decision)?
9. **`allocation_pct` estimation.** Is an "estimated" allocation share acceptable,
   given that the subscription meter cannot be read (BL428 §4a)? It would be token
   usage priced at API rates against an operator-declared notional allocation per
   `plan_tier`. The alternative is to ship `dollar` only and defer `allocation_pct`.
10. **Fork/join inside one Automation.** Parallel work is currently expressed as
    separate concurrent Automations. Is a true parallel fork with a join member
    needed inside one Automation? That would need a multi-cursor model.
11. **Decompose tier.** Should the Automation-level model default also cover
    decomposition (`PRD.DecompositionModel` / the decompose backend), or only
    execution and verification?
12. **Execution provenance.** Should task run records gain an `executor_source`
    (`task|story|prd|automation|global`) mirroring `verifier_source`, so the
    Automation tier's effect is visible on execution too?
13. **Record `c187962a`'s title.** It says "BL430 — Automation…" while the backlog
    number is BL431. Should it be retitled, or left alone with this doc and the
    BL428 doc noting the collision?
