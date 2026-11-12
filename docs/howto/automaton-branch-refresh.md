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
fails the Automaton. If the merge itself fails (not a conflict), the error is
logged, recorded with result `error`, and the next task starts as usual. If git
cannot even count how far behind the branch is (for example the base branch
does not exist in the worktree), that is logged only: nothing is recorded, and
the check tries again after the next task.

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

The result is one of `clean`, `conflict` or `error`; a conflict or an error
entry has the same wording with that result in place of `clean`, and for those
the commit named is the base's latest commit, not one that was merged. The
Automaton also keeps the state of its latest refresh (when, how many commits
behind, the base commit, the result, and whether refreshing is stopped) as
`branch_refresh` in its record.

The Automaton `GET` reply (`GET /api/autonomous/prds/{id}`, full detail) always
carries `branch_refresh` (null when no refresh has run),
`branch_refresh_override` (true, false or null) and `branch_refresh_enabled`,
the setting in effect for that Automaton.

The Automaton page's Overview tab shows the same state on the **Branch
refresh** card: the last refresh time, the commits behind main, the last result
(*Clean*, *Conflict*, *Skipped*, *Error* or *No refresh yet*), whether refreshing
is *Active* or *Stopped by a conflict*, and the override select. The daemon
does not record a *Skipped* result today, so you will not see it unless a later
release starts to. The stopped state is also `branch_refresh.stopped` on the
Automaton `GET` reply.

## When the merge conflicts

The daemon merges the base in with a no-fast-forward merge. There is one
exception it settles itself: when the **only** conflicted file is `CHANGELOG.md`,
it takes the base branch's copy of that file and commits, and the result is
`clean`.

Any other conflict is **never resolved for you**. The daemon backs the merge
out so the branch is left exactly as it was, records result `conflict`,
**stops refreshing this Automaton**, and raises one question for you, asked by
`branch-refresh`, listing the conflicting files. The same files raise only one
open question at a time. Find it with `datawatch question list` or in the PWA's
questions.

| Option | What it does to refreshing |
|---|---|
| **resolve by hand** | Refreshing stays stopped until you reset it (see below) |
| **retry later** | Refreshing is allowed again, so the next gap tries the merge again |
| **leave the branch alone** (the default) | The branch is untouched and refreshing stays stopped |

Your answer is applied to the Automaton at once and written to its Decisions
list as a **branch_refresh** entry that names the question. An answer that
names none of the three options (text only) changes nothing: refreshing stays
stopped. If you change your mind, answer the question again with the new
option; the latest answer wins.

**Resetting a stopped Automaton.** After you have merged the base into the
branch by hand, set the Automaton's branch refresh override again, with any
value, from the Branch refresh card, `datawatch autonomous branch-refresh <id>
enabled|disabled|inherit`, chat `autonomous branch-refresh`, MCP
`autonomous_prd_set_branch_refresh` or the REST route. Setting it clears the
stop and adds a **branch_refresh** entry to the Decisions list; to keep the
Automaton's current behaviour, set it to the value it already has (`inherit`
when it has no override). The next gap then checks how far behind the branch is
as usual.

If the daemon has no question store when a conflict happens, it logs the
conflicting files and still stops refreshing.

## Watching refreshes across Automata

`datawatch autonomous branch-refresh-stats` (chat `autonomous
branch-refresh-stats`, MCP `autonomous_branch_refresh_stats`, REST
`GET /api/autonomous/branch-refresh/stats`) shows the daemon setting and limit,
how many Automata have a recorded refresh and how each ended (`clean`,
`conflict`, `skipped`, `error`), how many are **stopped** by a conflict, and how
many carry an override. A stopped count above zero means an Automaton is waiting
for your answer or your reset. The same numbers are in the live stats broadcast
(`branch_refresh_clean`, `branch_refresh_conflict`, `branch_refresh_error`,
`branch_refresh_stopped`) and the Prometheus gauges
`datawatch_branch_refresh_automata{result}` and
`datawatch_branch_refresh_stopped`.

## Related

- [Automaton uncommitted-work check](automaton-uncommitted-work.md), the other
  between-task guard on the worktree.
- [Automaton worktree cleanup](automaton-worktree-cleanup.md)
- [Merge pipeline](merge-pipeline.md), which takes the finished branch to shipped.
