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
| `output.log` | Full raw claude-code output, streamed live by tmux pipe-pane |
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

When `auto_git_commit: true` (default), datawatch also manages commits in the
**project directory** — the folder where claude-code is actually working:

| Event | Commit message |
|-------|---------------|
| Session starts | `pre-session[a3f2]: refactor auth module` |
| Session completes | `session[a3f2](complete): refactor auth module` |

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
To disable project directory git commits:

```yaml
session:
  auto_git_commit: false
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
