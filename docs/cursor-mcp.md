# Cursor / Claude Desktop / VS Code MCP Integration

datawatch exposes an MCP (Model Context Protocol) server, letting Cursor and other
MCP-compatible clients list, start, and interact with AI coding sessions directly from
the IDE — no chat app required.

## Local Setup (Cursor / Claude Desktop)

The MCP server runs over stdin/stdout as a subprocess — no network port needed.

Add to your Cursor MCP config (`~/.cursor/mcp.json` or via **Cursor → Settings → MCP**):

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

For Claude Desktop (`~/.config/claude/claude_desktop_config.json`):

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

Restart Cursor/Claude Desktop after saving.

## Remote Setup (SSE — for remote AI agents)

The MCP SSE server allows **remote AI agents** (e.g. Claude API, OpenAI Assistants,
autonomous agents running elsewhere) to connect to datawatch over HTTPS and manage
sessions on your behalf.

Enable in `~/.datawatch/config.yaml`:

```yaml
mcp:
  sse_enabled: true
  sse_host: "0.0.0.0"
  sse_port: 8081
  token: "your-secret-token"      # Bearer token required for all SSE connections
  tls_enabled: true
  tls_auto_generate: true          # Auto-generates a self-signed cert in ~/.datawatch/tls/mcp/
```

Then start datawatch:

```bash
datawatch start   # starts SSE server alongside all other backends
# OR standalone:
datawatch mcp --sse
```

Remote AI client config (OpenAI Assistants, Claude API tool use, etc.):

```json
{
  "mcpServers": {
    "datawatch": {
      "url": "https://your-host:8081/sse",
      "headers": {
        "Authorization": "Bearer your-secret-token"
      }
    }
  }
}
```

### TLS and Post-Quantum Cryptography

When `tls_enabled: true`:

- TLS 1.3 is enforced (minimum version)
- Post-quantum hybrid key exchange (X25519Kyber768Draft00) is negotiated automatically
  by Go 1.23+ when a compatible client connects — no extra config needed
- A self-signed certificate is auto-generated at `~/.datawatch/tls/mcp/cert.pem`
  and persisted across restarts (valid 10 years)
- To use a CA-signed certificate, set `tls_cert` and `tls_key` paths instead

The same TLS configuration applies to the PWA server (`server.tls_enabled: true`).

## Remote Use via SSH (no port exposure needed)

If datawatch runs on a remote server, use SSH stdio forwarding for local IDE clients:

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

No ports to open — the MCP protocol runs over the SSH connection.

## Available Tools

datawatch exposes 100+ MCP tools across sessions, autonomous PRDs,
orchestrator, pipelines, memory, observer, agents, plugins,
profiles, templates, schedule, cooldown, devices, routing, cost,
audit, alerts. The authoritative live list is at
`GET /api/mcp/docs` (HTML or `?format=json`); see
[docs/mcp.md](mcp.md) for the family breakdown + per-tool detail.

Selected core tools:

| Tool | Description |
|------|-------------|
| `list_sessions` | List all active AI sessions on this host |
| `start_session` | Start a new AI session (supports `caller_session_id`, `kill_children`, `permission_mode`) |
| `session_output` | Get the last N lines of output from a session |
| `send_input` | Send a reply to a session waiting for input |
| `kill_session` | Terminate a session |
| `session_children` | List child sessions spawned by a parent session (v8.10.0) |
| `reply_to_parent` | Send a message back to the spawning parent session (v8.10.0) |
| `session_tag_list` | List the tags in use across live and archived sessions: each entry has the tag, its `axis` and `value` from the `axis:value` form (`axis` empty for a plain tag) and the count of sessions carrying it — a session counts once per tag and its own, system and inherited tags all count; sorted by axis (tags without an axis last), count (descending), then tag; read-only |
| `cost_report` | Cost report over the daemon's recorded session spend (the `internal/costreport` engine, same engine call as `GET /api/cost/report`): grouped by automaton, automation, backend or week (the `group_by=release` grouping is REST/CLI-only — it needs shipped-unit state this tool does not expose); token totals always, USD estimates when rates are configured, and the billing label read from each backend's LLM entry `billing_mode` (`api`, `allowance` or `local`), then the provider's `usage.billing` profile — a bundled plan (`allowance`) reads “bundled plan — API-equivalent, not billed” with the API-equivalent figure, usage points and share of the monthly price. Dollars and points are never added: a row that spans billing units is `mixed` with no single money figure, and `unit_totals` (per row) and `totals_by_unit` (whole report) give one total per unit; optional `group_by`, `since`, `until` — the grouping and window defaults come from the `cost_report` config section; read-only |
| `cost_charts` | The combined usage report over the spend ledger and Automaton attempts: cost over time, tool usage (a count per group, never a tool name) and retries and failure categories joined into one payload (`cost_over_time`, `tool_usage`, `failures`), each as `GET /api/cost/series`, `GET /api/cost/tool-usage` and `GET /api/cost/failures` return it; money is one total per billing unit, never added across units; optional `group_by` (automaton, project, model or automation — a view that does not know it keeps its default and the payload's notes say so) and `window` (such as `36h`, `7d`, `2w`); read-only |
| `llm_add` / `llm_update` | Add or change an LLM registry entry; both accept `billing_mode` (`api`, `allowance` or `local`) — an unknown value is refused, an empty one is resolved from the `usage.billing` profile, then the kind default. `llm_get` and `llm_list` return the stored `billing_mode` |
| `autonomous_prd_create` / `autonomous_prd_decompose` / `autonomous_prd_run` | Autonomous PRD lifecycle |
| `question_raise` | raise an operator question to this daemon's operator (the operator-question channel) |
| `question_list` | list the daemon's stored operator questions, optionally filtered (the poll read) |
| `project_list` | list the registered project aliases with their record fields |
| `project_upsert` | register or update a project alias and its record (owner, repository, default branch, arcs, target, lane defaults, weekly share, branch retention, major trigger); an omitted field keeps its stored value |
| `project_set_arcs` | replace a project's ordered arc list (`automation_id`, `priority`, `state`); `[]` clears it |
| `project_rollup` | a project's arcs with their Automata, task progress, points and throughput (optional `window`, days) |
| `project_history` | one page of a project's finished Automata, daily progress and Gantt rows (`limit`, `cursor`) |
| `project_timeline` | one level of a project's timeline: start, end, status and dependencies per arc, Automaton and task, plus Automaton chain edges (`level`, `parent`, `automaton`, `focus`, `limit`, `cursor`) |
| `project_policy_effective` | a project's effective policy with the tier that supplies each setting (optional `automaton`) |
| `project_alias_delete` | remove a project alias; the directory is not touched |
| `question_get` | read one stored question and its current answer (the asker's return path) |
| `question_answer` | answer a stored question by option id and/or the operator's own words; a changed mind supersedes, never edits; optional `follow_up` names one configured follow-up (`autonomous.decisions.follow_ups`) that the daemon runs once when the answer is applied |
| `question_withdraw` | withdraw an open question with a reason, attributed to a named actor |
| `autonomous_decision_record_create` / `autonomous_decision_record_update` / `autonomous_decision_record_withdraw` | Manage decision records — the structured operator decision (title, situation, deciding, options with unique ids, default — an option or `hold` — recommendation, label, links); the record is the source, the daemon builds the queue question from it and the answer comes back on it; proxies the POST, PUT and `/withdraw` of `/api/autonomous/decision-records`, needs `autonomous:write` |
| `autonomous_decision_record_list` / `autonomous_decision_record_get` | List or read decision records: `state` (`open`, `answered` or `withdrawn`), `prd_id`, `limit` (1–1000, default 100); listing also returns the daemon's folder-watch status; read-only, needs `autonomous:read` |
| `autonomous_prd_children` | List child PRDs spawned via `Task.SpawnPRD` |
| `autonomous_event_kinds` | List every lifecycle event kind a subscription can match, grouped, with each kind's description and whether it wakes the supervisor by default (read-only, no parameters) |
| `autonomous_prd_set_kind` | Set an Automaton's kind: `feature`, `bug` or `other` (empty derives it again) |
| `autonomous_progress` | Read the progress metrics object: counts by kind, throughput, utilization, remaining work and the completion range; optional `automation_id`, `tag_id`, `since`, `until` |
| `autonomous_progress_trend` | Read the progress trend from the saved daily snapshots; optional `days` (1 to 365, default 30) |
| `autonomous_overlap_search` | Search before create: the Automata, registry rows and unmerged branches that already cover a plan's `title` and `spec`, best match first, with the threshold and mode in force; optional `limit` (1 to 50, default 10); read-only |
| `generated_artefacts_get` | Read the generated-artefact switches in effect and each artefact kind with its format and tracked path; read-only |
| `autonomous_prd_change_record` | Read the change record of an Automaton (`id`): files, packages, settings, routes, locale keys, tests, commands; read-only |
| `autonomous_prd_generated_artefact` | Preview one generated artefact without writing it (`id`, `kind`: `gate_report`, `changelog_draft`, `parity_draft`, `tracker_rows`, `registry_row`); read-only |
| `autonomous_prd_regenerate_artefact` | Re-render one generated artefact and write it to its tracked path (`id`, `kind`); a file a person edited is kept |
| `automation_requirements_report` | Read the automation requirements report: capability minimums (built-in Ollama floor per capability plus the `autonomous.requirements.min_ollama.*` overrides), each node's reported version against it (meets / fails / unknown-fails-closed), the configured backends and the optional-provider state; optional `format` (`text` default, or `json`); a report, not a gate — read-only |
| `automation_model_pull_consent_get` | Read the model-pull consent policy: `models[]` (`name`, `allowed`, `note`), `publishers[]` and the decision rule in words (default-deny: listed-and-allowed, or unlisted with an allow-listed publisher); read-only |
| `automation_model_pull_consent_set` | Set the model-pull consent policy (replaces what is stored): `models` is a JSON array of model-name strings (stored as allowed) or `{name, allowed, note}` objects; `publishers` a JSON array of names; invalid entries are refused (400); audited as `model_pull_consent_changed` |
| `compute_node_backend_versions` | Read one compute node's `name`, `kind`, `address`, `backend`, `backend_version` (with `version_probed_at`) and the per-capability Ollama minimums applied to that version; required `name` (a known compute node, else 404); read-only |
| `autonomous_lane_fill_status` | Read the lane fill status: lanes, idle state, recent planner decisions and idle alerts; optional `limit` (1 to 1000, default 50) |
| `autonomous_prd_set_lane_preference` | Set an Automaton's lane to `gpu`, `claude` or `any` (empty means `any`); takes `id` and `lane_preference` |
| `compute_served_models` | Read which models each compute node serves; optional `node`, `model`, `backend` and `refresh` (true reads the node again); with `model` it also says whether the node would accept it |
| `autonomous_prd_set_task_node` | Pin one task of an Automaton to a compute node; takes `prd_id`, `task_id` and `node` (empty clears the pin) |
| `autonomous_prd_set_story_node` | Pin every task of one story of an Automaton to a compute node; takes `prd_id`, `story_id` and `node` (empty clears the pin) |
| `autonomous_infra_retries` | Read the infrastructure retry state: the retry policy in effect and the tasks retried or held because of an infrastructure failure, most recent first; optional `automaton` and `limit` (1 to 1000, default 50) |
| `autonomous_defect_intake` | Route one defect through the defect queue to the tiny-fix lane, an open draft batch or a new batch; takes `id`, `title`, `project` and optional `description`, `files`, `kind`, `area`, `priority`; returns `route`, `reason` and `batch_id` |
| `autonomous_defect_batches` | List the open defect batches with the story-to-defect mapping of each; optional `project` |
| `autonomous_defect_batch_split` | Split one story out of a defect batch into its own new Automaton; takes `batch_id` and `story_id`; returns `automaton_id` |
| `autonomous_defect_tinyfix_check` | Check whether a change qualifies for the tiny-fix lane without creating anything; takes `files`, `kind` and optional `project`; returns `eligible` and a `reason` |
| `autonomous_triage` | Read the triage records: one record per failed or blocked Automaton, newest first, with its identity, the failed or blocked task and its error, the session end reason, the retry budgets and the append-only notes — beside `alert_minutes`, the triage alert threshold currently in effect; optional `automation_id`, `prd_id` and `status` (`failed` or `blocked`) |
| `autonomous_merge` | Read the Automaton merge state: the `merge_pending_mode` in effect (`off`, `shadow` or `on`), the alert threshold in minutes (`alert_minutes`, `0` = off), one entry per finished Automaton with a work branch and its state (`merged`, `pending`, `needs_look`, `empty` or `unknown`), and in `shadow` mode the decisions the check would have acted on; takes no parameters, read-only, nothing is merged, deleted or pushed |
| `autonomous_merge_shadow_status` | Read the shadow merge plan state (Phase 4, shadow mode): per project the stored plans (`applied` always `false`), the stored comparisons (empty until the post-ship flow persists them) and the matched/different comparison counts per release size (`patch`, `minor`, `major`, `none`); optional `project`, read-only, reads stores only — runs no git, merges nothing, files nothing |
| `autonomous_safe_to_delete` | Read the safe-to-delete report for Automaton work branches and worktrees: `safe` (can be cleaned up), `held` (must stay: not merged yet, needs a look, or a worktree with uncommitted changes) and `held_dirty`; takes no parameters, read-only, never deletes anything |
| `supervisor_watch_list` | Read the supervisor watch: the `mode` in effect (`off`, `shadow` or `on`; an unset mode reads `shadow`, which reports only and never acts), `last_poll_at`, the stored `findings` (open first, most recently seen first: `kind`, `subject`, the operator line, first and last seen, the shadow would-alert mark and the raised-alert instant) and the `comparison` against the supervisor script's record; takes no arguments, read-only |
| `supervisor_watch_stats` | Read the supervisor watch as numbers: `mode`, `last_poll_at`, `last_poll_age_seconds` (-1 when it never polled), `findings_open`, `findings_would_alert`, `findings_alerted`, `findings_resolved` and `agreed_days`; takes no arguments, read-only |
| `supervisor_loop_jobs` | Read the supervisor loop's jobs: the loop's `enabled` state and, per job, its `name`, `kind` (`mechanical` or `model`), the model job's style, backend and model, whether it is enabled, and its last outcome and run instant; takes no arguments, read-only |
| `supervisor_loop_runs` | Read the supervisor loop's run records, newest first; optional `job`, `outcome` (`ok`, `validator_failed`, `escalated` or `error`) and `limit` (1 to 500, default 50); read-only |
| `supervisor_loop_run_job` | Run one supervisor loop job now; needs `job`; optional `input` and `candidates` (comma list); an unknown job is a tool error |
| `supervisor_loop_eval` | Compare models on a model job's eval cases; needs `job` and `models` (comma list); optional `cases_dir` (the name of a folder under the default cases folder); fails naming each model when none is reachable |
| `supervisor_loop_stats` | Read the supervisor loop as numbers: `enabled`, `jobs`, `jobs_enabled`, `runs`, the split by outcome (`runs_ok`, `runs_validator_failed`, `runs_escalated`, `runs_error`), `retries` and `last_run_age_seconds` (-1 when none has run); takes no arguments, read-only |
| `security_failure_list` | List the stored security failure reports, newest first: each entry has the failing run's `repo`, `ref_kind`, `ref_name`, `workflow`, `run_id`, `run_url` and `status` and the stored `report`; optional `repo` and `status` (`green`, `failing`, `pending`, `hung`, `rerunning` or `superseded`) |
| `dependency_update_status` | Read the weekly dependency update Automation: `enabled`, `schedule`, `max_majors` as in effect, `next_run` in local time, and the last run's `last_run_at`, `last_run_automaton_title` and `last_run_outcome` (`updated`, `nothing to update` or `needs attention`); read-only, no parameters |
| `autonomous_prd_rejudge` | Re-judge the completion guardrail of a blocked Automaton whose tasks are all completed, without resetting any task — pass unblocks it, block keeps it blocked; refused otherwise |
| `autonomous_prd_set_failure_escalation` | Set or clear the failure escalation override on an Automaton or one task (`policy` object, or `clear=true`) |
| `autonomous_prd_resolve_task_review` | Retry (optionally with a changed spec, worker or file list) or cancel a task held for review |
| `autonomous_tag_sweep` | Re-derive the system tags of every existing Automata object (Automata, Automations, tasks, templates, decision records, held questions) and every Automata-owned session under the `tags.placement` settings in effect now: turning a placement axis off removes its tag everywhere the sweep re-derives, turning it on adds the tag wherever the fact exists. The operator's own tags are a separate list and are never touched, and a second run is a no-op (zero objects changed, nothing written). Optional `dry_run=true` runs the full re-derivation and reports the would-be counts without writing; absent or false the sweep writes (default). Returns the per-kind tally `{sessions, automata, automations, tasks, templates, decision_records, questions}`, each `{examined, changed, unchanged, failed}` |
| `autonomous_prd_set_task_verification_llm` / `autonomous_prd_set_story_verification_llm` / `autonomous_prd_set_verification_llm` | Pick the LLM that verifies one task / one story / a whole Automaton (`verification_backend`, `verification_model`, `verification_effort`; `""` clears, omit = unchanged). Precedence task > story > Automaton > global (v9.0.69) |
| `autonomous_prd_set_notify_events` | Set which lifecycle event kinds notify for one Automaton (`notify_events`, e.g. `["prd.blocked","task.session_started"]`); empty or omitted clears the Automaton-level set. Default set: `prd.needs_review`, `prd.blocked`, `prd.failed`, `prd.completed`, `prd.cancelled`; `task.session_started`, `task.session_cleared`, `scope_warning`, `guardrail_warn` and the supervisor kinds (`automaton.stuck_running`, `automaton.merge_ready`, `automaton.tasks_done_or_cancelled`, `task.capacity_wait_too_long`, `burn_guard.rung_changed`, `judge.verdict_*`, `task.final_gate_failed`) are opt-in. Precedence: Automaton > guardrail profile > global default; a subscription filter can only narrow |
| `guardrail_profile_create` / `guardrail_profile_update` | Optional `notify_events_json` (JSON array of event kinds) sets the profile tier of the same notify_events precedence chain; on update omitted = unchanged, `"[]"` = clear; invalid JSON → error |
| `observer_stats` / `observer_envelopes` | Per-session + per-LLM-backend stats |
| `observer_envelopes_all_peers` | Federation-aware envelope view with cross-host caller attribution |
| `memory_remember` / `memory_recall` / `kg_query` | Episodic memory + knowledge graph |
| `orchestrator_graph_run` / `orchestrator_verdicts` | PRD-DAG orchestrator |
| `automation_list` / `automation_get` / `automation_create` / `automation_update` / `automation_delete` / `automation_set_llm` / `automation_set_verification_llm` | Automation records — a chain of Automatons with per-outcome edges, entry member, optional budget policy and execution/verification defaults (data records only — no automatic advance yet). `automation_set_llm` and `automation_set_verification_llm` set the Automation's default execution, planning and verification LLMs |
| `planning_routing_get` | Read the planning routing config (enabled, shadow, rules, ladders, classes) and its validation result |
| `planning_routing_set` | Update the planning routing config; omitted fields keep their value and a bad config is rejected |
| `planning_routing_test` | Route one input (`kind`, `tags`, `spec_bytes`, `files`, `project`) and return the decision; no LLM call, nothing changes |
| `planning_scorecard` | Counts-only planning scorecard and gate report over a `window` such as `7d` |
| `pipeline_start` / `pipeline_status` | Session chaining |
| `exit_hook_list` / `exit_hook_add` / `exit_hook_delete` / `exit_hook_enable` / `exit_hook_disable` | Session exit hooks (BL356) |
| `queue_push` / `queue_claim` / `queue_complete` / `queue_fail` / `queue_list` | Distributed work queue (BL357) |
| `discussion_subscribe` / `discussion_unsubscribe` / `discussion_subscriptions` | Discussion subscriptions (BL358) |
| `usage_advice` / `usage_readings` | Account usage calculator: the one advice result (lane choice, headroom, projected cycle end, run-out warnings, finish-early estimate, plain-English advice block, and `unit_labels`, a map of LLM name to the unit its figures are read in — dollars for an `api` entry, points for an `allowance` entry, none for a `local` entry) and the shared usage readings series |
| `trace_session` | Read the recorded trace events of one session in order, with its roll-up and index entry (counts, coverage); takes `id`, optional `tool`, `backend`, `kind`, `errors`, `limit` (last N) and `loop_threshold`. Same as `GET /api/trace/session/{id}`; needs the analytics read capability |
| `trace_automaton` | Read the trace events of every session that ran for an Automaton, ordered by time, with the Automaton roll-up; takes `id` and the same filters. Same as `GET /api/trace/automaton/{id}` |
| `trace_rollup` | Roll up time, errors and tokens along one `dimension` (`session`, `task`, `story`, `automaton`, `agent` or `backend`; default `session`); optional `key` keeps one group, optional `loop_threshold`. Same as `GET /api/trace/rollup` |
| `trace_coverage` | Read how much of each session's backend activity the trace captures (`full`, `partial`, `events-only`, `unknown`) and the counts per backend; optional `backend`. Same as `GET /api/trace/coverage` |
| `trace_stats` | Read the trace store totals (sessions, events, error events) and the write counters since the daemon started (appended, failed writes, pruned); no parameters. Same as `GET /api/trace/stats`; needs the analytics read capability |
| `llm_classifier` | The classification judge: `action=status` (verification mode per LLM, capability probes, calibration, publisher allowlist), `publishers`, `add_publisher` (needs `confirm=true`; audited), `verdicts` (an Automaton's verdict records; `automaton_id`, optional `task_id`, `low_confidence`). Set `verification_mode` with `llm_add` / `llm_update`. Same data as `GET /api/llm/classifier*` |
| `burn_guard_status` / `burn_guard_run` / `burn_guard_config_get` / `burn_guard_config_set` | The burn guard: its state, rung and projection against the pace line; evaluate now; read and change the `usage.burn_guard` settings (one invalid value rejects the whole call) |
| `checks_status` / `ship_status` | Read the Automata checks (pre-start verifier, plan rules, after-done checklist, registry check: resolved mode and recorded findings) and the ship steps (push, tag, CI watch: resolved settings and recorded outcomes); optional `prd_id` and `project_dir`; read-only |
| `release_push_status` | Read how a project's finished release is published, for an Automaton: `push_and_tag` (whether the daemon pushes and tags the release on its own; default off), `release_path` (`direct_push` or `pull_request`; default `direct_push`; on `pull_request` the daemon pushes the release branch, opens a pull request into the default branch, polls its checks and merges only when every check succeeded), `ci_window_minutes` (how long the daemon waits for the release checks; default 60), and `last_release` (the newest release recorded for the project: `tag`, `commit`, `release_path`, `push_outcome`, `ci_outcome`; null when none); required `project`; read-only, settings not set read their defaults |
| `rules_context_status` / `rules_context_set` | Read or change the rules and context settings used when Automata are planned (rules file, context files, digest mode, digest size limit, per-project overrides); `rules_context_status` is read-only and takes optional `project_dir` and `prd`; `rules_context_set` changes only the fields given |
| `platform_context_status` | Read the platform context state for a session (enabled, project override, text version, delivery); read-only, takes an optional `hint` session id |
| `automation_set_effort_ceiling` / `autonomous_prd_set_task_effort_ceiling` | Cap the effort of an Automation's task kinds (`docs`, `parity`, `live_check`, `final_gate`) or of one task at `low`, `medium`, `high` or `cheaper_backend`; an empty value clears it |
| `usage_reading_add` / `usage_reading_delete` | Record (percent or spend, when, session percent, lanes) or delete one reading of the shared series |
| `usage_extra_credit_set` / `usage_remaining_work_set` | Set the bought extra credit (shared setting) and the typed remaining work (daemon-local, run-flat-out policy) |
| `usage_autolanes_status` | Read-only status of the auto-lanes loop (enabled, policy, operator maximum, current cap, target and direction, recent decisions); the `usage.calculator.auto_lanes` settings are changed through the config path |
| `result_put` / `result_get` / `result_list` / `result_delete` | Result store (BL360) |
| `post_result` / `schedule_results_list` | Scheduled-job result panel — post a session's structured result (same contract as `POST /api/sessions/{id}/result`) and list per-schedule retained history (same as `GET /api/schedule-results`) |
| `channel_info` | Channel bridge status for the current session |
| `channel_diagnostics` | Full MCP channel bridge health — all sessions, bridge kind/path, per-session liveness, hints (BL362) |
| `get_my_scope` | Self-scope confirmation: own identity, per-session MCP server name, mode (host vs container), and every session's name/state (BL463) |
| `vision_describe` | Describe an image file using the configured vision model (requires `vision.enabled: true`) |
| `web_search_stats` | Web search configuration and stats — provider, URL, engine, query/error counters (requires `web_search.enabled: true`) |
| `plugin_service_status` | Read one plugin's service status (lifecycle mode, state, address, version, uptime, last health check) — the `service` object of `GET /api/plugins/{name}`; takes `name`; read-only |
| `plugin_widget_list` | List a plugin's declared widgets with each widget's latest value (null until one arrived) — same body as `GET /api/plugins/{name}/widgets`; takes `name`; read-only |
| `plugin_widget_get` | Fetch one widget as declared plus its latest value — same body as `GET /api/plugins/{name}/widgets/{widget_id}`; takes `name` and `widget_id`; read-only |
| `plugin_token_list` | List a plugin's token usage: the secret names it references (with the manifest fields and whether each exists) and the service token names with creation times — same body as `GET /api/plugins/{name}/tokens`; takes `name`; read-only; never returns a secret or token value; operator-token-only (a federation peer gets the REST 403) |
| `plugin_service_start` | Start the plugin's managed service (datawatch launches and supervises the lifecycle entry) — `POST /api/plugins/{name}/enable` with `{"managed": true}`; takes `name`; write; attach-only plugins are refused with a clear error |
| `plugin_service_stop` | Stop the plugin's managed service and leave the plugin enabled — `POST /api/plugins/{name}/enable` with `{"managed": false}`; takes `name`; write; attach-only plugins are refused with a clear error |
| `plugin_service_restart` | Restart the plugin's managed service: the stop call and then the start call, in order; takes `name`; write; attach-only plugins are refused with a clear error and the start is never attempted |
| `plugin_token_rotate` | Rotate one secret the plugin's manifest references: stores the new value and, when the name is a service token, revokes + re-mints it; takes `name`, `secret`, `new_value`; write; the new value is sent to the server and never returned (response carries only status, plugin and secret names); a secret the plugin doesn't reference is refused |

### Automata events (the Events tab) — no MCP tools

The one newest-first stream of what the daemon watches and does for you (the kinds `watch`, `decision`, `retry`, `check`, `cleanup`, `alert` and `error`) has no MCP tools. It is read-only over REST, the CLI and the comm channel:

| Parameter | Meaning |
|---|---|
| `kind` | Optional comma list of event kinds: `watch`, `decision`, `retry`, `check`, `cleanup`, `alert`, `error`; an unknown kind is a 400 |
| `project` | Optional exact project match |
| `q` | Optional case-insensitive text over the summary, detail, project, Automaton and Automation names |
| `limit` | Page size, 1–200, default 50; anything else is a 400 |
| `before` | Optional opaque cursor — the `next_cursor` from a previous page's body; returns older events; a bad cursor is a 400 |

The body of `GET /api/autonomous/event-log` is `{"events": [...], "next_cursor": "...", "total": n}`; `next_cursor` is empty on the last page, and `total` counts every event that matches the filters. Example: `GET /api/autonomous/event-log?kind=watch,retry&limit=10` — the ten newest `watch` and `retry` rows. The CLI equivalent is `datawatch autonomous event-log --kind watch --kind retry --limit 10`, and the comm verb is `autonomous event-log kind=watch,retry limit=10` (its limit is 1–10, default 10). What a row means, what an error entry and a quiet entry are, and the next action for a common error: [howto/automata-events.md](howto/automata-events.md).

### Changing the daemon's own settings

`get_config` / `config_set` read and change the daemon's settings (session, LLM, autonomous, server, …) over MCP. Self-config is **enabled by default** (`mcp.allow_self_config` aliases the builtin **self-config** capability group, which carries `config:write` in every session's scoped credential); when it is off, config-write tools are refused and the setting is read-only via `get_config`. The alias itself is bootstrap-protected — a session cannot flip it. Every attempt (allowed and refused) is in the self-config audit log, readable with `self_config_audit`. See [howto/self-config-capability.md](howto/self-config-capability.md) for the definition, hardening steps and audit queries.

## Example Usage in Cursor

Once configured, you can ask Claude in Cursor:

```
Start a session to write unit tests for my auth package at /home/<user>/myproject
```

Claude will call `start_session` with your task, then use `session_output` to
follow the progress — all without leaving Cursor.

```
What sessions are running?
```
→ calls `list_sessions`

```
Session a3f2 is waiting — tell it yes, proceed with the changes
```
→ calls `send_input(session_id="a3f2", text="yes, proceed with the changes")`

## Remote AI Agent Example

A remote AI agent (e.g. running in a cloud function) can:

1. Connect to `https://your-server:8081/sse` with bearer token
2. Call `start_session` to kick off a coding task
3. Poll `session_output` to monitor progress
4. Call `send_input` when the session asks for confirmation
5. Call `list_sessions` to check all active sessions

This enables fully autonomous AI workflows that delegate coding tasks to
datawatch-managed AI sessions on your development machines.

- `worker_profile_status` — read-only: built-in worker profile names, the defaults per role and task kind, the latest start-up averages per profile and role, and `in_force`, the profile each role and task kind starts with now. Profiles are set with `config_set` on `autonomous.worker_profile*`; an Automaton's profile with `autonomous_prd_set_worker_profile`.
- `secret_scan_config_get` / `secret_scan_config_set` — read and change the push-time secret scan (`git.secret_scan`); the setter validates the mode, detector names and fingerprints.
- `secret_scan_stats` — read the daemon's pre-push secret-scan counters (`runs`, `findings`, `shadow_reports`, `refusals`, `last_run_at`, `last_refusal_at`) plus the effective `git.secret_scan` mode; proxies `GET /api/git/secret-scan/stats`. Counters are in-memory since daemon start and reset on restart.
