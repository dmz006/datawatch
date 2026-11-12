---
docs:
  index: true
  topics: [autonomous, automation, automaton, cleanup, worktree, branch, temp files, disk space, disk alert, report mode, retention, unlanded, keep, archive, permission denied, read-only files]
exec_steps:
  - tool: autonomous_cleanup_status
    description: Read what both cleanup passes did last, the two disk readings and the disk alert
    args: {}
    read_only: true
  - tool: autonomous_safe_to_delete
    description: List the Automaton work branches and worktrees that are safe to remove right now, and the ones that are held back (read-only)
    args: {}
    read_only: true
  - tool: get_config
    description: Read the daemon settings, including the autonomous.automation cleanup keys
    args: {}
    read_only: true
  - tool: config_set
    description: Set the cleanup mode (off, report or on); it takes effect on the next poll
    args:
      key: autonomous.automation.cleanup_mode
      value: "{{params.mode}}"
    read_only: false
  - tool: config_set
    description: Set how many hours a finished Automaton's work is kept before it can be removed
    args:
      key: autonomous.automation.cleanup_retention_hours
      value: "{{params.hours}}"
    read_only: false
  - tool: config_set
    description: Set the most entries one cleanup pass may remove
    args:
      key: autonomous.automation.cleanup_max_removals_per_pass
      value: "{{params.limit}}"
    read_only: false
  - tool: config_set
    description: Set the disk usage percent at or above which the disk alert is raised
    args:
      key: autonomous.automation.disk_alert_percent
      value: "{{params.percent}}"
    read_only: false
---

# Automaton worktree cleanup

An Automaton that runs in worktree mode does its work on its own branch
(`automaton/<id>`), checked out in its own worktree under the project's
worktree base repository. Its worker sessions also leave scratch files in the
daemon's temporary directory. Nothing about finishing an Automaton used to
remove any of that, so a busy daemon slowly fills its disk.

The **cleanup** removes what a finished Automaton no longer needs, and only
what it is certain it can lose nothing by removing. It also watches the disks
involved and raises one alert when either one is filling up. It works for any
project you run Automata on: the repository it looks at is the worktree base
repository you already configured, and the branch it checks your work against
is that project's own default branch, whatever it is called.

## What it removes

There are two passes. Each has its own mode, counts and last-run record.

| Pass | What it removes | When |
|---|---|---|
| **Worktrees** | A finished Automaton's work branch and its worktree | When the Automaton completes or is cancelled, and again on a periodic sweep for anything that was held back or missed |
| **Temp files** | The scratch files and directories the daemon's own worker sessions created in the temporary directory | When the session ends, and again on a periodic sweep for anything that refused to go the first time |

The first time something is removed is the **owner** path: the Automaton or
session cleans up after itself the moment it ends. The periodic sweep is the
**safety net**. The status shows the two apart (see *Owner and net* below) so
you can see whether the net is finding real work. A healthy daemon mostly shows
owner removals and a net that finds little or nothing.

### What is never touched

The worktrees pass removes an entry only if **every** one of these holds. If any
one does not, the entry is left exactly as it is and the reason is recorded.

- The branch belongs to an Automaton that is **completed or cancelled**, as the
  store says right now. A running, paused, blocked or restarted Automaton is
  never touched.
- **No live session** is working in the worktree or bound to the Automaton, and
  no live lease holds the worktree.
- The worktree has **no uncommitted changes**. This is checked again at the
  moment of removal, not trusted from an earlier look.
- Every commit on the branch is **already on the project's default branch**.
  This is also checked again at removal time. A branch with work that exists
  nowhere else is never removed. If the default branch cannot be worked out, the
  entry is left alone.
- The branch is **at least as old as the retention window** (below), so work you
  may still want to look at stays reachable for a while. An entry whose age
  cannot be determined counts as young. This applies to the periodic sweep. The
  moment an Automaton completes or is cancelled, the owner releases its own work
  without waiting, because the age is measured from that very moment; the other
  conditions in this list still all have to hold, and in particular a branch with
  any commit not yet on the default branch is never released.
- It is not the **main checkout** of the repository, and it is not the default
  branch itself.
- Only branches in the `automaton/` namespace are ever considered. Branches you
  made yourself are not.

Removal itself is the cautious form of the git commands: `git worktree remove`
without `--force`, then `git branch -d` (never `-D`). If git refuses either one,
the entry stays and the refusal is recorded with its error; the next pass tries
again. Every removal is written to the Automaton's triage record and the audit
log, so you can reconstruct what was removed, when and why.

**Which branch counts as the default.** The cleanup uses the Automaton's own
base branch when one was set for it. Otherwise it asks git for the default
branch the remote records for the repository (the target of `origin/HEAD`, or
the same for another remote if there is no `origin`). It never assumes a name:
a project whose default branch is `master`, `trunk` or anything else is judged
against that branch.

**The temp sweep** is stricter still. It looks only inside the daemon's
temporary directory, only at names carrying the daemon's own `datawatch-` prefix,
and removes a path only if the daemon **recorded creating that exact path for a
session that is no longer alive and ended more than the retention window ago**.
A prefixed name the daemon has no record of is skipped as foreign. A session
whose state is unknown counts as alive. A symbolic link is never followed and
never removed. Nothing outside the temporary directory is touched. Because the
record of what the daemon created is kept in memory, scratch left behind by an
earlier daemon process (one that crashed, or ran before a restart) is not
attributed and is left alone; remove those by hand if you want them gone.

## The three modes

The mode is one key: `autonomous.automation.cleanup_mode`.

| Mode | What the passes do |
|---|---|
| `off` | Nothing. No git command is run for the cleanup and no temporary directory is scanned. The disk check still runs, so the disk alert still works. |
| `report` | The default. Every safety check runs and the passes record what they **would** remove, but nothing is removed. |
| `on` | The passes remove what passes every check, at most the per-pass limit each time. |

An unset or unrecognized value reads as `report`. A write that is not exactly
`off`, `report` or `on` is refused on every surface and nothing is saved. The
mode is read live, so a change applies on the next poll without a restart.

## The other settings

| Key | Meaning | Default | Allowed |
|---|---|---|---|
| `autonomous.automation.cleanup_retention_hours` | How long a finished Automaton's branch, or a finished session's scratch, must have sat before it can be removed | 24 | whole number, at least 1 |
| `autonomous.automation.cleanup_max_removals_per_pass` | The most entries one pass removes. In `report` mode, what is over the limit is counted as would-remove | 10 | whole number, at least 1 |
| `autonomous.automation.disk_alert_percent` | Disk usage percent at or above which the disk alert is raised | 85 | whole number, 1 to 100 |

A value that is not allowed is refused with an error and nothing is written.
Entries past the per-pass limit are simply left for the next pass.

## Base requirements

- A running daemon with the Automata engine enabled.
- For the worktrees pass, a worktree base repository set in the daemon
  configuration (`autonomous.worktree_base_repo`). Without one the worktrees
  pass has nothing to look at and logs that it cannot release. The temp sweep
  and the disk check on the temporary directory do not need it.
- `git` on the daemon's machine.

## Seeing what it would remove

Leave the mode on `report` (the default) and read the status. In `report` mode
the counts show what a pass found it could remove.

1. Read the status (any surface below). For each pass look at **would remove**
   (what the last run would have removed), **skipped** (what it left, and the
   safety check that held it) and **failed**.
2. For the worktrees pass, read the **safe-to-delete report**. It lists, branch
   by branch, which `automaton/*` branches and worktrees have everything on the
   default branch and no uncommitted work (**safe**), and which are held back and
   why (**held**). It is a read: it never deletes anything. The cleanup applies
   the retention window and the per-pass limit on top of that list, so it may
   remove fewer than the report shows as safe.
   The read is not slow each time: the daemon keeps the report in memory for
   60 seconds per base repository. The first read does the git work; every read
   within that lifetime gets the stored result at once. The card and the text
   views show when the result was computed, with a stale badge when a read
   misses the lifetime — it still gets the result at once, and at most one
   background refresh (bounded at 60 seconds) is started — further reads in
   that window start none and also get the stored result. While that refresh
   is still running, reads keep serving the last successful result, so a slow
   or failing refresh never leaves the card blank or blocked, and a failed
   refresh is counted in the daemon stats.
3. The daemon log carries one summary line per pass
   (`worktree-cleanup` and `temp-sweep`) with the mode and the counts.

### Moving from report to on

1. Run in `report` for long enough to see a few finished Automata go through.
   Check that **would remove** names only things you are happy to lose and that
   the held ones are the ones you expect to be held.
2. Check the retention window and the per-pass limit suit you. A small limit
   (the default is 10) makes the first `on` passes gentle.
3. Switch the mode to `on`. The next passes remove; the status now shows
   **removed** and **bytes freed**, and the owner-versus-net split starts to
   fill in.
4. If you want it to stop, set the mode back to `report` or `off`. Nothing it
   has already removed comes back, which is why the checks above come first.

The commands to change a setting, each shown for the mode and the same on every
key:

| Surface | How |
|---|---|
| REST | `PUT /api/config` with `{"autonomous.automation.cleanup_mode": "on"}`, or `PUT /api/autonomous/config` with the `automation` object |
| MCP | `config_set` with `key: autonomous.automation.cleanup_mode` and `value: on` |
| CLI | `datawatch config set autonomous.automation.cleanup_mode on` |
| Comm channel | `configure autonomous.automation.cleanup_mode=on` |
| PWA | Settings → General → the Automata settings: **Automaton cleanup**, **Automaton cleanup retention**, **Automaton cleanup limit** and **Automata disk alert** |

`GET /api/config` shows the keys under `autonomous.automation`. A key that was
never set is not shown; it is running on its default.

## The disk alert

Alongside the two passes the daemon measures how full two disks are: the one
holding the worktree base repository, and the one holding the daemon's
temporary directory. When either is at or above `disk_alert_percent`, it raises
one warning alert that names the disk and its usage and says that removing the
Automaton worktrees and session scratch that live there would free space.

- The alert is **rate limited**: while a disk stays above the threshold it
  fires at most once an hour, not once per poll. The time it last fired is kept
  across daemon restarts, so a restart does not repeat an alert.
- The disk check runs in **every** mode, `off` included. The alert only tells
  you to free space. It never removes anything by itself.
- If a disk cannot be measured, the status says there is no reading for it and
  it cannot raise an alert; it is never shown as 0%.
- The worktree disk reading is absent while no worktree base repository is set.

## Reading the status

Every surface reads the same status and none of them changes anything.

| Surface | How to read it |
|---|---|
| REST | `GET /api/autonomous/cleanup` (only GET; 503 while Automata are disabled; a federated peer needs the autonomous read capability) |
| MCP | `autonomous_cleanup_status` (read-only) |
| CLI | `datawatch autonomous cleanup`; add `--json` for the raw body |
| Comm channel | `autonomous cleanup` (the reply adds the alert text while the alert is active) |
| PWA | Automata tab → **Events** sub-tab → the **Automata cleanup status** card under the stream, below the safe-to-delete card, refreshed every 30 seconds; its worktrees pass lists the skipped worktrees by reason |
| Mobile and wear | The same REST contract, described in `docs/api/openapi.yaml` |

The status holds, for each pass (`worktrees` and `temp`):

| Field | Meaning |
|---|---|
| `mode` | The mode that pass last ran with (empty until it has run) |
| `last_run` | When it last ran; absent until the first run |
| `removed` | What the last run removed |
| `would_remove` | What the last run would have removed: all of it in `report` mode, or what went over the per-pass limit in `on` mode |
| `skipped` | What the last run left in place because a safety check held it |
| `failed` | Removals that hit an error and were left in place |
| `bytes_freed` | What the last run measured as freed |
| `last_error` | The last removal error, absent when the last run failed nothing |

and, for the whole status:

| Field | Meaning |
|---|---|
| `worktree_disk`, `temp_disk` | Path and usage percent of each guarded disk, and whether it is over the threshold |
| `disk_alert_percent` | The threshold in force |
| `disk_alert` | True while either disk is at or above the threshold, as of the last check |
| `last_disk_alert_at` | When the alert last actually fired; absent until it has |

Keys with no value are **absent**, not `null`.

### Owner and net

Each pass also keeps a running split: `owner_removed` and `owner_bytes_freed`
count what the Automaton or session released itself when it ended;
`net_removed` and `net_bytes_freed` count what the periodic sweep had to catch.
The aim is for the net to stay near zero. A net that keeps finding things means
something is not being released at the end, and the `last_error` line and the
daemon log are where to look.

## Walkthrough

1. Read the status:

   ```
   $ datawatch autonomous cleanup
   Automaton cleanup (disk alert threshold: 85%):
     worktrees pass: not run yet
     temp pass: not run yet
     worktree disk: /srv/projects/app at 42%
     temp dir disk: /tmp at 31%
     disk alert: not active
   This is a status read only: the cleanup passes do the removing, not this read.
   ```

2. After a few Automata have finished, read it again. In `report` mode:

   ```
     worktrees pass (mode report): last run 2031-03-02T10:14:00Z, removed 0, would-remove 3, skipped 1, failed 0, freed 0 bytes
       owner vs net: 0/0 removed, 0/0 bytes freed
   ```

   Three finished branches could be removed; one was held back.
3. Read the safe-to-delete report to see which ones:
   `datawatch autonomous safe-to-delete`.
4. Happy with that list: `datawatch config set autonomous.automation.cleanup_mode on`.
5. After the next passes the same read shows `removed` and `freed` filled in.

## What it never does

It never removes a branch with commits that are not on the default branch,
never removes a worktree with uncommitted changes, never touches a running or
unfinished Automaton, never uses `--force` or `-D`, never removes anything
outside the `automaton/` branch namespace or the daemon's own scratch files,
and never follows a symbolic link. In `off` it does not even run git for the
cleanup. The disk alert never removes anything.

## Skipped by reason, permission errors and the fix

The worktrees pass also groups what it left in place by the reason, so the
status can say *why* entries are held instead of only counting them. The status
carries `skipped_by_reason` on the `worktrees` pass: a map from reason to count
(absent when nothing was skipped). A removal the file system refused for
permission is counted under `permission_denied`.

**Permission errors.** A build or test sometimes leaves read-only files in a
worktree (a read-only module cache, for example), and git then cannot delete the
folder. The status turns every such failure of the last worktrees pass into one
record, `permission_issue`, instead of a list of raw errors:

| Field | Meaning |
|---|---|
| `count` | How many removals were refused for permission |
| `paths` | The affected worktree paths, sorted, without repeats |
| `why` | One plain line saying why this happens |
| `fixable` | True when the safe fix can run: at least one affected folder still exists |
| `hidden` | True when every affected folder is already gone. Nothing needs to be done, so the Housekeeping list does not show it |

**The fix.** The safe fix restores the owner write permission (and, on folders,
owner read and search) on each affected path and everything under it. It
touches only paths inside the worktree base, refuses a path outside it or one
that resolves outside it through a link, never follows or changes a symbolic
link, and skips a path that is already gone. The containment check runs on the
requested path **before any file-system call**, so a path outside the base (an
absolute path elsewhere, a `..` escape, an empty path or one with a NUL byte) is
refused with the same "outside the worktree base" text whether or not the target
exists, and a missing outside path no longer reads as "already gone". The check
is written out in the fix itself, as a plain comparison of the cleaned path
against the base (and, for an absolute path, against the base's resolved
location), so no step before it can touch the file system. A path equal to the
base itself is refused too: the fix works on things inside the base, never on
the base. A valid path inside the base behaves exactly as before. It reports how many entries it
changed. It changes permissions only: it removes nothing, so the next cleanup
pass still applies every check above before it deletes anything.

**Run the fix.** On the **Events** sub-tab (Automata tab → Automation
view), inside the **Cleanup** card, a permission problem that can be fixed shows its plain reason and a **Fix
permissions** button. The button posts the affected paths to
`POST /api/autonomous/cleanup/fix-permissions`. A problem that is `hidden` is not
shown. The PWA button and that REST route are the surfaces; there is no CLI verb,
MCP tool or comm verb for the fix.

## Unlanded branches (finished work that never reached the default branch)

The cleanup never removes a branch whose commits are not on the default branch,
so a finished Automaton whose work was never merged keeps its branch forever.
The **unlanded work** list is the review of exactly those branches. It looks at
the archived Automata whose `automaton/<id>` branch has commits not on the
default branch, and recommends one of three things for each. It is strictly
read-only, and it compares by commit content (so a merge that rewrote commits,
by rebase or renumbering, still counts as landed). Running Automata are left out
because their commits are expected to be ahead.

| Recommendation | Meaning |
|---|---|
| `landed` | Every commit is already on the default branch by content: the branch is a leftover |
| `superseded` | Some commits are not on the default branch, but every file they change was changed on the default branch since the branch forked, so later work replaced them |
| `needs_decision` | Commits are not on the default branch and nothing shows they were replaced: you decide |

Each entry shows the Automaton, its title, the branch, the number of commits not
landed, the last activity, the recommendation and a one-line reason. The list is
ordered least settled first (needs a decision, superseded, landed), then newest
activity first. Branches with nothing ahead, branches that no longer exist and
Automata that are not archived are not listed.

### The actions on an unlanded branch

| Action | What it does | Destructive |
|---|---|---|
| **Keep** | Records that you want the branch left alone, with an optional note, in `unlanded-keep.json` in the daemon's state directory. Keeping twice updates the note. The cleaner and the unlanded list skip a kept branch | No |
| **Archive branch** | Moves the branch under `refs/archive/<branch>` and removes the branch name. The commits stay reachable from the archive ref | No (reversible: recreate the branch from the ref) |
| **Delete worktree** | Writes a git bundle backup of the branch under the `backups` folder of the worktree base, then removes the worktree and the branch | Yes |

Delete is the only destructive action and it is guarded: it refuses unless the
entry carries a recommendation from the list above, a base repository and a
worktree base are known, and the caller passed an explicit confirm. The bundle
is written and verified **first**; if it cannot be, nothing is removed. A delete
returns the branch, the bundle path and the worktree path it removed.

### Where to see and act

| Surface | How |
|---|---|
| PWA | Automata tab → Automation view → **Housekeeping** sub-tab → the **Unlanded work** card, refreshed every 30 seconds. It is hidden until the list has a row. Each row shows the title, commits not landed, last activity and recommendation, with **Keep**, **Archive branch** and **Delete worktree** buttons (delete asks you to confirm) |
| REST | `GET /api/autonomous/unlanded-work`; `POST /api/autonomous/unlanded-work/keep`, `/archive-branch` and `/delete-worktree` (the last needs `confirm: true`) |
| CLI | `datawatch autonomous unlanded-work [--json]`; subcommands `keep <branch> [--note ...]`, `archive-branch <branch>` and `delete-worktree <branch> --confirm` |
| Comm channel | `autonomous unlanded-work`; `autonomous unlanded-work keep\|archive-branch\|delete-worktree <branch>` (delete needs the word `confirm`) |
| MCP | `autonomous_unlanded_work`, read-only. The three actions are not MCP tools |
| Mobile and wear | The same REST contract, described in `docs/api/openapi.yaml` |

The branch audit script (`scripts/branch-audit.py`) reports the same finding from
the command line: an `UNLANDED` row is an archived Automaton branch (cancelled,
blocked, failed or no record) with work not on main, and the script exits with
status 1 while any exist. Decide each one with Keep, Archive branch or Delete
worktree, or merge it.

**Review before relying on this.** Whether a kept branch also suppresses the
safe-to-delete report is still undecided; the operator must confirm that. The
datawatch-app (Android and iPhone) parity for the Unlanded work card is tracked
in its own repository and is not part of this change.

## Processes the worker left running

The cleanup above removes branches and folders. Separately, when a worker session ends (including cancel and failure), the daemon ends processes that worker started and left running. It matches them by `/proc` evidence only (a working directory, executable or script in the session worktree or scratch folder, ancestry to the session, or the session id in the environment), never by name. Read the result as a `session_end_reap` decision on the task or an audit entry of the same name (the decision exists only when the session is bound to a task). If the audit entry shows `survivors` above zero, those processes ignored the signal and are counted only, with no pid or command recorded: look for them by hand, even when the ended count is zero. Details and review notes: [operations](../operations.md#processes-a-session-leaves-behind).
