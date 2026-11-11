---
docs:
  index: true
  topics: [autonomous, automation, automaton, branch, refresh, main, base branch, merge, conflict, behind, worktree, decisions]
exec_params: []
exec_steps:
  - tool: get_config
    description: Read the daemon settings, including autonomous.branch_refresh_enabled and autonomous.branch_refresh_behind_limit
    args: {}
    read_only: true
  - tool: config_set
    description: Turn the between-task branch refresh on or off for every Automaton that has no override of its own
    args:
      key: autonomous.branch_refresh_enabled
      value: "{{params.enabled}}"
    read_only: false
  - tool: config_set
    description: Set how many commits behind its base a branch may be before it is refreshed (0 reads as 5)
    args:
      key: autonomous.branch_refresh_behind_limit
      value: "{{params.limit}}"
    read_only: false
  - tool: question_list
    description: List the open operator questions; a refresh that hit a conflict waits on one asked by branch-refresh
    args: {state: open}
    read_only: true
  - tool: question_get
    description: Open one question to read the conflicting files and the three options
    args: {id: ""}
    read_only: true
---

# Automaton branch refresh

An Automaton that runs in worktree mode does its work on its own branch
(`automaton/<id>`). While it runs, the base branch (usually `main`) keeps
moving. A long-running Automaton can fall hundreds of commits behind, and the
merge at the end then meets conflicts that are far harder to settle than they
would have been a few commits at a time.

**Branch refresh** merges the base branch into the Automaton's branch by
itself, **between tasks**, so the branch never falls far behind. It is **off by
default**.

## When it runs

The check happens in the gap after one task finishes and before the next one
starts. The daemon merges the base in only when all of these hold:

1. Refresh is on for this Automaton (see [the settings](#the-settings)).
2. Refreshing has not been **stopped** for this Automaton (a conflict stops it;
   see [a conflict](#when-the-merge-conflicts)).
3. The Automaton has a project directory (its worktree).
4. No task is running in the Automaton's own worktree. A task that runs in its
   own separate worktree does not block the refresh.
5. The branch is **at or over the behind limit**: it has at least that many
   commits that the base has and the branch does not.

If any of these fails, nothing happens and the run goes on. A refresh never
fails the Automaton: a git error is logged, recorded with result `error`, and
the next task starts as usual.

The base branch is the Automaton's own git base branch when it has one, else
the default branch of `autonomous.worktree_base_repo` when that is set, else
the remote's default branch, else `main`.

## The settings

| Setting | Default | Meaning |
|---|---|---|
| `autonomous.branch_refresh_enabled` | `false` | Turns the refresh on for every Automaton that has no override of its own |
| `autonomous.branch_refresh_behind_limit` | `5` (0 or unset reads as 5) | Commits behind the base a branch may be before it is refreshed |

Set them with `datawatch config set autonomous.branch_refresh_enabled true`,
`PUT /api/config`, the comm channel's `configure`, or MCP `config_set`. Both
are also listed in `docs/config-reference.yaml`.

Each Automaton also has an **override** that wins over the daemon setting:
`true` refreshes it even when the daemon setting is off, `false` never
refreshes it, and cleared (the default) follows the daemon setting. The
override is stored on the Automaton as `branch_refresh_override`.

Set it from any surface:

| Surface | How |
|---|---|
| Web app | Automaton page, Overview tab, **Branch refresh** card: the *Refresh for this Automaton* select (*Use the daemon setting*, *Always refresh*, *Never refresh*) |
| CLI | `datawatch autonomous branch-refresh <id> enabled\|disabled\|inherit` |
| Chat | `autonomous branch-refresh <id> enabled\|disabled\|inherit` |
| MCP | `autonomous_prd_set_branch_refresh` (`id`, `enabled` true or false, or `clear=true` to remove the override) |
| REST | `POST /api/autonomous/prds/{id}/branch-refresh` with `{"enabled": true}`, `{"enabled": false}` or `{"enabled": null}` (null clears the override); the reply is the Automaton record with its refresh state |

The two daemon settings are in the web app's Settings, Autonomous list
(*Refresh Automaton branches from main between tasks* and *Branch refresh
commits-behind limit*), or set with `datawatch config set`, chat `configure`
or MCP `config_set`.

## What you see

Each refresh that merges, hits a conflict or fails writes a **branch_refresh**
entry to the Automaton's Decisions list. A merge that went through reads:

> branch refresh: merged main commit `<commit>`, result clean (N commits behind)

The result is one of `clean`, `conflict` or `error`. The Automaton also keeps
the state of its latest refresh (when, how many commits behind, the base commit
merged, the result, and whether refreshing is stopped) as `branch_refresh` in
its record.

The Automaton `GET` reply (`GET /api/autonomous/prds/{id}`, full detail) always
carries `branch_refresh` (null when no refresh has run),
`branch_refresh_override` (true, false or null) and `branch_refresh_enabled`,
the setting in effect for that Automaton.

The Automaton page's Overview tab shows the same state on the **Branch
refresh** card: the last refresh time, the commits behind main, the last result
(*Clean*, *Conflict*, *Skipped*, *Error* or *No refresh yet*) and the override
select.

## When the merge conflicts

A conflict is **never resolved for you**. The daemon leaves the branch exactly
as it was (the merge is not kept), records result `conflict`, **stops refreshing
this Automaton**, and raises one question for you, asked by `branch-refresh`,
listing the conflicting files. The same files raise only one open question at a
time. Find it with `datawatch question list` or in the PWA's questions.

| Option | What it does to refreshing |
|---|---|
| **resolve by hand** | Stays stopped until you reset it |
| **retry later** | Allowed again; the next gap tries the merge again |
| **leave the branch alone** (the default) | The branch is untouched and refreshing stays stopped |

> **Review needed.** Answering the question does not change the Automaton yet.
> The code that applies the chosen option exists but is not connected to the
> question answer, so refreshing stays stopped whichever option you pick.
> Until that is connected, the only way to refresh a stopped Automaton again is
> to bring its branch up to date yourself.

If the daemon has no question store when a conflict happens, it logs the
conflicting files and still stops refreshing.

## Related

- [Automaton uncommitted-work check](automaton-uncommitted-work.md), the other
  between-task guard on the worktree.
- [Automaton worktree cleanup](automaton-worktree-cleanup.md)
- [Merge pipeline](merge-pipeline.md), which takes the finished branch to shipped.
