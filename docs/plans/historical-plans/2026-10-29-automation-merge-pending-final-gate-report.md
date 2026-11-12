# Automaton merge-pending: final gate report

Date: 2026-10-30 (all times are local, from the shell clock).

This report covers the final gate for the Automaton merge-pending work: the merge-pending state for finished Automata, the two read endpoints, the two config keys, the merge badge and the safe-to-delete report. It changes no code. Every command ran under `env -u TMUX -u TMUX_TMPDIR`, with the shell `time` builtin around it. No tmux server command, `pkill` or kill-by-name was used.

## Summary

| # | Check | Outcome |
|---|-------|---------|
| 1 | `go build ./...` on the whole repository | PASS |
| 2 | Targeted `go test` on six packages | PASS (115 passed, 0 failed, 1 skipped) |
| 3 | Full `go test ./...`, run once | **FAIL** (1 test failed in `internal/server`) |
| 4 | `node --test` on the PWA test files | **FAIL** on the first run (1 of 1497), PASS on the rerun (1497 of 1497) |
| 5 | `scripts/check-no-internal-refs.sh` | PASS |
| 6 | `scripts/check-howto-coverage.sh` | PASS |
| 7 | New locale keys present in all five bundles | PASS (19 of 19 in each of 5) |
| 8 | Parity issue exists and its title starts with `[sync]` | PASS |
| 9 | Live-check report exists and ends with its Result line | PASS |

Two required checks (3 and 4) failed on their prescribed run. Both failures did not reproduce on a rerun and neither touches the merge-pending code (see "Failures"). The result below stays FAIL because the gate asked for the prescribed full run to be recorded as it happened; the decision on whether to accept the reruns is the operator's.

## 1. Go build, whole repository

- Command: `env -u TMUX -u TMUX_TMPDIR go build ./...`
- Start 00:33:57, end 00:34:01 (real 4.196s).
- Outcome: exit 0, no output. PASS.

## 2. Targeted Go tests

- Command: `env -u TMUX -u TMUX_TMPDIR go test -count=1 -v -run 'TestMerge|TestSafeToDelete|TestMergePendingPass|TestTriage|TestMergePendingConfig' ./internal/autonomous ./internal/config ./internal/server ./internal/mcp ./cmd/datawatch ./internal/router`
- Start 00:34:06, end 00:34:10 (real 3.789s).
- Outcome: exit 0. PASS.

| Package | Passed | Failed | Skipped |
|---------|--------|--------|---------|
| `internal/autonomous` | 51 | 0 | 1 |
| `internal/config` | 5 | 0 | 0 |
| `internal/server` | 24 | 0 | 0 |
| `internal/mcp` | 12 | 0 | 0 |
| `cmd/datawatch` | 12 | 0 | 0 |
| `internal/router` | 11 | 0 | 0 |
| **Total** | **115** | **0** | **1** |

Counts are the top-level `--- PASS` / `--- FAIL` / `--- SKIP` lines in the verbose output.

Key output lines:

```
ok  	github.com/dmz006/datawatch/internal/autonomous	2.394s
ok  	github.com/dmz006/datawatch/internal/config	0.008s
ok  	github.com/dmz006/datawatch/internal/server	0.037s
ok  	github.com/dmz006/datawatch/internal/mcp	0.027s
ok  	github.com/dmz006/datawatch/cmd/datawatch	0.030s
ok  	github.com/dmz006/datawatch/internal/router	2.633s
```

The one skipped test is the live triage smoke test. It skips by design unless it is pointed at a sandbox daemon: `set DW_LIVE_TRIAGE_SMOKE to the sandbox daemon base URL to run the live triage check`. The live Automaton check is covered by check 9.

## 3. Full Go test, whole repository (run once)

- Command: `env -u TMUX -u TMUX_TMPDIR go test -count=1 ./...`
- Start 00:34:15, end 00:36:59 (real 2m43.666s).
- Outcome: exit 1. **FAIL.** 81 packages `ok`, 1 package `FAIL`, 12 packages with no test files.

Key output lines:

```
--- FAIL: TestQuestionStep2SwitchOn_LiveFlip_FansOutAndEmits (0.52s)
    question_step2_switch_off_test.go:262: router sends = 1, want 2 (one per raise)
FAIL
FAIL	github.com/dmz006/datawatch/internal/server	67.660s
```

Every other package passed, including `internal/autonomous`, `internal/config`, `internal/mcp`, `internal/router` and `cmd/datawatch`. See "Failures" for the diagnosis.

## 4. PWA tests with `node --test`

- Command: `env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/*.test.js` (126 test files).
- First run: start 00:34:21, end 00:34:23 (real 2.191s). Exit 1. **FAIL.** 1497 tests, 1496 passed, 1 failed.

```
ℹ tests 1497
ℹ pass 1496
ℹ fail 1
✖ settings modal: explicit list renders non-inherit with checked rows only (49.883032ms)
  AssertionError [ERR_ASSERTION]: modal element should be appended
      at TestContext.<anonymous> (internal/server/web/app-notify-events.test.js:278:10)
```

- Isolation reruns of the failing file, `node --test internal/server/web/app-notify-events.test.js`, run twice at about 00:34:33: 17 tests, 17 passed, 0 failed both times.
- Rerun of the whole suite, run after the Go reruns, ending 00:38:31, real 2.051s: exit 0, 1497 tests, 1497 passed, 0 failed.

The first run ran at the same time as the full Go test from check 3, so the machine was loaded.

## 5. No internal references in operator-facing text

- Command: `env -u TMUX -u TMUX_TMPDIR bash scripts/check-no-internal-refs.sh`
- Start 00:34:41 (real 0.033s). Exit 0. PASS.

```
==> Internal-ref leak audit (BL###, F##, B##, S## in user-facing strings)
✓ PASS: no internal refs in user-facing surfaces.
```

## 6. Howto coverage

- Command: `env -u TMUX -u TMUX_TMPDIR bash scripts/check-howto-coverage.sh`
- Start 00:34:41 (real 0.163s). Exit 0. PASS.

```
==> Howto coverage audit (every docs/howto/*.md must be authored or LLM-only)
✓ PASS: every howto under docs/howto/ has exec_steps or is LLM-only.
```

## 7. New locale keys in all five bundles

- Method: the keys in `internal/server/web/locales/en.json` that are not in `en.json` on `main`, checked for presence in `en.json`, `de.json`, `es.json`, `fr.json` and `ja.json` (a small Python check run under `python3 -I`).
- Outcome: 19 new keys, all 19 present in each of the 5 bundles, 0 missing. PASS.

```
en present=19/19 missing=[]
de present=19/19 missing=[]
es present=19/19 missing=[]
fr present=19/19 missing=[]
ja present=19/19 missing=[]
```

The 19 keys: `settings_automation_merge_pending_mode`, `settings_automation_merge_pending_alert_minutes`, `merge_state_pending`, `merge_state_needs_look`, `merge_state_merged`, `merge_state_empty`, `merge_state_unknown`, `merge_badge_title`, `safe_delete_card_title`, `safe_delete_read_only`, `safe_delete_safe_heading`, `safe_delete_held_heading`, `safe_delete_safe_empty`, `safe_delete_held_empty`, `safe_delete_worktree`, `safe_delete_commits`, `safe_delete_state`, `safe_delete_dirty`, `safe_delete_failed`.

Note: in `de`, `es`, `fr` and `ja` the 19 values are the English text, as placeholders until translations land. The parity issue says the same. This is a note, not a failure.

## 8. Parity issue

- File: `docs/plans/parity-issues/2026-10-29-automation-merge-pending-parity.md`. Its filing record names `datawatch-app#304`.
- Command: `gh issue view 304 -R dmz006/datawatch-app --json number,title,state`
- Outcome: PASS.

```
{"number":304,"state":"OPEN","title":"[sync] Automaton merge-pending state: two new read endpoints, two config keys, merge badge and safe-to-delete report (datawatch next release)"}
```

Issue 304 exists, is open, and its title starts with `[sync]`.

## 9. Live-check report

- File: `docs/plans/2026-10-29-automation-merge-pending-live-check.md`.
- Outcome: PASS. The file exists, and its last line is exactly `Result: PASS` followed by a newline, with nothing after it.
- The report's cleanup section records that the temporary Automata, the config values and the sandbox daemon were all restored or removed, and that the production daemon was not touched.

## Failures

Two required checks failed on their prescribed run. Nothing was fixed in this task. Both are left for the operator to decide.

1. **`TestQuestionStep2SwitchOn_LiveFlip_FansOutAndEmits` in `internal/server`** (full Go test, check 3).
   - Output: `question_step2_switch_off_test.go:262: router sends = 1, want 2 (one per raise)`.
   - This test is about the question-routing switch. `git diff main...HEAD` shows no change to the test file or to any question-related file on this branch.
   - It did not reproduce. The test alone with `-count=5` passed (`ok ... 2.575s`). The whole `internal/server` package, rerun with `-count=1`, passed (`ok ... 67.586s`, real 1m8.579s).
   - During the failing run I was also running the node suite, the two scripts and the other checks at the same time. The test asserts a send count on a live flip, which suggests a timing sensitivity under load. I have not confirmed that cause.

2. **`settings modal: explicit list renders non-inherit with checked rows only`** in `internal/server/web/app-notify-events.test.js` (node suite, check 4).
   - Output: `AssertionError: modal element should be appended` at line 278.
   - The file passed twice when run alone and the whole suite passed on the rerun (1497 of 1497). It ran while the full Go test was loading the machine.

Neither failure is in the merge-pending tests. The targeted run of the merge, safe-to-delete, merge-pending pass, triage and merge-pending config tests passed in all six packages (check 2). The operator can accept the green reruns as the gate or ask for the failing tests to be stabilised first.

Result: FAIL
