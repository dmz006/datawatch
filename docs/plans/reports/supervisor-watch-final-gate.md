# Supervisor watch — final gate

Date: 2026-10-30 (branch `automaton/85720001`). All commands were run from the repository root under `env -u TMUX -u TMUX_TMPDIR`, with start and end timestamps. Nothing was fixed by this gate.

Result: **FAIL** — three items failed (see "Failures, verbatim"). The build, vet, web test, internal-ref audit, gofmt, version and parity checks pass.

## Commands run

| # | Literal command | Exit | Result |
|---|---|---|---|
| 1 | `env -u TMUX -u TMUX_TMPDIR go build ./...` | 0 | clean, no output |
| 2 | `env -u TMUX -u TMUX_TMPDIR go vet ./cmd/datawatch ./internal/autonomous ./internal/config ./internal/federation ./internal/mcp ./internal/router ./internal/server` | 0 | clean, no output (these are the packages holding the `.go` files in `git diff --name-only main...HEAD`) |
| 3 | `env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous ./internal/server ./internal/mcp ./internal/router ./internal/config ./internal/federation ./cmd/datawatch` | 1 | **FAIL** — see the `go test` result lines below |
| 3b | `env -u TMUX -u TMUX_TMPDIR go test -count=1 -run 'TestAttemptSnapshotCleansUpTempDir' ./internal/autonomous` | 0 | `ok  	github.com/dmz006/datawatch/internal/autonomous	0.056s` (diagnostic only) |
| 3c | `env -u TMUX -u TMUX_TMPDIR go test -count=1 ./internal/autonomous` | 0 | `ok  	github.com/dmz006/datawatch/internal/autonomous	32.514s` (diagnostic rerun of the one failed package; it does not replace row 3) |
| 4 | `env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-supervisor-watch.test.js` | 0 | tests 9, pass 9, fail 0, cancelled 0, skipped 0 |
| 5 | `env -u TMUX -u TMUX_TMPDIR bash scripts/check-no-internal-refs.sh` | 0 | `✓ PASS: no internal refs in user-facing surfaces.` |
| 6 | `env -u TMUX -u TMUX_TMPDIR bash scripts/check-docs-sync.sh` | 1 | **FAIL** — see below |
| 7 | `env -u TMUX -u TMUX_TMPDIR gofmt -l $(git diff --name-only main...HEAD \| grep '\.go$')` | 0 | no files listed |
| 8 | version check: `git diff main...HEAD -U0 -- internal/server/api.go cmd/datawatch/main.go \| grep -i version` | — | no output; both files read `var Version = "9.42.1"`, the same value as at the merge-base (`git merge-base main HEAD`). Unchanged by this branch |
| 9 | PRD-word scan of the added lines of every non-test changed file, plus Go string literals | — | pass with one note, see "Operator-facing wording" |
| 10 | absolute-home-path scan of the added lines of every changed file and of all new files | — | **FAIL** — see below |

### `go test` result lines (command 3)

```
--- FAIL: TestAttemptSnapshotCleansUpTempDir (0.05s)
FAIL
FAIL	github.com/dmz006/datawatch/internal/autonomous	32.129s
ok  	github.com/dmz006/datawatch/internal/server	65.911s
ok  	github.com/dmz006/datawatch/internal/mcp	1.931s
ok  	github.com/dmz006/datawatch/internal/router	108.593s
ok  	github.com/dmz006/datawatch/internal/config	1.773s
ok  	github.com/dmz006/datawatch/internal/federation	(cached)
ok  	github.com/dmz006/datawatch/cmd/datawatch	113.297s
FAIL
```

## Parity checklist

| Item | Result | Evidence |
|---|---|---|
| Config keys | pass | `SupervisorWatchConfig` in `internal/config/config.go` has eight keys under `autonomous.automation.supervisor_watch`: `mode`, `alert_minutes`, `silent_minutes`, `poll_seconds`, `script_output_path`, `tmux_restart_baseline`, `main_checkout`, `working_model`; validation errors name each key |
| REST read | pass | `GET /api/autonomous/supervisor-watch`, handler `handleSupervisorWatch` in `internal/server/supervisor_watch.go`, registered at `internal/server/server.go:517` |
| MCP tool | pass | `supervisor_watch_list` in `internal/mcp/supervisor_watch_tools.go`, registered at `internal/mcp/server.go:672`, capability `CapAutonomousRead` in `internal/federation/mcp_tool_caps.go` |
| CLI command | pass | `datawatch autonomous supervisor-watch [--json]` in `cmd/datawatch/cli_supervisor_watch.go`, listed in `cmd/datawatch/cli_autonomous.go` |
| Comm verb | pass | `autonomous supervisor-watch` (alias `supervisor_watch`) in `internal/router/sx2_parity.go:1110` and `internal/router/supervisor_watch_comm.go`, in the help line |
| PWA card | pass | `internal/server/web/app.js` (58 supervisor-watch references); `app-supervisor-watch.test.js` 9 of 9 pass |
| Five locale bundles | pass | `en`, `de`, `es`, `fr`, `ja` each hold 29 `supervisor_watch` keys |
| Parity issue file with a real issue number | pass | `docs/plans/parity-issues/supervisor-watch.md` starts `Issue: dmz006/datawatch-app#307`; `gh issue view 307 -R dmz006/datawatch-app` returned the matching title, state OPEN |
| How-to | pass | `docs/howto/automation-supervisor-watch.md`, indexed in `docs/README.md` |
| Config reference | pass | `docs/config-reference.yaml` has the `supervisor_watch:` block with all eight keys |
| OpenAPI | pass | `internal/server/web/openapi.yaml` documents `/api/autonomous/supervisor-watch` |
| Testing tracker | pass | `docs/testing-tracker.md` section "Supervisor watch — shadow first, one finding per kind" with a row per finding kind |
| Definitions glossary | pass | `docs/datawatch-definitions.md` has the "Supervisor watch" entry and a how-to table row |
| CHANGELOG | pass | `CHANGELOG.md` has four added entries for the watch |

All fourteen parity items pass. This table covers the parity surfaces only; the overall result above is FAIL because of the three items below.

## Operator-facing wording

The word PRD does not appear in any operator-facing string the feature added: locale bundles, how-to, operations doc, config reference, openapi, CHANGELOG, CLI, MCP and comm strings, and the PWA all say Automaton or Automata, and no Go string literal added by the branch contains it. The PWA web test also asserts that none of the 29 locale keys say PRD. Remaining hits are Go identifiers and comments (`PRD` type names) and one note:

- `scripts/supervisor-watch.py:146` — the docstring of the new `map_finding` helper says "the PRD id". It is developer documentation inside a script, not printed output, and the script's pre-existing printed lines already use the word. I counted it as not operator-facing; it is listed here so it can be judged.

## Failures, verbatim

### 1. `go test` — `internal/autonomous`

```
--- FAIL: TestAttemptSnapshotCleansUpTempDir (0.05s)
    attempt_snapshot_test.go:159: SnapshotAttemptWork left temp dir attempt-snapshot-2203559920 behind — best-effort cleanup did not run
FAIL
FAIL	github.com/dmz006/datawatch/internal/autonomous	32.129s
```

Notes: `internal/autonomous/attempt_snapshot_test.go` is not among the files this branch changed. The test compares the shared temp directory before and after the call, so a concurrent test in the same package that creates its own `attempt-snapshot-*` directory can trip it. It passed alone (command 3b) and the whole package passed on a rerun (command 3c, 32.514s). The first run is still the gate result and is not overridden by the rerun; this looks like an existing flake of that test, not something this feature introduced.

### 2. `scripts/check-docs-sync.sh`

```
ERROR: embedded PWA docs drift from docs/ — run 'make sync-docs' and re-stage.
>f+++++++++ plans/reports/supervisor-watch-shadow-live-check.md
```

Note: the embedded copy under `internal/server/web/docs/` predates the shadow live-check report that an earlier task wrote under `docs/plans/reports/`. I did not run `make sync-docs` (the script's own remedy), to keep this gate read-only.

### 3. Absolute home path in files written by this feature

(The path values are masked below so this report does not itself carry an absolute home path; the line numbers locate the originals.)

Test fixtures in two new test files use absolute home paths as inert string values:

```
internal/autonomous/supervisor_watch_host_test.go:381:	dir := "<absolute home path A>"
internal/autonomous/supervisor_watch_host_test.go:397:	dir := "<absolute home path A>"
internal/autonomous/supervisor_watch_host_test.go:408:	dir := "<absolute home path A>"
internal/autonomous/supervisor_watch_config_test.go:226:		MainCheckout:        "<absolute home path B>",
internal/autonomous/supervisor_watch_config_test.go:234:	if got := c.MainCheckout; got != "<absolute home path B>" {
internal/autonomous/supervisor_watch_config_test.go:235:		t.Errorf("MainCheckout = %q, want <absolute home path B>", got)
```

No non-test file, how-to, report, parity issue or doc written by the feature contains an absolute home path. The fixtures are never read from disk (a fake probe serves them), so the fix is a repository-relative or synthetic value in those two test files; that belongs to the task that wrote them.
