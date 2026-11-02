# Final gate report — PWA test TLS-trust fix (alerts fix)

- Date: 2026-11-02
- Re-verified: 2026-11-02 19:04 (third verification run, all prior evidence re-confirmed identically); latest commit `79c768343`
- Branch: `automaton/b6be0001` (merge base with `main`: `d897c68c1`)
- Gate command: `bash .datawatch/verify/alerts-fix-final-gate.sh`
- Branch result: **exit 1** — one false-positive check (6) below; every substantive check passes. Full unredacted output in `alerts-fix-final-gate-output.txt`.
- Read-only gate: no file was fixed in this task.

## Check-by-check

| # | Gate step | Result | Evidence |
|---|-----------|--------|----------|
| 1 | Shared TLS helper test (`tls-trust-helper.sh`) | **PASS** | `node --check`/`bash -n` clean for `lib.mjs`, `e2e-pwa.sh`, `test-stories/lib.sh`, `tls-trust.test.mjs`; `TEST_TLS_CA` read/exported in all three; `node --test tls-trust.test.mjs` 3/3, `OK` |
| 2 | TS-833/834/835: no `rejectUnauthorized`, trusted agent used (`story-scripts-trust-cert.sh`) | **PASS** | Script `OK`. Independent grep: no `rejectUnauthorized`/`NODE_TLS_REJECT_UNAUTHORIZED` in the three scripts; all three `import { … sandboxTlsAgent } from './lib.mjs'` and call `sandboxTlsAgent()` |
| 3 | Two rewritten unit tests (`test-string-patterns.sh`) | **PASS** | The chained `&lt;`/`&gt;`/`&amp;` decode in `app-questions-history.test.js` is gone; the paren-only escape in `app-identity-backups.test.js` is gone; `node --test` on both files 23/23, `OK` |
| 4 | `node --test internal/server/web/*.test.js` | **PASS** | 1851 tests, **1851 pass, 0 fail**, 0 skipped/skipped-todo drift, ~2.0 s |
| 5 | CHANGELOG.md has an Unreleased section | **PASS** | `## [Unreleased]` at line 6; `### Fixed` includes the entry "The PWA test scripts no longer turn certificate checking off" covering TS-833/834/835 and the two rewritten string patterns |
| 6 | `cmd/datawatch/main.go` and `internal/server/api.go` unchanged | **FAIL by script — false positive** | The script compares against the **tip** of `main` (`git diff main --name-only`), but `main` has moved far past this branch's fork point (merge base `d897c68c1`, branch version `9.53.0`) and its own later commits modify exactly these two files among many others — fresh re-check at 19:04 shows the branch-vs-merge-base diff (`git diff d897c68c1..HEAD --name-only`) contains **neither** `cmd/datawatch/main.go` nor `internal/server/api.go` — it lists only the 11 expected files (CHANGELOG.md, the two rewritten test files, the 8 story/e2e TLS files) plus this gate's own report artifacts — **`cmd/datawatch/main.go` and `internal/server/api.go` are not among them**. No version file was touched on this branch; the check as written fails for any branch forked before `main`'s subsequent version work until rebase. |
| 7 | Local CodeQL scan (only when installed) | **NOT REACHED — irrelevant** | Gate aborted at step 6. `codeql` is not on `PATH` and no local `.datawatch/codeql/codeql/codeql` binary exists in this worktree, so step 7 would have printed `codeql not installed; skipping local scan` had it been reached. |

## Findings for the operator

- **Gate alert 735** (`js/incomplete-multi-character-sanitization` in `internal/server/web/app-request-watch.test.js`) is open on the default branch. It is **not in this request's scope** and that file is **not present on this branch** (`git diff d897c68c1..HEAD` does not list it). Listed here as a finding only — not fixed, not dismissed.
- Informational (out of this gate's scope): other PWA story scripts **TS-830.mjs** and **TS-831.mjs** still set `rejectUnauthorized: false` directly. The gate's stated scope is TS-833/834/835, so this is noted, not acted on.
- No alert in GitHub was dismissed.

## Verdict

The branch satisfies every substantive gate check: TLS helper verified, TS-833/834/835 trust the test daemon's certificate via the shared agent, the two rewritten unit tests pass, all 1851 PWA web unit tests are green, the CHANGELOG has the Unreleased entry, and the branch touches none of the protected `cmd/datawatch/main.go` / `internal/server/api.go` files. The script's single failure is an artifact of comparing against a `main` that has moved past the branch's fork point, not a defect in this branch; re-running after a rebase (or checking against the merge base) would clear it.
