# Auto-lanes smoke report

Counts only. Every check ran in process: a fake readings source on a fixed clock,
the real calculator service, the real change log in a temporary directory, a fake
settings writer, and an httptest server for the status endpoint. No daemon was
started, no operator folder was read or written, and no model was pulled or loaded.

## Checks

| # | Check | Result |
|---|-------|--------|
| 1 | Light recorded burn: the hourly decision raises the lane count to 3 | PASS |
| 2 | Heavy recorded burn: the decision lowers the lane count to 1 | PASS |
| 3 | Change log entry written with from, to, direction, policy and the fixed-clock time | PASS |
| 4 | Apply step: the fake settings writer receives exactly the chosen count, once | PASS |
| 5 | Target equal to the cap in force: decision logged, writer not called | PASS |
| 6 | Lane count never above the operator maximum (special items included) | PASS |
| 7 | Zero-lane advice settles at 1 lane, never zero | PASS |
| 8 | Hourly rhythm: one pass at start and one per tick, each using the readings recorded then | PASS |
| 9 | Failed apply: cap unchanged, decision still logged | PASS |
| 10 | Feature off, three reading profiles: no advice call, no write, no log entry, operator cap untouched | PASS |
| 11 | Feature off against the real registry: operator cap unchanged; moves only after the switch is on; left alone after it is off again | PASS |
| 12 | Status endpoint, feature on: target, direction, special count, last change and recent changes | PASS |
| 13 | Status endpoint: heavy burn gives 1 lane down; light burn plus special items stops at the maximum | PASS |
| 14 | Status endpoint, feature off: no target or direction, operator cap shown untouched, no log entries | PASS |

## Totals

- Checks: 14
- Passed: 14
- Failed: 0
- Daemons started: 0
- Models pulled or loaded: 0
- Existing tests that report a data race under the race detector: 1 (not part of
  this smoke; the plain test run passes)
