## Reproduction

command: GOMAXPROCS=1 timeout 480 go test -race -count=60 -v -run '^TestAutomationCommDecisionAssistantUsage$' ./internal/router/
busy loops: 48, GOMAXPROCS=1, runs: 60, race: yes
failures: 0 of 60
data race reports: 0
stray request: not observed
cause class: not-determined
notes: A second run with the fake daemon handler temporarily logging method, path and sequence number completed 175 more runs (cut off by the 420 s cap) with 0 failures, 0 race reports and no request logged at all.
notes: The "clear=maybe reached the daemon" failure was not reproduced, so no stray request was seen and nothing here shows a late recording or a background goroutine.
notes: Instrumentation was reverted with git checkout and git status was clean; all 48 busy loops were confirmed gone by PID.

## Fix

fix: the fake daemon's log now counts requests that reached its handler and reset() waits until each is recorded, so the fixed 200 ms sleep no longer alone separates one input's window from the next.
cause class: not-determined
notes: No stray request was observed, so this closes the fixture-side window (a request still being handled when the next input resets the log). The router's own HandleTestMessage is unchanged, and no production file or version was touched.
