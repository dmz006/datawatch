---
docs:
  index: true
  topics: [autonomous, automaton, parallel, tasks, read only, worktree, merge, conflict, speedup]
exec_steps:
  - tool: autonomous_config_get
    description: Read the autonomous settings, including the autonomous.parallel block (read_only_tasks, worktree_per_task, merge_conflict_policy, merge_conflict_model)
    args: {}
    read_only: true
  - tool: autonomous_prd_get
    description: Read one Automaton, its tasks with their merge state, and the parallel execution numbers recorded when it completed
    args:
      id: "<automaton id>"
    read_only: true
---

# Running the tasks of one Automaton in parallel

An Automaton is a graph of tasks. Tasks that do not depend on each other can run at
the same time. Two settings make that safe: **read-only tasks** skip the write lock,
and a **worktree per task** gives every other parallel task its own copy of the
project to write in. When a task is verified its work is **merged back** into the
Automaton branch, in dependency order. This page shows how to look at the settings,
flag a task read only, read what happened and choose what a merge conflict does.

The defaults below are the operator's choice for this installation. Other users may
change every one of them.

## The settings

All four live under `autonomous.parallel` and are changeable from the web app
(Settings, Automata), `PUT /api/config`, `datawatch config set`, the chat
`configure` verb and the MCP `autonomous_config_set` tool (arguments
`parallel_read_only_tasks`, `parallel_worktree_per_task`,
`parallel_merge_conflict_policy`, `parallel_merge_conflict_model`).

| Key | Default | Meaning |
|---|---|---|
| `autonomous.parallel.read_only_tasks` | `true` | A task flagged read only skips the tree lock and runs beside other tasks. `false` makes every task take the lock. |
| `autonomous.parallel.worktree_per_task` | `true` | Each task that runs alongside others gets its own worktree and branch off the Automaton branch. `false` keeps all tasks in the one worktree. |
| `autonomous.parallel.merge_conflict_policy` | `additive_then_model` | What happens when a task branch does not merge cleanly: `additive_then_model`, `block` or `triage`. |
| `autonomous.parallel.merge_conflict_model` | empty | The model that resolves a conflict. Empty reads the non-Opus daemon default (`sonnet`); a model the operator names, an Opus model included, is used as written. |

An explicit `false` is kept; only an absent key reads the default.

## Flag a task read only

1. Add or edit the task with `read_only` set (MCP `autonomous_prd_add_task` or
   `autonomous_prd_edit_task`, `datawatch autonomous prd-add-task` / `prd-edit-task
   --read-only`, the chat `add task` / `edit task` verb with `read_only=`, or the
   task edit in the web app).
2. Leave it unset for any task that writes files. A task that is not flagged takes
   the lock.

A read-only task still **cannot** make any other filesystem change: after it
finishes, datawatch re-checks the tree with `git status --porcelain
--untracked-files=all` and fails the task if it is dirty — unless the change is
exactly the one report file below. Only new dirt fails a read-only task: a path that was already dirty before the task started, or one that disappears while it runs, never counts against it.

## Allow a read-only task one report file

A read-only task can still *report*: it may carry a **`report_path`** — the one
clean repo-relative file it is allowed to write, so the rule stays one (no tree
changes) while the task can leave its deliverable (for example a gate counts
report).

- **A valid path** is repo-relative, under `.datawatch/reports/`, with no `..` and
  no absolute path. Anything else is refused with a message naming the offending
  piece; an empty value means the task writes nothing.
- **Only a read-only task may have one.** A `report_path` on a task that writes
  files — including an edit that sends `read_only: false` with a path in the same
  call — is refused with a clear `400` naming `read_only`, and nothing is stored.
- **The dirty check ignores exactly that file.** After the task finishes, the
  `git status` selection excludes `.datawatch` (as before) and exactly the report
  path, so writing the report completes the task while any other stray change
  still fails it. The same goes for the two untracked launcher files the session
  launcher drops into the working directory, `worker-mcp.json` and
  `worker-starting-context.md` (untracked root copies only — a tracked file with
  a launcher name at the root, and a same-named file in a subfolder, are still a
  change).

Set it next to the task's `read_only` mark, from the same surfaces as above:

```bash
datawatch autonomous prd-add-task <id> --story <s> --title <t> \
  --read-only --report-path .datawatch/reports/gate.txt
datawatch autonomous prd-edit-task <id> --task <t> \
  --read-only --report-path .datawatch/reports/gate.txt
```

or the `report_path` parameter of the two MCP tools, the `report path` token of the
chat `add task` / `edit task` verb, the `report_path` field of
`POST /api/autonomous/prds/{id}/add_task` and `.../edit_task` (an omitted field in
an edit leaves it unchanged; `""` clears it), or in the plan JSON as
`"report_path": ".datawatch/reports/..."` next to the task's `"read_only": true`
— plan coverage stops flagging the task's write verb for exactly that case, since
a report writes it. (Mobile parity for the field is tracked in datawatch-app
issue [#352](https://github.com/dmz006/datawatch-app/issues/352).)

### Report files left in the old reports folder

A new untracked file a read-only task leaves under `docs/plans/reports/` is
moved into the folder of its report path under `.datawatch/reports/`, so it does
not fail the task. No content is lost:

- It keeps its name when the name is free.
- When a file with **different content** already has the name, both are kept.
  The moved file gets a new name, `<name>.stray-<n>.<ext>`.
- The stray file is deleted only when a kept file has exactly the same content.
- It never takes the name of the daemon's default gate report,
  `final-gate.txt`.

The Automaton's decision record and the audit log
(`autonomous_read_only_report_settled`) name each file and where its content is.

## Choose a merge conflict policy

- `additive_then_model` keeps both sides when the two changes only add lines, and
  asks the merge conflict model to resolve anything else.
- `block` holds the task at once and leaves the conflict for you.
- `triage` raises the conflict as a question for you to answer.

## Before a task branch merges: daemon-side cleanup

Before a task branch is merged into the Automaton's work branch, the daemon
settles the two kinds of files a session can leave that are not the task's own
work:

- **Rules files carrying only the daemon's sections.** If the branch's
  committed `AGENT.md` or `CLAUDE.md` differs from the base commit only by the
  daemon's own sections, the file is restored to the base content; if it
  also carries a real edit, the daemon's sections are stripped and the edit is
  kept. Only text that is
  [exactly what the daemon writes](automaton-protected-paths.md#the-daemons-own-sections-in-the-rules-file)
  is stripped. Other text between its markers, under its headings, or after a
  marker that is never closed is merged as it stands.
- **The root launcher files.** If the branch committed the session launcher's
  own root files — `worker-mcp.json` and `worker-starting-context.md` — they
  are removed, but **only when the merge target state has no such files**
  (present at the branch tip, absent at the base commit). A launcher-named
  file that belongs to the target state, and a same-named file in a subfolder,
  are both kept — fail-closed.

Everything goes through the branch's ordinary `git add` / `git rm` /
`git commit` path, so the branch advances by one plain cleanup commit —
`Strip daemon sections from <file> before merge`, `Remove launcher files
<names> before merge`, or both — and a branch with nothing of the sort gets no
cleanup commit at all, so its merge moves the work branch exactly once. A
rules or launcher file with uncommitted changes beyond what the cleanup would
touch is an error, and the merge does not run.

**Operator review.** A task branch that committed the launcher files *before*
this change keeps them once merged, and already-landed history is never
rewritten: if you see `worker-mcp.json` or `worker-starting-context.md` on a
merged branch or in a release, review or remove them yourself.

## When a finished task cannot merge yet: the daemon repairs it

Two states used to leave a finished task retrying its merge every few seconds
with nothing you could do but repair the tree by hand. The daemon now repairs
both before it runs the merge, and logs what it did.

- **The task's branch is gone, but its worktree still holds the finished
  work.** This happens when the task worktree is a clone of its own, or sits on
  a detached commit. The daemon brings the commit into the Automaton's
  repository if it is not there yet, and recreates the branch at that commit.
  When the branch exists and the task worktree is detached on a newer commit
  (the cleanup commit above), the branch is moved forward to it. If neither the
  branch nor the worktree is left, the merge reports that the finished work
  cannot be recovered and nothing is changed.
- **An untracked file in the Automaton's worktree has the path of a file the
  task branch adds.** Git refuses such a merge ("untracked working tree files
  would be overwritten"). A copy identical to the branch's file is dropped. A
  different copy is moved, not deleted, to
  `.datawatch/merge-aside/<task id>/<path>` inside the Automaton's worktree, so
  you can read it afterwards. A file moved aside earlier is never replaced: a
  later one with different content gets the name `<name>.stray-<n>.<ext>`. The
  audit log (`autonomous_merge_files_moved_aside`) says where each one went.
  Untracked files the merge does not touch are never moved.

There is no setting for this: both repairs run on every task merge.

## Read what happened

- **Task merge state** on each task card, in `datawatch autonomous prd-get <id>` and
  in the chat `autonomous get` reply: `pending`, `merged` (with the merge commit),
  `conflict` or `held`.
- **Parallel execution** card on the Automaton view, a `parallel:` line in
  `datawatch autonomous prd-get`, and the fields of `GET /api/autonomous/prds/{id}`:
  total tasks, read-only tasks, wall-clock time, serial time and the **speedup**
  (serial time divided by wall-clock time; 1 means the tasks ran one after another).

A `held` task waits for you. Resolve the conflict in the task branch, then reset or
resume the task from the Automaton view.

## Related

- Definitions: "Read-only tasks", "Parallel task worktrees" and "Parallel speedup"
  in [datawatch-definitions.md](../datawatch-definitions.md).
- [config-reference.yaml](../config-reference.yaml) for the commented keys.
