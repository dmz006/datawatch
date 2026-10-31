# Usage calculator lane stamp: final gate report

**Date:** 2026-10-30
**Commit under test:** `b731ca6af` (branch `automaton/b3270001`)
**Toolchain:** go1.27.2 linux/amd64, node v24.15.0, golangci-lint 2.14.0

Feature: the usage calculator no longer takes a Claude lane count from the person typing a
reading. The daemon stamps each reading with the Claude lanes the ledger sees running
(`lanes`, with `lanes_source`), the advice reports `lanes_running_now` apart from the
what-if, and `GET /api/usage/advice?lanes=N` is a what-if that is never saved.

This is the final gate. It changed no code. Its only repository output is this report.
Every command ran under `env -u TMUX -u TMUX_TMPDIR`, from the repository root, after the
last code commit.

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
ok  	github.com/dmz006/datawatch/internal/usagecalc	0.074s
ok  	github.com/dmz006/datawatch/internal/server	68.014s
ok  	github.com/dmz006/datawatch/internal/mcp	1.916s
ok  	github.com/dmz006/datawatch/internal/router	108.387s
ok  	github.com/dmz006/datawatch/cmd/datawatch	114.326s
```

### 4. Test the whole repository, first run

```
env -u TMUX -u TMUX_TMPDIR go test ./...
```

Exit code: **1**. One package failed; every other package passed. Last lines:

```
--- FAIL: TestDaemonCleanupHelper (31.84s)
    --- FAIL: TestDaemonCleanupHelper/InterruptSighup (30.31s)
        daemon_cleanup_test.go:359: driver did not end within 30s on mode wait
FAIL
FAIL	github.com/dmz006/datawatch/tests/ci	107.497s
?   	github.com/dmz006/datawatch/tests/live-smoke/commitguard	[no test files]
FAIL
```

Context: this run overlapped with command 10 (`make lint`, which runs `golangci-lint`) because
both were started together. `TestDaemonCleanupHelper/InterruptSighup` is a 30 second signal
timing test of the test-daemon cleanup helper. This branch does not touch `tests/ci` or
`scripts/test-daemon-lib.sh`. The cause of the timeout is not proven; the load overlap is a
likely contributor, not a confirmed cause. No code was changed in response.

### 5. Test `tests/ci` alone

```
env -u TMUX -u TMUX_TMPDIR go test -count=1 ./tests/ci/
```

Exit code: **0**. Last line:

```
ok  	github.com/dmz006/datawatch/tests/ci	50.844s
```

### 6. Test the whole repository again, nothing else running

```
env -u TMUX -u TMUX_TMPDIR go test -count=1 ./...
```

Exit code: **0**. 82 packages `ok`, none failed. Last lines:

```
ok  	github.com/dmz006/datawatch/internal/websearch	0.065s
?   	github.com/dmz006/datawatch/internal/wizard	[no test files]
ok  	github.com/dmz006/datawatch/tests/ci	53.868s
?   	github.com/dmz006/datawatch/tests/live-smoke/commitguard	[no test files]
```

### 7. PWA node suite

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
ℹ duration_ms 2024.453931
```

### 8. Internal-ref leak check

```
env -u TMUX -u TMUX_TMPDIR bash scripts/check-no-internal-refs.sh
```

Exit code: **0**. Last lines:

```
==> Internal-ref leak audit (BL###, F##, B##, S## in user-facing strings)
✓ PASS: no internal refs in user-facing surfaces.
```

### 9. Locale and story checks from the locales unit

```
env -u TMUX -u TMUX_TMPDIR bash .datawatch/verify/calculator-lanes-locales-story.sh
```

Exit code: **0**. Last lines:

```
locales ok: 148 usage_calc keys in five bundles
ok  	github.com/dmz006/datawatch/internal/server	0.265s
calculator-lanes-locales-story: ok
```

The script checks that every `usage_calc_*` key exists in all five bundles, that English
carries no PRD wording, that the locale guard test passes, and that story `TS-840` parses and
is registered in `run-all.mjs`.

### 10. Lint

```
PATH=$PATH:/home/dmz/go/bin env -u TMUX -u TMUX_TMPDIR make lint
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

## Result

Every command exited 0 except one: the first `go test ./...` run (command 4) exited 1 on
`tests/ci` `TestDaemonCleanupHelper/InterruptSighup` (a 30 second signal timing test, in a
package this branch does not touch). That run overlapped with `make lint`. `tests/ci` alone
(command 5) and a second whole-repository `go test -count=1 ./...` with nothing else running
(command 6) both exited 0, so the failure did not reproduce. The cause was not proven, and
the first failure is recorded here rather than dropped; an operator who treats a single
failed run as disqualifying should read this as FAIL on command 4.

Failed commands: none in the final runs; command 4 failed once on its first run and passed on
the clean rerun (command 6).

Result: PASS
