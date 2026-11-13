# Parity issue draft — agent tracing (datawatch-app)

To be filed on **datawatch-app** by the merge step, titled `[sync] feat(trace): agent trace timeline, coverage and settings`.
Parent release: the next version above main at merge time.

## What changed in datawatch

Agents now leave a normalized trace: tool calls and results, model requests, state changes, capability checks, permission decisions, loop warnings and errors, from every backend. The daemon serves it as a timeline for one session or an Automaton and as roll-ups, with a coverage level per backend. It is on by default at the `summary` level and only records. Howto: `docs/howto/agent-tracing.md`.

This is a capability-parity issue: the app needs the **Trace card** (session and Automaton views), the **tracing settings** and the **Monitor tracing numbers**.

## REST contract from routes added

All five are `GET`, read-only, and need the analytics read capability. Each answers 503 with a plain-text message when tracing is off or the store could not open, and 405 for any other method. Full schemas are in `internal/server/web/openapi.yaml` (tag `Trace`).

| Route | Answers |
|---|---|
| `GET /api/trace/session/{id}` | `{session, total, events[], rollup, index}`; query `tool`, `backend`, `kind`, `errors` (true/false), `limit` (last N), `loop_threshold`; 404 `no trace for session <id>` when nothing is recorded (treat as empty, not a failure) |
| `GET /api/trace/automaton/{id}` | `{automaton, sessions[], total, events[], rollup}`; same query keys; 404 `no trace for automaton <id>` |
| `GET /api/trace/rollup` | `{dimension, rollups[]}`; query `dimension` (`session`, `task`, `story`, `automaton`, `agent`, `backend`; default `session`), `key`, `loop_threshold` |
| `GET /api/trace/coverage` | `{sessions[{session, backend, automaton, coverage, events}], by_backend{backend:{level:count}}}`; query `backend`; levels `full`, `partial`, `events-only`, `unknown` |
| `GET /api/trace/stats` | `{sessions, events, error_events, appended_total, append_errors_total, pruned_total, last_event_at}` for a Monitor card; the same numbers are `trace_*` fields of `GET /api/stats` |

Event fields: `session_id`, `backend`, `kind` (`tool_call`, `tool_result`, `model_request`, `state_change`, `capability_check`, `permission_decision`, `loop_warning`, `error`), `name`, `arg_summary`, `outcome`, `duration_ms`, `tokens_in`, `tokens_out`, `timestamp`. Render every value as text; event text is redacted by the daemon but is still untrusted.

## Card

Show the coverage badge per backend (with the note for a non-full level), the roll-up (events, tool calls, errors, model requests, tokens, slowest tools, loop warnings), the filter bar (tool, backend, kind, errors only, clear) and the timeline, newest last, capped to the last 200 lines with a "n earlier events not shown" line. Refresh about every 10 seconds for a running session and 30 seconds for an Automaton. Tracing off reads as "Tracing is off, or the trace store is not available", never an error banner.

## Settings (read and write)

`GET`/`PUT /api/config` under `tracing`. One invalid value rejects the whole PUT and stores nothing; show the daemon's message.

| Key | Label | Type | Default | Range |
|---|---|---|---|---|
| `tracing.mode` | Trace level | `off`, `summary`, `full` | `summary` | |
| `tracing.arg_cap_bytes` | Argument size cap (bytes) | whole number | 512 | 0 to 1048576 |
| `tracing.retention_days` | Days to keep events | whole number | 0 | 0 to 3650 |
| `tracing.backends` | Backends traced | map of backend to on/off | all on | a backend not listed is on |

## Locale keys

Add the strings to **all five bundles** (`en`, `de`, `es`, `fr`, `ja`): the `trace_*` keys, `timeline_trace_label`, the `settings_tracing_*` keys and the `stats_trace*` keys in `internal/server/web/locales/en.json`. Copy them with the same names. **Please translate DE, ES, FR and JA through the Compose pipeline**: the datawatch repository already carries real translations for all four, so check them against your own terms rather than leaving English.

## Acceptance

- The card shows coverage, roll-up, filters and timeline for a session and for an Automaton, and degrades to the off and empty messages.
- Each setting loads, saves and shows the daemon's rejection message on an invalid value.
- The Monitor screen shows the tracing numbers from `GET /api/trace/stats` (or the `trace_*` stats fields) and hides them while tracing is off.
- Nothing in the app changes how a session runs because of the trace.

## Parity surface

| Surface | Included | Reason or where |
|---|---|---|
| REST | Yes | the five `GET /api/trace/*` routes above; `tracing` in `GET`/`PUT /api/config` |
| MCP | Yes | `trace_session`, `trace_automaton`, `trace_rollup`, `trace_coverage`, `trace_stats`; the same numbers are on `get_stats` |
| CLI | Yes | `datawatch trace session|automaton|rollup|coverage` |
| comm channel | Yes | `trace session|automaton|rollup|coverage`; `configure tracing.<key>=<value>` |
| YAML/config | Yes | the `tracing` block in `docs/config-reference.yaml` |
| PWA | Yes | Trace card in the session and Automaton views, Settings > Tracing, the Monitor Tracing card |
| Android | Yes | this issue: card, settings and Monitor numbers |
| iPhone | Yes | the same issue text applies once the iOS app exists; there is no iOS app in this repository's release set, so it is tracked on the same issue |
