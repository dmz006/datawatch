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
`session.reserved_interactive`. See `docs/howto/automata-capacity.md`.

Ledger lock watchdog (`internal/capacity/watchdog.go`): `Ledger.SnapshotWatched` wraps
`Snapshot` for request paths (`/api/capacity`, the comm `capacity` reply). If the snapshot
has not returned after 5 seconds (`DefaultLockWatchdogThreshold`) `LogLockWedge` logs
`capacity ledger lock held for N seconds` once per wedge episode, however many callers
are waiting, followed by the stacks of goroutines inside `internal/capacity`, which include
the lock holder. The watched call keeps waiting and returns normally once the lock frees.
Internal loops (reaper, limit sync) use plain `Snapshot`.

Reactive usage back-off (`internal/autonomous/backoff.go`): an in-memory per-backend flag
that holds autonomous dispatch (never interactive starts) after a rate/usage limit.
Config: `autonomous.usage_backoff` (`pause_backend` | `off`, default `pause_backend`;
`off` disables only the automatic path) and `autonomous.backoff_max_wait_minutes`
(default 60; whole minutes, a value below 1 is treated as 60). Operator surfaces:
`/api/usage-backoff`, `usage_backoff_*` MCP tools, `datawatch usage-backoff`, comm
`usage_backoff`. See `docs/howto/automata-capacity.md`.

Usage calculator settings (`internal/config/usage_calculator.go`): the `usage.calculator`
block holds `variant` (`pace-gauge` | `lane-planner` | `burn-ledger` | `all`, default `all`),
`policy` (`use-all` | `pace` | `finish-early`, default `use-all`), `safety_points` (0 to 50,
default 5), `cost_per_task_points` (above 0, default 0.1, a starting figure to calibrate
against readings), `baseline_per_24h` (0 to 20, default 3), `max_lanes` (1 to 8, default 3),
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
long text field in the default `GET /api/autonomous/prds/{id}` reply
(`internal/autonomous/prd_view_cap.go`, `CapPRDText`). Capped fields end with a
`[truncated ...]` marker and are listed in the record's `truncated` array with the URL of the
full text. The store is never cut. `?full=1` returns the whole record, and
`GET /api/autonomous/prds/{id}/outputs` (optionally `?task=<id>`, or
`/tasks/{task_id}/outputs`) returns the heavy fields byte for byte. The list summary
(`BuildSummaryView`) does not read these fields.

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

### Peer usage cache

The shared-node gate (`fingerprint.PeerGate`) reads each opted-in peer's held counts from `fingerprint.PeerUsageCache`, an in-memory map of the last good `/api/capacity` answer per peer. `wireFingerprint` builds one cache, sets it as `PeerGate.Usage`, and sets `Syncer.AfterPass` to `RefreshAll`, so the cache is refreshed once at start and after every 15-minute sync pass. The ledger callback only reads memory; a missing or stale entry fails open and triggers a background refresh. Peers without fingerprint sync opted in get no HTTP call, and no config key is involved.

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
| `compute_nodes[].tls_insecure_skip_verify` | bool | `false` | Turns off TLS certificate verification for this node: the daemon's node usage probes (loaded models via `/api/ps`, GPU stats from `monitoring_endpoint`) and the supervisor watch probe. Off by default, so certificates are verified. Last resort only; prefer `tls_ca_file` or `tls_cert_fingerprint`. When on, it is reported in the node record, the PWA and the audit log. Cannot be combined with `tls_ca_file` or `tls_cert_fingerprint`: node add and update reject the combination, and a seeded node that sets both has its TLS probes fail instead of falling back to skipping verification. |
| `compute_nodes[].tls_ca_file` | string | `""` | Path to a PEM CA bundle trusted in addition to the system roots when verifying this node's certificate. Use it when the node serves a certificate from a private CA. May be combined with `tls_cert_fingerprint`; cannot be combined with `tls_insecure_skip_verify`. |
| `compute_nodes[].tls_cert_fingerprint` | string | `""` | SHA-256 fingerprint of this node's leaf certificate, pinned instead of chain verification. Use it for a single self-signed certificate. Accepts 64 hex characters, case-insensitive, colons optional. May be combined with `tls_ca_file`; cannot be combined with `tls_insecure_skip_verify`. |
| `compute_nodes[].scheduling_priority` | int | `50` (when `0` or unset) | Node priority, 0 to 100. When an LLM entry's pool lists two or more nodes, the ledger places a worker on the least-loaded healthy node that has a free slot (pool used count over its limit); priority only breaks a tie between equally loaded nodes, and a remaining tie keeps the order of the entry's list (`internal/capacity/ledger.go`). Set through REST, MCP `compute_node_add`/`compute_node_update`, the CLI `compute node add|update` and the PWA node form. |
| `llms[].compute_nodes` | list of names | empty | LLM registry entry, not a `config.yaml` key. The list of compute nodes a local-model worker, verifier and judge may run on. Each listed node that is known and serves the model is a candidate, and the request goes to the least loaded candidate (`scheduling_priority` only breaks a tie, then list order); a node that does not serve the model is skipped, and one engine per node still applies. With one node it behaves as before. With two or more candidates it is the pool the ledger picks from, and each worker's model is loaded on the chosen node before it starts; a failed load alerts, releases the lease and tries another node of the list. Set with `--compute-nodes` on `datawatch llm add|update`, REST `PUT /api/llms/{name}` and MCP `llm_add`/`llm_update`. |
| `compute_nodes[].session_slots` | int | `0` | Operator's worker count for a node that hosts a local engine. `0` derives it from observer data (`compute.NodeSlots`, `internal/compute/slots.go`): one slot per GPU limited by free GPU memory against the served model, or free system memory divided by model size with no GPU data, at least one slot. A positive value wins over the derived count. `max_concurrent_sessions` still caps the result. A negative value is rejected when the config loads (`validateLoaded`, `internal/config/config.go`) and when the node is saved (`Node.Validate`). The same field is on the node record in `internal/compute/node.go`, saved by the registry, and set through REST, MCP, the CLI, the comm channel and the PWA node form. |
| `data_dir` | string | `~/.datawatch` | Root directory for sessions.json, logs/, and config |
| `signal.account_number` | string | (required) | Signal phone number in E.164 format, e.g. `+12125551234` |
| `signal.group_id` | string | (required) | Signal group ID in base64 format (from `signal-cli listGroups`) |
| `signal.config_dir` | string | `~/.local/share/signal-cli` | signal-cli data directory containing account keys |
| `signal.device_name` | string | hostname | Name shown in Signal's Linked Devices list |
| `session.file_service_root` | string | `""` | Root directory for the federated file service and PWA image uploads. Per-peer files land in `<root>/peers/<name>/`; per-discussion files in `<root>/discussions/<id>/`. Empty = falls back to `session.root_path`, then the user home directory |
| `session.max_sessions` | int | `10` | Maximum number of concurrent claude-code sessions |
| `session.input_idle_timeout` | int | `10` | Seconds of idle output before declaring a session is waiting for input |
| `session.tail_lines` | int | `20` | Default number of lines returned by `tail` and `status` commands |
| `session.claude_code_bin` | string | `claude` | Path to the claude-code binary (can be absolute or a `PATH`-relative name) |
| `session.task_delivery_window_seconds` | int | `60` | How long one OneShot task-delivery attempt waits for the Claude main prompt to appear (the session must start its channel and tool servers before the prompt shows) before trying again |
| `session.task_delivery_attempts` | int | `3` | How many delivery attempts are made before the task fails as undelivered; between attempts a 5s then 10s backoff pause is waited, so the total wait is attempts × window + 15s (about 195s with the defaults). A session whose process has already exited still fails at once |
| `session.store_flush_interval` | int | `1000` | Milliseconds the session store batches changes before the background writer saves them; `0` saves on every change. Applies live on any config write (`PUT /api/config`, web UI, `configure`, `datawatch config set`, MCP `config_set`) |
| `session.store_archive_after_days` | int | `14` | Days after its last update that a finished session moves to the archive file; `0` disables archiving. Applies live |
| `session.store_field_cap_kb` | int | `256` | Largest session text field kept inline in the session store, in KB; longer text is cut with a marker and the full text goes to a side file. Applies to later writes |
| `session.store_side_file_cap_kb` | int | `64` | Automaton (PRD) text longer than this many KB is stored in a side file next to its record. Existing records are rewritten with the new value when their Automaton next changes |
| `server.enabled` | bool | `true` | Whether to start the HTTP/WebSocket server |
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
| `mcp.public_url` | string | `""` | Public base URL the MCP SSE server advertises to clients in its `endpoint` event (for example `https://host.tailnet.ts.net:8081`). Set it when `mcp.sse_host` is bound to localhost behind a TLS-terminating reverse proxy. Trailing slash is ignored. Empty advertises `<scheme>://<first sse_host>:<sse_port>`. Restart required. |
| `mcp.max_retries` | int | `3` | Number of automatic retries when a per-session MCP channel server fails to start or loses connection |
| `goose.enabled` | bool | `false` | Enable the goose/goose-prompt LLM backend |
| `goose.binary` | string | `goose` | Path to the goose binary (absolute or PATH-relative) |
| `goose.console_cols` | int | `0` | Terminal width for goose sessions (0 = session default) |
| `goose.console_rows` | int | `0` | Terminal height for goose sessions (0 = session default) |
| `goose.provider` | string | `""` | Provider injected as `GOOSE_PROVIDER` env var (e.g. `anthropic`, `openai`, `google`) |
| `goose.model` | string | `""` | Model injected as `GOOSE_MODEL` env var (e.g. `claude-sonnet-5`) |
| `goose.api_key_ref` | string | `""` | API key (literal or `${secret:name}`) injected as the provider-specific key env var; resolved at startup |
| `goose.channel_enabled` | bool | `false` | When true, inject `GOOSE_MCP__DATAWATCH__*` env vars so Goose connects back as an MCP client (stdio transport) |
| `autonomous.verifier_diff_max_bytes` | int | `0` | Byte cap on the git diff injected into the verifier prompt for evidence grounding. `0` = 8192. Raise for large changesets; lower to reduce token cost. |
| `autonomous.verification_timeout_seconds` | int | `1800` | Declared in `internal/config/config.go` (`VerificationTimeoutSeconds`). Bounds one verifier or per-task guardrail judgment session; consumed by the verifier and guardrail closures in `cmd/datawatch/main.go` through the `verificationSessionTimeout` resolver. `0` or negative = 1800-second (30m) fallback. Exposed in the PWA Settings autonomous card. |
| `autonomous.judge_policy` | list | `[]` | Ordered LLM backends the judges (PRD guardrail, plan review and other autonomous judges) try, one attempt per backend, so the attempts total the list length. Empty = the judges follow the verifier routing policy (prefer-free, fixed, balance and the presets choose among its backends). A unit with its own task or PRD guardrail backend override keeps that one as the only entry, still chosen through the selector. A judge moves to the next backend when its run hits the time limit (a warning each time) or when no capacity slot is granted within the wait (no warning; the wait is not counted against the limit); once a backend has timed out and no backend is left, the PRD is held in `needs_review` with a `judge-timeout` reason and an error alert lists every attempt (a backend that had no slot is listed as one that did not run). When no attempt timed out, the judge never ran and returns an infra verdict instead of a hold. A judge cancelled while it waits for a slot stops at once. The plan-review judge (run when `autonomous.plan_review_mode` is `warn` or `block`) takes the same path: it runs as a guardrail-style judge invocation, so its backend comes from the selector (a backend set in `autonomous.plan_review_backend` is its only entry), it takes a capacity lease and runs under the judge time limit. When no backend finishes, the plan-review verdict is recorded as held (`held`, with one finding per attempt): the Automaton stays in `needs_review`, lane fill does not start it and `block` mode refuses approval until the review is re-run. Declared in `internal/config/config.go` (`JudgePolicy`); normalized by `config.NormalizeJudgePolicy`. Surfaces: `GET/PUT /api/config` (`autonomous.judge_policy`), `GET/PUT /api/autonomous/config`, MCP `autonomous_config_set` (`judge_policy`), comm `autonomous config-set judge_policy=a,b`, CLI `autonomous config-set --judge-policy`, the PWA Settings autonomous card. |
| `autonomous.judge_timeout_seconds` | int | `1800` | Time limit for one judge run (30 minutes by default), counted from the judge's start, not from any wait for a capacity slot. Declared in `internal/config/config.go` (`JudgeTimeoutSeconds`); normalized by `config.NormalizeJudgeTimeoutSeconds`, so `0` or negative = `1800`. Same surfaces as `autonomous.judge_policy` (CLI `--judge-timeout-seconds`). |
| `autonomous.judge_size_threshold_lines` | int | `400` | Changed-line count above which the judges try `claude-code` first; at or below it, and when it is `0`, the judge backends keep their configured order. Lines are added plus deleted on the Automaton branch against its merge base, lock files left out, committed work only, counted by `JudgeDiffLines` in `internal/autonomous/judge_diff_size.go`. The reorder is `judgeSizeRoute` in `cmd/datawatch/judge_size_route.go`: `claude-code` moves to the front when it is in the chain and the rest keep their order, still one attempt per backend; in `balance` mode the load decides and the order only breaks ties. A unit with its own task or PRD guardrail backend override is not reordered, and a failed count keeps the default order. The Automaton decision log names the rule and the line count; a judge that times out on every backend still holds the Automaton. Declared in `internal/config/config.go` (`JudgeSizeThresholdLines`, default `JudgeSizeThresholdDefaultLines`); a negative value is refused. Same surfaces as `autonomous.judge_policy` (CLI `--judge-size-threshold-lines`, comm `judge-policy-set judge_size_threshold_lines=n`, PWA Settings > Automata > Autonomous Automata planning > Judge size threshold). |
| `autonomous.guardrail_model` | string | `""` | **v9.10.18** — pinned model for the guardrail JUDGE (rules-review, ci-watch, per-task / per-story guardrails, rejudge). Tier 3 of the 5-tier precedence chain implemented in `internal/autonomous/guardrail_model.go` (`ResolveGuardrailModel`): tier 1 PRD `guardrail_model`, tier 2 task `guardrail_model`, tier 3 this field, tier 4 `autonomous.verification_model`, tier 5 `autonomous.planning_model` (tiers 3–5 apply once the resolved judge backend is pi-family). First tier that yields a model the resolved backend can serve (`validateModelForBackend`) wins. If every tier fails (empty or unpairable), the guardrail record gets a loud, non-retried infra-category block (sentinel `ErrNoGuardrailModel`, verdict via `noGuardrailModelVerdict`) — a pi-family judge NEVER silently runs on the backend's built-in default model. The `set_prd_guardrail_llm` / `set_task_guardrail_llm` setters validate the pair (plus full resolution when a model is pinned) before mutating. |
| `autonomous.default_quality_gates.enabled` | bool | `false` | Master toggle for per-PRD test regression gates. When true, the executor captures a test baseline before the first task and re-runs after each task completes. |
| `autonomous.default_quality_gates.test_command` | string | `""` | ONE shell string run through `sh -c` in the PRD's `project_dir` (`pipeline.RunTests`), so compound commands (`a && b`) work. The baseline is validated at set time and at Automaton start; an unusable one (could not start, timed out, non-zero exit naming no test, no tests found) marks the PRD `quality_gate_status: "invalid"` and the gate then only warns. |
| `autonomous.default_quality_gates.timeout` | int | `0` | Per-run timeout in seconds for the test command. `0` = no cap. |
| `autonomous.default_quality_gates.block_on_regression` | bool | `false` | When true, a task that introduces a test regression is marked failed and triggers an auto-fix retry cycle. |
| `autonomous.default_quality_gates.scope` | string | `""` → `full` | Gate scope: `full` \| `touched-plus-smoke` \| `touched` — how much of the test suite a gate task runs (`internal/pipeline/gate_scope.go`: `ParseGateScope`, `PlanGateScope`). Empty inherits the PRD's `quality_gates.scope`, then this key, then the built-in `full` (`Manager.resolveGateScopeSettings`); the final gate task (last non-terminal task or `TaskKindFinalGate`) always runs `full` regardless. A `go test <flags> ./...` command is rewritten to the touched packages (+ one-level importers, + the smoke set for `touched-plus-smoke`); anything unrecomputable or non-rewritable falls back to the full command and records why in `scope_plan`. Module files (`go.mod`, `go.sum`, `go.work`, `go.work.sum`) and changed files outside any Go package force the full run; `go vet` runs over the same package set and new vet findings count as regressions. The baseline and current runs are both restricted to the same package set, so failures in run-omitted packages are not reportable as regressions; an invalid run stays warn-only. Invalid scope values are rejected on every surface (REST `400`, MCP/comm error reply, config load). The default stays `full` in the datawatch source tree until the smoke list in `docs/plans/2026-10-23-quality-gate-scope.md` is confirmed. Surfaces: REST `GET/PUT /api/config` + `POST /api/autonomous/prds/{id}/set_quality_gates` (PRD `quality_gates.scope`), MCP `autonomous_config_set` `quality_gates_scope` / `autonomous_prd_set_quality_gates` `scope`, CLI `autonomous config-set '{"default_quality_gates":{"scope":...}}'`, comm `configure autonomous.default_quality_gates.scope=…` / `autonomous set-quality-gates <id> scope=…`, PWA Settings selector (locale keys `settings_gate_scope`, `prd_task_gate_scope`). Operator guide: `docs/operations.md` → "Gate scope"; how-to: `docs/howto/quality-gate-scope.md`. |
| `autonomous.default_quality_gates.smoke_packages` | []string | built-in list | Smoke set for the `touched-plus-smoke` scope: Go package patterns that always run on a scoped gate. Empty list falls back to the PRD's `quality_gates.smoke_packages`, then this key, then `config.DefaultGateSmokePackages` (aliased as `pipeline.DefaultGateSmokePackages`): `./internal/autonomous/...`, `./internal/config`, `./internal/pipeline`, `./internal/server/...`, `./internal/mcp/...`, `./internal/observer/...`, `./internal/audit`, `./internal/metrics` (`internal/config/config.go`). REST `smoke_packages` accepts a list or a comma-separated string; MCP `quality_gates_smoke_packages` (config) / `smoke_packages` (per-PRD) likewise; comm takes a comma list. The proposal to confirm first is `docs/plans/2026-10-23-quality-gate-scope.md`. |
| `autonomous.gate_disagreement` | string | `"warn"` | Policy when a task's verifier passed but the quality gate confirmed a regression from a valid baseline (`config.AutonomousConfig.GateDisagreement`, read by `Manager.gateDisagreementPolicy`). `warn` records `task.gate_disagreement` plus a `gate_disagreement` decision and lets the task complete; `block` fails the task with a `gate_disagreement: …` error, with no auto-fix retry. Only `warn`/`block` are accepted (`400` on `PUT /api/config`, rejected on config load); an unrecognized stored value reads as `warn`. Never applies to an invalid gate. REST `autonomous.gate_disagreement`, MCP `autonomous_config_set` `gate_disagreement`, CLI `autonomous config-set`, comm `configure autonomous.gate_disagreement=block`, PWA Settings selector. |
| `autonomous.failure_escalation` | object | `{enabled: true, max_failures: 3, steps: []}` | Global failure escalation default (`config.AutonomousConfig.FailureEscalation`, read through `resolveFailureEscalation`). `enabled` turns the retry ladder on; `max_failures` (1 or more) is the failed-attempt count that holds a task for review; `steps` is the worker ladder (JSON array of `{backend, model, effort, attempts}`, or the compact text `backend:model[@attempts],...`, up to 8 steps). REST `GET /api/config` and `PUT /api/config` keys `autonomous.failure_escalation.enabled`, `.max_failures`, `.steps` (a bad value is a `400` naming the key, nothing applied); `PUT /api/autonomous/config` `failure_escalation`; comm `configure autonomous.failure_escalation.<key>=<value>`; CLI `datawatch autonomous failure-escalation global`; YAML `autonomous.failure_escalation`. Automaton and task overrides are set with `set_failure_escalation` (REST, MCP, CLI, comm). |
| `autonomous.questions.enabled` | bool | `false` | Master switch for the operator-question channel (`docs/plans/2026-10-25-operator-decision-channel-proposal.md`), off by default. Read live on every request: while off all six `/api/questions` endpoints answer `503` naming the switch, and flipping it on from any surface takes effect on the next request without a daemon restart. Surfaces: YAML `autonomous.questions.enabled`; REST `GET`/`PUT /api/config` key `autonomous.questions.enabled`; MCP `config_set` / `get_config` (the generic config tools); CLI `datawatch config set autonomous.questions.enabled true|false`; comm `configure autonomous.questions.enabled=true`; PWA Settings → General toggle (`settings_automata_questions_enabled`). The channel itself — raise, list, detail, answer, withdraw, answer log — is REST for this step; MCP tools, CLI and comm per-question verbs are deferred with their reasons in `docs/api-mcp-mapping.md` → Operator questions and `docs/parity-status.md`. On raise the recorded question is announced (each route below is toggleable): once to every PWA/hub WebSocket as `question_raised`, once per active messaging router as a plain line (`[hostname] question <id> — <label>`, a `(high priority)` marker, the text, the numbered options, the REST + PWA answer routes), and — only for `priority: "high"` — one warning-level alert (`GET /api/alerts`). Every announce is async and non-blocking; a missing comm backend is a silent no-op. On answer and on withdrawal the daemon emits the lifecycle events `question.answered` and `question.withdrawn` (both in the closed `KnownEventKinds` set, so lifecycle subscriptions with `filter.kinds` may name them; exactly-once — the emit is marker-keyed on the answer id and `ReconcileEmittedEvents` re-fires anything recorded but never delivered across a restart; `questionEventDetailsMax` = 512 chars caps the details). The default return path for an asker is events-plus-polling: `GET /api/questions/{id}` answers `answer_count`, `current_answer_id` and the standing answer's summary (verbatim quote, actor, surface) in one read. The opt-in per-question `DATAWATCH_ANSWER:` session line (raise with `return_path: true` while the question names a `session_id`) is sent exactly once per answer through the asking session's comm channel, best-effort — a lost line is one daemon warning, never a retry. How-to: `docs/howto/operator-questions-notifications.md`. Operator guide: `docs/operations.md` → "Operator questions". |
| `autonomous.questions.notify_messaging` | bool | `true` (when absent) | Per-raise toggle for the messaging-group announce (one line per active comm channel): false suppresses just that route and the WebSocket + alert routes are unaffected. Nil (key absent) = on; the `EffectiveNotifyMessaging()` resolver in `internal/autonomous/questions.go` maps nil → true. Same surfaces as the master switch, including PWA Settings → General and comm `configure`. `internal/server/question_raise_dispatch.go` gates the per-router fan-out. |
| `autonomous.questions.notify_alert` | bool | `true` (when absent) | Per-raise toggle for the warning-level alert — which also fires only for `priority: "high"` raises. Nil = on (`EffectiveNotifyAlert()`); `internal/server/question_raise_dispatch.go` gates the alert insert. |
| `autonomous.questions.return_path` | bool | `true` (when absent) | Toggle for the opt-in `DATAWATCH_ANSWER:` return-path line that an answer writes into the asking session (only questions raised with `return_path: true` and a `session_id` are eligible at all — the key is a global on/off, not a per-question default). Nil = on (`EffectiveReturnPath()`); `internal/autonomous/question_return_path.go` builds the line (one physical line, one-line JSON with the question id, answer id, chosen option, the verbatim quote, actor and timestamp — exactly those six fields) and `internal/server/questions.go` wires it in via `SetQuestionAnswerReturnPath`. Off = no line for any question. |
| `autonomous.questions.worker_marker` | bool | `false` (when absent) | Whether a `DATAWATCH_QUESTION:` marker line in a worker session's output is acted on. Absent (nil) is off: `config.AutonomousQuestionsConfig.EffectiveWorkerMarker()` returns false unless the pointer is set and true. It does nothing while `autonomous.questions.enabled` is off; `cmd/datawatch/main.go` hands `session.Manager.SetWorkerQuestionGate` a closure that reads both switches from the live config at every event, so a write takes effect with no restart. A valid line is raised by `autonomous.RaiseWorkerQuestion` through `QuestionStore.RaiseUnlessOpenSourceLine` (deduped on session id plus the line's SHA-256) and announced by the same fan-out a REST raise runs (`HTTPServer.NotifyQuestionRaised`); a refused line is audited as `question_marker_refused` (session id, reason category, line hash; never the text). A question with `blocking: true` parks its task with the `await_decision: <question id>` wait reason and the coordinator tick (`Manager.QuestionTick`) resumes it from the answered and withdrawn questions it reads through `QuestionStore.List`. Surfaces: YAML `autonomous.questions.worker_marker`; REST `GET`/`PUT /api/config`; MCP `config_set` / `get_config`; CLI `datawatch config set autonomous.questions.worker_marker true\|false`; comm `configure autonomous.questions.worker_marker=true`; PWA Settings → Autonomous Automata planning (`settings_automata_questions_worker_marker`). How-to: `docs/howto/operator-questions-workers.md`. |
| `autonomous.max_engines_per_node` | int | `1` | Local-model sessions one compute node runs at once. Read in `cmd/datawatch/capacity_wiring.go` (`syncPools`) to set the `node:<name>` pool limit for nodes that host local engines; a node's own `max_concurrent_sessions` stays a ceiling when smaller. Values below `1` clamp to `1` (`internal/config/config.go`). Only local-engine sessions (`pi`, `pi-prompt`, `ollama`, `opencode`, `opencode-acp`) are gated; hosted backends are not. Waiting sessions show `waiting for node <name>`. Operator guide: `docs/operations.md` → "Compute-node queue". |
| `autonomous.max_engines_per_node` | int | `1` | Local-model sessions one compute node runs at once. Read in `cmd/datawatch/capacity_wiring.go` (`syncPools`) as the fallback slot count of every compute node that hosts a local engine and has neither observer GPU data nor system memory data (`compute.NodeSlots`, `internal/compute/slots.go`) to set its `node:<name>` pool limit; a node's own `max_concurrent_sessions` stays a ceiling when smaller. Values below `1` clamp to `1` (`internal/config/config.go`). Only local-engine sessions (`pi`, `pi-prompt`, `ollama`, `opencode`, `opencode-acp`) are gated; hosted backends are not. Waiting sessions show `waiting for node <name>`. Operator guide: `docs/operations.md` → "Compute-node queue". |
| `autonomous.capacity_rate_requests` | int | `0` | Rate ceiling on lease grants per rolling window, applied to each `llm:<name>` pool. `0` = no requests ceiling. Negative values are rejected (config load, REST, MCP, CLI). Pushed into the ledger with `Ledger.SetRate` by `syncPools` in `cmd/datawatch/capacity_wiring.go` on startup and every 10 s, so config edits apply without a restart. |
| `autonomous.capacity_rate_tokens` | int | `0` | Rate ceiling on tokens per rolling window for each `llm:<name>` pool. `0` = no tokens ceiling. Usage is counted by `Ledger.RecordTokens`, called from the usage listener (`recordUsageSpend`) with each event's `TokensIn + TokensOut`. |
| `autonomous.capacity_rate_window_seconds` | int | `0` | Rolling window length for the two ceilings above; `0` = `3600`. Matters only when a ceiling is set. Surfaces: `GET/PUT /api/config` and `/api/autonomous/config`, MCP `autonomous_config_get/set`, CLI `autonomous config-set --capacity-rate-requests/--capacity-rate-tokens/--capacity-rate-window-seconds`, comm `configure autonomous.capacity_rate_*`, Settings → Autonomous. |
| `autonomous.injection_guard` | bool | `false` | Scan user-supplied PRD/task specs for prompt injection phrases at the API boundary on create and edit. Findings are always logged. |
| `autonomous.block_on_injection` | bool | `false` | When true (requires `injection_guard: true`), requests containing injection phrases are rejected with HTTP 400 instead of warn-only. |
| `autonomous.notify.default_events` | []string | `[prd.needs_review, prd.blocked, prd.failed, prd.completed, prd.cancelled]` | Global default set of lifecycle event kinds (see `autonomous.DefaultNotifyEvents`) — the third tier of the notify-event resolution, after an Automaton's own `notify_events` list and its guardrail profile's `notify_events` list (internal/autonomous/notify_events_resolve.go). Unset = the shipped five-kind baseline; an explicit empty list (`default_events: []`) is the operator opt-out (the global tier then resolves to no kinds). Lifecycle sinks (alert-store write, topic push to `alerts` + `automaton-<id>`, comm fan-out) act on the kinds listed. The global `notify_exclude` still applies to the comm fan-out |
| `autonomous.project_write_serialization` | string | `advisory` | BL428 §3a (docs/plans/2026-10-14-concurrency-and-budget-design.md): how Automaton task writes targeting the same project tree are serialized. `advisory` (default) = exclusive per-canonical-tree lock at admission — same-tree tasks queue, different trees run in parallel. `parallel` = RESERVED but DISABLED — recognized and accepted until the mechanism lands in the follow-up implementation PRD; the daemon still enforces the advisory lock and logs a startup warning. Any other value is a config error and the daemon refuses to start |
| `autonomous.refuse_dirty_tree_start` | bool | `false` | BL428 §3a ii admission-time companion: when true, task admission hard-fails if the PRD's project tree has uncommitted changes (`git status --porcelain` non-empty). The error names the dirty file count and the options: commit or stash first, `git clean`, or set this key back to `false` |
| `autonomous.stuck_running_seconds` | int | `900` | Stuck-running watchdog bound, in seconds. An Automaton that stays running with every task finished, no live worker session and no live executor for this long has its completion roll-up re-fired once; after a second full bound in the same state it moves to blocked. `0` or negative uses the default. Reachable from the PWA settings card, `PUT /api/config`, the MCP `autonomous_config_set` and `config_set` tools, the CLI `datawatch config set` and `configure autonomous.stuck_running_seconds=<n>` in a comm channel. |
| `autonomous.stale_task_seconds` | int | `900` | Stall window for a running worker, in seconds. Activity-based: a worker counts as stalled only when it has no output, no live child process, no backend activity, and no file change in its work directory for this many seconds. `0` or negative inherits `session.stale_timeout_seconds`. |
| `autonomous.task_max_attempt_seconds` / `autonomous.task_no_progress_seconds` | int | `0` / `0` | Optional per-attempt bounds (both off when `0`). `task_max_attempt_seconds` is the wall-clock cap on one attempt (recommended `5400`); `task_no_progress_seconds` stops an attempt that goes N seconds without progress — terminal output growth, a changed tracked/untracked file in the work directory (harness scratch files ignored), a new commit, or a live child process using CPU (recommended `1800`). On firing, the uncommitted work is saved to a ref before the worker is stopped and never discarded, and the normal retry budget applies. |
| `autonomous.preserve_attempt_work` | bool | `true` | When a stalled or killed attempt is stopped, its uncommitted work is saved to `refs/datawatch/attempts/<task>/<n>` and restored on the next attempt, unless that attempt was started with `clean_restart` (`reset_task`). `false` disables the save and restore. |
| `autonomous.tags.default_view` | string | `auto` | Which view the Automata list opens on: `auto` (the attention view when at least one Automaton needs attention, otherwise the tag browser), `attention` (always the attention view) or `tags` (always the tag browser). Any other value is refused. Set through REST `PUT /api/config` `autonomous.tags.default_view`, MCP `config_set`, CLI `datawatch config set autonomous.tags.default_view <value>`, comm `configure autonomous.tags.default_view=<value>`, and Settings → General → Automata → Tag browser. |
| `autonomous.tags.attention_statuses` | []string | `[blocked, needs_review, paused]` | Automaton statuses that place an Automaton in the attention group. Valid statuses: `draft`, `planning`, `needs_review`, `approved`, `revisions_asked`, `running`, `completed`, `rejected`, `cancelled`, `archived`, `blocked`, `failed`, `paused`. Any other entry is refused; an empty list takes the default statuses. A paused Automaton and an Automaton with a task held for operator review are always in the attention group. Same surfaces as `autonomous.tags.default_view`. |
| `autonomous.tags.default_match_mode` | string | `any` | How multiple selected tags combine in the tag browser: `any` widens the list (at least one selected tag), `all` narrows it (every selected tag). Only `any` or `all` are accepted. Same surfaces as `autonomous.tags.default_view`. |
| `autonomous.tags.direct_counts_visible` | bool | `false` | When `true`, each tag shows its direct count as a tooltip beside the subtree count. Same surfaces as `autonomous.tags.default_view`. |
| `autonomous.tags.registry_keeper` | string | `""` | Federation peer name of the daemon that keeps the shared tag registry. Empty means this daemon is its own keeper and nothing syncs. Same surfaces as `autonomous.tags.default_view`. |
| `autonomous.tags.registry_sync_seconds` | int | `30` | Interval, in whole seconds, at which the shared tag registry syncs with the keeper peer named in `autonomous.tags.registry_keeper`. Must be 5 or more; a smaller value is refused. Same surfaces as `autonomous.tags.default_view`. |
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

### Verifier no-progress watchdog (autonomous.verifier_no_progress_timeout_seconds)

- **Field:** `autonomous.verifier_no_progress_timeout_seconds`, default `900` (15 minutes); zero or negative normalizes to the default (`NormalizeVerifierNoProgressTimeoutSeconds` in `internal/config/verifier_no_progress_timeout.go`). Surfaces mirror the session idle timeout: config YAML, REST `GET`/`PUT`, MCP `autonomous_config_set`, the web settings card (5 locales, label key `settings_verifier_no_progress_timeout`).
- **Confirmed answers (operator, 2026-10-24):** a live runner process **or** a held capacity slot for the verifier's holder counts as progress (options B plus C); an active model-server request is deferred until a per-session request probe exists. The limit keeps its single setting and the default rose to 900 seconds. Time spent waiting for a capacity slot is excluded from the limit, and the docs say so. A trip hands off to the fallback verifier backend at once under the existing infra escalation threshold (option C, differing from the originally proposed option A).
- **Decision:** `VerifierWatchdog` (`internal/autonomous/verifier_watchdog.go`) receives one observation per poll with every available liveness signal in a `ProgressSignals` value — output-file presence, a fingerprint of the captured pane output, whether the runner process is alive, whether an active model-server request is reported, and the capacity-slot state. Progress — the output file appearing, a pane fingerprint changing, a live runner process, or a held capacity slot — resets the timer. Time spent waiting for a capacity slot is excluded from the limit, and the wait itself never trips it, so the bound measures the run rather than the queue. The active-requests signal is plumbed but not yet counted until a reliable per-session request probe exists. The decision function takes the clock as an injected parameter and never reads the wall clock, so the rule is deterministic in tests.
- **Hook point:** the poll loop in `cmd/datawatch/session_judgment.go`, verifier sessions only (`autonomous:verify:*`). A trip ends the session through the deferred kill and returns an `OneShotNoOutputError` reading `verifier no-progress watchdog: no output file or pane activity for 15m0s`, which classifies as infra like the timeout — so a session that keeps producing output is never ended, and repeated watchdog trips on one task escalate to the fallback backend under the same rule as other infra failures.

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

## Lane fill (autonomous.lane_fill and the per-Automaton lane preference)

- **Config block:** `autonomous.lane_fill` (`config.AutonomousLaneFillConfig` in `internal/config/config.go`, bridged by `LaneFillConfigFromYAML` in `internal/autonomous/lane_fill_config.go`). Keys: `mode` (`off`, `shadow`, `on`; empty means shadow), `gpu_lanes` (default 2), `claude_lanes` (default 3), `idle_alert_minutes` (default 30, 0 = off), `decompose_ahead_min` (default 2), `poll_seconds` (default 60, minimum 5), `gpu_backend` (default `pi-prompt`), `gpu_model` (empty), `claude_backend` (default `claude-code`), `count_verifier_wait` (default true). The `Effective*` helpers apply the defaults; validation is in `validate()` and `ValidateLaneFillMode`.
- **Field: `count_verifier_wait`:** `config.AutonomousLaneFillConfig.CountVerifierWait` in `internal/config/config.go`, a `*bool` with `omitempty` — the lone pointer in the block, so an absent key and an unset (nil) value both read as true via `EffectiveCountVerifierWait` (in `internal/config/config.go`, forwarded through `LaneFillConfig` in `internal/autonomous/lane_fill_config.go`) while an explicit false survives a round trip. When true, a Claude lane whose occupant is an Automaton that is blocked only while waiting on a verifier slot (`WaitingOnVerifier` on the lane occupant, filled in by `internal/autonomous/lane_fill_verifier_wait.go`) counts as available within the Claude lane count: `laneFree` in `internal/autonomous/lane_fill.go` reports such a lane as free to the planner, and the status view shows it with state `waiting_verifier`. When false the lane stays busy, as before; GPU lanes never free under the rule. The mode still decides whether anything starts — shadow starts nothing.
- **Per-Automaton field:** `lane_preference` on the Automaton record (`internal/autonomous/models.go`, `LanePreference`), with `gpu`, `claude` or `any`. Empty and absent mean `any`. Normalised by `internal/autonomous/lane_preference.go`.
- **Planner:** `PlanLaneFill` in `internal/autonomous/lane_fill.go` is a pure function over a snapshot (lanes, candidates, pool evidence, time). It does no I/O and starts no goroutine.
- **Coordinator:** `laneFillPoll` in `internal/autonomous/lane_fill_coordinator.go` runs on each poll. `off` returns before anything is read. `shadow` records decisions with `executed: false`. `on` carries them out through the starter's run call, the decompose call (in the background), and, where needed, the approve and backend-change calls, which are recorded under the actor `lane_fill`.
- **Records:** decisions are kept in `lane_fill_decisions.json` in the autonomous data directory, bounded to 1000 newest first (`internal/autonomous/lane_fill_records.go`). Each new record is audited as action `lane_fill`.
- **Evidence:** `NewPoolEvidenceSource` in `internal/autonomous/lane_fill_evidence.go`, wired in `cmd/datawatch/lane_fill_wiring.go`. Counts running sessions on the GPU backend or on a pool host, reads loaded models from each pool host's Ollama `/api/ps`, caches for 5 seconds, and reports partial data with the failing host named.
- **Idle alert:** `laneFillCheckAlerts` in `internal/autonomous/lane_fill_alert.go`, sent through the existing alert path at warning level.
- **Surfaces:** REST `GET /api/autonomous/lane-fill` and `POST /api/autonomous/prds/{id}/set_lane_preference` (`internal/server/lane_fill.go`); MCP `autonomous_lane_fill_status` and `autonomous_prd_set_lane_preference` (`internal/mcp/lane_fill_tools.go`, federation capability table in `internal/federation/mcp_tool_caps.go`); CLI `datawatch autonomous lane-fill` and `prd-set-lane-preference` (`cmd/datawatch/cli_lane_fill.go`); comm `autonomous lane-fill` and `set-lane-preference` (`internal/router/lane_fill_comm.go`); PWA lane card and preference select (`internal/server/web/app.js`).
- **Parity:** `cmd/datawatch/lane_fill_parity_test.go` maps each REST operation to its MCP tool, CLI subcommand and comm verb.

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
- **Report:** `internal/autonomous/merge_safe_delete.go` builds the safe-to-delete report (`for-each-ref`, `worktree list`, `rev-list`, `cherry`, `log`, `status`). It runs git when read and issues no command that changes anything. `merge_status.go` holds the merge status model, `merge_text.go` the shared CLI/comm/alert wording.
- **Surfaces:**
  - REST `GET /api/autonomous/merge` and `GET /api/autonomous/safe-to-delete` (`internal/server/merge.go`), GET-only, 503 while Automata are disabled;
  - MCP `autonomous_merge` and `autonomous_safe_to_delete` (`internal/mcp/merge_tools.go`), federation capability `autonomous:read` (`internal/federation/mcp_tool_caps.go`);
  - CLI `datawatch autonomous merge [--json]` and `datawatch autonomous safe-to-delete [--json]` (`cmd/datawatch/cli_merge.go`);
  - comm `autonomous merge` and `autonomous safe-to-delete` (`internal/router/merge_comm.go`), plus the alert text;
  - PWA merge badge and safe-to-delete card (`internal/server/web/app.js`), with the `merge_*`, `safe_delete_*` and `settings_automation_merge_pending_*` strings in the five locale bundles (en, de, es, fr, ja).
- **Parity:** the REST/MCP/CLI/comm mapping is asserted by `internal/mcp/merge_tools_test.go`, `internal/router/merge_comm_test.go` and `cmd/datawatch/cli_merge_test.go`; mobile parity issue datawatch-app#304 (`docs/plans/parity-issues/2026-10-29-automation-merge-pending-parity.md`). Operator reference: *Merge-pending check* in `docs/operations.md`; data flow: `docs/flow/automation-merge-pending-flow.md`.

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
