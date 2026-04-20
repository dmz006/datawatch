# Autonomous PRD decomposition (v3.10.0 — BL24 + BL25)

**Shipped in v3.10.0.** LLM-driven Product Requirements Document →
Stories → Tasks decomposition with independent verification. Each
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
DELETE /api/autonomous/prds/{id}           cancel + archive
POST   /api/autonomous/prds/{id}/decompose run LLM decomposition (creates stories+tasks)
POST   /api/autonomous/prds/{id}/run       kick the executor for this PRD

GET    /api/autonomous/learnings           extracted post-task learnings
```

When `autonomous.enabled` is false, every endpoint returns
`503 autonomous disabled`.

---

## Configuration

Per the no-hard-coded-config rule, every knob is reachable from
every channel:

```yaml
autonomous:
  enabled:                false       # off by default; opt-in
  poll_interval_seconds:  30          # background loop tick
  max_parallel_tasks:     3           # in-flight worker cap (per-PRD)
  decomposition_backend:  ""          # empty = inherit session.llm_backend
  verification_backend:   ""          # empty = inherit; set differently for cross-backend independence
  decomposition_effort:   "thorough"  # BL41 effort hint
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
datawatch autonomous prd-decompose <id>
datawatch autonomous prd-run <id>
datawatch autonomous prd-cancel <id>
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
| `autonomous_prd_decompose`  | run LLM decomposition |
| `autonomous_prd_run`        | kick executor |
| `autonomous_prd_cancel`     | cancel + archive |
| `autonomous_learnings`      | list learnings |

Typical AI workflow:
1. `autonomous_prd_create(spec="…", project_dir="…")` → `{id}`
2. `autonomous_prd_decompose(id)` → populates stories + tasks
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
rest POST /api/autonomous/prds/abcd1234/decompose
rest POST /api/autonomous/prds/abcd1234/run
```

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

**In:** data models, JSONL store, decomposition prompt + parser,
security scanner, manager + executor with auto-fix-retry verification
loop, REST endpoints, MCP tools, CLI commands, comm `rest`
passthrough parity, full config parity, operator + AI-ready docs.

**Deferred (v3.10.x patches or BL117):** web UI Gantt board,
cross-cluster orchestration, plugin/external-tool integration, richer
auto-pause / auto-resume across daemon restarts. Long-form PRD-DAG
orchestration with guardrail sub-agents is BL117 (Sprint S8 / v3.12.0+).
