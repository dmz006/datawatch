---
docs:
  index: true
  topics: [git, guardrails, autonomous, automata]
exec_params:
  - {name: automaton_id, required: true, description: "Automaton ID whose tasks you want to inspect for foreign_file_drift"}
  - {name: drift_mode, required: false, default: "block", description: "Daemon-wide enforcement for pre-existing drift: block (the default) stops the task; warn records the finding and the task completes. Only block or warn are accepted. The MCP set tool does not take this key; set it via CLI/REST/YAML."}
exec_steps:
  - tool: autonomous_config_get
    description: Read the current autonomous.drift_mode (block = default, warn = flag-only) and block_on_file_drift settings
    args: {}
  - tool: shell
    description: Set the daemon-wide drift_mode (block or warn). Per-Automaton opt-out to warn-only is instead block_on_file_drift=false on the Automaton or its guardrail profile (see per_automaton_guardrails_set / guardrail_profile_update)
    args: {command: "datawatch config set autonomous.drift_mode {{params.drift_mode}}"}
  - tool: autonomous_prd_get
    description: Read the Automaton; each task carries foreign_file_drift when its commit swept in a pre-existing uncommitted file, and the decision log has a file_drift_detected entry (in-run edits by the task's own worker get one too)
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

It's always on, costs no LLM call — it's a plain comparison of git file
lists — and it distinguishes two kinds of finding: a swept-in edit that
already existed (pre-existing drift) **blocks the task by default**, while
edits made by the task's own worker are always warn-only, never blocked.
You can relax the pre-existing default to warn-only with the
`autonomous.drift_mode` setting (below).

## What it detects and why

Before each worker session starts in direct mode, the executor
records:

- the commit `HEAD` points at (the pre-task SHA),
- every tracked file with uncommitted changes, unstaged or staged
  (`git diff --name-only HEAD` plus `git diff --name-only --cached HEAD`),
- every untracked file (`git ls-files --others --exclude-standard`).

After the session finishes and the task's verifier has run, it
collects every file touched by every commit in `<pre-task SHA>..HEAD`.
Each touched file lands in exactly one bucket:

- **foreign file drift (pre-existing)** — all three are true: a commit
  made during the task touched it, it was already uncommitted (modified,
  staged or untracked) before the task started, and it is **not** in the
  task's planned `files` list. This is the finding that can block the
  task (default mode).
- **in-run edit (the worker's own)** — the file was clean before the task
  started, so the task's worker made the change (planned or not, e.g. a
  newly created file). This is always recorded, warned and audited.
  `drift_mode` never blocks on it; whether an in-run edit to a file the
  task never declared can block is the separate `undeclared_edit_mode`
  setting (see [Undeclared edits](#undeclared-edits)).

The typical cause of pre-existing drift is a worker running `git add -A`
or `git commit -a` in a tree that also holds someone else's work in
progress. The diff looks fine to the verifier because it's real content;
it's just not this task's content.

## What is and isn't flagged

| Situation | Flagged? | Blocks (drift_mode: block)? |
|---|---|---|
| Task commits a file that was uncommitted beforehand and isn't in its planned `files` | **Yes — pre-existing drift: the task's `foreign_file_drift`, a `file_drift_detected` decision and an audit event** | **Yes (the default)** — task set to Blocked, Automaton halts; under `drift_mode: warn` it is flagged only and the task completes |
| Task commits a file that was uncommitted beforehand but **is** in its planned `files` | No. Planned files are never flagged, even if they were dirty when the task started | No |
| Task commits a file that was clean before it started (new edit by the task itself, planned or not) | **Yes — an in-run finding: a `file_drift_detected` decision noting it is the worker's own edit, logged and audited** | **Never under `drift_mode`** — in-run edits are warn-only there; undeclared ones are governed by `undeclared_edit_mode` (see [Undeclared edits](#undeclared-edits)) |
| Task leaves a pre-existing uncommitted file alone (still uncommitted afterwards) | No | No |
| Automaton runs in **worktree** mode (git branch set, auto-PR) | No. The check is skipped entirely; each task gets its own clean worktree, so there's nothing pre-dirty to protect | No |
| Automaton has no project directory, or the pre-task SHA couldn't be read | No. Skipped | No |

Paths are compared after normalising (`./a/b` and `a/b` are the same
file). Findings from several retries of the same task accumulate.

The check **never modifies your repository**. It doesn't revert,
reset, amend, un-stage or rewrite anything, and it never rewrites git
history.

## Blocking vs. warn (`drift_mode`)

The daemon-wide enforcement knob is **`autonomous.drift_mode`**, with
two values:

| `drift_mode` | What happens on a pre-existing finding |
|---|---|
| `block` (**the default**) | The task is set to **Blocked** with error `foreign_file_drift: <files>`, and the Automaton stops walking its tasks so you can look. |
| `warn` | The task keeps going and completes normally. The finding is recorded on the task, in the decision log and in the audit log. |

Notes on the mode:

- It applies to **pre-existing drift only**. Edits made by the task's
  own worker are recorded, warned and audited in *both* modes and never
  block, whether the files were in the task's planned `files` list or
  not.
- It is **global**: an Automaton or its guardrail profile does not
  override the mode. The daemon-wide setting cannot be relaxed per
  Automaton — the per-Automaton direction only goes *toward* warn-only
  (opt-out, next).
- Only `block` and `warn` are accepted; any other value is rejected at
  the config surface, and an empty/unknown value resolves to `block`.
  An unrecognized mode never silently relaxes enforcement.

**Per-Automaton opt-out.** Even under `drift_mode: block`, an explicit
`block_on_file_drift: false` on the Automaton itself or on its
guardrail profile opts *that* Automaton out of blocking: its findings
become warn-only. This is the operator's escape hatch for Automata you
trust in a shared tree. Beware the shape of the flag: it remains a
legacy opt-in boolean, so the daemon-wide `false` default by itself is
**not** an opt-out and does not change the default `block` behaviour;
what matters is the explicit `false` on the PRD or profile.

Reading the effective value:

- MCP: `autonomous_config_get` includes `drift_mode` (it shows the
  effective mode, so an unset value renders as `block`).
- REST: `GET /api/config` (key `autonomous.drift_mode`) or
  `GET /api/autonomous/config`.

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

With `drift_mode: block` (the default), the same task instead shows
`"status": "blocked"` and
`"error": "foreign_file_drift: docs/notes.md, internal/server/web/app.js"`.

**PWA.** The Automaton detail view shows a warning chip (⚠ and a
count) on the affected task, and the task's expanded view lists the
files under **Foreign file drift**.

**Audit log.** Each finding writes an `autonomous_file_drift_detected`
event (CEF severity informational) with `prd_id`, `task_id` and
`files`. Query it with `audit_query` or `GET /api/audit`. The daemon
log also gets a `WARNING ... foreign file drift` line.

## Changing the mode

Blocking pre-existing drift is the **default** (`drift_mode: block`).
Relax it to warn-only where you want, for example Automata you trust
in a shared tree.

### Daemon-wide: `drift_mode`

Only `block` and `warn` are accepted; any other value is rejected with
an error naming the two legal values, and an unset value behaves as
`block`.

YAML (`~/.datawatch/config.yaml`):

```yaml
autonomous:
  drift_mode: warn
```

REST:

```sh
BASE=https://localhost:8443
curl -sk -X PUT -H "Content-Type: application/json" \
  -d '{"key":"autonomous.drift_mode","value":"warn"}' \
  $BASE/api/config
```

CLI:

```sh
datawatch config set autonomous.drift_mode warn
```

Read it back with `autonomous_config_get` (or `GET /api/config`, key
`autonomous.drift_mode`) — it shows the effective value, so an unset
setting reads as `block`. Set it to `block` the same way to return to
the enforced default. Note the MCP `autonomous_config_set` tool does not
take this key yet; use YAML, REST, or the CLI for it.

### For a guardrail profile (opt-out)

An explicit `block_on_file_drift: false` on a profile opts that
profile's Automata out of blocking; their pre-existing findings become
warn-only even under `drift_mode: block`.

MCP:

```json
{
  "tool": "guardrail_profile_create",
  "args": {
    "name": "trusted-shared-tree",
    "description": "Automata that run in a tree other sessions also edit; trust their WIP",
    "guardrails_json": "[\"rules-review\"]",
    "block_on_file_drift": false
  }
}
```

`guardrail_profile_update` takes the same optional
`block_on_file_drift`; omit it to leave the profile's value as it is.

REST:

```sh
curl -sk -X POST -H "Content-Type: application/json" \
  -d '{"name":"trusted-shared-tree","guardrails":["rules-review"],"block_on_file_drift":false}' \
  $BASE/api/autonomous/guardrail_profiles
```

CLI:

```sh
datawatch guardrail profile create --name trusted-shared-tree \
  --guardrails rules-review --block-on-file-drift=false
```

Then assign the profile to an Automaton (next section, or
`guardrail automaton set <id> profile=trusted-shared-tree` from a comm
channel).

### For one Automaton (opt-out)

> The per-Automaton set call **replaces** the profile, all three
> guardrail lists and `block_on_file_drift` in one go. Read the
> Automaton first with `autonomous_prd_get` and pass what you want to
> keep. Omitting `block_on_file_drift` clears it back to inherit.

MCP:

```json
{
  "tool": "per_automaton_guardrails_set",
  "args": { "id": "a1b2c3d4", "guardrail_profile": "release", "block_on_file_drift": false }
}
```

REST:

```sh
curl -sk -X PUT -H "Content-Type: application/json" \
  -d '{"guardrail_profile":"release","block_on_file_drift":false}' \
  $BASE/api/autonomous/prds/a1b2c3d4/guardrails
```

CLI:

```sh
datawatch guardrail automaton set a1b2c3d4 --profile release --block-on-file-drift=false
```

Comm channel: the per-Automaton command takes a profile, so use a
profile with `block_on_file_drift` set (above).

YAML: per-Automaton settings live in the Automaton record, not in
`config.yaml`. Use any surface above.

## Undeclared edits

`drift_mode` governs files that were **already uncommitted before the task started**. A separate check covers the other half: files the task's own worker edited or created that the task never declared. A file is reported as an **undeclared edit** when all of these are true:

1. a commit made during the task touched it (`<pre-task SHA>..HEAD`),
2. it was clean before the task started (so it is the worker's own edit, not pre-existing drift), and
3. it is in neither the task's planned `files` nor its **generated files** (files the task is expected to produce, such as a verification script's output).

Generated files count as declared, so a task that creates the files it is expected to create is not flagged. The typical hit is a worker taking a drive-by edit, for example fixing an unrelated flaky test or tweaking an adjacent file.

A file is never reported by both checks. Pre-existing files belong to `drift_mode` and the worker's own edits belong to `undeclared_edit_mode`, so a single file can never trigger both. If both would block in the same attempt, the drift block fires first and the undeclared check is not reached for that attempt. Like the drift check it runs only in direct mode, right after the verifier, with read-only git commands, and it never reverts or modifies anything.

### off / warn / block

| `autonomous.undeclared_edit_mode` | What happens on a finding |
|---|---|
| `off` | The check does nothing: nothing is recorded or alerted. |
| `warn` (default) | The finding is recorded and alerted. The task keeps going and completes normally. |
| `block` | The task is set to **Blocked** with error `undeclared_edit: <files>` and the Automaton stops walking its tasks. |

Only `off`, `warn` and `block` are accepted; anything else is rejected with HTTP 400 on `PUT /api/config`. A bad value that reaches storage another way (for example hand-edited YAML) is treated as `warn`, never `off`. The mode is daemon-wide; there is no per-Automaton or per-profile override. Set it like any other config key, for example `datawatch config set autonomous.undeclared_edit_mode block`, `PUT /api/config`, MCP `autonomous_config_set`, comm `configure autonomous.undeclared_edit_mode=block`, or Settings → Autonomous Automata planning → **Undeclared edit mode** in the PWA. See [operations.md](../operations.md#undeclared-edit-mode-autonomousundeclared_edit_mode).

### Where the warning shows

- **On the task** — an **Undeclared edits** badge in the Automaton detail view, and an `undeclared_edits` list (path and lines changed, summed across retries) from `autonomous_prd_get` / `GET /api/autonomous/prds/<id>`.
- **In the Automaton's decisions** — one `undeclared_edit_detected` entry naming the task and files.
- **As an alert** — a warn-level "Undeclared edit detected" alert.
- **In the audit log** — an `autonomous_undeclared_edit_detected` event with `prd_id`, `task_id`, `files`, `lines` and `mode`.

With `block` the task instead shows `"status": "blocked"` and `"error": "undeclared_edit: <files>"`; recover it as described under [Recovery](#recovery).

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
4. If the task is **Blocked** and you're happy with what it committed:
   - **Approve that one task.** Per-task approval now accepts a PRD in
     the **blocked** status (draft and cancelled still refuse), and an
     approved blocked task is released back to `pending` with its stale
     error cleared, ready for an existing run path to pick up:

     ```
     autonomous_prd_approve_task {"id":"a1b2c3d4","task_id":"t-3"}
     autonomous_prd_run          {"id":"a1b2c3d4"}
     ```
   - **Or reset and re-run it**, or move on:

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
