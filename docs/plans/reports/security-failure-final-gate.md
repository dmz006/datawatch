# Security failure report — final gate

Run on 2026-10-30 from the `automaton/f1f20052` worktree, after the fixes for the rules-review findings
(B127, B128, B129; see `docs/plans/README.md`). Every command ran under `env -u TMUX -u TMUX_TMPDIR`, so no
tmux command could reach a tmux socket. Times are local, on the day of the run. Logs were kept in the
session scratchpad, not in the repository.

## Verdict

**Result: PASS.** Every command in the gate below exited 0, the live check (`security-failure-shadow-live-check.md`)
passes, and the PWA story TS-839 passes. One gate failure appeared during the run: `TestSmokeSectionCountDocMatches`
(`tests/ci`) failed because the new `release-smoke.sh` section 88 was not yet counted in `docs/testing.md`.
It was fixed in this pass and the full suite was rerun and passed.

## Commands

| # | Command | Start | End | Elapsed | Exit | Result |
|---|---|---|---|---|---|---|
| 1 | `go build ./...` | 03:49:30 | 03:49:33 | 3 s | 0 | no output |
| 2 | `go vet ./cmd/datawatch/ ./internal/autonomous/ ./internal/config/ ./internal/federation/ ./internal/mcp/ ./internal/router/ ./internal/server/` | 03:49:33 | 03:49:34 | 1 s | 0 | no output |
| 3 | `go test -count=1 ./...` (first run) | 03:49:39 | 03:52:23 | 164 s | 1 | `tests/ci`: `TestSmokeSectionCountDocMatches`, fixed below |
| 4 | `go test -count=1 ./...` (after the fix) | 03:56:13 | 03:58:57 | 164 s | 0 | 82 packages `ok`, 0 `FAIL`, 12 with no test files |
| 5 | `go test -count=1 -coverprofile … ./internal/autonomous/` | 03:59:03 | 03:59:34 | 31 s | 0 | see Coverage |
| 6 | `node --test internal/server/web/*.test.js` | 03:49:34 | 03:49:36 | 2 s | 0 | 1591 pass, 0 fail, 0 cancelled, 0 skipped |
| 7 | `node --check internal/server/web/app.js` | 03:49:36 | 03:49:36 | <1 s | 0 | no output |
| 8 | `make lint` | 03:55:24 | 03:56:01 | 37 s | 0 | `golangci-lint run ./...`: `0 issues.` |
| 9 | `make build` | 03:56:01 | 03:56:08 | 7 s | 0 | `docs-index-gen` wrote the index; binary built, version 9.38.0 |
| 10 | `scripts/check-no-direct-ignore-writes.sh` | 03:49:36 | 03:49:36 | <1 s | 0 | `OK: generated ignore files match the registry` |
| 11 | `scripts/check-no-internal-refs.sh` | 03:49:36 | 03:49:36 | <1 s | 0 | `PASS: no internal refs in user-facing surfaces.` |
| 12 | `scripts/check-howto-coverage.sh` | 03:49:36 | 03:49:36 | <1 s | 0 | `PASS: every howto under docs/howto/ has exec_steps or is LLM-only.` |
| 13 | `bash -n scripts/release-smoke.sh` | 03:49:36 | 03:49:36 | <1 s | 0 | syntax OK |
| 14 | PWA story `scripts/test-stories/TS-839.sh` (`pwa/TS-839.mjs`) | 03:47:30 | 03:47:35 | 5 s | 0 | passes; run against the sandbox daemon |

Row 3 is the first failed run; it is kept so the gate shows the failure and its fix. A second run with output
captured (03:52:27–03:55:07, exit 1) showed the same single failure before the fix was made. The fix is one sentence in
`docs/testing.md`: the count of top-level numbered sections is now 89 (it was 88), and the new section 88
(the security failure report, REST route and comm verb) is named there. Row 4 is the run that counts.

## Coverage

From row 5, `go tool cover -func` on the `internal/autonomous` package:

* `DecideSecurityFailure` (`security_failure_decide.go`): **100.0%**
* `defaultSecurityFailureLog` (`security_failure_pass.go`): **100.0%**
* `decideSecurityFailure` (`security_failure_decide.go`): 96.4%. The uncovered lines were not examined in this
  pass; the two public functions named in the task are at 100%.

## Monitoring & Observability Rule

`AGENT.md` requires six items for a new feature. Met where they apply, declared N/A with the reason where they
do not:

1. **Stats metrics** (`SystemStats`): N/A. The pass runs inside the existing repo CI watch poll; no new
   subsystem, counter, gauge or resident process was added.
2. **Stats endpoint** (`GET /api/<subsystem>/stats`): N/A for the same reason. The read surface is
   `GET /api/autonomous/security-failures`, a list of stored reports, listed in `openapi.yaml`.
3. **MCP tool**: **met**. `security_failure_list` (`internal/mcp/security_failure_tools.go`), capability
   `CapAutonomousRead` in `internal/federation/mcp_tool_caps.go`.
4. **Web UI card** (Monitor tab): N/A. The card is on the Automation tab (Chains sub-tab), not the Monitor tab,
   because it shows reports of the Automata, not system stats. It is tested by TS-839.
5. **Comm channel command**: **met**. `autonomous security-failure [run-id]` (`internal/router/security_failure_comm.go`),
   with the token fix in B128.
6. **Prometheus metrics**: N/A. The feature has no numeric counters or gauges of its own.

## Section A tokens

* **rules**: the rules-review findings (B127, B128, B129) and the Monitoring & Observability Rule (above).
* **tests**: `go test ./...` 82 `ok`, 0 failed (row 4); `node --test` 1591 pass (row 6); TS-839 passes (row 14).
* **changelog**: the existing `CHANGELOG.md` entry for the security failure report covers this feature (the fixes
  are to a feature not yet released, so this pass adds no separate entry).
* **id-check**: `check-no-internal-refs.sh` PASS (row 11). The B-numbers in `docs/plans/README.md` are in the
  plans area, which is where the rule puts them.
* **leak-check**: no line this pass adds contains a local path, host, user or process id. The matches a
  search of the changed files finds are in lines that already existed at HEAD, in `cmd/datawatch/main.go`,
  `docs/plans/README.md` and `docs/testing.md`; none is on an added line.
* **node-check**: `node --check internal/server/web/app.js` exit 0 (row 7), `node --test` 1591 pass (row 6).
* **make-build**: `make build` exit 0 (row 9).
* **lint**: `make lint` `0 issues.` (row 8).

## Live check

`docs/plans/reports/security-failure-shadow-live-check.md` ends with **`Result: PASS`**. Summary:

* Shadow: the report is stored with a `would_stop` line; nothing is stopped or alerted; the run is assessed once.
* `on`: the linked Automaton is blocked, the report is copied byte-identically into its triage record, and
  exactly one alert is raised.
* REST, MCP, CLI, the comm verb (with a token set) and the PWA card agree in both modes.
* The saved `security_workflows` list survives a save and a restart (B127).
* No security run on GitHub was rerun, cancelled or deleted.
* One disclosed deviation: the fixture Automata were patched to `completed` out of band, because no API sets
  `post_verify_sha` without a real task session.

## Parity issue

`docs/plans/parity-issues/security-failure-exception.md` records the datawatch-app parity issue as
**#306** (https://github.com/dmz006/datawatch-app/issues/306), titled "[sync] feat(automaton): security
failure report route and PWA card".

## Files this pass changes

Uncommitted at the time of the gate (`git status --short`):

```
 M cmd/datawatch/live_autonomous_config.go     (repoCIWatchFromYAML; B129)
 M cmd/datawatch/main.go                       (boot bridge, SetWebToken on router and test router; B128, B129)
 M docs/config-reference.yaml                  (the key is reachable via config_set)
 M docs/implementation.md                      (same)
 M docs/mcp.md                                 (row label Automata)
 M docs/plans/2026-10-25-automation-supervisor-duties-proposal.md  (Step 7 parity)
 M docs/plans/README.md                        (B127, B128, B129)
 M docs/plans/reports/security-failure-shadow-live-check.md
 M docs/plans/reports/security-failure-final-gate.md
 M docs/testing.md                             (section count 89; the gate fix)
 M internal/autonomous/security_failure_pass.go  (the gh runner seam)
 M internal/config/config.go                   (default security_workflows; B127)
 M internal/router/router.go                   (SetWebToken; B128)
 M internal/router/sx2_parity.go               (commDo, Authorization header; B128)
 M scripts/release-smoke.sh                    (section 88)
?? cmd/datawatch/repo_ci_watch_boot_test.go
?? internal/autonomous/security_failure_decide_caller_test.go
?? internal/autonomous/security_failure_log_test.go
?? internal/config/repo_ci_watch_security_workflows_test.go
?? internal/router/security_failure_comm_auth_test.go
?? scripts/test-stories/TS-839.sh
?? scripts/test-stories/pwa/TS-839.mjs
```

The sandbox directory was removed after the live check. `scripts/test-stories/pwa/node_modules` is the tracked
symlink again, restored from the index after a local `npm ci` replaced it during the PWA run.

Result: PASS
