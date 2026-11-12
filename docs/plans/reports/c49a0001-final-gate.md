# Final validation gate — counts report

Date: 2026-11-01
Gate: `.datawatch/verify/validation-gate-counts.sh` (read-only; no files changed, no daemon started, no `gh`, no version-variable change)

## Counts

- **passed = 14**
- **failed = 1**
- **skipped = 0**
- **New test functions whose names contain "Drain" (added by this feature, counted by grep): 19**

## Result per check (counts only)

| Check | Result |
|---|---|
| go build | PASS |
| go vet (touched packages) | PASS |
| go test — compute | PASS |
| go test — capacity | PASS |
| go test — autonomous | PASS |
| go test — server | PASS |
| go test — mcp | PASS |
| go test — router | PASS |
| go test — federation | PASS |
| go test — cmd/datawatch | PASS |
| PWA node tests | PASS |
| make check-internal-refs | PASS |
| make check-leaks | PASS |
| make check-test-daemons | PASS |
| golangci-lint | **FAIL** |

Failing step: `golangci-lint` (it is installed, so it ran and was counted as a failure rather than skipped).

## Overall

**FAIL** — 14 of 15 steps passed; the single failure is the golangci-lint step.

## Live smoke test

The live smoke test was **not run** because this feature request forbids starting a daemon. A live test is therefore **still owed** before this feature can be called validated.
