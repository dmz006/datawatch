# Event-driven supervisor wiring: final gate report

**Date:** 2026-11-12
**Scope:** the supervisor wake registered at daemon start-up, the fallback interval read by the scheduler,
the peer event subscriber with its setting and controls, and lifecycle kinds in the subscription preview.
Every number below comes from a command run in this branch; nothing is estimated.

**Result:** every recorded command exited 0 and no test failed, with the one caveat in the notes.

| Step | Command | Exit | Outcome |
|---|---|---|---|
| 1 | `go build -o /dev/null ./cmd/datawatch/` | 0 | built |
| 1 | `go vet` on autonomous, server, mcp, router, federation, config, cmd/datawatch | 0 | no findings |
| 1 | `gofmt -l` on the 29 changed Go files | 0 | listed 2 files, both formatted afterwards; a second `gofmt -l` listed nothing |
| 2 | `go test ./... -count=1` (230 s) | 0 | 101 packages ok, 0 failed, 12 without test files |
| 3 | `node --test internal/server/web/*.test.js` | 0 | 3004 tests, 3004 passed, 0 failed |
| 4 | `scripts/check-no-internal-refs.sh`, `scripts/check-no-unscoped-tmux.sh` | 0 | both clean |

## New tests

- `TestStartSupervisorWakeFollowsConfigAndRunsLoop` (cmd/datawatch): with a fake event source, wake off does nothing,
  turning the config block on registers the set and one subscribed event runs the loop once, turning it off removes it.
- `TestSupervisorWake*` (internal/server): the controller registers, replaces and removes the set; an unsubscribed kind
  does nothing; the timer picks up a change.
- `TestSupervisorLoopScheduledCheckHonoursFallbackInterval`, `TestSupervisorLoopIdleTickUsesFallbackInterval`
  (internal/autonomous): fake clock, pass at the interval, live interval change, wake off runs every tick.
- `TestPeerEventsStartReadStopAndEventCrosses`, `TestPeerEventsRefusals` (internal/server): start, one event crosses
  from a fake peer stamped with the peer name, status, stop, and the refusals.
- `TestEventPreviewAcceptsLifecycleAndLogKinds`, `TestEventPreviewLifecycleKindWithoutHistory` (internal/server).

## Notes

- The first broad run found three failures: the MCP golden tool list and the config key test (both updated for the new
  tool names and setting), and the notify-events help text, which had been missing nine subscribable kinds since earlier
  work; the help now lists them. The full run above came after those fixes.
- The two formatting-only edits came after the full test run; the build and the preview tests were rerun after them.
- Not covered by a test: a live daemon with a real supervisor session. The wake is proven against a fake event source.
