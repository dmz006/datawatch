# Final gate — counts-only report

Date: 2026-11-05
Scope: observer pull test packages; lint on the changed files. No daemon, no gh, no version change, no file changes.

## Checks

| Check | Result |
|---|---|
| Tests (top-level, count=1) | PASS — 6491 passed, 0 failed, 3 skipped |
| gofmt — changed files | FAIL — 3 unformatted files |
| Missing locale keys (de, es, fr, ja) | PASS — 0 missing (0, 0, 0, 0) |
| Tracker-ID matches in the observer howto | PASS — 0 matches |

## Overall

FAIL

Reason: gofmt found unformatted files among the changed files. All other checks pass.
