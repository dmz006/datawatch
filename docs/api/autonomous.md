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
| MCP   | `autonomous_status`, `autonomous_config_get/set`, `autonomous_prd_*`, `autonomous_learnings`, `autonomous_lane_fill_status`, `autonomous_triage` |
| CLI   | `datawatch autonomous <subcmd>` |
| Comm  | `rest GET /api/autonomous/status` etc. via the comm `rest` passthrough (Signal/Slack/Telegram/Matrix/etc.) |

---

## REST endpoints

```
GET    /api/autonomous/status              loop snapshot (running, active PRDs, queued/running)
GET    /api/autonomous/config              read current config
PUT    /api/autonomous/config              replace config (full body)

POST   /api/autonomous/prds                create PRD; body: {spec, project_dir, [backend], [effort], [kind]}
GET    /api/autonomous/prds                summary list, newest-updated first, paged; ?full=1 for whole records (see "Automata list" below)
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
POST   /api/autonomous/prds/{id}/set_verifier_policy         per-Automaton verifier policy; body: {[mode], [backends[]], actor}
POST   /api/autonomous/prds/{id}/set_task_verifier_policy    per-task verifier policy; body: {task_id, [mode], [backends[]], actor}
POST   /api/autonomous/prds/{id}/set_failure_escalation     failure escalation override; body: {[task_id], policy|null, actor} (null clears; see "Failure escalation")
POST   /api/autonomous/prds/{id}/resolve_task_review        operator decision on a task held for review; body: {task_id, action: retry|cancel, [spec], [backend], [model], [effort], [files[]], [note], actor}
POST   /api/autonomous/prds/{id}/set_dirs  directory scope; body: {write_dirs[], read_dirs[]} (absolute paths)
POST   /api/autonomous/prds/{id}/set_tags  replace the Automaton's tags; body: {[tag_ids[]], [primary_tag_id]}; tag_ids may be omitted when primary_tag_id is sent, which keeps the current tags ([] clears; primary_tag_id must be one of the tags or empty, else 400)
POST   /api/autonomous/prds/{id}/set_kind  Automaton kind; body: {kind} (feature|bug|other; "" derives it again); 400 on an unknown kind, 404 on an unknown id
POST   /api/autonomous/prds/{id}/set_lane_preference  lane an Automaton may take; body: {lane_preference: gpu|claude|any} ("" means any); 400 on an unknown value, 404 on an unknown id
POST   /api/autonomous/prds/{id}/set_notify_events  set the lifecycle event kinds the Automaton notifies on; body: {notify_events[]} — absent/null/[] clears (inherit); the reply carries the computed notify_events_effective
POST   /api/autonomous/prds/{id}/run       kick the executor for this PRD
GET    /api/autonomous/prds?tag={id}[&include_sub=false]  Automata carrying a tag (and, by default, any descendant tag)

GET    /api/autonomous/tags[?include_sub=&counts=]  tag tree (parent_id) with Automata counts; counts=true adds subtree, direct and session counts and an untagged object
POST   /api/autonomous/tags                 create; body: {name, [parent_id], [color], [description]}
PATCH  /api/autonomous/tags/{id}            rename / recolor / describe / reparent; 409 if the move makes a cycle
DELETE /api/autonomous/tags/{id}            409 while the tag has child tags, 409 with {count} while Automata carry it
GET    /api/autonomous/attention           the attention group: {statuses, items}; see "Attention list" below
GET    /api/autonomous/prds/{id}/children  list child PRDs spawned via SpawnPRD                                      [v5.9.0  / BL191 Q4]
GET    /api/sessions/{id}/progress         live progress snapshot for one session (turns, tool calls, last action,
                                           tokens) — same `progress` shape prd-get carries per running task

GET    /api/autonomous/learnings           extracted post-task learnings
GET    /api/autonomous/progress[?automation_id=&tag_id=&since=&until=]  progress metrics object (counts by kind, throughput, utilization, remaining work, completion range); since/until are YYYY-MM-DD, until is inclusive; 400 on a bad date
GET    /api/autonomous/progress/trend[?days=]  saved daily snapshots as {days, points[], direction}; days 1..365 (default 30), 400 outside that range
GET    /api/autonomous/lane-fill[?limit=N]  lane fill status: mode, lanes, queued eligible counts, pool evidence, newest decisions and idle alerts; limit 1..1000 (default 50), 400 outside that range

# Guardrail Library + Profiles
GET    /api/autonomous/guardrails                  list guardrail library (built-ins + skill-contributed)
GET    /api/autonomous/guardrail_profiles          list profiles
POST   /api/autonomous/guardrail_profiles          create profile; body: {name, description?, guardrails[], block_on_file_drift?, notify_events?}
GET    /api/autonomous/guardrail_profiles/{id}     get one profile
PUT    /api/autonomous/guardrail_profiles/{id}     update profile (a key left out stays as-is; an explicit empty notify_events clears it)
DELETE /api/autonomous/guardrail_profiles/{id}     delete profile
PUT    /api/autonomous/prds/{id}/guardrails        set per-Automaton override; body: {guardrail_profile?, per_task_guardrails[], per_story_guardrails[]}

# Automations — orchestration records (see the Automations section below)
GET    /api/automations              list Automations (each carries a computed tags array: the member Automata's tag IDs)
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
`task.session_started`, `task.session_cleared`, `scope_warning`,
`guardrail_warn`, `prd.updated`),
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

### Notify events (`set_notify_events`)
The set of lifecycle event kinds (the same known set as the subscription
filter above, `prd.completed`, `task.completed`, …) that the notification
pipeline fires for this Automaton, independent of who is subscribed.
Resolution is a tiered precedence chain — **Automaton > guardrail profile
> global default**. Each level may supply the whole set, or inherit
from the level behind it. An empty or unset list on a tier falls
through to the next one; the result is always an effective list.

```
POST /api/autonomous/prds/{id}/set_notify_events
     {"notify_events":["prd.completed","task.completed"]}
     # clear → inherit from the guardrail profile / global default:
     #   {}            /  {"notify_events":null}  /  {"notify_events":[]}
```

* **Per-Automaton** (`set_notify_events`): sets, replaces, or clears
    (`notify_events` null, `[]`, or an absent key) the list for that
    Automaton. POST only — any other method is `405`. Invalid entries are
    rejected with `400` and a message that names the offending kind (or
    its index) *and* lists the valid kinds, so nothing is ever partially
    applied.
* **Per-guardrail-profile** (create/update body `notify_events`, same
    key rule): applies to every Automaton that runs under the profile and
    whose Automaton-tier list is empty.
* **Global default** (config key `autonomous.notify.default_events`,
    written through the standard config surfaces — `PUT /api/config`
    and the autonomous config endpoint): the floor of the chain. Unset
    (`nil`) falls back to the built-in five default kinds; an explicit
    empty list is preserved as "no events at all", so it is distinguishable
    from unset.

Reads surface the resolution without ambiguity. The by-id Automaton and
`set_*` replies carry both `notify_events` (the raw list set at this
tier) and `notify_events_effective` — `{events, source}` where
`source` is `automaton`, `profile` or `global`, the tier that actually
supplied the effective kinds. `GET /api/autonomous/status` reports a
`notify` block: `default_events` (the in-effect global defaults) and
`overrides`, one entry per non-terminal Automaton whose effective list
resolves from the `automaton` or `profile` tier (`id`, `source`,
`events`) — sorted by Automaton ID and capped at 100. Both arrays are
always lists: empty, never null. All of these endpoints follow the same
auth as the other
`/api/autonomous` routes: the `set_notify_events` action requires
`autonomous:write` for federation peers, `autonomous:read` for the
status/config reads.

### Live task progress + looping warning

While a task is actively running — `status` `in_progress` or `verifying` —
the executor's periodic session poll also parses the worker's own pi JSONL
log and refreshes two fields that ride along in every PRD view (`prd-get`,
`tasks` and `full` detail, MCP `autonomous_prd_get`):

```json
{
  "id": "task-1234", "status": "in_progress",
  "progress": {
    "last_event_at": "2026-10-14T12:04:41Z",
    "assistant_turns": 12,
    "tool_calls": {"bash": 9, "write": 3},
    "last_tool_name": "bash",
    "last_tool_args": "{\"command\": \"go test ./...\"}",
    "last_summary": "Now running the full test suite before wrapping up.",
    "tokens": 48210
  },
  "looping_warning": "suspected looping: same call x5"
}
```

| Field | Meaning |
|---|---|
| `progress.last_event_at` | Timestamp (RFC3339) of the newest event in the log — freshness is the first thing to check. |
| `progress.assistant_turns` | Assistant-role messages so far, one per LLM reply (tool-calling ones included) — a proxy for "how much work has been attempted". |
| `progress.tool_calls` | Per-tool-name call counts across the log so far. |
| `progress.last_tool_name` / `last_tool_args` | The most recent tool call, with arguments as compact JSON (truncated to 500 chars). |
| `progress.last_summary` | First 200 chars of the most recent thinking/text block — what the worker was last planning or reporting. |
| `progress.tokens` | Cumulative tokens reported by the log's usage fields. |
| `looping_warning` | **Omitted unless set.** The worker re-issued the same tool name + arguments on three consecutive periodic polls (~an hour of real time); treat it as "stuck, needs an operator" rather than "progressing slowly". |

**Presence semantics.** The store forces both fields off the moment a task
leaves the active statuses (and strips them at every save/load), so
`progress` present ⇔ the worker is running right now. A `pending` task has
neither key in JSON; a `completed` task that ran for an hour keeps none of
its progress behind — history lives in the decisions log and the session
log on disk, not on the task.

`GET /api/sessions/{id}/progress` returns the same snapshot for any session
(pi-family backend, or a session whose log exists), regardless of autonomous
involvement — session id, full id, state, `log_path`, plus the fields above.
404 when the session is unknown or has no log (non-pi backend, or log not
yet flushed). Non-GET methods get 405.

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

### Verifier policy (`set_task_verifier_policy` / `set_verifier_policy`)

The verifier policy chooses **which backend runs a verification**, as
opposed to the verification LLM override above, which names the LLM that
judges. The global policy is `autonomous.verifier_policy`; an Automaton or
a single task can override its **mode** and **backend list**.

| Method | How |
|--------|-----|
| **YAML** | `autonomous.verifier_policy.*` in `~/.datawatch/config.yaml` (fields below) |
| **REST** | `POST /api/autonomous/prds/{id}/set_verifier_policy` and `POST /api/autonomous/prds/{id}/set_task_verifier_policy`; global keys through `PUT /api/config` and `PUT /api/autonomous/config` |
| **MCP** | `autonomous_prd_set_verifier_policy`, `autonomous_prd_set_task_verifier_policy`; the global keys are the `verifier_policy_*` arguments of `autonomous_config_set` |

Modes:

| Mode | Behaviour |
|------|-----------|
| `fixed` (default) | The first eligible backend only. |
| `prefer-free` | The first backend that has a free slot; falls back down the list. |
| `balance` | The backend with the lowest relative load. |
| `gpu-only` | Keeps only local/GPU backends in the list. |
| `claude-only` | Keeps only Claude backends in the list. |

Global fields (`autonomous.verifier_policy.<field>` in `PUT /api/config`, or
one `verifier_policy` object in `PUT /api/autonomous/config`):

| Field | Meaning |
|-------|---------|
| `mode` | One of the modes above. Empty means `fixed`. |
| `backends` | Ordered list of eligible verifier backends. Empty falls back to `autonomous.verification_backends`, then `autonomous.verification_backend`. |
| `claude_session_cap` | Most Claude verifier sessions started per window. `0` = unlimited. |
| `claude_cap_window_minutes` | Window length for the cap. `0` = 60. |
| `force_stronger_on_retry` | Send a retried verification to the stronger verifier. |
| `final_gate_stronger` | Send the final verification gate to the stronger verifier. |
| `stronger_backend` / `stronger_model` | The stronger verifier. Empty means `claude-code` and the default Sonnet model. |

A config write is validated before anything is stored: an unknown `mode`,
an empty entry in `backends`, a negative `claude_session_cap` or
`claude_cap_window_minutes`, a value of the wrong type, or an unknown field
is a `400` and changes nothing. A valid write applies to the next
verification with no restart, and is persisted to `config.yaml`.
`PUT /api/autonomous/config` merges the `verifier_policy` object onto the
current policy field by field, so a partial object leaves the other fields
alone. `GET /api/config` and `GET /api/autonomous/config` return the policy
under `verifier_policy`.

Per-unit body keys are presence-aware like the verification LLM keys:
**absent** leaves the field unchanged, **`""`** (mode) or **`[]`** (backends)
clears it so the level inherits, a value sets it. Clearing both fields
removes the override. `task_id` is required on the task variant. `mode`
must be one of the modes above; every `backends` entry must be a non-empty,
configured LLM name (an unknown name is a `400`). Resolution is task →
Automaton → global; the mode and the backend list each resolve
independently, so a task that sets only a mode still uses the Automaton's
backend list. The other policy fields come from the global policy.

Locking follows the verification LLM override: the Automaton variant is
accepted while the Automaton is `needs_review`, `revisions_asked` or
`cancelled`; the task variant while the Automaton's status is listed in
`autonomous.task_override_statuses`. Any other status is a `400`. Each change
is audit-logged and appended to the Automaton's decision log
(`set_prd_verifier_policy` / `set_task_verifier_policy`). The response is the
updated Automaton.

Each verification records the backend the policy picked on the task's
`verifier_policy_choice` (backend, model, reason, time) and in a
`verifier_policy` decision on the Automaton.

```bash
# balance one risky task across two local backends
curl -sX POST http://localhost:7433/api/autonomous/prds/<id>/set_task_verifier_policy \
  -H "Content-Type: application/json" \
  -d '{"task_id":"<task-id>","mode":"balance","backends":["ollama-a","ollama-b"]}'

# clear the mode only; the backend list stays
curl -sX POST http://localhost:7433/api/autonomous/prds/<id>/set_task_verifier_policy \
  -H "Content-Type: application/json" \
  -d '{"task_id":"<task-id>","mode":""}'

# global policy: prefer a free local backend, cap Claude verifiers at 4 per hour
curl -sX PUT http://localhost:7433/api/autonomous/config \
  -H "Content-Type: application/json" \
  -d '{"verifier_policy":{"mode":"prefer-free","backends":["ollama-a","claude-code"],"claude_session_cap":4,"claude_cap_window_minutes":60}}'
```

### Judge routing and time limit (`autonomous.judge_policy` / `autonomous.judge_timeout_seconds` / `autonomous.judge_size_threshold_lines`)

The judges (the Automaton's guardrail, plan review and other autonomous judges)
are a distinct role in the verifier routing policy. Their backend list is
`autonomous.judge_policy`, one attempt per backend; an empty list means the
judges follow the verifier policy above. `autonomous.judge_timeout_seconds` is
the time limit for one judge run, 1800 seconds by default, and the wait for a
capacity slot does not count against it. When every backend in the judge policy
has timed out, the Automaton is held in `needs_review` and an error alert lists
every attempt. `autonomous.judge_size_threshold_lines` (default 400, `0` = off) is the
changed-line count (added plus deleted, on the Automaton branch against its merge
base, lock files left out) above which the judges try `claude-code` first and the
rest follow in their usual order, still one attempt per backend; the decision log
names the rule and the line count. The settings are read and written with `GET/PUT /api/autonomous/config`
(`judge_policy`, `judge_timeout_seconds`, `judge_size_threshold_lines`), MCP `autonomous_config_set`, the CLI
`autonomous config-set --judge-policy --judge-timeout-seconds --judge-size-threshold-lines`, and the comm verbs
`autonomous judge-policy` and `autonomous judge-policy-set`. The guide, with the
modes, the alerts and how to resolve a hold, is [howto/judge-policy.md](../howto/judge-policy.md).

### Failure escalation and held tasks (`set_failure_escalation` / `resolve_task_review`)

A task that keeps failing moves up a ladder of workers and, at `max_failures` failed attempts, is held with status `review_needed` for the operator. The policy has three fields, `enabled`, `max_failures` and `steps`, and each resolves per field: task override, then Automaton override, then the daemon default (`autonomous.failure_escalation`). A nil, zero or empty field means "not set at this level." The guide is [howto/failure-escalation.md](../howto/failure-escalation.md).

```
POST /api/autonomous/prds/{id}/set_failure_escalation
{"task_id": "<optional>", "policy": {"enabled": true, "max_failures": 4,
  "steps": [{"backend": "pi-prompt", "model": "ollama/qwen3.8:27b", "attempts": 2},
            {"backend": "claude-code", "model": "claude-opus-5-5", "effort": "high"}]},
 "actor": "operator"}

POST /api/autonomous/prds/{id}/set_failure_escalation
{"policy": null}              # clears the Automaton override
{"task_id": "t1", "policy": null}   # clears one task's override
```

- `task_id` absent or empty addresses the Automaton; a non-empty `task_id` addresses that task.
- `policy` is required. `null` clears the override at that level. An absent `policy` is a 400.
- The response is the updated Automaton.

```
POST /api/autonomous/prds/{id}/resolve_task_review
{"task_id": "t1", "action": "retry", "spec": "<replacement spec>", "backend": "claude-code",
 "model": "claude-opus-5-5", "effort": "high", "files": ["internal/parser/parse.go"],
 "note": "the failure is in the parser only", "actor": "operator"}

{"task_id": "t1", "action": "cancel", "note": "out of scope for this release", "actor": "operator"}
```

- `action` is `retry` or `cancel`. The other fields apply to `retry` only.
- Each change in a retry is checked before anything is written, so a refused change leaves the task held.
- `note` is recorded on the Automaton's decision log and given to the next attempt.
- The call works while the Automaton is running and other tasks are live. It answers at once, does not wait for the executor, and starts no second executor: a running executor is woken to read the Automaton again and launch the retried task (or fail the dependents of a cancelled one). An Automaton with no running executor is relaunched, as Resume does.
- The response is the updated Automaton.

Status codes for both routes:

| Code | When |
|---|---|
| 200 | Applied. The response is the updated Automaton. |
| 400 | Bad JSON, an invalid policy or action, an unknown effort or model/backend pair, or a missing `policy` key. The message names the field. |
| 403 | The session token lacks the autonomous-write capability (federation peers). |
| 404 | Unknown Automaton or task. |
| 405 | Method other than POST. |
| 409 | `resolve_task_review` only: the task is not held for review. Nothing changes. |
| 501 | The Automaton manager does not support the operation (test doubles only; the daemon always supports it). |

Both routes write an audit entry (`set_failure_escalation` or `resolve_task_review`, target `automaton`).

**New and changed fields.** These appear in the JSON views:

| Where | Field | Meaning |
|---|---|---|
| Automaton | `failure_escalation` | The Automaton's override (omitted when unset). |
| Task | `failure_escalation` | The task's override (omitted when unset). |
| Task | `attempts` | One entry per failed, stalled or cancelled attempt: `number`, `backend`, `model`, `effort`, `started_at`, `ended_at`, `duration_seconds`, `outcome`, `failure_category`, `failure_summary`, `verifier_summary`, `verifier_issues`, `changed_files`, `snapshot_ref`, `escalated`, `runner_exit_status`, `runner_last_output`, `runner_processes`. The three runner fields are set only for a pi-prompt attempt that stalled and are omitted otherwise: `runner_exit_status` is pi's exit status when the pane showed it, `runner_last_output` the last non-empty pane lines (at most 20, oldest first), `runner_processes` the runner's processes as `pid name argv` lines (at most 40). The task's `runner_evidence` is internal: the worker wait loop sets it just before a stall and clears it when the attempt is recorded, so it is never returned on its own. |
| Task | `escalation_failures` | Failed attempts since the last operator action. Any reset of the task sets it back to 0. |
| Task | `review_summary` | Set while the task is held: `held_at`, `failure_count`, `total_seconds` and `text`, a plain-text report of every attempt. |
| Task | `operator_worker_set` | `true` when the operator's last review retry chose the task's worker. The escalation ladder then keeps that worker; a retry without a worker change clears it. |
| Task | `status` | `review_needed` while held. It is not terminal and not a failure. |
| Task | `executor_source` | `escalation` when a ladder step chose the worker. |
| Decision log | `kind` | `failure_escalation` when an attempt moves to a different worker; `review_needed` when a task is held. |
| `GET /api/autonomous/status` | `review_needed` | Every held task across all Automata, oldest hold first: `automaton_id`, `automaton_title`, `task_id`, `task_title`, `failure_count`, `last_backend`, `last_model`, `held_at`. Omitted when none is held. |
| Automaton summary | `review_needed` | The Automaton's held tasks, in the same shape. Omitted when none is held. |
| Task listing | `escalation_failures`, `attempt_count`, `review_summary` | The count, the number of attempts recorded, and the report text. |
| Automation status | `review_needed_count`, `review_needed_task_ids` | Held tasks of the current member Automaton. Omitted when none is held. |

Daemon default: `GET /api/config` returns `autonomous.failure_escalation` (`enabled`, `max_failures`, `steps`), and `PUT /api/config` takes the keys `autonomous.failure_escalation.enabled`, `.max_failures` and `.steps`. `steps` accepts a JSON array or the compact text `backend:model[@attempts],...`, where `-` means the task's normal worker. A bad value is a 400 naming the key, and nothing in that write is applied.

### Tags on Automata and the tag counts

Every Automaton carries its tags. A summary list row (the default
`GET /api/autonomous/prds`) carries them as `tags`; the full record
(`?full=1`, and `GET /api/autonomous/prds/{id}`) carries them as `tag_ids`. Both
are the same tag IDs. A row also carries `primary_tag_id` when the Automaton has
a primary tag. The primary tag is set with `POST .../set_tags` and is used only to
sort an Automaton in a tag browser; it never hides a membership. The primary
defaults to none, and removing the primary tag from the Automaton clears it.
The same route sets it from every surface: `datawatch autonomous prd-set-tags <id> --primary <tag-id>`,
the MCP `autonomous_prd_set_tags` tool with `primary_tag_id`, and the comm verb
`autonomous set-primary <prd-id> <tag-id|none>`. A body that sends `primary_tag_id` without
`tag_ids` keeps the current tag set and changes only the primary. A body that sends
`tag_ids` alone keeps the current primary while its tag stays and clears it when the
tag is removed.
`task_count`, `story_count` and `task_status_counts` are unchanged.

`GET /api/autonomous/tags?counts=true` adds these fields to each tag:

| Field | Meaning |
|---|---|
| `count` | Unchanged: Automata carrying the tag, rolled up with `include_sub`. |
| `subtree_count` | Distinct Automata carrying the tag or any descendant. An Automaton counts once per tag, however many of its tags sit in that subtree. This is the main figure a tag browser shows. |
| `direct_count` | Automata carrying exactly this tag. |
| `session_subtree_count` | As `subtree_count`, for sessions. A session counts through its own tags and through the tags it inherits from its Automaton. |
| `session_direct_count` | As `direct_count`, for sessions, with inherited tags included. |

The reply also carries `untagged: {count, session_count}`: the Automata, and the
sessions, that carry no tag at all. A session's inherited tags count, so a session
is untagged only when neither it nor its Automaton has a tag.

### Tag change events (WebSocket `tag_update`)

Every change to the tag registry and to tag assignments sends one `tag_update`
frame to every connected WebSocket client, so an open tag browser refreshes its
counts without polling. The frame is sent after the change is saved, and a refused
change (a cycle, a tag still in use, a tag with children, an unknown tag) sends nothing.

The data carries IDs only. A client that needs names or counts refetches
`GET /api/autonomous/tags?counts=true`.

| `kind` | Sent when | Fields |
|---|---|---|
| `created` | A tag is created | `tag_id` |
| `renamed` | A tag is renamed | `tag_id` |
| `reparented` | A tag's parent changes | `tag_id` |
| `recolored` | A tag's color changes | `tag_id` |
| `deleted` | A tag is deleted | `tag_id` |
| `assigned` | An Automaton's tag list or primary tag is saved | `prd_id` |
| `session_assigned` | A session's own tags are saved | `session_id` (the full ID) |
| `registry_synced` | A peer pulls a snapshot from its keeper and applies it | none (the client refetches `GET /api/autonomous/tags`) |

A tag description change sends nothing, because a description never changes a count.
A rename or a re-parent sends exactly one frame. An Automaton's tag change is also
reported by its `prd_update` frame, which the Automata list already uses.

A status change (for example `blocked`, `needs_review` or `paused`) sends one `prd_update`
frame for the Automaton, with its `prd_id` and `status`. Attention lists refresh from that frame.

Surfaces: `tag_update` is a WebSocket event only; it has no REST, MCP, CLI or comm-channel
form. The PWA refreshes its tag counts from it. The Android and iPhone clients have not
been given a handler for it yet. Tag changes made through REST, MCP, CLI or the comm channel
go through the same registry methods, so they send the same frames.

### Shared tag registry

Several daemons can share one tag registry, so the same tags (their names,
parents, colors and descriptions) appear on each of them. One daemon keeps the
registry. The others keep a cached copy of it and send their tag edits to the
keeper. The registry is set with `autonomous.tags.registry_keeper` and synced on
`autonomous.tags.registry_sync_seconds`. The sync paths (pull, forwarded write,
stale copy, merge) are drawn in [flow/tag-registry-sync-flow.md](../flow/tag-registry-sync-flow.md),
and the operator steps are in [howto/automata-tag-registry.md](../howto/automata-tag-registry.md).

**Roles.** The role follows from `registry_keeper`:

| Value | Role | What the daemon does |
|---|---|---|
| empty | standalone | Keeps its own tags and syncs with nobody. This is the default, and what a daemon did before the registry existed. |
| this daemon's own name | keeper | Holds the registry. Its peers pull the registry from it, and its own writes apply directly. |
| another peer's name | peer | Pulls the keeper's tags into a local cache and forwards its tag writes to the keeper. |

The keeper's name is a federation peer name, so the peer must be a federation peer
of the daemon. Both config keys take effect at runtime; no restart is needed.

**Cache and stale copies.** A peer keeps the last copy of the keeper's tags on disk,
so a restart does not lose it. Every `registry_sync_seconds` (a whole number of
seconds, minimum 5, default 30) the peer asks the keeper for its snapshot. While the
keeper has not answered, the peer shows the cached copy and reports `stale: true`
with the time of the last successful sync in `last_synced_at`.

**Writes.** On a peer, a create, rename, re-parent, recolor, describe or delete is
forwarded to the keeper, and the reply shows the keeper's value. A keeper or a
standalone daemon applies a write to its own registry.

If the keeper is unreachable from a peer:

- rename, re-parent, recolor, describe and delete are refused with 503. The
  message says the last copy is shown, and nothing changes;
- create still works. The tag is created on the peer and marked `pending_sync: true`
  until it reaches the keeper.

**Conflicts.** A peer sends the registry revision it last saw with each write, and
the keeper compares it with the last change to each field (name, parent, color and
description). When the keeper has changed a field the peer also edited, the keeper
keeps its own value and the reply carries `"conflict": true` and `"conflict_field"`
naming the field. The peer shows the keeper's value.

**Pending sync.** A tag a peer created while the keeper was unreachable is counted
in `pending_sync_count` until the keeper has it. When the keeper answers, the tag is
pushed. If the keeper already holds a tag with the same name under the same parent,
the two merge into the keeper's tag, and a merge note records it.

**Merge and removed-on-keeper notes.** The registry object lists the notes that
have not been dismissed on this daemon:

- `merged`: a pending tag was merged into a keeper tag. `kept_id` is the keeper's
  tag, `replaced_id` is the pending tag, and `merged_at` is when it happened.
- `removed_on_keeper`: the keeper deleted a tag that this daemon still had
  assignments on. `tag_id` names the deleted tag, so the operator can see which
  Automata and sessions it touched.

A note is dismissed on the daemon that shows it. A dismissed note stays dismissed
when later snapshots are applied. Dismissing a note changes no tag and no
assignment.

**Routes.**

| Method and path | Capability | Purpose |
|---|---|---|
| `GET /api/autonomous/tags` | autonomous read | Now carries a `registry` object (see below) with the tag reply. |
| `POST`, `PATCH`, `DELETE /api/autonomous/tags[/{id}]` | autonomous write | Go through the registry on a peer; see the write rules above. |
| `GET /api/autonomous/tags/registry` | autonomous read | Keeper-to-peer protocol route: the keeper's snapshot `{revision, tags, notes}`. A peer answers 409. |
| `POST /api/autonomous/tags/registry/write` | autonomous write | Keeper-to-peer protocol route: applies one forwarded write and returns the keeper's result. Not for operators. |
| `POST /api/autonomous/tags/registry/notes/{note-id}/dismiss` | autonomous write | Dismisses one note on this daemon. Returns `{"dismissed": id}`; 404 when no such note. |

The `registry` object on `GET /api/autonomous/tags`:

```json
"registry": {
  "role": "peer",
  "keeper": "keeper-node",
  "stale": false,
  "last_synced_at": "2026-10-26T02:10:00Z",
  "revision": 42,
  "pending_sync_count": 0,
  "notes": [],
  "last_error": ""
}
```

`last_synced_at` is omitted before the first answer from the keeper. `notes` is always
an array. Fields: `role` (`standalone`, `keeper` or `peer`), `keeper` (the keeper's
name, empty when standalone), `stale`, `last_synced_at`, `revision`,
`pending_sync_count`, `notes` and `last_error`.

**Refusal bodies.** A refusal is JSON `{"error": text, "kind": kind}`, and a `count`
field is added only for `in_use`.

| Kind | HTTP status | Meaning |
|---|---|---|
| `not_found` | 404 | Unknown tag |
| `invalid` | 400 | The name, parent, color or other value is not valid |
| `has_children` | 409 | The tag still has child tags |
| `in_use` | 409 | Automata still carry the tag; `count` is how many |
| `cycle` | 409 | A move would make a tag its own ancestor |
| `not_keeper` | 409 | A keeper-only route was called on a peer or standalone daemon |
| `note_not_found` | 404 | No such merge or removal note on this daemon |
| `unreachable` | 503 | The keeper cannot be reached from this peer |

**Configuration.**

```yaml
autonomous:
  tags:
    registry_keeper: ""          # "" = standalone; this daemon's own name = keeper; another peer's name = peer
    registry_sync_seconds: 30    # how often a peer pulls the keeper's snapshot; whole seconds, minimum 5
```

Both keys can be set from YAML under `autonomous.tags`, through `GET` and `PUT
/api/config`, the comm `configure` verb, MCP `config_set` and the CLI `config set`.

**Feature access.** Every surface for the registry, with the reason for each
exclusion. The keeper-to-peer routes are protocol routes, not operator actions.

| Endpoint | REST | MCP | CLI | Comm channel | YAML/config | PWA | Android | iPhone |
|---|---|---|---|---|---|---|---|---|
| `GET /api/autonomous/tags/registry` (keeper snapshot) | yes | no (keeper-to-peer protocol; the status tool shows the local registry object) | no (same reason) | no (same reason) | n/a | no (no UI; a peer's sync is not something an operator edits) | no (not in this change) | no (not in this change) |
| `POST /api/autonomous/tags/registry/write` | yes (protocol) | no (protocol route, peers only, not an operator action) | no (same reason) | no (same reason) | n/a | no | no | no |
| `GET /api/autonomous/tags` (registry object) | yes | yes (`autonomous_tag_registry_status`; `autonomous_tag_list` returns it too) | yes (`tag-registry`; `tag-list` prints it too) | yes (`tag-registry`) | n/a | reads it through the tag browser; no registry banner in this change | no (not in this change) | no (not in this change) |
| `POST /api/autonomous/tags/registry/notes/{id}/dismiss` | yes | yes (`autonomous_tag_registry_dismiss_note`) | yes (`tag-registry-dismiss`) | yes (`tag-registry-dismiss`) | n/a | no (no dismiss control in this change) | no (not in this change) | no (not in this change) |
| `autonomous.tags.registry_keeper` and `registry_sync_seconds` | yes (`GET`/`PUT /api/config`) | yes (`config_set`) | yes (`config set`) | yes (`configure`) | yes (`autonomous.tags`) | yes: the Settings card's Automata tag browser group has a text field for the keeper and a number field for the sync interval | no (not in this change) | no (not in this change) |

When a surface gains or loses one of these endpoints, update its cell in this table
in the same change.

### Attention list (`GET /api/autonomous/attention`)

Returns `{"statuses": [...], "items": [...]}`. The items are the Automata whose
status is one of `statuses`, plus every Automaton with a task held for operator
review. Each item is the summary view, with `blocked_reason`. `statuses` names the
statuses in force; they are `blocked`, `needs_review` and `paused`, so a paused
Automaton is in the group. Read-only; needs the autonomous read capability.

### Automata list (`GET /api/autonomous/prds`)

The default reply is a summary list, not whole records: each row carries ids,
title, status, counts, tags and timestamps, and none of the story, task or
verifier text. The body is `{"prds": [...], "total": N, "next_cursor": "..."}`.
Fetch one Automaton in full with `GET /api/autonomous/prds/{id}`.

| Parameter | Effect |
|---|---|
| `limit=<n>` | Rows per page. Default 50, maximum 500; anything below 1 or not a number returns 400. |
| `cursor=<opaque>` | The `next_cursor` of the previous page. Pages run newest `updated_at` first, ties by id descending. An invalid cursor returns 400. `next_cursor` is absent on the last page. |
| `status=<list>` | Comma list of statuses to keep, e.g. `running,blocked`. |
| `active=1` | Keep only running, queued, waiting, approved, blocked and paused work. |
| `q=<text>` | Case-insensitive match against title and id. |
| `tag=<id>` | Keep Automata carrying that tag. `tag_id` is the same parameter. |
| `full=1` | The original shape: every Automaton in full under `prds`, with stories and tasks. Ignores the paging and filter parameters. Can be tens of megabytes on a daemon with a long history. |

With no filter, the first page holds every active Automaton plus the most recent
`limit` others, so work in progress is never paged out of sight; `cursor` pages walk
only the others. `total` is the number of rows matching the filters.

**ETag.** The summary list carries `ETag` and `Cache-Control: no-cache`. Send the
value back in `If-None-Match`; an unchanged page answers `304 Not Modified` with an
empty body. The tag changes when a returned row's `rev` or `updated_at` changes, so
a poll that finds nothing new costs a header exchange only. `full=1` has no ETag.

### Capped text in the by-id GET

`GET /api/autonomous/prds/{id}` cuts each long text field (Automaton `spec` and
`memory_report`, story `description`, task `spec`, `error`, `prior_attempt_hint`,
`start_failure_tail[]`, `attempt_refs[]`, `verification.raw_judge`, `verification.summary`,
`verification.issues[]`, and verdict text) to `autonomous.detail_text_cap_bytes` (default
4096). A cut field ends with `\n…[truncated: kept N of M bytes]`, and the record carries a
`truncated` array of `{field, bytes, kept, ref}` where `ref` is the URL of the full text.
The store is never cut. To get everything:

- `?full=1` returns the record uncapped, as before.
- `GET /api/autonomous/prds/{id}/outputs` returns the heavy fields of every task, uncapped;
  `?task=<task_id>` or `GET /api/autonomous/prds/{id}/tasks/{task_id}/outputs` returns one
  task (404 for an unknown task).

`detail=tasks` applies the same cap to the task `error`. `POST .../edit_task` refuses a
`new_spec` that still ends with a truncation marker. The list (`GET /api/autonomous/prds`)
never reads these fields.

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
| `POST /api/automations/{id}/set_decision_assistant` | `draft`, `active`, `paused`, `blocked` | Sets or clears the optional local-model decision assistant of the Automation, or of one member (default off; see "Decision assistant"). Does not change the status. |

**Request body.** Every endpoint takes a JSON object. All fields are
optional on the wire, but each operation has the fields it needs.

| Field | Used by | Meaning |
|---|---|---|
| `expected_version` | all | The Automation `version` you read. It must equal the stored version exactly. A stale value, or an omitted one (read as `0`), is a `409` and nothing changes, whatever the Automation's state. Read the Automation again and retry. |
| `actor` | all | Who is acting, recorded in the audit trail. Defaults to `operator`. An agent or session passes `agent:<id>` or `session:<id>`. |
| `reason` | all | Free text recorded with the audit entry and the Automation's decision log. Required by `insert_member`. |
| `cancel_in_flight` | `cancel` | Boolean, required, no default. See below. |
| `member_id` | `skip_member`, `remove_member`, `reorder_edges`, `approve_insert`, `set_budget`, `set_decision_assistant` | The member to act on. For `approve_insert` it is the pending insert's ID, which is also the ID the member gets once approved. For `skip_member` it names the branch to act on in a forked Automation (see "Branches"). |
| `edge` | `skip_member` | `on_complete` (default when omitted), `on_blocked` or `on_failed`. Any other value is a `400`. |
| `on_complete`, `on_blocked`, `on_failed` | `reorder_edges` | The member ID each edge now names. An edge that is left out or empty becomes no next member, so send all three edges you want to keep. A target must exist and must not have started. |
| `member` | `insert_member` | Object `{"prd_id": "...", "name": "..."}`. `prd_id` is required. `name` is an optional display name. |
| `urgent` | `insert_member` | Boolean. `true` takes the next slot, `false` (default) goes to the end of the chain. |
| `after` | `insert_member` | Member ID to insert directly after. Empty means the end of the chain. An urgent insert must leave it empty. |
| `ceiling_type`, `ceiling_value`, `window`, `pacing`, `on_breach`, `backend`, `allocation_estimate`, `peer_window`, `reset_time`, `reset_weekday`, `reset_day`, `reset_zone` | `set_budget` | The spending limit. They replace the whole limit; see "Spending limit". |
| `estimated_cost_usd` | `set_budget` | With `member_id`: the member's expected cost in dollars (a number, `0` removes it). Alone it changes only the estimate. |
| `clear` | `set_budget` | Boolean. `true` removes the limit; it cannot be combined with any policy field or `estimated_cost_usd`. |
| `backend`, `model`, `mode`, `hooks`, `thresholds` | `set_decision_assistant` | The decision assistant. They replace the whole setting; see "Decision assistant". `backend` and `model` are required together; an empty `backend` turns it off. |
| `clear` | `set_decision_assistant` | Boolean. `true` turns the assistant off; it cannot be combined with `backend`, `model`, `mode`, `hooks` or `thresholds`. |

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

#### Decision assistant (`set_decision_assistant`)

An Automation, or one of its members, can hand a bounded judgment call to a
configured local model instead of a hardcoded default or an operator stop. It
is **off by default**. The setting is `decision_assistant` on the record, and a
member's setting wins over the Automation's.

```
POST /api/automations/3fa9c2d1/set_decision_assistant
{"backend": "pi-prompt", "model": "qwen3.8:27b", "mode": "shadow",
 "hooks": ["empty_edge", "budget_breach"], "thresholds": {"empty_edge": 0.85},
 "expected_version": 4, "actor": "operator"}
```

`mode` is `shadow` (the default; records what the assistant would have done)
or `live` (acts on a decision at or above the hook's threshold). `hooks` are
`empty_edge`, `skip_member`, `insert_member` and `budget_breach`; none enabled
means no hook consults the assistant. `thresholds` map a hook to a confidence
in `(0,1]`; the defaults are `0.9`, and `0.95` for `budget_breach`. Below the
threshold the assistant escalates to the operator. `member_id` targets one
member; `{"clear": true}` or an empty `backend` turns the assistant off. The
assistant only chooses among already-allowed options: destroying state or
history, raising a budget ceiling and any action a guardrail marks as needing
human sign-off stay with the operator.

Errors: `400` for an unknown backend, hook or mode, a threshold outside
`(0,1]`, a backend without a model, or `clear` combined with settings; `404`
for an unknown Automation or member; `409` for a stale `expected_version` or a
completed or cancelled Automation. The audit entry is
`automation_set_decision_assistant`.

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
`automation_approve_insert`, `automation_set_decision_assistant` (with the
backend, model, mode and hooks, or `cleared`) and `automation_set_budget` (with the policy
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

### Converting an orchestrator graph (`import-graph`)

`POST /api/automations/import-graph/{graph_id}` reads an orchestrator graph and
creates a draft Automation from it. Automaton nodes become members, a fan-out
and its fan-in become a paired fork and join, and guardrail nodes are dropped
with a warning. The graph is only read, never changed. Body: `{"dry_run": bool}`
(optional).

| Status | Meaning |
|---|---|
| `200` | The new draft Automation; with `dry_run` true, `{input, warnings, dry_run: true}` and nothing is stored or audited |
| `404` | Unknown graph |
| `409` | A member Automaton already belongs to another Automation |
| `422` | The graph cannot be expressed as an Automation; the body gives the reason |
| `503` | The orchestrator or the automation manager is not available |

Preview first with a dry run:

```
curl -sk -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"dry_run": true}' https://localhost:8443/api/automations/import-graph/3fa9c2d1
```

A real import writes one `automation_import_graph` audit entry. The same verb
is the MCP tool `automation_import_graph`, the CLI `datawatch automation
import-graph <graph-id> [--dry-run]` and the comm verb `automation import-graph
<graph-id> [dry-run]`.

#### Deprecation

The three orchestrator graph operations that build and start a graph are
deprecated and keep working until a later release removes them:

| Operation | REST | MCP tool | CLI / comm |
|---|---|---|---|
| Create | `POST /api/orchestrator/graphs` | `orchestrator_graph_create` | `orchestrator create` |
| Plan | `POST /api/orchestrator/graphs/{id}/plan` | `orchestrator_graph_plan` | `orchestrator plan` |
| Run | `POST /api/orchestrator/graphs/{id}/run` | `orchestrator_graph_run` | `orchestrator run` |

Each response carries a `Deprecation: true` header and a `deprecation` field (REST),
or a trailing notice (MCP, CLI, comm) that names `automation import-graph`. The daemon
logs the warning once per operation per process. Migrate existing graphs with
`import-graph`; see [howto/automation-chains](../howto/automation-chains.md).

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

## Lane fill (`GET /api/autonomous/lane-fill`)

Lane fill runs a planner on every coordinator poll. It records, for each idle lane, what it would start, and in `on` mode it carries the decisions out. The how-to is [howto/automation-lane-fill.md](../howto/automation-lane-fill.md).

**Status response.** `GET /api/autonomous/lane-fill` (`limit` 1 to 1000, default 50; the decisions are newest first):

```json
{
  "mode": "shadow",
  "last_poll_at": "2026-10-24T13:00:00Z",
  "lanes": [
    {"id": "gpu-1", "type": "gpu", "state": "occupied",
     "occupant": {"prd_id": "a1b2c3d4", "title": "Example Automaton", "backend": "pi-prompt"},
     "blocker": "", "eligible_waiting": 0},
    {"id": "claude-1", "type": "claude", "state": "idle", "occupant": null,
     "blocker": "no eligible work for this lane", "eligible_waiting": 0}
  ],
  "queued_eligible": {"gpu": 3, "claude": 1},
  "evidence": {"checked_at": "2026-10-24T13:00:00Z", "gpu_active_sessions": 1,
               "loaded_models": [{"host": "local", "model": "qwen3.8:27b"}], "error": ""},
  "decisions": [
    {"at": "2026-10-24T13:00:00Z", "mode": "shadow", "lane_id": "gpu-2", "lane_type": "gpu",
     "action": "start_prd", "prd_id": "b1c2d3e4", "title": "Example", "reason": "...",
     "blocker": "", "executed": false, "error": "", "repeat_count": 1,
     "last_seen_at": "2026-10-24T13:00:00Z"}
  ],
  "alerts": [
    {"at": "2026-10-24T12:30:00Z", "lane_id": "gpu-2", "work": "Example (b1c2d3e4)",
     "message": "Lane gpu-2 has been idle for 30 minutes with eligible work waiting. First eligible: Example (b1c2d3e4)."}
  ]
}
```

- `lanes[].state` is `occupied` or `idle`. An idle lane carries a `blocker` when the planner will not call it idle without a reason.
- `evidence.error` is set when a pool host could not be read. A GPU lane is then never reported as idle.
- `decisions[].action` is `start_prd`, `decompose`, `decompose_ahead`, `report_merge`, `blocked` or `idle`. `executed: false` is a would-start in shadow mode and must not be shown as a start.
- `decisions[].mode` is `off`, `shadow` or `on`, the mode in effect when the decision was made.

**Lane preference.** `POST /api/autonomous/prds/{id}/set_lane_preference` with `{"lane_preference": "gpu" | "claude" | "any"}`. An empty value means `any`. The stored value is returned on the Automaton as `lane_preference`, and an absent value means `any`. A value outside the three is refused with 400 and nothing is stored.

**Configuration.** Every key is under `autonomous.lane_fill` and is read live:

```yaml
autonomous:
  lane_fill:
    mode: shadow               # off | shadow | on (default shadow)
    gpu_lanes: 2               # GPU lanes on the local model pool
    claude_lanes: 3            # Claude lanes; 0 means none
    idle_alert_minutes: 30     # 0 turns the idle alert off
    decompose_ahead_min: 2     # decomposed Automata kept ready per lane type
    poll_seconds: 60           # minimum 5
    gpu_backend: pi-prompt
    gpu_model: ""              # empty = the backend's own model
    claude_backend: claude-code
```

**Surfaces.** The same status is read by MCP `autonomous_lane_fill_status`, the CLI `datawatch autonomous lane-fill [--limit N] [--json]` and the comm verb `autonomous lane-fill [limit]`. The preference is set with MCP `autonomous_prd_set_lane_preference`, the CLI `datawatch autonomous prd-set-lane-preference <id> gpu|claude|any` and the comm verb `autonomous set-lane-preference <prd-id> gpu|claude|any`. The PWA shows the status on the Automation sub-tab.

## Triage records (`GET /api/autonomous/triage`)

The engine keeps one **triage record** per Automaton that ends **failed** or **blocked**, and you read them from the daemon. The pass that fills them runs on every coordinator tick (~60 s); this section documents only the read. The operator reference — what the record contains, what the pass does, the alert key and what triage never does — is in [docs/operations.md](../operations.md) (section *Triage records*).

```
GET /api/autonomous/triage
    ?automation_id=<automation-id>
    ?prd_id=<automaton-id>
    ?status=failed | blocked

200: { "records": [ { ... } ], "alert_minutes": 30 }
```

The filter params are optional and can be combined. `status` accepts only `failed` or `blocked`; anything else is refused with the daemon's message. A read that matches nothing is `records: []`, not an error. The last field, `alert_minutes`, states the triage alert threshold currently in force (`autonomous.automation.triage_alert_minutes`, default 30, `0` = off) in the same body as the records it applies to.

A record (newest first in `records`):

```json
{
  "prd_id": "d0442737d38c",
  "prd_title": "nightly ingest",
  "automation_id": "4522f306c266",
  "automation_name": "nightly pipeline",
  "member_id": "member-2",

  "status": "failed",
  "task_id": "task-9",
  "task_title": "run integration tests",
  "error": "connection refused: testdb:5432 (truncated at 2000 chars)",
  "blocked_reason": "dependency gate: awaiting operator approval",
  "session_end_reason": "runtime error: exit 2 (truncated at 2000 chars)",

  "first_seen_at": "2026-10-27T09:12:44Z",
  "updated_at": "2026-10-27T09:44:02Z",
  "alerted_at": "2026-10-27T09:44:02Z",

  "retry_budget": {
    "infra":   { "limit": 3, "used": 1, "left": 2 },
    "content": { "limit": 4, "used": 4, "left": 0 },
    "scope":   { "limit": 2, "used": 0, "left": 2 },
    "policy":  { "limit": 1, "used": 0, "left": 1 },
    "ceiling_total": 10,
    "ceiling_left": 4
  },

  "notes": [
    { "at": "2026-10-27T09:12:44Z", "source": "triage", "text": "Automaton failed on task 'run integration tests'" }
  ]
}
```

The fields are copied from the live Automaton at the last pass, not derived: `prd_title` names the Automaton (absent when it has no title); `automation_name` and `member_id` place the record in its Automation; `task_id` and `task_title` name the task that failed (or was blocked), `error` is that task's error and `session_end_reason` why its live sessions were ended, each already truncated to 2000 chars by the pass; `blocked_reason` is the Automaton's own block reason, kept separate from the task error; `first_seen_at` is when the Automaton first ended in this failed or blocked state (its own status timestamp), `updated_at` when the pass last refreshed it, and `alerted_at` when the triage alert for this episode fired (absent until it does). `retry_budget` is the engine's existing retry budget, reused as-is and shown for all four categories — `limit` is the maximum, `used` what has been consumed, `left` what remains — plus `ceiling_total` and `ceiling_left`, the overall retry ceiling when one is set; it is **informational**: the pass neither spends nor resets any of it. `notes` is an append-only list (at, source, text) the pass grows over the life of the record. `alert_minutes` is not on the record: it rides the list body beside `records`.

The route is read-only and a sibling of the lane-fill read under the same access rules: bearer-authenticated locally, and it needs the autonomous read **capability** when called by a federated peer. Its surfaces are the REST route above, the MCP tool `autonomous_triage` (the same three filters, proxying the route), the CLI (`datawatch autonomous triage [--automation <id>] [--prd <id>] [--status failed|blocked] [--json]`), the comm verb (`autonomous triage [automation-id]`, one optional positional argument: the Automation id), the PWA triage card in the Automaton detail view, and the same contract documented for the mobile and wear surfaces in [mobile-surface.md](mobile-surface.md). The record card on each client displays, in order: the Automaton name (title, or id), the status chip (failed/blocked), the owning Automation name with its member id when one exists, the failed task title, the task error, the blocked reason, the session end reason, the first-seen time, whether the alert has fired and when, one row per retry-budget category (used of limit, left), and the overall retry ceiling when one is set; values the record does not carry are simply not shown. Triage has exactly one configuration key, `autonomous.automation.triage_alert_minutes`. The record shape is in `internal/server/web/openapi.yaml` (`TriageList`, `TriageRecord`).

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
  stuck_running_seconds:  900         # stuck-running watchdog: an Automaton running with every task finished, no live session and no live executor for this long gets its completion roll-up re-fired once, then is blocked after a second full bound; 0 = 900
  descendant_stall_seconds: 2700      # pi-prompt only: bound on a stalled worker whose only liveness is a live working descendant process; exceeded => infrastructure failure, descendants are cleaned up, task is retried via the auto_fix_retries budget; 0 or below => use the default 2700
  preserve_attempt_work:  true        # save a killed attempt's uncommitted work to refs/datawatch/attempts/; restored on the next attempt unless clean_restart
  auto_fix_retries:       1           # re-prompt count on verifier failure
  security_scan:          true        # nightwire-port .py scanner before commit
  verifier_policy:                    # which backend runs a verification (see "Verifier policy")
    mode:                 fixed       # fixed | prefer-free | balance | gpu-only | claude-only
    backends:             []          # ordered eligible backends; empty = verification_backends, then verification_backend
    claude_session_cap:   0           # Claude verifier sessions per window; 0 = unlimited
    claude_cap_window_minutes: 0      # window length; 0 = 60
    force_stronger_on_retry: false    # retried verification goes to the stronger verifier
    final_gate_stronger:  false       # final verification gate goes to the stronger verifier
    stronger_backend:     ""          # empty = claude-code
    stronger_model:       ""          # empty = the default Sonnet model
  judge_policy:           []          # ordered judge backends, one attempt each; empty = follow the verifier routing policy
  judge_timeout_seconds:  1800        # time limit for one judge run; the capacity-slot wait does not count
  judge_size_threshold_lines: 400     # changed lines above which claude-code is tried first; 0 = size rule off
```

### Compute-node engine cap (`max_engines_per_node`)

`autonomous.max_engines_per_node` is the slot count of a compute node that
hosts a local engine when the node has no operator `session_slots` and its
observer reports neither GPU data nor system memory with a model size. It is
the last step of the rule in [Node slots](#node-slots-session_slots). The
default is `1`, and a value below `1` is clamped to `1`. Only sessions on a
local engine on that node count (`pi`, `pi-prompt`, `ollama`, `opencode`,
`opencode-acp`); hosted backends such as `claude-code` are not gated by it.
Sessions past the count wait with the reason `waiting for node <name>`. The
operator guide, including queue priority, is in `docs/operations.md` →
"Compute-node queue".

### Node slots (`session_slots`)

Each compute node that hosts a local engine takes a slot count, the number of
workers it runs at once. Its `node:<name>` pool limit is that count. The count
is the first of these that applies:

1. `session_slots` on the node, when above `0`. Set by the operator; `0` (the
   default) means derive.
2. One slot per GPU, from the observer's GPU data, limited by the free GPU
   memory against the size of the served model. A GPU takes at most one engine.
3. Free system memory divided by the served model's size, at least one, when the
   observer reports no GPU data.
4. `autonomous.max_engines_per_node`.

`max_concurrent_sessions` on the node, when set and smaller, caps the count. A
derived count never falls below the workers already holding the node, but that
floor is capped at the GPU count. A negative `session_slots` is refused. A node
that hosts no local engine ignores `session_slots` and keeps its
`max_concurrent_sessions` limit. The count is re-read every 10 seconds.

- **REST:** `PUT /api/compute/nodes/{name}` with `session_slots`.
  `GET /api/compute/nodes/{name}/detail` returns `session_slots` (the operator's
  value, `0` = derive) and `derived_slots` (the count in effect). `derived_slots`
  is omitted until the node has a pool limit.
- **MCP:** `compute_node_add` and `compute_node_update` take `session_slots`.
  Update replaces the whole node, so omitting it clears the override.
  `compute_node_detail` returns `session_slots` and `derived_slots`.
- **CLI:** `datawatch compute node add|update <name> --session-slots N`;
  `datawatch compute node detail <name>`.
- **Comm channel:** `compute node update <name> session_slots=N`.

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

### Quality-gate disagreement policy

`autonomous.gate_disagreement` (`warn` by default, or `block`) decides what
happens when a task's own verifier passed but the quality gate confirmed a
regression from a valid baseline: `warn` records the disagreement on the task
and in the decision log and the task completes; `block` fails the task with a
`gate_disagreement: …` error. An invalid gate (unusable baseline) never
blocks under either value. Any other value is rejected. The operator guide is
`docs/operations.md` → "Quality gate".

- **REST:** `PUT /api/config` with `{"autonomous.gate_disagreement": "block"}`
  (`400` for any other value); `PUT /api/autonomous/config` with
  `{"gate_disagreement": "block"}`. Read back from `GET /api/config` and
  `GET /api/autonomous/config`. `GET /api/autonomous/prds/{id}` returns
  `quality_gate_status` / `quality_gate_invalid_reason` on the Automaton and
  `gate_disagreement` on a task.
- **MCP:** `autonomous_config_set` with `gate_disagreement`;
  `autonomous_config_get` returns it.
- **CLI:** `datawatch autonomous config-set '{"gate_disagreement":"block"}'`.
- **Comm:** `configure autonomous.gate_disagreement=block`.

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

## Capacity (`GET /api/capacity`) readable holder names

`GET /api/capacity` resolves raw holder ids (task ids, PRD ids) into
operator-readable names — "PRD title / task title (short id)" — and adds the
resolved form **beside** the raw id fields, never in place of them. Scripts
that read `holder`, `prd_id`, `task_id`, `pools` or `reason` see exactly what
they always saw; the name fields are pure additions and are simply absent
when the daemon has no name resolver wired (older daemons, or a build
without the autonomous manager).

| Where | New field(s) | Meaning, beside the unchanged id field |
|---|---|---|
| `pools[]` on `node:<name>` pools | `holder_names` (array) | the same holders as name objects, parallel to the untouched `holders` array and `holder` comma string. Always present on node pools — empty when idle. A name object can be omitted there only when the resolver can't resolve that one id. |
| `leases[]` | `holder_name` (object) | the resolved readable form of that lease's `holder`; `holder` untouched. Omitted when unresolvable. |
| `waiting[]` | `holder_name` (object) | the resolved readable form of the waiting `holder`; `holder`, `prd_id`, `pools`, `reason` untouched. |
| `tasks.waiting[]` | `readable`, `short_id`, `prd_title`, `task_title`, `readable_reason` | the task line in readable form beside `task_id` (omitted pair when the resolver gives no title); `prd_title`/`task_title` are the source titles; `readable_reason` is the same `reason` with holder ids replaced by names — empty for reasons that name no holders (back-off, budget waits). |
| `tasks.running[]` | `readable`, `short_id`, `prd_title`, `task_title` | the running task line beside `task_id`. |
| Session status `automaton` block | `wait_reason_readable` | the wait reason worded with names, beside the raw `wait_reason`. Same text, one source. |

Every name object (`holder_name`, each `holder_names[]` entry) has the same
shape:

| Field | Notes |
|---|---|
| `id` | the raw holder id (task or PRD id) — the same value the old fields carried |
| `kind` | `session` or `task` |
| `task_title`, `prd_title` | the source titles when known (omitted when not) |
| `prd_id` | the owning PRD id when known (omitted when not) |
| `short_id` | first 8 chars of the task or PRD id; what the UI shows as the secondary label |
| `readable` | the short form: each title capped and middle-truncated (…); falls back to the raw id when no title exists |
| `full` | the long form: the same sentence, titles never truncated. **Omitted by default**; add `?long=1` to the request and every name object carries it (the PWA tooltip reads it for long names) |

The short form is the default so cheap polls stay small; `?long=1` is purely
additive. When a resolved holder has a short id but no title, every surface
renders the fallback label **`(untitled <short id>)`** (localized via the
`capacity_name_untitled` PWA key) so a titleless task never shows as a bare
dot — the raw id fields still carry the real id in the body.

- **REST:** `GET /api/capacity` (bearer-authenticated). Query: `?prd_id=<id>`
  scopes the pools/leases/waiting/tasks lists to one Automaton (all four
  surfaces); `?long=1` includes the never-truncated `full` field in every
  name object.
- **MCP:** `capacity_status` — same body; the readable fields appear when
  the daemon resolves names.
- **CLI:** `datawatch capacity --json` — same body as the REST endpoint.
- **PWA:** the capacity card and task details render `readable` first with
  `short_id` as a smaller secondary label, the full text in a tooltip, and
  the `(untitled <short id>)` fallback above. The web tests asserting the
  rendering are in `internal/server/web/app-capacity-names.test.js`.

### Holder nodes

Every pool in `GET /api/capacity` carries `hold_nodes`, the node each holder's
lease sits on. It is a parallel array to `holders` (same length, same order):
the entry at index `i` is the node of the lease held by `holders[i]`, or `""`
when that lease names no node. The field is omitted when no holder of the pool
has a node, so a pool with only node-less leases serialises exactly as before.
`hold_nodes` is a pure addition; `holders`, `holder` and `holder_names` are
unchanged.

The same node is printed in the text views, which read the same snapshot. The
pool line gains ` on <node>` after each holder, so `llm:pi: 2/4 held by
task-a on gpu-a,task-b on gpu-b`. The node appears only on a pool whose holders
span two or more nodes; a one-node entry (every holder on one node) renders as
it did before.

- **REST:** `GET /api/capacity` and `GET /api/capacity?prd_id=<id>` both carry
  `hold_nodes` on each pool. The Automaton-scoped view keeps the same pool
  objects, so it carries the field too.
- **MCP:** `capacity_status` returns the same body, so it carries `hold_nodes`.
- **CLI:** `datawatch capacity` prints the node after each holder in the pool
  line; `datawatch capacity --json` carries `hold_nodes`.
- **Comm channel:** the `capacity` reply prints the node after each holder in
  the pool line, as the CLI does.
- **PWA:** the capacity card shows the node name as a small muted label beside
  each holder's short id, under the same one-node rule. The web tests asserting
  it are in `internal/server/web/app-capacity-holder-nodes.test.js`.

### Unknown node after a restart

When a daemon restart adopts a surviving worker of a local-engine LLM entry that
lists two or more compute nodes and the node cannot be determined (the task's
recorded node, the session's `compute_node_ref` and its endpoint matched against
a node `address` all come up empty), the lease holds **no** node and is flagged
`node_unknown: true`. No node is guessed from a host name, DNS or a partial name
match. The lease still counts against the `host` and `llm:<name>` pools, so those
caps are respected; it occupies no `node:<name>` pool. A later adoption that
learns the node clears the flag. A lease with no node that is not flagged (a
hosted backend) is unchanged, and the field is omitted when false.

- **REST / MCP / CLI:** each entry of `leases` in `GET /api/capacity` carries
  `node_unknown`, and each entry of `tasks.running` carries it for its task.
- **Text views:** the running task line says `node unknown`, and a
  `Leases with unknown node: N` line follows the pools.
- **Log:** one line at adoption names the task, session and endpoint that
  matched no node.
- **PWA:** the capacity card marks the holder `node unknown`.

### Rate ceilings

A pool can carry a rolling-window rate ceiling (requests and/or tokens per
window) on top of its concurrency limit. The fields below are pure additions;
a pool without a ceiling serialises exactly as before.

| Where | Field | Meaning |
|---|---|---|
| `pools[]` | `rate_requests`, `rate_tokens` | the ceilings (omitted when unset) |
| `pools[]` | `rate_window_seconds` | rolling window length |
| `pools[]` | `requests_in_window`, `tokens_in_window` | usage counted in the window now |
| `pools[]` | `rate_resets_at` | when the window next frees usage (omitted when nothing is counted) |
| `waiting[]` | `cause` | classified wait cause; `rate_ceiling` when a pool's rate ceiling blocks the waiter |
| `waiting[]` | `resets_at` | when the window will free the waiter (rate-ceiling waits only, when known) |
| `tasks.waiting[]`, `queue[]` in autonomous status | `cause`, `resets_at` | same `rate_ceiling` cause and reset time for a task waiting on the ceiling |

The wait `reason` reads `pool <p> rate ceiling: <requests|tokens> <used>/<ceiling> in <window>, resets at <RFC3339>`
(the reset part is left out when unknown). `readable_reason` words it as
`pool <p> rate ceiling: requests 3/3 used, resets in 5m`. The audit log records
the wait with the existing `autonomous_capacity_wait` action: `cause` is
`rate_ceiling`, and `pool` and `resets_at` appear in the details.

### Admission query

`GET /api/capacity/admission` answers "would a start of this backend be
admitted now?" without starting anything. It takes no lease, adds no waiter
and changes no state. It resolves the same `host`, `llm:<name>` and
`node:<name>` pools a real start uses.

| Query parameter | Meaning |
|---|---|
| `backend` (or `llm`) | backend or named LLM to start; required |
| `node`, `model` | compute node and model wanted |
| `role` | `worker` (default), `verifier`, `planner` or `interactive` |
| `priority` | integer queue priority, higher first (default 0) |
| `prd_id` | Automaton the start would belong to (fairness group) |

The response is a Decision:

```json
{"kind": "queued", "queue_position": 1, "reason": "pool llm:x rate ceiling: requests 3/3 in 1h0m0s, resets at 2026-10-23T11:00:00Z", "resets_at": "2026-10-23T11:00:00Z", "alternates": [{"pool": "llm:y", "node": "", "model": ""}]}
```

`kind` is `admit`, `queued` or `alternate_available`; `queue_position` is
1-based (0 when admitted); `resets_at` is present only when a rate ceiling is
the blocker and the time is known; `alternates` lists pools of the same kind
elsewhere with room. When the backend's LLM entry pools two or more known nodes
and no `node` is given, the node pools are not required, and `node` (present
only with `admit`) names the node the start would be placed on. A missing or
unknown backend, a non-integer priority or
an unknown role returns 400. The same query is the MCP tool
`capacity_admission` (read-only), `datawatch capacity admission --backend <b>`
and the chat command `capacity admission <backend> [node] [model]`.

---

## CLI

```
datawatch autonomous status
datawatch autonomous config-get
datawatch autonomous config-set '{"enabled":true,"max_concurrent_tasks":2}'   # merges; any deprecated_fields note goes to stderr
datawatch autonomous config-set '{"max_engines_per_node":2}'                  # local-model sessions per compute node (default 1)
datawatch autonomous config-set '{"gate_disagreement":"block"}'                 # fail a verifier-passed task on a confirmed gate regression (default warn)
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
datawatch autonomous tag-list [--include-sub]
datawatch autonomous tag-create "Mobile App" --parent <id> --color "#6366f1"
datawatch autonomous tag-rename <id> <new-name>
datawatch autonomous tag-move <id> --parent <new-parent-id|root>
datawatch autonomous tag-delete <id>
datawatch autonomous tag-registry                  # shared tag registry status: role, keeper, stale, pending sync, notes
datawatch autonomous tag-registry-dismiss <note-id>
datawatch autonomous prd-set-tags <prd-id> --tags <id1,id2>   # empty value clears
datawatch autonomous prd-create "Fix the retry loop" --kind bug   # optional; omit to derive the kind
datawatch autonomous prd-set-kind <id> <feature|bug|other>
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
| `autonomous_config_set`     | merge fields into config (incl. `max_concurrent_tasks`, `max_engines_per_node` and `gate_disagreement`) |
| `autonomous_prd_list`       | list all PRDs |
| `autonomous_prd_create`     | create draft PRD |
| `autonomous_prd_get`        | fetch one PRD with tree |
| `autonomous_prd_decompose`  | run LLM planning phase (creates stories+tasks) |
| `autonomous_prd_run`        | kick executor |
| `autonomous_prd_cancel`     | cancel + archive |
| `autonomous_learnings`      | list learnings |
| `autonomous_tag_list` / `_tag_create` / `_tag_rename` / `_tag_set_parent` / `_tag_set_color` / `_tag_delete` | Automata tag registry; see [howto/automata-tags](../howto/automata-tags.md) |
| `autonomous_prd_set_tags`   | replace an Automaton's tags (`tag_ids_json`) and/or set its primary tag (`primary_tag_id`; alone it keeps the tags) |
| `autonomous_tag_list` with `counts` | adds the subtree and direct counts (Automata and sessions) and the untagged totals |
| `autonomous_tag_registry_status` | the shared tag registry object of this daemon (read; the `registry` field of `GET /api/autonomous/tags`) |
| `autonomous_tag_registry_dismiss_note` | dismiss one merge or removed-on-keeper note by `id` (write; `POST /api/autonomous/tags/registry/notes/{id}/dismiss`) |
| `autonomous_attention_list` | the attention group: `{statuses, items}` (read) |
| `session_set_tags`          | replace a session's own tags (`id`, `tag_ids_json`); inherited tags are not set here (write) |
| `autonomous_prd_set_kind`   | set an Automaton's kind: `feature`, `bug` or `other` (empty derives it again); `autonomous_prd_create` takes the same optional `kind` |
| `autonomous_progress` / `autonomous_progress_trend` | read the progress metrics object (`automation_id`, `tag_id`, `since`, `until`) / the trend over the saved daily snapshots (`days`); read-only, proxied through the REST routes |
| `autonomous_prd_set_task_verification_llm` / `_set_story_verification_llm` / `_set_verification_llm` | per-task / per-story / per-Automaton verifier LLM |
| `autonomous_prd_set_verifier_policy` / `_set_task_verifier_policy` | per-Automaton / per-task verifier policy (`mode`, `backends`; `""` / `[]` clears, omit = unchanged) |
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
autonomous tags
autonomous tag-registry
autonomous tag-registry-dismiss 9f8e7d6c
autonomous tag-create "Mobile App" parent=1a2b3c4d
autonomous set-tags abcd1234 1a2b3c4d,5e6f7a8b
autonomous set-kind abcd1234 bug
```

`guardrail_backend=` / `guardrail_model=` tokens can be appended to
`autonomous set-llm` / `set-task-llm`; when they are the only settings
given, no backend positional is needed and the worker LLM is left
unchanged. `guardrail_model=` with no value clears the override.

### Automaton kind

An Automaton carries a kind of `feature`, `bug` or `other`. When none is set the
kind is derived from the title and the start of the spec. Set it when the Automaton
is created (`kind` in the create body, `--kind` on `prd-create`, `kind` on
`autonomous_prd_create`) or change it later with `set_kind` (`prd-set-kind`,
`autonomous_prd_set_kind`, `autonomous set-kind`). An unknown kind is refused with a
400 and the valid values. Each change is logged on the decision timeline.

### Progress metrics

`GET /api/autonomous/progress` returns the one metrics object every surface shows
(MCP `autonomous_progress` returns the same body). `GET /api/autonomous/progress/trend`
returns the saved daily snapshots as a trend (MCP `autonomous_progress_trend`).

```
GET /api/autonomous/progress?tag_id=t-ui&since=2026-10-10&until=2026-10-20
GET /api/autonomous/progress/trend?days=90
```

| Query | Route | Meaning |
|---|---|---|
| `automation_id` | progress | Count only the Automata of this Automation |
| `tag_id` | progress | Count only the Automata carrying this tag |
| `since` | progress | First day counted, `YYYY-MM-DD` (default: the configured `autonomous.progress.since`, else the first record's creation date) |
| `until` | progress | Last day counted, `YYYY-MM-DD`, inclusive (default: now) |
| `days` | trend | How many days back to read, 1 to 365 (default 30) |

The progress reply carries `groups` (feature, bug and other), `totals`, `throughput`,
`utilization`, `lead_time`, `inactive`, `remaining`, `completion`, `unfiled_rows`,
`backlog_available` and `not_measured`. The backlog is read from
`autonomous.progress.backlog_path`; a missing file gives `backlog_available: false`
and is not an error. Explicit inactive windows are read from
`operator-wait-windows.tsv` in the same folder as the backlog file. The trend reply is
`{"days": N, "points": [...], "direction": "converging|widening|flat|unknown"}`; with no
saved snapshots `points` is empty and `direction` is `unknown`. A bad date, `until`
before `since`, or `days` outside 1 to 365 is a 400. Both routes need the autonomous
read capability when called by a federated peer, and answer 503 when autonomous is off.

The per-node engine cap is a daemon setting, so it goes through `configure`
rather than an `autonomous` subcommand: `configure autonomous.max_engines_per_node=2`.

The reply is the JSON response from the daemon, pretty-printed when
short.

---

## Operator question channel

An Automaton (or its session, supervisor or another operator) raises a
decision to the operator, and the operator answers from whichever surface is
immediately to hand. There is **one generic question record**; the kind of
question is the `label`, not a different structure — the shipped labels are
`decision`, `design review`, `approval` and `information`, but users may add
their own. Questions are kept forever (no deadline, no TTL) and raise never
blocks the questioner.

**Off by default.** Every endpoint is gated on the switch
`autonomous.questions.enabled` (see [operations](../operations.md#operator-questions)).
While the switch is false, all six endpoints refuse with `503` and name the
switch: `operator questions are disabled: set autonomous.questions.enabled
to true`. The switch is read live on every request, so `PUT /api/config`
turns the channel on or off without a restart. If the daemon cannot open the
question store it answers `503 operator questions are not available`
instead of serving an empty channel.

### Endpoints

```
POST /api/questions               raise a question                     (autonomous:write)
GET  /api/questions               list, filters newest first           (autonomous:read)
GET  /api/questions/{id}         one question + answer metadata       (autonomous:read)
POST /api/questions/{id}/answer  record an answer                     (autonomous:write)
POST /api/questions/{id}/withdraw  withdraw an open question          (autonomous:write)
GET  /api/questions/{id}/answers  the answer log, oldest first        (autonomous:read)
```

Auth and capabilities follow the other `/api/autonomous` routes: the local
admin caller passes as always; a federation peer needs **`autonomous:read`**
for the three reads and **`autonomous:write`** for raise, answer and
withdraw (a missing capability is a `403`). Answering needs only the write
capability — an operator authorized for the Automaton may answer from
any communication channel, and there is no separate answering capability.

Common to every endpoint: `405` for a method other than the one shaped for
the path (the shape is learnable only through its real method), `503` while
the switch is off, and `404` both for an unknown question id and for any
path below one that is not one of the three named sub-paths (`/answer`,
`/withdraw`, `/answers`) — a fixed "not here", the same for every method,
so no client learns a false endpoint shape.

### Raise — `POST /api/questions`

```bash
curl -sX POST http://localhost:7433/api/questions \
  -H "Content-Type: application/json" \
  -d '{
    "label": "decision",
    "context": "task",
    "task_id": "a1b2c3d4",
    "text": "Which migration strategy for the users table?",
    "options": [
      {"id": "online",  "label": "online migration", "tradeoffs": "slow, zero downtime"},
      {"id": "offline", "label": "offline migration", "tradeoffs": "fast, 10 min downtime window"}
    ],
    "default": "hold",
    "recommendation": {"option_id": "online", "rationale": "the site is live 24/7"},
    "asked_by": "session:9f8e",
    "asker_kind": "session"
  }'
```

201 with `{"question": {…}}`. Body caps and rules:

| Field | Rule |
|---|---|
| `text` | required, ≤ 8192 bytes; whitespace-only is rejected |
| `label` | ≤ 64 characters, no control characters; empty defaults to `decision` |
| `options` | 2 to 20; unique non-empty `id` and `label` (label ≤ 200 bytes); `tradeoffs` ≤ 1000 bytes |
| `default` | required — either an option id of this question or the word `hold`, so no choice is ever silent |
| `recommendation` | optional; `option_id` must exist among the options; `rationale` ≤ 1000 bytes |
| `risk` | `"normal"` (default) or `"low"` |
| `context` | one of `none`, `prd`, `task`, `proposal`, `registry_row`; the matching link is required (`prd_id`, `task_id`, `proposal_path`, `registry_row_key`) and `none` must carry none of them. A proposal path, wherever given, must be repository-relative (no leading `/`, no `..` segment) |
| `asker_kind` | `session`, `supervisor`, `operator` or `system`; empty defaults to `operator` |
| `asked_by` | the asker of record; empty defaults to the caller (the audit actor) |

The full body is capped at 64 KiB (`413` when exceeded, refused before the
read runs to the end), and content after the JSON object is a `400` — a
double-encoded or duplicated body is malformed, not silently ignored.
Validation refusals are a `400` that names every offending field, and none of
it is stored.

### List — `GET /api/questions`

200 with `{"count": N, "questions": [ … ]}`, newest first (`questions` is
`[]`, never null, when nothing matches).

| Parameter | Values |
|---|---|
| `state` | `open`, `answered`, `withdrawn`, `expired` or `all` (default: all) |
| `context` | `none`, `prd`, `task`, `proposal`, `registry_row` or `all` (default: all) |
| `asker` | exact match on `asked_by` |
| `text` | case-insensitive substring over the question text, the option labels and tradeoffs, and the answer quotes |
| `limit` | positive integer; default 100, silently capped at 1000 |

An unknown `state` or `context` is a `400` naming the value; a `limit`
that is not a positive integer is a `400` (`limit must be a positive
integer`). Both are plain-text bodies — see the note under **Status codes**.

### One question — `GET /api/questions/{id}`

200 with the question record plus the metadata a caller needs to display
"answered, N times" in the same read:

```json
{
  "question": { "id": "q1", "state": "answered", "…": "…" },
  "answer_count": 3,
  "current_answer_id": "a3"
}
```

`answer_count` is every answer ever recorded — superseded ones included, they
are decided and never deleted — and `current_answer_id` is the id of the
answer in force (empty when there is none). The full answer log, with text,
is the answers read below. `404` for an unknown id.

### Answer — `POST /api/questions/{id}/answer`

```bash
curl -sX POST http://localhost:7433/api/questions/q1/answer \
  -H "Content-Type: application/json" \
  -d '{"chosen_option_id": "online", "quote": "online, and schedule it for the next low-traffic window", "actor": "operator", "surface": "cli"}'
```

201 with `{"answer": {…}, "question": {…}}` — the stored answer (including
`quote_sha256`) and the question's new state.

| Field | Rule |
|---|---|
| `chosen_option_id` | must name an option of the question; at least one of this or a non-blank `quote` is required |
| `quote` | the operator's own words, stored exactly as received — no trimming, normalising or re-encoding; ≤ 16384 bytes; must be valid UTF-8 (a byte string that decodes to nothing is a `400`, `answer quote is not valid UTF-8`, before anything is stored) |
| `supersedes` | the id of the answer being replaced (see "Changed minds" below) |
| `surface` | `pwa`, `android`, `ios`, `cli`, `rest`, `mcp` or `comm:<channel name>`; empty defaults to `rest`; anything else (including `system`) is a `400` naming the accepted set |
| `source_message_id` | the message that carried the answer, when it came over a comm channel |
| `actor` | the answering party of record; empty defaults to the caller (the audit actor) |

**Quotes and the SHA-256.** The store records the `quote_sha256`, the
lowercase hex SHA-256 of the exact bytes of the quote, so any later copy of a
stored quote can be checked against the record. The quote itself is never
derived, trimmed or altered.

**Secret refusal.** An answer whose text matches a secret pattern (private
keys, cloud key ids and secret keys, provider tokens, `sk-` keys, bearer
tokens, `password=/token=/…` assignments, …) is **refused with `422`, never
stored, and the response never echoes any part of the submitted text** —
`error` is the fixed phrase `matched: secret pattern` (*not* the name of
the specific rule that matched) and `suggestion` lists the two safe routes;
the matched rule's name goes to the audit entry instead:

```json
{
  "error": "Your answer was not stored because it looks like a secret (matched: secret pattern). Resend it without the secret. Two safe routes: store the secret in the datawatch secrets store and refer to it by name as ${secret:name}, or save it on the filesystem and point the session to the file.",
  "reason": "secret_detected",
  "suggestion": [
    "store it in the datawatch secrets store and refer to it as ${secret:name}",
    "save it on the filesystem and point the session to the file"
  ]
}
```

Well-formed `${secret:name}` references are references, not secrets, and pass
the scan. The refusal is itself audited (event `question_answer_refused`)
with the matched rule's name only — never with the answer's text, so a
rejected secret leaves no copy of it in the log.

### Withdraw — `POST /api/questions/{id}/withdraw`

```bash
curl -sX POST http://localhost:7433/api/questions/q1/withdraw \
  -H "Content-Type: application/json" \
  -d '{"reason": "superseded by the new plan", "actor": "operator"}'
```

200 with `{"question": {…}}` in its new state. `reason` and `actor` are both
optional; `actor` defaults to the caller. Withdrawing works only while the
question is still `open`; withdrawing one that has already moved on is a
`409` (the message names the state).

### Answer log — `GET /api/questions/{id}/answers`

200 with the full log, oldest first, each answer carrying its derived
superseded state plus the answer in force:

```json
{
  "answers": [
    {"id": "a1", "question_id": "q1", "quote": "offline is fine", "superseded": true, "superseded_by": "a2", "…": "…"},
    {"id": "a2", "question_id": "q1", "quote": "actually online", "chosen_option_id": "online", "superseded": false, "…": "…"}
  ],
  "current_answer_id": "a2"
}
```

`superseded` / `superseded_by` are derived on read from the log — the answer
lines themselves never change (see below). `404` for an unknown id.

### States, immutability and changed minds

A question moves `open` → `answered` or `open` → `withdrawn`; `expired` is
the fourth state, terminal like the other two. `open` and `answered` are the
only states that accept answers; a withdrawn question accepts none.

- **Questions and answers are immutable.** The record is append-only (JSON
  lines in `<data_dir>/questions.jsonl`); superseded answers are derived on
  read, and nothing is ever edited or deleted. There is no TTL and no
  expiry job — a question stays until it is answered or withdrawn.
- **A changed mind is a new answer, not an edit.** An open question accepts
  only an answer with no `supersedes` (carrying one is a `409` — the open
  question has no current answer to replace, so the target is wrong), and
  an answered question accepts only an answer whose `supersedes` names its
  current answer — a target that is absent or different is a `409` naming
  the answer ids. The new
  answer becomes current; the old one stays in the log, marked superseded.
- **No choice is silent.** A raised question must name a `default` — an
  option or `hold` — so automation that walks past an unanswered question
  has one declared default to act on.
- **Raise never blocks.** Raising records the question and returns; the
  asking side keeps running until it is answered (or the declared default is
  honored).

### Status codes

| Code | When |
|---|---|
| 200 | list, by-id get, withdraw, answer log |
| 201 | raise (`{question}`); answer (`{answer, question}`) |
| 400 | malformed body (bad JSON, content after the object); a validation rule failed (every offending field named); an unknown `surface` on an answer; an answer that carries neither option nor non-blank quote; an option id the question does not have; a quote that is not valid UTF-8 (`answer quote is not valid UTF-8`); an unknown `state` / `context` filter (the value named); a `limit` that is not a positive integer (`limit must be a positive integer`) |
| 403 | a federation peer without the capability the action needs |
| 404 | an unknown question id; any sub-path of a question that is not /answer, /withdraw or /answers |
| 405 | a method other than the one shaped for the path |
| 409 | answering a withdrawn or expired question; an answer on an open question that names a supersedes (no current answer to replace); an answer on an answered question whose supersedes is absent or wrong (the answer ids named); withdrawing a question that is not open |
| 413 | a raise or withdraw body over 64 KiB; an answer body over 64 KiB plus the quote cap; a quote over 16384 bytes |
| 422 | an answer looks like a secret (see "Secret refusal" above) — never stored, never echoed; the body carries the fixed phrase `matched: secret pattern`, not the rule name, plus the `suggestion` routes |
| 500 | the store could not append or replay an otherwise-valid write |
| 503 | the `autonomous.questions.enabled` switch is off (named in the message), or the store is not wired |

**Error body shape — one deliberate split, plan for it.** Raise and list
failures come back **plain text** — the message only, no JSON wrapper (e.g.
`limit must be a positive integer`, `options must have 2 to 20 items`).
Answer, withdraw and the answer-log failures come back as a **JSON object
with one `error` field** (e.g. `answer carries neither an option nor
non-blank text`, `answer must name the answer it supersedes`, `answer
supersedes the wrong target`, `question is not open`). A secret refusal uses that same object with `reason`
(`secret_detected`) and `suggestion` added. `403`, `404`, `405` and `503`
are plain text on every endpoint. Parse `Content-Type`, not the path.

### Audit

Each action writes an audit entry with the question's ids, the actor of
record and the fields needed to reconstruct the decision:
`question_raised`, `question_answered` (the quote **kept verbatim**, exactly
as that operator wrote it), `question_withdrawn`, `question_expired`,
`answer_superseded` when a supersede lands (the replacing answer's quote
kept), and `question_answer_refused` for a secret refusal — the matched
rule's name only, never the text.

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
