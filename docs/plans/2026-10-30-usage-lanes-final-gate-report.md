# Usage calculator lane stamp: final gate report

**Date:** 2026-10-30
**Commit under test:** `49324bc1c` (branch `automaton/b3270001`, worktree clean)
**Toolchain:** go1.27.2 linux/amd64, node v24.15.0, golangci-lint 2.14.0

Feature: the usage calculator no longer takes a Claude lane count from the person typing a
reading. The daemon stamps each reading with the Claude lanes the ledger sees running
(`lanes`, with `lanes_source`), the advice reports `lanes_running_now` apart from the
what-if, and `GET /api/usage/advice?lanes=N` is a what-if that is never saved.

This is the final gate. It changed no code. Its only repository output is this report.
Every command ran under `env -u TMUX -u TMUX_TMPDIR`, from the repository root, after the
last code commit. The commands ran one at a time with nothing else running, so no command
overlapped another.

**Gate rule:** the result is `Result: PASS` only if every recorded command exited 0. A command
that exited non-zero is a failed command and makes the gate `Result: FAIL`, whatever a later
rerun does; a rerun is recorded as extra evidence, never as a replacement.

## Parity surface

| Surface | Touched or excluded | Reason |
|---|---|---|
| REST | Touched | `POST /api/usage/readings` ignores a caller `lanes` and stamps `lanes` and `lanes_source`; `GET /api/usage/advice` returns `lanes_running_now`, `lanes_stamped` and `what_if_lanes`. OpenAPI updated in `docs/api/openapi.yaml` and `internal/server/web/openapi.yaml`. Driven live in `docs/plans/2026-10-30-usage-lanes-live-check.md`. |
| MCP | Touched | `usage_reading_add` takes `lanes` as deprecated and ignored; `usage_advice` `lanes` is the what-if; `usage_readings` describes `lanes_source` (`internal/mcp/usage_calculator.go`, tests in `internal/mcp/usage_calculator_test.go`). |
| CLI | Touched | `datawatch usage reading add --lanes` is deprecated and not sent; `datawatch usage advice --lanes` is the what-if (`cmd/datawatch/cli_usage.go`, tests in `cmd/datawatch/cli_usage_test.go`). |
| Comm channel | Touched | `usage reading <percent> lanes=N` accepts and drops the lane value; `usage advice lanes=N` is the what-if (`internal/router/commands.go`, `internal/router/usage_calculator.go`). |
| YAML/config | Excluded | No config key was added, renamed or removed. The existing `usage.calculator.*` keys are unchanged; the diff against `main` touches no config file (the only `yaml` files in it are the two OpenAPI documents). |
| PWA | Touched | The reading form lost its lanes input; the calculator card gained the running-now line and the what-if control (`internal/server/web/app.js`); locale keys added to all five bundles; story `TS-840` registered in `run-all.mjs`. |
| Android | Excluded from this change, tracked | The lane stamp changes the response shape the mobile app reads. Covered by the datawatch-app parity issue `dmz006/datawatch-app#315` (`docs/plans/parity-issues/2026-10-30-usage-lanes-stamp.md`). No Android code lives in this repository. |
| iPhone | Excluded from this change, tracked | Same issue, `dmz006/datawatch-app#315`. No iPhone code lives in this repository. |

## Commands

Each entry is the command as run, its exit code and the last lines of its output. Output is
trimmed to the final lines; the full logs were not committed. `<scratch>` stands for a
session scratch directory outside the repository.

### 1. Build the daemon outside the repository

```
env -u TMUX -u TMUX_TMPDIR go build -o <scratch>/bin/datawatch ./cmd/datawatch/
```

Exit code: **0**. Output: none (the build printed nothing).

### 2. Vet the touched packages

```
env -u TMUX -u TMUX_TMPDIR go vet ./internal/usagecalc/ ./internal/server/ ./internal/mcp/ ./internal/router/ ./cmd/datawatch/
```

Exit code: **0**. Output: none (vet printed nothing).

### 3. Test the touched packages

```
env -u TMUX -u TMUX_TMPDIR go test -count=1 ./internal/usagecalc/ ./internal/server/ ./internal/mcp/ ./internal/router/ ./cmd/datawatch/
```

Exit code: **0**. Last lines:

```
ok  	github.com/dmz006/datawatch/internal/usagecalc	0.077s
ok  	github.com/dmz006/datawatch/internal/server	68.117s
ok  	github.com/dmz006/datawatch/internal/mcp	1.925s
ok  	github.com/dmz006/datawatch/internal/router	108.413s
ok  	github.com/dmz006/datawatch/cmd/datawatch	108.489s
```

### 4. Test the whole repository

```
env -u TMUX -u TMUX_TMPDIR go test -count=1 ./...
```

Exit code: **1 (FAILED)**. 81 packages `ok`, one package failed. Last lines:

```
--- FAIL: TestDaemonCleanupHelper (31.87s)
    --- FAIL: TestDaemonCleanupHelper/InterruptSighup (30.33s)
        daemon_cleanup_test.go:359: driver did not end within 30s on mode wait
FAIL
FAIL	github.com/dmz006/datawatch/tests/ci	87.182s
?   	github.com/dmz006/datawatch/tests/live-smoke/commitguard	[no test files]
FAIL
```

Notes. `TestDaemonCleanupHelper/InterruptSighup` is a 30 second signal timing test of the
test-daemon cleanup helper in `tests/ci`. This branch does not touch `tests/ci` or
`scripts/test-daemon-lib.sh`. The same test failed the same way in an earlier whole-repository
run of this gate, which overlapped `make lint`; this run had nothing else running, so lint load
does not explain it. The cause is not proven. The failure is intermittent (see command 10).
No code was changed in response.

### 5. PWA node suite

```
env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/*.test.js
```

Exit code: **0**. Last lines:

```
ℹ tests 1665
ℹ suites 0
ℹ pass 1665
ℹ fail 0
ℹ cancelled 0
ℹ skipped 0
ℹ todo 0
ℹ duration_ms 2021.241144
```

### 6. Internal-ref leak check

```
env -u TMUX -u TMUX_TMPDIR bash scripts/check-no-internal-refs.sh
```

Exit code: **0**. Last lines:

```
==> Internal-ref leak audit (BL###, F##, B##, S## in user-facing strings)
✓ PASS: no internal refs in user-facing surfaces.
```

### 7. Locale and story checks from the locales unit

```
env -u TMUX -u TMUX_TMPDIR bash .datawatch/verify/calculator-lanes-locales-story.sh
```

Exit code: **0**. Last lines:

```
locales ok: 148 usage_calc keys in five bundles
ok  	github.com/dmz006/datawatch/internal/server	0.229s
calculator-lanes-locales-story: ok
```

The script checks that every `usage_calc_*` key exists in all five bundles, that English
carries no PRD wording, that the locale guard test passes, and that story `TS-840` parses and
is registered in `run-all.mjs`.

### 8. Lint

```
env -u TMUX -u TMUX_TMPDIR PATH=$PATH:/home/dmz/go/bin make lint
```

`golangci-lint` is installed at `/home/dmz/go/bin/golangci-lint` and was not on the default
`PATH`, so the home go bin directory was appended. Exit code: **0**. Last lines:

```
bash scripts/check-no-internal-refs.sh
==> Internal-ref leak audit (BL###, F##, B##, S## in user-facing strings)
✓ PASS: no internal refs in user-facing surfaces.
bash scripts/check-no-direct-ignore-writes.sh
==> Generated ignore file audit (.trivyignore, docs/security-review.md)
OK: generated ignore files match the registry
golangci-lint run ./...
0 issues.
```

A first attempt at this command in the gate runner exited 127 because the runner passed `-u`
after the `PATH=` assignment, so `env` tried to run `-u` as a program. That was a mistake in
how the command was written, not a lint result; lint had not run. The entry above is the
corrected invocation and is the only lint run.

### 9. Verification script

```
env -u TMUX -u TMUX_TMPDIR bash .datawatch/verify/usage-lanes-final-gate.sh
```

Exit code: **0**. Last lines:

```
ℹ tests 1665
ℹ pass 1665
ℹ fail 0
usage-lanes-final-gate: ok
```

The script checks that this report exists with its three sections and a `Result: PASS` or
`Result: FAIL` line, then runs vet, the touched-package tests and the PWA node suite. It
passes for a report that reads FAIL; it does not test the whole repository, so a pass here does
not clear command 4. It ran against the previous version of this report, and was run again
against this one with the same exit code 0 after this file was written.

### 10. Extra evidence: `tests/ci` alone

```
env -u TMUX -u TMUX_TMPDIR go test -count=1 ./tests/ci/
```

Exit code: **0**. Last line:

```
ok  	github.com/dmz006/datawatch/tests/ci	50.675s
```

This is not part of the required command list. It shows the failure in command 4 does not
reproduce when `tests/ci` runs on its own, which fits a timing or load dependence under
whole-repository package parallelism. It does not void command 4.

## Result

Commands 1, 2, 3, 5, 6, 7, 8 and 9 exited 0. Command 4 (`go test -count=1 ./...`) exited 1:
`tests/ci` `TestDaemonCleanupHelper/InterruptSighup` timed out after 30 seconds. The same test
failed in an earlier whole-repository run of this gate as well, and it passes when `tests/ci`
runs alone, so it is an intermittent failure that is not proven to be a load effect and not
shown to be unrelated to this branch beyond the fact that the branch does not touch `tests/ci`.
Under the gate rule stated at the top, one failed command is a failed gate.

Failed commands: 4 (`env -u TMUX -u TMUX_TMPDIR go test -count=1 ./...`).

Result: FAIL
