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
| `autonomous.parallel.merge_conflict_model` | empty | The model that resolves a conflict. Empty uses the strongest configured planning model. |

An explicit `false` is kept; only an absent key reads the default.

## Flag a task read only

1. Add or edit the task with `read_only` set (MCP `autonomous_prd_add_task` or
   `autonomous_prd_edit_task`, or the task edit in the web app).
2. Leave it unset for any task that writes files. A task that is not flagged takes
   the lock.

## Choose a merge conflict policy

- `additive_then_model` keeps both sides when the two changes only add lines, and
  asks the merge conflict model to resolve anything else.
- `block` holds the task at once and leaves the conflict for you.
- `triage` raises the conflict as a question for you to answer.

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
