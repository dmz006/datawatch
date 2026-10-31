# Operations Guide — datawatch

---

## Configuration Methods

All settings in this guide can be changed through any of these methods:

| Method | Example |
|--------|---------|
| **YAML config** | Edit `~/.datawatch/config.yaml` directly |
| **Interactive wizard** | `datawatch setup signal`, `datawatch setup web`, etc. |
| **Web UI** | Settings tab in the PWA (saves via REST API) |
| **REST API** | `PUT /api/config` with JSON body |
| **Chat commands** | `setup signal`, `setup web` from messaging channels |

Changes to most settings take effect immediately. Some (host, port, TLS) require a daemon restart — the web UI shows a restart hint when needed.

---

## 1. Service Management

### System service (Linux, installed with root)

```bash
# Start the daemon
sudo systemctl start datawatch

# Stop the daemon
sudo systemctl stop datawatch

# Restart the daemon (picks up config changes)
sudo systemctl restart datawatch

# Check daemon status
sudo systemctl status datawatch

# Enable automatic start on boot
sudo systemctl enable datawatch

# Disable automatic start on boot
sudo systemctl disable datawatch

# Follow live logs
journalctl -u datawatch -f

# Show last 100 log lines
journalctl -u datawatch -n 100

# Show logs since last boot
journalctl -u datawatch -b
```

### User service (Linux, no root required)

```bash
# Start the daemon
systemctl --user start datawatch

# Stop the daemon
systemctl --user stop datawatch

# Restart the daemon
systemctl --user restart datawatch

# Check daemon status
systemctl --user status datawatch

# Enable automatic start at login
systemctl --user enable datawatch

# Dedicated tmux server that hosts every session (installed alongside
# datawatch.service by install.sh). It has its own cgroup and Restart=always,
# so sessions survive daemon restarts and are not tied to any login session.
systemctl --user status datawatch-tmux
TMUX_TMPDIR=~/.datawatch/tmux tmux ls    # plain `tmux ls` won't show sessions

# Follow live logs
journalctl --user -u datawatch -f

# Show last 100 log lines
journalctl --user -u datawatch -n 100
```

Note: For user services to start at boot (without login), enable lingering:
```bash
sudo loginctl enable-linger $USER
```

**Keeping scripts away from the live tmux server.** PRD workers, verifiers and guardrails run with `TMUX` and `TMUX_TMPDIR` removed. Scripts and tests must never run `tmux kill-server` or `tmux kill-session -a` except against an explicit `-S` throwaway socket or under `env -u TMUX -u TMUX_TMPDIR`; setting `TMUX_TMPDIR` alone is not enough. `scripts/check-no-unscoped-tmux.sh` enforces this in `make lint`, in CI and at decompose time. See [testing.md](testing.md#tmux-safety-rules-for-tests-and-scripts).

**Finding and ending leftover test daemons.** If a job has left one of the daemons it started from a throwaway config under a temp path alive (a sandbox daemon under the temp dir, an end-to-end daemon from a `datawatch-<hex>` run folder), `env -u TMUX -u TMUX_TMPDIR bash scripts/check-no-leftover-test-daemons.sh check <baseline>` (or `make check-leftover-daemons LEFTOVER_BASELINE=<snapshot>`) reports it — PID, age, config path, listening TCP ports; with the default empty baseline that is every live test daemon at that moment, including the daemons of other sessions on a shared host, and the report says so. A daemon is named as a test daemon only while its live command line carries `start --foreground --config <path>` under temp or run-folder space, so your own daemon and any multiplexer server are never in that list. To end one that a job of yours owns: if it still serves its API, end the sessions it started through that API first; then re-read its `/proc/<pid>/cmdline` to confirm the throwaway config, signal that single recorded PID (SIGTERM first, SIGKILL only if it does not exit within a few seconds), and confirm the process is gone and its ports are closed — never by name pattern. The static audit `scripts/check-test-daemon-cleanup.sh` (`make check-test-daemons`, part of `make lint`) enforces the same duty on every script, and the full rules — including the marker scope and the mid-run baseline behaviour — are in [the testing guide](testing.md#test-daemon-cleanup-rules).

### Direct invocation

```bash
# Start daemon (background, PID written to ~/.datawatch/daemon.pid)
datawatch start

# Stop daemon (sends SIGTERM, removes PID file)
datawatch stop

# Stop daemon and kill all active sessions
datawatch stop --sessions

# Start in foreground (log to stdout, no PID file)
datawatch start --foreground

# Start in foreground with verbose logging
datawatch start --foreground --verbose

# Start with encrypted config (requires --foreground for interactive password)
datawatch --secure start --foreground

# Start with a custom config file
datawatch start --config /path/to/config.yaml
```

**Daemon mode details:**
- Logs are written to `~/.datawatch/daemon.log`
- PID is written to `~/.datawatch/daemon.pid`
- **PID lock:** On startup, datawatch checks `daemon.pid` — if the PID is still running, it refuses to start a second instance. This prevents multiple daemons from competing for the same sessions and Signal connection.
- Use `datawatch stop` to send SIGTERM gracefully
- Daemon mode is incompatible with `--secure` (encrypted config); use `--foreground` instead

---

## 2. Upgrading

### Check for updates

```bash
# Check only — prints latest version and whether an upgrade is available
datawatch update --check

# Download and install the latest version
datawatch update
```

`datawatch update` queries the GitHub releases API and, if a newer version is found, runs `go install github.com/dmz006/datawatch/cmd/datawatch@vX.Y.Z`. The binary is replaced in-place; the running daemon is unaffected until it is restarted.

### In-place upgrade procedure

```bash
# 1. Check what version is running and what's available
datawatch update --check

# 2. Stop the daemon gracefully (preserves tmux sessions)
datawatch stop
# or: sudo systemctl stop datawatch

# 3. Install the new binary
datawatch update

# 4. Restart the daemon
datawatch start
# or: sudo systemctl start datawatch
```

Active tmux sessions survive the daemon restart — they are independent OS processes. Sessions in `running` or `waiting_input` state continue uninterrupted, and the daemon re-attaches to them on startup. The exact guarantees, and the cases that do not hold, are in [Restart safety](#restart-safety) below.

### Restart safety

A restart (`systemctl --user restart datawatch`, `datawatch restart`, or a stop and start) does not require waiting for work to finish first, for the tmux-backed sessions that are the default. The table states what is verified and what enforces it.

The first two rows were observed on sandbox daemons during the restart audit, by signalling the daemon and relaunching it. The systemd unit path was not exercised there. The live test is the check to re-run on any daemon before relying on them. The release build has not yet been run through that live test, so the lease and subprocess rows are confirmed by unit tests only until it is.

The restart matrix (`scripts/restart-matrix.sh`, see [The restart matrix](#the-restart-matrix)) is the test that enforces the in-flight rows below. It restarts a throwaway daemon with one unit of autonomous work in each state and records what became of it; the release smoke runs it with `--strict` for the states that must survive.

| What survives a restart | Status | Enforced by |
|---|---|---|
| tmux pane and the LLM process inside it | Verified. The pane and process stay up across the restart. | `TestBL462_DaemonRestartSurvivesLive` (opt-in, below) |
| Session state and output monitoring | Verified. A live session stays `running` or `waiting_input`, and its log keeps growing after the restart (the output pipe is re-attached). | `TestBL462_DaemonRestartSurvivesLive` (opt-in, below) |
| Capacity-ledger lease for a PRD task on a live session | Fixed. A task in `verifying` on a live session has its lease re-adopted at boot, so the host pool counts it. Before the fix, the lease was dropped and the pool undercounted. | Unit tests `TestAdoptRestoresLeaseOverLimitWithoutWaiting`, `TestAdoptIsIdempotent` and `TestCapacity_BootResumeReadoptsLiveTaskLease`. The live test does not check leases. |
| Task completion and verification | Observed. A PRD task that was `verifying` on a live session ran to completion after two daemon restarts in the audit. | Live audit only. The live test does not check task completion. |
| Task whose worker is running | Verified on a sandbox daemon. The worker session is re-attached, the task keeps its session and capacity lease, and the worker is not started again. | Restart matrix state `worker-running` |
| Task waiting for capacity, or parked on an external condition | Verified on a sandbox daemon. The wait is rebuilt at boot from the live holder session; the task is admitted or released once and the worker runs once. The variant where a parked task gives up its host slot and waits for re-admission behind another task is not exercised. | Restart matrix states `waiting-capacity`, `waiting-external` |
| Two workers placed on different compute nodes | Verified on a sandbox daemon. Each node pool still holds its one worker, every lease keeps the node it was placed on and none is flagged `node_unknown`, and the worker sessions and processes are the same and are not started again. | Restart matrix state `worker-node-placement` |
| Task whose worker finished while it was verifying | Verified on a sandbox daemon. The task stays `verifying` with its worker session and result; no retry is counted and the worker is not run again. Only the verification runs again. | Restart matrix states `verify-script`, `verify-script-mid`, `final-gate`, `verify-session`; unit test `TestReconcileStuckKeepsFinishedWorkerVerifying` |
| Automation with active branches | Verified on a sandbox daemon. Both cursors come back, a branch with a live worker is kept, a branch that was verifying is verified once, and the join is released. | Restart matrix state `automation-branches` |
| Scheduled job that came due while the daemon was down | Verified on a sandbox daemon. The job fires once after boot and is marked done. | Restart matrix state `scheduled-due` |
| Open WebSocket clients | Verified on a sandbox daemon. Clients reconnect on their own and get a full session snapshot first. Events emitted while the daemon was down are not replayed, so clients must not assume gap-free delivery. | Restart matrix state `websocket-clients` |
| Git commit in flight | Verified for the task-start snapshot commit only. Git removes its own index lock on TERM, the task is reset to pending, and the commit is made once by the new run. A merge, a push and the commit of a worker's output are not measured. | Restart matrix state `git-operation` |
| Task whose first worker launch was held, so its pane holds only a bare shell | **Verified once on a sandbox daemon, after the fix.** The bare shell is ended at boot and the task runs again on a new session, with no retry counted. The run before the fix failed: the bare shell was kept as the live session. A `--strict` rerun is still owed. | Restart matrix state `restart-during-launch` (recorded FAIL before the fix; one run after it passed 10 of 10; see `docs/plans/restart-matrix-findings.md`) |

**A launch interrupted by a restart.** A worker whose agent never started before the restart (its pane holds only a bare shell) is not adopted. At boot the daemon ends that shell and resets the task to pending with the reason `session never started`. No retry is counted, and the task is launched once more. A task that was `verifying` on such a session is requeued the same way. A live session is adopted only once its agent has started, which the daemon checks by looking for the agent process, a channel-ready event, a delivered task or a first assistant turn, sampling three times a second apart so a snapshot taken between ticks is not read as a bare shell.

**Start-confirm bound for launched and adopted sessions.** A claude-code worker has one start-confirm bound whether the daemon launched it or adopted it after a restart: `autonomous.session_start_confirm_seconds`, default 120 seconds, measured from the last evidence of life. For an adopted worker that is the latest update, channel event or log write the session left before the restart; when none is known, the clock starts at the task's launch time. A launched worker has no evidence of life until its first sign of work, so its clock starts at the first check that finds it silent. If the session is still in `waiting_input` with no assistant output past that limit, it is ended and the start counts as a failure against the start retry budget: two requeues, then the task fails. Once a first assistant turn appears, the bound no longer applies. Other backends are not held to it, because their start cannot be confirmed from the assistant marker.

**Shutdown drain.** On a stop, the daemon first waits (at most 6 seconds) for the HTTP server to shut down, so no new launch arrives over HTTP. It then refuses new session launches and gives each launch still running a fixed grace of 15 seconds (`shutdownLaunchGrace` in `cmd/datawatch/shutdown_launch.go`; it is not a config key). A launch still running after that is rolled back, and the daemon logs `[session] rolled back launch <id>` for each one. The rollback's own settle wait (about 2 seconds) comes on top, so the stop stays well inside the 90-second default systemd stop timeout.

**What does not survive.** Two things, both listed as open choices in `docs/plans/restart-matrix-findings.md` and neither changed:

- **Planning.** A PRD whose decompose was running is reset to `draft` and its decompose session is ended, because the code that would read the plan was inside the stopped daemon. The PRD waits for the operator to plan again; nothing is planned automatically.
- **Daemon-run verification scripts.** A verification script, and a final-gate script, run as a direct child of the daemon with no durable record. A restart ends it (or leaves it orphaned, see the KillMode note) and the verification runs the script again from the start. The worker is not run again beside it. A script that is slow or has effects that cannot be repeated should not be relied on to survive a restart.

One more case runs part of the work again: a task verifying through a live verifier session keeps its worker, retry count and session count, but the verifier that was in flight is not adopted and a second verifier is started once.

**Tasks that were mid-run.** A task whose worker session is alive is not interrupted: the daemon re-attaches to the session at boot and the executor waits on it. A task that had no session yet (for example one inside its start-of-task commit) is reset to pending at boot and starts once, since there is nothing to re-attach to.

**Tasks that were verifying.** A task whose worker had finished and whose result was not yet recorded is kept at boot. Only its verification runs again. The worker session is not ended and no retry is counted. Reading the worker's recorded output for the completion marker depends on the shared output cleaner keeping every real line between two terminal escape sequences; it now does.

**KillMode.** The systemd unit uses the default `KillMode=control-group`: a stop or restart sends TERM to every process in the unit's control group, not only the daemon. Workers, verifiers and planners live in a multiplexer server outside the daemon's tree and are not affected. A process the daemon starts directly (the verification script, a git command) is ended, so its work starts again. With a restart that signals only the daemon process (`datawatch restart`, a `KillMode=process` unit), such a child is orphaned and can keep running beside its own re-run. The matrix simulates the control-group case by sending TERM to the daemon and to its direct children by pid (`RM_KILL_MODE`); it is a stand-in, not the service manager, and covers TERM only (not a crash, a kill signal, an out-of-memory kill or a reboot).

**Subprocess sessions** (`subprocess: true`, no tmux pane). A subprocess session records its process ID and exit status in its session directory. At boot, the daemon re-attaches to a subprocess that is still running and keeps the session `running` until the process exits, then records the exit status. If the process ended while the daemon was down, the recorded status is used. The session is marked `failed` only when the process is gone and no exit status was recorded. This restart behaviour is fixed but not yet confirmed by the live test. A systemd stop or restart kills the daemon's child processes too (the unit's default `KillMode=control-group`), so a subprocess survives only a restart that signals the daemon process alone.

**Encrypted sessions** (`--secure`). Not covered by this guarantee. At boot the daemon does not re-attach the output pipe for encrypted sessions, so their output may stop after a restart. After restarting a `--secure` daemon, check that each session that matters is still producing output, and restart any session whose output has stopped.

**Long task text and no-override restarts.** Restarting a session is no longer limited by how long its task text is. The claude-code backend, and the aider and goose-prompt one-shot paths, deliver the task through a scratch file that the typed command reads, so the full text is not typed inline. A no-override restart of a resumed conversation therefore no longer resends the original task. If a tmux command still fails, the returned and logged error now carries tmux's own stderr text (for example `command too long`) instead of a bare exit status.

#### The restart matrix

`scripts/restart-matrix.sh` is the test that enforces the table above. It puts one unit of autonomous work into each of fourteen states, restarts a sandbox daemon, and records session ids, process ids, retry counts, duplicates, lost output and work redone, before and after. Each state runs against its own throwaway daemon (generated config, own data dir, free ports, TLS off, own multiplexer socket directory). It refuses to start when a target matches the production daemon, its ports or its data directory, and it never touches them. No real model is used: planner, worker, verifier and guardrail judge are one fake backend, so the matrix measures the daemon's restart handling, not model behaviour.

```bash
scripts/restart-matrix.sh --list                      # state names
scripts/restart-matrix.sh --strict                    # exit 1 if any state fails
scripts/restart-matrix.sh --strict --only worker-running,scheduled-due
RM_KILL_MODE=process scripts/restart-matrix.sh --only verify-script   # daemon pid only
```

Set `DATAWATCH_BIN` to a daemon binary built from the tree under test. A full run takes about 25 minutes. The per-state result, the measured cells and the open choices are in `docs/plans/restart-matrix-findings.md`.

`scripts/release-smoke.sh` runs the matrix as section 80, in its own sandboxes and never against production. It runs `--strict` for the states that must survive (`worker-running`, `waiting-capacity`, `waiting-external`, `git-operation`, `automation-branches`, `scheduled-due`, `websocket-clients`, `worker-node-placement`). It skips with a reason the states still left as an open choice (`planning`, `verify-script`, `verify-script-mid`, `final-gate`) and `verify-session`, whose in-flight verifier is not adopted. Skipping is not a pass: those rows stay in the table above as not surviving until a choice is made and the skip is removed. `SMOKE_RESTART_MATRIX=0` turns the section off; `SMOKE_RESTART_TIMEOUT` bounds each state (default 900 seconds).

**Confirming on a live daemon.** `TestBL462_DaemonRestartSurvivesLive` in `internal/session/restart_safety_live_test.go` restarts a daemon and checks that a live probe session survives it. It is skipped unless `DW_LIVE_RESTART_TEST=1` is set, so `go test ./...` does not run it. It needs `DW_BASE` set to the daemon under test, and it must run on the daemon's host with the same `TMUX_TMPDIR`. The default restart path is `systemctl --user restart datawatch`. Use `DW_LIVE_RESTART_CMD` only for a sandbox daemon that systemd does not manage. Run it against a sandbox, never against production.

### Daemon restart and in-flight Automata

Automata (PRDs) do **not** silently resume across a daemon restart. The daemon only auto-resumes a PRD that was in `running` state when it stopped, and only if that work still looks fresh: the PRD's newest recorded activity must be within `autonomous.resume_max_age_seconds` (default 3600 — one hour; set `0` to never auto-resume, `-1` for no age limit), and the PRD's project directory must still exist on disk (a removed temp workspace means the work cannot continue in place). A PRD that fails either check is **parked**: it is set to `blocked` with a plain-English reason, an operator alert is raised, and it does **not** run. A PRD still in `planning` state (decompose in flight when the daemon stopped) is reset to `draft` instead so you can re-run planning, and a `failed` PRD is recovered only when all of its failed tasks were killed by the daemon with no recorded result.

**What the startup log says.** Each decision is printed as it happens and summarized at the end:

```
[autonomous] boot-resume: prd=a1b2c3 resumed age=412s reason=last activity 412 seconds ago is within the 3600-second resume limit and its project directory still exists
[autonomous] boot-resume: prd=d4e5f6 parked age=86400s reason=last activity was 86400 seconds ago ...
[autonomous] boot-resume: resumed 1, parked 1
```

**Seeing what the last restart did.** The per-PRD boot-resume report for the current boot — outcome (`resumed` or `parked`), activity age in seconds, and the reason — is on the loop status: `GET /api/autonomous/status` (field `boot_resume`), the same data `datawatch autonomous status` prints, and as the `boot_resume` array on the loop-state JSON. A parked PRD also shows its reason as its blocked reason on its card in the PWA Automata view.

**Resuming a parked PRD by hand.**

1. Read the blocked reason first (PWA Automata card, or the `boot_resume` entry above). It names exactly why the PRD was held.
2. If the reason is a missing project directory but the project still exists at another path, re-point the PRD at the live directory (PRD settings in the PWA detail view) — the gate checks the directory at startup only, but the executor also needs it to exist while the work runs.
3. Open the blocked PRD in the PWA and use **Retry** on the stopped task(s) in its stories. Retrying a failed or blocked task is allowed while the PRD is blocked, re-queues it to pending, moves the PRD back to `running`, and the executor picks it up — completed work is kept.
4. From a bare daemon or a shell, kick the executor the same way: `datawatch autonomous prd-run <id>` (`POST /api/autonomous/prds/<id>/run`) once the PRD is back in a runnable state.
5. If the work is stale or no longer wanted, or the project directory is genuinely gone, do not resume: cancel the PRD (PWA or `DELETE /api/autonomous/prds/<id>`) so it stops showing as blocked.

If you routinely see PRDs parked for age, either raise `autonomous.resume_max_age_seconds` (or set it to `-1`) or resume them by hand within the window; if they are parked for the missing-directory reason, the projects were in a temp location that did not survive the restart — give long-running Automata a stable project directory.

**Orphan worker sessions.** The orphan sweep never ends a live task worker session that a non-terminal task is bound to, so a worker that is still working on its task keeps running through a restart and through the sweep. Sessions are matched by their short id on both sides, so the id form the session list reports does not matter. A live worker that no non-terminal task is bound to is reported the first time the sweep sees it and is ended only after it has stayed unbound longer than `autonomous.orphan_grace_seconds` (default `300`; unset or `0` uses the default). The grace period is measured from when the sweep first saw the session, so a worker that gets bound to a task again within the period is left alone. When the session list and the tasks store session ids in different forms, the sweep logs one line that starts `orphan-sweep: session id form check:` and names both forms with an example of each. The line is logged at most once per daemon run, so it can appear again after a restart. It is a self-check only: it does not change which sessions are ended, and on its own it is not a reason to stop the daemon.

**Stuck Automaton.** An Automaton that stays `running` with every task finished, no live worker, verifier or guardrail session and no executor is caught by the stuck-running watchdog. After `autonomous.stuck_running_seconds` (default `900`; `0` or a negative value uses the default) it re-runs the completion rollup once; if the Automaton is still stuck after a second full bound it moves to `blocked`, with a `stuck_running_blocked` decision, an `autonomous_stuck_running_blocked` audit entry and the status event. The bound is read on every pass, so changing it needs no restart. See [autonomous recovery](howto/autonomous-recovery.md#stuck-running-watchdog).

**Compute node of a surviving worker.** When the daemon restarts, a surviving pi-prompt worker is re-adopted on the compute node it was placed on. The node comes from the node recorded when the worker was placed. When none was recorded, it comes from the worker's own endpoint, matched against the compute node's address field - never from the worker's name and never from a DNS lookup. The node pools therefore show one worker per node before and after a restart. A worker whose node cannot be determined holds no node. It is shown as `node_unknown` on its lease in `GET /api/capacity` (also in `capacity_status` over MCP, and as `node unknown` in the status text) instead of being counted on a default node. If you see it, check the compute node's address field.

### Data compatibility

datawatch data files (`sessions.json`, `schedule.json`, `commands.json`, `filters.json`, `alerts.json`) use a flat JSON schema. Fields are added forwards-compatibly; the daemon ignores unknown fields from older schema versions.

**Encrypted stores (`--secure` mode):** The encryption formats (`DWDAT2\n` XChaCha20-Poly1305 and legacy `DWDAT1\n` AES-256-GCM) are stable. The derived key (Argon2id with salt embedded in config header) is deterministic — the same password always yields the same key across versions. Upgrading does not require re-encrypting data files. Legacy v1 files are read transparently.

### Rolling back

If you need to downgrade:

```bash
# Install a specific version
go install github.com/dmz006/datawatch/cmd/datawatch@v0.3.0

# Restart
datawatch start
```

Data files written by a newer version are generally readable by older versions (unknown fields are dropped). Exception: if a major version bump explicitly changes the schema, the CHANGELOG will note it.

---

## 3. CLI Session Management (without Signal)

All Signal commands are also available directly from the CLI. These commands connect to the local daemon's data directory and do not require Signal connectivity.

```bash
# List all sessions with status (shows name, backend, state)
datawatch session list

# Start a new claude-code session (uses current directory)
datawatch session new "build a REST API in Go"

# Start with a name, explicit directory, and backend
datawatch session new --name "api work" --dir ~/projects/api --backend aider "build a REST API"

# Show recent output from a session
datawatch session status a3f2

# Get the last 50 lines of output
datawatch session tail a3f2 --lines 50

# Send input to a session waiting for a prompt
datawatch session send a3f2 "yes, continue"

# Rename a session at any time
datawatch session rename a3f2 "api refactor"

# Terminate a session
datawatch session kill a3f2

# Kill all running sessions on this host
datawatch session stop-all

# Print the tmux attach command for a session
datawatch session attach a3f2
# Prints: tmux attach -t cs-myhost-a3f2
# (or TMUX_TMPDIR=~/.datawatch/tmux tmux attach -t cs-myhost-a3f2 when
# sessions run on the dedicated datawatch-tmux.service socket)
# Run that command to get a full terminal in the session
```

The `--config` flag works with all session subcommands:

```bash
datawatch session list --config /path/to/config.yaml
```

### Command Library

```bash
# Save a named reusable command
datawatch cmd add approve "yes"
datawatch cmd add deny "no"
datawatch cmd add skip "skip"

# List all saved commands
datawatch cmd list

# Delete a saved command
datawatch cmd delete approve
```

Saved commands are stored at `~/.datawatch/commands.json` and can be referenced in sessions by name.

### Seeding Defaults

```bash
# Pre-populate default commands and output filters
datawatch seed
```

`datawatch seed` writes a curated set of common commands and output filter rules to
`commands.json` and `filters.json`. It is safe to run on an existing installation —
it only adds entries that do not already exist.

---

## 4. Configuration

Full `~/.datawatch/config.yaml` with all fields and example values:

```yaml
# Identifies this machine in Signal messages and session IDs.
# Auto-detected from OS hostname if not set.
hostname: hal9000

# Root directory for sessions.json, logs/, and config.
# Default: ~/.datawatch
data_dir: ~/.datawatch

signal:
  # Your Signal phone number in E.164 format.
  account_number: +12125551234

  # Signal group ID in base64 format.
  # Get it from: signal-cli -u +12125551234 listGroups
  group_id: abc123base64groupid==

  # signal-cli data directory containing account keys and identity.
  # Default: ~/.local/share/signal-cli
  config_dir: ~/.local/share/signal-cli

  # Name shown in Signal's Settings → Linked Devices list.
  # Default: hostname
  device_name: hal9000

session:
  # Maximum number of concurrent claude-code sessions.
  # Default: 10
  max_sessions: 10

  # Seconds of idle output before declaring a session is waiting for input.
  # Lower values = faster detection; higher values = fewer false positives.
  # Default: 10
  input_idle_timeout: 10

  # Default number of lines returned by tail and status commands.
  # Default: 20
  tail_lines: 20

  # Path to the claude-code binary. Can be an absolute path or a PATH-relative name.
  # Default: claude
  claude_code_bin: /usr/local/bin/claude

  # Active LLM backend. Must match a registered backend name.
  # Available: claude-code, aider, goose, gemini, opencode, shell
  # Default: claude-code
  llm_backend: claude-code

  # Default project directory for sessions started via messaging commands.
  # Sessions started via CLI or PWA use the directory selected at session creation.
  # Default: ~/.datawatch/workspace
  default_project_dir: ~/projects

  # Pass --dangerously-skip-permissions to claude-code, bypassing permission prompts.
  # Useful for headless/unattended sessions. Use with caution.
  # Default: false
  skip_permissions: false

  # Kill all active sessions when the daemon exits (SIGINT/SIGTERM).
  # If false, sessions continue running in tmux after the daemon stops.
  # Default: false
  kill_sessions_on_exit: false

  # Restrict the web UI file browser to this directory tree.
  # Users cannot navigate above this path when choosing a project directory.
  # Leave empty to allow browsing the entire filesystem.
  # Default: "" (no restriction)
  root_path: ~/projects

update:
  # Enable automatic background updates.
  # When enabled, the daemon checks for a new release on the configured schedule
  # and replaces the running binary in-place.
  # Default: false
  enabled: false

  # How often to check for updates. Options: hourly, daily, weekly.
  # Default: daily
  schedule: daily

  # Time of day to perform the update check (24-hour HH:MM).
  # Only used when schedule is "daily" or "weekly".
  # Default: "03:00"
  time_of_day: "03:00"

server:
  # Enable the HTTP/WebSocket server for the PWA.
  # Default: true
  enabled: true

  # Bind address. Use 0.0.0.0 for all interfaces (including Tailscale).
  # Use a specific IP to bind only on that interface.
  # Default: 0.0.0.0
  host: 0.0.0.0

  # HTTP/WebSocket listen port.
  # Default: 8080
  port: 8080

  # Optional bearer token for PWA authentication.
  # Empty string = no authentication (rely on Tailscale for network security).
  # Default: "" (no auth)
  token: ""

  # Optional TLS certificate path.
  # Leave empty for plain HTTP (Tailscale provides encryption at the network layer).
  tls_cert: ""

  # Optional TLS key path.
  tls_key: ""

mcp:
  # Enable MCP server (stdio transport) for Cursor, Claude Desktop, VS Code.
  # Default: true
  enabled: true

  # Enable HTTP/SSE transport for remote AI clients.
  # Default: false
  sse_enabled: false

  # Bind address for the SSE server.
  # Default: 127.0.0.1 (localhost only — set to 0.0.0.0 for remote AI agents)
  sse_host: "127.0.0.1"

  # Listen port for the SSE server.
  # Default: 8081
  sse_port: 8081

  # Public base URL advertised to SSE clients (the "endpoint" event).
  # Set when sse_host is localhost behind a TLS-terminating reverse proxy.
  # Default: "" (advertise <scheme>://<sse_host>:<sse_port>)
  public_url: ""

  # Bearer token for SSE connections. Empty = no auth.
  # Default: ""
  token: ""

  tls_enabled: false
  tls_auto_generate: true

  # Maximum retries when a per-session MCP channel server fails to connect.
  # The daemon retries with exponential backoff before marking the channel unavailable.
  # Default: 3
  max_retries: 3

dns_channel:
  # Enable the DNS covert channel backend.
  # Default: false
  enabled: false

  # Operating mode: "server" (authoritative DNS) or "client" (query via resolver).
  mode: server

  # Domain name for DNS queries. Must match your DNS zone delegation.
  domain: ctl.example.com

  # Server: bind address for the DNS server.
  # Default: ":53"
  listen: ":15353"

  # Client: upstream resolver address (IP:port).
  upstream: "8.8.8.8:53"

  # Shared secret for HMAC-SHA256 query authentication.
  # Must match on both server and client.
  secret: "your-shared-secret-here"

  # Response TTL in seconds. 0 = non-cacheable.
  # Default: 0
  ttl: 0

  # Maximum response size in bytes.
  # Default: 512
  max_response_size: 512

  # Client polling interval (Go duration string).
  # Default: "5s"
  poll_interval: "5s"

  # Per-IP rate limit: max queries per IP per minute.
  # Default: 30. Set to -1 to disable.
  rate_limit: 30

servers:
  # Remote datawatch server connections. Added via: datawatch setup server
  # Use --server <name> flag to target a specific remote server.
  - name: ""         # Short identifier, e.g. "nas" or "workstation"
    url: ""          # Base URL of the remote instance, e.g. http://203.0.113.10:8080
    token: ""        # Bearer token for that remote server
    enabled: true
```

**Note on `--secure` mode:** When started with `datawatch --secure start`, ALL data stores
are encrypted with XChaCha20-Poly1305: `sessions.json`, `schedule.json`, `commands.json`,
`filters.json`, and `alerts.json` — not just `config.yaml`. The encryption key is derived
from your passphrase using Argon2id with a salt embedded in the encrypted config header.
Set `DATAWATCH_SECURE_PASSWORD` env variable for non-interactive restarts.
See [docs/encryption.md](encryption.md) for full details.

**Minimum viable config** (everything else uses defaults):

```yaml
signal:
  account_number: +12125551234
  group_id: abc123base64groupid==
```

### Task scope lint

Before an Automaton task spawns, the scope lint checks the task's spec and
planned files against the Automaton's read and write directories. Set the mode
with `autonomous.scope_lint` in the config file, in the PWA under Settings →
Automate → Task scope lint, or with `autonomous_config_set` over MCP.

| Mode | What it does |
|---|---|
| `off` | Skips the lint. No problems are recorded, and no scope warnings are produced at plan time. |
| `warn` | Lints the task and records each problem as a `scope_lint_warn` decision, then spawns the task anyway. |
| `enforce` | Blocks the task with a `scope:` error and does not spawn it. This is the default. |

An empty or unrecognised value is treated as `enforce`, so an existing
configuration keeps blocking.

**Placeholder and quoted patterns are never flagged.** In every mode the lint
ignores template and glob syntax (`<name>`, `{x}`, `*`, `$NAME`, `~user`) and a
path that sits inside a quoted search pattern, such as `grep '/home/x/.ssh'`.
A task that only mentions such text is not blocked.

**Remedies.** A lint message names the token, the line and one of two fixes:

- *A spec path outside the allowed directories:* edit the task spec to remove
  or quote the path, or widen the PRD read dirs if the task really needs to
  read it.
- *A planned file outside the writable directories:* remove the file from the
  task's files, or widen the PRD write dirs.

**Failed Automatons.** When a task fails only because the scope lint rejected
its spec, the failed Automaton still accepts a spec edit for that one task. Use
the task spec edit surface (`autonomous_prd_edit_task` over MCP, or the
`edit_task` REST route). Correct the spec, then retry the task. You do not need to
widen the PRD dirs or recreate the Automaton. Other failed tasks, and completed
tasks in any status, stay locked.

---

### Usage calculator

The usage calculator advises how many lanes the weekly provider allowance can
carry. It never stops or slows a lane. Keys live under `usage.calculator.`;
`usage.calculator.variant` (`all` by default, or `pace-gauge`, `lane-planner`, `burn-ledger`),
`policy`, `safety_points` (default 5), `cost_per_task_points`, `baseline_per_24h`,
`max_lanes`, `reset_weekday`, `reset_time`, `reset_zone` (a zone name, never a
fixed offset) and `keeper` (the federation peer that keeps the shared
readings). Settings apply without a restart.

| Method | How |
|--------|-----|
| **YAML config** | `usage.calculator.*` in `~/.datawatch/config.yaml` |
| **CLI** | `datawatch config set usage.calculator.<key> <value>`; `datawatch usage advice` |
| **Web UI** | The calculator screen and its Settings fields (not yet shipped; use another method) |
| **REST API** | `PUT /api/config` with `{"usage.calculator.<key>": value}`; `GET /api/usage/advice` |
| **Comm channel** | `configure usage.calculator.<key>=<value>`; `usage advice` |

See [howto/usage-calculator.md](howto/usage-calculator.md).

### Verifier node limit

Verifier runs take a lease on the same node pool as the worker they check, and a compute node that hosts a local engine gets its pool limit from its slot count (see "Compute-node queue" below), which is the node's `session_slots` when set, otherwise one slot per GPU when the observer reports GPU data and `autonomous.max_engines_per_node` (default 1) when it does not, capped by the compute node's own `max_concurrent_sessions` when that is smaller (see `syncPools` in `cmd/datawatch/capacity_wiring.go`). A node that hosts no local engine keeps its `max_concurrent_sessions` limit. A node with one GPU therefore runs one verifier at a time, and a verification can queue behind another task's lease.

Assessment: the limit should not rise on a guess. One slot is the right default while the GPU is the bottleneck, because a second concurrent judge slows both runs. It becomes too tight only when verifiers spend a large share of their time waiting rather than running. This work does **not** change the limit; it adds the counters that decide the question.

Read `verifier` in `GET /api/capacity` (also `capacity_status` over MCP and the CLI status text, and the `datawatch_verifier_*` Prometheus counters). Raise the limit only if the average queue wait (`wait_avg_seconds`) stays well above the average run time (`run_avg_seconds`, for example wait over half of run) across a representative window, and the node has idle GPU headroom while verifiers queue. If waits are short, or `infra`, `watchdog` and `timeout` counts are high, a higher limit would not help: fix the failures first.

### Verifier throughput

Verification is the throughput limit of the Automata pipeline, so the verifier path has four guards and a set of counters. Assessment of how many verifiers a node should run is in [Verifier node limit](#verifier-node-limit).

**No-progress watchdog.** A verifier session that shows no output file, no change in its pane output, no live runner process and no held capacity slot for `autonomous.verifier_no_progress_timeout_seconds` (default 900, fifteen minutes; unset or non-positive uses the default) is ended instead of waiting out the long verifier timeout. Any of these — an output file, a pane change, a live runner process, a held capacity slot — resets the timer, the first pane capture is only the baseline, and time spent waiting for a capacity slot never counts against the limit. The end is recorded as a failed verification whose summary reads `verifier no-progress watchdog: no output file or pane activity for <limit>`, and it is counted under `watchdog` in the verifier counters below. Because the trip classifies as an infra failure, repeated hangs on one task escalate that task's next verification to the fallback backend (see the **Infra-failure escalation** paragraph below).

**Lease reaper.** The capacity lease reaper never frees the lease of a verifier that is still running. A verifier lease is judged by the verifier session it started, and while the verifier is registered as running it stays live whatever the bound worker session or the lease age says. Leases whose verifier session has finished, and leaked leases with no live holder, are still reclaimed, so a pool never goes over its limit while verifiers run.

**Restart reconcile.** When the daemon starts it lists the verifier sessions that survived the restart and reconciles them against the task store: verifiers of a task that no longer awaits verification are ended, when several verifiers exist for one task the newest is kept and the others ended, and a single surviving verifier is adopted and its lease re-bound to it. Running the reconcile again changes nothing.

**Infra-failure escalation.** After `autonomous.verifier_infra_escalation_threshold` consecutive infra verifier failures on one task (default 2), the next verification of that task runs on `autonomous.fallback_verification_backend` (default the Claude backend, `claude-code`). The escalation is per task only: the count lives on the task, a pass or content result resets it, the end of the escalated run resets it, and other tasks keep using the configured verifier. It does nothing when the fallback equals the configured backend. Each escalation is recorded as a `verifier_escalation` decision on the PRD, as an `autonomous.verifier_escalation` entry in the audit log, and under `escalated` in the counters.

All three keys can be read and set through the autonomous config on REST, MCP (`autonomous_config_set`), the CLI and the PWA settings.

**Failure escalation and held tasks.** A failing task moves to the next worker on its ladder after each failed attempt. After `max_failures` failed attempts (default 3) it is held in `review_needed` for operator review instead of failing outright; `enabled: false` restores the retry-then-fail behaviour. Each field resolves task override, then Automaton override, then the daemon default (`autonomous.failure_escalation.{enabled,max_failures,steps}`).

| Method | How |
|--------|-----|
| **YAML** | `config.yaml` → `autonomous.failure_escalation:` with `enabled`, `max_failures` and `steps` |
| **CLI** | `datawatch autonomous failure-escalation show\|set\|clear <automaton> [--task <task>]`; `datawatch autonomous resolve-review <automaton> --task <task> --action retry\|cancel`; `datawatch autonomous status` lists held tasks under `review_needed`; `datawatch config set autonomous.failure_escalation.max_failures 4` |
| **Web UI** | Settings → Automata → Autonomous block (enable, failures before review, steps); Automaton detail → Overview (override editor); each task row (held badge, attempts, report, and the Retry, Edit spec and retry, Change worker, Narrow scope and Cancel task actions) |
| **REST API** | `PUT /api/config` with `autonomous.failure_escalation.enabled`, `.max_failures` or `.steps` (a JSON array or `backend:model[@attempts],...`); `POST /api/autonomous/prds/{id}/set_failure_escalation` with `{"task_id","policy"}`; `POST /api/autonomous/prds/{id}/resolve_task_review` with `{"task_id","action","spec","backend","model","effort","files","note"}` |
| **Comm channel** | `configure autonomous.failure_escalation.max_failures=4`; `autonomous set-failure-escalation <automaton> [task] clear \| enabled=false max_failures=4 steps=...`; `autonomous review-task <automaton> <task> retry\|cancel [key=value ...]`; `autonomous status` names each held task |
| **MCP** | `autonomous_prd_set_failure_escalation`, `autonomous_prd_resolve_task_review`, `autonomous_status` (`review_needed`), `config_set` |

**Defaults and the retry count.** Escalation is on by default. A task gets `max_failures` failed attempts (default 3) before it is held, where the old behaviour was one attempt plus `autonomous.auto_fix_retries` (default 1, so two attempts) before the task was marked failed. An Automaton with no settings of its own therefore gets one more attempt than before, and a task that uses them up waits for the operator instead of failing. A held task keeps its attempts and its report, and its Automaton shows as blocked until the operator retries or cancels the task. To restore the old behaviour, set `autonomous.failure_escalation.enabled: false` (or `datawatch config set autonomous.failure_escalation.enabled false`); the retry count is then `autonomous.auto_fix_retries` again. The operator guide is [howto/failure-escalation.md](howto/failure-escalation.md).

**Counters.** `GET /api/capacity` carries a `verifier` object (`capacity_status` over MCP returns it too) with `runs`, queue wait and run time totals, averages and maxima in seconds, `categories` (`pass`, `content`, `infra`, `watchdog`, `timeout`, `capacity`, `escalated`), `watchdog_ends`, `escalations` and `since`. The CLI and comm channel status text adds one line, for example `verifiers: 12 runs, avg wait 38s, avg run 6m, infra 3, watchdog 1`, or `verifiers: no runs yet`. Prometheus exposes `datawatch_verifier_runs_total{category}`, `datawatch_verifier_wait_seconds_total` and `datawatch_verifier_run_seconds_total`. The aggregates are kept since the daemon started.

---

## 5. Signal Account Management

### Find your group ID

```bash
# List all joined Signal groups
signal-cli -u +12125551234 listGroups
# Output:
# Id: abc123base64groupid==  Name: AI Control  Active: true  Members: +12125551234, +19175559876
```

### List linked devices

```bash
signal-cli -u +12125551234 listDevices
# Output:
# Device 1 (primary): iPhone, last seen: 2026-03-24
# Device 2: hal9000, last seen: 2026-03-25
# Device 3: nas, last seen: 2026-03-25
```

### Remove a linked device

```bash
signal-cli -u +12125551234 removeDevice --deviceId 3
```

### Re-link a device (after removal or key rotation)

```bash
# Interactive re-link with QR in terminal
datawatch link

# Or using signal-cli directly
signal-cli --config ~/.local/share/signal-cli link -n myhost
```

### Unregister from Signal entirely

```bash
signal-cli -u +12125551234 unregister
```

---

## 6. Backup and Recovery

### What to back up

| Path | Contents | Importance |
|---|---|---|
| `~/.datawatch/config.yaml` | All daemon configuration | Critical — required to restart |
| `~/.datawatch/sessions.json` | Session state and history | Important — lose this and running sessions can't be resumed |
| `~/.local/share/signal-cli/` | Signal account keys and identity | Critical — lose this and you must re-link from scratch |
| `~/.datawatch/identity.yaml` and `~/.datawatch/identity-backups/` | The operator identity and the daemon's own timestamped copies of it | Important — it is injected into every session; the backups are what bring it back after a bad save |
| `~/.datawatch/logs/` | Session output logs | Nice to have — historical output |

Autonomous Automata state lives as plain JSONL files under the autonomous data directory: `prds.jsonl` (Automata) and, alongside it, `automations.jsonl` (Automations — named groups of Automata with next-step edges). Both are captured automatically by any backup of that directory.

**Automation advance and idle alert.** An active Automation starts its next Automaton by itself when the current one reaches a terminal state; after a daemon restart it reconciles once boot-resume has finished (a member that finished while the daemon was down advances, a running one is left alone). `autonomous.automation.idle_alert_minutes` (default `0` = off, whole minutes) raises one alert when an active Automation has had no member running that long; it is read live, so changing it needs no restart. See [howto/automation-queue-advance.md](howto/automation-queue-advance.md).

**Progress engine settings.** Every parameter of the progress engine is a configuration key, changed live with no restart. A bad value is rejected (HTTP 400, or an error on load) and nothing is written.

| Key | Default | Meaning |
|---|---|---|
| `autonomous.progress.lanes` | `0` (= 5) | Parallel lanes of Automata the utilization figures assume. Whole number >= 0. |
| `autonomous.progress.inactive_gap_hours` | `0` (= 3) | Hours without activity that count as an inactive window. Number >= 0. |
| `autonomous.progress.backlog_path` | `docs/plans/README.md` | Backlog file. A relative path resolves against `autonomous.worktree_base_repo` when set, else the daemon working directory. |
| `autonomous.progress.since` | empty | First day counted, `YYYY-MM-DD`; empty = the first record's creation date. |
| `autonomous.progress.snapshot_retention_days` | `0` (= 90) | Days saved progress snapshots are kept. Whole number >= 0. |

Each key is reachable through all five configuration methods (MCP agents use `config_set` with the same keys, or the `progress_lanes`, `progress_inactive_gap_hours`, `progress_backlog_path`, `progress_since` and `progress_snapshot_retention_days` arguments of `autonomous_config_set`):

| Key | YAML (`autonomous.progress:` block) | CLI | Web UI (Settings → General → Autonomous) | REST | Chat |
|---|---|---|---|---|---|
| `autonomous.progress.lanes` | `lanes: 5` | `datawatch config set autonomous.progress.lanes 5` | *Progress lanes* | `PUT /api/config` `{"autonomous.progress.lanes": 5}` | `configure autonomous.progress.lanes=5` |
| `autonomous.progress.inactive_gap_hours` | `inactive_gap_hours: 3` | `datawatch config set autonomous.progress.inactive_gap_hours 3` | *Progress inactive gap* | `PUT /api/config` `{"autonomous.progress.inactive_gap_hours": 3}` | `configure autonomous.progress.inactive_gap_hours=3` |
| `autonomous.progress.backlog_path` | `backlog_path: docs/plans/README.md` | `datawatch config set autonomous.progress.backlog_path docs/plans/README.md` | *Progress backlog file* | `PUT /api/config` `{"autonomous.progress.backlog_path": "docs/plans/README.md"}` | `configure autonomous.progress.backlog_path=docs/plans/README.md` |
| `autonomous.progress.since` | `since: "2026-09-01"` | `datawatch config set autonomous.progress.since 2026-09-01` | *Progress start date* | `PUT /api/config` `{"autonomous.progress.since": "2026-09-01"}` | `configure autonomous.progress.since=2026-09-01` |
| `autonomous.progress.snapshot_retention_days` | `snapshot_retention_days: 90` | `datawatch config set autonomous.progress.snapshot_retention_days 90` | *Progress snapshot retention* | `PUT /api/config` `{"autonomous.progress.snapshot_retention_days": 90}` | `configure autonomous.progress.snapshot_retention_days=90` |

### Identity backups

The daemon protects the operator identity (`identity.yaml` in the data
directory) on its own. You do not schedule anything; this is what it does and
where to look.

- **Location.** `identity-backups/` next to `identity.yaml`, by default
  `~/.datawatch/identity-backups/`. The folder is created on the first backup
  (mode `0700`); each file is mode `0600`. One file per backup, named
  `identity-<UTC timestamp>.yaml` with the time to the millisecond, for example
  `identity-20261029T101500.000Z.yaml`. Names sort oldest to newest. Two backups
  in the same millisecond get a `_001`, `_002` suffix.
- **When a backup is made.** Before every write that replaces `identity.yaml`:
  a save from any surface, a Reset and a Restore. Nothing is made when the
  current file is missing or has no content, or when it matches the newest
  backup apart from its `updated_at` line. If the backup cannot be written
  the daemon does not write the identity, and the save fails with an error.
- **Retention.** The newest `identity.backups_to_keep` files are kept and older
  ones are removed at the next write. Default 10, minimum 1; a lower or
  non-numeric value is refused (`PUT /api/config` answers 400). The key is
  live, so a change needs no restart. Set it like any configuration key (YAML
  `identity.backups_to_keep`, `datawatch config set identity.backups_to_keep 20`,
  `PUT /api/config`, `configure identity.backups_to_keep=20` in chat, or
  Settings → Automata → Identity in the PWA). A lower number removes the surplus
  at the next write, not at once.
- **Empty-write refusal.** A save with no content over a saved identity is
  refused with HTTP 409 `identity_wipe_refused` and recorded in the audit log
  (action `identity_set_refused`); Reset (`POST /api/identity/reset`,
  `datawatch identity reset`, the PWA Reset button, MCP `reset_identity`) is
  the only way to clear it, and keeps a backup. Restores and resets are audited
  as `identity_restore` and `identity_reset`.
- **Restore.** `datawatch identity backups` lists them and
  `datawatch identity restore [--backup <name>]` brings one back. The same is on
  the PWA (Settings → Automata → Identity → Restore), MCP (`identity_backups`,
  `restore_identity`), REST (`GET /api/identity/backups`,
  `POST /api/identity/restore`) and chat (`identity backups`,
  `identity restore <name|latest>`). A restore backs up the identity it
  replaces, so it can be undone. See
  [howto/identity-and-telos](howto/identity-and-telos.md#backups-restore-and-the-empty-write-guard).
- **Restore by hand.** With the daemon stopped, copy the chosen file over
  `identity.yaml` and start the daemon again:

  ```bash
  systemctl --user stop datawatch
  cp ~/.datawatch/identity-backups/identity-20261028T080000.000Z.yaml ~/.datawatch/identity.yaml
  chmod 600 ~/.datawatch/identity.yaml
  systemctl --user start datawatch
  datawatch identity get
  ```

  Stopping the daemon first matters: a running daemon rewrites the file on its
  next save, and a copy made during a save can be lost. If the daemon uses a
  different data directory, use that one.
- **In your own backups.** The `tar` backup below takes `identity-backups/` with
  the rest of `~/.datawatch/`. The folder only holds the last few identities
  the daemon saw, so keep a copy elsewhere of a backup you want to keep for
  good.

### Backup command

```bash
tar czf datawatch-backup-$(date +%Y%m%d).tar.gz \
  ~/.datawatch/ \
  ~/.local/share/signal-cli/
```

### Restore on a new machine

1. Install dependencies: `signal-cli`, Java 17+, `tmux`, `claude` (claude-code CLI), `datawatch`

2. Restore the backup:
   ```bash
   tar xzf datawatch-backup-20260325.tar.gz -C ~/
   ```

3. Verify the restored config points to the correct paths:
   ```bash
   cat ~/.datawatch/config.yaml
   ```

4. Start the daemon:
   ```bash
   datawatch start
   ```

5. Verify it connects to Signal:
   ```bash
   journalctl --user -u datawatch -f
   # Look for: "subscribed to Signal messages"
   ```

Note: Sessions that were `running` on the old machine will be marked `failed` on resume because their tmux sessions no longer exist. This is expected.

---

## 7. Troubleshooting

### signal-cli not found in PATH

**Symptom:** `datawatch start` fails with `signal-cli: executable file not found in $PATH`

**Fix:**
```bash
# Check if signal-cli is installed
which signal-cli

# If not found, install it:
wget https://github.com/AsamK/signal-cli/releases/latest/download/signal-cli.tar.gz
tar xf signal-cli.tar.gz
sudo mv signal-cli-*/bin/signal-cli /usr/local/bin/
sudo mv signal-cli-*/lib/ /usr/local/lib/signal-cli/

# For systemd services, the PATH may differ from your shell PATH
# Add to the service override:
sudo systemctl edit datawatch
# Add:
# [Service]
# Environment=PATH=/usr/local/bin:/usr/bin:/bin
```

### Java not installed or wrong version

**Symptom:** signal-cli starts but immediately exits, or shows `UnsupportedClassVersionError`

**Fix:**
```bash
# Check Java version
java -version
# Need: java version "17" or higher

# Install Java 17+ (Ubuntu/Debian)
sudo apt-get install openjdk-17-jre-headless

# Install Java 17+ (Fedora/RHEL)
sudo dnf install java-17-openjdk

# Install Java 17+ (macOS)
brew install openjdk@17

# Set JAVA_HOME if multiple versions installed
export JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64
```

### QR code appears but scanning fails

**Symptom:** QR is displayed, Signal scanner reads it, but linking fails or times out

**Possible causes and fixes:**

1. **signal-cli config directory permissions:**
   ```bash
   ls -la ~/.local/share/signal-cli/
   # Should be owned by your user with 700 permissions
   chmod 700 ~/.local/share/signal-cli/
   ```

2. **Clock skew:** Signal is sensitive to time drift. Check your system clock:
   ```bash
   timedatectl status
   # Ensure NTP is synchronized
   sudo timedatectl set-ntp true
   ```

3. **QR code expired:** The sgnl:// link expires after a few minutes. Run `datawatch link` again and scan immediately.

4. **Wrong Signal account:** Ensure the account number in `config.yaml` matches the Signal account you're scanning with.

### Sessions not resuming after restart (tmux sessions gone)

**Symptom:** After `systemctl restart datawatch`, all sessions show as `failed`

**Explanation:** This is expected behavior. When the daemon restarts, it checks whether the tmux session for each `running`/`waiting_input` session still exists. If the machine was rebooted or tmux was killed, those sessions are gone and are marked `failed`.

**Prevention:**
- Use tmux server persistence plugins (e.g. `tmux-resurrect`) to restore tmux sessions across reboots
- Only restart the daemon, not the whole machine, to preserve running sessions
- For the daemon itself: `systemctl restart datawatch` preserves tmux sessions; only a machine reboot or `tmux kill-server` loses them. Which restart cases are covered, and which are not, is in [Restart safety](#restart-safety)

### PWA can't connect (port, firewall, Tailscale)

**Symptom:** Browser shows "ERR_CONNECTION_REFUSED" or WebSocket fails to connect

**Diagnostic steps:**

```bash
# 1. Verify the daemon is running and listening
systemctl --user status datawatch
curl http://localhost:8080/api/health

# 2. Check the port is open
ss -tlnp | grep 8080

# 3. Find your Tailscale IP
tailscale ip -4

# 4. Test connectivity from the phone (via Tailscale)
# On the server:
curl http://$(tailscale ip -4):8080/api/health

# 5. Check for firewall rules blocking the port
sudo ufw status
# If firewall is active, allow the port on the Tailscale interface:
sudo ufw allow in on tailscale0 to any port 8080

# 6. Verify Tailscale is connected on both devices
tailscale status
```

### claude-code not found (PATH in service environment)

**Symptom:** Session starts, tmux session is created, but immediately transitions to `failed`. Logs show `claude: command not found`.

**Cause:** systemd services have a restricted PATH. The `claude` binary may be in a location not on the service's PATH (e.g. `~/.local/bin`, `~/.npm/bin`, `/usr/local/bin`).

**Fix:**

Option 1 — Set the full path in config:
```yaml
session:
  claude_code_bin: ~/.local/bin/claude
```

Option 2 — Add the PATH to the systemd service:
```bash
systemctl --user edit datawatch
# Add:
# [Service]
# Environment=PATH=~/.local/bin:/usr/local/bin:/usr/bin:/bin
```

### Group ID not found / wrong format

**Symptom:** Daemon starts, no errors, but Signal messages are ignored

**Diagnosis:**
```bash
# List all groups your account knows about
signal-cli --config ~/.local/share/signal-cli -u +12125551234 listGroups
```

**Common issues:**
- Group ID in config is missing the trailing `==` (base64 padding) — copy the full ID from `listGroups` output
- Using the wrong account number — the phone number must match the account that joined the group
- The device is not yet linked — run `datawatch link` and scan the QR

### Multiple machines replying (expected behavior explanation)

**Symptom:** You send `list` and receive two or more replies from different machines

**This is expected behavior.** Each machine running `datawatch` in the same Signal group will receive and process `list` commands independently, replying with its own sessions. Each reply is prefixed with `[hostname]` so you know which machine is responding.

To send a command to a specific machine's session, use the hostname-prefixed ID:
```
status myhost-a3f2
send myhost-a3f2: yes
```

Short IDs (`a3f2`) work if only one machine has a session with that ID. If both machines have a session `a3f2`, the command is ambiguous — each machine will act on its own `a3f2` session.

---

### Auto-commit refused

**Symptom:** An automatic commit fails with an error beginning `auto-commit refused:`, prefixed with the repository directory:

```
~/project: auto-commit refused: 12504 files staged exceeds the limit of 10000 (MaxStagedFiles). Narrow the write scope (read_dirs/write_dirs) so the commit stages fewer files, or point the project directory at a repository rather than a home directory.
```

**What it means:** The commit would have staged more files than the staged-file limit allows (`MaxStagedFiles`, default 10000). Datawatch refused rather than commit a whole directory tree. The usual cause is a session write scope that covers a directory holding a large number of files, most often caches, dependency trees, or model weights inside the project directory.

**Datawatch does not stage anything when it refuses.** Nothing is added to the index, so there is nothing to unstage and no partial commit exists.

**How to fix it:**

1. **Narrow the write scope.** Set the session's `read_dirs` and `write_dirs` to the directories the work actually touches, so the commit's files are the ones the session changed.
2. **Move out-of-tree assets out of the project directory.** Keep caches (`.cache`, `.gradle`, `node_modules`, build output) and model weights (`*.gguf`, `*.safetensors`, and similar) in a location outside the project directory, and reference them by absolute path.
3. **Check the project directory.** If it is your home directory, the log also carries a prominent home-directory warning. Point the project directory at the repository for the work instead.

Retry the commit after the change. The guard evaluates each automatic commit against the current working tree.

### Automatic commit refused in a non-project directory

**Symptom:** A session ends without a commit, and the audit log carries a single line saying the automatic commit was refused because the directory is not a project. The refusal is not retried.

**What it means:** Automatic commits are refused for the home directory, a filesystem root, or a directory that has no repository and no project marker. Datawatch will not create or commit into such a directory.

**How to fix it:** Point the session's project directory at the repository or project folder for the work (or initialise a repository there), then start the session again.

### Worker commit fails with "the harness commits after validation"

**Symptom:** A `git commit` run inside a session fails with a message saying the harness commits after validation.

**What it means:** This is expected. Only the harness commits, and only after the session's validation passes. The refusal is enforced by hooks in a harness-owned temporary directory outside the worktree. Those hooks are removed at session end and when the daemon adopts sessions on startup.

**How to fix it:** Nothing is needed. Leave the changes in the working tree and finish the task; the harness commits after validation. If validation fails, no commit is made, so fix the failure and let the session complete again.

---

## 8. Monitoring

### Health and Readiness (Kubernetes probes)

| Method | How |
|--------|-----|
| **REST API** | `GET /healthz` (liveness), `GET /readyz` (readiness) — both public, no auth |
| **Web UI** | Settings → Monitor tab → Infrastructure card shows bound interfaces |
| **Comm channel** | `stats` command includes session count and uptime |
| **CLI** | `curl http://localhost:8080/healthz` |

```bash
# Liveness probe — returns 200 if the HTTP server is responding
curl http://localhost:8080/healthz
# Response: {"status":"ok"}

# Readiness probe — returns 200 when the daemon is fully operational
curl http://localhost:8080/readyz
# Response: {"status":"ready","active_sessions":3}
# Returns 503 if session store is not loaded or backends are not connected
```

Both endpoints are public (no auth required) and follow the Kubernetes probe convention.

### Legacy health check

```bash
curl http://localhost:8080/api/health
# Response: {"status":"ok","uptime":"1h23m4s","version":"1.1.0"}
```

### Prometheus Metrics

| Method | How |
|--------|-----|
| **REST API** | `GET /metrics` — Prometheus text format, public, no auth |
| **Web UI** | Settings → Monitor tab → all stats cards (CPU, Memory, Disk, GPU, Daemon, RTK) |
| **Comm channel** | `stats` command returns CPU, memory, disk summary |
| **Grafana** | Add Prometheus data source pointing to `http://datawatch-host:8080/metrics` |

```bash
curl http://localhost:8080/metrics
```

Returns Prometheus text format with gauges, counters, and histograms:

| Metric | Type | Description |
|--------|------|-------------|
| `datawatch_sessions_active` | gauge | Active sessions (labels: backend, state) |
| `datawatch_sessions_total` | counter | Total sessions created (labels: backend) |
| `datawatch_cpu_usage_percent` | gauge | CPU usage |
| `datawatch_memory_usage_bytes` | gauge | Memory usage |
| `datawatch_messages_total` | counter | Messages sent/received (labels: channel, direction) |
| `datawatch_uptime_seconds` | gauge | Daemon uptime |

### Daemon info

```bash
curl http://localhost:8080/api/info
# Response: {"hostname":"hal9000","version":"0.1.0","sessions":3,"uptime":"1h23m4s"}
```

### Session list via API

```bash
curl http://localhost:8080/api/sessions | jq '.[] | {id,state,task}'
```

`?view=summary` returns a compact list (long text fields truncated,
only the newest ended sessions kept); `?states=active` and
`?since=<RFC3339|unix>` narrow it further. See
[api/sessions.md](api/sessions.md#compact-session-list-viewsummary).

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/sessions?view=summary&states=active" | jq '.[] | {id,state,task}'
```

### Allowed project roots

A project directory that comes from a request or a stored record is only
used when it lies under an allowed root. This covers a session start (REST,
MCP, chat, the CLI and a PRD task), an Automaton created with an explicit
`project_dir` (and a per-task `project_dir`), the pipeline and quality-gate
test runner's working directory, and the git guard's own path handling. The
rule is always on, with or without an API token.

Allowed roots are:

1. the PRD worktree base, `autonomous.worktree_dir` (default
   `~/.datawatch/prd-worktrees`), the folder the daemon creates PRD worktrees in;
2. the workspace root: `session.workspace_root` when set, otherwise the
   default workspace folder `~/workspace` (the `workspace` folder in the
   daemon user's home directory). Setting `session.workspace_root` replaces
   the default; it does not add to it;
3. every folder listed in `server.allowed_project_roots` (absolute paths, a
   leading `~` is allowed, no filesystem root). Nothing else widens the set.

Sessions, Automata with an explicit `project_dir` and test runs under
`~/workspace` therefore work without any configuration. For repositories that
live elsewhere, set `session.workspace_root` to their parent folder, or list
them in `server.allowed_project_roots`.

`..` segments and symlinks, including a dangling link, are resolved before
the check, so a link inside a root that points outside it is refused, as is a
missing path that a dangling link would create outside the roots. A refused
directory returns an error that names the roots (REST answers 400). A
relative `project_dir` is joined under `session.workspace_root` when that is
set, or under the daemon's working directory when it is not (the default
`~/workspace` folder is an allowed root but not a base for relative paths), and
checked the same way. The daemon's
own `session.default_project_dir` is configuration, not request input, so it
is used as is.

Changes apply live; no restart is needed.

| Key | YAML (`server:` block) | CLI | Web UI (Settings → General → Sessions) | REST | Chat |
|---|---|---|---|---|---|
| `server.allowed_project_roots` | `allowed_project_roots: [/srv/projects, ~/work]` | `datawatch config set server.allowed_project_roots /srv/projects,~/work` | *Extra allowed project roots (comma-separated)* | `PUT /api/config` `{"server.allowed_project_roots": ["/srv/projects", "~/work"]}` | `configure server.allowed_project_roots=/srv/projects,~/work` |

MCP agents can use `config_set` with the same key and a comma-separated value.

### Payload settings for low-bandwidth clients

These `server.*` settings tune the compact session list, the WebSocket
streams and API compression used by phones and other low-bandwidth
clients. Every one is read from the live config, so a change applies
without a restart (WebSocket defaults apply without a reconnect). The
recommended client flow is in
[api/mobile-surface.md](api/mobile-surface.md#low-bandwidth-client-flow).

| Key | Default | Meaning |
|---|---|---|
| `server.session_summary_terminal_limit` | `50` | Ended (complete / failed / killed) sessions kept by `GET /api/sessions?view=summary`, newest first; `0` = no cap. The unfiltered list is never capped. |
| `server.session_summary_text_chars` | `200` | Characters kept of `task`, `prompt_context`, `last_response` and `last_summary_long` in the summary view and in summary `session_state` frames (minimum 20). |
| `server.ws_session_list_default` | `full` | WebSocket session-list mode for clients that send no `hello`: `full` or `delta`. |
| `server.ws_output_scope_default` | `all` | WebSocket terminal-output mode for clients that send no `output` in `hello`: `all` or `subscribed`. Keep `all` while the web app is in use. |
| `server.api_gzip` | `true` | Gzip `/api/*` JSON responses for clients sending `Accept-Encoding: gzip`. |
| `server.api_gzip_min_bytes` | `1024` | Only bodies larger than this are compressed. |

Each key is reachable through all five configuration methods:

| Key | YAML (`server:` block) | CLI | Web UI (Settings → General → Sessions) | REST | Chat |
|---|---|---|---|---|---|
| `server.session_summary_terminal_limit` | `session_summary_terminal_limit: 50` | `datawatch config set server.session_summary_terminal_limit 50` | *Session summary: ended sessions kept (0 = all)* | `PUT /api/config` `{"server.session_summary_terminal_limit": 50}` | `configure server.session_summary_terminal_limit=50` |
| `server.session_summary_text_chars` | `session_summary_text_chars: 200` | `datawatch config set server.session_summary_text_chars 200` | *Session summary: text length (chars)* | `PUT /api/config` `{"server.session_summary_text_chars": 200}` | `configure server.session_summary_text_chars=200` |
| `server.ws_session_list_default` | `ws_session_list_default: full` | `datawatch config set server.ws_session_list_default delta` | *Live session updates (clients without hello)* | `PUT /api/config` `{"server.ws_session_list_default": "delta"}` | `configure server.ws_session_list_default=delta` |
| `server.ws_output_scope_default` | `ws_output_scope_default: all` | `datawatch config set server.ws_output_scope_default subscribed` | *Live terminal output (clients without hello)* | `PUT /api/config` `{"server.ws_output_scope_default": "subscribed"}` | `configure server.ws_output_scope_default=subscribed` |
| `server.api_gzip` | `api_gzip: true` | `datawatch config set server.api_gzip false` | *Compress API responses (gzip)* | `PUT /api/config` `{"server.api_gzip": false}` | `configure server.api_gzip=false` |
| `server.api_gzip_min_bytes` | `api_gzip_min_bytes: 1024` | `datawatch config set server.api_gzip_min_bytes 4096` | *API compression threshold (bytes)* | `PUT /api/config` `{"server.api_gzip_min_bytes": 4096}` | `configure server.api_gzip_min_bytes=4096` |

MCP agents can use `config_set` with the same keys. `GET /api/info`
lists what the daemon supports in its `capabilities` array (for example
`sessions.view_summary`, `alerts.count`, `ws.hello`, and `api.gzip`
while compression is on).

### Session output via API

```bash
curl "http://localhost:8080/api/output?id=a3f2&n=50"
```

### Check log files directly

```bash
# Tail the output log for session a3f2
tail -f ~/.datawatch/logs/myhost-a3f2.log

# Show last 50 lines
tail -n 50 ~/.datawatch/logs/myhost-a3f2.log

# Count sessions in sessions.json
jq 'length' ~/.datawatch/sessions.json

# Show all session states
jq '.[] | {id, state, task}' ~/.datawatch/sessions.json
```

---

## 9. Log Levels

```bash
# Info logging (default) — startup, session state changes, errors
datawatch start

# Debug logging (verbose) — all Signal messages, JSON-RPC traffic, monitor events
datawatch start --verbose
```

When running as a systemd service, adjust the service override to add `--verbose`:

```bash
systemctl --user edit datawatch
```

Add:
```ini
[Service]
ExecStart=
ExecStart=/usr/local/bin/datawatch start --verbose --config ~/.datawatch/config.yaml
```

Then:
```bash
systemctl --user daemon-reload
systemctl --user restart datawatch
journalctl --user -u datawatch -f
```

---

## Audit & Access Logging

Two append-only JSON-lines logs under `data_dir`:

- **`audit.log`** — operator action log: session/config/etc. state
  changes (`start`, `kill`, `send_input`, `configure`, `rollback`,
  `schedule`, ...). Query: `GET /api/audit` or the `audit_query` MCP
  tool.
- **`access.log`** — every HTTP request/response, auth failure, and
  WebSocket connect/disconnect, from all of CLI, REST, and MCP (all
  three are HTTP clients against the same `/api/*` surface behind one
  auth middleware, so this one log covers all three). Query:
  `GET /api/audit/access` or the `audit_access_query` MCP tool, both
  taking `actor`, `action`, `since`, `until`, `limit`.

Neither log ever records the `Authorization` header or a token/nonce
value — only request metadata (method, path, status, remote IP, user
agent) and the resolved principal (`admin`, `session-scoped`,
`peer:<name>`, `proxy:<name>`, or `unauthenticated`).

**Retention**: `audit.retention_days` (default 30; negative = never
prune) applies to both logs, pruned at daemon startup and every 24h.

**SIEM forwarding**: set `audit.cef_mirror_enabled: true` to
additionally write a CEF-formatted line (`audit.log.cef`,
`access.log.cef`) alongside every entry — point your Splunk/QRadar/
ArcSight/Sentinel forwarder at the `.cef` file. The JSON-lines file
stays the one the daemon's own REST/MCP query surface reads.

**Metrics**: `datawatch_access_log_events_total{action,principal_kind}`
(Prometheus, `/metrics`) and `access_log_events_total`/
`access_log_auth_failures_total` on `GET /api/stats` (in-process
counts, reset on restart — the Prometheus counter is the durable one).

Example — find every successful connection and auth failure from a new
IP in the last day:
```bash
curl -s "$DATAWATCH_API_URL/api/audit/access?action=http_access&since=$(date -u -d '1 day ago' +%Y-%m-%dT%H:%M:%SZ)" \
  -H "Authorization: Bearer $DATAWATCH_TOKEN" | jq -r '.entries[].details.remote_ip' | sort -u
curl -s "$DATAWATCH_API_URL/api/audit/access?action=auth_failure" \
  -H "Authorization: Bearer $DATAWATCH_TOKEN" | jq '.entries'
```

**Cross-hop attribution (federation)**: when a request to this daemon
was forwarded by another datawatch daemon acting as a federation peer
— LLM delegation (`ProxyRouter`), `/api/proxy/{name}/...`, or
`/remote/{name}/...` — a `peer:<name>` entry only tells you which peer
presented the request, not who behind that peer actually initiated it.
If the forwarding daemon sent a verified `X-Datawatch-Hop-Chain`
header, the entry also carries `details.origin_actor` (who initiated
it, e.g. `admin` or `session-scoped`, on the forwarding daemon) and the
full `details.hop_chain` (every daemon the request passed through).
The chain is HMAC-signed per hop using each pair of daemons' existing
shared peer bearer token — not new asymmetric keys — and verified
hop-by-hop: each daemon checks only the link signed with the token it
directly shares with its sender, not the whole chain back to the
origin. A chain that fails verification is dropped (never trusted,
never rejected) and logged as `details.hop_chain_invalid: true`; a
peer running an older version that doesn't send a chain at all just
gets the plain `peer:<name>` attribution, same as before this existed.
```bash
curl -s "$DATAWATCH_API_URL/api/audit/access?action=http_access" \
  -H "Authorization: Bearer $DATAWATCH_TOKEN" \
  | jq '.entries[] | select(.details.origin_actor != null) | {actor, origin_actor: .details.origin_actor, hop_chain: .details.hop_chain}'
```

---

## 7. Network Security

### Listener Overview

datawatch exposes multiple network services. Each should be configured for the deployment environment.

| Service | Default Bind | Default Port | Auth | TLS | Purpose |
|---------|--------------|--------------|------|-----|---------|
| HTTP/PWA | `0.0.0.0` | 8080 | Optional token | Optional | Web UI, REST API, WebSocket |
| MCP SSE | `127.0.0.1` | 8081 | Optional token | Optional | AI agent tool server |
| DNS Channel | `:53` | 53 | HMAC-SHA256 | N/A (DNS) | Covert command channel |
| GitHub Webhook | `127.0.0.1` | 9001 | HMAC-SHA256 | No | GitHub event receiver |
| Generic Webhook | `127.0.0.1` | 9002 | Optional token | No | HTTP POST task receiver |
| Twilio Webhook | `127.0.0.1` | 9003 | None built-in | No | SMS receiver |
| Per-session MCP | `127.0.0.1` | Random | None | No | Internal claude-code channels |

### Recommended: Localhost + Reverse Proxy

For production deployments, bind all services to `127.0.0.1` and use a reverse proxy (nginx, caddy, Tailscale) for external access:

```yaml
server:
  host: "127.0.0.1"    # PWA only via reverse proxy or Tailscale
  port: 8080
  token: "strong-random-token-here"

mcp:
  sse_host: "127.0.0.1"  # AI agents on same machine
  sse_port: 8081
  token: "mcp-auth-token"
  # Required when remote MCP clients reach SSE through the proxy:
  public_url: "https://host.example.ts.net:8081"
```

The MCP SSE transport tells each client where to POST messages (the
`endpoint` event). By default that URL is built from the bind address, so
a server bound to `127.0.0.1` behind a proxy would point remote clients at
their own localhost. Set `mcp.public_url` to the URL clients actually use
to reach the proxy (for example the `tailscale serve --https=8081` URL) and
the server advertises that instead. Changing it requires a daemon restart.

The value is empty, or an absolute `http` or `https` URL with a host and no
user-info, query or fragment; surrounding whitespace and trailing slashes are
ignored. An invalid value is rejected with a 400 by the REST API (and so by the
CLI, MCP `config_set` and chat), and an invalid value in a hand-edited YAML
file is ignored with a warning at SSE start, so the bind address is advertised.

| Access method | How to set `mcp.public_url` |
|---|---|
| YAML | `mcp.public_url: "https://host.example.ts.net:8081"` in `~/.datawatch/config.yaml` |
| CLI | `datawatch config set mcp.public_url https://host.example.ts.net:8081` |
| Web UI | Settings → Comms → MCP Server → "SSE public URL (reverse proxy)" |
| REST API | `PUT /api/config` with `{"mcp.public_url": "https://host.example.ts.net:8081"}`; read it from `GET /api/config` (`mcp.public_url`). MCP `config_set` uses the same path. |
| Comm channel | `configure mcp.public_url=https://host.example.ts.net:8081` |

### Unauthenticated listener startup warning

At every start, the daemon checks every listener it is about to serve and
logs one warning line per listener that is **enabled, not token-protected,
and bound to a non-loopback address**. Covered listeners and their tokens:

| Listener | Token / secret that silences the warning |
|---|---|
| `rest` (Web UI + REST API, `server.host:server.port`) | `server.token` |
| `rest_tls` (`server.host:<TLS port>` when `server.tls_enabled`) | `server.ttls` |
| `mcp_sse` (`mcp.sse_host:mcp.sse_port`) | `mcp.token` (or `server.token` when `mcp.token` is empty) |
| `proxy_sandbox` (`server.proxy_sandbox_port`) | `server.token` (same gate as `rest`); warns when the port is set, the address is non-loopback, and `server.token` is empty |
| `webhook` | `webhook.token` |
| `github_webhook` | `github_webhook.secret` |

The `twilio_webhook` listener has no built-in authentication, so the daemon
never warns for it.

**Preferred fixes — protect or un-expose the listener:**

- Set the listener's token (`server.token`, `server.ttls`, `mcp.token`,
  `webhook.token`, `github_webhook.secret`).
- Bind the listener to loopback (`server.host: "127.0.0.1"`,
  `mcp.sse_host: "127.0.0.1"`) and expose it through a reverse proxy, as
  in the Recommended deployment above. A listener bound only to
  `127.0.0.1` (or `::1` / `localhost`) never produces the warning, with or
  without a token.

**Or acknowledge it** — if exposing the listener open is deliberate (for
example an air-gapped LAN dashboard), set
`server.acknowledge_unauthenticated: true`. That silences the warning and
sets the `acknowledged` flag on the matching entries of the `/api/health`
`listeners` array and the `listener_*` diagnose checks. It does **not**
change the bind address, the token, or the warning condition itself: with
it set, an exposed, unauthenticated listener is still exposed — the flag
only records that you reviewed it. Default is `false`.

| Access method | How to set `server.acknowledge_unauthenticated` |
|---|---|
| YAML | `server.acknowledge_unauthenticated: true` in `~/.datawatch/config.yaml` |
| CLI | `datawatch config set server.acknowledge_unauthenticated true` |
| Web UI | Settings → General → Web Server → “Acknowledge unauthenticated listeners” |
| REST API | `PUT /api/config` with `{"server.acknowledge_unauthenticated": true}`; read it back from `GET /api/config` (`server.acknowledge_unauthenticated`) |
| Comm channel | `configure server.acknowledge_unauthenticated=true` |

The per-listener posture is inspectable without logging in: `GET
/api/health` (no auth) returns a `listeners` array of
`{name, enabled, auth_required, loopback, acknowledged}` — posture only,
never bind addresses or token values — and `GET /api/diagnose` (auth
required) carries one informational `listener_<name>` check per listener
with detail `address=… enabled=… auth_required=… loopback=…
acknowledged=… exposed=…`.

### DNS Channel (Public-Facing)

The DNS service is designed to be the **only publicly exposed** listener. All other services should be bound to localhost or behind a VPN.

**Security features:**
- **HMAC-SHA256 authentication** — every query must include a valid signature
- **Nonce replay protection** — bounded store (10K entries, 5-minute TTL)
- **Per-IP rate limiting** — default 30 queries/IP/minute (configurable)
- **Uniform REFUSED response** — invalid auth, wrong domain, non-TXT queries, and rate-exceeded all return identical `REFUSED` responses (no information leakage)
- **No recursion** — the server does not resolve queries for other domains

**Recommended DNS deployment:**

```yaml
dns_channel:
  enabled: true
  mode: server
  domain: ctl.example.com
  listen: "0.0.0.0:53"       # public-facing
  secret: "$(openssl rand -hex 32)"  # 64-char random secret
  rate_limit: 30              # per-IP per-minute
  max_response_size: 512
```

**Firewall rules (iptables example):**

```bash
# Allow DNS on port 53 (UDP + TCP)
iptables -A INPUT -p udp --dport 53 -j ACCEPT
iptables -A INPUT -p tcp --dport 53 -j ACCEPT

# Block all other datawatch ports from external access
iptables -A INPUT -p tcp --dport 8080 -s 127.0.0.0/8 -j ACCEPT
iptables -A INPUT -p tcp --dport 8080 -j DROP
iptables -A INPUT -p tcp --dport 8081 -s 127.0.0.0/8 -j ACCEPT
iptables -A INPUT -p tcp --dport 8081 -j DROP
```

**With Tailscale (recommended):**

```bash
# Bind HTTP to Tailscale interface only
server:
  host: "100.x.y.z"   # Your Tailscale IP
  port: 8080
  token: "your-token"

# DNS on public interface
dns_channel:
  listen: "0.0.0.0:53"
```

### Webhook Security

Webhooks default to `127.0.0.1` and require a reverse proxy or tunnel for external access:

```yaml
# GitHub: always set a webhook secret
github_webhook:
  addr: "127.0.0.1:9001"
  secret: "github-webhook-secret"

# Generic webhook: set a bearer token
webhook:
  addr: "127.0.0.1:9002"
  token: "webhook-bearer-token"

# Twilio: use behind a reverse proxy with IP allowlisting
twilio:
  webhook_addr: "127.0.0.1:9003"
```

For external access, use a reverse proxy or `ngrok`/`cloudflared` tunnel.

### PWA Install (Add to Home Screen)

> **Full PWA setup guide:** See [pwa-setup.md](pwa-setup.md) for complete instructions including TLS configuration, CA certificate installation, and standalone mode checklist.

Datawatch is a Progressive Web App. When installed to your home screen, it runs in
standalone mode (no browser address bar, full-screen app experience).

**Requirements for standalone mode:**
- **HTTPS** — Chrome requires HTTPS for service worker registration (except localhost).
  Without a service worker, "Add to Home Screen" creates a browser shortcut, not a standalone app.
- **PNG icons** — Chrome Android requires PNG icons (192x192 and 512x512) in the manifest.
  SVG-only manifests may fall back to browser mode.

**To enable standalone PWA on your network:**

1. Enable TLS in datawatch:
   ```yaml
   server:
     tls: true
     tls_auto_generate: true
   ```
2. Navigate to `https://your-host:8080` on your phone
3. Accept the self-signed certificate warning
4. Tap the browser menu (three dots) → "Add to Home Screen" or "Install app"
5. The app will launch in standalone mode (no address bar)

**If using Tailscale:** Access via `https://your-host.your-tailnet.ts.net:8080` —
Tailscale provides valid HTTPS certificates automatically.

**Tab navigation in standalone mode:** Use the bottom navigation bar built into
datawatch (Sessions, New, Alerts, Settings) — the browser tab bar is hidden.

---

### Chrome Push Notifications (Android / HTTP)

Chrome on Android blocks notification permissions on non-HTTPS sites. If you see
"Notifications blocked" when trying to enable push notifications, use one of these fixes:

**Option A: Enable TLS in datawatch (recommended)**
```yaml
server:
  tls: true
  tls_auto_generate: true  # auto-generates self-signed cert
```
Or via comm channel: `configure server.tls=true`

Access via `https://your-host:8080` and accept the certificate warning once.

**Option B: Tailscale HTTPS**

If using Tailscale, access via the auto-HTTPS URL:
```
https://your-host.your-tailnet.ts.net:8080
```
Tailscale provides valid certs automatically.

**Option C: Install CA certificate on your device**

When using self-signed TLS (`tls_auto_generate: true`), install the CA cert so your browser trusts it. In the web UI: Settings > Comms > Web Server > **Download CA Certificate (.crt)**. Or visit: `https://your-host:8443/api/cert?format=der`

**Android:**
1. Download the .crt file from the link above
2. Settings → Security & privacy → More security & privacy → Encryption & credentials → Install a certificate → CA certificate
3. Select the downloaded file, confirm install

**iPhone/iPad:**
1. Download the .pem file (use the "PEM format" link or `https://your-host:8443/api/cert`)
2. Settings → General → VPN & Device Management → tap the downloaded profile → Install
3. Settings → General → About → Certificate Trust Settings → enable full trust for the datawatch certificate

**Option D: Chrome flag override (quick workaround, no TLS needed)**

1. On Android Chrome, navigate to `chrome://flags`
2. Search for **Insecure origins treated as secure**
3. Add your datawatch URL: `http://your-host.example.com:8080`
4. Set to **Enabled**, relaunch Chrome
5. Navigate back to datawatch — notification permission prompt will now appear

> **Note:** Options A–C also enable service worker registration for PWA install-to-homescreen.

### Service worker on a self-signed certificate

When you browse the PWA with the self-signed certificate (`server.tls_auto_generate: true`) and accept the certificate warning, the browser rejects service worker registration because the origin's TLS is not trusted. That is expected, not a failure: the browser console shows one info line beginning "Service worker is off on this origin" (a quiet `console.info`, at most once per page load — not an error). The app works normally; only the offline cache, push and install-to-home-screen are unavailable on that origin.

To enable the service worker, use a certificate the browser trusts:

- **Install the daemon's CA certificate** into the OS/browser trust store —
  Settings > Comms > Web Server > **Download CA Certificate**, or
  `https://your-host:8443/api/cert?format=der` (the per-device install steps
  above, *Option C: Install CA certificate on your device*). Or
- **Use the native ACME / Let's Encrypt path** already documented in
  [Native ACME / Let's Encrypt](#native-acme--lets-encrypt-bl397) below. Or
- **Browse via `https://localhost:8443`** — the browser trusts localhost,
  so the service worker registers there without extra steps.

Then hard-reload the page (or run `forcePWAUpdate()` in the browser console)
and the service worker registers normally.

---

### Prompt and Input Logging

**Important:** Session prompts and user inputs are logged in alert messages, session tracking files, and communication channel notifications. This means any text typed into a session prompt — including responses to AI tool-approval dialogs — is visible in:

- Alert history (`datawatch alerts`)
- Communication channel messages (Signal, Telegram, etc.)
- Session tracking folder (`conversation.md`, `timeline.md`)
- Web UI alert panel

**Never type passwords, API keys, or secrets directly into AI session prompts.** Instead:
- Use environment variables (`export MY_API_KEY=...`)
- Use secrets managers or credential stores
- Configure credentials in config files (encrypted with `--secure`)
- Use `.env` files that are gitignored

This is good security practice for AI coding in general — AI assistants may log, transmit, or include prompt text in API calls.

### TLS Configuration

Both the HTTP server and MCP SSE server support TLS:

```yaml
server:
  tls: true
  tls_cert: "/path/to/cert.pem"    # omit for auto-generated self-signed
  tls_key: "/path/to/key.pem"

mcp:
  tls_enabled: true
  tls_cert: "/path/to/cert.pem"
  tls_key: "/path/to/key.pem"
```

When `tls_auto_generate` is true (default), self-signed certificates are generated in `{data_dir}/tls/` if no cert/key paths are provided.

### Native ACME / Let's Encrypt (BL397)

For a public deployment with a real hostname, datawatch can obtain and
auto-renew a browser-trusted certificate itself — no external tool
(Caddy, certbot) and no DNS management. The daemon performs the full
ACME HTTP-01 handshake, writes the issued cert alongside the existing
`{data_dir}/tls/` auto-cert location, and renews automatically.

```yaml
acme:
  enabled: true
  endpoint: staging              # staging (default, untrusted certs,
                                  # safe to test against) | production
  domains:
    - datawatch.example.com      # one or more names on ONE cert
  method: http01                 # http01 (no DNS management) | dns01
                                  # (wildcard support, needs a provider token)
  renewal_days: 30               # renew when <= N days of life remain
  retry:
    interval_minutes: 30
    max_consecutive_failures: 5
  apply:
    update_mcp_cert: true        # also point mcp.tls_cert/tls_key at it
    hot_swap: false              # true = most renewals apply with zero
                                  # downtime (see "Cert apply" below)
```

**What `http01` requires**: one A/AAAA record for each domain pointing
at this host, and **inbound TCP/80 reachable from the public internet**
— Let's Encrypt's validator connects to port 80 specifically, which is
not the same as `server.port` (default `8080`; the existing dual-port
model only *redirects* 8080→8443, it doesn't bind port 80). To actually
satisfy HTTP-01 you need either:
- `server.port: 80` directly (root, or `setcap cap_net_bind_service=+ep`
  on the binary if running as an unprivileged user), or
- an external port-forward from 80 to whatever `server.port` is.

**What `dns01` requires** (no port 80 needed — use this for wildcards,
or when port 80 genuinely can't be opened): a zone-scoped DNS provider
API token. Only Cloudflare is wired in this build:

```yaml
acme:
  method: dns01
  dns01:
    provider: cloudflare
    token_secret: "${secret:cf-zone-edit-token}"  # Zone > DNS > Edit on
                                                     # ONE zone — never the
                                                     # account-global key
    zone_id: "..."                                  # reference only
```

Store the token via the secrets manager (`datawatch secret set
cf-zone-edit-token <token>` or the PWA Secrets card) — `token_secret` is
a `${secret:name}` reference, same convention as every other credential
in this codebase. The resolved token is held only in memory; `config.yaml`
always keeps the unresolved reference, even across renewals.

**Workflow**: start with `endpoint: staging` (Let's Encrypt's staging
directory — generous rate limits, but the cert itself isn't browser
trusted) to verify the whole flow works, then flip to
`endpoint: production` for a real cert. Each endpoint is tracked as a
**separate ACME account registration** — switching endpoints re-registers
automatically (same underlying account key, new registration), so no
manual account reset is needed. Note that flipping `endpoint` alone
doesn't force a new order — the existing cert isn't due for renewal yet —
run `datawatch acme renew` explicitly after the flip.

**Cert apply**: by default, applying a new/renewed cert writes the PEM,
updates `server.tls_cert`/`tls_key` (and MCP's, if `apply.update_mcp_cert`),
and restarts the daemon — one short window per issue/renewal (roughly
every 90 days). Set `apply.hot_swap: true` to apply with **zero
downtime** instead: the TLS listener reloads the cert from disk on its
next handshake when the file's mtime changes, no restart. The one
exception is the very first switch to ACME (or the first-ever issue),
where the cert *path* itself is changing — a running listener can't
hot-reload a path it was never watching, so that one restart still
happens regardless of `hot_swap`.

**Other surfaces**: `GET/POST /api/acme/{status,renew,verify}`; MCP tools
`acme_status`/`acme_renew`/`acme_issuer_log`; CLI
`datawatch acme status|renew|verify`; comm verb
`!acme status|renew|verify`; PWA Settings → Web Server card's
certificate-source selector (Self-signed | Custom | Let's Encrypt), with
a validation-method picker when Let's Encrypt is selected.

Full design, the operator-interview decision log, and the real bugs
found during live verification: `docs/plans/historical-plans/2026-10-06-bl397-native-acme-letsencrypt.md`.

### APNs Push Notifications (BL397 Phase 4 / BL335)

The daemon can send push notifications directly to registered iOS
devices via Apple's APNs provider API — closing the gap where device
registration (`POST /api/devices/register`, `kind: apns`) worked but
nothing ever actually sent a push.

```yaml
push:
  apns:
    enabled: true
    key_id: "ABC1234567"        # from the Apple Developer portal
    team_id: "TEAM123456"
    bundle_id: "com.example.YourApp"
    key_secret: "${secret:apns-auth-key}"  # the .p8 key's PEM contents
    # key_path: "/path/to/AuthKey_ABC1234567.p8"  # alternative to key_secret
    sandbox: false               # true for TestFlight / development builds
```

Either `key_secret` (a `${secret:name}` reference holding the raw `.p8`
PEM — the Auth Key is a long-lived credential, treat it the same as any
other secret) or `key_path` (a file on disk) works; `key_secret` takes
precedence if both are set.

**Dispatch**: every new alert automatically fans out to every registered
`kind: apns` device — same trigger point the existing FCM/UnifiedPush
path uses. A device that returns Apple's `410 Unregistered` (app
uninstalled, token rotated) is automatically pruned from the registry.

**Test send**: `POST /api/push/apns/test` (optional `{"device_id":
"..."}` to target one device, omit for all) or
`datawatch push apns-test [--device-id <id>]` — useful for verifying the
key/config is correct without waiting for a real alert.

**Sandbox vs. production**: each device's own `apns_environment`
(`"production"` or `"development"`, sent by the app at
`POST /api/devices/register` and refreshed on every re-registration —
GH#183) decides which host that device's pushes go to:
`api.sandbox.push.apple.com` for `"development"` (TestFlight and Xcode
debug builds), `api.push.apple.com` for `"production"` (App Store
builds). This is per-device because a daemon can have both kinds of
builds registered at once. `push.apns.sandbox: true` only sets the
fallback used for a device that never sent `apns_environment` at all
(devices registered before this field existed) — it is not a daemon-wide
switch once devices report their own environment.

### Encryption at Rest

When `--secure` mode is enabled, all data stores are encrypted with XChaCha20-Poly1305.
Set `DATAWATCH_SECURE_PASSWORD` as an environment variable for automatic restarts.
See [encryption.md](encryption.md) for details.

### Interface Configuration Summary

Every listener's bind address is fully configurable:

| Config Key | Field | Controls |
|------------|-------|----------|
| `server.host` | HTTP/PWA bind interface | `0.0.0.0` / `127.0.0.1` / specific IP |
| `server.port` | HTTP/PWA port | Any port number |
| `mcp.sse_host` | MCP SSE bind interface | `0.0.0.0` / `127.0.0.1` / specific IP |
| `mcp.sse_port` | MCP SSE port | Any port number |
| `mcp.public_url` | URL advertised to MCP SSE clients | Reverse-proxy URL, or empty for the bind address |
| `dns_channel.listen` | DNS bind address | `host:port` format |
| `github_webhook.addr` | GitHub webhook bind | `host:port` format |
| `webhook.addr` | Generic webhook bind | `host:port` format |
| `twilio.webhook_addr` | Twilio webhook bind | `host:port` format |

### Generic Webhook Image Attachments

`POST /task` on the generic webhook listener (`webhook.addr`, default
`127.0.0.1:9002`) accepts an optional `image_url` field. A
`data:<mime>;base64,...` value always works. A plain local file path is
**disabled by default** — set `webhook.image_dir` to the one directory
you want this feature allowed to read from:

```yaml
webhook:
  image_dir: "~/datawatch-webhook-images"
```

Only paths resolving inside that directory are accepted; anything else
(including `../` traversal attempts) is rejected. Before this existed,
`image_url` accepted **any** path the daemon process could read, with no
restriction at all — and this listener's bearer token is itself optional
(`webhook.token` unset means no auth), so any caller able to reach the
listener could read any file. If you were relying on local-path image
attachments before upgrading, set `webhook.image_dir` to get the feature
back, scoped.

### Outbound Mobile Push Endpoint Validation

`POST /api/push/register` lets a mobile client register an endpoint the
daemon delivers push notifications to. By default the daemon:

- Always refuses loopback, link-local, and cloud-metadata addresses — no
  configuration can allow these.
- Requires `https://` for the registered endpoint.
- Allows private-range addresses (RFC1918, Tailscale CGNAT `100.64.0.0/10`,
  IPv6 ULA) — this is deliberate, since that's where most self-hosted push
  distributors (ntfy, Gotify) actually run.

If your self-hosted push distributor runs over plain `http://` on your
LAN (common for a local ntfy instance with no TLS in front of it), set:

```yaml
push:
  allow_insecure_endpoints: true
```

If you want to additionally lock the daemon down so it can *only* push to
public internet addresses (e.g. a hosted ntfy/UnifiedPush provider, never
anything on your own network), set:

```yaml
push:
  block_private_endpoints: true
```

Both default `false`. If push notifications stop arriving after an
upgrade and you run a self-hosted distributor, check these two settings
first before anything else — see `internal/server/push_ssrf.go`'s header
comment for the full design rationale if you need to dig further.

### Remote PWA Proxy Sandbox Origin

Viewing a federation peer's own PWA (the "PWA" link next to a server in
Settings → Servers, or `GET /remote/{name}/`) is served from a **second
listener**, bound to `server.proxy_sandbox_port` (default `8444`) on the
same host and TLS cert as the main server — a genuinely separate browser
origin from your own dashboard, not just a different path. This exists
because the PWA authenticates every API call with a token read from
`localStorage`, not a cookie: without origin separation, a malicious or
compromised peer's proxied JS could read that token directly out of
`localStorage` and act with your full daemon privileges. A different
origin has its own, naturally separate `localStorage`, so there is
nothing for proxied content to steal regardless of what its JS does.

`GET /remote/{name}/` on the **main** origin now only 301-redirects to
the sandbox origin (old bookmarks/links still work); it never serves
proxied content itself. If `server.proxy_sandbox_port` is `0` or
negative, the sandbox listener is disabled and viewing a remote peer's
PWA becomes unavailable — it does **not** fall back to the old,
same-origin behavior. If a firewall or port conflict prevents the
sandbox port from binding, the daemon logs a warning and starts
normally otherwise; check the startup log for `proxy sandbox listener`
if the "PWA" link stops working after an upgrade.

The sandbox origin's own CSP names your main dashboard's origin in
`frame-ancestors` (computed per-request from whatever hostname you used
to reach it, so it works the same over localhost, a LAN IP, or a
Tailscale name) rather than `'self'` — it is only ever meant to be
embedded by your own daemon, never anyone else. See
`docs/plans/historical-plans/2026-10-03-bl394-security-findings-review.md` §6 for the
full design writeup, including why this needed a second origin rather
than CSP or iframe sandboxing alone.

**Embedded viewer:** the "PWA" link opens the peer's dashboard inline
(a modal, with an "↗ New tab" link and an expand toggle, for the cases
where you want a full separate window instead) rather than only
switching tabs. The embedded dashboard authenticates its own API calls
with a short-lived (1 hour), single-peer-scoped token — minted only
after your own login already succeeded, valid for nothing except
proxying to that one peer, and never the real admin token, so a
compromised peer's JS reading it out of the sandbox origin's storage
can't reach anything beyond that one peer for longer than an hour. One
small, known, cosmetic gap: the embedded view's own staleness check
(`/api/health`) always 401s, since that endpoint is intentionally
unauthenticated on the main origin but the proxy path requires auth for
its whole surface — harmless, just means that one version-mismatch
reload guard doesn't fire for a proxied view. See
`docs/plans/historical-plans/2026-10-03-bl394-security-findings-review.md` §6a for the
full writeup, including four more bugs (a missing CSP `frame-src`, the
token-passing gaps above, and a pre-existing `Accept-Encoding` bug that
silently corrupted every proxied script) found only by actually driving
a real browser against this feature.

### OWASP ZAP workflow and GitHub API throttling

The `owasp-zap` workflow runs after the Release workflow completes and scans
the daemon's PWA and API surfaces with OWASP ZAP. Its last step, **Attach
ZAP reports to GitHub release**, uploads the baseline and API-scan HTML
reports to the GitHub release page for the tag being released. That step only
runs for a real release tag — branch refs are normalized away and ignored, so
a main-branch run never passes a tag name to the upload.

The attach step uses the installation's GitHub token, and GitHub can throttle
that token at times — it answers that the installation token has been
throttled, rather than serving the upload. When that happens the step retries
a bounded number of times with a growing wait (30s, 60s, 120s, 240s, never
sleeping less than until the reported reset), retrying only when the response
indicates throttling. If the token is still throttled after all attempts, the
step logs a warning and the run stays green — the **scan result is what
counts**, and the reports remain available from the run's `owasp-zap-reports`
artifact, which GitHub retains for 90 days. An attach failure unrelated to
throttling still fails the run, and a scan finding above the accepted
thresholds still fails the run; only the release attach is tolerated.

To attach the reports yourself, download the `owasp-zap-reports` run artifact
and re-upload with:

```bash
gh release upload <release-tag> report_pwa-baseline.html report_api-scan.html --clobber
```

or simply re-run the workflow later, once GitHub has lifted the throttling
on the installation token — the attach step will then complete normally.

---

## 8. Web UI Features

### Alert Format

Alert titles follow the format `hostname: name [id]: event` (e.g.
`ralfthewise: myproject [a1b2]: running → waiting_input`). Toast notifications in the
web UI show a truncated title only (no body text); the full alert with prompt context
is available in the Alerts tab.

For claude-code sessions with an active MCP channel, console-based state detection is
suppressed after the channel connects (`channel_ready: true`). This eliminates the
rapid `running ↔ waiting_input` cycling caused by screen-scraping Claude's animated
terminal. See `docs/claude-channel.md` for details.

### Suppress Toasts for Active Session

**Config key:** `server.suppress_active_toasts` (default: `true`)

When enabled, toast notifications about a session's state changes (e.g. "running → waiting_input") are hidden while you are actively viewing that session's detail page. This reduces notification noise when you're already watching the output.

The setting is stored server-side in the config file and applies across all browsers/devices.

### Auto-Restart on Config Save

**Config key:** `server.auto_restart_on_config` (default: `false`)

When enabled, the daemon automatically restarts after saving configuration changes that require a restart (host, port, TLS, MCP binds). If the config is encrypted and `DATAWATCH_SECURE_PASSWORD` is not set, the restart is skipped with a warning toast.

**Why most config changes don't trigger a restart even when this is on:** the daemon hot-applies the majority of config keys via `applyConfigPatch` — LLM toggles, comm-channel routing, autonomous knobs, memory settings, plugins. Only keys in the `RESTART_FIELDS` set genuinely can't hot-reload (port bind, TLS cert path, MCP server start). When you save one of those with auto-restart on, the daemon restarts; when you save anything else, the change applies live and the toggle stays silent — that's by design, not a bug.

**Subsystem hot-reload (v5.27.2 — operator-asked).** `POST /api/reload?subsystem=<name>` hot-reloads a specific subsystem without restarting the daemon. Registered names: `config` (full hot-reload), `filters` (invalidate FilterEngine regex cache), `memory` (refresh memory adapter caches). Reachable through every parity surface:

```bash
# CLI
datawatch reload                # full config reload (default)
datawatch reload filters        # subsystem-specific

# REST
curl -X POST 'https://localhost:8443/api/reload?subsystem=filters' \
  -H "Authorization: Bearer $TOKEN"

# MCP (any IDE client)
{ "name": "reload", "arguments": { "subsystem": "filters" } }

# Comm channel (Signal / Telegram / etc.)
reload filters
```

### Claude Auto-Accept Disclaimer (v5.27.2)

**Config key:** `session.claude_auto_accept_disclaimer` (default: `false`)

When enabled, datawatch detects claude-code's startup disclaimer / "trust this folder" / "Loading development channels" prompts and auto-accepts them by sending the appropriate confirmation key (Enter for plain confirms; "1\n" for the numbered "Yes, I trust this folder" menu).

The detection mirrors the PRD-automation pattern: the prompt is recognised via `FilterActionDetectPrompt`, marked as `waiting_input`, and 750 ms later the auto-response is sent. Only fires when the active backend is `claude-code` and the flag is on; the existing operator-side trust-prompt review path is unaffected when the flag is off.

```bash
# Enable via CLI
datawatch config set session.claude_auto_accept_disclaimer true
datawatch reload                # apply (the flag is read at runtime so reload picks it up)

# Reachable through every parity surface — REST PUT /api/config,
# MCP config_set, comm `configure session.claude_auto_accept_disclaimer true`,
# PWA Settings → LLM → Auto-accept startup disclaimer toggle.
```

### ANSI Terminal (xterm.js)

Session output is rendered in a real terminal emulator (xterm.js) with full ANSI color and TUI support. TUI applications like `top`, `htop`, and interactive LLM UIs (claude, opencode) render correctly with cursor positioning, colors, and scrollback.

The terminal auto-fits to the container width and supports 5000-line scrollback. If xterm.js fails to load, output falls back to plain-text rendering.

**Click-to-type:** You can click directly on the terminal area and type — keystrokes are sent to the tmux session in real time. This works for interactive prompts, shell commands, and any input the running process expects. The input bar below the terminal also sends text, but direct typing in the terminal is often faster for short responses.

**Anti-clobber typing hold (v8.9.17+):** When an agent or automation sends a message into an active session, the daemon checks whether you are actively typing in the xterm terminal. If TTY activity was detected within the last 30 seconds, the message is held until you pause (up to 90 seconds, then it sends anyway so no message is ever lost). Multiple messages that arrive while you are typing queue up in order — the first message waits for the idle gap, subsequent queued messages flush immediately after without re-waiting. This prevents agent pushes from clobbering mid-keystroke operator input when multiple sessions are running in parallel.

**Line-printing renderer lock (v8.9.20+):** datawatch writes `"tui": "default"` into its scoped claude settings at startup (`<dataDir>/.claude/settings.json`). This pins the claude-code renderer to line-printing mode so tmux scrollback and `pipe-pane` capture continue to work correctly. Without this, Anthropic's server-side `tengu_pewter_brook` flag can switch claude-code sessions into an alternate-screen mode (`\e[?1049h`) that bypasses the tmux scrollback buffer and breaks `pipe-pane` capture.

**Known issue:** The web terminal display occasionally gets out of sync with the actual tmux pane content (blank screen, frozen output, or garbled rendering). If this happens, navigate back to the session list and re-enter the session, or refresh the browser page. This resets the screen capture and xterm.js state. Pull requests to improve terminal sync reliability are welcome.

### Scheduled Prompts

Schedule commands to be sent to sessions at a future time or when a session enters the waiting-for-input state.

**Natural language time:** "in 30 minutes", "at 14:00", "tomorrow at 9am", "next wednesday at 2pm"

View and manage schedules in:
- **Session detail** — inline pending schedules with cancel buttons
- **Sessions page** — badge showing pending schedule count with dropdown
- **Settings** — collapsible paginated list of all scheduled events

### Detection Filters

Prompt, completion, rate-limit, and input-needed patterns are configurable via the `detection` config section. Defaults match the built-in patterns; customize per deployment via config file or API.

### System Statistics

Statistics are collected every 5 seconds and streamed to the web UI in real-time via WebSocket.

**Metrics collected:**

| Category | Metric | Source | Root Required |
|----------|--------|--------|---------------|
| CPU | Load average (1/5/15 min), core count | `/proc/loadavg` | No |
| Memory | Total, used, available, swap | `/proc/meminfo` | No |
| Disk | Total, used, free (data_dir partition) | `syscall.Statfs` | No |
| GPU | Name, temperature, utilization, VRAM | `nvidia-smi` exec | No |
| Network | Total RX/TX bytes (all interfaces) | `/proc/net/dev` | No |
| Daemon | RSS, goroutines, open FDs | `/proc/self/statm`, `/proc/self/fd` | No |
| Sessions | Active/total count | Session store | No |
| Per-session | Memory (RSS + children), uptime | `/proc/<pid>/statm`, `pgrep` | No |
| Tmux | Session count, orphaned sessions | `tmux list-sessions` | No |
| Uptime | Daemon uptime | Internal timer | No |
| Interfaces | Bound network interfaces | Config | No |

**API endpoints:**
- `GET /api/stats` — latest snapshot (all metrics above)
- `GET /api/stats?history=N` — last N minutes of time-series data (ring buffer, 720 entries max)
- `POST /api/stats/kill-orphans` — kill orphaned tmux sessions
- `WS stats` message — real-time broadcast on every collection cycle

**Web UI:** Settings → System Statistics shows a live dashboard with auto-updating cards.
No manual refresh needed — data streams via WebSocket every 5 seconds.

**Messaging:** `stats` command returns a text summary (CPU, memory, disk, sessions, GPU).

**Finished one-shot sessions:** a one-shot whose command has ended but whose session
never reached complete is closed automatically at startup and every minute (it runs
the normal completion step, closes its pane, removes its task file). Settings → About →
*Finished one-shot sessions* lists any still open and cleans them up on demand
(`GET`/`POST /api/sessions/finished-oneshots`). A one-shot started by hand commits only
after its `--validate` commands pass. Finished session folders are deleted after
`session.session_retention_days` (default 30).

**Two Automata on one tree:** starting an Automaton is refused while another one is
running on the same project tree. Run them one at a time, or give one its own worktree.

**RTK Token Savings:** When `rtk.enabled: true` and RTK is installed, the stats dashboard
includes token savings metrics: total commands compressed, total tokens saved, and average
savings percentage. RTK auto-initializes hooks on supported backends (claude-code, gemini,
aider) if `rtk.auto_init: true`. See `rtk` section in `docs/config-reference.yaml`.

**Future:** Per-session network/CPU via eBPF (see `docs/plans/2026-03-30-ebpf-stats.md`).

### Rate-Limit Auto-Recovery

When a session hits a rate limit, datawatch automatically:
1. Detects the rate limit via console output patterns
2. Selects the "wait" option in Claude's rate limit menu
3. Creates a **persisted scheduled command** to resume after the limit resets
4. The schedule survives daemon restarts (stored in `schedule.json`)
5. At the scheduled time, sends a resume message with context from `PAUSED.md`

This is a key differentiator — no other tool recovers from rate limits automatically.

### Backend Profiles and Fallback Chains

Named profiles allow different accounts/API keys for the same backend.

| Method | How |
|--------|-----|
| **YAML** | `profiles:` section + `session.fallback_chain:` in `config.yaml` |
| **Web UI** | Settings → General → **Profiles & Fallback** card; New Session → **Profile dropdown** |
| **REST API** | `GET /api/profiles`, `POST /api/profiles`, `DELETE /api/profiles?name=X` |
| **Comm channel** | `configure session.fallback_chain=claude-personal,gemini-backup`; `new claude-personal: <task>` |

```yaml
profiles:
  claude-work:
    backend: claude-code
  claude-personal:
    backend: claude-code
    env:
      ANTHROPIC_API_KEY: "sk-ant-..."

session:
  fallback_chain: ["claude-personal", "gemini-fallback"]
```

When the primary backend hits a rate limit, datawatch auto-switches to the next profile in the chain.

### Voice Input (Whisper Transcription)

Voice messages sent via Telegram or Signal are automatically transcribed to text using
OpenAI Whisper, then processed as normal commands.

| Method | How |
|--------|-----|
| **YAML** | `whisper:` section in `config.yaml` |
| **CLI** | `datawatch config set whisper.model small`, `datawatch config set whisper.chunk_seconds 120` |
| **Web UI** | Settings → General → **Voice Input (Whisper)** card |
| **REST API** | `PUT /api/config` with `{"whisper.enabled": true, "whisper.model": "small", "whisper.chunk_seconds": 120}` |
| **Comm channel** | `configure whisper.enabled=true`, `configure whisper.language=es`, `configure whisper.chunk_seconds=120` |

**Requirements:** Python venv with `openai-whisper`, ffmpeg on PATH.

```yaml
whisper:
  enabled: true
  model: base        # tiny/base/small/medium/large
  language: en       # ISO 639-1 code, or "auto" for detection
  venv_path: .venv   # path to Python venv
  chunk_seconds: 120 # local whisper: split audio longer than this (0 = off, else >= 30)
```

**Long recordings.** The local whisper backend splits audio longer than
`whisper.chunk_seconds` (default 120, `0` turns it off, otherwise 30 or more) and
transcribes it slice by slice, so memory stays flat. PCM WAV is split in pure Go;
other formats need `ffmpeg` and are sent whole when it is missing. Hosts with 8 GiB
of RAM or less should use the `small` or `base` model. A failed transcription
returns HTTP 502 with a `cause` (`timeout`, `killed`, `out_of_memory`,
`ffmpeg_missing`, `backend_error`) and a `hint`, and writes a `[voice] transcribe
failed:` log line; the PWA keeps the recording with Retry and Discard. See
[howto/voice-input.md](howto/voice-input.md#long-recordings-and-memory-local-whisper).

99 languages supported. Per-user language preferences are planned for the multi-user access control feature.

See [setup.md](setup.md#optional-set-up-voice-input-whisper) for installation and
[messaging-backends.md](messaging-backends.md#voice-input-whisper-transcription) for details.

### Browser Auto-Refresh

When the daemon is updated and restarted, connected browsers automatically reload to
pick up new JavaScript/CSS assets. The daemon version is included in WebSocket `sessions`
messages; if the client detects a version mismatch after reconnecting, it triggers
`location.reload()`.

### Proxy Mode (Multi-Machine Management)

When remote servers are configured, datawatch acts as a proxy relay:

| Feature | How it works |
|---------|-------------|
| **Aggregated list** | `list` command shows sessions from all servers |
| **Auto-routing** | `send a3f2: yes` finds which server owns the session |
| **Explicit routing** | `new: @prod: task` creates on a specific remote |
| **WS relay** | Selecting a remote in web UI proxies all API calls + WS |
| **CLI** | `datawatch --server prod session list` targets a specific remote |

**Setup:** Add remote servers via `datawatch setup server` or edit `config.yaml`:

```yaml
servers:
  - name: prod
    url: http://203.0.113.10:8080
    token: "bearer-token"
    enabled: true
```

**Monitoring:**
- `GET /api/servers` — lists all configured remotes with health status
- `GET /api/sessions/aggregated` — sessions from all remotes, tagged with `server` field
- Web UI server picker (Settings → Comms → Servers) switches between instances

**Troubleshooting:**
- If a remote is unreachable, `list` skips it (5s timeout per server)
- Session discovery cache refreshes every 30 seconds
- Check remote health: `curl http://remote:8080/healthz`
- Verify token: proxy injects `Authorization: Bearer <token>` on all forwarded requests

See [setup.md](setup.md#optional-set-up-proxy-mode-multi-machine) for full setup and [architecture.md](architecture.md#proxy-mode-architecture) for flow diagrams.

### Configure via Chat Commands

All comm channel backends (Signal, Telegram, Discord, Slack, etc.) support the `configure` command to change settings without the web UI:

```
configure list                          — show all configurable keys
configure session.console_cols=120      — set a value
configure session.llm_backend=aider     — change LLM backend
configure rtk.enabled=true              — enable RTK
configure whisper.enabled=true          — enable voice input
```

Changes are applied via the REST API (`PUT /api/config`) and saved to `config.yaml`. Some settings (host, port, TLS) require a daemon restart.

### Test Message Endpoint

`POST /api/test/message` simulates an incoming comm channel message through the router. This enables testing all chat commands without needing actual Signal/Telegram connections:

```bash
# Test help command
curl -X POST http://localhost:8080/api/test/message \
  -H 'Content-Type: application/json' \
  -d '{"text":"help"}'

# Test session creation
curl -X POST http://localhost:8080/api/test/message \
  -H 'Content-Type: application/json' \
  -d '{"text":"new: echo hello"}'

# Test configure
curl -X POST http://localhost:8080/api/test/message \
  -H 'Content-Type: application/json' \
  -d '{"text":"configure list"}'
```

Returns `{"input":"...","responses":[...],"count":N}` with the exact responses the router would send to the messaging backend.

Supported commands: `help`, `list`, `status <id>`, `tail <id>`, `send <id>: <msg>`, `kill <id>`, `new: <task>`, `version`, `stats`, `alerts`, `configure`, `schedule`, `setup`, `update check`.

### MCP Channel Cleanup

On daemon startup, stale MCP channel registrations (from deleted sessions) are
automatically removed from Claude's MCP config. This prevents the MCP server list from
growing unboundedly over time.

### MCP transport diagnostics (connection drops, stream health)

Every MCP SSE stream is observed: the daemon logs one `STREAM OPEN` / `STREAM CLOSE`
line pair per stream (duration, write count, bytes, idle time at close, and the first
write/flush error if one occurred), so client disconnects, middlebox kills,
and server-side write failures are distinguishable from the daemon log alone. The
stdio MCP entry points (`datawatch mcp`) log their listener start and stop reason
(client disconnect / error / recovered panic). Failed tool handlers no longer kill
the bridge process — a panic returns an `isError` result and the session keeps
serving.

- **Keepalive:** the SSE stream pings every 10 s so idle read timeouts in proxies or
  clients do not silently kill held-open streams.
- **Timeout profile:** `ReadTimeout` 30 s / `IdleTimeout` 120 s, `WriteTimeout`
  deliberately unset (it is the only stdlib deadline that can kill an in-flight SSE
  stream); the exact profile is printed at serve start.
- **Verbose per-request logging:** set `DATAWATCH_MCP_DEBUG=1` on the daemon or
  bridge process.
- **`DATAWATCH_MCP_DEBUG` and the stream lines are log-channel output only** — the
  stdio MCP server's stdout stays strict JSON-RPC; recovery/diagnostic messages go
  to stderr, so no line outside the protocol can corrupt a client's stream.

---

## Chat Mode and Output Modes

Sessions can display in three output modes, configurable per-backend:

| Mode | Display | Best for |
|------|---------|----------|
| `terminal` | xterm.js interactive terminal (capture-pane) | Claude Code, OpenCode TUI apps |
| `log` | Formatted log viewer (color-coded status lines) | Headless/script backends |
| `chat` | Rich chat UI (message bubbles, streaming, memory commands) | Ollama, OpenWebUI, OpenCode-ACP |

### Defaults

| Backend | Default output_mode |
|---------|-------------------|
| Ollama | `chat` |
| OpenWebUI | `chat` |
| OpenCode-ACP | `chat` |
| Claude Code | `terminal` |
| OpenCode | `terminal` |
| All others | `terminal` |

### Configure per-backend

**Web UI:** Settings > LLM backend > Output mode dropdown (terminal / log / chat)

**API:**
```bash
curl -X PUT http://localhost:8080/api/config \
  -H 'Content-Type: application/json' \
  -d '{"ollama.output_mode": "chat"}'
```

**Comm channel:** `configure ollama.output_mode=chat`

**Config file:**
```yaml
ollama:
  output_mode: chat
opencode_acp:
  output_mode: chat
```

---

## Prompt Detection and Debounce

Datawatch monitors sessions for idle prompts to detect when an LLM is waiting for user input. Two configurable timers prevent false positives and notification floods:

### `detection.prompt_debounce` (default: 3 seconds)

After a prompt pattern is first detected, datawatch waits this many seconds before transitioning to `waiting_input`. If new output arrives during this window, the timer resets. This prevents false alerts during LLM thinking pauses (e.g., Claude between tool calls).

### `detection.notify_cooldown` (default: 15 seconds)

Minimum time between repeated "needs input" notifications for the same session. Even if the session oscillates between `running` and `waiting_input`, only one notification is sent per cooldown window.

### `detection.alert_settle` (default: 45 seconds)

A "needs input" alert or push is only sent once the session has stayed in `waiting_input` for this long. If it goes back to `running` first (a pause, a long tool call), the alert is dropped. Set higher to be quieter.

### `detection.alert_repeat` (default: 300 seconds)

An identical "needs input" alert for the same session (same prompt, no new input from you in between) is not sent again within this window. A new prompt, or a prompt after you reply, alerts normally.

### Configure

```bash
# API
curl -X PUT http://localhost:8080/api/config \
  -d '{"detection.prompt_debounce": 5, "detection.notify_cooldown": 30}'

# Comm channel
configure detection.prompt_debounce=5

# Config file
detection:
  prompt_debounce: 5
  notify_cooldown: 30
```

### Chat-mode sessions skip terminal detection

Sessions with `output_mode: chat` skip tmux capture-pane and idle timeout prompt detection entirely. Chat sessions use their conversation manager for state — the tmux pane is a fallback log, not the interactive surface.

## Rate-limit detection: the built-in per-backend table

Rate-limit (throttle) detection no longer starts from a generic list of phrases
that applies to every backend. The built-in matching is a table in the daemon,
keyed by backend family (`internal/session/rate_limit_lines.go`), and a phrase
counts only when the backend's own throttle line begins with it (after
surrounding whitespace is trimmed). A line that merely quotes a phrase in the
middle of a sentence — a log, an echoed command, a doc paragraph — never
pauses a backend. The shared `DATAWATCH_RATE_LIMITED:` protocol line is the one
rule that applies to every backend, also line-start only.

### This is a deliberate decision

- **The table replaces the generic list.** The former built-in default for
  `detection.rate_limit_patterns` was removed from `config.DefaultDetection()`;
  the field is now unset out of the box. The table lives in code, next to the
  fixtures that justify each entry (`testdata/rate_limit_lines/`), because a
  phrase is only trustworthy together with the backend that prints it and the
  line it was captured from. A flat editable list cannot carry that
  association.
- **Operator patterns still work.** If you set `detection.rate_limit_patterns`
  (globally or per LLM, from the file, `PUT /api/config`, a comm channel or
  Settings → Detection Filters), your list replaces the built-in table and is
  applied as before: a flat, case-insensitive substring match anywhere on the
  line, for every backend. Clear the list and the built-in table applies again.
- **Saving the settings card does not bring the generic list back.** The card
  shows no phrases and a note that the built-in table applies while no list is
  set. Only a pattern you add yourself is written to the config.
- **Adding a phrase for a backend.** Record the line in
  `testdata/rate_limit_lines/<backend>.txt` with its source in the directory
  README, then add the phrase to the table; a test checks both directions.
  Backends with no captured line rely on the protocol line or on an operator
  list until one is recorded.

After an upgrade the usage back-off status reports the pattern fingerprint as
stale until it is rechecked (`POST /api/usage-backoff/recheck`).

---

## PWA connection stability (cellular / Tailscale)

The PWA keeps its WebSocket healthy on flaky links (cellular handoffs, carrier NAT, Tailscale roaming) without hammering the daemon.

### Reconnect behavior

- **Jittered exponential backoff.** After a drop the PWA retries with a 1 s base delay doubling up to a 30 s cap, using full jitter (each delay is random in `0..current cap`) so many clients do not reconnect in lockstep.
- **15 s stability window.** The attempt counter resets only after a connection has stayed up for 15 s. A link that connects and drops again inside the window keeps its backoff growing instead of restarting at 1 s.
- **Immediate retry on recovery signals.** The browser `online` event and the tab becoming visible again both cancel the pending backoff timer and retry right away.
- **No timers while hidden.** A hidden tab schedules no reconnect or heartbeat timers; it resumes on the visibility event.

### Heartbeat and stale detection

The PWA sends a heartbeat every 15 s and treats the socket as stale after 35 s without any inbound traffic, then closes it and reconnects. Carrier NATs commonly drop idle TCP mappings after 30 s or less, so a quiet socket can be dead without the browser noticing; the 15 s heartbeat keeps the mapping alive and the 35 s threshold catches a half-open connection within roughly two missed beats.

### Connection-state indicator

The status dot shows one of: **connected**, **reconnecting** (with the current attempt count), or **offline** (browser reports no network). The tooltip carries the detail.

### Update-available banner

When the daemon reports a different version than the loaded page, the PWA shows a single tap-to-reload banner. It never reloads the page automatically while the user is typing, has the terminal focused, or has an unsent draft; it reloads on its own only when none of those apply.

### Incremental resync

After a reconnect the PWA makes one `GET /api/sessions` request and sends one subscribe for the open session, rather than reloading every view. Terminal content already on screen is kept.

### Diagnosing with the access log

Each WebSocket open and close is recorded as a `ws_connect` / `ws_disconnect` access-log event. Query them with the `audit_access_query` MCP tool (or the audit access endpoint) filtered on those actions and look at the timing per client:

- A burst of `ws_connect` / `ws_disconnect` pairs spaced about 30-35 s apart points to a NAT idle timeout or stale-detection cycle.
- Pairs spaced at the backoff steps (about 1, 2, 4, 8 s up to 30 s) mean the client is correctly backing off against a network that is down.
- Many connects per second from one client would indicate a regression in the backoff.

---

## Session Reconnect on Daemon Restart

When the daemon restarts, running sessions in tmux survive but in-memory backend state is lost. Datawatch automatically reconnects:

### How it works

1. On session launch, backend connection state is saved to `backend_state.json` in the session's tracking directory
2. On daemon startup, `ReconnectBackends()` scans all running sessions
3. For each session with saved state:
   - **ACP**: probes HTTP server, re-registers state map, re-subscribes to SSE event stream
   - **Ollama**: restores conversation history into conversation manager
   - **OpenWebUI**: restores conversation history into conversation manager
4. If the tmux session no longer exists, the session is marked complete

### What's preserved

| Backend | Preserved across restart |
|---------|------------------------|
| ACP | HTTP port, session ID, SSE subscription |
| Ollama | Host, model, full conversation history |
| OpenWebUI | URL, model, API key, full conversation history |

### Cleanup

`backend_state.json` is automatically deleted when a session completes, fails, or is killed.

---

## Autonomous PRD LLM Configuration

Autonomous PRDs use two separate backends:

| Role | Config | Description |
|------|--------|-------------|
| **Planning** (decompose) | `autonomous.planning_backend` (global) or `decomposition_profile` (per-PRD, takes priority) | Any configured LLM. `opencode`/`claude-code` spawn a full session with codebase tool access; `ollama`/`openwebui` run headless via `/api/ask`. |
| **Execution** (task sessions) | `backend` per-PRD | Spawns interactive coding sessions. Any registered backend: `opencode`, `claude-code`, `goose`, `aider`, etc. |

**Global planning backend** (applies to all PRDs that don't set `decomposition_profile`):

```yaml
autonomous:
  planning_backend: "opencode"    # any configured LLM; opencode/claude-code get full codebase tool access
  planning_effort: "thorough"
```

**Per-PRD settings** — use the PWA Settings modal (Settings button on any PRD in the detail view), the MCP tool, CLI, or REST API. Per-PRD `decomposition_profile` takes priority over the global setting.

MCP:
```
autonomous_prd_set_llm(
  id="<prd-id>",
  backend="opencode",              # task execution (any session backend)
  decomposition_profile="opencode", # planning (overrides global; any LLM)
  effort="thorough"
)
```

CLI:
```bash
datawatch autonomous prd-set-llm <id> --backend opencode --decomposition-profile opencode --effort thorough
```

REST API (also used by Android / iPhone clients):
```bash
curl -sX POST http://localhost:7433/api/autonomous/prds/<id>/set_llm \
  -H "Content-Type: application/json" \
  -d '{"backend":"opencode","decomposition_profile":"opencode","effort":"thorough"}'
```

**Guardrail judge LLM** — `guardrail_backend` / `guardrail_model` choose
the LLM that judges guardrail/verification results, per Automaton
(`set_llm`) or per task (`set_task_llm`). Resolution: task →
Automaton → global `autonomous.verification_backend` / `verification_model`. These two keys are
presence-aware: leave a key out to keep the current value, send `""`
to clear it. Setting only guardrail keys does not change the worker
`backend` / `effort` / `model` / `decomposition_profile`.

```
# MCP
autonomous_prd_set_llm(id="<prd-id>", guardrail_backend="ollama", guardrail_model="qwen3.8:27b")
autonomous_prd_set_task_llm(id="<prd-id>", task_id="<task-id>", guardrail_backend="")   # clear

# CLI
datawatch autonomous prd-set-llm <id> --guardrail-backend ollama --guardrail-model qwen3.8:27b
datawatch autonomous prd-set-task-llm <id> --task <task-id> --guardrail-model ""

# Comm channel
autonomous set-llm <id> guardrail_backend=ollama guardrail_model=qwen3.8:27b
autonomous set-task-llm <id> <task-id> guardrail_backend=ollama

# REST
curl -sX POST http://localhost:7433/api/autonomous/prds/<id>/set_llm \
  -H "Content-Type: application/json" \
  -d '{"guardrail_backend":"ollama","guardrail_model":"qwen3.8:27b"}'
```

**Accepted while** — task-level and story-level LLM override calls are
accepted while the Automaton's status matches one of
`autonomous.task_override_statuses` (default: `needs_review`,
`revisions_asked`, `cancelled`, `blocked` — v9.5.5 adds `blocked`:
repointing the next retry's LLM on a blocked Automaton used to be refused with
`status "blocked" is locked`). Change the set with
`datawatch config set autonomous.task_override_statuses blocked,running`
(or REST `PUT /api/config {"autonomous.task_override_statuses":["blocked","running"]}`,
MCP `autonomous_config_set`, or comm channel
`configure autonomous.task_override_statuses=blocked,running`). Unknown
status names are rejected with a 400 that lists the known statuses; an
empty list restores the 4-item default. Automaton-level overrides (per-Automaton
`set_llm`, per-Automaton guardrail / verification LLM) are not affected by this
setting and remain locked to the 3 pre-Run statuses.

**Verification LLM** — `verification_backend` / `verification_model` /
`verification_effort` choose the LLM that verifies a task's work, per
task, per story or per Automaton. Resolution: task → story → Automaton
→ Automation → global `autonomous.verification_backend` / `verification_model` /
`verification_effort`; a backend set at any per-unit level bypasses the
`autonomous.verification_backends` pool. Verifier sessions are bounded by `autonomous.verification_timeout_seconds` (default 30m). Guardrail judge sessions are bounded by `autonomous.judge_timeout_seconds` (default 30m, counted from the judge's start, not from any wait for a capacity slot). A judge that times out moves to the next backend in `autonomous.judge_policy`, one attempt each; when every backend has timed out, the PRD needs review (`needs_review`, with a `judge-timeout` reason) and an error alert lists every attempt. When the Automaton branch has more than `autonomous.judge_size_threshold_lines` changed lines against its merge base (added plus deleted, lock files left out; default 400, `0` = off), the judge tries `claude-code` first and the other backends follow in their usual order, still one attempt each; in `balance` mode the load still decides and the order only breaks ties. The Automaton decision log names the rule and the line count, and a judge that times out on every backend still holds the Automaton. See `docs/howto/judge-policy.md`. If a `verifier(session):` or `guardrail(session):` failure names `backend=`, `model=` and `timed out after X`, raise that key rather than treating the model's output as a judgment. Same presence rules: omit to
keep, `""` to clear. The task's `verification` result records
`verifier_backend` / `verifier_model` / `verifier_effort` /
`verifier_source` (`task`, `story`, `prd` for the Automaton level,
`automation`, `global` or `pool`). The task itself records
`executor_source` (`task`, `story`, `prd`, `automation` or `global`): the
level that supplied the backend it ran on.

**Automation model defaults.** An Automation can carry its own default
execution, verification and planning LLM for the member Automata it runs.
The Automation level sits between the Automaton and the global default for
the execution backend, model and effort and for the verification LLM; backend
and model come from the same level, and effort resolves independently. The
planning pair (`planning_backend`, `planning_model`) is used only when a member
is decomposed, and unset means the global planning backend. These are runtime
data on each Automation rather than config keys: set them with
`POST /api/automations/{id}/set_llm` and `/set_verification_llm`, the MCP tools
`automation_set_llm` and `automation_set_verification_llm`, `datawatch
automation set-llm` and `set-verification-llm`, or the comm verbs
`automation set-llm` and `automation set-verification-llm`, while the
Automation is draft, active, paused or blocked. Omitted fields are kept,
empty ones are cleared, and `expected_version` is optional. Full reference:
[`api/autonomous.md`](api/autonomous.md).

```
# MCP
autonomous_prd_set_task_verification_llm(id="<prd-id>", task_id="<task-id>", verification_backend="claude-code", verification_model="opus")
autonomous_prd_set_story_verification_llm(id="<prd-id>", story_id="<story-id>", verification_backend="ollama")
autonomous_prd_set_verification_llm(id="<prd-id>", verification_effort="")   # clear

# CLI
datawatch autonomous prd-set-task-verification-llm <id> --task <task-id> --backend claude-code --model opus
datawatch autonomous prd-set-story-verification-llm <id> --story <story-id> --backend ollama
datawatch autonomous prd-set-verification-llm <id> --effort ""

# Comm channel
autonomous set-task-verification-llm <id> <task-id> backend=claude-code model=opus
autonomous set-story-verification-llm <id> <story-id> backend=ollama
autonomous set-verification-llm <id> effort=

# REST
curl -sX POST http://localhost:7433/api/autonomous/prds/<id>/set_task_verification_llm \
  -H "Content-Type: application/json" \
  -d '{"task_id":"<task-id>","verification_backend":"claude-code","verification_model":"opus"}'
```

PWA: the task **Edit**, story **LLM** and Automaton **Set LLM** modals
each have a *Verification LLM* row; the task's verification result
shows **Judged by**.

`verification_script`, `files_generated` and `files_touched` are
filled in by the system and are read-only; view them with
`autonomous_prd_get` / `prd-get` / `autonomous get`.

**Typical setup for opencode-based PRDs:**

1. Configure a global planning backend in `config.yaml` (`autonomous.planning_backend: opencode`)
2. Create the PRD — decompose will spawn an opencode session that reads the codebase before writing the plan
3. Or set per-PRD via PWA Settings modal → "Planning backend" dropdown → pick any configured LLM
4. Decompose → Review → Approve → Run

### Plan requirement coverage gate (autonomous.plan_coverage_check)

Before an Automaton is Approved for its first `Run`, datawatch runs a
**deterministic, no-LLM** plan-requirement coverage check and stores the result
on the PRD as the `plan_coverage` record. It is a function of the spec text and
the plan text — the same inputs always produce the same report — so nothing
about it varies from run to run the way a model judgment would.

**What the report contains.** The settings the PRD was pinned to (gate `mode`
and `plan_coverage_min_ratio` at decompose time), the report (the covered /
missing / weakened requirement counts and each unmatched requirement with its
verdict — `covered`, `missing` or `weak`), any **structural findings** (a
dangling `DependsOn`, a dependency cycle, a plan entry with no file provenance,
a step with no verification reason, a bad tracker-ID name, a write task that
declares no files), and — when one
exists — the operator `ack` `{by, at, note, fingerprint}`.

**How the `unresolved_file` finding is determined.** File-reference
recognition is deliberately conservative so plan prose is not mistaken for
paths:

- **Bare file names resolve by tree search.** A bare name (no directory
  component, e.g. `executor.go`) passes when a file with that base name exists
  *anywhere* under the project tree — it does not need to sit at a specific
  directory.
- **Prose is not a file.** Tokens without a known file extension, dotted code
  identifiers (`json.Unmarshal`, `GuardrailVerdict.Issues`) and
  slash-separated prose lists (`absent/null/empty`, `task/story/PRD`) are not
  treated as file references and never surface as `unresolved_file` findings.
  A slash-separated token counts as a path only if its last segment carries a
  known file extension or its first segment names an existing directory.
- **Untracked runtime files are ignored.** Runtime artifacts such as
  `output.log` (like other `*.log`, `*.pid`, `*.tmp`, `*.out` files) are not
  flagged when they are absent from the tree — they are usually produced at
  run time, not checked in.
- **Declared outputs count as existing.** A file listed in the `FilesGenerated` of the same task, or of any task in the referencing story or an
  earlier story, satisfies the reference — the plan may name a file a task is
  about to create. (A file a *later* task generates still counts as a miss at
  the earlier reference point.)
- **Real gaps are still reported.** A file path under an existing directory
that does not exist in the tree and is not generated by any same-or-earlier
task or story is still flagged as `unresolved_file`, so genuinely broken file
references keep failing the report.

**The `write_task_without_files` finding.** A task whose title starts with a
write verb — `Write`, `Add`, `Update`, `Create`, `Fix`, `Bump`, `Implement`,
`Document`, `Register`, `Wire`, `Make`, `Move`, `Replace`, `Rename`, `Extend` or
`Remove` — is expected to change files, so its `files` list must say which. The
finding is raised when all of these hold:

- the first word of the title is one of those write verbs;
- the task's `files` list is empty (the generated-files list does not count; it
  is verification output, not a declaration of what the task changes);
- the first non-empty line of the task spec does not begin with `READ-ONLY:`;
- the task is not a smoke or final-gate task, and its title does not contain
  one of the gate phrases `final gate`, `stability gate`, `suite run`, `eval`,
  `install` or `investigation` (whole words, matched on the title only).

To fix it, either add the files the task will change to its `files` list, or make
the first line of the spec `READ-ONLY:` when the task really changes nothing. Put
a reason after the marker; the check looks only at the marker, so the reason is
for the reader and is not enforced. In `block` mode the finding blocks approval like any other
finding, and the operator clears it with the existing acknowledgement path
below, with a note. In `warn` mode it is stored and shown while approval
proceeds, and `off` records it only. See
[autonomous planning](howto/autonomous-planning.md#declaring-files-for-write-tasks)
for the planner side.

**A report fails in two ways**, and both stop approval in `block` mode:
the covered/extractable requirement ratio is below `plan_coverage_min_ratio`, or
any structural finding is present (findings fail the report regardless of ratio).

**The three modes** (`autonomous.plan_coverage_check`, default **block**):

| Mode | Behavior on a failing report |
|---|---|
| `block` (default) | Approve is **blocked** until the operator acknowledges that exact failing surface. |
| `warn` | The failing report is stored and shown on the PRD; **approval proceeds**. |
| `off` | No gating; the report is still recorded for visibility. |

**Reading the stored report on a `needs_review` PRD.**

```sh
# CLI — pretty-print the stored plan_coverage record
# (mode, min_ratio, report, findings, ack)
datawatch autonomous prd-coverage <id>

# REST — full PRD detail carries the plan_coverage field
curl -sX GET http://localhost:7433/api/autonomous/prds/<id>?detail=full

# MCP — the full detail exposes the plan_coverage record
autonomous_prd_get(id="<prd-id>", level="full")
```

In the **PWA**, open the PRD; the coverage panel shows the status line —
"Plan covers the spec requirements" (pass), "Plan coverage below threshold —
warning mode" (`warn`), or "Plan coverage below threshold — blocked mode"
(`block`) — next to the covered/missing requirement list and any findings.

**warn vs block in practice.** In `warn` mode you see the same failing report
in the panel, but Approve is enabled and the PRD moves on — the coverage gap is
informational. In `block` mode the same report locks Approve: the panel shows
the failing surface and an **Acknowledge** control instead of letting Approve
through. `off` behaves like `warn` but the report is purely for the record.

**Acknowledging a failing report (the `block`-mode override).** To unblock
Approve without fixing the plan, an operator acknowledges the *exact* failing
surface. Every surface requires a **non-empty note** (the justification) and an
actor identity. The ack is **bound to the stored report's fingerprint** — a
stable hash of the unmatched requirements + findings at the time it was recorded —
so it cannot silently cover a *different* failure surface: if the plan changes
after an ack, the fingerprint no longer matches and a fresh ack is required.

```sh
# CLI — note is required (non-empty); actor defaults to "operator"
datawatch autonomous prd-ack-coverage <id> --actor alice --note "step 3 covers this via the shared helper, acknowledged for this release"

# MCP — id + non-empty note
autonomous_prd_plan_coverage_acknowledge(id="<prd-id>", note="...")
```

```sh
# REST POST
# -H "Authorization: Bearer <token>" -H "Content-Type: application/json"
curl -sX POST http://localhost:7433/api/autonomous/prds/<id>/plan_coverage/acknowledge \
  -d '{"actor":"alice","note":"acknowledged for this release"}'
```

In the **PWA** the PRD coverage panel exposes an **Acknowledge** control: type
the note, submit, and the panel switches to "Acknowledged: <actor> — <note>".
Approval is then allowed.

**Why this is deterministic (no LLM).** The coverage check and the structural
findings are pure string-level functions of the spec and the plan: no model
call, no filesystem access, no clock in the verdict. That means the report on a
`needs_review` PRD is exactly reproducible — reading it again, or re-running the
decompose from the same spec + plan, gives the same covered/missing/weakened
counts, the same findings, and the same fingerprint. The only judgment in the
flow is the operator's own acknowledgement, which is why that step requires an
actor and a non-empty note and is pinned to the failing surface.

**Changing the settings.** Both keys are hot-applied (no restart) and reachable
on every parity surface that carries `autonomous.*` config: YAML `config.yaml`,
REST `PUT /api/config`, REST `GET/PUT /api/autonomous/config`, MCP
`autonomous_config_set`, CLI `datawatch config set autonomous.plan_coverage_check=…`
/ `autonomous.plan_coverage_min_ratio=…`, comm `configure
autonomous.plan_coverage_check=…`, and the PWA Settings fields. A PRD is pinned
**at decompose time**: a mode/ratio changed later does not retroactively
re-evaluate an already-`needs_review` PRD's stored report, and does not undo an
ack — the ack is bound to the stored report's fingerprint, not to the live
settings.

### Session idle timeout (autonomous.session_idle_timeout_seconds)

This bounds how long one autonomous worker or verifier session may sit
idle before datawatch gives up on it. The default is 900 seconds (15
minutes).

- **What it watches:** a worker task session or a verifier one-shot
  session whose state is `waiting_input` or `idle` and that has no live
  child process.
- **When it fires:** the session has stayed in that state with no live
  child process for longer than the bound.
- **What you see:**
  - a datawatch alert for the session;
  - the task failure reason `session idle without completion`, naming
    the session id, backend and model;
  - the task is retried through the existing `auto_fix_retries` budget.
    Once that budget is spent, the task stays failed like any other
    failed task.
- **Setting it:** `autonomous.session_idle_timeout_seconds` in
  `config.yaml`, or through the web settings card, the REST
  `autonomous.*` config endpoints, or the MCP `autonomous_config_set`
  tool. A value of `0` means "use the default 900".

```yaml
autonomous:
  session_idle_timeout_seconds: 900
```

### Descendant-only stall bound (autonomous.descendant_stall_seconds)

This bounds how long a pi backend worker may show no sign of life while
its only liveness is a work child it started. The default is 2700 seconds
(45 minutes). It only applies to one-shot `pi` workers — a `pi` run writes
nothing to its log until it finishes, so the usual log-based stall check
does not reach it, and the exit-silence check does not cover the case
where the runner has exited but a child it started (a build, a test) is
still alive.

- **What it watches:** a running worker attempt for which the descendant
  process tree is readable, where a live descendant of the work is present,
  and the last write to the session log, and the newest file in the
  worker's working directory, both older than the bound. Any newer
  evidence resets the clock.
- **When it fires:** the live descendant count stays above zero while the
  most recent log write and workdir file are both older than the bound.
- **What happens when it fires:**
  - the attempt's session is ended by the descendant-only stall detector;
  - the work descendants that attempt was waiting for are cleaned up:
    they get a 5-second grace period, then a real kernel signal, and only
    where the process still carries the identity it was recorded with —
    a recycled process id is never signalled. Which ones needed the
    escalation, and which stayed alive, are recorded on the task along
    with the pane tail;
  - a `Task worker descendant-only stall` alert fires naming the PRD,
    the task and the measured reason, and the attempt ends as an
    infrastructure failure;
  - the task is retried through the existing `auto_fix_retries` budget,
    and stays failed like any other failed task once that budget is
    spent.
- **Setting it:** `autonomous.descendant_stall_seconds` in `config.yaml`,
or via any other settings surface (settings card, CLI `config set`,
comm channel `configure`, REST config `PUT`, MCP `autonomous_config_set`).
A value of `0` or below means "use the default 2700" — the setting
can never be accidentally disabled.

```yaml
autonomous:
  descendant_stall_seconds: 2700
```

### Worker idle nudge (autonomous.worker_idle_nudge_seconds, autonomous.worker_idle_nudge_stall_seconds)

A claude-code worker that finishes a turn without printing its completion
marker gets one nudge before the session idle timeout applies.

- **`autonomous.worker_idle_nudge_seconds`** (default 180): how long the
  worker may sit idle with no output before it is nudged once. Only work
  children block a nudge: a process the worker's runner started, such as a
  build or a test run. The runner itself, and the session's own channel and
  MCP servers, do not block it, so an idle worker that has only its own
  servers running is still nudged.
- **Adopted sessions:** a worker adopted after a daemon restart is nudged on
  the same schedule and under the same work-child rule as a fresh one.
- **`autonomous.worker_idle_nudge_stall_seconds`** (default 600): how long,
  counted from the nudge, the worker may stay silent before it is treated as
  stalled and retried through the existing `auto_fix_retries` budget.
- **Visibility:** each nudge is recorded on the task (`nudge_events`,
  `nudge_count`). Each `nudge_events` entry carries `session_id`, the session
  the nudge was typed into, and `outcome`: `sent` when it is typed, then
  `answered` (the worker printed its completion marker or fresh output
  followed) or `stalled` (the worker stayed silent past the stall window).
  `autonomous_status` reports `nudges_total`, `nudge_tasks` and
  `nudges_by_backend`.
- **Wait-start line:** when a claude-code worker finishes its turn, the
  journal gets one `post-turn wait start` line with the session, the task,
  the backend family, `nudge applies=yes|no|pending` (`pending` while the
  session is not yet visible), the nudge and stall windows, the session idle
  limit and `persisted nudge=true|false`. Example:
  `[autonomous] post-turn wait start: session <session> task <automaton>/<task>: backend family claude-code, nudge applies=yes, nudge window 3m0s, stall window 10m0s, session idle limit 15m0s, persisted nudge=false`.
- **Nudge and outcome lines:** `idle worker <session> task <automaton>/<task>: nudge N sent after D idle`
  when the nudge is typed, then `nudge answered by completion marker`, `nudge answered by new output`
  or `nudge answered by session completion`. A stall logs `still silent for D after nudge; ending session for retry`.
- **Idle-detector end reason:** when the session idle detector ends a worker,
  the end reason finishes with the nudge history of that session: `no nudge sent`,
  or `nudge N sent at <time>, outcome <sent|answered|stalled>`. An event with no
  outcome yet reads as `sent`.
- **Nudge window not shorter than the idle limit:** while the session idle
  detector is on, if `worker_idle_nudge_seconds` is not smaller than the
  session idle limit, each wait logs one `post-turn wait warning` line at its start. The idle
  detector can then end the worker before the nudge is sent. Lower the nudge
  window, or raise the idle limit, to clear it.
- **Effective values on the config read:** `GET /api/config` (the `autonomous`
  section, also MCP `get_config` with `section: autonomous`) returns two
  read-only keys, `worker_idle_nudge_effective_seconds` and
  `worker_idle_nudge_stall_effective_seconds`, the windows the daemon applies.
  Unset or non-positive settings read as the defaults (180 and 600).
- **Setting them:** `config.yaml`, the web settings card, `PUT /api/config`,
  MCP `autonomous_config_set`, or the `configure` chat command. Hot-applied;
  `0` means "use the default".

```yaml
autonomous:
  worker_idle_nudge_seconds: 180
  worker_idle_nudge_stall_seconds: 600
```

See [howto/autonomous-recovery](howto/autonomous-recovery.md).

---

## Autonomous Plan Review before approval (optional, off by default)

An optional second opinion sits between decompose and your approval:
a one-shot, non-interactive LLM judge looks at the Automaton's plan (spec vs.
decomposed stories/tasks) and records a verdict on the Automaton. **This is
off by default** — with the keys unset, datawatch behaves exactly as
before: no LLM verdict call, no effect on approval.

**Keys** (all under `autonomous:`):

| Key | Default | Meaning |
|-----|---------|---------|
| `plan_review_backend` | `""` (off) | LLM that does the review. Any configured LLM, same rules as `planning_backend`. |
| `plan_review_model` | `""` | Model for the reviewer; empty = the backend's own default. |
| `plan_review_mode` | `""` (= off) | `off` / `warn` / `block`. Unknown values are a config error — a typo cannot silently disable a gate. |
| `smoke_verification_backend` | `""` | LLM that verifies the plan's own smoke task (see below). Empty = no split. |
| `smoke_verification_model` | `""` | Model for that verifier; empty = backend default. |

Set them any surface: `~/.datawatch/config.yaml`, REST `PUT /api/config`
(dot-path keys), MCP `autonomous_config_set`, comm channel
`configure autonomous.plan_review_mode=warn`, CLI
`datawatch config set autonomous.plan_review_mode warn`, or the PWA
Settings (Autonomous section).

### Modes

- **off** (default): the review does not run; nothing is recorded; approval is unaffected.
- **warn**: the review runs after decompose; the verdict (`ok`, findings,
  summary, cost/tokens) is recorded on the Automaton and shown in the PWA, REST
  and MCP; approval is always still allowed — the verdict is advisory.
- **block**: a verdict with `ok: false` refuses `approve`
  (`POST /api/autonomous/prds/{id}/approve`) with the finding categories
  and the reviewer's summary. Approval stays refused until the verdict is
  **replaced** — re-run the review until it passes, or re-decompose. There
  is no force-approve bypass: the point of block mode is that the gate
  works.

### What the reviewer sees and judges

The reviewer gets a **bounded projection** of the plan — the spec plus
story/task titles, task specs, dependencies, kinds and verification
scripts, each field length-capped (it never sees the whole repository).
It judges: faithfulness to the spec, completeness (nothing important
missing), task size/verifiability, dependency ordering, acceptance
criteria that can actually be checked, and in/out-of-scope work. It
reports findings only — it does not re-plan. Each finding carries a
bounded category (`missing`, `weakened`, `too_large`,
`wrong_dependency`, `fragile_verification`, `out_of_scope`) and one
actionable sentence; `high` severity is the approval-blocking grade.
The Automaton content goes to the reviewer inside a neutralized data block so a
spec string cannot inject instructions into the prompt.

### Reviewer outage — record only, never block

If the reviewer call fails or returns an unparseable verdict, the
recorded verdict carries `reviewer_failed: true` (the PWA shows an error-
styled "reviewer failed — outage, not a plan failure" panel). In every
mode, an outage verdict **never blocks approval**: the reviewer's
availability must not wedge your approval gate. Re-run the review once
the backend is back.

### Re-running (works in every mode)

- **PWA**: the re-run button in the Automaton detail's plan-review panel.
- **REST**: `POST /api/autonomous/prds/{id}/plan-review` (body
  `{actor?, note?}`; the response is the updated Automaton with the new
  verdict).
- **MCP**: `autonomous_prd_plan_review`.
- **Comm channel**: `autonomous plan-review <id> [note]`.
- **CLI**: `datawatch autonomous prd-plan-review <id> [note]` (alias `plan-review`).

Only the **latest verdict counts** — a re-run or a re-decompose *replaces*
the previous one, so a blocked Automaton unblocks itself as soon as one review
run passes. In mode `off` a re-run is a no-op that returns the Automaton as
stored. Each verdict also appends a `plan_review` decision row to the Automaton's
decision log (the PWA decision list shows it with the reviewer's
summary) and emits an SSE update on the Automaton channel.

### Reviewer cost and tokens

The verdict records `est_cost_usd`, `tokens_in` / `tokens_out` when the
backend reports spend (one-shot backends usually don't — the fields are
0 in that case); the automation budget policy reads these off the verdict
for Automaton-level cost accounting. The PWA shows the line
"cost: $… · in/out tokens" only when present.

### Smoke / final-gate verifier split

When the plan contains a task **that is itself the smoke verification**,
decompose tags it `kind: "smoke"` (the terminal verification task is
`kind: "final_gate"` and runs the script itself). The judge for that smoke
task can be a *different* LLM — the verifier — set via
`autonomous.smoke_verification_backend` / `..._model`. These two keys are
the **default surface only**: an explicit per-task verifier
(`set-task-verification-llm`, including a deliberate `""` clear) always
wins and survives re-decompose (the bulk apply never clobbers a filled
slot). Titles like "smokescreen" are not smoke tasks — matching is
word-boundary (`smoke`, `final gate`, `final-gate`) plus the explicit
`kind` tag. The PWA shows a kind chip + the resolved verifier LLM on
those tasks.

### Mobile parity

Android/iOS equivalents (verdict panel, re-run button, long
approve-rejection toast, the three settings fields, task kind chips) are
tracked in [dmz006/datawatch-app#266](https://github.com/dmz006/datawatch-app/issues/266).


### Session start confirm timeout (autonomous.session_start_confirm_seconds)

This bounds how long a claude-code autonomous session may sit in
`waiting_input` with no sign of work after it has started. The default
is 120 seconds (2 minutes). It exists because a session whose process is
alive but that never actually begins working looks healthy to the plain
idle detector and would otherwise sit there indefinitely.

A sign of work is any of: an assistant turn, a tool call in progress or
finished (including a spinner), a process the worker started, a change in
the pane, or a channel event after the task was delivered. Assistant output
is not the only proof. A worker in one long silent tool call is not ended.

- **What it watches:** a claude-code task session that has started (the
  process is running) but shows no sign of work — it is still in
  `waiting_input` instead of getting to work.
- **When it fires:** the session stayed in that state with no sign of work
  for longer than the bound. The clock restarts at each sign of work, so a
  worker whose tool line or spinner is still on screen is not ended.
- **Launched and adopted workers:** one clock serves both. It is measured from
  the last evidence of life (its last update, channel event or log write), not
  from the moment the daemon adopted the worker. A worker the daemon launched
  has no evidence of life until its first sign of work, so its clock starts at
  the first check that finds it silent.
- **While a tool call runs:** the session shows as `running` (the busy
  probe), not `waiting_input`, while the pane shows the agent working.
- **What you see:**
  - a datawatch alert naming the session, with the last 20 lines of the
    session transcript included so you can see why it never got going;
  - the task failure reason `session never started`.
- **Start-failure budget:** up to 2 start failures are retried
  automatically before the task stays failed and is left for an operator.
  This budget is separate from `auto_fix_retries`, which covers retries
  after the verifier rejects finished work.
- **Setting it:** `autonomous.session_start_confirm_seconds` in
  `config.yaml`, or through the web settings card, the REST `autonomous.*`
  config endpoints, the MCP `autonomous_config_set` tool, the
  `datawatch config set` CLI, or a comm channel
  (`configure autonomous.session_start_confirm_seconds=<seconds>`). A value
  of `0` means "use the default 120".

```yaml
autonomous:
  session_start_confirm_seconds: 120
```

---

## Tag browser settings (autonomous.tags)

The Automata list has two views. The **attention view** shows the Automata that
need an operator now; the **tag browser** shows the tag tree, with a count per
tag. The `autonomous.tags` block sets which view opens first, which statuses
count as needing attention, how selected tags combine, and how the shared tag
registry syncs between federated daemons.

| Key | Default | What it does |
|---|---|---|
| `autonomous.tags.default_view` | `auto` | `auto` opens the attention view when at least one Automaton needs attention, otherwise the tag browser. `attention` always opens the attention view. `tags` always opens the tag browser. |
| `autonomous.tags.attention_statuses` | `[blocked, needs_review, paused]` | The Automaton statuses that place an Automaton in the attention group. Valid statuses are `draft`, `planning`, `needs_review`, `approved`, `revisions_asked`, `running`, `completed`, `rejected`, `cancelled`, `archived`, `blocked`, `failed` and `paused`. Any other entry is refused; an empty list takes the default statuses. The default includes `paused`, so a paused Automaton is in the attention group unless you remove it from the list. An Automaton with a task held for operator review is in the attention group whatever the list says. |
| `autonomous.tags.default_match_mode` | `any` | How several selected tags combine. `any` widens the list to Automata carrying at least one selected tag. `all` narrows it to Automata carrying every selected tag. |
| `autonomous.tags.direct_counts_visible` | `false` | When `true`, each tag shows its direct count (Automata tagged with that tag itself) as a tooltip beside its subtree count. |
| `autonomous.tags.registry_keeper` | `""` | The federation peer name of the daemon that keeps the shared tag registry. Empty means standalone: this daemon keeps its own tags and nothing syncs. Set it to this daemon's own name to keep the registry, or to another federation peer's name to follow that keeper. Both take effect at once, with no restart. |
| `autonomous.tags.registry_sync_seconds` | `30` | How often, in whole seconds, the shared tag registry syncs with the keeper. Minimum 5. |

**Access methods**

| Method | How |
|--------|-----|
| **YAML** | Edit `~/.datawatch/config.yaml` → `autonomous:` → `tags:` (the six keys above) |
| **CLI** | `datawatch config set autonomous.tags.<name> <value>`; read one with `datawatch config get autonomous.tags.<name>` |
| **Web UI** | Settings → General → Automata → "Autonomous Automata planning" card → **Tag browser** group |
| **REST API** | `PUT /api/config` with `{"autonomous.tags.<name>": <value>}`; read with `GET /api/config` (the values sit under `autonomous.tags`) |
| **Comm channel** | `configure autonomous.tags.<name>=<value>` |
| **MCP** | `config_set` with key `autonomous.tags.<name>` (requires `mcp.allow_self_config=true`) |

Every key is settable on every surface above. An invalid value is refused on
every surface with the same message, for example
`autonomous.tags.default_match_mode: must be any or all`, and nothing is written.

```yaml
autonomous:
  tags:
    default_view: auto
    attention_statuses: [blocked, needs_review, paused]
    default_match_mode: any
    direct_counts_visible: false
    registry_keeper: ""
    registry_sync_seconds: 30
```

**Shared registry state on disk.** A peer keeps the last copy of the keeper's
tags in `tag-registry.json` in the autonomous data directory, so a restart does
not lose the copy a stale peer is showing. The keeper's own tags stay in the
normal `tags.jsonl` store. The registry needs the keeper to be a federation peer
of the daemon that follows it; a keeper name that is not a federation peer leaves
the follower stale. See [howto/automata-tag-registry.md](howto/automata-tag-registry.md)
for the operator steps and [flow/tag-registry-sync-flow.md](flow/tag-registry-sync-flow.md)
for the sync paths.

---

## Quality gate (test baseline + regression check)

An Automaton can carry a quality gate: a test command that runs once as a
baseline before the first task, then again after each task. The gate compares
the two runs by failing test name and reports a regression, an improvement,
or no change. Set it per Automaton (`quality_gates` on the Automaton, or
`autonomous.default_quality_gates` for every Automaton that sets none).

**Where the command runs.** `test_command` is ONE shell string, run through
`sh -c` in the Automaton's project directory (the task's own directory when a
task sets one). Compound commands work, for example
`go test ./... && node --test internal/server/web/*.test.js`. The same
applies to `pipeline.quality_gates.test_command`. `timeout` is in seconds;
`0` means no limit.

### Valid and invalid runs

Every run is classified before it is trusted. A run is **invalid** when any
of these is true:

- **Could not start** — the project directory is missing, there is no `sh`,
  or the command string is empty.
- **Timed out** — the command was killed at the configured timeout.
- **Non-zero exit, no named failing test** — the run ended with an error but
  did not name a single failing test. That is a build, setup or environment
  problem (a missing generated file, a compile error), not a test failure.
- **No tests found** — the output held no recognizable tests.

A run that finished, found tests and names some failing ones is **valid**,
even when those failures were already there before the work started.

### What an invalid baseline does

The baseline is validated at two moments: when the gate is set (creating the
Automaton with a gate, or changing it later) and when the Automaton starts.
An invalid baseline is recorded on the Automaton as
`quality_gate_status: "invalid"` with the plain-language reason in
`quality_gate_invalid_reason`, plus a `quality_gate_invalid` entry in the
decision log, and the daemon logs a warning. Setting a usable command later
clears the stale verdict.

**An invalid gate never blocks.** From then on the gate only warns: it does
not fail a task, does not trigger an auto-fix retry, and is not counted as a
pass or a regression, whatever `block_on_regression` or `gate_disagreement`
say. An unusable run is always reported as an *invalid gate*, never as a
regression.

The web UI shows an amber "Quality gate invalid" badge with the reason on the
Automaton card and the Automata list row, and the Monitor's quality-gate card
gets an invalid line with the count.

### Regression: compared by test name

The gate compares the set of failing test *names*, not counts. A test that
fails now and did not fail in the baseline is a regression even when the
total number of failures is unchanged. Tests that failed in the baseline and
pass now are reported as `IMPROVED`.

A regression is **confirmed by one re-run** before it is reported:

- For a plain `go test` command, only the new failing tests are re-run, in
  their packages.
- For any other command (compound commands, `node --test`, `pytest`), the
  whole command is re-run once.

Only the tests that fail again, by name, count as the regression and are
listed in `failing_tests`. If none fails again the result is `STABLE` (a
flaky failure). If the re-run itself is unusable, the gate is invalid, not a
regression.

### gate_disagreement (default `warn`)

When the task's own verifier **passed** but the gate found a confirmed
regression from a **valid** baseline, `autonomous.gate_disagreement` decides
what happens:

| Value | Outcome |
|---|---|
| `warn` (default) | The task completes. The disagreement is recorded on the task (`gate_disagreement`) and as a `gate_disagreement` decision-log entry, with a logged warning. |
| `block` | The task fails with a `gate_disagreement: …` error that names the new failing tests. It is not auto-fix retried. |

Only `warn` and `block` are accepted; any other value is rejected when it is
written (REST answers `400`). The policy applies to a valid-baseline
regression only. For an invalid gate the task still records
`gate_disagreement` with `gate_invalid: true`, and nothing is blocked under
either value. With `warn`, an Automaton whose `block_on_regression` is on
still fails the task and retries it as before.

### Setting gate_disagreement, per surface

| Surface | How |
|---|---|
| REST | `PUT /api/config` with `{"autonomous.gate_disagreement": "block"}`; read it back from `GET /api/config` (`autonomous.gate_disagreement`). `PUT /api/autonomous/config` accepts `gate_disagreement`. |
| MCP | `autonomous_config_set` with parameter `gate_disagreement` (`autonomous_config_get` returns it) |
| CLI | `datawatch autonomous config-set '{"gate_disagreement":"block"}'` (`config-get` shows it) |
| Comm | `configure autonomous.gate_disagreement=block` |
| PWA | Settings → General → Autonomous: "When the gate disagrees with a passing verifier" (warn / block) |
| YAML | `autonomous.gate_disagreement: warn` in `~/.datawatch/config.yaml` |

On the Automaton JSON (`GET /api/autonomous/prds/{id}`) look at
`quality_gate_status` and `quality_gate_invalid_reason`; on a task, at
`gate_disagreement` (`verifier_ok`, `gate_regression`, `gate_invalid`,
`policy`, `summary`) and `quality_gate_result`.

### Gate scope (`autonomous.default_quality_gates.scope`, `quality_gates.scope`)

Gate scope steers how much of the suite a gate task runs, independently of
the disagreement policy above. Values: `full` (the built-in default — the
configured command runs unchanged), `touched-plus-smoke` (the touched
packages plus their one-level importers, plus the smoke set) and `touched`
(touched packages plus one-level importers). An Automaton's unset scope
resolves to the global default, then to `full`; the resolved value and its
source (`automaton` / `global` / `default`) are recorded on the run as the
`scope` and `scope_source` fields of `quality_gate_result.scope_plan`
(see `docs/howto/quality-gate-scope.md`).

**Touched packages** are every package with a changed file; **importers**
are packages importing a touched package directly (second level is not
walked). The **smoke set** is `autonomous.default_quality_gates.smoke_packages`
(or the per-Automaton `quality_gates.smoke_packages`), falling back to the
built-in list in `internal/config` (aliased as
`internal/pipeline` for the planner): `./internal/autonomous/...`,
`./internal/config`, `./internal/pipeline`, `./internal/server/...`,
`./internal/mcp/...`, `./internal/observer/...`, `./internal/audit`,
`./internal/metrics`. The gate rewrites `go test <flags> ./...` to the
scoped package list; anything it cannot compute, and any command that is
not a rewritable `go test ... ./...`, falls back to the full command for
that run and is recorded in `scope_plan` (`full_reason` /
`fallback_reason`). Module-level files (`go.mod`, `go.sum`, `go.work`,
`go.work.sum`), changed files outside any Go package, and a mapping failure
all force the full run. `go vet` runs over the same package set and its
new findings count as regressions; pre-existing vet findings, and failures
in packages that did not run, do not.

Guarantees that hold at every scope:

- **The final gate task always runs the full suite** — the last
  non-terminal task of an Automaton (or any task tagged as the final gate),
  whatever the resolved scope is.
- **Failures that cannot be attributed are not reported as regressions** —
  the baseline and current run are both restricted to the same package set,
  so a pre-existing failure in a run-omitted package is not a new
  failing test. An invalid run (could not classify: startup failure,
  timeout, no named tests) is warn-only at every scope, exactly as it is
  at `full`.

**Where set and read (scope + smoke_packages):**

- **YAML** — `autonomous.default_quality_gates.scope` (string) and
  `autonomous.default_quality_gates.smoke_packages` (list of Go package
  patterns); load with an unrecognized scope value is refused. See
  `config.reference.yaml`.
- **REST** — `PUT /api/config` (both keys; `smoke_packages` accepts a list
  or a comma-separated string), read back from `GET /api/config`. Per
  Automaton: `POST /api/autonomous/prds/{id}/set_quality_gates` also takes
  `scope` and `smoke_packages` (anything other than the three values is a
  400; empty inherits the global/default), read back from
  `GET /api/autonomous/prds/{id}` (`quality_gates.{scope,smoke_packages}`).
  Every task that had a gate run carries
  `quality_gate_result.scope_plan` on `GET /api/autonomous/prds/{id}` and
  in every task list; see `docs/api-mcp-mapping.md` for the field list.
- **MCP** — `autonomous_config_set` takes `quality_gates_scope` and
  `quality_gates_smoke_packages` (both optional, forwarded only when
  supplied; unrecognized scope values are rejected); `autonomous_prd_set_quality_gates`
  takes `scope` and `smoke_packages`. See `docs/mcp.md`.
- **CLI** — `datawatch autonomous config-set
  '{"default_quality_gates":{"scope":"touched-plus-smoke","smoke_packages":["internal/pipeline"]}}'`;
  `datawatch autonomous config-get` shows both keys; `--help` names the
  example. There is no per-Automaton CLI flag — use REST or MCP.
- **comm** — `configure autonomous.default_quality_gates.scope=<value>` and
  `configure autonomous.default_quality_gates.smoke_packages=a/b,c/d` (both
  keys are listed in `configure help` / `configure list`); per-Automaton
  override with `autonomous set-quality-gates <id> [scope=...] [smoke_packages=a,b]`.
- **PWA** — Settings → General → the autonomous quality-gate card: a
  quality-gate-scope selector (empty = inherit, i.e. `full`) and a
  smoke-packages box (comma-separated, saved as a list); keys
  `autonomous.default_quality_gates.scope` /
  `autonomous.default_quality_gates.smoke_packages` (locale keys
  `settings_gate_scope`, `settings_gate_scope_hint`). Each Automaton's
  tasks render `scope_plan` under the task (locale keys
  `prd_task_gate_scope`, `prd_task_gate_scope_full`). There is no
  per-Automaton scope editor in the PWA — set it via REST, MCP or comm.

**In the datawatch source tree** the default stays `full` — the scoped
scopes are implemented, tested and surfaced on every surface above, but the
new default is not flipped until the operator confirms the suggested smoke
set in `docs/plans/2026-10-23-quality-gate-scope.md`.

**Mobile parity:** the mobile app does not render the new fields yet and
must not break on them; the brief is
`docs/plans/2026-10-23-datawatch-app-gate-scope-note.md`. See
`docs/parity-status.md` (the gate-scope row) for the tracked state.

Full walkthrough (scopes, override order, `scope_plan` fields, fallback
reasons, confirming the smoke set): `docs/howto/quality-gate-scope.md`.

### Pi worker observability (progress, heartbeat, loop detection)

Pi-based autonomous workers (interactive `pi` and the `pi-prompt` one-shot
backend) are watched through the worker's own session log. Pi writes that
log natively as a stream of JSON lines (its session JSON mode); datawatch
only ever *reads* it — it never parses the pane.

**Progress.** While a pi task is running, the wait loop re-reads the log
about every 30 seconds (`cmd/datawatch/progress_refresh.go` over
`internal/pilog`) and turns it into a progress snapshot: last event time,
assistant-turn count, per-tool call counts, the last tool call and its
arguments, the last text/thinking summary (truncated to 200 characters),
and token counts. That snapshot is what you see in the PWA PRD task
progress block and in the task objects of the REST/MCP PRD views (the
`progress` and `looping_warning` fields, present only on the task that
is running right now). Nothing here requires operator action; it exists
so a running pi worker can be told "working" from "stuck" without
opening the pane.

**Heartbeat.** Liveness is judged from the worker's own activity: the
log still being written (or its newest event being recent), a live runner
child process, a busy backend, or a recently changed file in the task's
work directory. When *none* of those signals of life is present for
longer than `autonomous.stale_task_seconds` (falling back to
`session.stale_timeout_seconds`), the worker is stopped as an
infrastructure failure (`worker stalled: no output, no children, no file
changes for N seconds (stale_task_seconds=...)`) and retried through its
existing `auto_fix_retries` budget — even though the pane may still show
output that looks like it worked. One-shot `pi-prompt` runs skip the
log-mtime part of this check by design: `pi --print` writes its log only
when it finishes, so the mtime gate would report false stalls; the other
signals still apply. `autonomous.session_idle_timeout_seconds` (above)
covers a different case — a session that went idle instead of dying.

**Loop detection and rescue.** A worker that repeats one action over and
over is caught at two levels, both from the same log:

1. **Streak warning (always on).** The same tool call with identical
   arguments seen unchanged across three consecutive ~30 s progress
   refreshes (about a minute of spinning) adds a `looping suspected`
   warning to the task. Arguments that evolve (a growing offset, new
   content) break the streak, so only genuine repetition trips it.
2. **Live detection (tunable).** The detector looks at the worker's last
   `autonomous.loop_detect_turns` assistant turns (default 3) and fires
   when that window shows **announcing without acting** (a turn claims a
   next action but calls no tool) or **repeating** the identical tool call
   with identical arguments. A finding triggers the loop rescue, once per
   loop: the finding is recorded, a `task.looping` event is emitted, the
   task carries a `looping` status, and — for an interactive pi worker — a
   corrective message is sent into the session; for a one-shot `pi-prompt`
   worker the run is stopped and retried with the loop hint attached.

**Settings.**

- `autonomous.loop_detect_turns` — the window N above. `0` turns loop
  detection **off** (the streak warning and the heartbeat keep working);
  positive N compares the last N turns; negative values are rejected at
  every write edge (REST, MCP, CLI, comm channel). Set it in `config.yaml`,
  through web settings (Settings → Autonomous card, "Worker loop
  detection window"), or `datawatch config set autonomous.loop_detect_turns 5`.
- `pi.thinking_level` — pi's own `--thinking` level, passed on every pi
  worker launch (pi-prompt one-shots and pi sessions). `""` (default) =
  don't pass the flag and use pi's model default; allowed levels are
  `off`, `minimal`, `low`, `medium`, `high`. Per-task, per-story and
  per-PRD thinking-LLM overrides ("Set LLM" / thinking-LLM rows) take
  precedence over this global default. Set it in `config.yaml` (`pi:`
  section) or web settings (Settings → *Pi (worker backend)* card).

```yaml
pi:
  thinking_level: ""        # off|minimal|low|medium|high; empty = pi model default
autonomous:
  loop_detect_turns: 3     # 0 = loop detection off; default 3
```

---

### Task attempt limits (autonomous.task_max_attempt_seconds, autonomous.task_no_progress_seconds)

Two optional bounds on a single autonomous task attempt. Both are off by
default (`0`); when enabled and exceeded, the worker is stopped and the
attempt fails, then the normal retry budget (`auto_fix_retries` / the PRD's
retry policy) handles a retry exactly like any other failed attempt.

- **`task_max_attempt_seconds`** — a hard wall-clock cap on one attempt,
  regardless of activity. If the attempt is still running when it expires,
  it is stopped with a max-attempt failure reason. Recommended: `5400`
  (90 minutes).
- **`task_no_progress_seconds`** — how long an attempt may go *without
  progress* before it is stopped. It fails with the text
  `no progress for N seconds: no output, no file changes, no commits`. A
  change in any of the following counts as progress:
  - terminal output growth;
  - a changed tracked or untracked file in the work directory (harness
    scratch files are ignored);
  - a new commit;
  - a live child process using CPU.

  Recommended: `1800` (30 minutes).

**Work is never discarded.** Before the worker is stopped, any
uncommitted work is saved to a ref (the same mechanism as
`preserve_attempt_work`), so the next retry can build on it instead of
starting from scratch.

**Setting them:** any of the usual autonomous config surfaces —
`config.yaml`, the web settings card, the REST `autonomous.*` config
endpoints, the CLI, the comm channel, or the MCP `autonomous_config_set`
tool.

```yaml
autonomous:
  task_max_attempt_seconds: 5400   # wall-clock cap on one attempt; 0 = off
  task_no_progress_seconds: 1800   # stop after N seconds with no output, file changes, commits, or CPU-using child; 0 = off
```

---

## Autonomous Prompt Injection Protection

When using autonomous PRDs with user-supplied specs (especially via federation peers, comm channels, or operator-created tasks), enable the injection guard to detect and optionally block known prompt injection patterns before they reach the LLM:

```yaml
autonomous:
  injection_guard: true       # log hits on every create/edit
  block_on_injection: true    # reject suspicious input with HTTP 400 (optional)
```

**Data-boundary tags** — regardless of the guard flag, all three LLM call sites (PRD decomposition, task verification, guardrail invocation) always wrap user-supplied content in `<user_data>` XML tags and prefix prompts with a security preamble that instructs the model to treat the content as data, not instructions.

**Federation trust notice** — when a PRD was submitted by a remote federation peer (`autonomous.owner_peer` is set), both the verifier and guardrail prompts include an explicit untrusted-origin notice. This applies automatically; no additional configuration is needed.

**Prometheus metric** — `datawatch_injection_guard_hits_total` counts all scanner hits (raised whether blocking or warn-only) for dashboarding and alerting.

---

## Undeclared Edit Mode (`autonomous.undeclared_edit_mode`)

Every direct-mode (non-worktree) autonomous task is checked after
verification for **undeclared edits**: files the task's own worker
committed in `<pre-task SHA>..HEAD` that were clean before the task started
and are in neither the task's planned `files` nor its generated files.
Pre-existing uncommitted files it swept in belong to `drift_mode` (see
[docs/howto/file-drift-guardrail.md](howto/file-drift-guardrail.md)), and a
file is never reported by both. The check never reverts or modifies
anything; it decides only what *happens* to the task:

| Value | Behavior on a finding |
|---|---|
| `off` | The check does nothing: nothing is recorded or alerted. |
| `warn` (default) | The finding is recorded on the task (`undeclared_edits`, lines summed across retries), appended as an `undeclared_edit_detected` decision, audited, logged and raised as a warn-level alert. The task continues and completes normally. |
| `block` | The task is set to **Blocked** with error `undeclared_edit: <files>` and the Automaton stops walking its tasks. |

Only `off`, `warn` and `block` are accepted. `PUT /api/config` and
`PUT /api/autonomous/config` reject anything else with HTTP 400; a bad value
that is stored another way (hand-edited YAML) resolves to `warn`, never `off`.

### Reading and writing it

| Surface | Read | Write |
|---|---|---|
| **CLI** | `datawatch config get autonomous.undeclared_edit_mode` | `datawatch config set autonomous.undeclared_edit_mode block` |
| **REST** | `GET /api/config` (key `autonomous.undeclared_edit_mode`, `"warn"` when unset); `GET /api/autonomous/config` | `PUT /api/config` with `{"autonomous.undeclared_edit_mode":"block"}`, or `PUT /api/autonomous/config` with `{"undeclared_edit_mode":"block"}` |
| **MCP** | `autonomous_config_get` | `autonomous_config_set` with `{"undeclared_edit_mode":"block"}` |
| **Comm chat** | `configure` (no args) lists the key | `configure autonomous.undeclared_edit_mode=block` |
| **Web (PWA)** | Settings → Autonomous Automata planning → **Undeclared edit mode** | Same select: `off`, `warn` or `block` |
| **YAML** | `autonomous.undeclared_edit_mode:` in `~/.datawatch/config.yaml` | Set it under `autonomous:` |

---

## Notification Routing Across Comm Channels

Every comm channel you enable (Signal, Telegram, Discord, Slack, Matrix,
`imap_mcp`, ntfy, email, generic webhook) receives the **same**
session-lifecycle notification broadcast by default — task started,
waiting for input, completed, or failed. This has always been true;
what's easy to miss is that it applies identically to every channel,
including ones you enabled for a different reason (e.g. `imap_mcp` for
inbound email commands) — if the channel is enabled, it gets the
broadcast too, with no separate per-category toggle.

```yaml
notify_exclude:
  - imap_mcp   # stays enabled for inbound commands, opted out of the broadcast
  - ntfy
```

Found 2026-10-10: an operator was surprised by `imap_mcp` emails that, it
turned out, had always been sent — they'd simply always bounced before a
separate outbound-recipient bug (B110) was fixed, so this was the first
time that channel's share of the existing broadcast actually arrived.
`notify_exclude` (Settings → Comms → Notifications in the PWA) lets you
keep a channel enabled for whatever else you use it for while opting it
out of this specific broadcast — inbound commands on an excluded channel
are unaffected. Checked fresh on every send, so a config change applies
without a restart.

---

## Autonomous External GitHub Actions

Two autonomous-PRD mechanisms call out to GitHub on the operator's own
`gh` credentials, without an explicit per-call confirmation step — worth
knowing both exist before enabling them:

- **Auto-PR on completion** — when a `ProjectProfile`'s `Git.AutoPR` is
  set, a worker session's branch is pushed and a PR opened automatically
  when its session ends (`internal/agents/post_session.go`).
- **`file_upstream_issue` project rule action** (BL406 Phase 4) — a
  configured `autonomous.scan.project_rules` entry with
  `action: file_upstream_issue` files a real issue in the repo named by
  its `upstream_target` (resolved against `autonomous.upstream_repos`)
  whenever that rule's finding fires — at task, story, or PRD
  completion, per the rule's own `granularity`.

Both use the same `gh`-CLI-backed provider (`internal/git`), so they
need `gh auth login` already run on the daemon host — the same
credential the operator uses interactively. Neither mechanism asks for
confirmation before acting; review a project rule's `action` field (or
a profile's `Git.AutoPR`) before enabling it on a PRD whose task specs
you don't fully trust.

---

## Guided Mode and the Per-Task Approval Gate

Setting `guided_mode: true` on a PRD (`POST .../set_guided_mode`, the PRD
Settings checkbox, or the New-Automaton wizard checkbox) pauses the
executor before every task and waits for an explicit approval before it
runs — `POST .../approve_task` (REST), `autonomous_prd_approve_task`
(MCP), `autonomous approve-task <prd-id> <task-id>` (comm), or the
Approve button on a task row showing the ⏸ glyph in the PWA. A gated
task sits in `awaiting_approval` status and is skipped by the run loop
until approved; approving one task does not auto-approve the rest. This
is independent of the existing per-story approval gate
(`per_story_approval`) — a PRD can use either, both, or neither.

Before v9.0.17 this checkbox had no effect at all (tracked as B111): the
set-side plumbing existed across every surface, but nothing in the
executor ever read the flag. It is useful specifically as a checkpoint
against decomposer scope drift — see AGENT.md's "Decomposer Scope-Drift
Rule" (BL384) for when to reach for it, and the `scope_drift` project
rule below for an automated detector covering the same failure mode.

Internally, the flag is now `GuidedModeSource` (`""` off, `operator`
real today, `council`/`guardrail_auto` declared but not yet
differentiated — `council` falls back to `operator` with a logged
reason until BL405 Phase 8 ships a real council-consensus mechanism).
The boolean wire format (`guided_mode: true/false`) is unchanged on
every surface; there is no separate "source" field to set.

### `scope_drift` project rule (opt-in, B112)

A built-in `scope_drift` entry in `autonomous.scan.project_rules` flags
a task spec containing code-creation language ("Implement ...", "Write
code for ...", "Create .go files ...") whenever the parent PRD's own
spec declares doc-only/no-code-changes work. Unlike the
parity-inheritance rule, this is **not** enabled by default — a normal
PRD with no such constraint saying "Implement X" is expected work, not
drift, so defaulting it on would false-positive constantly. See
`docs/config-reference.yaml`'s `scan.project_rules` for the commented-out
example to copy in; run `autonomous_prd_scan` after decomposition and
before approving any task to check it.

---

## PRD Git-Worktree Isolation + Completion PR (BL407, all phases)

By default, a PRD with no explicit `project_dir`/`project_profile`/
`cluster_profile` runs directly in whatever directory the daemon
process itself happens to be in — fine for an operator typing
commands, risky for an autonomous loop about to run unattended against
a real backlog (uncommitted/in-progress work in that directory is
exposed to every task's worker).

Setting `autonomous.worktree_base_repo` to a real git repo path (e.g.
this checkout, for self-build/dogfooding work) changes that default:
every future PRD meeting the condition above gets its own `git
worktree` under `autonomous.worktree_dir` (default
`~/.datawatch/prd-worktrees/<prd-id>/`), checked out on its own branch
(`automaton/<prd-id>-<slug>`) sharing the base repo's `.git` object
store — cheap, and isolated at the filesystem/git level. This is
automatic once configured, not a per-PRD opt-in: an operator who sets
this once gets the safety default for every PRD going forward without
having to remember to ask for it each time. A PRD that already sets an
explicit `project_dir` is left alone (explicit always wins). Task
sessions always commit automatically now, in every mode — the
worktree case is no longer a special case (`session.auto_git_commit` is
deprecated and ignored everywhere) — the whole point of the branch is
to be reviewable, which breaks silently if nothing ever gets committed
onto it.

Worktrees are also made buildable the moment they exist: when a PRD
worktree is created or reused, the daemon ensures
`internal/docsindex/assets/docs-bm25-index.json` exists in repos that
contain `internal/docsindex`, writing the same `{}` placeholder
`make docs-index` writes — and only when the file is missing (an
already-generated index is never touched), while the file stays
git-ignored (falling back to the worktree's own `info/exclude` file if
the ignore rule is absent) and is never staged or committed. The
consequence of the placeholder: a daemon binary built inside the
worktree runs with an empty docs search until `make docs-index` is
actually run there.

The worktree is created once, at first `Run()`, and is idempotent
across a daemon restart mid-run. Setting `PRD.Git.AutoPR` to `true` (per-PRD, defaults false — or set
`autonomous.default_auto_pr: true` to start every future PRD with it
already on; see `docs/config-reference.yaml`) makes a
successfully-completed worktree-mode
PRD push its branch and open a real PR automatically — one PR per PRD,
not one per task — then remove the local worktree directory (the
branch + PR are the durable record now, not the local checkout). A
push or PR failure is logged and recorded on the PRD (check its
Decisions) but never fails the run; the worktree is left in place for
inspection whenever that happens, and whenever `AutoPR` is left false.
This is the same category of unconfirmed GitHub action as "Autonomous
External GitHub Actions" above — review before enabling on a PRD whose
task specs you don't fully trust. Set it (or the base-branch override)
on an existing PRD via `POST /api/autonomous/prds/{id}/set_git`, the
`autonomous_prd_set_git` MCP tool, `datawatch autonomous prd-set-git
<id> on|off [--base-branch <name>]`, or the PWA's PRD Settings → Git
section. See `docs/plans/2026-10-10-prd-git-workflow.md`.

A `cluster_profile` PRD gets the same `Git.AutoPR` behavior, but the
mechanics differ: there's no local worktree for the parent to push
from (the worker runs in its own container/pod), so the **worker**
commits and pushes its own branch itself, using the same git token the
parent minted it to clone with, then reports the branch + commit SHA
back on completion. The parent only ever opens the PR from that
reported branch — it never attempts a local push for cluster-dispatched
work. Everything else (review-before-enabling, non-fatal on failure,
Decision-logged) is identical to worktree mode.

As of Phase 4, cluster mode gets the same dedicated per-PRD
`automaton/<prd-id>-<slug>` branch worktree mode always has — the
dispatching worker clones the Project Profile's repo on its own
default branch, then creates the PRD's branch locally (`git checkout
-b`) rather than landing on whatever branch the Project Profile's
`git.branch` names. Two or more tasks on the same cluster-dispatched
PRD requesting that same branch concurrently is expected (one PR per
PRD) and handled by waiting, not failing: a second task's dispatch
waits for the first task's agent to finish before it gets its turn —
shown as the task's normal capacity-wait state — rather than being
rejected outright.

**Out of scope, named so it isn't lost**: everything above covers the
*final* diff — the branch push + PR at PRD completion. It does not
cover interim session output/telemetry durability *during* a run for a
container/cluster-dispatched PRD — that path is still pull-only today
(`forwardSessionToAgent` live-proxies every output request; nothing
durable accumulates on the orchestrating daemon), so an abrupt
container death before a clean session-end loses the transcript with
no forensic record. That's a separate, not-yet-designed F-2
container-hardening concern — see
`docs/plans/2026-10-04-f2-session-worker-isolation.md` §11 for the
operator's push-primary/poll-fallback direction for it.

---

## Session git opt-in (`git` true/false at start, `session.git_default`)

Every session start can opt the session in or out of git activity in
its project directory. This is the mechanism that makes a session's
output reviewable and rewritable: checkpoint tags around the work, a
pre-session snapshot for PRD task sessions, and a gated completion
commit.

### `git: true` — what the session does in the project tree

1. **Repo setup (only if needed).** If the project directory is not yet
   a git repository, one is initialized there (branch `main`, committer
   identity `datawatch <datawatch@localhost>`) so the steps below have
   something to commit to.
2. **Pre-session snapshot (PRD-owned task sessions only).** The tree
   state at launch is committed (message starts `pre-session: <task`),
   so the work the session is about to do can always be told apart from
   what was already there. Ad-hoc one-shot sessions without a PRD owner
   never get a snapshot commit — an operator's in-flight work is not
   something a session should own.
3. **Checkpoint tags.** An annotated tag `datawatch-pre-<session id>` is
   placed before the session runs and `datawatch-post-<session id>`
   after it finishes. These are the handles for review and rollback:
   - `git diff datawatch-pre-<id> datawatch-post-<id>` shows exactly
     what the session changed.
   - `POST /api/sessions/<id>/rollback` (or the `session_rollback` MCP
     tool) resets the tree back to the pre-session state. It refuses
     while uncommitted changes exist unless you pass `{"force": true}`.
4. **Completion commit.** When the session signals completion, its work
   is committed exactly once, message built from the session's
   completion summary (falling back to the task title):
   - A PRD-owned session is gated by the PRD's own verifier before it
     reaches the commit.
   - An ad-hoc one-shot without a PRD owner commits only after its
     declared validation commands all pass; with none declared the
     commit is skipped and the reason is logged.
   - Staging is scoped to the session's write directories — files
     elsewhere in the same tree are never swept in. If the session has
     no write scope, only the files changed during that run are staged,
     and a log warning names them.

### `git: false` — what the session does

Nothing to git, full stop: no `git init`, no snapshot commit, no
checkpoint tags, and no completion commit — on any path (completion
watcher, one-shot reaper, daemon restart) that would otherwise try to
commit a finished session. This is the safe setting for a tree you
don't want the daemon touching.

### What the default is on each surface

| Start surface | Default |
|---|---|
| PRD task session spawned by the autonomous executor | Git on when the PRD has git settings (a non-empty PRD git block — provider/URL/branches/Auto PR — or a project profile); off otherwise |
| Any other start request that omits `git` | `session.git_default` (default **false**) |
| Any start request with an explicit `git: true` / `git: false` | The explicit value always wins |

So PRD task sessions are git-enabled out of the box when the PRD is set
up with git; everything else (MCP, schedules, chat, API, PWA) is
git-less unless it asks for it explicitly or you flip the daemon-wide
default.

### `session.git_default`

The daemon-wide default for the `git` field when a start request does
not set it:

```yaml
session:
  git_default: false   # default: sessions started without git leave the tree alone
```

- `git_default: true` — every session started without an explicit `git`
  field runs under git (repo setup, pre-session snapshot for PRD task
  sessions, checkpoint tags, gated completion commit). Useful when your
  one-shots come from MCP, schedules, or chat and you want the completion
  commit in history.
- `git_default: false` (default) — keeps the historical behaviour:
  sessions started without a `git` field touch git in no way.
- An explicit `git: true` or `git: false` in the start request always
  beats the setting, in both directions.
- Change it like any other setting: `config.yaml`, the PWA settings
  surface, or the REST config API (key `session.git_default`). It is
  read at session start, so the new value applies to sessions started
  after the change — no restart needed.

---

## Project-tree write serialization (one writer per tree)

When more than one Automaton can touch the same working tree, datawatch
serializes writes per tree. This exists because concurrent PRDs running
workers in one tree interleave commits and diffs — the 2026-10-13 incident
saw three Automata do exactly that, producing cross-contaminated changes
and release-version drift in one checkout.

### What gets serialized, and what still runs in parallel

**Serialized:** task runs whose canonical tree resolves to the same key.
In direct mode that means the main checkout, any subdirectory of it, and
every linked worktree of the same git repository — the lock key is the
repo's shared base, so a linked worktree of your main branch serializes
against the main checkout. An ancestor (parent) Automaton that spawned
the running one is exempt — it is waiting on its child, not competing
with it.

**Still in parallel:** Automata on different trees (different checkout
roots, different worktrees, or different non-git directories) — the
mechanism is per-tree, not global. Worktree-mode Automata (per-PRD
`automaton.git.branch`) each key on their own worktree, so they run in
parallel with each other and with other branches. Capacity admission
(`autonomous.capacity.*`) is a separate gate on top.

### What you see

- A task blocked on the tree lock shows a wait reason:
  `tree lock: <tree> held by task <id> (PRD <id>) since <time>`, and it
  resumes when the holder releases (auto-wait works at the task level).
- Starting an Automaton whose tree is already held by a running one is
  refused with `tree busy: PRD <id> is already running on this tree; run
  one PRD per direct tree or use worktree mode`. The Automaton stays in
  an approvable state; re-kick it after the first one completes. Known
gap: there is no PRD-level auto-wait/auto-resume yet — a refused loser
  sits in `approved` until re-kicked (live-confirmed on 2026-10-18, see
  `tests/live-smoke/REPORT.md`).
- If you enabled `autonomous.refuse_dirty_tree_start`, a task whose tree
  has uncommitted changes is refused at admission with the dirty-file
  count and the options (commit or stash, `git clean`, or set the key
  back to `false`).

### Inspecting and unblocking the lock

- The lock file is at `<canonical-tree>/.datawatch/tree.lock`. Its JSON
  names the holder task, the Automaton and the acquisition time — that is
  who holds it and since when.
- It is git-ignored (`.datawatch/`), so a held lock never dirties `git
  status`.
- Normal case: an orphaned lock is reclaimed automatically once its
  holder task is gone or terminal (bounded reclaim attempts; a 30-second
  grace protects a fresh-but-unreadable record from being reclaimed
  mid-write). No operator action needed.
- Force-clear: if you are certain the holder is dead (daemon crash
  mid-write, corrupted task record) and the tree is wedged, delete
  `.datawatch/tree.lock` in the canonical tree; the next task re-creates
  it. Destructive — only when you can confirm no live worker is mid-commit.

### `refuse_dirty_tree_start` — the tradeoff

- **On:** a task in a dirty tree is refused at admission instead of
  starting. Catches the "previous run left cruft behind" state before
  any work begins, at the cost of requiring you to commit/stash/`git
  clean` before re-kicking.
- **Off (default):** tasks start regardless of tree state; the
  foreign-file drift guard after verification remains the after-the-fact
  detector for unexplained changes.
- The lock is advisory: it serializes only datawatch's own task writers.
  A human hand-editing the tree while an Automaton runs is outside the
  lock's scope — if that happens regularly, worktree mode (below) is the
  fix, not a setting.

### Worktree mode as the parallelism escape hatch

Want more than one Automaton on the same repo without waiting? Use
worktree mode (per-PRD `automaton.git.branch`) — each gets its own
worktree and the per-tree lock keys on each worktree's own path, so
there is no cross-worktree contention. The 2026-10-18 live smoke
verified this exactly: two PRDs on distinct trees ran concurrently for
the full duration while a third PRD on the shared tree was correctly
serialized (evidence: `tests/live-smoke/EVIDENCE/20261019-1637/`).

Reproduce the smoke: `bash tests/live-smoke/tree-serialization-smoke.sh`
against an isolated scratch daemon (never your production daemon).

---

## Ancestor CLAUDE.md files hidden from Automaton workers

**Symptom.** Claude Code loads every `CLAUDE.md` it finds walking up from
its working directory. A worker running in a PRD worktree (or in the main
checkout) therefore also read parent-directory files such as another
session's guardrails (`Session: ... | Task: ...`). The worker then took the
foreign task line as its own instruction, stopped, and asked which task to
do, which the daemon reports as "worker stopped and asked an interactive
question instead of completing the one-shot task".

**Mechanism.** The claude-code backend passes the launched `claude` a
per-worker `--settings` flag carrying the `claudeMdExcludes` setting:

```
claude ... --settings '{"claudeMdExcludes":["/abs/parent/CLAUDE.md", ...]}'
```

The list is computed at launch: every existing `CLAUDE.md` in a strict
ancestor directory of the worker's project directory, as absolute paths.
The project directory's own `CLAUDE.md` and the user-level
`~/.claude/CLAUDE.md` are never listed, so they still load. The
alternatives were rejected: `CLAUDE_CODE_DISABLE_CLAUDE_MDS=1` also drops the
project's own and the user-level file (workers rely on the worktree's timing
and completion rules), and `--bare` also disables auth, hooks and
auto-memory. No fallback file was needed.

**Verification.** Checked against Claude Code 2.1.306 (the version
installed when this was built) with `claude -p` and a scratch parent/child
`CLAUDE.md` pair carrying unique markers: with the exclude, the parent
marker disappeared and the child's, the worktree's and the user-level files
still loaded; with the env var everything disappeared. Not tested: glob
patterns (explicit paths only) and how `--settings` merges with the project's
own settings files. Findings: `docs/plans/ancestor-claude-md-findings.md`.
Unit tests are listed in `docs/testing-tracker.md`.

**Which launches.** Every launch of a session that belongs to an Automaton
(`Start` and `Restart`), in both PRD-worktree mode and main-checkout mode,
including resumed launches. Interactive and other non-Automaton sessions are
never touched: the flag is set explicitly on each launch, so a stale
setting cannot leak from a previous worker into an interactive session.
Only the claude-code backend is affected.

**Confirm it.** Look at the session's launch command (the `claude ...` line
at the top of the session's tmux pane or log). A worker shows
`--settings '{"claudeMdExcludes":[...]}'` naming each ancestor
`CLAUDE.md`; an interactive session shows no `--settings` flag. A worker
with no ancestor `CLAUDE.md` files also shows none.

---

## Compute-node queue (max_engines_per_node)

A compute node can only run so many local models at once. Each node has a
capacity pool named `node:<name>`. For a node that hosts a local engine, the
limit is its slot count. The operator's `session_slots` on the node wins when it
is set. Otherwise the count comes from
the node's observer GPU data: one slot per GPU, limited by the free GPU memory
against the size of the model it serves. Memory in use by any other program on
the GPU lowers the count, because it is subtracted from the free memory.
A node with no observer GPU data divides its free system memory by the size of
the model it serves, with a floor of one slot. A node with neither GPU data nor
system memory data falls back to `autonomous.max_engines_per_node` (default
`1`; values below `1` are clamped to `1`). The node's own
`max_concurrent_sessions` still acts as a ceiling when it is set and smaller.

A node that hosts no local engine takes its limit from `max_concurrent_sessions`
alone, as it always has: 0 means unlimited, and no observer count is used.
`session_slots` has no effect on such a node.

Running workers are not counted out when free memory shrinks. The count never
falls below the number of workers already holding the node, but that floor is
capped at the node's GPU count, so a node running more workers than it has GPUs
reads a limit below the held count. The workers keep running; new work waits
until the held count falls below the limit.

The count is re-read every 10 seconds, so a change takes effect without a
restart. The node detail (`GET /api/compute/nodes/{name}/detail`) carries
`session_slots`, the operator's override (0 = derive), and `derived_slots`, the
count in effect (the node's pool limit).

### What is gated

Only sessions whose model runs on a local engine on that node are gated: the
`pi`, `pi-prompt`, `ollama`, `opencode` and `opencode-acp` backends, when the
model's provider prefix (the text before the first `/`) names the node. Hosted
backends such as `claude-code` are never gated by this setting, and neither is
a model with no provider prefix.

### Waiting for node

When a node's pool is full, a session that needs it does not start. Its task
shows as `waiting_capacity`, and the wait reason names the node, for example:

```
waiting for node alpha: 1/1 engines in use, held by task-42
```

The `GET /api/capacity` response lists each `node:<name>` pool with its
`limit`, `held`, `queue_depth` (how many requests wait on that pool) and
`holder` (the IDs occupying its slots). The wait ends when a slot frees. It
is bounded by `autonomous.capacity_wait_timeout_seconds`, and it needs
capacity admission on (`autonomous.capacity_enabled`, on by default).

### Queue priority

When a slot frees, waiting requests are granted in this order:

1. **Verifier and guardrail sessions** for a task that already has finished
   work. They go first so finished work is checked and released.
2. **Workers**: ordinary task sessions.
3. **Planners**: planning and decomposition sessions. They wait behind workers
   and verifiers, so a PRD waiting on a busy node does not start extra
   planning work past the limit.

Within the same role, the existing order applies: higher PRD priority, then
waiters that have aged past the override window, then model affinity on the
node, then the least-recently-served PRD, then first in, first out.

### Setting it

```bash
datawatch autonomous config-set '{"max_engines_per_node":2}'
```

The same key is available in `config.yaml`, `PUT /api/config`, the MCP
`autonomous_config_set` tool, and the comm `configure` command
(`configure autonomous.max_engines_per_node=2`). The full
key list is in `docs/config-reference.yaml`. Raising the value lets more local
sessions run on each node at once. It does not change how many hosted-backend
sessions run.

---

## Pinning a model on a GPU node

Ollama keeps a limited set of models loaded on each node. A request for a
different model can unload the one your builds rely on. Datawatch's own
callers (ask, session summaries, memory helpers) and hand-started ollama
sessions can do this without anyone noticing. A pin stops it.

A pin has three parts:

| Setting | Scope | Default | Effect |
|---|---|---|---|
| `node_pinned_model` | per compute node | empty (no pin) | Requests with no explicit model run on the pin. An explicit request for the same model runs on it. An explicit request for a different model is refused. |
| `allow_model_swap` | per compute node | `false` | Lets an explicit request for a different model run on the node and replace the pin. Ignored when the node has no pin. |
| `pin_check_seconds` | global | `300` | How often the pin keeper checks that each pinned node still has its model loaded. If the model was evicted, the keeper loads it again and raises a warning alert. `0` turns the keeper off. |

The refusal and the default-model rule apply to every request as soon as the
pin is saved. The keeper's check loop is started once at daemon boot, so after
you change `pin_check_seconds` or add a pin to a node, restart the daemon.
On a systemd install, use `systemctl --user restart datawatch`.

### Pin a model on a GPU node

Pick the node and the model it must keep. The model name is the Ollama tag,
for example `qwen3.8-64k:27b`.

```bash
datawatch compute node update gpu-1 --kind ollama --address http://gpu-1:11434 \
  --pinned-model qwen3.8-64k:27b
```

`compute node update` replaces the whole node record. Pass `--pinned-model`
every time you update the node, or the pin is cleared. The same change works
through the other surfaces:

- REST: `PUT /api/compute/nodes/gpu-1` with `"node_pinned_model": "qwen3.8-64k:27b"`
- MCP: `compute_node_update` with `node_pinned_model`
- Comm channel: `compute node update gpu-1 kind=ollama node_pinned_model=qwen3.8-64k:27b`
- PWA: open gpu-1 in the Compute panel and edit it. Set **Pinned model** and **Allow explicit requests to swap the pinned model**.

Check it with `datawatch compute node get gpu-1`, or the MCP `compute_node_get` tool.
A whitespace-only pinned model is rejected with a 400 error.

### Allow a model swap

Set `allow_model_swap` only when you want an explicit request for another
model to take over the node. Without it, that request is refused.

```bash
datawatch compute node update gpu-1 --kind ollama --address http://gpu-1:11434 \
  --pinned-model qwen3.8-64k:27b --allow-model-swap
```

Requests with no model still run on the pinned model. To turn the swap off,
run the update again without `--allow-model-swap`. Omitting the flag resets it
to `false`.

Caveat: the keeper does not know about `allow_model_swap`. After a swap, the
next keeper check finds the pinned model missing and loads it again. That can
evict the model the swap brought in. If swaps need to stay in place, set
`pin_check_seconds` to `0` while you use them, and restore the value after.

### Turn the keeper off

Setting `pin_check_seconds` to `0` stops the re-pin loop. The pins still apply
to requests, so requests for other models are still refused unless the node
allows swaps.

```bash
datawatch compute pin config-set --seconds 0    # off
datawatch compute pin config-set --seconds 300  # back to the default
```

Equivalent calls: comm channel `compute pin 0`, REST
`PUT /api/config {"pin_check_seconds": 0}`, MCP `compute_pin_settings` with
`pin_check_seconds: 0`. A value is a whole number of seconds, 0 or more.
Read the current value with `datawatch compute pin config-get`, the comm
channel's `compute pin`, or MCP `compute_pin_settings` with no argument.
Restart the daemon after changing it.

### Warnings

- **Collision warning.** When two different model names that each run on the
  same ollama node are configured, the daemon warns at startup, on every config
  write, and in the config and capacity responses. The warning means a call to
  one model can evict the other.
- **Repin alert.** When the keeper reloads a pinned model that was evicted, it
  raises a warning alert titled "Repinned … it had been evicted". A repeating
  alert means something on the node keeps loading other models. Pin the
  caller's model explicitly, or find the caller.

## Per-LLM concurrency limits and plan tiers

Each LLM registry entry carries a concurrency cap for its `llm:<name>`
pool. Where it comes from, in precedence order:

1. **`max_inflight` > 0** on the LLM entry (explicit operator override);
2. **`plan_tier` + the `autonomous.plan_tier_max_inflight` table** — the
   entry's subscription tier (`unset`, `pro`, `max_5x`, `max_20x`, `api`)
   mapped to a cap (default table: `pro: 1`, `max_5x: 2`, `max_20x: 4`,
   `api: 0` / unlimited);
3. **kind default** — 1 in-flight for subscription kinds (claude-code and
   the other non-local, non-API kinds), unlimited for local
   Ollama/OpenWebUI kinds (those stay governed by the `node:` pools and
   `max_engines_per_node`) and for dollar-metered API kinds.

The table is **operator policy, not a technical limit** — tune it to what
your subscription plans actually allow, in one place:

```yaml
autonomous:
  plan_tier_max_inflight:
    pro: 1        # one concurrent session
    max_5x: 2     # the plan tolerates two
    max_20x: 4
    api: 0        # unlimited (cap dollars with a per-LLM budget instead)
```

An explicit `max_inflight` on the entry always beats the tier (a cap set
by mistake on one entry does not leak into every entry of that tier). The
caps count simultaneous autonomous inflight sessions per LLM; they are not
a rate limit, and they are only enforced when `autonomous.capacity_enabled`
is on (default). Setting `max_inflight` and `plan_tier` to the same value
is harmless (same limit, one source). Every value is visible: `GET /api/llms`
and `GET /api/capacity` each carry `effective_max_inflight` plus
`inflight_source` (`explicit` / `plan_tier` / `kind`), so you can see which
rule is actually applying.

### Setting it

| Channel | How |
|---|---|
| YAML | `autonomous.plan_tier_max_inflight: {...}` (see above) |
| PWA | Settings → Compute → LLM edit: *Plan tier* and *Max in flight* fields |
| REST | `PUT /api/llms/{name}` with `plan_tier` and/or `max_inflight`; `GET /api/llms` to read them back |
| MCP | `llm_update` / `llm_add`; `capacity_status` to see the effective limit |
| CLI | `datawatch llm update <name> ...` (tier and explicit cap) |

An unknown tier or a negative cap is a config error: the daemon refuses to
start and names the key.

---

## Parking a task while waiting on an external condition

A worker sitting on a long external event — a CI run, a deploy finishing,
an upstream review — would otherwise pin its capacity slot (and, while it
has work to do, its project-tree lock) for the whole wait. Instead, the
worker emits one marker line and stops:

```
DATAWATCH_AWAIT_EXTERNAL: CI run on the branch
```

What the operator sees after:

- **The slot frees immediately.** The task returns its capacity lease the
  moment the marker is detected, so queued tasks move forward instead of
  waiting behind a parked one. It is then re-admitted through the normal
  bounded capacity wait (`capacity_wait_timeout_seconds`); when the slot
  comes back, the parked session is re-prompted ("[automaton await-external]
  Your await-external cycle is complete …") and continues on the **same
  session** with its full context — no fresh spawn, and no retry or failure
  is counted.
- **The wait is visible, not a stall.** The task's `wait_reason` carries the
  `await_external` sub-reason with the operator's condition (e.g. `await_external:
  CI run on the branch`) on every status surface; the one-shot kill paths
  (waiting-input, idle, stale-log) are suspended while parked, so the task
  is neither killed nor auto-retried for waiting.
- **The tree lock follows the work.** The project-tree write lock is
  released with the slot — *unless the tree has uncommitted changes or
  commits not reachable from a remote-tracking ref*, i.e. the branch is
  ahead of its remote. Unpushed work keeps the lock (a sibling on the same
  tree could still interleave), fully pushed-and-clean releases it so other
  trees' work is not blocked by a parked task.
- **Timeouts and restarts are safe.** Re-admission failure leaves the task
  lease-free in its wait state and hands it back to the normal retry
  machinery. A daemon restart re-adopts a parked task whose session
  survived (or re-queues it if the session is dead), so a parked task never
  leaks a slot across restarts.
- **Every cycle is audited.** `autonomous_idle_wait_release` records which
  capacity pools the task's lease was released from, plus the tree key
  unlocked on the same cycle — a `null` tree key means the tree was held
  for unpushed work.

If the await-external cycle itself fails, the task is handed back to the
normal retry machinery — it is never silently counted as a task failure.

---

## Per-LLM dollar-window budget

A budget caps how many dollars autonomous work may spend on one LLM inside
a window. It complements the in-flight caps above: the caps limit how many
sessions run at once, the budget limits how much they may cost over time.
An LLM with no budget is never limited by dollars.

**Window styles.** Pick exactly one per LLM:

- **Rolling** (`hours`, 1 to 720): the trailing N hours. Spend ages out one
  entry at a time, so the window never "resets"; it frees up as old spend
  leaves it.
- **Weekly** (`weekday`, optional `time` as 24h `HH:MM`, default `00:00`,
  and optional `zone`, an IANA zone, default UTC): the window starts at that
  weekday and clock time and ends seven calendar days later. The clock time
  is kept across a daylight-saving change. Use this to line the budget up
  with a provider's weekly reset.

A budget needs `ceiling_usd` greater than zero and one window style.
Setting both styles, neither, or an unknown weekday or zone is rejected
before anything is stored.

### Setting it

| Channel | How |
|---|---|
| PWA | Settings > Compute > LLM edit: the budget fields (ceiling, rolling hours or weekly weekday/time/zone, allocation estimate, peers); the Capacity card shows spend, window end and the full state; the allocation table is in Settings > Autonomous |
| REST | `PUT /api/llms/{name}` with a `budget` object. It replaces the whole object, is kept when the member is absent, and is cleared by `null` or `{}` |
| MCP | `llm_add` / `llm_update` with a `budget` object |
| CLI | `datawatch llm add\|update <name> --budget-ceiling-usd 50 --budget-hours 168` (or `--budget-weekday monday --budget-time 09:00 --budget-zone America/New_York`); `--clear-budget` removes it |

Reading it back, on every surface, gives the same fields:

| Channel | How |
|---|---|
| REST | `GET /api/budget` (every budgeted LLM) or `GET /api/budget?llm=<name>` (one; 404 when it has no budget); the `budgets` array of `GET /api/capacity` and `GET /api/autonomous/status` |
| MCP | `budget_status` (optional `llm`); `capacity_status` |
| CLI | `datawatch budget status [--llm NAME]` |
| Comm | `budget [status] [<llm>]` |

Each entry shows spend, ceiling, remaining dollars, the window, when it
rolls over (`rolls_over_at`) and `full`.

### What "budget window full" means

Once an LLM's spend in the window reaches its ceiling, autonomous work that
needs that LLM waits instead of failing. The task shows
`waiting_capacity` with a reason such as
`budget window full: spent $50.12 of $50.00 ceiling; window rolls over at
2026-10-26T09:00:00Z`. The wait uses no retry and burns no failure count.
Only the `llm:` pool is gated: operator-started interactive sessions are
never held back, and other LLMs keep running.

The wait ends by itself. Admission re-checks on the capacity wait loop's
next poll, so as soon as the window rolls over (weekly) or enough old spend
ages out (rolling), the task is admitted and starts. Nothing needs to be
cleared or re-run. If the task waits longer than
`autonomous.capacity_wait_timeout_seconds`, the normal capacity-wait timeout
applies. Each time a window fills and each time it frees up, an audit event
is recorded once (`autonomous_budget_ceiling_wait`,
`autonomous_budget_window_resumed`).

The check happens when work is admitted, not while it runs, and admission
cannot know in advance what a task will cost. Work already admitted when
the ceiling is crossed runs to completion and can overshoot the ceiling.
Set the ceiling a little below the figure you must not exceed.

#### Automation budget policy at member start

An Automation with a budget policy checks it before it starts any member,
including the entry member on activation and a whole fork. What happens when
the check refuses depends on why:

- **Dollar ceiling reached.** The cursor stays on the member that was about to
  start and nothing starts. If the window's reset time is known (a day, week or
  month window the daemon defines itself, or a reset time parsed from a
  rate-limit message) and `on_breach` is `pause`, the Automation pauses and
  resumes by itself at that time. If the reset time is unknown (a lifetime
  window with no parsed reset) or `on_breach` is `block`, the Automation is
  blocked and waits for you. A warn alert is raised either way, once per
  breach, and the breach is audited (`automation_budget_breach`).
- **Pacing or start estimate.** This is a hold, not a breach: the Automation
  stays active with the cursor on the member, one `budget_hold` decision is
  recorded, and the periodic check retries until the pace or estimate allows
  the start.
- **allocation_pct estimate.** Never pauses or blocks; it is only a labelled
  estimate.

At the reset time the daemon checks the policy again. If the Automation may
start it resumes (actor `budget`, audited as `automation_budget_resumed`, with
an info alert). If the ceiling is still reached it is moved to blocked once, so
a pause never loops. A blocked Automation never resumes by itself: raise the
ceiling or wait for the window, then resume it. Resume re-checks the policy and
refuses with a 409 that carries the reason while the ceiling is still reached.
Another daemon's usage counts toward a ceiling only for peers you opted in, and
that is not confirmed automatically.

### Where spend comes from

The daemon records the cost of every session on the LLM as it is reported,
priced at the configured API rates, and keeps it in a ledger under
`<data_dir>/autonomous/`. The ledger survives a daemon restart; entries
older than 31 days are dropped. It counts this daemon's sessions only,
unless peers are opted in (below).

### The allocation estimate

Set `allocation_pct: true` on the budget to add an estimated
share-of-allocation figure to the status. It is this daemon's spend in the
window divided by a notional dollar allocation that **you** declare per plan
tier in `autonomous.plan_tier_allocation_usd` (for example
`{max_5x: 100}`), with the LLM's `plan_tier` choosing the entry. It is
always marked estimated, appears only when the tier and the allocation are
both set, and **never blocks or throttles anything**; only `ceiling_usd`
does. The table is operator policy, not a provider-published figure; the
default is empty.

### Counting a peer's spend

By default only this daemon's own spend counts. If the same account is also
used from another datawatch, name that peer in the budget's `peer_window`
(CLI: `--budget-peer-window peerA,peerB`). Listing a peer is the explicit
opt-in: only listed peers are asked, periodically in the background (never on
the admission path), for their own spend through `GET /api/budget/usage`, which reports local spend
only, so two daemons that count each other never count the same dollars
twice. The status shows one line per peer with its spend and whether it is
counted, and why not (`not opted in`, `not registered`, `unreachable`).

A peer that cannot be reached is skipped, not treated as full: the local
spend alone decides, and the failure is logged once per outage. The
fingerprint exchange (below) can help you decide which peers truly share the
account, but it never changes this list: it stays your explicit choice.

## Automation spending limit

An Automation can carry a spending limit: a dollar ceiling over a window.
It complements the per-LLM budget above. The per-LLM budget caps what one
LLM may cost across everything; the Automation limit caps what one chain of
Automata may cost, whichever LLMs they use. Both apply, and the tighter one
decides. An Automation with no limit is never stopped by dollars.

### What it covers

The limit counts the cost of every member Automaton of the Automation: the
sessions that did the tasks, the planning (decompose) sessions and the
verification and judge sessions. A member that is itself a nested Automation
counts the same way, down through its own members. Each session's cost is
counted once even if it is reachable twice. The figures come from the same
spend ledger the per-LLM budget uses, so there is no second set of numbers to
reconcile. Set `backend` on the limit to count only one LLM's spend; leave it
empty to count every member's spend.

A member can carry its own limit as well. It narrows the Automation's limit
and counts only that member's spend; it never widens it. When both apply, the
one with the least room left decides.

### Setting it

| Channel | How |
|---|---|
| REST | `POST /api/automations/{id}/set_budget` with the policy fields (below), `expected_version`, `actor` and `reason`; `member_id` targets one member's limit; `clear: true` removes the limit |
| MCP | `automation_set_budget` |
| CLI | `datawatch automation set-budget <id> --ceiling-type dollar --ceiling-value 40 --window week --pacing even` (add `--member <id>` for one member; `--clear` removes it) |
| Comm | `automation set-budget <id> ceiling_type=dollar ceiling_value=40 window=week pacing=even` (`member=<id>`; `clear=true`) |

The fields replace the whole limit each time. `ceiling_type` is `dollar` (the
only type that can stop work) or `allocation_pct` (an estimate, see below),
`ceiling_value` is above zero, and `window` is `automation` (the whole life of
the Automation), `day`, `week` or `month`. Optional: `pacing` (`none` or
`even`), `on_breach` (`pause`, the default, or `block`), `backend`,
`allocation_estimate`, `peer_window`, and the reset anchors `reset_time`
(24h `HH:MM`), `reset_weekday`, `reset_day` (1 to 28) and `reset_zone`.
A member's expected cost is set with `estimated_cost_usd` together with
`member_id`. Setting a limit does not change the Automation's status. It can
be set on a draft, active, paused or blocked Automation, and on a member that
has not finished.

Read it back from `GET /api/automations/{id}`, `automation_get`,
`datawatch automation get <id>` or the comm command `automation status <id>`.
Each Automation row of the autonomous status carries the same `budget` view
with `spent_usd` and a short `ceiling_display` such as `$12.40 of $40.00
(week)`. The view shows spend, ceiling, remaining dollars, the window, when it
resets or that the reset time is unknown, what a breach will do, and any
pacing hold.

### Windows and when they reset

A `day`, `week` or `month` window is one the daemon defines itself. It starts
at the most recent reset and ends at the next. Unless you set anchors, a day
resets at local midnight, a week on Monday and a month on the first, in the
daemon's time zone. Set `reset_time`, `reset_weekday`, `reset_day` and
`reset_zone` to line the window up with a provider's reset. The `automation`
window never resets: it covers the whole life of the Automation.

### Pacing

`pacing: none` (the default) lets spend run up to the ceiling at any speed.
`pacing: even` spreads the ceiling across the window: if spend is already
ahead of an even spread, no new member starts until the window has caught up.
Pacing needs a window that ends, so it does nothing on the `automation`
window. A pacing hold is not a breach. The Automation stays active, one
decision is recorded, and the periodic check retries.

A member with a declared expected cost is held the same way when starting it
would take spend past the ceiling. Without a declared cost a member starts
whenever spend is below the ceiling, except that a member being resumed uses
the spend it already had as its estimate.

### What happens on a breach

The ceiling is checked before any member starts: when the Automation is
activated, when it moves on to the next member, and before a whole fork
starts. It is also checked before a task of a member starts a new session.
Work already running when the ceiling is crossed is never cut off and runs to
completion, so spend can end a little above the ceiling. Set the ceiling a
little below the figure you must not exceed.

When the ceiling is reached:

- **The reset time is known** (a day, week or month window, or a reset time
  parsed from a provider's rate-limit message) and `on_breach` is `pause`.
  The Automation pauses and resumes by itself at the reset, with no action
  from you. If the ceiling is still reached when the time comes, it is
  blocked instead, once, so a pause never loops.
- **The reset time is unknown** (the `automation` window with no parsed
  reset), or `on_breach` is `block`. The Automation is blocked and waits for
  you. Raise the ceiling or wait for your own budget to refill, then resume
  it. Resume checks the limit again and is refused with the reason while the
  ceiling is still reached.
- **Either way** an alert is raised once for the breach, the breach is
  recorded in the audit trail, and the Automation's decision log says why.
  Its current state (paused or blocked, since when, and the time it will
  resume if it will) is in the `state` field of the budget view.

### The estimate

Set `allocation_estimate: true` to add an estimated share-of-allocation
figure. It divides the spend by a notional dollar allocation you declare per
plan tier in `autonomous.plan_tier_allocation_usd`. It is always labelled
estimated, it is off by default, and it never pauses or blocks anything. A
limit of type `allocation_pct` behaves the same way: it only shows the
estimate. Only a `dollar` ceiling stops work.

### Counting another daemon's usage

By default only this daemon's spend counts. If another datawatch spends on the
same account, name that peer in `peer_window` (and set `backend`, because peer
usage is reported per LLM). Only the peers you name are asked, and only for
their own local spend, so two daemons that count each other never count the
same dollars twice. Nothing writes to the list: even a confirmed fingerprint
match (below) does not add the peer to it, so it stays your explicit choice,
and the status says so beside every counted peer. A peer that cannot be
reached is skipped and counts as zero, so a peer outage never stalls your
work.

---

## Resource fingerprint exchange between federated peers

When two daemons both opt the other in, they periodically exchange **hashes**
of their shared-resource fingerprints and flag a hit as a **pending match** —
you decide, nothing is automatic:

- **Confirm** — the two daemons hold the *same underlying resource*. For a
  compute node, this starts counting the peer's admitted work against your
  admission capacity (one shared pool, not two); rejecting it again removes
  the count.
- **Reject** — the resources stay separate. A reject also undoes a prior
  confirm, and the record then stays rejected.

What's compared: the **compute node's own identity** as reported by the node
itself (its GPU UUID, or the cloud instance ID the platform reports) and the
**billing-group label** you set on LLM entries that bill against one shared
account (the LLM's `billing_group` — a label, up to 64 characters, empty = no
group). Node hostname, IP address and node names are never part of the
comparison, and a node with no resolvable identity is skipped rather than
matched against a guess.

### Opting a peer in

The flag is `fingerprint_sync` on the peer record. Opt-in is **per peer, on
both sides** — here you opt the peer in *and* you must be opted in on the
peer's own record of you; the default is `false` in both directions. The
daemon also has to carry the `federation:fingerprint` capability (full-control
set only) — a peer token without it cannot read the exchange endpoint at all.

- **PWA**: Settings → Federation Peers → the opt-in toggle on the peer form,
or the checkbox when adding a peer.
- **CLI**: `datawatch federation peer add --name beta --url … --fingerprint-sync` /
  `datawatch federation peer update beta --fingerprint-sync` — `--fingerprint-sync=false`
  turns it off again (never send an empty value to "reset" it; unset means
  unchanged).
- **Comms**: `federation peer fingerprint <name> on|off`.
- **MCP**: `federation_peer_add` / `federation_peer_update` with `fingerprint_sync`.
- **REST**: `POST /api/federation/peers` / `PUT /api/federation/peers/<name>` with `fingerprint_sync`.

To label an LLM entry: PWA Settings → Compute → the billing-group field on
the LLM entry; `datawatch llm add|update … --billing-group <label>`; comms
`llm add|update … billing_group=<label>`; or `llm_update` / `llm_add`
(`billing_group`). Use the **same label on both daemons** for the entry that
bills against the shared account.

### Seeing matches and deciding

A match appears as `pending` after the next reconciliation pass (the default
interval is about 10 minutes, so not seconds). Where to act:

- **PWA**: the pending panel in Settings → Federation Peers shows one row per
  match (kind, both resource names) with *Confirm* / *Reject*; confirmed
  matches stay listed (collapsed) and each row keeps a *Reject*.
- **CLI**: `datawatch federation fingerprint list [--state pending|confirmed|rejected]`,
  … `confirm <id>`, `reject <id>` — the id is the one in the first column.
- **Comms**: `federation fingerprint list [state]`, `federation fingerprint confirm <id>`,
  `reject <id>` — an unknown id gets the usage line back, nothing is sent.
- **MCP**: `federation_fingerprint_matches` (list), `federation_fingerprint_confirm`,
  `federation_fingerprint_reject`.
- **REST**: `GET /api/federation/fingerprint-matches`,
  …/{id}/confirm`, `/reject` (the decision routes echo back the updated record).

Every confirm/reject is written to the audit log (`fingerprint_match_decision`).

### What you are (not) sending

The exchange never sends **raw values** — node names, addresses, the
billing-group label, tokens, credentials, *never*. What goes on the wire are
**per-peer-pair hashes**: the same value hashes to a different string for
each peer pair, and each side's label/identity is hashed on its own daemon
before it leaves. Neither side can reverse one hash into the other side's
value, and a peer can't tell whether two of your entries share a label unless
you confirm a match yourself.

### Limits you should know about

- A peer that is unreachable when the exchange runs is **skipped, not
  treated as full**: the local decision alone proceeds, and the failure is
  logged once per outage — a peer outage never stalls your daemon.
- The **billing-group hash only proves that two daemons carry the same
  label**; it does not prove the accounts are actually shared. Use it as a
  strong hint to verify, not as verification itself.
- A pending match is inert: only a confirmed one starts counting the peer's
  admitted hold against local capacity.
- This is not a new budget: confirming a shared node changes admission
  (capacity), and a confirmed shared billing account is something you then
  choose to add to a budget's `peer_window` yourself. The list never
  changes on its own.

The full operator walkthrough and security walkthrough, with a
recommended two-daemon live test, is in `docs/howto/federation-fingerprint.md`.

---

## Capacity visibility and audit trail

Every task that waits says why, in the same words on every surface, and the
waits, admissions, usage back-offs and budget transitions are written to the
audit log.

**Reading a wait.** `autonomous_status` (REST `GET /api/autonomous/status`)
lists queued tasks under `queue` and running tasks under `holdings`;
`GET /api/capacity` (MCP `capacity_status`, `datawatch capacity`, the `capacity`
chat command) adds pools, holders, back-off flags and per-LLM budgets. The
Automata task detail and the session Status tab show the same reason with a
cause label (pool full, node busy, back-off, budget, cool-down, awaiting
external, tree lock) and the time the wait ends, when known. The PWA Capacity
card shows pools with limits and holders, back-off flags, and budget spend
bars; its allocation share is an estimate and never blocks anything.

**Reading the history.** Eight actions record the capacity and budget
machinery: `autonomous_capacity_wait`, `autonomous_capacity_admitted`,
`autonomous_usage_backoff_set`, `autonomous_usage_backoff_cleared`,
`autonomous_usage_backoff_auto_cleared`, `autonomous_budget_ceiling_wait`,
`autonomous_budget_window_resumed` and `autonomous_budget_window_rollover`.
Each is written once per change, not once per poll. All four surfaces take
the same filters (`actor`, `action`, `subject`, `session_id`, `since`,
`until`, `limit`), and an `action` ending in `*` is a prefix match:

```bash
datawatch audit --action 'autonomous_budget_*' --subject claude-code --limit 20
```

REST `GET /api/audit`, MCP `audit_query` and the `audit` chat command
(`audit action=autonomous_budget_* subject=claude-code`) take the same
filters. The full table of causes, example texts, actors, subjects and values
is in [howto/automata-capacity.md](howto/automata-capacity.md).

---

## Lane fill (`autonomous.lane_fill`)

Lane fill keeps the GPU and Claude lanes busy. On every poll the planner looks at the lanes and the queue and decides what each idle lane should start. It is in **shadow** mode by default: it records every decision it would make and starts nothing. Switch `autonomous.lane_fill.mode` to `on` only after the shadow comparison in [howto/automation-lane-fill.md](howto/automation-lane-fill.md) has passed.

**Verifier-wait lane.** A lane whose occupant is running but all of its work is stalled waiting on a verification slot is not idle: the status shows it as `state: "waiting_verifier"` with `waiting_on_verifier: true` and the `wait_reason` from the wait queue, the lane line reads "waiting on verifier: <reason>", and the idle-lane alert names the wait and its reason before it fires. With `autonomous.lane_fill.count_verifier_wait` on (the default) such a Claude lane counts available, because the waiting occupant is not spending Claude usage; `false` keeps it busy. In shadow mode this counts nothing as a start and records a would-start, as with every other lane fill decision. See [howto/automation-lane-fill.md](howto/automation-lane-fill.md).

| Key | Default | Meaning |
|---|---|---|
| `autonomous.lane_fill.mode` | `shadow` | `off` (nothing runs), `shadow` (decide and record, start nothing) or `on` (carry the decisions out). |
| `autonomous.lane_fill.gpu_lanes` | `2` | GPU lanes on the local model pool. |
| `autonomous.lane_fill.claude_lanes` | `3` | Claude lanes. `0` means none. |
| `autonomous.lane_fill.idle_alert_minutes` | `30` | How long a lane may sit idle with eligible work waiting before one alert. `0` turns the alert off. |
| `autonomous.lane_fill.decompose_ahead_min` | `2` | Decomposed Automata kept ready per lane type. |
| `autonomous.lane_fill.poll_seconds` | `60` | Seconds between planner polls. Values below 5 are raised to 5. |
| `autonomous.lane_fill.gpu_backend` | `pi-prompt` | Backend the GPU lanes run. |
| `autonomous.lane_fill.gpu_model` | empty | Model the GPU lanes run. Empty leaves it to the backend. |
| `autonomous.lane_fill.claude_backend` | `claude-code` | Backend the Claude lanes run. |

Every key is read live. Mode and threshold changes apply on the next poll, with no restart. A bad value is refused and nothing is written.

**Lane preference.** Each Automaton has a lane preference, `gpu`, `claude` or `any` (the default). It is stored on the Automaton, and the planner honours `gpu` and `claude` exactly. Set it per Automaton: REST `POST /api/autonomous/prds/{id}/set_lane_preference`, MCP `autonomous_prd_set_lane_preference`, CLI `datawatch autonomous prd-set-lane-preference <id> gpu|claude|any`, comm `autonomous set-lane-preference <prd-id> gpu|claude|any`, or the Lane preference select in the Automaton settings form.

### Lane fill surfaces

| Surface | Read the status | Change the mode or a threshold | Set a lane preference |
|---|---|---|---|
| REST | `GET /api/autonomous/lane-fill?limit=N` (`limit` 1 to 1000, default 50) | `PUT /api/config` with the `autonomous.lane_fill.*` key | `POST /api/autonomous/prds/{id}/set_lane_preference` |
| MCP | `autonomous_lane_fill_status` | `autonomous_config_set` with `lane_fill_mode` and the other `lane_fill_*` arguments | `autonomous_prd_set_lane_preference` |
| CLI | `datawatch autonomous lane-fill [--limit N] [--json]` | `datawatch config set autonomous.lane_fill.mode on` | `datawatch autonomous prd-set-lane-preference <id> gpu\|claude\|any` |
| Comm channel | `autonomous lane-fill [limit]` | `configure autonomous.lane_fill.mode=on` | `autonomous set-lane-preference <prd-id> gpu\|claude\|any` |
| PWA | Automata tab, Automation sub-tab, the **Lane fill** card (reloads every 5 seconds) | Settings → General → Autonomous, the Lane fill fields | Automaton settings form, **Lane preference** select |

**What the status shows.** Each lane with its occupant or its blocker, the queued eligible counts for GPU and Claude work, the pool evidence (active GPU sessions and loaded models, or the error when a pool host could not be read), the newest decisions with whether each was executed, and the idle-lane alerts. In shadow mode a decision with `executed: false` is a would-start and must not be read as a start.

**Restart.** The decision history (up to 1000 decisions) is kept in a file in the autonomous data directory and reloads after a restart. Nothing is decided until boot recovery has finished, so Automata that were running are not read as idle lanes. A started Automaton is never started a second time. Idle timers start again after a restart, so an idle lane alerts only after a full threshold from the restart.

**What lane fill never does.** It never merges finished work. It never judges design or difficulty. It never holds GPU work because of Claude usage limits, since GPU lanes cost no Claude usage and the planner does not read usage or budget. It never starts an Automaton that is held (a `HOLD` line, "do not run", "sequenced last", or "sequenced after" an unfinished Automaton). It adds no second start path: every start in `on` mode goes through the normal admission, approval and quality-gate rules.

**Known gaps before `on`.** The plan's open questions are still open: whether `on` mode may approve a review-pending Automaton through the approve call (the shipped code does, subject to the plan-coverage and plan-review gates), whether an Automaton on a backend other than the GPU or Claude backend counts toward lane occupancy, and the default Claude lane count and alert minutes. Answer them before switching to `on`.

---

## Create-time overlap check and shadow mode (`autonomous.automation.create_search_mode`)

When an Automaton is created, the daemon can check whether an existing Automaton, a plans-registry row or an unmerged Automaton branch already covers the same work, and report any match as an overlap warning. The check never blocks a create and never changes what it finds. The full guide is [howto/automaton-search-before-create.md](howto/automaton-search-before-create.md); the data flow is [flow/automaton-create-overlap-flow.md](flow/automaton-create-overlap-flow.md).

| Key | Default | Meaning |
|---|---|---|
| `autonomous.automation.create_search_mode` | `shadow` | `off` (no check), `shadow` (check and record in the audit log; the create reply is unchanged) or `on` (check and add `overlap_warnings` to the create reply). |
| `autonomous.automation.create_search_threshold` | `0.5` | Score a match must reach, above 0 and at most 1. A match also needs at least three shared words. |

Both keys are read on every create, so a change applies at once with no restart. A bad value is refused and nothing is written.

**Shadow mode.** It is the default so you can test the matching on real work before anyone sees a warning. Each create writes one audit entry with the action `automaton_create_search`, the new Automaton as its subject, and the mode and the warnings the check would have shown (an empty list when it found none). Read them with `datawatch audit --action automaton_create_search`, then compare each one with what the supervisor did with that Automaton: right warning, noise, or missed. Raise the threshold for noise, lower it for misses, and switch to `on` when both are low enough.

**Reading overlaps by hand.** `GET /api/autonomous/overlap?title=...&spec=...`, `datawatch autonomous overlap`, the comm verb `autonomous overlap <text>` and the MCP tool `autonomous_overlap_search` run the same search at any time, in every mode, and write nothing.

**If the check fails.** A source that cannot be read becomes an `unavailable` warning and the others still run. If the whole search fails, the daemon logs one `[overlap]` line and the create replies as if the mode were `off`. A failure never fails a create.

---

## Repo CI watch (`autonomous.automation.repo_ci_watch`)

Repo CI watch stands over the repository's CI: on every poll it reads the head of `main` (or the default branch) and the newest `tag_count` release-shaped tags (`v<M>.<m>.<p>`) across every workflow that ran on those head commits, keeps one record per run, and classifies each run as `none`, `infra`, `security`, `policy`, `code_or_test`, `judging` or `hung`. In `on` mode only, it takes the budgeted actions: at most one rerun of an `infra` run per run id (while the infra retry budget holds), one cancel of a run that hung, and the retention deletion of superseded runs named by a green record. It is **off** by default. The full behaviour, the classification rules, the shadow-mode comparison and the never-does list are in [howto/automation-repo-ci-watch.md](howto/automation-repo-ci-watch.md).

| Key | Default | Meaning |
|---|---|---|
| `autonomous.automation.repo_ci_watch.mode` | `off` | `off`, `shadow` (decide and record everything, write nothing, no alert) or `on` (the actions are taken, budgeted). |
| `autonomous.automation.repo_ci_watch.repos` | empty | Absolute paths of the repositories to watch; empty means nothing is watched. |
| `autonomous.automation.repo_ci_watch.poll_seconds` | `300` (unset or `0` reads `300`) | Seconds between polls; below 60 is refused on write (minimum 60). |
| `autonomous.automation.repo_ci_watch.alert_minutes` | `30` | How long a non-green run may sit before the once-per-episode alert; `0` turns the alert off. |
| `autonomous.automation.repo_ci_watch.tag_count` | `5` | Newest release tags covered per poll; `0` means none. |
| `autonomous.automation.repo_ci_watch.hung_after_minutes` | `60` | Minutes a run may stay pending before it is hung (minimum 1). |
| `autonomous.automation.repo_ci_watch.security_workflows` | `CodeQL`, `dependency-review`, `owasp-zap`, `secret-scan`, `security-scan` | Workflow names classified `security` (record-only, never rerun). |
| `autonomous.automation.repo_ci_watch.delete_superseded` | `report` | `report` (name the superseded runs, do not delete) or `delete` (in `on` mode the pass issues the retention deletions, on the later pass). |

Every key is read live; a bad value is refused on write by every path (unknown `mode`, non-absolute repository path, `poll_seconds` below 60, negative `alert_minutes` or `tag_count`, `hung_after_minutes` below 1) and nothing is written. The infra retry budget is the Manager's `RetryPolicy["infra"]` (`retry_policy.infra`); **unset reads 0 — no automatic reruns at all**, and a rerun is at most once per run id, shared across the repository·ref·workflow while the budget holds; a rerun that errors is not counted as spent.

**`security` and `policy` runs are record-only.** A failed security workflow is evidence, not a flake: the pass never reruns it, and not even as a would-rerun in shadow mode. The record carries the classification and the run URL, so the operator sees which security workflows are failing, on which ref, and for how long. `policy` is a judge verdict — at most one judge call per poll, on the first non-green run it meets; the verdict is cached per run id and never re-judged — and, like `security`, record-only. The other verdict, `code_or_test`, means the defect is in code or test and only the owner of the change can fix it: no automatic action either. `judging` is the transient state of a non-green run that had no verdict in this pass (the judge call was already spent, or failed) — not a verdict, and a later poll settles it.

**The one alert.** Once a non-green run has sat for `alert_minutes` (default 30; `0` turns the alert off), measured from its first-seen stamp, the pass raises exactly one alert per episode, naming the repository, ref, workflow, run, classification, first-seen stamp and run URL. The alert is stamped on the record as `alerted_at`, which is what keeps it once-per-episode. The alert raises in `on` mode only — shadow mode records its would-dos and raises no alert.

**Shadow mode records and never acts.** No rerun posts, no cancel posts, no deletions, no alert — the would-do lines on the record are the whole surface, and the read interfaces are identical in every mode.

**The automatic actions (on mode, budgeted).** At most one rerun of an `infra` run per run id, while the infra retry budget (`retry_policy.infra`; unset reads 0 — no automatic reruns at all) still has one for that repository·ref·workflow. One cancel of a run pending for more than `hung_after_minutes`, checked not-already-cancelled before the cancel is posted. Superseded-run deletions only when `delete_superseded` is `delete`, addressed by the recorded run ID, issued on the pass after the green record named the runs — never in shadow, and never on the pass that named them.

### Repo CI watch surfaces

| Surface | Read the state | Notes |
|---|---|---|
| REST | `GET /api/autonomous/repo-ci-watch?repo=…&status=…` | body: mode, alert_minutes, repos, last_poll_at, records newest first; the status filter accepts `green`, `failing`, `pending`, `hung`, `rerunning`, `superseded` — anything else is a 400 naming the values; an empty match is 200 with an empty list |
| MCP | `autonomous_repo_ci_watch` (read-only; `repo` / `status` filters) | same body as the GET route |
| CLI | `datawatch autonomous repo-ci-watch [--repo …] [--status …] [--json]` | table: repository, ref, workflow, status, classification, first seen, alerted |
| Comm channel | `autonomous repo-ci-watch` (alias `repo_ci_watch`) | mode line, then one line per record: repo, ref, workflow, status, classification, run URL; no arguments |
| Security failure reports | `security_failure_list`, `datawatch autonomous security-failure` and `autonomous security-failure` (alias `security_failure`) | the reports of failed `security` runs: see [Security failure report: stop and report](#security-failure-report-stop-and-report) |
| PWA | Automata tab, the **Repo CI watch** status line at the top of the tab | mode, failing/pending counts, last-poll age; refreshes every 30 s; hidden if the endpoint is unavailable |

Change the mode or a threshold: `PUT /api/config` (or Settings → General → Autonomous in the PWA) with the `autonomous.automation.repo_ci_watch.*` key, or `datawatch config set autonomous.automation.repo_ci_watch.mode shadow`, or `configure autonomous.automation.repo_ci_watch.mode=shadow` on the comm channel. Every change applies on the next poll — no restart.

**Restart.** The record history is in `<data-dir>/autonomous/repo_ci_watch.jsonl` and reloads after a restart. Records survive a stop (`mode: off` and a restart both keep the full history readable on every surface). A hung run's age is read from the CI run's own start time, so a restart does not reset the hung clock. A rerun or cancel in flight before the restart is not re-issued after it.

**What the watch never does.** It never merges, never starts or holds Automata — it watches the repository's CI runs, not the queue. It never reruns a `security` or `policy` run, in any mode, and never as a would-do. It never posts a second cancel for a run that is already resolving. It never issues a deletion in `off`, `shadow` or `report`, and never one on the pass that named the run. It never re-judges a run that already carries a verdict, and never re-alerts an already-alerted episode. It never spends retry budget it does not have. In shadow it writes nothing: the records and the would-do text are the whole surface, and no alert is raised. The shadow-mode comparison to do before `on` is in [howto/automation-repo-ci-watch.md](howto/automation-repo-ci-watch.md).

---

## Security failure report: stop and report

When a security check fails because of a dependency, the coordinator stops the Automaton line the failing commit belongs to and stores a **security failure report** for the run, instead of fixing the finding or documenting it away. The pass reads the repo CI watch's records: runs that are `failing`, classified `security` and on a workflow in `autonomous.automation.repo_ci_watch.security_workflows`. For each new run it reads the failed log (`gh run view <run> --log-failed`), reads the advisory identifier and the dependency from it, looks the advisory up, checks `security/accepted-risks.yml` for an entry with the same identifier, and finds the Automaton whose last completed task ended on the run's commit. A run is assessed once; a run that already carries a report is never assessed or alerted again. The full walk-through is in [howto/automation-security-failure.md](howto/automation-security-failure.md).

| Key | Default | Meaning |
|---|---|---|
| `autonomous.automation.security_failure.mode` | `shadow` (unset or empty reads `shadow`) | `off` (nothing is read, stored or stopped), `shadow` (the report is stored and a stop is only recorded as a would-do; nothing is stopped and no alert is raised) or `on` (the linked Automaton is stopped and one alert is raised at once). Any other value is refused on every write path. |

The security workflow set is read from `autonomous.automation.repo_ci_watch.security_workflows` and the alert time from the existing `autonomous.automation.triage_alert_minutes`; there is no other security failure key. The repo CI watch must itself be in `shadow` or `on` for there to be any failed security runs to read. Change the mode with `PUT /api/config`, `datawatch config set autonomous.automation.security_failure.mode on`, the `config_set` MCP tool, `configure autonomous.automation.security_failure.mode=on` on the comm channel, or Settings → General → Autonomous in the PWA. It applies on the next poll — no restart.

**The recommendation.** `upgrade` (a fixed version is known, whatever the registry says), `keep` (no fix, and the registry holds an entry for the advisory), `keep_no_entry` (no fix, and no entry: the operator adds one through the registry) or `unknown` (no fix, and the lookup or the registry read failed). There is no age or waiting clock: an advisory published an hour ago is acted on exactly like one published a year ago, and a missing publication date reads `unknown`.

**The action.** `stopped` (mode `on`, a dependency or CVE failure, and an Automaton line carries the commit: the Automaton is moved to blocked with the reason `security_failure_block`, and its triage record carries the report), `would_stop` (the same in `shadow`: one would-do line on the watch record, nothing stopped) or `report_only` (not a dependency failure, or no Automaton carries the commit; the note says which).

### Security failure report surfaces

| Surface | Read the report | Notes |
|---|---|---|
| REST | `GET /api/autonomous/security-failures?repo=…&status=…` | a JSON list, newest first; each entry has the run's identity (`repo`, `ref_kind`, `ref_name`, `workflow`, `run_id`, `run_url`, `status`) and the report under `report`; the status filter takes the repo CI watch states and anything else is a 400 naming them; an empty match is 200 with an empty list; GET only |
| MCP | `security_failure_list` (read-only; `repo` / `status` filters) | same list as the GET route |
| CLI | `datawatch autonomous security-failure [--repo …] [--status …] [--json]` | table: repository, ref, workflow, run, status, dependency, advisory, recommendation, action; without `--status` only open reports (watch status neither `green` nor `superseded`) |
| Comm channel | `autonomous security-failure [run-id]` (alias `security_failure`) | with no argument, the open reports as the CLI table; with a run id, the full report of that run, or a not-found reply |
| PWA | Automata tab → Automation view → Chains sub-tab, the **Security failure** card beside the triage card | one block per report; refreshes every few seconds; hidden if the endpoint is unavailable |

**Accepting a risk is the operator's act.** The coordinator never writes a suppression: it does not touch `security/accepted-risks.yml`, the generated `.trivyignore` or the generated container table in `docs/security-review.md`. To accept a risk, add the entry to `security/accepted-risks.yml` and run `make security-registry-gen`, which validates the registry and regenerates both generated files. `make lint` runs `scripts/check-no-direct-ignore-writes.sh`, which fails when either generated file differs from what the registry produces. The coordinator never reruns a security run, in any mode, and never cancels or deletes one. It never resumes or restarts the Automaton it stopped.

**Restart.** The reports live on the repo CI watch records and reload with them. An Automaton that was stopped stays blocked across a restart.

---

## Supervisor watch (`autonomous.automation.supervisor_watch`)

The supervisor watch is the daemon-side version of the supervisor script (`scripts/supervisor-watch.py`). On every coordinator poll, with no model call, it runs fourteen checks — twelve that the script also runs, plus `worktree_not_registered` and `verify_script_not_executable`, which only the watch has and the comparison therefore shows as *only the watch* — and keeps one finding per kind and subject. The script also checks things the watch does not yet (an unreachable model host, node balancing, capacity lease holds, placement, commits ahead of origin); see [howto/automation-supervisor-watch.md](howto/automation-supervisor-watch.md) for the differences to expect in the comparison. The kinds are `tmux_restart`, `daemon_inactive`, `model_not_resident`, `task_failed`, `task_review_needed`, `automaton_not_merged`, `idle_lane_eligible_work`, `worktree_no_gomod`, `worktree_not_registered`, `worktree_silent`, `verify_script_not_executable`, `main_checkout_dirty`, `ci_red` and `ci_stuck`; each is explained in plain words in [howto/automation-supervisor-watch.md](howto/automation-supervisor-watch.md). The watch is report-only in every mode: it never merges, starts, holds, cancels or retries an Automaton and never restarts a service. A check that cannot tell (a probe that failed, a node that cannot be reached) reports nothing. A finding closes by itself when its check stops reporting it, and re-opens in place, keeping its first-seen time, if the problem returns.

| Key | Default | Meaning |
|---|---|---|
| `autonomous.automation.supervisor_watch.mode` | `shadow` (unset or empty reads `shadow`) | `off` (the pass does nothing), `shadow` (everything runs and is recorded, a finding open for the alert time is marked would-alert, no alert is raised) or `on` (the alert is raised, once per episode). |
| `autonomous.automation.supervisor_watch.alert_minutes` | `30` | How long a finding may stay open before its one alert; `0` turns the alert off; negative is refused. |
| `autonomous.automation.supervisor_watch.silent_minutes` | `60` | How long an Automaton with a task in progress may show no activity before `worktree_silent`; negative is refused. |
| `autonomous.automation.supervisor_watch.poll_seconds` | `60` (unset or `0` reads `60`) | The interval the script record's freshness is judged against (stale at three times this); below 60 is refused (minimum 60). |
| `autonomous.automation.supervisor_watch.script_output_path` | empty | The file the supervisor script writes with `--record`; empty means no comparison. |
| `autonomous.automation.supervisor_watch.tmux_restart_baseline` | empty | The starting point `tmux_restart` compares against; empty means that check reports nothing. |
| `autonomous.automation.supervisor_watch.main_checkout` | empty | The main checkout inspected for `main_checkout_dirty`; empty turns that check off. |
| `autonomous.automation.supervisor_watch.working_model` | empty | The model every registered compute node should keep loaded, checked by `model_not_resident`; empty means no working model is set and that check reports nothing. |

Every key is read live; a bad value is refused on every write path (unknown mode, negative minute threshold, `poll_seconds` below 60, a path or model name that is not text) and nothing is written. Change a key with `PUT /api/config` (or Settings → General in the PWA), `datawatch config set autonomous.automation.supervisor_watch.alert_minutes 15`, or `configure autonomous.automation.supervisor_watch.alert_minutes=15` on the comm channel; each change applies on the next pass, no restart.

**Shadow mode records and never acts.** It runs every check, records the findings, writes the triage-record note for a finding about one Automaton the first time it is seen, and marks a finding `would alert` once it has been open for `alert_minutes`. It raises no alert. It ships first so you can compare it with the script before you rely on it.

**Trying it in a sandbox.** `bash scripts/supervisor-watch-sandbox-check.sh` builds this tree, starts a throwaway daemon (shared daemon-library traps, scratch `HOME`, `server.proxy_sandbox_port: 0` so it never binds the production proxy port) and shows the watch raise `main_checkout_dirty` live in shadow. It touches no other daemon.

**The comparison record.** With `script_output_path` set, each pass compares its own kind-and-subject findings with the script's record file and stores the result: what only the watch raised, what only the script raised, and whether they agreed. The last 500 comparisons are kept, with a per-day tally; a day counts toward **days of full agreement** only when it had a comparison and every one agreed. A script record that is missing, unreadable or older than three times `poll_seconds` records no comparison at all — unavailable is never a disagreement. Run the script with `--record` more often than that window, or raise `poll_seconds`. Retire the script after one full calendar day of agreement with a recent last poll.

**The one alert.** In `on` mode a finding open for longer than `alert_minutes` raises exactly one warning alert per episode; the title names the kind and the subject and the body carries the finding's line and how long it has been open. The alert time is stamped on the finding (`alerted_at`), which keeps it to one, including across a restart. A finding that resolves and returns is a new episode. The alert reaches your chat channels through the existing alert fan-out.

### Supervisor watch surfaces

| Surface | Read the state | Notes |
|---|---|---|
| REST | `GET /api/autonomous/supervisor-watch` | body: `mode`, `last_poll_at`, `findings` (open first, then most recently seen first), `comparison`; no parameters; 405 for any other method, 503 while the autonomous engine is disabled |
| MCP | `supervisor_watch_list` (read-only, no arguments) | same body as the GET route; an argument is refused |
| CLI | `datawatch autonomous supervisor-watch [--json]` | the mode, the last poll, one line per finding (kind, state, last seen, the operator line), then the comparison summary |
| Comm channel | `autonomous supervisor-watch` (alias `supervisor_watch`) | the same lines as the CLI; no arguments |
| PWA | Automation view → Chains sub-tab, the **Supervisor watch** card | mode, last-poll age, up to twenty open findings, the comparison line; refreshes every 30 s; hidden if the endpoint is unavailable |

### Supervisor watch monitoring

| Where | What |
|---|---|
| REST | `GET /api/autonomous/supervisor-watch/stats` — `mode`, `last_poll_at`, `last_poll_age_seconds` (-1 when it never polled), `findings_open` (includes the next two), `findings_would_alert`, `findings_alerted`, `findings_resolved`, `agreed_days` |
| MCP | `supervisor_watch_stats` (read-only, no arguments) — the same object |
| Stats snapshot | `supervisor_watch_mode`, `supervisor_watch_findings_open`, `supervisor_watch_findings_would_alert`, `supervisor_watch_findings_alerted`, `supervisor_watch_last_poll_age_seconds`, `supervisor_watch_agreed_days` on the WS stats broadcast |
| Web UI | Settings → Monitor → **Supervisor watch** |
| Prometheus | `GET /metrics` → `datawatch_supervisor_watch_findings{state}`, `datawatch_supervisor_watch_agreed_days`, `datawatch_supervisor_watch_last_poll_age_seconds` |

**The triage note is written in shadow too.** The one thing shadow writes beyond the watch's own records is the triage-record note for an Automaton-scoped finding; it is text on a record the daemon already keeps, changes no state and raises no alert. `mode: off` stops it.

**Restart.** The findings are in `<data-dir>/autonomous/supervisor_watch.jsonl` and the comparison history in `<data-dir>/autonomous/supervisor_watch_comparisons.json`; both reload after a restart, and `mode: off` keeps them readable too.

**What the watch never does.** It never calls a model, never merges, never starts, holds, cancels or retries an Automaton, never restarts a service or reloads a model, and never writes to a repository, the CI service or a worktree. It never alerts in `shadow` or twice in one episode, never turns an unknown into a finding, and never counts a missing or stale script record as a disagreement.

---

## Triage records (failed and blocked Automata)

The autonomous engine keeps one triage record per **failed** or **blocked** Automaton, so an operator can see what failed, when it has been sitting, how much retry budget is left, and whether the triage alert has fired — from every surface at once. The record is a **read**: filling it starts, retries and edits nothing.

**What the pass does, and when.** A triage pass runs on every coordinator tick (~60 s, the same idle tick as the rest of autonomous). It reads the live Automaton list once and then, per Automaton: fills a fresh record (identity, failed-task block, retry budget, notes) for each `failed` or `blocked` Automaton, refreshes the record if one is already kept, and clears the record if the Automaton is in any other state. So a record is kept while the Automaton stays failed or blocked, refreshed with each pass, and gone once it leaves that state. `first_seen_at` is taken from the Automaton's own status timestamp, so it survives refreshes. The failed-task error and the session end reason are each truncated to 2000 chars by the pass.

**The triage alert.** Once a record has been sitting failed or blocked for `autonomous.automation.triage_alert_minutes` (default 30; `0` turns the alert off), the engine raises one triage alert per episode through the existing lifecycle alert sink (the same path the lane-fill idle alert uses). The alert names the Automaton, its Automation, the state, the failed task (if any), how long it has been sitting, and the retry budgets, and states plainly that the Automaton **is failed (or blocked) and waiting for operator triage** — triage is a read for the operator, not a re-runner. The alert is audited and stamped on the record as `alerted_at`.

**The one key it has.**

| Key | Meaning | Default | Allowed |
|---|---|---|---|
| `autonomous.automation.triage_alert_minutes` | How long a failed/blocked record sits before the triage alert fires; `0` turns the alert off | 30 | whole number ≥ 0 (negative or non-whole values refused) |

Keep `30` for the default half-hour; set `0` to keep the records and the pass but silence the alert. The live value is also what the read body returns as `alert_minutes`, so every surface shows the threshold actually in force. Set it with `datawatch config set autonomous.automation.triage_alert_minutes 30`, `PUT /api/config` with the key, or comm `configure autonomous.automation.triage_alert_minutes=0`. The value is read live; the change applies to the next pass.

**What a record contains.**

| Part | Source |
|---|---|
| Identity | `prd_id` and `prd_title` (when it has one), `automation_id`, `automation_name`, `member_id`, `status` (`failed`\|`blocked`) |
| Failed task | `task_id`, `task_title` (when one exists), `error` (truncated), `blocked_reason` (the Automaton's own block reason), plus `session_end_reason` (truncated) |
| Timing | `first_seen_at` (the Automaton's own status timestamp), `updated_at` (last pass refresh), `alerted_at` (set when the alert fires). `alert_minutes` is on the read body beside `records`, not on the record |
| Retry budget | The engine's existing budget for all four categories — `infra`, `content`, `scope`, `policy`, each with `limit`, `used`, `left` — plus `ceiling_total` and `ceiling_left`, the overall auto-retry ceiling when one is set (0 when there is none). **Informational only**: the pass neither spends nor resets any of it. |
| Notes | An append-only list (`at`, `source`, `text`) grown over the life of the record |

**Surfaces.**

| Surface | Entry point |
|---|---|
| REST | `GET /api/autonomous/triage` (filters: `automation_id`, `prd_id`, `status=failed\|blocked`) |
| MCP | `autonomous_triage` (same filters; proxies the read route; federated peers need the autonomous read capability) |
| CLI | `datawatch autonomous triage [--automation <id>] [--prd <id>] [--status failed\|blocked] [--json]` |
| Comm channel | `autonomous triage [automation-id]` (one optional positional argument, the automation id) |
| PWA | Triage card in the Automaton detail view (reloads with the view) |
| Mobile/wear | The same REST contract, documented in `docs/api/mobile-surface.md` |

A read that matches nothing is an empty list, not an error. The full field documentation and JSON shape are in `docs/api/autonomous.md` (section *Triage records*); the schema is `TriageList`/`TriageRecord` in the OpenAPI spec (both files carry it).

**What triage never does.** It never starts or restarts anything: the pass is a reader, and the alert names what failed and what is left, then stops. It never touches the retry budget, which it only reports. It never edits a task, a spec or a status — no task reset, no status flip, no spec rewrite. It never alerts twice for the same episode and never alerts at all when the key is `0`. And it never invents a second budget or alert system: the alert reuses the existing lifecycle alert sink, and the only knob is `autonomous.automation.triage_alert_minutes`.

---

## Merge-pending check (finished Automata whose branch is not on main)

When an Automaton finishes, its work lives on its own branch (`automaton/<id>`) in the worktree base repository (`worktree_base_repo`). Nothing about finishing puts that work on `main`, so a completed Automaton can look done while its commits are still only on the branch. The merge-pending check compares each completed Automaton's branch with `main` so an operator can see which finished Automata are waiting to be merged, from every surface at once. The check is a **read**: it never merges, deletes or pushes anything, and it is **off by default**.

**The five states.** The check asks git, read-only, how many commits the branch has that `main` does not (`git rev-list --count main..branch`), whether each of those commits' patches is already on `main` (`git cherry`), and whether each commit's subject line appears in `main`'s log. It then reports one state:

| State | Meaning | Record and alert |
|---|---|---|
| `merged` | Every commit on the branch has its patch and its subject line on `main` | None |
| `pending` | At least one commit's patch is not on `main` and at least one subject line is not on `main` either: the work has not been merged | Merge-pending record and, after the threshold, one alert (`on` mode only) |
| `needs_look` | The patch and subject checks disagree, so the check cannot say whether the work landed | None. A prompt for the operator to compare the branch by hand |
| `empty` | The branch has no commits that are not on `main` (nothing ahead, or already contained in `main`) | None |
| `unknown` | The branch could not be compared: no `worktree_base_repo` is configured, the branch or `main` is missing, or git failed or timed out | None. The previous state is left alone and the reason is logged once per distinct failure |

**The needs-a-look caveat.** A commit whose patch changed while it was merged shows as `needs_look`, not as `pending`. The check identifies merged work by matching a commit's patch **and** its subject line against `main`. Suppose a commit was merged, but the patch that landed differs from the one on the branch, for example because a conflict was fixed by hand or the commit was edited on the way in. Its subject line is on `main` but its patch is not, which is exactly the case the check cannot settle either way, so it reports `needs_look` instead of calling the work unmerged. The opposite mismatch (every patch is on `main` but a subject line was reworded) is also `needs_look`. Treat `needs_look` as a request to compare the branch and `main` yourself; the check raises no alert for it. The cost of this choice is that a branch whose work really is unmerged, but which reuses subject lines already on `main`, is also shown as `needs_look`.

**The modes.** `autonomous.automation.merge_pending_mode` takes three values:

| Value | What it does |
|---|---|
| `off` (default) | The check does not run. Git is never called, nothing is recorded, and the merge reads return empty lists. A missing key reads as `off`. |
| `shadow` | The check runs on every coordinator tick and only records what it would have done. No merge-pending record is written, no alert is raised, and a leftover merge-pending record is cleared. |
| `on` | The check runs, a `pending` Automaton gets a merge-pending record (shown as a `merge_pending` status in its triage record, with the branch and `commits_ahead`), and one alert is raised per episode after the threshold. |

**Shadow mode and what it records.** Shadow mode is the way to find out how the check judges your own branches before it can alert anyone. On each tick, for every completed Automaton with a work branch, the check writes one shadow decision: the Automaton id and name, the branch, the state, `commits_ahead`, and `would_alert`, which says whether an alert would have fired by now given `merge_pending_alert_minutes` (always false when that is `0`). A decision identical to the newest one for the same Automaton is not written again; it only raises that entry's `repeat_count` and moves `last_seen_at`, so a quiet poll adds no new rows. The history is bounded at 1000 entries, newest first, and is kept in `merge_shadow_decisions.json` beside the other autonomous stores, so it survives a daemon restart. Read it with the merge read (`shadow_decisions`). Outside shadow mode the list is returned empty. Leave shadow on for as long as you need to trust the states, then switch to `on`; the switch applies on the next tick.

**The alert and when it fires.** In `on` mode, when a finished Automaton is `pending`, the check writes a merge-pending record stamped with the time it was first seen. Once that record has stood for `autonomous.automation.merge_pending_alert_minutes` (default 30), one `warn` alert is raised through the existing alert sink, the same path the triage alert uses. The alert is titled "Automaton `<name>` merge pending" and its body names the Automaton and its id, the work branch, how many commits are not on `main`, the Automation it is a member of (when it is), how long it has been waiting, and states that nothing has been merged, deleted or pushed. The time of the alert is stamped on the record (`alerted_at`). It fires **once per episode**: it does not repeat while the Automaton stays pending. An episode ends when the branch leaves `pending`, whether it was merged (`merged`) or ended up `empty` or `needs_look`; the record is then cleared and a later pending episode alerts again. `0` turns the alert off: the record is still written and shown, but no alert is ever raised. A branch that is `unknown` changes nothing, neither starting nor ending an episode.

| Key | Meaning | Default | Allowed |
|---|---|---|---|
| `autonomous.automation.merge_pending_mode` | Whether the check is off, records only (shadow) or on | `off` | `off`, `shadow`, `on` (an empty string or any other word is refused on every write surface) |
| `autonomous.automation.merge_pending_alert_minutes` | How long a pending branch may wait before the one alert | 30 | whole number ≥ 0 (`0` = alert off; negative or non-whole values refused) |

Set them with `datawatch config set autonomous.automation.merge_pending_mode shadow`, `PUT /api/config` with the key, comm `configure autonomous.automation.merge_pending_mode=shadow`, the MCP tool `autonomous_config_set`, or the PWA Settings fields. They are read live; a change applies to the next tick, with no restart. The check needs `worktree_base_repo` to be set: without it every branch is `unknown`.

**The safe-to-delete report.** The report answers a different question: which Automata work branches and worktrees can be cleaned up. It looks at every local `automaton/*` branch in the worktree base repository (the base branch and the branch checked out in the main checkout are skipped) and sorts each into one of two lists:

| List | Which branches | Fields |
|---|---|---|
| `safe` | State `merged` (reason "all commits on main") or `empty` (reason "no commits ahead of main"), and the worktree, if any, has no uncommitted changes | `branch`, `worktree` (empty when none), `reason`, `commits` |
| `held` | State `pending`, `needs_look` or `unknown`; or a `merged` or `empty` branch whose worktree has uncommitted changes (`dirty`) | `branch`, `worktree`, `state`, `dirty`, `note` |

`held_dirty` counts the branches that are safe by state but held back only because of uncommitted changes in their worktree. Unlike the merge status, the report runs git when it is read (it is not served from the daemon's memory) and works in every mode, including `off`; it needs only `worktree_base_repo`. **The report never deletes anything.** It does not delete a branch, remove a worktree, push, reset, check out or merge, and there is no surface that acts on it. Cleaning up a branch or worktree is a separate step: you carry it out yourself, or you switch on the Automaton worktree cleanup described in the next section, which applies a retention window and a per-pass limit on top of this report. Read `safe` as "the check found nothing to lose", not as an instruction, and use `needs_look` entries in `held` as the reminder to look first.

**Surfaces.**

| Surface | Merge state | Safe-to-delete report |
|---|---|---|
| REST | `GET /api/autonomous/merge` | `GET /api/autonomous/safe-to-delete` |
| MCP | `autonomous_merge` | `autonomous_safe_to_delete` |
| CLI | `datawatch autonomous merge [--json]` | `datawatch autonomous safe-to-delete [--json]` |
| Comm channel | `autonomous merge` | `autonomous safe-to-delete` |
| PWA | Merge badge on a finished Automaton's detail view and on its Automation card (`pending` and `needs_look` only) | Safe-to-delete card on the Automation Overview sub-tab, refreshed every 60 seconds |
| Mobile/wear | The same REST contract, documented in `docs/api/mobile-surface.md` | Same |

Both reads are GET-only (any other method is 405), answer 503 while Automata are disabled, and need the autonomous read capability when called by a federated peer. The merge state is served from the daemon's last comparison, so a read never runs git and an Automaton not compared yet reads `unknown`. The full field documentation and JSON shapes are in `docs/api/autonomous.md` (section *Merge state and safe-to-delete report*), and the walk-through for reading them is in `docs/howto/automation-tab.md`.

**What the merge-pending check never does.** It never merges a branch, deletes a branch, removes a worktree, pushes, resets or checks anything out: every git command it issues is a read. It never alerts in `off` or `shadow` mode, never alerts twice for one episode, and never alerts for `merged`, `empty`, `needs_look` or `unknown`. It never changes an Automaton's own status or fields; the only thing it writes is the merge-pending record in `on` mode and the shadow history in `shadow` mode.

---

## Automaton worktree cleanup (finished work branches, worktrees and daemon temp files)

An Automaton that runs in worktree mode works on its own branch (`automaton/<id>`) in its own worktree under the worktree base repository (`autonomous.worktree_base_repo`), and its worker sessions leave scratch files in the daemon's temporary directory. The cleanup removes what a finished Automaton no longer needs, and only what it is certain it can lose nothing by removing, and watches the disks involved. It is not tied to one repository: it reads the worktree base repository from the config, takes the branch an Automaton's work must be on from that Automaton's own base branch or, failing that, from the default branch the remote records for the repository (`origin/HEAD`), and works for a project whose default branch is `master`, `trunk` or any other name. The operator guide is [howto/automaton-worktree-cleanup.md](howto/automaton-worktree-cleanup.md).

**Two passes.** The **worktrees** pass removes a finished (completed or cancelled) Automaton's work branch and worktree when it ends and again on the coordinator's periodic sweep. The **temp files** pass removes the scratch files and directories the daemon's own worker sessions created in the temporary directory, when the session ends and again on a periodic sweep. The removal at the end of the Automaton or session is the **owner** path; the sweep is the **net**. The status keeps the two apart so you can see whether the net finds real work.

**The safety criteria (worktrees).** An entry is removed only if all of these hold, each re-checked at removal time: the Automaton is completed or cancelled in the store now; no live session or live lease uses the worktree; the worktree has no uncommitted changes; every commit on the branch is on the base branch (and if the base branch cannot be worked out, nothing is removed); the branch is at least `cleanup_retention_hours` old (an unknown age counts as young; the release an Automaton makes when it completes or is cancelled skips only this age check, because the age counts from that moment); and it is neither the main checkout nor the base branch. Only branches in the `automaton/` namespace are considered. Removal is `git worktree remove` without `--force` then `git branch -d`, never `-D`; a refusal leaves the entry in place and records the error. Every removal is written to the Automaton's triage record and the audit log.

**The safety criteria (temp files).** A path is removed only if its name starts with `datawatch-` and sits inside the temporary directory, the daemon recorded creating that exact path for a session, that session is known not to be alive, and it ended at least `cleanup_retention_hours` ago. Unrecorded prefixed names are skipped as foreign, an unknown session counts as alive, symbolic links are never followed or removed, and the record of created paths is in memory, so scratch from an earlier daemon process is left alone.

**The modes.** `autonomous.automation.cleanup_mode`:

| Value | What it does |
|---|---|
| `off` | No cleanup git command is run and the temporary directory is not scanned. The disk check still runs. |
| `report` (default) | Every check runs and each pass records what it would remove; nothing is removed. An unset key and any unrecognized value read as `report`. |
| `on` | The passes remove what passes every check, at most `cleanup_max_removals_per_pass` per pass. |

| Key | Meaning | Default | Allowed |
|---|---|---|---|
| `autonomous.automation.cleanup_mode` | Whether the cleanup is off, reports only or removes | `report` | `off`, `report`, `on` (anything else is refused on every write surface) |
| `autonomous.automation.cleanup_retention_hours` | How long finished work must have sat before it can be removed | 24 | whole number ≥ 1 |
| `autonomous.automation.cleanup_max_removals_per_pass` | The most entries one pass removes; the rest wait for the next pass and count as would-remove | 10 | whole number ≥ 1 |
| `autonomous.automation.disk_alert_percent` | Disk usage percent that raises the disk alert | 85 | whole number 1–100 |

Set them with `datawatch config set autonomous.automation.cleanup_mode on`, `PUT /api/config` or `PUT /api/autonomous/config`, comm `configure autonomous.automation.cleanup_mode=on`, the MCP tool `config_set`, or the PWA Settings fields. They are read live; a change applies on the next poll with no restart.

**The disk alert.** The daemon reads the usage of the disk holding the worktree base repository and of the disk holding the daemon's temporary directory on start and on every coordinator poll, in every mode including `off`. While either is at or above `disk_alert_percent` it raises one warning alert, at most once an hour, that names the disk and its usage. The time of the last alert is kept across restarts. The alert never removes anything. A disk that cannot be measured has no reading and cannot alert.

**Status.** One read on every surface:

| Surface | How |
|---|---|
| REST | `GET /api/autonomous/cleanup` |
| MCP | `autonomous_cleanup_status` |
| CLI | `datawatch autonomous cleanup [--json]` |
| Comm channel | `autonomous cleanup` |
| PWA | **Cleanup** card on the Automation Overview sub-tab, refreshed every 30 seconds |
| Mobile/wear | The same REST contract, described in `docs/api/openapi.yaml` |

The read is GET-only (any other method is 405), answers 503 while Automata are disabled, and needs the autonomous read capability when called by a federated peer. It serves the daemon's in-memory status, so a read never runs git or scans a disk; the status is also saved to `cleanup_status.json` beside the other autonomous stores so the last run survives a restart. Each pass reports its `mode`, `last_run`, `removed`, `would_remove`, `skipped`, `failed`, `bytes_freed`, `last_error` and the owner-versus-net split; the status adds the two disk readings, `disk_alert_percent`, `disk_alert` and `last_disk_alert_at`. The full field documentation is in `docs/api/openapi.yaml`.

**Moving from report to on.** Run in `report` until a few finished Automata have gone through; read `would_remove` and `skipped` and the safe-to-delete report above to confirm the pass would remove only what you are happy to lose; keep the per-pass limit small for the first `on` passes; then set the mode to `on`. Nothing removed comes back, so the checks come first.

**What the cleanup never does.** It never removes a branch with commits that are not on the base branch, a worktree with uncommitted changes, anything belonging to a running or unfinished Automaton, a branch outside `automaton/`, or a path outside the daemon's own scratch files. It never uses `--force` or `-D`, never follows a symbolic link, and never touches anything in `off` mode.

---

## Merged-member policy (a member Automaton cancelled after its work shipped)

When a member Automaton of an Automation ends cancelled, the Automation has no `on_complete` edge to follow and becomes `blocked`. That is right when the member's work never landed and wrong when it did: the member's branch (`automaton/<id>`) is already merged into the project's default branch and only the Automaton was cancelled afterwards. The merged-member policy decides what the Automation does in that case. When a member ends cancelled or completed, the coordinator asks read-only git whether the member's branch is merged on the default branch, and if it is, `autonomous.automation.merged_member_policy` chooses the outcome. A branch that is not merged, has no commits ahead of the default branch, or whose state git cannot read always keeps the blocked outcome, whatever the policy. The check never merges, deletes, pushes or checks out a branch.

**The three values.**

| Value | What it does |
|---|---|
| `completed` (default) | The member counts as completed and the Automation follows its `on_complete` edge. One info alert, "Automaton advanced on a merged member", names the Automaton, the member, the branch and the proving commit |
| `manual` | The Automation stays blocked, as before, and the block reason carries the merge proof so the operator can resume it or skip the member |
| `block` | Exactly the behaviour before the key existed: the Automation stays blocked and no git runs |

| Key | Meaning | Default | Allowed |
|---|---|---|---|
| `autonomous.automation.merged_member_policy` | What an Automation does when a cancelled member's work is already merged | `completed` | `completed`, `manual`, `block` (an empty string or any other word is refused on every write surface; an absent key reads as `completed`) |

Set it with `datawatch config set autonomous.automation.merged_member_policy manual`, `PUT /api/config` with the key, comm `configure autonomous.automation.merged_member_policy=manual`, the MCP tool `autonomous_config_set` (`automation_merged_member_policy`), or the PWA field **Automaton merged-member policy** under Settings, General, Automate. The key is read live; a change applies to the next member that ends, with no restart. The proof needs `worktree_base_repo` to be set: without it nothing can be proved and every cancelled member keeps the blocked outcome.

**The project's own default branch.** The proof does not assume `main`. It resolves the default branch per project from git: the symbolic ref of the base repository's remote `HEAD` (`refs/remotes/origin/HEAD`), and, if that gives no answer, the branch currently checked out in the base repository. A slash-named default branch such as `release/1.0` is kept whole. If neither answers, the state is unknown and the Automation stays blocked. The comparison itself is the one the merge-pending check uses (patch match through `git cherry` plus a commit-subject check), and the proving commit is the branch tip when the tip is on the default branch, otherwise the default-branch commit that carries the same patch as the tip.

**What is recorded.** Each time the proof took part in a member's end, the Automation's decision log gets one `merged_member` entry and the audit trail gets one `automation_merged_member` entry. Both carry the member, the branch, the default branch, the proving commit, the policy and the outcome: `advanced` (the Automation moved on) or `blocked` (it stayed for the operator).

**Reopening a member.** Running a cancelled member again while the Automation is blocked on it because of that cancellation resumes the Automation: the member goes back to `running`, the Automation back to `active`, and a `reopened` decision is recorded. A reopened member never revives an Automation that is cancelled, completed, paused or blocked for another reason.

**Surfaces.**

| Surface | Merged-member decisions |
|---|---|
| REST | `GET /api/automations/merged-members` (every Automation), `GET /api/automations/{id}/merged-members` (one) |
| MCP | `automation_merged_members` (optional `id`) |
| CLI | `datawatch autonomous merged-members [automation-id] [--json]` |
| Comm channel | `autonomous merged-members [automation-id]` (newest first, at most ten lines) |
| PWA | **Automaton merged-member decisions** block on the Automation detail view, and a one-line entry in its decision log |
| Mobile/wear | The same REST contract |

The reads are GET-only (any other method is 405), answer 503 while Automata are disabled, and need the autonomous read capability when called by a federated peer. The full field documentation is in `docs/api/autonomous.md` (section *Merged-member decisions*), the walk-through is in `docs/howto/automation-merged-member.md`, and the decision as a diagram is in `docs/flow/automation-merged-member-flow.md`.

**What the merged-member policy never does.** It never merges a branch, deletes a branch, removes a worktree, pushes, resets or checks anything out: every git command it issues is a read. It never changes the outcome for a branch that is not proved merged. It never runs git at all under `block`.

---

## Automata progress dashboard

A read-only view of how far the Automata have got and roughly when the rest
will be done: counts by kind (feature, bug, other), throughput and worker
utilization per **active** day, lead time, the work left split into **known**
(Automata that exist) and **estimated** (backlog rows not filed yet, Automata
with no tasks yet), and a completion range in active days with the method behind
each figure. Stretches with no activity are left out of every rate and listed,
never hidden. Lead time is shown but never feeds an estimate. Anything that
could not be measured reads *not measured*, not zero. Operator guide:
[howto/automata-progress.md](howto/automata-progress.md).

**Inputs.** The Automata store; the backlog table (`autonomous.progress.backlog_path`,
default `docs/plans/README.md`), where `{kind=feature|bug|other}` and
`{phases=N}` in an item cell set a row's kind and how many Automata it becomes;
and, optionally, `operator-wait-windows.tsv` in the same folder as the backlog
file (tab separated: start, end, kind, reason) for stretches to leave out. A
missing backlog or waits file is not an error.

**Data written.** `progress-snapshots.jsonl` in the Automata data directory, one
snapshot a day (UTC) plus one each time an Automaton completes, pruned after
`autonomous.progress.snapshot_retention_days`. Back it up with the other
Automata files if you want the trend history kept.

**The five access methods.**

| Task | YAML | CLI | Web UI | REST | Chat |
|---|---|---|---|---|---|
| Read the numbers | n/a | `datawatch autonomous progress [--automation ID] [--tag ID] [--since DATE] [--until DATE] [--json]` | Automata tab, *Progress* card | `GET /api/autonomous/progress` | `progress [automation=ID] [tag=ID] [since=DATE] [until=DATE]` |
| Read the trend | n/a | `datawatch autonomous progress-trend [--days N] [--json]` | Trend section of the card | `GET /api/autonomous/progress/trend?days=N` | `progress trend [days]` |
| Set an Automaton kind | n/a | `datawatch autonomous prd-set-kind ID KIND`, `prd-create --kind KIND` | Automaton settings, *Kind* | `POST /api/autonomous/prds/{id}/set_kind` | `autonomous set-kind ID KIND` |
| Change a setting | `autonomous.progress.*` | `datawatch config set autonomous.progress.lanes 5` | Settings, General, Autonomous | `PUT /api/config` | `configure autonomous.progress.lanes=5` |

MCP agents use `autonomous_progress`, `autonomous_progress_trend` (both
read-only), `autonomous_prd_set_kind`, and `config_set`. All surfaces return or
print the same object. The settings keys and their defaults are in the table
under *Progress engine settings* in the backup section above and in
`docs/config-reference.yaml`.

**Where to see it.**

| Where | What |
|---|---|
| Web UI, Automata tab | The *Progress* card; shows the last good data with a stale notice when a refresh fails. |
| CLI and chat | The status table as text. |
| REST and MCP | The JSON object, for dashboards and scripts. |
| Android and iOS | Not yet. |

---

## Operator questions

A channel for the operator's own decisions that the Automata and the rest of the
system should be able to point at: one operator question is one decision,
recorded when it is raised and kept until it is answered or withdrawn — there
is no deadline. Raising never blocks the asker. An answer is **immutable**: a
changed mind is a new answer that supersedes the old one, and both stay in the
log. The answer quote is stored exactly as received, with its SHA-256
recorded alongside, so a decision can always be shown back word-for-word with
proof of the bytes. API reference: [api/autonomous](api/autonomous.md#operator-question-channel).

**Enabled by config.** Off by default behind `autonomous.questions.enabled`.
All six endpoints answer `503` naming the switch while it is off; the switch
is read live on every request, so `PUT /api/config` turns the channel on or
off without a restart. While the switch is off, none of the notification
routes below fire at all — no WebSocket message, no messaging line, no
alert, no event, no session line.

**Notifications (step 2 of the channel).** Once the switch is on, three
optional toggles decide what the operator hears (all three default on when
the key is absent — the keys are `autonomous.questions.notify_messaging`,
`autonomous.questions.notify_alert` and `autonomous.questions.return_path`,
all readable/writable through the same config routes as the switch):

| Toggle | Default | When it fires | What lands |
|---|---|---|---|
| `notify_messaging` | on | every raise | one line in each active messaging group (the `[hostname] question <id> — <label>`, a `(high priority)` marker, the text, the numbered options, and the REST + PWA answer routes); a missing comm backend is a silent no-op |
| `notify_alert` | on | every raise **with `priority: high`** | one warning-level alert in `GET /api/alerts`, titled `Operator question raised (high priority): <id> — <label>`; normal-priority raises never alert |
| `return_path` | on | an answer on a question raised with `return_path: true` (and a `session_id`) | exactly one `DATAWATCH_ANSWER: {…}` line delivered into that session through its input path — the JSON carries the question id, answer id, chosen option, the verbatim quote (JSON-escaped, never multi-line), actor, timestamp. The payload is exactly these six fields — `question_id`, `answer_id`, `chosen_option`, `quote`, `actor`, `at` — and the line is fired exactly once per answer (a superseding answer fires its own). |

The WebSocket `question_raised` message to connected PWA/hub clients needs
no toggle — it rides the always-on hub once the raise is stored. The
**default return path is always the events-plus-polling path**: the
`question_raised` message, the lifecycle events `question.answered` and
`question.withdrawn` (in the closed kinds set, so a lifecycle subscription
with `filter.kinds` may name them — exactly-once delivery, re-fired at
startup for any recorded-but-never-delivered event), and polling
`GET /api/questions/{id}`, which answers `answer_count`,
`current_answer_id` and the standing answer's summary (verbatim
quote, actor, surface) in one read. The session line above is an
**opt-in per question** on top of that, not the default: a question raised
without `return_path: true` (or a daemon with no router wired for the line)
never gets it, and a lost delivery is one daemon warning, never a retry.
How-to: [howto/operator-questions-notifications](howto/operator-questions-notifications.md).
**Answering (this step):** open the queue, pick an option or type your words, and read the record back — from every surface: [howto/operator-questions-answering](howto/operator-questions-answering.md).

**Body caps.** Raising: 64 KiB. Answer: 64 KiB plus a 16 KiB quote cap.

**Secrets.** An answer whose text looks like a secret is refused `422`, is
never stored, and the response never echoes any part of the submitted text —
`error` is the fixed phrase `matched: secret pattern` (not the name of the
specific rule that matched) and `suggestion` names the two safe routes (the
datawatch secrets store via `${secret:name}`, or the filesystem). The matched
rule's name goes into the `question_answer_refused` audit entry instead — and
the submitted text goes nowhere (not the response, not the audit log, not any
session log). Well-formed `${secret:name}` references pass the scan.

**Six endpoints, four verbs + one switch.**

| Endpoint | What it does |
|---|---|
| `POST /api/questions` | Raise a question: `text`, 2 to 20 options with unique ids, a `default` (an option or `hold`), a `context` (`none`, `prd`, `task`, `proposal`, `registry_row`) with its matching link; `asker_kind` defaults to `operator` |
| `GET /api/questions` | List newest first: `?state=`, `?context=`, `?asker=` (exact on `asked_by`), `?text=` (case-insensitive substring over question text, option labels and tradeoffs, and answer quotes), `?limit=` (default 100, capped 1000) |
| `GET /api/questions/{id}` | One question (state, `priority`, `return_path` on the record) + `answer_count` + `current_answer_id` + the standing answer's summary (`answer`, or `null` while it has no answer) — the asker's polling read |
| `POST /api/questions/{id}/answer` | Record an answer: `chosen_option_id` and/or a non-blank `quote`; `supersedes` for a changed mind; `surface` (`pwa`/`android`/`ios`/`cli`/`rest`/`mcp`/`comm:<channel>`), `source_message_id`, `actor` |
| `POST /api/questions/{id}/withdraw` | Withdraw an open question (`reason`, `actor`); `409` when it has already moved on |
| `GET /api/questions/{id}/answers` | The full answer log, oldest first, each answer with its derived superseded flags, plus `current_answer_id` |

**The access methods.**

| Task | YAML | REST | MCP | CLI / comm | PWA / Android / iOS |
|---|---|---|---|---|---|
| Toggle the channel | `autonomous.questions.enabled` | `PUT /api/config` | `config_set` (the generic config tool — wired; round-trip pinned in `internal/mcp/questions_config_surface_test.go`) | `datawatch config set autonomous.questions.enabled true\|false` (wired; round-trip pinned in `cmd/datawatch/questions_config_surface_test.go`); comm `configure autonomous.questions.enabled=true` (wired — the key is in the comm `configure` key set) | Settings → General toggle, labeled in all five locales (`settings_automata_questions_enabled`), round-trip covered by `internal/server/questions_settings_locale_test.go` |
| Raise | n/a | `POST /api/questions` | `question_raise` (registered) | `datawatch question raise --text … --option id=label …` (no comm raise verb yet) | the Automation tab's queue card lists open questions; the raise form is not in the PWA or the companion apps yet |
| List / read | n/a | `GET /api/questions`, `GET /api/questions/{id}` | `question_list`, `question_get` | `datawatch question list …`, `datawatch question show <id>`; comm `question list`, `question show <id>` | the Automation tab's queue card, with search |
| Answer | n/a | `POST /api/questions/{id}/answer` | `question_answer` | `datawatch question answer <id> --option <id> [--text "…"]`; comm `question answer <id> <N\|option id> [quote…]` (and the bare-reply rule when exactly one question is open) | **Answer** on a row — pick an option or type the quote — in the Automation tab's queue card |
| Withdraw | n/a | `POST /api/questions/{id}/withdraw` | `question_withdraw` | `datawatch question withdraw <id> --reason …` (no comm withdraw verb yet) | not in the PWA or the companion apps yet — CLI / MCP / REST |
| Answer log | n/a | `GET /api/questions/{id}/answers` | `question_get` (the standing answer's summary); the full log is the REST route | `datawatch question show <id>` (the standing answer); the full log is the REST route | the queue card's **History** — full log with the **Current** / **Superseded** markers |

Parity, per AGENT.md's rule, per cell above: the switch, the six REST routes and
the PWA queue card (Automation tab: queue search, answer sheet, history with the
answer log) are wired; the MCP question tools (`question_raise`, `question_list`,
`question_get`, `question_answer`, `question_withdraw` — mappings in
[api-mcp-mapping](api-mcp-mapping.md#operator-questions)) and the CLI / comm
verbs (`datawatch question …`; comm `question list` / `show` / `answer` + the
bare-reply rule) are wired too. The companion apps (Android / iOS) still owe the
queue view (list, raise, answer) — tracked in
[parity-status](parity-status.md) ([datawatch-app#283](https://github.com/dmz006/datawatch-app/issues/283)).

**Answering by surface** — the queue, the options, your words, and the record:

| Surface | How you answer |
|---|---|
| **PWA** | Automation tab → *Operator questions* card → **Answer** on a row — pick an option or type your words in the quote field → submit; **History** for the answer log (the earlier answers stay, marked *Superseded by …*) |
| **CLI** | `datawatch question answer <id> --option <id> [--text "…"]`; `datawatch question withdraw <id> --reason …` removes an open question |
| **MCP** | `question_answer` — `chosen_option_id` (or `null`) and/or `quote`; `question_withdraw` while it is still open |
| **Comm channel** | `question answer <id> <option number or id> [quote…]`; with exactly one open question a bare reply also works (first word the option number, or the whole message an exact option label) |
| **REST API** | `POST /api/questions/{id}/answer` with `{"chosen_option_id": "proceed", "quote": "…"}` |

**Access control (federation).** Reads need the autonomous read capability
(`autonomous:read`); raising, answering and withdrawing need the autonomous
write capability (`autonomous:write`). A federated peer without the
capability gets `403`; the capability rows are `fedCap` in the peer definition.

**Storage.** One append-only JSON-lines file per host, `questions.jsonl` under
the data directory (questions, answers and withdrawals as separate lines). It
never loses or rewrites a line — superseded answers are derived on read from
the log, not deleted. Back it up with the rest of the data directory.

**Audit.** Every mutation is an audit event, all keyed on the question id and
kept forever: `question_raised`; `question_answered` (the quote verbatim,
with its SHA-256); `answer_superseded` (a later answer replaced an earlier
one — both quotes kept verbatim); `question_withdrawn`; `question_expired`
(a reserved state for external question sources that have deadlines — this
channel has none, so it never fires here); and `question_answer_refused`
(a secret refusal — the name of the matched rule only, never any part of the
answer text). A crash or restart is recovered by replaying the append-only
JSONL store, which is the store's own mechanism, not a separate audit entry.
See the audit section of the API reference above.

**Where to see it.**

| Where | What |
|---|---|
| REST | The six endpoints above — every state, the whole answer log, the same JSON for every caller. |
| Config | The one switch (`autonomous.questions.enabled`), via YAML, `PUT /api/config`, MCP `config_set` (round-trip pinned in `internal/mcp/questions_config_surface_test.go`), `datawatch config set` (round-trip pinned in `cmd/datawatch/questions_config_surface_test.go`), comm `configure`, or the PWA Settings → General toggle (all five locales). |
| MCP / CLI / comm | The switch and the verbs both work there: MCP `config_set` plus the `question_*` tools, `datawatch question list \| show \| answer \| withdraw \| raise` and `datawatch config set`, and comm `question list / show / answer` (plus the bare-reply rule) and `configure` — all on the same object. |
| Android and iOS | Not yet — the queue view (list, raise, answer) is not built in the companion apps yet. Tracked in [parity-status](parity-status.md) and [datawatch-app#283](https://github.com/dmz006/datawatch-app/issues/283). |

**Monitoring (B7), n/a per item, with reasons.** This step's parity line is
config + REST only (step 1 of the plan), so there is no channel `SystemStats`
field, no `GET /api/<subsystem>/stats` endpoint for it, no channel stats MCP
tool, no Monitor card, and no comm stats command. In the meantime, every
mutation lands in the audit log under its question CEF action
(`question_raised`, `question_answered`, `answer_superseded`,
`question_withdrawn`, `question_answer_refused`), and queue and state counts
are queryable through `GET /api/questions` with its `state`, `context`,
`asker` and `limit` filters. The queue view and the surface verbs shipped
with this channel (see the answering how-to above); what is deferred here is
the channel's own statistics wiring, and that is the reason recorded for
deferring monitoring.

---

## Decision gate

A task whose spec names an [operator decision](howto/automaton-operator-decisions.md) id (`q-` followed by at least 20 lowercase hex characters) does not start until that decision is answered. The gate is the check that does this. It reads the question store, so it only sees decisions raised through the [operator question channel](#operator-questions): keep `autonomous.questions.enabled` on whenever the gate is on. How-to for operators: [howto/automaton-operator-decisions.md](howto/automaton-operator-decisions.md). API reference: [api/autonomous](api/autonomous.md#operator-decisions-get-apiautonomousdecisions).

**Holds.** Before a pending or queued task starts, the gate checks each decision id the spec names. An answered decision passes. An open one holds the task. A withdrawn or expired one holds it too, since it has no answer, and so does an id the store does not know. A held task has the status `waiting_decision` and a `wait_reason` sentence naming the decision (for an open one, with the first 160 characters of the question). It has not started and is not counted as a failure or a retry. Tasks that depend on it wait behind it, and the Automaton stays running while it waits. An answer wakes the Automaton at once; a 30-second re-check covers a withdrawal, a cancelled task and an answer that lands between the check and the wait. When every decision the spec names is answered the task goes back to `pending` by itself and `wait_reason` clears. Turning the gate off, or setting the mode to `shadow`, also sends a held task back to `pending` on the next check. A task whose spec names no decision is never affected, and the separate text `HOLD` line rule is untouched.

**Shadow mode.** The gate ships in `shadow`: it works out which tasks it would hold and holds none. For each such task it adds one `decision_gate_shadow` entry to the Automaton's decision log (note: `would hold: <reason> (task <id>)`), once per task and reason, however many times it checks. Shadow mode raises no alert and changes no task status. Read the entries on `GET /api/autonomous/prds/{id}` (the `decisions` array) or on the Decisions tab of the Automaton, and switch the mode to `on` when they match what you expect. Going back to `shadow` is the way to stop all holds without turning the gate off.

**The alert.** When a task first enters the hold (mode `on` only) the daemon raises one warning alert titled `Automaton task waiting for a decision`. The body names the Automaton (id and title), the task (id and title), the reason, and says the task starts by itself once the decision is answered. It goes through the same alert sink as the other Automaton alerts, so it reaches the alert list, connected clients and the configured routes. It is raised when the task enters the hold, not on each later check and not when the reason text changes while the task stays held; a task released and held again raises it again. Nothing is raised when no alert sink is wired.

**Config.** Two keys under `autonomous.questions`, read at the moment a task is about to start, so a change needs no restart:

| Key | Default | Allowed |
|---|---|---|
| `autonomous.questions.decision_gate` | on when absent | `true` or `false` |
| `autonomous.questions.decision_gate_mode` | `shadow` when empty | `shadow` or `on`; anything else is refused with 400 and nothing changes |

| Method | How |
|--------|-----|
| **YAML** | `~/.datawatch/config.yaml` → `autonomous:` → `questions:` → `decision_gate` and `decision_gate_mode` |
| **CLI** | `datawatch config set autonomous.questions.decision_gate_mode on`; `datawatch config set autonomous.questions.decision_gate false` |
| **Web UI** | Settings → **Autonomous Automata planning** → *Hold a task on an unanswered decision it names* and *Decision gate mode* |
| **REST API** | `PUT /api/config` with `{"autonomous.questions.decision_gate_mode": "on"}` or `{"autonomous.questions.decision_gate": false}` |
| **Comm channel** | `configure autonomous.questions.decision_gate_mode=on` |
| **MCP** | `config_set` with the key and value |

**Where to see it.**

| Location | What you see |
|----------|-------------|
| **Web UI** | the Decisions card on the Automaton detail; *Waiting for a decision* and the reason under a held task; the alert |
| **REST API** | `GET /api/autonomous/decisions`; `status` and `wait_reason` per task on `GET /api/autonomous/prds/{id}`; `decision_gate_shadow` entries in its `decisions` log |
| **MCP** | `autonomous_decisions` |
| **CLI** | `datawatch autonomous decisions [automaton-id] [--state ...] [--limit N] [--json]` |
| **Comm channel** | `autonomous decisions [automaton-id]` |

**Access control.** The read needs the autonomous read capability when called by a federated peer. The gate and the config keys are local to the daemon that runs the Automaton.

**Monitoring: n/a, with reasons.** The gate adds no subsystem with a state of its own to gauge: a held task is visible as a task status, the shadow record is on the decision log and the one alert covers the first hold.

## Worker questions

A worker session of an Automaton can ask the operator a design question by printing one line in its output: `DATAWATCH_QUESTION:` followed by a single line of JSON (`question`, `options`, `recommendation`, `default`, `blocked`, and optionally `label` and `risk`). The daemon stores it as an ordinary [operator question](#operator-questions) asked by `worker-<short session id>`, linked to the session and its task, and announces it like any other raise. A line with `"blocked": true` also parks the task on the question until it is decided. How-to: [howto/operator-questions-workers](howto/operator-questions-workers.md). API reference: [api/autonomous](api/autonomous.md#worker-questions).

**Two switches, both off by default.** The marker is acted on only while `autonomous.questions.enabled` and `autonomous.questions.worker_marker` are both `true`. Both are read live at each marker line, so a change needs no restart. With either off the line is ignored like other output.

| Method | How |
|--------|-----|
| **YAML** | `~/.datawatch/config.yaml` → `autonomous:` → `questions:` → `worker_marker: true` |
| **CLI** | `datawatch config set autonomous.questions.worker_marker true` |
| **Web UI** | Settings → **Autonomous Automata planning** → *Let a worker raise a question with a marker in its output* |
| **REST API** | `PUT /api/config` with `{"autonomous.questions.worker_marker": true}` |
| **Comm channel** | `configure autonomous.questions.worker_marker=true` |
| **MCP** | `config_set` with key `autonomous.questions.worker_marker` and value `true` |

**What the daemon does with a marker line.**

| Line | Result |
|---|---|
| Valid | One question is stored (`source: "marker"`, `blocking` from `blocked`, `source_line_sha256` the hash of the line) and announced. The same line read again while its question is open raises nothing new. |
| Invalid (malformed, too long, a blank question, fewer than two options, a repeated option id, an unknown label or risk, a `default` or `recommendation` naming no option) | Nothing is stored or announced. The audit log gets `question_marker_refused` with the session id, a reason category and the SHA-256 of the line, never the line text. |
| Not a question (the marker in the middle of a sentence, or followed by a placeholder) | Dropped silently. |

**A blocked task.** While a blocking question is open, its task is waiting with the reason `await_decision: <question id>`: its capacity slot is returned, its project-tree lock is released unless the tree holds unpushed work, and no retry or failure is counted. The worker session keeps running. A question that is not blocking leaves the task running.

**How a task resumes.** Supervisor and coordinator ticks read `GET /api/questions` with `state` `open`, and `answered` or `withdrawn` for the resume pass, and **never read the terminal**. The coordinator tick reads the answered and withdrawn questions through the same question read, re-admits each parked task whose question was decided, re-claims the tree lock if the park released it, and sends the worker one `[automaton decision]` line carrying the decision (or saying the question was withdrawn and no default was applied). A task is resumed once per answer; a superseding answer becomes the new current answer without a second resume.

**Access methods for worker questions.**

| Surface | See the open questions | Answer |
|---|---|---|
| **REST API** | `GET /api/questions?state=open`; `GET /api/questions/{id}` | `POST /api/questions/{id}/answer` with `chosen_option_id` and/or `quote` |
| **MCP** | `question_list` with `state: open`; `question_get` | `question_answer` |
| **CLI** | `datawatch autonomous decisions --state open` (read only) | n/a: no CLI verb answers a question; use REST, MCP or the Web UI |
| **Comm channel** | `autonomous decisions` (read only, newest ten); the raise announcement names the id | n/a: no chat verb answers a question; use REST, MCP or the Web UI |
| **Web UI** | Automation view → *Operator questions* card; the task's waiting reason on the Automaton | **Answer** on the row. The Automaton's decision log shows the answer id on an answer entry as a link that opens the quote, the chosen option, the actor and the time |

**Not covered yet.** One-shot print-mode backends are not asked on their behalf: a run that prints its result and exits cannot be paused and then prompted with the decision, and this step adds no handling for it. A non-blocking question's answer is stored but not sent into the worker's session.

**Monitoring: n/a, with reasons.** The worker marker adds no subsystem with a state of its own to gauge. A raised question is on `GET /api/questions`, a parked task is visible as its waiting reason, and a refused line is an audit entry (`question_marker_refused`); there is no separate stats field, endpoint, tool, card or command for it.

**Watching it.** The marker has its own counters, since daemon start: lines raised as questions, repeats of a question still open, lines refused, tasks parked on a blocking question and tasks resumed. Read them from `GET /api/questions/stats` (MCP `question_stats`), from the *Worker Questions* card on the Monitor tab (shown once any counter is above zero), from the stats snapshot (`worker_questions_*`), or from the `datawatch_worker_questions_total{outcome=…}` Prometheus metric. A rising `refused` count means workers are printing marker lines that fail their checks; each refusal is in the audit log as `question_marker_refused` with the session, the reason category and a hash of the line, never the text.

## Terminal Performance

### xterm.js load time

The terminal loads session content in ~30-80ms:

- **TailOutput** reads only the last 64KB of the log file (not the entire file)
- **Initial pane_capture** sent synchronously on WebSocket subscribe
- **Pending capture buffer** catches captures that arrive before terminal initializes

### Output batching

Real-time output lines are batched per session at 100ms intervals before broadcasting via WebSocket. This reduces message count from 300+ to ~2-5 per batch, preventing send channel saturation.

### Live-update delivery

The PWA draws each `pane_capture` frame at most every 33 ms. A frame that arrives inside that window is held and drawn by a 40 ms trailing timer, so the newest frame always reaches the terminal (the daemon only resends a pane when it changes, so a dropped frame would otherwise leave the view stale).

- While the terminal is scrolled up, the newest frame is held instead of drawn and a **Jump to live** button appears. Clicking it, typing, sending input, or scrolling back to the bottom scrolls to the end and draws the held frame.
- If an open session view receives no `pane_capture` frame for 30 s, the 20 s WebSocket watchdog tick re-sends `subscribe` so the daemon sends a fresh frame.
- Regression story: `TS-798` (`feature:terminal-live-update`) in `docs/testing/v9.0.0/cookbook.md`.

### Crash resilience

- `pane_capture` writes wrapped in try/catch with auto-recovery
- ResizeObserver cleaned up on terminal dispose (prevents memory leak)
- Frame rate capped at ~30fps to prevent xterm.js buffer overload

## Managing Automata lifecycle event subscriptions (Unreleased — Phases 3–4 of [plans/2026-10-13-prd-lifecycle-events.md](plans/2026-10-13-prd-lifecycle-events.md))

**Operator workflow: no config, no restart.** Subscriptions are *data*, not config: create them from any surface, change them at any time, and the daemon's subscription engine re-attaches to the store immediately (`OnChanged`) — there is nothing to restart and nothing to restart-safe. The same is true of the *notify set* (below): changing which event kinds notify is data on the Automaton and its profile, not config. The only lifecycle-related *config* is `autonomous.notify.default_events` (the global set, below — changed once, applies at daemon start) and `autonomous.notify.pending_events_max` (the per-queue size of the pending-events queue, below).

### Default change: which kinds notify now (and how to turn it off)

This release lands the **notify set**: three tiers, an Automaton's own `notify_events` list, its guardrail profile's `notify_events` list, and finally the global `autonomous.notify.default_events`. The first non-empty tier decides the kinds; a subscription's filter can only narrow that set, never widen it. Two behaviour changes:

1. **The global default is now the five-kind baseline** — `prd.needs_review`, `prd.blocked`, `prd.failed`, `prd.completed`, `prd.cancelled` — instead of an empty set. Automata and profiles without their own list will now notify on those five kinds (where before nothing fired from the global tier).
2. **Opt-in kinds are gated** — `task.session_started` / `task.session_cleared` / `scope_warning` / `guardrail_warn` (and every task-level kind) fire only when the Automaton's resolved set includes them. A subscription on `task.session_started` for an Automaton that was not given that kind does nothing until the kind is added.

**Turning it off — by scope:**

- **One Automaton (keep everything else).** Give it an explicit list that excludes the unwanted kinds — the list *replaces* the inherited one, it does not delete from it: `datawatch autonomous prd-set-notify-events <id> prd.blocked` (from now on only `prd.blocked` notifies for this Automaton). Note: at the Automaton tier and the profile tier, an empty list (or `inherit`) means *inherit the next tier*, not off — only the global tier can express "no kinds" (`default_events: []`).
- **One profile (every Automaton that uses it).** `datawatch guardrail profile update <id> --notify-events prd.blocked,prd.failed` (or `--notify-events inherit` to return the profile to the global tier).
- **The whole daemon.** Set the global tier to an explicit empty list — in `~/.datawatch/config.yaml` under `autonomous.notify`, `default_events: []`, or `PUT /api/config` with the JSON body `{"autonomous.notify.default_events": []}` (a JSON-array value; a comma-separated string value with nothing after the comma also clears, but the empty array is the explicit form). Unsetting the key restores the five-kind baseline.
- **One sink.** Delete the subscription (`datawatch autonomous unsubscribe <id>`), or filter it down to kinds the resolved set still allows — the filter is applied *after* the gate, so narrowing a subscription never restores a kind that is not in the resolved set.

Check where any Automaton stands with `datawatch autonomous prd-get <id>` — `notify_events` (its own list), `notify_events_effective` (the resolved kinds) and `notify_events_effective.source` (`automaton` | `profile` | `global`). `datawatch autonomous status` shows the global tier plus the per-Automaton overrides (capped at 100, first in ID order when there are more). Every change is audit-logged as `autonomous_notify_events_changed` with the old and new list.

Full walkthrough with examples for CLI, comm, MCP, REST and PWA: the how-to [howto/automaton-lifecycle-subscriptions.md](howto/automaton-lifecycle-subscriptions.md) (§ *Choosing which events notify*) and [howto/guardrail-library.md](howto/guardrail-library.md) (§ *Notify events on a profile*).

### What is where

- Storage: `<data_dir>/lifecycle_subscriptions.json` — secfile-encrypted at rest (same treatment as `channel_routing.json`), modeled on the discussion-subscription store. Back up with the data dir; re-apply after migration.
- Audit: create/update/test/delete are audit-logged. Per-event *delivery* is not audited (high-frequency, same convention as discussion `subscribe` deliveries) — the daemon log is the delivery ledger (`lifecycle-sub` lines).
- Engine: in-daemon, wired in `cmd/datawatch/main.go` (store → engine → REST/MCP/CLI/comm all hit the same store). REST returns **503** in a context where the engine is not wired (unit-test / stripped-down daemons).

### Managing — every surface

- **CLI** — `datawatch autonomous subscribe` (repeatable `--on <kind>`, `--level prd|task`, `--prd`, `--task`, `--sink`, `--target`, `--send`, `--one-shot[=false]`, `--coalesce`, `--expires <seconds>`) · `datawatch autonomous unsubscribe <id>` · `datawatch autonomous subscriptions [--owner <owner>]` · `datawatch autonomous subscription-update <id> …` (same flags as `subscribe`; replaces the configuration) · `datawatch autonomous subscription-test <id>` · `datawatch autonomous events --queue <name> [--since <cursor>] [--limit n] [--json]`.
- **MCP** — `autonomous_subscribe`, `autonomous_unsubscribe`, `autonomous_subscriptions`, `autonomous_subscription_update`, `autonomous_subscription_test`, `autonomous_events_pending`. The MCP server holds no state; it proxies the daemon's REST on loopback, so the daemon store remains the single source of truth across host restarts.
- **Comm channel** — `autonomous subscribe <kinds…> [prd=<id>] [task=<id>]`, `autonomous subscriptions`, `autonomous unsubscribe <id>`, `autonomous subscription-update <id> key=value…`, `autonomous subscription-test <id>`, `autonomous events [queue=<name>] [since=<cursor>]`. Owner-scoped: the calling channel is both the owner and the sink target; a channel can only ever see, change, test and remove *its own* records, and read only the queues its own subscriptions feed.
- **REST** — `GET/POST /api/autonomous/subscriptions` (optional `?owner=`), `PUT /api/autonomous/subscriptions/{id}` (replace the mutable fields → `200` + the record), `POST /api/autonomous/subscriptions/{id}/test` (→ `200 {"ok","sink","error"}`), `DELETE /api/autonomous/subscriptions/{id}`, `GET /api/autonomous/events?queue=<name>&since=<cursor>&limit=<n>`; auth `autonomous:read` (list, events) / `autonomous:write` (create, update, test, delete); `400` malformed/unknown kind (or a missing `queue` / non-numeric `since` / `limit` on events), `404` unknown id, `503` engine not wired.
- **PWA** — Automaton detail → **Subscriptions** tab: lists the subscriptions that apply to that Automaton (scoped to it, or unscoped and so firing for every Automaton) with **Add subscription**, **Edit**, **Test** and **Delete**. The tab loads on open and after each change and does not poll. It shows "Lifecycle subscriptions are disabled on this server." when the engine is not wired (`503`).

### Session-sink delivery order (what the operator should expect)

Bridge first, then typed text: if the target session has a channel bridge with a local port, the daemon POSTs the message to `http://127.0.0.1:<port>/send` (5 s timeout). On port 0, POST error, or no bridge, it types the text into the session via `SendInput` with source tag **`lifecycle-sub`** — visible in the session input history so you can distinguish daemon-typed text from operator-typed text. The `send_input` action (up to 4096 chars) applies only to session sinks.

### Semantics

- **One-shot** — removed only after *successful* delivery; failure → one retry → log + drop (the daemon never blocks on a dead target).
- **Expiry** — `expires_at` is enforced by the janitor; the record stops matching once the timestamp elapses.
- **Coalesce** — human-facing sink deliveries (comm/alert) are batched in quiet windows rather than one message per event.
- **Update** — `PUT` replaces filter, sink, action, `coalesce`, `one_shot` and `expires_at` wholesale: a field left out is cleared, so send the full desired configuration. The id and creation time are kept, and the engine re-attaches immediately.
- **Test-fire** — delivers one synthetic `prd.updated` event whose text starts with `[test] ` to just that subscription's sink, through the same sink wiring a real event uses. It never types the `send_input` action text, never consumes a one-shot and bypasses coalescing, so it is safe to run against a live rule. The reply is `{"ok":true,"sink":"<type>"}` or `{"ok":false,"sink":"<type>","error":"…"}` (for example `sink not available` when that delivery path is not wired in this daemon). Test-fire is audit-logged.

### Pending-events queue (`mcp_queue` sink) and polling

Clients that cannot receive pushes (a generic MCP host, a script) poll instead. Procedure:

1. Create a subscription with sink type `mcp_queue`; the sink target is the **queue name** (letters, digits and `. _ : -`, at most 64 characters). Several subscriptions may feed one queue.
2. Poll `autonomous_events_pending(queue, since_cursor?, limit?)` (or `GET /api/autonomous/events?queue=<name>&since=<cursor>`). The first call uses cursor `0`.
3. Process `events` (oldest first), then pass the returned `next_cursor` back as `since_cursor` on the next poll. Each call returns `{"events":[...],"next_cursor":<n>,"dropped":<bool>}`; `events` is always an array and `limit` defaults to 50, capped at 200.
4. If `dropped` is `true`, events you had not yet read were evicted because the queue was full — re-sync from the daemon's own state (for example `autonomous_prd_get`) rather than assuming a complete history.

Queue contents are **in memory only**: they are bounded per queue, cursors restart at 1 when the daemon restarts, and nothing in a queue survives a restart (the subscription itself does). Deleting the last subscription that feeds a queue also discards its retained events. Size the bound with `autonomous.notify.pending_events_max` (default `500`, values below 1 fall back to 500; read and set it with `datawatch config get|set` or `config_set`; applied at daemon start). The oldest events are evicted first.

### Verification & troubleshooting

- `datawatch autonomous subscriptions` (+ `--owner`) — the live list is the state of truth.
- Confirm an injection actually typed text: check the target session's input history for the `lifecycle-sub` source tag.
- `503` → engine not wired in that context (see above); unknown-kind rejection at creation ("not a known event kind") → use the full kind — e.g. **`prd.updated`**, *not* the `prd_updated` shorthand, which currently maps to `task.prd_updated` and is rejected at validation (known gap — see the design doc's Phase 3 notes).
- A `webhook` or `peer` subscription is refused while its switch is off. Both switches ship off; see [Outbound webhook sink and peer forwarding](#outbound-webhook-sink-and-peer-forwarding-ships-disabled).
- A test-fire that reports `ok:false` names the cause in `error`; check the sink's own prerequisites (a comm router configured, the target session exists, topic publishing wired).
- A poll that always returns no events: confirm a subscription with sink type `mcp_queue` and the *same queue name* exists (`datawatch autonomous subscriptions`), and that you are not passing a `since` cursor from before a daemon restart (cursors restart at 1).

**Checklist record:** `docs/config-reference.yaml` — `autonomous.notify.pending_events_max` documented; localization — the Subscriptions tab strings are in all five locale bundles; version — Unreleased (final number set at merge); Mobile-Parity — app-side UI tracked in [plans/2026-10-21-datawatch-app-lifecycle-subscriptions-note.md](plans/2026-10-21-datawatch-app-lifecycle-subscriptions-note.md);
 full checklist outcome recorded in the design doc's Phase 3 and Phase 4 bullets.


### Outbound webhook sink and peer forwarding (ships disabled)

Two sinks send Automaton lifecycle events out of this daemon. **Both ship disabled.** `autonomous.notify.webhook.enabled` and `autonomous.notify.forwarding.enabled` default to `false`. While a switch is off, a subscription with that sink is refused at creation, and any such subscription already stored delivers nothing. Subscriptions with the other sinks are not affected. The operator-facing guide is [howto/automaton-lifecycle-webhooks.md](howto/automaton-lifecycle-webhooks.md). The design, and the values still pending confirmation, are in [plans/2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal.md](plans/2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal.md).

**Enable and set the allow-list, on any of the five access methods:**

| Method | Enable the webhook sink | Set the webhook allow-list | Enable peer forwarding |
|---|---|---|---|
| YAML (`config.yaml`) | `autonomous.notify.webhook.enabled: true` | `autonomous.notify.webhook.allow_hosts: [hooks.example.net, hooks.example.net:8443]` | `autonomous.notify.forwarding.enabled: true` |
| CLI | `datawatch config set autonomous.notify.webhook.enabled true` | `datawatch config set autonomous.notify.webhook.allow_hosts hooks.example.net,hooks.example.net:8443` | `datawatch config set autonomous.notify.forwarding.enabled true` |
| Web UI (Settings → Comms → Notifications) | *Outbound webhook sink: enabled* toggle | *Webhook allow-list (host or host:port)* field | *Peer forwarding of lifecycle events: enabled* toggle |
| REST (`PUT /api/config`) | `{"autonomous.notify.webhook.enabled": true}` | `{"autonomous.notify.webhook.allow_hosts": ["hooks.example.net"]}` | `{"autonomous.notify.forwarding.enabled": true}` |
| Comm channel | `configure autonomous.notify.webhook.enabled=true` | `configure autonomous.notify.webhook.allow_hosts=hooks.example.net,hooks.example.net:8443` | `configure autonomous.notify.forwarding.enabled=true` |

The allow-list is the only list of hosts that may receive a webhook. Each entry is an exact host or an exact `host:port`, case-insensitive, with no scheme, path, wildcard or user name. It is checked at creation and on every delivery. An empty list means nothing can receive an event. A change applies to the next event with no restart; the YAML file is read when the daemon starts. Every other key (`max_attempts`, `backoff_seconds`, `jitter_percent`, `attempt_timeout_seconds`, `max_body_bytes`, `signature_tolerance_seconds`, and for forwarding `max_hops` and the retry keys) is in [config-reference.yaml](config-reference.yaml). A value below 1 means the default.

**Signing secret.** The signing key lives in the secrets vault: `datawatch secrets set <name> <value>`. A webhook subscription names it as `${secret:name}`. A literal value is refused at creation. The reference is resolved on every delivery, so a rotation applies to the next event. The resolved value is never stored, logged, audited or returned.

**Subscribe.** `datawatch autonomous subscribe --on <kind> --sink webhook --target https://<host>/<path> --secret-ref '${secret:name}'` for a webhook, or `--sink peer --target <registered-peer-name>` for a peer (no secret). The same sink types are accepted by the MCP tools, REST, the comm verb and the PWA Subscriptions tab.

**Delivery.** A webhook is signed with HMAC-SHA256 (`X-Datawatch-Signature: t=<unix>,v1=<hex>`), with `X-Datawatch-Delivery` and `X-Datawatch-Event` headers. A failed attempt is retried: 5 attempts in total, delays of 1, 4, 16 and 64 seconds with ±20 % jitter, a 10-second limit per attempt. Network errors, timeouts, `408`, `429` and `5xx` are retried; other `4xx`, `3xx` (redirects are not followed) and an over-size payload (64 KiB) are not. Peer forwarding uses the same schedule. A delivery that is dropped raises one error-level alert (*Lifecycle webhook delivery failed* or *Lifecycle peer forward failed*) and writes a final audit record. There is no outbox for a peer that stays offline.

**SSRF guard.** A webhook URL must be `https`; `http` is accepted only for an allow-listed host. Loopback, link-local, unspecified, multicast, private (RFC 1918 and unique-local) and Tailscale CGNAT addresses are refused unless their host is on the allow-list. Cloud metadata addresses (`169.254.169.254`, `fd00:ec2::254`) are refused even when allow-listed, pending the operator's confirmation. The resolved address is checked again at connect time, on every attempt.

**Audit actions.** The JSON-lines audit log (`audit.log`) and, when the CEF mirror is enabled, `audit.log.cef` carry these actions:

| JSON-lines action | CEF name | Written |
|---|---|---|
| `autonomous_lifecycle_webhook_delivery` | `AutonomousLifecycleWebhookDelivery` | Each webhook attempt, and each refused or skipped delivery |
| `autonomous_lifecycle_webhook_failed` | `AutonomousLifecycleWebhookFailed` | Once per dropped webhook delivery |
| `autonomous_lifecycle_forward_delivery` | `AutonomousLifecycleForwardDelivery` | Each peer forward attempt, and the final outcome |
| `autonomous_lifecycle_forward_receive` | `AutonomousLifecycleForwardReceive` | Each forwarded request this daemon answers, accepted or refused |

**Receiving endpoint.** A peer forwards to `POST /api/autonomous/events/forward` with the peer's bearer token and a signature. The answers are `202` (accepted and re-emitted locally), `400` (malformed or over the size cap), `401` (token or signature missing or wrong), `403` (the sending peer lacks `autonomous:write`), `409` (a loop rule refused it: this host is the origin, is already in the visited list, or the hop cap was reached), and `503` (forwarding is off here or the engine is not wired). The sender retries `5xx` and does not retry `4xx` or `409`. A forwarded event is never forwarded on.

**Checklist record (outbound webhook sink and peer forwarding):**
`docs/config-reference.yaml` — every `autonomous.notify.webhook.*` and `autonomous.notify.forwarding.*` key is documented, with the defaults and "pending operator confirmation" marked; localization — the Notifications settings and the subscription form strings are in all five locale bundles (en, de, es, fr, ja); version — Unreleased (final number set at merge); Mobile-Parity — the app-side subscription UI stays tracked in [plans/2026-10-21-datawatch-app-lifecycle-subscriptions-note.md](plans/2026-10-21-datawatch-app-lifecycle-subscriptions-note.md), and the new sink types reach mobile clients through the same REST contract (`POST` and `PUT /api/autonomous/subscriptions` with `sink.type` `webhook` or `peer`, and `sink.secret_ref`). Test coverage is in [testing-tracker.md](testing-tracker.md).

## Session and PRD storage layout

The session store and the Automaton (PRD) store write only what changed. Before this change every update rewrote the whole `sessions.json` and `prds.jsonl`; 200 channel events over 4,000 sessions wrote 8.0 GiB. The same burst now writes about 106 KB. Design and measurements: [plans/2026-10-23-session-store-write-amplification.md](plans/2026-10-23-session-store-write-amplification.md).

### Coalesced writer and flush interval

A session change marks the session dirty and wakes one background writer. The writer waits one flush interval, then writes every dirty session in one batch, so 200 changes inside the interval cost one write per changed session. `session.store_flush_interval` sets the interval in milliseconds (default 1000). `0` writes on every change. A change is at risk for at most one interval if the process is killed; a clean stop, a restart and a re-exec flush synchronously first.

### Per-session records

```
<data_dir>/sessions.d/LAYOUT                      marker; its presence selects this layout
<data_dir>/sessions.d/p.<FullID>.rec              one session, compact JSON
<data_dir>/sessions.d/x.<sha256 of FullID>.rec    same, for a FullID with unusual characters or a long one
<data_dir>/sessions/<FullID>/session.json         per-session working data (unchanged)
<data_dir>/sessions/<FullID>/fields/<field>.txt   full text of a truncated field
<data_dir>/sessions-archive.jsonl                 archived sessions
```

Each record is written alone through a temp file, fsync and rename, so a crash leaves each record at its old or new version and never half written. A record that cannot be read is skipped with a warning and restored from `sessions/<FullID>/session.json` when that file exists.

### Per-PRD records and side files

```
<data_dir>/autonomous/prds/LAYOUT                         marker
<data_dir>/autonomous/prds/<id>.json                      one PRD with its stories and tasks
<data_dir>/autonomous/prds/<id>.files/<sha256>.txt        side file for large text
```

Only PRDs changed since the last write are written, and a record whose bytes are unchanged is skipped. Text longer than `session.store_side_file_cap_kb` (default 64 KB) moves to a content-addressed side file and the record keeps a `datawatch-side-file:v1:<hash>` reference that is resolved on load, so the API shows the full text. Bulky output fields (judge output, task output, checkpoints, transcripts, errors, specs) move to a side file above 4 KB. Text over 32 MB is cut and ends with `[datawatch: truncated, <n> bytes dropped at the <limit> byte limit]`. Changing a PRD no longer rewrites the learnings, profile, tag or retry-profile files.

### Backups made during migration

On the first start of a build with these stores, each old file is converted once:

| Old file | Backup | New layout |
|---|---|---|
| `sessions.json` | `sessions.json.pre-records.bak` | `sessions.d/` |
| `autonomous/prds.jsonl` | `autonomous/prds.jsonl.pre-records.bak` | `autonomous/prds/` |

The migration copies the old file to the backup (an existing backup is never overwritten; a later copy gets `.1`, `.2`), writes every record, reads them back and requires the same sessions or PRDs, writes the `LAYOUT` marker last, and only then removes the old file. A failure before the marker leaves the old file in place and the daemon on the old format; the next start retries. An old file found next to an existing layout is imported for the entries the layout lacks, then renamed `.imported`.

To roll back to an older daemon: stop the daemon, copy the backup over `sessions.json` (or `prds.jsonl`), and remove `sessions.d/` (or `autonomous/prds/`). Sessions and PRDs created after the migration are not in the backup.

### Archive

Every hour, and once at start, a finished session (complete, failed or killed) that has not been updated for `session.store_archive_after_days` days (default 14, `0` turns archiving off) moves from the active store to `sessions-archive.jsonl`. Running and waiting sessions are never archived.

- The file is append-only: one JSON line per session, with its id and update time readable and the record after them. Restoring appends a tombstone line. Nothing in it is rewritten or deleted.
- The daemon keeps only an index of id to file offset in memory, so an archive of thousands of sessions costs little.
- Archived sessions stay readable. `GET /api/sessions/<id>` falls back to the archive. `GET /api/sessions?history=1` and `?q=<text>` add archived sessions to the list, newest first, with paging (up to 500 per request); the web UI History view reloads the list to include them. The default list and `active=1` never include them.
- Restore moves a session back to the active store and out of the archive index (`Manager.RestoreArchivedSession`, used by the server code and the tests). There is no REST, CLI or comm-channel command for it yet.
- A torn last line (a crash during append) is ignored on load.

### Field size cap and truncation markers

A session text field (task, prompts, summaries and similar) longer than `session.store_field_cap_kb` KB (default 256) is stored in its record cut at the cap and ending with

```
[truncated <n> bytes, full text in side file sessions/<FullID>/fields/<field>.txt]
```

The full text is in that side file, written before the record. The in-memory session keeps the full text; a session loaded after a restart shows the cut text and the marker. `0` disables the cap.

### Encryption

When the store has an encryption key (secure mode), each session record and each archive line is encrypted on its own, the same as `sessions.json` was; the archive keeps only the id and update time in clear. The `LAYOUT` markers are plain text and hold no session data. An encrypted `sessions.json` is read with the key during migration and its backup stays encrypted. An archive line encrypted with a key the daemon does not have is reported as unreadable instead of being skipped silently. Session field side files are encrypted the same way. PRD records and PRD side files are plain JSON and text, as `prds.jsonl` was.

### Configuration keys

| Key | Default | Meaning |
|---|---|---|
| `session.store_flush_interval` | `1000` | Milliseconds the writer batches session changes; `0` writes on every change. Applies live. |
| `session.store_archive_after_days` | `14` | Days after its last update that a finished session is archived; `0` turns it off. Applies live. |
| `session.store_field_cap_kb` | `256` | Largest session text field kept inline, in KB. Applies to later writes. |
| `session.store_side_file_cap_kb` | `64` | PRD text above this many KB goes to a side file. A PRD is rewritten with the new value when it next changes. |

All four are available in `config.yaml`, the web UI Settings, `datawatch config set`, the comm channel `configure` command, MCP `config_set` and `PUT /api/config`.
