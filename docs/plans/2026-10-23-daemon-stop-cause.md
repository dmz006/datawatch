# 2026-10-23 datawatch daemon stop — sender attribution audit

Status: **findings recorded; actual sender unexplained** (see §6); **e2e verification gate passed 2026-10-23** (see §9)
Date: 2026-10-23
Scope: repo-wide verification of every path that can stop or restart the
production `datawatch` daemon, cross-referenced against the 05:33:03 incident.
Verification method: `grep -rn` across `scripts/`, `tests/`, `internal/`,
`cmd/`, `install/`, `.github/`, `Makefile`, `docker/`, `deploy/` for
service-manager stop/restart of the unit, `datawatch stop|restart` CLI,
direct `kill` / `syscall.Kill` / `os.FindProcess` + `Signal`, and kill-by-name
(`pkill`/`killall`). Every claim below cites file:line. No item is asserted
without a cited line.

## 1. Incident (observed facts)

- **2026-10-23 05:33:03** — the production daemon logged
  `Shutting down... (signal=terminated)`. That exact line is emitted by the
  daemon's own SIGINT/SIGTERM handler at `cmd/datawatch/main.go:1855`
  (`fmt.Printf("\nShutting down... (signal=%v)\n", sig)`), registered via
  `signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)` at `cmd/datawatch/main.go:1852`,
  handled in the goroutine at `:1853-1855`.
- "terminated" is Go's name for **SIGTERM** (SIGINT would print "interrupt").
  The handler ran, so this was **not** SIGKILL (a `kill -9` cannot produce
  this line) and not SIGINT.
- The systemd service then became **inactive** and stayed down. Both unit
  templates use `Restart=on-failure`:
  user unit `install/install.sh:530`, system unit
  `install/systemd/datawatch.service:14`. A graceful SIGTERM exit (exit 0)
  does not trigger `on-failure`, so "SIGTERM + no (re)start + exit 0 ⇒
  inactive" is consistent with any one-shot SIGTERM delivery.
- PRDs were missing a coordinator for ~3 minutes (reported), while the tmux
  **AI sessions survived**. Consistent with
  `cmd/datawatch/main.go:1857` — sessions are killed on exit only when
  `cfg.Session.KillSessionsOnExit` is set; otherwise the handler proceeds to
  a clean exit and the tmux panes stay.
- The service manager (systemd user/system scope) is the observed service
  boundary; the daemon binary itself contains no self-termination beyond the
  paths listed in §3 (exec-restart only).

## 2. Repo-wide search — what exists, what doesn't

Search patterns (all directories above): `systemctl`, `smctl`,
`service-manager`, `pkill`, `killall`, `datawatch stop|restart`,
`kill`, `Stop(`, `Terminate`, `SIGTERM`, `syscall.Kill`, `unix.Kill`,
`os.FindProcess`, `Signal(`, `restart_daemon`, `api/restart`, `update`,
`Restart=` (units), `ExecStart=`.

**Kill-by-name does not exist anywhere in the repo.** `pkill`/`killall`
appear only as: the shutdown-cause recorder's verb table
(`internal/shutdowncause/capture_linux.go:191-192`, matched-intent logic
`internal/shutdowncause/capture_linux.go:270`) and as comments stating those
commands are *not* run (`scripts/merge-branches-bump-version.sh:28`,
`internal/autonomous/verifying_scenarios_test.go:16`). No code sends a signal
to a daemon discovered by process name.

**No production stop in CI.** GitHub Actions only kills job-local daemons
(`.github/workflows/owasp-zap.yaml:161` `kill -9 "${DW_PID:-0}"` in the
health-check/restart step and `:241` `kill "${DW_PID:-0}"` in "Stop daemon";
that daemon is the instance the job itself started). `Makefile`'s only stop
is `docker stop datawatch-registry` (`Makefile:207`) — the registry
container, not the daemon. `docker/` and `deploy/` contain no stop/restart of
the datawatch unit (only `deploy/systemd/datawatch-stats.service:37-38`
declares `Restart=on-failure`/`RestartSec=5s` for the *stats* peer agent).

**Systemd unit lifecycle in-tree** is install/removal only:
`install/install.sh:488-489` (enable, system), `install/install.sh:543-544`
(enable, user), `install/systemd/install-service.sh:13-14` (enable), and
`install/uninstall.sh:13` (`sudo systemctl stop datawatch`) /
`install/uninstall.sh:23` (`systemctl --user stop datawatch`) — the latter
two also delete the binary (`install/uninstall.sh:32-33`), so an uninstall
run would be visible and is the last candidate.

## 3. Findings — every path that can reach the production daemon

| # | Path (file:line) | Trigger | Signal to production daemon | Consistent with 05:33:03? |
|---|------------------|---------|-----------------------------|---------------------------|
| 1 | `datawatch stop` CLI — `cmd/datawatch/main.go:9311` (`runStop`), delegates to `systemctl --user stop datawatch` at `main.go:9338` when the unit is active, else raw `os.FindProcess(pid).Signal(syscall.SIGTERM)` at `main.go:9325-9341` | operator on the daemon host | **SIGTERM** (unit stop or direct) — logs `Shutting down... (signal=terminated)`, exits 0, unit left **inactive** | **Yes — full match** (log line + inactive + sessions survive) |
| 2 | `datawatch restart` CLI — `cmd/datawatch/main.go:9362` (`runRestart`); delegates to `systemctl --user restart` at `main.go:9372`, else SIGTERM at `main.go:9376` then re-runs `datawatch start` | operator on the daemon host | **SIGTERM** (old instance) then a *new* instance started ⇒ unit ends **active** | Log line yes, but "left inactive" **no** (a restart would not leave the unit down) — **ruled out** by post-incident state |
| 3 | `systemctl --user restart datawatch` as the test's operator path — `internal/session/restart_safety_live_test.go:229-233` (`bl462RestartCommand`, gate `DW_LIVE_RESTART_TEST=1` at `:281`), fired at `:345-348`; same helper used by `internal/session/parked_prd_resume_live_test.go:79` (gate `:106`) and `internal/session/restart_adopt_live_test.go:332` | **only** if someone ran the live tests with `DW_LIVE_RESTART_TEST=1` and `DW_BASE` pointed at the production daemon (explicitly warned at `restart_safety_live_test.go:29`: "never point this at production by accident") | **SIGTERM** (old) then systemd starts a new one ⇒ **active** | Log line yes; "left inactive" **no** — **ruled out** unless its exec also failed (not evidenced) |
| 4 | `POST /api/restart` — `internal/server/api.go:9724` (`handleRestart`), declares `shutdowncause.MarkAPIInitiated("api-restart")` at `:9753`, then calls `s.restartFn()` (wired at `api.go:991-992`, daemon side `cmd/datawatch/main.go:7649-7659`, HTTP variant `:2876-2884`). Restart is **`syscall.Exec` of the daemon itself** (`main.go:7656`, `:2883`) | any API client with federation `config:write` cap (`api.go:9725`) | **No signal** — in-process `exec(2)` of the same PID; **no** `Shutting down...` line at all, unit stays **active** (same PID) | **No — mechanism mismatch** (incident produced the log line and an inactive unit) — **ruled out** |
| 5 | MCP `restart_daemon` tool — `internal/mcp/server.go:2800` (`handleRestartDaemon`), `MarkAPIInitiated("mcp-restart")` at `:2812`, then `s.restartFn()` (same exec closures as #4; tool registered `server.go:1298`) | any MCP caller with `config:write` cap (`server.go:2801`) | **No signal** — in-process `exec(2)` | **No — mechanism mismatch** — **ruled out** |
| 6 | Chat-command `restart` — `internal/router/router.go:1358` (`handleRestart`), calls `r.restartFn()` (same exec closure via `main.go:2876-2884`) | the daemon's own chat/pipe user | **No signal** — in-process `exec(2)` | **No — mechanism mismatch** — **ruled out** |
| 7 | `uninstall.sh` — `install/uninstall.sh:13` (`sudo systemctl stop datawatch`), `:23` (`systemctl --user stop datawatch`), binary removal `:32-33` | operator on the daemon host | **SIGTERM** via unit stop, unit left **inactive** | **Yes** (log line + inactive) — **ruled out only if** the binary/units were still present afterwards (they were — the unit was later restarted, per the incident report) |
| 8 | Test-daemon teardown — `scripts/run-tests.sh:326-340` (`wait_or_kill`: SIGTERM at `:328`, SIGKILL fallback at `:333`), `stop_test_daemon` at `:342` (kills its own `$DAEMON_PID`, spawned at `:218`); orphan reaper `_kill_orphan_daemon_for` at `:70-76` (`kill` any PID matching `pgrep -f "config.*${dir}/config.yaml"` for stale `/tmp/dw-test-*` / `/tmp/dw-docker-sim-*` dirs older than 1 day (`:78`, called from `:79`) and `datawatch-<hex>` dirs under the repo parent older than 3 days (`:84`, called from `:85`)); `scripts/e2e-pwa.sh:49` (`kill "$DAEMON_PID"`, daemon spawned at `:101`) | whoever ran `run-tests.sh`/`e2e-pwa.sh` | **SIGTERM** (or SIGKILL) — but only to **test daemons** whose cmdline contains a *test* `config.yaml` path (`run-tests.sh:73`) | Production daemon's cmdline contains the *production* config path, not a `/tmp/dw-*` or `datawatch-<hex>` test dir — **ruled out** by path, unless an operator deliberately pointed these at production (no evidence) |
| 9 | Sandbox teardown — `scripts/sec-assess/sandbox.sh:114` (`kill "$pid"` from `$PIDF`, spawned by its own `start` at `:98` (PID written at `:99`)), SIGKILL fallback `:119`; self-describes as the only teardown path, `:19` | whoever ran `sandbox.sh wipe` | **SIGTERM** then SIGKILL — only for the sandbox's own PID | **No** — kills only the PID it spawned itself — **ruled out** |
| 10 | `scripts/eval-model-tiering.sh:270-271` — `kill -TERM` of its own per-tier child + mock task PID (`$tmp/task.pid`) during its SIGTERM self-test | whoever ran the tiering eval | **SIGTERM** — only its own subprocess tree | **No** — does not touch the daemon — **ruled out** |
| 11 | Auto-updater — `cmd/datawatch/main.go:8480` (`runAutoUpdater`); on a new version it installs the binary then `syscall.Exec`s the daemon in place at `:8519` (same PID, **no** `Shutting down...` line, unit stays active); its exec-failure message at `:8520` says "run `datawatch stop && datawatch start`" | runs inside the daemon when `update.schedule` is set (default schedule string at `:8482-8490`, e.g. `daily`) | **No signal** — in-process `exec(2)` | **No — mechanism mismatch** — **ruled out** |
| 12 | `datawatch update` (operator) — `cmd/datawatch/main.go:13248` (`runUpdate`); installs the new binary and **tells the operator** to run `datawatch stop && datawatch start` (messages at `:13284`, `:13296`); it never signals the daemon itself | operator on the daemon host | **None directly** — the *follow-up* `datawatch stop` is path #1 | The update itself is **ruled out**; a follow-up manual `datawatch stop` is path #1 |

### Summary of mechanism classes

- **SIGTERM to the daemon (produces `Shutting down... (signal=terminated)`,
  exit 0, unit can be left inactive):** #1 `datawatch stop`, #2/#3
  `datawatch restart` / `systemctl restart` (but these *re*start, so the unit
  ends active), #7 `uninstall.sh`, #8 test/orphan reaper (test daemons only),
  #9 sandbox (its own PID only).
- **In-process `exec(2)` restart (no signal, no log line, same PID, unit
  stays active):** #4 `POST /api/restart`, #5 MCP `restart_daemon`, #6 chat
  `restart`, #11 auto-updater. These are the "exec restart" class and all
  structurally inconsistent with the incident's log line + inactive state.
- **SIGKILL (`kill -9`), which produces *no* log line at all:**
  `.github/workflows/owasp-zap.yaml:161` (job daemon),
  `scripts/run-tests.sh:333` (test daemon), `scripts/sec-assess/sandbox.sh:119`
  (sandbox PID). Ruled out by absence of the log line and by target scope.

## 4. Cross-check against the 05:33:03 evidence

- **Log line present** → SIGINT or SIGTERM was delivered and handled
  (`main.go:1855`). "terminated" ⇒ SIGTERM. All exec-restart paths (#4, #5,
  #6, #11) and all SIGKILL sites are therefore inconsistent with the event.
- **Unit left inactive** ⇒ whatever sent SIGTERM did not successfully start a
  replacement. This rules out #2 (`datawatch restart`) and #3 (live-test
  `systemctl restart`) *unless* their fresh exec failed to start — for which
  there is no evidence. It also rules out #7 (`uninstall.sh`) *unless* the
  binary/units were reinstalled after — the incident notes the unit was later
  restarted, so this is possible but requires an extra step.
- **Sessions survived** ⇒ `KillSessionsOnExit` was off
  (`main.go:1857`), i.e. nothing session-scoped was being torn down either; any
  one of the SIGTERM paths above fits.
- **Time (05:33:03)** sits in the middle of the session window (sessions
  04:50/05:08/05:11 active). The auto-updater default tick is `daily`
  (`main.go:8482-8490`) but again execs in place — no log line — so it cannot
  be the source of this record even if it fired near that time.

## 5. What the audit *can* conclude

1. **No in-repo automation, CI job, or daemon self-path produces the exact
   observed signature** (SIGTERM + graceful exit + unit left inactive + no
   follow-on start) **except** an operator- or process-originated one-shot
   stop: `datawatch stop` (#1), `uninstall.sh` (#7), or an equivalent
   out-of-repo command (`systemctl --user stop datawatch`, `pkill`/`kill` by
   name, systemd `stop`/mask, or a host-level signal). Path #1 is the only
   in-tree stop that leaves the binary installed and the unit stopped — it is
   the minimal reproduction of the observed state.
2. **The exec-restart family (#4/#5/#6/#11) is excluded** by the log line
   alone — none of them can emit `Shutting down... (signal=terminated)`
   because they never receive a signal and never `exit`.
3. **Kill-by-name and SIGKILL are excluded** as the *logged* event (no in-tree
   `pkill`/`killall` of the production daemon; SIGKILL cannot run the Go
   handler at `main.go:1855`, registered at `:1852`).
4. **The `datawatch stop`/`systemctl stop` / `uninstall.sh` SIGTERM class
   remains** — i.e. the sender is an **external process** that sent SIGTERM to
   the daemon's main PID (or its cgroup) and then did not (re)start it. The
   repo contains nothing that does that to the production unit on a schedule,
   in CI, or on a timer.

## 6. Unexplained

**The actual sender of the 2026-10-23 05:33:03 stop is not identified by this
audit.** Every in-repo mechanism that could send SIGTERM to the production
daemon is either (a) scoped to test/sandbox daemons (#8/#9), (b) self-scoped
(#11), (c) an operator-initiated stop (#1/#7) with no recorded trigger, or
(d) an exec-restart that cannot produce the observed log line at all
(#4/#5/#6). Nothing in `scripts/`, `.github/`, `install/`, `Makefile`,
`docker/`, `deploy/`, or the daemon itself schedules or automates a
production SIGTERM stop, and no kill-by-name path exists in the tree. The new
shutdown-cause recorder (`internal/shutdowncause/`, defined and invoked from the signal handler at
`cmd/datawatch/main.go:1811`/`:1856`, persisted as `last-shutdown.json` and surfaced
at startup via `PreviousShutdownLine` at `main.go:958`) will attribute the
*next* occurrence (it captures source, sender PID/uid/cmdline, and any
systemd stop job — `internal/shutdowncause/cause.go:18-20`), but it was not in
the binary at the time of this incident, so the 05:33:03 sender remains
**unexplained** on the evidence in this repository.

## 7. Verification commands used

```bash
# 1. service-manager / unit / kill-by-name verbs across the tree
grep -rn -E 'systemctl|smctl|service-manager|pkill|killall' \
  scripts/ tests/ internal/ cmd/ install/ .github/ Makefile docker/ deploy/ \
  --include='*.sh' --include='*.go' --include='*.yml' --include='*.yaml' \
  --include='Makefile' --include='Dockerfile*' \
  | grep -iE 'datawatch|daemon|stop|restart|kill'

# 2. direct signal delivery in Go or shell
grep -rn -E 'syscall.Kill|unix.Kill|os.FindProcess|SendSignal|Signal\(' \
  scripts/ tests/ internal/ cmd/ install/ .github/ Makefile docker/ deploy/

# 3. restart APIs / MCP / CLI / update paths
grep -rn -E 'restart_daemon|"/api/restart"|api/restart' \
  scripts/ tests/ internal/ cmd/ install/ .github/ Makefile docker/ deploy/

grep -rn -E 'pkill|killall' scripts/ tests/ internal/ cmd/ install/ .github/ Makefile docker/ deploy/

grep -rn -E 'stop|restart|kill|update' .github/workflows/ | grep -iE 'datawatch|daemon'

grep -rn -E 'restart|stop|kill|replace|binary' install/*.sh
grep -rn -E 'Restart=|ExecStart=|ExecStop=|KillMode' install/
grep -nE 'stop|restart|kill|systemctl' Makefile
grep -rn -iE 'stop|restart|kill|pkill' docker/ deploy/ | grep -iE 'datawatch|daemon|systemctl'

# 4. line-number pinning for every in-row citation
grep -n '_kill_orphan_daemon_for|pgrep' scripts/run-tests.sh
grep -n 'kill -9|wait_or_kill|stop_test_daemon' scripts/run-tests.sh
grep -n 'kill' .github/workflows/owasp-zap.yaml scripts/sec-assess/sandbox.sh scripts/e2e-pwa.sh
grep -n -E 'KillSessionsOnExit|Shutting down|recordShutdownCause|runStop|runRestart|runAutoUpdater|runUpdate|syscall.Exec' cmd/datawatch/main.go
grep -n 'DW_LIVE_RESTART_TEST|bl462RestartCommand' internal/session/*.go
grep -n 'Restart=|ExecStart=' install/install.sh install/systemd/datawatch.service
cat install/uninstall.sh
grep -n 'Restart=|ExecStart=' install/install.sh install/systemd/datawatch.service deploy/systemd/*.service
```

Plus direct reads of: `cmd/datawatch/main.go:1840-1870` (signal handler),
`:7645-7665` (server restart closure), `:8480-8525` (auto-updater),
`:9231-9395` (`systemdMainPID`, `systemdUnitName` at `:9224`,
`delegateToSystemd`, stop/restart CLI), `:13280-13300` (update),
`:2870-2890` (router restart closure);
`internal/server/api.go:9715-9765`; `internal/server/server.go:1062-1064`;
`internal/mcp/server.go:2790-2820`; `internal/router/router.go:1039-1040,
:1358-1363`;
`internal/session/restart_safety_live_test.go:24-43, :213-282, :345`,
`parked_prd_resume_live_test.go:79, :106-107`, `restart_adopt_live_test.go:332`;
`scripts/run-tests.sh:70-86, :326-354`; `scripts/e2e-pwa.sh:48-50, :101`;
`scripts/sec-assess/sandbox.sh:15-22, :88-100, :113-119`;
`scripts/eval-model-tiering.sh:270-271`;
`.github/workflows/owasp-zap.yaml:158-161, :238-241`;
`install/uninstall.sh` (full); `install/install.sh:488-490, :510-512,
:528-530, :543-545`; `install/systemd/datawatch.service:12-14`;
`install/systemd/install-service.sh:13-15`; `Makefile:203-207`;
`deploy/systemd/datawatch-stats.service:35-40`;
`internal/shutdowncause/cause.go:14-24`, `capture_linux.go:188-195, :266-274`.

All line numbers were re-verified with `grep -n`/`sed -n` against the
current tree on 2026-10-23 (session h-87ec).

## 8. Remediation — closing the daemon-reachable paths (session h-7959, 2026-10-23)

The audit above proved that three classes of in-repo code are reachable by a
live daemon (tests #10, scripts #9, and pidfile restores). This section
records the fixes applied afterward, each with the code change and the
evidence that the daemon can no longer cause a production stop through it.
Nothing in this section touched a running service; all verification is
static, unit-level, or against throwaway processes the verifying command
itself started.

### 8.1 Live restart tests can no longer fall back to production (#10)

**Changed** — `internal/session/restart_safety_live_test.go`:

- `bl462RestartCommand(t)` now has exactly one path: `DW_LIVE_RESTART_CMD`,
  passed through `bl462RestartEnv`. The default `systemctl --user restart
  datawatch` fallback and the `datawatch restart` CLI fallback are DELETED.
- New `bl462SandboxRestartContract(t) int` gate (called by
  `TestBL462_DaemonRestartSurvivesLive` and, via `bl462RestartCommand`, by the
  two companion tests): requires `DW_BASE`, `DW_LIVE_RESTART_CMD`, and
  `DW_LIVE_SANDBOX_PID` to be set and numeric; verifies the pid is a live
  process (`kill -0`) and that its `/proc/<pid>/cmdline` is a datawatch
  daemon; `t.Skip`s with an explicit message when the contract is unmet and
  `t.Fatal`s (refuse to run) when the pid is dead or not a datawatch binary,
  so a recycled pid number can never be signaled.
- `bl462RestartEnv` no longer has the CLI exception that kept `TMUX_TMPDIR`;
  both `TMUX` and `TMUX_TMPDIR` are now always stripped (they were only an
  optimization for the deleted CLI path).

**Changed** — `internal/session/parked_prd_resume_live_test.go:` and
`restart_adopt_live_test.go` — doc headers updated to the contract, and both
test bodies call `bl462SandboxRestartContract(t)` after the opt-in gate so
they fail fast in the safe (skip) state.

**Evidence the daemon can no longer execute this path:**

- `go vet ./internal/session/` clean; `golangci-lint run ./internal/session/`
  reports 0 issues; gofmt clean.
- Behavior matrix (observed, 2026-10-23):
  - no `DW_LIVE_RESTART_TEST` → all four live tests `--- SKIP` (0.00s, no
    command run);
  - `DW_LIVE_RESTART_TEST=1` without `DW_BASE`/cmd/pid → `--- SKIP`
    ("DW_LIVE_RESTART_TEST=1 is set but the sandbox daemon contract is not —
    set DW_BASE, DW_LIVE_RESTART_CMD and DW_LIVE_SANDBOX_PID");
  - full contract but `DW_LIVE_SANDBOX_PID=999999` (dead pid) → `--- FAIL`
    with "is not a live process; refusing to run a restart command targeting
    a pid this suite does not own (the number may be recycled)"; the
    `DW_LIVE_RESTART_CMD` value (`echo should-not-run`) never executed.
- `grep` of the tree for the deleted defaults: the only remaining
  `systemctl --user ... datawatch` text outside docs is fixture data in
  `internal/shutdowncause/*_test.go` (annotated `service-scope-ok`; it is
  expected-value strings, never executed).

### 8.2 Other daemon-reachable hits: pid-from-file kills now verify ownership

The rule: a script that signals a pid read from a file (or a pattern) must
first verify the pid is alive AND that its cmdline is the daemon that script
harness started.

| Site | Before | After |
|---|---|---|
| `scripts/run-tests.sh` `_kill_orphan_daemon_for` | `pgrep -f "config.*${dir}/config.yaml"` → `kill $pid` | each candidate pid re-verified: `/proc/$pid/cmdline` must contain `datawatch` AND `${dir}/config.yaml`, else skip with a log line |
| `scripts/run-tests.sh` `stop_second_test_daemon` | `kill -0` only, then `wait_or_kill` | cmdline must contain `datawatch` AND this run's own `<FED_PEER_DATA|dirname pidfile>/config.yaml` (the exact config TS-785 started the peer against), else don't kill |
| `scripts/eval-model-tiering.sh` SIGTERM self-test | `kill -TERM $(cat $tmp/task.pid)` blind | new `_dw_ppid`/`_dw_is_descendant` helpers: the task.pid value must be alive AND a /proc-verified descendant of the bg job the script started (`$bgpid`), else don't signal |
| `tests/live-smoke/tree-serialization-smoke.sh` cleanup | `kill $(cat daemon.pid)` blind | pid must be alive AND its cmdline must be `*datawatch*config.yaml*serve*` (the scratch daemon this script started), else don't kill |

**Evidence:** `bash -n` clean on all four scripts; the matching patterns are
pure local pid-ownership checks (liveness + `/proc/<pid>/cmdline`
membership against the harness's own startup marker) — no service-manager
invocation remains on any kill path, and none of these paths runs without
the script having started the process first (each site is in a script the
daemon spawns as a child; the production daemon, by contrast, is a
`systemd`-managed unit whose pid never appears in any of these pidfiles —
`install/systemd/datawatch.service` uses the default cgroup placement, and
the pidfiles named above are only ever written by
`scripts/run-tests.sh`/`TS-785.sh`, the tiering self-test's own bg job, and
the smoke script's own `echo $!`).

### 8.3 New checker: `scripts/check-no-production-service-control.sh`

- `set -euo pipefail`; scans `scripts/`, `tests/`, `.datawatch/verify/`
  (when present) plus every `*_test.go` in the tree (vendor/ and .git/
  excluded); never executes any service command — it only greps.
- Flags, family by family, with `file:line` and a non-zero exit:
  1. `(systemctl|smctl|launchctl|systemd) ... stop|restart|reload|kill|off ... datawatch`;
  2. `datawatch stop` / `datawatch restart` as CLI commands (word-boundary
     safe: `mydatawatch stop`, `datawatch start`, and `datawatch
     restarted` do NOT match);
  3. `pkill`/`killall ... datawatch` (comment text after `#` does not make
     an unrelated kill match).
- Exemptions, line-level: comment-only lines (`#`/`//`), lines carrying the
  `service-scope-ok` marker (fixture strings in `internal/shutdowncause`
  tests that assert on expected `cause.String()` text), and the checker
  itself. Read-only verbs (`status`, `is-active`, `show-environment`, ...)
  and `datawatch start` are intentionally not violations.
- `CHECK_ROOT` overrides the scanned root so tests can point it at a fixture
  tree (done: a planted-violation fixture fails with exactly the 5 planted
  hits and none of the 4 legitimate exceptions; the real tree passes).
- Wired into `make lint`: new `check-service-scope` target beside
  `check-tmux` (`lint: check-tmux check-service-scope check-go-pins`), and
  `.datawatch/verify/production-service-guard.sh` already required it and
  now passes (it runs this checker, the tmux checker, `go vet`, and greps
  the Makefile for the wiring).

**Evidence:** `bash scripts/check-no-production-service-control.sh` → `OK:
no production service-control hits` (exit 0) on the tree; the planted
fixture (see above) → `FAIL: 5 production service-control hit(s)` (exit 1)
with the expected `file:line` rows; `bash
.datawatch/verify/production-service-guard.sh` → all green; `make -n lint`
shows both checkers in the prerequisite chain.

### 8.4 Test status after remediation

- `go vet ./internal/session/` — clean.
- `golangci-lint run ./internal/session/` — 0 issues. (Pre-existing
  `errcheck` findings in `internal/shutdowncause/{capture_linux.go,
  cause.go, cause_linux_test.go}` date from before this session — untouched
  by #8.1–#8.3's code changes; they make `make lint` fail on the whole
  package, which is the pre-existing state of this worktree.)
- `go test ./internal/shutdowncause/` — 20 pass (fixture-string annotation
  changed no behavior).
- Live restart tests: skip (not run) in all contract-incomplete states, per
  #8.1 — i.e., they cannot be the sender of any production SIGTERM.

## 9. E2E verification gate — throwaway instance, all forms (session h-9e56, 2026-10-23, PASS)

Executed against **throwaway instances only** (binary built under
`.tmp-evidence/`, isolated data dir + config bound to 127.0.0.1 on one
free port; no production service started, signalled, or touched):

| Gate step | Expected | Observed |
|-----------|----------|----------|
| `go build -o datawatch ./cmd/datawatch/` | exit 0 | exit 0 (4 s) |
| start instance-1 (pid 3741884, 127.0.0.1:19487) | healthy | "datawatch server listening … 19487", loopback ok |
| child-shell `kill -TERM` | log "shutdown cause:" + audit `shutdown` entry with signal + `last-shutdown.json` | all three: `shutdown cause: … signal=terminated source=procscan sender_pid=3744119 sender_user=dmz`; audit `action=shutdown subject=terminated, details.signal=terminated`; JSON present at 0600 |
| second start, same data dir | "previous shutdown: signal terminated from …" | exact line with child-shell cmdline + timestamp; record consumed |
| SIGKILL fresh start; start again | "previous shutdown: unknown/crash" | exact line (no record readable) |
| `POST /api/restart` | API-initiated form | `shutdown cause: signal= source=api-restart via="api-restart"`; audit entry `via=api-restart`; `last-shutdown.json` `source=api-restart`; next start prints `previous shutdown: api-restart at 2026-10-23T12:07:40-04:00` |
| `go vet` + `go test` `internal/shutdowncause` & `internal/audit` (TMUX unset) | pass | vet clean; 96 tests passed |
| `scripts/check-no-production-service-control.sh`, `scripts/check-no-unscoped-tmux.sh` (TMUX/TMUX_TMPDIR unset) | pass | both `OK`, exit 0 |

One environmental note: in this harness every process inherits
`INVOCATION_ID` (the shell tree runs under `systemd --user`), so after the
API-marked cause was fully attributed and persisted, instance-3 took the
*systemd-exit recovery* branch of `daemonRestartFn` ("exiting for
Restart=") instead of the `selfRestart` shell-spawn branch. That branch
choice is the intended production behavior for a supervised daemon, and
either way the cause was written before the exit — which is the property
this gate verifies. All scratch instances were killed afterwards; no
scratch processes remain.
