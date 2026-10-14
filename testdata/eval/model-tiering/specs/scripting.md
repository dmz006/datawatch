# Task: reproduction harness for session-tracker git growth

## Ground rules for this cell (read first)

- You are given ONE working directory (the "cell directory"). It is a
  standalone snapshot of the datawatch Go repository with its own fresh git
  repo containing a single baseline commit. Work ONLY inside the cell
  directory. Do not read, write or list anything outside it (scratch temp dirs
  created with `mktemp -d` are fine for test runs).
- Do NOT consult memory recall / knowledge-graph / research tools, other
  branches, other checkouts of this project, or any project history beyond the
  cell's own git repo. Solve the task from the code and this spec alone.
- Commit your work inside the cell's own git repo (`git add` + `git commit`,
  conventional-commit message). Do not push anywhere. Do not bump versions.
- Wrap shell commands with start/end timestamps
  (`echo "[$(date '+%H:%M:%S')] start" && <cmd>; echo "[$(date '+%H:%M:%S')] done"`).
- This is a one-shot run: no one will answer questions. Make the most
  reasonable judgment call yourself and finish.

## Background

Each datawatch session has a tracker folder that is its own git repo. Before a
recent fix, the tracker committed `git add -A` on every event, including an
ever-growing `output.log`, with git's default gc settings. Over many commits
the repo grew without bound and git's implicit auto-gc eventually ran a huge,
memory-hungry repack. The fix (already in this tree, see
`internal/session/tracker.go`) sets `gc.auto 0`, `gc.autoDetach false`,
`pack.windowMemory 64m`, `pack.threads 1`, `core.bigFileThreshold 16m`, and
lists `output.log`, `output.log.enc`, `output.log.pipe` in `.git/info/exclude`.

## What to write

`scripts/repro-tracker-git-growth.sh` — a self-contained, bounded-scale
reproduction harness that runs entirely inside a disposable scratch directory.

CLI (exact):

```
scripts/repro-tracker-git-growth.sh --workdir <dir> [--commits N (default 200)] [--chunk-kb K (default 512)] [--go]
```

- Refuse (non-zero exit, clear message) if `--workdir` is missing, or is
  under `$HOME/.datawatch`.
- `#!/usr/bin/env bash`, `set -euo pipefail`, and timestamp each phase with
  `echo "[$(date '+%H:%M:%S')] ..."`.
- NEVER write any system-binary directory as a literal absolute path (no
  leading-slash tool paths anywhere except the `#!/usr/bin/env bash` shebang
  line). Reference every tool by command name only and let `$PATH` resolve it;
  use `command -v <tool>` to detect availability.
- Touch nothing outside `--workdir`.

Behaviour — two modes, each in its own subdirectory of `--workdir`:

1. LEGACY mode (mirrors the pre-fix tracker): `git init -b main`, set
   `user.email` / `user.name` only, git's default gc settings except
   `gc.auto` set to a small value (e.g. 50) so the auto-gc trigger is reached
   at bounded scale. Loop N times: append K KB of random base64 text to
   `output.log` and one line to `timeline.md`, then
   `git add -A && git commit -q -m ...`. Measure each commit's max RSS with the
   `time` command found via `command -v time` if it supports `-f '%M'` (GNU
   time); otherwise sample with `ps`. Record: final `du -sk .git`, max commit
   RSS, loose objects / packs from `git count-objects -v`, whether an auto
   gc/repack ran (packs > 0, or trace output mentioning `gc --auto` /
   `repack`), and `git log --oneline -- output.log | wc -l`.
2. FIXED mode: same, but with the fixed tracker's config values and exclude
   lines listed above.
3. Optional `--go`: additionally run
   `go test -run TestTracker_OutputLogNotCommitted ./internal/session/` from
   the repo root (the shell simulation is the required part).

Print a comparison table (mode, .git size, max RSS during commit, auto-gc
triggered y/n, output.log commits). Finish with a line `RESULT: PASS` (exit 0)
only if ALL hold: fixed mode has 0 output.log commits; no auto-gc in fixed
mode; fixed `.git` size < 1/4 of legacy's; legacy mode actually exhibited the
problem (output.log commits == N). Otherwise print `RESULT: FAIL: <reason>`
and exit 1.

Keep runtime under ~2 minutes at defaults; `--commits 60 --chunk-kb 256` must
also work and PASS. Make it executable, make it pass `bash -n` and (if
installed) `shellcheck -S warning`. Run it once in a `mktemp -d` dir and paste
the table into the commit message body. Commit (`test: ...`).
