# Final gate (counts only)

Date: 2026-11-01. All checks run with the terminal-session environment variable unset, no daemon started, no model pulled, no version variable changed.

| Check | Result |
|---|---|
| `go vet` over the changed Go packages | PASS — 0 findings |
| `go test -v -count=1` over the changed Go packages | PASS — 11/11 packages ok, 0 failed; 11,256 tests run (7,601 top-level), 7,601 passed, 0 failed, 0 skipped top-level failures |
| Node `--test` run over the new web test file(s) (3 files) | PASS — 32 tests, 32 pass, 0 fail |
| Internal-refs leak audit script | PASS — no internal refs found (0 violations) |
| Docs-sync check script | PASS — source and mirrored docs in sync (0 differences) |

## Totals

- Checks: 5 run, 5 passed, 0 failed.
- Go: 11 packages ok, 0 failed; 11,256 tests run, 0 failed.
- Node: 32 tests, 32 passed, 0 failed.
- Script checks: 2 passed, 0 failed.

**Gate: PASS.**
