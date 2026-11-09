# Automation cleanup — final gate report

**Date:** 2026-10-30
**Branch:** `automaton/f8f60001` (HEAD `f47e85c3d`), forked from `main` at `f508b3d90`
**Mode:** read-only. The gate ran the whole suite and wrote only this file. No source, test, doc or locale file was changed, and nothing was committed.
**Environment:** every build and test ran under `env -u TMUX -u TMUX_TMPDIR`.

## Result: NOT CLEAN — 2 of 11 checks failed

| # | Result | Check |
|---|---|---|
| 1 | PASS | `go build -o datawatch ./cmd/datawatch/` |
| 2 | PASS | `go vet ./...` |
| 3 | PASS | `go test ./internal/autonomous/` |
| 4 | PASS | `go test ./internal/config/` |
| 5 | PASS | `go test ./internal/server/` |
| 6 | PASS | `go test ./internal/mcp/` |
| 7 | PASS | `go test ./internal/router/` |
| 8 | **FAIL** | `go test ./cmd/datawatch/` — `TestMergeCLI_MergeCommandPrints` |
| 9 | PASS | JS tests for the cleanup card (`internal/server/web/app-cleanup-card.test.js`) |
| 10 | **FAIL** | `scripts/check-docs-sync.sh` — embedded docs drift from `docs/` |
| 11 | PASS | `scripts/check-no-internal-refs.sh` and `scripts/check-test-daemon-cleanup.sh` |

The four content checks that follow all passed (see below).

## Command output summaries

### Build and vet
- `go build -o datawatch ./cmd/datawatch/` — exit 0, no output. The binary is untracked and ignored; `git status` stayed clean.
- `go vet ./...` — exit 0, no output.

### Go tests
```
ok    internal/autonomous   58.812s
ok    internal/config        1.948s
ok    internal/server       67.760s
ok    internal/mcp           1.977s
ok    internal/router      107.599s
FAIL  cmd/datawatch        114.450s   (one failing test)
```

### JS tests (`node --test internal/server/web/app-cleanup-card.test.js`)
16 tests, 16 pass, 0 fail, 0 skipped. They include the "every cleanup key exists in every locale bundle, says Automaton or Automata and has no tracker id" test and the "every t() key the card and loader use is in KEYS" test.

### Scripts
- `scripts/check-no-internal-refs.sh` — exit 0: "PASS: no internal refs in user-facing surfaces."
- `scripts/check-test-daemon-cleanup.sh` — exit 0: "OK: no daemon cleanup violations".
- `scripts/check-docs-sync.sh` — **exit 1**:
  ```
  ERROR: embedded PWA docs drift from docs/ — run 'make sync-docs' and re-stage.
  >f.st...... operations.md
  >f.st...... howto/automaton-worktree-cleanup.md
  >f+++++++++ plans/2026-10-30-automation-cleanup-live-check.md
  ```

## Failures

### 1. `TestMergeCLI_MergeCommandPrints` fails (cmd/datawatch) — caused by this branch

```
cli_merge_test.go:41: output lacks
  "Automaton \"Add paging\" (id p1): branch automaton/p1 is pending, 3 commit(s) ahead of main"
actual: "... 3 commit(s) ahead of the default branch, merge pending since ..."
```

- **Cause:** this branch changed `RenderMergeState` in `internal/autonomous/merge_text.go` from a fixed `mergePendingBase` to `mergeBaseText(e.Base)`. It added `MergeStatusEntry.Base` (`internal/autonomous/merge_status.go`), so the text now names the base branch the comparison used. A record with no base reads "the default branch".
- **What was and was not updated:** the router test fixture (`internal/router/merge_comm_test.go`) was updated with `"base": "main"` and passes. The CLI fixture `mergeCLISample` in `cmd/datawatch/cli_merge_test.go` has no `"base"` field and still expects `ahead of main`, so it fails.
- **Not flaky:** the failure is deterministic. It is the only failing test in `cmd/datawatch`.
- **Suggested fix (not applied, this gate is read-only):** add `"base":"main"` to the entry in `mergeCLISample` (the router fixture's approach). The test then passes with its current expected text.

### 2. `check-docs-sync.sh` reports drift

- `operations.md` and `howto/automaton-worktree-cleanup.md` differ between `docs/` and the embedded copy.
- `plans/2026-10-30-automation-cleanup-live-check.md` is in `docs/plans/` but has no embedded copy.
- **Suggested fix (not applied):** run `make sync-docs` and stage the result. It writes outside this report file, so the gate did not run it. This report is also new under `docs/plans/`, so run the sync after it is added, because the script will list it as well.

## Content checks (all passed)

### The internal tracker term in user-visible strings: none
- Scanned every added line in the branch diff against `main`, excluding `_test.go` and `.test.js` files, for the whole word `PRD` or `PRDs`. Three lines matched:
  - two Go identifiers in `internal/autonomous/worktree_cleanup.go` (`prdFor` returns the type `*PRD`; `ReleaseAutomatonWorktree(prd *PRD)`). They are code, not strings.
  - one row in `docs/testing-tracker.md` that names the test `TestLocales_PRDNeverUserFacing`. It is a test name, not wording shown to a user.
- None of the 16 new non-test files contains the word in a string. The only hit in new files is the same two identifiers, plus a JS test assertion that forbids the word.
- No match in `app.js`, the five locale bundles, `openapi.yaml`, the how-to, `operations.md` or the live-check report.

### The five locale bundles carry the same new keys: yes
`en`, `de`, `es`, `fr`, `ja` each have 2869 keys, 26 added and 0 removed against `main`. The added sets are identical, and so are the full key sets. The 26 keys are `cleanup_*` (22) and `settings_automation_{cleanup_mode,cleanup_retention_hours,cleanup_max_removals_per_pass,disk_alert_percent}` (4).

### Version unchanged: yes, relative to the fork point
- `cmd/datawatch/main.go` and `internal/server/api.go` both read `9.42.1` at the merge-base and at HEAD. The branch diff touches no `Version` line, so the minor bump is left for merge time.
- **Note:** the current `main` tip is `9.42.2` (`c925bf702`, 16 commits ahead of the fork point). A plain diff of HEAD against `main` therefore shows `9.42.1` vs `9.42.2`. That difference comes from `main` moving on, not from this branch. The merge must take `main`'s value and then bump for this release.

### Live check report shows mode report listed without removing: yes
`docs/plans/2026-10-30-automation-cleanup-live-check.md` (v9.42.1, sandbox daemon only) records 24 checks, 24 passed, 0 failed. Section 1 shows the unset mode runs as `report`:
- worktrees pass: `removed: 0`, `would_remove: 3`, `skipped: 5`, `failed: 0`, `bytes_freed: 0`
- temp pass: mode `report`, `removed: 0`
- check 5: "report mode removed no branch and no work tree"; the branch and work tree lists before and after are the same.

Removals happen only later, after the setting is switched to `on` (section 4), and only for the three safe entries. The held entries (dirty, unmerged, unfinished, young) and the operator's own branch were kept.

## Disposition

Do not advance this Automaton to merge until both failures are fixed. Both fixes are small and mechanical: one test fixture field, and one docs sync. After they are applied, re-run `go test ./cmd/datawatch/` and `scripts/check-docs-sync.sh`.
