# Deflake round seven: the UI prompt dismissal tests

Counts only. Written by the supervisor at the merge, because the Automaton's own measurement and report tasks were cancelled after their results were recorded (the read-only check failed on scratch files, not on the results).

## Before the fix

- Command family: `TestUIPromptDismiss_` tests, `go test -race -count=100` under one busy CPU loop per core with `GOMAXPROCS=1`.
- Runs: 800. Failures: 6, all data races reported by the race detector in the dismissal tests. The Release failure `no dismissed audit event; got 0 events` did not appear in this reproduction.

## The change

- A bounded wait on the dismissed audit event, with a delayed-hook regression test, in the failing test and in every sibling dismissal test that reads the audit store.

## After the fix

- Same command and load. Runs: 100. Failures: 0. Timed out: 0. 900 passing lines in 265 seconds under a 480 second cap.

## Notes

- The measurement before and after used different run counts (800 and 100) because of the time cap; the after-run is a spot check, and the earlier flake rate was about 1 failure in 130 runs.
- The Release failure signature (no audit event) was not reproduced, so the bounded wait is a precaution for it; the data races were the failures seen under load.

## Parity surface

None. This is a test-only change.
