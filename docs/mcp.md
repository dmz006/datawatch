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
| Vision | `vision_describe` |
| Web Search | `websearch_providers_list/get/add/update/delete/enable/disable/test`, `websearch_stats`, `websearch_history`, `web_search_stats` (deprecated alias) |
| Exit Hooks | `exit_hook_list`, `exit_hook_add`, `exit_hook_delete`, `exit_hook_enable`, `exit_hook_disable` |
| Work Queue | `queue_push`, `queue_claim`, `queue_complete`, `queue_fail`, `queue_list` |
| Discussion Subscribe | `discussion_subscribe`, `discussion_unsubscribe`, `discussion_subscriptions` |
| Result Store | `result_put`, `result_get`, `result_list`, `result_delete` |
| Scheduled Job Results | `post_result`, `schedule_results_list` |
| Automata | `autonomous_status`, `autonomous_config_get/set`, `autonomous_prd_list/create/get/decompose/approve/resume/reject/request_revision/edit_task/instantiate/run/cancel/rejudge/set_kind/set_llm/set_task_llm/set_task_verification_llm/set_story_verification_llm/set_verification_llm/children`, `autonomous_learnings`, `autonomous_progress`, `autonomous_progress_trend`, `autonomous_lane_fill_status`, `autonomous_triage`, `security_failure_list`, `supervisor_watch_list`, `supervisor_watch_stats`, `autonomous_decisions`, `autonomous_merge`, `autonomous_safe_to_delete`, `automation_merged_members`, `autonomous_prd_set_lane_preference`, `capacity_status` |
| Usage back-off | `usage_backoff_status`, `usage_backoff_set`, `usage_backoff_clear`, `usage_backoff_recheck` |
| Usage calculator | `usage_advice`, `usage_readings`, `usage_reading_add`, `usage_reading_delete`, `usage_extra_credit_set`, `usage_remaining_work_set` |
| Lifecycle subscriptions (BL427) | `autonomous_subscribe`, `autonomous_unsubscribe`, `autonomous_subscriptions`, `autonomous_subscription_update`, `autonomous_subscription_test`, `autonomous_events_pending` |
| Search before create | `autonomous_overlap_search` |
| Orchestrator | `orchestrator_graph_create/plan/run/get/list/cancel`, `orchestrator_verdicts`, `orchestrator_config_get/set` |
| Pipelines | `pipeline_start/list/status/cancel` |
| Memory + KG | `memory_remember/recall/list/forget/export/import/reindex/stats/learnings`, `kg_add/query/timeline/stats/invalidate`, `research_sessions`, `get_prompt`, `copy_response` |
| Observer | `observer_stats`, `observer_envelopes`, `observer_envelopes_all_peers` (BL180 cross-host), `observer_envelope`, `observer_peers_list/get/register/delete`, `observer_peer_stats`, `observer_agent_list/stats`, `observer_config_get/set`, `ollama_stats` |
| Agents | `agent_list/get/spawn/terminate/logs/audit` |
| Plugins | `plugins_list/reload`, `plugin_get/test/enable/disable` |
| Profiles + projects | `profile_list/get/create/update/delete/smoke`, `project_list/summary/upsert`, `project_alias_delete` |
| Templates / scheduling / cooldown | `template_list/upsert/delete`, `schedule_list/add/cancel`, `cooldown_status/set/clear` |
| Devices + routing | `device_alias_list/upsert/delete`, `routing_rules_list/test` |
| Channel bridge | `channel_info`, `channel_diagnostics` |
| Cost + audit + config + alerts | `cost_summary/usage/rates`, `analytics`, `audit_query`, `audit_access_query`, `get_config`, `config_set`, `get_stats`, `get_version`, `diagnose`, `reload`, `restart_daemon`, `splash_info`, `get_alerts`, `create_alert`, `mark_alert_read` |
| Identity | `get_identity`, `set_identity`, `update_identity`, `configure_identity`, `identity_backups`, `restore_identity`, `reset_identity` |
| Saved commands | `list_saved_commands`, `send_saved_command` |
| Ask / assist | `ask`, `assist` |
| Voice | (no MCP tools — REST `/api/voice/transcribe` + chat-channel auto-handle) |

Autonomous PRD reads and writes:

- **`autonomous_prd_get`** — takes an optional `detail` argument that sets how much comes back. The default is `summary`. Any other value is rejected with an error.
  - `summary` returns the Automaton's ids, titles, statuses and counts, and the same for its stories and tasks.
  - `tasks` returns everything in `summary`, plus each task's status, error and verification summary.
  - `full` returns everything, including task specs and logs, and the `foreign_file_drift` field on tasks.
- **Behaviour change for PRD write tools.** `autonomous_prd_reset_task`, `autonomous_prd_run`, `autonomous_prd_approve`, `autonomous_prd_cancel`, and the set, add and edit tools no longer return the whole Automaton. Each write reply is now a small object with the Automaton's `id`, its `status` and `rev`, the id and status of the task or story that changed, and a `message`. To read the full Automaton after a write, call `autonomous_prd_get` with `detail` set to `full`.

Tools added in v5.9 → v5.26 (catch-up since the last doc sweep):

- **`autonomous_prd_resume`** — resume a PRD the boot-resume gate parked after a daemon restart (also resumes a paused PRD). Takes `id` and an optional `note`; errors if the PRD is in neither state.
- **`autonomous_prd_children`** (v5.9.0, BL191 Q4) — list child PRDs spawned from a parent's `Task.SpawnPRD` shortcuts.
- **`autonomous_prd_edit_task`** (v5.9.0+) — edit a task's `spec` while a PRD is in `needs_review`, `revisions_asked`, `cancelled`, or `blocked` (per-task `backend` / `effort` / `model` go through `autonomous_prd_set_task_llm`). On a `blocked` PRD only the `failed`/`blocked` task that halted it can be edited, and a `completed` task's spec is immutable in every status. Recovery flow for a stuck task: `autonomous_prd_edit_task` to fix the spec, then `autonomous_prd_reset_task` to retry it — completed sibling tasks are left untouched, no re-decompose needed.
- **`autonomous_prd_edit_story`** — rewrite a story's `title`/`description` while a PRD is in `needs_review` or `revisions_asked`.
- **`autonomous_prd_add_story`** / **`autonomous_prd_remove_story`** — add a new, empty story, or remove a story (and its tasks), without re-running decompose. Only allowed pre-approval.
- **`autonomous_prd_add_task`** / **`autonomous_prd_remove_task`** — add a new task to an existing story, or remove one, without re-running decompose. Only allowed pre-approval. `remove_task` is distinct from `autonomous_prd_cancel_task`, which stops a running/queued task without deleting it from the PRD.
- **`autonomous_prd_cancel`** — cancel a PRD (proxies bare `DELETE /api/autonomous/prds/{id}`) and best-effort kill its worker sessions. Response: `id`, `status` (`"cancelled"`), `killed_sessions` (count of session slots recorded on the PRD's tasks), and `killed_session_ids` (array of the session IDs a kill was actually attempted on — always an array, `[]` when none). A `kill_errors` map (`{"<id>": "<error>"}`) is added only when a kill failed. Use `killed_session_ids` to verify no unrelated session was stopped.
- **`autonomous_prd_set_llm`** (v5.4.0; v8.20.0 extended; v8.33.9 priority fix) — operator-pinned LLM override at the PRD level. Parameters: `backend` (task-execution backend — opencode, claude-code, goose, etc.), `effort`, `model`, and `decomposition_profile` (planning/decompose backend — any configured LLM; opencode/claude-code spawn a full session with codebase tool access, ollama/openwebui run headless; empty = use global `autonomous.planning_backend`). Per-PRD `decomposition_profile` takes priority over the global setting. Optional `guardrail_backend` / `guardrail_model` set the guardrail/verification judge LLM override; it inherits task → Automaton → global `autonomous.verification_backend` / `verification_model`. Only the guardrail params you pass are sent (omitted = unchanged, `""` = clear), and a call with only guardrail params leaves the worker LLM untouched.
- **`autonomous_prd_set_task_llm`** (v5.4.0) — operator-pinned LLM override at the task level (overrides the Automaton-level execution backend for that specific task). Accepts the same optional `guardrail_backend` / `guardrail_model` params with the same absent-vs-empty semantics, scoped to the one task. Acceptance is governed by `autonomous.task_override_statuses` (default `needs_review` / `revisions_asked` / `cancelled` / `blocked`).
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
  | `tag_id` | no | Count only the Automata carrying this tag |
  | `since` | no | First day counted, `YYYY-MM-DD` (default: the configured date, else the first record's creation date) |
  | `until` | no | Last day counted, `YYYY-MM-DD`, inclusive (default: now) |

  ```
  autonomous_progress()
  autonomous_progress(tag_id="t-ui", since="2026-10-10")
  ```

  A missing backlog file is not an error: the reply carries `backlog_available: false`. A bad date, or `until` before `since`, is refused. Needs the autonomous read capability when called by a federated peer.
- **`autonomous_progress_trend`** — read the progress trend from the saved daily snapshots. Read-only; returns the same body as `GET /api/autonomous/progress/trend`: `days`, `points` (one per snapshot, oldest first: `date`, `reason`, `completion_low_days`, `completion_high_days`, `known_tasks`, `estimated_tasks`, `throughput_3d`, `utilization`, `completed_prds`) and `direction` (`converging`, `widening`, `flat` or `unknown`).

  | Parameter | Required | Description |
  |---|---|---|
  | `days` | no | How many days back to read, a whole number from 1 to 365 (default 30) |

  ```
  autonomous_progress_trend()
  autonomous_progress_trend(days=90)
  ```

  Without saved snapshots `points` is empty and `direction` is `unknown`. A `days` value outside 1 to 365 is refused.
- **`autonomous_lane_fill_status`** — read the lane fill status: each GPU and Claude lane with what it is running or waiting on, whether it is idle, the newest planner decisions (newest first) and any idle-lane alerts. Read-only; returns the same object as `GET /api/autonomous/lane-fill`.

  | Parameter | Required | Description |
  |---|---|---|
  | `limit` | no | How many decisions to return, a whole number from 1 to 1000 (default 50) |

  ```
  autonomous_lane_fill_status()
  autonomous_lane_fill_status(limit=200)
  ```

  A `limit` outside 1 to 1000 is refused with the daemon's message. Needs the autonomous read capability when called by a federated peer.
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
- **`security_failure_list`** — list the stored security failure reports, newest first, as a JSON list. Each entry carries the failing security run's identity (`repo`, `ref_kind`, `ref_name`, `workflow`, `run_id`, `run_url`, `status`) and the report stored for it under `report`: `dependency`, `advisory_ids`, `workflow`, `run_id`, `run_url`, `sha`, `published_at`, `fixed_version`, the accepted-risks registry result (`registry_found`, `registry_id`, `registry_first_added`, `registry_added`, `registry_expires`, `registry_error`), `lookup_error`, `recommendation`, `note`, the `mode` it was made under, the `action` the Automaton line received (`report_only`, `would_stop` or `stopped`) and `assessed_at`. Read-only: listing never reruns, cancels, stops or edits anything. Returns the same list as `GET /api/autonomous/security-failures`; nothing matching is `[]`.
- **`supervisor_watch_stats`** — read the supervisor watch as numbers: `mode`, `last_poll_at` and `last_poll_age_seconds` (-1 when the coordinator never polled), `findings_open`, `findings_would_alert`, `findings_alerted`, `findings_resolved` and `agreed_days` (days of full agreement with the supervisor script). Takes no arguments; any argument is refused. Read-only. Returns the same object as `GET /api/autonomous/supervisor-watch/stats`.
- **`supervisor_watch_list`** — read the supervisor watch state as one JSON object: the `mode` in effect (`off`, `shadow` or `on`; an unset mode reads `shadow`, which reports only and never acts), `last_poll_at` (the instant the coordinator last polled), the stored `findings` (open first, most recently seen first: `kind`, `subject`, the operator line, first and last seen, the shadow would-alert mark and the raised-alert instant) and the `comparison` against the supervisor script's record. Takes no arguments; any argument is refused. Read-only: listing never raises an alert, resolves a finding or changes the mode. Returns the same object as `GET /api/autonomous/supervisor-watch`.

  | Parameter | Required | Description |
  |---|---|---|
  | `repo` | no | only the reports of this repository path |
  | `status` | no | only reports whose run is in this watch state: `green`, `failing`, `pending`, `hung`, `rerunning` or `superseded` (anything else is refused) |

  ```
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
- **`autonomous_prd_rejudge`** — re-judge the PRD-completion "rules-review" guardrail of a **blocked** Automaton whose work is fully done, on an operator's behalf, without resetting or editing any task. Use it when the guardrail verdict that blocked the Automaton deserves a fresh evaluation (new evidence, revised gate settings) but no task needs another attempt. Refused — with a clear error, no side effects on any task — when the Automaton is not `blocked`, or when any task in any story is not `completed` (the refusal names the first such task and its status; re-run that work with `autonomous_prd_reset_task` first). The identical completion-guardrail invocation the rollup runs is re-executed through the same infra-retry wrapper. Outcome: the verdict is durably recorded on the Automaton's decision log (`guardrail_verdict` + a `guardrail_rejudge` Decision attributed to the acting operator) **before** any status change — `pass` clears the blocked reason and completes the Automaton, `block` keeps it `blocked` with the blocked reason set from the new verdict, and no verdict (judge error or empty outcome) keeps it `blocked` with its previous reason and returns an error. Proxies `POST /api/autonomous/prds/{id}/rejudge` (see [howto/autonomous-recovery.md](howto/autonomous-recovery.md)).

  | Parameter | Required | Description |
  |---|---|---|
  | `id` | yes | Automaton (PRD) ID |

  ```
  autonomous_prd_rejudge(id="a1b2c3d4")
  ```

  Returns the write reply described above (a small summary, not the whole Automaton); the guardrail verdict and the `guardrail_rejudge` Decision are in the Automaton's decision log, readable with `autonomous_prd_get`. The REST body `actor` (default `operator`) is fixed to `operator` here, as with the other PRD write tools.
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
- **`automation_pause`, `automation_resume`, `automation_cancel`, `automation_skip_member`, `automation_insert_member`, `automation_remove_member`, `automation_reorder_edges`, `automation_approve_insert`, `automation_set_budget`, `automation_set_decision_assistant`** (Phase 3b) — operator control of a running Automation; see "Automation control tools" below. Over REST these are `POST /api/automations/{id}/{pause|resume|cancel|skip_member|insert_member|remove_member|reorder_edges|approve_insert|set_budget}`. `automation_get` and `automation_list` return each Automation with its computed `budget` view, and `autonomous_status` carries the same `budget` on each Automation row. `automation_get` also returns the record's `pending_inserts` (omitted when none); each entry's `id` is the `member_id` to pass to `automation_approve_insert`.

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

- **Automation control tools (Phase 3b).** Eight tools steer a running Automation. Each proxies to `POST /api/automations/{id}/<operation>` (see [api/autonomous.md](api/autonomous.md) for the full semantics, response codes and audit entries). Every tool takes these arguments, and a call that leaves out a required one is refused before any request is sent:

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
  | `automation_insert_member` | `prd_id` (**required**), `reason` (**required**), `name`, `urgent`, `after` | Inserts a PRD as a member. `urgent` takes the next slot and `false` appends to the end (or goes after `after`); an urgent insert must leave `after` empty. On a draft the member lands at once. On an `active`, `paused` or `blocked` Automation it is held as a pending insert until `automation_approve_insert`, and only one urgent insert may be pending at a time (`409` otherwise). |
  | `automation_remove_member` | `member_id` (required) | Removes a member that has not started and has no outgoing edges. The entry member, forks, joins and branches are refused. |
  | `automation_reorder_edges` | `member_id` (required), `on_complete`, `on_blocked`, `on_failed` | Rewires the edges of a member that has not started. Each argument is the member ID the edge now names; an edge left out is cleared, so pass every edge to keep. |
  | `automation_approve_insert` | `member_id` (required) | Promotes a pending insert into the graph. `member_id` is the pending insert's `id` — the one `automation_insert_member` returned in `pending_inserts[].id`, or the same `pending_inserts[].id` you read on `automation_get`. Only the operator may approve. |
  | `automation_set_budget` | `member_id`, `ceiling_type` (`dollar` or `allocation_pct`), `ceiling_value`, `window` (`automation`, `day`, `week` or `month`), `pacing` (`none` or `even`), `on_breach` (`pause` or `block`), `backend`, `allocation_estimate`, `peer_window` (array of peer names), `reset_time`, `reset_weekday`, `reset_day`, `reset_zone`, `estimated_cost_usd` (member only), `clear` | Sets, replaces or clears the budget policy of the Automation, or the override of one member when `member_id` is given. The policy fields replace the whole policy; `clear` removes it and cannot be combined with them. The status is not changed by this call. On a breach the Automation pauses and resumes by itself at the window rollover only when the reset time is known (a window the daemon defines itself, anchored at the declared reset or the calendar default, or a reset time parsed from a rate-limit message); when the reset time is unknown it goes to `blocked` and waits for the operator. An alert is raised either way. `allocation_estimate` is off by default, is always labelled estimated and never pauses or blocks. Another daemon's spend counts toward the ceiling only for peers named in `peer_window`; that is off by default and a shared account is not confirmed automatically. |
  | `automation_set_decision_assistant` | `member_id`, `backend`, `model`, `mode` (`shadow` or `live`), `hooks` (array of `empty_edge`, `skip_member`, `insert_member`, `budget_breach`), `thresholds` (object, hook to a number in (0,1]), `clear` | Sets or clears the optional local-model decision assistant of the Automation, or of one member when `member_id` is given. Off by default. The fields replace the whole setting; `clear`, or an empty `backend`, turns it off. `shadow` (the default) only records what the assistant would have done; `live` acts at or above the hook threshold (default 0.9, `budget_breach` 0.95). The assistant only picks among already-allowed options. The setting comes back as `decision_assistant` on `automation_get`. |

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
- **`autonomous_config_get` / `autonomous_config_set` — judge routing.** `judge_policy` (array, default empty) is the ordered list of LLM backends the judges (Automaton guardrail, plan review and other autonomous judges) try, one attempt per backend; empty means the judges follow the verifier routing policy. `judge_timeout_seconds` (number, default `1800`, i.e. 30 minutes) is the time limit for one judge run and does not count the wait for a capacity slot; `0` or a negative value means the default. A judge moves to the next backend when its run hits the limit; when every backend has timed out the Automaton is held in `needs_review` and an error alert lists every attempt. `judge_size_threshold_lines` (number, default `400`) is the changed-line count (added plus deleted, on the Automaton branch against its merge base, lock files left out) above which the judges try `claude-code` first and the rest follow in their usual order, still one attempt per backend; in `balance` mode the load still decides and the order only breaks ties; `0` turns the rule off and a negative value is refused. `judge_policy` replaces the whole list, so read it with `autonomous_config_get` first. Same keys over REST/CLI/comm as `autonomous.judge_policy` / `autonomous.judge_timeout_seconds` / `autonomous.judge_size_threshold_lines`.
- **`autonomous_config_get` / `autonomous_config_set` — `block_on_file_drift`.** Boolean, default `false`. Daemon-wide default for the foreign-file-drift check that runs after every direct-mode (non-worktree) task: `false` only flags a task whose own commit(s) swept in files that were already uncommitted (modified, staged or untracked) before it started and are not in its planned `files`; `true` sets that task to blocked (`error: "foreign_file_drift: <files>"`) and halts the walk. `autonomous_config_set` forwards it only when supplied. Same key over REST/CLI/comm/YAML as `autonomous.block_on_file_drift`; PWA Settings → Automate → Block on file drift. See [howto/file-drift-guardrail.md](howto/file-drift-guardrail.md).
- **`autonomous_config_get` / `autonomous_config_set` — `scope_lint`.** String, one of `off`, `warn` or `enforce`, default `enforce`. Sets the pre-spawn scope lint: `off` skips it, `warn` records problems as a `scope_lint_warn` decision and spawns anyway, `enforce` blocks the task. Placeholder and quoted pattern text is never flagged. `autonomous_config_set` forwards it only when supplied, and the tool schema declares the three values as an enum. Same key over REST/CLI/comm/YAML as `autonomous.scope_lint`; PWA Settings → Automate → Task scope lint. See [operations.md](operations.md#task-scope-lint).
- **`autonomous_config_get` / `autonomous_config_set` — `max_concurrent_tasks`.** Integer, default `1`. Daemon-wide default for how many independent tasks of one Automaton run at once; an Automaton's own setting (`max_concurrent_tasks` on the Automaton) overrides it, and `1` means sequential. `autonomous_config_set` forwards it only when supplied and `>= 1`. Same key over REST (`PUT /api/config` `autonomous.max_concurrent_tasks`, `PUT /api/autonomous/config`), CLI (`datawatch autonomous config-set '{"max_concurrent_tasks":2}'`), comm (`configure autonomous.max_concurrent_tasks=2`) and YAML. `max_parallel_tasks` is **deprecated**: it is accepted and ignored (it never had an effect), and the response carries a `deprecated_fields` note naming `max_concurrent_tasks`; it will be removed in a future release.
- **`autonomous_config_get` / `autonomous_config_set` — `task_override_statuses`.** Array of Automaton status names (any of `draft`, `planning`, `needs_review`, `approved`, `revisions_asked`, `active`, `running`, `completed`, `rejected`, `cancelled`, `archived`, `blocked`, `failed`, `paused`); default `["needs_review","revisions_asked","cancelled","blocked"]`; an empty array restores the default. Controls which statuses accept task-level and story-level LLM override calls (`autonomous_prd_set_task_llm`, story-level LLM, per-task guardrail / verification LLM); Automaton-level overrides are not affected. Same key over REST (`PUT /api/config` `autonomous.task_override_statuses`), CLI (`datawatch config set autonomous.task_override_statuses blocked,running`), comm channel (`configure autonomous.task_override_statuses=blocked,running`) and YAML. Unknown status names are rejected with the list of known names.
- **`autonomous_config_get` / `autonomous_config_set` — `undeclared_edit_mode`.** String, `off`|`warn`|`block`, default `warn`. Daemon-wide enforcement for the undeclared-edit check that runs after every direct-mode task: it flags files the task's own worker committed that are in neither its planned `files` nor its generated files (pre-existing dirty files stay under `drift_mode`; a file is never reported by both). `off` = no-op; `warn` = record `undeclared_edits` on the task, an `undeclared_edit_detected` decision, an audit event and a warn alert, task completes; `block` = task blocked with `undeclared_edit: <files>` and the walk halts. `autonomous_config_set` forwards it only when supplied; invalid values are rejected. Same key over REST (`PUT /api/config` `autonomous.undeclared_edit_mode`), CLI (`datawatch config set autonomous.undeclared_edit_mode warn`), comm (`configure autonomous.undeclared_edit_mode=warn`) and YAML; PWA Settings → Autonomous Automata planning → Undeclared edit mode. See [howto/file-drift-guardrail.md](howto/file-drift-guardrail.md).
- **`autonomous_config_get` / `autonomous_config_set` — `gate_disagreement`.** String, `warn`|`block`, default `warn`. What happens when a task's own verifier passed but the quality gate confirmed a regression from a valid baseline: `warn` records `gate_disagreement` on the task and in the decision log and the task completes; `block` fails the task with a `gate_disagreement: …` error naming the new failing tests. An invalid gate (a baseline that could not produce usable results, reported as `quality_gate_status: "invalid"` on the Automaton) never blocks under either value. `autonomous_config_set` forwards it only when supplied; any other value is rejected. Same key over REST (`PUT /api/config` `autonomous.gate_disagreement`), CLI (`datawatch autonomous config-set '{"gate_disagreement":"block"}'`) and comm (`configure autonomous.gate_disagreement=block`). See `docs/operations.md` → "Quality gate".
- **`autonomous_config_get` / `autonomous_config_set` — `descendant_stall_seconds`.** Number, default `2700` (45 minutes); `0` or a negative value is normalized to the default, so the detector can never be silently disabled. Bound on one pi-prompt autonomous worker, in seconds: a run whose runner has exited but whose work descendant (a build, a test, a long command) is still alive, with no new log write and no new workdir file, exceeds this bound and the attempt ends as an infra failure — the recorded descendants are cleaned up (5-second grace, a real kernel signal, only where the recorded identity still verifies), the session ends, and the task is retried through the `auto_fix_retries` budget. `autonomous_config_set` forwards it only when supplied; it is read back in `autonomous_config_get`. Same key over REST (`PUT /api/config` `autonomous.descendant_stall_seconds`), CLI (`datawatch config set autonomous.descendant_stall_seconds 2700`), comm channel (`configure autonomous.descendant_stall_seconds=2700`) and YAML. See [operations.md](operations.md#descendant-only-stall-bound-autonomousdescendant_stall_seconds).
- **`guardrail_profile_create` / `guardrail_profile_update` / `per_automaton_guardrails_set` — `block_on_file_drift`.** Optional boolean on all three. Enforcement mode is daemon-wide: `autonomous.drift_mode` (`block` = the default, or `warn`). Under `block`, an explicit `block_on_file_drift: false` on a profile (for its Automata) or on `per_automaton_guardrails_set` (that Automaton itself) opts that scope out of blocking — its findings become warn-only; unset means inherit the mode. The daemon-wide flag's `false` default is the legacy opt-in shape and by itself is not an opt-out. Worktree-mode Automata are never checked.
- **`autonomous_prd_get` — `foreign_file_drift` task field.** Each task may carry `foreign_file_drift` (string array, absent when empty): the files its own commit(s) in `<pre-task SHA>..HEAD` touched that were already uncommitted before the worker session started and are not in the task's planned `files`. Findings accumulate across retries. Each finding also appends a `file_drift_detected` entry to the Automaton's `decisions` (actor `autonomous`, `note` lists the files) and writes an `autonomous_file_drift_detected` audit event. Nothing in the repository is reverted or rewritten. See [howto/file-drift-guardrail.md](howto/file-drift-guardrail.md).
- **`autonomous_prd_get` — `ci_watch` field.** When the `ci-watch` completion guardrail ran, the Automaton includes `ci_watch {status, sha, repo, runs[], summary, category, issues, checked_at}`; absent when it never ran. `status`: `skipped` (nothing pushed, origin not GitHub, not a git repo, or `gh` missing; `summary` says which), `pass`, `warn` (judge flagged a non-blocking concern), `pending` (CI unfinished at the timeout; one follow-up check rewrites it later), `failed_policy` (project rules say an operator decides; Automaton blocked, nothing auto-fixed), `failed_content` (fixable finding; a corrective task is appended to the same Automaton when content auto-fix is enabled, otherwise blocked), `failed_infra` (runs cancelled/timed out/never started and the infra retry budget is spent; blocked), `error` (no judge configured; not blocking). `runs[]` items: `id`, `name`, `status`, `conclusion`, `url`. See [howto/ci-watch-guardrail.md](howto/ci-watch-guardrail.md).



### `list_sessions`

List all AI coding sessions on this host, including their state and task description.

**Parameters:**

| Parameter | Type | Required | Description |
|---|---|---|---|
| `name` | string | No | BL361 — glob pattern to filter sessions by name (e.g. `worker-*`, `agent-?`). Empty = all. |
| `state` | string | No | BL361 — filter by exact state: `running`, `waiting_input`, `rate_limited`, `complete`, `failed`, `killed`. Empty = all. |
| `backend` | string | No | BL361 — filter by backend family name (e.g. `opencode`, `claudecode`). Empty = all. |
| `alive` | string | No | BL361 — filter by `claude_alive` field: `true`, `false`, or `any` (default). Empty = any. |
| `format` | string | No | BL361 — output format: `text` (default) or `json`. `json` returns a structured JSON array. |
| `orphaned` | boolean | No | BL350 — when `true`, return only sessions whose `parent_id` points to a session that no longer exists (orphaned by parent death or eviction). |

**Example response:**
```
Sessions on my-server:

ID:      a3f2
State:   running
Task:    write unit tests for the auth package
Dir:     /home/me/myproject
Updated: 2026-03-26T14:32:01Z

ID:      b7c1
State:   waiting_input
Task:    add Docker support
Dir:     /home/me/myproject
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
Dir:     /home/me/myproject
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

### `vision_describe`

Describe an image file using the configured vision model. Returns a natural-language description. Requires `vision.enabled: true` in config.

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
| `configure_identity` | Returns where the setup wizard lives (PWA, CLI) and how the backup tools fit; it changes nothing. | Yes |
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

### `usage_advice`

One advice result: the latest reading of the shared series (percent and/or
spend, when, session percent, lanes), the extra credit and effective
ceiling, lane choice (recent lanes and the max), the pace line and headroom
after the safety margin, the one burn figure (the weekly average, with a
fallback notice when the recent window fell back to 48h), the projected end
of cycle per burn line, run-out warnings, the finish-early estimate (queue ×
per-task cost vs the typed remaining work) under the run-flat-out policy, and
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
Start a datawatch session to write unit tests for the auth package in /home/me/myproject
```
→ Claude calls `start_session(task="write unit tests...", project_dir="/home/me/myproject")`

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
