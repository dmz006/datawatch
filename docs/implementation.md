# Implementation Guide — datawatch

This document is intended for developers who want to understand, modify, or extend `datawatch`.

---

## 1. Project Layout

```
datawatch/
├── cmd/
│   └── datawatch/          # CLI entry point (cobra commands)
│       ├── main.go             # Root command, start, link, config, session subcommands
│       └── ...
├── internal/
│   ├── alerts/
│   │   └── store.go            # Alert, Store — persistent system alert log
│   ├── config/
│   │   ├── config.go           # Config struct, Load(), Save(), LoadSecure(), SaveSecure(), applyDefaults()
│   │   └── encrypt.go          # AES-256-GCM encryption helpers (IsEncrypted, Encrypt, Decrypt, DeriveKey, LoadOrGenerateSalt)
│   ├── secfile/
│   │   └── secfile.go          # AES-256-GCM file encryption helpers (Encrypt/Decrypt/ReadFile/WriteFile)
│   ├── llm/
│   │   ├── backend.go          # llm.Backend interface definition
│   │   ├── registry.go         # Register() and Get() for named backends
│   │   └── claudecode/
│   │       └── backend.go      # claude-code implementation of llm.Backend
│   ├── messaging/
│   │   └── backend.go          # messaging.Backend interface + registry
│   ├── router/
│   │   └── commands.go         # Parse(), HelpText(), Command types (including CmdSetup)
│   ├── server/
│   │   ├── server.go           # HTTP server setup, mux routing, auth middleware
│   │   ├── api.go              # REST API handlers (/api/sessions, /api/config, etc.)
│   │   └── ws.go               # WebSocket hub, client pumps, message types
│   ├── wizard/
│   │   ├── wizard.go           # WizardManager, WizardSession, Step, Def — stateful multi-turn wizard engine
│   │   └── defs.go             # Wizard definitions for all 12 services (signal/telegram/.../web/server)
│   ├── session/
│   │   ├── store.go            # Session struct, Store (JSON persistence), state constants
│   │   ├── schedule.go         # ScheduledCommand, ScheduleStore — persistent command scheduler
│   │   ├── cmdlib.go           # SavedCommand, CmdLibrary — named reusable command library
│   │   └── filter.go           # FilterPattern, FilterStore, FilterEngine, ActionHandlers
│   └── signal/
│       ├── backend.go          # SignalBackend interface, Group, IncomingMessage types
│       ├── signalcli.go        # SignalCLIBackend — signal-cli subprocess management
│       └── types.go            # JSON-RPC types: Request, Response, Envelope, DataMessage
├── docs/
│   ├── api/                    # OpenAPI specification
│   ├── architecture.md         # Component overview and diagrams
│   ├── app-flow.md             # Application flow with Mermaid diagrams
│   ├── commands.md             # Command reference
│   ├── data-flow.md            # Data flow sequence diagrams
│   ├── design.md               # Design rationale and decisions
│   ├── future-native-signal.md # Roadmap for native Go Signal backend
│   ├── implementation.md       # This document
│   ├── multi-session.md        # Multi-machine setup guide
│   ├── operations.md           # Operations and troubleshooting
│   ├── planning.md             # Development phases and milestones
│   ├── pwa-setup.md            # PWA and Tailscale setup
│   └── setup.md                # Installation and quickstart
├── install/
│   ├── install.sh              # System-wide install script (requires root)
│   └── install-user.sh         # User service install script (no root)
├── web/                        # PWA static files (embedded into binary)
│   ├── index.html
│   ├── app.js
│   ├── style.css
│   └── manifest.json
├── go.mod
├── go.sum
├── Makefile
├── README.md
└── LICENSE
```

---

## 2. Core Concepts

### Session Lifecycle and States

A session moves through these states in its lifetime:

```
[created] → running → waiting_input ⇄ running
                    ↓
              complete | failed | killed
```

| State | Meaning |
|---|---|
| `running` | tmux session alive, claude-code executing, output being produced |
| `waiting_input` | Output has been idle for `input_idle_timeout` seconds and the last line matches a prompt pattern |
| `complete` | claude-code exited cleanly (tmux session gone, no error) |
| `failed` | tmux session gone unexpectedly, or claude-code exited non-zero |
| `killed` | User sent `kill <id>` command |

State transitions trigger the `onStateChange` callback set on the session manager, which both the Signal router and the HTTP WebSocket hub subscribe to.

### The Session Store

`internal/session/store.go` implements a simple in-memory cache backed by a flat JSON file.

- All sessions are kept in `map[string]*Session` (key: `FullID = hostname-shortid`)
- A `sync.RWMutex` protects concurrent access from multiple goroutines (monitor goroutines, HTTP handlers, router callbacks)
- Every mutating operation (Save, Delete) calls `persist()`, which atomically rewrites the full JSON file
- On startup, `NewStore()` reads and unmarshals the existing file; an empty or missing file produces an empty store

### The tmux Integration

Every session is backed by a named tmux session:

- **Naming convention:** `cs-{hostname}-{shortid}` (e.g. `cs-myhost-a3f2`)
  - `cs-` prefix namespaces datawatch sessions from user sessions
  - hostname is included to avoid collisions when multiple users share a tmux server
- **Output capture:** `tmux pipe-pane -o -t <session> 'cat >> <logfile>'` redirects all pane output to a log file
  - The `-o` flag means "only pipe stdout, not stdin"
  - The log file path is `{data_dir}/logs/{hostname}-{shortid}.log`
- **Input injection:** `tmux send-keys -t <session> "<text>" Enter` sends keystrokes to the running process

### Output Monitoring

Each active session has a dedicated `monitorOutput` goroutine that uses **fsnotify**
(interrupt-driven file watching) instead of polling. The goroutine:

1. Opens the session's log file (waits if not yet created)
2. Seeks to the end of the file (skips history on resume)
3. Watches the log file with fsnotify; reads new lines on write events
4. Maintains a line buffer and resets an idle timer on each new line
5. When the idle timer fires (no new output for `input_idle_timeout` seconds):
   - Checks if the tmux session still exists
   - If gone: marks the session `complete` or `failed`
   - If alive: checks whether the last buffered line matches a prompt pattern (ends with `?`, `>`, `[y/N]`, `: `, etc.)
   - If pattern matches: marks `waiting_input`, calls `onNeedsInput`
6. When new output arrives after `waiting_input`: marks `running` again
7. The goroutine exits when the session reaches a terminal state or the context is cancelled

### Needs-input alert gating

`onNeedsInput` detections pass through `session.AlertGate` (`internal/session/alertgate.go`)
before any alert, push or comm message is sent. Config (`DetectionConfig` in
`internal/config/config.go`, read via `Config.GetDetection`):

| Field | Default | Meaning |
|---|---|---|
| `detection.alert_settle` | 45 | seconds a session must stay `waiting_input` before alerting; leaving the state cancels it |
| `detection.alert_repeat` | 300 | seconds an identical prompt for the same session is not re-alerted |

Both are settable via YAML, `PUT /api/config`, comm `configure detection.alert_settle=N`,
MCP `detection_config_set`, CLI `datawatch config set`, and the PWA Settings > Detection timing.
Android/iPhone reflect the behaviour through push (no client setting; see datawatch-app#190).

### Capacity admission

`internal/capacity.Ledger` holds named pools (`host`, `node:<n>`, `llm:<n>`) with
limits. `Manager.admit` (`internal/autonomous/capacity.go`) acquires an all-or-nothing
lease before each task spawn and releases it when `executeOne` returns; waiting shows
as `waiting_capacity` with `wait_reason`. `cmd/datawatch/capacity_wiring.go` builds the
ledger, syncs limits every 10 s from `session.max_sessions`/`reserved_interactive`,
compute node `max_concurrent_sessions` and LLM `max_inflight`, reaps leaked leases and
raises a 30-minute wait alert. Config: `autonomous.capacity_enabled`,
`autonomous.capacity_wait_timeout_seconds`, `autonomous.capacity_gpu_util_pct`,
`autonomous.capacity_fallthrough`,
`session.reserved_interactive`. See `docs/howto/automata-capacity.md`.

Pool fall-through: `autonomous.capacity_fallthrough` (bool, default false). When on and a task's
requested pools cannot be granted now (`Ledger.WouldGrant`) while another allowed backend/model
for the same task can, `acquireLease` admits it there (`internal/autonomous/capacity.go`,
alternates from `Manager.SetCapacityAlternates`), the executor spawns on that pair, and the
`autonomous_capacity_admitted` audit entry carries `fall_through`, `from_pools`, `to_pools` and a
readable note. When off, a task always waits for its own pool, explicit backend or not.

Node placement rules (`internal/capacity/ledger.go`, wired in `cmd/datawatch/capacity_wiring.go`;
each switch is read at call time, so a change applies to the next admission with no restart,
except the adoption wait, which is read once as the ledger starts):

- `autonomous.capacity_strict_node_limit` (bool, unset = true): `Adopt` never lets a
  `node:<n>` pool read over its limit. A session adopted onto a node already at its limit
  is recorded with `Lease.NodeOverflow`, held outside the node pool but still counted in
  the host and `llm:` pools and as an occupant of the node for new admissions
  (`Snapshot` reports it as the node's `Overflow`, so `Held+External` never exceeds
  `Limit`). It joins the node pool when the pool has room. A verifier or judge whose
  entry pools two or more nodes carries those nodes as its choices and is placed on the
  least loaded node with room, or waits (`judgeChoices` in `cmd/datawatch/judge_capacity.go`).
  Off: `Adopt` adds the node pool whatever its use.
- `autonomous.capacity_count_pool_by_node` (bool, unset = true): an `llm:<n>` pool counts
  the sessions on each node at most up to that node's own limit (`heldByNodeLocked`), so
  sessions piled on one node do not use slots a free node could take. The pool's own
  limit is unchanged. Off: every lease counts once.
- `autonomous.capacity_adopt_before_admit` (bool, unset = true) and
  `autonomous.capacity_adoption_wait_seconds` (whole seconds >= 0, unset or 0 = 30):
  the ledger starts holding every request (`BeginAdoption`) until
  `resumeRunningPRDs` has adopted the work that survived a restart
  (`MarkAdoptionComplete`), or the wait passes. The daemon's node health view also
  starts strict: no node is healthy until the first read of the compute registry, so
  nothing is placed on a node the ledger has not loaded. Off: requests are admitted
  at once and the strict view does not hold placement.

All four keys are in `GET`/`PUT /api/config`, `datawatch config set`, the MCP
`config_set` and `autonomous_config_set` tools, the chat `configure` verb and the PWA
Settings capacity section; defaults are in `internal/config/capacity_node_limit.go`.

Ledger lock watchdog (`internal/capacity/watchdog.go`): `Ledger.SnapshotWatched` wraps
`Snapshot` for request paths (`/api/capacity`, the comm `capacity` reply). If the snapshot
has not returned after 5 seconds (`DefaultLockWatchdogThreshold`) `LogLockWedge` logs
`capacity ledger lock held for N seconds` once per wedge episode, however many callers
are waiting, followed by the stacks of goroutines inside `internal/capacity`, which include
the lock holder. The watched call keeps waiting and returns normally once the lock frees.
Internal loops (reaper, limit sync) use plain `Snapshot`.

Reactive usage back-off (`internal/autonomous/backoff.go`): an in-memory per-backend flag
that holds autonomous dispatch (never interactive starts) after a rate/usage limit.
Each entry carries `backend`, `until`, `reason` (the free-form text), `source`
(`rate_limit` | `operator` | `burn_guard`) and `reset_known`; a flag set by a ladder
rung (the burn guard's `hold_claude` hold, via `Backoff.SetWithRung`) additionally
carries `rung` with the rung's name — omitted in the JSON on plain rate-limit and
operator entries, so older clients read the legacy fields unchanged. On the capacity
response (`GET /api/capacity`) the `backoff` array mirrors the same fields plus
`rung`, and `until` is rendered in the daemon's own zone. Usage-backoff status
(`/api/usage-backoff`, the `usage_backoff` field of the autonomous status) reuses the
entry view directly, so `rung` appears there too. The `capacity_status` MCP tool
description names `rung` beside `reset_known`. Config: `autonomous.usage_backoff`
(`pause_backend` | `off`, default `pause_backend`; `off` disables only the automatic
path) and `autonomous.backoff_max_wait_minutes`
(default 60; whole minutes, a value below 1 is treated as 60). Operator surfaces:
`/api/usage-backoff`, `usage_backoff_*` MCP tools, `datawatch usage-backoff`, comm
`usage_backoff`. See `docs/howto/automata-capacity.md`.

Usage calculator settings (`internal/config/usage_calculator.go`): the `usage.calculator`
block holds `variant` (`pace-gauge` | `lane-planner` | `burn-ledger` | `all`, default `all`),
`policy` (`use-all` | `pace` | `finish-early`, default `use-all`), `safety_points` (0 to 50,
default 5), `cost_per_task_points` (above 0, default 0.1, a starting figure to calibrate
against readings), `baseline_per_24h` (0 to 20, default 3), `max_lanes` (1 to 8, default 3),
`auto_lanes` (`true` | `false`, default `false` — when on the daemon applies the
calculator's lane advice itself), `auto_lanes_max` (the operator ceiling the daemon
never exceeds, 1 to 8, default 2), `auto_lanes_special_priority` (an open automaton
whose capacity priority is at or above this value counts as a special item while
auto-lanes weighs the lanes, 0 or above, default 1),
`reset_weekday` (`sunday`..`saturday`, default `sunday`), `reset_time` (24-hour `HH:MM`,
default `08:00`), `reset_zone` (IANA zone name, default empty — empty means the zone the
daemon runs in, falling back to `UTC`; a fixed offset or an unknown name is rejected) and
`keeper` (federation peer that keeps the shared readings; empty means
this daemon). Normalisation, `ValidateUsageCalculator*` and `EffectiveUsageCalculator` follow
`usage_backoff.go`. A saved value that is no longer allowed falls back to its default, and a
retired `variant` falls back to `all` with one log line. All keys are settable at runtime:
`GET`/`PUT /api/config` (`internal/server/usage_calculator_config.go`, a 400 on an invalid
value), comm `configure usage.calculator.<key>=<value>`, the generic `config_set` tool and
CLI `datawatch config set`. The PWA Settings fields are a later change.

Cost report settings (`internal/config/config.go`, `CostReportConfig`): the `cost_report` block. An unset or unrecognised value reads its default.
- `default_group_by` — how a report with no grouping is built: `automaton`, `automation`, `backend` or `week` (default `automaton`).
- `default_window_days` — the window when a request names no start, 1 or more (default `7`).
- `show_usd_estimates` — show dollar figures for subscription backends as labelled estimates at API rates (default `true`).
- `week_start` — the first day of the week for the `week` grouping (default `monday`).
- `include_peer_spend` — include spend recorded on federation peers (default `false`).

Burn guard settings (`internal/config/burn_guard.go`): the `usage.burn_guard` block. An invalid
saved value falls back to its default with a logged warning, as `normalizeUsageCalculator` does.
- `enabled` — guard on or off (default `true`).
- `interval_minutes` — minutes between evaluations, 1 to 1440 (default `60`).
- `margin_points` — how far the projection may run over the pace line before the guard acts, 0 to 50 (default `2.0`).
- `max_projection_gap_points` — largest gap in points allowed between the projection and the pace line, 0 to 100 (default `25`); `0` means never compare.
- `ask_on_change` — whether a rung change also raises an operator question (default `false`: one alert per change, no question; `true`: at most one open guard question, and none for a step that changes nothing).
- `ladder` — ordered rungs applied while over the line: `lower_inflight`, `route_pi_prompt`, `hold_claude` (default all three in that order; unknown or repeated rungs are dropped).
- `step_back` — undo rungs in reverse once back under the line (default `true`).
- `cache_write_points_per_million` — weekly points per million cache-write tokens, above 0 (default `0.373`).
- `cache_read_points_per_million` — weekly points per million cache-read tokens, above 0 (default `0.0069`).
- `refit_on_reading` — refit the cost model when a new usage reading arrives (default `true`).
- `effort_ceiling` — map of task kind to ceiling `low` | `medium` | `high` | `cheaper_backend` (default `docs`, `parity`, `live_check`, `final_gate` all `low`; an unknown ceiling becomes `low`).
- `retry_limit_claude` — most retries of one task on Claude, 0 to 20 (default `2`).

Tracing settings (`internal/trace/config.go`, the `Tracing` field of `Config`, block `tracing`): normalized agent trace events
written by `internal/session/trace_ingest.go` and read through `internal/trace`. Unset values read as their defaults through
`Settings.WithDefaults`; an invalid value is refused at config load and on `PUT /api/config` through `config.ValidateTracing`.
- `mode` — `off` | `summary` | `full` (default `summary`; unset reads `summary`). `off` records nothing and the `/api/trace/*` routes answer 503. `full` keeps the argument summary, `summary` keeps counts and names.
- `arg_cap_bytes` — most bytes of an argument summary stored per event, 0 to 1048576 (default `512`; 0 reads the default).
- `retention_days` — days events are kept before the prune pass removes them, 0 to 3650 (default `14`; 0 reads the default).
- `backends` — map of backend name to on/off; a backend not listed is traced (default empty, all on).
Observability: `Store.Stats()` feeds the `trace_*` fields of `SystemStats` (`trace_enabled`, `trace_sessions`, `trace_events`,
`trace_error_events`, `trace_appended_total`, `trace_append_errors_total`, `trace_pruned_total`, `trace_last_event_age_seconds`), the
Monitor Tracing card, `GET /api/trace/stats` and the Prometheus series `datawatch_trace_events_total`,
`datawatch_trace_append_errors_total`, `datawatch_trace_stored_events` and `datawatch_trace_stored_sessions`.
Redaction runs before anything is stored (`internal/trace/redact.go`); see [howto/agent-tracing.md](howto/agent-tracing.md).

Billing profiles (`internal/config/billing_profile.go`, the `Billing` field of
`UsageConfig`): the `usage.billing` block is an open map keyed by provider
(`claude-code`, `openai`, `gemini`, or any other operator-chosen key). Each
entry is a `BillingProfile` with:
- `mode` — `bundled_plan` | `api_metered` | `free_local`. Unset resolves to `api_metered`, but that is an *assumed default*, not a fact about the provider: `EffectiveBillingProfile` returns it flagged via `IsDefault` so surfaces must label it.
- `plan` — free text naming the subscription plan. For the `claude-code` provider the accepted plans are `pro`, `max_5x` and `max_20x` (the same `inference.PlanTier` vocabulary the LLM entries' `plan_tier` uses).
- `monthly_price` — the plan's monthly price, a finite number ≥ 0 in `currency` (0 = a free plan).
- `currency` — ISO 4217 code of `monthly_price` (e.g. `USD`).
- `reset_weekday` / `reset_time` — optional weekly allowance reset, same vocabulary as `usage.calculator`: a weekday name (`sunday`..`saturday`) and a 24-hour `HH:MM`.
- `allowance_unit` — `percent` (default) | `requests` | `tokens`, the unit the weekly allowance is counted in.

Methods and helpers on the profile:
- `(UsageConfig) EffectiveBillingProfile(provider)` — the profile for an open provider key, or the assumed default profile (`api_metered`, unit `percent`) flagged `IsDefault` when the provider has no entry. Empty `mode`/`allowance_unit` of a configured profile resolve to those defaults.
- `(BillingProfile) PointCostEquivalent()` — the monthly cost-equivalent of one point (1% of the weekly allowance), returned as an estimate: `monthly_price / (weekly allowances per month) / 100` with 52/12 allowances per month. It assumes the full allowance is used each week and ignores overage, mid-cycle prorating and non-`percent` units.
- `(Config) EffectivePlanTierAllocationUSD(tier)` — reconciliation with the existing tier settings. Precedence, documented in the code: the explicit `autonomous.plan_tier_allocation_usd` entry for the tier always wins; when it is absent and a `usage.billing.<provider>.plan` names the tier (`pro`, `max_5x`, `max_20x`), the profile's `monthly_price` stands in for the tier's allocation. The value is *derived, never copied* — the price is stored only once, in the billing profile — so the two cannot drift, and an LLM entry's `plan_tier` needs no stored dollar figure of its own (it resolves through the same table lookup). When several profiles name the same tier with different prices the lowest `monthly_price` wins.

Validation runs in the config validate path next to the `plan_tier_*` checks
(`validateLoaded` → `validateBillingProfiles`): an empty provider key, an
unknown `mode`, a negative or non-finite `monthly_price`, a bad `reset_weekday`
or `reset_time`, and an unknown `allowance_unit` are all refused. Tests are in
`internal/config/billing_profile_test.go` (temp dirs and fakes only).

Shared readings federation (`internal/usagecalc/shared.go`,
`internal/server/usage_readings.go`): `usage.calc.SharedSeries` wires one daemon's local
store to the federation — with an empty `keeper` the daemon keeps the series; otherwise
it reads the series from the keeper daemon and forwards the writes there, holding the
last on-disk copy (served stale-flagged when the keeper is down, decision 8). The REST
surface: `GET /api/usage/readings` (read), `POST /api/usage/readings`,
`DELETE /api/usage/readings/{id}`, and `PUT /api/usage/extra-credit` (write), gated on
`federation.CapUsageRead`/`CapUsageWrite`. Requests carrying `X-Usage-Hop: 1` (sent by a
peer's fetch) always see the keeper's local store, whatever that daemon's own keeper
setting says — the read can never re-forward. A write that cannot reach the keeper is
refused whole and stored locally nowh ere (`usagecalc.ErrKeeperUnreachable`). A stale
read carries `X-Usage-Stale` / `X-Usage-Copied-At` response headers; the body is always
the same bare record. Server-side tests are in `internal/server/usage_readings_test.go`.

LLM role tags (`internal/inference/role_resolve.go`, `role_contention.go`): `llms[].roles`
is an open set of strings (trimmed, lower-cased, de-duplicated; empty rejected). An
explicit per-call-site backend or model always wins; otherwise the entry claiming the role
is used (several claimants resolve by name with an ambiguity warning). Contention (two
entries claiming one role, or two roles converging on one entry) is checked on every llms
write and by a periodic scan. Config: `autonomous.role_scan_interval_seconds` (default 300;
a value below 1 is treated as 300). Warnings are audited as `llm_role_contention` and shown
under `status.role_warnings` of the llm listing; they never fail a write.

`autonomous.ci_watch_timeout_seconds` (default 1500) bounds how long the optional
`ci-watch` completion guardrail waits for the CI runs of the commit an Automaton
pushed. It only applies when `ci-watch` is listed in `autonomous.per_prd_guardrails`
(or an Automaton's own PRD-level guardrails); the list is empty by default.

`autonomous.detail_text_cap_bytes` (default 4096; 0 = default, negative = off) caps each
heavy output text field (PRD `memory_report`, task `error`, `prior_attempt_hint`,
`start_failure_tail[]`, `attempt_refs[]`, verdict and verification text) in the default
`GET /api/autonomous/prds/{id}` reply
(`internal/autonomous/prd_view_cap.go`, `CapPRDText`). The PRD `spec`, every story
description and every task spec are always returned whole. Capped fields end with a
`[truncated ...]` marker and are listed in the record's `truncated` array with the URL of the
full text, and every spec/description write refuses (400) text carrying the marker
(`autonomous.TruncationRefusal`). The store is never cut. `?full=1` returns the whole record, and
`GET /api/autonomous/prds/{id}/outputs` (optionally `?task=<id>`, or
`/tasks/{task_id}/outputs`) returns the heavy fields byte for byte. The list summary
(`BuildSummaryView`) does not read these fields.

`autonomous.inherited_tags_cache_seconds` (default 15; 0 = default, negative = off)
sets the lifetime of the shared cache of inherited (system) tags per Automaton
that the session manager builds when listing sessions (`internal/session`
`system_tag_index.go`, `Manager.SetInheritedTagsCacheTTL` / `InheritedTagsCacheTTL`).
A negative value turns the cache off: every read rebuilds the index (concurrent
callers still share one in-flight build). A config write applies the lifetime to
the live session manager immediately (no restart), and the daemon applies it at
startup alongside the other session-manager settings.

Foreign-file drift is checked for direct-mode (non-worktree) Automata only. The
capture point is the worker spawn in `cmd/datawatch/main.go`: alongside the pre-task
`HEAD` SHA and untracked files it calls `autonomous.CapturePreTaskDirtyFiles`
(unstaged ∪ staged tracked files), skipped when the task runs in a PRD-owned worktree.
The values ride back on the spawn result into `Task.PreTaskSHA` /
`PreTaskUntrackedFiles` / `PreTaskDirtyFiles`. In `executeOne`, right after the
verifier returns, `checkForeignFileDrift` (`internal/autonomous/file_drift.go`)
unions the files of every commit in `<pre-task SHA>..HEAD` and splits them via
`ClassifyFileDrift` into **pre-existing drift** (already dirty/untracked before
the task started and not in the task's `files`) and **in-run edits** (the
worker's own committed files, planned or not; e.g. a new file it created). Pre-existing findings are stored on `Task.ForeignFileDrift`; both kinds are
appended as a `file_drift_detected` decision and emitted as the
`autonomous_file_drift_detected` audit event (audited as `pre_existing` or
`in_run`).

Enforcement is set by `autonomous.drift_mode` — `"block"` (the default; an
empty or unknown value resolves to `"block"`, and the config surfaces accept
only `"block"` or `"warn"`) or `"warn"` — and applies only to pre-existing
findings (see `resolveDriftMode` + `driftBlockExplicitlyOff`). Under `"block"`
the task is set to `TaskBlocked` with error
`foreign_file_drift: <files>`, the PRD halts as `blocked`, and the task walk
stops — unless the operator explicitly opted that Automaton out with
`block_on_file_drift=false` on the PRD or its guardrail profile (the legacy
daemon-wide flag's `false` default is *not* an opt-out). Under `"warn"` the
task records the finding and still completes. In-run edits (the task's own
worker's commits) are always warned, never blocked, in either mode.

Recovery: a blocked PRD now accepts per-task approval — `ApproveTask`
(`ApproveTask` in `internal/autonomous/manager.go`, REST
`POST /api/autonomous/prds/{id}/approve_task`, MCP
`autonomous_prd_approve_task`) accepts PRDs in the `blocked` status (draft and
cancelled still refuse), and approving a `TaskBlocked` task releases it back to
`TaskPending` with the stale error cleared, ready for the existing run path to
pick it up; the PRD's own status transition stays with the run path. The
drift check remains read-only — it never reverts, resets, amends or rewrites
history. See `docs/howto/file-drift-guardrail.md`.

Undeclared-edit mode — `autonomous.undeclared_edit_mode`
(`config.AutonomousConfig.UndeclaredEditMode`, YAML `undeclared_edit_mode`;
default `"warn"`, no `omitempty` so an explicit value round-trips) governs
the undeclared-edit check, which covers the worker's own edits while
`drift_mode` keeps governing pre-existing drift. Resolution is
`EffectiveUndeclaredEditMode` on both the daemon config and
`autonomous.Manager`: lowercase/trim, only `off|warn|block` pass, anything
else resolves to `"warn"`. The enforcement is `checkUndeclaredEdits`
(`internal/autonomous/file_drift.go`), run in `executeOne` right after
`checkForeignFileDrift`. It takes the `inRun` set from `ClassifyFileDrift`
(so a file is never in both sets) and subtracts `FilesPlanned` and
`FilesGenerated`; what remains is recorded as `Task.UndeclaredEdits`
(`{path, lines_changed}`, from `CommittedLineStatsSince`, summed across
retries), an `undeclared_edit_detected` decision, an
`autonomous_undeclared_edit_detected` audit event and a warn alert through
`Manager.SetAlertSink` (wired to the alerts store in `cmd/datawatch/main.go`;
nil-safe). `off` is a full no-op, `warn` never blocks, `block` sets
`TaskBlocked` with `undeclared_edit: <files>` and halts the walk.
Worktree-mode PRDs are skipped. Read-only git only.

### LLM entry `verification_mode`

An LLM registry entry carries `verification_mode` (`classify` | `generate`, JSON/YAML key
`verification_mode`). Default: absent/empty, which resolves to `generate`, so existing setups are
unchanged. The value is validated on `Registry.Add`, `Registry.Update` and registry load; an unknown
value is rejected (a stored file with an unknown value fails to load with the entry name).

- Resolution: `autonomous.ResolveVerificationLLM` reports `Mode` (always `generate`);
  `ResolveVerificationLLMWithMode` reads the chosen entry's mode through a lookup and returns an
  error for an unknown value instead of coercing it. The mode belongs to the entry; it does not take
  part in the task/story/Automaton/global override chain, and the `Verification*` fields are unchanged.
- Live path: the daemon's verifier resolves through the registry lookup; an invalid mode produces a
  `setup` verdict (block, not a finding about the task).
- Surfaces: REST `POST|PUT /api/llms` (`verification_mode`), MCP `llm_add`/`llm_update`
  (`verification_mode`), CLI `datawatch llm add|update --verification-mode`, comm `llm add|update
  verification_mode=<v>`, PWA Settings > Compute > LLM edit (Verification mode select). Android and
  iPhone follow through the same REST field (capability parity; no client change in this core).

### Classifier capability probe (`classifier.*`)

`internal/classifier` reads a guardrail verdict as a classification (native `/v1/systemone` or
generic logprobs). The keys below are read from the `classifier:` block of the config file
(`classifier.ProbeConfigFrom`) and are settable through `PUT /api/config`, `datawatch config set`,
comm `configure` and the web Settings → Classifier section.

| Key | Default | Meaning |
|---|---|---|
| `classifier.probe_ttl_seconds` | `600` | Whole seconds. How long a supported or unsupported probe result is cached per (node, model, path). Unknown is never cached. |
| `classifier.probe_timeout_seconds` | `5` | Whole seconds. Per-probe HTTP timeout. A timeout is state `unknown`. |

### Classifier client and calibration (`classifier.*`)

`classifier.Client.Classify` builds a closed-option prompt (options lettered A, B, C... in a fixed
set), calls the native endpoint or the generic logprobs path (`logprobs: true`, `top_logprobs: N`,
`max_tokens: 1`, `chat_template_kwargs.enable_thinking: false` for matching models), and returns
`{Chosen, Probabilities, Route, Mode}`. Token variants (`A`, ` A`, `▁a`, `(A`, `A.`) are summed per
option on the generic path. Missing scores, timeouts, transport errors and abstains return a typed
`*InfraError` (category `infra`); there is no fallback to generate-and-parse. Keys are read from the
`classifier:` block of the config file on every call (`classifier.ClientConfigFrom` /
`classifier.CalibrationConfigFrom`), so a change applies without a restart. Each is settable through
`PUT /api/config` (and shown, with effective defaults, by `GET /api/config`), `datawatch config set
classifier.<key>=<value>`, comm `configure classifier.<key>=<value>` and the web Settings → Classifier
section (five locales). A zero reads as the default; an invalid value rejects the whole PUT.

| Key | Default | Meaning |
|---|---|---|
| `classifier.timeout_seconds` | `30` | Whole seconds. Per-call timeout. A timeout is an `infra` error. |
| `classifier.top_logprobs` | `10` | `top_logprobs` on the generic path. |
| `classifier.abstain_margin` | `0.10` | The winner must lead the runner-up by this much, else abstain. |
| `classifier.min_probability` | `0.50` | The winner must reach this probability, else abstain. |
| `classifier.min_option_mass` | `0.50` | Generic path: option tokens must hold this share of first-token mass, else scores are missing. |
| `classifier.rotate_options` | `false` | Rotate option order and average scores on the generic path. Never applies to the native path. |
| `classifier.thinking_off_models` | `["qwen3"]` | Model-name substrings whose template gets thinking disabled. |
| `classifier.calibration.test_fraction` | `0.30` | Held-out share, split deterministically by a hash of seed and sample id. |
| `classifier.calibration.split_seed` | `classifier-calibration` | Salt for the split. |
| `classifier.calibration.min_fit_samples` | `20` | Least fit-set size a threshold may be fitted on. |
| `classifier.calibration.false_pass_weight` | `5` | Cost of one false pass in false-block units when fitting. |
| `classifier.calibration.rotation_bias_threshold` | `0.10` | Position-mass deviation from uniform at which `MeasurePositionBias` recommends rotation. |
| `classifier.calibration.rotation_flip_threshold` | `0.10` | Share of items whose winner flips across rotations at which rotation is recommended. |

A threshold is fitted on the fit set only and scored on the test set only. The stored record carries a
fingerprint of model, quantization, node, prompt hash and chat-template hash; `CheckFresh` /
`FileStore.Require` return a `*StaleError` (category `infra`) when no record exists or the fingerprint
changed, forcing re-calibration. Below-threshold probabilities are a block and are never turned back
into a pass.

Classify calls share the `node:<name>` inference-load pool. See
[classifier-api-shape.md](plans/reports/classifier-api-shape.md).

### Classify-mode task verdicts (`autonomous.verification_record_scores`)

When the resolved verification LLM entry has `verification_mode: classify`, the verifier closure in
`cmd/datawatch/main.go` skips the generate-and-parse path and calls `autonomous.ClassifyVerify`
(`internal/autonomous/classify_verify.go`). It makes two classifier reads over the same verifier prompt
body: `ok` (A = the diff implements the spec, B = it does not) and `severity` (five labels: info, low,
medium, high, critical). Generate mode is unchanged and does not call it.

Verdict rules, all fail closed:

- Missing scores (no probability for the pass option), an empty or unknown `Chosen` (abstain), a nil result, timeout, transport error, abstain, capability, gate or stale calibration is a block with
  category `infra`. A missing runtime is a block with category `setup`. There is no fallback to generate mode.
- The `ok` winner B is a content block (empty category) whatever the severity read says.
- The `ok` winner A must reach the calibrated threshold of the model's calibration record; below it the
  block has category `low-confidence`.
- A pass needs a usable severity read (otherwise `infra`); a pass whose severity read is `high` or
  `critical` is a block with category `inconsistent-verdict`.
- A block is never downgraded (`NeverDowngrade`).
- The summary and issues come from an optional separate generative call (`ClassifyRuntime.Explain`). It
  never affects `ok`, severity or category; a failed or garbled answer keeps the verdict and uses a fixed summary.

`VerificationResult` records `verifier_mode: classify` on every classify-mode verification. Generate mode is
byte-for-byte unchanged, so it adds no field: an absent `verifier_mode` means generate (the byte-for-byte
rule is read as overriding record-on-every-verification for generate results). In classify mode it also records `scores` (ok and severity probability vectors, pass probability, threshold, calibration
fingerprint) and `route` (node, model, path) so the verdict is reproducible. The full record is returned
by default; `autonomous.verification_record_scores: false` keeps only `verifier_mode`. The daemon supplies
the classifier client, calibration store and explanation call through `classifyRuntimeProvider`
(`cmd/datawatch/classify_verifier.go`); until it is installed a classify-mode verification blocks with `setup`.

| Key | Default | Meaning |
|---|---|---|
| `autonomous.verification_record_scores` | `true` | Keep the score vector and route on the verification result. Set through the `PUT /api/config` key `autonomous.verification_record_scores` and shown in the autonomous section of `GET /api/config`, like its sibling `autonomous.*` keys. |

### Classifier operator surfaces and the publisher allowlist (`classifier.publisher_allowlist`)

The classification settings are reachable on every surface (see [parity-status.md](parity-status.md)):

- `GET /api/llm/classifier` returns each LLM's resolved verification mode, the cached capability probe
  results (`classifier.Prober.Snapshot`: node, model, path, state, expiry; unknown is never cached so it
  never appears), the calibration records under `<data_dir>/classifier` (`classifier.FileStore.List`:
  fingerprint, threshold, test metrics, age), `calibration_error` when the store cannot be read, and the
  publisher allowlist. `wired` is false until the daemon installs the status source
  (`Server.SetClassifierStatus`, `cmd/datawatch/classifier_status.go`).
- `GET /api/llm/classifier/verdicts?automaton=<id>[&task=<id>][&low_confidence=true]` returns
  `autonomous.ClassifyRecords`: one record per verification and guardrail verdict with `mode`, `scores`,
  `route`, `outcome`, `category` and `low_confidence` (a block with category `low-confidence`). A
  low-confidence block is decided with `resolve_task_review`; the chat verb `llm classifier review`
  checks that the block exists before it sends the decision.
- The MCP surface is one tool, `llm_classifier`, because no existing LLM or Automaton tool fits these reads.

| Key | Default | Meaning |
|---|---|---|
| `classifier.publisher_allowlist` | empty | Model publishers a classify-mode judge model may come from. Empty trusts nothing. Read with `GET /api/llm/classifier/publishers`. Added only by `POST /api/llm/classifier/publishers` (and the MCP, CLI and chat forms of it), which refuse without `confirm=true`, save the config file, and write a `classifier_publisher_add` audit entry (publisher, actor). It is deliberately not settable through `PUT /api/config`, so the confirmation cannot be bypassed. Names are trimmed and lower-cased; a duplicate is a no-op. |

### Scope-breadth classifier (`autonomous.scope_breadth_enabled`)

On a content-category task-verification failure, before the retry decision, the executor asks the classifier
the closed question "A) one coherent change, B) bundles multiple separable units"
(`internal/autonomous/scope_breadth.go`). It is a separate call site, not a guardrail, and does not depend on
the verdict wiring. The answer is written to the additive `FailureReason.scope_too_broad` field; no existing
category changes. The read has its own calibration set: its fingerprint hashes the scope question
(`ScopeBreadthFingerprint`), so a verifier calibration is never reused. `scope_too_broad` is true only when B
wins and A is below the calibrated threshold.

It never blocks. Missing runtime, stale calibration, missing scores, abstain, timeout or transport error leave
`scope_too_broad` false, set `FailureReason.scope_breadth_note`, and append a `scope_breadth_infra` decision to
the PRD. The consumer (auto-split) is not part of this feature.

| Key | Default | Meaning |
|---|---|---|
| `autonomous.scope_breadth_enabled` | `false` | Run the scope-breadth read. Also needs the classifier capability. Set through `PUT /api/config`; shown in the autonomous section of `GET /api/config`. |

### Peer usage cache

The shared-node gate (`fingerprint.PeerGate`) reads each opted-in peer's held counts from `fingerprint.PeerUsageCache`, an in-memory map of the last good `/api/capacity` answer per peer. `wireFingerprint` builds one cache, sets it as `PeerGate.Usage`, and sets `Syncer.AfterPass` to `RefreshAll`, so the cache is refreshed once at start and after every 15-minute sync pass. The ledger callback only reads memory; a missing or stale entry fails open and triggers a background refresh. Peers without fingerprint sync opted in get no HTTP call, and no config key is involved.

### Rules and context settings (`autonomous.rules_context`)

Defined in `internal/config/autonomous_rules_context.go`; validated by `Validate` and written by `internal/config/rules_context_update.go`. Resolution order for each setting: the project's entry in `projects`, then the global value, then the legacy `autonomous.rules_file` / `autonomous.context_file` keys, then the built-in default (`AGENT.md` / `CONTEXT.md`).

| Key | Type and default | Notes |
|---|---|---|
| `autonomous.rules_context.rules_file` | string, unset = legacy key or `AGENT.md` | Relative to the project directory; absolute names and `..` are refused |
| `autonomous.rules_context.context_files` | list of strings, unset = legacy key or `CONTEXT.md` | At most 8 names; same file-name rules; an empty entry is refused |
| `autonomous.rules_context.digest_mode` | `off`, `no_tools` or `all`, unset = `no_tools` | Case-insensitive; `no_tools` adds the digest only for a backend without file tools |
| `autonomous.rules_context.digest_max_bytes` | integer, unset or 0 = 4096 | An explicit value must be 512 to 65536 |
| `autonomous.rules_context.projects` | list, default empty | One override per `project_dir`; each of the four settings above may be set and an empty one inherits |
| `autonomous.rules_context.projects[].project_dir` | string, required | The project directory the override applies to |
| `autonomous.rules_context.projects[].rules_file` | string, inherits when empty | As `rules_file` |
| `autonomous.rules_context.projects[].context_files` | list, inherits when empty | As `context_files` |
| `autonomous.rules_context.projects[].digest_mode` | string, inherits when empty | As `digest_mode` |
| `autonomous.rules_context.projects[].digest_max_bytes` | integer, inherits when 0 | As `digest_max_bytes` |

A change applies to the next Automaton that is decomposed, with no restart. Surfaces: `GET`/`PUT /api/autonomous/rules-context`, `datawatch autonomous rules-context`, the MCP tools `rules_context_status` and `rules_context_set`, the chat verbs `autonomous rules`, and Settings, General, *Rules and context*.

### Worker session profile settings (`autonomous.worker_profiles`)

Defined in `internal/config/config.go` (`AutonomousConfig.WorkerProfiles`, `WorkerProfileDefaults`, `WorkerProfile`); the profile type is `internal/workerprofile.WorkerProfile`, whose yaml and json keys are snake case. `ValidateWorkerProfileConfig` checks the three settings and runs from `validateLoaded`, so both `Load` and `LoadSecure` refuse a bad config. Every write surface that changes these settings must call it before it persists them. `internal/workerprofile` does not import `internal/config`.

| Key | Type and default | Notes |
|---|---|---|
| `autonomous.worker_profiles` | map of profile name to profile, default empty | Each value uses the `WorkerProfile` fields (`name`, `mcp`, `tools`, `skills`, `context`, `model`, `effort`). A name set on the value must match its key. The name must not equal a built-in name (`minimal`, `digest`, `full`, `datawatch-tools`). Each profile must pass `workerprofile.Validate` |
| `autonomous.worker_profile_defaults.role` | map of role to profile name, default empty | Keys: `planner`, `worker`, `verifier`, `judge`. Values name a built-in or user profile. An empty value means no default |
| `autonomous.worker_profile_defaults.kind` | map of task kind to profile name, default empty | Keys: `implementation`, `docs`, `note`, `final_gate`. Values as for `role` |
| `autonomous.worker_profile` | string, default empty | Global-layer profile name. Must name a built-in or user profile when set. Empty = the role default |

Refused at load time: a user profile named like a built-in; a profile that fails `workerprofile.Validate`; a name on a profile that does not match its key; a defaults value or the global name that names no known profile; an unknown role or kind key.

**Launch behaviour.** A worker, verifier or judge launch resolves its profile by name, in this order: task, Automaton, project, `autonomous.worker_profile`, the kind default, the role default. With nothing configured at any tier, no profile is applied and the session starts with the full harness. **Wiring status:** the worker launch (`applyWorkerProfileToRun` in `internal/autonomous/executor.go`) applies the resolved profile. The verifier and judge launches resolve it through `ResolveSessionProfile` (`internal/autonomous/worker_profile_launch.go`), called by `withSessionProfile` in `cmd/datawatch/session_judgment.go` on the verifier (`main.go`) and judge (`main.go`) paths, and send it with the one-shot session's start request, which applies it on the start. The field-level outcomes for those sessions were not re-checked in this docs pass. Once a role or kind default is configured, a configured role default applies to kinds with no configured kind entry. A profile that cannot be applied at launch falls back to the full harness, and the attempt records the fallback with its reason. An unknown name set on a task or an Automaton is recorded as a decision on the task record and in the Automaton's decision list. The profile in force for each role and task kind, and the tier that set it, reads back on `GET /api/config` (`autonomous.in_force`), MCP `worker_profile_status`, `datawatch config get`, the comm channel `configure` keys and the web settings group. The operator guide is [howto/worker-session-profiles.md](howto/worker-session-profiles.md).

### Pre-spawn scope lint mode (`autonomous.scope_lint`)

`autonomous.scope_lint` (`config.AutonomousConfig.ScopeLint`, JSON
`scope_lint`, default `enforce`) selects what the pre-spawn scope lint does
with a hit: `off` skips the lint, `warn` records the problems and spawns the
task, `enforce` blocks it. `ScopeLintMode` normalises the value, and any empty
or unknown value resolves to `enforce`, so an existing configuration keeps
blocking. `ValidateScopeLint` rejects unknown values on the write surfaces.
The executor reads the resolved mode (`Manager.scopeLintMode`) in
`executeOne`, before the task spawns. Plan-time `ScopeWarnings` are skipped
when the mode is `off` (`planScopeWarnings`). Placeholder and quoted pattern
text is never flagged in any mode. A failed Automaton accepts a spec edit
(`EditTaskSpec`) only for a task whose failure is a scope-lint rejection
(`isScopeLintFailure`). Surfaces: YAML `autonomous.scope_lint`, REST
`PUT /api/config` and `GET /api/config`, MCP `autonomous_config_get` /
`autonomous_config_set`, comm `configure`, CLI `autonomous config-set`, and the PWA
Settings → Automate → Task scope lint select. See `docs/operations.md`.

### Planning restart policy (`autonomous.planning_restart_policy`)

What a daemon restart does to an Automaton still in planning. `reattach` (default, and
what an unset or unrecognised value reads as) keeps planning running across the restart;
`restart` restarts planning once and records a decision; `draft` resets the Automaton to
draft, as before the setting existed. Parsing lives in `internal/config/planning_restart_policy.go`
(`NormalizePlanningRestartPolicy`, `ValidPlanningRestartPolicy`). The empty string reads as
reattach on load but is reported invalid, so write paths can refuse it. A `PUT /api/config`
with an unknown value is refused with a 400 naming `reattach`, `restart` and `draft`; a
config file holding an unknown value logs a `[warn]` line at startup and reads as `reattach`.
The field sits beside `autonomous.restart_policy`; `docs/config-reference.yaml` documents it.

### Per-task project directory (Automata)

`autonomous.Task.ProjectDir` (JSON `project_dir`) overrides the
Automaton's `PRD.ProjectDir` for one task. `ResolveTaskProjectDir(prd,
task)` returns the task value when it is set and the PRD value when it
is empty, so the precedence is task → Automaton and an empty value
inherits. Every per-task check calls the resolver: diff capture, scope
(task-scoped write dirs), file drift, quality gates and the verifier.
`Manager.SetTaskProjectDir` validates the path (it must be absolute, must
not be a filesystem root or system directory, and must be inside the
Automaton's write dirs when those are set) and applies the same lock rule as
`SetTaskLLM`. An empty dir clears the override.

Surfaces: REST `POST /api/autonomous/prds/{id}/set_task_project_dir`
(`{task_id, project_dir}`), MCP `autonomous_prd_set_task_project_dir`,
CLI `datawatch autonomous prd-set-task-project-dir <prd> <task> [dir]`,
comm verb `autonomous set-task-project-dir`, and the PWA task edit
modal's **Project dir override** field (shown on the task card as a
`dir:` badge).

### Per-tree write lock (Automata)

The per-tree write lock (BL428 Phase 2,
docs/plans/2026-10-14-concurrency-and-budget-design.md §3a) makes writes to a
single project tree exclusive. It fixes the failure mode where two or more
Automata ran workers in the same working tree concurrently — interleaved
diffs and release-version drift — the shape of the 2026-10-13 incident
that three concurrent PRDs in one tree produced. Two cooperating layers
protect a tree:

**Layer 1 — PRD admission gate (BL469, `internal/autonomous/tree_admission.go`).**
`admitTree` runs under the manager lock before a PRD is marked `running`. It
refuses to start a PRD whose canonical tree is already held by a PRD in
`running` or `active` status, with a message naming the holder
(`tree busy: PRD %s is already running on this tree (%s); run one PRD per
direct tree or use worktree mode`). Ancestor (parent/child) PRDs never
conflict: a spawned Automaton running on its parent's tree is exempt, since
the parent is waiting on it, not competing with it. The walk is bound to 32
levels to defend against a corrupt parent chain. No project dir yet =
nothing to contend on, so it's a no-op.

**Layer 2 — task writer lock (BL428 §3a, `internal/autonomous/tree_lock.go`).**
This is the finer-grained mechanism. `executeOne` (executor.go) acquires the
task's canonical-tree lock before spawning the worker and releases it after
verification finishes. The lock is held across the full critical section
(spawn → run → verify → retry loop), covering every exit path including
cancellation and daemon shutdown. The `holdTree` closure in `executeOne`
is idempotent per task: re-calling `acquireTreeWritersLock` with the same
holder is a no-op (the record already names this task).

Key resolution — `ResolveTreeKey`:

- **Git repo (no explicit branch)**: canonical base = `git rev-parse
  --git-common-dir` resolved to an absolute path. All worktrees of the same
  repo map to the same key, so a linked worktree serializes against the
  main checkout.
- **Worktree-mode Automaton** (`prd.Git.Branch != ""`): key = the worktree's
  own path (no canonicalization). Each worktree is its own lock domain, so
  worktree-mode PRDs run in parallel with each other.
- **Non-git path**: key = the project directory itself (path-based key, no
  git).

Lock file: `<canonical-tree>/.datawatch/tree.lock` (a `tree.lock` file
inside a `.datawatch/` directory at the tree root). datawatch's own
`.gitignore` covers `.datawatch/`, and the regression test
(`TestTreeLockFileDoesNotDirtyGitStatus`) pins the invariant that holding
the lock must never show up in `git status --porcelain` (a project that
adopts the lock gitignores `.datawatch/` for the same reason). The lock is
advisory by design: only datawatch's own task writers consult it; a human
editor or another tool bypassing it is outside the lock's scope.

Lock record (`LockRecord`): `holder_task_id`, `holder_prd_id`,
`acquired_at` (unix seconds), `pid`. This lets the PWA, CLI and operator
tell who holds the lock and since when.

Stale lock reclamation:
- A lock whose holder task no longer exists in the store → stale, reclaimed
  (up to `maxStaleReclaimAttempts = 3` consecutive reclaim attempts).
- A lock whose holder task is in a terminal status → stale, reclaimed.
- A lock whose record is missing/unreadable but the file is younger than
  `staleLockGrace = 30s` → not reclaimed (likely a live acquirer two steps
  into writing the record); after the grace window, reclaimed.
- A live holder's record → never reclaimed (`TestTreeLock_LiveHolderWaits_NotReclaimed`).

Wait behavior: if the lock is held by another live task, `AcquireTreeLock`
polls every `defaultTreeLockPoll = 5s` (production passes the
Manager-controlled interval) until the lock is released or the
context timeout (`Manager.capacityWait()`, default
`capacity_wait_timeout_seconds = 14400s` = 4 h) fires. While waiting, the
task's wait reason is visible in the PWA as:
`tree lock: <canonical-tree> held by task <id> (PRD <id>) since <time>`.
On timeout the acquire returns an error and the task is refused — the PRD
returns to an approvable state (this is the known PRD-level auto-wait gap;
the per-task wait+auto-resume in the lock itself works as documented
above and is exercised by `TestExecutorTreeLock_WaitReason`).

The `resolveRefuseDirtyTree` companion gate:
`autonomous.refuse_dirty_tree_start` (default `false`, per-PRD override via
`PRD.RefuseDirtyTreeStart *bool`) is checked at admission, before the tree
lock is acquired. When enabled, `dirtyTreeRefused` calls
`git status --porcelain` on the canonical tree; non-empty output → the task
is refused with a message naming the dirty-file count and the options
(commit or stash first, `git clean`, or the config key back to `false`).
Git not available or not a repo → the gate passes the tree through silently
(the gate only reasons about dirt it can observe).

Config: `autonomous.project_write_serialization` — `advisory` (default) or
`parallel` (recognized, reserved; daemon still enforces the advisory lock
and logs a startup warning). Any other value is a config error and prevents
daemon start.

CI-watch own-commit pin (BL423, `internal/autonomous/ci_watch.go`):
the ci-watch completion guardrail now judges the last completed task's
recorded post-verify SHA (`Task.PostVerifySHA`, selected by
`prdLastPostVerifySHA` — iteration order over the PRD's stories/tasks, so
the last one in that order wins) rather than the tree's live `HEAD`.
This prevents a later write on the shared tree from dragging
someone else's CI run into a PRD's verdict. If no
task in the PRD recorded a `PostVerifySHA` (legacy store), ci-watch falls
back to live `HEAD` (the pre-fix behavior). If the pinned SHA is not
reachable from the remote (`git merge-base --is-ancestor <sha> <remote-tracking-ref/>` fails), ci-watch skips with a logged reason —
this is the expected behavior for worktree/auto-PR Automata that push
after completion.

Tests: `tree_lock_test.go` (20 tests covering key resolution, lock path,
exclusivity, stale reclamation, release guards, and git-status purity),
`executor_tree_lock_test.go` (9 tests: same-tree serialization, wait reason,
release-on-spawn-failure, release-after-verify, dirty-tree refused,
clean-tree admitted, gate-off, missing-tree, worktree parallel),
`same_tree_serialization_test.go` (2 regression tests for the
2026-10-13 shape: three-PRD serialization and release-bump no-clobber),
and the live smoke `tests/live-smoke/tree-serialization-smoke.sh` (2026-10-18,
v9.5.4; same-tree serialized PASS, cross-tree parallel PASS, loser
auto-resume GAP documented in `tests/live-smoke/REPORT.md`).

### Automatic commit staging guard

Datawatch makes two kinds of automatic commit: the pre-session and post-session commits made by `stageChanges` in `internal/session/git.go`, and the attempt snapshots made by `SnapshotAttemptWork` in `internal/autonomous/attempt_snapshot.go`. Both stage through the `internal/gitguard` package, which holds the staging policy (`gitguard.Default`) and the checks that run around `git add`.

The guard enforces these rules:

1. **Named files only.** An automatic commit never stages more than the session's named files or its declared write scope. Files outside that set stay unstaged, even when they are dirty in the working tree.

2. **Size cap.** A candidate file larger than 50 MB is skipped with a warning. The skip is recorded with reason `size`.

3. **Ignored directories and model weights.** A candidate is skipped (reason `ignore-dir`) when one of its directory components, taken relative to the repository root, is one of `.cache`, `.ollama`, `.gradle`, `.android`, `go`, `snap`, `node_modules`, `model`, `models`, `build`, `dist`, `out`, or `target`. Relative matching means a repository that sits under a directory such as `~/go` is not rejected wholesale. A candidate is also skipped (reason `pattern`) when its base name matches a model-weight pattern: `*.gguf`, `*.safetensors`, `*.pt`, `*.pth`, `*.bin`, `*.ckpt`, `*.onnx`, or `*.tflite`.

4. **Staged-file limit.** A commit whose candidate file count exceeds `MaxStagedFiles` (default 10000) is refused. The refusal is an operator-facing error whose text begins `auto-commit refused:` and states the file count and the limit. A refused commit stages nothing, and the guard never falls back to committing a whole directory tree. The operator fix is described in [operations.md](operations.md#auto-commit-refused).

5. **Bounded staging.** `git add` runs under a timeout (`AddTimeout`, default 5 minutes). On a failed or interrupted add, the guard removes interrupted pack debris from `.git/objects/pack`: `tmp_pack_*` files, and `*.pack` files that have neither a sibling `*.idx` nor a sibling `*.rev`. No other file in that directory is touched.

6. **Home-directory detection.** When the repository directory is the user's home directory or lies inside it, the guard logs a prominent warning. The check resolves symbolic links before comparing paths.

7. **Only the harness commits, and only after validation.** A worker session cannot commit. A `git commit` run by a worker fails with a message saying the harness commits after validation. The harness commits once the session's validation has passed; when validation fails, nothing is committed. The commit refusal comes from hooks that live in a harness-owned temporary directory outside the worktree, so a worker has nothing in its tree to edit or bypass. The hooks are removed when the session ends and again when the daemon adopts sessions on startup (stale hook directories left by a dead daemon are reaped, live ones are kept).

8. **Non-project directories.** Automatic commits are refused when the directory is the home directory, a filesystem root, or a directory with no repository and no project marker. The refusal writes one audit line and is not retried. The operator fix is described in [operations.md](operations.md#automatic-commit-refused-in-a-non-project-directory).

On the session commit path (pre-session snapshot and completion commit), the candidates are additionally filtered to the task's declared write scope (skipped with reason `outside-plan`) and checked against the `autonomous.snapshot_never_stage` patterns (reason `never-stage`) and the repository's ignore rules (reason `ignored`); each refusal is logged; the staging pass is recorded on the task result as `skipped_snapshot_paths` by the normal completion commit, the stall-recovery commit and the attempt snapshot path. The operator view of the scope, the setting and the skip record is [howto/autonomous.md](howto/autonomous.md).

### Message Routing

The router (`internal/router`) is stateless. It:

1. Receives an `IncomingMessage` (or `messaging.Message`) from the Signal backend
2. Filters by group ID — messages from other groups are ignored
3. Filters out messages from the daemon's own account (echo suppression)
4. Calls `Parse(text)` to produce a `Command`
5. Dispatches to the appropriate session manager method
6. Formats a reply and calls `backend.Send(groupID, reply)`

The router never holds state. Session state lives exclusively in the session store.

### DNS Channel Backend

The DNS channel (`internal/messaging/backends/dns/`) implements the `messaging.Backend` interface
using DNS TXT queries for covert command/response communication.

**Package structure:**
- `protocol.go` — `EncodeQuery`/`DecodeQuery` (HMAC-SHA256), `EncodeResponse`/`DecodeResponse` (fragmented TXT)
- `nonce.go` — `NonceStore` (bounded LRU, 10K entries, 5-minute TTL)
- `server.go` — `ServerBackend` implementing `messaging.Backend` (miekg/dns UDP+TCP)
- `client.go` — `Client.Execute()` for CLI DNS queries

**Config struct:** `DNSChannelConfig` in `internal/config/config.go`
- Fields: `enabled`, `mode`, `domain`, `listen`, `upstream`, `secret`, `ttl`, `max_response_size`, `poll_interval`

**Router exception:** DNS messages bypass the group ID filter since DNS has no concept of groups.
The router checks `msg.Backend == "dns"` and processes commands regardless of group ID.

---

## 3. Adding a New LLM Backend

To add support for a new AI coding assistant (e.g. `aider`):

### Step 1: Create the backend package

```
internal/llm/aider/backend.go
```

### Step 2: Implement `llm.Backend`

```go
package aider

import (
    "context"
    "fmt"
    "os/exec"

    "github.com/dmz006/datawatch/internal/llm"
)

// Backend implements llm.Backend for aider.
type Backend struct {
    bin string // path to aider binary
}

// New creates an aider backend.
func New(bin string) *Backend {
    if bin == "" {
        bin = "aider"
    }
    return &Backend{bin: bin}
}

// Name returns the backend name.
func (b *Backend) Name() string { return "aider" }

// SupportsInteractiveInput returns true — aider accepts yes/no on stdin.
func (b *Backend) SupportsInteractiveInput() bool { return true }

// Launch starts aider for the given task inside the pre-created tmux session.
// Output is captured to logFile via tmux pipe-pane (already set up by the session manager).
func (b *Backend) Launch(ctx context.Context, task, tmuxSession, logFile string) error {
    // Build the aider command: run non-interactively with an initial message
    aiderCmd := fmt.Sprintf("%s --yes --message %q", b.bin, task)

    // Send the command to the existing tmux session
    cmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", tmuxSession, aiderCmd, "Enter")
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("send aider command to tmux: %w", err)
    }
    return nil
}

// Ensure Backend implements the interface at compile time.
var _ llm.Backend = (*Backend)(nil)
```

### Step 3: Register in `internal/llm/registry.go`

```go
import "github.com/dmz006/datawatch/internal/llm/aider"

func init() {
    Register(aider.New(""))
}
```

### Step 4: Add a config option

In `internal/config/config.go`, add to `SessionConfig`:

```go
// LLMBackend selects the LLM backend. One of: "claude-code", "aider".
// Defaults to "claude-code".
LLMBackend string `yaml:"llm_backend"`
```

### Step 5: Wire into the session manager

In the session manager's `Start()` method, look up the backend by name:

```go
backend, ok := llm.Get(cfg.Session.LLMBackend)
if !ok {
    backend, _ = llm.Get("claude-code") // fallback
}
if err := backend.Launch(ctx, task, tmuxSession, logFile); err != nil {
    return nil, fmt.Errorf("launch backend: %w", err)
}
```

Users then configure the backend in `config.yaml`:

```yaml
session:
  llm_backend: aider
```

---

## 4. Adding a New Messaging Backend

To add Slack as a control channel:

### Step 1: Create the backend package

```
internal/messaging/slack/backend.go
```

### Step 2: Implement `messaging.Backend`

```go
package slack

import (
    "context"
    "github.com/dmz006/datawatch/internal/messaging"
)

type Backend struct {
    botToken  string
    channelID string
    client    *slackAPIClient
}

func New(botToken, channelID string) *Backend {
    return &Backend{botToken: botToken, channelID: channelID}
}

func (b *Backend) Name() string { return "slack" }
func (b *Backend) SelfID() string { return b.client.BotUserID() }

func (b *Backend) Send(recipient, message string) error {
    return b.client.PostMessage(recipient, message)
}

func (b *Backend) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
    // Use Slack Events API Socket Mode for no-public-URL deployment
    return b.client.RunSocketMode(ctx, func(event SlackEvent) {
        if event.Type != "message" || event.BotID != "" {
            return // ignore bot messages
        }
        handler(messaging.Message{
            GroupID:    event.Channel,
            Sender:     event.User,
            SenderName: event.UserName,
            Text:       event.Text,
            Backend:    b.Name(),
        })
    })
}

// Link is a no-op for Slack — bots authenticate with a bot token, no QR linking needed.
func (b *Backend) Link(deviceName string, onQR func(string)) error { return nil }

func (b *Backend) Close() error { return b.client.Close() }

var _ messaging.Backend = (*Backend)(nil)
```

### Step 3: Add a config section

In `internal/config/config.go`:

```go
type SlackConfig struct {
    BotToken  string `yaml:"bot_token"`
    ChannelID string `yaml:"channel_id"`
}

type Config struct {
    // ...existing fields...
    Slack SlackConfig `yaml:"slack"`
}
```

### Step 4: Wire into main.go

```go
if cfg.Slack.BotToken != "" {
    slackBackend := slack.New(cfg.Slack.BotToken, cfg.Slack.ChannelID)
    messaging.Register(slackBackend)
    go slackBackend.Subscribe(ctx, router.HandleMessage)
}
```

### Step 5: Configure

```yaml
slack:
  bot_token: xoxb-...
  channel_id: C01234567
```

---

## 5. signal-cli JSON-RPC Protocol

`signal-cli` in `jsonRpc` mode reads newline-delimited JSON-RPC 2.0 requests from stdin and writes responses and notifications to stdout.

### Subscribe to incoming messages

Request:
```json
{"jsonrpc":"2.0","method":"subscribeReceive","id":1}
```

Response:
```json
{"jsonrpc":"2.0","result":{},"id":1}
```

After subscribing, incoming messages arrive as notifications (no `id` field):
```json
{
  "jsonrpc": "2.0",
  "method": "receive",
  "params": {
    "envelope": {
      "source": "+12125551234",
      "sourceName": "Alice",
      "dataMessage": {
        "message": "new: write tests",
        "groupInfo": {
          "groupId": "base64groupid=="
        }
      }
    }
  }
}
```

### Send a message to a group

Request:
```json
{
  "jsonrpc": "2.0",
  "method": "send",
  "params": {
    "groupId": "base64groupid==",
    "message": "[myhost][a3f2] Started session for: write tests"
  },
  "id": 2
}
```

Response:
```json
{"jsonrpc":"2.0","result":{"timestamp":1711234567890},"id":2}
```

### List joined groups

Request:
```json
{"jsonrpc":"2.0","method":"listGroups","id":3}
```

Response:
```json
{
  "jsonrpc": "2.0",
  "result": [
    {"id": "base64groupid==", "name": "AI Control"},
    {"id": "anothergroup==", "name": "Family"}
  ],
  "id": 3
}
```

### Link a new device

Device linking is not a JSON-RPC call — it uses a separate `signal-cli link` subprocess invocation:

```bash
signal-cli --config ~/.local/share/signal-cli link -n myhost
```

stdout/stderr: `sgnl://linkdevice?uuid=...&pub_key=...`

The Go code captures this URI, converts it to a QR code, and displays it (in terminal or via SSE to the PWA).

---

## 6. WebSocket Protocol

All messages use a common envelope:

```json
{
  "type": "<message_type>",
  "data": { ... },
  "ts": "2026-03-25T14:32:01.123Z"
}
```

### Server → Client Messages

| Type | When Sent | `data` Payload |
|---|---|---|
| `sessions` | On connect; on any session state change | `{"sessions": [<Session>, ...]}` |
| `session_state` | When one session's state changes | `{"session": <Session>}` |
| `output` | New output lines from a monitored session | `{"session_id": "a3f2", "lines": ["line1", "line2"]}` |
| `needs_input` | Session enters `waiting_input` | `{"session_id": "a3f2", "prompt": "Do you want to continue? [y/N]"}` |
| `notification` | General informational message | `{"message": "Session a3f2 killed."}` |
| `error` | Command or protocol error | `{"message": "Session not found: zz99"}` |
| `hello_ack` | Reply to `hello` | `{"session_list": "delta", "session_view": "summary", "output": "subscribed", "capabilities": ["ws.hello", ...]}` — the modes now in effect |
| `session_removed` | A session left the list (`session_list=delta` clients only) | `{"session_id": "myhost-a3f2"}` |
| `alert` | A new system alert | `<Alert>` — `{"id": "...", "level": "warn", "title": "...", "body": "...", "read": false, ...}` |
| `alerts_read` | Alerts were marked read on any client | `{"ids": ["..."], "unread_count": 2}` or `{"all": true, "unread_count": 0}` |

With `session_view=summary`, `session_state` carries the compact
`GET /api/sessions?view=summary` record instead of the full session.
With `output=subscribed`, `output`, `raw_output`, `pane_capture` and
`chat_message` arrive only for subscribed sessions. See "WebSocket
session updates" and "WebSocket output scoping" below.

**Example — sessions message on connect:**
```json
{
  "type": "sessions",
  "data": {
    "sessions": [
      {
        "id": "a3f2",
        "full_id": "myhost-a3f2",
        "task": "write tests for auth module",
        "tmux_session": "cs-myhost-a3f2",
        "state": "running",
        "created_at": "2026-03-25T14:00:00Z",
        "updated_at": "2026-03-25T14:01:30Z",
        "hostname": "myhost"
      }
    ]
  },
  "ts": "2026-03-25T14:32:01.000Z"
}
```

**Example — needs_input event:**
```json
{
  "type": "needs_input",
  "data": {
    "session_id": "a3f2",
    "prompt": "Do you want to overwrite auth_test.go? [y/N] "
  },
  "ts": "2026-03-25T14:35:22.000Z"
}
```

### Client → Server Messages

| Type | When Sent | `data` Payload |
|---|---|---|
| `command` | Raw command string (same syntax as Signal) | `{"text": "list"}` |
| `new_session` | Start a new session | `{"task": "build a REST API"}` |
| `send_input` | Send input to a waiting session | `{"session_id": "a3f2", "text": "y"}` |
| `subscribe` | Subscribe to output lines for a session (full or short ID) | `{"session_id": "a3f2"}` |
| `unsubscribe` | Stop output for a session and stop the screen capture `subscribe` started | `{"session_id": "a3f2"}` |
| `hello` | Negotiate lighter streams; every key optional | `{"session_list": "delta", "session_view": "summary", "output": "subscribed"}` |
| `ping` | Keepalive (server replies with pong) | `{}` |

**Example — subscribe to session output:**
```json
{
  "type": "subscribe",
  "data": {"session_id": "a3f2"},
  "ts": "2026-03-25T14:32:05.000Z"
}
```

After subscribing, the server sends `output` messages for that session as new lines appear.

---

## 7. Output Monitoring Algorithm

The `monitorOutput` goroutine implements the following algorithm:

```
1. Open log file at sess.LogFile
   - If file does not exist, wait with exponential backoff (up to 5s) then retry
2. Seek to EOF (skip historical output on resume after daemon restart)
3. Create an idle timer: time.NewTimer(cfg.Session.InputIdleTimeout * time.Second)
4. Enter read loop:
   a. Attempt to read the next line from the file
   b. If a new line is available:
      - Append to in-memory line buffer (capped at 100 lines)
      - Reset the idle timer
      - If current state is waiting_input: transition back to running
   c. If no new line (EOF):
      - Wait for the next fsnotify write event (interrupt-driven, no polling)
   d. If idle timer fires (no new output in InputIdleTimeout seconds):
      - Check if tmux session exists: `tmux has-session -t <name>`
      - If session is gone:
          * If last state was running/waiting_input: mark complete or failed
          * Call onStateChange(sess, newState)
          * Return (goroutine exits)
      - If session is alive and last line matches prompt pattern:
          * Mark waiting_input
          * Set sess.LastPrompt = last buffered line
          * Call onNeedsInput(sess, lastPrompt)
          * Call onStateChange(sess, waiting_input)
      - Reset idle timer and continue loop
5. On context cancellation: return (daemon shutting down)
```

**Prompt patterns** (heuristic — last line ends with one of):
- `? ` — generic question
- `[y/N] ` or `[Y/n] ` — yes/no prompt
- `> ` — interactive continuation prompt
- `: ` — input request
- `(Y/n)` — alternate format

These patterns are checked case-insensitively on the last non-empty line in the buffer.

---

## 8. Configuration Reference

All fields in `~/.datawatch/config.yaml`:

| Field | Type | Default | Description |
|---|---|---|---|
| `hostname` | string | OS hostname | Identifies this machine in Signal messages and session IDs |
| `pin_check_seconds` | int | `300` | How often the pin keeper re-verifies that each compute node with a `node_pinned_model` is still serving that model. `0` turns the check off. The value is written without `omitempty`, so an explicit `0` survives a save/load round trip. |
| `compute_nodes[].node_pinned_model` | string | `""` | Keeps the node on one model. Requests with no explicit model run on the pin. An explicit request for the pinned model runs on it. An explicit request for a different model is refused unless `allow_model_swap` is true. Datawatch never swaps the pinned model implicitly. Resolved by `internal/inference/pinned_model.go`. |
| `compute_nodes[].allow_model_swap` | bool | `false` | Lets an explicit request replace the node's pinned model. Ignored when the node has no `node_pinned_model`. |
| `compute_placement.missing_model` | enum | `ask` | What to do when the routed node does not have the requested model: `ask` (default) records a one-shot placement ask and waits for the operator to approve (pull + load) or deny, expiring after `ask_timeout_minutes` (a refusal, not an approval); `auto_load` pulls and loads without asking; `refuse` never loads — the refusal is audited (`placement_refused`). Read at decision time; a change from any surface applies at the next placement decision. |
| `compute_placement.mem_headroom_pct` | int (0–100) | `10` | Percent of free memory kept out of reach when a placement candidate is considered: the model must fit, and this much free memory must be left over. |
| `compute_placement.disk_headroom_gb` | int ≥ 0 | `5` | GB of free disk kept out of reach when a placement candidate is considered: the model must fit, and this much free disk must be left over. |
| `compute_placement.ask_timeout_minutes` | int ≥ 0 | `30` | How long a placement ask may wait for an answer before it expires (a refusal); `0` = no timeout. Unanswered, the ask is recorded as `decided_by: timeout`. |
| `compute_placement.assume_model_size_gb` | int ≥ 0 | `0` | Size in GB assumed for a model the node carries but the daemon is not told the size of; `0` (default) = unknown. Affects the placement free-space estimate only. |
| `compute_nodes[].tls_insecure_skip_verify` | bool | `false` | Turns off TLS certificate verification for this node: the daemon's node usage probes (loaded models via `/api/ps`, GPU stats from `monitoring_endpoint`) and the supervisor watch probe. Off by default, so certificates are verified. Last resort only; prefer `tls_ca_file` or `tls_cert_fingerprint`. When on, it is reported in the node record, the PWA and the audit log. Cannot be combined with `tls_ca_file` or `tls_cert_fingerprint`: node add and update reject the combination, and a seeded node that sets both has its TLS probes fail instead of falling back to skipping verification. |
| `compute_nodes[].tls_ca_file` | string | `""` | Path to a PEM CA bundle trusted in addition to the system roots when verifying this node's certificate. Use it when the node serves a certificate from a private CA. May be combined with `tls_cert_fingerprint`; cannot be combined with `tls_insecure_skip_verify`. |
| `compute_nodes[].tls_cert_fingerprint` | string | `""` | SHA-256 fingerprint of this node's leaf certificate, pinned instead of chain verification. Use it for a single self-signed certificate. Accepts 64 hex characters, case-insensitive, colons optional. May be combined with `tls_ca_file`; cannot be combined with `tls_insecure_skip_verify`. |
| `compute_nodes[].verify_tls` | bool | `false` | When `true`, model pulls to this node (the compute-node model pull, the marketplace task pull and the model loader's pull) verify the node's `https://` certificate against the system roots, and a failed check fails the pull with an error that names the node and the certificate problem. Off by default, so pulls do not check the certificate. Governs model pulls only; it is separate from `tls_ca_file`, `tls_cert_fingerprint` and `tls_insecure_skip_verify`, which govern the usage and supervisor watch probes, and a pull does not use the CA file or pin. Node update replaces the whole node, so leaving it out turns it off. |
| `compute_nodes[].scheduling_priority` | int | `50` (when `0` or unset) | Node priority, 0 to 100. When an LLM entry's pool lists two or more nodes, the ledger places a worker on the least-loaded healthy node that has a free slot (pool used count over its limit); priority only breaks a tie between equally loaded nodes, and a remaining tie keeps the order of the entry's list (`internal/capacity/ledger.go`). Set through REST, MCP `compute_node_add`/`compute_node_update`, the CLI `compute node add|update` and the PWA node form. |
| `llms[].compute_nodes` | list of names | empty | LLM registry entry, not a `config.yaml` key. The list of compute nodes a local-model worker, verifier and judge may run on. Each listed node that is known and serves the model is a candidate, and the request goes to the least loaded candidate (`scheduling_priority` only breaks a tie, then list order); a node that does not serve the model is skipped, and one engine per node still applies. With one node it behaves as before. With two or more candidates it is the pool the ledger picks from, and each worker's model is loaded on the chosen node before it starts; a failed load alerts, releases the lease and tries another node of the list. Set with `--compute-nodes` on `datawatch llm add|update`, REST `PUT /api/llms/{name}` and MCP `llm_add`/`llm_update`. |
| `compute_nodes[].session_slots` | int | `0` | Operator's worker count for a node that hosts a local engine. `0` derives it from observer data (`compute.NodeSlots`, `internal/compute/slots.go`): one slot per GPU limited by free GPU memory against the served model, or free system memory divided by model size with no GPU data, at least one slot. A positive value wins over the derived count. `max_concurrent_sessions` still caps the result. A negative value is rejected when the config loads (`validateLoaded`, `internal/config/config.go`) and when the node is saved (`Node.Validate`). The same field is on the node record in `internal/compute/node.go`, saved by the registry, and set through REST, MCP, the CLI, the comm channel and the PWA node form. |
| `compute_nodes[].verifier_slots` | int, nullable | unset (`null`) | Operator's verifier worker count for a node. Unset resolves to 1 on a node that hosts a local engine and 0 on one that does not; an explicit 0 takes no verifier work at all. Resolved by `compute.EffectiveVerifierSlots` (`internal/compute/verifier_slots.go`) and applied by `syncVerifierPools` (`cmd/datawatch/capacity_wiring.go`) as the limit of the node's own verifier pool (pool name `node-verify:<name>`, `capacity.VerifierPool`, `internal/capacity/verifier_pool.go`). A verifier leases it when the limit is above 0; with it at 0 a verifier leases the executor pool `node:<name>` as before, and the executor cap is never raised. A negative value is rejected at config load (`validateLoaded`, `internal/config/config.go`) and when the node is saved (`Node.Validate`). Same field on the node record in `internal/compute/node.go`, set through REST, MCP, the CLI, the comm channel and the PWA node form. |
| `compute_nodes[].verifier_min_free_mem_mb` | int, nullable | unset (`null`) → 1024 | Free GPU memory in MB a new verifier slot wants free before it starts. Unset uses the default of 1024; an explicit 0 turns the headroom check off. Resolved by `compute.EffectiveVerifierMinFreeMB` and enforced by the ledger gate (`VerifierHeadroom`, `internal/capacity/ledger.go`) on the node's verifier pool; `syncVerifierPools` records the node's last headroom verdict in the admission cache (`VerifierNodeStatus`, `cmd/datawatch/capacity_wiring.go`), which the REST node detail and the per-pool state read back. A node the observer cannot reach is recorded as no data and the gate fails open on it. Negative values rejected the same way. |
| `compute_nodes[].verifier_context_mb` | int | `0` | Context memory in MB one verifier slot holds on the node, used only for the advice figure: with the node's free GPU data the suggestion is free MB divided by this value whole slots (`compute.SuggestVerifierSlots`, reported as `suggested_verifier_slots`). 0 means unknown, so no figure. Advice only: nothing applies it automatically and it never raises the executor cap. Set through the same five surfaces. |
| `data_dir` | string | `~/.datawatch` | Root directory for sessions.json, logs/, and config |
| `signal.account_number` | string | (required) | Signal phone number in E.164 format, e.g. `+12125551234` |
| `signal.group_id` | string | (required) | Signal group ID in base64 format (from `signal-cli listGroups`) |
| `signal.config_dir` | string | `~/.local/share/signal-cli` | signal-cli data directory containing account keys |
| `signal.device_name` | string | hostname | Name shown in Signal's Linked Devices list |
| `session.file_service_root` | string | `""` | Root directory for the federated file service and PWA image uploads. Per-peer files land in `<root>/peers/<name>/`; per-discussion files in `<root>/discussions/<id>/`. Empty = falls back to `session.root_path`, then the user home directory |
| `session.max_sessions` | int | `10` | Maximum number of concurrent claude-code sessions |
| `session.input_idle_timeout` | int | `10` | Seconds of idle output before declaring a session is waiting for input |
| `session.liveness_window_seconds` | int | `120` (when `0` or unset) | liveness: seconds the liveness rule watches a print-mode backend (a family name ending in `-prompt`, `IsPromptBackend` in `internal/session/backend_family.go`) before considering it stalled. `0` means the built-in default 120; callers resolve through `Config.LivenessWindowSecondsEffective()` (`internal/config/backend_liveness.go`) rather than reading the raw field |
| `session.liveness_idle_timeout_overrides` | map[string]int | `{}` (global applies) | liveness: per-backend input-idle timeout overrides in seconds, keyed by backend family name (`pi` matches `pi-prompt`; full backend names also match). Empty map = the global `session.input_idle_timeout` applies, with the rule that the print-mode default is off (`0` — headless one-run adapters never wait for input) while interactive backends keep the global default of `10`. An explicit `0` value is a valid "off by choice" override. The single reader is `Config.LivenessInputIdleTimeoutEffective(backend)`; callers never read the raw map |
| `session.tail_lines` | int | `20` | Default number of lines returned by `tail` and `status` commands |
| `session.claude_code_bin` | string | `claude` | Path to the claude-code binary (can be absolute or a `PATH`-relative name) |
| `session.task_delivery_window_seconds` | int | `60` | How long one OneShot task-delivery attempt waits for the Claude main prompt to appear (the session must start its channel and tool servers before the prompt shows) before trying again |
| `session.task_delivery_attempts` | int | `3` | How many delivery attempts are made before the task fails as undelivered; between attempts a 5s then 10s backoff pause is waited, so the total wait is attempts × window + 15s (about 195s with the defaults). A session whose process has already exited still fails at once |
| `session.store_flush_interval` | int | `1000` | Milliseconds the session store batches changes before the background writer saves them; `0` saves on every change. Applies live on any config write (`PUT /api/config`, web UI, `configure`, `datawatch config set`, MCP `config_set`) |
| `session.store_archive_after_days` | int | `14` | Days after its last update that a finished session moves to the archive file; `0` disables archiving. Applies live |
| `session.store_field_cap_kb` | int | `256` | Largest session text field kept inline in the session store, in KB; longer text is cut with a marker and the full text goes to a side file. Applies to later writes |
| `session.store_side_file_cap_kb` | int | `64` | Automaton (PRD) text longer than this many KB is stored in a side file next to its record. Existing records are rewritten with the new value when their Automaton next changes |
| `session.dismiss_ui_prompts` | bool | `true` (when unset) | Whether dismissing UI prompts is on. An explicit `false` turns it off and survives a round trip (pointer-typed, like `identity.backups_to_keep`). Read live on every use |
| `session.ui_prompt_dismiss_delay_seconds` | int | `5` (when unset) | Whole seconds to wait before sending the UI prompt dismissal. `0` = send at once (only explicit, never the default) |
| `session.ui_prompt_clear_bound_seconds` | int | `30` (when unset) | Whole seconds ≥ 1; the UI prompt clear bound. A wrong type, a fraction, a zero or a negative value is rejected (nothing applied) |
| `session.ui_prompt_max_attempts` | int | `2` (when unset) | UI prompt dismissal attempts cap, whole number ≥ 1. A wrong type, a fraction, zero or a negative value is rejected (nothing applied) |

None of these four values is hard-coded: `cmd/datawatch/main.go` hands `session.Manager.SetUIPromptSettings` a closure that re-reads the config on every call, so any surface's write (`PUT /api/config`, comm `configure`, CLI `config set`, MCP `config_set`) takes effect immediately with no daemon restart.| `server.enabled` | bool | `true` | Whether to start the HTTP/WebSocket server |
| `server.host` | string | `0.0.0.0` | Bind address. Use `0.0.0.0` for all interfaces (including Tailscale) |
| `server.port` | int | `8080` | HTTP/WebSocket listen port |
| `server.token` | string | `""` | Optional bearer token for PWA authentication. Empty = no auth |
| `server.tls_cert` | string | `""` | Path to TLS certificate PEM file. Leave empty for plain HTTP |
| `server.tls_key` | string | `""` | Path to TLS key PEM file. Leave empty for plain HTTP |
| `server.tls_enabled` | bool | `false` | Enable TLS. Use with `tls_auto_generate=true` or explicit cert/key paths |
| `server.tls_auto_generate` | bool | `false` | Auto-generate a self-signed certificate at `~/.datawatch/tls/server/` on start |
| `DATAWATCH_CA_CERT` (channel bridge env) | string | `~/.datawatch/tls/server/cert.pem` | Environment variable read by `datawatch-channel` (`cmd/datawatch-channel/main.go`, `buildTLSConfig`). PEM file of the daemon's own TLS certificate, added to the trusted roots so the loopback HTTPS hop is verified. The daemon sets it for every channel it launches when `server.tls_enabled` is true (`server.tls_cert`, else `<data_dir>/tls/server/cert.pem`) |
| `DATAWATCH_CHANNEL_INSECURE_SKIP_VERIFY` (channel bridge env) | bool | `false` | Operator opt-in for `datawatch-channel`: skip TLS verification of the daemon (loopback only). Logs a warning at startup when on. Leave off unless the bridge cannot read the daemon certificate |
| `server.session_summary_terminal_limit` | int | `50` | Maximum number of terminal (complete / failed / killed) sessions returned by `GET /api/sessions?view=summary`, newest by `updated_at` first. Non-terminal sessions are never dropped. `0` = no cap. Only applies to `view=summary`; see "Session list views" below |
| `server.session_summary_text_chars` | int | `200` | Characters (runes) kept of `task`, `prompt_context`, `last_response` and `last_summary_long` in `GET /api/sessions?view=summary`. Minimum `20` |
| `server.ws_session_list_default` | string | `full` | WebSocket session-list mode for clients that never send `hello`: `full` (a `sessions` frame with the whole list on every change) or `delta` (only `session_state` / `session_removed` frames; no connect-time list — fetch it with `GET /api/sessions?view=summary`). Read live on every broadcast. See "WebSocket session updates" below |
| `server.ws_output_scope_default` | string | `all` | WebSocket terminal-output mode for clients that do not send `output` in `hello`: `all` (every session's `output` / `raw_output` / `pane_capture` / `chat_message` frames) or `subscribed` (only sessions the client sent `subscribe` for). Read live for every frame. See "WebSocket output scoping" below |
| `server.allowed_project_roots` | list of paths | `[]` | Extra folders a request-supplied project directory may lie under. The allowed roots are the PRD worktree base (`autonomous.worktree_dir`), then the workspace root (`session.workspace_root` when set, otherwise the default workspace folder `~/workspace`), plus the folders in this list; the first two are always allowed. A directory outside every root is refused at session start, Automaton creation, the quality-gate test runner and the git guard. `..` and symlinks (including dangling links) are resolved first. Absolute paths only; a filesystem root is refused. See operations.md "Allowed project roots" |
| `server.api_gzip` | bool | `true` | Gzip-compress `/api/*` responses for clients that send `Accept-Encoding: gzip`. See "API response compression" below |
| `server.api_gzip_min_bytes` | int | `1024` | A `/api/*` response is compressed only once its body exceeds this many bytes; smaller bodies are sent unchanged. Minimum `0` |
| `servers[].name` | string | — | Short identifier for a remote datawatch server (used with `--server` flag) |
| `servers[].url` | string | — | Base URL of the remote server (e.g. `http://203.0.113.10:8080`) |
| `servers[].token` | string | — | Bearer token for the remote server |
| `servers[].enabled` | bool | `true` | Whether this remote server is active |
| `mcp.public_url` | string | `""` | Public base URL the MCP SSE server advertises to clients in its `endpoint` event (for example `https://host.tailnet.example.net:8081` — an example value; the real URL lives in the operator config and is not committed). Set it when `mcp.sse_host` is bound to localhost behind a TLS-terminating reverse proxy. Trailing slash is ignored. Empty advertises `<scheme>://<first sse_host>:<sse_port>`. Restart required. |
| `mcp.max_retries` | int | `3` | Number of automatic retries when a per-session MCP channel server fails to start or loses connection |
| `mcp.allow_self_config` | bool | `true` (when unset) | The self-config alias. The operator-facing name for the builtin **self-config** federation capability group (`internal/federation/capabilities.go`): on, the default capability grant for a spawned session includes `config:write`, `autonomous:write`, `observers:write`, `llms:write`, `usage:write` (`SessionDefaultGrantCaps`, unless an explicit `session.capabilities` list overrides it); off, every config-write tool (`config_set`, `autonomous_config_set`, the `*_config_set` section tools) refuses with "self-modify is disabled" while config reads stay available. `mcp.allow_self_config` itself: `config_set` refuses it bootstrap-protected (`internal/mcp/memory_tools.go`); on other session surfaces only a credential holding `config:write` can reach it. Change it in YAML / CLI / PWA / `PUT /api/config` + restart. |
| `mcp.allow_sensitive_writes` | bool | `true` (when unset) | Switch for the **sensitive write** MCP tool class (`federation.IsSensitiveWriteTool`, `internal/federation/mcp_tool_caps.go`): secret set/delete/get, the identity writers, `restart_daemon`, `reload`, `plugin_install`, `plugin_run_subcommand`, `send_input`, `start_session`, `tailscale_acl_push`, `tailscale_auth_key`, `acme_renew`. Absent or `true` = allowed. An explicit `false` makes the tool-handler gate (`internal/mcp/sensitive_write_gate.go`) refuse these tools for session and peer callers; operator calls are unaffected. Tri-state `*bool` read through `MCPConfig.SensitiveWritesEnabled()`; no migration adds the key. YAML-only today — `applyConfigPatch` does not handle it. Calls are audited with `class: "sensitive"` (argument names only, no values). |
| `mcp.self_config_audit_path` | string | `""` → `<data dir>/audit/config-self-modify.jsonl` | JSONL self-config audit log. One record per config-write attempt by a session or peer credential on any surface — `mcp` (config-write tools, `internal/mcp/config_write_audit.go`), `rest` (`internal/server/config_audit.go`, `PUT /api/config`), `comm` (router `configure`, `internal/router/router.go`) — allowed and (unless `self_config_audit_denied` is `false`) denied, with surface, tool/route, keys, redacted values, caller (`session:<id>` / `peer:<name>`), allowed flag, refusal reason. Written by `federation.ConfigAuditor` (`internal/federation/config_audit.go`): non-blocking, also echoed to stderr. Read via `GET /api/audit/self-config` (limit 1–1000, default 100), CLI `datawatch audit self-config`, MCP `self_config_audit` (read-only, never capability-gated even when the group is off). |
| `mcp.self_config_audit_denied` | bool | `true` (when unset) | Whether refused self-config attempts are recorded (in addition to allowed ones). `false` = allowed writes only. Pointer-typed so an explicit `false` survives a round trip (`internal/config/config.go`). |
| `goose.enabled` | bool | `false` | Enable the goose/goose-prompt LLM backend |
| `goose.binary` | string | `goose` | Path to the goose binary (absolute or PATH-relative) |
| `goose.console_cols` | int | `0` | Terminal width for goose sessions (0 = session default) |
| `goose.console_rows` | int | `0` | Terminal height for goose sessions (0 = session default) |
| `goose.provider` | string | `""` | Provider injected as `GOOSE_PROVIDER` env var (e.g. `anthropic`, `openai`, `google`) |
| `goose.model` | string | `""` | Model injected as `GOOSE_MODEL` env var (e.g. `claude-sonnet-5`) |
| `goose.api_key_ref` | string | `""` | API key (literal or `${secret:name}`) injected as the provider-specific key env var; resolved at startup |
| `goose.channel_enabled` | bool | `false` | When true, inject `GOOSE_MCP__DATAWATCH__*` env vars so Goose connects back as an MCP client (stdio transport) |
| `autonomous.verifier_diff_max_bytes` | int | `0` | Byte cap on the git diff injected into the verifier prompt for evidence grounding. `0` = 8192. Raise for large changesets; lower to reduce token cost. |
| `autonomous.verifier_min_free_mem_mb` | int (pointer) | unset → `1024` | Free GPU MB a node must keep, with the extra verifier slots running, before a verifier cap above 1 is admitted and before a node's `verifier_slots` above 1 is saved. Unset reads 1024 (`compute.EffectiveVerifierMinFreeMB`, the only default); explicit `0` turns the check off; negative is a config error. Read by `AutonomousConfig.EffectiveVerifierMinFreeMB` in both gates. |
| `autonomous.verifier_headroom_max_age_hours` | int | `0` → `24` | How old, in hours, a headroom measurement may be and still count as fresh for both gates. `0` or unset reads 24 (`AutonomousConfig.EffectiveVerifierHeadroomMaxAge`); negative is a config error. |
| `autonomous.verification_timeout_seconds` | int | `1800` | Declared in `internal/config/config.go` (`VerificationTimeoutSeconds`). Bounds one verifier or per-task guardrail judgment session; consumed by the verifier and guardrail closures in `cmd/datawatch/main.go` through the `verificationSessionTimeout` resolver. `0` or negative = 1800-second (30m) fallback. Exposed in the PWA Settings autonomous card. |
| `autonomous.judge_policy` | list | `[]` | Ordered LLM backends the judges (PRD guardrail, plan review and other autonomous judges) try, one attempt per backend, so the attempts total the list length. Empty = the judges follow the verifier routing policy (prefer-free, fixed, balance and the presets choose among its backends). A unit with its own task or PRD guardrail backend override keeps that one as the only entry, still chosen through the selector. A judge moves to the next backend when its run hits the time limit (a warning each time) or when no capacity slot is granted within the wait (no warning; the wait is not counted against the limit); once a backend has timed out and no backend is left, the PRD is held in `needs_review` with a `judge-timeout` reason and an error alert lists every attempt (a backend that had no slot is listed as one that did not run). When no attempt timed out, the judge never ran and returns an infra verdict instead of a hold. A judge cancelled while it waits for a slot stops at once. The plan-review judge (run when `autonomous.plan_review_mode` is `warn` or `block`) takes the same path: it runs as a guardrail-style judge invocation, so its backend comes from the selector (a backend set in `autonomous.plan_review_backend` is its only entry), it takes a capacity lease and runs under the judge time limit. When no backend finishes, the plan-review verdict is recorded as held (`held`, with one finding per attempt): the Automaton stays in `needs_review`, lane fill does not start it and `block` mode refuses approval until the review is re-run. Declared in `internal/config/config.go` (`JudgePolicy`); normalized by `config.NormalizeJudgePolicy`. Surfaces: `GET/PUT /api/config` (`autonomous.judge_policy`), `GET/PUT /api/autonomous/config`, MCP `autonomous_config_set` (`judge_policy`), comm `autonomous config-set judge_policy=a,b`, CLI `autonomous config-set --judge-policy`, the PWA Settings autonomous card. |
| `autonomous.judge_timeout_seconds` | int | `1800` | Time limit for one judge run (30 minutes by default), counted from the judge's start, not from any wait for a capacity slot. Declared in `internal/config/config.go` (`JudgeTimeoutSeconds`); normalized by `config.NormalizeJudgeTimeoutSeconds`, so `0` or negative = `1800`. Same surfaces as `autonomous.judge_policy` (CLI `--judge-timeout-seconds`). |
| `autonomous.judge_size_threshold_lines` | int | `400` | Changed-line count above which the judges try `claude-code` first; at or below it, and when it is `0`, the judge backends keep their configured order. Lines are added plus deleted on the Automaton branch against its merge base, lock files left out, committed work only, counted by `JudgeDiffLines` in `internal/autonomous/judge_diff_size.go`. The reorder is `judgeSizeRoute` in `cmd/datawatch/judge_size_route.go`: `claude-code` moves to the front when it is in the chain and the rest keep their order, still one attempt per backend; in `balance` mode the load decides and the order only breaks ties. A unit with its own task or PRD guardrail backend override is not reordered, and a failed count keeps the default order. The Automaton decision log names the rule and the line count; a judge that times out on every backend still holds the Automaton. Declared in `internal/config/config.go` (`JudgeSizeThresholdLines`, default `JudgeSizeThresholdDefaultLines`); a negative value is refused. Same surfaces as `autonomous.judge_policy` (CLI `--judge-size-threshold-lines`, comm `judge-policy-set judge_size_threshold_lines=n`, PWA Settings > Automata > Autonomous Automata planning > Judge size threshold). |
| `autonomous.judge_strong_backend` | string | `""` | Strong judge for size-aware routing: the backend a diff at or above `autonomous.judge_size_threshold_lines` changed lines is routed to first. Empty (the default) resolves to the last backend in `autonomous.judge_policy` (the strong Claude judge under a "local then Claude" chain); a value that is not in `judge_policy` leaves the judge backend order unchanged. Declared in `internal/config/config.go` (`JudgeStrongBackend`), resolved per judge chain by `internal/config/judge_policy.go` (`ResolveJudgeStrongBackend`). Accepts any LLM backend name; the key is not validated against a backend list (a backend name is not required to be one). Same surfaces as `autonomous.judge_policy` (REST `judge_strong_backend` on `GET/PUT /api/autonomous/config`; MCP `autonomous_config_set` `judge_strong_backend`; CLI `autonomous config-set --judge-strong-backend`; comm `autonomous judge-policy-set judge_strong_backend=…` / `configure autonomous.judge_strong_backend=…`; YAML `autonomous.judge_strong_backend`; PWA Settings > Automata > Autonomous Automata planning > Strong judge backend (large-diff reroute) text field). The review-state surface (`timed_out` → `action: reroute`) is documented in `docs/howto/judge-policy.md` → The review state. |
| `autonomous.judge_verdict_cache_enabled` | bool | unset reads `true` | Per-commit judge verdict cache, on by default. A completed pass, or a block whose category is a real finding (content, or the empty pre-category value), is stored keyed on the triple **(commit sha, judge policy, rules)** — rules = SHA-256 over the guardrail name and the exact inputs the judge saw (its prompt, the unit's title and spec, and the judge model) — and replayed on that triple instead of running the judge again (`internal/autonomous/judge_verdict_cache.go`: `NewJudgeVerdictCache`, `Lookup`, `Store`, `policyKey`, `JudgeVerdictRulesHash`; a pinned backend, the judge chain `judge:a|b`, and an empty chain are different keys). A verdict whose review timed out (the `judge-timeout` category), or one that crashed, or returned unparseable output — `infra`, `invalid-response` and `judge-timeout` categories, or a hard error — is never stored and never replayed as a verdict, so a later infra retry still runs for real; a judged unit with no recorded commit sha bypasses the cache in both directions. Cache file `judge_verdict_cache.json` in the PRD store directory, written atomically; it survives daemon restarts. Declared in `internal/config/config.go` (`JudgeVerdictCacheEnabled`, `*bool`); normalized by `config.NormalizeJudgeVerdictCacheEnabled` (nil → `true`), so an explicit `false` is the only way off, and it re-judges every guardrailed commit. Same surfaces as `autonomous.judge_policy` (REST `judge_verdict_cache_enabled`; MCP `autonomous_config_set` `judge_verdict_cache_enabled`; CLI `autonomous config-set --judge-verdict-cache=false`; comm `autonomous judge-policy-set judge_verdict_cache_enabled=false` / `configure autonomous.judge_verdict_cache_enabled=false`; YAML `autonomous.judge_verdict_cache_enabled`; PWA Settings > Automata > Autonomous Automata planning > Judge verdict cache toggle). How-to: `docs/howto/judge-policy.md` → The per-commit verdict cache. |
| `autonomous.guardrail_model` | string | `""` | **v9.10.18** — pinned model for the guardrail JUDGE (rules-review, ci-watch, per-task / per-story guardrails, rejudge). Tier 3 of the 5-tier precedence chain implemented in `internal/autonomous/guardrail_model.go` (`ResolveGuardrailModel`): tier 1 PRD `guardrail_model`, tier 2 task `guardrail_model`, tier 3 this field, tier 4 `autonomous.verification_model`, tier 5 `autonomous.planning_model` (tiers 3–5 apply once the resolved judge backend is pi-family). First tier that yields a model the resolved backend can serve (`validateModelForBackend`) wins. If every tier fails (empty or unpairable), the guardrail record gets a loud, non-retried infra-category block (sentinel `ErrNoGuardrailModel`, verdict via `noGuardrailModelVerdict`) — a pi-family judge NEVER silently runs on the backend's built-in default model. The `set_prd_guardrail_llm` / `set_task_guardrail_llm` setters validate the pair (plus full resolution when a model is pinned) before mutating. |
| `autonomous.default_quality_gates.enabled` | bool | `false` | Master toggle for per-PRD test regression gates. When true, the executor captures a test baseline before the first task and re-runs after each task completes. |
| `autonomous.default_quality_gates.test_command` | string | `""` | ONE shell string run through `sh -c` in the PRD's `project_dir` (`pipeline.RunTests`), so compound commands (`a && b`) work. The baseline is validated at set time and at Automaton start; an unusable one (could not start, timed out, non-zero exit naming no test, no tests found) marks the PRD `quality_gate_status: "invalid"` and the gate then only warns. |
| `autonomous.default_quality_gates.timeout` | int | `0` | Per-run timeout in seconds for the test command. `0` = no cap. |
| `autonomous.default_quality_gates.block_on_regression` | bool | `false` | When true, a task that introduces a test regression is marked failed and triggers an auto-fix retry cycle. |
| `autonomous.default_quality_gates.scope` | string | `""` → `full` | Gate scope: `full` \| `touched-plus-smoke` \| `touched` — how much of the test suite a gate task runs (`internal/pipeline/gate_scope.go`: `ParseGateScope`, `PlanGateScope`). Empty inherits the PRD's `quality_gates.scope`, then this key, then the built-in `full` (`Manager.resolveGateScopeSettings`); the final gate task (last non-terminal task or `TaskKindFinalGate`) always runs `full` regardless. A `go test <flags> ./...` command is rewritten to the touched packages (+ one-level importers, + the smoke set for `touched-plus-smoke`); anything unrecomputable or non-rewritable falls back to the full command and records why in `scope_plan`. Module files (`go.mod`, `go.sum`, `go.work`, `go.work.sum`) and changed files outside any Go package force the full run; `go vet` runs over the same package set and new vet findings count as regressions. The baseline and current runs are both restricted to the same package set, so failures in run-omitted packages are not reportable as regressions; an invalid run stays warn-only. Invalid scope values are rejected on every surface (REST `400`, MCP/comm error reply, config load). The default stays `full` in the datawatch source tree until the smoke list in `docs/plans/2026-10-23-quality-gate-scope.md` is confirmed. Surfaces: REST `GET/PUT /api/config` + `POST /api/autonomous/prds/{id}/set_quality_gates` (PRD `quality_gates.scope`), MCP `autonomous_config_set` `quality_gates_scope` / `autonomous_prd_set_quality_gates` `scope`, CLI `autonomous config-set '{"default_quality_gates":{"scope":...}}'`, comm `configure autonomous.default_quality_gates.scope=…` / `autonomous set-quality-gates <id> scope=…`, PWA Settings selector (locale keys `settings_gate_scope`, `prd_task_gate_scope`). Operator guide: `docs/operations.md` → "Gate scope"; how-to: `docs/howto/quality-gate-scope.md`. |
| `autonomous.default_quality_gates.smoke_packages` | []string | built-in list | Smoke set for the `touched-plus-smoke` scope: Go package patterns that always run on a scoped gate. Empty list falls back to the PRD's `quality_gates.smoke_packages`, then this key, then `config.DefaultGateSmokePackages` (aliased as `pipeline.DefaultGateSmokePackages`): `./internal/autonomous/...`, `./internal/config`, `./internal/pipeline`, `./internal/server/...`, `./internal/mcp/...`, `./internal/observer/...`, `./internal/audit`, `./internal/metrics` (`internal/config/config.go`). REST `smoke_packages` accepts a list or a comma-separated string; MCP `quality_gates_smoke_packages` (config) / `smoke_packages` (per-PRD) likewise; comm takes a comma list. The proposal to confirm first is `docs/plans/2026-10-23-quality-gate-scope.md`. |
| `autonomous.gate_disagreement` | string | `"warn"` | Policy when a task's verifier passed but the quality gate confirmed a regression from a valid baseline (`config.AutonomousConfig.GateDisagreement`, read by `Manager.gateDisagreementPolicy`). `warn` records `task.gate_disagreement` plus a `gate_disagreement` decision and lets the task complete; `block` fails the task with a `gate_disagreement: …` error, with no auto-fix retry. Only `warn`/`block` are accepted (`400` on `PUT /api/config`, rejected on config load); an unrecognized stored value reads as `warn`. Never applies to an invalid gate. REST `autonomous.gate_disagreement`, MCP `autonomous_config_set` `gate_disagreement`, CLI `autonomous config-set`, comm `configure autonomous.gate_disagreement=block`, PWA Settings selector. |
| `autonomous.first_attempt` | map | `{}` (absent = today's choice) | Per task kind (`implementation`, `docs`, `note`, `final_gate`), the backend and model the first attempt runs on (`config.AutonomousConfig.FirstAttempt`, a `config.FirstAttemptChoice` with `backend` and `model`). An empty or absent entry keeps today's choice. A backend that needs a model (`pi`, `pi-prompt`, `ollama`) must name one. Checked by `config.ValidateAttemptCostSettings` on config load; an unknown kind or a missing model is refused with an error naming the key. |
| `autonomous.first_attempt_escalate_after_failures` | int | `1` | Failed attempts that move the next attempt to today's stronger choice (`config.AutonomousConfig.FirstAttemptEscalateAfterFailures`). The default `1` means the first failure escalates. Range `0`–`20`; a value outside it is refused by `config.ValidateAttemptCostSettings`, which names the key. |
| `autonomous.retry_resume_max_context_tokens` | int | `150000` | Largest prior-context size, in tokens, a retry may resume with (`config.AutonomousConfig.RetryResumeMaxContextTokens`). `0` means every retry starts a fresh session. Range `0`–`2000000`; a negative or larger value is refused by `config.ValidateAttemptCostSettings`, which names the key. The decision is the pure `decideRetryResume` in `internal/autonomous/retry_resume_decision.go` (checks run in order: session alive, backend support, enabled-by-setting, context limit; each outcome returns an operator-readable reason); `tryRetryResume` in `internal/autonomous/retry_resume_flow.go` wires it to the live session (liveness, context size, send-input hook) and, on resume, types the framed failure summary — newlines flattened, capped at 4,000 bytes — into the session and the attempt waits on that session; a missing send path or a failed send falls back to a fresh session. Only the `claude-code` backend resumes; only attempts that failed verification are candidates. The outcome and reason land on the attempt record as `resumed` / `resume_reason`. A pure-decision test lives in `internal/autonomous/attempt_resume_record_test.go`. |
| `autonomous.failure_escalation` | object | `{enabled: true, max_failures: 3, steps: []}` | Global failure escalation default (`config.AutonomousConfig.FailureEscalation`, read through `resolveFailureEscalation`). `enabled` turns the retry ladder on; `max_failures` (1 or more) is the failed-attempt count that holds a task for review; `steps` is the worker ladder (JSON array of `{backend, model, effort, attempts}`, or the compact text `backend:model[@attempts],...`, up to 8 steps). REST `GET /api/config` and `PUT /api/config` keys `autonomous.failure_escalation.enabled`, `.max_failures`, `.steps` (a bad value is a `400` naming the key, nothing applied); `PUT /api/autonomous/config` `failure_escalation`; comm `configure autonomous.failure_escalation.<key>=<value>`; CLI `datawatch autonomous failure-escalation global`; YAML `autonomous.failure_escalation`. Automaton and task overrides are set with `set_failure_escalation` (REST, MCP, CLI, comm). |
| `autonomous.auto_fix_retries` | int | `1` | Content auto-fix retries per Automaton (`0` turns content auto-fix off). Each retry is one corrective task created from the completion review's findings, and the retry budget is consumed only after that task exists. When the completion review still blocks and the retries are spent (or auto-fix is off), the Automaton is set to `needs_review` with the findings attached — not blocked, not running — so the operator or supervisor adds a corrective task with the normal add-task call and approves it. The total retry ceiling (`autonomous.retry_ceiling_total`) also applies. Set through YAML, `PUT /api/config`, MCP `autonomous_config_set` (`auto_fix_retries`), CLI `datawatch config set autonomous.auto_fix_retries <n>`, comm `configure autonomous.auto_fix_retries=<n>`; reads back under `autonomous.auto_fix_retries` on `GET /api/config` and in the autonomous config. |
| `autonomous.planning_routing` | object | `PlanningRoutingDefaults()` (off, no rules, built-in `default` ladder + `security`/`design` risk classes) | Planning-call routing block: per-rule backend/model selection with an ordered planner list (first entry = the planner, the rest = escalation targets) (`config.AutonomousConfig.PlanningRouting`, types + `ValidatePlanningRouting` in `internal/config/planning_routing.go`). `enabled` (default `false`) leaves the normally resolved planner untouched; `shadow` (default `false`) computes and records the routed decision but keeps the planner unchanged. `rules` is ordered, first match wins; each rule's `match` selects on `kind` / `tags` / `project` / `risk_class` (empty list = wildcard, a non-empty list matches if ANY entry matches), `spec_size_min`/`spec_size_max` (0 = unbounded) and a free-form `override`. `ladders` names budget ladders `touch_up_max` (default 1), `replan_max` (default 1), `escalate_after_attempts` (default 2); a rule's empty `ladder` means the built-in `default`. `classes` overrides/extends the built-ins `security` (tags `security`; globs `security/**`, `internal/server/auth*`) and `design` (tags `design`, `ui`). `ExampleRules()` ships examples routing design-tagged and security-tagged Automata to a stronger planner — examples only, never active by default. Validation (`ValidatePlanningRouting`) rejects duplicate rule names, rules with no planners, empty backend/model, unknown ladder references, `spec_size_min > spec_size_max`, negative ladder counts and unknown risk-class references. Declared in `internal/config/config.go` (`PlanningRouting`); not applied by `applyDefaults` — an existing config without the block loads unchanged, no version bump. Surface: YAML `autonomous.planning_routing` (plus `GET/PUT /api/config` via the generic config round-trip). |
| `autonomous.questions.enabled` | bool | `false` | Master switch for the operator-question channel (`docs/plans/2026-10-25-operator-decision-channel-proposal.md`), off by default. Read live on every request: while off all six `/api/questions` endpoints answer `503` naming the switch, and flipping it on from any surface takes effect on the next request without a daemon restart. Surfaces: YAML `autonomous.questions.enabled`; REST `GET`/`PUT /api/config` key `autonomous.questions.enabled`; MCP `config_set` / `get_config` (the generic config tools); CLI `datawatch config set autonomous.questions.enabled true|false`; comm `configure autonomous.questions.enabled=true`; PWA Settings → General toggle (`settings_automata_questions_enabled`). The channel itself — raise, list, detail, answer, withdraw, answer log — is REST for this step; MCP tools, CLI and comm per-question verbs are deferred with their reasons in `docs/api-mcp-mapping.md` → Operator questions and `docs/parity-status.md`. On raise the recorded question is announced (each route below is toggleable): once to every PWA/hub WebSocket as `question_raised`, once per active messaging router as a plain line (`[hostname] question <id> — <label>`, a `(high priority)` marker, the text, the numbered options, the REST + PWA answer routes), and — only for `priority: "high"` — one warning-level alert (`GET /api/alerts`). Every announce is async and non-blocking; a missing comm backend is a silent no-op. On answer and on withdrawal the daemon emits the lifecycle events `question.answered` and `question.withdrawn` (both in the closed `KnownEventKinds` set, so lifecycle subscriptions with `filter.kinds` may name them; exactly-once — the emit is marker-keyed on the answer id and `ReconcileEmittedEvents` re-fires anything recorded but never delivered across a restart; `questionEventDetailsMax` = 512 chars caps the details). The default return path for an asker is events-plus-polling: `GET /api/questions/{id}` answers `answer_count`, `current_answer_id` and the standing answer's summary (verbatim quote, actor, surface) in one read. The opt-in per-question `DATAWATCH_ANSWER:` session line (raise with `return_path: true` while the question names a `session_id`) is sent exactly once per answer through the asking session's comm channel, best-effort — a lost line is one daemon warning, never a retry. How-to: `docs/howto/operator-questions-notifications.md`. Operator guide: `docs/operations.md` → "Operator questions". |
| `autonomous.questions.notify_messaging` | bool | `true` (when absent) | Per-raise toggle for the messaging-group announce (one line per active comm channel): false suppresses just that route and the WebSocket + alert routes are unaffected. Nil (key absent) = on; the `EffectiveNotifyMessaging()` resolver in `internal/autonomous/questions.go` maps nil → true. Same surfaces as the master switch, including PWA Settings → General and comm `configure`. `internal/server/question_raise_dispatch.go` gates the per-router fan-out. |
| `autonomous.questions.notify_alert` | bool | `true` (when absent) | Per-raise toggle for the warning-level alert — which also fires only for `priority: "high"` raises. Nil = on (`EffectiveNotifyAlert()`); `internal/server/question_raise_dispatch.go` gates the alert insert. |
| `autonomous.questions.return_path` | bool | `true` (when absent) | Toggle for the opt-in `DATAWATCH_ANSWER:` return-path line that an answer writes into the asking session (only questions raised with `return_path: true` and a `session_id` are eligible at all — the key is a global on/off, not a per-question default). Nil = on (`EffectiveReturnPath()`); `internal/autonomous/question_return_path.go` builds the line (one physical line, one-line JSON with the question id, answer id, chosen option, the verbatim quote, actor and timestamp — exactly those six fields) and `internal/server/questions.go` wires it in via `SetQuestionAnswerReturnPath`. Off = no line for any question. |
| `autonomous.questions.worker_marker` | bool | `false` (when absent) | Whether a `DATAWATCH_QUESTION:` marker line in a worker session's output is acted on. Absent (nil) is off: `config.AutonomousQuestionsConfig.EffectiveWorkerMarker()` returns false unless the pointer is set and true. It does nothing while `autonomous.questions.enabled` is off; `cmd/datawatch/main.go` hands `session.Manager.SetWorkerQuestionGate` a closure that reads both switches from the live config at every event, so a write takes effect with no restart. A valid line is raised by `autonomous.RaiseWorkerQuestion` through `QuestionStore.RaiseUnlessOpenSourceLine` (deduped on session id plus the line's SHA-256) and announced by the same fan-out a REST raise runs (`HTTPServer.NotifyQuestionRaised`); a refused line is audited as `question_marker_refused` (session id, reason category, line hash; never the text). A question with `blocking: true` parks its task with the `await_decision: <question id>` wait reason and the coordinator tick (`Manager.QuestionTick`) resumes it from the answered and withdrawn questions it reads through `QuestionStore.List`. Surfaces: YAML `autonomous.questions.worker_marker`; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set autonomous.questions.worker_marker true\|false`; comm `configure autonomous.questions.worker_marker=true`; PWA Settings → Autonomous Automata planning (`settings_automata_questions_worker_marker`). How-to: `docs/howto/operator-questions-workers.md`. |
| `autonomous.merge_shadow.enabled` | bool | `true` (when absent) | Master switch for the shadow merge plan (record of what merging an Automaton branch would do, kept per project). Pointer field with no `omitempty`. Helper `EffectiveEnabled()`. Read from `config.MergeShadowConfig` (`internal/config/merge_shadow.go`). |
| `autonomous.merge_shadow.gate_timeout` | duration | `20m` | How long a merge gate may wait before giving up; Go duration string. Unparseable or non-positive values fall back to the default. Helper `EffectiveGateTimeout()`. Read from `config.MergeShadowConfig` (`internal/config/merge_shadow.go`). |
| `autonomous.merge_shadow.plan_keep` | int | `200` | Shadow merge plans kept per project; older plans are pruned past this count. Values below 1 fall back to the default. Helper `EffectivePlanKeep()`. Read from `config.MergeShadowConfig` (`internal/config/merge_shadow.go`). |
| `autonomous.decisions.enabled` | bool | `true` (when absent) | Master switch for decision records (structured records of one operator decision: title, situation, options with tradeoffs, recommendation, forced default). Pointer field with no `omitempty`, so an explicit `false` survives a round trip. Helper `EffectiveEnabled()`. Surfaces: YAML; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set`; comm `configure`. Read from `config.AutonomousDecisionsConfig` (`internal/config/config.go`) through its `Effective*` helper; a bad value is refused by the validator the config file load and `PUT /api/config` share. |
| `autonomous.decisions.folder` | text | empty | Folder of decision files the daemon watches; empty = folder watch off. Helper `EffectiveFolder()`. Surfaces: YAML; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set`; comm `configure`. Read from `config.AutonomousDecisionsConfig` (`internal/config/config.go`) through its `Effective*` helper; a bad value is refused by the validator the config file load and `PUT /api/config` share. |
| `autonomous.decisions.folder_glob` | text | `*.md` | Which files in `folder` are read. Helper `EffectiveFolderGlob()`. Surfaces: YAML; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set`; comm `configure`. Read from `config.AutonomousDecisionsConfig` (`internal/config/config.go`) through its `Effective*` helper; a bad value is refused by the validator the config file load and `PUT /api/config` share. |
| `autonomous.decisions.front_matter_delimiter` | text | `---` | Line that opens and closes the front matter of a decision file. An empty value is refused (`ValidateDecisionFrontMatterDelimiter`). Surfaces: YAML; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set`; comm `configure`. Read from `config.AutonomousDecisionsConfig` (`internal/config/config.go`) through its `Effective*` helper; a bad value is refused by the validator the config file load and `PUT /api/config` share. |
| `autonomous.decisions.front_matter_separator` | text | `:` | Splits a front matter line into key and value. An empty value is refused. Surfaces: YAML; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set`; comm `configure`. Read from `config.AutonomousDecisionsConfig` (`internal/config/config.go`) through its `Effective*` helper; a bad value is refused by the validator the config file load and `PUT /api/config` share. |
| `autonomous.decisions.front_matter_field_separator` | text | `\|` | Separates the parts of an option or recommendation line. An empty value is refused. Surfaces: YAML; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set`; comm `configure`. Read from `config.AutonomousDecisionsConfig` (`internal/config/config.go`) through its `Effective*` helper; a bad value is refused by the validator the config file load and `PUT /api/config` share. |
| `autonomous.decisions.front_matter_keys` | map text→text | empty (canonical names) | Maps a canonical key name to the name a file uses. `PUT /api/config` takes an object or its JSON text. Helper `EffectiveFrontMatterKeys()` returns a copy. Surfaces: YAML; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set`; comm `configure`. Read from `config.AutonomousDecisionsConfig` (`internal/config/config.go`) through its `Effective*` helper; a bad value is refused by the validator the config file load and `PUT /api/config` share. |
| `autonomous.decisions.folder_withdraw_on_delete` | bool | `true` (when absent) | Deleting a decision file withdraws its record. Helper `EffectiveFolderWithdrawOnDelete()`. Surfaces: YAML; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set`; comm `configure`. Read from `config.AutonomousDecisionsConfig` (`internal/config/config.go`) through its `Effective*` helper; a bad value is refused by the validator the config file load and `PUT /api/config` share. |
| `autonomous.decisions.clarity_mode` | text | `warn` | `off`, `warn` or `refuse`: how a record is checked for being readable. Anything else is refused (`ValidateDecisionClarityMode`). Surfaces: YAML; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set`; comm `configure`. Read from `config.AutonomousDecisionsConfig` (`internal/config/config.go`) through its `Effective*` helper; a bad value is refused by the validator the config file load and `PUT /api/config` share. |
| `autonomous.decisions.clarity_min_text_chars` | int | `60` | Fewest characters the situation and deciding text may have; whole number >= 0, an explicit `0` stays `0` (length check off). A negative value is refused. Surfaces: YAML; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set`; comm `configure`. Read from `config.AutonomousDecisionsConfig` (`internal/config/config.go`) through its `Effective*` helper; a bad value is refused by the validator the config file load and `PUT /api/config` share. |
| `autonomous.decisions.clarity_require_tradeoffs` | bool | `true` (when absent) | Every option needs a one-line cost and risk. Surfaces: YAML; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set`; comm `configure`. Read from `config.AutonomousDecisionsConfig` (`internal/config/config.go`) through its `Effective*` helper; a bad value is refused by the validator the config file load and `PUT /api/config` share. |
| `autonomous.decisions.clarity_require_label` | bool | `true` (when absent) | A record needs a short human label. Surfaces: YAML; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set`; comm `configure`. Read from `config.AutonomousDecisionsConfig` (`internal/config/config.go`) through its `Effective*` helper; a bad value is refused by the validator the config file load and `PUT /api/config` share. |
| `autonomous.plan_records.enabled` | bool | `true` (when absent) | Master switch for plan records (registry rows and findings kept by the daemon). While `false` every `/api/plan-records` route answers 404 and the Plan records section of the Projects page hides; records stay on disk. Helper `EffectiveEnabled()`. Surfaces: YAML; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set`; chat `configure`; PWA Settings, Automata. |
| `autonomous.plan_records.write_through` | bool | `false` (when absent) | When on, the merge pipeline's `registry_row` post-ship hook records a shipped row as a plan record instead of editing `docs/plans/README.md`. Helper `EffectiveWriteThrough()`. Same surfaces as `enabled`. |
| `autonomous.plan_records.import_conflict_policy` | text | `suffix` | `suffix`, `rename` or `skip`: what an import does with a title or row number that is already taken, when the call names no policy. Any other value is refused. Helper `EffectiveImportConflictPolicy()`. Same surfaces as `enabled`. |
| `autonomous.decisions.clarity_tracker_id_pattern` | regular expression | empty (the built-in `DefaultQuestionQualityTrackerIDPattern`) | Spots an internal tracker identifier in a record. A pattern that does not compile is refused. Surfaces: YAML; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set`; comm `configure`. Read from `config.AutonomousDecisionsConfig` (`internal/config/config.go`) through its `Effective*` helper; a bad value is refused by the validator the config file load and `PUT /api/config` share. |
| `autonomous.decisions.doc_root` | text | empty (the daemon's working directory) | Folder document paths on a record resolve against and must stay inside. Surfaces: YAML; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set`; comm `configure`. Read from `config.AutonomousDecisionsConfig` (`internal/config/config.go`) through its `Effective*` helper; a bad value is refused by the validator the config file load and `PUT /api/config` share. |
| `autonomous.decisions.doc_status_mode` | text | `report` | `off`, `report` or `on`: whether an answer is written back to the linked document's status line. Anything else is refused (`ValidateDecisionDocStatusMode`). Surfaces: YAML; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set`; comm `configure`. Read from `config.AutonomousDecisionsConfig` (`internal/config/config.go`) through its `Effective*` helper; a bad value is refused by the validator the config file load and `PUT /api/config` share. |
| `autonomous.decisions.doc_status_pattern` | regular expression | `^Confirmed:.*$` | Finds the status line. A pattern that does not compile is refused. Surfaces: YAML; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set`; comm `configure`. Read from `config.AutonomousDecisionsConfig` (`internal/config/config.go`) through its `Effective*` helper; a bad value is refused by the validator the config file load and `PUT /api/config` share. |
| `autonomous.decisions.doc_status_template` | text | `Confirmed: {choice} - "{quote}" ({actor}, {date})` | Replacement text for the status line; placeholders `{choice}`, `{choice_label}`, `{quote}`, `{actor}`, `{date}`, `{decision_id}`. Surfaces: YAML; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set`; comm `configure`. Read from `config.AutonomousDecisionsConfig` (`internal/config/config.go`) through its `Effective*` helper; a bad value is refused by the validator the config file load and `PUT /api/config` share. |
| `autonomous.decisions.follow_ups` | list of {name, kind, target} | empty (nothing runs) | Actions an answered record may request; `kind` is `start_template` (target a template id) or `open_automaton` (target an Automaton id). An empty name, another kind, an empty target or a repeated name is refused (`ValidateDecisionFollowUps`). `PUT /api/config` takes a list or its JSON text. Surfaces: YAML; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set`; comm `configure`. Read from `config.AutonomousDecisionsConfig` (`internal/config/config.go`) through its `Effective*` helper; a bad value is refused by the validator the config file load and `PUT /api/config` share. |
| `autonomous.max_engines_per_node` | int | `1` | Local-model sessions one compute node runs at once. Read in `cmd/datawatch/capacity_wiring.go` (`syncPools`) to set the `node:<name>` pool limit for nodes that host local engines; a node's own `max_concurrent_sessions` stays a ceiling when smaller. Values below `1` clamp to `1` (`internal/config/config.go`). Only local-engine sessions (`pi`, `pi-prompt`, `ollama`, `opencode`, `opencode-acp`) are gated; hosted backends are not. Waiting sessions show `waiting for node <name>`. Operator guide: `docs/operations.md` → "Compute-node queue". |
| `autonomous.max_engines_per_node` | int | `1` | Local-model sessions one compute node runs at once. Read in `cmd/datawatch/capacity_wiring.go` (`syncPools`) as the fallback slot count of every compute node that hosts a local engine and has neither observer GPU data nor system memory data (`compute.NodeSlots`, `internal/compute/slots.go`) to set its `node:<name>` pool limit; a node's own `max_concurrent_sessions` stays a ceiling when smaller. Values below `1` clamp to `1` (`internal/config/config.go`). Only local-engine sessions (`pi`, `pi-prompt`, `ollama`, `opencode`, `opencode-acp`) are gated; hosted backends are not. Waiting sessions show `waiting for node <name>`. Operator guide: `docs/operations.md` → "Compute-node queue". |
| `autonomous.host_load_alert.mode` | string | `alert` | Host load check mode: `off`, `alert` or `reap`. Empty reads as `alert`; any other value is refused on load and on every write path. `reap` also ends processes of ended sessions that the orphan reaper's evidence rule proves, by exact process id. Read from the live config every minute by `startHostLoadCheck` in `cmd/datawatch/host_load_wiring.go`; the check itself is `autonomous.HostLoadCheck`; the chat read is `autonomous host-load` (`internal/router/host_load_comm.go`). |
| `autonomous.host_load_alert.multiple` | float | `1.5` | Load-to-cores multiple that counts as a breach. `0` reads as `1.5`; negative is refused. |
| `autonomous.host_load_alert.minutes` | int | `10` | Minutes a breach must last before one alert is raised for the episode. `0` reads as `10`; negative or fractional is refused. |
| `autonomous.capacity_rate_requests` | int | `0` | Rate ceiling on lease grants per rolling window, applied to each `llm:<name>` pool. `0` = no requests ceiling. Negative values are rejected (config load, REST, MCP, CLI). Pushed into the ledger with `Ledger.SetRate` by `syncPools` in `cmd/datawatch/capacity_wiring.go` on startup and every 10 s, so config edits apply without a restart. |
| `autonomous.capacity_rate_tokens` | int | `0` | Rate ceiling on tokens per rolling window for each `llm:<name>` pool. `0` = no tokens ceiling. Usage is counted by `Ledger.RecordTokens`, called from the usage listener (`recordUsageSpend`) with each event's `TokensIn + TokensOut`. |
| `autonomous.capacity_rate_window_seconds` | int | `0` | Rolling window length for the two ceilings above; `0` = `3600`. Matters only when a ceiling is set. Surfaces: `GET/PUT /api/config` and `/api/autonomous/config`, MCP `autonomous_config_get/set`, CLI `autonomous config-set --capacity-rate-requests/--capacity-rate-tokens/--capacity-rate-window-seconds`, comm `configure autonomous.capacity_rate_*`, Settings → Autonomous. |
| `autonomous.injection_guard` | bool | `false` | Scan user-supplied PRD/task specs for prompt injection phrases at the API boundary on create and edit. Findings are always logged. |
| `autonomous.block_on_injection` | bool | `false` | When true (requires `injection_guard: true`), requests containing injection phrases are rejected with HTTP 400 instead of warn-only. |
| `autonomous.notify.default_events` | []string | `[prd.needs_review, prd.blocked, prd.failed, prd.completed, prd.cancelled]` | Global default set of lifecycle event kinds (see `autonomous.DefaultNotifyEvents`) — the third tier of the notify-event resolution, after an Automaton's own `notify_events` list and its guardrail profile's `notify_events` list (internal/autonomous/notify_events_resolve.go). Unset = the shipped five-kind baseline; an explicit empty list (`default_events: []`) is the operator opt-out (the global tier then resolves to no kinds). Lifecycle sinks (alert-store write, topic push to `alerts` + `automaton-<id>`, comm fan-out) act on the kinds listed. The global `notify_exclude` still applies to the comm fan-out |
| `autonomous.project_write_serialization` | string | `advisory` | BL428 §3a (docs/plans/2026-10-14-concurrency-and-budget-design.md): how Automaton task writes targeting the same project tree are serialized. `advisory` (default) = exclusive per-canonical-tree lock at admission — same-tree tasks queue, different trees run in parallel. `parallel` = RESERVED but DISABLED — recognized and accepted until the mechanism lands in the follow-up implementation PRD; the daemon still enforces the advisory lock and logs a startup warning. Any other value is a config error and the daemon refuses to start |
| `autonomous.refuse_dirty_tree_start` | bool | `false` | BL428 §3a ii admission-time companion: when true, task admission hard-fails if the PRD's project tree has uncommitted changes (`git status --porcelain` non-empty). The error names the dirty file count and the options: commit or stash first, `git clean`, or set this key back to `false` |
| `autonomous.stuck_running_seconds` | int | `900` | Stuck-running watchdog bound, in seconds. An Automaton that stays running with every task finished, no live worker session and no live executor for this long has its completion roll-up re-fired once; after a second full bound in the same state it moves to blocked. `0` or negative uses the default. Reachable from the PWA settings card, `PUT /api/config`, the MCP `autonomous_config_set` and `config_set` tools, the CLI `datawatch config set` and `configure autonomous.stuck_running_seconds=<n>` in a comm channel. |
| `autonomous.checks.pre_start_verifier` | string | `on` | `off` \| `shadow` \| `on`. Before a task starts, check that its verification script exists, is executable and starts with a shebang line (`internal/autonomous/verifier_prestart.go`). `shadow` records the finding on the triage record only. Reachable from the PWA Settings Checks group, `PUT /api/config`, the MCP `autonomous_config_set` and `config_set` tools, the CLI `datawatch config set` and `configure autonomous.checks.<key>=<value>` in a comm channel. An unknown mode is rejected (`internal/config/autonomous_checks.go`); a per-project override lives under `autonomous.checks.projects.<project_dir>`. |
| `autonomous.checks.block_when_missing` | bool | `true` | When the pre-start verifier is `on`, a task whose verification script is missing is blocked instead of started; `false` records the finding and lets the task start. Reachable from the PWA Settings Checks group, `PUT /api/config`, the MCP `autonomous_config_set` and `config_set` tools, the CLI `datawatch config set` and `configure autonomous.checks.<key>=<value>` in a comm channel. An unknown mode is rejected (`internal/config/autonomous_checks.go`); a per-project override lives under `autonomous.checks.projects.<project_dir>`. |
| `autonomous.checks.plan_rules` | string | `on` | `off` \| `shadow` \| `on`. Plan-rule findings on a decomposed plan: a feature with no test, a tracker id in a new name, a fixed release version. Reachable from the PWA Settings Checks group, `PUT /api/config`, the MCP `autonomous_config_set` and `config_set` tools, the CLI `datawatch config set` and `configure autonomous.checks.<key>=<value>` in a comm channel. An unknown mode is rejected (`internal/config/autonomous_checks.go`); a per-project override lives under `autonomous.checks.projects.<project_dir>`. |
| `autonomous.checks.after_done` | string | `on` | `off` \| `shadow` \| `on`. The after-done checklist run when an Automaton finishes: version files, change log, configuration reference, parity issue and registry row. Reachable from the PWA Settings Checks group, `PUT /api/config`, the MCP `autonomous_config_set` and `config_set` tools, the CLI `datawatch config set` and `configure autonomous.checks.<key>=<value>` in a comm channel. An unknown mode is rejected (`internal/config/autonomous_checks.go`); a per-project override lives under `autonomous.checks.projects.<project_dir>`. |
| `autonomous.checks.registry_readme` | string | `on` | `off` \| `shadow` \| `on`. The registry check: the registry table is compared against the plan documents (`internal/autonomous/registry_readme_check.go`). Reachable from the PWA Settings Checks group, `PUT /api/config`, the MCP `autonomous_config_set` and `config_set` tools, the CLI `datawatch config set` and `configure autonomous.checks.<key>=<value>` in a comm channel. An unknown mode is rejected (`internal/config/autonomous_checks.go`); a per-project override lives under `autonomous.checks.projects.<project_dir>`. |
| `autonomous.ship.push` | string | `on` | `off` \| `shadow` \| `on`. After a release is finished, push the branch to `autonomous.ship.remote` (`internal/autonomous/ship_step.go`). `shadow` records the command it would run and runs nothing. Reachable from the PWA Settings Ship steps group, `PUT /api/config`, the MCP `autonomous_config_set` and `config_set` tools, the CLI `datawatch config set` and `configure autonomous.ship.<key>=<value>` in a comm channel. An unknown mode is rejected (`internal/config/autonomous_ship.go`); a per-project override lives under `autonomous.ship.projects.<project_dir>`. |
| `autonomous.ship.tag` | string | `on` | `off` \| `shadow` \| `on`. Create an annotated tag named from `autonomous.ship.tag_format` and push it (`internal/git/tag.go`). Reachable from the PWA Settings Ship steps group, `PUT /api/config`, the MCP `autonomous_config_set` and `config_set` tools, the CLI `datawatch config set` and `configure autonomous.ship.<key>=<value>` in a comm channel. An unknown mode is rejected (`internal/config/autonomous_ship.go`); a per-project override lives under `autonomous.ship.projects.<project_dir>`. |
| `autonomous.ship.ci_watch` | string | `on` | `off` \| `shadow` \| `on`. Watch the CI runs of the pushed commit and tag, limited to `autonomous.ship.ci_workflows` (`internal/autonomous/ship_ci_watch.go`). Reachable from the PWA Settings Ship steps group, `PUT /api/config`, the MCP `autonomous_config_set` and `config_set` tools, the CLI `datawatch config set` and `configure autonomous.ship.<key>=<value>` in a comm channel. An unknown mode is rejected (`internal/config/autonomous_ship.go`); a per-project override lives under `autonomous.ship.projects.<project_dir>`. |
| `autonomous.ship.tag_format` | string | `v{version}` | Tag name template; must contain `{version}`, otherwise the value is rejected. Reachable from the PWA Settings Ship steps group, `PUT /api/config`, the MCP `autonomous_config_set` and `config_set` tools, the CLI `datawatch config set` and `configure autonomous.ship.<key>=<value>` in a comm channel. An unknown mode is rejected (`internal/config/autonomous_ship.go`); a per-project override lives under `autonomous.ship.projects.<project_dir>`. |
| `autonomous.ship.remote` | string | `origin` | Git remote the push and tag steps use. Reachable from the PWA Settings Ship steps group, `PUT /api/config`, the MCP `autonomous_config_set` and `config_set` tools, the CLI `datawatch config set` and `configure autonomous.ship.<key>=<value>` in a comm channel. An unknown mode is rejected (`internal/config/autonomous_ship.go`); a per-project override lives under `autonomous.ship.projects.<project_dir>`. |
| `autonomous.ship.ci_workflows` | []string | empty (all workflows) | Workflow names the CI watch follows; comma separated on the CLI and in a comm channel. Reachable from the PWA Settings Ship steps group, `PUT /api/config`, the MCP `autonomous_config_set` and `config_set` tools, the CLI `datawatch config set` and `configure autonomous.ship.<key>=<value>` in a comm channel. An unknown mode is rejected (`internal/config/autonomous_ship.go`); a per-project override lives under `autonomous.ship.projects.<project_dir>`. |
| `autonomous.push_and_tag` | bool | `false` | Whether the daemon publishes a finished release itself: when on, after the ship steps it pushes the release commit to the default branch, pushes the release tag and waits for the release CI for `git.release_ci_window_minutes`; when off (the default) only the work branch is pushed and the operator publishes the release. Read live on ship (no restart). Surfaces: PWA Settings *Release* group, `GET`/`PUT /api/config`, MCP `release_push_status` and `autonomous_config_set`, CLI `datawatch config set`, comm `configure`. The setting is stored and read back, but the daemon's ship path does not call the push and tag code yet. See `docs/howto/release-push-and-tag.md`. |
| `autonomous.release_path` | string | `direct_push` | How a finished release is published when `autonomous.push_and_tag` is on: `direct_push` (default; push the release commit to the default branch, then the release tag) or `pull_request` (push the release branch, open a pull request into the default branch, poll the pull request's checks for `git.release_ci_window_minutes`, and merge only when every check succeeded; a failed or still-pending check set is recorded as `checks_failed` / `checks_timed_out` and leaves the pull request open and unmerged; `internal/autonomous/release_pull_request.go`). Any other value behaves as `direct_push`. Surfaces as `autonomous.push_and_tag`. |
| `autonomous.stale_task_seconds` | int | `900` | Stall window for a running worker, in seconds. Activity-based: a worker counts as stalled only when it has no output, no live child process, no backend activity, and no file change in its work directory for this many seconds. `0` or negative inherits `session.stale_timeout_seconds`. |
| `autonomous.task_max_attempt_seconds` / `autonomous.task_no_progress_seconds` | int | `0` / `0` | Optional per-attempt bounds (both off when `0`). `task_max_attempt_seconds` is the wall-clock cap on one attempt (recommended `5400`); `task_no_progress_seconds` stops an attempt that goes N seconds without progress — terminal output growth, a changed tracked/untracked file in the work directory (harness scratch files ignored), a new commit, or a live child process using CPU (recommended `1800`). On firing, the uncommitted work is saved to a ref before the worker is stopped and never discarded, and the normal retry budget applies. |
| `autonomous.preserve_attempt_work` | bool | `true` | When a stalled or killed attempt is stopped, its uncommitted work is saved to `refs/datawatch/attempts/<task>/<n>` and restored on the next attempt, unless that attempt was started with `clean_restart` (`reset_task`). `false` disables the save and restore. |
| `autonomous.tags.default_view` | string | `auto` | Which view the Automata list opens on: `auto` (the attention view when at least one Automaton needs attention, otherwise the tag browser), `attention` (always the attention view) or `tags` (always the tag browser). Any other value is refused. Set through REST `PUT /api/config` `autonomous.tags.default_view`, MCP `config_set`, CLI `datawatch config set autonomous.tags.default_view <value>`, comm `configure autonomous.tags.default_view=<value>`, and Settings → General → Automata → Tag browser. |
| `autonomous.tags.attention_statuses` | []string | `[blocked, needs_review, paused]` | Automaton statuses that place an Automaton in the attention group. Valid statuses: `draft`, `planning`, `needs_review`, `approved`, `revisions_asked`, `running`, `completed`, `rejected`, `cancelled`, `archived`, `blocked`, `failed`, `paused`. Any other entry is refused; an empty list takes the default statuses. A paused Automaton and an Automaton with a task held for operator review are always in the attention group. Same surfaces as `autonomous.tags.default_view`. |
| `autonomous.tags.default_match_mode` | string | `any` | How multiple selected tags combine in the tag browser: `any` widens the list (at least one selected tag), `all` narrows it (every selected tag). Only `any` or `all` are accepted. Same surfaces as `autonomous.tags.default_view`. |
| `autonomous.tags.direct_counts_visible` | bool | `false` | When `true`, each tag shows its direct count as a tooltip beside the subtree count. Same surfaces as `autonomous.tags.default_view`. |
| `autonomous.tags.registry_keeper` | string | `""` | Federation peer name of the daemon that keeps the shared tag registry. Empty means this daemon is its own keeper and nothing syncs. Same surfaces as `autonomous.tags.default_view`. |
| `autonomous.tags.registry_sync_seconds` | int | `30` | Interval, in whole seconds, at which the shared tag registry syncs with the keeper peer named in `autonomous.tags.registry_keeper`. Must be 5 or more; a smaller value is refused. Same surfaces as `autonomous.tags.default_view`. |
| `autonomous.auto_tags.enabled` | bool | `true` | Whether auto tagging runs at all: the tags an Automaton gives itself as it runs (automation, stage, kind), kept current in the tag browser and its counts without a human (`internal/config/auto_tags_settings.go`, `internal/autonomous/auto_tags.go`). `false` turns it off daemon-wide, and it is operative in both senses — an unset `enabled` reads `true`, and an explicitly stored `false` stays `false` after a save-and-reload. Same surfaces (REST `PUT /api/config`, MCP `config_set`, CLI `datawatch config set`, comm `configure`) as `autonomous.tags.default_view`, with the same message on every surface; an invalid value is refused everywhere with the same message and nothing is written. |
| `autonomous.auto_tags.automation_template` | string | `automation:{automation}` | Template for the Automation's tag. Same surfaces as `autonomous.auto_tags.enabled`. |
| `autonomous.auto_tags.stage_template` | string | `stage:{stage}` | Template for the stage tag. Same surfaces as `autonomous.auto_tags.enabled`. |
| `autonomous.auto_tags.kind_template` | string | `kind:{kind}` | Template for the kind tag. `{kind}` renders as the kind's label from `autonomous.auto_tags.kind_labels`. Same surfaces as `autonomous.auto_tags.enabled`. |
| Template placeholders | — | — | Any of the three templates may reference only the placeholders `{automation}`, `{stage}` and `{kind}` — any other `{name}` is refused with a message naming the placeholder (`autonomous.auto_tags.<field>: placeholder {owner} is not one of {automation}, {stage}, {kind}`). The template must not be empty, must contain no newline, and must render to a non-empty name. |
| `autonomous.auto_tags.kind_labels` | map | `{feature: feature, bug: bug, other: maintenance}` | What `{kind}` renders as per Automaton kind (`internal/autonomous/auto_tags.go`): the kind's own label from `feature`, `bug`, `other`, then the default vocabulary (`feature` → `feature`, `bug` → `bug`, `other` → `maintenance`), then the kind itself as a last resort. Rename a label (e.g. `other: support`) to change what kind tags say. Shipped as a map in the config file and accepted as `kind:label` pairs in the CLI/comm surfaces (for example `datawatch config set autonomous.auto_tags.kind_labels other:support`). kind_labels are daemon-global: a per-Automation override cannot carry its own. |
| `autonomous.housekeeping_repairs.task_chain` | string | `report` | Mode of the housekeeping repair for an Automation chain whose next step is waiting and was not started: `off` does nothing, `report` records what it would do and changes nothing, `on` repairs it. The group also holds three repairs for stuck task chains (a dependent reset, a docs mirror sync for a final gate, removal of one untracked leaked file), each retried at most once (`internal/autonomous/housekeeping_repair_task_chain.go`); the daemon does not start them yet. Each repair writes one decision-log line (found, done, how to undo) and a case skipped as unsafe raises one alert. Repairs never merge, ship, delete or end a session, change model or backend settings, or touch security workflows (`internal/config/housekeeping_repairs.go`, `internal/autonomous/housekeeping_repairs.go`). Same surfaces and refusal message as `autonomous.auto_tags.enabled`. |
| `autonomous.housekeeping_repairs.advance_judge` | string | `report` | Mode of the housekeeping repair for a task held after infrastructure retries ran out: `off` does nothing, `report` records what it would do and changes nothing, `on` repairs it. The group also holds three repairs for finished and review-blocked Automata (an advance of a running Automaton whose tasks are all finished, one repair task made once from the rules review findings, and a merge-wait/merge-ready report that never merges) (`internal/autonomous/housekeeping_repair_advance.go`); the daemon does not start them yet. Each repair writes one decision-log line (found, done, how to undo) and a case skipped as unsafe raises one alert. Repairs never merge, ship, delete or end a session, change model or backend settings, or touch security workflows (`internal/config/housekeeping_repairs.go`, `internal/autonomous/housekeeping_repairs.go`). Same surfaces and refusal message as `autonomous.auto_tags.enabled`. |
| `autonomous.housekeeping_repairs.lane_fill` | string | `report` | Mode of the housekeeping repair for a running lane count that differs from the wanted count inside its bounds: `off` does nothing, `report` records what it would do and changes nothing, `on` repairs it. The group also holds a lane fill repair that starts the next eligible Automaton on an idle lane through the normal run path, never changing a model or backend (`internal/autonomous/housekeeping_repair_lanes.go`); the daemon does not start it yet. Each repair writes one decision-log line (found, done, how to undo) and a case skipped as unsafe raises one alert. Repairs never merge, ship, delete or end a session, change model or backend settings, or touch security workflows (`internal/config/housekeeping_repairs.go`, `internal/autonomous/housekeeping_repairs.go`). Same surfaces and refusal message as `autonomous.auto_tags.enabled`. |
| `autonomous.housekeeping_repairs.leftovers` | string | `report` | Mode of the housekeeping repair for leftover disk state that crossed the cleanup alert threshold: `off` does nothing, `report` records what it would do and changes nothing, `on` repairs it. Each repair writes one decision-log line (found, done, how to undo) and a case skipped as unsafe raises one alert. Repairs never merge, ship, delete or end a session, change model or backend settings, or touch security workflows (`internal/config/housekeeping_repairs.go`, `internal/autonomous/housekeeping_repairs.go`). Same surfaces and refusal message as `autonomous.auto_tags.enabled`. |
| `autonomous.housekeeping_repairs.grace_seconds` | int | `300` | Seconds a stuck state must persist before it is reported or repaired; 0 means no grace. Same surfaces as the group modes. |
| Per-Automation override | — | — | An Automation may carry an `auto_tags` override (`AutoTagsOverride`, set through the same validation) with `enabled` (only an explicit value wins over the daemon's), and any of the three templates (an empty one inherits the daemon's). The override is stored on the Automation and survives an update that does not name it. `ResolveAutoTags(daemonCfg, override)` is the single merge point; `RenderAutoTag` renders one tag and refuses when auto tagging is off or the rendered name is empty. |
| Sync engine | — | — | The tags are reconciled, not hand-managed: `Manager.syncAutoTags` runs on every membership join or leave, member move, stage rename, Automation rename, kind change, delete, and daemon flip (`SyncAutoTagsAll`), and after a config save that changed the block (REST `PUT /api/config`, MCP `config_set`, CLI `config set`, comm `configure`) (internal/autonomous/auto_tags.go). It gets or creates the engine's system tags (colour-neutral, under the root), renames the record in place when the rendered name changes (tag ID and assignments survive a rename), keeps operator tags and tags of other Automations untouched, strips the engine's own stale tags from a PRD, and reclaims an engine tag record no member carries anymore (an empty stage's stage tag goes; the shared kind vocabulary stays). A tag name an operator tag already owns is refused loudly everywhere the engine would create it — it is never silently renamed or overwritten; a refused create leaves no Automation behind. Sessions read the tags live: a session of a member PRD inherits the three engine tags (`internal/session/InheritedTags`) with no session write. |
| System tags | — | — | Tags datawatch creates for its own vocabulary (the automation/stage/kind auto tags) carry `Tag.System` (`internal/autonomous/models.go`). `RenameTag`, `SetTagParent` and `DeleteTag` refuse a system tag with `ErrTagSystem` (REST 409) so an operator's rename can never orphan an Automaton's tags, while `SetTagColor` and `SetTagDescription` stay allowed (`internal/autonomous/tags.go`). |
| `autonomous.tags.placement.enabled` | bool | `true` | Master switch for the placement tags (`internal/placementtags/derive.go`): when `false`, no placement tags are derived at all. Set through `config.yaml`; REST, comm-channel, CLI and web-settings surfaces come with the follow-up wiring task. Invalid values are refused at load, so nothing bad is ever stored. |
| `autonomous.tags.placement.<axis>` | {enabled, template} | all ten on: compute, peer, backend, lane, role, project, automaton, automation, stage, kind | One entry per built-in placement axis. `enabled` turns the axis on or off; `template` builds the tag and must contain the axis's own placeholder (`{node}` for compute, `{peer}`, `{backend}`, `{lane}`, `{role}`, `{project}`, `{automaton}`, `{automation}`, `{stage}`, `{kind}`), replaced with the fact value. Defaults `compute:{node}`, `peer:{peer}`, `backend:{backend}`, `lane:{lane}`, `role:{role}`, `project:{project}`, `automaton:{automaton}`, `automation:{automation}`, `stage:{stage}`, `kind:{kind}`. A piece of work missing the axis's fact value yields no tag. A template without its placeholder is refused. Same surfaces as `autonomous.tags.placement.enabled`. |
| `autonomous.tags.placement.custom` | []{name, field, template, enabled} | empty | User-defined placement axes (empty by default). `name`: lowercase letters, digits and hyphens, unique, and not a built-in axis name. `field`: the session field read, one of Name, Backend, Model, Node, PRDID, Role, Project, Automaton, Automation, Stage, Kind (the list lives in `internal/placementtags/derive.go`). `template`: must contain the placeholder `{value}`, replaced with the field's value. `enabled`: on or off for that axis. Each refusal is at load time with the axis's message. Same surfaces as `autonomous.tags.placement.enabled`. |
| `web_search.enabled` | bool | `false` | Inject a `web_search` MCP tool into opencode and goose sessions via SearXNG. |
| `web_search.provider` | string | `searxng` | Search provider. Only `"searxng"` is supported. |
| `web_search.url` | string | `""` | Base URL of the SearXNG instance (required when enabled). |
| `web_search.engine` | string | `bing` | SearXNG engine to use. Only `bing` is reliable; others trigger CAPTCHA/rate-limiting. |
| `web_search.num_results` | int | `10` | Maximum results returned per query. |
| `audit.access_log_enabled` | bool | `true` | Log every HTTP request/response, auth failure, and WS connect/disconnect to `access.log` (method, path, status, remote IP, user agent, resolved principal — never the token). |
| `audit.retention_days` | int | `30` | Prune entries older than this from both `audit.log` and `access.log`. Negative = never prune. |
| `audit.cef_mirror_enabled` | bool | `false` | Additionally write a CEF-formatted line (`<file>.cef`) alongside every entry, for SIEM forwarding. |
| `imap_mcp.token` | string | `""` | Bearer token imap-mcp ≥ 0.5.3 requires on every request except `GET /api/health`. Accepts a `${secret:name}` reference. Needs `read` + `send` scopes. |
| `plugins.managed` | []string | `[]` | Names of `kind: service` plugins the operator allows datawatch to start/stop as supervised child processes. Absent = never managed. Only honoured when the plugin's manifest `lifecycle.supported_modes` declares `managed` and the plugin is enabled. |
| `cross_session.enabled` | bool | `true` | Inject the Cross-Session Communication Rule into spawned claude-code sessions' `CLAUDE.md` — steers the LLM toward datawatch's own audited memory/discussion/reply tools instead of Claude Code's native cross-session messaging. |
| `git.push_on_complete` | string | `branch` | What to push to the remote when work completes: `never` (no push at all), `branch` (push the finished work branch after each validated task and again at completion; the default), or `default_branch` (plain fast-forward push of HEAD onto the remote's default branch, only after the automaton's final gates passed at completion). Empty/unknown values behave as `branch`. Read from the live config on every push attempt (no restart needed). Pushes are never force. A PRD's own `Git.PushOnComplete` override wins over this when set. See `docs/operations.md` → “Pushing finished work to the remote”. |
| `git.push_remote` | string | `origin` | Remote name the finished branch is pushed to. Read live on every push attempt. |
| `git.release_ci_window_minutes` | int | `60` (when unset) | Minutes the release CI (the workflows listed in the release config) is waited for; applies only when `autonomous.push_and_tag` is on. `0` or unset reads as `60`. See `docs/howto/release-push-and-tag.md`. |

### Session list views

`GET /api/sessions` accepts three optional query parameters. Without any
of them the response is exactly what it has always been.

- `view=summary` — same JSON array of session objects (including
  `scheduled_count` when schedules are enabled), but the long text
  fields `task`, `prompt_context`, `last_response` and
  `last_summary_long` are cut to `server.session_summary_text_chars`
  characters with a trailing `…`. Each cut record gets a
  `truncated_fields` array naming the fields that were shortened.
  Terminal sessions (complete, failed, killed) are capped to the newest
  `server.session_summary_terminal_limit` by `updated_at`; when any are
  dropped the response carries `X-Datawatch-Sessions-Omitted: <n>`. Any
  other `view` value returns 400.
- `since=<RFC3339 | unix seconds>` — only sessions whose `updated_at` is
  strictly after the given time. Unparseable values return 400.
- `states=active` — only non-terminal sessions (running, waiting_input,
  rate_limited). A comma list of explicit state names is also accepted,
  e.g. `states=complete,failed`; unknown names return 400.

`view=summary` now returns row-only records, and `limit`, `cursor`, `status`,
`active` and `q` page and filter them (envelope `{sessions, total, next_cursor}`)
with an `ETag`; `GET /api/autonomous/prds` is likewise a paged summary list
(`limit`, `cursor`, `status`, `active`, `q`, `tag`, `full=1`) with an `ETag`. See
`docs/api/sessions.md` and `docs/api/autonomous.md`. `scripts/perf-budget.sh`
enforces the first-page size budget (256 KB) and the 304 behaviour.

`since` and `states` also work without `view=summary` (full records).
`tree=1` and `orphaned=1` take precedence and ignore all three. Fetch
the full record for one session with `GET /api/sessions/{id}`.

**Retention decision:** the unfiltered list is intentionally not capped,
so existing clients keep working unchanged. Retention applies only to
`view=summary`. Clients should check for the `sessions.view_summary`
entry in the `capabilities` array of `GET /api/info` before using these
parameters. Both limits are read from the live config on each request,
so a `PUT /api/config` change applies without a restart.

### WebSocket session updates

By default every session change is sent to every WebSocket client as a
`sessions` frame carrying the whole list. A client can ask for less by
sending `hello` at any time after connecting:

```json
{"type":"hello","data":{"session_list":"delta","session_view":"summary"}}
```

The daemon answers with `hello_ack`, echoing the modes now in effect and
the WebSocket capabilities it supports:

```json
{"type":"hello_ack","data":{"session_list":"delta","session_view":"summary","capabilities":["ws.hello","ws.session_list_delta","ws.session_view_summary"]}}
```

- `session_list: "full"` — the existing stream (whole-list `sessions`
  frames plus per-change `session_state`).
- `session_list: "delta"` — no `sessions` frames. Each session that is new
  or changed gets one `session_state` `{session}` frame; each session that
  leaves the list gets one `session_removed` `{session_id}` frame. Fetch
  the starting list with `GET /api/sessions?view=summary` right after
  `hello_ack` and apply the stream on top of it.
- `session_view: "summary"` — `session_state` carries the same compact
  record as `GET /api/sessions?view=summary` (long text fields cut to
  `server.session_summary_text_chars`, with `truncated_fields`).

Unknown `hello` keys are ignored and an unknown value leaves that mode
unchanged. Clients that never send `hello` get
`server.ws_session_list_default`. `GET /api/info` lists `ws.hello`,
`ws.session_list_delta` and `ws.session_view_summary` in `capabilities`.

### WebSocket output scoping

By default every running session's terminal output reaches every
WebSocket client, whether or not it has that session open. A client can
opt in to output for only the sessions it subscribed to by adding
`"output":"subscribed"` to `hello` (`hello_ack` echoes the effective
`output` mode, `all` or `subscribed`):

```json
{"type":"hello","data":{"output":"subscribed"}}
{"type":"subscribe","data":{"session_id":"myhost-a1b2"}}
{"type":"unsubscribe","data":{"session_id":"myhost-a1b2"}}
```

- Scoped frames: `output`, `raw_output`, `pane_capture` and
  `chat_message` (the chat-mode sessions' message stream).
- Never scoped: `response` (one per completed turn), `session_state`,
  `sessions`, `session_removed`, `needs_input`, `alert`, `alerts_read`,
  `notification`, `channel_*` and every other control frame.
- `subscribe` / `unsubscribe` accept the full or short session ID; either
  form matches output for that session.
- `unsubscribe` (any output mode) removes the subscription and stops the
  screen capture the client's `subscribe` started. Before, captures ran
  until the client disconnected.

Clients that never send `output` get `server.ws_output_scope_default`
(`all` unless configured). The PWA buffers output for sessions it is not
viewing, so leave that at `all` while PWA clients are connected.
`GET /api/info` lists `ws.output_scoped` and `ws.unsubscribe` in
`capabilities`.

### API response compression

When `server.api_gzip` is on (the default), every `/api/*` response
carries `Vary: Accept-Encoding`, and a response is sent with
`Content-Encoding: gzip` (and no `Content-Length`) when all of these
hold:

- the request sends `Accept-Encoding: gzip` and is not `HEAD`;
- the body grows past `server.api_gzip_min_bytes` bytes — the daemon
  buffers up to that many bytes first, so small responses go out
  byte-for-byte unchanged with their original headers;
- the `Content-Type` is `application/json`, `application/*+json`,
  `text/plain` or YAML.

Never compressed: WebSocket upgrades and other connection takeovers,
`text/event-stream` streams, a response flushed before reaching the
threshold (it is committed uncompressed so streaming is not delayed),
`204`/`206`/`304`, responses that already set `Content-Encoding` or
`Content-Disposition`, `/api/files/download`, and the `/api/proxy/...`
forwarding paths (a peer's body is passed through as received; Go's
HTTP client already decodes a peer's own gzip transparently). Both
settings are read on each request. Clients can look for `api.gzip` in
the `capabilities` array of `GET /api/info`; it is listed only while
`server.api_gzip` is on.

### Lifecycle event sinks (`autonomous.notify.default_events`)

`autonomous.notify.default_events` is the list of PRD/task lifecycle event
kinds (e.g. `prd.updated`, `prd.completed`, `prd.failed`,
`task.session_started`, `task.completed`, `task.failed`) the built-in
sinks act on. The daemon-wide list enables each kind only when it is
explicitly present.

**The shipped default is the five-kind baseline**
(`prd.needs_review`, `prd.blocked`, `prd.failed`, `prd.completed`,
`prd.cancelled`; see `DefaultNotifyEvents`). An operator sets a custom
list to replace it, or an explicit empty list (`default_events: []`)
to turn the built-in set off — with no kinds listed nothing new is
sent and the daemon behaves as it did before this default. This list is
also the global tier of the notify-event resolution: each Automaton's
own `notify_events` list and its guardrail profile's `notify_events`
list take precedence over it (internal/autonomous/
notify_events_resolve.go).

The key is enforced at the autonomous sink installer + daemon wiring
(`internal/autonomous`): each sink subscribes only to the lifecycle event
kinds listed in `default_events`, and when one of those kinds fires the
sinks

- write an entry to the alert store (the PWA Alerts tab / `GET /api/alerts`);
- push a notification to the `alerts` topic and the emitting Automaton's
  own `automaton-<id>` topic;
- fan out to comm channels, coalesced per Automaton so a burst of events
  from one Automaton does not become a burst of messages.

The global `notify_exclude` list (see the `notify_exclude` entry above)
still applies to the comm fan-out: a channel named there skips the
lifecycle message broadcast even when the event kind is enabled, and
still handles inbound commands normally.

#### The `notify_events` fields (per-Automaton and per profile)

The resolution above is implemented on two data-level fields — the
**notify set** — rather than config alone:

- `prd.notify_events` — the Automaton's own list (`internal/autonomous/manager.go`, persisted with the PRD record; an empty/missing list inherits the next tier, it can never mean "none").
- `guardrail_profile.notify_events` (struct in internal/autonomous/models.go, persisted by the manager) — the profile tier; every Automaton assigned to the profile inherits it unless it has its own list.
- `autonomous.notify.default_events` — the global tier (above); unset = the five-kind baseline, explicit `[]` = "no kinds".

`ResolveNotifyEvents` / `ResolveNotifyEventsFor`
(internal/autonomous/notify_events_resolve.go) walk those tiers in order,
returning the first **non-empty** list plus its `source`
(`automaton` | `profile` | `global`). All writes go through
`ValidateNotifyEvents` (internal/autonomous/notify_events.go), which
validates against the closed set `ValidNotifyEventKinds()` — every known
lifecycle kind except `prd.updated` (which is deliberately not a notify
kind) — and rejects blank or unknown entries with the valid list in the
error. `task.session_started` and `task.session_cleared` are opt-in:
they fire only when present in the resolved set.

The same rule gates the subscription delivery path (`Manager.NotifyEventAllowed` in internal/autonomous/notify_events_resolve.go, enforced in lifecycle_sinks.go and lifecycle_session_push.go): a subscription for a kind that is not in its Automaton's resolved set is never enqueued or fanned out — filters can only narrow, never widen. Set and clear are audited with action `autonomous_notify_events_changed` (CEF `NotifyEventsChanged`, id 45; internal/audit/events.go + internal/audit/cef.go), carrying the old and new lists and, for Automaton-tier changes, the resolved `source` ("automaton"/"profile"/"global") after the change.

Surfaces (all delegate to those two manager methods / the profile
store): REST `POST /api/autonomous/prds/{id}/set_notify_events`
(internal/server/api.go) and the profile create/update body field; MCP
`autonomous_prd_set_notify_events` and the `notify_events` argument on
`guardrail_profile_create`/`guardrail_profile_update`
(internal/mcp/notify_events_tools.go, internal/mcp/guardrail_tools.go);
CLI `autonomous prd-set-notify-events` and `guardrail profile
create/update --notify-events` (cmd/datawatch/main.go, cmd/datawatch/cli_guardrail.go); comm `autonomous set-notify-events` and `guardrail profile notify-events` (internal/router/sx2_parity.go, internal/router/guardrails.go); the PWA "Notification events" settings panel and the Guardrail Profiles editor (internal/server/web, locales `notify_events_*`).

### Outbound webhook sink and peer forwarding settings

Two lifecycle delivery paths send events beyond this daemon. Both **ship
disabled** and stay off until the operator enables them.

- **Outbound webhook sink.** A webhook subscription POSTs each selected
  lifecycle event to a receiver over HTTPS, signed with HMAC-SHA256. Only hosts
  in `autonomous.notify.webhook.allow_hosts` can receive an event.
- **Peer forwarding.** A peer subscription sends each selected event to a
  federated peer that grants `autonomous:write`. The receiving daemon re-emits
  it locally, marked as forwarded. Loop prevention uses the origin host, the
  hop count and a visited list.

The design and the recommended values are in
`docs/plans/2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal.md`.
The retry numbers, the signature header format, the payload shape and the
peer addressing are pending operator confirmation. Each one is a named constant
or config default, so a changed decision is a one-place edit.

| Method | How |
|--------|-----|
| **YAML** | `~/.datawatch/config.yaml` → `autonomous.notify.webhook` and `autonomous.notify.forwarding` (see `docs/config-reference.yaml`) |
| **CLI** | `datawatch config set <key> <value>`, e.g. `datawatch config set autonomous.notify.webhook.allow_hosts hooks.example.net,hooks.example.net:8443` |
| **Web UI** | Settings → General → **Autonomous Automata planning** card → the **Outbound webhook sink** and **Peer forwarding of lifecycle events** fields, with the allow-list and tunables listed beside them |
| **REST API** | `PUT /api/config` with `{"autonomous.notify.webhook.enabled": true}`; `GET /api/config` returns the same keys under `autonomous.notify.webhook` and `autonomous.notify.forwarding` |
| **Comm channel** | `configure autonomous.notify.webhook.enabled=true` (also listed in the `configure` help) |

**Keys (webhook sink).**

| Key | Type | Default | Meaning |
|-----|------|---------|---------|
| `autonomous.notify.webhook.enabled` | bool | `false` | Master switch. Off refuses webhook subscriptions and delivers nothing from ones already stored. |
| `autonomous.notify.webhook.allow_hosts` | list of `host` or `host:port` | `[]` | The only hosts that may receive. Exact match, case-insensitive; no scheme, path, wildcard or userinfo. Empty means nothing receives. |
| `autonomous.notify.webhook.max_attempts` | integer | `5` | Total attempts per delivery, the first included. A value below 1 means the default. |
| `autonomous.notify.webhook.backoff_seconds` | list of integers | `[1, 4, 16, 64]` | Delay after each failed attempt; the last entry repeats. |
| `autonomous.notify.webhook.jitter_percent` | integer 1-100 | `20` | +- spread on each delay. A value below 1 means the default. |
| `autonomous.notify.webhook.attempt_timeout_seconds` | integer | `10` | Bound on one attempt. |
| `autonomous.notify.webhook.max_body_bytes` | integer | `65536` | Cap on the serialised envelope. Also the size a forwarding receiver accepts. |
| `autonomous.notify.webhook.signature_tolerance_seconds` | integer | `300` | Replay window a receiver enforces on the signed timestamp. |

**Keys (peer forwarding).**

| Key | Type | Default | Meaning |
|-----|------|---------|---------|
| `autonomous.notify.forwarding.enabled` | bool | `false` | Master switch. Off refuses peer subscriptions and makes this daemon refuse forwarded events. |
| `autonomous.notify.forwarding.max_hops` | integer | `3` | Hop cap of a forwarded event, counting the sender. |
| `autonomous.notify.forwarding.max_attempts` | integer | `5` | Total attempts per forwarded event. |
| `autonomous.notify.forwarding.backoff_seconds` | list of integers | `[1, 4, 16, 64]` | Delay after each failed attempt; the last entry repeats. |
| `autonomous.notify.forwarding.jitter_percent` | integer 1-100 | `20` | +- spread on each delay. |
| `autonomous.notify.forwarding.attempt_timeout_seconds` | integer | `10` | Bound on one attempt. |

**Secrets.** A webhook subscription's signing secret is never a literal value in
this config file or in a subscription. The subscription carries a
`${secret:name}` reference into the secrets vault, resolved on each delivery. A
literal value is refused at creation.

**Validation.** A `PUT /api/config` that sets one of these keys with an invalid
value is rejected with HTTP 400 before anything is applied. The rejections are:
an allow-list entry with a scheme, path, query, wildcard or userinfo, or one
that is not a host or `host:port`; a non-numeric or fractional number; a
`jitter_percent` above 100; or a delay that is not a whole number of seconds
of at least 1.

**Live effect.** The sink and the forwarder read these settings on every call,
so a change applies to the next event with no daemon restart.

### Identity backups setting (`identity.backups_to_keep`)

The daemon keeps timestamped backups of the operator identity file and refuses a
write that would empty a saved identity. One key sets how many backups are kept.

How it works, for someone reading the code:

- `internal/identity` owns the file. Every write (`Set`, `Update`, `Reset`,
  `Restore`) runs under one lock: it re-reads `identity.yaml` from disk, so an
  edit made outside the daemon is merged rather than overwritten, copies the
  existing file to `identity-backups/identity-<UTC timestamp>.yaml` next to it,
  prunes to the retention limit, then writes the new content through a temp file,
  fsync and rename. A file that is missing, empty, or equal to the newest backup
  (ignoring `updated_at`) is not backed up. If the backup fails the write is
  aborted.
- `Set` returns `ErrEmptyOverwrite` when the new identity is empty and the saved
  one is not. The server turns that into HTTP 409 with the code
  `identity_wipe_refused` and the fields kept; `Reset` is the only way to clear.
- The routes are `GET /api/identity/backups`, `POST /api/identity/restore` and
  `POST /api/identity/reset` (`internal/server/identity.go`); the MCP tools are
  `identity_backups`, `restore_identity` and `reset_identity`; the CLI is
  `datawatch identity backups|restore|reset`; chat has `identity backups` and
  `identity restore` and deliberately no reset.
- The retention limit is read on every write through a provider, so a change to
  the key applies to the next write with no restart. A missing provider, or a
  value below 1 at that point, reads as the default, so the protection cannot be
  switched off.

| Method | How |
|--------|-----|
| **YAML** | `~/.datawatch/config.yaml` → `identity.backups_to_keep` (see `docs/config-reference.yaml`) |
| **CLI** | `datawatch config set identity.backups_to_keep 20` |
| **Web UI** | Settings → Automata → **Identity** card → *Identity backups to keep* |
| **REST API** | `PUT /api/config` with `{"identity.backups_to_keep": 20}`; `GET /api/config` returns it under `identity.backups_to_keep` |
| **MCP** | `config_set` with key `identity.backups_to_keep` |
| **Comm channel** | `configure identity.backups_to_keep=20` |

**Key.**

| Key | Type | Default | Meaning |
|-----|------|---------|---------|
| `identity.backups_to_keep` | integer | `10` | How many identity backups are kept; the oldest beyond this are removed at the next write. |

**Validation.** A whole number of 1 or more. Through `PUT /api/config` a value
below 1, a fraction, or anything that is not a JSON number is rejected with
HTTP 400 naming the key, before anything is applied. A value below 1 in the YAML
file makes the configuration fail to load with an error naming the key. An
absent key reads as 10 and `GET /api/config` shows 10.

**Live effect.** The next identity write uses the new number; no daemon restart
is needed.

**Live effect.** The next UI prompt dismissal uses the new values; no daemon
restart is needed.

### UI prompt handling settings (`session.dismiss_ui_prompts`, `session.ui_prompt_*`)

Four keys size the daemon's dismissal of a session's UI prompt. None of the
values is hard-coded: they are the only source, they are read live from the
daemon config on every use, and a change from any surface applies immediately
with no daemon restart.

How it works, for someone reading the code:

- `internal/config/config.go` carries the four keys as pointer fields in
  `SessionConfig` (`DismissUIPrompts *bool`, `UIPromptDismissDelaySeconds`,
  `UIPromptClearBoundSeconds`, `UIPromptMaxAttempts *int`), so an unset key
  and an explicit value — including an explicit `false` or `0` — are distinct
  and both survive a YAML round trip and a `PUT /api/config` write. The
  `Effective*()` helpers resolve unset to the defaults `true`, `5`, `30`, `2`
  (named `DefaultUIPrompt*` constants).
- `internal/server/api.go`: `GET /api/config` returns the four effective
  values under `session`; `PUT /api/config` accepts the four dot-path keys
  and, before anything is applied, a pre-validation step rejects a wrong
  type (a string where a boolean or number is expected, a fraction, a
  boolean or null where a whole number is expected), a zero or a negative
  value where the key requires at least 1, with HTTP 400 naming the key.
  `session.ui_prompt_dismiss_delay_seconds` allows `0` (send at once); the
  other two numbers and the boolean do not take zero. Validation rules live
  once in `internal/config` (`ValidateUIPrompt*`), shared with the
  config-load path.
- `internal/session/ui_prompt_settings.go` defines `UIPromptSettings` and
  `Manager.SetUIPromptSettings(func() UIPromptSettings)`: the getter is
  read on every call, and with no getter the defaults apply.
- `cmd/datawatch/main.go` wires the getter to re-read `cfg.Session`
  effectively on every dismissal, so a write from any surface takes effect
  with the next dismissal, no restart.

| Method | How |
|--------|-----|
| **YAML** | `~/.datawatch/config.yaml` → `session.dismiss_ui_prompts`, `session.ui_prompt_dismiss_delay_seconds`, `session.ui_prompt_clear_bound_seconds`, `session.ui_prompt_max_attempts` (see `docs/config-reference.yaml`) |
| **CLI** | `datawatch config set session.dismiss_ui_prompts true\|false`, `datawatch config set session.ui_prompt_dismiss_delay_seconds 60`, `datawatch config set session.ui_prompt_clear_bound_seconds 45`, `datawatch config set session.ui_prompt_max_attempts 3` |
| **REST API** | `PUT /api/config` with, e.g., `{"session.ui_prompt_max_attempts": 3}`; `GET /api/config` returns each under `session` with its effective value |
| **MCP** | `config_set` with key `session.ui_prompt_clear_bound_seconds` (or any of the four) |
| **Comm channel** | `configure session.ui_prompt_max_attempts=3` |

**Keys.**

| Key | Type | Default | Meaning |
|-----|------|---------|---------|
| `session.dismiss_ui_prompts` | boolean | `true` (when unset) | Whether dismissing UI prompts is on; an explicit `false` turns it off. |
| `session.ui_prompt_dismiss_delay_seconds` | integer | `5` (when unset) | Whole seconds to wait before sending the dismissal; `0` = send at once. |
| `session.ui_prompt_clear_bound_seconds` | integer | `30` (when unset) | Whole seconds ≥ 1; the clear bound. |
| `session.ui_prompt_max_attempts` | integer | `2` (when unset) | Attempts cap, whole number ≥ 1. |

**Validation.** Through `PUT /api/config` a bad value is rejected with HTTP
400 naming the key, before anything is applied: `dismiss_ui_prompts` must be
a real boolean (a string such as `"true"` is not — `1` in JSON is also not).
The delay must be a whole number ≥ 0 (an explicit `0` sends at once); the
bound and the attempts cap must be whole numbers ≥ 1 — a zero or a negative
value, a fraction, a string, a boolean or null is rejected for all of them.
The same rules apply when the config file loads: an out-of-range value makes
the configuration fail to load with an error naming the key. `GET /api/config`
always shows the value in effect, default or explicit.

**Live effect.** The next UI prompt dismissal uses the new values; no daemon
restart is needed.

### Dependencies

- **fsnotify** — used for interrupt-driven log file monitoring (replaces polling). Each session's `monitorOutput` goroutine watches the log file via fsnotify and reads new lines on write events, reducing CPU usage and improving latency.

### Per-Session MCP Architecture

When `channel_enabled: true`, each session gets its own dedicated MCP channel server
on a random port. This replaces the previous global MCP channel and enables true
multi-session support — each claude-code instance communicates with its own channel
server independently. The per-session servers are started automatically when a session
launches and stopped when the session ends. The `mcp.max_retries` config field controls
how many times a failed channel server connection is retried before giving up.

### MCP SSE Bind Retry (BL444)

The daemon's HTTP/SSE MCP server (`mcp.sse_enabled`) no longer aborts startup on the
first failed TCP bind. `ServeSSE` (in `internal/mcp/server.go`, via
`listenWithRetry` in `internal/mcp/sse_listen_retry.go`) tries every
`mcp.sse_host` address once, logs each initial bind failure ONCE at info level, then
retries the still-failing addresses with exponential backoff (1s, 2s, 4s, 8s, ...
capped at 15s between attempts) until every address binds or the cumulative elapsed
window of `mcp.sse_bind_timeout` (default `120s`; zero/negative also means 120s) is
exhausted. If it gives up, any listeners that did bind are closed and the last bind
error is returned; retry iterations log at debug level and the final outcome is logged
ONCE at info level. The thread-safe reader `mcp.Server.SSEListenerState()` reports
`disabled` (never attempted), `retrying`, `listening`, or `failed`.

### MCP resource subscriptions (`mcp.resource_subscriptions`)

`mcp.resource_subscriptions` (bool, default `false`; restart to apply) sets the `subscribe` flag of the resources capability in `internal/mcp/server.go` and, when on, installs the mcp-go subscribe hooks from `internal/mcp/resource_subscriptions.go`. A `resources/subscribe` on `datawatch://events/lifecycle?kinds=…&level=…&prd_id=…&task_id=…&automation_id=…&peer=…` parses the query into an `autonomous.LifecycleFilter`, validates it with `autonomous.ValidateSubscription` (the persisted-subscription rules) and registers it with `Manager.Subscribe`: one more consumer of the existing dispatcher. Each matching event passes `Manager.LifecycleEventDeliverable` (the notify-event set, then the Automation scope; the persisted-subscription engine calls the same method) and is sent as `notifications/resources/updated`. Nothing is stored; no sink type or subscription field is added. Subscriptions end on `resources/unsubscribe` or when the MCP session unregisters; at most 32 per session. Off: no hooks, `subscribe=false`, no events resource. Surfaces: YAML, `GET/PUT /api/config`, `datawatch config set`, comm `configure`, MCP `config_set`, the PWA MCP Server card (label and help in five locales).

### Peer event subscription

`internal/autonomous/lifecycle_peer_subscription.go` is the pull direction of federation. `PeerEventSubscriber.Start` checks a peer (registered, enabled, a federation peer, `autonomous:read` granted, a token, a usable URL) and starts one goroutine for it; a refused peer starts none, writes one `autonomous_lifecycle_forward_receive` audit record (`direction: read`) and raises one alert. The goroutine polls `GET <peer>/api/autonomous/events?queue=&since=&limit=` with the peer's bearer token, the forwarder's timestamped HMAC header and hop-chain header, under the forwarding timeout and the shared `webhookBackoff` curve (a 401 or 403 stops it). Accepted events are published to the Manager's dispatcher with `Peer` set from the registry name and `Forward` set, so they are never forwarded on or written to executor state. Forwarded: lifecycle events only. Not forwarded: session output, secrets, configuration. `ForwardSettings.Enabled` gates it; no setting or event kind is added.

### Data Files

All persistent data is stored in `~/.datawatch/` (or `data_dir` if overridden):

| File | Description |
|---|---|
| `config.yaml` | Daemon configuration |
| `sessions.json` | Session state and history |
| `schedule.json` | Persistent command scheduler entries |
| `commands.json` | Named reusable command library (SavedCommand records) |
| `filters.json` | Output filter rules (FilterPattern records) |
| `alerts.json` | Persistent system alert log |
| `audit.log` | Operator action log (session/config/etc. state changes) — JSON-lines |
| `access.log` | HTTP access / WS lifecycle / auth-failure log — JSON-lines |
| `audit.log.cef`, `access.log.cef` | CEF mirror of the above, written only when `audit.cef_mirror_enabled: true` |
| `logs/<hostname>-<id>.log` | Per-session output log |
| `tls/server/` | Auto-generated TLS certificate/key (when `tls_auto_generate: true`) |
| `daemon.pid` | PID file written in daemon mode |
| `daemon.log` | Daemon log in daemon mode |
| `last-shutdown.json` | Most recent shutdown cause (see Shutdown Cause capture below) |
| `identity.yaml` | Operator identity (role, goals, projects, values, focus, notes); written atomically, mode 0600 |
| `identity-backups/identity-<UTC timestamp>.yaml` | Timestamped copy of `identity.yaml` made before each write; the newest `identity.backups_to_keep` are kept (see Identity backups setting above) |

### Shutdown Cause Capture

When the daemon stops, `cmd/datawatch` records why (package `internal/shutdowncause`, file `<data_dir>/last-shutdown.json`):

- **Signal path (SIGINT/SIGTERM):** on the moment the signal is received the
  cause is captured (`Capture`, best-effort sender PID/UID/cmdline + systemd
  stop-job detection on Linux), a `shutdown cause: ...` line is printed next
  to the existing “Shutting down...” line, and the daemon tears down as
  before (`kill_sessions_on_exit`, `cancel`).
- **API / MCP / update restart paths** (these exit without a signal):
  `handleRestart` (REST) declares `via=api-restart`, the update-install path
  `via=update-restart`, and the `restart_daemon` MCP tool `via=mcp-restart`
  (`shutdowncause.MarkAPIInitiated`). The restart function in `main.go`
  captures + records the cause before `syscall.Exec` / `os.Exit`.
- **Audit entry:** every path writes one entry to `audit.log` —
  `actor="daemon"`, `action="shutdown"`, `subject` = the signal name
  (or the `via` string on non-signal paths), `details` = the full Cause
  fields (`signal`, `sender_pid`, `sender_uid`, `sender_user`, `cmdline`,
  `parent_chain`, `source`, `service_stop_job`, `time`, `via`). The write is
  synchronous, so the entry is on disk before teardown or exec/exit.
- `source` values: `signalinfo`, `procscan`, `service-manager`, `api-restart`
  (via contains “restart”), `api-stop`, `unknown`. The file is written
  atomically (temp file + rename) and read back with
  `shutdowncause.ReadLast`.
- **Startup report:** at daemon startup, right after the data dir is known
  and the logger is ready, `cmd/datawatch` calls
  `shutdowncause.PreviousShutdownLine` once and prints the previous run’s
  cause — `previous shutdown: signal <name> from <command> <pid> at <time>`
  (signal stop), `previous shutdown: <via> at <time>` (API-initiated stop),
  or `previous shutdown: unknown/crash` (record missing/unreadable). The
  record is then consumed (renamed to `last-shutdown.json.consumed`, removed
  if the rename fails) so a later crash after a clean start is never
  misreported as an already-shown stop.
- **Regression test:** `internal/shutdowncause/signal_child_test.go`
  (`TestSignalChildProcScan`) re-executes the test binary as a child
  (TestMain helper-env pattern); the child sends SIGTERM to the parent by
  the exact PID it was passed. It asserts `Signal=terminated`; on the siginfo
  path `SenderPID` must equal the child’s PID, on the /proc fallback the
  child must appear among `CandidatesFor` results with the test binary in
  its cmdline. Skips with a message on non-Linux; signals only its own
  process via the child it started.

---

## 9. Building

### Local binary

```bash
make build
# produces: ./bin/datawatch
```

Or directly with Go:

```bash
go build -o datawatch ./cmd/datawatch/
```

### Cross-compilation

```bash
make cross
# produces:
#   bin/datawatch-linux-amd64
#   bin/datawatch-linux-arm64
#   bin/datawatch-darwin-amd64
#   bin/datawatch-darwin-arm64
```

### Install to `$GOPATH/bin`

```bash
make install
# equivalent to: go install ./cmd/datawatch/
```

### Build tags

No build tags are currently required. CGO is disabled for the default build (`CGO_ENABLED=0`), which produces a fully static binary.

The future native Signal backend (Phase 4) will require `CGO_ENABLED=1` and a Rust toolchain for libsignal-ffi.

---

## 10. Local Development

### Running without a real Signal account

You can run the daemon and exercise all session management functionality without a Signal account by disabling the Signal backend and using the CLI or HTTP API directly.

**Option 1: Use `datawatch session` subcommands (no daemon needed)**

```bash
datawatch session list
datawatch session new "write a hello world program"
datawatch session tail a3f2 --lines 50
datawatch session send a3f2 "yes, continue"
```

**Option 2: Start the daemon with Signal disabled**

Remove (or comment out) the `signal.account_number` and `signal.group_id` from config. The daemon will start with only the HTTP server active:

```bash
datawatch start --config ./dev-config.yaml
```

Then drive commands via curl:

```bash
# List sessions
curl http://localhost:8080/api/sessions | jq .

# Start a new session
curl -X POST http://localhost:8080/api/command \
  -H 'Content-Type: application/json' \
  -d '{"text": "new: write hello world in Go"}'

# Get session output
curl "http://localhost:8080/api/output?id=a3f2&n=50"

# Send input to a waiting session
curl -X POST http://localhost:8080/api/command \
  -H 'Content-Type: application/json' \
  -d '{"text": "send a3f2: yes"}'
```

**Option 3: Mock signal-cli with a shell script**

Create a fake `signal-cli` that reads JSON-RPC from stdin and writes a subscription acknowledgement:

```bash
#!/bin/bash
# fake-signal-cli.sh — minimal signal-cli mock for development
while IFS= read -r line; do
  # Echo a subscribeReceive response
  echo '{"jsonrpc":"2.0","result":{},"id":1}'
done
```

Point the config at it via a wrapper that puts it on PATH before the real signal-cli.

### Using the `--config` flag

All daemon commands accept `--config` to point at a non-default config file:

```bash
datawatch start --config /tmp/dev-config.yaml
datawatch session list --config /tmp/dev-config.yaml
```

### Testing with a dedicated Signal number

The recommended approach for integration testing is to use a cheap VoIP number (e.g. Google Voice, Twilio) to register a dedicated Signal account for the daemon. Keep a test Signal group with that account and your personal number, then run the daemon against it in a tmux session on your development machine.

---

## 11. Memory Lifecycle Management (v8.30.0)

### Memory scope hierarchy

Six-layer scope hierarchy (broadest → narrowest):

```
persona-global → persona-in-project → project-shared → prd-shared → story-shared → session-local
```

`ScopeRef.Resolve()` maps a scope + IDs to `(projectDir, role, sessionID)`:

| Scope | role | sessionID |
|---|---|---|
| `project-shared` | `""` | `""` |
| `prd-shared` | `"prd/<prdID>"` | `""` |
| `story-shared` | `"story/<storyID>"` | `""` |
| `session-local` | `""` | session ID |

SQLite deduplication is per `(projectDir, content_hash)` — same content in the same project only stores once regardless of role.

### New REST endpoints (v8.30.0)

#### Archive import

```
POST /api/memory/scopes/archive-import
```

Seeds memories from archived breadcrumbs into a target Automaton's `prd-shared` scope. Optional `role_filter`, `max`, `dry_run` params.

Request body:
```json
{
  "target_scope": {"scope": "prd-shared", "project": "/path", "prd_id": "abc"},
  "source_project": "/path",
  "role_filter": "archived",
  "max": 50,
  "dry_run": false
}
```

#### Scope inventory

```
GET /api/memory/scopes/inventory?project=/path/to/project
```

Returns all role/session_id groups with row counts for a given project directory.

Response:
```json
{
  "project": "/path/to/project",
  "scopes": [
    {"role": "prd/abc123", "count": 12},
    {"role": "story/s01", "count": 4},
    {"role": "", "session_id": "sess-xyz", "count": 7}
  ],
  "count": 3
}
```

#### Automaton memory report

```
GET /api/autonomous/prds/{id}/memory-report?include_scopes=prd,story,session&max_per_scope=50
```

Aggregates and deduplicates all memories for a completed Automaton across all configured scopes. Returns grouped results with total deduplicated count.

Response:
```json
{
  "prd_id": "abc",
  "total": 18,
  "scopes": {
    "prd-shared": [...],
    "story-shared": [...],
    "session-local": [...]
  }
}
```

### New MCP tools (v8.30.0)

| Tool | Description |
|---|---|
| `memory_archive_import` | Import archived memories into a target Automaton scope; supports `dry_run` |
| `memory_handoff` | Write a structured handoff summary to `story-shared` for the next task |
| `memory_prd_report` | Aggregate all memories for an Automaton across scopes (wraps REST endpoint) |
| `memory_scope_inventory` | Discover which scopes have data for a project, with row counts |

### Internal packages

- **`internal/memory/scopes.go`** — `PurgeScope`, `ArchiveScope`, `SweepScopedByAge`
- **`internal/memory/backend.go`** — `InventoryBackend` optional interface + `ScopeInventoryEntry`
- **`internal/memory/store.go`** — `Store.Inventory()` GROUP BY SQL; `.UTC()` fix on all `Prune*` calls
- **`internal/server/memory_scopes.go`** — `archive-import` and `inventory` REST handlers
- **`internal/server/autonomous.go`** — `memory-report` route + `handlePRDMemoryReport`
- **`internal/mcp/memory_scopes.go`** — all four new MCP tools

## Session idle timeout (autonomous.session_idle_timeout_seconds)

- **Field:** `autonomous.session_idle_timeout_seconds`, default `900` (15 minutes). Bounds one autonomous worker or verifier session that sits in `waiting_input` or `idle` with no live child process.
- **Surfaces:**
  - config YAML (`internal/config/config.go`, `Autonomous.SessionIdleTimeoutSeconds`);
  - REST `GET` / `PUT` on the `autonomous.*` config (`internal/server/api.go`);
  - MCP `autonomous_config_set` (`internal/mcp/autonomous.go`, `session_idle_timeout_seconds` parameter);
  - web settings card (`internal/server/web/app.js`) with strings in 5 locales (en, de, es, fr, ja).
- **Default:** `SessionIdleTimeoutDefaultSeconds = 900` and `NormalizeSessionIdleTimeoutSeconds` in `internal/config/session_idle_timeout.go`. Zero or negative values normalize to the default.
- **Detector hook points:**
  - `cmd/datawatch/main.go`, the worker wait loop, for task sessions;
  - `cmd/datawatch/session_judgment.go`, the verifier one-shot session.
- **Failure path:** a tripped bound is reported as the infra failure `session idle without completion`, raises an alert, and is retried through the existing `auto_fix_retries` budget.

### Verifier headroom store and the two cap gates (autonomous.verifier_min_free_mem_mb, autonomous.verifier_headroom_max_age_hours)

A compute node runs one verifier by default and needs no measurement for that. A verifier count above 1 is accepted only while the node has a fresh memory-headroom measurement that supports it. One in-memory store and one decision serve both places that check it.

- **Store.** `compute.HeadroomStore` (`internal/compute/headroom.go`) holds the latest `compute.HeadroomMeasurement` per node: free MB with the resident model loaded, MB one extra parallel request needs, the resident model and the time measured. It is memory-only by design; a daemon restart clears it and every node falls back to one verifier until it is measured again. `newHeadroomWiring` (`cmd/datawatch/headroom_wiring.go`) creates the single store the daemon owns, and `SetHeadroomStore` hands that pointer to the REST server. Every other surface (MCP, CLI, comm channel) proxies to the REST routes, so a measurement taken on any surface lands in the one store both gates read.
- **Decision.** `compute.CapCoverage` (`internal/compute/headroom.go`) refuses a cap above 1 when there is no measurement (`no fresh measurement`), the measurement is older than the age (`measurement stale`), or `free - minimum free` does not cover `(cap - 1) x per-request MB` (`insufficient free memory`, with the shortfall). A cap of 1 or less always passes. `compute.ResolveVerifierCap` and `compute.CheckVerifierSlotsWrite` (`internal/compute/verifier_cap_gate.go`) wrap it for the two gates; neither gate has a rule of its own.
- **Write gate.** Saving a node with `verifier_slots` above 1 over REST (`internal/server/compute.go`) calls `CheckVerifierSlotsWrite`; the MCP node tools (`internal/mcp/compute.go`) run the same check before they proxy the write; a refusal is returned as the `CapRefusal` text naming the node, the measured free MB and the shortfall, and the node is left unchanged.
- **Admission gate.** `syncVerifierPools` (`cmd/datawatch/capacity_wiring.go`) resolves each node's configured count through `verifierCapGate.resolve`, which calls `ResolveVerifierCap`. A refused count is clamped to 1 for the node's verifier pool, and the refusal text is logged once per change.
- **One age and one floor.** Both gates take the free-memory floor from `AutonomousConfig.EffectiveVerifierMinFreeMB` (`autonomous.verifier_min_free_mem_mb`, default 1024, `0` turns the check off; `compute.EffectiveVerifierMinFreeMB` holds the only default) and the freshness age from `AutonomousConfig.EffectiveVerifierHeadroomMaxAge` (`autonomous.verifier_headroom_max_age_hours`, default 24, `0` or unset reads 24). The admission gate reads them on each sync and the write gate on each save, so a config change reaches both with no restart.
- **Measurement.** `compute.Measure` (`internal/compute/headroom_probe.go`) refuses a node that holds any lease (`ErrNodeBusy`) and a resident or probe model outside the allow-list (`ErrModelNotAllowed`). The on-demand service `measureNodeHeadroom` (`internal/server/compute_headroom.go`) builds that allow-list from the models the node serves that the model-pull consent allows, ordered by `compute.SortSmallestFirst`. That function receives bare model names, so the order is the size parsed from the model tag, then the name, with models whose tag carries no size last; node-listed sizes are not available to it. The first name is the probe model. REST (`POST` and `GET /api/compute/nodes/{name}/headroom`), MCP (`compute_node_headroom_measure`, `compute_node_headroom_get`), the CLI (`datawatch compute headroom measure|get <node>`), the comm channel (`compute headroom [measure] <node>`) and the PWA node detail all call that one service. The daemon also runs `verifierHeadroomMeasurer` (`cmd/datawatch/verifier_headroom_measure.go`), which re-measures an idle node whose configured count is above 1 when its stored measurement is older than a few minutes.
- **View.** `headroomView` (`internal/server/compute_headroom.go`) adds `age_seconds`, `fresh`, `min_free_mb`, `max_age_hours` and `supported_cap` to a stored measurement using the same two accessors. The operator guide is [howto/compute-nodes.md → Raising verifiers above one](howto/compute-nodes.md#raising-verifiers-above-one-the-measured-headroom-check).

### Verifier no-progress watchdog (autonomous.verifier_no_progress_timeout_seconds)

- **Field:** `autonomous.verifier_no_progress_timeout_seconds`, default `900` (15 minutes); zero or negative normalizes to the default (`NormalizeVerifierNoProgressTimeoutSeconds` in `internal/config/verifier_no_progress_timeout.go`). Surfaces mirror the session idle timeout: config YAML, REST `GET`/`PUT`, MCP `autonomous_config_set`, the web settings card (5 locales, label key `settings_verifier_no_progress_timeout`).
- **Confirmed answers (operator, 2026-10-24):** a live runner process **or** a held capacity slot for the verifier's holder counts as progress (options B plus C); an active model-server request is deferred until a per-session request probe exists. The limit keeps its single setting and the default rose to 900 seconds. Time spent waiting for a capacity slot is excluded from the limit, and the docs say so. A trip hands off to the fallback verifier backend at once under the existing infra escalation threshold (option C, differing from the originally proposed option A).
- **Decision:** `VerifierWatchdog` (`internal/autonomous/verifier_watchdog.go`) receives one observation per poll with every available liveness signal in a `ProgressSignals` value — output-file presence, a fingerprint of the captured pane output, whether the runner process is alive, whether an active model-server request is reported, and the capacity-slot state. Progress — the output file appearing, a pane fingerprint changing, a live runner process, or a held capacity slot — resets the timer. Time spent waiting for a capacity slot is excluded from the limit, and the wait itself never trips it, so the bound measures the run rather than the queue. The active-requests signal is plumbed but not yet counted until a reliable per-session request probe exists. The decision function takes the clock as an injected parameter and never reads the wall clock, so the rule is deterministic in tests.
- **Hook point:** the poll loop in `cmd/datawatch/session_judgment.go`, verifier sessions only (`autonomous:verify:*`). A trip ends the session through the deferred kill and returns an `OneShotNoOutputError` reading `verifier no-progress watchdog: no output file or pane activity for 15m0s`, which classifies as infra like the timeout — so a session that keeps producing output is never ended, and repeated watchdog trips on one task escalate to the fallback backend under the same rule as other infra failures.

### Verifier run cap (autonomous.verifier_max_run_seconds)

- **Scope:** the total wall-clock length of one verifier session. The no-progress watchdog ends a verifier only while it shows no sign of life; a verifier that keeps producing output — the incident case was one that ran past two hours, alive and compacting its context, holding a slot while other requests waited — was previously unbounded except by the no-progress timeout. This key is the hard cap on a single run, enforced by the verifier poll loop (see Enforcement and Status below).
- **Field:** `autonomous.verifier_max_run_seconds`, default `2700` (`VerifierRunCapDefaultSeconds` in `internal/config/verifier_run_cap.go`). Unlike the neighbouring timeout keys, `0` is a valid configured value meaning **no cap**, and `VerifierRunCapSeconds` returns the configured value unchanged: an explicit `0` is never replaced by the default. The field has no `omitempty`, so an explicit `0` round-trips as an explicit key through YAML.
- **Run-cap end:** when the run cap ends a verifier session, `runVerifierSessionWithRunCapHook` (`cmd/datawatch/verifier_run_cap_retry.go`) calls `Manager.HandleVerifierRunCap` before the retry decision. It frees the lease held under `VerifyHolder(taskID)` (only when one is held, so it is released once), then `RecordVerifierRunCap` (`internal/autonomous/verifier_run_cap_decision.go`) appends a `verifier_run_cap` decision with the note `task=<id> elapsed_seconds=<n> cap_seconds=<n> end_reason=verifier_max_run` and writes an `autonomous.verifier_run_cap` audit record. `/api/sessions/kill` takes no reason field, so the end reason `verifier_max_run` is carried on the verification result and written to the attempt (`TaskAttempt.EndReason`).
- **Validation:** `ValidateVerifierRunCapSeconds` refuses a negative value with a message that names `autonomous.verifier_max_run_seconds`. It is called from both `Load` and `LoadSecure`, so a negative value in the config file fails the load — the loader returns no config alongside the error — on either path.
- **Surfaces:** config YAML under `autonomous:`, plus every autonomous config surface. The set paths all validate before storing: the REST `PUT /api/config` handler rejects a negative or fractional value for the key (the whole PUT is rejected, so the stored cap is never silently truncated or ignored); the CLI `config set autonomous.verifier_max_run_seconds <n>` parses and validates the value locally before it reaches the daemon; the comm `configure autonomous.verifier_max_run_seconds=<n>` verb refuses a negative or non-whole value in the handler before it reaches the REST setter; the MCP `autonomous_config_set` tool carries the `verifier_max_run_seconds` argument and refuses a negative or fractional value; and the PWA settings row (`settings_verifier_max_run_seconds`, General section, with help docs linked to [howto/automaton-verifier-run-cap.md](howto/automaton-verifier-run-cap.md)) writes through the same `PUT /api/config`. The read paths are `GET /api/config` (which returns `verifier_max_run_seconds`, `0` = no cap), the CLI `config get`, the PWA settings field, and the comm channel. Note: the MCP `autonomous_config_get` response does not carry the key — it proxies `/api/autonomous/config`, and the field lives in `/api/config`.
- **Enforcement:** `VerifierRunLimit` (`internal/autonomous/verifier_run_limit.go`) is a pure clock that takes `now` and the slot state on each `Decide` call. It adds the interval since the previous observation to the elapsed total unless the slot state is `SlotWaiting`, ignores progress signals, and reports true once elapsed reaches the limit; a zero limit never trips. The poll loop in `cmd/datawatch/session_judgment.go` builds one per verifier session (`autonomous:verify:*` only) from `config.VerifierRunCapSeconds` and calls `Decide` after the no-progress watchdog check. A trip returns an `OneShotNoOutputError` with `RunCap`, `RunCapElapsed` and `RunCapSeconds` set (`internal/autonomous/one_shot_no_output.go`); the config value is read at session start.
- **Run time origin:** the accumulated run time starts at the verifier's capacity grant; intervals in which the verifier is still waiting for a slot are left out (`VerifierRunLimit.Decide` skips them).
- **Retry:** a cut run is not a failed task. `runVerifierSessionWithRunCapHook` runs the end hook, then `NextVerifierRunCapBackend` (`internal/autonomous/verifier_run_cap_retry.go`) picks the first non-blank backend after the cut one in the verifier policy order — at most one retry per verification, none for a single-backend list. `IsVerifierRunCapEnd` identifies a cap end; one that is not retried surfaces as an infra result.
- **Status:** the run cap is fully delivered: field, validation, every read/set surface, enforcement, lease release, decision record and one-time retry. Operator guidance: [operations.md — Verifier throughput](operations.md#verifier-throughput), [howto/automaton-verifier-run-cap.md](howto/automaton-verifier-run-cap.md), [howto/compute-nodes.md — Verifier slots](howto/compute-nodes.md#verifier-slots).

### Post-exit silence check (one-shot pi runs that exits without verdict)

- **Scope:** applies only to the `pi-prompt` backend family. One-shot `pi --print` runs write nothing to the session log until they finish, so the existing log-mtime stall check deliberately excludes pi-prompt (`staleWorkerAppliesTo`); a pi process that **exits without** writing a completion or a verdict was previously invisible, especially when the pane state flickers and never settles into `waiting_input`/`idle`.
- **How it works:** `ExitSilenceTracker` (`internal/autonomous/exit_silence.go`) is state-independent — it only counts silence after the runner process has stopped. The clock is advanced by a live runner child or log growth (non-zero log mtime), so a pane that has not yet launched pi, or a run still emitting output, is never flagged. `exitSilenceTick` (`cmd/datawatch/exit_silence_wait.go`) gates the tracker to the pi-prompt family and returns control to the existing detectors for every other backend.
- **Config:** reuses `autonomous.session_idle_timeout_seconds` (same default, `NormalizeSessionIdleTimeoutSeconds`) — no new config field.
- **Failure path:** the tripped bound stops the session, records the reason "pi print run exited without a completion or verdict" on the task, and re-enters the normal retry flow.

### Descendant-only stall bound (autonomous.descendant_stall_seconds)

- **Scope:** applies only to the `pi-prompt` backend family. A one-shot `pi --print` run writes nothing to the session log until it finishes, so the existing log-mtime stall check excludes that family (`staleWorkerAppliesTo`). Both the log-mtime detector and the exit-silence detector above leave one hole open: a pi run whose runner process has exited but whose work descendant — a build, a test, a long command — is still alive and shows no other sign of life. While `stale_task_seconds` is off that attempt would otherwise be polled forever with no safety net.
- **Field:** `autonomous.descendant_stall_seconds`, default `2700` (45 minutes). `DescendantStallDefaultSeconds` and `NormalizeDescendantStallSeconds` in `internal/config/descendant_stall.go`: zero or negative values normalize to the default at read time, so the detector can never be silently disabled. The value is read from the live config on every attempt start, so a change via any surface applies to the next attempt without a daemon restart.
- **Tracker:** `DescendantStallTracker` (`internal/autonomous/descendant_stall.go`) is the one silence clock. It is state-independent: it fires only when the worker's live descendant count is greater than zero. The clock is the latest of the tracker start, the previous observation, the session-log mtime and the newest regular file in the worker's work directory (`newestWorkDirMtime` in `cmd/datawatch/descendant_stall_wait.go`), so any real write resets the silence. Four liveness sources keep it honest: (1) a work descendant process is alive, (2) the session log is fresh, (3) the workdir has a fresh file, and (4) the tracker's own last-activity clock — any of these being fresh moves the clock forward and the observation returns not-stalled. A zero descendant count resets the clock to now and reports not stalled, so the detector never fires while the runner itself is still working.
- **Tick hook:** `descendantStallTick` (`cmd/datawatch/descendant_stall_wait.go`) gates the tracker to the `pi-prompt` family and returns `(false, "")` for every other backend, so the wait loop is unchanged for them. It is called from the worker wait loop in `cmd/datawatch/main.go` on the ~30-second cadence (`waitTick % 10 == 9`, the same cadence as the SSE-stall scan), and only when the descendant process tree could actually be read — a failed read makes no observation, so the silence clock does not move on an unreadable tree.
- **Identity rule:** the descendants a given attempt was awaiting (`workDescendantPIDs`, unioned across observations into `recordedDescendants`) are the only PIDs the kill path is ever allowed to signal. `endRecordedDescendants` sends `SIGTERM`, waits a 5-second grace, escalates to `SIGKILL` only for the still-live recorded PIDs, and re-verifies each PID still carries its recorded start time before signalling, so a recycled PID is never touched. No name-pattern kill, no whole-PGID sweep. A failed observation — an unreadable process tree — carries no kill authority.
- **Evidence:** on trip, `appendDescendantEvidence` records the recorded descendants (pid, start time, command) on the task as runner evidence before the kill, and afterwards records which ones ended on `SIGTERM` and which needed `SIGKILL`.
- **Failure path:** the tripped bound ends the session, records the end reason `descendant-only stall detector: <reason>` on the session, raises a `Task worker descendant-only stall` alert naming the PRD, the task and the measured reason, and returns a `DescendantStallError` that wraps `ErrWorkerStalled`. Because it wraps the same sentinel, the executor's stall retry path handles it exactly as it handles a session-idle, exit-silence or SSE stall: the attempt is retried through the existing `auto_fix_retries` budget, and once that budget is spent the task stays failed like any other failed task.
- **Surfaces:**
  - config YAML (`internal/config/config.go`, `Autonomous.DescendantStallSeconds`; documented in `docs/config-reference.yaml`);
  - REST `GET` exposes `descendant_stall_seconds` in the `autonomous.*` config read, and `PUT` accepts `autonomous.descendant_stall_seconds` with the same non-negative integer validation as `session_idle_timeout_seconds` (`internal/server/api.go`);
  - MCP `autonomous_config_set` (`internal/mcp/autonomous.go`, `descendant_stall_seconds` parameter; also round-trips through the manager config that `autonomous_config_get` serves);
  - CLI `datawatch config set autonomous.descendant_stall_seconds=<n>` (generic config-set path);
  - comm channels via `configure autonomous.descendant_stall_seconds=<n>`, which the router forwards to the REST `PUT` config endpoint;
  - web settings card (`internal/server/web/app.js`), positioned directly after the session idle timeout field, with the `settings_descendant_stall_seconds` label in 5 locales (en, de, es, fr, ja).

## Session start confirm timeout (autonomous.session_start_confirm_seconds)

- **Field:** `autonomous.session_start_confirm_seconds`, default `120` (2 minutes). Bounds a claude-code autonomous session that is still in `waiting_input` and shows no evidence of work — i.e. started but never began working.
- **Start evidence model:** `StartEvidence` (`internal/autonomous/session_start_evidence.go`) lists what counts as started. Any one of these rules out a stall:
  - an assistant response bullet (`•`) after the prompt line (`HasAssistantOutput`);
  - a tool call in the pane (`PaneShowsToolCall`): a tool-use line (`⏺ Bash(...)`), a tool result or running line (`⎿ ...`), the older `● Tool(...)` line, or a spinner line (`✻ Running…`);
  - a process the runner started since the previous observation. `runnerDescendantPIDs` feeds `childWorkStarted` in `internal/autonomous/start_tracker.go`; only new pids count, so a helper the runner started at launch and kept alive is not work;
  - a pane change since the previous tick (`PaneChangedAt`);
  - a channel event that arrived after the task was delivered.

  Task delivery alone is never evidence: `TaskDelivered` is context only.
- **Clock:** `StartTracker` (`internal/autonomous/start_tracker.go`) is the one start-confirm clock, used for a launched worker and for an adopted one. It measures the silence from the last tick with evidence of work. Before any evidence is seen, a launched worker is measured from its first silent `waiting_input` tick, and an adopted worker from its seed, or from the task's launch time when no evidence of life is known (see After adoption). Its launch and adopted modes share the one limit and differ in which evidence restarts the window. A session stalls only when that silence is strictly longer than the limit, so a worker in a long silent tool call is never ended while the pane keeps showing its tool line or spinner.
- **After adoption:** an adopted session's clock starts from its seed, the latest of its update time, last channel event and session log mtime (`SessionStartAdoptionSeed`). A worker that was live before a daemon restart is therefore judged from its last evidence of life, not from the moment it was adopted. If that evidence is already older than the limit, the first tick judges it. When no evidence of life is known, the clock starts at the task's launch time, or at adoption when no launch time is recorded.
- **Busy probe:** `paneLooksBusy` (`internal/session/busy_probe.go`) keeps a claude-code worker in `running` while its pane shows work: the `esc to interrupt` status bar, a `⎿ …` running line or a `✻ Verb…` spinner line in the last 15 non-empty lines, or a live pane child process when no idle prompt (`❯`, `$`, `%`) is visible. `DeliverOneShotTask` runs the probe once the task is delivered. The channel state watcher runs it in two places: a delivered one-shot in `waiting_input` whose pane is busy returns to `running`, and a `running` session that the gap check would flip is kept `running`. The probe never changes terminal or rate-limited states, and it governs only the claude-code family.
- **Surfaces:**
  - config YAML (`internal/config/config.go`, `Autonomous.SessionStartConfirmSeconds`);
  - REST `GET` / `PUT` on the `autonomous.*` config (`internal/server/api.go`);
  - MCP `autonomous_config_set` (`internal/mcp/autonomous.go`, `session_start_confirm_seconds` parameter; also round-trips through the manager config that `autonomous_config_get` serves);
  - web settings card (`internal/server/web/app.js`) with strings in 5 locales (en, de, es, fr, ja);
  - comm channels via `configure autonomous.session_start_confirm_seconds=<n>`, which the router forwards to the REST `PUT` config endpoint.
- **Default:** `SessionStartConfirmDefaultSeconds = 120` and `NormalizeSessionStartConfirmSeconds` in `internal/config/session_start_confirm.go`. Zero or negative values normalize to the default.
- **Failure path:** a tripped bound is reported as the infra failure `session never started`; the alert carries the last 20 lines of the session transcript as evidence, and the same 20 lines are stored on the task as `start_failure_tail`. The session end reason reads `session start detector: <state> without assistant output for <n>s since its last evidence of work (or first silent tick), limit <n>s`. Up to 2 start failures are retried (start-failure budget) before the task stays failed. This budget is separate from `auto_fix_retries`.

## Script gate timeout (autonomous.script_gate_timeout_seconds)

- **Config key:** `autonomous.script_gate_timeout_seconds` (`int`, no `omitempty`, in `config.AutonomousConfig` in `internal/config/config.go`). Default `900` (`config.DefaultScriptGateTimeoutSeconds`, set in `DefaultConfig`).
- **Meaning:** caps how long a script gate may run before it is stopped. `0` means no cap. A negative value is refused at load time by `validateScriptGateTimeout` (`internal/config/script_gate.go`), with an error that names the key and says 0 means no cap.
- **Helper:** `(*AutonomousConfig).EffectiveScriptGateTimeout()` returns the cap as a `time.Duration`; `0` returns `0` (no cap).
- **Tests:** `internal/config/script_gate_timeout_test.go` (`TestScriptGateTimeoutSetting*`).

## Project gate command (autonomous.project_gate_command)

- **Config key:** `autonomous.project_gate_command` (`string`, `omitempty`, in `config.AutonomousConfig` in `internal/config/config.go`). Default empty, meaning unset. It names the project's own build-and-test command, so the gate is not tied to any language.
- **Per-project override:** `project_gate_command` on a project's policy block (`config.ProjectPolicy`, `*string`). Nil inherits the daemon value. An explicit empty string unsets it for that project. `ResolveProjectPolicy` reports the value as `EffectivePolicy.ProjectGateCommand` with its source tier.
- **Validation:** `ValidateProjectGateCommand` in `internal/config/script_gate.go`. The value is trimmed, at most `ProjectGateCommandMaxBytes` (1024) bytes, and must hold no NUL and no newline or carriage return. A refusal names the setting path plus `.project_gate_command`. It runs from `validateLoaded` for the daemon key and from `ValidateProjectPolicy` for project blocks.
- **Helper:** `(*AutonomousConfig).EffectiveProjectGateCommand(policy *ProjectPolicy)` returns the trimmed effective command for one project: the policy value when set, otherwise the daemon value. A nil receiver or policy is allowed.
- **Tests:** `internal/config/project_gate_command_test.go` (`TestProjectGateCommand*`).

## Script gate runner (final_gate tasks)

- **Where:** `internal/autonomous/script_gate.go` (the runner) and `internal/autonomous/script_gate_task.go` (the final-gate task), wired into the executor before any session is spawned. A task of kind `final_gate` is now performed by the daemon itself — no worker session, no verifier, no retry loop — and the mode allowed for that id (`llm`, `off`) no longer decides who performs the verification.
- **Resolution (strict preference; a higher step takes the whole run) — `ResolveGateCheck`:** the task's own `verification_script`, when it is a file under the project's `.datawatch/verify/` — it runs alone; else the project gate command (`projectGateCommand`: the effective policy value when the policy resolver is wired, else `Config().ProjectGateCommand`), run `sh -c` — it runs alone; else **every** other story task's script under `.datawatch/verify/`, in story order until the first failure (all the scripts it finds run, each with the time left on the shared deadline); else no checks, with a reason saying nothing runnable was declared.
- **Path safety:** `isGateVerifyScript` judges a script reference on its text (no absolute path, no parent reference, strictly below `.datawatch/verify/`), and `gateScriptPath` additionally refuses a path — or a symlink — that resolves outside that folder. A gate command is operator configuration and runs as configured.
- **Execution — `RunScriptGate`:** scripts via `bash`, commands via `sh -c` (`gateArgv`, with the `TMUX` environment unset), each under the remaining shared deadline (`EffectiveScriptGateTimeout`, i.e. `autonomous.script_gate_timeout_seconds`); on timeout the process group is killed (`configureGateProcessGroup`, `gateWaitDelay`) so no child outlives the gate, and output is captured capped. `GateResult` carries the pass/fail, exit code, timeout flag and a reason naming the cause on every failure.
- **Outcome — `finishScriptGateTask`:** every check's output and the final verdict are written to the task's `report_path` (`.datawatch/reports/final-gate.txt` when the task names none or its path fails `validateReportPath`; `writeGateReport` creates the folder). A clean pass completes the task; a non-zero exit, a timeout, a failed launch, or a gate with no runnable check fails it, the reason lands on the task, and a `script_gate` decision note records what the daemon did. A final gate the daemon cannot run is a failed task, not an unclosed story; `off` removes the gate itself.
- **Docs:** [script-gate how-to section](howto/automaton-decompose-lint.md#the-script-gate-the-final-gate-no-longer-spends-a-session), [operations → standard task modes](operations.md#standard-task-modes-autonomoustask_templatesmode), the [final-gate bullets of the autonomous-planning how-to](howto/autonomous-planning.md) and the [Task templates and Template mode glossary entries](datawatch-definitions.md#decompose-lint).

## Lane fill (autonomous.lane_fill and the per-Automaton lane preference)

- **Config block:** `autonomous.lane_fill` (`config.AutonomousLaneFillConfig` in `internal/config/config.go`, bridged by `LaneFillConfigFromYAML` in `internal/autonomous/lane_fill_config.go`). Keys: `mode` (`off`, `shadow`, `on`; empty means shadow), `gpu_lanes` (default 2), `claude_lanes` (default 3), `idle_alert_minutes` (default 30, 0 = off), `decompose_ahead_min` (default 2), `poll_seconds` (default 60, minimum 5), `gpu_backend` (default `pi-prompt`), `gpu_model` (empty), `claude_backend` (default `claude-code`), `count_verifier_wait` (default true). The `Effective*` helpers apply the defaults; validation is in `validate()` and `ValidateLaneFillMode`.
- **Field: `count_verifier_wait`:** `config.AutonomousLaneFillConfig.CountVerifierWait` in `internal/config/config.go`, a `*bool` with `omitempty` — the lone pointer in the block, so an absent key and an unset (nil) value both read as true via `EffectiveCountVerifierWait` (in `internal/config/config.go`, forwarded through `LaneFillConfig` in `internal/autonomous/lane_fill_config.go`) while an explicit false survives a round trip. When true, a Claude lane whose occupant is an Automaton that is blocked only while waiting on a verifier slot (`WaitingOnVerifier` on the lane occupant, filled in by `internal/autonomous/lane_fill_verifier_wait.go`) counts as available within the Claude lane count: `laneFree` in `internal/autonomous/lane_fill.go` reports such a lane as free to the planner, and the status view shows it with state `waiting_verifier`. When false the lane stays busy, as before; GPU lanes never free under the rule. The mode still decides whether anything starts — shadow starts nothing.
- **Fields: eligibility, lane counts and per-lane-kind mode (seven keys):** all in `config.AutonomousLaneFillConfig` (`internal/config/config.go`), forwarded through `LaneFillConfig` in `internal/autonomous/lane_fill_config.go`, and reachable over `GET/PUT /api/config`, `GET/PUT /api/autonomous/config`, MCP `autonomous_config_set`, comm `autonomous config-set`, CLI `autonomous config-set` and the PWA Automata settings card. Five are pointers with `omitempty`, so an absent key reads as the default while an explicit `false` or `0` survives a round trip; two are plain strings.
  - `autonomous.lane_fill.stale_after_hours` (`*int`, default 72, `EffectiveStaleAfterHours`): a queued Automaton older than this many hours is refused as `stale`. An explicit 0 turns the rule off; a negative value fails validation.
  - `autonomous.lane_fill.require_confirmed_proposal` (`*bool`, default true, `EffectiveRequireConfirmedProposal`): an implementation Automaton must have a proposal file carrying a `Confirmed: yes` line; otherwise it is refused as `proposal_not_confirmed` or `proposal_missing`.
  - `autonomous.lane_fill.check_main_carries` (`*bool`, default true, `EffectiveCheckMainCarries`): an Automaton whose branch work is already on the base branch is refused as `main_carries_work`.
  - `autonomous.lane_fill.require_worktree` (`*bool`, default true, `EffectiveRequireWorktree`): a recorded worktree that is missing or not registered refuses the Automaton as `worktree_invalid`.
  - `autonomous.lane_fill.lane_counts_from_caps` (`*bool`, default true, `EffectiveLaneCountsFromCaps`): the lane count follows the in-flight caps of the enabled LLM entries for the lane kind's backend (`internal/autonomous/lane_fill_caps.go`); false uses `gpu_lanes` and `claude_lanes` as configured.
  - `autonomous.lane_fill.gpu_mode` and `autonomous.lane_fill.claude_mode` (`string`, default empty, `EffectiveModeFor`): the mode for one lane kind. Empty inherits `mode`; otherwise `off`, `shadow` or `on`. Checked by `ValidateLaneFillKindMode`. A kind in `shadow` records decisions without executing them while the other kind can be `on`.
  - Refusals are `blocked` decisions carrying `eligibility_reason` (seven stable codes: `held`, `sequenced_after_unshipped`, `stale`, `proposal_not_confirmed`, `proposal_missing`, `main_carries_work`, `worktree_invalid`); `GET /api/autonomous/lane-fill` also returns `lane_counts` and `modes`.
- **Per-Automaton field:** `lane_preference` on the Automaton record (`internal/autonomous/models.go`, `LanePreference`), with `gpu`, `claude` or `any`. Empty and absent mean `any`. Normalised by `internal/autonomous/lane_preference.go`.
- **Planner:** `PlanLaneFill` in `internal/autonomous/lane_fill.go` is a pure function over a snapshot (lanes, candidates, pool evidence, time). It does no I/O and starts no goroutine.
- **Coordinator:** `laneFillPoll` in `internal/autonomous/lane_fill_coordinator.go` runs on each poll. `off` returns before anything is read. `shadow` records decisions with `executed: false`. `on` carries them out through the starter's run call, the decompose call (in the background), and, where needed, the approve and backend-change calls, which are recorded under the actor `lane_fill`.
- **Records:** decisions are kept in `lane_fill_decisions.json` in the autonomous data directory, bounded to 1000 newest first (`internal/autonomous/lane_fill_records.go`). Each new record is audited as action `lane_fill`.
- **Evidence:** `NewPoolEvidenceSource` in `internal/autonomous/lane_fill_evidence.go`, wired in `cmd/datawatch/lane_fill_wiring.go`. Counts running sessions on the GPU backend or on a pool host, reads loaded models from each pool host's Ollama `/api/ps`, caches for 5 seconds, and reports partial data with the failing host named. A failed host read blocks unless the host is degraded (holds no work per `HostHolders`, unreachable for at least `autonomous.lane_fill.host_down_after_minutes`, `*int`, default 10, `EffectiveHostDownAfterMinutes`; `0` never degrades); a degraded host is listed in `PoolEvidence.Degraded` and the status `evidence.degraded`, not in the error. A failed session list always blocks.
- **Idle alert:** `laneFillCheckAlerts` in `internal/autonomous/lane_fill_alert.go`, sent through the existing alert path at warning level.
- **Surfaces:** REST `GET /api/autonomous/lane-fill` and `POST /api/autonomous/prds/{id}/set_lane_preference` (`internal/server/lane_fill.go`); MCP `autonomous_lane_fill_status` and `autonomous_prd_set_lane_preference` (`internal/mcp/lane_fill_tools.go`, federation capability table in `internal/federation/mcp_tool_caps.go`); CLI `datawatch autonomous lane-fill` and `prd-set-lane-preference` (`cmd/datawatch/cli_lane_fill.go`); comm `autonomous lane-fill` and `set-lane-preference` (`internal/router/lane_fill_comm.go`); PWA lane card and preference select (`internal/server/web/app.js`).
- **Parity:** `cmd/datawatch/lane_fill_parity_test.go` maps each REST operation to its MCP tool, CLI subcommand and comm verb.

## Priority by kind and defect intake (autonomous.priority_by_kind, autonomous.defect_intake_automation)

- **Fields:** on `AutonomousConfig` in `internal/config/config.go`, next to `LaneFill`: `PriorityByKind` (`map[string]int`, yaml/json `priority_by_kind`) and `DefectIntakeAutomation` (`string`, yaml/json `defect_intake_automation`, `omitempty`; empty by default on purpose, so each installation names the Automaton that takes defect intake on).
- **Defaults and effective view:** constants `DefaultPriorityByKindBug = 80`, `DefaultPriorityByKindMaintenance = 70`, `DefaultPriorityByKindFeature = 20`, `DefaultPriorityByKindOther = 0`; `DefaultPriorityByKindMap()` returns a fresh copy of the shipped table (bug 80, maintenance 70, feature 20, other 0); `EffectivePriorityByKind()` on `AutonomousConfig` starts from that table and layers the operator's `PriorityByKind` entries on top — operator-supplied keys override the defaults, unknown keys are kept, and kinds left out fall back to their shipped value. The returned map is a fresh copy: neither the receiver's map nor the shipped default is mutated.
- **Round trip:** `priority_by_kind` carries no `omitempty`, for the same reason as `PlanTierMaxInflight`: an operator's explicit map edit (including clearing a kind to 0) must survive the JSON round trip through the config surfaces. `DefaultConfig()` seeds the shipped table, so an unset key reads as the defaults.
- **Tests:** table-driven in `internal/config/priority_by_kind_test.go` — defaults from `DefaultConfig`, empty effective map, operator overrides, explicit zero, unknown keys kept, non-mutation of the shipped table, YAML round trip and `Load` from file (including the default-empty `defect_intake_automation`).

## Merge pipeline source and conflict settings (autonomous.merge_pipeline)

- **Config block:** `autonomous.merge_pipeline` (`config.MergePipelineConfig` in `internal/config/merge_pipeline.go`, held as `MergePipeline` on `config.AutonomousConfig`). Keys and defaults: `file_name` (`.datawatch/merge-pipeline.yaml`, relative to the project), `source_order` (`[file, record]`; the built-in pipeline always applies after them), `fallback_on_bad_file` (`record`; also `default` or `none`), `conflict_policy` (`mechanical_and_model`; also `stop_and_ask` or `mechanical`), `conflict_model` (a LLM registry entry name; unset reads the named constant `MergePipelineDefaultConflictModel`, an explicit empty string means no model), `conflict_mode` (`shadow`, the default, runs the model and records its proposal without acting on it; `on` acts on the proposal; `config.MergeConflictModeDefault`, constants `ConflictModeShadow` and `ConflictModeOn`), `conflict_confirm_first_n` (a whole number, default 5 through `config.MergeConflictConfirmFirstNDefault`: how many of a project's first conflict resolutions wait for the operator to confirm them; an explicit 0 turns confirmation off and a negative value is refused), and `projects`, a map of per-project overrides of the same seven keys. The `Effective*` accessors take a project name and return the project value, else the global value, else the default. `Validate` rejects an absolute or escaping file name, an unknown or repeated source, and an unknown fallback or policy, naming the key. The block never adds, edits or deletes a registry entry.
- **Release settings:** the same block and the same per-project overrides carry four release keys (`config.MergePipelineConfig` fields `ReleaseSizeMap`, `MajorTrigger`, `ReleaseCadence`, `VersionScheme`). `release_size_map` maps an Automaton kind to `patch`, `minor`, `major` or `none`; the built-in map (`MergePipelineDefaultReleaseSizeMap`) is `feature` = minor and `bug` = patch, and a project or global entry adds to or overrides it (`EffectiveReleaseSizeMap` returns a copy). A `major` entry is accepted only because the operator wrote it; no built-in kind is major. `major_trigger` is `manual` (`MergePipelineDefaultMajorTrigger`, also `milestone` or `never`). `release_cadence` is `per_merge` (the only accepted value); `batch` and `manual` are refused with "release cadence batch is not available yet" (and the same wording for manual). `version_scheme` is `semver` (the only accepted value); any other name is refused with "version scheme X is not available yet". Each `Effective*` accessor gives the project value, else the global value, else the default. Tests: `internal/config/merge_pipeline_release_test.go`.
- **Conflict degradation:** `ResolveConflictPolicy` returns the policy to act on. When the policy needs a model and the entry is empty, missing from the registry or disabled, it returns `stop_and_ask` and no model with a plain reason; it never picks a weaker model.
- **Resolution:** `Resolver.Resolve` in `internal/mergepipeline/resolver.go` returns the definition in effect, its `Source` (`file`, `record` or `default`) and a plain-language reason. The file in the repository wins over the daemon record, the record is the fallback when no file exists, and the built-in default applies when neither does. A malformed, invalid or oversized file is returned in `Resolution.FileError` and is never passed over silently; `fallback_on_bad_file` decides whether the record, the built-in default or nothing stands in (with nothing, `Resolve` returns the error).
- **Record store:** `RecordStore` in `internal/mergepipeline/record.go` keeps one validated pipeline per project (get, set, delete, list), persisted as `<data dir>/merge-pipeline/records.json` by writing a temporary file and renaming it, like the federation group store.

- **Routes and config keys:** `internal/server/merge_pipeline.go` serves `GET /api/autonomous/merge-pipeline`, `PUT`/`DELETE /api/autonomous/merge-pipeline/record` and `POST /api/autonomous/merge-pipeline/validate`, gated with `fedCap` (`CapAutonomousRead` for the read and the check, `CapAutonomousWrite` for the record). `GET /api/config` shows the block under `autonomous.merge_pipeline` (effective values, per-project overrides under `projects`). `PUT /api/config` validates the block as a whole through `validateMergePipelinePatch` (unknown keys included) before `applyMergePipelinePatch` writes it; per-project keys are `autonomous.merge_pipeline.projects.<project>.<key>`, and `version_files` is the sixth key alongside the five above.

- **Other surfaces:** `internal/mergepipeline/view.go` writes the plain-language text of the pipeline in effect, a check and a record once, from the REST bodies; the MCP tools (`internal/mcp/merge_pipeline_tools.go`: `autonomous_merge_pipeline_get`, `_set_record`, `_validate`, plus the `merge_pipeline_*` arguments of `autonomous_config_set`), the command line (`cmd/datawatch/merge_pipeline.go`: `datawatch autonomous merge-pipeline show|set-record|clear-record|validate`) and the chat verb (`internal/router/merge_pipeline_comm.go`: `autonomous merge-pipeline`, with the six keys through `configure`) all call it, so the wording cannot drift. `set-record` and `validate` read the pipeline from a file or standard input on the command line, and take it inline (YAML or JSON) in chat.

## Infrastructure retry (autonomous.infra_retry)

- **Config block:** `autonomous.infra_retry` (`config.InfraRetryConfig` in `internal/config/infra_retry.go`, held as `InfraRetry` on `config.AutonomousConfig` in `internal/config/config.go`). Keys and defaults: `enabled` (true), `max_tries` (3), `backoff_seconds` (`[60, 300, 540]`), `categories` (all four classes), `exhausted_action` (`hold`; the other value is `count`), `retry_dependents` (true), `raise_question` (true). The three booleans are `*bool` and the two lists keep nil (unset, reads the default) apart from a non-nil empty list (explicit: no delay, or no class retried); `MarshalYAML` and `IsZero` keep an explicit empty list in the saved file. The `Effective*` accessors apply the defaults, `Validate` rejects a try limit below 1, a negative delay, an unknown class and an unknown exhausted action, naming the key; the write surfaces call it, the config load does not, so a bad hand-edited value reads through the `Effective*` accessors instead.
- **Failure classes:** `spawn_error`, `storage_write`, `session_killed_before_delivery` and `daemon_restart` (constants `InfraRetryClass*` in `internal/config/infra_retry.go`). `ClassifyInfraFailure` and `ClassifyInfraText` in `internal/autonomous/infra_failure.go` decide the class from the error type, wrapped sentinels and errno, or from fixed phrases in already-recorded text; a session that never started (`ErrSessionNeverStarted`) is never classified, because it has its own start budget.
- **Where it is read:** `Manager.infraRetryConfig` in `internal/autonomous/infra_retry.go` returns a clone of the mirror held on the autonomous manager config (`InfraRetry`, filled from `acfgIn.InfraRetry.Clone()` in `cmd/datawatch/main.go`), so a change through the config surfaces applies on the next failure with no restart. `planInfraRetry` decides whether a failure is retried and the delay; `recordInfraRetry` writes the `InfraRetryRecord` on the task (the newest 50 are kept) and an `infra_retry` decision; `waitInfraRetry` sleeps out the delay and sets the task wait reason. The executor calls them through `handleInfraFailure`; `retryAfterDaemonRestart` in `internal/autonomous/executor.go` caps a restart-caused wait at the first delay.
- **Exhaustion:** `infraRetriesExhausted` in `internal/autonomous/infra_retry_hold.go`. `count` records one ordinary failed attempt; `hold` parks the task in `review_needed` without counting a failure (`holdAfterInfraRetries`), raises one alert, and raises one operator question when `raise_question` and the operator question channel (`autonomous.questions.enabled`) are on. An answered question resolves the task through `ResolveTaskReview`; a retry or cancel made some other way withdraws the open question.
- **Dependents:** `internal/autonomous/infra_retry_dependents.go`. `infraDependentReason` marks a dependent failed behind an infrastructure failure (source `dependency-infra`), and `recoverInfraDependents` returns those dependents to pending when the root is no longer failed, cancelled, blocked or held. Both do nothing when `retry_dependents` is off.
- **Read view:** `InfraRetrySettings`, `InfraRetryTask` and `InfraRetryStatus` in `internal/autonomous/infra_retry_view.go`; the text form shared by the CLI and the comm channel is `RenderInfraRetryText` in `internal/autonomous/infra_retry_text.go`.
- **Surfaces:**
  - REST `GET /api/autonomous/infra-retries` and the `autonomous.infra_retry` block on `GET /api/config` and `PUT /api/config` (`internal/server/infra_retry.go`; `PUT` checks every key before anything is applied, with upper bounds of 1000 tries, 1000 delays and 7 days per delay);
  - MCP `autonomous_infra_retries` and the `infra_retry_*` arguments of `autonomous_config_set` (`internal/mcp/infra_retry_tools.go`);
  - CLI `datawatch autonomous infra-retries` (`cmd/datawatch/cli_infra_retry.go`);
  - comm `autonomous infra-retries` (`internal/router/infra_retry_comm.go`);
  - PWA Infrastructure retries card, task badges and settings fields (`internal/server/web/app.js`), with the `infra_retry_*` and `settings_infra_retry_*` strings in the five locale bundles (en, de, es, fr, ja).

## Model request progress (autonomous.request_watch)

The daemon observes the in-flight model request of a worker or verifier session whose model runs on a registered compute node, so a hung request can be told from a slow one. It is observation only: it makes no model call and performs no remediation (it never ends a worker, retries on another backend or unloads a model).

- **Config block:** `autonomous.request_watch` (`config.RequestWatchConfig` in `internal/config/request_watch.go`, held as `RequestWatch` on `config.AutonomousConfig` in `internal/config/config.go`). Booleans are `*bool` and the list keeps unset (nil, reads as all four options) apart from an explicit empty list (keep_waiting only), as `InfraRetryConfig` does. Every key has an `Effective*` accessor, so an unset key reads its default; `Validate` runs when the file loads.
- **Config field table:**

| Key | Type | Default | Accepted values | Accessor |
|---|---|---|---|---|
| `enabled` | `*bool` | `true` | `true`, `false` | `EffectiveEnabled` |
| `poll_seconds` | int | `30` | whole number, 1 or more | `EffectivePollSeconds` |
| `no_progress_minutes` | int | `30` | whole number, 1 or more | `EffectiveNoProgressMinutes` |
| `observe_workers` | `*bool` | `true` | `true`, `false` | `EffectiveObserveWorkers` |
| `observe_verifiers` | `*bool` | `true` | `true`, `false` | `EffectiveObserveVerifiers` |
| `use_gpu_signal` | `*bool` | `true` | `true`, `false` | `EffectiveUseGPUSignal` |
| `gpu_busy_percent` | int | `10` | whole number, 1 to 100 | `EffectiveGPUBusyPercent` |
| `unreachable_counts` | `*bool` | `true` | `true`, `false` | `EffectiveUnreachableCounts` |
| `raise_alert` | `*bool` | `true` | `true`, `false` | `EffectiveRaiseAlert` |
| `raise_question` | `*bool` | `true` | `true`, `false` | `EffectiveRaiseQuestion` |
| `remediation_options` | `[]string` | unset (all four) | `keep_waiting`, `end_worker`, `retry_other_backend`, `unload_reload_model` | `EffectiveRemediationOptions` (keep_waiting first and always present) |

- **Package:** `internal/requestwatch`. A `Source` reads one signal from a node; `OllamaSource` (`source_ollama.go`) is the only one today and sends one GET of the running-models listing per node and model, never a generate, chat or completion route. A `Tracker` (`tracker.go`) folds the samples of one session into an `Observation` (states `generating`, `idle`, `unreachable`, `unknown`) and reports when the no-progress time first reaches the threshold and when progress resumes. The `Watcher` (`watcher.go`) lists the targets, takes one node read per node and model, and fires the callbacks. `Summary` and `Explain` (`text.go`) render the plain-text wording. `BuildStatus` (`status.go`) assembles the read.
- **Targets:** `requestWatchLister` in `cmd/datawatch/request_watch_wiring.go` lists a session only when it is running or waiting for input, carries a compute node reference that resolves in the node registry, and is the worker of an in-progress task or a verifier session named for its task. The live block is read on every tick, so a change applies with no restart.
- **Alert and question:** `RaiseRequestWatchNoProgress` in `internal/autonomous/request_watch_question.go` sends one warning alert and, when `raise_question` is on, the question channel is on and at least two options are offered, raises one non-blocking question with `keep_waiting` as the default. An episode is handled once. An answer is recorded as a `request_watch_answer` decision and changes no task. An open question is withdrawn when the request recovers or the session ends.
- **Surfaces:**
  - REST `GET /api/autonomous/request-watch` with optional `session` and `automaton` (`internal/server/request_watch.go`); the block on `GET /api/config`, `PUT /api/config` and the Automata settings route (`internal/server/request_watch_config.go`; `PUT` checks every key before anything is applied);
  - MCP `autonomous_request_watch` and the `request_watch_*` arguments of `autonomous_config_set` (`internal/mcp/request_watch_tools.go`);
  - CLI `datawatch autonomous request-watch` (`cmd/datawatch/cli_request_watch.go`);
  - comm `autonomous request-watch` (`internal/router/request_watch_comm.go`);
  - web app status card on the Automata Events tab, session and task chips, and the Model request watch settings group (`internal/server/web/app.js`), with the `request_watch_*` and `settings_request_watch_*` strings in the five locale bundles (en, de, es, fr, ja).
- **How-to:** [howto/model-request-progress.md](howto/model-request-progress.md).
## Model routing (autonomous.model_routing)

- **Config block:** `autonomous.model_routing` (`config.ModelRoutingConfig` in `internal/config/model_routing.go`, held as `ModelRouting` on `config.AutonomousConfig` in `internal/config/config.go`). The booleans and numbers are pointers so an explicit `false` or `0` survives a YAML and JSON round trip; every reader goes through the `Effective*` accessors, so an unset key reads its default. `ModelRoutingConfig.Validate` runs on config load (`validateLoaded`) and on every write surface, and names the key and the valid values. Every key is read live, so a change applies with no restart.
- **Keys:**
  - `autonomous.model_routing.refusal_mode`: `refuse` (default) or `warn`, read through `EffectiveRefusalMode` and, for the pin-time check, `Manager.modelRouting` in `internal/autonomous/model_route.go`. `refuse` returns an error wrapping `ErrModelNotServed` whose text is the verdict message (it names the model and every node checked); `warn` returns the same text as a warning, stored as a `model_not_served_warning` decision (`recordModelWarning`). An unregistered node is refused in both modes (`ErrNodeUnknown`, `internal/autonomous/node_pin.go`).
  - `autonomous.model_routing.served_models_cache_seconds`: whole number >= 0, default `ModelRoutingDefaultServedModelsCacheSeconds` (300), read through `EffectiveServedModelsCacheAge`. It is the age function of `compute.ServedModelsCache` (`internal/compute/served_models.go`); a read, successful or failed, is kept for that long, `refresh` bypasses and replaces the kept answer, and `0` never keeps anything. A negative value is rejected by `Validate`.
  - `autonomous.model_routing.verifier_follows_worker_node`: boolean, default `true`, read through `EffectiveVerifierFollowsWorkerNode` and carried to the Automaton manager in `ModelRoutingSettings`. `Manager.followWorkerNode` and the pure rule `decideVerifierNode` in `internal/autonomous/verifier_node.go` place the verifier on the worker's node and exclude every other candidate, so it waits for that node; when it cannot follow it records a `verifier_node_not_followed` decision. Only a verifier holder follows, and only with two or more candidate nodes.
  - `autonomous.model_routing.unknown_node_policy`: `assume_serves` (default) or `assume_not`, read through `EffectiveUnknownNodePolicy` and passed to `compute.CheckModel` (`internal/compute/model_check.go`), which counts a node whose models could not be read as serving or not serving accordingly.
  - `autonomous.model_routing.probe_timeout_seconds`: whole number >= 1, default `ModelRoutingDefaultProbeTimeoutSeconds` (8), read through `EffectiveProbeTimeout` and used as the per-read timeout of `compute.HTTPModelLister`. `Validate` rejects anything below 1.
  - `autonomous.model_routing.alert_on_refusal`: boolean, default `true`, read through `EffectiveAlertOnRefusal`. When on, `raiseModelNotServedAlert` sends one alert titled "Automaton model not served by any node" through the Automaton alert sink: error level for a refusal, warning level in warn mode.
- **Check:** `newModelHostCheck` in `cmd/datawatch/model_route_wiring.go` builds the `autonomous.ModelHostCheckFn` the pin setters, the node pin and the served-models route share, so their answers cannot disagree. It applies only to a backend that runs on a local engine (`modelCheckEntry`); a hosted backend is not applicable. The nodes checked are the hosting LLM entry's `compute_nodes` when it lists any, otherwise every enabled registered node. Name matching (`compute.MatchesModel`) drops the provider prefix and treats an untagged name as `:latest`. `wireModelRouting` runs at daemon start, beside `wireCapacity`.
- **Node pin:** `Manager.SetTaskNode` and `Manager.SetStoryNode` (`internal/autonomous/node_pin.go`) store `PinnedNode` on the task or story after `checkNodePin`; a task's own pin wins over its story's (`effectiveNodePin`), and `acquireLease` in `internal/autonomous/capacity.go` lets only the pinned node be chosen (`ErrPinnedNodeUnavailable` when it cannot be used).
- **Surfaces:**
  - REST `GET /api/compute/served-models` with `node`, `model`, `backend` and `refresh` (`internal/server/model_routing.go`), `POST /api/autonomous/prds/{id}/set_task_node` and `set_story_node` (`internal/server/autonomous.go`; a refusal is a 400), and the `autonomous.model_routing` block on `GET /api/config` and `PUT /api/config` (`internal/server/model_routing_config.go`; `PUT` checks every key before anything is applied);
  - MCP `compute_served_models`, `autonomous_prd_set_task_node`, `autonomous_prd_set_story_node` and the `model_routing_*` arguments of `autonomous_config_set` (`internal/mcp/model_routing_tools.go`);
  - CLI `datawatch compute node served-models` and `datawatch autonomous prd-set-task-node` / `prd-set-story-node` (`cmd/datawatch/cli_model_routing.go`);
  - comm `compute node models`, `autonomous set-task-node` and `autonomous set-story-node` (`internal/router/model_routing_comm.go`);
  - PWA Served models block on each compute node row, Pin to node select on the task and story editors, the node badge, and the six settings fields (`internal/server/web/app.js`), with the `model_routing_*` and `settings_model_routing_*` strings in the five locale bundles (en, de, es, fr, ja).
- **Docs:** operator guide `docs/howto/model-routing.md`; glossary section `### Model routing` in `docs/datawatch-definitions.md`, one heading per key, which the settings fields link to; reference block in `docs/config-reference.yaml`.
- **Parity:** the REST/MCP/CLI/comm mapping is asserted in `internal/mcp/model_routing_tools_test.go`, `internal/router/model_routing_comm_test.go` and `cmd/datawatch/cli_model_routing_test.go`; mobile parity record `docs/plans/parity-issues/model-routing.md` (datawatch-app#321).

## Automation triage (autonomous.automation.triage_alert_minutes)

- **Config key:** `autonomous.automation.triage_alert_minutes` (`config.AutonomousAutomationConfig.TriageAlertMinutes` in `internal/config/config.go`). Whole minutes >= 0; unset reads `DefaultAutomationTriageAlertMinutes` (30) via `EffectiveTriageAlertMinutes`, an explicit 0 turns the triage alert off. A negative value is refused with a 400 on the write paths (`validateTriageAlertMinutes` in `internal/server/api.go`) and with an error on config load (`validateLoaded`); a non-numeric value fails to parse, and a fractional YAML number is not refused on load (the YAML decoder narrows 1.5 to 1).
- **Pass:** `triagePass` in `internal/autonomous/automation_coordinator.go`, with the record rendering in `internal/autonomous/triage_pass.go` and record store helpers in `internal/autonomous/triage_record.go`. Called from the coordinator's idle tick beside the idle check (`internal/autonomous/automation_idle.go`), it walks every Automaton: a failed or blocked Automaton gets its stored triage record filled in or refreshed (identity, the failed or blocked task with its error, the session end reason, start/retry counts, the four retry-budget categories, append-only notes); one that has left failed/blocked has its record cleared; when a record has stood for at least the live threshold the pass raises exactly one triage alert for the episode through the coordinator's alert sink and stamps `alerted_at`, so a refresh never re-raises and a cleared-and-refiled record is a new episode that alerts again. An unchanged pass skips the store write (`triageRecordUnchanged`).
- **Surfaces:**
  - REST `GET /api/autonomous/triage` with `automation_id`, `prd_id` and `status` (`failed`|`blocked`) filters; the body lists the records newest first beside the effective `alert_minutes` (`internal/server/triage.go`);
  - MCP `autonomous_triage` (`internal/mcp/triage_tools.go`), federation capability `autonomous:read` (`internal/federation/mcp_tool_caps.go`);
  - CLI `datawatch autonomous triage [--automation <id>] [--prd <id>] [--status failed|blocked] [--json]` (`cmd/datawatch/cli_triage.go`);
  - comm `autonomous triage [automation-id]` (`internal/router/triage_comm.go`);
  - PWA triage card in the Automaton detail view (`internal/server/web/app.js`), with the `triage_*` strings in the five locale bundles (en, de, es, fr, ja).
- **Parity:** the REST/MCP/CLI/comm mapping is asserted in `internal/router/triage_parity_test.go` and `cmd/datawatch/lane_fill_parity_test.go`; the triage alert sink (including the untruncated comm-body contract with the alert store) is covered in `internal/router/triage_alert_sink_test.go`.

## Automation merge pending (autonomous.automation.merge_pending_mode, merge_pending_alert_minutes)

- **Config keys:** both live on `config.AutonomousAutomationConfig` in `internal/config/config.go`.
  - `autonomous.automation.merge_pending_mode`: `off` (default), `shadow` or `on`. An unset key reads `off` via `EffectiveMergePendingMode`. On config load (`validateLoaded` → `ValidateMergePendingMode`) an empty value is accepted as unset and any other unknown word is an error; the write paths (`validateMergePendingMode` in `internal/server/api.go`: `PUT /api/config` and the Automata config object) refuse an empty string and any unknown word with a 400.
  - `autonomous.automation.merge_pending_alert_minutes`: whole minutes >= 0. Unset reads `DefaultAutomationMergePendingAlertMinutes` (30); an explicit 0 turns the alert off and survives a round trip (`MergePendingAlertMinutes` is a `*int`, read through `EffectiveMergePendingAlertMinutes`). A negative value is refused with a 400 on the write paths (`validateMergePendingAlertMinutes`) and with an error on config load.
  - Both are read live on every tick, so a change applies to the next tick with no restart. Chat `configure`, MCP `autonomous_config_set` (`automation_merge_pending_mode`, `automation_merge_pending_alert_minutes`) and the PWA Automata settings fields write the same keys.
- **Comparison:** `internal/autonomous/merge_compare.go` classifies a finished Automaton's `automaton/<id>` branch against `main` with read-only git (`rev-list`, `cherry`, `log`): `merged`, `pending`, `needs_look` (patch and subject line disagree), `empty` or `unknown` (not compared, or a git error). One comparison is bounded by `mergePendingGitTimeout` (30 seconds, `internal/autonomous/merge_pending_pass.go`) and results are cached while the branch tip and the `main` tip are unchanged.
- **Pass:** `mergePendingCheck` and its helpers (`mergeCompare`, `mergePendingRecord`, `mergePendingClear`) in `internal/autonomous/merge_pending_pass.go`, called from the coordinator's poll in `internal/autonomous/automation_coordinator.go` beside the triage pass. `off` returns before git is called. `shadow` writes shadow decisions only (`internal/autonomous/merge_shadow_record.go`, `merge_shadow_decisions.json`, bounded at 1000 newest first, an identical decision raises `repeat_count` instead of adding a row). `on` writes a `merge_pending` triage record (`internal/autonomous/triage_record.go`, with `branch` and `commits_ahead`) for a `pending` Automaton and raises one `warn` alert per episode once the record has stood for the threshold, stamping `alerted_at`; the record is cleared when the branch leaves `pending`.
- **Report (batched fetch):** `internal/autonomous/merge_safe_delete.go` builds the safe-to-delete report. It issues no command that changes anything, and keeps git work down: a fixed listing cost (one `for-each-ref` with the ahead-behind atom — falling back to a plain ref listing plus one `rev-list --count` per branch on older git — and one `worktree list`, plus one base subject log only when at least one branch is ahead), then per-branch at most two calls (cherry, branch subject log) plus one status read per safe candidate worktree, run through a bounded worker pool (`safeDeleteGitConcurrency`, 4).
- **Report (cache):** `merge_safe_delete_cache.go` front-loads that: a per-repository in-memory cache for 60 seconds (`safeDeleteCacheTTL`), cold reads single-flight (concurrent readers share one computation, or its unwrapped error), fresh reads served at once, stale reads served at once with `Stale` set and the original computation time still stamped as `ComputedAt` — plus exactly one background refresh per repository at a time, bounded by `safeDeleteRefreshTimeout` (60 s), whose failure keeps the last good report and logs one line (`[autonomous] safe-to-delete cache refresh: repo ...`).
- **Report (stats):** `merge_safe_delete_stats.go` records the counters (`Refreshes`, `RefreshFailures`, `SlowReports`, `LastDurationMs`, `LastComputedAt`, `Worktrees`) behind `SafeDeleteStats`, and a computation longer than `safeDeleteSlowWarn` (3 s) is logged exactly once with the duration, threshold and worktree count — never repository, branch or worktree names or paths. `merge_status.go` holds the merge status model, `merge_text.go` the shared CLI/comm/alert wording (including the computed-at line and the stale note, both only when the report carries them).
- **Surfaces:**
  - REST `GET /api/autonomous/merge` and `GET /api/autonomous/safe-to-delete` (`internal/server/merge.go`), GET-only, 503 while Automata are disabled;
  - MCP `autonomous_merge` and `autonomous_safe_to_delete` (`internal/mcp/merge_tools.go`), federation capability `autonomous:read` (`internal/federation/mcp_tool_caps.go`);
  - CLI `datawatch autonomous merge [--json]` and `datawatch autonomous safe-to-delete [--json]` (`cmd/datawatch/cli_merge.go`);
  - comm `autonomous merge` and `autonomous safe-to-delete` (`internal/router/merge_comm.go`), plus the alert text;
  - PWA merge badge and safe-to-delete card (`internal/server/web/app.js`), with the `merge_*`, `safe_delete_*` and `settings_automation_merge_pending_*` strings in the five locale bundles (en, de, es, fr, ja).
- **Parity:** the REST/MCP/CLI/comm mapping is asserted by `internal/mcp/merge_tools_test.go`, `internal/router/merge_comm_test.go` and `cmd/datawatch/cli_merge_test.go`; mobile parity issue datawatch-app#304 (`docs/plans/parity-issues/2026-10-29-automation-merge-pending-parity.md`). Operator reference: *Merge-pending check* in `docs/operations.md`; data flow: `docs/flow/automation-merge-pending-flow.md`.

## Dependency update (autonomous.automation.dependency_update)

Settings for the weekly Automation that updates every dependency together. Defaults are applied only in the `Effective*` accessors in `internal/config/dependency_update.go`; `Validate` refuses a bad value on config load and on every write path.

| Key | Type | Default | Allowed |
|---|---|---|---|
| `enabled` | bool | `true` (unset reads true) | true or false |
| `schedule` | string | `monday 05:00` | `weekday HH:MM` in the daemon's local time: monday..sunday (case-insensitive) then a 24-hour time |
| `max_majors` | integer | `5` | 0..20; 0 means no major updates in a run |

The coordinator checks the schedule at most once a minute. When it is due and the Automaton from the previous run is finished, it creates one Automaton from the built-in `dependency_update` template, passing `max_majors` as a template variable, and records the time and the Automaton's title as the last run in `dependency_update_state.json` in the Automata data directory, so a restart does not run the same week twice. While the previous Automaton is not finished the run is held and one note goes in the Events record; it starts once that Automaton ends. A first start never runs on its own: the next scheduled time applies.

## Repo CI watch (autonomous.automation.repo_ci_watch)

The coordinator watches the CI runs of repositories you name it to, on the
head of main and on the newest release tags. See `docs/howto/automation-repo-ci-watch.md`
for the operator view and the "Repo CI watch" section of `docs/operations.md`
for the poll's contract.

- **Config:** `autonomous.automation.repo_ci_watch` — a block inside the
  existing `autonomous.automation` section, so it rides on every
  configuration channel (PUT /api/config, MCP `autonomous_config_set`, CLI
  `config set`, comm `configure`, the PWA Settings fields). Keys and
  defaults (each with what an unset value reads, an explicit-0 meaning
  where one is legal, and the write-path refusal below the minimum):
  `mode` (off, the default, shadow or on; any other value refused), `repos`
  (absolute repository paths; empty means nothing is watched and a
  repository is never inferred from the working directory),
  `poll_seconds` (minimum 60, 300 default), `alert_minutes` (0 turns the
  alert off, explicit 0 survives a merge round trip, unset reads the 30-
  minute default), `tag_count` (0 means no tags, unset reads the 5 default),
  `hung_after_minutes` (minimum 1, 60 default), `security_workflows` (unset
  reads the default list `CodeQL, dependency-review, owasp-zap, secret-scan,
  security-scan`; an explicitly empty list means none), `delete_superseded`
  (report, the default, or delete). The per-repository infra retry budget is
  not a key here: unset reads 0, meaning no automatic reruns.
- **Poll:** the coordinator lists the workflows on the head of main and on
  the newest release tags of every watched repository on each poll, records
  each run, and classifies it (none, infra, security, policy, code_or_test,
  judging, hung). A failed run gets a judge call at most once per
  poll to decide infra versus code_or_test; the verdict is cached per run id,
  so a run is never judged twice. Shadow mode records everything and logs
  every action it would take on the record (`would_do`) without writing to
  GitHub and without raising the alert.
- **Actions (on mode only):** a hung run is cancelled at most once per run
  id; an infra-failing run is rerun at most once per run id, and only while
  the infra retry budget still has reruns left for that
  repository/ref/workflow; a failed run that a later green run
  of the same workflow on a descendant commit has superseded is named in that
  green record's superseded_run_ids the poll it is discovered in and, when
  `delete_superseded` is delete, deleted by a later poll — the poll that
  names a superseded run never deletes it. A security or a policy run is
  record-only: it is never cancelled, rerun or deleted. The per-episode alert
  (unresolved for `alert_minutes` since first seen) is raised once per record
  and never in shadow mode; `alert_minutes` 0 or an off mode disables it.
- **Surfaces:** REST read `GET /api/autonomous/repo-ci-watch` (body: mode,
  alert_minutes, repositories, last_poll_at, and the records; optional
  `repo` and `status` filters), the MCP read tool
  `autonomous_repo_ci_watch`, the CLI `datawatch autonomous repo-ci-watch`, the
  comm channel `repo-ci-watch` verb, the PWA read-only status line and the
  settings fields, and the config block itself. The federation capability
  `autonomous:read` already covers the read, so the read does not gain
  separate capabilities. The status line is hidden when the endpoint is
  unavailable, so it never displays a stale state.
- **Test:** `internal/autonomous/repo_ci_watch_*_test.go` (config validation
  and defaults, poll pass over the fixture, action phase, alert, the judge
  cache), `internal/server/repo_ci_watch_rest_test.go` (the REST read), and
  the parity tests that cover the CLI and comm verbs.

## Supervisor loop (autonomous.supervisor_loop)

The daemon's standing loop that keeps every Automaton moving: eight mechanical
steps (no model call) and three model jobs, each run recorded. See
`docs/howto/supervisor-loop.md` for the operator view and
`docs/plans/2026-11-02-supervisor-loop.md` for the plan.

- **Config:** `autonomous.supervisor_loop` — a block inside the existing
  `autonomous` section, held as flat dotted keys that each keep the full
  `supervisor_loop.` prefix (`"supervisor_loop.lane_count.max": 4`). The type is
  `config.SupervisorLoopConfig` (`internal/config/config.go`, a
  `map[string]any`); `supervisorloop.ParseSettings` (`internal/supervisorloop/settings.go`)
  reads it, an unprefixed or unknown key is reset to its default and logged,
  and `Settings.Set` validates every write. Keys: `supervisor_loop.enabled`,
  `supervisor_loop.<step>.enabled` for the eight steps
  (`close_finished_records`, `deploy_after_ship`, `retry_failed_infra`,
  `cleanup_sweep`, `apply_answers`, `lane_count`, `start_next_chain_step`,
  `run_merge_until_conflict`), `supervisor_loop.<job>.style|backend|model` for
  the three model jobs (`draft_spec`, `classify_ci_failure`, `pick_backend`),
  `validator_retries` (default 2), `escalate_after_failures` (default 2),
  `run_retention` (default 500), `archive_cancelled_after_minutes`
  (default 60), `lane_count.min` / `lane_count.max` (default
  0; max 0 means no bounds, so the lane count step moves nothing; max must be at
  least min), and the event wake block `wake.enabled` (default false),
  `wake.kinds` (a list of lifecycle event kinds, default the eleven kinds in
  `supervisorloop.DefaultWakeKinds`), `wake.coalesce_seconds` (0 to 3600,
  default 30) and `wake.fallback_interval_minutes` (1 to 1440, default 20,
  the scheduled check's interval). Over `PUT /api/config`, the CLI `config set` and the comm
  `configure` verb the key is `autonomous.supervisor_loop.<key>` without the
  second prefix.
- **Wake settings:** `internal/supervisorloop/wake_settings.go` holds the
  `WakeSettings` block (`Settings.Wake`), its keys, defaults, bounds and
  `Wakes(kind)`. `wake.kinds` accepts a `[]string`, a JSON list, or a comma-,
  newline- or space-separated string, so YAML, REST, the CLI, the comm channel
  and the PWA textarea all reach the same list; blanks drop out, duplicates
  and unknown kinds are refused, and the list may not be empty while the wake
  is on. The kind check is injected: the package cannot import `autonomous`,
  so `internal/autonomous/supervisor_loop_wake_kinds.go` installs
  `IsKnownEventKind` through `supervisorloop.SetEventKindChecker` at start-up,
  and with no checker only blanks are refused. `Settings.Get(key)` reads one
  value as `Flat` lists it. Tests: `internal/supervisorloop/wake_settings_test.go`
  (defaults, off by default, bounds, unknown-kind rejection, the accepted
  list forms) and `internal/autonomous/supervisor_loop_wake_kinds_test.go`
  (every default kind is a registered, subscribable kind).
- **Engine:** `internal/supervisorloop` holds the engine, the validators, the
  model jobs (`jobs_model.go`), the eval harness (`eval.go`), the run store
  (`store.go`, `supervisor_loop_runs.jsonl` in the data directory, trimmed to
  `run_retention`) and the stats snapshot (`stats.go`). The mechanical steps
  live in `internal/autonomous/supervisor_loop_steps.go` and run on the
  coordinator's idle tick. An empty backend and model use the decision
  assistant's; with none configured a model job escalates to the operator and
  never falls back to a hosted model.
- **Close step (archive grace + branch guard):** `closeFinishedRecords`
  (`internal/autonomous/supervisor_loop_steps.go`) runs first every pass and
  walks terminal-state PRDs. `completed` and `rejected` are archived on the
  same pass. `cancelled` are held while `now - lastCancel` is below
  `archive_cancelled_after_minutes` (default 60; 0 archives at once), where
  `lastCancel` is the timestamp of the record's latest `cancel` decision
  (falling back to `UpdatedAt`), so a re-cancel restarts the grace. A held
  cancelled PRD additionally fails `archiveKeepsBranch` (`internal/autonomous/supervisor_loop_archive_branch.go`) while its branch is present and holds commits ahead of its base branch (`Git.BaseBranch`, default `main`); the guard is read-only (`for-each-ref` plus
  `CompareBranchToMain`) and conservative — no base repository, a git error,
  or an unknown comparison keeps the record — only a missing branch, no
  recorded branch, or zero commits ahead lets it go. The loop archives
  through `Manager.Archive` under the actor `supervisor-loop` (the
  `supervisorLoopActor` constant); the operator Archive path is unchanged and
  records under the actor `operator`. The step's run detail reports closed /
  held-in-grace / kept-for-branch counts.
- **Surfaces:** REST `GET /api/supervisor-loop/jobs|runs|stats`,
  `POST /api/supervisor-loop/jobs/{name}/run` and `POST /api/supervisor-loop/eval`
  (`internal/server/supervisor_loop.go`, federation capability
  `autonomous:read` for reads and `autonomous:run` for runs); MCP
  `supervisor_loop_jobs|runs|run_job|eval|stats`
  (`internal/mcp/supervisor_loop_tools.go`); CLI `datawatch supervisor-loop
  jobs|runs|stats|run|eval`; comm `supervisor jobs|runs|stats|run`
  (`internal/router/supervisor_loop_comm.go`); the PWA card on the Automation
  view and the settings group.
- **Observability:** `SystemStats.SupervisorLoop*` (`internal/stats/collector.go`),
  the Monitor tab's Supervisor loop card, `GET /api/supervisor-loop/stats`, the MCP
  tool `supervisor_loop_stats` and the Prometheus gauges
  `datawatch_supervisor_loop_runs{outcome}`,
  `datawatch_supervisor_loop_jobs_enabled` and
  `datawatch_supervisor_loop_last_run_age_seconds`.

## Supervisor watch (autonomous.automation.supervisor_watch)

The coordinator runs the daemon-side version of the supervisor script on every
poll, with no model call. See `docs/howto/automation-supervisor-watch.md` for
the operator view and the "Supervisor watch" section of `docs/operations.md`.

- **Config:** `autonomous.automation.supervisor_watch` — a block inside the
  existing `autonomous.automation` section, so it rides on every
  configuration channel (PUT /api/config, the generic MCP `config_set`, CLI
  `config set`, comm `configure`, the PWA Settings fields). The type is
  `autonomous.SupervisorWatchConfig` (`internal/autonomous/supervisor_watch_config.go`),
  mirrored by `config.SupervisorWatchConfig` in `internal/config/config.go`.
  Eight keys: `mode` (off, shadow or on, case-insensitive; an unset or empty
  mode reads shadow through the `EffectiveMode()` accessor, the one place the
  default is applied; any other value is refused on every write path),
  `alert_minutes` (a pointer on purpose: unset reads the shared 30-minute
  triage default, an explicit 0 turns the alert off and survives a merge round
  trip, negative refused), `silent_minutes` (a pointer: unset reads 60, an
  explicit 0 is legal, negative refused), `poll_seconds` (unset or 0 reads 60,
  an explicit value below 60 refused), `script_output_path` (the file the
  script writes with `--record`; empty means no comparison),
  `tmux_restart_baseline` (empty means `tmux_restart` reports nothing),
  `main_checkout` (empty turns `main_checkout_dirty` off) and `working_model`
  (empty turns `model_not_resident` off; there is no default working model).
- **Pass:** one pass per coordinator poll, at most once a minute
  (`supervisor_watch_pass.go`), over three groups of pure checks that read
  state already observed and call no model: the host checks over a probe
  interface (`supervisor_watch_host.go` — the real adapters shell out to
  systemctl, git and the node model report; every probe error reads as
  unknown and raises nothing), the work checks over the Automaton list, the
  lane fill status and the repo CI watch records (`supervisor_watch_work.go`),
  and the worktree checks over a filesystem probe
  (`supervisor_watch_worktree.go`). Findings are keyed by kind plus subject in
  `supervisor_watch_record.go` (`<data-dir>/autonomous/supervisor_watch.jsonl`);
  a finding whose check stops reporting it is resolved, and re-opened in place
  with its first-seen time. Operator wording lives in one place
  (`supervisor_watch_text.go`).
- **Modes:** shadow records everything, marks a finding open for the alert time
  as would-alert and raises nothing; on raises one warning alert per episode
  and stamps it on the finding. The triage-record note for an
  Automaton-scoped finding is written in shadow and on alike (text on an
  existing record, no state change); off does nothing. The watch is
  report-only in every mode.
- **Comparison:** with `script_output_path` set each pass compares its own
  kind-and-subject set with the script record (`supervisor_watch_compare.go`);
  an unavailable (missing, unreadable or stale) record records nothing. The
  last 500 comparisons and a per-day tally are kept in
  `supervisor_watch_comparisons.json`.
- **Surfaces:** REST read `GET /api/autonomous/supervisor-watch`
  (`internal/server/supervisor_watch.go`), the MCP read tool
  `supervisor_watch_list` (`internal/mcp/supervisor_watch_tools.go`), the CLI
  `datawatch autonomous supervisor-watch`, the comm verb
  `autonomous supervisor-watch` (`internal/router/supervisor_watch_comm.go`;
  the CLI and the verb share `autonomous.RenderSupervisorWatch`), the PWA card
  on the Automation view and the eight settings fields. The federation
  capability `autonomous:read` covers the reads.
- **Monitoring:** `GET /api/autonomous/supervisor-watch/stats` (the
  `SupervisorWatchStats` numbers, derived by `SupervisorWatchStatus.Stats`),
  the MCP tool `supervisor_watch_stats`, the `supervisor_watch_*` fields of
  `SystemStats` filled in `SetAutonomousStatsFunc`, the Monitor card in
  `renderStatsData()` and the Prometheus gauges
  `datawatch_supervisor_watch_findings{state}`,
  `datawatch_supervisor_watch_agreed_days` and
  `datawatch_supervisor_watch_last_poll_age_seconds`.
- **Test:** `internal/autonomous/supervisor_watch_*_test.go` (every decision
  path over fakes, plus the real host and worktree adapters against a stub
  command runner, a stub node HTTP server and a temporary directory),
  `internal/config/supervisor_watch_config_test.go`,
  `internal/server/supervisor_watch*_test.go`,
  `internal/mcp/supervisor_watch_tools_test.go`,
  `internal/router/supervisor_watch_comm_test.go`, and the web tests
  `app-supervisor-watch.test.js` and `app-supervisor-watch-stats.test.js`.

## Parallel tasks in one Automaton (autonomous.parallel)

Operator view: `docs/howto/automaton-parallel-tasks.md`. Terms: "Read-only tasks", "Parallel task worktrees" and "Parallel speedup" in `docs/datawatch-definitions.md`.

- **Config:** the `autonomous.parallel` block (`config.AutonomousParallelConfig` in `internal/config/config.go`). The two toggles are pointers so an explicit `false` survives a YAML/JSON round trip and a `PUT /api/config` write; the defaults are applied only by the getters in `internal/autonomous/parallel_config.go`. The defaults are the operator's choice for this installation and other users may change them.
  - `read_only_tasks` — default `true` (`EffectiveReadOnlyTasks`). A task with `read_only` set skips the per-tree write lock (`internal/autonomous/read_only_parallel.go`).
  - `worktree_per_task` — default `true` (`EffectiveWorktreePerTask`). Each parallel task runs in its own worktree and branch (`task_worktree.go`).
  - `merge_conflict_policy` — default `additive_then_model` (`EffectiveMergeConflictPolicy`); valid `additive_then_model`, `block`, `triage`; checked by `config.ValidateMergeConflictPolicy`, which refuses an unknown value on load and on write.
  - `merge_conflict_model` — default empty (`EffectiveMergeConflictModel`); empty means the strongest configured planning model.
- **Merge back:** task branches merge into the Automaton branch in dependency order, one at a time, under the conflict policy (`internal/autonomous/task_merge.go`). The merge state (`pending`, `merged`, `conflict`, `held`) and the merge commit are fields of the task.
- **Metrics:** `internal/autonomous/parallel_metrics.go` records total and read-only task counts, concurrent tasks, wall-clock and serial seconds and the speedup when an Automaton completes; they ride on the Automaton read.
- **Surfaces:** `PUT /api/config` with the dotted keys, the MCP `autonomous_config_set` arguments `parallel_*`, the CLI and chat `configure`, the web app Settings rows (each with a definitions help link in `DOCS_LINKS_BY_KEY`), the task merge badge and the Parallel execution card.
- **Tests:** `internal/config/parallel_policy_test.go`, `internal/autonomous/parallel_config_test.go`, `parallel_metrics_test.go`, `internal/mcp/parallel_config_test.go`, `internal/router/parallel_config_surface_test.go`, `cmd/datawatch/parallel_config_cli_test.go`, and the web tests `app-parallel-task-merge.test.js` and `app-parallel-metrics.test.js`.

## Read-only tasks and report paths

Operator-facing description of what a read-only task may do and where its
results land. Governed by the existing setting
`autonomous.parallel.read_only_tasks` (default `true`; see the parallel
section above) — no new config key is introduced here.

- **A read-only task changes no files.** A task flagged `read_only` runs
  without the per-tree write lock and with no commit step. When it finishes,
  the tree-dirty check fails the task if the tree is changed at all.
- **The one allowed write: a report file.** The task may declare a
  `report_path`, which must be repo-relative, clean, and under
  `.datawatch/reports/`; it may write that single file and nothing else.
  The tree-dirty check excludes exactly that one path and still fails the
  task on any other change. An empty `report_path` is valid: the task writes
  nothing and the tree must be untouched. A `report_path` on a task that is
  not flagged `read_only` is rejected.
- **Where `report_path` is set:** the planner (task decomposition), the
  edit-task surface and the add-task surface all accept `report_path`. A
  rejected edit or add mutates nothing.
- **Verification script and exemption reason on the edit path:** the
  edit-task surface (REST `edit_task`, MCP `autonomous_prd_edit_task`, CLI
  `prd-edit-task`, comm `edit-task`, the PWA task editor) also takes
  `verification_script` and `verification_script_reason`. The script is
  validated as one clean repo-relative file that exists in the project and is
  appended to the task's generated files; the reason is stored as given and
  independent of the script. An omitted field is unchanged, and a rejected
  edit mutates nothing. See `internal/autonomous/manager.go` (the edit path)
  and [the how-to](howto/automaton-verification-scripts.md).
- **Stable generated scripts:** the planning prompt carries a shared helper
  text (`internal/autonomous/verification_script_helpers.go`): docs sync
  first, a cumulative allowed-file list, a merge-base comparison and no
  package-wide formatter run. Three block-severity lint rules enforce it
  (`verify_script_task_files_only`, `verify_script_docs_without_sync`,
  `final_gate_without_script`, via the `verification_script_task_files_only`,
  `verification_script_docs_without_sync` and `verification_script_reason_empty`
  /`kind` match predicates in `internal/autonomous/decompose_lint_verify_script.go`).
  A planner-written final gate off the last story is rejected by the plan check
  (`verification_script_coverage.go`).
- **Gate resolution across the Automaton:** the script gate
  (`ResolveGateCheckForAutomaton` in `internal/autonomous/script_gate.go`)
  now resolves, after the task's own script and the project gate command,
  every other task's script across every story, in story order, skipping
  standard-task tasks and other final gates.
- **Gate reports write counts only.** A gate that reports to a path writes
  exactly three lines, `passed=<n>`, `failed=<n>`, `skipped=<n>`, in that
  order, each followed by a newline, and no other text — never the
  gate's output text. Counts may not be negative. The parent directory is
  created as needed (mode 0755) and the report file is written mode 0644.

## Search-before-create (autonomous.automation.create_search_mode, autonomous.automation.create_search_threshold)

- **Config keys:** `autonomous.automation.create_search_mode` (string: `off`, `shadow` default, `on`) and `autonomous.automation.create_search_threshold` (number in the open-closed interval (0, 1]; unset reads `DefaultAutomationCreateSearchThreshold` (0.5)) on `config.AutonomousAutomationConfig` in `internal/config/config.go`, with the exported validators `ValidateCreateSearchMode` / `ValidateCreateSearchThreshold` and the effective getters `EffectiveCreateSearchMode` / `EffectiveCreateSearchThreshold`. A mode outside `off`/`shadow`/`on` is refused (400 on the write paths, an error on config load in `validateLoaded`); a threshold that is zero, negative, above 1, non-finite or non-numeric is refused the same way.
- **What the keys gate:** the over-duplicate pre-check that searches the existing sources (`internal/autonomous/overlap_finder.go`, a composition of the Automata lister, the registry-row source and the unmerged-branch source) before a new Automaton is created: an `OverlapWarning` is a `(kind, score, shared-term)` finding, and a finding at or above the threshold counts as an over-duplicate. `off` skips the pre-check entirely; `shadow` runs it and surfaces the over-duplicate warning to the operator while the creation still proceeds; `on` runs it and holds an over-duplicate creation back to the operator.
- **Surfaces:** the manager `Config` mirror (`internal/autonomous/automation_config.go`, effective defaults resolved by `EffectiveCreateSearch*`), REST GET/PUT `/api/config` (`internal/server/api.go` — `validateCreateSearchModeValue` / `validateCreateSearchThresholdValue`, both pre-PUT validation and `applyConfigPatch`), REST GET/PUT `/api/autonomous/config` (`internal/server/autonomous.go`), the MCP `autonomous_config_set` arguments `automation_create_search_mode` / `automation_create_search_threshold` (`internal/mcp/autonomous.go`), the config YAML bridge into the manager (`cmd/datawatch/main.go`), and the CLI/comm `config set autonomous.automation.<key>` paths that ride on the same REST PUT.
- **Tests:** `internal/config/overlap_config_test.go` (defaults, valid values, refusal on load) and `internal/server/overlap_config_test.go` (GET defaults, PUT accept, PUT refusal with 400, round-trip write→read).

## Automation cleanup keys (cleanup_mode, cleanup_retention_hours, cleanup_max_removals_per_pass, disk_alert_percent)

Four flat keys under `autonomous.automation` (no nested sub-object, no `*_enabled` companion): one mode string and three whole-number knobs for the safe-to-delete cleanup pass and its disk alert.

- `cleanup_mode` (string): `off` does not run a cleanup pass, `report` (the default) records and reports the pass only, `on` performs the removals it decides on. Stored as written (any string). An empty string and any unrecognized word both read as `report` through `EffectiveCleanupMode` — the read never errors. This is the deliberate contrast with `EffectiveMergePendingMode` and `EffectiveCreateSearchMode`: an unknown word is a legal value that resolves to the safe default instead of an error state.
- `cleanup_retention_hours` (whole hours): how long a removal candidate may sit unused before it counts as retained. Stored as written in a plain `int` (zero = unset); `v <= 0` reads the 24-hour default via `EffectiveCleanupRetentionHours`, a positive value reads back unchanged.
- `cleanup_max_removals_per_pass` (whole number): maximum removals one cleanup pass may perform. Stored as written in a plain `int`; `v <= 0` reads the 10 default via `EffectiveCleanupMaxRemovalsPerPass`, a positive value reads back unchanged.
- `disk_alert_percent` (whole percent): disk usage at or above which a disk alert is raised. Stored as written in a plain `int`; `v <= 0` reads the 85 default via `EffectiveDiskAlertPercent`, a positive value is clamped to 1..100 on read (a stored 130 reads as 100, a stored 0 reads as the 85 default).

The three whole-number keys use plain ints, not pointers: zero has exactly one meaning ("use the default"), so there is no explicit-0-off spelling and no off-sentinel to keep straight. The mode key deliberately does not reject unknown words on config load or on the write paths — the write path only checks that it holds a string — and `DefaultConfig()` leaves all four raw values unset so their effective readings come out at the defaults.

The effective accessors are defined on `config.AutonomousAutomationConfig` and mirrored on `autonomous.AutomationConfig`, the same two-layer pattern the other automation keys use. Settable via REST `GET /api/config` (the automation object shows the effective values for all four keys) and `PUT /api/config`; via the `applyConfigPatch` dotted keys `autonomous.automation.cleanup_mode`, `autonomous.automation.cleanup_retention_hours`, `autonomous.automation.cleanup_max_removals_per_pass` and `autonomous.automation.disk_alert_percent` (a non-string mode or a non-whole-number value is skipped there rather than mutating the config); via CLI `datawatch config set` and comm `configure`; and via the MCP `autonomous_config_set` tool (`automation_cleanup_mode`, `automation_cleanup_retention_hours`, `automation_cleanup_max_removals_per_pass`, `automation_disk_alert_percent`). On the daemon, `main.go`'s config-bridge copies the four keys from `config.AutonomousAutomationConfig` into `autonomous.AutomationConfig` alongside the idle/triage/merge-pending/create-search keys.

## Security failure (autonomous.automation.security_failure)

- **Setting:** `autonomous.automation.security_failure` — a block inside
  the existing `autonomous.automation` section, so it rides on every
  configuration channel (PUT /api/config, the generic MCP `config_set`
  tool, CLI `config set`, comm `configure`). The `autonomous_config_set`
  MCP tool does not carry this key. It has one key: `mode` (off, shadow or
  on, case-insensitive; any other value is refused on every write path —
  a 400 on the REST PUT and an error on config load). An unset or empty
  mode reads shadow, which is the first-release default: the feature is
  switched on in shadow mode first, with no behaviour change. The one
  default is applied in exactly one place, the `EffectiveMode()` accessor
  (`autonomous.SecurityFailureConfig` in
  `internal/autonomous/security_failure_config.go`, mirrored by
  `config.SecurityFailureConfig` in `internal/config/config.go` for the
  config-load and REST surfaces); no call site hard-codes it.
- **Relationship to the other blocks:** the security workflow set is not
  a key here — it is the workflows to watch for the security signals,
  read from `autonomous.automation.repo_ci_watch.security_workflows`
  (unset reads `CodeQL, dependency-review, owasp-zap, secret-scan,
  security-scan`), and the alert time is the existing
  `autonomous.automation.triage_alert_minutes` (unset reads the 30-minute
  default).
- **Surfaces:** the config block itself. The GET /api/config surface shows
  `security_failure.mode` as the effective value (read from the
  accessor), and the REST PUT validation and store run in
  `internal/server/api.go` (`validateSecurityFailurePatch`,
  `applySecurityFailureValue`), mirroring the repo_ci_watch block.
  The report is read on REST `GET /api/autonomous/security-failures`
  (`internal/server/security_failure.go`; optional `repo` and `status`
  filters; GET only; the `autonomous:read` capability), the MCP read tool
  `security_failure_list` (`internal/mcp/security_failure_tools.go`), the
  CLI `datawatch autonomous security-failure`
  (`cmd/datawatch/cli_security_failure.go`), the comm channel
  `security-failure [run-id]` verb (alias `security_failure`,
  `internal/router/security_failure_comm.go`) and the PWA Security failure
  card in the Automation view. The CLI and the comm verb share one
  renderer, `internal/autonomous/security_failure_render.go`.
- **Pass:** `securityFailurePass` in
  `internal/autonomous/security_failure_pass.go` runs on the coordinator's
  idle tick beside the triage and repo CI watch passes. It visits failing,
  `security`-classified repo CI watch records on the effective security
  workflow set once each, decides with `decideSecurityFailure`
  (`security_failure_decide.go`, a pure function), and stores the
  `SecurityFailureReport` on the record. In `on` mode it stops the linked
  Automaton by the executor's own blocked transition and raises one alert;
  in `shadow` it appends one would-do entry and changes nothing. It never
  reruns, cancels or deletes a run and never writes the generated ignore
  file, `docs/security-review.md` or `security/accepted-risks.yml`;
  `scripts/check-no-direct-ignore-writes.sh` (`make
  check-security-ignore-writes`, part of `make lint`) and
  `security_failure_guard_test.go` enforce the last point. Operator view:
  `docs/howto/automation-security-failure.md` and the "Security failure
  report" section of `docs/operations.md`.
- **Test:** `TestSecurityFailureConfig` — `internal/autonomous/
  security_failure_config_test.go` (unset and empty read shadow, off/
  shadow/on are accepted, any other value is refused, YAML and JSON
  round trips, the effective value comes from the accessor),
  `internal/config/security_failure_config_test.go` (the config-load
  refusal), and `internal/server/security_failure_config_test.go` (the
  REST write path: the default surface reads shadow, legal values are
  accepted, any other value is a 400 and leaves the stored value intact).

## Automation decision assistant (set_decision_assistant)

- **Setting:** `decision_assistant` on an Automation and on a member (`DecisionAssistant` in `internal/autonomous/decision_assistant.go`): `backend`, `model`, `mode` (`shadow` default, `live`), `hooks` (`empty_edge`, `skip_member`, `insert_member`, `budget_breach`) and per-hook `thresholds`. **Default: off** — an unset setting, or an empty backend, means no hook consults the assistant. A member's setting wins over the Automation's (`EffectiveDecisionAssistant`).
- **Default thresholds:** `0.9` for `empty_edge`, `skip_member` and `insert_member`, `0.95` for `budget_breach`; each is overridable per hook and must lie in `(0,1]`. Below the threshold the assistant escalates to the operator.
- **Operation:** `set_decision_assistant`, following the `set_llm` pattern. The fields replace the whole setting; `clear` (or an empty backend) turns it off. `member_id` targets one member. Allowed while the Automation is `draft`, `active`, `paused` or `blocked`; a stale `expected_version` is a `409`, an unknown member a `404`, an invalid setting a `400`. `Manager.SetDecisionAssistant` records a Decision and an `automation_set_decision_assistant` audit record.
- **Surfaces:**
  - REST `POST /api/automations/{id}/set_decision_assistant` (`internal/server/autonomous.go`, `internal/server/automation_ops.go`), audit action `automation_set_decision_assistant` with `resource_type` `automation`;
  - MCP `automation_set_decision_assistant` (`internal/mcp/automation_tools.go`), federation capability `autonomous:write` (`internal/federation/mcp_tool_caps.go`);
  - CLI `datawatch automation set-decision-assistant` (`cmd/datawatch/cli_automation.go`);
  - comm `automation set-decision-assistant <id> key=value...` (`internal/router/automation_cmd.go`).
- **Read side:** `decision_assistant` appears on `automation get` / `list` and on each `autonomous_status` Automation row (`AutomationStatusSummary`); `automation get` in the CLI prints an `Assistant:` line.
- **Parity:** `cmd/datawatch/automation_parity_test.go` lists the operation, so the REST/MCP/CLI/comm parity test covers it.

## Verifier infra escalation (autonomous.verifier_infra_escalation_threshold, autonomous.fallback_verification_backend)

- **Fields:** `autonomous.verifier_infra_escalation_threshold` (default `2`; zero or negative means the default) and `autonomous.fallback_verification_backend` (default `claude-code`; empty means the default). Normalizers: `NormalizeVerifierInfraEscalationThreshold` and `NormalizeFallbackVerificationBackend` in `internal/config/verifier_escalation.go`.
- **Surfaces:** config YAML (`Autonomous` struct), REST config GET/PUT (`internal/server/api.go`), MCP `autonomous_config_set`, the manager `Config` mirror, and the web settings card with strings in 5 locales.
- **Tracking:** `Task.VerifierInfraFailures` counts consecutive infra verifier results, persisted with the task; any non-infra result resets it.
- **Decision:** `DecideVerifierBackend` in `internal/autonomous/verifier_escalation.go` is a pure function. When the count reaches the threshold and the configured backend differs from the fallback, the next verification of that task runs on the fallback backend. The switch is recorded as a `verifier_escalation` decision on the PRD and as an `autonomous.verifier_escalation` audit event. After that run, pass or fail, the count resets; no other task and no global setting changes.
- **Hook point:** the verifier closure in `cmd/datawatch/main.go`.

## Pi worker observability (progress, heartbeat, loop detection)

How datawatch watches pi-based autonomous workers, and where the two
operator knobs live. Operator-facing behavior:
"Pi worker observability" in [`operations.md`](operations.md).

### Components

- **`internal/pilog`** — read-only reader over pi's native session log (a
  stream of JSON lines written by pi itself). `LocateLog` finds the log
  newest-for the task session under the pi sessions root; `Read` builds a
  `pilogo.Progress` snapshot (last event time, assistant-turn count,
  per-tool call counts, last tool name + compact args, last text/thinking
  summary truncated to 200 chars, token counts); `ReadTurns`/`Recent`
  expose the last N assistant turns for loop detection. datawatch never
  writes to the log and never parses the pane.
- **`cmd/datawatch/progress_refresh.go`** — `refreshTaskProgress` is the
  progress + loop-detection half: locate log → read → snapshot →
  evaluate both loop detectors. Called from the autonomous manager's wait
  loop (`cmd/datawatch/main.go`) about every 30 seconds while a pi task is
  running. Writes the snapshot to `Task.Progress` and, in addition to the
  loop warning, the `autonomous` package's live detection.
- **Heartbeat (stall) check** — `cmd/datawatch/stale_worker.go`,
  `stall_decision.go`, `stale_evidence.go`: a worker counts as stalled
  only when *every* signal of life is absent for longer than
  `autonomous.stale_task_seconds` (falling back to
  `session.stale_timeout_seconds`): no log output (log mtime *and* age of
  the newest log event), no live runner child process, no busy backend,
  no recently changed file in the task's work directory. The failure names
  that evidence (`worker stalled: no output, no children, no file changes
  for N seconds (stale_task_seconds=...)`) and wraps `ErrWorkerStalled` so
  the executor's retry path picks it up. `staleWorkerAppliesTo` exempts
  the `pi-prompt` family from the log-mtime part of the check: `pi
  --print` writes its log only on completion, so the mtime gate would
  report false stalls there.
- **`internal/autonomous/loop_detect.go`** — `DetectLoop(turns, n)`: over
  the last `n` assistant turns, fires `KindAnnounce` (a turn claims a next
  action — "now i will …", "let me create/write …", "i will now …" — but
  calls no tool) or `KindRepeat` (the identical tool + arguments repeated
  across the window). Produces a `LoopFinding` with `Warning()`
  (plain-language text recorded on the task) and a `Hint()`.
- **`internal/autonomous/loop_rescue.go`** — `RescueLoop` has three parts,
  in order: deliver the rescue (a corrective message into an input-
capable pi session via `SendInput`, or stop the one-shot `pi-prompt`
  session), record the loop warning on `Task.LoopingWarning`, and emit the
  `task.looping` lifecycle event (`KindTaskLooping`). A stopped one-shot
  run is routed into the worker's stall-retry path via `LoopRetryError`
  (wraps `ErrWorkerStalled`, hint in the error text) so the next
  attempt's spec carries the loop hint. Fires at most once per distinct
  finding: an already-rescued `(session, finding-key)` pair (`kind + named-
  action`) is reported as `skipped` without re-record, re-emit, re-send, or
  re-stop; a failed delivery consumes no fires-once opportunity, so the
  next poll retries cleanly.
- **`cmd/datawatch/progress_refresh.go` streak detector** — the always-on
  coarser warning: the same tool name + arguments unchanged across three
  consecutive ~30 s refreshes adds `looping suspected: tool … repeated
  across N consecutive progress refreshes` to the task. Evolving args
  reset the streak.
- **`internal/pilog` UI consumer** — the web progress block
  `renderPiProgressBlock` in `internal/server/web/app.js` renders
  `Task.Progress`: last-event age, assistant-turn count, tool-calls
  table, last tool, last thinking/text summary, tokens, and the loop
  warning. Pinned by `app-pi-progress.test.js` (progress keys +
  poll-stacking guard).

### Knobs

- **`pi.thinking_level`** (`PiConfig.ThinkingLevel` in
  `internal/config/config.go`, validated in `internal/config/pi_thinking_level.go`;
  `""` → no `--thinking` flag, i.e. pi's model default; `off|minimal|low|medium|high` otherwise).
  Applied to every pi worker launch (`cmd/datawatch/main.go` and
  `autonomous_effort.go` build the launch request with the global
  `pi.thinking_level`); per-task/story/PRD thinking-LLM overrides
  (`/api/autonomous/prds/…/set_thinking*`) take precedence over the
global default. Web surface: settings card *Pi (worker backend)*
  (`id: 'piworker'` in `GENERAL_CONFIG_FIELDS`, `internal/server/web/app.js`)
  with labels `settings_pi_thinking_level` in all 5 locales.
- **`autonomous.loop_detect_turns`** (`Autonomous.LoopDetectTurns *int` in
  `internal/config/config.go`; `internal/config/loop_detect_turns.go`:
  `nil` → `DefaultLoopDetectTurns = 3`, `0` → detection off, `n > 0` →
  window of n turns, negative rejected by
  `ValidateLoopDetectTurns` at every write edge). Consumed by
  `refreshTaskProgress` (`cmd/datawatch/progress_refresh.go`) as the
  `DetectLoop` turn window. Web surface: the autonomous settings card
  (`GENERAL_CONFIG_FIELDS`, `internal/server/web/app.js`) with label
  `settings_loop_detect_turns` in all 5 locales.

### Tests

- `internal/autonomous/loop_detect_test.go`, `loop_rescue_test.go` — detector and rescue semantics.
- `cmd/datawatch/progress_refresh_test.go` — refresh with `loop_detect_turns = 0` (detector off, snapshot still produced), `TestLoopDetectTurnsEffective`, `TestValidatePiThinkingLevel`.
- `internal/autonomous/thinking_level_test.go`, `internal/session/thinking_level_passthrough_test.go` — thinking-level inheritance down to the launch.
- `internal/server/loop_detect_thinking_test.go` — REST round-trips: `pi.thinking_level` and `autonomous.loop_detect_turns` accepted/validated through `PUT /api/config` (invalid levels rejected, wrong types rejected, `""` re-enables the pi default).
- `internal/server/web/app-autonomous-settings-fields.test.js` — asserts `autonomous.loop_detect_turns` renders as a number with its label key, `pi.thinking_level` renders as a select offering `""/off/minimal/low/medium/high`, and both `settings_loop_detect_turns` / `settings_pi_thinking_level` labels are non-empty in en, de, es, fr, ja.
- `internal/server/web/app-pi-progress.test.js` — the PWA progress block: the progress keys it renders and the poll-stacking guard.

## Defect queue configuration (autonomous.defect_queue)

The `autonomous.defect_queue` block holds the settings the defect queue reads
when it batches defects and when it decides a defect is a tiny fix. It is
configuration only: it defines, validates and exposes the values.

- **`internal/config/defect_queue.go`** — `DefectQueueConfig` and its parts.
  Fields are pointer or zero-valued when unset, so an absent block reads the
  built-in defaults and an explicit `false` survives a save and load.
  - `enabled` (default true).
  - `batch.size_cap` (5, at least 1), `batch.grouping_keys` (`project_area`,
    `shared_files`, `shared_surfaces`), `batch.allow_unrelated` (false),
    `batch.priority_rule` (`highest_member`, or `first_member`).
  - `tiny_fix.enabled` (true), `tiny_fix.max_files` (3, at least 1),
    `tiny_fix.allowed_path_globs` (test files, docs, scripts, lint / rule
    config), `tiny_fix.allowed_kinds` (`test_only`, `docs_only`,
    `script_only`, `rule_tweak`, `lint_tweak`) and
    `tiny_fix.operator_visible_path_globs` (`internal/server/web/**`,
    `cmd/**`, `internal/server/api.go`).
  - `projects.<name>.{batch,tiny_fix}.*` — per-project overrides of every
    batch and tiny-fix key.
- **Accessors** — `EffectiveEnabled`, `EffectiveBatch`, `EffectiveTinyFix`
  and `EffectiveForProject(project)`. Precedence is project override, then the
  block value, then the default; lists are returned as copies.
- **Setting a value** — `DefectQueueConfig.Set(name, value)` is the one setter
  behind `PUT /api/config`, the comm `configure` verb, MCP `config_set` and the
  CLI. `ValidateDefectQueueSetting(key, value)` is the same check without
  storing. Per-project keys are
  `autonomous.defect_queue.projects.<project>.<batch.* | tiny_fix.*>`; a `nil`
  value clears that override. Lists accept a JSON list or a comma-separated
  string. A refusal names the full key.
- **Load** — `loadDefectQueue` re-runs every set field through the same
  validator, so a bad `config.yaml` refuses startup with the same message.
- **Server** — `internal/server/defect_queue_config.go` renders the block for
  `GET /api/config` (effective block values plus raw project overrides) and
  checks every `autonomous.defect_queue.*` key of a `PUT /api/config` patch
  before anything is applied; `applyConfigPatch` stores each accepted key
  through `Set`. Operator reference: `docs/config-reference.yaml`.

## Shipped state (autonomous.shipped)

How datawatch records and displays the shipped state of Automata (PRDs)
and Automations, and where the five operator knobs live. Operator-facing
behavior: [`docs/howto/automata-shipped-status.md`](howto/automata-shipped-status.md)
and the "Shipped state" entry in
[`docs/datawatch-definitions.md`](datawatch-definitions.md).

Shipped state is orthogonal to the stored lifecycle status. A record
carries four fields — `merged_in_release` (the release version the work
landed in), `merged_at` (RFC 3339 merge time), `merge_commit` (the
main-branch merge commit that proves it — the idempotency key) and
`shipped_source` (`merge_step` or `backfill`) — or it carries none. A
record *is shipped* exactly when `merge_commit` is non-empty. Recording
shipped state never writes the status: a cancelled PRD whose work landed
stays cancelled and carries shipped state, and a merged-and-cancelled
Automation still reads `cancelled`. The `cancelled` and `shipped` buckets
are therefore different answers to different questions: "did you stop it?"
vs. "did the work land?". While the surface is enabled
(`autonomous.shipped`, below), a shipped record additionally *displays* as
`shipped` in effective-status views and counts under its own `shipped`
bucket while `count_separately` holds; `shipped` is a display/count
bucket, not a stored status value.

### Components

- **`internal/autonomous/shipped.go`** — the state itself: the four fields
  on the PRD and Automation records, `IsShipped()` (non-empty
  `MergeCommit`), the source constants (`ShippedSourceMergeStep`,
  `ShippedSourceBackfill`) and `RecordPRDShipped` /
  `RecordAutomationShipped` setters, which validate the source, accept the
  three facts (release, time, commit) and never touch `Status`.
- **`internal/autonomous/shipped_counts.go`** — display and count
  derivation: `PRDEffectiveDisplayStatus` /
  `AutomationEffectiveDisplayStatus` (`shipped` while enabled and the
  record is shipped, else the stored status), `PRDShippedMatch` /
  `AutomationShippedMatch` (the `shipped=true|false` list filter) and
  `BuildShippedCounts` (one bucket per displayed status; shipped records
  group under `shipped` only while `enabled` and `count_separately` are
  both set).
- **`internal/autonomous/shipped_backfill.go`** — the one-pass backfill:
  over the base repository's main branch, list merge commits whose subject
  names an `automaton/<8-hex PRD id>` branch (newest first); for each,
  find the first release at or after the merge (the next version-bump
  commit's subject, else the first CHANGELOG `## vN.N.N` heading after it);
  record the four fields on the matching PRDs (source `backfill`); an
  Automation is recorded once every non-skipped member is shipped, using
  its latest member merge. Idempotent per recorded `merge_commit`.
- **`internal/autonomous/manager.go`** — entry points:
  `ShippedBackfillOnce(dryRun)` (the single pass used by every surface)
  and `StartShippedBackfill(enabled)` (daemon start, non-blocking
  goroutine when `autonomous.shipped.backfill_on_start` is on).
- **Surfaces**
  - REST (registered in `internal/server/server.go`):
    `POST /api/autonomous/prds/{id}/shipped` body
    `{merged_in_release, merged_at, merge_commit, source}`, audit action
    `record_shipped`; the Automation equivalent
    `POST /api/automations/{id}/shipped`;
    `GET /api/autonomous/shipped/counts`;
    `POST /api/autonomous/shipped/backfill` (`?dry_run=true` for the
    report without writing). The `shipped=true|false` filter on
    `GET /api/autonomous/prds` and `GET /api/automations` with the
    `filter_default` fallback (see `shippedSelectionFromQuery` in
    `internal/server/autonomous.go`).
  - MCP (`internal/mcp/autonomous.go`, `internal/mcp/automation_tools.go`):
    `autonomous_shipped_record`, `autonomous_shipped_backfill`,
    `autonomous_shipped_counts`; `shipped` filter on
    `autonomous_prd_list` and `automation_list`.
  - CLI: `cmd/datawatch/cli_autonomous_shipped.go` —
    `datawatch autonomous shipped record|backfill|counts`
    (`--release --commit --at --source --automation --dry-run`);
    `--shipped true|false` on the PRD/Automation list commands;
    `datawatch automation shipped <id>`.
  - Comm channel (`internal/router/sx2_parity.go`): `shipped <id>
    --commit <sha>` (aliases `shipped-record`, `shipped_record`,
    `record-shipped`, `record_shipped`), `shipped-backfill` and
    `shipped-counts`.
  - Web app: the shipped state is served to the client on the same
    shapes the PWA already reads — the PRD/Automation list entries carry
    the four shipped fields plus the effective display status, the
    `shipped` query selection and the `status=shipped` axis work on the
    list endpoints, and the dashboard card registry carries the system
    card id `shipped` with its live
    per-status-count payload (`internal/server/api_dashboard_cards.go`).
    The PWA itself renders none of it yet — no card definition, no
    shipped selection, no chip, no locale keys — that client-side work
    is the companion-app parity item in
    `plans/2026-11-01-shipped-status-parity-issue.md`.
  - Dashboard: the shipped-vs-cancelled separation is reported by the
    per-status count pair from `BuildShippedCounts`.

### Knobs

- **`autonomous.shipped.enabled`** (default true) — the shipped display
  and count surface; off folds every record back to its stored status.
- **`autonomous.shipped.show_chip`** (default true) — whether the
  shipped chip renders on shipped items in the list views.
- **`autonomous.shipped.filter_default`** (default `all`) — the
  shipped-state view the lists open on: `all`, `shipped` or
  `unshipped`, applied when a list read carries no explicit `shipped`
  parameter.
- **`autonomous.shipped.count_separately`** (default true) — shipped
  records count under their own `shipped` bucket instead of their stored
  status (requires `enabled`).
- **`autonomous.shipped.backfill_on_start`** (default false) — run one
  backfill pass in the background at daemon start; failures are logged and
  never block startup.

### Tests

- `internal/autonomous/shipped_test.go` — record semantics: the fields,
  the source validation, and that recording never changes the stored
  status (cancelled stays cancelled; shipped and cancelled compose).
- `internal/autonomous/shipped_counts_test.go` — effective-status and
  count-bucket derivations, including the `enabled` × `count_separately`
  matrix and the shipped / unshipped / all selections.
- `internal/autonomous/shipped_backfill_test.go` — merge-commit parsing,
  release lookup (version-bump subject and CHANGELOG heading), idempotent
  apply, and the Automation all-members-shipped rule.
- `internal/server/shipped_rest_test.go`, `shipped_counts_rest_test.go`,
  `autonomous_shipped_backfill_test.go` — REST round-trips: record (200 / 404 /
  source validation), counts, backfill (`dry_run` reports without writing),
  and the `shipped` filter with the `filter_default` fallback.
- `internal/autonomous/shipped_start_test.go` — the daemon-start backfill
  gate (`backfill_on_start` on runs one pass, off runs none).
- `internal/config/shipped_config_test.go` — the five keys' defaults, the
  `filter_default` vocabulary and its refused values on write.
- `internal/mcp/shipped_record_test.go`, `shipped_counts_test.go`,
  `autonomous_shipped_backfill_test.go` — the shipped MCP tools and filters.
- `cmd/datawatch/shipped_cli_test.go`, `shipped_list_cli_test.go`,
  `cmd/datawatch/cli_autonomous_shipped_test.go` — CLI flag parsing and
  subcommand wiring (`shipped record|backfill|counts`, `--shipped`,
  `datawatch automation shipped`).
- `internal/router/shipped_comm_test.go`,
  `sx2_parity_shipped_backfill_test.go` — the comm verb, its aliases, and the
  backfill/counts verbs.

## Secret scan before push (`git.secret_scan`)

- `internal/config/config.go`: `GitSecretScanConfig` (`mode`, `detectors`, `allow_list.paths`, `allow_list.fingerprints`), `ValidateGitSecretScanMode`, `ValidateGitSecretScanDetectors` and `GitConfig.EffectiveSecretScanDetectors`.
- `internal/session/git_push.go`: `effectiveSecretScan` and the scan in the push path; shadow mode records findings and lets the push go, enforce mode refuses it. Findings carry detector, file:line and fingerprint, never the matched secret.
- `internal/server/api.go`: the config PUT validates the mode, detector names and fingerprints before anything is stored.
- `internal/mcp/secret_scan_tools.go`: `secret_scan_config_get` and `secret_scan_config_set`.
- Web settings in `internal/server/web/app.js` with five locale bundles.

## Change records and generated artefacts

`internal/changerecord` builds a `ChangeRecord` (files, Go packages, settings, routes, locale keys,
tests and recorded command runs) from a `DiffSource`; `GitDiff` is the git-backed source. `Merge`
combines task records into one Automaton record, and `Save`/`Load` persist it as
`<automaton data dir>/change-records/<id>.json` (atomic write). Executor wiring comes later.

Config: `autonomous.generated_artefacts` with `change_record`, `gate_report`, `changelog_draft`,
`parity_draft`, `tracker_rows`, `registry_row` and `combined_docs_task` (all bool, all on when
unset). `AutonomousConfig.EffectiveGeneratedArtefacts()` resolves the defaults.

Each field is a `*bool` in `config.GeneratedArtefacts` (nil reads as on, so an explicit off survives a
YAML or JSON round trip); `GeneratedArtefacts.Set(name, bool)` stores one by its YAML name. A value
that is not a boolean is rejected by the YAML loader and by `PUT /api/config` (400, nothing stored).

| Field | Effect when off |
|-------|-----------------|
| `autonomous.generated_artefacts.change_record` | No per-task or merged change record is built or stored. |
| `autonomous.generated_artefacts.gate_report` | The gate report (counts only) is not generated. |
| `autonomous.generated_artefacts.changelog_draft` | The changelog draft is not generated. |
| `autonomous.generated_artefacts.parity_draft` | The parity issue draft is not generated. |
| `autonomous.generated_artefacts.tracker_rows` | The testing tracker rows are not generated. |
| `autonomous.generated_artefacts.registry_row` | The plan registry row is not generated. |
| `autonomous.generated_artefacts.combined_docs_task` | Decompose does not ask for the one combined documentation task. |

Surfaces: `GET /api/config` returns `autonomous.generated_artefacts` as the effective set;
`PUT /api/config` takes the dotted keys `autonomous.generated_artefacts.<field>` (`handlePutConfig`
validates, `applyConfigPatch` stores). The daemon wires `Manager.SetGeneratedArtefacts` with a provider
that reads the live `cfg.Autonomous`, so a change applies to a running Manager without restart.

REST (`internal/server/generated_artefacts_rest.go`): `GET /api/autonomous/generated-artefacts`
(settings plus kinds with format description and tracked path), `GET /api/autonomous/prds/{id}/change-record`
(the stored merged record; the completion step saves it under the Automaton id), `GET
/api/autonomous/prds/{id}/generated/{kind}` (preview, nothing written) and `POST
/api/autonomous/prds/{id}/generated/{kind}/regenerate` (re-render and write; an edited file is kept and
the result is `skipped`). Reads need the autonomous read capability, regenerate the write capability.
The manager side is `generated_artefacts_read.go`; an unknown id, kind or missing record is a 404.
MCP tools `generated_artefacts_get`, `autonomous_prd_change_record`, `autonomous_prd_generated_artefact`
and `autonomous_prd_regenerate_artefact` proxy those routes.
### Splash dwell settings (`ui.splash_min_seconds`, `ui.splash_min_seconds_new_version`)

Two keys size how long the PWA splash screen stays on screen before it may
dismiss. Neither value is hard-coded: both are read from the daemon config at
the moment the splash gating decision runs (on connect), and a change from
any surface applies to the next splash show with no daemon restart. A failed
config fetch or a non-number value keeps the defaults.

How it works, for someone reading the code:

- `internal/config/config.go` carries the two keys as pointer fields in
  `UIConfig` (`SplashMinSeconds *int`, `SplashMinSecondsNewVersion *int`), so
  an unset key and an explicit `0` are distinct and both survive a YAML round
  trip and a `PUT /api/config` write. `EffectiveSplashMinSeconds()` and
  `EffectiveSplashMinSecondsNewVersion()` resolve unset to the defaults `6`
  and `9` seconds (constants `DefaultUISplashMinSeconds`,
  `DefaultUISplashMinSecondsNewVersion`; `MaxUISplashSeconds` is `60`).
  `validateUISplashSeconds` (shared with the config-load path) accepts a whole
  number 0–60 and rejects a fraction, a negative, `61`, and a non-number,
  naming the key.
- `internal/server/api.go`: `GET /api/config` returns the two effective
  values under `ui`; `PUT /api/config` accepts the two dot-path keys
  (`ui.splash_min_seconds`, `ui.splash_min_seconds_new_version`) and, before
  anything is applied, rejects out-of-range or non-whole-number values with
  HTTP 400 naming the key.
- `internal/server/web/app.js` drives the behaviour at display time:
  `splashDwellRemainingMs({ minSeconds, legacyStartAt, now })` computes the
  remaining dwell — `minSeconds * 1000` measured from the moment the gating
  allowed the splash, or, when the resolved setting is `0`, the legacy window
  of 3000 ms measured from `window._splashStart` (page start) minus what
  already elapsed. `window._splashStart` is set at the top of the file, before
  anything else runs. `_scheduleSplashDismiss(splash, remainingMs, { connected })`
  waits that long then starts the 700 ms fade (`splash.classList.add
  ('fade-out')`) and removes the splash element exactly once (a prior
  dismiss path clears the timers and the flag). Early dismiss: once
  `connected` is true, `pointerdown`, `click`, and `keydown` on the document
  start the fade immediately (the first one wins; a later input on the same
  splash does nothing). The gating branch on connect (`shouldShow`) covers
  first visit (`!lastSplashTime`), new version (`serverVer !== lastSplashVer`
  against the `cs_splash_version` localStorage key), or 24+ hours
  (`hoursSince >= 24` against `cs_splash_time`). The dwell settings are
  fetched from `GET /api/config` before the timing decision, so the first
  connect uses the operator's values; a failed fetch or non-number keeps the
  defaults (6 / 9).
- Version line: when the splash is shown for a new version,
  `#splashVersionPrev` carries the previous version via the locale key
  `splash_version_previous` (the version the splash last showed for, read
  before the key is overwritten); on an ordinary show it stays hidden.
- The PWA settings view exposes both keys as number fields
  (`settings_splash_min_seconds`, `settings_splash_min_seconds_new_version`
  and their `*_hint` strings in all five locale bundles), seeded with the
  effective values (6 / 9 when unset).

| Method | How |
|--------|-----|
| **YAML** | `~/.datawatch/config.yaml` → `ui.splash_min_seconds`, `ui.splash_min_seconds_new_version` (whole number 0–60; `0` keeps the legacy timing) |
| **CLI** | `datawatch config set ui.splash_min_seconds 8`, `datawatch config set ui.splash_min_seconds_new_version 15` |
| **REST API** | `PUT /api/config` with, e.g., `{"ui.splash_min_seconds": 8}`; `GET /api/config` returns each under `ui` with its effective value |
| **MCP** | `config_set` with key `ui.splash_min_seconds` (or `_new_version`) |
| **Comm channel** | `configure ui.splash_min_seconds=8` |

**Keys.**

| Key | Type | Default | Meaning |
|-----|------|---------|---------|
| `ui.splash_min_seconds` | whole seconds, 0–60 | `6` | Minimum dwell for an ordinary splash show (first visit or > 24 h). `0` keeps the legacy 3000 ms timing. |
| `ui.splash_min_seconds_new_version` | whole seconds, 0–60 | `9` | Minimum dwell when the splash is shown because the daemon version changed. `0` keeps the legacy timing. |

Tests:

- `internal/config/ui_splash_settings_test.go` — defaults when unset,
  explicit `0` kept, `60` accepted, `61` and `−1` rejected, both keys round
  trip YAML.
- `internal/server/splash_settings_test.go` — `GET /api/config` returns the
  defaults when unset, `PUT` round-trips without a restart, bad values are
  refused naming the key.
- `internal/router/splash_configure_test.go` — comm `configure` help and
  listing mention both keys; `configure` accepts them.
- `internal/mcp/splash_config_set_test.go` — MCP `config_set` accepts both
  keys and refuses bad values.
- `cmd/datawatch/splash_config_set_test.go` — CLI `config set`/`get` for both
  keys; bad values refused.
- `internal/server/web/app-splash-dwell.test.js` — dwell timing, late
  connection, `0` = legacy, early dismiss (click / pointerdown / keydown),
  version line, 700 ms fade, single teardown.
- `internal/server/web/app-splash-settings.test.js` — the settings fields and
  their floors/ceilings on the five locale bundles.

### Classify mode on guardrail outcomes

When the guardrail judge's LLM entry has `verification_mode: classify`, `classifyGate`
(`internal/autonomous/guardrail_classify.go`) is the final gate over the generative findings of the
per-task, per-story, `rules-review` and `ci-watch` guardrails. The outcome becomes pass, warn or block
from the classifier; the generative prose stays as the optional explanation. A prior block is never
downgraded (and makes no classifier call), a prior warn is never raised to pass, and a classifier
error, missing scores or abstain is a block with category `infra` (set by the harness only). There is
no fallback to generate-and-parse. Scan guardrails, project-rules-scan and the scripted-pass
short-circuit stay deterministic and never reach the gate. The verdict records `mode`, `scores` and
`route` (scores and route follow `autonomous.verification_record_scores`).

## Progress and session tag filters (repeatable `tag` + `tag_match=any|all`, trend scope)

The Automaton-progress filter accepts one `tag` before; it now accepts
several, on every surface, with an explicit match rule, and the session list
gains the same tag filter (a session matches when one of its tags — its own
or the ones it inherits from its Automaton — is present). The progress trend
reports the scope its snapshots were counted over.

### Components

- `internal/autonomous/progress_engine.go` — `ProgressOptions.TagIDs []string`
  + `TagMatch` (`any` default, `all`); the legacy singular `TagID` is merged
  in by `tagFilterIDs()`. `tagsMatch(record, filter, needAll)` does the
  any/all test. `ProgressScope()` builds the scope string (`all`,
  `automation:<id>`, `tag:<id>`, `tags:<a>,<b>`, `tags(all):<a>,<b>`,
  combined); it is stamped on the metrics (`Scope`) and on every
  `ProgressTrendPoint` / `ProgressTrendBody`. `RenderTrendReply(days, scope,
  direction, points)` appends ` (scope …)` when narrower than `all`;
  `RenderProgressText` prints a `Scope:` line likewise.
- `internal/server/progress.go` — `progressTagParams()` reads the repeatable
  `tag` query parameter (comma lists split, deduped, first-seen order),
  validates `tag_match` (`any`/`all`) with HTTP 400 on anything else, and
  feeds both `ProgressOptions`. The trend REST response carries `scope`.
- `internal/server/api.go` — `handleSessions` parses the same `tag` /
  `tag_match` parameters and routes them into `session.SessionFilter`.
- `internal/session/manager.go` — `SessionFilter.Tags + TagMatch`;
  `sessionEffectiveTagIDs()` = own + inherited (via
  `Manager.InheritedTags` → `TagOwnerLookup`), deduped;
  `sessionMatchesTagFilter()` applies any/all. `ListSessions` and
  `ListArchivedSessions` honour it alongside the existing state/search
  filters.
- `internal/mcp/progress_tools.go`, `internal/mcp/server.go`,
  `internal/mcp/list_sessions_view.go` — `autonomous_progress` and
  `autonomous_progress_trend` take `tags` (array) + `tag_match`;
  `list_sessions` takes `tags` + `tag_match`; the low-bandwidth
  `list_sessions_view` proxy forwards them verbatim.
- `internal/router/progress_cmd.go` — `progress` and `progress trend` accept
  repeatable `tag=<id>` and `tag_match=any|all`; trend renders with scope.
- `internal/router/router.go` — the `list` comm verb parses `tag=<id>` (repeatable)
  and `tag_match=<any|all>` tokens from the free-form filter.
- `cmd/datawatch/cli_progress.go` — `--tag` (repeatable) + `--tag-match` on
  both commands; the tag is sent as repeatable `tag=` query parameters.
- `cmd/datawatch/main.go`, `cmd/datawatch/cli_list_views.go` — `session list`
  (and the shared query builder) gain `--tag` (repeatable) / `--tag-match`,
  forwarded as repeatable `tag` + `tag_match`.
- `cmd/datawatch/progress_parity_test.go` — the trend parity check now passes
  the scope through `RenderTrendReply` so the comm, CLI, REST and MCP
  surfaces must agree on it.

### Knobs

No new configuration key. The match rule is a per-read parameter
(`tag_match=any|all`), matching the behaviour the web list views already
expose (`match ANY` / `match ALL`).

### Tests

- `internal/autonomous/progress_engine_test.go` — one tag, several tags
  under `any` and `all`, empty filter, and the scope string for each
  combination (including automation + tag).
- `internal/autonomous/progress_text_test.go` — the `Scope:` line and the
  trend ` (scope …)` suffix appear only when narrower than `all`.
- `internal/server/progress_test.go` — the repeatable `tag` parameter under
  both match rules, the legacy singular `tag_id`, a refused `tag_match`, and
  the `scope` field on the trend response.
- `internal/mcp/progress_tools_test.go` — `tags` arrays and `tag_match`
  reach the backend as repeated `tag=` parameters.
- `internal/session/tags_test.go` — own vs inherited tags, any vs all, and
  untagged sessions against a tag filter.
- `internal/server/session_list_tag_test.go` — `GET /api/sessions?tag=…`
  matches own and inherited tags under both rules; a bad `tag_match` is 400.
- `internal/mcp/list_sessions_tag_test.go` — the `list_sessions` `tags` +
  `tag_match` parameters (legacy path) and the `list_sessions_view`
  forwarding.
- `internal/router/comm_list_tag_test.go`, `progress_cmd_test.go` — the `list`
  and `progress` comm verbs parse, forward and reply.
- `cmd/datawatch/cli_progress_test.go`, `cli_list_views_test.go` — the flag
  wiring: repeatable `--tag`, the `--tag-match` default (empty = any) and the
  emitted query.

## Automation requirements and the model pull gate (autonomous.requirements, autonomous.model_pull_consent)

Operator-facing behavior:
[`docs/howto/automation-requirements.md`](howto/automation-requirements.md)
(what the report shows) and
[`docs/howto/model-pull-consent.md`](howto/model-pull-consent.md)
(the pull gate). The operator reference lives in
[`docs/operations.md`](operations.md), section 4, "Automation requirements
report" and "Model pull consent".

### Requirements report

The requirements report answers "can this daemon run the automations it is
configured to run?" It states what is configured, what is missing and the
backend minimums: at least one execution backend plus a planning and a
verification backend; Claude and the local model pool are each optional
but at least one provider is required; and, per capability, the minimum
backend versions against each known node (meets / fails / unknown —
unknown fails closed). It is a report, not a gate: nothing is blocked on
it. The text rendering is produced by one shared renderer
(`RenderRequirementsReport` in `internal/autonomous`) so every surface —
REST, MCP, CLI, comm and web — prints the same bytes.

- **`internal/autonomous/capability_requirements.go`** — the single
  in-code minimum-version table keyed by capability
  (`minimumOllamaByCapability`; today exactly one floor,
  `classification_verdict` → Ollama 0.35.0 for the `/v1/systemone`
  decision endpoint), `KnownCapabilities()`, `EffectiveMinimumOllama`
  (operator override wins; an empty override clears the floor), the
  version comparison, the enable/refusal enforcement (a capability
  enabled on a node below its effective minimum is refused naming the
  node, the found and the required versions; a node that later falls
  below is flagged and the capability fails closed — unavailable until a
  recheck finds it back at or above the minimum) and the report builder
  (`RequirementsReport`).
- **`internal/autonomous/requirements_config.go`** —
  `ValidateMinOllamaOverrides` / `NormalizeMinOllamaOverrides`, the one
  reject rule every config write surface reuses: an empty capability key,
  a capability not in `KnownCapabilities()`, or a non-empty value that
  does not parse as a dotted semantic version ("0.35.0") is refused; on
  a REST write surface that is a 400 and nothing is written; an empty
  value under a known capability clears that capability's floor.
- **`internal/server/requirements_surfaces.go`** — the REST routes
  `GET /api/automation/requirements-report` (JSON, or the shared text
  with `?format=text`), and `GET /api/compute/nodes/{name}/versions` (the
  node's kind, address, backend, reported backend version with when it
  was recorded, and the per-capability Ollama minimums applied to that
  version).
- **`internal/mcp/requirements_surfaces.go`** — the MCP tools
  `automation_requirements_report` (optional `format`: `text` default or
  `json`) and `compute_node_backend_versions` (`name` required, a known
  compute node or 404). Both proxy the REST routes.
- **`cmd/datawatch/requirements_surfaces.go`** and
  `cmd/datawatch/cli_compute.go` — `datawatch automation
  requirements-report [--format text|json]` (text is the shared render,
  `--format json` prints the REST body untouched) and `datawatch compute
  node versions <name>`.
- **`internal/router/requirements_surfaces_comm.go`** — the comm verbs
  `requirements-report [text|json]` and `node-versions <name>` (under
  the `autonomous` / `automation` help group), which read the same
  routes over the loopback and print the shared text.
- **`internal/server/requirements_config_surfaces*`** — the
  `autonomous.requirements.min_ollama.<capability>` keys on `GET/PUT
  /api/config` with the shared validator; the comm `configure` verb
  forwards them.

### Model pull consent (the pull gate)

`autonomous.model_pull_consent` is the operator's allow-list that gates
every model pull — both the compute-node model pull and the marketplace
task pull are checked before anything is pulled. The rule is default-deny:
a model may be pulled when it is listed with `allowed: true`, or when it
is unlisted and its publisher is on the publisher allow-list. A listed
model with `allowed: false` is an explicit refusal (its `note` travels
with the refusal); everything else is refused and the refusal says
exactly which key to set. Matching is case-insensitive and ignores the
`:tag` suffix, so a listed `gpt-oss` matches a pull of `gpt-oss:20b`. A
model's publisher is the entry's `Publisher` when the model is listed,
else the leading `publisher/model` segment of the name; plain names
carry no publisher. The stored policy is *complete*: a set replaces what
is stored, it does not append.

- **`internal/config/model_pull_consent.go`** — the shape:
  `ModelConsentEntry {name, publisher?, allowed, note?}`,
  `ModelPullConsent {models[], publishers[]}`, and
  `ValidateModelPullConsent`, the one reject rule every write surface
  reuses (empty model names, duplicate model names — case-insensitive,
  tag-ignoring — and empty publisher entries are 400 on write surfaces
  and nothing is written). The block is validated on config load and on
  every operator write path; `config.Save` persists it and the autonomous
  manager is re-synced in-process, so it applies without a restart.
- **`internal/autonomous/model_pull_consent.go`** — the decision rule:
  `ModelPullConsentCheck` (pure: model listed → entry decides; unlisted
  with an allow-listed publisher → allowed via the publisher; anything
  else refused with the reason), `ModelPullDecision` and
  `ModelPullPublisher`.
- **`internal/server/marketplace.go`** — the two guarded pull paths call
  the check before pulling.
- **`internal/server/requirements_surfaces.go`** — the REST routes
  `GET /api/automation/model-consent` (models with `allowed`/`note`,
  publishers, and the decision rule in words) and `PUT
  /api/automation/model-consent` (the complete policy; validated, saved,
  synced, and audited as `model_pull_consent_changed`).
- **`internal/mcp/requirements_surfaces.go`** — the MCP tools
  `automation_model_pull_consent_get` (read) and
  `automation_model_pull_consent_set` (`models`: JSON array of model-name
  strings — each stored as allowed — or `{name, allowed, note}` objects;
  `publishers`: JSON array of names; both replace the stored lists).
- **`cmd/datawatch/requirements_surfaces.go`** — `datawatch automation
  model-consent get` and `datawatch automation model-consent set
  --model <name> [--deny <name> ...] [--publisher <p> ...] | --clear`
  (the repeated flags compose the complete list to store; `--deny`
  records a refusal; `--clear` wipes the policy, after which every pull
  is denied).
- **`internal/router/requirements_surfaces_comm.go`** — the comm verbs
  `autonomous model-pull-consent-get` and
  `autonomous model-pull-consent-set model=<name> [deny=<name> ...]
  [publisher=<p> ...]`, or `… model-pull-consent-set clear`.
- **`internal/server/web`** — the PWA settings section "Model pull
  consent" (the `model_pull_consent` settings block: per-model allow
  toggles and publisher chips) and the five locale bundles.

For the write-path rules per surface and the operator workflow, see the
two how-to guides above and the "Model pull consent" / "Automation
requirements report" entries in `docs/operations.md`.

## Memory ceiling settings (resource_limits)

- **Config:** `resource_limits.memory_share_percent` (1 to 100, default 50), `resource_limits.memory_floor_mb` and `resource_limits.memory_cap_mb` (whole MB, floor not above cap). Set them with `PUT /api/config`, `datawatch config set`, the chat command `configure <key>=<value>` (listed under `configure help` and `configure list`) or the `config_set` MCP tool.
- **Status:** `GET /api/autonomous/status` and the `autonomous_status` MCP tool carry a `memory_ceiling` object: `effective_mb` (0 when none), `source` (`default`, `project`, `task_kind` or `none`), `enforcement_path` (`scope`, `watchdog` or `none`) and the `share_percent`, `floor_mb` and `cap_mb` it comes from. It describes a verification run with no project or task kind override; `source` is `none` while no ceiling settings are installed in the autonomous manager. Code: `internal/autonomous/memory_ceiling_status.go`.

## Daemon-level project defaults (projects_defaults)

The `projects_defaults` block (`internal/config/projects_defaults.go`) holds three daemon-wide defaults a project inherits when it sets none of its own: `branch_retention_days` (0 = unset), `weekly_share_percent` (0 = unset, otherwise 1 to 100) and `major_trigger` (`manual`, `milestone` or `never`; default `manual`). It sits beside the `projects` map, not inside it, so a project named `defaults` cannot collide. It is validated on config load and on every `PUT /api/config` write, and it is exposed through GET/PUT `/api/config`, the comm `configure` verb, MCP `config_set`/`get_config` and `datawatch config set`. The operator-facing description is in [howto/project-automation-policy.md](howto/project-automation-policy.md#daemon-level-project-defaults).
