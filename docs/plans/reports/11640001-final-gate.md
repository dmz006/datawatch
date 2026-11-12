# 11640001 final gate report

Date: 2026-11-01
Scope: counts-only final gate. No daemon, no network tooling, no version change.

## Checks

| # | Check | Result |
|---|-------|--------|
| 1 | Secret-scan gate report script | PASS |
| 2 | Production binary build | PASS |
| 3 | Package test suites (6 packages) | PASS |

## Check 1 — secret-scan gate report

- findings tests passed: 6
- built-in detectors: 5
- allow-list test cases: 4
- failures: 0

## Check 2 — build

- build exit status: 0 (success)
- no other output of note

## Check 3 — package test suites

- packages passed: 6 of 6
- tests passed: 6408
- tests skipped: 17
- tests failed: 0

## Overall

PASS — all 3 checks green.
