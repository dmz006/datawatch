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
```

When `autonomous.enabled` is false, every endpoint returns
`503 autonomous disabled`.

### Guardrail judge LLM override (`set_llm` / `set_task_llm`)

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
`guardrail_backend` → global `orchestrator.guardrail_backend`.

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

`verification_script`, `files_generated` and `files_touched` are
system-populated and read-only — they appear in
`GET /api/autonomous/prds/{id}` (and the MCP / CLI / comm `get`
surfaces, which pass the same JSON through) but have no setter.

---

## Configuration

Per the no-hard-coded-config rule, every knob is reachable from
every channel:

```yaml
autonomous:
  enabled:                false       # off by default; opt-in
  poll_interval_seconds:  30          # background loop tick
  max_parallel_tasks:     3           # in-flight worker cap (per-PRD)
  planning_backend:       ""          # empty = inherit session.llm_backend (BL304; old key decomposition_backend still accepted)
  verification_backend:   ""          # empty = inherit; set differently for cross-backend independence
  planning_effort:        "thorough"  # BL41 effort hint (BL304; old key decomposition_effort still accepted)
  verification_effort:    "normal"
  stale_task_seconds:     0           # 0 = inherit session.stale_timeout_seconds
  auto_fix_retries:       1           # re-prompt count on verifier failure
  security_scan:          true        # nightwire-port .py scanner before commit
```

---

## CLI

```
datawatch autonomous status
datawatch autonomous config-get
datawatch autonomous config-set '{"enabled":true,"max_parallel_tasks":4}'
datawatch autonomous prd-list
datawatch autonomous prd-create "Add OAuth login with Google + GitHub"
datawatch autonomous prd-get <id>
datawatch autonomous prd-plan <id>        # canonical (prd-decompose is a back-compat alias)
datawatch autonomous prd-run <id>
datawatch autonomous prd-cancel <id>
datawatch autonomous prd-set-llm <id> --guardrail-backend ollama --guardrail-model qwen3.8:27b
datawatch autonomous prd-set-task-llm <id> --task <task-id> --guardrail-backend ""   # clear
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
| `autonomous_config_set`     | replace config |
| `autonomous_prd_list`       | list all PRDs |
| `autonomous_prd_create`     | create draft PRD |
| `autonomous_prd_get`        | fetch one PRD with tree |
| `autonomous_prd_decompose`  | run LLM planning phase (creates stories+tasks) |
| `autonomous_prd_run`        | kick executor |
| `autonomous_prd_cancel`     | cancel + archive |
| `autonomous_learnings`      | list learnings |

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
```

`guardrail_backend=` / `guardrail_model=` tokens can be appended to
`autonomous set-llm` / `set-task-llm`; when they are the only settings
given, no backend positional is needed and the worker LLM is left
unchanged. `guardrail_model=` with no value clears the override.

The reply is the JSON response from the daemon, pretty-printed when
short.

---

## Storage

Each PRD's full state is persisted as JSON-lines under
`<data_dir>/autonomous/`:

```
prds.jsonl       — PRDs with embedded story + task trees
learnings.jsonl  — extracted post-task learnings
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
