# Autonomous PRD planning (v3.10.0 — BL24 + BL25)

**Shipped in v3.10.0.** LLM-driven Product Requirements Document →
Stories → Tasks planning with independent verification. Each
Task can be spawned as a worker session under F10. Inspired by
HackingDave/nightwire's autonomous module; the design map is in
`docs/plans/2026-04-20-bl24-autonomous-decomposition.md`.

Disabled by default — opt in by setting `autonomous.enabled: true`.

---

## Surfaces (full channel parity)

| Channel | Entry-point |
|---|---|
| YAML  | `autonomous:` block in `~/.datawatch/config.yaml` |
| REST  | `/api/autonomous/*` (bearer-authenticated) |
| MCP   | `autonomous_status`, `autonomous_config_get/set`, `autonomous_prd_*`, `autonomous_learnings` |
| CLI   | `datawatch autonomous <subcmd>` |
| Comm  | `rest GET /api/autonomous/status` etc. via the comm `rest` passthrough (Signal/Slack/Telegram/Matrix/etc.) |

---

## REST endpoints

```
GET    /api/autonomous/status              loop snapshot (running, active PRDs, queued/running)
GET    /api/autonomous/config              read current config
PUT    /api/autonomous/config              replace config (full body)

POST   /api/autonomous/prds                create PRD; body: {spec, project_dir, [backend], [effort]}
GET    /api/autonomous/prds                list all PRDs newest-first
GET    /api/autonomous/prds/{id}           one PRD with story+task tree
PATCH  /api/autonomous/prds/{id}           edit PRD title + spec (non-running only); body: {title, spec, actor}     [v5.19.0]
DELETE /api/autonomous/prds/{id}           cancel + archive (status → cancelled)
DELETE /api/autonomous/prds/{id}?hard=true permanently delete the PRD + descendants spawned via SpawnPRD             [v5.19.0]
POST   /api/autonomous/prds/{id}/plan      run LLM planning phase (creates stories+tasks) [BL304 canonical]
POST   /api/autonomous/prds/{id}/decompose run LLM planning phase (back-compat alias for /plan)
POST   /api/autonomous/prds/{id}/approve   review/approve gate                                                       [v5.2.0  / BL191 Q1]
POST   /api/autonomous/prds/{id}/resume   resume a paused PRD, or one the boot-resume gate parked; body: {actor, note} (409-style 400 if neither)
POST   /api/autonomous/prds/{id}/reject    operator rejects the plan; body: {actor, reason}                          [v5.2.0  / BL191 Q1]
POST   /api/autonomous/prds/{id}/request_revision  ask for a re-plan; body: {actor, note}                            [v5.2.0  / BL191 Q1]
POST   /api/autonomous/prds/{id}/edit_task per-task spec edit on needs_review/revisions_asked; body: {task_id, …}    [v5.2.0  / BL191 Q1]
POST   /api/autonomous/prds/{id}/instantiate  template instantiation; body: {vars{}, actor}                          [v5.2.0  / BL191 Q2]
POST   /api/autonomous/prds/{id}/set_llm   PRD-level worker LLM override; body: {backend, effort, model, decomposition_profile, [guardrail_backend], [guardrail_model], actor}   [v5.4.0  / BL203]
POST   /api/autonomous/prds/{id}/set_task_llm  per-task LLM override; body: {task_id, backend, effort, model, [guardrail_backend], [guardrail_model], actor}             [v5.4.0  / BL203]
POST   /api/autonomous/prds/{id}/set_task_project_dir  per-task project dir override; body: {task_id, project_dir, actor} ("" clears)   [v9.1.0 / BL447]
POST   /api/autonomous/prds/{id}/set_task_verification_llm   per-task verifier LLM; body: {task_id, [verification_backend], [verification_model], [verification_effort], actor}   [v9.0.69]
POST   /api/autonomous/prds/{id}/set_story_verification_llm  per-story verifier LLM; body: {story_id, [verification_backend], [verification_model], [verification_effort], actor} [v9.0.69]
POST   /api/autonomous/prds/{id}/set_verification_llm        per-Automaton verifier LLM; body: {[verification_backend], [verification_model], [verification_effort], actor}       [v9.0.69]
POST   /api/autonomous/prds/{id}/set_dirs  directory scope; body: {write_dirs[], read_dirs[]} (absolute paths)
POST   /api/autonomous/prds/{id}/run       kick the executor for this PRD
GET    /api/autonomous/prds/{id}/children  list child PRDs spawned via SpawnPRD                                      [v5.9.0  / BL191 Q4]

GET    /api/autonomous/learnings           extracted post-task learnings

# Guardrail Library + Profiles
GET    /api/autonomous/guardrails                  list guardrail library (built-ins + skill-contributed)
GET    /api/autonomous/guardrail_profiles          list profiles
POST   /api/autonomous/guardrail_profiles          create profile; body: {name, description?, guardrails[]}
GET    /api/autonomous/guardrail_profiles/{id}     get one profile
PUT    /api/autonomous/guardrail_profiles/{id}     update profile
DELETE /api/autonomous/guardrail_profiles/{id}     delete profile
PUT    /api/autonomous/prds/{id}/guardrails        set per-Automaton override; body: {guardrail_profile?, per_task_guardrails[], per_story_guardrails[]}

# Automations — orchestration records (see the Automations section below)
GET    /api/automations              list Automations
POST   /api/automations              create (returns draft, version 1); body: {name, entry_member_id?, members[], budget_policy?, project_dir?, actor, reason}
GET    /api/automations/{id}         get one
PUT    /api/automations/{id}         update; body: create fields + expected_version (required)
DELETE /api/automations/{id}         delete (draft/completed/cancelled only); body: {actor?, reason?}
POST   /api/automations/{id}/set_llm               set the execution and planning defaults; body: {backend?, model?, effort?, planning_backend?, planning_model?, expected_version?, actor?, reason?}
POST   /api/automations/{id}/set_verification_llm  set the verification default; body: {backend?, model?, effort?, expected_version?, actor?, reason?}
POST   /api/automations/{id}/set_budget   set, replace or clear the spending limit of an Automation or one member (see "Spending limit" below); body: {member_id?, ceiling_type, ceiling_value, window, pacing?, on_breach?, backend?, allocation_estimate?, peer_window?, reset_time?, reset_weekday?, reset_day?, reset_zone?, estimated_cost_usd?, clear?, expected_version, actor?, reason?}

# Lifecycle event subscriptions (BL427 — PRD lifecycle events, phase 3)
GET    /api/autonomous/subscriptions        list persisted lifecycle event subscriptions (optional ?owner= lists only those created by the named owner)
POST   /api/autonomous/subscriptions        create (201 + the record); body: {filter:{kinds[], level?, prd_id?, task_id?, story_id?, owner?}, sink:{type, target}, action:{type, text}?, coalesce?, one_shot?, expires_at?}. Sink types: comm | session | topic | alert. An action is only valid with a session sink (action.type must be send_input, ≤4096 chars). 400 with the validation message on bad input
DELETE /api/autonomous/subscriptions/{id}   delete (204; 404 when unknown) — also accepted as DELETE /api/autonomous/subscriptions?id={id}. One-shot subscriptions are also auto-deleted by the engine after their first delivery. Changes take effect live — the engine re-attaches on store changes, no restart
```

`GET/POST/DELETE /api/autonomous/subscriptions` sit on the operator's
persisted lifecycle subscriptions (`<data_dir>/lifecycle_subscriptions.json`):
`filter.kinds` picks event kinds from the known set (`prd.completed`,
`task.completed`, … every PRD/task status-change kind plus
`task.session_started`, `task.session_cleared`, `prd.updated`),
`filter.level` narrows to `prd` or `task` events, and the `sink` is
where matching events are delivered (`comm` = the notify pipeline, `session`
= a datawatch-managed session, `topic` = the push-topic bus, `alert` = the
in-app alerts store). With `sink.type = session`, `action` may type
`action.text` into the session each time (`send_input`), which replaces
per-event watch-and-inject scripts. `one_shot: true` consumes the
subscription after its first delivery; `expires_at` (RFC 3339, future)
expires it instead. These endpoints require the same auth as the other
`/api/autonomous` routes (federation peers need `autonomous:read` for
list, `autonomous:write` for create/delete). They are served only when the
daemon runs the subscription engine; otherwise 503.

When `autonomous.enabled` is false, every endpoint returns
`503 autonomous disabled`.

### Clean restart on `reset_task`

`POST /api/autonomous/prds/{id}/reset_task` accepts a `clean_restart` boolean
(default `false`) alongside `task_id` and `force`. By default the next attempt
restores the work that `preserve_attempt_work` saved from earlier killed
attempts. Setting `clean_restart: true` starts that one attempt from the
pre-task tree instead, so the saved work is not restored for that attempt. The
flag applies to the single reset only; later attempts restore again unless they
are reset with `clean_restart` too. The MCP tool and the comm-channel command
accept the same option.

### Guardrail judge LLM override (`set_llm` / `set_task_llm`)

Set a PRD's worker LLM with `set_llm`: Omitted fields stay unchanged; an
empty value (`""`) clears that field (falls back to global). Every `set_llm`
call is audit-logged and appended to the PRD's decision log.

Command-line usage: `autonomous set-llm <prd-id> [backend=<v>] [effort=<v>] [model=<v>] [decomposition_profile=<v>]`

`guardrail_backend` / `guardrail_model` pick the LLM that judges the
guardrail/verification step for an Automaton or a single task. They
are presence-aware, unlike the worker-LLM keys:

- **absent** — the field is left unchanged
- **`""`** — the override is cleared (falls back to the next level)
- **a value** — sets the override (`guardrail_backend` must be a configured LLM name; unknown names return 400)

A body that carries **only** guardrail keys (plus `task_id` / `actor`)
does not touch `backend` / `effort` / `model` / `decomposition_profile`.
A body that carries any worker-LLM key keeps the existing
overwrite-all behaviour for those keys.

Resolution order: task `guardrail_backend` → Automaton
`guardrail_backend` → global `autonomous.verification_backend` / `verification_model`.

```bash
# guardrail-only: leaves the worker LLM untouched
curl -sX POST http://localhost:7433/api/autonomous/prds/<id>/set_llm \
  -H "Content-Type: application/json" \
  -d '{"guardrail_backend":"ollama","guardrail_model":"qwen3.8:27b"}'

# clear a task's guardrail model override only
curl -sX POST http://localhost:7433/api/autonomous/prds/<id>/set_task_llm \
  -H "Content-Type: application/json" \
  -d '{"task_id":"<task-id>","guardrail_model":""}'
```

### Per-task project dir override (`set_task_project_dir`)

Each task carries an optional `project_dir` field (JSON `project_dir`).
When set, the task runs in that directory instead of the Automaton's
`project_dir`.

- **Precedence:** task `project_dir` → Automaton `project_dir`. An empty
  value means the task inherits the Automaton's directory, and sending
  `""` clears an existing override.
- **Every per-task check follows the override:** diff capture, scope
  (write/read dirs), file drift detection, quality gates and the
  verifier all use the task's resolved directory.
- **Validation:** the path must be absolute and must not be a
  filesystem root or system directory. When the Automaton has write
  dirs, the path must be inside them. Validation errors and unknown task IDs return
  `400`. Like `set_task_llm`, it is accepted only while the Automaton is
  `needs_review`, `revisions_asked` or `cancelled`.

| Surface | How |
|---|---|
| **REST** | `POST /api/autonomous/prds/{id}/set_task_project_dir`, body `{task_id, project_dir, actor}` |
| **MCP** | `autonomous_prd_set_task_project_dir` (`id`, `task_id`, `project_dir`) |
| **CLI** | `datawatch autonomous prd-set-task-project-dir <prd> <task> [dir]` (leave out `dir` to clear) |
| **Comm** | `autonomous set-task-project-dir <prd> <task> [dir]` (alias `set_task_project_dir`) |
| **PWA** | Task edit modal → **Project dir override** (empty inherits); the task card shows a `dir:` badge when it is set |

```bash
curl -sX POST http://localhost:7433/api/autonomous/prds/<id>/set_task_project_dir \
  -H "Content-Type: application/json" \
  -d '{"task_id":"<task-id>","project_dir":"/srv/repo/services/api"}'

# clear: inherit the Automaton's project_dir again
curl -sX POST http://localhost:7433/api/autonomous/prds/<id>/set_task_project_dir \
  -H "Content-Type: application/json" \
  -d '{"task_id":"<task-id>","project_dir":""}'
```

### Verification LLM override (`set_task_verification_llm` / `set_story_verification_llm` / `set_verification_llm`)

Pick the LLM that **verifies** (judges) a task's work, per task, per
story or per Automaton. The values are stored on the Automaton itself
(`verification_backend` / `verification_model` / `verification_effort`
on the task, story or Automaton record), not in `config.yaml`.

| Method | How |
|--------|-----|
| **YAML** | No per-unit config key — overrides live on the Automaton. The global fallback stays `autonomous.verification_backend` / `verification_model` / `verification_effort` in `~/.datawatch/config.yaml` |
| **CLI** | `datawatch autonomous prd-set-task-verification-llm <id> --task <task-id> --backend B --model M --effort E` (also `prd-set-story-verification-llm --story`, `prd-set-verification-llm`) |
| **Web UI** | Automaton detail → task **Edit**, story **LLM**, or Automaton **Set LLM** modal → *Verification LLM* row (Backend / Effort / Model). The task's verification result shows **Judged by** |
| **REST** | `POST /api/autonomous/prds/{id}/set_task_verification_llm` (etc.), body below |
| **MCP** | `autonomous_prd_set_task_verification_llm`, `autonomous_prd_set_story_verification_llm`, `autonomous_prd_set_verification_llm` |
| **Comm channel** | `autonomous set-task-verification-llm <id> <task-id> backend=X model=Y effort=Z` (also `set-story-verification-llm <id> <story-id> …`, `set-verification-llm <id> …`) |

Body keys are presence-aware like the guardrail keys: **absent** leaves
the field unchanged, **`""`** clears it (inherit the next level), a
value sets it. `task_id` / `story_id` are required on the task / story
variants. `verification_backend` must be a configured LLM name (unknown
→ 400); `verification_effort` must be one of `low`, `medium`, `high`,
`max`, `quick`, `normal`, `thorough`. Overrides are accepted while the
Automaton is `needs_review`, `revisions_asked` or `cancelled`. The
response is the updated Automaton.

Resolution: task → story → Automaton → Automation → global
`autonomous.verification_backend` / `verification_model` /
`verification_effort`. The model is taken from the same level the
backend was found at; effort resolves independently. A backend set at
any per-unit level bypasses the `autonomous.verification_backends`
capacity pool. A model or effort set with no per-unit backend leaves
the verifier on the global path. Effort is applied when the verifier
runs as a session.

Each verification records what actually ran on the task's
`verification` object: `verifier_backend`, `verifier_model`,
`verifier_effort` and `verifier_source` (`task`, `story`, `prd` for the
Automaton level, `automation` for the default set on the Automation the
Automaton belongs to, `global`, or `pool`).

```bash
# a stronger verifier for one risky task
curl -sX POST http://localhost:7433/api/autonomous/prds/<id>/set_task_verification_llm \
  -H "Content-Type: application/json" \
  -d '{"task_id":"<task-id>","verification_backend":"claude-code","verification_model":"opus","verification_effort":"high"}'

# clear it again (inherit story → Automaton → global)
curl -sX POST http://localhost:7433/api/autonomous/prds/<id>/set_task_verification_llm \
  -H "Content-Type: application/json" \
  -d '{"task_id":"<task-id>","verification_backend":"","verification_model":"","verification_effort":""}'
```

These are independent of `guardrail_backend` / `guardrail_model`
above, which pick the guardrail judge.

`verification_script`, `files_generated` and `files_touched` are
system-populated and read-only — they appear in
`GET /api/autonomous/prds/{id}` (and the MCP / CLI / comm `get`
surfaces, which pass the same JSON through) but have no setter.

---

## Automations (orchestration records)

An **Automation** is a named group of member Automata (PRDs), linked
into a graph where each member names at most one next member per
outcome:

| Member PRD outcome at finish | Edge followed |
|---|---|
| `completed` | `on_complete` member (empty = the Automation is `completed`) |
| `blocked` | `on_blocked` member (empty = the Automation is `blocked`) |
| `failed` | `on_failed` member (empty = the Automation is `blocked`) |

A linear Automation has one cursor: members run one at a time, as
sequential steps with conditional branches. An Automation can also fan
out with `fork`/`join` members and nest other Automations; see the
Phase 3a section below. Records persist under `<data_dir>/autonomous/`
and survive daemon restarts.

**Example — creating two members, the first handing off to the second
on completion:**

```json
{
  "name": "nightly sweep",
  "actor": "operator",
  "reason": "prune, then reindex if pruning succeeds",
  "entry_member_id": "mprune",
  "members": [
    {"id": "mprune",   "prd_id": "a1b2c3d4", "on_complete": "mreindex"},
    {"id": "mreindex", "prd_id": "e5f6a7b8"}
  ]
}
```

`mprune.on_complete` points at `mreindex`; `mreindex` has no
`on_complete`, so when it finishes the Automation is terminal. Edges
can only name members that exist, and the graph must stay acyclic —
a loop is a 400. Member IDs are stable across edits and may be
operator-supplied (the store generates them when absent).

**Concurrency — `expected_version`**: every write bumps `version`.
Send the last value you saw as `expected_version` in update bodies;
a stale value is a 409 and the record is left unchanged. Members and
the entry member may only actually change while an Automation is in
`draft`; deleting an active/paused/blocked Automation is also a 409.

**Error codes**

| HTTP | Meaning |
|------|---------|
| 400 | validation: unknown member or edge target, cycle in the member graph (including a cycle across nested Automations), member PRD already owned by another Automation, a template/owned PRD enrolled as a member, or an invalid fork/join/nested member (the message names the member ID) |
| 401 | missing token |
| 404 | no Automation with that id |
| 405 | wrong method (e.g. PATCH) |
| 409 | `expected_version` stale; members/entry changed while not in draft; or delete of a non-deletable status |
| 503 | the daemon runs with autonomous disabled |

> **Creating stores; activating starts.** Creating an Automation stores
> its definition, membership and any history. `POST
> /api/automations/{id}/activate` starts a draft Automation at its entry
> member, and from then on the daemon advances it: each finished member's
> outcome edge starts the next member, or completes or blocks the
> Automation.
>
> Full request/response schemas (`Automation`, `AutomationMember`,
> `BudgetPolicy`) are in `openapi.yaml` next to this file.

### Phase 3a — fork/join branches and nested Automations

An Automation tracks a **set of active cursors** instead of one. A
linear Automation (no `fork`, `join` or `automation` members) behaves
exactly as described above.

**Member fields.** Each member gains the fields below. All are optional,
and `kind` defaults to `prd` when omitted or empty.

| Field | Used by `kind` | Meaning |
|---|---|---|
| `kind` | all | `""` or `prd` (default): a member Automaton named by `prd_id`. `automation`: a nested Automation named by `automation_id`. `fork`: starts its branches together. `join`: collects its branches. |
| `automation_id` | `automation` | The child Automation's ID. |
| `fork_members` | `fork` | Branch member IDs started together. At least two, distinct, each a `prd` or `automation` member. |
| `join_members` | `join` | Branch member IDs the join collects. Must be the same set as the paired fork's `fork_members`. |
| `on_branch_failure` | `join` | `all_must_succeed` (default when omitted), `any_one_is_enough` or `continue_anyway`. See the join rules below. |

Set only the fields that belong to a member's kind. Any other field is
a `400` that names the member ID: a `prd` member cannot carry
`fork_members`, `join_members` or `on_branch_failure`, and a `fork` or
`join` member cannot carry `prd_id`.

**Fork and branches.** When a fork is reached, through an incoming edge
or as the entry member, every branch starts at once, in sorted member-ID
order. Each branch goes through the normal start gate. A branch that the
gate denies, or whose start fails, is recorded as a failed branch, and
its siblings still start. Branches carry no outcome edges:
`on_complete`, `on_blocked` and `on_failed` must all be empty. The fork
has no outcome edges either. Its forward path is its paired join.

**Join rules.** A join is reached only through its fork, so no outcome
edge may name a join. Its only outgoing edge is `on_complete`.
`on_blocked` and `on_failed` must be empty, because a join that cannot
fire blocks the Automation rather than routing along an edge. A branch
counts as successful only when it completes. Blocked, failed, cancelled
and skipped branches count as not successful. Rule evaluation reads the
branches' final statuses, not the order of events, so a replay gives the
same result.

- `all_must_succeed` (default): the join fires `on_complete` only when
  every branch succeeded. While any branch is still live, the join waits.
  Once every branch is final, a join with a non-success branch blocks the
  Automation, with the reason `join <id>: not all branches succeeded
  (non-success: <member IDs>)`.
- `any_one_is_enough`: the join fires as soon as one branch succeeds.
  Branches still running finish, and their later results are recorded
  without firing the join again. If every branch ends without success,
  the Automation blocks with the reason `join <id>: no branch succeeded`.
- `continue_anyway`: the join fires once every branch is final, whatever
  their outcomes. Non-success branches are named in the join's decision.

An empty `on_complete` on the join completes the Automation when the join
fires.

**Nested Automations.** An `automation` member starts its child by
activating it, and the child's entry member starts through the normal
start gate. A child that is not a `draft` cannot be activated, so the
start fails with a `409`. That counts as a failed start of the member: a
failed branch inside a fork, and otherwise a blocked Automation, as for
any start failure. The child's terminal status
selects the nested member's edge:

| Child ends | Edge followed |
|---|---|
| `completed` | `on_complete` |
| `blocked` | `on_blocked` |
| `cancelled` | `on_failed` |

- **Ownership.** A child Automation may be a member of at most one
  non-terminal Automation at a time. Non-terminal means `draft`, `active`,
  `paused` or `blocked`. A write that would add the child to a second one
  is a `400`. The child's PRDs belong to the child. The parent's member
  list is the only record of the nesting, and the child has no parent
  pointer. The child keeps its own budget policy, and the parent does not
  cap it.
- **Cycle rejection.** A create or update is a `400` when the Automation
  can reach itself through its nested members, directly or transitively.
  That covers a member that names its own Automation, and the two-cycle
  A → B → A. The same check follows fork-to-branch and branch-to-join
  pairings, so a loop through a fork is rejected too. The error names the
  member ID.

**Concurrency.** A stale `expected_version` on update returns `409`, and
the record is left unchanged. Inside the daemon, a write that races
another one re-reads the Automation, re-evaluates, and does nothing if
the state has already moved.

**Status.** `GET /api/autonomous/status` returns the following on each
`automations[]` row. The existing fields are kept.

- `active_cursors`: one entry per in-flight member, sorted by
  `member_id`. Each entry has `member_id`, `prd_id` for a `prd` member or
  `automation_id` for an `automation` member, and `status`. The list is
  always present, and it is `[]` when the Automation is idle.
- `join_wait`: present only while a join waits on its branches. It has
  `join_member_id`, `rule`, `pending_branches` (still live) and
  `failed_branches` (ended without success).
- `current_member_id`, `current_prd_id` and `current_prd_title` are
  derived from the first active cursor, sorted by member ID, and are empty
  when no cursor is active. A client that draws a fork/join Automation
  should read `active_cursors` and `join_wait`, not `current_member_id`
  alone.

```json
{
  "id": "3fa9c2d1",
  "name": "parallel checks",
  "status": "active",
  "current_member_id": "tests",
  "current_prd_id": "e5f6a7b8",
  "current_prd_title": "unit test sweep",
  "active_cursors": [
    {"member_id": "tests", "prd_id": "e5f6a7b8", "status": "running"}
  ],
  "join_wait": {
    "join_member_id": "join1",
    "rule": "all_must_succeed",
    "pending_branches": ["tests"],
    "failed_branches": ["lint"]
  }
}
```

**Restart.** On daemon start, reconciliation runs once over the whole
cursor set. A member that finished while the daemon was down replays its
outcome. Unstarted branches of a fork start, and branches that were
already running keep running. A waiting join fires or blocks exactly
once, with the same decision it would have made before the restart.

**Example — a fork with two branches, joined by any one success:**

```json
{
  "name": "parallel checks",
  "actor": "operator",
  "reason": "run lint and tests side by side; join when either passes",
  "entry_member_id": "fork1",
  "members": [
    {"id": "fork1", "kind": "fork", "fork_members": ["lint", "tests"]},
    {"id": "lint",  "prd_id": "a1b2c3d4"},
    {"id": "tests", "prd_id": "e5f6a7b8"},
    {"id": "join1", "kind": "join", "join_members": ["lint", "tests"],
     "on_branch_failure": "any_one_is_enough"}
  ]
}
```

The REST endpoints do not change. `GET/POST /api/automations` and
`GET/PUT/DELETE /api/automations/{id}` accept the member fields above.
`openapi.yaml` documents them.

### Phase 3b — control operations

An operator, or a session or agent acting through the API, can steer a
running Automation with nine `POST` endpoints under
`/api/automations/{id}/`. They need the autonomous write capability, like
the other writes. A `GET`, `PUT` or `DELETE` on one of these paths is a
`405`.

| Endpoint | Allowed while the Automation is | What it does |
|---|---|---|
| `POST /api/automations/{id}/pause` | `active` | Moves it to `paused`. Members already running keep running and nothing is ended. A paused Automation moves its cursor on when a member finishes but starts nothing. |
| `POST /api/automations/{id}/resume` | `paused` | Moves it back to `active`. Members that finished while paused are routed on and the next member is started. Finished members are not restarted. |
| `POST /api/automations/{id}/cancel` | `active`, `paused`, `blocked` | Cancels it. Needs `cancel_in_flight` (below). |
| `POST /api/automations/{id}/skip_member` | `active`, `paused` | Skips an active member and follows one of its edges, as if the member had ended that way. A session the skipped member is running is not ended. |
| `POST /api/automations/{id}/insert_member` | `draft`, `active`, `paused`, `blocked` | Adds a member PRD to the chain (see "Inserting a member"). |
| `POST /api/automations/{id}/remove_member` | `active`, `paused` | Removes a member that has not started and has no outgoing edges. Edges of other members that named it are cleared. |
| `POST /api/automations/{id}/reorder_edges` | `active`, `paused` | Rewires the outgoing edges of a member that has not started. |
| `POST /api/automations/{id}/approve_insert` | `draft`, `active`, `paused`, `blocked` | Promotes a pending insert into the graph (operator only). |
| `POST /api/automations/{id}/set_budget` | `draft`, `active`, `paused`, `blocked` | Sets, replaces or clears the spending limit of the Automation, or of one member that has not finished (see "Spending limit"). Does not change the status. |

**Request body.** Every endpoint takes a JSON object. All fields are
optional on the wire, but each operation has the fields it needs.

| Field | Used by | Meaning |
|---|---|---|
| `expected_version` | all | The Automation `version` you read. It must equal the stored version exactly. A stale value, or an omitted one (read as `0`), is a `409` and nothing changes, whatever the Automation's state. Read the Automation again and retry. |
| `actor` | all | Who is acting, recorded in the audit trail. Defaults to `operator`. An agent or session passes `agent:<id>` or `session:<id>`. |
| `reason` | all | Free text recorded with the audit entry and the Automation's decision log. Required by `insert_member`. |
| `cancel_in_flight` | `cancel` | Boolean, required, no default. See below. |
| `member_id` | `skip_member`, `remove_member`, `reorder_edges`, `approve_insert`, `set_budget` | The member to act on. For `approve_insert` it is the pending insert's ID, which is also the ID the member gets once approved. For `skip_member` it names the branch to act on in a forked Automation (see "Branches"). |
| `edge` | `skip_member` | `on_complete` (default when omitted), `on_blocked` or `on_failed`. Any other value is a `400`. |
| `on_complete`, `on_blocked`, `on_failed` | `reorder_edges` | The member ID each edge now names. An edge that is left out or empty becomes no next member, so send all three edges you want to keep. A target must exist and must not have started. |
| `member` | `insert_member` | Object `{"prd_id": "...", "name": "..."}`. `prd_id` is required. `name` is an optional display name. |
| `urgent` | `insert_member` | Boolean. `true` takes the next slot, `false` (default) goes to the end of the chain. |
| `after` | `insert_member` | Member ID to insert directly after. Empty means the end of the chain. An urgent insert must leave it empty. |
| `ceiling_type`, `ceiling_value`, `window`, `pacing`, `on_breach`, `backend`, `allocation_estimate`, `peer_window`, `reset_time`, `reset_weekday`, `reset_day`, `reset_zone` | `set_budget` | The spending limit. They replace the whole limit; see "Spending limit". |
| `estimated_cost_usd` | `set_budget` | With `member_id`: the member's expected cost in dollars (a number, `0` removes it). Alone it changes only the estimate. |
| `clear` | `set_budget` | Boolean. `true` removes the limit; it cannot be combined with any policy field or `estimated_cost_usd`. |

**Responses.** `200` returns the updated Automation (same shape as
`GET /api/automations/{id}`).

| HTTP | Meaning |
|------|---------|
| 400 | Malformed body. `cancel` without `cancel_in_flight` (a missing value or `null`). `insert_member` without a `reason`, without `member.prd_id`, or naming an unknown PRD. An invalid `edge`. A bad edge target in `reorder_edges`. A branch-naming rejection (below). |
| 404 | No Automation with that ID, no such member, or no such pending insert. |
| 405 | A method other than `POST`. |
| 409 | Stale or omitted `expected_version`. The Automation is in a state the operation does not allow (the message names the state and the states it needs). A draft that needs activating first. A member that has started and so cannot be removed or rewired. A PRD that is already a member, already pending, or owned by another Automation. A second urgent insert while one is pending. An `approve_insert` by anyone but the operator. |
| 500 | `cancel` only: the Automation is cancelled, but ending one or more in-flight member sessions failed. The message names the members. |
| 503 | Autonomous is disabled, or the daemon has no Automation control support. |

#### Spending limit (`set_budget`)

An Automation can carry a `budget_policy`: a dollar ceiling over a window.
A member can carry one too, which narrows the Automation's and counts only
that member's spend. `set_budget` sets, replaces or clears one of them.

```json
POST /api/automations/3fa9c2d1/set_budget
{"expected_version": 7, "actor": "operator", "reason": "weekly cap",
 "ceiling_type": "dollar", "ceiling_value": 40, "window": "week",
 "pacing": "even", "on_breach": "pause", "backend": "claude-code",
 "reset_weekday": "monday", "reset_time": "09:00", "reset_zone": "America/New_York"}
```

| Field | Meaning |
|---|---|
| `ceiling_type` | `dollar` or `allocation_pct`. Required. Only `dollar` can pause or block; `allocation_pct` is accepted but is an estimate only. |
| `ceiling_value` | Number above zero (dollars). Required. |
| `window` | `automation` (the whole life of the Automation, never resets), `day`, `week` or `month`. Required. |
| `pacing` | `none` (default) or `even`. Even pacing holds a member start while spend is ahead of an even spread across the window. Ignored for the `automation` window. |
| `on_breach` | `pause` (default) or `block`. See below. |
| `backend` | Optional LLM whose spend is measured; empty counts every member's spend. Needed for `peer_window`. |
| `allocation_estimate` | Boolean, default `false`. Adds an estimated share-of-allocation figure to the budget view, always labelled estimated, never used to pause or block. It uses `autonomous.plan_tier_allocation_usd`. |
| `peer_window` | Array of peer names, default empty. Another daemon's usage counts toward the ceiling only for the peers named. There is no automatic confirmation that a peer shares the account. |
| `reset_time`, `reset_weekday`, `reset_day`, `reset_zone` | Optional anchors for the day, week and month windows: 24h `HH:MM`, `monday`..`sunday` (week only), `1`..`28` (month only) and an IANA zone. Unset anchors default to local midnight, Monday and the first of the month in the daemon's zone. |

The ceiling is checked before any member starts (activation, moving to the
next member, a whole fork) and before a member's task starts a new session.
Work already running is not interrupted. When a dollar ceiling is reached the
Automation **pauses and resumes by itself at the window reset** when the reset
time is known (a `day`, `week` or `month` window, or a reset time parsed from a
rate-limit message) and `on_breach` is `pause`. When the reset time is unknown,
or `on_breach` is `block`, it goes to `blocked` and waits for the operator; a
`resume` is refused with a `409` carrying the reason while the ceiling is still
reached. An alert is raised either way. A pacing or estimate hold is not a
breach: the Automation stays `active` and the periodic check retries.

`GET /api/automations/{id}` returns the stored record with the policy in
`budget_policy` and the current breach, if any, in `budget_state`
(`action`, `breached_at`, `reason`, `scope`, `spent_usd`, `ceiling_usd`,
`window`, `resume_at`, `reset_known`, `reset_source`). It also returns a
computed `budget` view, never stored: `scope`, `ceiling_type`, `ceiling_usd`,
`spent_usd`, `remaining_usd`, `window`, `window_start`, `window_end`, `pacing`,
`pacing_allowed_usd`, `on_breach`, `ceiling_reached`, `hold` and `hold_reason`,
`reset_at`, `reset_known`, `reset_source` (`window` or `rate_limit`), `state`,
`allocation_estimate`, and for opted-in peers `peers[]`, `peer_spend_usd`,
`peer_basis` and `peer_note`. Each Automation row of `GET
/api/autonomous/status` carries the same `budget` view plus `spent_usd` and
`ceiling_display`. Spend is the sum, over every member Automaton (and nested
Automation), of the cost of its task, planning and verification sessions.

Errors specific to `set_budget`: `400` for an invalid policy (unknown type,
window, pacing or `on_breach`; ceiling not above zero; an anchor that does not
parse or does not fit the window; a repeated or empty peer name; `clear`
combined with policy fields; `estimated_cost_usd` without `member_id` or
negative; a fork or join member); `404` for an unknown Automation or member;
`409` for a stale `expected_version`, an Automation that is `completed` or
`cancelled`, or a member that has finished.

#### `cancel` and the required `cancel_in_flight`

`cancel` has no default for what happens to members that are running at
that moment, so the request must say. `cancel_in_flight` is a boolean
and **both values are valid**:

- `"cancel_in_flight": true` ends the session of every in-flight member
  and marks it cancelled, using the same path as cancelling the member's
  Automaton on its own. A running nested Automation is cancelled the same
  way.
- `"cancel_in_flight": false` leaves running members alone. The Automation
  is cancelled, and the later end of a running member is ignored because
  the Automation is already cancelled.

In both cases, members that had not started become cancelled, the
Automation's status becomes `cancelled`, and the member Automatons are
released from the Automation. A missing or `null` value is a `400` whose
message names both allowed values: `cancel_in_flight=true` (end the
session of every in-flight member) and `cancel_in_flight=false` (leave
in-flight members running). If ending a session fails, the Automation
stays cancelled, and the `500` message names the members it could not end.

```json
POST /api/automations/3fa9c2d1/cancel
{"expected_version": 7, "cancel_in_flight": false, "actor": "operator", "reason": "superseded"}
```

#### Inserting a member

`insert_member` adds `member.prd_id` to the chain. The PRD must exist, and
it must not already be a member or a pending insert of this Automation or
belong to another Automation that has not ended.

- **Placement.** A normal insert goes to the end of the chain, or directly
  after `after` when that is given. An urgent insert takes the next slot: on
  a draft it becomes the entry member, and on a started Automation it goes
  between the one running member and that member's `on_complete` target.
  An urgent insert on a started Automation needs exactly one active member
  to follow, so in a forked Automation with several branches active it is a
  `409` that lists them.
- **Draft.** On a draft Automation the member lands in the graph at once.
- **Started Automation.** On an `active`, `paused` or `blocked` Automation
  the insert is always held as a **pending insert**: it appears in the
  Automation's `pending_inserts` (`id`, `prd_id`, `name`, `urgent`,
  `after_member_id`, `actor`, `reason`, `added_at`, `status: pending`),
  and the Automation keeps advancing without it. It enters the graph, at
  the position reserved for it, when the operator calls `approve_insert`
  with `member_id` set to the pending insert's `id`. The endpoint requires
  a `reason`. A `completed` or `cancelled` Automation refuses an insert.
- **Agent insert policy.** The global setting
  `autonomous.automation.agent_insert_policy` decides whether an insert
  from a session or agent (an `actor` that starts with `session:` or
  `agent:`) may edit a draft directly:
  - `allow` (default): the draft insert lands at once.
  - `require_operator`: the draft insert is held as a pending insert too,
    and needs `approve_insert`.

  An insert from the operator on a draft always lands at once, and
  every insert on a started Automation is held whatever the policy.
- **One urgent insert at a time.** An Automation can have at most one
  pending urgent insert. A second urgent `insert_member` is a `409` naming
  the pending one. Once that insert is approved, another urgent insert is
  accepted. `approve_insert` of a pending urgent insert is also a `409`
  while a different urgent insert is pending.
- **Approval.** Only the operator may approve (`actor` empty or
  `operator`). Any other actor is a `409`.

```json
POST /api/automations/3fa9c2d1/insert_member
{"expected_version": 8, "actor": "agent:ab12", "reason": "tests found a gap",
 "member": {"prd_id": "c9d0e1f2", "name": "fix flaky step"}, "urgent": true}
```

#### Branches (active cursors)

In a forked Automation, several members can be active at once. An
operation that acts on a position must say which active cursor it means,
by naming that branch member in `member_id`. `skip_member` is the
operation that needs it. In a linear Automation, or when only one cursor is
active, no name is needed. If it is needed and missing, the request is a
`400` with a message that lists the active branch member IDs:
`automation <id> has <n> active branches; name the branch member to act
on, one of [<member IDs>]`. A `member_id` that is not an active branch is
also a `400`: `member <member ID> is not an active branch of automation
<id>; active branches are [<member IDs>]`. An Automation with no active
member to act on is a `409`.

#### Audit

Every control operation writes an audit entry on the `automation`
resource: `automation_pause`, `automation_resume`, `automation_cancel`
(with `cancel_in_flight` and the members that were in flight),
`automation_skip_member`, `automation_insert_member`,
`automation_remove_member`, `automation_reorder_edges`,
`automation_approve_insert` and `automation_set_budget` (with the policy
that was set, or `clear`). The limit also writes `automation_budget_breach`
(once per breach, again when a pause turns into a block; details: Automation,
scope, spent, ceiling, window, what it did, and the reset time when known) and
`automation_budget_resumed` (when a pause or block is lifted by the daemon at
the window reset or by an operator). The entry records the actor, the reason, the
`expected_version` the caller sent, the operation's own fields, and the new
`version`. The Automation's `decisions` list gets a matching entry. A
`cancel` that cancelled the Automation but could not end every member
session is audited with the error before the `500` is returned.

The same operations are reachable as MCP tools (`automation_pause`,
`automation_resume`, `automation_cancel`, `automation_skip_member`,
`automation_insert_member`, `automation_remove_member`,
`automation_reorder_edges`, `automation_approve_insert`,
`automation_set_budget`; see
[mcp.md](../mcp.md)), as `datawatch automation
pause|resume|cancel|skip|insert|remove|edges|approve|set-budget` (`cancel`
requires `--cancel-in-flight <true|false>`; `set-budget` takes
`--ceiling-type`, `--ceiling-value`, `--window` and the optional flags, or
`--clear`), and as the comm verbs `automation
pause|resume|cancel|skip|insert|remove|edges|approve|set-budget` (`cancel`
requires `in-flight=true` or `in-flight=false`; `set-budget <id>
ceiling_type=dollar ceiling_value=40 window=week` or `clear=true`). `openapi.yaml` documents the
endpoints.

### Model-tier defaults (`set_llm` and `set_verification_llm`)

An Automation can carry default model settings for the member Automata it
runs, so a whole chain can be pointed at one backend, model and effort without
editing every Automaton. Two more `POST` endpoints under
`/api/automations/{id}/` set them. They need the autonomous write capability,
like the control operations above, and a `GET`, `PUT` or `DELETE` on either
path is a `405`.

| Endpoint | Body fields | What it sets |
|---|---|---|
| `POST /api/automations/{id}/set_llm` | `backend`, `model`, `effort`, `planning_backend`, `planning_model` | The execution default (`backend`, `model`, `effort` on the Automation) and the planning default (`planning_backend`, `planning_model`). |
| `POST /api/automations/{id}/set_verification_llm` | `backend`, `model`, `effort` | The verification default (`verification_backend`, `verification_model`, `verification_effort` on the Automation). |

Both also take the optional `expected_version`, `actor` and `reason` fields
described under "Request body" above. The response is the updated
Automation.

**Presence-aware fields.** A field that is left out is kept as it is, and a
field sent as an empty string clears it. At least one setting field is
required: a body with none is a `400` that names the fields the endpoint
accepts. `planning_backend` and `planning_model` are not accepted by
`set_verification_llm` (a `400`). `backend` and `planning_backend` must be a
configured LLM name (an unknown name is a `400`; check `/api/llms`), and an
empty value clears without a lookup. `effort` must be one of `low`, `medium`,
`high`, `max`, `quick`, `normal` or `thorough`. A backend and model pair that
cannot work together, as the Automation would hold it after the call, is a
`400`.

**`expected_version` is optional here.** Unlike the control operations
above, an omitted `expected_version`, or `0`, skips the version check. A
stale positive value is a `409` and nothing changes.

**When.** Both endpoints are allowed while the Automation is `draft`,
`active`, `paused` or `blocked`. A `completed` or `cancelled` Automation is a
`409`. A change applies to member Automata that start afterwards. It does not
change a task that is already running.

**Resolution order.** For the execution backend, model and effort, and for
the verification LLM, the first level that sets a value wins:

1. the task
2. the story
3. the Automaton
4. the Automation the Automaton belongs to (the new level)
5. the global default

Backend and model are taken from the same level, so a model set on a less
specific level is never paired with a backend from a more specific one. Effort
resolves independently of them. The Automation level sits between the
Automaton and the global default, so it never overrides anything set on a
task, story or Automaton.

**Planning is separate.** `planning_backend` and `planning_model` are an
optional pair used only when a member Automaton is decomposed into stories and
tasks. Unset means the global planning backend and model. They never change
the execution or verification default, and the execution and verification
defaults never change which LLM plans.

**What gets recorded.** Each task run record gains `executor_source`, beside
the verification result's `verifier_source`:

| Field | Where | Values |
|---|---|---|
| `executor_source` | the task, set when its run starts and overwritten by each later attempt | `task`, `story`, `prd` (the Automaton level), `automation`, `global` |
| `verifier_source` | the task's `verification` object | `task`, `story`, `prd`, `automation`, `global`, `pool` |

`executor_source` names the level that supplied the execution backend. The
session status for a task shows it too, and so does the PWA.

**Errors.**

| HTTP | Meaning |
|------|---------|
| 400 | Malformed body, no setting field, a planning field sent to `set_verification_llm`, an unknown backend name, an invalid `effort`, or a backend and model pair that do not fit. |
| 403 | The caller lacks the autonomous write capability. |
| 404 | No Automation with that ID. |
| 405 | A method other than `POST`. |
| 409 | A stale positive `expected_version`, or an Automation in a status that does not allow the change. |
| 503 | Autonomous is disabled, or the daemon has no Automation control support. |

Each successful call writes one audit entry, `automation_set_llm` or
`automation_set_verification_llm`, with the actor, the fields that were sent
and the new `version`, and adds a matching entry to the Automation's
`decisions` list.

```bash
# run every member on a stronger model unless it names its own
curl -sX POST http://localhost:7433/api/automations/<id>/set_llm \
  -H "Content-Type: application/json" \
  -d '{"backend":"claude-code","model":"claude-opus-5-5","effort":"high"}'

# a separate, cheaper planner for decomposing members, and clear the model
curl -sX POST http://localhost:7433/api/automations/<id>/set_llm \
  -H "Content-Type: application/json" \
  -d '{"planning_backend":"pi-prompt","planning_model":"qwen3.8:27b","model":""}'

# a different verifier, guarded by the version you read
curl -sX POST http://localhost:7433/api/automations/<id>/set_verification_llm \
  -H "Content-Type: application/json" \
  -d '{"expected_version":7,"backend":"claude-code","model":"claude-sonnet-5-5"}'
```

The same operations are reachable as the MCP tools `automation_set_llm` and
`automation_set_verification_llm` (see [mcp.md](../mcp.md)), as `datawatch
automation set-llm <id>` and `datawatch automation set-verification-llm <id>`
(flags `--backend`, `--model`, `--effort`, plus `--planning-backend` and
`--planning-model` on `set-llm`; an empty value clears), and as the comm verbs
`automation set-llm <id> backend=<b> model=<m> effort=<e>
planning-backend=<b> planning-model=<m>` and `automation set-verification-llm
<id> backend=<b> model=<m> effort=<e>` (name at least one; `model=` with an
empty value clears).

---

## Configuration

Per the no-hard-coded-config rule, every knob is reachable from
every channel:

```yaml
autonomous:
  enabled:                false       # off by default; opt-in
  poll_interval_seconds:  30          # background loop tick
  max_concurrent_tasks:   1           # daemon-wide default: independent tasks of one Automaton run at once (1 = sequential; an Automaton's own setting overrides)
  # max_parallel_tasks is deprecated and ignored (it never had an effect); use max_concurrent_tasks
  max_engines_per_node:   1           # local-model sessions one compute node runs at once (default 1; below 1 clamps to 1; hosted backends are not gated)
  planning_backend:       ""          # empty = inherit session.llm_backend (BL304; old key decomposition_backend still accepted)
  verification_backend:   ""          # empty = inherit; set differently for cross-backend independence
  planning_effort:        "thorough"  # BL41 effort hint (BL304; old key decomposition_effort still accepted)
  verification_effort:    "normal"    # per-task/story/Automaton overrides (set_*_verification_llm) take precedence
  stale_task_seconds:     900         # activity-based stall window: no output, child process, backend activity, or file change for this long = stalled; 0 = inherit session.stale_timeout_seconds
  preserve_attempt_work:  true        # save a killed attempt's uncommitted work to refs/datawatch/attempts/; restored on the next attempt unless clean_restart
  auto_fix_retries:       1           # re-prompt count on verifier failure
  security_scan:          true        # nightwire-port .py scanner before commit
```

### Compute-node engine cap (`max_engines_per_node`)

`autonomous.max_engines_per_node` caps how many local-model sessions one
compute node runs at once. The default is `1`, and a value below `1` is
clamped to `1`. Only sessions on a local engine on that node count
(`pi`, `pi-prompt`, `ollama`, `opencode`, `opencode-acp`); hosted backends
such as `claude-code` are not gated by it. Sessions past the cap wait with
the reason `waiting for node <name>`. The operator guide, including queue
priority, is in `docs/operations.md` → "Compute-node queue".

- **REST:** `PUT /api/config` with `{"autonomous.max_engines_per_node": N}`;
  `PUT /api/autonomous/config` with `{"max_engines_per_node": N}`. Both
  read back from `GET /api/config` (`autonomous.max_engines_per_node`) and
  `GET /api/autonomous/config` (`max_engines_per_node`). The value is
  persisted to `config.yaml`. Each `node:<name>` pool's `queue_depth` and
  `holder` are reported by `GET /api/capacity`.
- **MCP:** `autonomous_config_set` with `max_engines_per_node`. The field
  is forwarded only when the caller supplies it, so `0` is sent and clamped
  to `1`. `autonomous_config_get` returns it.
- **CLI:** `datawatch autonomous config-set '{"max_engines_per_node":2}'`
  (merges into the config; `config-get` shows it).
- **Comm:** `configure autonomous.max_engines_per_node=2` (any channel that
  accepts `configure`; the key is listed in the usage help).

### Model pairing warnings

A model only reaches its session when it pairs with that session's backend.
A planning model written as `<prefix>/<model>` for a different backend, or a
planning model set for a different planning backend than a non-finished PRD
decomposes with, is dropped (planning) or sent to a session that may reject
it (verification). The daemon checks this at startup and on every
`PUT /api/autonomous/config`, and reports the plain-language messages in:

- the `warnings` array of the `PUT /api/autonomous/config` response (always present, empty when none);
- the `warnings` field of `GET /api/autonomous/status` (omitted when none).

Each warning is written to the daemon log once per process. Completed,
cancelled, rejected and archived PRDs are not checked.

---

## CLI

```
datawatch autonomous status
datawatch autonomous config-get
datawatch autonomous config-set '{"enabled":true,"max_concurrent_tasks":2}'   # merges; any deprecated_fields note goes to stderr
datawatch autonomous config-set '{"max_engines_per_node":2}'                  # local-model sessions per compute node (default 1)
datawatch autonomous prd-list
datawatch autonomous prd-create "Add OAuth login with Google + GitHub"
datawatch autonomous prd-get <id>
datawatch autonomous prd-plan <id>        # canonical (prd-decompose is a back-compat alias)
datawatch autonomous prd-run <id>
datawatch autonomous prd-cancel <id>
datawatch autonomous prd-set-llm <id> --guardrail-backend ollama --guardrail-model qwen3.8:27b
datawatch autonomous prd-set-task-llm <id> --task <task-id> --guardrail-backend ""   # clear
datawatch autonomous prd-set-task-verification-llm <id> --task <task-id> --backend claude-code --model opus --effort high
datawatch autonomous prd-set-story-verification-llm <id> --story <story-id> --backend ollama --model qwen3:8b
datawatch autonomous prd-set-verification-llm <id> --model ""   # clear the Automaton-level verifier model
datawatch autonomous learnings
```

---

## MCP tools (AI-ready)

Every endpoint above has an MCP tool of the same shape so an
operator-facing AI can drive the autonomous loop end-to-end:

| Tool | Purpose |
|---|---|
| `autonomous_status`         | loop snapshot |
| `autonomous_config_get`     | read config |
| `autonomous_config_set`     | merge fields into config (incl. `max_concurrent_tasks` and `max_engines_per_node`) |
| `autonomous_prd_list`       | list all PRDs |
| `autonomous_prd_create`     | create draft PRD |
| `autonomous_prd_get`        | fetch one PRD with tree |
| `autonomous_prd_decompose`  | run LLM planning phase (creates stories+tasks) |
| `autonomous_prd_run`        | kick executor |
| `autonomous_prd_cancel`     | cancel + archive |
| `autonomous_learnings`      | list learnings |
| `autonomous_prd_set_task_verification_llm` / `_set_story_verification_llm` / `_set_verification_llm` | per-task / per-story / per-Automaton verifier LLM |
| `autonomous_prd_set_task_project_dir` | per-task project dir override (empty = inherit Automaton dir) |

Typical AI workflow:
1. `autonomous_prd_create(spec="…", project_dir="…")` → `{id}`
2. `autonomous_prd_decompose(id)` → runs planning phase, populates stories + tasks
3. `autonomous_prd_get(id)` → review tree
4. `autonomous_prd_run(id)` → start execution
5. Poll `autonomous_status` until done

---

## Comm channels

Every comm channel (Signal / Telegram / Slack / Matrix / Discord /
ntfy / Twilio / Email / GitHub Webhook / generic Webhook) gets full
parity through the existing `rest` passthrough:

```
rest GET /api/autonomous/status
rest POST /api/autonomous/prds {"spec":"add pagination","project_dir":"/srv/app"}
rest POST /api/autonomous/prds/abcd1234/plan
rest POST /api/autonomous/prds/abcd1234/run
autonomous set-llm abcd1234 guardrail_backend=ollama guardrail_model=qwen3.8:27b
autonomous set-task-llm abcd1234 t1 claude-code high guardrail_backend=ollama
autonomous set-task-verification-llm abcd1234 t1 backend=claude-code model=opus effort=high
autonomous set-story-verification-llm abcd1234 s1 backend=ollama model=qwen3:8b
autonomous set-verification-llm abcd1234 model=
```

`guardrail_backend=` / `guardrail_model=` tokens can be appended to
`autonomous set-llm` / `set-task-llm`; when they are the only settings
given, no backend positional is needed and the worker LLM is left
unchanged. `guardrail_model=` with no value clears the override.

The per-node engine cap is a daemon setting, so it goes through `configure`
rather than an `autonomous` subcommand: `configure autonomous.max_engines_per_node=2`.

The reply is the JSON response from the daemon, pretty-printed when
short.

---

## Storage

Each PRD's full state is persisted as JSON-lines under
`<data_dir>/autonomous/`:

```
prds.jsonl       — PRDs with embedded story + task trees
learnings.jsonl  — extracted post-task learnings
automations.jsonl — Automation records (member graphs + history)
```

Files are rewritten in full on every change; small dataset assumption.
A SQLite upgrade via `internal/memory` is planned for v3.10.x if PRD
count outgrows JSON-lines.

---

## What ships in v3.10.0 (and what doesn't)

**In:** data models, JSONL store, planning prompt + parser,
security scanner, manager + executor with auto-fix-retry verification
loop, REST endpoints, MCP tools, CLI commands, comm `rest`
passthrough parity, full config parity, operator + AI-ready docs.

**Deferred (v3.10.x patches or BL117):** web UI Gantt board,
cross-cluster orchestration, plugin/external-tool integration, richer
auto-pause / auto-resume across daemon restarts. Long-form PRD-DAG
orchestration with guardrail sub-agents is BL117 (Sprint S8 / v3.12.0+).

---

<!-- BL279 see-also footer -->
## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [howto/autonomous-planning](../howto/autonomous-planning.md)
- [howto/autonomous-review-approve](../howto/autonomous-review-approve.md)
- [howto/automata-orchestrator](../howto/automata-orchestrator.md)
- [howto/guardrail-library](../howto/guardrail-library.md)
