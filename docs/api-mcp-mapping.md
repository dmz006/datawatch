# API ↔ MCP Tool Mapping

This document maps every REST API endpoint to its MCP tool equivalent,
documenting coverage gaps and the reasoning behind them.

## Full Mapping

### Session Management

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/sessions` | `list_sessions` | Complete | `name`/`state`/`backend`/`alive` filters match the REST params. `view` (`full` default, or `summary`), `states` and `since` map to the REST `view`/`states`/`since` params; using any of them returns the REST JSON array, plus a text note with the `X-Datawatch-Sessions-Omitted` count when the summary cap dropped sessions. Not combinable with `orphaned`. |
| `POST /api/sessions/start` | `start_session` | Complete | `git` boolean param (opt-in git init/snapshot/auto-commit; default false) is forwarded to the REST `git` body field. |
| `GET /api/output?id=&n=` | `session_output` | Complete | |
| `GET /api/sessions/timeline` | `session_timeline` | Complete | |
| `POST /api/sessions/kill` | `kill_session` | Complete | |
| `POST /api/sessions/rename` | `rename_session` | Complete | |
| `POST /api/sessions/restart` | `restart_session` | Complete | v2.2.1 |
| `POST /api/sessions/delete` | `delete_session` | Complete | v2.2.1 |
| `POST /api/sessions/state` | — | Not in MCP | Internal use: manual state override. Not useful for MCP clients |
| `POST /api/sessions/tags` | `session_set_tags` | Complete | body `{id, tag_ids[]}`, MCP arg `tag_ids_json`; replaces the session's own tags, an empty list clears them; inherited tags come from the Automaton and are not set here; needs the sessions write capability |
| `POST /api/command` | `send_input` | Partial | API accepts raw command strings; MCP has typed `send_input` for session input. Use `send_saved_command` for named commands |
| `POST /api/sessions/bind` | `session_bind_agent` | Complete | Bind a session to an agent worker |
| `POST /api/sessions/import` | `session_import` | Complete | Import an orphan session from disk |
| `GET /api/sessions/reconcile` | `session_reconcile` | Complete | List orphan session dirs |
| `POST /api/sessions/{id}/rollback` | `session_rollback` | Complete | Roll back to pre-session checkpoint |
| `GET /api/sessions/stale` | `sessions_stale` | Complete | Running sessions stuck longer than threshold |
| `GET /api/sessions/aggregated` | — | Not in MCP | Multi-server aggregation — proxy/federation feature |

### Memory

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/memory/stats` | `memory_stats` | Complete | |
| `GET /api/memory/list` | `memory_list` | Complete | |
| `GET /api/memory/search?q=` | `memory_recall` | Complete | |
| `POST /api/memory/delete` | `memory_forget` | Complete | |
| `GET /api/memory/export` | `memory_export` | Complete | v2.2.1 |
| `POST /api/memory/import` | — | Not in MCP | Import is a file upload — MCP tools return text, not receive file uploads. Use API directly |
| `GET /api/memory/test` | — | Not in MCP | Pre-enable test — used by web UI toggle. MCP clients don't enable/disable features |
| `GET /api/memory/wal` | — | Not in MCP | Audit log — operational debugging tool. Low value for MCP clients |

### Knowledge Graph

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/memory/kg/query` | `kg_query` | Complete | |
| `POST /api/memory/kg/add` | `kg_add` | Complete | |
| `POST /api/memory/kg/invalidate` | `kg_invalidate` | Complete | |
| `GET /api/memory/kg/timeline` | `kg_timeline` | Complete | |
| `GET /api/memory/kg/stats` | `kg_stats` | Complete | |

### Intelligence

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| — (comm channel) | `research_sessions` | MCP only | Cross-session research. No dedicated API endpoint (uses memory search + KG + session scan) |
| `GET /api/sessions/response` | `copy_response` | Complete | |
| `GET /api/sessions/prompt` | `get_prompt` | Complete | |

### Monitoring

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/stats` | `get_stats` | Complete | v2.2.1 |
| `GET /api/ollama/stats` | `ollama_stats` | Complete | |
| `GET /healthz` | — | Not in MCP | Kubernetes probe — not useful for AI clients |
| `GET /readyz` | — | Not in MCP | Kubernetes probe |
| `GET /metrics` | — | Not in MCP | Prometheus scrape endpoint — not for MCP |

### Configuration

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/config` | `get_config` | Complete | v2.2.1 |
| `PUT /api/config` | — | Not in MCP | Config changes should be deliberate human actions, not AI-driven. Security boundary: AI agents shouldn't modify daemon config |
| `GET /api/backends` | — | Not in MCP | Backend list is static — MCP clients can read from `get_config` section |
| `GET /api/info` | `get_version` | Complete | Version + hostname |

### Scheduling

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/schedules` | `schedule_list` | Complete | |
| `POST /api/schedules` | `schedule_add` | Complete | |
| `DELETE /api/schedules` | `schedule_cancel` | Complete | |

### Commands & Filters

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/commands` | `list_saved_commands` | Complete | |
| — | `send_saved_command` | MCP only | Sends named command to session |
| `GET /api/filters` | — | Not in MCP | Output filters are operational config — not useful for AI clients |
| `POST /api/filters` | — | Not in MCP | Same reasoning |

### Alerts

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/alerts` | `get_alerts` | Complete | `limit`/`unread`/`before` paging maps to the tool's `limit`/`unread_only`/`before` |
| `GET /api/alerts/count` | — | Not in MCP | Badge counts `{unread_count, total}` for polling clients; MCP clients use `get_alerts` |
| `POST /api/alerts` (mark read) | `mark_alert_read` | Complete | |
| `POST /api/alerts/create` | `create_alert` | Complete | GH#201 Phase 5 — raises a new system alert (`CapAlertsWrite`, not granted to any built-in group by default) |

### Autonomous PRD Planning

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/autonomous/status` | `autonomous_status` | Complete | |
| `GET /api/autonomous/config` | `autonomous_config_get` | Complete | includes `per_prd_guardrails` (completion guardrails: `rules-review`, `ci-watch`; empty = off) and `ci_watch_timeout_seconds` (default 1500) — [howto](howto/ci-watch-guardrail.md); `block_on_file_drift` (default `false`, warn-only) — [howto](howto/file-drift-guardrail.md) |
| `PUT /api/autonomous/config` | `autonomous_config_set` | Complete | accepts `per_prd_guardrails` (e.g. `["rules-review","ci-watch"]`) and `ci_watch_timeout_seconds` — [howto](howto/ci-watch-guardrail.md); accepts `block_on_file_drift` (bool; `true` blocks a task on foreign file drift) — [howto](howto/file-drift-guardrail.md); accepts `max_concurrent_tasks` (int, default 1 = sequential; daemon-wide default an Automaton's own setting overrides); `max_parallel_tasks` is deprecated — accepted, ignored, and reported under `deprecated_fields` in the response; accepts `judge_policy` (ordered backend list the judges try, one attempt each; empty = follow the verifier routing policy) and `judge_timeout_seconds` (time limit for one judge run, default 1800; 0 = default); accepts `judge_size_threshold_lines` (whole number of changed lines above which the judges try `claude-code` first, default 400; 0 = size rule off) |
| `autonomous.gate_disagreement` (config key: `GET`/`PUT /api/config`; also accepted by `PUT /api/autonomous/config`; comm `configure autonomous.gate_disagreement=block`; CLI `datawatch autonomous config-set '{"gate_disagreement":"block"}'`) | `autonomous_config_set` / `autonomous_config_get` | Complete | quality-gate disagreement policy; accepts only `"warn"` (default) or `"block"` (anything else is a 400). When a task's verifier passed but the gate confirmed a regression from a valid baseline, `warn` records `gate_disagreement` on the task and in the decision log and the task completes; `block` fails the task with a `gate_disagreement: …` error. An invalid gate never blocks. `GET /api/autonomous/prds/{id}` also returns `quality_gate_status` (`"invalid"` when the baseline is unusable) and `quality_gate_invalid_reason` on the Automaton, and `gate_disagreement {verifier_ok, gate_regression, gate_invalid, policy, summary}` on a task — [operations](operations.md#quality-gate-test-baseline--regression-check) |
| `GET /api/autonomous/prds` | `autonomous_prd_list` | Complete | |
| `POST /api/autonomous/prds` | `autonomous_prd_create` | Complete | optional `kind` (`feature`, `bug` or `other`); `?view=compact` returns the write view (`id`, `status`, `rev`, `message`) instead of the whole Automaton; absent or `view=full` = full. MCP sends `view=compact` |
| `GET /api/autonomous/prds/{id}` | `autonomous_prd_get` | Complete | `ci_watch {status, sha, repo, runs[], summary, category, issues, checked_at}` when the `ci-watch` completion guardrail ran; status `skipped`/`pass`/`warn`/`pending`/`failed_policy`/`failed_content`/`failed_infra`/`error` — [howto](howto/ci-watch-guardrail.md); per-task `foreign_file_drift[]` and `file_drift_detected` decisions when a direct-mode task committed someone else's pre-existing uncommitted files — [howto](howto/file-drift-guardrail.md). `?detail=summary|tasks|full` picks how much comes back: absent = `full` on REST, and `foreign_file_drift` is only in `full`. The REST default is unchanged; the MCP tool `autonomous_prd_get` sends `detail=summary` by default |
| `PUT /api/autonomous/prds/{id}/guardrails` | `per_automaton_guardrails_set` | Complete | body `{guardrail_profile, per_task_guardrails[], per_story_guardrails[], per_prd_guardrails[]}` — replaces all four; `per_prd_guardrails` takes `rules-review` / `ci-watch`; optional `block_on_file_drift` (bool; omitted = inherit from profile/global); `?view=compact` returns the write view (absent or `view=full` = full; MCP sends `view=compact`) |
| `POST /api/autonomous/guardrail_profiles` | `guardrail_profile_create` | Complete | body `{name, description, guardrails[], block_on_file_drift?, notify_events?}`; `block_on_file_drift` omitted = inherit `autonomous.block_on_file_drift` — [howto](howto/file-drift-guardrail.md); `notify_events` is a JSON array of event kinds (MCP arg `notify_events_json`); omitted = default set (`prd.needs_review`, `prd.blocked`, `prd.failed`, `prd.completed`, `prd.cancelled`) — `task.session_started` / `task.session_cleared` and the warning kinds `scope_warning` / `guardrail_warn` are opt-in (warnings stay visible in the PWA but never block); precedence Automaton > guardrail profile > global |
| `PUT /api/autonomous/guardrail_profiles/{id}` | `guardrail_profile_update` | Complete | optional `block_on_file_drift` (bool; omitted = unchanged) — [howto](howto/file-drift-guardrail.md); optional `notify_events` (array of event kinds; MCP arg `notify_events_json`; omitted = unchanged, `[]` = clear, default set as on create — the opt-in warning kinds `scope_warning` / `guardrail_warn` are valid entries; `[]`/null/missing all mean "no profile-tier kinds") |
| `DELETE /api/autonomous/prds/{id}` | `autonomous_prd_cancel` | Complete | response names killed sessions: `killed_sessions` (count) + `killed_session_ids` (array); `?view=compact` returns the write view with `killed_sessions` (count) and no `killed_session_ids` (absent or `view=full` = full; MCP sends `view=compact`) |
| `POST /api/autonomous/prds/{id}/decompose` | `autonomous_prd_decompose` | Complete | `?view=compact` (absent or `view=full` = full; MCP sends `view=compact`) |
| `POST /api/autonomous/prds/{id}/set_priority` | `autonomous_prd_set_priority` | Complete | capacity-queue priority; higher runs first; `?view=compact` (absent or `view=full` = full; MCP sends `view=compact`) |
| `POST /api/autonomous/prds/{id}/set_kind` | `autonomous_prd_set_kind` | Complete | Automaton kind `feature`, `bug` or `other` (empty derives it again); 400 on an unknown kind, 404 on an unknown id; `?view=compact` (absent or `view=full` = full; MCP sends `view=compact`) |
| `POST /api/autonomous/prds/{id}/set_notify_events` | `autonomous_prd_set_notify_events` | Complete | body `{notify_events[]}` (MCP arg `notify_events`, array of event kinds); replaces the Automaton-level lifecycle event set — empty or omitted clears it to inherit from the next tier. Default set: `prd.needs_review`, `prd.blocked`, `prd.failed`, `prd.completed`, `prd.cancelled`; `task.session_started` and `task.session_cleared` are opt-in. Precedence: Automaton > guardrail profile > global default; a subscription filter can only narrow |
| `GET /api/autonomous/tags` | `autonomous_tag_list` | Complete | tags as a tree via `parent_id`, each with its Automata `count`; `include_sub` rolls counts up — [howto](howto/automata-tags.md); `counts=true` (MCP arg `counts`) adds subtree and direct counts and the untagged totals |
| `POST /api/autonomous/tags` | `autonomous_tag_create` | Complete | body `{name, parent_id?, color?, description?}`; on a peer it goes through the shared tag registry, and while the keeper is unreachable the tag is created and marked `pending_sync` |
| `PATCH /api/autonomous/tags/{id}` | `autonomous_tag_rename` / `autonomous_tag_set_parent` / `autonomous_tag_set_color` | Complete | one MCP tool per field; a move that would make a tag its own ancestor is refused (409); on a peer it goes through the shared tag registry, a keeper conflict comes back as `conflict` and `conflict_field`, and an unreachable keeper gives 503 |
| `DELETE /api/autonomous/tags/{id}` | `autonomous_tag_delete` | Complete | refused (409) while the tag has child tags or is assigned to Automata; the assigned refusal reports the count; on a peer the delete goes through the shared tag registry and is refused with 503 while the keeper is unreachable |
| `GET /api/autonomous/tags` (`registry` object) | `autonomous_tag_registry_status` | Complete | returns only the `registry` object of the tag reply: role, keeper, stale, last sync time, revision, pending sync count, notes and last error; read-only. Feature-access table: [api/autonomous.md](api/autonomous.md#shared-tag-registry) |
| `POST /api/autonomous/tags/registry/notes/{id}/dismiss` | `autonomous_tag_registry_dismiss_note` | Complete | MCP arg `id` (a note ID from `autonomous_tag_registry_status`); dismisses the note on this daemon only; 404 for an unknown note |
| `GET /api/autonomous/tags/registry` | — | Not in MCP | Keeper-to-peer protocol route that serves the keeper's snapshot to peers. Not an operator action; the status tool shows this daemon's registry object instead |
| `POST /api/autonomous/tags/registry/write` | — | Not in MCP | Keeper-to-peer protocol route that applies one forwarded tag write on the keeper. Not an operator action; peers call it, operators use the tag routes on the peer |
| `POST /api/autonomous/prds/{id}/set_tags` | `autonomous_prd_set_tags` | Complete | body `{tag_ids[], primary_tag_id?}`, MCP args `tag_ids_json` and `primary_tag_id`; replaces the list (empty clears), and `primary_tag_id` alone keeps the tags |
| `GET /api/autonomous/attention` | `autonomous_attention_list` | Complete | the attention group `{statuses, items}`: Automata in the attention statuses (blocked, needs_review, paused) plus those with a task held for review; read-only |
| `POST /api/autonomous/prds/{id}/set_llm` | `autonomous_prd_set_llm` | Complete | Set the PRD's worker LLM fields; OMITTED fields stay unchanged, an empty value clears that field (falls back to global) |
| `GET /api/autonomous/lane-fill` | `autonomous_lane_fill_status` | Complete | the lane fill status: `mode`, `last_poll_at`, `lanes[]`, `queued_eligible`, `evidence`, `decisions[]` (newest first) and `alerts[]`; query `limit` (1 to 1000, default 50; 400 outside that range); needs the autonomous read capability; CLI `datawatch autonomous lane-fill [--limit N] [--json]`, comm `autonomous lane-fill [limit]` |
| `GET /api/autonomous/triage` | `autonomous_triage` | Complete | the triage records: `records[]` newest first + `alert_minutes`; query `automation_id`, `prd_id`, `status` (`failed`, `blocked`; case-sensitive, 400 otherwise); 403 without the autonomous read capability, 503 with the manager down; an empty match is `records: []`, not an error. CLI `datawatch autonomous triage [--automation <id>] [--prd <id>] [--status failed\|blocked] [--json]`, comm `autonomous triage [automation-id]` (one optional positional argument, the automation id) |
| `POST /api/autonomous/prds/{id}/set_lane_preference` | `autonomous_prd_set_lane_preference` | Complete | body `{lane_preference}`: `gpu`, `claude` or `any` (empty = any); 400 on an unknown value, 404 on an unknown id; CLI `datawatch autonomous prd-set-lane-preference <id> <gpu\|claude\|any>`, comm `autonomous set-lane-preference <prd-id> <gpu\|claude\|any>` |
| `GET /api/autonomous/progress` | `autonomous_progress` | Complete | the progress metrics object; query `automation_id`, `tag_id`, `since`, `until` (dates, `YYYY-MM-DD`; `until` includes the whole day); a missing backlog file gives `backlog_available: false`; 400 on a bad date or `until` before `since`; needs the autonomous read capability |
| `GET /api/autonomous/progress/trend` | `autonomous_progress_trend` | Complete | `{days, points[], direction}` from the saved daily snapshots; query `days` (1 to 365, default 30; 400 outside that range); empty `points` and `direction: unknown` without snapshots; needs the autonomous read capability |
| `GET /api/autonomous/overlap` | `autonomous_overlap_search` | Complete | the overlap warnings `{warnings[], threshold, mode}`, best match first; query `title`, `spec` (at least one) and `limit` (1 to 50, default 10); 400 when both are blank or `limit` is outside 1 to 50; 403 without the autonomous read capability; 500 when the search fails, never an empty list; read-only, in every mode. CLI `datawatch autonomous overlap [spec] [--title T] [--spec S] [--json]`, comm `autonomous overlap <text>` |
| `GET /api/capacity` | `capacity_status` | Complete | admission ledger: pools, leases, wait queue; each `llm:*` pool carries `effective_max_inflight` (cap admitted against) + `inflight_source` (`explicit` | `plan_tier` | `kind`); response also carries `patterns_version`, `patterns_stale` and usage-backoff `claude_cli_version`; it also carries a `budgets` array (see Per-LLM dollar budget); a `tasks` object lists `waiting` (task, `cause`, plain `reason`, `since`, optional `resets_at`, `holder`) and `running` (task, `pools`, `node`, `session_id`, `note`) tasks, both always arrays, narrowed by `?prd_id=`; each `backoff` entry carries `reset_known` (false when no reset time was named and `until` is the maximum back-off). CLI `datawatch capacity` and comm `capacity` print the queued reasons and holdings; each `node:*` pool may carry a `usage` object (model probe `state` — `ok` | `unreachable` | `pending` | `no_data`, `checked_at`, optional `error`, `models_supported`, `models[]` with loaded model name/size/expiry, `gpu[]` with utilisation and memory) and those surfaces print one line per loaded model and GPU, or a state line such as `checking...` / `unreachable: <reason>` |
| `GET /api/capacity/admission` | `capacity_admission` | Complete | read-only admission query: `backend` (or `llm`), `node`, `model`, `role` (`worker`, `verifier`, `planner`, `interactive`), `priority`, `prd_id`; resolves the same host, `llm:*` and `node:*` pools a real start uses and returns `kind` (`admit` \| `queued` \| `alternate_available`), `queue_position`, `reason`, `resets_at`, `alternates`; takes no lease and changes no state; same auth as `GET /api/capacity`. CLI `datawatch capacity admission --backend X [--node N --model M --role R --priority P] [--json]`; comm `capacity admission <backend> [node] [model]` |
| `PUT /api/compute/nodes/{name}` (`max_concurrent_sessions`) | `compute_node_update` / `compute_node_add` | Complete | per-node cap on autonomous sessions |
| `PUT /api/compute/nodes/{name}` (`session_slots`), `GET /api/compute/nodes/{name}/detail` (`derived_slots`) | `compute_node_update` / `compute_node_add`; `compute_node_detail` | Complete | per-node worker count (0 = derive from observer GPU data); the detail carries the count in effect |
| `PUT /api/llms/{name}` (`max_inflight`, `plan_tier`) | `llm_update` / `llm_add` | Complete | per-LLM cap on autonomous sessions; `plan_tier` (`pro`/`max_5x`/`max_20x`/`api`) admits against that tier's `plan_tier_max_inflight` value unless an explicit `max_inflight > 0` overrides it; `GET /api/llms` responses carry both `effective_max_inflight` and `inflight_source`; also carries the optional `budget` object (see Per-LLM dollar budget) |
| `POST /api/autonomous/prds/{id}/run` | `autonomous_prd_run` | Complete | `?view=compact` (absent or `view=full` = full; MCP sends `view=compact`) |
| `GET /api/autonomous/learnings` | `autonomous_learnings` | Complete | |
| `GET /api/autonomous/subscriptions` | `autonomous_subscriptions` | Complete | list the operator's persisted lifecycle event subscriptions (optional `?owner=` filter) |
| `POST /api/autonomous/subscriptions` | `autonomous_subscribe` | Complete | body `{filter:{kinds[], level?, prd_id?, task_id?, story_id?, owner?}, sink:{type, target}, action:{type, text}?, coalesce?, one_shot?, expires_at?}`; 400 with the validation message on bad input; sinks `comm`/`session`/`topic`/`alert`/`mcp_queue`/`webhook`/`peer` (the queue name is the sink target; `webhook` takes the https URL as target and `sink.secret_ref` = one `${secret:name}` reference, never a literal; `peer` takes the registered federation peer name as target). `webhook` and `peer` are refused with a 400 while their delivery is disabled, while the URL is off the allow-list, or while the peer is not a federated peer with `autonomous:write`; the answer never echoes the secret. Listings return `secret_ref` as the reference text only. MCP `expires_in_seconds` maps to `expires_at`; MCP `secret_ref` maps to `sink.secret_ref` |
| `DELETE /api/autonomous/subscriptions/{id}` | `autonomous_unsubscribe` | Complete | 404 for unknown ids (also accepted as `DELETE ?id={id}`); one-shot subscriptions are also auto-deleted after first delivery; changes take effect live (engine re-attaches), no restart needed |
| `PUT /api/autonomous/subscriptions/{id}` | `autonomous_subscription_update` | Complete | full replace of the mutable fields (filter, sink, action, coalesce, one_shot, expires_at; owner kept unless supplied); id and created_at are kept; 200 + the record, 400 validation message, 404 unknown id; auth `autonomous:write`; live re-attach, no restart |
| `POST /api/autonomous/subscriptions/{id}/test` | `autonomous_subscription_test` | Complete | delivers a synthetic `[test] ` prd.updated event to just that subscription's sink (never the action text, never consuming a one-shot); for `webhook` and `peer` it goes through the same sender as a real event; 200 `{ok, sink, error, status?, attempts?}` (`status` is the last HTTP status, `attempts` the POST count, both set for `webhook` and `peer` only), 404 unknown id; auth `autonomous:write` |
| `GET /api/autonomous/events` | `autonomous_events_pending` | Complete | `?queue=` (required), `?since=` cursor (default 0), `?limit=` (default 50, capped at 200); returns `{events, next_cursor, dropped}` with `events` always an array; 400 for a missing queue or non-numeric since/limit, 503 when no queue is wired; reads the queue behind an `mcp_queue` subscription sink. CLI `datawatch autonomous events`; comm `autonomous events` |
| `POST /api/autonomous/prds/{id}/set_failure_escalation` (body `{task_id, policy\|null, actor}`) | `autonomous_prd_set_failure_escalation` | Complete | `policy` is the object `{enabled, max_failures, steps}`; `clear=true` sends a null policy. Task override when `task_id` is set — [howto](howto/failure-escalation.md) |
| `POST /api/autonomous/prds/{id}/resolve_task_review` (body `{task_id, action, spec, backend, model, effort, files, note, actor}`) | `autonomous_prd_resolve_task_review` | Complete | `action` is `retry` or `cancel`; a 409 when the task is not held for review — [howto](howto/failure-escalation.md) |
| `GET /api/config` and `PUT /api/config` keys `autonomous.failure_escalation.enabled`, `.max_failures`, `.steps` | `config_set` / `get_config` | Complete | `steps` takes a JSON array or the text `backend:model[@attempts],...` — [howto](howto/failure-escalation.md) |

### Operator questions

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `POST /api/questions` | — | Deferred | Raise an operator question; needs the autonomous write capability. Body caps 64 KiB; the required shape is `text`, 2 to 20 options with unique ids, a `default` (an option or `hold`) and a `context` (`none`, `prd`, `task`, `proposal`, `registry_row`) with its matching link. Step 1 fields: `priority` (`normal` default / `high` — gates the alert announce, nothing else) and `return_path` (bool, opt-in `DATAWATCH_ANSWER:` line; needs `session_id`). MCP, CLI and comm rows are deferred pending the question channel's MCP and surface coverage |
| `GET /api/questions` | — | Deferred | List, newest first: `?state=`, `?context=`, `?asker=` (exact on `asked_by`), `?text=` (case-insensitive substring over question text, option labels/tradeoffs, answer quotes), `?limit=` (default 100, capped 1000). Needs the autonomous read capability |
| `GET /api/questions/{id}` | — | Deferred | One question (including `priority` and `return_path` on the record) + `answer_count` + `current_answer_id` + the standing answer's summary (`answer`, or `null` while it has no answer) in the same read — the polled return path for an asker |
| `POST /api/questions/{id}/answer` | — | Deferred | Record an answer: `chosen_option_id` and/or a non-blank `quote`; `supersedes` for a changed mind (the old answer is immutable, kept in the log); `surface` (`pwa`, `android`, `ios`, `cli`, `rest`, `mcp` or `comm:<channel>`), `source_message_id`, `actor`. A secret-looking answer is 422-refused — the body carries the fixed phrase `matched: secret pattern`, the two safe routes, and `reason: secret_detected`, but no part of the submitted text and no rule name (the matched rule's name lands on the `question_answer_refused` audit entry) |
| `POST /api/questions/{id}/withdraw` | — | Deferred | Withdraw an open question; 409 when it has already moved on |
| `GET /api/questions/{id}/answers` | — | Deferred | The full answer log, oldest first, each answer with its derived superseded flags, plus `current_answer_id` |

All rows are gated on `autonomous.questions.enabled` (off by default; `503` naming the switch while off). The rows are deferred because the MCP tools do not exist yet — the REST endpoints are implemented and live, and the switch itself is already reachable from MCP and comm (`config_set` / `configure autonomous.questions.enabled`). The three notification toggles (`notify_messaging`, `notify_alert`, `return_path`) are likewise reachable from every surface through the generic config keys (`config_set` / `datawatch config set` / `configure` / PWA Settings → General). The lifecycle subscription machinery already covers the new event kinds: `autonomous_subscribe` (MCP), `POST /api/autonomous/subscriptions` (REST), `datawatch autonomous subscribe --on question.answered,question.withdrawn` (CLI) and `autonomous subscribe --on …` (comm) accept both in `filter.kinds` — exactly-once, restart-durable delivery over every sink type. See [api/autonomous](api/autonomous.md#operator-question-channel) and [operations](operations.md#operator-questions).

### Automation records

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/automations` | `automation_list` | Complete | Data records only — no automatic advance yet; each record carries the computed `budget` view (see the Automation spending limit rows below) |
| `POST /api/automations` | `automation_create` | Complete | body `{name, description, entry_member_id, members[], budget_policy, project_dir, backend, model, effort, verification_*}` |
| `GET /api/automations/{id}` | `automation_get` | Complete | the record plus a computed `budget` view (absent when the Automation has no budget policy in play): `scope`, `ceiling_type`, `ceiling_usd`, `spent_usd`, `remaining_usd`, `window`, `window_start`, `window_end`, `pacing`, `pacing_allowed_usd`, `on_breach`, `ceiling_reached`, `hold` / `hold_reason`, `reset_at` / `reset_known` / `reset_source`, `state` (the current breach), `allocation_estimate` (always estimated), `peers[]` with `peer_spend_usd`, `peer_basis` and `peer_note`. Computed on every read, never stored |
| `PUT /api/automations/{id}` | `automation_update` | Complete | optional `expected_version` — optimistic concurrency, `409` if the record moved since |
| `DELETE /api/automations/{id}` | `automation_delete` | Complete | member Automatons are unaffected — the back-reference is cleared |
| `POST /api/automations/import-graph/{graph_id}` | `automation_import_graph` | Complete | CLI `datawatch automation import-graph <graph-id> [--dry-run]`; comm `automation import-graph <graph-id> [dry-run]`. Body `{dry_run}`; `404` unknown graph, `409` member already owned, `422` not representable |
| `POST /api/automations/{id}/set_budget` | `automation_set_budget` | Complete | Automation spending limit. Body `{member_id?, ceiling_type, ceiling_value, window, pacing?, on_breach?, backend?, allocation_estimate?, peer_window?, reset_time?, reset_weekday?, reset_day?, reset_zone?, estimated_cost_usd?, clear?, expected_version, actor, reason}`. The policy fields replace the whole policy of the Automation, or of one member when `member_id` is given; `clear` removes it and cannot be combined with them; `estimated_cost_usd` (member only) declares the member's expected cost. Does not change the Automation's status. `400` invalid policy, `404` unknown Automation or member, `409` stale `expected_version`, a finished member or a finished Automation. Audit action `automation_set_budget`. CLI `datawatch automation set-budget <id>`; comm `automation set-budget <id> key=value...` |
| `POST /api/automations/{id}/set_decision_assistant` | `automation_set_decision_assistant` | Complete | Optional local-model decision assistant (default off). Body `{member_id?, backend, model, mode?, hooks?, thresholds?, clear?, expected_version, actor, reason}`. The fields replace the whole setting of the Automation, or of one member when `member_id` is given; `clear` or an empty `backend` turns it off. `mode` is `shadow` (default) or `live`; `hooks` are `empty_edge`, `skip_member`, `insert_member`, `budget_breach`; `thresholds` map a hook to a number in (0,1]. `400` invalid setting, `404` unknown Automation or member, `409` stale `expected_version`. Audit action `automation_set_decision_assistant`. CLI `datawatch automation set-decision-assistant <id>`; comm `automation set-decision-assistant <id> key=value...` |
| `GET /api/autonomous/status` (`automations[].budget`) | `autonomous_status` | Complete | each Automation row carries the same `budget` view plus `spent_usd` and `ceiling_display` (for example `$12.40 of $40.00 (week)`); a block caused by the limit also shows as the row's blocked reason, and a pause or block is recorded in `budget.state` (`action`, `reason`, `resume_at`). CLI `datawatch autonomous status`; comm `automation status <id>` prints the budget lines |

### Observer peers

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/observer/peers` | `observer_peers_list` | Complete | Federated peers (any of the three shapes) |
| `POST /api/observer/peers` | `observer_peer_register` | Complete | Mints token (only opportunity) |
| `GET /api/observer/peers/{name}` | `observer_peer_get` | Complete | TokenHash redacted |
| `GET /api/observer/peers/{name}/stats` | `observer_peer_stats` | Complete | Last-known StatsResponse v2 |
| `DELETE /api/observer/peers/{name}` | `observer_peer_delete` | Complete | Auto re-register on next push |
| `GET /api/observer/peers/{agent_id}` | `observer_agent_stats` | Complete | Agent-flavoured alias — agent_id is the peer name |
| `GET /api/observer/peers` (filtered shape=A) | `observer_agent_list` | Complete | Agent-flavoured alias — peers running in agent shape |

### Observer — unified stats + sub-process monitoring

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/observer/stats` | `observer_stats` | Complete | StatsResponse v2 snapshot |
| `GET /api/observer/envelopes` | `observer_envelopes` | Complete | per-session + per-backend rollup |
| `GET /api/observer/envelope?id=` | `observer_envelope` | Complete | drill-down to one envelope's tree |
| `GET /api/observer/config` | `observer_config_get` | Complete | |
| `PUT /api/observer/config` | `observer_config_set` | Complete | |
| `GET /api/stats?v=2` | `observer_stats` | Alias | Negotiates v2 on the existing stats path |

### PRD-DAG Orchestrator

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/orchestrator/config` | `orchestrator_config_get` | Complete | |
| `PUT /api/orchestrator/config` | `orchestrator_config_set` | Complete | |
| `GET /api/orchestrator/graphs` | `orchestrator_graph_list` | Complete | |
| `POST /api/orchestrator/graphs` | `orchestrator_graph_create` | Complete — **Deprecated** | Use `automation import-graph` |
| `GET /api/orchestrator/graphs/{id}` | `orchestrator_graph_get` | Complete | |
| `DELETE /api/orchestrator/graphs/{id}` | `orchestrator_graph_cancel` | Complete | |
| `POST /api/orchestrator/graphs/{id}/plan` | `orchestrator_graph_plan` | Complete — **Deprecated** | Use `automation import-graph` |
| `POST /api/orchestrator/graphs/{id}/run` | `orchestrator_graph_run` | Complete — **Deprecated** | Fire-and-forget. Use `automation import-graph` |
| `GET /api/orchestrator/verdicts` | `orchestrator_verdicts` | Complete | |

### Cost tracking

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/cost` | `cost_summary` | Complete | Token + USD rollup |
| `GET /api/cost/usage` | `cost_usage` | Complete | Per-session detail |
| `GET /api/cost/rates` | `cost_rates` | Complete | Per-model price-per-token |

### Cooldown

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/cooldown` | `cooldown_status` | Complete | Global rate-limit pause state |
| `PUT /api/cooldown` | `cooldown_set` | Complete | Set cooldown (G/P/D scopes) |
| `DELETE /api/cooldown` | `cooldown_clear` | Complete | Clear cooldown |

### Usage back-off

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/usage-backoff` | `usage_backoff_status` | Complete | Active per-backend flags, effective config, pattern staleness (`patterns_version`, `patterns_stale`) and `claude_cli_version` |
| `POST /api/usage-backoff` | `usage_backoff_set` | Complete | Set a flag (`backend` + `minutes` or `until`, optional `reason`) |
| `DELETE /api/usage-backoff` | `usage_backoff_clear` | Complete | Clear one backend, or all |
| `POST /api/usage-backoff/recheck` | `usage_backoff_recheck` | Complete | Record rate-limit patterns and the current `claude_cli_version` as rechecked (clears `patterns_stale`) |

### Usage calculator

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/usage/advice` | `usage_advice` | Complete | One advice result (latest reading, ceiling with extra credit, lane choice, headroom after the safety margin, one burn figure, projected cycle end, run-out warnings, finish-early estimate, plain-English advice block). Optional query parameters `variant=`, `policy=`, `lanes=`, `cost=` override the stored settings for this one call only; variant and policy are independent |
| `GET /api/usage/readings` | `usage_readings` | Complete | The shared readings series (percent and/or spend, when, session percent, lanes, source), the bought extra credit, and the per-policy remaining-work figure; shared across federated daemons through a keeper, stale reads carry `stale`/`stale_reason`/`copied_at` |
| `POST /api/usage/readings` | `usage_reading_add` | Complete | Exactly one figure — `percent` (0–100) or `spend_usd` (>= 0); optional `at` (RFC 3339), `session_percent` (0–100), `lanes` (>= 0). Source defaults to the built (typed) kind; pasted/account kinds answer 501. Stored once when forwarded by a peer |
| `DELETE /api/usage/readings/{id}` | `usage_reading_delete` | Complete | Delete one reading by id; 404 on an unknown id; audited by the storing daemon; the id is URL-escaped so it stays one path segment |
| `PUT /api/usage/extra-credit` | `usage_extra_credit_set` | Complete | `{"points": <0 or more>}`: the bought extra credit on the shared series; effective ceiling = weekly allowance + extra credit. A setting, never a reading |
| `PUT /api/usage/remaining-work` | `usage_remaining_work_set` | Complete | `{"points": <0 or more>}`: the typed remaining work under the run-flat-out policy; the finish-early estimate = queue × per-task cost vs this figure. `{"points":null}` clears the typed figure — the tool omits `points`, or passes it as null. Daemon-local, never part of the shared series |
| `PUT /api/config` key `usage.calculator.variant` (and `usage.calculator.policy`) | `config_set` | Complete | the calculator's stored variant/policy are ordinary daemon configuration, set through the general config tool: `{ "usage.calculator.variant": "pace-gauge" }`, with the same validation as REST |

### Per-LLM dollar budget

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/budget` | `budget_status` | Complete | `{"budgets": [...]}` for every LLM that has a budget (spend, ceiling, remaining, window, `rolls_over_at`, `full`, optional estimated `allocation`, per-peer lines); `?llm=<name>` returns one LLM (404 without a budget). CLI `datawatch budget status [--llm NAME]`; comm `budget [status] [<llm>]` |
| `GET /api/budget/usage` | — | Complete | This daemon's own spend for `?llm=<name>`, never any peer spend; optional RFC 3339 `from`/`to` default to the local window. Read by a peer that lists this daemon in its `peer_window`; no MCP tool |
| `PUT /api/llms/{name}` (`budget` object) | `llm_update` / `llm_add` (`budget`) | Complete | `budget` is `{ceiling_usd, hours` or `weekday`+`time`+`zone, allocation_pct, peer_window}`; replaced as a whole, kept when absent, cleared by `null` or `{}`. CLI `datawatch llm add/update --budget-ceiling-usd/--budget-hours/--budget-weekday/--budget-time/--budget-zone/--budget-allocation-pct/--budget-peer-window/--clear-budget` |
| `GET /api/capacity` (`budgets` array) | `capacity_status` | Complete | Same budget states next to the pools; `GET /api/autonomous/status` carries the same `budgets` array |
| `PUT /api/config` (`autonomous.plan_tier_allocation_usd`) | `autonomous_config_set` (`plan_tier_allocation_usd`) | Complete | Operator policy: notional per-window dollar allocation per plan tier; feeds only the estimated `allocation_pct` and never blocks |

### Resource fingerprint exchange between federated peers

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/federation/fingerprints` | — | Complete | Peer-facing responder: the **peer daemon** calls it (not the operator), so no MCP tool. Requires the `federation:fingerprint` capability **and** per-peer `fingerprint_sync` opt-in on both sides; a peer token without the capability gets no read access at all. Returns per-pair hashes only — node identities and billing-group labels, never raw values |
| `GET /api/federation/fingerprint-matches[?state=pending\|confirmed\|rejected]` | `federation_fingerprint_matches` | Complete | Operator-facing match records; `state` maps to the REST parameter (omit for all states); each record has `id`, `peer`, `kind`, both resource names, state, and `shared_with` (confirmed peers only) |
| `POST /api/federation/fingerprint-matches/{id}/confirm` | `federation_fingerprint_confirm` | Complete | Operator decision: the two resources become one shared pool — a confirmed shared compute node makes the peer's admitted hold count against local capacity; re-confirming is safe; echoes back the updated record |
| `POST /api/federation/fingerprint-matches/{id}/reject` | `federation_fingerprint_reject` | Complete | Operator decision: the resources stay separate (undoes a prior confirm); the record stays rejected on later re-observation; unknown id is a 404 |

Opt-in rides on the peer record (`federation_peer_add` / `federation_peer_update`
`fingerprint_sync`, CLI `federation peer add|update --fingerprint-sync[=false]`),
and the billing-group label on LLM entries (`llm_add` / `llm_update`
`billing_group`). Decisions are audit-logged (`fingerprint_match_decision`).

### Routing rules

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/routing-rules` | `routing_rules_list` | Complete | Backend auto-selection rules |
| `POST /api/routing-rules/test` | `routing_rules_test` | Complete | Test which backend a task would route to |

### Templates

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/templates` | `template_list` | Complete | |
| `POST /api/templates` | `template_upsert` | Complete | |
| `DELETE /api/templates` | `template_delete` | Complete | |

### Project aliases

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/projects` | `project_list` | Complete | |
| `POST /api/projects` | `project_upsert` | Complete | |
| `DELETE /api/projects` | `project_alias_delete` | Complete | |
| `GET /api/project/summary?dir=` | `project_summary` | Complete | Git status + sessions + stats |

### Device aliases

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/device-aliases` | `device_alias_list` | Complete | For `new: @<alias>:` routing |
| `POST /api/device-aliases` | `device_alias_upsert` | Complete | |
| `DELETE /api/device-aliases` | `device_alias_delete` | Complete | |

### Profiles (Project + Cluster)

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/profiles` | `profile_list` | Complete | |
| `GET /api/profiles/{name}` | `profile_get` | Complete | |
| `POST /api/profiles` | `profile_create` | Complete | |
| `PUT /api/profiles/{name}` | `profile_update` | Complete | |
| `DELETE /api/profiles/{name}` | `profile_delete` | Complete | |
| `POST /api/profiles/{name}/smoke` | `profile_smoke` | Complete | Smoke-test a profile |

### Agents

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/agents` | `agent_list` | Complete | |
| `POST /api/agents` | `agent_spawn` | Complete | |
| `GET /api/agents/{id}` | `agent_get` | Complete | |
| `GET /api/agents/{id}/logs` | `agent_logs` | Complete | |
| `DELETE /api/agents/{id}` | `agent_terminate` | Complete | |
| `GET /api/agents/audit` | `agent_audit` | Complete | Agent audit log |

### Singleton operations

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `POST /api/ask` | `ask` | Complete | Single-shot LLM ask |
| `POST /api/assist` | `assist` | Complete | Quick-response assistant |
| `GET /api/audit` | `audit_query` | Complete | Operator-action log, newest first. Filters: `actor`, `action`, `subject`, `session_id`, `since`, `until`, `limit` (default 100); the same set on the MCP tool arguments, the CLI flags of `datawatch audit` and the `audit key=value` comm command. `since` / `until` must be RFC 3339 or the call is a 400 |
| `GET /api/audit?subject=<s>` | `audit_query` (`subject`) | Complete | Exact match on what the event is about: a task id, a backend name or an LLM name. CLI `datawatch audit --subject <s>`; comm `audit subject=<s>` |
| `GET /api/audit?action=<prefix>*` | `audit_query` (`action` ending in `*`) | Complete | A trailing `*` makes `action` a prefix match: `autonomous_budget_*` lists the budget events, `autonomous_*` every autonomous event (capacity wait and admission, back-off, budget); without it the match is exact. CLI `datawatch audit --action 'autonomous_*'`; comm `audit action=autonomous_*` |
| `GET /api/analytics` | `analytics` | Complete | Session counts, backend distribution |
| `POST /api/reload` | `reload` | Complete | Hot config reload |
| `GET /api/diagnose` | `diagnose` | Complete | Backend reachability + recent errors |
| `GET /api/splash/info` | `splash_info` | Complete | Splash banner info |

### Voice + Vision + Channel bridge

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `POST /api/voice/transcribe` | — | Not in MCP | File upload — MCP tools don't accept binary blobs. Use REST directly |
| `POST /api/vision/describe` | `vision_describe` | Partial | REST accepts multipart file upload; MCP tool accepts a local file path. Both require `vision.enabled: true` |
| `POST /api/channel/reply` | — | Not in MCP | Internal MCP-bridge callback (channel.js / datawatch-channel) |

### Plugin Framework

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `GET /api/plugins` | `plugins_list` | Complete | |
| `POST /api/plugins/reload` | `plugins_reload` | Complete | |
| `GET /api/plugins/{name}` | `plugin_get` | Complete | |
| `POST /api/plugins/{name}/enable` | `plugin_enable` | Complete | |
| `POST /api/plugins/{name}/disable` | `plugin_disable` | Complete | |
| `POST /api/plugins/{name}/test` | `plugin_test` | Complete | Synthetic hook invocation for debugging |

### System Operations

| API Endpoint | MCP Tool | Status | Notes |
|-------------|----------|--------|-------|
| `POST /api/restart` | `restart_daemon` | Complete | |
| `POST /api/update` | — | Not in MCP | Binary update — security-sensitive, must be human-initiated |
| — | `stop_all_sessions` | MCP only | Kill all running sessions |
| — | `memory_reindex` | MCP only | Re-embed all memories |

### Infrastructure

| API Endpoint | Reason not in MCP |
|-------------|-------------------|
| `GET /api/interfaces` | Network interface list — operational, not for AI |
| `GET /api/logs` | Daemon log viewer — debugging tool |
| `GET /api/servers` | Remote server list — proxy config |
| `GET /api/servers/health` | Server health — operational monitoring |
| `GET /api/profiles` | Profile CRUD — config management |
| `POST /api/link/start` | Signal device linking — interactive setup |
| `GET /api/link/status` | Signal link status |
| `GET /api/link/stream` | SSE stream for linking |
| `GET /api/files` | File browser — web UI utility |
| `GET /api/mcp/docs` | MCP tool documentation — meta |
| `POST /api/test/message` | Comm channel test — debugging |
| `POST /api/channel/*` | MCP channel internals |
| `GET /api/ollama/models` | Model list for web UI dropdowns |
| `GET /api/openwebui/models` | Model list for web UI dropdowns |
| `GET /api/rtk/discover` | RTK optimization analysis |
| `POST /api/stats/kill-orphans` | Kill orphaned tmux — operational |
| `GET /api/sessions/finished-oneshots` | List finished one-shot sessions still open — operational (Settings → About) |
| `POST /api/sessions/finished-oneshots` | Close finished one-shot sessions — operational (Settings → About) |
| `GET /api/proxy/*` | Proxy forwarding — infrastructure |
| `GET /api/sessions/aggregated` | Multi-server aggregation — proxy feature |

## Summary

| Category | API Endpoints | MCP Tools | Coverage |
|----------|--------------|-----------|----------|
| Sessions (incl. bind/import/reconcile/rollback/stale) | 16 | 14 | **87%** |
| Memory | 8 | 7 | **88%** (import is file-based) |
| Knowledge Graph | 5 | 5 | **100%** |
| Intelligence | 3 | 3 | **100%** |
| Monitoring | 4 | 2 | **50%** (probes are k8s-specific) |
| Config | 3 | 1 | **33%** (writes are security boundary) |
| Scheduling | 3 | 3 | **100%** |
| Commands | 2 | 2 | **100%** |
| Alerts | 2 | 2 | **100%** |
| Operations | 2 | 2 | **100%** |
| Cost tracking | 3 | 3 | **100%** |
| Cooldown | 3 | 3 | **100%** |
| Routing rules | 2 | 2 | **100%** |
| Templates | 3 | 3 | **100%** |
| Project aliases | 4 | 4 | **100%** |
| Device aliases | 3 | 3 | **100%** |
| Profiles (Project + Cluster) | 6 | 6 | **100%** |
| Agents (ephemeral workers) | 6 | 6 | **100%** |
| Singleton operations | 7 | 7 | **100%** |
| Voice + Channel | 2 | 0 | **0%** (file uploads + internal callbacks) |
| Autonomous PRD planning | 10 | 10 | **100%** |
| Automation records | 5 | 5 | **100%** (data records only — no automatic advance yet) |
| Observer (unified stats + envelopes) | 4 | 4 | **100%** |
| Plugin framework | 6 | 6 | **100%** |
| PRD-DAG orchestrator | 9 | 9 | **100%** |
| Infrastructure | 17 | 0 | **0%** (by design) |
| **Total** | **137** | **107** | **78%** (100% of user-facing features that fit MCP's tool model) |

All user-facing features have MCP coverage. The 30 endpoints without MCP tools
are infrastructure, operational, file-upload, or security-sensitive operations
that should remain human-controlled or don't fit MCP's text-in / text-out
contract.
