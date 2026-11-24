---
docs:
  index: true
  topics: [autonomous, automation, automaton, cleanup, worktree, branch, temp files, disk space, disk alert, report mode, retention, unlanded, keep, archive, permission denied, read-only files, pending branch alert, unlanded alert threshold, unlanded counts, age band, landing state, branch reaper, branch-reap, branch-reap-run, branch-reap-restore, task sub-branch, backup ref, restore reaped branch, reaped_branch_backup_days, cache folder sweep, go-build, trivy scan-cache, liveness probe, biggest temp folders, live daemon housekeeping routes, autonomous disabled, federation capability]
exec_steps:
  - tool: autonomous_cleanup_status
    description: Read what the cleanup passes did last, the two disk readings, the disk alert and the biggest temp folders while the temp disk is over threshold
    args: {}
    read_only: true
  - tool: autonomous_safe_to_delete
    description: List the Automaton work branches and worktrees that are safe to remove right now, and the ones that are held back (read-only)
    args: {}
    read_only: true
  - tool: autonomous_branch_reap
    description: Read the branch reaper's report — the task sub-branches a pass would reap, the ones held, and every backup ref (a report; it deletes nothing)
    args: {}
    read_only: true
  - tool: autonomous_branch_reap_run
    description: Perform one branch reap pass — each listed branch is backed up first, then deleted
    args: {}
    read_only: false
  - tool: autonomous_branch_reap_restore
    description: Recreate one reaped task sub-branch from its newest backup ref
    args:
      branch: "{{params.branch}}"
    read_only: false
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
  - tool: config_set
    description: Set the whole days a reaped branch's backup ref is kept before it is pruned (0 or unset = 30)
    args:
      key: autonomous.reaped_branch_backup_days
      value: "{{params.days}}"
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

Three safety nets. Two of them — the worktrees pass and the temp sweep — each have their own mode, counts and last-run record. The third, the **cache folders sweep**, removes stale `go-build` and Trivy scan-cache folders directly in your temp directory; it has no row of its own in the status and you read it in the log and audit stream (**The cache folders sweep** below).

| Pass | What it removes | When |
|---|---|---|
| **Worktrees** | A finished Automaton's work branch and its worktree | When the Automaton completes or is cancelled, and again on a periodic sweep for anything that was held back or missed |
| **Temp files** | The scratch files and directories the daemon's own worker sessions created in the temporary directory | When the session ends, and again on a periodic sweep for anything that refused to go the first time |
| **Cache folders** | Stale Go build cache (`go-build`) and Trivy scan-cache (`trivy-scan-cache*`) folders directly under your temp directory, at least 60 minutes old, that no live process holds | Not at all on the owner path: once at daemon start, and again on a periodic sweep beside the temp sweep — the sweep is their only cleanup |

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
- The worktree has **no uncommitted changes the cleanup cannot explain**,
  re-checked at the moment of removal, not trusted from an earlier look. The
  one kind of uncommitted content the cleanup does understand: harness files
  its own workers leave behind — the per-session MCP discovery file, the
  injected skills folder and launcher files, and the backends' own settings
  and caches (`.mcp.json`, `.aider.conf.yml`, `.aider.chat.history.md`,
  `.aider.tags.cache.*`, `.goose/`, `.gemini/`, `.opencode/`,
  `opencode.json`). When those are the only untracked files in the work tree,
  the entry is treated as clean and the pass may remove it; the files the
  cleanup resolved are listed on the removal (as `resolved_files`) so a later
  "what did that free?" read is answerable. A changed tracked file, or an
  untracked file the cleanup cannot name as harness, is never treated as
  generated content and holds the pass — a project that tracks any of those
  names in its working tree keeps the full protection, and an untracked file
  the cleanup does not recognize is your work, so it stays.
- The daemon's **own generated documents** do not count either: the gate report
  under `docs/plans/reports/`, the parity issue draft under
  `docs/plans/parity-issues/`, the drafts under `docs/plans/drafts/`, and the
  hash file kept beside each. The name alone is never enough. The document must
  be untracked, still carry the generated marker on its first line, carry no
  edited line and still match its recorded hash. A file you wrote in one of
  those folders, a generated document you changed, a hash file without its
  document and a link all hold the work tree. The cleanup checks each document
  again just before it deletes it, then removes the tree without force; the
  deleted files are listed on the removal in `resolved_files`. The
  safe-to-delete report applies the same rule, so these documents no longer
  count under `held_dirty`. If a work tree also holds harness files that git
  does not ignore, the safe-to-delete report still holds it.
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

Removal itself is the cautious form of the git commands, with one narrow
exception. `git worktree remove` runs **strict** — without `--force` — so that
git itself confirms the work tree holds no untracked files before it goes
away. The one case a strict removal cannot work — a work tree whose only
untracked files are the harness files above — removes with `--force`, and the
files it resolved are recorded on the removal and in the audit entry. The
branch side is unchanged: `git branch -d`, never `-D`. If git refuses either
one, the entry stays and the refusal is recorded with its error; the next
pass tries again. Every removal is written to the Automaton's triage record
and the audit log, so you can reconstruct what was removed, when and why.

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
whose state is unknown counts as alive. A path the file system refuses —
unreadable while the sweep is walking, or refused while it is removing — is
left in place and counted as **skipped for reason `unreadable`**, not as a
failure; any other error still lands in the pass's errors. A symbolic link is
never followed and never removed. Nothing outside the temporary directory is touched. Because the
record of what the daemon created is kept in memory, scratch left behind by an
earlier daemon process (one that crashed, or ran before a restart) is not
attributed and is left alone; remove those by hand if you want them gone.

**The cache folders sweep.** A build or scan runs under the Automaton's
worktree, but its cache folders land in **your temp directory**, not in the
Automaton's directory or worktree: Go's build cache folders (`go-build`) and
Trivy's scan-cache folders (`trivy-scan-cache*`). The temp sweep does not cover
them — the daemon never recorded creating them, and they do not carry the
`datawatch-` prefix — and nothing on the worktree side ever does. Left alone,
they outlive every Automaton that made them and the temp directory grows until
builds and tests start failing. The sweep is the only thing that removes them.

It runs once when the daemon starts, before the first poll, and on every
coordinator poll beside the temp sweep, in every mode. It looks only at entries
directly under the temp directory (a symbolic link there is counted as skipped
and never followed; a folder inside a subfolder is outside the pass), and
removes one only if all of these hold:

1. its name is a Go build cache folder (`go-build`, or `go-build` followed by
   digits) or a Trivy scan-cache folder (a `trivy*` name containing
   `scan-cache` or `scan_cache`) — anything else is left and counted as
   skipped;
2. it is a directory, not a file or a symbolic link;
3. it is at least **60 minutes** old by its last modification — a fixed
   retention, not `cleanup_retention_hours` and not a setting you can change;
   an age the sweep cannot read is not removable; and
4. no live process holds it: no open handle into the folder, and no live
   process with its working directory inside it.

Point 4 is the **liveness probe**. On Linux the daemon walks `/proc` for every
live process and checks its open files and working directory against the
folder. On a platform without `/proc` — or when `/proc` cannot be read —
liveness is **unknown**, and unknown is treated as held: the folder is kept.
In practice the sweep is a no-op on hosts without a readable `/proc`, even in
`on` mode; it removes nothing it cannot prove safe.

The mode is the same `autonomous.automation.cleanup_mode` as the other passes:
`off` does nothing, not even a scan; `report` lists the folders that would be
removed and removes nothing; `on` removes. The per-pass removal limit
caps the worktrees and temp passes, not this one. Its result — like the temp
sweep's — always carries the number of folders removed and the bytes of space
freed. Where you see it: one summary line in the daemon log per pass
(`cache-folder-sweep`) with the counts, skip reasons and bytes freed, plus the
audit event `autonomous_cache_folder_sweep`. A removal that errors is left in
place and recorded in the pass result, like the other passes.

**The scan itself is its own housekeeper.** The pre-tag run's Trivy scan (the
`docker-agent-base` step; see the Pre-tag run section of
`docs/operations.md`) does not wait for the sweep at all: before the scan it
creates a **throwaway, isolated cache directory** under the operator's temp
space — `mktemp -d`, never a fixed shared path — and exports it as
`TRIVY_CACHE_DIR`, and traps on `EXIT`, `INT` and `TERM` remove that directory
on **every exit path — a passing scan, a failing scan and an interrupted run
alike** — so the scan neither reuses a warmed shared cache nor leaves a folder
behind in the temp directory. The cache is fresh on each run, so the scan
re-fetches what it needs; that is the trade-off for leaving nothing behind. If
`trivy` is not on PATH the scan is skipped with a message and no cache
directory is created. The sweep above remains the net for the stray `go-build`
and Trivy scan-cache folders it could not remove itself.

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

One threshold next to these is fixed, not a setting: the disk holding the temp
folder always alerts at **80%** or lower — the lower of `disk_alert_percent`
and 80, with 80 as the ceiling (so at the default 85 the effective temp-disk
threshold is 80). The worktree disk keeps whatever you have set. See **The
disk alert**.

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
3. The daemon log carries one summary line per
   pass (`worktree-cleanup`, `temp-sweep` and `cache-folder-sweep`) with the
   mode and the counts.

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

Alongside the passes the daemon measures how full two disks are: the one
holding the worktree base repository, and the one holding the daemon's
temporary directory. Each is checked against its own threshold — the worktree
disk against `disk_alert_percent` (default 85), the temp disk against the
lower of that value and the fixed 80 — and when either is at or above its
threshold the daemon raises one warning alert that names every over-threshold
disk with its usage. When the temp disk is the one over, the alert body also
lists the **biggest folders directly in the temp folder** — the top five —
each with its approximate size and whether a **running process** holds it — its **ownership**, judged by the same `/proc` liveness probe the sweep uses (an open handle into it, or a process working out of it). That list
is what points you at what is eating the space; in practice it is the stale
`go-build` and Trivy scan-cache folders the sweep above was added for.

- The alert is **rate limited**: while a disk stays above its threshold it
  fires at most once an hour, not once per poll. The time it last fired is kept
  across daemon restarts, so a restart does not repeat an alert.
- The disk check runs in **every** mode, `off` included. The alert only names
  what is full and what is biggest on it. It never removes anything — nor does
  its wording promise to: the cleanup paths are the ones that remove, and the
  alert says so.
- If a disk cannot be measured, the status says there is no reading for it and
  it cannot raise an alert; it is never shown as 0%.
- The worktree disk reading is absent while no worktree base repository is set.
- While the temp disk is at or above its threshold, a read of the status
  carries the same folder report — `biggest_temp_folders`: the top five
  folders directly in the temp folder, each with its path, approximate size
  and its liveness (`held`, `liveness_known`). It is measured on the check
  that found the disk over, is absent until the first such check, and clears
  again once the disk drops back below its threshold.
- Each disk reading also carries its own `threshold`, so a reader knows which
  threshold that reading was checked against even though the two thresholds
can differ.

## The no-space gate reason (temporary folder full)

The same full-temp condition speaks in one place outside the cleanup: the
Automaton's final gate, which the daemon runs as the **script gate**. When a
gate check exits non-zero and its captured output says the storage it wrote to
ran out — `no space left on device`, `ENOSPC` or `disk quota exceeded`, in any
casing — the task's failure reason reads **temporary folder full**, not the
exit code. That exact text is what the daemon's infra-retry classifier matches,
and it maps it to the `storage_write` class (the device is full, the quota is
used up, or the file system is read-only): the task takes the infra-retry path
— a back-off schedule, recorded on the task and as an `infra_retry` decision on
the Automaton — instead of being counted against it as a failed test. A no-space
failure at the gate is treated as an **infrastructure storage shortage**, not a
content or test failure, and the retry is not counted against the task.

The gate's other bad endings keep their own reasons — a gate killed by a
signal, a gate that never ran, a gate that hit its deadline — so only a
real no-space exit reads this way. When a daemon restart leaves a finished
gate result to consume, the same reason comes out of the stored result, so the
classification does not change across the restart. The reason itself removes
nothing; it only tells you which side of the failure line the machine is on.

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
| `worktree_disk`, `temp_disk` | Path and usage percent of each guarded disk, whether it is over its own threshold (`alert`), and the `threshold` that reading was checked against |
| `disk_alert_percent` | The threshold you set, in force |
| `disk_alert` | True while either disk is at or above its own threshold, as of the last check |
| `last_disk_alert_at` | When the alert last actually fired; absent until it has |
| `last_worktree_skips` | The worktrees pass's latest held entries (each `branch`, `worktree`, `reason`, `detail`, and where present the held `files` and the `release_action` that frees the hold); absent when nothing was held |
| `biggest_temp_folders` | While the temp disk is at or above its threshold: the top five folders directly in the temp folder, each as `path`, `size`, `held`, `liveness_known`. Absent until the first such reading, and cleared once the disk drops back below its threshold |

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
     worktree disk: /srv/projects/app at 42% (threshold 85%)
     temp dir disk: /tmp at 31% (threshold 80%)
     disk alert: not active
   This is a status read only: the cleanup passes do the removing, not this read.
   ```

   Note the two different thresholds on the two disk lines: the worktree disk
   is checked at the 85 you set, the temp dir disk at the fixed 80.

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
never removes a worktree still holding a changed tracked file or an untracked
file it cannot name as harness, never touches a running or unfinished
Automaton, removes a work tree with `--force` only in that harness-file case
and never deletes a branch with `-D`, never removes anything outside the
`automaton/` branch namespace or the daemon's own scratch files, and never
follows a symbolic link. It also does not hide what it leaves: an entry held
only because of uncommitted work is reported with the exact files holding it
and the one action that releases it (below). It never removes a cache folder whose age
it cannot read or whose liveness it cannot prove — on platforms without a
readable `/proc`, that is all of them — and the disk alert never removes
anything, in any mode. The no-space gate reason (temporary folder full) is
only a label on a gate failure plus an infra-retry path; it removes nothing,
either. In `off` it does not even run git for the
cleanup, and it does not scan the cache folders either. The only thing that
still runs in `off` is the disk reading and its alert. Its status record is
guarded the same way: the status file path is resolved once inside the data
directory, a data directory that is not usable leaves the status recorder
inert (it neither loads nor saves anything), and the "still there" probe on a
failed worktree path never stats an empty, NUL-laden or `../`-climbing path.

The "still there" probe is now resolved through a handle opened on the worktree
base. A path that is truly gone is still skipped as "nothing to fix"; a path
that escapes the base (for example through a symbolic link) is refused and
reported as `outside the worktree base` in the status errors instead of being
inspected.

**Still needs your review:** an `outside the worktree base` entry in the
cleanup status means a recorded path pointed outside the base. Nothing was
removed; check how that path was recorded before clearing the entry.

## Skipped by reason, permission errors and the fix

The worktrees pass also groups what it left in place by the reason, so the
status can say *why* entries are held instead of only counting them. The status
carries `skipped_by_reason` on the `worktrees` pass: a map from reason to count
(absent when nothing was skipped). A removal the file system refused for
permission is counted under `permission_denied`.

**A held entry carries its fix.** The pass never reports only that a worktree
was held. When an entry is held because of uncommitted work, the skipped entry
carries `files` — the exact untracked paths the cleanup could not classify as
harness — and `release_action`, the one plain action that frees the hold:
commit the work or discard those files so the work tree is clean, and the
next worktree cleanup pass resolves the entry and the hold lifts. An entry
held under a safety report carries the same pair, with the action named for
the report's state. The status keeps the pass's latest of these as
`last_worktree_skips` (each entry: `branch`, `worktree`, `reason`, `detail`,
and where present `files` and `release_action`), and the read renders each
held entry that carries a release action as a plain line — which worktree it
is, why it is held, the held files and the one action that frees it (entries
without a release action, like a young branch, only appear in `skipped` and
`skipped_by_reason`). So "skipped" tells you what to do next, not only what
was left.

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
MCP tool or comm verb for the fix. This route is one of the seven housekeeping
routes described in **These routes work on the live daemon** below: with
Automata enabled the live daemon serves it, and it is refused only while
Automata are off (**503** `autonomous disabled`) or when a federated peer's
token lacks the autonomous write capability (**403**).

**Still worth your review.** Two things in the skip model change what a quiet
status means, so look at both before trusting "nothing was left":

- **Harness-resolved removals.** A work tree whose only untracked files are
  generated harness files is now removed (with `--force`, those files recorded
  as `resolved_files`). If one of your workers intentionally leaves an
  untracked, harness-named file in a work tree — an assistant chat history, a
  tool config you want to keep — that file now goes away with the work tree.
  Such a file only survives if you land it (commit it) or keep it tracked
  elsewhere. The files each removal resolved are on the removal record (as
  `resolved_files`) and in its audit entry (`resolved_files`), so check the
  audit for the first few `on` passes.
- **Unreadable temp paths are skips, not failures.** A temp path the file
  system refuses to walk or remove is counted under the temp pass's `skipped`
  (reason `unreadable`) and left in place — it is *not* in `failed`, so a pass
  with no errors can still be holding a path the sweep never manages to get
  to. If the temp disk grows while the temp pass reports zero failures, look
  for an `unreadable` path and fix the cause (permissions, a stuck mount);
  the sweep will then remove it on a later pass once it is readable.

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
| `landed` | The work is already on the default branch: every commit matches by content, or the Automaton's shipped record names a commit that is on the default branch, or every file the branch changed has the same content there (a hand port). The branch is a leftover |
| `superseded` | Some commits are not on the default branch, but every file they change was changed on the default branch since the branch forked, so later work replaced them |
| `needs_decision` | Commits are not on the default branch and nothing shows they were replaced: you decide |

A hand port shares no commit with its branch, so the commit comparison cannot
see it. The list therefore also checks the Automaton's shipped record (release
and merge commit; the commit must be on the default branch) and the file
content, and shows such a branch as `landed` with the reason. A branch whose
landing record says kept or dropped (see the landing record how-to) is settled
on record and is not listed, until the branch moves past the commit it pointed
at when you decided: then it is listed again with the reason "new commits after
the decision". The archived-unmerged list and the finished-not-merged badge use
the same shipped-record and content check.

Each entry shows the Automaton, its title, the branch, the number of commits not
landed, the last activity, the recommendation and a one-line reason. The list is
ordered least settled first (needs a decision, superseded, landed), then newest
activity first. Branches with nothing ahead, branches that no longer exist and
Automata that are not archived are not listed.

### Counts per state and per age

Every read of the list also carries a `counts` block, so you can see the size of
the backlog without reading each row. Each listed branch is counted once per
landing state and once per age band; every key is present and reads 0 when empty.

| Landing state | Recommendation it counts |
|---|---|
| `landed` | `landed` |
| `pending` | `superseded` (later work replaced it; it waits for cleanup) |
| `unlanded` | `needs_decision` |
| `live` | any branch without a settled recommendation yet |

| Age band | Last commit |
|---|---|
| `under_one_day` | less than 24 hours ago (a future time from clock skew counts here) |
| `under_one_week` | from exactly one day up to, not including, seven days ago |
| `older` | seven days or more ago, or no last-commit time known |

`total` is the number of listed branches, and `oldest_pending` names the oldest
pending branch (`branch`, `title`, `age` in plain words such as `3d 4h`, and
`age_ns`); it is absent when nothing is pending. The CLI, the chat verb and the
text output print the same numbers before the rows, and the PWA card shows the
same block: chips per landing state and per age band, the oldest pending branch
and its age, and an “Above alert threshold” line when the pending count is over
the configured threshold (the card reads the two settings from
`GET /api/autonomous/config` for that marker):

```
Counts: 1 landed, 2 pending, 0 live, 1 unlanded
Age: 1 under one day, 1 under one week, 2 older
Oldest pending: Fix the widget (automaton/ab12cd34), 9d 3h
```

The counts come from the same read (`GET /api/autonomous/unlanded-work`, the
`autonomous_unlanded_work` MCP tool, `datawatch autonomous unlanded-work [--json]`
and `autonomous unlanded-work`); there is no separate endpoint and no setting.
The daily pending-branch alert below uses the `pending` count.

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
returns the branch, the bundle path and the worktree path it removed. Know what the bundle covers: the branch's commits. Uncommitted or untracked work inside the worktree is not in the backup, and delete removes the worktree with the force flag, so that work is gone with the worktree. If the worktree might hold uncommitted work, decide it before deleting (the safe-to-delete report flags it as `held_dirty`).

### Where to see and act

| Surface | How |
|---|---|
| PWA | Automata tab → Automation view → **Housekeeping** sub-tab → the **Unlanded work** card, refreshed every 30 seconds. It is hidden until the list has a row. A counts block at the top shows the per-state and per-age chips, the oldest pending branch and its age, and an “Above alert threshold” line while the pending count is over the configured threshold. Each row below it shows the title, commits not landed, last activity and recommendation, with **Keep**, **Archive branch** and **Delete worktree** buttons (delete asks you to confirm) |
| REST | `GET /api/autonomous/unlanded-work`; `POST /api/autonomous/unlanded-work/keep`, `/archive-branch` and `/delete-worktree` (the last needs `confirm: true`) |
| CLI | `datawatch autonomous unlanded-work [--json]`; subcommands `keep <branch> [--note ...]`, `archive-branch <branch>` and `delete-worktree <branch> --confirm` |
| Comm channel | `autonomous unlanded-work`; `autonomous unlanded-work keep\|archive-branch\|delete-worktree <branch>` (delete needs the word `confirm`) |
| MCP | `autonomous_unlanded_work`, read-only. The three actions are not MCP tools |
| Mobile and wear | The same REST contract, described in `docs/api/openapi.yaml` |

### These routes work on the live daemon

The routes on this list pass through one gate in the server:

| Route | Method | Federated peer needs |
|---|---|---|
| `/api/autonomous/unlanded-work` | GET | autonomous read |
| `/api/autonomous/unlanded-work/keep` | POST | autonomous write |
| `/api/autonomous/unlanded-work/archive-branch` | POST | autonomous write |
| `/api/autonomous/unlanded-work/delete-worktree` | POST | autonomous write |
| `/api/autonomous/uncommitted-work/diff` | GET | autonomous read |
| `/api/autonomous/uncommitted-work/commit` | POST | autonomous write |
| `/api/autonomous/cleanup/fix-permissions` | POST | autonomous write |

The daemon hands the server a wrapper around the worker manager, and that
wrapper carries every method the gate needs, so all seven routes answer on the
live daemon. The server checks this when it is built, against the exact wrapper
the daemon wires in, so a build whose wrapper lost one of these methods does not
compile. An empty list still reads empty (no base repository, or nothing
unlanded) and is never an error.

What you can still see when an action cannot run:

Gate refusals — the request was rejected before the action ran, and nothing
was touched:

| Answer | Meaning | What to do |
|---|---|---|
| **503** `autonomous disabled` | Automata are off on this daemon | Turn Automata on (`autonomous.enabled`), then retry |
| **403** `session token lacks capability: ...` | A federated peer called the route without the autonomous read (GET) or write (POST) capability | Grant that capability to the peer, or run the action on the daemon itself |
| **405** `method not allowed` | The route was called with the wrong method | Use the method in the table above |

The action's own failures — the request passed the gate and the action ran. The message says what failed, and the same failure keeps answering the same way while the cause is not resolved:

| Answer | Meaning | What to do |
|---|---|---|
| **400** | Incomplete or invalid input: `branch is required`, `worktree is required`, `confirm is required to delete a worktree`, `no base repository is configured`, `commit message is empty`, `worktree is outside the Automaton worktree base`, `the worktree has no uncommitted files` (an invalid JSON body answers **400** too) | Send the missing field (a delete needs `confirm: true`), configure the base repository, or pick a worktree inside the worktree base that actually has uncommitted files |
| **404** `branch is not in the unlanded list` | `keep`, `archive-branch` or `delete-worktree` was called for a branch that is not in the current unlanded list (it was archived or deleted, or was never listed) | Read the list again (`GET /api/autonomous/unlanded-work`) and act on a branch it names |
| **409** | The action's own failure — for example the delete's bundle backup could not be written or verified (in which case nothing was removed), or the working directory refused the archive ref | Read the message for the cause, clear it, then rerun the action |

One operator flag: because the current build proves at build time that the
wrapper carries every gate method, a daemon answering **503**
`Housekeeping actions unavailable` on any of the seven routes above is an old
build from before the fix — upgrade the daemon binary, there is no setting
that changes that answer.

### Daily pending-branch alert

A **pending** branch is one the list marks `superseded`: later work replaced it
and it only waits for cleanup. Once per calendar day (the daemon host's local
day) the daemon counts the pending branches. When the count is strictly above
`autonomous.unlanded_alert_threshold` and `autonomous.unlanded_alert_enabled` is
on, it raises one `warn` alert, "Pending Automaton branches above threshold",
that gives the count, the threshold, and the oldest pending branch with the age
of its last commit. At or below the threshold nothing is raised; with the alert
off nothing is raised.

- At most one alert per day. The day of the last alert is stored in
  `unlanded_alert.json` in the Automata data folder, so a daemon restart on the
  same day does not raise it again. The next day raises it again if the count is
  still above the threshold.
- The check runs when the Automata loop starts and on every poll tick. It reads
  the branch list at most once an hour while that day's alert is not yet raised,
  and not at all after it is raised. The loop only runs when `autonomous.enabled`
  is true.
- Each alert is also written to the audit log as `autonomous_unlanded_alert`.

| Setting | Default | Meaning |
|---|---|---|
| `autonomous.unlanded_alert_enabled` | `true` (unset reads true) | Turns the daily alert on or off |
| `autonomous.unlanded_alert_threshold` | `3` (unset reads 3) | The alert fires when more pending branches than this exist. A whole number of 0 or more; 0 alerts on any pending branch; a negative value is refused |

A change applies on the next check without a restart.

| Method | How |
|---|---|
| **YAML** | `~/.datawatch/config.yaml` → `autonomous:` → `unlanded_alert_enabled: true`, `unlanded_alert_threshold: 3` |
| **CLI** | `datawatch config set autonomous.unlanded_alert_enabled false`; `datawatch config set autonomous.unlanded_alert_threshold 5` |
| **Web UI** | Settings → **Autonomous Automata planning** card → **Daily pending-branch alert** toggle and **Pending-branch alert threshold** |
| **REST API** | `PUT /api/config` with `{"autonomous.unlanded_alert_threshold": 5}`, or `PUT /api/autonomous/config` with `{"unlanded_alert_threshold": 5}`; read back with `GET /api/config` (section `autonomous`) or `GET /api/autonomous/config` |
| **Comm channel** | `configure autonomous.unlanded_alert_threshold=5` |
| **MCP** | `autonomous_config_set` with `unlanded_alert_enabled` / `unlanded_alert_threshold`, or `config_set` with the full key; read with `autonomous_config_get` or `get_config` |

| Where to see | What you see |
|---|---|
| **Web UI** | The alerts list (bell), level `warn` |
| **Messaging channels** | The alert, where alerts are routed to channels |
| **REST API** | `GET /api/alerts` |
| **MCP** | `get_alerts` |

The branch audit script (`scripts/branch-audit.py`) reports the same finding from
the command line: it reads the daemon's unlanded-work list through
`datawatch autonomous unlanded-work --json`, an `UNLANDED` row is an archived
Automaton branch (cancelled, blocked, failed or no record) with work not on
main, and the script exits with status 1 while any exist. Decide each one with Keep, Archive branch or Delete
worktree, or merge it.

**Review before relying on this.** Whether a kept branch also suppresses the
safe-to-delete report is still undecided; the operator must confirm that. If the
force-delete of a worktree is not wanted when it hides uncommitted work, the
debate over Delete worktree is not yet settled; review that action before using
it. An empty **Unlanded work** card means nothing is unlanded, or no base
repository is set; to tell the two apart, read
`GET /api/autonomous/unlanded-work` directly (**These routes work on the live
daemon** above). The
datawatch-app (Android and iPhone) parity for the Unlanded work card is tracked
in its own repository and is not part of this change.

### The landing record

The list above reviews *branches*. Separately, each **Automaton record** that
carries a work branch now carries a **landing record** — the record's own
account of whether that branch's work reached the project's default branch. It
is the field `landing` on the record (`state`, `commit`, `actor`, `at`,
`reason`), and it has four states:

| State | Set by |
|---|---|
| `pending` | the daemon, when the Automaton's work branch is created. This is the default: an Automaton with a branch starts `pending`. |
| `landed` | the pipeline's `mark_completed` post-ship hook (through the daemon's completed mark) when the branch ships, naming the merge commit. See [Post-ship hooks](post-ship-hooks.md). |
| `kept` | you, with an actor and a non-empty reason, when the branch is kept on purpose and will not be merged. |
| `dropped` | you, with an actor and a non-empty reason, when the work is abandoned and will not be merged. |

`kept` and `dropped` are operator decisions on the record: they need a branch to act on, an actor and a non-empty reason, and are refused when the branch is already `landed` (a landed landing is never rewritten). Each writes one decision on the record's decision log (`landing_keep` or `landing_drop`). The record shows the state, who set it, when and the reason.

**What the record gates: archive.** Archiving the Automaton — by hand on any
surface, or by the daemon's own close of a finished record — is **refused
while its landing is still `pending`**, with the branch named:

> `automaton "…" cannot be archived: its branch automaton/… has not landed on the default branch; merge it, or record keep or drop for its landing`

Once the landing is `landed`, `kept` or `dropped` the archive goes through as
before. An Automaton with **no** landing record — no branch at all, or a record
that predates this record and was never migrated — archives as it always did.
So no unlanded work branch can silently disappear behind an archived record,
and the work the daemon never removes (above) is exactly the work the record
keeps in view.

**The one-time migration.** The daemon runs a one-time **landing
migration** at start: every archived Automaton that has a branch, no landing record, and
whose branch read-only git cannot prove merged on the default branch gets a
`pending` record (actor `landing_migration`). It skips records that already carry a landing record, that have no branch, that are not archived, and those whose merge the git evidence proves on the default branch; when the default branch cannot be established it skips rather than guesses. Running it again changes nothing. It changes no status and writes no decision — it only adds the record. The count it marked is written to the daemon log and, when above zero, raised as one operator alert (*Archived Automata with unlanded branches*); a done marker (`landing_migration.done`) stops it from running again after a clean pass.

**Keep and drop, on every surface.** Both the record display and the two `pending` decisions — `kept` (the branch is kept on purpose and will not be merged) and `dropped` (the work is abandoned and will not be merged), each with an actor and a non-empty reason — are available on every operator surface:

| Surface | What it shows |
|---|---|
| PWA | Automata tab → an Automaton's detail page → **Overview** sub-tab: the landing record — a colored state badge, the commit, actor, time and reason when set. While the state is `pending`, **Keep (branch not merged)** and **Drop (work not merged)** buttons ask for a reason and record the decision. 14 `automata_detail_landing_*` locale keys, in all five bundled locales |
| REST | `POST /api/autonomous/prds/{id}/landing/keep` and `POST /api/autonomous/prds/{id}/landing/drop`, body `{"actor": "<who>", "reason": "<why>"}` — both need the autonomous write capability, and both refuse a reason-less or already-decided record. The record itself rides on every Automaton read (its `landing` field) |
| CLI | `datawatch autonomous prd-landing-keep <id> --reason "<why>"` and `datawatch autonomous prd-landing-drop <id> --reason "<why>"` (both take `--actor`, defaulting to `operator`). `datawatch autonomous prd-get <id>` prints the record as the landing line |
| MCP | `autonomous_landing_keep` and `autonomous_landing_drop` (both need the autonomous write capability); the record comes back in every Automaton read, like `autonomous_prd_get` |
| comm | `autonomous landing <id>` shows the record's line. `autonomous landing keep <id> <reason...> [actor=<who>]` and `autonomous landing drop <id> <reason...>` record the decision — both orders work (decision word before or after the id) |

All refusals say why: a missing actor or reason is refused with the requirement named; a record that is already `landed`, or already `kept`/`dropped`, is refused — the record is never rewritten once settled.

## The task sub-branch reaper (backup first, then remove)

A short operator guide to this reaper is [Reap contained task branches](reap-contained-branches.md).

**On a live daemon.** Found 2026-11-18 and fixed: before the fix, a running
daemon answered 503 — `the autonomous manager does not expose the branch
reaper` — on all three routes **even when** `autonomous.worktree_base_repo` was
set, because the daemon's API wrapper did not carry the reaper's methods. Every
surface in the table below failed with it, and that is why the repository kept
piling up finished Automata while cleanup lagged. The daemon now wires the
reaper in with the routes, so with a base repo set the report, the pass and the
restore work on a live daemon; the 503 now means only that the base repo is
unset. On a daemon upgraded across the gap, the first move is to read the
report and run a pass once the list reads as expected (see the review item
below); the reaper does not catch up on its own.

The worktrees pass removes the Automaton's own `automaton/<id>` work branch.
Every task that ran in parallel mode also left **its** branch (`worktree_branch`)
in the repository. The **branch reaper** removes those leftovers once the parent
Automaton has settled (merged — its work branch's commits are all on the base —
or dropped — its work branch is gone).

**When it is safe.** The reaper works out each Automaton's **landing state** from git alone — **landed**: the work branch exists and every commit of it is already on the base branch by commit *content* (`git cherry`; a cherry-picked copy counts); **dropped**: the work branch is gone, so only the base branch can contain its work; **pending**: the work branch still carries commits not on the base (a comparison git cannot answer reads as pending, so an unknown state never loses a branch) — and skips pending Automata whole. For each settled one it looks at each task branch and removes it only when **every commit on it is contained in the Automaton's work branch or in the base branch**; even one uncontained commit keeps it (with a plain reason), a branch that is already absent is a no-op, and if git cannot compare, the branch is kept — never a guess. And if a merge is in progress (`MERGE_HEAD` in any of the Automata's working directories), the pass ends early and removes nothing, so work mid-merge is never judged against a moving base.

**Backup first.** Before any removal, the reaper writes a backup ref,
`refs/datawatch/reap-backup/<branch>/<Unix seconds>` (branch-name characters
that would not be ref-safe become dashes, so `automaton/task-1` lands as
`refs/datawatch/reap-backup/automaton-task-1/<timestamp>`), and only **then** removes the branch.
That removal is `git branch -D` — the one forced delete in this area — but it is
permitted only because the backup ref exists: if the backup ref cannot be
written, the branch is not removed and the entry is counted. If the backup
succeeds and the removal fails, the backup ref stands and the branch is still
restorable. You can inspect a backup any time with
`git for-each-ref refs/datawatch/reap-backup/`.

**Restore.** A restore takes the branch name and recreates it from the
*latest* backup ref under that name. If there is no backup ref for that name it
returns an error — it never guesses a target.

**Expiry.** Backup refs are not permanent. `autonomous.reaped_branch_backup_days`
is the whole days a reaped branch's backup ref is kept before it is pruned:
unset or 0 reads the default of **30** days, and a negative value is refused as
a config error. The expiry prunes only refs inside `refs/datawatch/reap-backup/`
whose numeric timestamp suffix is already older than the retention; a ref whose
name has no numeric timestamp is never touched, and pruning closes the restore
window for that branch.

### Setting the retention

| Method | How |
|--------|-----|
| **YAML** | `~/.datawatch/config.yaml` → `autonomous.reaped_branch_backup_days` |
| **CLI** | `datawatch config set autonomous.reaped_branch_backup_days 45` |
| **REST** | `PUT /api/config` or `PUT /api/autonomous/config` with `{"autonomous.reaped_branch_backup_days": 45}` |
| **Comm channel** | `configure autonomous.reaped_branch_backup_days=45` |
| **MCP** | the `config_set` call above |
| **Web app / PWA** | Settings → General, field labelled "Reaped Automaton branch backup kept (days; 0 or unset = 30)" |

It is read live, so a change applies with no restart, and `GET /api/config`
reports the effective days under `autonomous`.

### Where to see and act

| Surface | How |
|---|---|
| REST | `GET /api/autonomous/branch-reap` for the report: `reaped` (each with `automaton_id`, `backup_ref`, `contained_in`, `commits`, a plain `reason`), `waiting` (same shape, with the reason it was kept), `backups` (every ref under `refs/datawatch/reap-backup/`, the branch named when it matches a stored task sub-branch), `backup_days` and `computed_at`. `POST /api/autonomous/branch-reap/run` performs one pass (takes no body; a pass already in progress answers **409**). `POST /api/autonomous/branch-reap/restore` with `{"branch": "..."}` recreates one branch from its newest backup ref (a branch that was never reaped answers **404**). All three answer **503** while the base repo (`autonomous.worktree_base_repo`) is unset, and the two `POST`s need the autonomous write capability from a federated peer. |
| CLI | `datawatch autonomous branch-reap [--json]` (report), `datawatch autonomous branch-reap-run [--json]` (pass), `datawatch autonomous branch-reap-restore <branch> [--json]` (restore). All three print the same wording the chat verbs show; `--json` prints the daemon's body untouched. |
| MCP | `autonomous_branch_reap` (report, read-only), `autonomous_branch_reap_run` (pass) and `autonomous_branch_reap_restore` (restore; `branch` is required). Each defaults to plain wording; `format: json` returns the daemon's body untouched. |
| Comm channel | `autonomous branch-reap` (report) and `autonomous branch-reap-restore <branch>` (restore). There is **no comm verb for the pass** — that is REST, CLI and MCP only. |
| Web app / PWA | Automata tab → **Events** sub-tab → the **Branch reaper** card under the cleanup card (it refreshes while the sub-tab is open): "Would be reaped (N)" and "Held — would be kept (N)" with the Automaton, the containment, the commit count and the backup ref per branch, then the backup refs and the retention, and a **Restore** button per listed branch. The card has no run button. |
| Mobile and wear | The same REST contract, documented in `docs/api/openapi.yaml`. |

A CLI walkthrough of the three steps:

1. `datawatch autonomous branch-reap` — the report:
   ```
   Branch reaper preview (the report deletes nothing yet)

   Would be reaped:
     - automaton/p1--tasks-fix-t1  (Automaton p1)  every commit of automaton/p1--tasks-fix-t1 is on main  [backup refs/datawatch/reap-backup/automaton-p1--tasks-fix-t1/1784000000]

   Held (branch kept):
     - automaton/p1--tasks-fix-t2  (Automaton p1)  3 commit(s) of automaton/p1--tasks-fix-t2 are not in main

   Backup refs (retention 30 whole days when unset):
     - refs/datawatch/reap-backup/automaton-p1--tasks-fix-t1/1784000000 -> a1b2c3d  (branch automaton/p1--tasks-fix-t1)
   ```
   Nothing has been backed up, deleted or restored — a report.
2. When you are ready: `datawatch autonomous branch-reap-run` — each listed
   branch gets its backup ref first, then `git branch -D`; the reply says
   "Reaped (each backup ref was written before the delete)" with the ref per
   branch. A pass already in progress answers a conflict, not a silent skip.
3. To bring one back: `datawatch autonomous branch-reap-restore
   automaton/p1--tasks-fix-t1` — "Branch restored from its newest backup ref"
   with the branch, the backup ref and the commit it now points at.

### How it relates to the rest of this guide

- It removes **task sub-branches**, not `automaton/<id>` work branches or
  worktrees — that is the worktrees pass's job, with its own mode, retention
  window and per-pass limit.
- It does **not** read the unlanded **Keep** records (`unlanded-keep.json`),
  the project's **Branch retention** (`projects_defaults.branch_retention_days`)
  or any of the cleanup pass's state; its safety tests are settlement, the
  containment check and no merge in progress. A **Keep** record protects the
  Automaton's work branch from the worktrees pass — it does not speak for the
task sub-branches. (That a Keep record leaves task sub-branches unprotected is one of the things still to review below.)
- It is a different feature from *Processes the worker left running* (no branch
  removal at all) and from the **session-end** reaper, which reaps a session's
  leftover processes.

### Still needs your review

- **Backlog from the wiring gap (fixed 2026-11-18).** Until the fix, all three
  routes answered 503 on a live daemon even with a base repo set, so no report
  could be read and no pass could be run from any surface, and completed
  Automata kept piling up. On a daemon that has the fix, verify the first pass
  by hand — read the report, confirm the list is what you expect, run the
  pass, and spot-check one restore before trusting it at scale.
- **Nothing runs a pass on a timer, and the expiry has no caller.** The
  report, the pass and the restore are all operator-triggered (above), but no
  schedule runs a reap pass and nothing calls the backup-ref expiry — the refs
  accumulate until something prunes them. Review where and when the pass and
  the expiry should run.
- **The pass is not on the comm channel or the PWA card.** Those two surfaces
  have the report and the restore; the pass is REST, CLI and MCP only. Review
  whether that is the split you want.
- **Keep-record gap.** A task sub-branch whose parent Automaton you **Keep** for
  the record is reaped as soon as it is contained (it is still protected only
  while the landing state is `pending`). If you rely on Keep records as your
  "do not touch this", review whether the reaper should also honor them.
- **Retention fit.** 30 days is the default restore window per project. Decide
  whether it is right for your review cycles before a backup you might need is
  pruned.

## Processes the worker left running

The cleanup above removes branches and folders. Separately, when a worker session ends (including cancel and failure), the daemon ends processes that worker started and left running. It matches them by `/proc` evidence only (a working directory, executable or script in the session worktree or scratch folder, ancestry to the session, or the session id in the environment), never by name. Read the result as a `session_end_reap` decision on the task or an audit entry of the same name (the decision exists only when the session is bound to a task). If the audit entry shows `survivors` above zero, those processes ignored the signal and are counted only, with no pid or command recorded: look for them by hand, even when the ended count is zero. Details and review notes: [operations](../operations.md#processes-a-session-leaves-behind).
