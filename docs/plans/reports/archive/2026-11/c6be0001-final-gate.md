# c6be0001 final gate — backend-aware session state (counts only)

Date: 2026-11-03. Per-check results with test counts only; no paths, test names, or config contents.

| # | Check | Result | Pass | Fail |
|---|-------|--------|------|------|
| 1 | Unscoped-tmux guard | PASS | — | — |
| 2 | Go vet (config, session, autonomous, server) | PASS | — | — |
| 3 | Go tests: config package | PASS | 575 | 0 |
| 4 | Go tests: session package | PASS | 1300 | 0 |
| 5 | Go tests: autonomous package | PASS | 5347 | 0 |
| 6 | Go tests: server package (locale subset) | PASS | 23 | 0 |
| 7 | Go build to discard output | PASS | — | — |
| 8 | Audit-report check | FAIL | 0 | 0 |
| 9 | PWA node tests | PASS | 2051 | 0 |

Totals: 8 of 9 checks pass. 9296 tests pass, 0 tests fail.

Notes:
- Check 8 fails because the audit-report verification script referenced by the gate does not exist in the repository; no test was executed under it. It is the only non-passing check and not a test failure.
- Because the gate script halts at check 8 (strict mode), check 9 was executed the same way the gate defines it, immediately after, and passed in full.
- No daemons started, no `gh` invocations, no version variable changes, no source files modified.
