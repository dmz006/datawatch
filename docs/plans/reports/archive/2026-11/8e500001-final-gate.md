# 8e500001 Placement-Tag Final Gate — Counts-Only Report

Date: 2026-11-01

## Checks

| Check | Result |
|---|---|
| Repository builds (`go build ./...`) | pass |
| Scenario 1 (local node) tests found: 2 | pass (≥ 1) |
| Scenario 2 (federated peer) tests found: 1 | pass (≥ 1) |
| Scenario 3 (retry on another node) tests found: 1 | pass (≥ 1) |
| Scenario 4 (axis switched off) tests found: 1 | pass (≥ 1) |
| Scenario 5 (startup backfill) tests found: 5 | pass (≥ 1) |
| Test run, 9 required packages, `-count=1` | pass |

## Test totals (9 packages, `-count=1`)

- pass: 8801
- fail: 0
- skip: 18

## Gate result

**PASS** — build ok, every scenario has at least one test, zero failures.
