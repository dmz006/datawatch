# Session Tracking

Every datawatch session has a dedicated git-tracked folder that records the complete
history of the session: task, output, conversation, state changes, and git commits.

## Folder Location

```
~/.datawatch/sessions/<hostname>-<id>/
```

Example: `~/.datawatch/sessions/hal9000-a3f2/`

## Folder Contents

| File | Description |
|------|-------------|
| `.git/` | Git repository — every event is a commit |
| `README.md` | Auto-updated session overview (task, status, timestamps, tmux name) |
| `task.md` | Original task description and any amendments |
| `output.log` | Full raw claude-code output, streamed live by tmux pipe-pane. Kept on disk only: it is listed in `.git/info/exclude` and is **not** committed to the folder's git history (nor are `output.log.enc` / `output.log.pipe`). |
| `conversation.md` | Human-readable history of prompts received and inputs sent |
| `timeline.md` | Append-only timestamped event log |
| `session.json` | Machine-readable session state snapshot |
| `CLAUDE.md` or `AGENT.md` | Session guardrails (auto-generated from templates). `CLAUDE.md` is used for claude-code sessions; `AGENT.md` is used for all other backends (opencode, aider, goose, etc.) |
| `PAUSED.md` | Written by claude-code when it hits a rate limit (auto-deleted on resume) |

## Git Commit Timeline

Every significant event creates a git commit in the session folder:

```
$ git -C ~/.datawatch/sessions/hal9000-a3f2 log --oneline

d8f3c2a session: complete
b7e1f90 session: input sent
a3d4c81 session: waiting for input — Should I add tests?
9c2b7e3 session: state running→waiting_input
4f8a1e2 session: start — refactor the authentication module to use JWT
```

This gives you a full audit trail of every interaction.

## Project Directory Git Tracking

For a one-shot task session, datawatch snapshots the project directory
before the worker starts and commits automatically the moment the worker
signals it's done — before verification looks at the result. The
worker's own one-line summary becomes the commit message (the task
title if there isn't one); workers never run `git commit` themselves.
This is unconditional for task sessions.
`session.auto_git_commit` is **deprecated and ignored** — it no longer
changes this behavior; it is kept only so old config files still load.

This makes it trivial to see exactly what claude changed:

```bash
# In the project directory:
git log --oneline
# a8f3c12 session[a3f2](complete): refactor auth module
# 7b2e9d1 pre-session[a3f2]: refactor auth module

# Diff of everything claude did:
git diff HEAD~2 HEAD~1..HEAD
```

If something went wrong, roll back:
```bash
git reset --hard HEAD~1   # undo claude's changes, keep pre-session state
```

## Accessing Session History

### Via CLI
```bash
# Navigate to session tracking folder
cd $(datawatch session log a3f2)

# View git history
datawatch session history a3f2

# Follow live output
datawatch session tail a3f2 --lines 50

# Pi sessions: tail also prints one status line (e.g.
# "progress: turns=5 tools=bash:3,edit:1,read:1 last=bash idle=42s");
# --follow keeps re-emitting it every --status-interval (default 60s).
# Non-pi sessions print no status line.
datawatch session tail a3f2 --lines 50 --follow --status-interval 30s

# Read the conversation
cat conversation.md

# See full timeline
cat timeline.md
```

### Via PWA
The session detail view in the PWA shows live output. The session tracking folder
path is shown in the session info panel.

### Via Signal
```
tail a3f2 30     — last 30 lines of output
status a3f2      — current state + recent output
```

## Rate Limit Handling

When claude-code hits an API quota or rate limit, the session transitions to a
`rate_limited` state instead of failing:

1. claude-code writes `PAUSED.md` with a progress summary
2. The daemon detects the `DATAWATCH_RATE_LIMITED:` output line
3. State changes to `rate_limited` — you receive a Signal/PWA notification
4. The daemon schedules a retry after the reset time
5. On retry: the session resumes, `PAUSED.md` is used as context, and the session
   continues from where it left off

You can also manually resume a paused session:
```
send a3f2: continue from where you left off, see PAUSED.md for context
```

## Session Guardrails (CLAUDE.md / AGENT.md)

Each session has a guardrails file auto-generated from templates and placed in both the
session tracking folder and the project directory:

- **claude-code sessions** use `CLAUDE.md` (generated from `templates/session-CLAUDE.md`)
- **All other backends** (opencode, aider, goose, etc.) use `AGENT.md` (generated from `templates/session-AGENT.md`)

This file gives the LLM operating constraints for the session:

- **Scope**: constrained to the project directory tree
- **Git**: required to commit frequently with conventional messages
- **Rate limits**: wait and write a pause note, do not fail
- **Input protocol**: `DATAWATCH_NEEDS_INPUT:` line for async input requests
- **Completion protocol**: `DATAWATCH_COMPLETE:` line when done
- **Safety**: no deletes without confirmation, no secrets in commits

## Disabling Tracking

The session folder is always created — it's how the daemon tracks state.
`auto_git_commit` no longer has any effect; project-directory commits
for a task session are unconditional and cannot be disabled.
`auto_git_init` still controls whether datawatch initializes a git repo
in the project directory if one doesn't exist:

```yaml
session:
  auto_git_init: false
```

## Storage

Session folders accumulate over time. Each folder is small (text files + git objects).
To clean up old completed sessions:

```bash
# List sessions older than 30 days
find ~/.datawatch/sessions -maxdepth 1 -mtime +30 -type d

# Archive or remove
datawatch session purge --older-than 30d
```

### Repository size & maintenance

Each session folder's git repo only records the small text files (`task.md`,
`timeline.md`, `conversation.md`, `session.json`, `README.md`, guardrail files).
The live `output.log` (and `output.log.enc` / `output.log.pipe`) stays on disk and is
never committed. Repos created before this change had `output.log` tracked; the
daemon untracks it automatically (`git rm --cached`, the file itself is kept) on the
first commit it makes to that repo after a restart.

When the daemon creates or resumes a session repo it also sets these git options on it:

| Option | Value | Why |
|--------|-------|-----|
| `gc.auto` | `0` | Turns off git's implicit auto-gc. Without this, an ordinary `git commit` made by the daemon can start a full repack in the background. |
| `gc.autoDetach` | `false` | If a gc does run, it does not detach and keep running unseen. |
| `pack.windowMemory` | `64m` | Caps the delta-search memory per pack thread. |
| `pack.threads` | `1` | One pack thread, so the cap above is the total. |
| `core.bigFileThreshold` | `16m` | Files over 16MB are stored without delta compression, which is where repack memory goes. |

Why this matters: the daemon makes these commits from inside its own process and
systemd cgroup. Any git work they trigger counts against the daemon's memory, so an
implicit auto-gc on a large repo can exhaust host memory and the OOM killer then takes
down the whole daemon, not just that git process. With auto-gc off, session repos are
never repacked by the daemon. They stay small because `output.log` is no longer in
history.

Repos that grew large before this change keep their old history until you shrink them.
Use `scripts/shrink-session-repo.sh` (below), which runs outside the daemon under a
memory cap. `scripts/repro-tracker-git-growth.sh` reproduces the growth in a scratch
directory and prints a size table, before and after the fix.

### Shrinking a bloated session repo

Session folders created before `output.log` was excluded from git history can grow
to many gigabytes, because every commit stored a full copy of the growing log. Do not
run `git gc` or `git repack` on one of these repos from inside the daemon, and do not
run them by hand either. On 2026-10-13 a repack of such a repo used over 100GB of
memory and the OOM killer took down the whole datawatch service.

Use `scripts/shrink-session-repo.sh` instead. It deletes `output.log` and
`output.log.enc` from the repo's git **history** only. The on-disk `output.log` is
moved aside during the rewrite and then put back, and the script verifies that its
size and inode are unchanged. The heavy git steps (history rewrite, `gc --prune=now`,
`fsck`) run in a transient `systemd-run --user` scope with `MemoryMax=4G` and
`MemorySwapMax=0`, at nice 19 and idle I/O priority. If they overrun, only that scope is
killed, never `datawatch.service`. The script prints the scope's peak memory. Without a
systemd user manager it falls back to `nice`/`ionice` plus `ulimit -v`.

When to run it: when `du -sh ~/.datawatch/sessions/*/.git` shows a session repo far
larger than its text files. Run it on completed sessions. It refuses a session whose
`session.json` state is `running`/`waiting_input`, a repo that committed within the
last 2 minutes, and a repo with a `.git/index.lock`.

```bash
# See what would happen (changes nothing)
scripts/shrink-session-repo.sh --dry-run ~/.datawatch/sessions/<session-dir>

# Shrink one or more session repos
scripts/shrink-session-repo.sh ~/.datawatch/sessions/<dir1> ~/.datawatch/sessions/<dir2>

# Cheap, non-destructive sweep: apply only the git hygiene settings
# (gc.auto=0, bounded pack memory, output.log in .git/info/exclude) to every session repo
scripts/shrink-session-repo.sh --all-hygiene ~/.datawatch/sessions
```

Options and environment:

| Setting | Effect |
|---------|--------|
| `--allow-active` | Rewrite even if the session looks active. `.git/index.lock` is still re-checked just before rewriting. |
| `SHRINK_MEMORY_MAX=8G` | Raise the memory cap. If the scope is killed, the script says so, restores `output.log`, and leaves history intact, so you can retry with a higher cap. |
| `SHRINK_FILTER_TOOL=filter-branch` | Force `git filter-branch` even if `git-filter-repo` is installed (the default is filter-repo when available). |
| `SHRINK_NO_SCOPE=1` | Skip `systemd-run` and use the `ulimit -v` fallback. |

The rewrite changes commit hashes in the session repo, so anything that recorded a
session-repo commit hash (not the project repo's) will no longer resolve. History of
`timeline.md` and `conversation.md` is kept: the script checks that their commit counts
are unchanged. If a run is interrupted hard (e.g. `kill -9`), the live file is left in
`<session-dir>/.git/shrink-backup/`. The script refuses to run again until you move it
back.
