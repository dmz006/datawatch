# Burn guard final gate: verification report (2026-11-02)

Read-only run. No source files were edited. The only file written is this report.

## Result: FAIL (2 blocking findings)

| Check | Result |
|---|---|
| go build -o datawatch ./cmd/datawatch/ | PASS |
| go vet (6 packages) | PASS |
| go test burnguard, autonomous, server, config, mcp | PASS |
| go test ./cmd/datawatch/ | **FAIL** (TestAutomationParity) |
| node --test internal/server/web/app-burn-guard.test.js | PASS (8/8) |
| make lint | **FAIL** (errcheck) |
| scripts/check-docs-sync.sh | PASS |
| scripts/check-howto-coverage.sh | PASS |
| Friday-night replay climbs the ladder before the recorded limit time | PASS (TestFridayNightProjectsOverWellBeforeLimit, TestFridayNightReplayThroughLadder) |
| Ladder steps back when the series is under the line | PASS (TestStepBackOff, replay step-back segment) |
| No code path ends a running session | PASS (the branch diff adds no kill, terminate, stop-session or send-keys call in non-test Go code, and internal/burnguard has none) |
| Every setting has REST, MCP, CLI, YAML and PWA paths | PARTIAL, see finding 1 |

## Finding 1: TestAutomationParity fails in cmd/datawatch

```
--- FAIL: TestAutomationParity (0.00s)
    automation_parity_test.go:85: REST operation "set_effort_ceiling" has no entry in parityMap; map it to its MCP tool, CLI verb and comm verb
```

- REST: `set_effort_ceiling` is registered in internal/server/automation_ops.go:33.
- MCP: the `automation_set_effort_ceiling` tool exists.
- Comm channel: internal/router/automation_effort_ceiling.go exists.
- CLI: no `set-effort-ceiling` verb or flag was found under cmd/. This is a real CLI gap. The parity test needs both a `parityMap` entry and an actual CLI verb, or a `knownParityGaps` entry with a reason.
- Settings paths: the global `burn_guard.effort_ceiling` setting is also in YAML and in the PWA (app.js).

## Finding 2: make lint fails with one errcheck issue

```
internal/burnguard/tokens.go:141:16: Error return value of `fh.Close` is not checked (errcheck)
	defer fh.Close()
```

All the other lint prerequisite scripts passed.

## Notes
- The first cmd/datawatch test run also exited FAIL for the same single test. Everything else in that package passed.
- The earlier two attempts failed because a read-only task leaves an empty diff, which the verifier rejects. This report file is the task's output.
