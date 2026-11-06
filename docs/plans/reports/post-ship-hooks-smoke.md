# Post-ship hooks — smoke (in-process)

Date: 2026-11-01
Scope: smoke of all four built-in post-ship actions, end to end, in process only.

Method: each built-in was configured on a real pipeline run simulated against a
temporary repository, with a fake ship runner whose hosting-service half is a
local in-process HTTP service (httptest), a fake delivery notifier, and a fixed
clock standing in for the wall clock. Simulated deploy, then hooks. No daemon,
no real model, no remote, no real git host.

## Checks

| # | Check | Result |
|---|---|---|
| 1 | Every built-in action runs after the simulated deploy, in configured order | PASS |
| 2 | The hook pass does not change or block the ship's successful result | PASS |
| 3 | The action that files an issue reaches the fake hosting service exactly once, with placeholders rendered | PASS |
| 4 | The action that marks the registry updates the right row, marked shipped | PASS |
| 5 | The release-tag action asks the fake ship runner for exactly one tag | PASS |
| 6 | The notification action delivers the rendered message exactly once to the fake notifier | PASS |
| 7 | Failing hooks (refused filing, refused delivery) are recorded as failed steps with a detail, on their own step | PASS |
| 8 | Failing hooks do not fail or stop the ship; the ship's success is preserved | PASS |
| 9 | A hook listed after a failing one still runs and its work lands | PASS |
| 10 | Disabled hooks record no step and reach no fake wiring at all | PASS |
| 11 | Fixed clock is what the recorded run carries (start and every step end) | PASS |

## Counts

- Checks passed: 11
- Checks failed: 0
- Fake hosting-service issuances accepted: 1 (run 1), 0 (failed run), 0 (disabled run)
- Deliveries received by the fake notifier: 1 (run 1), 0 (other runs)
- Test functions: 3 passed, 0 failed
- Package test totals where the smoke lives, with neighbouring packages: 3190 passed, 0 failed
- Build and vet: clean
