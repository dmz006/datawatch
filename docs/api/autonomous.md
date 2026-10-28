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
```

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

Resolution: task → story → Automaton → global
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
Automaton level, `global`, or `pool`).

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

One cursor per Automation: members run one at a time — sequential
steps with conditional branches, no fan-out. Records persist under
`<data_dir>/autonomous/` and survive daemon restarts.

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
| 400 | validation: unknown member or edge target, cycle in the member graph, member PRD already owned by another Automation, or a template/owned PRD enrolled as a member |
| 401 | missing token |
| 404 | no Automation with that id |
| 405 | wrong method (e.g. PATCH) |
| 409 | `expected_version` stale; members/entry changed while not in draft; or delete of a non-deletable status |
| 503 | the daemon runs with autonomous disabled |

> **Stored records only, for now.** Creating an Automation stores its
> definition, membership and any history — it does not kick anything
> off, and nothing on the server advances from one member to the next
> or starts the next Automaton automatically yet. Running a member
> stays a normal run action on that PRD today.
>
> Full request/response schemas (`Automation`, `AutomationMember`,
> `BudgetPolicy`) are in `openapi.yaml` next to this file.

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
  task_max_attempt_seconds:  0        # wall-clock cap on one task attempt in seconds; 0 = off; recommended 5400
  task_no_progress_seconds:  0        # stop an attempt after N seconds without output, file changes, commits, or a CPU-using child process; 0 = off; recommended 1800
  preserve_attempt_work:  true        # save a killed attempt's uncommitted work to refs/datawatch/attempts/; restored on the next attempt unless clean_restart
  auto_fix_retries:       1           # re-prompt count on verifier failure
  security_scan:          true        # nightwire-port .py scanner before commit
```

### Task attempt limits (`task_max_attempt_seconds`, `task_no_progress_seconds`)

Both default to `0` (off). `task_max_attempt_seconds` hard-caps one
attempt by wall clock; `task_no_progress_seconds` stops an attempt that
goes N seconds without progress, where progress is terminal output
growth, a changed tracked or untracked file in the work directory (harness
scratch files ignored), a new commit, or a live child process using CPU.
A no-progress stop fails the attempt with `no progress for N seconds: no
output, no file changes, no commits`. In both cases the uncommitted work
is saved to a ref before the worker is stopped (never discarded) and the
normal retry budget applies. Recommended values: `5400` and `1800`.

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
