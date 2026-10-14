---
docs:
  index: true
  topics: [git, guardrails, autonomous, automata]
exec_params:
  - {name: automaton_id, required: true, description: "Automaton ID whose tasks you want to inspect for foreign_file_drift"}
  - {name: block_on_file_drift, required: false, default: "true", description: "Daemon-wide default: true blocks a task whose commit swept in someone else's uncommitted file, false keeps it warn-only"}
exec_steps:
  - tool: autonomous_config_get
    description: Read the current daemon-wide block_on_file_drift setting (false = warn-only, the default)
    args: {}
  - tool: autonomous_config_set
    description: Set the daemon-wide block_on_file_drift default (an Automaton's own setting or its guardrail profile still overrides it)
    args: {block_on_file_drift: "{{params.block_on_file_drift}}"}
  - tool: autonomous_prd_get
    description: Read the Automaton; each task carries foreign_file_drift when its commit swept in a pre-existing uncommitted file, and the decision log has a file_drift_detected entry
    args: {id: "{{params.automaton_id}}"}
---
# How-to: File Drift Guardrail

An Automaton running in **direct mode** (no worktree; its tasks work in
your real project directory) commits its work into the same tree you,
another session, or another Automaton may be editing. If a task's
commit picks up a file that was already sitting there uncommitted
before the task started, and the task never planned to touch it, that
edit now ships under the Automaton's commit message. The file drift
check notices this and tells you.

It's always on, warn-only by default, and costs no LLM call: it's a
plain comparison of git file lists.

## What it detects and why

Before each worker session starts in direct mode, the executor
records:

- the commit `HEAD` points at (the pre-task SHA),
- every tracked file with uncommitted changes, unstaged or staged
  (`git diff --name-only HEAD` plus `git diff --name-only --cached HEAD`),
- every untracked file (`git ls-files --others --exclude-standard`).

After the session finishes and the task's verifier has run, it
collects every file touched by every commit in `<pre-task SHA>..HEAD`.
A file is reported as **foreign file drift** when all three are true:

1. a commit made during the task touched it,
2. it was already uncommitted (modified, staged or untracked) before
   the task started, and
3. it is **not** in the task's planned `files` list.

The typical cause is a worker running `git add -A` or `git commit -a`
in a tree that also holds someone else's work in progress. The diff
looks fine to the verifier because it's real content; it's just not
this task's content.

## What is and isn't flagged

| Situation | Flagged? |
|---|---|
| Task commits a file that was uncommitted beforehand and isn't in its planned `files` | **Yes** |
| Task commits a file that was uncommitted beforehand but **is** in its planned `files` | No. Planned files are never flagged, even if they were dirty when the task started |
| Task commits a file that was clean before it started (new edit by the task itself) | No |
| Task leaves a pre-existing uncommitted file alone (still uncommitted afterwards) | No |
| Automaton runs in **worktree** mode (git branch set, auto-PR) | No. The check is skipped entirely; each task gets its own clean worktree, so there's nothing pre-dirty to protect |
| Automaton has no project directory, or the pre-task SHA couldn't be read | No. Skipped |

Paths are compared after normalising (`./a/b` and `a/b` are the same
file). Findings from several retries of the same task accumulate.

The check **never modifies your repository**. It doesn't revert,
reset, amend, un-stage or rewrite anything, and it never rewrites git
history.

## Warn-only vs. blocking

| `block_on_file_drift` | What happens on a finding |
|---|---|
| `false` (default) | The task keeps going and completes normally. The finding is recorded on the task, in the decision log and in the audit log. |
| `true` | The task is set to **Blocked** with error `foreign_file_drift: <files>`, and the Automaton stops walking its tasks so you can look. |

The setting is resolved in the same priority order as other guardrail
settings ([guardrail-library.md](guardrail-library.md)):

1. **The Automaton's own setting** (`block_on_file_drift` on the
   Automaton; unset = inherit)
2. **Its guardrail profile** (`block_on_file_drift` on the profile;
   unset = inherit)
3. **Global config** (`autonomous.block_on_file_drift`, default
   `false`)

## Where to see it

**MCP / REST.** `autonomous_prd_get` (REST `GET
/api/autonomous/prds/<id>`) returns each task with a
`foreign_file_drift` list when drift was found (absent otherwise):

```json
{
  "id": "t-3",
  "title": "Add retry to the fetch client",
  "status": "completed",
  "files": ["internal/fetch/client.go", "internal/fetch/client_test.go"],
  "foreign_file_drift": ["docs/notes.md", "internal/server/web/app.js"]
}
```

The Automaton's `decisions` list gets one entry per finding:

```json
{
  "at": "2026-10-14T12:03:51Z",
  "kind": "file_drift_detected",
  "actor": "autonomous",
  "note": "task t-3: committed pre-existing uncommitted file(s) outside its planned files: docs/notes.md, internal/server/web/app.js"
}
```

With strict mode on, the same task shows `"status": "blocked"` and
`"error": "foreign_file_drift: docs/notes.md, internal/server/web/app.js"`.

**PWA.** The Automaton detail view shows a warning chip (⚠ and a
count) on the affected task, and the task's expanded view lists the
files under **Foreign file drift**.

**Audit log.** Each finding writes an `autonomous_file_drift_detected`
event (CEF severity informational) with `prd_id`, `task_id` and
`files`. Query it with `audit_query` or `GET /api/audit`. The daemon
log also gets a `WARNING ... foreign file drift` line.

## Turning strict mode on

### Globally (every Automaton)

YAML (`~/.datawatch/config.yaml`):

```yaml
autonomous:
  block_on_file_drift: true
```

PWA: Settings → **Automate** → **Block on file drift** toggle.

REST:

```sh
BASE=https://localhost:8443
curl -sk -X PUT -H "Content-Type: application/json" \
  -d '{"key":"autonomous.block_on_file_drift","value":true}' \
  $BASE/api/config
```

MCP:

```json
{ "tool": "autonomous_config_set", "args": { "block_on_file_drift": true } }
```

CLI:

```sh
datawatch config set autonomous.block_on_file_drift true
```

Comm channel:

```
You: configure autonomous.block_on_file_drift=true
```

Read it back with `autonomous_config_get` (or `GET /api/config`, key
`autonomous.block_on_file_drift`). Set it to `false` the same way to
return to warn-only.

### For a guardrail profile

MCP:

```json
{
  "tool": "guardrail_profile_create",
  "args": {
    "name": "shared-tree",
    "description": "Automata that run in a tree other sessions also edit",
    "guardrails_json": "[\"rules-review\"]",
    "block_on_file_drift": true
  }
}
```

`guardrail_profile_update` takes the same optional
`block_on_file_drift`; omit it to leave the profile's value as it is.

REST:

```sh
curl -sk -X POST -H "Content-Type: application/json" \
  -d '{"name":"shared-tree","guardrails":["rules-review"],"block_on_file_drift":true}' \
  $BASE/api/autonomous/guardrail_profiles
```

CLI:

```sh
datawatch guardrail profile create --name shared-tree \
  --guardrails rules-review --block-on-file-drift
```

Then assign the profile to an Automaton (next section, or
`guardrail automaton set <id> profile=shared-tree` from a comm
channel).

### For one Automaton

> The per-Automaton set call **replaces** the profile, all three
> guardrail lists and `block_on_file_drift` in one go. Read the
> Automaton first with `autonomous_prd_get` and pass what you want to
> keep. Omitting `block_on_file_drift` clears it back to inherit.

MCP:

```json
{
  "tool": "per_automaton_guardrails_set",
  "args": { "id": "a1b2c3d4", "guardrail_profile": "release", "block_on_file_drift": true }
}
```

REST:

```sh
curl -sk -X PUT -H "Content-Type: application/json" \
  -d '{"guardrail_profile":"release","block_on_file_drift":true}' \
  $BASE/api/autonomous/prds/a1b2c3d4/guardrails
```

CLI:

```sh
datawatch guardrail automaton set a1b2c3d4 --profile release --block-on-file-drift
```

Comm channel: the per-Automaton command takes a profile, so use a
profile with `block_on_file_drift` set (above).

YAML: per-Automaton settings live in the Automaton record, not in
`config.yaml`. Use any surface above.

## Recovery

Nothing is reverted automatically. The drifted edit is already in a
commit, with the Automaton's message on it, and that's the only thing
that's "wrong": the content itself is whatever was in the file.

1. Read the finding: `autonomous_prd_get` → the task's
   `foreign_file_drift`, or the `file_drift_detected` decision.
2. Find whose edit it was (your own work in progress, another session,
   another Automaton) and tell them it has been committed, so they
   don't commit it again or lose track of it.
3. If you want the history to say who really made the change, fix it
   by hand with a new commit: for example revert just those files in a
   follow-up commit and let the owner commit them with their own
   message. Don't rewrite published history.
4. If the task is **Blocked** (strict mode) and you're happy with what
   it committed, reset and re-run it, or move on:

   ```
   autonomous_prd_reset_task {"id":"a1b2c3d4","task_id":"t-3","force":true}
   autonomous_prd_run        {"id":"a1b2c3d4"}
   ```

   See [autonomous-recovery.md](autonomous-recovery.md) for the other
   recovery routes.

To stop it happening: commit or stash your own work before starting a
direct-mode Automaton, list every file a task should touch in its
`files`, or run the Automaton in worktree mode.

## See also

- [guardrail-library.md](guardrail-library.md): guardrail profiles, per-Automaton overrides, resolution order
- [ci-watch-guardrail.md](ci-watch-guardrail.md): the completion guardrail that watches CI for what an Automaton pushed
- [autonomous-recovery.md](autonomous-recovery.md): resetting and re-running tasks on a Blocked Automaton
- [mcp.md](../mcp.md): MCP tool reference
