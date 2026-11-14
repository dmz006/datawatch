# 7db40001 — final gate (counts only)

- Date: 2026-11-01
- Mode: verify + build + test, no files changed, no daemon, no gh, no version bump.

| # | Check | Result |
|---|-------|--------|
| 1 | Verification suite (guard script) | FAIL — exit 1: the format, build, and filtered-test subchecks all passed, but the docs-changed gate fired (tree had no uncommitted docs delta) |
| 2 | Binary build | PASS |
| 3 | Test suite (4 target packages) | PASS |

## Test counts

- Packages passed: 4
- Packages failed: 0
- Tests passed: 10196
- Tests failed: 0
- (12 tests skipped)

## Marker refusal coverage

- Write endpoints covered by the truncation-marker refusal: 9

## Notes

- No spec text, paths, endpoint names, or config contents are included in this report by design.
- Overall: 2 of 3 checks PASS; the single failure is the guard script's docs-changed gate, not a build, format, or test failure.
