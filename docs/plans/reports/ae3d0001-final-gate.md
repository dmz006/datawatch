# ae3d0001 final gate report

Counts only. 3 of 4 checks passed, 1 failed (CPU-load run). Data races reported: 0.

| Check | Result |
|---|---|
| race, count=3 | pass |
| race, count=3, GOMAXPROCS=1 | pass |
| race, count=3, under CPU load | fail |
| vet (autonomous, server, router) | pass |

Check 3 detail: 1 test failure (a timing-sensitive assertion on a stale-read recompute count) and the 10m go test timeout was reached; 0 data race stacks.
