# Test tmp cleanliness report

- Date: 2026-10-30 (initial gate 13:45 EDT; final gate re-run and this revision
  14:05–14:36 EDT, same day)
- Version: v9.42.1 base (branch `automaton/0c000001`)
- Worktree: the PRD worktree for task `0c000001` (branch `automaton/0c000001`)
- Gate: read-only — all commands ran under `env -u TMUX -u TMUX_TMPDIR` (the
  survey script itself also strips `TMUX`/`TMUX_TMPDIR`); no tmux server
  commands, no kill-by-name anywhere.
- Status: **PASS**

## Overall result: PASS

No leaked temp-dir prefixes were observed for any package, `make lint` is
clean, and `go test ./...` passes.

## Commands, in order (final re-run, 2026-10-30 14:05–14:36 EDT)

| # | Command | Result |
|---|---------|--------|
| 1 | `make lint` | **PASS** — all pre-checks and `golangci-lint run ./...` → `0 issues` |
| 2 | `go build ./...` | **PASS** |
| 3 | `env -u TMUX -u TMUX_TMPDIR make test` (→ `go test ./...`) | **PASS** — all packages `ok`, incl. `tests/ci` 48.7s |
| 4 | `env -u TMUX -u TMUX_TMPDIR make test-tmp-guard` (→ `DW_LEAK_GUARD_FULL=1 go test -count=1 -timeout 20h ./internal/session/ -run TestNoNewTmpLeakEntries`) | **PASS** — `ok github.com/dmz006/datawatch/internal/session` 586.8s |
| 5 | `env -u TMUX -u TMUX_TMPDIR make check-tmux` (→ `bash scripts/check-no-unscoped-tmux.sh`) | **PASS** — `OK: no unscoped tmux kills` |
| 6 | `env -u TMUX -u TMUX_TMPDIR bash scripts/survey-test-tmp.sh` (whole module: `go test -count=1` per package, each in a fresh private `TMPDIR`) | **PASS** — `OK: all 94 package(s) passed`; 94/94 private directories empty |
| 7 | `env -u TMUX -u TMUX_TMPDIR make clean-test-tmp` (→ `bash scripts/clean-test-tmp.sh`) | **PASS** — removed 40 stale scratch entries older than an hour; see below |

## Remaining prefix counts per package

None. Every one of the 94 surveyed packages left **0** entries in its private
temp directory (each recorded as `(private directory empty)`). No `dw-test-*`,
`dw-tmux-*`, `svchelper-*`, `datawatch-*-task-*`, or any other prefix survived
in any package's isolated `TMPDIR`.

The repo-retained summary of the before/after survey output is
`docs/plans/2026-10-30-test-tmp-leak-survey.md`; the raw per-run survey logs
were kept only locally in gitignored `*.log` files and are intentionally not
part of the repository (raw per-package timings are reproducible with the
command in step 6).

## Stale cleanup (step 7) — informational

`clean-test-tmp` is a janitor for **pre-existing** stale scratch entries older
than one hour; its removals are prior-test debris, not leaks observed by this
gate. The 40 removed entries were 20 × `dw-test-home-*` and 20 × `dw-test-tmux-*`,
all direct children of the program temp directory, all owned by the current
user, all matching the allowed prefix list, none a symlink.

## Verdict

Clean. No leaks; gate passes; `make lint` clean; build and full test suite
green.

## Parity surface

Same scope as the survey plan — test hygiene only, no operator-facing surface:
REST, MCP, CLI, comm channel, YAML/config, PWA, Android, iPhone/iOS are all
intentionally excluded, with the per-surface reasons listed in
`docs/plans/2026-10-30-test-tmp-leak-survey.md`.
