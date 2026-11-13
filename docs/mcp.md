# MCP Server

datawatch exposes a [Model Context Protocol](https://modelcontextprotocol.io) (MCP)
server, enabling Cursor, Claude Desktop, VS Code, and remote AI agents to manage AI
coding sessions directly — without leaving the IDE or chat app.

---

## Daemon Mode Compatibility

Since v0.2.0, `datawatch start` daemonizes by default.

- **Stdio transport** (`datawatch mcp`): unaffected — this is a separate foreground command invoked directly by the IDE. The daemon mode of `datawatch start` does not affect it.
- **SSE transport**: runs as a goroutine inside the daemon process. It starts and stops with the daemon regardless of whether the daemon is foreground or daemonized.

To run MCP SSE on a daemonized server, set `mcp.sse_enabled: true` in config and run `datawatch start` as usual.

---

## Overview

MCP is an open protocol for connecting AI models to tools and data sources. When you
configure datawatch as an MCP server, your IDE's AI assistant can:

- List active sessions on any connected machine
- Start new sessions for coding tasks
- Read live session output
- Send input to sessions waiting for a reply
- Terminate sessions

Two transport modes are supported:

| Mode | When to use |
|---|---|
| **stdio** | Local IDE clients (Cursor, Claude Desktop, VS Code) — no port required |
| **HTTP/SSE** | Remote AI agents, autonomous workflows, cross-machine access |

---

## Local Setup (stdio)

The stdio transport runs datawatch as a subprocess. Your IDE starts it on demand and
communicates over stdin/stdout. No port, no network, no TLS needed.

### Cursor

Add to `~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "datawatch": {
      "command": "datawatch",
      "args": ["mcp"]
    }
  }
}
```

Or via **Cursor → Settings → MCP → Add Server**.

### Claude Desktop

Add to `~/.config/claude/claude_desktop_config.json` (Linux/macOS):

```json
{
  "mcpServers": {
    "datawatch": {
      "command": "datawatch",
      "args": ["mcp"]
    }
  }
}
```

On macOS, the config file is at `~/Library/Application Support/Claude/claude_desktop_config.json`.

### VS Code (Copilot / Continue)

For Continue extension, add to `.continue/config.json`:

```json
{
  "mcpServers": [
    {
      "name": "datawatch",
      "command": "datawatch",
      "args": ["mcp"]
    }
  ]
}
```

For GitHub Copilot workspace MCP (if supported), follow the workspace MCP config format
for your version of VS Code.

Restart Cursor, Claude Desktop, or VS Code after saving the config.

### Remote server via SSH

If datawatch runs on a remote machine and you don't want to expose any ports, use SSH
stdio forwarding:

```json
{
  "mcpServers": {
    "datawatch-remote": {
      "command": "ssh",
      "args": ["myserver", "datawatch", "mcp"]
    }
  }
}
```

The MCP protocol runs over the SSH connection — no firewall rules or port forwarding needed.

---

## Remote Setup (HTTP/SSE)

The SSE transport starts an HTTP server that remote AI agents connect to. This enables:

- Remote AI agents (running in cloud functions, other machines, etc.) to manage sessions
- Multi-machine datawatch orchestration
- Programmatic access from scripts and CI systems

### Enable in config

```yaml
mcp:
  enabled: true
  sse_enabled: true
  sse_host: "0.0.0.0"     # bind address; default: 0.0.0.0
  sse_port: 8081           # port; default: 8081
  token: "your-secret"    # bearer token — required for remote connections
  tls_enabled: true
  tls_auto_generate: true  # auto-generate self-signed cert in ~/.datawatch/tls/mcp/
```

Start datawatch normally — the SSE server starts alongside all other backends:

```bash
datawatch start
```

Or run standalone:

```bash
datawatch mcp --sse
```

### Remote AI client config

For OpenAI Assistants, Claude API tool use, or any MCP-compatible remote agent:

```json
{
  "mcpServers": {
    "datawatch": {
      "url": "https://your-server:8081/sse",
      "headers": {
        "Authorization": "Bearer your-secret"
      }
    }
  }
}
```

### TLS

When `tls_enabled: true`:

- TLS 1.3 is enforced (TLS 1.2 and below are rejected)
- Post-quantum hybrid key exchange (X25519Kyber768) is negotiated automatically by
  Go 1.23+ when the client supports it — no extra config needed
- `tls_auto_generate: true` (default) generates a self-signed certificate at
  `~/.datawatch/tls/mcp/cert.pem` (valid 10 years, persisted across restarts)
- To use a CA-signed certificate, set `tls_cert` and `tls_key`:

```yaml
mcp:
  tls_enabled: true
  tls_auto_generate: false
  tls_cert: /etc/ssl/certs/datawatch.crt
  tls_key: /etc/ssl/private/datawatch.key
```

### Trusting self-signed certificates

When using `tls_auto_generate: true`, MCP clients need to trust the self-signed cert:

**Download the certificate:**
- Web UI: Settings > Comms > Web Server > Download CA Certificate
- API: `GET /api/cert?format=der` (.crt) or `GET /api/cert` (.pem)
- File: `~/.datawatch/tls/mcp/cert.pem`

**For Cursor / VS Code MCP clients:**
- Most MCP clients over SSE use HTTPS. Set `NODE_TLS_REJECT_UNAUTHORIZED=0` in the
  client environment, or install the CA cert system-wide.

**For Claude Desktop:**
- Claude Desktop uses stdio transport (not SSE), so TLS is not needed for local use.
- For remote SSE access, configure the cert in the OS certificate store.

**System-wide cert install:**
```bash
# Linux (Debian/Ubuntu)
sudo cp ~/.datawatch/tls/mcp/cert.pem /usr/local/share/ca-certificates/datawatch.crt
sudo update-ca-certificates

# macOS
sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain ~/.datawatch/tls/mcp/cert.pem
```

**For mobile PWA (Android/iPhone):**
See the operations guide for device-specific cert install instructions.

---

## Available Tools

The MCP server exposes **150+ tools** across the surfaces below
(v8.10.16 count — includes BL356–BL362 additions). The authoritative live list is served at
`GET /api/mcp/docs` (HTML) or `GET /api/mcp/docs?format=json` (JSON)
— `claude mcp list` queries this on connect, and the PWA Settings →
About → "MCP tools" link opens it. The selected tools below are
documented inline; the rest follow the same parameter-table-and-example
shape.

**Failure responses** (v9.5.29, BL463): a failed tool call returns an
`isError:true` result named after the tool (e.g.
`template_delete: HTTP 404: template not found`) — tools mirroring a REST
route also carry the upstream HTTP status and the daemon's error body
(truncated where long), holding MCP to REST's reliability bar. In stdio mode
`datawatch mcp` logs to **stderr only**; stdout is the JSON-RPC channel and
must stay strict JSON-RPC (release-smoke §7r asserts all of this).

### Tool families (high-level)

| Family | Tools |
|--------|-------|
| Sessions | `list_sessions`, `start_session`, `send_input`, `copy_response`, `kill_session`, `delete_session`, `restart_session`, `rename_session`, `session_output`, `session_timeline`, `session_bind_agent`, `session_import`, `session_reconcile`, `session_rollback`, `sessions_stale`, `stop_all_sessions`, `session_children`, `reply_to_parent`, `get_my_session_id`, `get_my_scope` |
| Platform context | `platform_context_status` |
| Vision | `vision_describe` |
| Web Search | `websearch_providers_list/get/add/update/delete/enable/disable/test`, `websearch_stats`, `websearch_history`, `web_search_stats` (deprecated alias) |
| Exit Hooks | `exit_hook_list`, `exit_hook_add`, `exit_hook_delete`, `exit_hook_enable`, `exit_hook_disable` |
| Work Queue | `queue_push`, `queue_claim`, `queue_complete`, `queue_fail`, `queue_list` |
| Discussion Subscribe | `discussion_subscribe`, `discussion_unsubscribe`, `discussion_subscriptions` |
| Result Store | `result_put`, `result_get`, `result_list`, `result_delete` |
| Scheduled Job Results | `post_result`, `schedule_results_list` |
| Automata | `autonomous_status`, `autonomous_config_get/set`, `autonomous_prd_list/create/get/decompose/approve/resume/reject/request_revision/edit_task/instantiate/run/cancel/rejudge/set_kind/set_llm/set_task_llm/set_task_verification_llm/set_story_verification_llm/set_verification_llm/children`, `autonomous_prd_plan_coverage_get`, `autonomous_learnings`, `autonomous_progress`, `autonomous_progress_trend`, `autonomous_lane_fill_status`, `autonomous_infra_retries`, `autonomous_defect_intake`, `autonomous_defect_batches`, `autonomous_defect_batch_split`, `autonomous_defect_tinyfix_check`, `onboarding_report`, `autonomous_triage`, `checks_status`, `ship_status`, `rules_context_status`, `rules_context_set`, `autonomous_uncommitted_work`, `autonomous_unlanded_work`, `autonomous_protected_paths`, `autonomous_standard_tasks`, `planning_routing_get/set/test`, `planning_scorecard`, `autonomous_request_watch`, `autonomous_timeline`, `autonomous_pending_items`, `autonomous_pending_item_add`, `autonomous_pending_item_attach`, `autonomous_pending_items_refresh`, `security_failure_list`, `dependency_update_status`, `supervisor_watch_list`, `supervisor_watch_stats`, `supervisor_loop_jobs`, `supervisor_loop_runs`, `supervisor_loop_run_job`, `supervisor_loop_eval`, `supervisor_loop_stats`, `autonomous_decisions`, `autonomous_decision_record_list/get/create/update/withdraw`, `autonomous_merge`, `autonomous_merge_shadow_status`, `autonomous_safe_to_delete`, `automation_merged_members`, `autonomous_prd_set_lane_preference`, `autonomous_prd_set_authorized_paths`, `capacity_status`, `autonomous_shipped_record`, `autonomous_shipped_backfill`, `autonomous_shipped_counts` |
| Usage back-off | `usage_backoff_status`, `usage_backoff_set`, `usage_backoff_clear`, `usage_backoff_recheck` |
| Tags and placement | `autonomous_tag_list/create/rename/set_parent/set_color/delete`, `autonomous_prd_set_tags`, `autonomous_tag_sweep`, `session_set_tags`, `session_tag_list` |
| Automation requirements and model pull | `automation_requirements_report`, `automation_model_pull_consent_get`, `automation_model_pull_consent_set`, `compute_node_backend_versions` |
| Usage calculator | `usage_advice`, `usage_readings`, `usage_reading_add`, `usage_reading_delete`, `usage_extra_credit_set`, `usage_remaining_work_set`, `usage_autolanes_status` |
| Burn guard | `burn_guard_status`, `burn_guard_run`, `burn_guard_config_get`, `burn_guard_config_set` |
| Lifecycle subscriptions (BL427) | `autonomous_subscribe`, `autonomous_unsubscribe`, `autonomous_subscriptions`, `autonomous_subscription_update`, `autonomous_subscription_test`, `autonomous_events_pending` |
| Search before create | `autonomous_overlap_search` |
| Orchestrator | `orchestrator_graph_create/plan/run/get/list/cancel`, `orchestrator_verdicts`, `orchestrator_config_get/set` |
| Pipelines | `pipeline_start/list/status/cancel` |
| Memory + KG | `memory_remember/recall/list/forget/export/import/reindex/stats/learnings`, `kg_add/query/timeline/stats/invalidate`, `research_sessions`, `get_prompt`, `copy_response` |
| Observer | `observer_stats`, `observer_envelopes`, `observer_envelopes_all_peers` (BL180 cross-host), `observer_envelope`, `observer_peers_list/get/register/delete`, `observer_peer_stats`, `observer_agent_list/stats`, `observer_config_get/set`, `ollama_stats` |
| Agents | `agent_list/get/spawn/terminate/logs/audit` |
| Plugins | `plugins_list/reload`, `plugin_get/test/enable/disable`, `plugin_service_status`, `plugin_widget_list`, `plugin_widget_get`, `plugin_token_list` |
| Plugin service lifecycle + rotation (MCP-only) | `plugin_service_start`, `plugin_service_stop`, `plugin_service_restart`, `plugin_token_rotate` |
| Profiles + projects | `profile_list/get/create/update/delete/smoke`, `project_list/summary/upsert`, `project_alias_delete`, `project_set_arcs`, `project_rollup`, `project_history`, `project_policy_effective` |
| Templates / scheduling / cooldown | `template_list/upsert/delete`, `schedule_list/add/cancel`, `cooldown_status/set/clear` |
| Devices + routing | `device_alias_list/upsert/delete`, `routing_rules_list/test` |
| Compute nodes | `compute_node_add`, `compute_node_update`, `compute_node_detail`, `compute_node_list`, `compute_node_get`, `compute_node_health`, `compute_node_delete`, `compute_node_attach_observer`, `compute_node_detach_observer`, `compute_node_pull_model`, `compute_node_remove_model`, `compute_node_headroom_measure`, `compute_node_headroom_get` |
| Channel bridge | `channel_info`, `channel_diagnostics` |
| Cost + audit + config + alerts | `cost_summary/usage/rates/report`, `analytics`, `audit_query`, `audit_access_query`, `self_config_audit`, `get_config`, `config_set`, `get_stats`, `worker_profile_status`, `autonomous_prd_set_worker_profile`, `get_version`, `diagnose`, `reload`, `restart_daemon`, `splash_info`, `get_alerts`, `create_alert`, `mark_alert_read` |
| Identity | `get_identity`, `set_identity`, `update_identity`, `configure_identity`, `identity_backups`, `restore_identity`, `reset_identity` |
| Saved commands | `list_saved_commands`, `send_saved_command` |
| Ask / assist | `ask`, `assist` |
| Voice | (no MCP tools — REST `/api/voice/transcribe` + chat-channel auto-handle) |
| Automata events (the Events tab) | (no MCP tools — REST `GET /api/autonomous/event-log`, CLI `datawatch autonomous event-log`, comm `autonomous event-log`) |

- **`autonomous_standard_tasks`** — lists the built-in standard tasks the decomposer adds to each story, with whether each is on for the project and its `mode`. Parameters: `project` (optional string; a project directory or alias, selects the per-project override; empty reports the global `enabled`). Returns the same object as `GET /api/autonomous/standard-tasks`: `{enabled, tasks[]}`, where each row has `id`, `title`, `kind`, `summary`, `offered_when`, `enabled`, `project_override`, `mode` (`llm`, `daemon` or `off`, from `autonomous.task_templates.mode`) and `runner` (`session`, `script gate`, `daemon` or `none`). Example: `autonomous_standard_tasks {"project": "/home/<user>/app"}`. The comm channel has the same list as `standard tasks` (or `autonomous standard-tasks`): one line per task, `id | title | on|off | mode | runner`, using the global setting.

Compute node tools:

- **`compute_node_add` / `compute_node_update`** — create or replace a compute node. Alongside the executor's `session_slots` (0 = derive from observer data) and `max_concurrent_sessions` they take the three verifier keys:

  | Parameter | Meaning |
  |---|---|
  | `verifier_slots` | worker slots for verifiers on this node (unset = 1 when the node hosts a local engine, else 0; an explicit `0` = no verifier slots). Negative values are refused. `update` replaces the whole node, so omitting the key clears the override back to unset |
  | `verifier_min_free_mem_mb` | free GPU memory in MB the node must still have before a new verifier slot starts (unset = 1024; `0` = check off). Negative values are refused |
  | `verifier_context_mb` | context MB one verifier slot holds, used only for the advice figure (unset/`0` = unknown, no figure) |

  Verifiers take a slot in the node's own verifier pool rather than the executor's, so verification can run while every executor slot is taken; the pool never raises the executor's cap.
- **`compute_node_detail`** — node detail. The result carries the verifier settings beside the executor pair: `verifier_slots` (null when unset), `verifier_min_free_mem_mb` and its effective value (1024 MB when unset), `verifier_context_mb`, `verifier_slots_effective`, `suggested_verifier_slots` (null when there is no usable figure), plus `headroom_ok`, `headroom_reason` and `free_gpu_mb` from the daemon's last-seen verifier admission state (absent until the node has been synced). Read-only; makes no model call and changes no setting.
- **`compute_node_headroom_measure`** — measure one compute node's headroom now and store it. Free memory plus the per-request cost of one small request against the smallest pull-allowed model the node serves. Runs only on demand and only when the node holds no lease: a busy node returns an error naming it (409). A node whose resident or probe model is not on the model-pull consent list is refused (403) and nothing is sent to it. Returns the measurement with `age_seconds`, `fresh` and `supported_cap`. Proxies `POST /api/compute/nodes/{name}/headroom`; needs the compute write capability when called by a federated peer.
- **`compute_node_headroom_get`** — read the stored headroom measurement of one compute node: `free_mb`, `per_request_mb`, `resident_model`, `measured_at`, `age_seconds`, `fresh`, `min_free_mb`, `max_age_hours` and `supported_cap` (the verifier cap it covers: 1 when stale, 0 when unbounded). Read-only; nothing is measured. An error (404) when the node has never been measured. Proxies `GET /api/compute/nodes/{name}/headroom`; needs the compute read capability when called by a federated peer.

Autonomous PRD reads and writes:

- **`autonomous_prd_get`** — takes an optional `detail` argument that sets how much comes back. The default is `summary`. Any other value is rejected with an error.
  - `summary` returns the Automaton's ids, titles, statuses and counts, and the same for its stories and tasks.
  - `tasks` returns everything in `summary`, plus each task's status, error and verification summary.
  - `full` returns everything, including task specs and logs, and the `foreign_file_drift` field on tasks.
- **`autonomous_prd_plan_coverage_get`** — read one Automaton's stored plan-requirements coverage report (the plan-coverage gate record): the report for that exact decomposed plan — matched and unmatched numbered requirements, any structural findings, and the overall verdict — plus the operator's acknowledgement, and the counts of findings the plan-coverage filters suppressed from the report (`suppressed.created_files`, `suppressed.requirements`). Read-only: it changes nothing and runs no gate, judge or scan. Takes `id` (required).
- **Behaviour change for PRD write tools.** `autonomous_prd_reset_task`, `autonomous_prd_run`, `autonomous_prd_approve`, `autonomous_prd_cancel`, and the set, add and edit tools no longer return the whole Automaton. Each write reply is now a small object with the Automaton's `id`, its `status` and `rev`, the id and status of the task or story that changed, and a `message`. To read the full Automaton after a write, call `autonomous_prd_get` with `detail` set to `full`.

Autonomous events (the Events tab of the Automata view) — no MCP tools:

- **Events stream** — the one newest-first stream of what the daemon watches and does for you (supervisor watch, lane fill decisions, infrastructure retries, Automata checks, cleanup, the repo CI watch and the recorded errors). The kinds are `watch`, `decision`, `retry`, `check`, `cleanup`, `alert` and `error`. It has no MCP tools and is read-only over REST, the CLI and the comm channel:

  | Parameter | Meaning |
  |---|---|
  | `kind` | Optional comma list of event kinds: `watch`, `decision`, `retry`, `check`, `cleanup`, `alert`, `error`; an unknown kind is a 400 |
  | `project` | Optional exact project match |
  | `q` | Optional case-insensitive text over the summary, detail, project, Automaton and Automation names |
  | `limit` | Page size, 1–200, default 50; anything else is a 400 |
  | `before` | Optional opaque cursor — the `next_cursor` from a previous page's body; returns older events; a bad cursor is a 400 |

  The body is `{"events": [...], "next_cursor": "...", "total": n}`; `next_cursor` is empty on the last page, and `total` counts every event that matches the filters across all pages. Example: `GET /api/autonomous/event-log?kind=watch,retry&limit=10` — the ten newest `watch` and `retry` rows. CLI: `datawatch autonomous event-log --kind watch --kind retry --limit 10`; comm: `autonomous event-log kind=watch,retry limit=10` (its limit is 1–10, default 10). What a row means, what an error entry and a quiet entry are, and the next action for a common error: [howto/automata-events.md](howto/automata-events.md).

Tools added in v5.9 → v5.26 (catch-up since the last doc sweep):

- **`autonomous_prd_resume`** — resume a PRD the boot-resume gate parked after a daemon restart (also resumes a paused PRD). Takes `id` and an optional `note`; errors if the PRD is in neither state.
- **`autonomous_prd_children`** (v5.9.0, BL191 Q4) — list child PRDs spawned from a parent's `Task.SpawnPRD` shortcuts.
- **`autonomous_prd_edit_task`** (v5.9.0+) — edit a task's `spec` while a PRD is in `needs_review`, `revisions_asked`, `cancelled`, or `blocked` (per-task `backend` / `effort` / `model` go through `autonomous_prd_set_task_llm`). On a `blocked` PRD only a `failed`, `blocked` or `review_needed` (held) task can be edited — editing never changes its status; retry a held task with `autonomous_prd_resolve_task_review`, which also accepts `read_only` and `report_path` — and a `completed` task's spec is immutable in every status. Recovery flow for a stuck task: `autonomous_prd_edit_task` to fix the spec, then `autonomous_prd_reset_task` to retry it — completed sibling tasks are left untouched, no re-decompose needed.
- **`autonomous_prd_edit_story`** — rewrite a story's `title`/`description` while a PRD is in `needs_review` or `revisions_asked`.
- **`autonomous_prd_add_story`** / **`autonomous_prd_remove_story`** — add a new, empty story, or remove a story (and its tasks), without re-running decompose. Accepted in `needs_review`, `revisions_asked` and `cancelled`. An Automaton whose completion review blocked with no retry left (or whose fix task could not be created) is parked in `needs_review` for exactly this purpose: add the work, then run it again.
- **`autonomous_prd_add_task`** / **`autonomous_prd_remove_task`** — add a new task to an existing story, or remove one, without re-running decompose. Accepted in `needs_review`, `revisions_asked` and `cancelled`. An Automaton whose completion review blocked with no retry left (or whose fix task could not be created) is parked in `needs_review` for exactly this purpose: add the work, then run it again. `remove_task` is distinct from `autonomous_prd_cancel_task`, which stops a running/queued task without deleting it from the PRD.
- **Truncation-marker refusal (spec and description writes)** — `autonomous_prd_create`, `autonomous_prd_edit`, `autonomous_prd_edit_task`, `autonomous_prd_edit_story`, `autonomous_prd_add_story`, `autonomous_prd_add_task`, `autonomous_prd_resolve_task_review` (retry with a replaced `spec`), and the PRD template create/update tools all refuse text that carries the capped-view truncation marker — a copy pasted from a capped view, **even one edited after the cut** — with the error `<field>: contains a truncation marker — it was copied from a capped view; read the full text and edit that`. This is intentional: a capped view ends in `…[truncated: kept N of M bytes]`, so a spec or description saved from one silently drops its tail. The read never caps the field you would edit — the Automaton `spec`, every story `description` and every task `spec` are always whole; only the heavy output fields (PRD `memory_report`, task `error`, `prior_attempt_hint`, `start_failure_tail[]`, `attempt_refs[]`, verdict and verifier text) are capped. Each capped field ends in the marker, is named in the record's `truncated` array together with the URL of the full text, and comes back uncapped from `prds/{id}?full=1`, `prds/{id}/outputs`, or `prds/{id}/tasks/{task_id}/outputs` (the last one is `autonomous_prd_task_outputs`). If you are about to save spec or description text, edit the full text, not a capped copy.
- **`autonomous_prd_cancel`** — cancel a PRD (proxies bare `DELETE /api/autonomous/prds/{id}`) and best-effort kill its worker sessions. Response: `id`, `status` (`"cancelled"`), `killed_sessions` (count of session slots recorded on the PRD's tasks), and `killed_session_ids` (array of the session IDs a kill was actually attempted on — always an array, `[]` when none). A `kill_errors` map (`{"<id>": "<error>"}`) is added only when a kill failed. Use `killed_session_ids` to verify no unrelated session was stopped.
- **`autonomous_prd_set_llm`** (v5.4.0; v8.20.0 extended; v8.33.9 priority fix) — operator-pinned LLM override at the PRD level. Parameters: `backend` (task-execution backend — opencode, claude-code, goose, etc.), `effort`, `model`, and `decomposition_profile` (planning/decompose backend — any configured LLM; opencode/claude-code spawn a full session with codebase tool access, ollama/openwebui run headless; empty = use global `autonomous.planning_backend`). Per-PRD `decomposition_profile` takes priority over the global setting. Optional `guardrail_backend` / `guardrail_model` set the guardrail/verification judge LLM override; it inherits task → Automaton → global `autonomous.verification_backend` / `verification_model`. Only the guardrail params you pass are sent (omitted = unchanged, `""` = clear), and a call with only guardrail params leaves the worker LLM untouched. While the PRD is still `planning` (queued in the capacity ledger), changing the decomposition pair retargets the queued planning run to the new pool in place — the reply then carries a `planning` object (`outcome`: `requeued` | `deferred_to_next_run` | `unchanged`, plus old/new backend, model and pools and a plain `reason`); a started run is left alone and the new pair applies from the next planning run. On a `running` PRD the worker-pair change is applied per task under `autonomous.override_queued_tasks` (default on): unstarted tasks are changed, started/finished tasks reported as left alone, and the reply carries an `override` object (`changed` / `left_alone` arrays of `{task_id, reason}` with `capacity_moved` on a retargeted `waiting_capacity` task).
- **`autonomous_prd_set_task_llm`** (v5.4.0) — operator-pinned LLM override at the task level (overrides the Automaton-level execution backend for that specific task). Accepts the same optional `guardrail_backend` / `guardrail_model` params with the same absent-vs-empty semantics, scoped to the one task. Acceptance is governed by `autonomous.task_override_statuses` (default `needs_review` / `revisions_asked` / `cancelled` / `blocked`). When the Automaton is outside that set and `autonomous.override_queued_tasks` is on (the default; unset reads as `true`, explicit `false` restores the classic refusal), the call is still accepted and applies only to unstarted tasks (`pending`, `queued`, `waiting_capacity`, `waiting_decision`, `awaiting_approval`, or never scheduled): unstarted tasks are changed, started/finished/parked tasks are reported as left alone with the reason (a task-level call aimed at an already-started or finished task is not refused — it answers 2xx with the task in `left_alone`), and a `waiting_capacity` task gets its queued capacity request retargeted to the new pool (`capacity_moved: true`). The reply then carries an `override` object with `changed` and `left_alone` arrays of `{task_id, reason[, capacity_moved]}`; a classic-accepted or refused call returns the plain PRD view.
- **`autonomous_prd_set_failure_escalation`** — set the failure escalation override on an Automaton (leave `task_id` empty) or on one task. `policy` is an object with `enabled` (boolean), `max_failures` (1 or more) and `steps` (an array of `{backend, model, effort, attempts}`); fields left out inherit from the next level up. `clear=true` removes the override at that level. Requires the autonomous-write capability for federation peers; a missing policy and no `clear` is refused before anything is sent.
- **`autonomous_prd_resolve_task_review`** — the operator's decision on a task held for review. `action` is `retry` (requeue it, optionally with a replaced `spec`, a worker `backend`/`model`/`effort` for the task, a narrowed `files` list, and a `note` the next attempt reads) or `cancel`. It works while the Automaton is running and other tasks are live: it answers at once and starts no second executor, and the running executor picks the decision up. A task that is not held answers 409, and an unknown Automaton or task answers 404.
- **`autonomous_prd_set_task_verification_llm`** / **`autonomous_prd_set_story_verification_llm`** / **`autonomous_prd_set_verification_llm`** (v9.0.69) — pick which LLM *verifies* (judges) the work of one task, every task in one story, or a whole Automaton. Precedence, most specific wins: task → story → Automaton → Automation → global `autonomous.verification_backend` / `verification_model` / `verification_effort`. Backend and model come from the same level (a less specific level's model never leaks past a more specific backend override); effort resolves independently. A backend set at any of these levels also bypasses the global `autonomous.verification_backends` capacity pool; a model or effort set without a backend at any per-unit level leaves the verifier on the global path. Effort is applied when the verifier runs as a session. Accepted while the Automaton is `needs_review`, `revisions_asked`, `cancelled` or `blocked`: acceptance is governed by `autonomous.task_override_statuses` (default `needs_review` / `revisions_asked` / `cancelled` / `blocked`). Each verification result records what actually ran as `verifier_backend` / `verifier_model` / `verifier_effort` / `verifier_source` (`task` | `story` | `prd` | `automation` | `global` | `pool`) on the task's `verification` object in `autonomous_prd_get`. The task itself records `executor_source` (`task` | `story` | `prd` | `automation` | `global`), the level that supplied its execution backend. `automation` is the default set on the Automation the Automaton belongs to (see `automation_set_llm` and `automation_set_verification_llm` below); it sits between the Automaton and the global default.

  | Parameter | Tools | Required | Description |
  |---|---|---|---|
  | `id` | all three | yes | Automaton ID |
  | `task_id` | `..._set_task_verification_llm` | yes | Task to override |
  | `story_id` | `..._set_story_verification_llm` | yes | Story to override (a task-level override still wins) |
  | `verification_backend` | all three | no | Any configured LLM name (unknown → error). `""` clears (inherit the next level); omit to leave unchanged |
  | `verification_model` | all three | no | Model for the verifier. `""` clears; omit = unchanged |
  | `verification_effort` | all three | no | `low` / `medium` / `high` / `max` / `quick` / `normal` / `thorough`. `""` clears; omit = unchanged |
  | `actor` | all three | no | Who made the change (default `operator`) |

  ```
  autonomous_prd_set_task_verification_llm(id="a1b2c3d4", task_id="t7", verification_backend="claude-code", verification_model="opus", verification_effort="high")
  autonomous_prd_set_story_verification_llm(id="a1b2c3d4", story_id="s2", verification_backend="ollama", verification_model="qwen3:8b")
  autonomous_prd_set_verification_llm(id="a1b2c3d4", verification_model="")   # clear the Automaton-level model only
  ```

  Returns the write reply described above (a small summary, not the whole Automaton). These overrides are separate from `guardrail_backend` / `guardrail_model` on `autonomous_prd_set_llm` / `autonomous_prd_set_task_llm`, which pick the guardrail judge.
- **`autonomous_prd_set_kind`** — set the kind of an Automaton: `feature`, `bug` or `other`. Takes `id` and `kind`; an empty `kind` clears the setting so the kind is derived again from the title and spec. An unknown kind is refused. `autonomous_prd_create` takes the same optional `kind`. Proxies `POST /api/autonomous/prds/{id}/set_kind`.
- **`autonomous_progress`** — read the progress metrics: counts by kind (feature, bug, other) and in total, throughput per active day, worker utilization, lead time per stage, the inactive windows left out of every rate, the remaining work split into known records and estimated work, and the completion range in days with the method behind each figure. Read-only; returns the same object as `GET /api/autonomous/progress`. All parameters are optional.

  | Parameter | Required | Description |
  |---|---|---|
  | `automation_id` | no | Count only the Automata of this Automation |
  | `tags` | no | Count only the Automata carrying these tags (array) |
  | `tag_match` | no | With several `tags`: `any` (default) counts an Automaton carrying at least one; `all` requires every tag named |
  | `since` | no | First day counted, `YYYY-MM-DD` (default: the configured date, else the first record's creation date) |
  | `until` | no | Last day counted, `YYYY-MM-DD`, inclusive (default: now) |

  ```
  autonomous_progress()                                  # the whole fleet
  autonomous_progress(tags=["1a2b3c4d"], since="2026-10-10")                    # the tag with name "stage:build"
  autonomous_progress(tags=["4f5e6d7c", "1a2b3c4d"], tag_match="all")  # "automation:release" ∩ "stage:build"
  ```

  A `tag_match` that is neither `any` nor `all` is refused with a message that names it. A tag id that is not in the registry simply matches no Automaton — an empty count means the id is stale, so re-read the ids from `autonomous_tag_list`.

  A missing backlog file is not an error: the reply carries `backlog_available: false`. A bad date, or `until` before `since`, is refused. Needs the autonomous read capability when called by a federated peer.
- **`autonomous_progress_trend`** — read the progress trend from the saved daily snapshots. Read-only; returns the same body as `GET /api/autonomous/progress/trend`: `days`, `points` (one per snapshot, oldest first: `date`, `reason`, `completion_low_days`, `completion_high_days`, `known_tasks`, `estimated_tasks`, `throughput_3d`, `utilization`, `completed_prds`) and `direction` (`converging`, `widening`, `flat` or `unknown`).

  | Parameter | Required | Description |
  |---|---|---|
  | `days` | no | How many days back to read, a whole number from 1 to 365 (default 30) |
  | `tags` | no | The tag scope the trend reply reports (array) |
  | `tag_match` | no | With several `tags`: `any` (default) was at least one; `all` every tag named |

  ```
  autonomous_progress_trend()
  autonomous_progress_trend(days=90)
  autonomous_progress_trend(days=14, tags=["1a2b3c4d"])   # the tag with name "stage:build"
  ```

  Without saved snapshots `points` is empty and `direction` is `unknown`. A `days` value outside 1 to 365 is refused.
- **`autonomous_timeline`** — read the Automata interval chart: every project that moved, each with its Automata (kind, status), its stages (bands) and its members (bars) with start and end, their blocked and waiting intervals (each with a reason, an end while the stretch is still open), the `today` marker and the projected finish per Automaton with the reason behind it (or why there is none). Times are UTC; the text bars drawn from the same object promise day precision. An Automaton with `end` set is finished — its date is its end, not a projection. An unknown filter value answers an **empty chart, not an error**, so a typo shows up as "no projects". Read-only; returns the same object as `GET /api/autonomous/timeline`.

  | Parameter | Required | Description |
  |---|---|---|
  | `project` | no | Only the Automata of this project directory |
  | `automaton` | no | Only this Automaton, by id or title (with it, only that Automaton's chart comes back) |
  | `stage` | no | Only this stage (story), by id or title |
  | `kind` | no | Only Automata of this kind: `feature`, `bug` or `other` |
  | `tag` | no | Only the Automata carrying this tag |

  ```
  autonomous_timeline()
  autonomous_timeline(kind="bug")
  autonomous_timeline(automaton="a1b2c3d4")
  ```

  Needs the autonomous read capability when called by a federated peer; the route answers 503 while autonomous is disabled.
- **`autonomous_lane_fill_status`** — read the lane fill status: each GPU and Claude lane with what it is running or waiting on, whether it is idle, the newest planner decisions (newest first) and any idle-lane alerts. A decision that refused an Automaton carries `eligibility_reason` (`stale`, `held`, `sequenced_after_unshipped`, `proposal_not_confirmed`, `proposal_missing`, `main_carries_work` or `worktree_invalid`); the key is absent when the decision is not a refusal. `lane_counts` gives, for `gpu` and `claude`, the configured count, the effective count the last poll used and its source (`config` or `llm_caps`), and `modes` gives the mode in effect for each lane kind. Read-only; returns the same object as `GET /api/autonomous/lane-fill`.

  | Parameter | Required | Description |
  |---|---|---|
  | `limit` | no | How many decisions to return, a whole number from 1 to 1000 (default 50) |

  ```
  autonomous_lane_fill_status()
  autonomous_lane_fill_status(limit=200)
  ```

  A `limit` outside 1 to 1000 is refused with the daemon's message. Needs the autonomous read capability when called by a federated peer.
- **`autonomous_lane_utilization`** — read how the lanes were used over a window ending now: per-lane busy and idle time in buckets, plus the lane fill status with its newest decisions. Read-only; returns the JSON of `GET /api/autonomous/lane-utilization` unchanged.

  | Parameter | Required | Description |
  |---|---|---|
  | `window` | no | Window length as a duration such as `6h`, `24h` or `7d` (default `24h`) |
  | `bucket` | no | Number of time buckets, a whole number |
  | `decisions` | no | How many of the newest decisions to return, a whole number |

  ```
  autonomous_lane_utilization()
  autonomous_lane_utilization(window="7d", bucket=14, decisions=20)
  ```

  A value out of range is refused with the daemon's message. Needs the autonomous read capability when called by a federated peer.
- **`onboarding_report`** — read the onboarding report: what is configured (backends, models, projects, comm channels, memory, merge pipeline), what is missing, and what to try next with a how-to link for each step. `format` is `text` (the default, rendered by the same function as the REST route, the CLI and the comm verb) or `json` (the REST body). Read-only; says so when `onboarding_report.enabled` is off. Same data as `GET /api/automation/onboarding-report`.
- **`compute_served_models`** — read which models each compute node serves right now. Without arguments it lists every enabled node with its served models and whether the list could be read. Give `model` (and optionally `node` and `backend`) to also get the answer to "would this node accept this model?" — the same check an Automaton makes when a task or story is pinned to a node, so the two never disagree. A node's list is kept for `autonomous.model_routing.served_models_cache_seconds` (default 300); `refresh=true` reads it from the node again. Read-only. Needs the compute read capability when called by a federated peer.

  | Parameter | Required | Description |
  |---|---|---|
  | `node` | no | Limit the answer to one compute node name |
  | `model` | no | A model name to check against the node's served list |
  | `backend` | no | The LLM backend the model would run on, used together with `model` |
  | `refresh` | no | `true` reads the served list from the node now instead of using the kept copy |

  ```
  compute_served_models()
  compute_served_models(node="gpu1", model="qwen3:27b", backend="pi-prompt", refresh=true)
  ```

- **`autonomous_prd_set_task_node`** — pin one task of an Automaton to a compute node, so its worker runs there. An empty `node` clears the pin and the task goes back to normal node choice. A node that does not serve the task's model is refused, or only warned about, according to `autonomous.model_routing.refusal_mode`. The verification job follows the worker's node unless `autonomous.model_routing.verifier_follows_worker_node` is `false`. Needs the Automata write capability when called by a federated peer.

  | Parameter | Required | Description |
  |---|---|---|
  | `prd_id` | yes | Automaton ID |
  | `task_id` | yes | Task ID |
  | `node` | no | Compute node name to pin the task to; empty clears the pin |

  ```
  autonomous_prd_set_task_node(prd_id="a1b2c3d4", task_id="t1", node="gpu1")
  autonomous_prd_set_task_node(prd_id="a1b2c3d4", task_id="t1", node="")
  ```

- **`autonomous_prd_set_story_node`** — pin every task of one story of an Automaton to a compute node. An empty `node` clears the pin. A task's own node pin wins over the story's. The same model check applies. Needs the Automata write capability when called by a federated peer.

  | Parameter | Required | Description |
  |---|---|---|
  | `prd_id` | yes | Automaton ID |
  | `story_id` | yes | Story ID |
  | `node` | no | Compute node name to pin the story to; empty clears the pin |

  ```
  autonomous_prd_set_story_node(prd_id="a1b2c3d4", story_id="s1", node="gpu2")
  ```

  The routing behaviour is set with six keys under `autonomous.model_routing.*`, either with `config_set` (for example `config_set(key="autonomous.model_routing.refusal_mode", value="warn")`) or with the `model_routing_*` arguments of `autonomous_config_set`. They are read with `get_config`, under `autonomous.model_routing`; `autonomous_config_get` does not return them. The daemon names the valid values in a refusal.

  | Argument | Sets | Type and default |
  |----------|------|------|
  | `model_routing_refusal_mode` | `autonomous.model_routing.refusal_mode` | `refuse` (default) or `warn` |
  | `model_routing_served_models_cache_seconds` | `autonomous.model_routing.served_models_cache_seconds` | whole number >= 0, default 300; 0 reads from the node every time |
  | `model_routing_verifier_follows_worker_node` | `autonomous.model_routing.verifier_follows_worker_node` | boolean, default true |
  | `model_routing_unknown_node_policy` | `autonomous.model_routing.unknown_node_policy` | `assume_serves` (default) or `assume_not`: how a node whose models cannot be read counts |
  | `model_routing_probe_timeout_seconds` | `autonomous.model_routing.probe_timeout_seconds` | whole number >= 1, default 8 |
  | `model_routing_alert_on_refusal` | `autonomous.model_routing.alert_on_refusal` | boolean, default true |

- **`autonomous_infra_retries`** — read the infrastructure retry state: the retry policy in effect (`enabled`, `max_tries`, `backoff_seconds`, `categories`, `exhausted_action`, `retry_dependents`, `raise_question`) and the tasks of Automata that were retried, or are held, because the machinery around them failed rather than their own work. Each task carries its Automaton, `retry_count`, `max_tries`, the `retries[]` history, and `held` with `held_cause` and `question_id` when it waits for the operator. The most recently retried task comes first. Read-only; returns the same object as `GET /api/autonomous/infra-retries`.

  | Parameter | Required | Description |
  |---|---|---|
  | `automaton` | no | An Automaton ID; only its tasks are listed |
  | `limit` | no | How many tasks to return, a whole number from 1 to 1000 (default 50) |

  ```
  autonomous_infra_retries()
  autonomous_infra_retries(automaton="a1b2c3d4", limit=20)
  ```

- **`autonomous_defect_intake`** — route one incoming defect through the defect queue. The decision is `tiny_fix` (a one-task Automaton), `open_batch` (the defect is added to an open draft batch of the same project and area) or `new_batch`. The reply carries `route`, `reason` and `batch_id`; when the tiny-fix lane refused a change that looked eligible, `tiny_fix_refusal` names the paths and the reason, and the defect went to a batch instead. Writes: it creates or extends an Automaton and appends to the intake decision log. Refused with a plain message when `autonomous.defect_queue.enabled` is off for the project. Same as `POST /api/autonomous/defects/intake`.

  | Parameter | Required | Description |
  |---|---|---|
  | `id` | yes | Defect id |
  | `title` | yes | Short defect title |
  | `project` | yes | Project the defect belongs to |
  | `description` | no | Defect description |
  | `files` | no | Repo-relative paths the fix is expected to touch |
  | `kind` | no | Declared change kind: `test_only`, `docs_only`, `script_only`, `rule_tweak` or `lint_tweak`; anything else is not a tiny fix |
  | `area` | no | Area used to pick an open batch |
  | `priority` | no | Higher is more urgent (default 0) |

  ```
  autonomous_defect_intake(id="D-41", title="Reconnect stalls the session list", project="datawatch", area="web-sessions", files=["internal/server/web/app.js"], priority=2)
  ```

  Needs the autonomous write capability when called by a federated peer.
- **`autonomous_defect_batches`** — list the open defect batches with the story-to-defect mapping of each: batch id, project, area, status, size and one `{story_id, defect_id}` per story. Read-only. Same as `GET /api/autonomous/defects/batches`.

  | Parameter | Required | Description |
  |---|---|---|
  | `project` | no | Only batches of this project; empty lists all |

  ```
  autonomous_defect_batches()
  autonomous_defect_batches(project="datawatch")
  ```

  Needs the autonomous read capability when called by a federated peer.
- **`autonomous_defect_batch_split`** — split one story out of a defect batch into its own new Automaton, for example after that story failed. The new Automaton holds that story with its own gate, documentation and changelog tasks; the batch keeps its other stories and the dependencies on the moved tasks are removed. Returns the new `automaton_id`. Writes. Same as `POST /api/autonomous/defects/batches/{id}/split`.

  | Parameter | Required | Description |
  |---|---|---|
  | `batch_id` | yes | Defect batch id |
  | `story_id` | yes | Story to split out |

  ```
  autonomous_defect_batch_split(batch_id="a1b2c3d4", story_id="a1b2c3d4-story-D-41")
  ```

  An unknown batch or story is a not-found answer. Needs the autonomous write capability when called by a federated peer.
- **`autonomous_defect_tinyfix_check`** — check whether a change qualifies for the tiny-fix lane, without creating anything. Returns `eligible` and a `reason`; a change that touches operator-visible or out-of-lane paths is refused with the paths (`refused_paths`) and the reason named. Read-only. Same as `POST /api/autonomous/defects/tiny-fix/check`.

  | Parameter | Required | Description |
  |---|---|---|
  | `files` | yes | Repo-relative paths the change touches |
  | `kind` | yes | Declared change kind: `test_only`, `docs_only`, `script_only`, `rule_tweak` or `lint_tweak` |
  | `project` | no | Project whose settings apply; empty uses the block-level settings |

  ```
  autonomous_defect_tinyfix_check(files=["internal/autonomous/x_test.go"], kind="test_only", project="datawatch")
  ```

  Needs the autonomous read capability when called by a federated peer.

- **`generated_artefacts_get`** — read the seven `autonomous.generated_artefacts.*` switches in effect (`change_record`, `gate_report`, `changelog_draft`, `parity_draft`, `tracker_rows`, `registry_row`, `combined_docs_task`) and each artefact kind with its format and tracked path. Read-only. Same as `GET /api/autonomous/generated-artefacts`. The switches are changed with `config_set`.

  | Parameter | Required | Description |
  |---|---|---|
  | (none) | | Takes no arguments |

  ```
  generated_artefacts_get()
  ```

  Needs the autonomous read capability when called by a federated peer. Operator view: [`howto/generated-artefacts.md`](howto/generated-artefacts.md).
- **`autonomous_prd_change_record`** — read the stored change record of one Automaton: files, Go packages, settings, routes, locale keys, tests and the commands that were run. Read-only. Same as `GET /api/autonomous/prds/{id}/change-record`.

  | Parameter | Required | Description |
  |---|---|---|
  | `id` | yes | Automaton id |

  ```
  autonomous_prd_change_record(id="a1b2c3d4")
  ```

  An unknown Automaton, or one with no record yet, is a not-found answer. Needs the autonomous read capability when called by a federated peer.
- **`autonomous_prd_generated_artefact`** — preview one generated artefact (the draft that would be written, its tracked path and whether its switch is on) without writing anything. Same as `GET /api/autonomous/prds/{id}/generated/{kind}`.

  | Parameter | Required | Description |
  |---|---|---|
  | `id` | yes | Automaton id |
  | `kind` | yes | One of `gate_report`, `changelog_draft`, `parity_draft`, `tracker_rows`, `registry_row` |

  ```
  autonomous_prd_generated_artefact(id="a1b2c3d4", kind="changelog_draft")
  ```

  An unknown Automaton or kind is a not-found answer. Needs the autonomous read capability when called by a federated peer.
- **`autonomous_prd_regenerate_artefact`** — render one generated artefact again and write it to its tracked path. A file a person edited is kept and the answer is `skipped` with the reason. Same as `POST /api/autonomous/prds/{id}/generated/{kind}/regenerate`.

  | Parameter | Required | Description |
  |---|---|---|
  | `id` | yes | Automaton id |
  | `kind` | yes | One of `gate_report`, `changelog_draft`, `parity_draft`, `tracker_rows`, `registry_row` |

  ```
  autonomous_prd_regenerate_artefact(id="a1b2c3d4", kind="gate_report")
  ```

  Needs the autonomous write capability when called by a federated peer; the call is written to the audit log as `regenerate_artefact`.
- **`autonomous_merge_pipeline_get`**, **`autonomous_merge_pipeline_set_record`**, **`autonomous_merge_pipeline_validate`** — the merge pipeline of a project. `_get` reads the pipeline that would run, where it came from (the repository file, the record the daemon keeps, or the built-in pipeline) and why, the conflict policy, the release settings in effect (the release size each kind adds, the major release trigger, the release cadence and the version scheme) and the project's overrides, and the release approval modes in effect for the project, as `release_approval_in_effect` in the JSON answer (`patch`, `minor` and `major`, each resolved to `auto`, `ask` or `manual`; the text answer shows them on a `Release approval:` line). `_set_record` keeps a pipeline record for a project (`yaml` text or a `definition` object, not both) or removes it (`clear=true`). `_validate` checks a pipeline you give, or the repository file (`file` is `true` or a file name inside the project), without saving; an invalid pipeline is a normal answer with every problem listed. All three answer in plain language, or with the daemon's JSON when `format` is `json`. For `_get`, when no project is given and no merge base repository is set, the tool fails with a `project_required` error that says to choose a project or set the merge base repository. The settings are changed with `autonomous_config_set` (`merge_pipeline_file_name`, `merge_pipeline_source_order`, `merge_pipeline_fallback_on_bad_file`, `merge_pipeline_conflict_policy`, `merge_pipeline_conflict_model`, `merge_pipeline_version_files`, and the release settings `merge_pipeline_release_size_map` (kind=size pairs such as `feature=minor,bug=patch`, or a JSON object; sizes are `patch`, `minor`, `major` or `none`), `merge_pipeline_major_trigger` (`manual`, `milestone` or `never`), `merge_pipeline_release_cadence` (`per_merge`) and `merge_pipeline_version_scheme` (`semver`), and the release approval arguments `merge_pipeline_release_approval_patch`, `merge_pipeline_release_approval_minor` and `merge_pipeline_release_approval_major` — each `auto`, `ask` or `manual`, written as `autonomous.merge_pipeline.release_approval.<size>`, and a value outside the three is refused with the key named; `merge_pipeline_project` applies them to one project). A cadence other than `per_merge` (`batch` and `manual` are not available yet) or a scheme other than `semver` is refused by the daemon, and the refusal comes back as a tool error that names the value to use. Reads need the autonomous read capability, `_set_record` the write capability. The decision the daemon's own merge run raises in `ask` mode is one ordinary stored operator question raised by the daemon's release approval; answer it with `question_answer` — option id `yes` ships, `no` (the default) holds — and there is no release-specific tool.
- **`release_push_status`** — how a project's finished release is published, for an Automaton. Answers `push_and_tag` (whether the daemon pushes and tags the release on its own; default off — the default, unattended behaviour stays off), `release_path` (`direct_push` or `pull_request`; default `direct_push` — on `pull_request` the daemon pushes the release branch, opens a pull request into the default branch, polls its checks for `ci_window_minutes` and merges only when every check succeeded; a failed or still-pending check set leaves the pull request open and unmerged), `ci_window_minutes` (how long the daemon waits for the release CI; default 60) and `last_release` (the newest release recorded for the project: `tag`, `commit`, `release_path`, `push_outcome`, `ci_outcome`; `null` when none). Settings not set read their defaults, and no release setting is set or cleared by the call. Reads need the autonomous read capability when called by a federated peer; the settings are changed with `autonomous_config_set` (`push_and_tag`, `release_path`, `release_ci_window_minutes`) or `config_set` under `git.release_ci_window_minutes`.

  | Parameter | Required | Description |
  |---|---|---|
  | `project` | yes | Project name the status is read for |

  ```
  release_push_status(project="demo")
  ```

  An unknown project is a not-found answer; a project with no release yet answers with `last_release` null and defaults for unset settings.

  ```
  autonomous_merge_pipeline_get(project="demo")
  autonomous_merge_pipeline_validate(file="true")
  autonomous_merge_pipeline_set_record(project="demo", yaml="steps:\n  - name: tests\n    kind: test\n    command: make test\n")
  ```

  A `limit` outside 1 to 1000 is refused with the daemon's message. Needs the autonomous read capability when called by a federated peer. The policy keys are set with `config_set` under `autonomous.infra_retry.*` (for example `config_set(key="autonomous.infra_retry.max_tries", value="5")`) or with the `infra_retry_*` arguments of `autonomous_config_set`, and read with `autonomous_config_get`, which returns the `infra_retry` block with the rest of the config. `autonomous_config_set` builds its request from a fixed list of arguments, so the block is reachable there only through these arguments:

  | Argument | Sets | Type |
  |----------|------|------|
  | `infra_retry_enabled` | `autonomous.infra_retry.enabled` | boolean |
  | `infra_retry_max_tries` | `autonomous.infra_retry.max_tries` | whole number from 1 to 1000 |
  | `infra_retry_backoff_seconds` | `autonomous.infra_retry.backoff_seconds` | array of whole numbers of seconds; `[]` means no delay |
  | `infra_retry_categories` | `autonomous.infra_retry.categories` | array of failure class names; `[]` retries none |
  | `infra_retry_exhausted_action` | `autonomous.infra_retry.exhausted_action` | `hold` or `count` |
  | `infra_retry_retry_dependents` | `autonomous.infra_retry.retry_dependents` | boolean |
  | `infra_retry_raise_question` | `autonomous.infra_retry.raise_question` | boolean |

  Each one is sent only when given, so an explicit `false`, `0` or `[]` is applied and an argument left out changes nothing. They travel as dotted keys in a `PUT /api/config` body, where the daemon checks every value and a refused value comes back as a tool error naming the key and the valid values. The two lists replace the whole list.

  ```
  autonomous_config_set(infra_retry_max_tries=5, infra_retry_backoff_seconds=[30, 120])
  ```
- **`compute_placement_list`** — list the pending placement asks: requests whose model is present on no eligible ComputeNode, queued for approval while `compute_placement.missing_model` is `ask`. No arguments. Read-only. Returns the same list as `GET /api/compute/placements`. See [howto/compute-placement-ask.md](howto/compute-placement-ask.md).
- **`compute_placement_answer`** — approve or deny one pending placement ask. Arguments: `id` (required, the ask ID from `compute_placement_list`, for example `ask-1`) and `decision` (required, `approve` or `deny`). The decision is one-shot: an approved, denied or expired ask cannot be decided again. Proxies `POST /api/compute/placements/{id}/approve` or `/deny`; needs the compute write capability when called by a federated peer.
- **`autonomous_request_watch`** — read the in-flight model request of every watched worker or verifier session whose model runs on a registered compute node, so a hung request can be told from a slow one. The object carries the watch settings in effect (`enabled`, `poll_seconds`, `no_progress_minutes`) and `observations[]` (an empty list, never null, when nothing is watched or the watch is off). Each observation has `session_id`, `automaton_id`, `task_id`, `role` (`worker` or `verifier`), `node`, `model`, `state` (`generating`, `idle`, `unreachable`, or `unknown` before the first sample), `last_change` (RFC 3339), `no_progress_seconds`, `last_poll`, `over_threshold`, `error` (only when the node could not be read) and a plain-text `summary`: `waiting on a model request: generating`, `waiting on a model request: no progress for N minutes` or `waiting on a model request: node unreachable, no progress for N minutes`. Read-only and observation only: it makes no model call and never ends a worker, retries on another backend or unloads a model. Returns the same object as `GET /api/autonomous/request-watch`.

  | Parameter | Required | Description |
  |---|---|---|
  | `session` | no | A session ID; only that session's observation is returned |
  | `automaton` | no | An Automaton ID; only the observations of that Automaton's sessions are returned |

  ```
  autonomous_request_watch()
  autonomous_request_watch(automaton="a1b2c3d4")
  autonomous_request_watch(session="ab12", automaton="a1b2c3d4")
  ```

  An ID that matches nothing returns an empty list. Needs the autonomous read capability when called by a federated peer. The watch keys are set with `config_set` under `autonomous.request_watch.*` (for example `config_set(key="autonomous.request_watch.no_progress_minutes", value="45")`) or with the `request_watch_*` arguments of `autonomous_config_set`, and read with `autonomous_config_get`, which returns the `request_watch` block with the rest of the config. `autonomous_config_set` builds its request from a fixed list of arguments, so the block is reachable there only through these arguments:

  | Argument | Sets | Type |
  |----------|------|------|
  | `request_watch_enabled` | `autonomous.request_watch.enabled` | boolean (default true) |
  | `request_watch_poll_seconds` | `autonomous.request_watch.poll_seconds` | whole number of 1 or more (default 30) |
  | `request_watch_no_progress_minutes` | `autonomous.request_watch.no_progress_minutes` | whole number of 1 or more (default 30) |
  | `request_watch_observe_workers` | `autonomous.request_watch.observe_workers` | boolean (default true) |
  | `request_watch_observe_verifiers` | `autonomous.request_watch.observe_verifiers` | boolean (default true) |
  | `request_watch_use_gpu_signal` | `autonomous.request_watch.use_gpu_signal` | boolean (default true) |
  | `request_watch_gpu_busy_percent` | `autonomous.request_watch.gpu_busy_percent` | whole number from 1 to 100 (default 10) |
  | `request_watch_unreachable_counts` | `autonomous.request_watch.unreachable_counts` | boolean (default true) |
  | `request_watch_raise_alert` | `autonomous.request_watch.raise_alert` | boolean (default true) |
  | `request_watch_raise_question` | `autonomous.request_watch.raise_question` | boolean (default true) |
  | `request_watch_remediation_options` | `autonomous.request_watch.remediation_options` | array or comma separated string of `end_worker`, `retry_other_backend`, `unload_reload_model`; unset offers all of them, `[]` or an empty string offers `keep_waiting` only |

  Each one is sent only when given, so an explicit `false` or `[]` is applied and an argument left out changes nothing. They travel as dotted keys in a `PUT /api/config` body, where the daemon checks every value and a refused value comes back as a tool error naming the key and the valid values. `keep_waiting` is always offered and is always the default answer; the question raised at the threshold is not blocking and answering it only records the choice. The option list replaces the whole list.

  ```
  autonomous_config_set(request_watch_no_progress_minutes=45, request_watch_remediation_options="end_worker,retry_other_backend")
  ```
- **`autonomous_triage`** — read the triage records: one record per failed or blocked Automaton (newest first), with its identity (`prd_id`, `prd_title` when it has one, `automation_id`, `automation_name`, `member_id`), the failed or blocked task (`task_id`, `task_title` when present) with its `error` and `blocked_reason` and `session_end_reason` (each truncated to 2000 chars by the pass), `first_seen_at`, `updated_at` and, once set, `alerted_at`, the retry budgets shown as given (all four categories with `limit`, `used`, `left`, plus `ceiling_total` and `ceiling_left` when an overall ceiling is set), and the append-only notes — beside `alert_minutes`, the triage alert threshold in minutes currently in effect (`0` = alert off), which rides the list body, not each record. Read-only: listing never starts, retries or edits anything. Returns the same object as `GET /api/autonomous/triage`.

  | Parameter | Required | Description |
  |---|---|---|
  | `automation_id` | no | only records in this automation |
  | `prd_id` | no | the record of one Automaton |
  | `status` | no | `failed` or `blocked` (case-sensitive; anything else is refused) |

  ```
  autonomous_triage()
  autonomous_triage(status="failed")
  autonomous_triage(automation_id="aut_01", status="blocked")
  ```

  A wrong `status` comes back as a tool error with the daemon's message. Needs the autonomous read capability when called by a federated peer.
- **`checks_status`** — read the automatic checks: the resolved mode of each check (`pre_start_verifier`, `plan_rules`, `after_done`, `registry_readme`, each `off`, `shadow` or `on`, plus `block_when_missing`) and the findings they recorded, newest first. Optional `prd_id` (one Automaton) and `project_dir` (per-project policy). Read-only; the same object as `GET /api/autonomous/checks`.
- **`ship_status`** — read the ship step: the resolved `push`, `tag` and `ci_watch` modes, `tag_format`, `remote` and `ci_workflows`, and the outcomes recorded for finished Automata, newest first. Optional `prd_id` and `project_dir`. Read-only; the same object as `GET /api/autonomous/ship`.
- **`restart_interruptions`** — report what a daemon restart now would interrupt: the planning Automata, verifying tasks, running judges, running tasks (executing worker, running tests or verifying) and live sessions, each with ids and counts; plus `needs_force` (set while any of them is in flight), the restart policy in force (`autonomous.restart_policy`: `immediate`, `wait_quiet` or `drain`) and the launch-draining state. No arguments. Read-only; the same object as `GET /api/autonomous/restart-interruptions`. `restart_daemon` is refused while `needs_force` is set unless it is given `force=true`; the refusal carries this same report, and a forced call then still applies the policy.
- **`rules_context_status`** — read the rules and context settings that shape how Automata are planned: the configured values (rules file, context files, digest mode, digest size limit, per-project overrides) and the policy resolved for a project. Optional `project_dir` (resolve for that project) and `prd` (an Automaton id; adds its planning record: file names, hashes, sizes, missing files, digest size, never content). Read-only; the same object as `GET /api/autonomous/rules-context`.
- **`rules_context_set`** — change those settings; only the fields given change. Optional `rules_file`, `context_files` (list, at most 8, empty clears), `digest_mode` (`off`, `no_tools` or `all`), `digest_max_bytes` (0 or 512 to 65536) and `project_dir` (change that project's override instead of the global values). File names must be relative with no `..`. Writes the configuration file; the same change as `PUT /api/autonomous/rules-context`.

- **`security_failure_list`** — list the stored security failure reports, newest first, as a JSON list. Each entry carries the failing security run's identity (`repo`, `ref_kind`, `ref_name`, `workflow`, `run_id`, `run_url`, `status`) and the report stored for it under `report`: `dependency`, `advisory_ids`, `workflow`, `run_id`, `run_url`, `sha`, `published_at`, `fixed_version`, the accepted-risks registry result (`registry_found`, `registry_id`, `registry_first_added`, `registry_added`, `registry_expires`, `registry_error`), `lookup_error`, `recommendation`, `note`, the `mode` it was made under, the `action` the Automaton line received (`report_only`, `would_stop` or `stopped`) and `assessed_at`. Read-only: listing never reruns, cancels, stops or edits anything. Returns the same list as `GET /api/autonomous/security-failures`; nothing matching is `[]`.
- **`dependency_update_status`** — read the weekly dependency update Automation: the three settings as they are in effect (`enabled`, `schedule` as 'weekday HH:MM', `max_majors`), the next scheduled run in local time (`next_run`, empty while the Automation is off), the time and Automaton title of the last run (`last_run_at`, `last_run_automaton_title`) and its outcome (`last_run_outcome`: `updated`, `nothing to update` or `needs attention`). The last-run fields are empty until a run has happened. Read-only; takes no parameters.
- **`supervisor_watch_stats`** — read the supervisor watch as numbers: `mode`, `last_poll_at` and `last_poll_age_seconds` (-1 when the coordinator never polled), `findings_open`, `findings_would_alert`, `findings_alerted`, `findings_resolved` and `agreed_days` (days of full agreement with the supervisor script). Takes no arguments; any argument is refused. Read-only. Returns the same object as `GET /api/autonomous/supervisor-watch/stats`.
- **`supervisor_watch_list`** — read the supervisor watch state as one JSON object: the `mode` in effect (`off`, `shadow` or `on`; an unset mode reads `shadow`, which reports only and never acts), `last_poll_at` (the instant the coordinator last polled), the stored `findings` (open first, most recently seen first: `kind`, `subject`, the operator line, first and last seen, the shadow would-alert mark and the raised-alert instant) and the `comparison` against the supervisor script's record. Takes no arguments; any argument is refused. Read-only: listing never raises an alert, resolves a finding or changes the mode. Returns the same object as `GET /api/autonomous/supervisor-watch`.
- **`supervisor_loop_jobs`** — read the supervisor loop's jobs: the loop's `enabled` state and, per job, its `name`, `kind`, the model job's style, backend and model, the enabled state, the last outcome and the last run instant. Takes no arguments; any argument is refused. Read-only. Returns the same body as `GET /api/supervisor-loop/jobs`.
- **`supervisor_loop_runs`** — read the supervisor loop's run records, newest first. Optional `job` (a job name), `outcome` (`ok`, `validator_failed`, `escalated` or `error`) and `limit` filters; any other argument is refused. Read-only. Returns the same body as `GET /api/supervisor-loop/runs`.
- **`supervisor_loop_run_job`** — run one supervisor loop job now. Needs `job` (the job name); optional `input` (text) and `candidates` (a comma list, for the model jobs that choose among candidates). An unknown job name comes back as a tool error with the daemon's `unknown job` message. Proxies `POST /api/supervisor-loop/jobs/<job>/run`. The loop's settings are the `supervisor_loop.*` keys, set with `config_set`.
- **`supervisor_loop_eval`** — compare models on a model job's eval cases. Needs `job` (`draft_spec`, `classify_ci_failure` or `pick_backend`) and `models` (a comma list); optional `cases_dir` (the name of a folder under the default cases folder). Returns per model: cases, passed, validator failures, retries, median and maximum duration, escalations. Fails naming each model when none is reachable. Proxies `POST /api/supervisor-loop/eval`.
- **`supervisor_loop_stats`** — read the supervisor loop as numbers: `enabled`, `jobs`, `jobs_enabled`, `runs`, the split by outcome (`runs_ok`, `runs_validator_failed`, `runs_escalated`, `runs_error`), `retries` and `last_run_age_seconds` (-1 when no run is stored). Takes no arguments; any argument is refused. Read-only. Returns the same object as `GET /api/supervisor-loop/stats`.

  | Parameter | Required | Description |
  |---|---|---|
  | `repo` | no | only the reports of this repository path |
  | `status` | no | only reports whose run is in this watch state: `green`, `failing`, `pending`, `hung`, `rerunning` or `superseded` (anything else is refused) |

  ```
  dependency_update_status()
  security_failure_list()
  security_failure_list(repo="dmz006/datawatch")
  security_failure_list(repo="dmz006/datawatch", status="failing")
  ```

  A wrong `status` comes back as a tool error with the daemon's message. Needs the autonomous read capability when called by a federated peer.
- **`autonomous_decisions`** — read the operator decisions, newest first. Each entry carries the decision `id`, the `question` and `label`, the `state` (`open`, `answered`, `withdrawn` or `expired`), `asked_by` and `asked_at`, the Automaton it belongs to (`prd_id`), `task_ids` (the tasks of that Automaton whose spec names the decision), `waiting_task_ids` and `waiting_tasks` (the ones held on it right now, each with its `wait_reason`) and `answer`. `answer` is `null` until the decision is answered; then it holds the answer `id`, the operator's words verbatim in `quote`, `quote_sha256`, `actor`, `surface` and `answered_at`. Read-only: it never raises, answers or withdraws a decision. Returns the same object as `GET /api/autonomous/decisions`. How an Automaton task waits on a decision is in [howto/automaton-operator-decisions](howto/automaton-operator-decisions.md).

  | Parameter | Required | Description |
  |---|---|---|
  | `prd_id` | no | only the decisions linked to this Automaton |
  | `state` | no | `open`, `answered`, `withdrawn` or `expired` (anything else is refused) |
  | `limit` | no | at most this many decisions, newest first; default 100, maximum 1000 |

  ```
  autonomous_decisions()
  autonomous_decisions(state="open")
  autonomous_decisions(prd_id="d0442737d38c", state="answered", limit=20)
  ```

  A wrong `state` or a `limit` that is not a positive whole number comes back as a tool error with the daemon's message. Needs the autonomous read capability when called by a federated peer.
- **`question_stats`** — read the worker question marker's counters since daemon start: `raised`, `duplicate`, `refused`, `parked` and `resumed`, plus `open_marker_questions` and `open_blocking_questions` now and the two switches `questions_enabled` and `worker_marker_enabled`. Takes no parameters. Read-only; the same object as `GET /api/questions/stats`, which answers whether or not the channel is on. Needs the autonomous read capability when called by a federated peer.

  ```
  question_stats()
  ```
- **`autonomous_held_questions`** — read the held-question report. The report carries `mode` (in effect: `off`, `shadow` or `on`), the held `states` it covers, `open` (the questions held right now in `on` mode, each with `question_id`, the held `state`, the owning Automaton `prd_id`, the task `task_id`, the worker `automation_id` and its `title`, the `reason` it is held, the `recommended_option` and the forced `default`) and `shadow` (the would-be holds the daemon logged instead of raising in `shadow` mode, each with the owning `prd_id`, the held `state`, the `recommended_option` and the time `at`). While the mode is `off` both lists are empty. Read-only: it never raises, answers, withdraws or unholds. Returns the same object as `GET /api/autonomous/held-questions`; acting on a hold is a worker question answer, not this tool.

  | Parameter | Required | Description |
  |---|---|---|
  | `prd_id` | no | only that Automaton's held questions and shadow entries |
  | `state` | no | one of the five held state ids: `blocked`, `review_held`, `revision_requested`, `awaiting_decision` or `pending_insert` (anything else is refused) |

  ```
  autonomous_held_questions()
  autonomous_held_questions(prd_id="d0442737d38c")
  autonomous_held_questions(state="awaiting_decision")
  ```

  A wrong `state` comes back as a tool error with the daemon's message (it names the accepted held state ids). Needs the autonomous read capability when called by a federated peer.
- **`autonomous_merge`** — read the Automaton merge state: `mode` (the `merge_pending_mode` in effect: `off`, `shadow` or `on`), `alert_minutes` (the merge-pending alert threshold in whole minutes; `0` = alert off), `automata` (one entry per finished Automaton that has a work branch, sorted by `automaton_id`, each with `automaton_id`, `name`, `branch`, `state` — `merged`, `pending`, `needs_look`, `empty` or `unknown` — `commits_ahead`, `pending_since`, `alerted_at` and, in `shadow` mode only, `would_alert`) and `shadow_decisions` (what the check would have acted on, newest first; empty outside `shadow` mode). While the mode is `off` both lists are empty. The state comes from the daemon's last comparison, so a read never runs git, and an Automaton not compared yet reads `unknown` (treat it as "no information"). Read-only: nothing is merged, deleted or pushed. Returns the same object as `GET /api/autonomous/merge`.

  | Parameter | Required | Description |
  |---|---|---|
  | — | — | The tool takes no parameters |

  ```
  autonomous_merge()
  ```

  Example result (shortened):

  ```json
  {"mode": "shadow", "alert_minutes": 30,
   "automata": [{"automaton_id": "d0442737d38c", "name": "nightly ingest", "branch": "automaton/d0442737d38c", "state": "pending", "commits_ahead": 3, "pending_since": "2026-10-29T09:12:44Z", "alerted_at": null, "would_alert": false}],
   "shadow_decisions": []}
  ```

  Answers a tool error while Automata are disabled (the daemon's `503`). Needs the autonomous read capability when called by a federated peer. The two settings that drive it are set through `autonomous_config_set` with `automation_merge_pending_mode` (`off`, `shadow` or `on`) and `automation_merge_pending_alert_minutes` (whole number >= 0; `0` turns the alert off, unset keeps 30):

  ```
  autonomous_config_set(automation_merge_pending_mode="shadow")
  autonomous_config_set(automation_merge_pending_alert_minutes=0)
  ```
- **`autonomous_merge_shadow_status`** — read the shadow merge plan state (Phase 4, shadow mode) grouped by project: `projects` (sorted by project name; an empty list when no plan or comparison is stored — the normal empty state, not an error), each with `plans` (the stored shadow merge plans; `applied` is always `false` — a record, not a queue), `comparisons` (the stored comparisons; the post-ship flow persists the defect candidates, not the comparison record, so this list is empty until that wiring lands) and `counts` (per release size — `patch`, `minor`, `major`, `none` — the matched and different comparison counts; all four sizes are always present). Read-only: it reads the two stores under the daemon's data directory and runs no git, merges nothing, and files nothing into the defect queue. Needs the autonomous read capability when called by a federated peer. Returns the same object as `GET /api/autonomous/merge/shadow`; the walkthrough is [howto/merge-shadow.md](howto/merge-shadow.md#where-the-shadow-state-is-read).

  | Parameter | Required | Description |
  |---|---|---|
  | `project` | no | only that project's entry; the default is every project |

  ```
  autonomous_merge_shadow_status()
  autonomous_merge_shadow_status(project="datawatch")
  ```

  Example result (shortened):

  ```json
  {"projects": [{"project": "datawatch",
   "plans": [{"automaton_id": "d0442737d38c", "release_size": "patch", "planned_version": "9.35.0", "applied": false}],
   "comparisons": [],
   "counts": {"patch": {"matched": 0, "different": 0}, "minor": {"matched": 0, "different": 0}, "major": {"matched": 0, "different": 0}, "none": {"matched": 0, "different": 0}}}]}  
  ```
- **`autonomous_config_set`** host load check arguments — `host_load_alert_mode` (`off`, `alert` or `reap`; default `alert`), `host_load_alert_multiple` (load-to-cores multiple that counts as a breach; default `1.5`) and `host_load_alert_minutes` (minutes a breach must last before one alert; default `10`). Only the arguments you give are sent; the daemon refuses an unknown mode and negative numbers. `autonomous_config_get` returns the `host_load_alert` block. Applies within a minute, with no restart:

  ```
  autonomous_config_set(host_load_alert_mode="reap", host_load_alert_multiple=2, host_load_alert_minutes=15)
  ```
- **`housekeeping_repairs`** — read the housekeeping repairs: per group (`task_chain`, `advance_judge`, `lane_fill`, `leftovers`) the mode in effect (`off`, `report` or `on`), what a report-mode pass would do (`would`), what an on-mode pass did (`did`), and the cases that `failed` or were `skipped_unsafe`; each entry says what was found, what was done and how to undo it. Takes no arguments (one is refused). Read-only: reading never repairs anything, raises an alert or changes a mode. Change a mode with `config_set` and the key `autonomous.housekeeping_repairs.<group>` (`off`, `report` or `on`; default `report`); `autonomous.housekeeping_repairs.grace_seconds` sets the grace (default 300). Returns the same object as `GET /api/autonomous/housekeeping-repairs`.
- **`autonomous_unlanded_work`** — read the list of archived Automaton branches whose commits are not on the project's default branch: `branches[]`, one entry per branch with the Automaton, title, branch, commits not on the default branch, last activity and a recommendation (`landed`, `superseded` or `needs_decision`). Read-only: keeping, archiving a branch and deleting a worktree (which writes a backup bundle first) are REST, CLI, comm channel and PWA actions, not MCP tools. Returns the same object as `GET /api/autonomous/unlanded-work`. See [howto/automaton-worktree-cleanup.md](howto/automaton-worktree-cleanup.md).
- **`autonomous_pending_items`**, **`autonomous_pending_item_add`**, **`autonomous_pending_item_attach`**, **`autonomous_pending_items_refresh`** — pending items: named pieces of work that have no Automaton yet. An Automation carries one as a member (`automation_insert_member` with `pending_item_id`) and holds at it until an Automaton is attached. `autonomous_pending_items` is read-only and returns `policy` and `auto_resume` in effect, `source` (the file registry settings), `last_refresh`, `items` and `held`; it takes optional `state` (`waiting`, `attached` or `done`) and `automation_id`. `autonomous_pending_item_add` takes `title` (**required**) and `ref` and returns the new item. `autonomous_pending_item_attach` takes `item_id` and `automaton_id` (both **required**) and converts the waiting members in place; it is refused with `409` when the item is not waiting and `404` for an unknown item or Automaton. `autonomous_pending_items_refresh` reads the file registry now and returns the counts of rows added, updated, done, dropped, kept and skipped, or the error. They return the same objects as `GET` and `POST /api/autonomous/pending-items`, `.../{id}/attach` and `.../refresh`. The nine `autonomous.automation.pending_item_*` settings are set through `config_set` or `autonomous_config_set` (`automation_pending_item_policy`, `automation_pending_item_auto_resume` and `automation_pending_item_source_path`, `_id_pattern`, `_id_column`, `_title_column`, `_state_column`, `_done_values`, `_on_missing_row`). See [howto/automaton-pending-items.md](howto/automaton-pending-items.md).

  | Parameter | Tool | Required | Description |
  |---|---|---|---|
  | `state` | `autonomous_pending_items` | no | `waiting`, `attached` or `done` |
  | `automation_id` | `autonomous_pending_items` | no | Narrow the items and holds to one Automation |
  | `title` | `autonomous_pending_item_add` | yes | Name of the work; must not be blank |
  | `ref` | `autonomous_pending_item_add` | no | Optional outside label for the item |
  | `item_id` | `autonomous_pending_item_attach` | yes | The pending item id (8 hex characters) |
  | `automaton_id` | `autonomous_pending_item_attach` | yes | The Automaton to attach |

  ```
  autonomous_pending_item_add(title="Write the migration guide", ref="guide-1")
  autonomous_pending_item_attach(item_id="a1b2c3d4", automaton_id="7e41c0a9")
  ```

- **`autonomous_safe_to_delete`** — read the safe-to-delete report for Automaton work branches and worktrees: `safe` lists the `automaton/*` branches that can be cleaned up (state `merged` or `empty`, no uncommitted changes in the worktree; each with `branch`, `worktree`, `reason`, `commits`), `held` lists the ones that must stay (`pending`, `needs_look` or `unknown`, or a worktree with uncommitted changes; each with `branch`, `worktree`, `state` and, when set, `dirty` and `note`) and `held_dirty` counts the branches held only for uncommitted changes. Unlike `autonomous_merge`, the daemon runs git when this is read (bounded at 60 seconds), and it works in every mode including `off`. Read-only: it never deletes a branch, removes a worktree, merges or pushes — cleaning up stays a separate step the operator approves. Returns the same object as `GET /api/autonomous/safe-to-delete`.

  | Parameter | Required | Description |
  |---|---|---|
  | — | — | The tool takes no parameters |

  ```
  autonomous_safe_to_delete()
  ```

  Example result (shortened):

  ```json
  {"safe": [{"branch": "automaton/a1b2c3d4e5f6", "worktree": "", "reason": "all commits on main", "commits": 4}],
   "held": [{"branch": "automaton/d0442737d38c", "worktree": "", "state": "pending"}],
   "held_dirty": 0}
  ```

  Answers a tool error when Automata are disabled or no `worktree_base_repo` is set (the daemon's `503`) or the branches cannot be listed (`500`). Needs the autonomous read capability when called by a federated peer.
- **`autonomous_shipped_record`** — record the shipped state of an Automaton (PRD) or an Automation: `id`, `merge_commit` (required), `merged_in_release`, `merged_at` (RFC 3339), `source` and `automation` (= `true` to address an Automation instead of a PRD, default off). Recording never changes the record's status and is idempotent per merge commit; a different commit overwrites all four shipped fields. `source` must be `merge_step` or `backfill` — the daemon refuses to record with neither (see the note in [autonomous.md](api/autonomous.md#the-shipped-state-autonomousshipped)). Proxies `POST /api/autonomous/prds/{id}/shipped` or `POST /api/automations/{id}/shipped`; needs the autonomous write capability when called by a federated peer.

  | Parameter | Required | Description |
  |---|---|---|
  | `id` | yes | Automaton (PRD) ID, or Automation ID when `automation=true` |
  | `merge_commit` | yes | The merge commit on the default branch — non-empty is what makes the record shipped |
  | `merged_in_release` | no | The release the work landed in, e.g. `v1.2.0` |
  | `merged_at` | no | The merge time, RFC 3339 |
  | `source` | no* | `merge_step` or `backfill` (*the daemon demands one of the two) |
  | `automation` | no | `true` records an Automation instead of a PRD |

  ```
  autonomous_shipped_record(id="a1b2c3d4", merge_commit="9d8e7f6a", merged_in_release="v1.2.0", source="merge_step")
  autonomous_shipped_record(id="auto77", merge_commit="bead1", merged_in_release="v1.2.0", source="backfill", automation=true)
  ```
- **`autonomous_shipped_backfill`** — one shipped-state backfill pass over the base repository's main branch (`MAIN_BRANCH` when set): it finds merge commits naming an `automaton/<8-hex id>` branch and records shipped state (`source=backfill`) on the matching PRDs, and on every Automation whose non-skipped member PRDs are all shipped; it attributes each one to the first release at or after its merge; and it never touches any status. `dry_run=true` scans and computes the report without writing; false or absent (the default) writes. Returns the report — `prds_scanned`, `prds_marked`, `prds_already_marked`, `automations_marked`, `left_cancelled`, `unmatched_ids`, `errors`. Idempotent per merge commit. Proxies `POST /api/autonomous/shipped/backfill`; needs the autonomous write capability when called by a federated peer.

  | Parameter | Required | Description |
  |---|---|---|
  | `dry_run` | no | `true`: scan and report without writing anything; false or absent: the pass writes (default) |

  ```
  autonomous_shipped_backfill(dry_run=true)   # see what would be recorded
  autonomous_shipped_backfill()               # run the pass
  ```
- **`autonomous_shipped_counts`** — read the per-status shipped counts, the PRD and Automation buckets, computed under the in-force `autonomous.shipped` settings: one count per displayed status, with the shipped bucket separated from cancelled while `enabled` + `count_separately` hold (a PRD cancelled without its work landing counts as cancelled; one whose work landed counts as shipped, while the stored status stays cancelled). The reply also carries the settings it was computed under. Read-only — it never writes a record and touches no status; the same numbers are on the dashboard's shipped card. Proxies `GET /api/autonomous/shipped/counts`; needs the autonomous read capability when called by a federated peer.

  | Parameter | Required | Description |
  |---|---|---|
  | — | — | The tool takes no parameters |

  ```
  autonomous_shipped_counts()
  ```
- **`autonomous_prd_set_lane_preference`** — set which lane an Automaton may take: `gpu`, `claude` or `any`. Takes `id` and `lane_preference`; an empty `lane_preference` clears the setting, which means `any`. An unknown value is refused. Proxies `POST /api/autonomous/prds/{id}/set_lane_preference`. Needs the autonomous write capability when called by a federated peer.

  | Parameter | Required | Description |
  |---|---|---|
  | `id` | yes | Automaton ID |
  | `lane_preference` | yes | `gpu`, `claude`, `any`, or empty for `any` |

  ```
  autonomous_prd_set_lane_preference(id="a1b2c3d4", lane_preference="gpu")
  autonomous_prd_set_lane_preference(id="a1b2c3d4", lane_preference="")   # back to any lane
  ```

The lane fill settings are also set through `autonomous_config_set`, under the `lane_fill_*` arguments: `lane_fill_mode` (`off`, `shadow` or `on`), `lane_fill_gpu_lanes`, `lane_fill_claude_lanes`, `lane_fill_idle_alert_minutes`, `lane_fill_decompose_ahead_min`, `lane_fill_poll_seconds`, `lane_fill_gpu_backend`, `lane_fill_gpu_model` and `lane_fill_claude_backend`. Each one sets the matching `autonomous.lane_fill.*` key; arguments left out are not changed.

  ```
  autonomous_config_set(lane_fill_mode="shadow", lane_fill_claude_lanes=3)
  ```
- **`autonomous_prd_instantiate`** (v5.x) — instantiate from a template PRD with variable substitution.
- **`autonomous_prd_rejudge`** — re-judge the PRD-completion "rules-review" guardrail of a **blocked** Automaton (or of a `needs_review` Automaton held by a rules-review retries-spent or auto-fix-disabled hold) whose work is fully done, on an operator's behalf, without resetting or editing any task. Use it when the guardrail verdict that blocked the Automaton deserves a fresh evaluation (new evidence, revised gate settings) but no task needs another attempt. Refused — with a clear error, no side effects on any task — when the Automaton is not `blocked` (a rules-review hold is the one exception, covered in item 3 below), or when any task in any story is not `completed` (the refusal names the first such task and its status; re-run that work with `autonomous_prd_reset_task` first). The identical completion-guardrail invocation the rollup runs is re-executed through the same infra-retry wrapper. Outcome: the outcome is durably recorded on the Automaton's decision log **before** any status change — a new review records a `guardrail_verdict` plus a `guardrail_rejudge` Decision attributed to the acting operator, while a reused stored verdict records only the `guardrail_rejudge` Decision, with `verdict_reused` true, and is not appended to the Automaton's verdicts again; either Decision names the commit judged in `judged_commit_sha` — `pass` clears the blocked reason and completes the Automaton, `block` keeps it `blocked` with the blocked reason set from the new verdict, and no verdict (judge error or empty outcome) keeps it `blocked` with its previous reason and returns an error. Proxies `POST /api/autonomous/prds/{id}/rejudge` (see [howto/autonomous-recovery.md](howto/autonomous-recovery.md)).

  | Parameter | Required | Description |
  |---|---|---|
  | `id` | yes | Automaton (PRD) ID |

  ```
  autonomous_prd_rejudge(id="a1b2c3d4")
  ```

  Returns the write reply described above (a small summary, not the whole Automaton) with a `message` that says whether a new review ran on the named commit or the stored verdict was reused; the guardrail verdict (when a new review ran) and the `guardrail_rejudge` Decision (with `judged_commit_sha` and `verdict_reused`) are in the Automaton's decision log, readable with `autonomous_prd_get`. The REST body `actor` (default `operator`) is fixed to `operator` here, as with the other PRD write tools.

  **How the verdict is chosen (branch head aware).** The rejudge compares the branch head that the stored verdict was judged on with the current head:

  1. **Head commit and rules text unchanged, not held for review:** the stored blocking verdict is reused and no new judge runs, but only while the judge verdict cache is on (`autonomous_config_set` `judge_verdict_cache_enabled`, on by default). The head commit is the real head of the Automaton's working tree. The rules text is a fingerprint of the rules and context text the judge reads, kept on the verdict as `rules_fingerprint`. The judge policy and the guardrail's prompt are part of the match too; changing any of them makes the stored verdict a miss. A stored verdict with no fingerprint is judged again once. The reply message and the `guardrail_rejudge` Decision say plainly that the stored verdict was reused and why (`verdict_reused` true), and no verdict is added. With the cache off, every rejudge runs a fresh judge, whether or not the head moved.
  2. **Head commit moved since the stored verdict, or the rules text or judge inputs changed:** a fresh judge session runs on the configured judge backends against the current head. Its verdict is recorded with its own time and the commit it judged, and the reply message says a new review ran on that commit.
  3. **Held by a rules-review hold** (`needs_review` after the retries were spent, or after auto-fix was turned off): the rejudge is accepted once new commits have landed on the branch, with no `autonomous_prd_cancel`, `autonomous_prd_add_task` or `autonomous_prd_reset_task` needed. The fresh judge runs on the new head.
  4. **Held by a rules-review hold with the head unchanged:** the rejudge is refused before any judge runs, with the error `branch head "<sha>" is unchanged since the held verdict was made; new commits are needed before a rejudge`. Nothing is recorded and the Automaton stays held. Commit the fix on the Automaton branch first, then rejudge.

  The refusal rules above still apply on every path: a rejudge is refused when the Automaton is neither `blocked` nor a rules-review hold, and when any task is not `completed`.
- **`autonomous_prd_set_notify_events`** — set the lifecycle event kinds that produce notifications **for this Automaton**, independent of who is subscribed. Parameters:

  | Parameter | Required | Description |
  |---|---|---|
  | `id` | yes | Automaton ID |
  | `notify_events` | no | Array of event kinds (e.g. `["prd.completed","task.session_started"]`). Empty or omitted clears the Automaton-level set and inherits from the next tier |

  ```
  autonomous_prd_set_notify_events(id="a1b2c3d4", notify_events=["prd.blocked","task.session_started"])
  autonomous_prd_set_notify_events(id="a1b2c3d4")   # clear the Automaton-level set (inherit)
  ```

  Default set (when unset at this tier): `prd.needs_review`, `prd.blocked`, `prd.failed`, `prd.completed`, `prd.cancelled`. The opt-in kinds `task.session_started`, `task.session_cleared` (the session-end kind), `scope_warning` and `guardrail_warn` are off by default — add them to the list to be notified. Warnings never block or change any work. Precedence: **Automaton > guardrail profile > global default**. A subscription's own event filter can only **narrow** this resolved set, never widen it. Returns the write reply described above (a small summary, not the whole Automaton).
- **`autonomous_subscribe` / `autonomous_unsubscribe` / `autonomous_subscriptions`** (BL427) — persisted Automaton lifecycle-event subscriptions (design: [plans/2026-10-13-prd-lifecycle-events.md](plans/2026-10-13-prd-lifecycle-events.md)). Register, list and remove subscriptions so matching lifecycle events (e.g. `task.session_started`, `task.completed`, `prd.blocked`) are delivered to a sink when they fire.

  `autonomous_subscribe` parameters:

  | Parameter | Required | Description |
  |---|---|---|
  | `kinds` | one of `kinds`/`level` | Comma-separated event kinds (also accepts a string array), e.g. `"task.session_started,prd.blocked"` |
  | `level` | one of `kinds`/`level` | Event level: `prd` or `task` |
  | `prd_id` | no | Scope to one Automaton (PRD) id |
  | `task_id` | no | Scope to one task id |
  | `sink_type` | yes | `comm`, `session`, `topic`, `alert`, `mcp_queue`, `webhook` or `peer` (`webhook` and `peer` ship disabled; see below) |
  | `sink_target` | yes | The sink's target (session id, topic, router/alert name, or the `mcp_queue` queue name). For `webhook` it is the `https` URL of the receiver; for `peer` it is the registered federation peer name. |
  | `secret_ref` | yes for `webhook`, refused otherwise | The `${secret:name}` vault reference whose value signs each webhook delivery (HMAC-SHA256). A literal value is refused. The daemon resolves it on every delivery and never returns it. |
  | `action_text` | no | Text typed into the sink session on every match (the `send_input` action — the only action in this phase; requires `sink_type` `session`) |
  | `one_shot` | no | Delete the subscription after its first delivery (default false) |
  | `coalesce` | no | Batch human-facing sink deliveries in quiet windows (default false) |
  | `expires_in_seconds` | no | TTL in seconds; the subscription stops matching after it elapses |

  Returns the stored subscription record (with its `id`). `autonomous_unsubscribe(id)` removes one (`id` — 404 for unknown ids). `autonomous_subscriptions` lists them, optionally filtered by `owner`. Proxies `POST /api/autonomous/subscriptions`, `DELETE /api/autonomous/subscriptions/{id}`, `GET /api/autonomous/subscriptions`. The same surface over CLI is `datawatch autonomous subscribe` / `unsubscribe <id>` / `subscriptions [--owner <owner>]` — the CLI's watch-and-inject shorthand is `datawatch autonomous subscribe --task <id> --on session_started --send "<text>"` (defaults to a one-shot `send_input` action into a session sink).

  Example — a one-shot nudge to a session when its task's session starts:
  ```
  autonomous_subscribe(
    kinds="task.session_started",
    task_id="t7",
    sink_type="session", sink_target="pi-host",
    action_text="Task t7 session started — pick up the build.",
    one_shot=true
  )
  → the stored subscription record (has an id you can pass to autonomous_unsubscribe)
  ```

  Example — a signed webhook (the `webhook` sink ships disabled: enable `autonomous.notify.webhook.enabled` and add the host to `autonomous.notify.webhook.allow_hosts` first; the signing key is stored in the vault as `lifecycle-hook-prod`):
  ```
  autonomous_subscribe(
    kinds="task.completed,task.failed,prd.blocked",
    sink_type="webhook",
    sink_target="https://hooks.example.net/datawatch",
    secret_ref="${secret:lifecycle-hook-prod}"
  )
  ```

  Example — forwarding to a registered federation peer (the `peer` sink ships disabled: enable `autonomous.notify.forwarding.enabled` on both daemons first; a peer subscription carries no secret):
  ```
  autonomous_subscribe(kinds="task.failed,prd.blocked", sink_type="peer", sink_target="hub-east")
  ```

  The webhook and peer rules, the request the receiver gets and the retry rules are in [howto/automaton-lifecycle-webhooks.md](howto/automaton-lifecycle-webhooks.md).
- **`autonomous_subscription_update` / `autonomous_subscription_test`** — change or check an existing lifecycle-event subscription by id.

  `autonomous_subscription_update(id, ...)` takes `id` (required) plus the same optional fields as `autonomous_subscribe` (`kinds`, `level`, `prd_id`, `task_id`, `sink_type`, `sink_target`, `secret_ref`, `action_text`, `one_shot`, `coalesce`, `expires_in_seconds`) and **replaces** the subscription's mutable fields with them: a field left out is cleared, so pass the full desired configuration. The id and creation time are kept. `expires_in_seconds` must be positive when given and is converted to an absolute expiry, as for `autonomous_subscribe`. Returns the updated record. Proxies `PUT /api/autonomous/subscriptions/{id}`; the daemon's validation message comes back for a malformed configuration (400) and an unknown id is a 404.

  `autonomous_subscription_test(id)` delivers one labelled test event to the subscription's sink so the route can be checked without waiting for a real event, and returns `{"ok","sink","error"}`. Proxies `POST /api/autonomous/subscriptions/{id}/test`; 404 for unknown ids.

  The same surface over CLI:
  ```
  datawatch autonomous subscription-update <id> --on <kind> [--level prd|task] [--prd <id>] [--task <id>] --sink <type> --target <t> [--send <text>] [--one-shot] [--coalesce] [--expires <seconds>]
  datawatch autonomous subscription-test <id>
  ```
  `subscription-update` takes the same flags as `subscribe` and replaces the configuration; unlike `subscribe`, `--task` does not imply `--one-shot`. From a comm channel: `autonomous subscription-update <id> key=value...` (keys `kinds`, `level`, `prd`, `task`, `coalesce`, `one_shot`, `expires`; unnamed fields keep their stored value) and `autonomous subscription-test <id>`. A channel can only update or test subscriptions it owns; any other id gets the same not-found answer.

- **`autonomous_events_pending`** — poll the pending lifecycle events of a named queue, for a generic MCP client that cannot receive pushes. Create the queue by subscribing with `autonomous_subscribe` using `sink_type` `mcp_queue` and `sink_target` set to the queue name (letters, digits and `. _ : -`); matching events then accumulate there, bounded per queue (`autonomous.notify.pending_events_max`, default 500).

  | Parameter | Required | Description |
  |---|---|---|
  | `queue` | yes | Queue name (the `mcp_queue` subscription's `sink_target`) |
  | `since_cursor` | no | Return only events after this cursor (default 0) — pass back the previous call's `next_cursor` |
  | `limit` | no | Maximum events (default 50, capped at 200) |

  Returns `{"events":[...],"next_cursor":<n>,"dropped":<bool>}`. Pass `next_cursor` back as `since_cursor` on the next call; `dropped` is true when events you had not yet seen were evicted because the queue overflowed. Proxies `GET /api/autonomous/events`. The CLI equivalent is `datawatch autonomous events --queue <name> [--since <cursor>] [--limit n] [--json]`; over a comm channel it is `autonomous events [queue=<name>] [since=<cursor>]` (the queue defaults to one derived from the calling channel, and a channel can only read queues of its own subscriptions).
- **`observer_envelopes_all_peers`** (v5.12.0, BL180 cross-host) — federation-aware envelope view; cross-peer caller attribution surfaces as `<peer>:<envelope-id>` rows on each matched server envelope.
- **`session_bind_agent`** (v5.x) — bind a manual session to an existing agent record so observer/cost roll-ups attribute correctly.
- **`session_import`** (v5.x) — import a foreign tmux session record into datawatch state without re-spawning.
- **`session_reconcile`** (v5.x) — reconcile post-restart: walk live tmux + persisted state, drop ghosts, re-attach orphans.
- **`session_rollback`** (v5.x) — checkout the per-session pre-run git tag.
- **`sessions_stale`** (v5.x) — list sessions whose tmux pane has gone away but state hasn't been reaped yet.
- **`stop_all_sessions`** (v5.x) — bulk stop with optional state filter.
- **`reload`** (v5.7.0) — `POST /api/reload` shortcut, mirrors the new `datawatch reload` CLI subcommand.
- **`session_children`** (v8.10.0) — list child sessions of a parent session. Returns ID, state, backend, and task for each child.
- **`reply_to_parent`** (v8.10.0) — send a message to the parent session that spawned this one. Sub-agents use this to report completion back to their orchestrator.
- **`start_session` extended** (v8.10.0) — added `caller_session_id` (records parent lineage), `kill_children` (cascade kill opt-in), `permission_mode` (v8.9.24), and `name` parameters.
- **`list_sessions` extended** (BL361) — added `name` (glob filter), `state`, `backend`, `alive`, and `format` parameters.
- **`restart_session`** — restarts a session from any terminal state; accepts `session_id` or `session_name` (BL354 name resolution) and optional `task` override.
- **`exit_hook_list/add/delete/enable/disable`** (BL356) — session crash/exit hooks: fire `restart` or `notify` actions when a named session goes zombie or enters failed/killed state.
- **`queue_push/claim/complete/fail/list`** (BL357) — durable role-based work queue for multi-agent coordination.
- **`discussion_subscribe/unsubscribe/subscriptions`** (BL358) — subscribe sessions to discussions so new entries are forwarded via `send_input`.
- **`result_put/get/list/delete`** (BL360) — structured named-result store for agents to share outputs without out-of-band coordination.
- **`post_result` / `schedule_results_list`` (GH#204) — scheduled-job result panel: post the calling session's structured result (required `title`, `ok`/`warn`/`error` `status`, optional `summary`, `metrics`, `tables`, `markdown`) under the exact same payload contract and validation as `POST /api/sessions/{id}/result`, then read back per-schedule histories. A session belonging to a named schedule has its result appended to that schedule's retained history (`stored: true`); the session's status-board result slot is always updated either way. `schedule_results_list()` returns one summary per schedule (latest result, retained count, recent status sequence for a sparkline); `schedule_results_list(schedule_name=...)` returns the full retained history for one schedule — the same data as `GET /api/schedule-results` and `GET /api/schedule-results/{schedule_name}`.
- **`channel_info`** (BL216) — daemon's resolved MCP bridge kind (go|js), path, ready state, stale .mcp.json detection. Read-only.
- **`channel_diagnostics`** (BL362) — per-session bridge ports, live `/health` probe results, and actionable remediation hints. Use when sessions fail to connect or MCP errors appear with no clear cause.
- **`get_my_scope`** (BL463) — self-scope confirmation for the session calling over its MCP bridge. Read-only (`health:read`), no session argument required — resolution order is `hint` (optional session ID or name) → the in-band caller scope carried by the session's MCP bridge token (HLLM-004) → the first channel-ready session (flagged as such in `resolved_via`). Returns the session's identity and mode ("host" vs "container(agent=<id>)" — F10 sessions), its per-session MCP server name (`datawatch-<FullID>`; the exact `<mcpServers>` key it was registered under in `~/.claude.json` / `.mcp.json`), `shared_entry_name` (the common `datawatch` entry in the per-project `.mcp.json` every host-mode session shares), its project dir + tmux identity, channel readiness, and `peers[]` — every other session's name, state, and MCP server name, so the caller can confirm its own name is distinct from each peer's. Use it to answer "which MCP server is actually mine, and which of these are peers I must not be using?" (the BL467 first-come bridge race, see [plans/2026-10-16-mcp-bridge-registration-race.md](plans/2026-10-16-mcp-bridge-registration-race.md); audit in [plans/2026-10-20-bl463-mcp-drop-findings.md](plans/2026-10-20-bl463-mcp-drop-findings.md)). Same data over REST: `GET /api/sessions/self/scope` (optional `?hint=<id>`).
- **`guardrail_library_list`** (BL303 S2) — list all registered guardrails (built-in scan guardrails + skill-contributed).
- **`guardrail_profile_list/create/get/update/delete`** (BL303 S2) — manage named guardrail profiles (named sets of guardrails that can be applied to sessions or PRDs). `create` and `update` take an optional `notify_events_json` (a JSON array of event kinds, e.g. `["prd.blocked","task.session_started"]`) setting the profile tier's notification event set — the middle tier of the notify_events precedence chain (Automaton > guardrail profile > global default). On create: omitted = default; on update: omitted = unchanged, `"[]"` = clears the profile's list. Invalid JSON is reported as an error before any request is sent.
- **`automation_merged_members`** — read the merged-member decisions of one Automaton, or of every Automaton when no `id` is given. A decision is written when a member of an Automaton ended cancelled or completed and its work branch was found already merged into the project's default branch. The result is `{automation_id, count, merged_members}`, oldest first, each row with `automation_id`, `automation_name`, `member`, `prd_id`, `branch`, `default_branch`, `proving_commit` (the commit that proves the merge), `policy` (`completed`, `manual` or `block`), `outcome` (`advanced` when the chain moved on, `blocked` when the Automaton stayed blocked for the operator) and `at`. It is the same object as `GET /api/automations/{id}/merged-members` (or `GET /api/automations/merged-members` for all). Read-only: nothing is changed, merged or resumed. An unknown `id` is a tool error (the daemon's `404`), as is a daemon with Automata disabled (`503`). Needs the autonomous read capability when called by a federated peer.

  | Parameter | Required | Description |
  |---|---|---|
  | `id` | No | The Automaton ID. Leave out for every Automaton |

  ```
  automation_merged_members(id="4522f306c266")
  ```

  Example result (shortened):

  ```json
  {"automation_id": "4522f306c266", "count": 1,
   "merged_members": [{"automation_id": "4522f306c266", "automation_name": "nightly pipeline", "member": "b", "branch": "automaton/d0442737d38c", "default_branch": "main", "proving_commit": "3f9a1c0e7b2d4a58c6e1f0a9b8d7c6e5f4a3b2c1", "policy": "completed", "outcome": "advanced", "at": "2026-10-30T09:12:44Z"}]}
  ```

  The policy that drives it is set through `autonomous_config_set` with `automation_merged_member_policy` (`completed`, `manual` or `block`; an empty or other value is refused, and leaving the argument out sends nothing):

  ```
  autonomous_config_set(automation_merged_member_policy="manual")
  ```
- **`per_automaton_guardrails_set`** (BL303 S2) — override guardrail profile and per-task/per-story guardrails for a specific PRD, plus the Automaton-completion list (`per_prd_guardrails_json`, e.g. `["rules-review","ci-watch"]`). The call replaces the profile and all three lists, so pass the ones you want to keep.
- **`session_guardrail_run`** (BL303 S3) — run a named guardrail against a session's project directory; appends the verdict to the session's telemetry. Parameters: `session_id`, `name` (guardrail library name, e.g. `sast-scan`).
- **`session_guardrail_approve`** (v8.27.5, GH#153) — approve a single blocked guardrail verdict so the session can proceed. Parameters: `session_id`, `guardrail` (guardrail name in telemetry, required), `note` (optional). Returns `{guardrail, approved, session_unblocked, telemetry}`. Use when a block verdict was a false positive; approval is scoped to the current session run (in-memory only). See also `POST /api/sessions/{id}/guardrail/{name}/approve`.
- **`retry_profile_list/create/get/update/delete`** (BL410 Phase 1) — manage named retry profiles (per-category retry budgets + behavior: ceiling, infra escalation threshold, content auto-fix, auto-fix approval-skip) — mirrors `guardrail_profile_*` exactly.
- **`per_automaton_retry_policy_set`** (BL410 Phase 1) — override retry profile and per-category retry policy for a specific PRD.
- **`automation_list/create/get/update/delete`** — manage Automation records: a named chain of member Automatons (PRDs) connected by per-outcome edges (`on_complete` / `on_blocked` / `on_failed`), with an entry member, an optional budget policy, and Automation-level execution/verification defaults. These tools manage the records. Activating a draft Automation is a separate action (`automation_activate`); from then on the daemon advances it through its members on its own. Tool shape mirrors `retry_profile_*` / `guardrail_profile_*`; same surface over REST at `GET/POST /api/automations` and `GET/PUT/DELETE /api/automations/{id}`.
- **`automation_import_graph`** — convert an orchestrator graph into a draft Automation (`id` = graph ID, optional `dry_run` to preview without storing). The graph is only read. Over REST it is `POST /api/automations/import-graph/{graph_id}`.
- **Deprecated:** `orchestrator_graph_create`, `orchestrator_graph_plan` and `orchestrator_graph_run` still work but their results end with a deprecation notice naming `automation_import_graph`; the daemon logs the warning once per tool per process.
- **`automation_pause`, `automation_resume`, `automation_cancel`, `automation_skip_member`, `automation_insert_member`, `automation_remove_member`, `automation_reorder_edges`, `automation_approve_insert`, `automation_set_budget`, `automation_set_decision_assistant`, `automation_rename_stage`** (Phase 3b) — operator control of a running Automation; see "Automation control tools" below. Over REST these are `POST /api/automations/{id}/{pause|resume|cancel|skip_member|insert_member|remove_member|reorder_edges|approve_insert|set_budget|stages/rename}`. `automation_get` and `automation_list` return each Automation with its computed `budget` view, and `autonomous_status` carries the same `budget` on each Automation row. `automation_get` also returns the record's `pending_inserts` (omitted when none); each entry's `id` is the `member_id` to pass to `automation_approve_insert`.

  Parameters shared by the create/update family (`list` takes none; `get` and `delete` take only `id`; `update` sends only the fields to change):

  | Parameter | Used by | Description |
  |---|---|---|
  | `name` | create, update | Automation name (required on create; blank on update keeps the existing name). |
  | `description` | create, update | Optional free-text description. |
  | `entry_member_id` | create, update | The member ID where the chain starts. |
  | `members_json` | create, update | JSON array of members; each member has: `id` (blank = the daemon assigns one), `on_complete` / `on_blocked` / `on_failed` (a member ID, or `""` = the chain stops there), and `added_reason`. `kind` picks the member's shape: `""` or `prd` (default) names a member Automaton with `prd_id`; `automation` names a nested Automation with `automation_id`; `fork` starts the branch members listed in `fork_members` (at least two) together; `join` collects the branch members listed in `join_members` (the same set as its fork's `fork_members`) and applies `on_branch_failure` (`all_must_succeed`, the default when omitted, or `any_one_is_enough` or `continue_anyway`). Set only the fields that belong to a member's kind. |
  | `budget_policy_json` | create, update | Optional JSON object — budget policy for this Automation. |
  | `project_dir` | create, update | Optional project directory (informational). |
  | `backend` / `model` / `effort` | create, update | Optional execution defaults for the chain. |
  | `verification_backend` / `verification_model` / `verification_effort` | create, update | Optional verification defaults for the chain. |
  | `id` | get, update, delete | Automation ID (required). |
  | `expected_version` | update | The `version` you last read; the update is rejected (`409`) if the record moved since (optimistic concurrency). |
  | `reason` | update | Reason for this update; recorded alongside the change and the operator audit log. |

  The per-Automation auto-tags override is **not** a parameter of these tools: it lives in the `auto_tags` object of the REST body (`POST /api/automations`, `PUT /api/automations/{id}`), in CLI `datawatch automation update <id> --auto-tags on|off` (plus the `--auto-tags-{automation,stage,kind}-template` flags), and in the *Auto tags* section of the Automation's settings in the web app. See the [Auto tags how-to](howto/automata-auto-tags.md).

  Example — create a two-member chain (member 2 follows only when member 1 completes):
  ```
  automation_create(
    name="cve-sweep",
    entry_member_id="m1",
    members_json='[
      {"id":"m1","prd_id":"a1b2c3d4","on_complete":"m2"},
      {"id":"m2","prd_id":"e5f6a7b8","on_complete":""}
    ]',
    backend="claude-code", model="sonnet", effort="medium"
  )
  → 201: the record, stamped with an 8-hex `id` and `version` 1

  automation_get(id="3fa9c2d1")        # status, members, edges, budget policy, defaults
  automation_update(id="3fa9c2d1", expected_version=1, name="cve-sweep v2", reason="renaming")
  automation_delete(id="3fa9c2d1")     # member Automatons are unaffected — the back-reference is cleared
  ```

- **Automation fork/join and nested members (Phase 3a).** `automation_create` and `automation_update` take the new member shapes in `members_json`; no new tool was added. The same member object carries five new keys beside `id`, `prd_id` and the edges: `kind`, `automation_id`, `fork_members`, `join_members` and `on_branch_failure`. A `fork` member starts the branches named in `fork_members` (at least two) together. A `join` member collects the branches named in `join_members` (the same set as its fork's `fork_members`), and its `on_branch_failure` picks the rule: `all_must_succeed` (default when omitted), `any_one_is_enough` or `continue_anyway`. An `automation` member runs a nested Automation named by `automation_id`. Branches carry no outcome edges, and a join carries only `on_complete`. A nested child may belong to one non-terminal Automation at a time, and a cycle through nested members or a fork is a validation error. Invalid shapes return an error that names the member ID.

  ```
  automation_create(
    name="parallel checks",
    entry_member_id="fork1",
    members_json='[
      {"id":"fork1","kind":"fork","fork_members":["lint","tests"]},
      {"id":"lint","prd_id":"a1b2c3d4"},
      {"id":"tests","prd_id":"e5f6a7b8"},
      {"id":"join1","kind":"join","join_members":["lint","tests"],"on_branch_failure":"any_one_is_enough"}
    ]'
  )
  ```

- **Automation control tools (Phase 3b).** The tools below steer a running Automation. Each proxies to `POST /api/automations/{id}/<operation>` (see [api/autonomous.md](api/autonomous.md) for the full semantics, response codes and audit entries). Every tool takes these arguments, and a call that leaves out a required one is refused before any request is sent:

  | Argument | Required | Description |
  |---|---|---|
  | `automation_id` | yes | Automation ID. |
  | `expected_version` | yes | The `version` you read with `automation_get`. It must equal the stored version; a stale value comes back as a conflict error (`409`) and nothing changes. |
  | `actor` | no | Who is acting, recorded in the audit trail. Defaults to `operator`. Use `agent:<id>` or `session:<id>` for an agent. |
  | `reason` | no (see `automation_insert_member`) | Recorded with the audit entry and the Automation's decision log. |

  | Tool | Extra arguments | What it does |
  |---|---|---|
  | `automation_pause` | none | Pauses an `active` Automation. Running members keep running and nothing is ended; nothing new starts until resume. |
  | `automation_resume` | none | Resumes a `paused` Automation and starts the next member. |
  | `automation_cancel` | `cancel_in_flight` (boolean, **required**, no default) | Cancels the Automation. `true` ends the session of every running member. `false` leaves running members alone. Members that had not started are cancelled either way. A missing, `null` or non-boolean value is refused with a message naming `cancel_in_flight=true` and `cancel_in_flight=false`. |
  | `automation_skip_member` | `member_id`, `edge` (`on_complete` default, `on_blocked` or `on_failed`) | Skips an active member and follows that edge. A session the member is running is not ended. In a forked Automation with several active branches, `member_id` is required and must name an active branch; a rejection lists the active branch member IDs. |
  | `automation_insert_member` | `prd_id` or `pending_item_id` (exactly one of the two), `reason` (**required**), `name`, `stage`, `urgent`, `after` | Inserts an Automaton (`prd_id`) or a pending item (`pending_item_id`, see `autonomous_pending_items`) as a member; a pending item member holds the Automation at the cursor until `autonomous_pending_item_attach` gives it an Automaton. `urgent` takes the next slot and `false` appends to the end (or goes after `after`); an urgent insert must leave `after` empty. `stage` is an optional stage label recorded on the inserted member (it shows on the member in `automation_get`; `automation_rename_stage` treats it like any other member stage). On a draft the member lands at once. On an `active`, `paused` or `blocked` Automation it is held as a pending insert until `automation_approve_insert`, and only one urgent insert may be pending at a time (`409` otherwise). |
  | `automation_remove_member` | `member_id` (required) | Removes a member that has not started and has no outgoing edges. The entry member, forks, joins and branches are refused. |
  | `automation_reorder_edges` | `member_id` (required), `on_complete`, `on_blocked`, `on_failed` | Rewires the edges of a member that has not started. Each argument is the member ID the edge now names; an edge left out is cleared, so pass every edge to keep. |
  | `automation_approve_insert` | `member_id` (required) | Promotes a pending insert into the graph. `member_id` is the pending insert's `id` — the one `automation_insert_member` returned in `pending_inserts[].id`, or the same `pending_inserts[].id` you read on `automation_get`. Only the operator may approve. |
  | `automation_set_budget` | `member_id`, `ceiling_type` (`dollar` or `allocation_pct`), `ceiling_value`, `window` (`automation`, `day`, `week` or `month`), `pacing` (`none` or `even`), `on_breach` (`pause` or `block`), `backend`, `allocation_estimate`, `peer_window` (array of peer names), `reset_time`, `reset_weekday`, `reset_day`, `reset_zone`, `estimated_cost_usd` (member only), `clear` | Sets, replaces or clears the budget policy of the Automation, or the override of one member when `member_id` is given. The policy fields replace the whole policy; `clear` removes it and cannot be combined with them. The status is not changed by this call. On a breach the Automation pauses and resumes by itself at the window rollover only when the reset time is known (a window the daemon defines itself, anchored at the declared reset or the calendar default, or a reset time parsed from a rate-limit message); when the reset time is unknown it goes to `blocked` and waits for the operator. An alert is raised either way. `allocation_estimate` is off by default, is always labelled estimated and never pauses or blocks. Another daemon's spend counts toward the ceiling only for peers named in `peer_window`; that is off by default and a shared account is not confirmed automatically. |
  | `automation_set_decision_assistant` | `member_id`, `backend`, `model`, `mode` (`shadow` or `live`), `hooks` (array of `empty_edge`, `skip_member`, `insert_member`, `budget_breach`), `thresholds` (object, hook to a number in (0,1]), `clear` | Sets or clears the optional local-model decision assistant of the Automation, or of one member when `member_id` is given. Off by default. The fields replace the whole setting; `clear`, or an empty `backend`, turns it off. `shadow` (the default) only records what the assistant would have done; `live` acts at or above the hook threshold (default 0.9, `budget_breach` 0.95). The assistant only picks among already-allowed options. The setting comes back as `decision_assistant` on `automation_get`. |
  | `automation_rename_stage` | `from` (**required**), `to` (**required**) | Renames every member stage named `from` to `to` in place, while the Automation is `draft`, `active`, `paused` or `blocked`. Stages are the optional operator label on a member (`stage` on each member in the `automation_get` record) — cosmetic: edges, advance rule and budget never read it. When auto tagging is on, the stage tag of the Automation's members migrates to the new name in place; operator tags are never touched. A missing or empty `from` or `to` is refused before any request goes out (`400`). |

  The global setting `autonomous.automation.agent_insert_policy` (`allow`, the default, or `require_operator`) decides whether an insert by an agent or session into a draft Automation lands at once or is held for `automation_approve_insert`; every insert into a started Automation is held whatever the policy. Set it with `PUT /api/config`, the comm `configure` verb, or `config_set` when `mcp.allow_self_config` is on.

  ```
  automation_get(id="3fa9c2d1")                                    # read version, e.g. 7
  automation_pause(automation_id="3fa9c2d1", expected_version=7, reason="review the plan")
  automation_insert_member(automation_id="3fa9c2d1", expected_version=8, prd_id="c9d0e1f2",
                           urgent=true, reason="tests found a gap")   # held: pending_inserts[0].id
  automation_approve_insert(automation_id="3fa9c2d1", expected_version=9, member_id="<pending id>")
  automation_resume(automation_id="3fa9c2d1", expected_version=10)
  automation_cancel(automation_id="3fa9c2d1", expected_version=11, cancel_in_flight=false)
  ```

- **`usage_backoff_status` / `usage_backoff_recheck` — pattern staleness fields.** The status response carries `patterns_version` (a fingerprint of the effective rate-limit patterns), `patterns_stale` (true when the effective patterns or the claude tool's version text changed since the last operator recheck) and `claude_cli_version` (the claude tool's version text, `unknown` when the tool is absent or its version command fails — which on its own never raises the flag). `usage_backoff_recheck` stores both the pattern fingerprint and the current version text as rechecked, clearing `patterns_stale`. `capacity_status` and `autonomous_status` carry the same three fields. See [howto/automata-capacity.md](howto/automata-capacity.md) (`### Pattern staleness and manual recheck`).
- **Automation model-tier tools: `automation_set_llm` and `automation_set_verification_llm`.** An Automation carries default model settings for the member Automata it runs. These two tools set them; each proxies to `POST /api/automations/{id}/set_llm` or `/set_verification_llm` (see [api/autonomous.md](api/autonomous.md) for the full semantics, response codes and audit entries). Resolution order for the execution backend, model and effort, and for the verification LLM: task → story → Automaton → Automation → global. Backend and model come from the same level; effort resolves independently. Each task run records `executor_source` (`task` | `story` | `prd` | `automation` | `global`) beside `verifier_source`. Planning has its own optional pair, `planning_backend` and `planning_model` (unset means the global planning backend); it is used only when a member is decomposed and never changes the run or verify default. Both tools are allowed while the Automation is `draft`, `active`, `paused` or `blocked`.

  | Argument | Required | Tools | Description |
  |---|---|---|---|
  | `automation_id` | yes | both | Automation ID. |
  | `backend` | no | both | Backend default; empty string clears it. On `automation_set_verification_llm` this is the verification backend. |
  | `model` | no | both | Model default; empty string clears it. |
  | `effort` | no | both | Effort default (`low`, `medium`, `high`, `max`, `quick`, `normal`, `thorough`); empty string clears it. |
  | `planning_backend` | no | `automation_set_llm` only | Planning (decompose) backend; empty string clears it. |
  | `planning_model` | no | `automation_set_llm` only | Planning (decompose) model; empty string clears it. |
  | `expected_version` | no | both | The `version` you read with `automation_get`. Omit it or pass `0` to skip the check; a stale positive value comes back as a conflict error (`409`) and nothing changes. |
  | `actor` | no | both | Who is acting, recorded in the audit trail. Defaults to `operator`. |
  | `reason` | no | both | Recorded with the audit entry. |

  Fields are presence-aware: a field that is left out is kept, and a field passed as an empty string is cleared. A call that names none of the setting fields is refused before any request is sent, with a message listing them.

  ```
  automation_set_llm(automation_id="3fa9c2d1", backend="claude-code", model="claude-opus-5-5", effort="high")
  automation_set_llm(automation_id="3fa9c2d1", planning_backend="pi-prompt", planning_model="qwen3.8:27b")
  automation_set_verification_llm(automation_id="3fa9c2d1", expected_version=7, backend="claude-code", model="claude-sonnet-5-5")
  automation_set_verification_llm(automation_id="3fa9c2d1", model="")   # clear the verification model
  ```

- **`autonomous_status` — `review_needed[]`.** Lists every task held for operator review across all Automata, oldest hold first: `automaton_id`, `automaton_title`, `task_id`, `task_title`, `failure_count`, `last_backend`, `last_model` and `held_at`. It is omitted when nothing is held. Decide a held task with `autonomous_prd_resolve_task_review`.
- **`autonomous_status` — `automations[]` cursor fields (Phase 3a).** Each row keeps its existing fields and adds `active_cursors` and `join_wait`. `active_cursors` is one entry per in-flight member, sorted by `member_id`, with `member_id`, `prd_id` (a `prd` member) or `automation_id` (an `automation` member), and `status`. It is always present and `[]` when idle. `join_wait` is present only while a join waits on its branches, with `join_member_id`, `rule`, `pending_branches` and `failed_branches`. `current_member_id`, `current_prd_id` and `current_prd_title` are still returned and are derived from the first active cursor. Read `active_cursors` and `join_wait` to follow a fork/join Automation. See [docs/api/autonomous.md](api/autonomous.md) for the full semantics.

- **`autonomous_config_get` / `autonomous_config_set` — completion guardrails + CI watch.** `per_prd_guardrails` (array, default empty) is the daemon-wide list of guardrails run once when an Automaton completes; valid names are `rules-review` and `ci-watch`, both off unless listed (an Automaton's own list or its guardrail profile overrides it). `ci_watch_timeout_seconds` (number, default `1500`; `0` = default) is how long `ci-watch` waits for the CI runs of the commit the Automaton pushed; runs still unfinished at the deadline leave the Automaton completed with `ci_watch.status` `"pending"`. Same keys over REST/CLI/comm as `autonomous.per_prd_guardrails` / `autonomous.ci_watch_timeout_seconds`. See [howto/ci-watch-guardrail.md](howto/ci-watch-guardrail.md).
- **`autonomous_config_get` / `autonomous_config_set` — judge routing.** `judge_policy` (array, default empty) is the ordered list of LLM backends the judges (Automaton guardrail, plan review and other autonomous judges) try, one attempt per backend; empty means the judges follow the verifier routing policy. `judge_timeout_seconds` (number, default `1800`, i.e. 30 minutes) is the time limit for one judge run and does not count the wait for a capacity slot; `0` or a negative value means the default. A judge moves to the next backend when its run hits the limit; when every backend has timed out the Automaton is held in `needs_review` and an error alert lists every attempt. `judge_size_threshold_lines` (number, default `400`) is the changed-line count (added plus deleted, on the Automaton branch against its merge base, lock files left out) above which the judges try `claude-code` first and the rest follow in their usual order, still one attempt per backend; in `balance` mode the load still decides and the order only breaks ties; `0` turns the rule off and a negative value is refused. `judge_policy` replaces the whole list, so read it with `autonomous_config_get` first. Same keys over REST/CLI/comm as `autonomous.judge_policy` / `autonomous.judge_timeout_seconds` / `autonomous.judge_size_threshold_lines`. `judge_prompt_file_threshold_lines` (default 300; 0 turns the file route off) and `judge_delivery_wait_per_thousand_lines_seconds` (default 60) set how a long judge prompt is delivered — see `docs/howto/automation-tab.md`.
- **`autonomous_config_get` / `autonomous_config_set` — verifier run cap.** `verifier_max_run_seconds` (whole seconds; default `2700` = 45 minutes; `0` = no cap; a negative or fractional value is refused) is the hard total run time of one verifier session. Run time is counted from the verifier's capacity grant, so time spent waiting for a slot does not count. A session the cap ends is retried once on the next backend in the verifier policy order and is not a failed task. `autonomous_config_set` takes the key; `autonomous_config_get` does not return it (it proxies `/api/autonomous/config`), so read the current value from `GET /api/config`. Same key over REST/CLI/comm/YAML as `autonomous.verifier_max_run_seconds`. See [howto/automaton-verifier-run-cap.md](howto/automaton-verifier-run-cap.md).
- **`autonomous_config_get` / `autonomous_config_set` — `block_on_file_drift`.** Boolean, default `false`. Daemon-wide default for the foreign-file-drift check that runs after every direct-mode (non-worktree) task: `false` only flags a task whose own commit(s) swept in files that were already uncommitted (modified, staged or untracked) before it started and are not in its planned `files`; `true` sets that task to blocked (`error: "foreign_file_drift: <files>"`) and halts the walk. `autonomous_config_set` forwards it only when supplied. Same key over REST/CLI/comm/YAML as `autonomous.block_on_file_drift`; PWA Settings → Automate → Block on file drift. See [howto/file-drift-guardrail.md](howto/file-drift-guardrail.md).
- **`autonomous_config_get` / `autonomous_config_set` — plan-requirements coverage gate filters.** `plan_coverage_suppress_created_files` (boolean, default on — a requirement that appears only on the "create Y" side of an "X or create Y" alternative does not count as an unmatched requirement; pass `false` to count every numbered requirement). `plan_coverage_scoped_requirements` (boolean, default on — only the numbered requirements inside the configured sections count toward the gate; pass `false` to count numbered requirements across the whole spec). `plan_coverage_requirement_sections` (string, default `scope,constraints` — comma-separated spec section names scanned for numbered requirements; names are trimmed and lowercased, an empty value restores that default). `plan_coverage_min_requirement_chars` (number, default `30` — the minimum characters a numbered line needs to be treated as a requirement; `0` restores the 30-character default and a negative value is refused). `autonomous_config_set` forwards each key only when its argument is present — an explicit `false` is sent (an off is a meaningful setting), while an omitted argument changes nothing. Same keys over REST/CLI/comm as `autonomous.plan_coverage_suppress_created_files` / `autonomous.plan_coverage_scoped_requirements` / `autonomous.plan_coverage_requirement_sections` / `autonomous.plan_coverage_min_requirement_chars`.
- **`autonomous_config_get` / `autonomous_config_set` — `scope_lint`.** String, one of `off`, `warn` or `enforce`, default `enforce`. Sets the pre-spawn scope lint: `off` skips it, `warn` records problems as a `scope_lint_warn` decision and spawns anyway, `enforce` blocks the task. Placeholder and quoted pattern text is never flagged. `autonomous_config_set` forwards it only when supplied, and the tool schema declares the three values as an enum. Same key over REST/CLI/comm/YAML as `autonomous.scope_lint`; PWA Settings → Automate → Task scope lint. See [operations.md](operations.md#task-scope-lint).
- **`autonomous_config_get` / `autonomous_config_set` — `verifier_min_free_mem_mb` and `verifier_headroom_max_age_hours`.** The two verifier headroom gates, which read one shared measurement store. `verifier_min_free_mem_mb` is the free GPU memory (MB) a node must keep with extra verifier slots running; unset reads 1024, `0` turns the check off. `verifier_headroom_max_age_hours` is how old a measurement may be and still count as fresh; `0` or unset reads 24. Negative values are refused: over MCP the tool returns an error, over REST (`PUT /api/config`) a 400. Same keys over CLI (`datawatch config set`) and comm (`configure`). A node's `verifier_slots` raised above 1 is saved only when a fresh measurement supports it, otherwise `compute_node_update` returns 409 naming the node, the measured free memory and the shortfall; a save that leaves `verifier_slots` at its stored value is not gated. The daemon writes the measurement itself: while a node is idle and its configured cap is above 1, it takes one measurement about every five minutes, sending one small probe request to the node unless the observer reports a per-request figure.
- **`autonomous_config_get` / `autonomous_config_set` — `max_concurrent_tasks`.** Integer, default `1`. Daemon-wide default for how many independent tasks of one Automaton run at once; an Automaton's own setting (`max_concurrent_tasks` on the Automaton) overrides it, and `1` means sequential. `autonomous_config_set` forwards it only when supplied and `>= 1`. Same key over REST (`PUT /api/config` `autonomous.max_concurrent_tasks`, `PUT /api/autonomous/config`), CLI (`datawatch autonomous config-set '{"max_concurrent_tasks":2}'`), comm (`configure autonomous.max_concurrent_tasks=2`) and YAML. `max_parallel_tasks` is **deprecated**: it is accepted and ignored (it never had an effect), and the response carries a `deprecated_fields` note naming `max_concurrent_tasks`; it will be removed in a future release.
- **`autonomous_config_get` / `autonomous_config_set` — `task_override_statuses`.** Array of Automaton status names (any of `draft`, `planning`, `needs_review`, `approved`, `revisions_asked`, `active`, `running`, `completed`, `rejected`, `cancelled`, `archived`, `blocked`, `failed`, `paused`); default `["needs_review","revisions_asked","cancelled","blocked"]`; an empty array restores the default. Controls which statuses accept task-level and story-level LLM override calls (`autonomous_prd_set_task_llm`, story-level LLM, per-task guardrail / verification LLM); Automaton-level overrides are not affected. Same key over REST (`PUT /api/config` `autonomous.task_override_statuses`), CLI (`datawatch config set autonomous.task_override_statuses blocked,running`), comm channel (`configure autonomous.task_override_statuses=blocked,running`) and YAML. Unknown status names are rejected with the list of known names.
- **`autonomous_config_get` / `autonomous_config_set` — `override_queued_tasks`.** Boolean; unset reads as `true` (default), explicit `false` restores the classic refusal. When on, a task-level / story-level LLM override call that `task_override_statuses` would refuse — most often a PRD that is `running` while the target task has not started yet — is still accepted and applied to the unstarted tasks only: started, finished or parked tasks are reported as left alone with the reason, and a `waiting_capacity` task's queued capacity request is retargeted to the new pool (`capacity_moved: true`). Same key over REST (`PUT /api/config` `autonomous.override_queued_tasks`), CLI (`datawatch config set autonomous.override_queued_tasks false`), comm channel (`configure autonomous.override_queued_tasks=false`) and YAML. Never affects guardrail / verification LLM setters.
- **`autonomous_config_get` / `autonomous_config_set` — `undeclared_edit_mode`.** String, `off`|`warn`|`block`, default `warn`. Daemon-wide enforcement for the undeclared-edit check that runs after every direct-mode task: it flags files the task's own worker committed that are in neither its planned `files` nor its generated files (pre-existing dirty files stay under `drift_mode`; a file is never reported by both). `off` = no-op; `warn` = record `undeclared_edits` on the task, an `undeclared_edit_detected` decision, an audit event and a warn alert, task completes; `block` = task blocked with `undeclared_edit: <files>` and the walk halts. `autonomous_config_set` forwards it only when supplied; invalid values are rejected. Same key over REST (`PUT /api/config` `autonomous.undeclared_edit_mode`), CLI (`datawatch config set autonomous.undeclared_edit_mode warn`), comm (`configure autonomous.undeclared_edit_mode=warn`) and YAML; PWA Settings → Autonomous Automata planning → Undeclared edit mode. See [howto/file-drift-guardrail.md](howto/file-drift-guardrail.md).
- **`autonomous_config_get` / `autonomous_config_set` — `gate_disagreement`.** String, `warn`|`block`, default `warn`. What happens when a task's own verifier passed but the quality gate confirmed a regression from a valid baseline: `warn` records `gate_disagreement` on the task and in the decision log and the task completes; `block` fails the task with a `gate_disagreement: …` error naming the new failing tests. An invalid gate (a baseline that could not produce usable results, reported as `quality_gate_status: "invalid"` on the Automaton) never blocks under either value. `autonomous_config_set` forwards it only when supplied; any other value is rejected. Same key over REST (`PUT /api/config` `autonomous.gate_disagreement`), CLI (`datawatch autonomous config-set '{"gate_disagreement":"block"}'`) and comm (`configure autonomous.gate_disagreement=block`). See `docs/operations.md` → "Quality gate".
- **`autonomous_config_get` / `autonomous_config_set` — `descendant_stall_seconds`.** Number, default `2700` (45 minutes); `0` or a negative value is normalized to the default, so the detector can never be silently disabled. Bound on one pi-prompt autonomous worker, in seconds: a run whose runner has exited but whose work descendant (a build, a test, a long command) is still alive, with no new log write and no new workdir file, exceeds this bound and the attempt ends as an infra failure — the recorded descendants are cleaned up (5-second grace, a real kernel signal, only where the recorded identity still verifies), the session ends, and the task is retried through the `auto_fix_retries` budget. `autonomous_config_set` forwards it only when supplied; it is read back in `autonomous_config_get`. Same key over REST (`PUT /api/config` `autonomous.descendant_stall_seconds`), CLI (`datawatch config set autonomous.descendant_stall_seconds 2700`), comm channel (`configure autonomous.descendant_stall_seconds=2700`) and YAML. See [operations.md](operations.md#descendant-only-stall-bound-autonomousdescendant_stall_seconds).
- **`autonomous_config_get` / `autonomous_config_set` — attempt cost.** Three keys, read back in `autonomous_config_get` and set with the same-named arguments of `autonomous_config_set`: `first_attempt` (object mapping a task kind — `implementation`, `docs`, `note` or `final_gate` — to `{backend, model}`; the object replaces the whole map, and `pi`, `pi-prompt` and `ollama` need a model), `first_attempt_escalate_after_failures` (whole number 0 to 20, default 1) and `retry_resume_max_context_tokens` (whole number 0 to 2000000, default 150000; 0 = every retry starts a fresh session). Every set is checked with the shared attempt-cost validator before anything is written: a refused value returns the validator's message, which names the key, and writes nothing. Same keys over REST (`PUT /api/config`), CLI, comm channel (`configure`) and YAML.
- **`autonomous_status`** — each `review_needed` row carries the attempt record of its last attempt next to `last_backend` and `last_model`: `last_backend_reason` (why that backend was chosen), `last_resumed` (true when the retry resumed the previous session), `last_resume_reason` and `no_retry_reason` (why the task went to review without another attempt). The reason and resume fields are omitted when not recorded.
- **`autonomous_config_get` / `autonomous_config_set` — `quality_gates_scope` + `quality_gates_smoke_packages`.** The gate scope: how much of the test suite a gate task runs. `quality_gates_scope` is a string, `full`|`touched-plus-smoke`|`touched`: `full` (the built-in default) runs the configured command unchanged; `touched-plus-smoke` runs the touched packages plus their one-level importers plus the smoke set; `touched` the same without the smoke set. `quality_gates_smoke_packages` is a string (comma-separated or JSON array) of Go package patterns; empty = the built-in list (`internal/config` `DefaultGateSmokePackages`, the proposal to confirm in `docs/plans/2026-10-23-quality-gate-scope.md`). `autonomous_config_set` forwards each only when supplied; a scope value other than the three is rejected. Both land in `autonomous.default_quality_gates.{scope,smoke_packages}`; `autonomous_config_get` returns them. Same keys over REST (`PUT /api/config`), CLI (`config-set`), comm (`configure autonomous.default_quality_gates.scope=…` / `…smoke_packages=a,b`), YAML and the PWA Settings quality-gate card. See `docs/operations.md` → "Gate scope" and [howto/quality-gate-scope.md](howto/quality-gate-scope.md).
- **`autonomous_prd_set_quality_gates` — `scope` + `smoke_packages`.** The per-Automaton gate-scope override with the same three values / pattern list (both optional, forwarded only when supplied; an invalid scope is an error). Resolution order: this Automaton → global → `full`; an empty scope inherits the global then default, an empty smoke list inherits the global then the built-in set. `autonomous_prd_get` returns the override under `quality_gates`. Comm: `autonomous set-quality-gates <id> [scope=…] [smoke_packages=a,b]`; REST: `POST /api/autonomous/prds/{id}/set_quality_gates`. No per-Automaton CLI flag.
- **`autonomous_prd_get` / `autonomous_status` — `quality_gate_result.scope_plan` task field.** Every task that ran a quality gate carries the recorded scope decision: `{scope, scope_source (automaton|global|default), full, full_reason, packages[], reasons[\{package,reason\}], changed_files[], changed_files_total, warnings[], fallback_reason, nothing_to_run, vet_failures[]}` (type `GateScopePlan`, `internal/pipeline/gate_scope.go`). `full: true` marks a full-command run — final gate tasks always run full whatever the scope is (`full_reason` names it); `packages[]`/`reasons[]` name what a scoped run executed (reasons `changed`, `imports <pkg>`, `smoke set`); `fallback_reason` explains a scoped-to-full fallback (module file changed, changed file outside any Go package, unrewritable command, mapping failure). Honesty at every scope: the baseline and the run are restricted to the same package set, new `go vet` findings on those packages count as regressions (pre-existing ones do not), and an invalid run still warns rather than blocks.
- **`guardrail_profile_create` / `guardrail_profile_update` / `per_automaton_guardrails_set` — `block_on_file_drift`.** Optional boolean on all three. Enforcement mode is daemon-wide: `autonomous.drift_mode` (`block` = the default, or `warn`). Under `block`, an explicit `block_on_file_drift: false` on a profile (for its Automata) or on `per_automaton_guardrails_set` (that Automaton itself) opts that scope out of blocking — its findings become warn-only; unset means inherit the mode. The daemon-wide flag's `false` default is the legacy opt-in shape and by itself is not an opt-out. Worktree-mode Automata are never checked.
- **`autonomous_prd_get` — `foreign_file_drift` task field.** Each task may carry `foreign_file_drift` (string array, absent when empty): the files its own commit(s) in `<pre-task SHA>..HEAD` touched that were already uncommitted before the worker session started and are not in the task's planned `files`. Findings accumulate across retries. Each finding also appends a `file_drift_detected` entry to the Automaton's `decisions` (actor `autonomous`, `note` lists the files) and writes an `autonomous_file_drift_detected` audit event. Nothing in the repository is reverted or rewritten. See [howto/file-drift-guardrail.md](howto/file-drift-guardrail.md).
- **`autonomous_prd_get` — `ci_watch` field.** When the `ci-watch` completion guardrail ran, the Automaton includes `ci_watch {status, sha, repo, runs[], summary, category, issues, checked_at}`; absent when it never ran. `status`: `skipped` (nothing pushed, origin not GitHub, not a git repo, or `gh` missing; `summary` says which), `pass`, `warn` (judge flagged a non-blocking concern), `pending` (CI unfinished at the timeout; one follow-up check rewrites it later), `failed_policy` (project rules say an operator decides; Automaton blocked, nothing auto-fixed), `failed_content` (fixable finding; a corrective task is appended to the same Automaton when content auto-fix is enabled, otherwise blocked), `failed_infra` (runs cancelled/timed out/never started and the infra retry budget is spent; blocked), `error` (no judge configured; not blocking). `runs[]` items: `id`, `name`, `status`, `conclusion`, `url`. See [howto/ci-watch-guardrail.md](howto/ci-watch-guardrail.md).



### `trace_session`, `trace_automaton`, `trace_rollup`, `trace_coverage`, `trace_stats`

Read-only views of the agent trace the daemon records (tool calls, results, model requests, state changes, capability and permission decisions, loop warnings, errors). Each proxies the matching `GET /api/trace/*` route; event text is redacted when it is stored.

| Tool | Parameters | What it returns |
|------|-----------|-----------------|
| `trace_session` | `session` (required); optional `tool`, `backend`, `kind`, `errors`, `limit`, `loop_threshold` | The session's events in order, its roll-up and index entry |
| `trace_automaton` | `automaton` (required); same optional filters | Events of every session that ran for the Automaton, plus its roll-up |
| `trace_rollup` | optional `dimension` (session, task, story, automaton, agent, backend), `key`, `loop_threshold` | Roll-ups: tool counts, slowest tools, error rate, loop warnings, capability denials, model requests and tokens |
| `trace_coverage` | optional `backend` | Per-session coverage (full, partial, events-only, unknown) and counts per backend |
| `trace_stats` | none | Store totals (sessions, events, error events, last event time) and write counters since the daemon started (appended, failed writes, pruned); same data as `GET /api/trace/stats` |

The `tracing.*` settings are changed with `config_set` and read with `get_config`.

### `cost_report`

Cost report over the daemon's recorded session spend: the `internal/costreport` engine, the same engine call `GET /api/cost/report` makes, with the daemon's `cost_report` config block as the source of every default. Grouped by automaton, automation, backend or week (the `group_by=release` grouping is REST/CLI-only — it needs shipped-unit state this tool does not expose, and is rejected with a clear message); token totals always, USD estimates when rates are configured, and — where the owning provider has a `usage.billing` profile — the billing label (a bundled plan reads “bundled plan — API-equivalent, not billed”) with the point figures. Read-only.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `group_by` | string | No | One of `automaton`, `automation`, `backend`, `week`. `release` is rejected by this tool (it is REST/CLI-only — it needs shipped-unit state the tool does not expose). Defaults to the `cost_report` config (`default_group_by`). |
| `since` | string | No | Lower bound, RFC3339 or `YYYY-MM-DD`. Defaults to the `cost_report` config window (`default_window_days`). |
| `until` | string | No | Upper bound, RFC3339 or `YYYY-MM-DD`. Defaults to now. |

**Example response:**
```json
{
  "group_by": "backend",
  "rows": [
    {"key": "claude-code", "sessions": 3, "tokens_in": 120000, "tokens_out": 24000, "rate_configured": true, "usd_estimate": 0.78,
     "billing_mode": "bundled_plan", "billing_label": "bundled plan — API-equivalent, not billed",
     "api_equivalent_usd": 0.78, "estimated_usage_points": 3.9, "share_of_monthly_price": 0.008,
     "unit_totals": [{"unit": "points", "billing_mode": "bundled_plan", "sessions": 3, "tokens_in": 120000, "tokens_out": 24000,
                      "active_seconds": 5400, "points": 3.9, "percent": 0.78}]},
    {"key": "opencode", "sessions": 1, "tokens_in": 50000, "tokens_out": 9000, "rate_configured": true,
     "billing_mode": "free_local", "billing_label": "free local inference — not billed, token usage only",
     "unit_totals": [{"unit": "none", "billing_mode": "free_local", "sessions": 1, "tokens_in": 50000, "tokens_out": 9000,
                      "active_seconds": 1200}]}
  ],
  "totals_by_unit": [
    {"unit": "points", "billing_mode": "bundled_plan", "sessions": 3, "tokens_in": 120000, "tokens_out": 24000,
     "active_seconds": 5400, "points": 3.9, "percent": 0.78},
    {"unit": "none", "billing_mode": "free_local", "sessions": 1, "tokens_in": 50000, "tokens_out": 9000,
     "active_seconds": 1200}
  ],
  "retention_days": 31,
  "lifetime_fallback": false,
  "retention_note": "Figures rest on the last 31 days of recorded sessions."
}
```

Rows are sorted by key. `rate_configured` is `false` when at least one session in the row has no rate (the row then has no `usd_estimate`). `billing_mode` and `billing_label` are read from each backend's LLM entry `billing_mode` (`api` → `api_metered`, `allowance` → `bundled_plan`, `local` → `free_local`), then from its `usage.billing` profile — a row with neither reads mode `api_metered` with the "(assumed — no usage.billing entry)" label. A row whose backends are billed in different units reads `mixed` ("mixed billing modes — see the per-unit totals") and has no `usd_estimate`: dollars are never added to points. `unit_totals` (per row) and `totals_by_unit` (whole report) give one total per unit — `dollars` for `api`, `points` and `percent` for `allowance`, tokens and time only for `local` — each with `sessions`, `tokens_in`, `tokens_out` and `active_seconds`. A `free_local` row carries no `usd_estimate` at all (nothing to bill and no rate to price by — the `rate_configured: true` above is why it isn't omitted). `api_equivalent_usd`, `estimated_usage_points` and `share_of_monthly_price` appear only on bundled rows whose profile carries a `monthly_price`. `units` only in `release` rows (REST/CLI only).

---

### `cost_charts`

Read-only. The combined usage report: cost over time (`GET /api/cost/series`), tool usage (`GET /api/cost/tool-usage`) and retries and failure categories (`GET /api/cost/failures`) joined into one payload. It reads the three REST routes through the loopback and returns each body unchanged under `cost_over_time`, `tool_usage` and `failures`, so it needs the REST loopback. Tool usage carries a count per group, never a tool name. Capability: `analytics:read`.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `group_by` | string | No | One of `automaton`, `project`, `model`, `automation`. Cost over time and tool usage group by `automaton`, `project` or `model`; retries and failures group by `project`, `automation` or `automaton`. A grouping a view does not know leaves that view on its default and is named in `notes`. |
| `window` | string | No | How far back from now: `36h`, `7d`, `2w`. Defaults to the `cost_report` default window (twelve weeks for retries and failures). |

**Example response:**
```json
{
  "group_by": "project",
  "window": "7d",
  "cost_over_time": {"group_by": "project", "bucket": "day", "groups": [{"key": "datawatch", "tokens": 4500}]},
  "tool_usage": {"group_by": "project", "groups": [{"key": "datawatch", "tool_calls": 31}], "total_tool_calls": 31},
  "failures": {"group_by": "project", "groups": [{"key": "datawatch", "attempts": 6, "tasks": 3, "retries": 5}], "total_retries": 5}
}
```

The same report is available from the CLI (`datawatch cost charts`) and the comm channel (`cost charts`).

### `list_sessions`

List all AI coding sessions on this host, including their state and task description.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | No | BL361 — glob pattern to filter sessions by name (e.g. `worker-*`, `agent-?`). Empty = all. |
| `state` | string | No | BL361 — filter by exact state: `running`, `waiting_input`, `rate_limited`, `complete`, `failed`, `killed`. Empty = all. |
| `backend` | string | No | BL361 — filter by backend family name (e.g. `opencode`, `claudecode`). Empty = all. The session's own backend — not an axis shortcut; to filter the backend placement axis, pass `backend:<value>` in `tags`. |
| `alive` | string | No | BL361 — filter by `claude_alive` field: `true`, `false`, or `any` (default). Empty = any. |
| `format` | string | No | BL361 — output format: `text` (default) or `json`. `json` returns a structured JSON array. |
| `orphaned` | boolean | No | BL350 — when `true`, return only sessions whose `parent_id` points to a session that no longer exists (orphaned by parent death or eviction). |
| `tags` | array of strings | No | Count only sessions that carry at least one of the tags — their own tags or the tags inherited from the Automaton that started them (the auto tags of the Automation, stage and kind) |
| `tag_match` | string | No | With several `tags`: `any` (default) needs at least one, `all` needs every tag named |
| `project` | string | No | Axis shortcut — expanded to the `project:<value>` tag filter (a session's own, system, and inherited tags all count). Empty = all. |
| `automaton` | string | No | Axis shortcut — expanded to the `automaton:<value>` tag filter. Empty = all. |
| `automation` | string | No | Axis shortcut — expanded to the `automation:<value>` tag filter. Empty = all. |
| `stage` | string | No | Axis shortcut — expanded to the `stage:<value>` tag filter. Empty = all. |
| `kind` | string | No | Axis shortcut — expanded to the `kind:<value>` tag filter. Empty = all. |
| `role` | string | No | Axis shortcut — expanded to the `role:<value>` tag filter. Empty = all. |
| `lane` | string | No | Axis shortcut — expanded to the `lane:<value>` tag filter. Empty = all. |
| `compute` | string | No | Axis shortcut — expanded to the `compute:<value>` tag filter (the compute node the work was placed on). Empty = all. |

**Example response:**
```
Sessions on my-server:

ID:      a3f2
State:   running
Task:    write unit tests for the auth package
Dir:     /home/<user>/myproject
Updated: 2026-03-26T14:32:01Z

ID:      b7c1
State:   waiting_input
Task:    add Docker support
Dir:     /home/<user>/myproject
Updated: 2026-03-26T14:45:22Z
Prompt:  Overwrite existing Dockerfile? [y/N]
```

---

### `start_session`

Start a new AI coding session.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `task` | string | Yes | Task description to send to the AI |
| `project_dir` | string | No | Absolute path to the project directory. Defaults to `session.default_project_dir` from config |
| `llm` | string | No | LLM registry name to use for this session (e.g. `"ollama"`, `"claude"`). Overrides daemon default. |
| `compute_node` | string | No | ComputeNode registry name. Requires `llm` to be set. |
| `name` | string | No | Human-readable session name (used by `send_input` name resolution). |
| `permission_mode` | string | No | Permission mode for this session: `default`, `plan`, `acceptEdits`, `auto`, `bypassPermissions`, `dontAsk`. Empty uses daemon config default. |
| `caller_session_id` | string | No | Full ID (`hostname-hex`) of the session spawning this one. Records parent-child lineage. Agents should pass `$CLAUDE_SESSION_ID`. |
| `kill_children` | boolean | No | When `true`, killing this session also kills all child sessions it spawns. Default: `false` (children survive independently). |
| `image_paths` | string[] | No | Local file paths to images (jpeg/png/webp/gif). Each image is described by the vision service and prepended to the task as `[image: ...]`. Requires `vision.enabled: true`. |

**Example response:**
```
Session started.
ID:      a3f2
Task:    write unit tests for the auth package
Dir:     /home/<user>/myproject
Tmux:    cs-myserver-a3f2

Use session_output(id="a3f2") to follow progress.
```

---

### `session_output`

Get the last N lines of output from a session.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `session_id` | string | Yes | Session ID (4-char short form or full `hostname-id`) |
| `lines` | number | No | Number of lines to return. Default: 50 |

**Example response:**
```
[a3f2] State: waiting_input | Task: write unit tests for the auth package
---
Waiting for input: Found 3 test files. Overwrite? [y/N]
Use send_input(session_id="a3f2", text=...) to respond.
---
  Creating auth_test.go...
  Writing 14 test cases...
  Found existing file: auth_test.go
  Overwrite? [y/N]
```

---

### `send_input`

Send a reply to a session waiting for input.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `session_id` | string | Yes | Session ID |
| `text` | string | Yes | Text to send as input |
| `image_paths` | string[] | No | Local file paths to images. Each is described by the vision service and prepended to the text as `[image: ...]`. Requires `vision.enabled: true`. |

**Example response:**
```
Input sent to session a3f2.
```

---

### `platform_context_status`

Read the platform context state for one session: `enabled` (effective for that session), `project_override` (the project's own value, null when none), `text_version` and `delivery` (`rules_file`, `first_prompt` or `rules_file_and_first_prompt`).

| Parameter | Required | Description |
|-----------|----------|-------------|
| `hint` | no | Session id (short or full). Without it the call reports for the calling session. |

Read-only; an unknown session is a tool error.

```
platform_context_status(hint="a1b2")
```

returns

```json
{"session": "host-a1b2", "enabled": true, "project_override": null, "text_version": 1, "delivery": "rules_file"}
```

### `worker_profile_status`

Read-only. Returns the built-in worker profile names (`minimal`, `digest`, `full`, `datawatch-tools`), the built-in default profile for each role (`planner`, `worker`, `verifier`, `judge`) and task kind (`implementation`, `docs`, `note`, `final_gate`), and the latest average start-up token size per profile and per role from the stats block (`worker_startup` in `GET /api/stats`). The `in_force` field lists one entry per worker role and task kind (`scope`, `key`, `profile`, `tier`, `reason`) with the profile each starts with now, built from the live config (`GET /api/config`) by the same rule a launch uses. Takes no parameters.

Configure profiles with `config_set` on `autonomous.worker_profiles`, `autonomous.worker_profile_defaults` and `autonomous.worker_profile`; read them back with `get_config section=autonomous`. A refused value changes nothing.

### `autonomous_prd_set_worker_profile`

Set the worker profile an Automaton's sessions start with. Parameters: `id` (required, the Automaton ID) and `worker_profile` (required; a built-in or user-defined profile name, empty to clear the override). Posts to `POST /api/autonomous/prds/{id}/set_worker_profile`, which refuses a name that is neither built in nor in `autonomous.worker_profiles`; a refused call changes nothing.

### `audit_access_query`

Query the HTTP access / WebSocket lifecycle / auth-failure log (GH#201). Newest-first. Never returns a token or the Authorization header — only request metadata and the resolved principal.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `actor` | string | No | Filter by resolved principal: `admin`, `session-scoped`, `peer:<name>`, `proxy:<name>`, `unauthenticated` |
| `action` | string | No | Filter by event type: `http_access`, `auth_failure`, `ws_connect`, `ws_disconnect` |
| `since` | string | No | RFC3339 lower bound |
| `until` | string | No | RFC3339 upper bound |
| `limit` | number | No | Max entries (default 100) |

**Example — auth failures in the last day:**
```
audit_access_query(action="auth_failure", since="2026-10-08T00:00:00Z")
```
```json
{"count":1,"entries":[{"ts":"2026-10-09T02:14:03Z","actor":"unauthenticated","action":"auth_failure","details":{"method":"GET","path":"/api/sessions","status":401,"remote_ip":"198.51.100.4","user_agent":"curl/8.4.0"}}]}
```

---

### `self_config_audit`

Query the **self-config audit log**. One record per attempt to change the daemon's own non-secret settings through an agent surface: a config-write MCP tool (`surface: "mcp"`, e.g. `config_set`, `autonomous_config_set`), a session- or peer-origin REST config PUT (`"rest"`), or a comm `configure` command (`"comm"`). Allowed attempts are logged; denied attempts too — with the refusal reason — unless `mcp.self_config_audit_denied` is explicitly `false`. Secret values are redacted (`"<redacted>"`); the changed key(s) with their new values, the tool/route, and the caller are kept (a denied attempt may have no `keys`). Newest-first. Read-only: it reads the JSONL file (`mcp.self_config_audit_path`, default `<data dir>/audit/config-self-modify.jsonl`) and never writes or changes anything. The group settings themselves — `mcp.allow_self_config`, `mcp.self_config_audit_path`, `mcp.self_config_audit_denied` — are read with `get_config` and changed with `config_set` (which stays bootstrap-protected for `mcp.allow_self_config` itself).

MCP records also carry a `class` field: `config` for a configuration-write tool and `sensitive` for a sensitive write tool (`secret_set`, `secret_get`, `restart_daemon`, `start_session` and the rest of the class described in [operations.md](operations.md#sensitive-writes-what-sessions-and-peers-may-not-do)). A `sensitive` record lists the argument names in `keys` and never any values. While `mcp.allow_sensitive_writes` is `false`, a session or peer call to a sensitive tool is refused and the refusal appears here. The setting is read with `get_config` (the `mcp` section reports the effective value, `true` when the key was never set) and changed with `config_set`; unlike `mcp.allow_self_config` it is not bootstrap-protected, so a session with self-config on can write it back, and that write appears here as a `config` record.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `limit` | number | No | Max entries returned (default 100, cap 1000) |

**Example:**
```
self_config_audit(limit=5)
```
```json
{"count":2,"entries":[{"time":"2026-10-29T02:14:03Z","surface":"mcp","tool":"config_set","keys":["mcp.allow_self_config"],"values":{"mcp.allow_self_config":true},"caller":"session:opencode-a3f2","allowed":true},{"time":"2026-10-29T02:13:41Z","surface":"mcp","tool":"config_set","caller":"session:goose-b7c1","allowed":false,"reason":"bootstrap: mcp.allow_self_config cannot be changed via config_set"}]}
```

---

### Sensitive write tools

Every MCP tool belongs to one of three classes: configuration writes, **sensitive writes** and everything else. The sensitive class holds `secret_set`, `secret_delete`, `secret_get` (it only reads, but it returns the secret value), `set_identity`, `update_identity`, `configure_identity`, `restore_identity`, `reset_identity`, `restart_daemon`, `reload`, `plugin_install`, `plugin_run_subcommand`, `send_input`, `start_session`, `tailscale_acl_push`, `tailscale_auth_key` and `acme_renew`. `secret_list`, `secret_exists` and `secrets_vault_status` are not in it.

The class has its own switch, `mcp.allow_sensitive_writes`, which is on when the key is absent. While it is `false`, a call to one of these tools from a model session or a federation peer is refused with a message that names the setting, and the refusal is audited (see `self_config_audit` above). Operator calls from the Web UI, REST, the CLI and an admin MCP caller are never refused. The switch is independent of `mcp.allow_self_config`.

- **Read it:** `get_config` with `section=mcp` returns `allow_sensitive_writes` next to `allow_self_config`, each as its effective on/off state, so a config that never set the key reads `true`. Reading is never gated.
- **Change it:** the operator uses the Settings page, `datawatch config set mcp.allow_sensitive_writes false`, `PUT /api/config` or the comm `configure` verb; each is applied live and saved. A session can reach it through `config_set`, because the key is not bootstrap-protected.
- **Value text:** send a real boolean. The daemon reads `true`, `yes` and `1` as on and any other text, such as `on`, as off.

Walk-through: [howto/self-config-capability.md](howto/self-config-capability.md#sensitive-writes-a-separate-switch). Definition: [datawatch-definitions.md](datawatch-definitions.md#sensitive-writes).

---

### `vision_describe`

Describe an image file using the configured vision model. Returns a natural-language description. Requires `vision.enabled: true` in config.

The vision block — including `vision.describe_timeout_seconds`, the per-describe deadline in seconds (whole number >= 1 through the write path; unset reads the built-in 5-second default) — is set with `config_set` (for example `config_set(key="vision.describe_timeout_seconds", value="15")`) and read with `get_config`, under `vision`; the same key is reachable over REST, the web app Settings page, the comm channel and the CLI. A description that passes its timeout, errors or comes back empty degrades the image to a bare `@<path>` file reference at the call site, so the message is never lost. See the [vision image descriptions howto](howto/vision-image-descriptions.md).

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `image_path` | string | Yes | Absolute local path to the image file (jpeg/png/webp/gif) |
| `prompt` | string | No | Override the default description prompt. Leave empty for the built-in default (`"Describe this image concisely."`). |

**Example response:**
```
A terminal screenshot showing a Go compilation error in main.go at line 47:
'undefined: visionPkg'. The error is highlighted in red. The terminal is
running in a dark theme with white text.
```

**Use cases:**
- Agents analyzing screenshots of their own output (test failures, UI regressions)
- Reading captured terminal frames or error dialogs
- Describing diagrams, mockups, or architecture images to pass as context to other tools

---

### `websearch_providers_list` / `websearch_provider_get`

List every configured web search provider (BL391), or fetch one by name. Read-only.

**Parameters (`websearch_provider_get` only):**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Provider name |

**Example response:**
```json
{"providers":[{"name":"searxng-primary","type":"searxng","enabled":true,"priority":0,"url":"http://searxng.example.com:3001","engine":"bing","num_results":10}]}
```

---

### `websearch_provider_add` / `websearch_provider_update`

Add a new provider, or update fields on an existing one (only provided fields change on update).

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Provider name |
| `type` | string | add: yes | `searxng` \| `brave` |
| `enabled` | string | No | `true`/`false`, default true |
| `priority` | string | No | Try order, lower first |
| `url` | string | No | SearXNG instance URL (searxng only) |
| `engine` | string | No | Comma-separated SearXNG engines, default `bing` |
| `api_key` | string | No | Literal key or `${secret:name}` ref (brave only) — write-only, never echoed back by a get/list call |
| `num_results` | string | No | Default results per query |
| `cache_ttl_seconds` | string | No | Per-provider cache TTL override, 0 = registry default |

---

### `websearch_provider_delete` / `websearch_provider_enable` / `websearch_provider_disable`

Remove, enable, or disable a provider by name.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Provider name |

---

### `websearch_provider_test`

Run one live connectivity test query through a single named provider, bypassing its `enabled`/priority — lets an operator validate credentials before flipping a new provider on.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Provider name |

**Example response:**
```json
{"ok":true,"result_count":3,"sample":[{"title":"...","url":"...","content":"..."}]}
```

---

### `websearch_stats`

Multi-provider usage summary (total/today/this week/this month/cache hits, per provider and overall) plus a zero-filled daily time series for charting. Read-only.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `days` | string | No | Length of the daily series, default 30, max 365 |

---

### `websearch_history`

Recent search events, newest first: query, provider, cache hit/miss, success/error, result count, session. Read-only.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `limit` | string | No | Page size, default 50 |
| `offset` | string | No | Page offset, default 0 |

---

### `web_search_stats` (deprecated alias)

Legacy single-provider stats tool, kept for backward compatibility. Returns the current web search configuration and runtime statistics for this datawatch instance. Requires `web_search.enabled: true` in config. Prefer `websearch_stats` above for multi-provider detail.

**Parameters:** none

**Example response:**
```json
{
  "enabled": true,
  "provider": "searxng",
  "url": "http://searxng.example.com:3001",
  "engine": "bing"
}
```

**Use cases:**
- Confirming that web search injection is active before querying
- Checking which SearXNG instance and engine are in use
- Verifying configuration changes took effect (REST PATCH then this tool)

---

### `kill_session`

Terminate a session.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `session_id` | string | Yes | Session ID to terminate |

**Example response:**
```
Session a3f2 killed.
```

---

### `session_children`

List child sessions spawned by a given parent session. Useful for agents orchestrating sub-agents to check progress.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `session_id` | string | Yes | ID or name of the parent session |

**Example response:**
```
Children of session pp01 (2):
  [cc01] running | claude-code | write integration tests
  [cc02] complete | claude-code | update documentation
```

---

### `reply_to_parent`

Send a message to the parent session that spawned this session. Used by sub-agents to report results back to the spawning agent without out-of-band coordination.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `text` | string | Yes | Text to send as input to the parent session |
| `session_id` | string | No | ID of the child session (used to look up `parent_id`). If omitted, the tool has no child reference and will return an error. |

**Example response:**
```
Sent to parent session pp01.
```

**Usage pattern for sub-agents:**
```
# At spawn time, parent passes its own session ID:
start_session(task="...", caller_session_id="host-abc123")

# When the sub-agent finishes, it calls:
reply_to_parent(session_id="cc01", text="Task complete. Wrote 14 tests, all passing.")
```

---

### `restart_session`

Restart a session from any state (running, waiting_input, complete, failed, or killed). Kills the session if still alive, then relaunches with the same task. Session ID and name are preserved.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `session_id` | string | No | Session ID to restart (required if `session_name` not provided) |
| `session_name` | string | No | Session name as alternative to `session_id`; resolves to the oldest active session with this name (BL354 name resolution) |
| `task` | string | No | Optional new task string. If omitted, the session's existing task is reused. |

**Example response:**
```
Restarted session myhost-a3f2 (state: running)
```

### Operator question channel (v9.34.0)

The operator-question tools let a client that cannot subscribe to the
lifecycle events poll, raise and answer an operator question on this
daemon — the remote-client counterpart to the CLI, the comm verbs and
the PWA automation queue. They proxy the `/api/questions` REST
surface one-for-one, and they need the `autonomous:write`
(`question_raise`, `question_answer`, `question_withdraw`) or
`autonomous:read` (`question_list`, `question_get`, `question_quality`) capability when
called by a federated peer.

| Tool | What it does | Rest call |
|---|---|---|
| `question_raise` | Raise an operator question to the operator of this daemon (an automation blocked on a decision gets its answer back through its return path) | `POST /api/questions` |
| `question_list` | List the daemon's stored operator questions, optionally filtered, for a client that cannot subscribe to poll | `GET /api/questions` |
| `question_get` | Return one stored operator question by id, plus its current answer — the default return path for the asker | `GET /api/questions/{id}` |
| `question_answer` | Record an answer to a stored operator question; the answer is immutable, a changed mind supersedes | `POST /api/questions/{id}/answer` |
| `question_withdraw` | Withdraw an open operator question (marked withdrawn, can no longer be answered) | `POST /api/questions/{id}/withdraw` |
| `autonomous_held_questions` | Read the held-question report: the mode in effect (off, shadow, on), the held states, the held questions now (on mode) and the shadow entries (shadow mode); read-only, acting on a hold is a `question_answer` | `GET /api/autonomous/held-questions` |
| `question_quality` | Read the question quality policy the daemon checks every raised question against (read-only) | `GET /api/questions/quality` |

**Common behaviour.** While the `autonomous.questions.enabled`
switch is off the daemon answers 503 and names the switch.
`context` takes one of `none`, `prd`, `task`, `proposal` or
`registry_row`. `question_answer` and `question_withdraw` return
409 when the question is not answerable (already answered or
withdrawn) and 404 for unknown ids. `question_answer` refuses
422 when the answer text looks like a secret — store it in the
datawatch secrets store and refer to it as `${secret:name}`
instead.

**`question_raise` parameters:**

| Parameter | Required | Description |
|---|---|---|
| `text` | yes | The question itself, one line of what decision is needed |
| `options` | yes | At least one option: a string (id and label together), a comma-separated string, or a JSON array of `{"id","label","tradeoffs"}` objects; the ids are what `question_answer` passes back |
| `asker` | no | Who is asking (default: the caller of record) |
| `context` | no | What the question is about (`none`, `prd`, `task`, `proposal`, `registry_row`) |
| `prd_id` | no | The Automaton id the question is about, when `context` is `prd` or `task` |
| `return_path` | no | Also deliver the answer line into the asking session (`DATAWATCH_ANSWER` marker, best-effort; default false) |
| `tradeoffs` | no | What each option costs: an object mapping an option id to its trade-off text, such as `{"ship": "faster, skips the soak"}`. It fills an option that has no trade-offs of its own; an id that is not one of the options is a `400` |

**Quality check on raise.** The daemon checks how readable the question is. In `warn` mode the question is raised and the findings come back in the response under `warnings` (each with a `code` and a `message`). In `refuse` mode the question is not raised: the daemon answers `422` with the findings, so fix them and send the question again. `off` skips the check. Read the policy with `question_quality`, which takes no parameters and returns `mode`, `min_text_chars`, `tracker_id_pattern` and `queue_preview_lines`; like the other question tools it answers `503` while the `autonomous.questions.enabled` switch is off.

**`question_list` parameters:**

| Parameter | Required | Description |
|---|---|---|
| `state` | no | Question state filter: `open`, `answered`, `withdrawn`, `expired` or `all` (default `all`) |
| `context` | no | Context filter: one of the values above, or `all` |
| `asker` | no | Only questions asked by this asker |
| `text` | no | Only questions whose text contains this substring |
| `limit` | no | Maximum questions to return (default 50, capped at 200) |

**`question_get` parameters:** `id` (required) — the question id.

**`question_answer` parameters:**

| Parameter | Required | Description |
|---|---|---|
| `id` | yes | The question id to answer |
| `chosen_option_id` | one with `quote` | The option you pick: one of the question's option ids |
| `quote` | one with `chosen_option_id` | Your own words. Optional when you pick an option, and the only thing needed when you do not. Whatever you type is saved exactly as written, with your choice, as the record of your decision, and the asker reads it as your answer (capped at 16 KiB) |
| `supersedes` | no | The id of the answer this answer replaces (a changed mind; `409` if the id is wrong) |
| `follow_up` | no | Optionally names one entry of the configured follow-up list (`autonomous.decisions.follow_ups`); when the answer is applied the daemon runs it once (statuses `done`, `skipped` or `error` on the owning decision record). Unknown names — or any name while the list is empty — are refused, naming the configured follow-ups. See [Decision records](#decision-records) |
| `actor` | no | Who is answering (default: the caller of record) |

**`question_withdraw` parameters:**

| Parameter | Required | Description |
|---|---|---|
| `id` | yes | The question id to withdraw |
| `actor` | yes | Who is withdrawing (attribution is required — a withdrawal is named, an answer is not) |
| `reason` | no | What the operator said when withdrawing |

The full surface — the CLI `datawatch question` group, the comm
verbs, the PWA automation queue, the answer log and the
supersession rule — is walked through in
[`howto/operator-questions-answering.md`](howto/operator-questions-answering.md).

### Placement tags: sweep and tags in use

- **`autonomous_tag_sweep`** — re-derive the system tags of every existing Automata object (Automata, Automations, tasks, templates, decision records, held questions) and every Automata-owned session under the `tags.placement` settings in effect now, replacing each object's derived system tags with the newly derived set: turning a placement axis off removes its tag, turning it on adds the tag wherever the fact exists. The operator's own tags are a separate list that the sweep never touches, and a second run is a no-op (it changes zero objects and writes nothing). Returns the per-kind tally `{sessions, automata, automations, tasks, templates, decision_records, questions}`, each `{examined, changed, unchanged, failed}`.

  | Parameter | Type | Required | Description |
  |---|---|---|---|
  | `dry_run` | boolean | No | `true`: re-derive and report the would-be counts without writing; `false` or absent: the sweep writes (default) |

  Proxies `POST /api/autonomous/tags/sweep` (`?dry_run=true` when set). The same pass also runs automatically when a `tags.placement.*` change is saved (after the save answers), and once at daemon startup, after the lease backfill, when the block has changed since the last sweep.

- **`session_tag_list`** — read-only, no parameters. Lists the tags in use across live and archived sessions. Each entry has the tag, its axis and value from the `axis:value` form (axis is empty for a plain tag), and the number of sessions that carry it — a session counts once per tag, and its own, system, and inherited tags all count. Sorted by axis (tags without an axis last), then count (descending), then tag. The same object as `GET /api/sessions/tags` (comm `autonomous session-tags list`) — the fastest way to see what values an axis actually has before filtering on one.

- **Object tag filters (REST)** — the list endpoints for Automata (`/api/autonomous/prds`), Automations, templates, decision records and held questions share one filter over the object axes: `?tag=<axis>:<value>` (repeatable), `?tag_match=any` (default) or `all`; the single-value shortcuts `?project=`, `?automation=`, `?automaton=`; and `?tag_id=` for a registry tag id. For example `GET /api/autonomous/prds?tag=stage:task&tag=project:web&tag_match=all`. See the [Placement axes entry](datawatch-definitions.md) and [automata-tags](howto/automata-tags.md).

### Decision records

A **decision record** is the structured description of one operator
decision: the `label` (a short name), the `title` (the decision in one
line), the `situation` (what is true right now), the `deciding` (what
the operator is being asked to make), the `options` (each a
`{"id","label","tradeoff"}` object; the ids are what the answer
picks), the author's `recommendation` (`{"option_id","reason"}`,
optional), the `default` (the option id chosen when the decision times
out, optional) and the `links` (`doc`, `automaton`, `registry_row`).
Beside that the store owns the bookkeeping: the `state` (`open`,
`answered` or `withdrawn`), the `question_id` and `question_state` (the
queue question built from the record), the `answer` (once it is
applied: `quote`, `actor`, `surface`, `answered_at`), the `doc_change` and
the `follow_up` outcome (once set).

The record is the **source**: the daemon builds the queue question from
it and raises it, and the answer comes back on the record — so the five
tools here manage the record and the operator-question tools above read
and answer the question. The tools proxy the
`/api/autonomous/decision-records` REST surface one-for-one and need
the `autonomous:read` (`autonomous_decision_record_list`,
`autonomous_decision_record_get`) or `autonomous:write` (`create`,
`update`, `withdraw`) capability when called by a federated peer.

| Tool | What it does | Rest call |
|---|---|---|
| `autonomous_decision_record_create` | Create a decision record; the daemon builds its queue question from it and raises it | `POST /api/autonomous/decision-records` |
| `autonomous_decision_record_list` | List decision records with the daemon's folder-watch status (read-only) | `GET /api/autonomous/decision-records` |
| `autonomous_decision_record_get` | Read one decision record and its bookkeeping (read-only) | `GET /api/autonomous/decision-records/{id}` |
| `autonomous_decision_record_update` | Replace the content of an open record; the daemon rebuilds its queue question | `PUT /api/autonomous/decision-records/{id}` |
| `autonomous_decision_record_withdraw` | Mark a record withdrawn so its question stops asking | `POST /api/autonomous/decision-records/{id}/withdraw` |

**`autonomous_decision_record_create` parameters:**

| Parameter | Required | Description |
|---|---|---|
| `title` | yes | The decision in one line |
| `situation` | yes | What is true right now, in plain words |
| `deciding` | yes | The decision the operator is being asked to make |
| `options` | yes | At least 2 (at most 20) options: a JSON array of `{"id","label","tradeoff"}` objects with unique non-empty ids; the ids are what the answer picks |
| `default` | yes | The option id chosen when the decision times out, or the word `hold` |
| `label` | no | A short name for the decision (default: `decision`) |
| `recommendation` | no | The author's pick and why: `{"option_id","reason"}`; `option_id` must be one of the options |
| `links` | no | What the record is about: any of `{"doc","automaton","registry_row"}` |

**`autonomous_decision_record_update` parameters:** `id` (required) plus the
same content fields, which replace the record's content — the given content
must satisfy the same rules as create (title, situation, deciding, at least
2 options, a default). Updating a record that is already `answered` or
`withdrawn` is refused (the daemon's `409`).

**`autonomous_decision_record_withdraw` parameters:** `id` (required) and
`reason` (optional, what the operator said when withdrawing).

**`autonomous_decision_record_list` parameters:**

| Parameter | Required | Description |
|---|---|---|
| `state` | no | `open`, `answered` or `withdrawn` (anything else is refused) |
| `prd_id` | no | Only the records linked to this Automaton |
| `limit` | no | At most this many records, most recently updated first; default 100, maximum 1000 |

**`autonomous_decision_record_get` parameters:** `id` (required) — the
decision record id.

**Common behaviour.** While the daemon's decision switches
(`autonomous.decisions.enabled`, `autonomous.questions.enabled`) are off a
create or update answers the daemon's `503` naming the switch. A record
that is not clear enough is refused with the clarity findings
(`422`) — fix them and send it again — or, when the clarity mode is
`warn`, raised with the findings under `warnings`. Create and update
answer `201` / `200` with `record`, `question_id` and `warnings`; a
question that could not be brought in step yet comes back as the
`question_not_in_step` warning (the next daemon poll retries) rather
than as a failure. Unknown ids are `404`.

```
autonomous_decision_record_create(label="storage choice", title="Pick the question storage",
  situation="The question store is in-memory", deciding="Which store to move to",
  options=[{"id":"jsonl","label":"JSONL","tradeoff":"simple, single file"},
           {"id":"sqlite","label":"SQLite","tradeoff":"new dependency"}],
  recommendation={"option_id":"jsonl","reason":"no new dependency"}, default="jsonl",
  links={"automaton":"d0442737d38c"})
autonomous_decision_record_list()
autonomous_decision_record_list(state="open", prd_id="d0442737d38c", limit=20)
autonomous_decision_record_get(id="rec-1")
autonomous_decision_record_update(id="rec-1", title="Pick the question storage", options=[{"id":"jsonl","label":"JSONL"}])
autonomous_decision_record_withdraw(id="rec-1", reason="superseded by a newer plan")
```

When answering a record's question, `question_answer` accepts the
optional `follow_up` name that runs one of the configured follow-ups
(`autonomous.decisions.follow_ups`) once per record; the outcome is
stored on the record under `follow_up` with a `status` of `done`,
`skipped` or `error` and a one-line `result`.

---

### `autonomous_overlap_search`

Search before create: find the Automata, registry rows and unmerged branches that already cover a plan's title and spec, best match first, before you create the plan. It is read-only and works in every search-before-create mode. It proxies `GET /api/autonomous/overlap`. A failed search returns an error, never an empty list.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `title` | string | No | Title of the plan about to be created. Give a title, a spec, or both. |
| `spec` | string | No | Spec of the plan about to be created. Give a title, a spec, or both. |
| `limit` | number | No | Most warnings returned, from 1 to 50. Default 10. |

**Reply:** `warnings`, best match first. Each warning has `kind` (`automaton`, `registry`, `branch` or `unavailable`), `id` and `status` for an Automaton, `row` for a registry row, `branch` for a branch, `score` from 0 to 1, up to five `shared_terms`, `reason` and `read_hint`. The reply also carries `threshold` (the match threshold in force) and `mode` (`off`, `shadow` or `on`, the mode in force).

**Example:**
```
autonomous_overlap_search(title="Add search before create", spec="Warn when a new Automaton overlaps an existing one", limit=5)
```

### `planning_routing_get`

Read the planning routing config and its validation result. Routing is off by default; when on it picks the planner for an Automaton decompose per rule. Read-only. It proxies `GET /api/autonomous/planning-routing`.

**Parameters:** none.

**Reply:** `enabled`, `shadow`, `rules`, `ladders` and `classes` (the built-in default ladder and risk classes are filled in under any entries you set) and `validation` (`valid`, and `error` when it is not valid).

**Example:**
```
planning_routing_get()
```

### `planning_routing_set`

Update the planning routing config. Every field is optional: a field you leave out keeps its current value (the tool reads the current config and sends the merged block). The result is validated first and a bad config is rejected with nothing applied. It proxies `PUT /api/autonomous/planning-routing`.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `enabled` | boolean | No | Turn planning routing on or off. |
| `shadow` | boolean | No | Record the routed decision only; keep the planner unchanged. |
| `rules` | string (JSON array) | No | Replaces the current rules: `[{"name","match":{"kind","tags","project","risk_class","spec_size_min","spec_size_max","override"},"planners":[{"backend","model"}],"ladder"}]`. |
| `ladders` | string (JSON object) | No | Ladders by name, replacing the current ones: `{"default":{"touch_up_max","replan_max","escalate_after_attempts"}}`. |
| `classes` | string (JSON object) | No | Risk classes by name, replacing the current ones: `{"security":{"tags":[],"path_globs":[]}}`. |

**Reply:** the same view as `planning_routing_get`, for the config now in force. An invalid `rules`, `ladders` or `classes` string, or a non-boolean `enabled` or `shadow`, is an error.

**Example:**
```
planning_routing_set(enabled=true, shadow=true)
```

### `planning_routing_test`

Route one input under the current planning routing config and return the decision. No LLM is called and nothing changes. It proxies `POST /api/autonomous/planning-routing/test`.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `kind` | string | No | Routing input kind, such as `feature` or `bug`. |
| `tags` | string | No | Comma-separated tags. |
| `spec_bytes` | number | No | Spec size in bytes (0 or more). |
| `files` | string | No | Comma-separated repo-relative paths the tasks touch. |
| `project` | string | No | Project identifier. |

**Reply:** `enabled`, `rule` (the matching rule's name), `classes`, `planners` (each `{backend, model}`), `ladder`, `shadow`, `fallback`, `reason` and `decided_at`.

**Example:**
```
planning_routing_test(kind="feature", tags="security", spec_bytes=12000, files="internal/auth/login.go")
```

### `planning_scorecard`

Counts-only planning scorecard and gate report per planner and rule class over a window: decomposes, passes by attempt, escalations, errors and shadow would-route counts. It carries no spec or task text. Read-only. It proxies `GET /api/autonomous/planning-routing/scorecard`.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `window` | string | No | A duration such as `36h` or a day count such as `7d`. Default `7d`; `0` means all records. A negative or malformed window is an error. |

**Reply:** `window`, `scorecard`, `gate` (the gate summary) and `report` (the gate report as text).

**Example:**
```
planning_scorecard(window="7d")
```

---

## Automation requirements and model pull consent

Two operator surfaces around the local-model capability floors and the model-pull gate:
the requirements report (what is configured, what is missing, backend minimums) and the
model-pull consent policy (which models or publishers may be pulled). All four tools proxy
their REST routes, so the same data is on every surface: REST, these tools, `datawatch
automation requirements-report`, `datawatch automation model-consent get|set`,
`datawatch compute node versions <name>`, and the comm verbs (see
docs/comm-verbs.md and docs/automation/comm-verbs.md).

- **`automation_requirements_report`** — read the automation requirements report: the
capability minimums (the built-in Ollama floor per known capability, with the operator's
`autonomous.requirements.min_ollama.*` overrides), each known node's reported backend
version against it (`meets`, `fails`, or `unknown` — an unknown version fails closed), the
configured execution/planning/verification backends, and whether at least one of the
two optional providers (a Claude backend, the local model pool) is configured. It is a
report, not a gate: nothing is blocked on it. Proxies `GET
/api/automation/requirements-report`; needs the autonomous read capability when called
by a federated peer.

  | Parameter | Type | Required | Description |
  |---|---|---|---|
  | `format` | string | No | `text` (default, the shared plain-text render) or `json` (the report object). |

  **Example:**
  ```
  automation_requirements_report()          # text
  automation_requirements_report(format="json")
  ```

- **`automation_model_pull_consent_get`** — read the current model-pull consent
  policy: the model list (each with `name`, `allowed` and, when noted, `note`), the
  publisher allow-list, and the decision rule in words. Default-deny: a model may be
  pulled when it is listed with `allowed: true`, or when it is unlisted and its
  publisher is allow-listed; everything else is refused and the refusal names the key
to set. Read-only. Proxies `GET /api/automation/model-consent`.

  **Example:**
  ```
  automation_model_pull_consent_get()
  ```

- **`automation_model_pull_consent_set`** — set the model-pull consent policy.
  The set is complete and replaces what is stored: `models` is the whole new model
  list (each item a model-name string — stored as allowed — or a
  `{name, allowed, note}` object), and `publishers` is the whole new publisher
  allow-list. Invalid entries are refused (empty names, duplicate model names —
  case-insensitive and tag-ignoring — and empty publisher entries). Audited as
  `model_pull_consent_changed`. Proxies `PUT /api/automation/model-consent`; needs the
  config-write capability when called by a federated peer.

  | Parameter | Type | Required | Description |
  |---|---|---|---|
  | `models` | string | No | JSON array — model-name strings or `{name, allowed, note}` objects. Replaces the stored list. |
  | `publishers` | string | No | JSON array of publisher names; each entry allows every model of that publisher. Replaces the stored list. |

  **Example:**
  ```
  automation_model_pull_consent_set(
    models='["gpt-oss:120b", {"name":"big-model-65b","allowed":false,"note":"too large for the pool"}]',
    publishers='["mistralai"]')
  ```

- **`compute_node_backend_versions`** — read one compute node's identity and reported
  backend version (with when it was recorded), and apply the per-capability Ollama
  minimums to that version: each line says `ok` (`true` below the minimum, `false`
  below it, or absent when the version is unparseable — unknown fails closed). For
  Ollama nodes and nodes whose version was probed against Ollama. A node name that is
  not in the registry is refused (the daemon's 404). Proxies `GET
  /api/compute/nodes/{name}/versions`; needs the compute-node read capability when
  called by a federated peer.

  | Parameter | Type | Required | Description |
  |---|---|---|---|
  | `name` | string | Yes | Compute node name (as listed by `compute_nodes`). |

  **Example:**
  ```
  compute_node_backend_versions(name="ollama-local")
  ```

The config keys behind these tools — `autonomous.requirements.min_ollama.*` and
`autonomous.model_pull_consent` — are written through `autonomous_config_set` / the
config surface, the dedicated surfaces above, `datawatch config set`, comm
`configure`, and the PWA settings; all apply without a restart (the gate is read at
pull time, the minimums at report and enable time). See
docs/config-reference.yaml (the `autonomous` section), docs/howto/automation-requirements.md,
docs/howto/model-pull-consent.md and docs/implementation.md ("Automation requirements and
the model pull gate").

---

## Operator identity and its backups

The operator identity (role, north-star goals, current projects, values, current
focus and context notes) is read and written through seven tools. They mirror
the `/api/identity` routes. Every write keeps a timestamped backup of the
identity it replaces, in the `identity-backups` folder next to `identity.yaml`,
and the daemon keeps the newest `identity.backups_to_keep` of them (default 10).
See [howto/identity-and-telos](howto/identity-and-telos.md#backups-restore-and-the-empty-write-guard)
for the steps on every surface.

| Tool | What it does | Read-only |
|---|---|---|
| `get_identity` | Returns the saved identity. | Yes |
| `set_identity` | Replaces the whole identity with the fields given. | No |
| `update_identity` | Merges the non-empty fields given into the identity; omitted and empty fields are kept. | No |
| `configure_identity` | Returns where the setup wizard lives (PWA, CLI) and how the backup tools fit; it changes nothing. | No (federation capability `config:write`) |
| `identity_backups` | Lists the saved backups, newest first, with the number kept. | Yes |
| `restore_identity` | Brings a backup back and returns the restored identity. | No |
| `reset_identity` | Clears the identity on purpose, keeping a backup. | No |

### `set_identity` refuses an empty write

`set_identity` replaces the identity, so a call with no fields (or only empty
ones) over a saved identity would empty it. The daemon refuses that: the tool
returns an error carrying `HTTP 409` and the code `identity_wipe_refused`, with
the fields that were kept, and nothing is written. The error also says to call
`reset_identity` when the clear is meant. A `set_identity` that carries at least
one field still replaces the identity with those fields, after taking a backup,
and `update_identity` is unchanged: it only merges, so an empty call changes
nothing. Setting an empty identity over an empty one is allowed.

### `identity_backups`

List the saved copies of the identity, newest first. Read-only.

**Parameters:** none

**Reply:** `backups`, each with `name`, `created_at` (UTC), `size_bytes`, `role`
and `field_count` (how many of the six fields have content), and `keep`, the
current `identity.backups_to_keep`. Same body as `GET /api/identity/backups`;
no backups yet is an empty list.

### `restore_identity`

Bring a saved copy back. The identity being replaced is backed up first, so a
restore can be undone with another `restore_identity`.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `backup` | string | No | A name from `identity_backups`, or `latest` (the default), the newest backup that has content. |

**Reply:** the restored identity. An unknown or malformed name is an error with
`HTTP 404`; a backup that holds no content is an error with `HTTP 422`. Same
body as `POST /api/identity/restore`.

**Example:**
```
identity_backups()
restore_identity(backup="identity-20261028T080000.000Z.yaml")
```

### `reset_identity`

Clear the identity on purpose. This is the only way to empty a saved identity;
`set_identity` with no fields is refused. The identity cleared is kept as a
backup, so `restore_identity` undoes the reset. Resetting an identity that is
already empty changes nothing and makes no backup.

**Parameters:** none

**Reply:** the now-empty identity. Same body as `POST /api/identity/reset`.

The tools need the same federation capabilities as the routes they call: the
read tools `config:read`, the write tools `config:write`.

---

## Project record, arcs and the Projects sub-tab

The seven project tools read and write the project record behind the web app's
Projects sub-tab. The same data is on REST (`/api/projects…`), the CLI
(`datawatch projects …`) and the comm channel (`project …`). The guide is
[howto/projects-tab.md](howto/projects-tab.md).

| Tool | Reads or writes | Parameters |
|---|---|---|
| `project_list` | read | none |
| `project_upsert` | write | `name`, `dir` (required); `default_backend`, `description`, `policy`, `owner`, `repository`, `default_branch`, `arcs`, `target`, `lane_defaults`, `weekly_share`, `branch_retention_days`, `major_trigger` (optional; an omitted record field keeps its stored value) |
| `project_set_arcs` | write | `name`, `arcs` (required, an array; order is the arc order; `[]` clears) |
| `project_rollup` | read | `name` (required), `window` (days, 1 to 365, default 7) |
| `project_history` | read | `name` (required), `limit`, `cursor` |
| `project_policy_effective` | read | `name` (required), `automaton` (optional top tier) |
| `project_alias_delete` | write | `name` (required); the directory is not touched |

Arc objects are `{"automation_id": "<id>", "priority": <int>, "state": "planned" | "active" | "done"}`.
`automation_id` is required and must be unique within the project; an empty state means `planned`.

`project_rollup` and `project_history` need the autonomous read capability and the
two write tools need the autonomous write capability. Costs in the roll-up are
usage points, never API dollars. `weekly_share`, `branch_retention_days` and
`major_trigger` are stored and shown; the scheduler, the worktree cleanup and the
release decision do not read the project's own values yet (see the howto).

Set the arcs of the `core-api` project:

```json
{
  "tool": "project_set_arcs",
  "arguments": {
    "name": "core-api",
    "arcs": [
      {"automation_id": "a1b2c3d4", "priority": 1, "state": "active"},
      {"automation_id": "e5f6a7b8", "priority": 2, "state": "planned"}
    ]
  }
}
```

Read a 30-day roll-up of the same project:

```json
{"tool": "project_rollup", "arguments": {"name": "core-api", "window": 30}}
```

## Plugin service reads

Four read-only tools expose the plugin service surfaces the REST layer already serves: one plugin's service status, its declared widgets with their latest values, and the token usage listing. All four take the plugin's `name`; only `plugin_widget_get` takes an extra argument. Each tool proxies the same REST handler it mirrors, so the response body is the handler's verbatim — including its error codes (unknown plugin is a 404 surfaced as a tool error, a plugin without widget or token wiring is a 503).

All four are read-only and change nothing. Because the tools only read, they hold the plugin read gate — the same `plugin:read` a federation peer needs to fetch the corresponding REST endpoints — not the config capabilities the other plugin tools (list, enable, disable, install) carry.

### `plugin_service_status`

Read one plugin's service object: its lifecycle mode (`managed` or `attach_only`), state, address, version, uptime in seconds, and the last health check — the `service` object of `GET /api/plugins/{name}`. A non-service plugin still gets its manifest; it simply has no `service` object. Fields with no value (no health data yet, service not up) are omitted rather than sent as placeholders, exactly as the REST endpoint sends them.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Plugin name |

**Example response:**
```json
{
  "name": "embeddings",
  "kind": "service",
  "enabled": true,
  "service": {
    "mode": "managed",
    "state": "healthy",
    "address": "http://127.0.0.1:9",
    "version": "1.2.3",
    "uptime_seconds": 42,
    "last_health_check": "2026-11-01T01:00:00Z",
    "last_health_status": "up"
  }
}
```

### `plugin_widget_list`

List one plugin's declared widgets with each widget's latest stored value (the value is null until the service has pushed one). Same body as `GET /api/plugins/{name}/widgets`.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Plugin name |

**Example response:**
```json
{
  "plugin": "embeddings",
  "widgets": [
    {
      "id": "queue_depth",
      "surface": "dashboard",
      "kind": "stat_tile",
      "data_mode": "push",
      "value": {
        "plugin": "embeddings",
        "widget_id": "queue_depth",
        "data": 0,
        "updated_at": "2026-11-01T01:00:00Z",
        "source": "push"
      }
    },
    { "id": "pending", "surface": "dashboard", "kind": "stat_tile", "data_mode": "push", "value": null }
  ]
}
```

### `plugin_widget_get`

Fetch one widget as declared plus its latest value, same body as `GET /api/plugins/{name}/widgets/{widget_id}`. An unknown widget id surfaces the REST 404.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Plugin name |
| `widget_id` | string | Yes | Widget id as declared in the plugin's manifest |

**Example response:**
```json
{
  "id": "queue_depth",
  "surface": "dashboard",
  "kind": "stat_tile",
  "data_mode": "push",
  "value": {
    "plugin": "embeddings",
    "widget_id": "queue_depth",
    "data": 0,
    "updated_at": "2026-11-01T01:00:00Z",
    "source": "push"
  }
}
```

### `plugin_token_list`

List one plugin's token usage: the secret names its manifest references (with the manifest fields that carry the reference — `connection.auth.token`, `env.<KEY>` — and whether each secret exists in the store), and the service token names minted for the service with their creation times. Same body as `GET /api/plugins/{name}/tokens`, and like that route it **never returns a secret value or a service token value**; it is also operator-token-only, so a federation peer calling the tool through `POST /api/mcp/call` is refused with the REST 403.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Plugin name |

**Example response:**
```json
{
  "plugin": "embeddings",
  "secrets": [
    { "name": "embeddings_api_key", "fields": ["connection.auth.token"], "exists": true }
  ],
  "service_tokens": [
    { "name": "embeddings_api_key", "created_at": "2026-10-25T00:00:00Z" }
  ]
}
```

### `plugin_service_start`

Start the managed service of a plugin: `POST /api/plugins/{name}/enable` with `{"managed": true}` — datawatch launches the plugin's `lifecycle.command` under its supervisor (health checks, backoff restarts per the manifest's `lifecycle` policy). The plugin is enabled as part of the same call (it is an `enable` route). A plugin that does not declare the `managed` mode — attach-only or non-service plugins — is refused with a clear error: attach-only services are run and stopped by their owner, never by datawatch.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Plugin name |

**Example response:**
```json
{ "action": "enabled", "managed": true, "previous_managed": false }
```

### `plugin_service_stop`

Stop the managed service of a plugin: `POST /api/plugins/{name}/enable` with `{"managed": false}` — the daemon terminates the supervised child (per its `stop_timeout_ms`) and drops health checking; the plugin stays enabled so `plugin_service_start` brings it straight back. Refused with the same clear error as `plugin_service_start` when the plugin is attach-only or does not declare managed lifecycle support. (Use `plugin_disable` to stop and disable in one go — that is the disable route, not this one.)

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Plugin name |

**Example response:**
```json
{ "action": "disabled", "managed": false, "previous_managed": true }
```

### `plugin_service_restart`

Restart the managed service of a plugin: the two calls in order — `POST /api/plugins/{name}/enable` with `{"managed": false}` (stop the supervised child), then the same route with `{"managed": true}` (start it again). If the stop leg is refused (attach-only plugin), the start leg is never attempted and the refusal is surfaced. The response carries both legs.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Plugin name |

**Example response:**
```json
{
  "stop": { "action": "disabled", "managed": false, "previous_managed": true },
  "start": { "action": "enabled", "managed": true, "previous_managed": false }
}
```

### `plugin_token_rotate`

Rotate one secret a plugin's manifest references: `POST /api/plugins/{name}/tokens/{secret}/rotate` with `value`. The new value is stored through the same secrets-store write every other secret write uses, and when the name is also registered as a service token the old token is revoked and a fresh one minted under the same name — no other token or secret is touched. The rotation lands in the audit log. The new value is sent to the server and **never returned**: the response carries only `status`, `plugin` and `secret` names (the tool re-emits exactly those fields even if the upstream sent more).

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Plugin name |
| `secret` | string | Yes | Secret name as referenced by the plugin's manifest (e.g. `connection.auth.token`, `env.<KEY>`); a secret the plugin does not reference is refused by the upstream route (400) |
| `new_value` | string | Yes | The new secret value; sent to the server, never echoed back |

**Example response:**
```json
{ "status": "rotated", "plugin": "embeddings", "secret": "embeddings_api_key" }
```

> The four tools are MCP-only: the agent calls the existing operator-facing REST routes over the loopback proxy, and the daemon's REST API is unchanged. All four hold the federation `plugin:write` capability (in no built-in group). `plugin_token_rotate` is additionally operator-token-only, because its upstream route is: a federation peer gets the same 403 the REST route gives.

---

## Exit Hooks (BL356)

Exit hooks fire when a named session exits unexpectedly — either its `claude_alive` flag flips to false (zombie detection) or the session enters `failed`/`killed` state. Two actions are supported: `restart` (relaunch the session with the same task) and `notify` (send a message to another session).

### `exit_hook_list`

List all configured session crash/exit hooks.

**Parameters:** none

**Example response:**
```
Exit hooks (1):

ID:       eh-abc123
Name:     worker-main
Action:   restart
Enabled:  true
Cooldown: 300s
Created:  2026-06-01T10:00:00Z
```

---

### `exit_hook_add`

Add a session crash/exit hook.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Session name to watch (exact match) |
| `action` | string | Yes | Action to take: `restart` or `notify` |
| `notify_session` | string | No | For `action=notify`: name of the session to send a message to |
| `notify_message` | string | No | For `action=notify`: message text to send (default: auto-generated) |
| `cooldown_seconds` | number | No | Minimum seconds between firings. Default: 300. |

**Example response:**
```json
{
  "id": "eh-abc123",
  "name": "worker-main",
  "action": "restart",
  "enabled": true,
  "cooldown_seconds": 300,
  "created_at": "2026-06-01T10:00:00Z"
}
```

---

### `exit_hook_delete`

Delete a session crash/exit hook by ID.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Exit hook ID to delete |

**Example response:**
```
Exit hook eh-abc123 deleted.
```

---

### `exit_hook_enable`

Enable a previously disabled session crash/exit hook.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Exit hook ID to enable |

**Example response:**
```
Exit hook eh-abc123 enabled.
```

---

### `exit_hook_disable`

Disable a session crash/exit hook without deleting it.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Exit hook ID to disable |

**Example response:**
```
Exit hook eh-abc123 disabled.
```

---

## Work Queue (BL357)

A durable, role-based work queue for coordinating multi-agent pipelines. Agents push items for a named role; worker agents atomically claim the oldest pending item, then mark it complete or failed. Items persist across daemon restarts.

### `queue_push`

Push a new work item onto the role-based work queue.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `role` | string | Yes | Role that worker agents claim items from (e.g. `"coder"`, `"reviewer"`) |
| `payload` | string | No | Optional JSON object payload for the work item. Example: `{"key":"value"}` |

**Example response:**
```json
{
  "id": "qi-abc123",
  "role": "coder",
  "state": "pending",
  "payload": {"key": "value"},
  "created_at": "2026-06-01T10:00:00Z"
}
```

---

### `queue_claim`

Atomically claim the oldest pending work item for a given role. Returns the item and sets a lease timer; if the item is not completed or failed within `lease_seconds`, it reverts to `pending`.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `role` | string | Yes | Role to claim a work item for |
| `claimed_by` | string | Yes | Session FullID or identifier of the claiming worker |
| `lease_seconds` | number | No | How long (seconds) to hold the claim before it auto-expires back to pending. Default: 300. |

**Example response:**
```json
{
  "id": "qi-abc123",
  "role": "coder",
  "state": "claimed",
  "claimed_by": "myhost-a3f2",
  "payload": {"key": "value"}
}
```
Returns `"No pending items available for role: <role>"` when the queue is empty.

---

### `queue_complete`

Mark a claimed work item as complete.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | ID of the queue item to complete |
| `result` | string | No | Optional JSON object result payload. Example: `{"output":"done"}` |

**Example response:**
```
Queue item qi-abc123 marked complete.
```

---

### `queue_fail`

Mark a claimed work item as failed.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | ID of the queue item to fail |
| `error` | string | Yes | Error message describing why the item failed |

**Example response:**
```
Queue item qi-abc123 marked failed.
```

---

### `queue_list`

List work queue items, optionally filtered by role and/or state.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `role` | string | No | Filter by role. Empty = all roles. |
| `state` | string | No | Filter by state: `pending`, `claimed`, `complete`, or `failed`. Empty = all states. |

**Example response:**
```json
[
  {
    "id": "qi-abc123",
    "role": "coder",
    "state": "pending",
    "created_at": "2026-06-01T10:00:00Z"
  }
]
```

---

## Discussion Subscribe (BL358)

These tools let sessions subscribe to discussions so that new entries written to a discussion are automatically delivered to the session via `send_input`.

### `discussion_subscribe`

Subscribe a session to a discussion. New entries written to the discussion are delivered to the session via `send_input`.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `discussion_id` | string | Yes | Discussion scope ID to subscribe to |
| `session_name` | string | Yes | Name of the session to deliver discussion entries to |

**Example response:**
```json
{"ok":true,"discussion_id":"proj-alpha","session_name":"monitor","status":"subscribed"}
```

---

### `discussion_unsubscribe`

Unsubscribe a session from a discussion. The session will no longer receive new entries from the discussion.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `discussion_id` | string | Yes | Discussion scope ID |
| `session_name` | string | Yes | Session name to remove from the discussion |

**Example response:**
```json
{"ok":true,"discussion_id":"proj-alpha","session_name":"monitor","status":"unsubscribed"}
```

---

### `discussion_subscriptions`

List all active discussion subscriptions.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `discussion_id` | string | No | Filter by discussion ID. Empty = all subscriptions. |

**Example response:**
```json
[
  {"discussion_id": "proj-alpha", "session_name": "monitor"},
  {"discussion_id": "proj-alpha", "session_name": "logger"}
]
```

---

## Result Store (BL360)

A structured named-result store for agents to exchange data without out-of-band coordination. Results are keyed by name and support optional TTL-based expiry.

### `result_put`

Store a named result payload in the agent result store. Upserts by name.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Unique name / key for this result entry |
| `payload` | string | Yes | JSON object payload to store. Example: `{"output":"done","count":3}` |
| `ttl_seconds` | number | No | Time-to-live in seconds. `0` (default) = no expiry. |

**Example response:**
```json
{
  "name": "build-result",
  "payload": {"output": "done", "count": 3},
  "stored_at": "2026-06-01T10:00:00Z",
  "expires_at": null
}
```

---

### `result_get`

Retrieve a named result from the agent result store.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Name / key of the result to retrieve |

**Example response:**
```json
{
  "name": "build-result",
  "payload": {"output": "done", "count": 3},
  "stored_at": "2026-06-01T10:00:00Z",
  "expires_at": null
}
```
Returns `"Not found: <name>"` when the key does not exist or has expired.

---

### `result_list`

List results in the agent result store. Optionally filter by name prefix.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `prefix` | string | No | Filter results whose name starts with this prefix. Empty = all. |

**Example response:**
```json
[
  {"name": "build-result", "stored_at": "2026-06-01T10:00:00Z"},
  {"name": "build-logs",   "stored_at": "2026-06-01T10:01:00Z"}
]
```

---

### `result_delete`

Delete a named result from the agent result store.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Name / key of the result to delete |

**Example response:**
```
Deleted: build-result
```

---

## Scheduled Job Results (result panel)

Parity tools for the scheduled-job result panel REST endpoints — same payload
contract, same validation, and same store as
`POST /api/sessions/{id}/result` and
`GET /api/schedule-results[/{schedule_name}]`.

### `post_result`

Post the structured result for a session (normally the calling session itself).

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `session_id` | string | Yes | Session posting the result (its short ID or hostname-full-ID) |
| `title` | string | Yes | One-line headline |
| `status` | string | No | `ok`, `warn`, or `error` (default `ok`) |
| `summary` | string | No | Short human-readable summary |
| `metrics` | string | No | JSON object of free-form metrics, e.g. `{"score": 92.5}` |
| `tables` | string | No | JSON array of `{columns, rows}` tables (max 20) |
| `markdown` | string | No | Full markdown report |

Returns the same JSON shape as the REST endpoint:
`{ok, session_id, full_id, schedule_name, stored, count}`. When the session
belongs to a named schedule the entry is appended to that schedule's retained
history (`stored: true`); otherwise only the session's status-board result
slot is updated (`stored: false`).

**Example:**
```json
{
  "session_id": "host-a1b2",
  "title": "Build succeeded — 847 tests passed",
  "status": "ok",
  "metrics": "{\"score\": 92.5, \"builds\": 12}",
  "markdown": "## Overview\n..."
}
```

### `schedule_results_list`

List the results posted by scheduled jobs.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `schedule_name` | string | No | When set: the full retained history for just that schedule (oldest → newest). When omitted: one summary per schedule — latest result, retained entry count, and the recent status sequence that powers the sparkline. |

---

## Usage Calculator

The account usage calculator: a shared usage-readings series plus one advice
result saying how many lanes can still run and when the cycle ends. Advice
only — the calculator never stops or slows lanes. The readings series is
shared across federated daemons through a keeper; a peer forwards writes to
the keeper (stored once). A read served from the last on-disk copy because
the keeper could not be reached carries the stale markers (`stale`,
`stale_reason`, `copied_at`).

Variant and policy are independent: the variant picks the answer shape, the
policy picks the spend target. Where both are stored and passed, the tool
argument overrides the stored setting for this one call only.

### Classification judge tool

`llm_classifier` is one tool with an `action`, because no existing LLM or Automaton tool reads probe or calibration status, lists the publisher allowlist or shows a verdict record. It proxies `/api/llm/classifier`. The `verification_mode` setting itself rides `llm_add` and `llm_update`; a low-confidence block is decided with `autonomous_prd_resolve_task_review`. The tool needs `llms:write` (add_publisher widens which models may judge).

| `action` | Other arguments | Result |
|---|---|---|
| `status` | none | The verification mode of every LLM, the capability probe status per node, model and path, the calibration status per fingerprint and the publisher allowlist |
| `publishers` | none | The publisher allowlist (empty by default: no publisher is trusted until added) |
| `add_publisher` | `publisher`, `confirm` | Adds a publisher. Refused unless `confirm=true`; every addition is written to the audit log |
| `verdicts` | `automaton_id`, `task_id`, `low_confidence` | The Automaton's verdict records: mode, score vector, route, outcome, category. `low_confidence=true` keeps only the low-confidence blocks |

### Burn guard tools

The burn guard compares the projected weekly burn with the pace line and steps down a ladder of admission changes (`lower_inflight`, `route_pi_prompt`, `hold_claude`) when the projection runs over the line plus the margin. It never ends a running session. The tools proxy `/api/usage/burn-guard`; status and config get need `usage:read`, run and config set need `usage:write`.

| Tool | Arguments | Result |
|---|---|---|
| `burn_guard_status` | none | State, `rung`, `applied`, last projection (with `line` and `margin`), last decision, coefficients, last error, counters |
| `burn_guard_run` | none | Evaluates now; the `decision` and the refreshed `status`. An error when the guard is not running |
| `burn_guard_config_get` | none | The `usage.burn_guard` settings |
| `burn_guard_config_set` | any of `enabled`, `interval_minutes` (1–1440), `margin_points` (0–50), `ladder`, `step_back`, `cache_write_points_per_million`, `cache_read_points_per_million`, `refit_on_reading`, `retry_limit_claude` (0–20), `ask_on_change`, `max_projection_gap_points` (0–100; 0 never compares the guard with the usage advice; default 25) | The stored settings. One invalid value rejects the call and stores nothing; give at least one setting |

```
burn_guard_status()
burn_guard_run()
burn_guard_config_set(margin_points=3, ladder=["lower_inflight","hold_claude"])
```

### `usage_advice`

One advice result: the latest reading of the shared series (percent and/or
spend, when, session percent, lanes), the extra credit and effective
ceiling, lane choice (recent lanes and the max), the pace line and headroom
after the safety margin, the one burn figure (the weekly average, with a
fallback notice when the recent window fell back to 48h), the projected end
of cycle per burn line, run-out warnings, the finish-early estimate (queue ×
per-task cost vs the typed remaining work) under the run-flat-out policy,
the point-value line from the billing profile — the estimated cost of one
point and of a full week, an API-equivalent estimate, not a bill, sourced
from the configured `usage.billing` profile or the assumed default — and
the plain-English advice block naming the formulas used.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `variant` | string | No | Answer shape; overrides the stored variant for this call only |
| `policy` | string | No | Spend target; overrides the stored policy for this call only |
| `lanes` | number | No | Whole number of 0 or more; overrides the stored max lanes for this call only |
| `cost` | number | No | Points of 0 or more; the per-task cost used for the finish-early estimate |

Reads (`usage:read`).

### `usage_readings`

The shared readings series: the readings the account has been measured at
(percent and/or spend, when, session percent, lanes, source), the bought
extra credit, and the per-policy remaining-work figure. No parameters.

Reads (`usage:read`).

### `usage_reading_add`

Append one reading to the shared series.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `percent` | number | One of | 0–100, the account's own number. Exactly one of `percent` / `spend_usd`; both or neither is rejected |
| `spend_usd` | number | One of | 0 or more. Exactly one of `percent` / `spend_usd` |
| `at` | string | No | RFC 3339 time; now when omitted |
| `session_percent` | number | No | 0–100, the account's session number (the secondary reading) |
| `lanes` | number | No | 0 or more, the lanes that were in play when the reading was taken |

Writes (`usage:write`). The source is the built (typed) kind; the planned
pasted and account kinds are not built yet (they answer 501 at REST) and are
never sent by the tool.

### `usage_reading_delete`

Delete one reading from the shared series by its id (see `usage_readings`).
Unknown id answers 404; the storing daemon audits the operation.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Reading id to delete |

Writes (`usage:write`).

### `usage_extra_credit_set`

Set the bought extra credit, in points of the weekly allowance (0 or more),
on the shared series: the effective ceiling is the weekly allowance plus this
figure, per policy. A setting, never a reading — it stays out of the readings
series and is shown only as its own setting. 0 is a valid setting (nothing
extra bought).

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `points` | number | Yes | 0 or more, in points of the weekly allowance |

Writes (`usage:write`).

### `usage_remaining_work_set`

Set the operator's typed remaining work, in points of the weekly allowance
(0 or more), under the run-flat-out policy: the finish-early estimate
compares the queue estimate (this daemon's unfinished tasks × the per-task
cost) against this typed figure, and the typed figure replaces the queue
estimate once present. A setting, never a reading: daemon-local only, it
never enters the shared series and is never sent to another daemon.

`points` is optional: omit it, or pass it as `null`, to clear the typed
figure — the setting is removed and the finish-early estimate falls back to
the queue estimate.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `points` | number | No | 0 or more, the typed remaining work in points; omit or pass `null` to clear |

Writes (`usage:write`).

### `usage_autolanes_status`

Read-only status of the daemon's auto-lanes loop (the opt-in that applies the
calculator's lane advice to the lanes it runs, capped by the operator maximum
in `usage.calculator.auto_lanes_max`): whether it is enabled, the policy in
force, the operator maximum, the cap currently applied, what a fresh advice's
decision would do to that cap (target and direction), the settings' last
change, and the loop's recent decisions, newest first.

**Parameters:** none.

Reads (`usage:read`). Changes go through the config path.

### Examples

One call per tool:

```
usage_advice(lanes=5, cost=2.5)
usage_advice(variant="pace-gauge", policy="run-flat-out")
usage_readings()
usage_reading_add(percent=42.5, at="2026-12-15T09:00:00Z", session_percent=33, lanes=2)
usage_reading_add(spend_usd=6.75)
usage_reading_delete(id="r-8f2c1d")
usage_extra_credit_set(points=25)
usage_remaining_work_set(points=15.5)
usage_remaining_work_set()            # points omitted — clears the typed figure
usage_remaining_work_set(points=null) # same, explicit
usage_autolanes_status()
```

The calculator variant and policy are plain daemon configuration, so they are
set with the general `config_set` tool, not a usage tool:

```
config_set(key="usage.calculator.variant", value="pace-gauge")
config_set(key="usage.calculator.policy", value="run-flat-out")
```

## Example Workflows in Cursor

Once configured, you can ask Claude in Cursor:

**Start a task:**
```
Start a datawatch session to write unit tests for the auth package in /home/<user>/myproject
```
→ Claude calls `start_session(task="write unit tests...", project_dir="/home/<user>/myproject")`

**Check progress:**
```
What's the status of my sessions?
```
→ Claude calls `list_sessions` and `session_output` for each active session

**Reply to a prompt:**
```
Session a3f2 is waiting — tell it yes, overwrite the file
```
→ Claude calls `send_input(session_id="a3f2", text="y")`

**Kill a runaway session:**
```
Kill session b7c1
```
→ Claude calls `kill_session(session_id="b7c1")`

---

## Remote AI Agent Example

A remote AI agent (running in a cloud function, GitHub Action, or autonomous pipeline)
can use the SSE transport to manage sessions on your machine:

```python
# Example: autonomous agent triggers a coding task via MCP
import anthropic

client = anthropic.Anthropic()

# The agent has access to datawatch MCP tools
response = client.messages.create(
    model="claude-opus-4-6",
    max_tokens=1024,
    tools=[...],  # MCP tools from datawatch SSE server
    messages=[{
        "role": "user",
        "content": "Start a session to run database migrations in /opt/myapp, wait for it to complete, and report the result."
    }]
)
```

The agent will:
1. Call `start_session` to kick off the task
2. Poll `session_output` to monitor progress
3. Call `send_input` if confirmation is needed
4. Return the final output when the session completes

---

## Configuration Reference

Full MCP config block:

```yaml
mcp:
  enabled: true            # enable MCP server (default: true)
  sse_enabled: false       # enable HTTP/SSE transport for remote clients
  sse_host: "0.0.0.0"     # SSE server bind address (default: 0.0.0.0)
  sse_port: 8081           # SSE server port (default: 8081)
  public_url: ""           # public base URL advertised to clients when behind a reverse proxy (empty = bind address)
  token: ""                # bearer token for SSE connections (strongly recommended)
  tls_enabled: false       # enable TLS for SSE server
  tls_auto_generate: true  # auto-generate self-signed cert (default: true when tls_enabled)
  tls_cert: ""             # path to PEM cert (overrides auto-generate)
  tls_key: ""              # path to PEM key (overrides auto-generate)
```

The `datawatch mcp` CLI command:

```bash
# stdio mode (for IDE clients)
datawatch mcp

# SSE mode (for remote clients, overrides config)
datawatch mcp --sse
```

- **`secret_scan_config_get`** / **`secret_scan_config_set`** — read and change the push-time secret scan (`git.secret_scan`: `mode` off, shadow or enforce; `detectors`; `allow_list_paths`; `allow_list_fingerprints`). The setter refuses an unknown mode, an unknown detector name or a malformed fingerprint, and honours `allow_self_config`. See [secret scan push gate](howto/secret-scan-push-gate.md).

- **`autonomous_attention_list`** — lists the Automata that need the operator's attention (`GET /api/autonomous/attention`). Each item carries `rules_review_outcome` (`action`, `findings`, `retries`) when the completion review blocked it. An Automaton whose review blocked with no retry left sits in `needs_review` until you add work or decide.
- **`autonomous_prd_get` / `autonomous_status` — `rules_review_outcome`.** Carried on an Automaton whose completion review blocked: what was done (`action`), the `findings`, and how many `retries` were used. The `auto_fix_retries` setting in `autonomous_config_set` is the number of content auto-fix retries per Automaton (default 1); once spent, the Automaton goes to `needs_review` with the findings attached.
- **`secret_scan_stats`** — read the daemon's pre-push secret-scan counters with the effective `git.secret_scan` mode (off, shadow or enforce — unset defaults to shadow). Read-only: it proxies `GET /api/git/secret-scan/stats`, so it changes nothing and runs no scan, push or judge. The counters are in-memory since daemon start and reset on daemon restart.

  **Parameters**: none.

  **Returns** the REST body as-is — `mode`, plus `stats` with `runs` (scans that executed), `findings`, `shadow_reports` (shadow-mode findings, the push proceeded) and `refusals` (enforce-mode refusals), and `last_run_at` / `last_refusal_at` (RFC 3339, the zero time `0001-01-01T00:00:00Z` when never recorded). Needs the health read capability when called by a federation peer.

  ```
  secret_scan_stats()
  # {"mode":"shadow","stats":{"runs":4,"findings":1,"shadow_reports":1,"refusals":0,
  #  "last_run_at":"2026-11-01T10:12:05Z","last_refusal_at":"0001-01-01T00:00:00Z"}}
  ```

