---
docs:
  index: true
  topics: [agent-tracing, trace, observability]
exec_params: []
exec_steps:
  - tool: trace_coverage
    description: Show how much of each backend's activity is traced
    args: {}
    read_only: true
  - tool: trace_rollup
    description: Roll up time, errors and tokens across traced sessions
    args: {}
    read_only: true
---
# Agent tracing

Agent tracing records what your agents did: each tool call and its result, each
model request, state changes, capability and permission decisions, loop warnings
and errors. You read it as a timeline for one session or for a whole Automaton,
or as a roll-up that shows where time, errors and tokens went.

Tracing is on by default at the **summary** level. It records and reads; it never
changes how an agent runs.

> OpenTelemetry export is a follow-up. The trace stays in datawatch for now and
> is read through the channels listed below.

## 1. Base requirements

- A running daemon (see [Setup + install](setup-and-install.md)).
- At least one session or Automaton that has run since tracing was on. Events
  recorded before then do not exist and are not reconstructed.
- Reading the trace needs the analytics read capability (relevant for federated
  peers and scoped tokens).

## 2. Setup

Tracing needs no setup for the default. To change it, set the `tracing.*`
settings in the config file, with `datawatch config`, in the PWA under
Settings → Tracing, or over the API.

```yaml
tracing:
  mode: summary        # off | summary | full
  arg_cap_bytes: 512   # 0 = default
  retention_days: 0   # 0 = keep while the session exists
  backends:            # a backend not listed is on
    aider: false
```

### Modes

| Mode | What is stored |
|------|----------------|
| `off` | Nothing. Trace reads answer "tracing is not available" (HTTP 503). |
| `summary` (default) | Every event, with a short argument summary: the first line only, at most 120 bytes (less if `arg_cap_bytes` is smaller). |
| `full` | Every event, with the whole argument and outcome text up to `arg_cap_bytes`. Use it while you debug an agent, then go back to summary. |

### Settings

| Setting | Default | Meaning |
|---------|---------|---------|
| `tracing.mode` | `summary` | Detail level, above. |
| `tracing.arg_cap_bytes` | `512` | Upper bound, in bytes, of a stored argument or outcome text. Range 0 to 1048576. A longer value ends with `...[truncated]`. |
| `tracing.retention_days` | `0` | Age-based pruning, in days. A trace is kept as long as its session exists; `0` (the default) never prunes by age. A value above 0 prunes traces older than that many days even while the session exists, applied at the next daily prune. A negative value or one above 3650 is refused. |
| `tracing.backends` | all on | Map of backend name to `true` or `false`. Turning one off stops tracing for that backend only. Mode `off` overrides it. |

A value outside its range, an unknown mode or an empty backend name is rejected
when the config loads or when you set it.

## 3. Coverage level per backend

Backends differ in how much of their activity datawatch can see. The trace states
this per backend instead of hiding it.

| Level | Meaning | Backends |
|-------|---------|----------|
| **full** | Every tool call, result and model request is traced, from the backend's own transcript. | `claude-code`, `pi`, `pi-prompt`, `goose`, `goose-prompt`, `opencode-acp` |
| **partial** | Some event kinds are traced; others are not visible. | `opencode`, `opencode-prompt`, `aider`, `gemini` |
| **events-only** | Only coarse events the daemon observes itself: request start and end, status, duration, tokens where known, and session state changes. | `ollama`, `openwebui`, `shell` |

Missing detail is not the same as no activity. A backend with less coverage may
have done work the trace cannot show. Events derived from daemon-observed state
carry the note `detail unavailable: derived from daemon-observed state`.

Check coverage live with `datawatch trace coverage`; it reports per session and
per backend.

## 4. Walkthrough: why did this Automaton stall?

1. Find the Automaton id (the Automata tab, or `datawatch automation list`).
2. Read its trace, errors only:

   ```
   datawatch trace automaton <id> --errors
   ```

3. Roll it up by task to see which task burned the time:

   ```
   datawatch trace rollup --dimension task
   ```

4. Narrow to one session and one tool:

   ```
   datawatch trace session <session-id> --tool Bash --limit 50
   ```

5. Check the backend's coverage before concluding nothing happened:

   ```
   datawatch trace coverage --backend <backend>
   ```

## 5. Reading the timeline

Events appear oldest first. Each has a kind, a name (tool, model or state), an
argument summary, an outcome, a duration, and token counts in and out where the
backend reports them. Every event carries the session, and where they apply the
task, story, Automaton, agent and backend.

| Kind | What it records |
|------|-----------------|
| `tool_call` | An agent called a tool. |
| `tool_result` | The tool answered. Calls and results are paired, so a result shows the duration. |
| `model_request` | A request to the model, with tokens in and out. |
| `state_change` | A session changed state (running, waiting for input, complete, and so on). |
| `capability_check` | A capability was checked; a denial is recorded with outcome `denied`. |
| `permission_decision` | A permission request was relayed or decided. |
| `loop_warning` | The same tool was called again and again with the same argument summary. |
| `error` | A failure, with the redacted message as the outcome. |

The **roll-up** groups events along one dimension: `session` (default), `task`,
`story`, `automaton`, `agent` or `backend`. It shows event, tool-call, error and
model-request counts, tokens in and out, and per tool the calls, errors and
average and maximum milliseconds. A **loop warning** is raised when one tool is
called with the same argument summary five times or more; change the threshold
with `loop_threshold` (CLI `--loop-threshold`).

Filters work on every read: `tool`, `backend`, `kind`, `errors` (error events and
failed outcomes only) and `limit` (the last N events).

## 6. All channels

| Channel | How |
|---------|-----|
| **PWA** | A Trace sub-tab under the session's Status tab (beside Status and Stats) and a Trace tab in the Automaton view: timeline, filters (tool, backend, kind, errors only), roll-up, loop warnings and a coverage badge per backend. It refreshes itself while a session runs. Settings → Tracing holds the four settings. The card is localised (English, German, Spanish, French, Japanese). |
| **CLI** | `datawatch trace session <id>`, `trace automaton <id>`, `trace rollup`, `trace coverage`. Filters: `--tool`, `--backend`, `--kind`, `--errors`, `--limit`, `--loop-threshold`. Settings with `datawatch config`. |
| **REST** | `GET /api/trace/session/{id}`, `GET /api/trace/automaton/{id}`, `GET /api/trace/rollup`, `GET /api/trace/coverage`. Query keys: `tool`, `backend`, `kind`, `errors`, `limit`, `loop_threshold`, and for the roll-up `dimension` and `key`. |
| **MCP** | `trace_session`, `trace_automaton`, `trace_rollup`, `trace_coverage`, `trace_stats`. They call the REST routes, so results match. |
| **Chat** | `trace session <id> [tool=] [backend=] [kind=] [errors=true] [limit=]`, `trace automaton <id> ...`, `trace rollup [dimension=] [key=]`, `trace coverage [backend=]`. |

## 7. Redaction guarantees

Redaction runs **before** an event is stored. Nothing unredacted is written to
disk, so no later read, export or channel can reveal it.

- **Secrets** are removed with the same detectors as the push-time secret scan
  (see [Secret scan push gate](secret-scan-push-gate.md)). They are removed
  before truncation, so a cut can never split a secret and leave part of it.
- **Paths:** a project directory becomes a relative path, and any home directory
  (Linux, macOS or Windows, including the user name) becomes `~`.
- **Size:** argument and outcome text is cut to the cap and marked
  `...[truncated]`. In summary mode only the first line survives, after the whole
  text was redacted, so a multi-line secret is removed before any line is dropped.
- **Mode off** stores no argument summary at all.
- Files are written with owner-only permissions under the data directory's
  `trace/` folder.

Redaction is pattern based. A secret in an unusual shape may not match a detector.
Keep `full` mode for debugging, and review `arg_cap_bytes` if agents handle
sensitive text.

## 8. Retention

A trace is kept as long as its session exists. This is the rule for the
default, `tracing.retention_days: 0`: it never prunes by age, so a trace lives
and dies with its session. Deleting a session deletes its trace file and its
index entries too — including when the session is removed by Automaton cleanup
of sessions.

A value above 0 adds age-based pruning: traces older than that many days are
removed even while the session still exists, applied at the next daily prune
(every 24 hours while the daemon runs). Lowering the value applies at the next
prune; raising it never brings back what was pruned. A negative value or one
above 3650 is refused when the config loads or when you set it.

An archived or ended session that is not deleted keeps its trace. Trace files
are plain files, one per session, under the data directory's `trace/` folder.

## 9. Troubleshooting

| Symptom | Cause and fix |
|---------|---------------|
| Reads return 503 "tracing is not available" | `tracing.mode` is `off`, or the trace store could not open. Set a mode and check the daemon log. |
| Card says "No trace events recorded yet" | The session has not produced events yet, its backend is turned off in `tracing.backends`, or it ran before tracing was on. |
| A backend shows few events | Check its coverage level in section 3. |
| Argument text is cut | Raise `arg_cap_bytes` and use `full` mode. Only new events are affected. |

## See also

- [Audit logging](audit-logging.md): operator actions, a separate record.
- [Session telemetry](session-telemetry.md): structured task telemetry from hooks.
- [Datawatch definitions: Agent trace](../datawatch-definitions.md#agent-trace)
