# Task: review two candidate fixes (verifier role)

You are acting as the quality-gate verifier for a coding task. Two candidate
implementations were submitted as unified diffs against this tree. Your job is
to decide, for each one, whether it satisfies the task spec below.

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

## Inputs (in the cell directory)

- Two files matching `candidate-*.diff`, each a complete candidate diff
  against the current tree (`internal/session/`).

## What to do

1. Read the task spec below and the current code it refers to.
2. Evaluate each candidate against EVERY requirement in the spec. You may
   apply a candidate in a scratch copy (e.g. `git stash`/`git apply`, or a
   `git worktree`, or a `mktemp -d` copy of the cell) and build / test it. Leave
   the cell's tracked source files unchanged when you are done.
3. Write `review-verdict.json` in the cell directory root, keyed by each
   candidate's exact file name:

```json
{
  "<candidate file name>": {"verdict": "pass", "reasons": ["..."]},
  "<other candidate file name>": {"verdict": "fail", "reasons": ["..."]}
}
```

`verdict` is exactly `"pass"` or `"fail"`. `pass` means the candidate meets
every requirement; any spec violation means `fail`. Give concrete reasons
(which requirement, which line). Commit `review-verdict.json` (`chore: ...`).

## The task spec the candidates were written against

> Fix `internal/session/tracker.go` so session-tracker git repos can no
> longer grow without limit from `output.log`, and so git's implicit auto-gc
> can never fire from the tracker's commit path.
>
> 1. Add a small unexported helper that, idempotently: (a) sets local git
>    config `gc.auto 0` and `gc.autoDetach false`, plus caps
>    `pack.windowMemory 64m`, `pack.threads 1`, `core.bigFileThreshold 16m`;
>    (b) writes `output.log`, `output.log.enc`, `output.log.pipe` (and
>    `*.pipe`) to `.git/info/exclude`, appending only missing lines;
>    (c) if `output.log` / `output.log.enc` is currently tracked, runs
>    `git rm --cached` so later commits stop snapshotting it (file stays on
>    disk). Run it ONCE per Tracker instance (sync.Once or equivalent).
> 2. Call it in `NewTracker` after the user.email/user.name config, BEFORE
>    the first commit. For `ResumeTracker`-created trackers, call it lazily at
>    the start of the commit path so existing long-lived repos are fixed on
>    their next commit after a restart. `ResumeTracker`'s signature must not
>    change.
> 3. Update `renderReadme`'s `output.log` row (kept on disk, NOT committed)
>    and the "Git History" paragraph.
> 4. Comment explaining why.
>
> Tests in a new `internal/session/tracker_test.go`: NewTracker sets
> gc.auto=0 / pack.windowMemory / pack.threads; output.log never enters
> history across ~20 growing Record* calls while conversation.md/timeline.md
> do; a hand-built legacy repo + ResumeTracker + Record* untracks output.log,
> sets gc.auto=0 and writes the exclude; the helper is idempotent. gofmt, go
> vet and `go test ./internal/session/` must pass.
