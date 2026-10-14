# Task: stop session-tracker repos committing output.log, and make auto-gc safe

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

`internal/session/tracker.go` defines `Tracker`, which owns a per-session
folder (`<dataDir>/sessions/<fullID>/`) that is its own git repository.
`NewTracker` runs `git init -b main`, sets `user.email` / `user.name`, writes
the initial files (including an empty `output.log`) and makes a first commit.
Every `Record*` method ends in `commitAll`, which runs `git add -A`,
`git status --porcelain` and `git commit`. `ResumeTracker` builds a `Tracker`
for an EXISTING repo without re-initialising it. Callers live in
`internal/session/manager.go` (find them).

`output.log` is the live terminal output of the session (written by tmux
`pipe-pane`; with encryption on, output goes to `output.log.enc` via a FIFO at
`output.log.pipe`). It is append-only and grows without bound for long-lived
sessions. Because `git add -A` snapshots it on every event, the repo's history
grows without bound too. Eventually git's implicit auto-gc (fired from
`git commit`) ran a `pack-objects` delta search over those large near-duplicate
blobs that used >100GB of RAM; it ran as a child of the daemon, inside the
daemon's own cgroup, so the kernel OOM killer took down the whole daemon.

Everything that reads `output.log` reads the working-tree file on disk; nothing
reads it out of the tracker's git history. Session rollback operates on the
user's project directory, not on the tracker repo. Verify this yourself by
reading the callers before you change anything.

## What to do

Read `tracker.go` and its callers in `manager.go` end to end, then make a
minimal, idiomatic fix in `tracker.go` (match the file's existing style):

1. Add a small unexported helper (name it for what it does) that,
   idempotently:
   a. sets local git config `gc.auto 0` and `gc.autoDetach false`, plus the
      caps `pack.windowMemory 64m`, `pack.threads 1`,
      `core.bigFileThreshold 16m`, so any future explicit gc is memory-bounded;
   b. adds `output.log`, `output.log.enc`, `output.log.pipe` and `*.pipe` to
      `.git/info/exclude` (not a tracked `.gitignore`), appending only lines
      that are missing;
   c. if `output.log` / `output.log.enc` is currently tracked (repos created
      before this fix), removes it from the index only
      (`git rm --cached ...`) so later commits stop snapshotting it. The file
      must stay on disk.
2. It must run at most once per `Tracker` instance and be safe if `Tracker`
   methods are called from multiple goroutines (use an appropriate sync
   primitive). Call it in `NewTracker` after the identity config and BEFORE
   the first commit. For trackers built by `ResumeTracker`, run it lazily at
   the start of the commit path so every existing long-lived repo is fixed on
   its next commit. `ResumeTracker`'s signature must not change.
3. Update the README text that `renderReadme` generates so the `output.log`
   row says it is live output kept on disk and NOT committed to git history,
   and adjust the "Git History" paragraph to match.
4. Add a short comment explaining why (implicit auto-gc inside the daemon's
   cgroup + unbounded output.log blobs OOM-killed the daemon). Do not put
   tracker/backlog IDs in identifiers.

## Tests

Create `internal/session/tracker_test.go` (new file) covering at least:

- `NewTracker` in `t.TempDir()` sets `gc.auto` = `0`, `gc.autoDetach` =
  `false`, and sets `pack.windowMemory` and `pack.threads`.
- After `NewTracker`, appending a large chunk (e.g. 256KB) to
  `OutputLogPath()` and calling `Record*` methods ~20 times: `git log --all
  --oneline -- output.log` is empty, `git ls-files output.log` is empty,
  `conversation.md` / `timeline.md` ARE in history, and `output.log` is still on
  disk at full size.
- A legacy repo built by hand (git init + commit an `output.log`), then
  `ResumeTracker` + one `Record*` call: `output.log` is no longer in the index,
  `gc.auto` is `0`, `.git/info/exclude` contains `output.log`, file still on
  disk.
- Calling the helper twice does not duplicate exclude lines.

Skip a test (`t.Skip`) only if `git` is not on PATH. Then run `gofmt -l`,
`go build ./...`, `go vet ./internal/session/` and `go test ./internal/session/`
— the whole package must pass. Commit (`fix(session): ...`). Do not edit
CHANGELOG or version files.
