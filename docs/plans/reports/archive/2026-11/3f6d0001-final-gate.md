# Counts-only final gate report — automatic grouping tags

Date: 2026-11-05

| Check | Result |
|---|---|
| Go build of the datawatch command | PASS |
| Go vet (7 in-scope packages + command) | PASS |
| Go test suites (in-scope packages) | PASS |
| Node test suites (the four new web test files) | PASS |
| Internal-refs rule script | PASS |
| Docs-sync rule script | **FAIL** |

Counts:

- Go packages: 8 passed / 0 failed
- Go tests: 8672 passed / 0 failed / 18 skipped
  (8672 top-level test results; an additional 4054 subtest result lines make 12783 total PASS result lines)
- JS tests: 56 passed / 0 failed
- Rule scripts: 1 passed / 1 failed

Failing item: scripts/check-docs-sync.sh (exit 1) — the embedded PWA docs mirror
drifts from docs/ (the report file under docs/plans/reports/ is absent from the
mirror; the `plans` entry in the embed-skip manifest is defeated by the rsync
filter ordering in the sync rule, since the directory include precedes the
exclude). The docs/ and mirror were otherwise in sync.

One rule-script failure count is non-zero. Gate result: **FAIL**.
