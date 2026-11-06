# Deploy feature final gate (counts only)

| # | Check | Result |
|---|-------|--------|
| 1 | go vet | PASS |
| 2 | go test -count=1 (full suite) | PASS (0 failing packages) |
| 3 | make lint | FAIL (1 finding, in the production service-control audit; later lint steps after it did not run because make stopped) |
| 4 | node --check on the web app script | PASS |
| 5 | node --test on the deploy web test | PASS (11 pass, 0 fail) |
| 6 | internal-ref leak audit | PASS |
| 7 | howto coverage audit | PASS |
| 8 | deploy package has no repository name in non-test Go files | PASS (only Go import lines match; no other occurrence) |
| 9 | every deploy key present in config reference and web help links | PASS (7 of 7 keys in both) |
| 10 | Version variable lines unchanged vs merge base (both files) | PASS (0 changed lines) |

Not run: the lint steps after the failing service-control audit (make stopped at the first failure). Nothing else was skipped. No daemon started, no gh run, no version variable changed.

Overall: 9 PASS, 1 FAIL.
