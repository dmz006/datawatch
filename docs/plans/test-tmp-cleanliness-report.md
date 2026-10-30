# Test tmp cleanliness report

Date: 2026-10-30 (13:45 local, EDT)
Branch/worktree: `/home/dmz/.datawatch/prd-worktrees/0c000001`
Gate: read-only — all commands ran under `env -u TMUX -u TMUX_TMPDIR` (the survey script itself also strips `TMUX`/`TMUX_TMPDIR`); no tmux server commands, no kill-by-name anywhere.

## Overall result: PASS

No leaked temp-dir prefixes were observed for any package.

## Commands, in order

| # | Command | Result |
|---|---------|--------|
| 1 | `env -u TMUX -u TMUX_TMPDIR make test-tmp-guard` (→ `DW_LEAK_GUARD_FULL=1 go test -count=1 -timeout 20h ./internal/session/ -run TestNoNewTmpLeakEntries`) | **PASS** — `ok github.com/dmz006/datawatch/internal/session  590.081s` (ran ~10 min) |
| 2 | `env -u TMUX -u TMUX_TMPDIR make clean-test-tmp` (→ `bash scripts/clean-test-tmp.sh`) | **PASS** — removed 61 stale entries (older than an hour) from `/tmp`; see prefixes below |
| 3 | `env -u TMUX -u TMUX_TMPDIR make check-tmux` (→ `bash scripts/check-no-unscoped-tmux.sh`) | **PASS** — `OK: no unscoped tmux kills` |
| 4 | `bash scripts/survey-test-tmp.sh` (whole module: `go test -count=1` per package, each with a fresh private `TMPDIR`, `TMUX`/`TMUX_TMPDIR` unset) | **PASS** — `OK: all 94 package(s) passed`; 94/94 private directories empty |

## Remaining prefix counts per package

None. Every one of the 94 surveyed packages left **0** entries in its private temp directory (each recorded as `(private directory empty)`). No `dw-test-*`, `dw-tmux-*`, `svchelper-*`, `datawatch-*-task-*`, or any other prefix survived in any package's isolated `TMPDIR`.

Raw survey output retained at `survey-testtmp.log` (471 lines).

## Stale cleanup (step 2) — informational

`clean-test-tmp` is a janitor for pre-existing stale scratch dirs older than one hour; its removals are prior-test debris, not leaks observed by this gate. The 61 removed entries were:

- `dw-test-home-*` — 30 entries
- `dw-test-tmux-*` — 30 entries
- `dw-tmp-leak-guard-*` — 1 entry

## Verdict

Clean. No leaks; gate passes.
