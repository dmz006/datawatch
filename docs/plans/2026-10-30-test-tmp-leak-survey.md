# Survey: test temp-file leaks (private-TMPDIR sweep)

- Date: 2026-10-30
- Version: v9.42.1 (at the time of planning)
- Branch: `automaton/0c000001`
- Tool: `scripts/survey-test-tmp.sh`
- Status: Done — shipped in this branch (on top of v9.42.1; not a tagged release)
- Requirement: every Go package's test binary must leave **zero** files in its
  private `TMPDIR` after it exits. Prefixes to watch: `dw-*`, `svchelper-*`,
  `sched-test-*`, `datawatch-*-task-*` (and any other temp-file prefix).

## Scope

- `scripts/survey-test-tmp.sh` (new survey tool)
- `scripts/clean-test-tmp.sh`, `internal/testhome/` (manifest + sweep, janitor)
- `internal/session/tmp_leak_regression_test.go` (leak guard, extended)
- One-line fixes in `internal/channel`, `internal/llm/backends/{pi,aider,goose,opencode}`,
  `internal/llm/claudecode`, `internal/session` (store crash-safety tests)
- `Makefile` (`clean-test-tmp`, `test-tmp-guard` targets), `docs/testing.md`, `CHANGELOG.md`

## Phases

1. **Baseline survey** — run `bash scripts/survey-test-tmp.sh` (whole module,
   `go test -count=1` per package, each with a fresh private `TMPDIR`,
   `TMUX`/`TMUX_TMPDIR` unset) at the base commit and record what each package
   leaves behind. Status: **Done** (see "What the survey found (before)";
   the before-state below is corrected per the 2026-11-01 rule review).
2. **Fix every leaker** — each leaking test now points its scratch space at a
   test-scoped directory (or isolates the Node compile cache), so every
   artifact dies with `t.TempDir`. Status: **Done**.
3. **Gates and report** — leak guard, `clean-test-tmp`, tmux check, full
   survey, `go build`, `make lint`; results in
   `docs/plans/2026-10-30-test-tmp-cleanliness-report.md`. Status: **Done**.

## How the survey works

`bash scripts/survey-test-tmp.sh [pkgs …]` (default: `./...`) builds and runs
**each package's test binary** under:

```sh
env -u TMUX -u TMUX_TMPDIR TMPDIR=<fresh private dir> go test -count=1 <pkg>
```

and then lists everything still inside the private directory, summarized by
top-level entry (prefix) with counts. A package **passes** only when its
private directory is empty. No `pkill`, no `tmux kill-server`, no shared-state
inspection — only the private directory content.

The run covers all **94 packages** in `go list ./...` (81 test packages + 13
build-only, the latter report as `OK (no test files)`).

## What the survey found (before)

Six packages leaked at the base commit. (The first recorded before-table
counted only `internal/channel`; that was an undercount — the 2026-11-01 rule
review established the backend packages below also leaked, and this table is
the corrected before-state.)

| leaking package | left behind in the private `TMPDIR` | creator |
|---|---|---|
| `internal/channel` | `node-compile-cache/` — 532 entries | `EnsureExtracted` → `npm install --production`; the npm-bundled node (v24) writes its compile cache to **`$TMPDIR/node-compile-cache` by default** — a directory the test process cannot clean up |
| `internal/llm/backends/pi` | `datawatch-pi-task-*`, `datawatch-pi-status-*`, `datawatch-pi-out-*` | `writeTaskFile`/`reserveScratchFile` via `os.CreateTemp` — the launch/cmd-build tests registered no cleanup |
| `internal/llm/backends/aider` | `datawatch-aider-task-*.txt` | `writeTaskFile` via `os.CreateTemp` — no cleanup |
| `internal/llm/backends/goose` | `datawatch-goose-task-*.txt` | `writeTaskFile` via `os.CreateTemp` — no cleanup |
| `internal/llm/backends/opencode` | `datawatch-opencode-task-*` | `writeTaskFile` via `os.CreateTemp` — no cleanup |
| `internal/llm/claudecode` | `datawatch-claudecode-task-*` | `os.CreateTemp` scratch task file — no cleanup |
| `internal/session` | `dw-test-home-*`, `dw-test-tmux-*` | the crash-safety tests SIGKILL a helper whose TestMain-deferred testhome cleanup never runs, orphaning its scratch trees |

No `dw-*` / `svchelper-*` / `sched-test-*` leaks from any other package. The
other known creators were already clean (verified, not leaking):

- `dw-test-home-*` / `dw-test-tmux-*` — `internal/testhome` `Run()`/
  `TmuxIsolation()` register `t.Cleanup` before returning the path.
- `svchelper-*` — `internal/plugins` `TestMain` removes it before `os.Exit`.
- `sched-test-*` / `dw-leak-*` / `dw-tmp-leak-guard-*` — no creator in the
  repo; the strings exist only in the regression guard list
  (`internal/session/tmp_leak_regression_test.go`).

## The fix (after)

- `internal/channel/v6070_bl218_test.go`: both `TestEnsureExtracted_*` tests
  now call `isolateNodeCompileCache`, which sets `NODE_COMPILE_CACHE` to a
  directory **inside the test's `t.TempDir()`** (pre-created, per Node's
  requirement) so node writes the cache into a directory `testing` removes
  automatically. (`NODE_COMPILE_CACHE` is honored by the same node — verified:
  with it set, the 532-entry cache lands in the target dir and nothing is
  left in `$TMPDIR`.)
- `internal/llm/backends/{pi,aider,goose,opencode}`, `internal/llm/claudecode`:
  the affected tests call `testhome.PrivateTmp(t)`, which points the test
  process's `TMPDIR` at a directory under `t.TempDir()` — so
  `os.CreateTemp` scratch files land in a directory the test framework
  removes. The fallback test that *wants* `os.CreateTemp` to fail still
  overrides `TMPDIR` after `PrivateTmp`, so the fallback assertion is
  unchanged.
- `internal/session` store crash-safety tests: the SIGKILLed helper's
  environment gains `TMPDIR=<test-scoped dir>` (last duplicate wins in
  `exec.Cmd.Env`) so its orphaned testhome trees are removed by the outer
  test's cleanup.
- `internal/testhome` gains a creation manifest + `SweepStale`, and the leak
  guard (`internal/session`) is extended with every temp-creating package in
  scope; `make clean-test-tmp` is the manual janitor (see `docs/testing.md`).

## Note: one flaky *test failure* during the early full run (not a leak)

`tests/ci` `TestDaemonCleanupHelper/InterruptSighup` ("driver did not end
within 30s on mode wait") failed once, then passed in a standalone rerun and
in every direct run since. It is a temp-directory **non**-leak, and it is
environment-dependent with a verified explanation: the driver installs a bash
`trap` for SIGHUP, and bash cannot trap a signal whose disposition is already
**ignored** at process start. Under a detached runner that SIGIGNs SIGHUP
(e.g. a `nohup`-style parent — confirmed by rerunning the single test under
`nohup`: fail; run it directly from an ordinary shell: pass), the driver
cannot catch SIGHUP and the interrupt test times out. Gate rule: run `go
test` (including `make test`) from an ordinary shell, not from a
SIGHUP-ignoring detached context; the survey gate itself (private-directory
content) is unaffected in both contexts — it passed here with `tests/ci`
leaving 0 entries.

## Totals

| run | packages | passing (empty private `TMPDIR`) | failing | total leftover entries |
|---|---|---|---|---|
| before (corrected) | 94 | 87 (incl. 13 no-test-file) | 6 leakers (plus 1 flaky test failure, `tests/ci`, not a leak) | 532 (`internal/channel`) + one scratch file per leaking launch path in the 5 backend packages + testhome trees in `internal/session` |
| after | 94 | **94** | 0 | **0** |

The full per-package after-state (every package `OK` with an empty private
directory) is recorded in
`docs/plans/2026-10-30-test-tmp-cleanliness-report.md`.

## Reproduce

```sh
bash scripts/survey-test-tmp.sh
# =>
# OK: all 94 package(s) passed
```

## Gates (all run under `env -u TMUX -u TMUX_TMPDIR` where they touch the harness)

```
bash scripts/survey-test-tmp.sh                        # exit 0, all 94 pass
go test -run '^TestDaemonCleanupHelper$' ./tests/ci/   # ok (flake gone)
go build ./...                                         # ok
make lint                                              # clean (golangci-lint 0 findings)
```

## Parity surface

This change is test-hygiene and build tooling inside the Go module. It adds
or changes no operator-facing behavior, API contract, message format, or
client surface, so every surface in the full set is intentionally excluded:

| surface | status | reason |
|---|---|---|
| REST | excluded | no API added/changed; the leak guard only makes an existing `go test` test stricter |
| MCP | excluded | no tool or schema touched |
| CLI | excluded | no daemon command changed; `make` targets affect operators' local test hygiene only and are documented in `docs/testing.md` |
| comm channel | excluded | no lifecycle/event messages added or reshaped |
| YAML/config | excluded | no config key added, renamed, or defaulted |
| PWA | excluded | no page, state, or wire format touched |
| Android | excluded | no contract the Android client parses changed |
| iPhone/iOS | excluded | no contract the iOS client parses changed |

## Phase checklist

- [x] Functionality implemented per the phase descriptions (survey tool, fixes,
      janitor, guard extension).
- [x] New code has tests: `internal/testhome/manifest_test.go`, the extended
      `internal/session` guard, and the per-package leak tests that now pass.
- [x] N/A: live daemon smoke — no daemon behavior changed (test hygiene only).
- [x] `go test ./...` green, including the leak guard (see the report).
- [x] General documentation checklist: `CHANGELOG.md` entry added;
      `docs/config-reference.yaml` N/A (no config surface); `docs/operations.md`
      N/A (no operator workflow change; operator hygiene lives in
      `docs/testing.md`, updated); README/doc-index N/A (no new user-facing
      feature); `docs/testing-tracker.md` N/A (no production test behavior
      changed, only test-suite hygiene of the repo's own suites); no tracker
      IDs leaked; this plan's `Parity surface` section accurate.
- [x] Conditional docs N/A: no new operator-facing concept, data flow, or
      user-followable workflow (internal test-hygiene; covered by
      `docs/testing.md`).
- [x] Mobile-Parity Rule N/A: nothing operator-visible on the PWA or in any
      REST/WS contract changed.
- [x] N/A: no new `t('key')` usage.
- [x] N/A: not a shippable/tagged unit of its own — lands in the next release
      after v9.42.1; version bump happens with that release.
- [x] Phase status lines above updated to reflect Done.
