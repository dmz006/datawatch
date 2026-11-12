# supervisor-watch: pi transcript activity measure — final gate report

Date: 2026-11-02

## Defect

A working pi worker was reported SILENT by `scripts/supervisor-watch.py`
because only the worktree was measured for activity. A pi worker that is
thinking or running tools writes to its transcript, not to the worktree, so
the worktree looked idle past the threshold.

## What changed

- Transcript folder lookup (locate the pi session transcript folder for a worker).
- Transcript mtime measure, taken across the Automaton and all of its task
  worktrees, and folded into `newest_activity` alongside the worktree measure.
- The measure that decided the result is now named on the SILENT line.
- `PI_CODING_AGENT_DIR` is supported when locating transcripts.
- Offline unit tests: `scripts/tests/test_supervisor_watch_activity.py`.

## Commands run (each with `env -u TMUX -u TMUX_TMPDIR`, from the project directory)

1. `python3 -I -m py_compile scripts/supervisor-watch.py scripts/tests/test_supervisor_watch_activity.py`
   — passed (rc 0).
2. `python3 -I scripts/tests/test_supervisor_watch_activity.py -v`
   — Ran 7 tests, OK (rc 0). Cases: missing transcript folder uses worktree
   measure; newest of several transcripts wins; nothing anywhere returns None;
   old transcript + old worktree is SILENT; recent transcript + old worktree
   is not SILENT; SILENT threshold is 60; task-worktree transcript is newest.
3. `bash scripts/check-no-unscoped-tmux.sh`
   — "OK: no unscoped tmux kills" (rc 0).

## Scope notes

- No version bump is needed: `scripts/supervisor-watch.py` is exempt in
  `scripts/check-version-bump.sh`.
- The 60 minute SILENT threshold is unchanged.
