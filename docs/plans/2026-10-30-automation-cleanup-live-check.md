# Automation cleanup — live check on a sandbox daemon

**Date:** 2026-10-30 (run started 14:49:23, local time)
**Version under test:** datawatch v9.42.1 (binary built from this worktree)
**Script:** `scripts/cleanup-live-check.sh` (run as `env -u TMUX -u TMUX_TMPDIR bash scripts/cleanup-live-check.sh`)
**Scope:** `GET /api/autonomous/cleanup`, the worktrees pass in `report` and then `on` mode, the release when an Automaton is cancelled, the daemon temp-file cleanup of worker-session scratch, the triage record and audit log entries for each removal, and the MCP, CLI and comm-channel reads, against a throwaway project repository whose default branch is `trunk`.

## Summary

| # | Result | Check |
|---|---|---|
| 1 | PASS | `GET /api/autonomous/cleanup` answers 200 with both passes |
| 2 | PASS | the unset mode runs as report |
| 3 | PASS | the three removable Automaton branches are counted as would-remove and none is removed |
| 4 | PASS | the five held entries (dirty, unmerged, two unfinished, young) are counted as skipped |
| 5 | PASS | report mode removed no branch and no work tree |
| 6 | PASS | safe-to-delete judges the merged branches against `trunk` (the project has no `main`): safe entries name it, none is held as unknown |
| 7 | PASS | REST, MCP, CLI and comm reads agree (report mode) |
| 8 | PASS | retention 0, mode `sometimes` and disk percent 101 are refused with 400 and nothing is saved |
| 9 | PASS | the pass picked up mode on without a restart |
| 10 | PASS | the three safe entries are gone (two with their work trees, one branch only) |
| 11 | PASS | the dirty, unmerged, unfinished and young entries and the operator's own branch were all kept |
| 12 | PASS | the cleanup judged merged work against `trunk` (the repository has no `main`) |
| 13 | PASS | each of the three removals is in the audit log (action `autonomous_worktree_remove`, with branch, base branch, age and bytes freed) |
| 14 | PASS | each removal is in the Automaton's triage record |
| 15 | PASS | the daemon log carries a summary line for the worktrees pass |
| 16 | PASS | cancelling an Automaton whose work is already on the default branch released its branch and work tree at once |
| 17 | PASS | each worker session left its own prefixed scratch file in the daemon's temp folder |
| 18 | PASS | the scratch file of the ended session is gone (session end released it) |
| 19 | PASS | the scratch file of the live session is still there |
| 20 | PASS | a pass in mode on kept the live session's file and a prefixed name the daemon never created |
| 21 | PASS | the pass counted the two names it kept (the live session's file and the foreign name) as skipped |
| 22 | PASS | once the live session ended, its file went too |
| 23 | PASS | REST, MCP, CLI and comm reads agree (on mode) |
| 24 | PASS | the sandbox daemon, its sessions and its tmux server are gone and no test daemon was left behind |

**Checks:** 24 passed, 0 failed.

## Setup

- **Sandbox daemon only.** Started with `td_start` from `scripts/test-daemon-lib.sh` from a config generated from `testdata/datawatch.yaml` by the substitutions the test runner uses (data folder, ports, loopback host, test token). Added: `autonomous.worktree_base_repo` and `autonomous.worktree_dir` (the throwaway project and its worktree root), `server.allowed_project_roots` (one folder for the two worker sessions) and an `aider` backend entry that points at a stand-in worker. The production daemon, its service and its data were not touched, and nothing was ended by name.
- **Ports.** HTTP 41325, TLS 47445, MCP 57265, DNS 35619, webhook 48033, notify 44891 — each probed free just before use.
- **tmux.** A `tmux` shim first on the daemon's `PATH` pins every tmux call to a socket this script owns (`$SCRATCH/tmux.sock`). The operator's tmux server was not reachable from the daemon and no tmux command was run against it.
- **Daemon temp folder.** The daemon's `TMPDIR` is `$SCRATCH/daemon-tmp`, so its temp sweep scans only that folder.
- **Everything** lives under one `mktemp` folder outside the worktree, removed on every exit path.

Throwaway project: a bare repository (`remote.git`) whose default branch is `trunk`, cloned to the worktree base repository; `git remote set-head origin trunk` records it as `refs/remotes/origin/HEAD`. The branch `main` does not exist in it.

Default branch as git records it:
```
refs/remotes/origin/trunk; main exists: no

```

| Automaton id | Name | Store status | Entry age | Branch state | Expected |
|---|---|---|---|---|---|
| `c0a10001` | release-completed | completed | 72h | merged | removed — finished, merged, clean, old |
| `c0a10002` | release-cancelled | cancelled | 72h | merged | removed — cancelled, merged, clean, old |
| `c0a10003` | release-branch-only | completed | 72h | nowt | removed — finished, merged, branch only (no work tree) |
| `c0a10004` | held-dirty | completed | 72h | dirty | kept — uncommitted change in the work tree |
| `c0a10005` | held-unmerged | completed | 72h | unmerged | kept — commit not on the default branch |
| `c0a10006` | held-unfinished | paused | 72h | merged | kept — Automaton not finished (paused) |
| `c0a10007` | held-young | completed | 0h | merged | kept — entry younger than the retention window |
| `c0a10008` | end-release | paused | 72h | merged | kept (unfinished) until it is cancelled in step 5 |
| — | `feature/user-owned` (a branch the operator made) | — | — | merged, with a work tree | kept — outside the `automaton/` namespace |

Branch heads and work trees before the daemon starts:
```
automaton/c0a10001-release-completed 10d6c6c
automaton/c0a10002-release-cancelled 26be768
automaton/c0a10003-release-branch-only a74f11c
automaton/c0a10004-held-dirty 2de2701
automaton/c0a10005-held-unmerged 74bc7de
automaton/c0a10006-held-unfinished 44251a0
automaton/c0a10007-held-young 3d664b7
automaton/c0a10008-end-release 999a539
feature/user-owned 1748ca7
trunk 8ce1afb
---
/tmp/dw-cleanup-live.tQLoME/project                              8ce1afb [trunk]
/tmp/dw-cleanup-live.tQLoME/worktrees/c0a10001-release-completed 10d6c6c [automaton/c0a10001-release-completed]
/tmp/dw-cleanup-live.tQLoME/worktrees/c0a10002-release-cancelled 26be768 [automaton/c0a10002-release-cancelled]
/tmp/dw-cleanup-live.tQLoME/worktrees/c0a10004-held-dirty        2de2701 [automaton/c0a10004-held-dirty]
/tmp/dw-cleanup-live.tQLoME/worktrees/c0a10005-held-unmerged     74bc7de [automaton/c0a10005-held-unmerged]
/tmp/dw-cleanup-live.tQLoME/worktrees/c0a10006-held-unfinished   44251a0 [automaton/c0a10006-held-unfinished]
/tmp/dw-cleanup-live.tQLoME/worktrees/c0a10007-held-young        3d664b7 [automaton/c0a10007-held-young]
/tmp/dw-cleanup-live.tQLoME/worktrees/c0a10008-end-release       999a539 [automaton/c0a10008-end-release]
/tmp/dw-cleanup-live.tQLoME/worktrees/user-owned                 1748ca7 [feature/user-owned]

```

Sandbox daemon pid `3749822` (recorded by the lifecycle helper) was healthy at 14:49:27.

## 1. Report mode: would-remove is counted, nothing is removed

**Check 1 — PASS:** `GET /api/autonomous/cleanup` answers 200 with both passes

{"worktrees":"report","temp":"report"}

Cleanup status (read from: REST), mode left at its default:
```
{
  "worktrees": {
    "mode": "report",
    "last_run": "2026-10-30T18:49:27.256824023Z",
    "removed": 0,
    "would_remove": 3,
    "skipped": 5,
    "failed": 0,
    "bytes_freed": 0,
    "owner_removed": 0,
    "net_removed": 0,
    "owner_bytes_freed": 0,
    "net_bytes_freed": 0
  },
  "temp": {
    "mode": "report",
    "last_run": "2026-10-30T18:49:27.256899737Z",
    "removed": 0,
    "would_remove": 0,
    "skipped": 0,
    "failed": 0,
    "bytes_freed": 0,
    "owner_removed": 0,
    "net_removed": 0,
    "owner_bytes_freed": 0,
    "net_bytes_freed": 0
  },
  "disk_alert_percent": 85,
  "worktree_disk": {
    "path": "/tmp/dw-cleanup-live.tQLoME/project",
    "percent": 47,
    "alert": false
  },
  "temp_disk": {
    "path": "/tmp/dw-cleanup-live.tQLoME/daemon-tmp",
    "percent": 47,
    "alert": false
  },
  "disk_alert": false
}

```

**Check 2 — PASS:** the unset mode runs as report

mode=`report`

**Check 3 — PASS:** the three removable Automaton branches are counted as would-remove and none is removed

would_remove=3 removed=0 skipped=5

**Check 4 — PASS:** the five held entries (dirty, unmerged, two unfinished, young) are counted as skipped

skipped=5

**Check 5 — PASS:** report mode removed no branch and no work tree

branches: automaton/c0a10001-release-completed automaton/c0a10002-release-cancelled automaton/c0a10003-release-branch-only automaton/c0a10004-held-dirty automaton/c0a10005-held-unmerged automaton/c0a10006-held-unfinished automaton/c0a10007-held-young automaton/c0a10008-end-release feature/user-owned trunk 

Daemon log, cleanup summary lines:
```
2026/10/30 14:49:27 [autonomous] worktree-cleanup pass: mode="report" removed=0 would_remove=3 skipped=5 failed=0
2026/10/30 14:49:27 [autonomous] temp-sweep: mode="report" considered=0 removed=0 would_remove=0 skipped=0 failed=0 bytes_freed=0

```

`GET /api/autonomous/safe-to-delete` on the same project (for comparison with the cleanup's own count):
```
{"safe":[{"branch":"automaton/c0a10001-release-completed","worktree":"/tmp/dw-cleanup-live.tQLoME/worktrees/c0a10001-release-completed","reason":"no commits ahead of trunk","commits":0},{"branch":"automaton/c0a10002-release-cancelled","worktree":"/tmp/dw-cleanup-live.tQLoME/worktrees/c0a10002-release-cancelled","reason":"no commits ahead of trunk","commits":0},{"branch":"automaton/c0a10003-release-branch-only","worktree":"","reason":"no commits ahead of trunk","commits":0},{"branch":"automaton/c0a10006-held-unfinished","worktree":"/tmp/dw-cleanup-live.tQLoME/worktrees/c0a10006-held-unfinished","reason":"no commits ahead of trunk","commits":0},{"branch":"automaton/c0a10007-held-young","worktree":"/tmp/dw-cleanup-live.tQLoME/worktrees/c0a10007-held-young","reason":"no commits ahead of trunk","commits":0},{"branch":"automaton/c0a10008-end-release","worktree":"/tmp/dw-cleanup-live.tQLoME/worktrees/c0a10008-end-release","reason":"no commits ahead of trunk","commits":0}],"held":[{"branch":"automaton/c0a10004-held-dirty","worktree":"/tmp/dw-cleanup-live.tQLoME/worktrees/c0a10004-held-dirty","state":"empty","dirty":true,"note":"Automaton worktree has uncommitted changes"},{"branch":"automaton/c0a10005-held-unmerged","worktree":"/tmp/dw-cleanup-live.tQLoME/worktrees/c0a10005-held-unmerged","state":"pending"}],"held_dirty":1}

```

**Check 6 — PASS:** safe-to-delete judges the merged branches against `trunk` (the project has no `main`): safe entries name it, none is held as unknown

safe=6, safe reasons not naming `trunk`=0, held as unknown=0

## 2. The four reads agree (report mode)

CLI text:
```
Automaton cleanup (disk alert threshold: 85%):
  worktrees pass (mode report): last run 2026-10-30T18:49:27Z, removed 0, would-remove 3, skipped 5, failed 0, freed 0 bytes
    owner vs net: 0/0 removed, 0/0 bytes freed
  temp pass (mode report): last run 2026-10-30T18:49:27Z, removed 0, would-remove 0, skipped 0, failed 0, freed 0 bytes
    owner vs net: 0/0 removed, 0/0 bytes freed
  worktree disk: /tmp/dw-cleanup-live.tQLoME/project at 47%
  temp dir disk: /tmp/dw-cleanup-live.tQLoME/daemon-tmp at 47%
  disk alert: not active
This is a status read only: the cleanup passes do the removing, not this read.

```

Comm-channel reply:
```
[dw-e2e-test] autonomous cleanup
Automaton cleanup (disk alert threshold: 85%):
  worktrees pass (mode report): last run 2026-10-30T18:49:27Z, removed 0, would-remove 3, skipped 5, failed 0, freed 0 bytes
    owner vs net: 0/0 removed, 0/0 bytes freed
  temp pass (mode report): last run 2026-10-30T18:49:27Z, removed 0, would-remove 0, skipped 0, failed 0, freed 0 bytes
    owner vs net: 0/0 removed, 0/0 bytes freed
  worktree disk: /tmp/dw-cleanup-live.tQLoME/project at 47%
  temp dir disk: /tmp/dw-cleanup-live.tQLoME/daemon-tmp at 47%
  disk alert: not active
This is a status read only: the cleanup passes do the removing, not this read.

```

MCP result (first 600 bytes):
```
{"worktrees":{"mode":"report","last_run":"2026-10-30T18:49:27.256824023Z","removed":0,"would_remove":3,"skipped":5,"failed":0,"bytes_freed":0,"owner_removed":0,"net_removed":0,"owner_bytes_freed":0,"net_bytes_freed":0},"temp":{"mode":"report","last_run":"2026-10-30T18:49:27.256899737Z","removed":0,"would_remove":0,"skipped":0,"failed":0,"bytes_freed":0,"owner_removed":0,"net_removed":0,"owner_bytes_freed":0,"net_bytes_freed":0},"disk_alert_percent":85,"worktree_disk":{"path":"/tmp/dw-cleanup-live.tQLoME/project","percent":47,"alert":false},"temp_disk":{"path":"/tmp/dw-cleanup-live.tQLoME/daemo

```

**Check 7 — PASS:** REST, MCP, CLI and comm reads agree (report mode)

REST: `GET /api/autonomous/cleanup`; CLI: `datawatch autonomous cleanup --json` and `datawatch autonomous cleanup`; MCP: `autonomous_cleanup_status` over `datawatch mcp` (stdio); comm: `POST /api/test/message {"text":"autonomous cleanup"}`. Comparison result: `OK`. Comm reply as read: `[dw-e2e-test] autonomous cleanup Automaton cleanup (disk alert threshold: 85%): `

## 3. A setting that is not allowed is refused and saves nothing

**Check 8 — PASS:** retention 0, mode `sometimes` and disk percent 101 are refused with 400 and nothing is saved

HTTP 400 / 400 / 400; automation settings identical before and after

Note: the brief asked for "retention 0". The smallest retention the cleanup accepts is 1 hour (0 is refused above, and an unset or hand-edited 0 reads as the 24-hour default), so the on-mode step below uses 1 hour and the sample Automata are 72 hours old.

## 4. On mode: only the safe entries are removed

`PUT /api/config` (retention 1 hour, mode on):
```
{"status":"ok"}

```

Cleanup status after the pass (read from: REST):
```
{
  "worktrees": {
    "mode": "on",
    "last_run": "2026-10-30T18:50:27.257634732Z",
    "removed": 3,
    "would_remove": 0,
    "skipped": 5,
    "failed": 0,
    "bytes_freed": 237,
    "owner_removed": 0,
    "net_removed": 3,
    "owner_bytes_freed": 0,
    "net_bytes_freed": 237
  },
  "temp": {
    "mode": "on",
    "last_run": "2026-10-30T18:50:27.257696154Z",
    "removed": 0,
    "would_remove": 0,
    "skipped": 0,
    "failed": 0,
    "bytes_freed": 0,
    "owner_removed": 0,
    "net_removed": 0,
    "owner_bytes_freed": 0,
    "net_bytes_freed": 0
  },
  "disk_alert_percent": 85,
  "worktree_disk": {
    "path": "/tmp/dw-cleanup-live.tQLoME/project",
    "percent": 47,
    "alert": false
  },
  "temp_disk": {
    "path": "/tmp/dw-cleanup-live.tQLoME/daemon-tmp",
    "percent": 47,
    "alert": false
  },
  "disk_alert": false
}

```

**Check 9 — PASS:** the pass picked up mode on without a restart

mode=on

**Check 10 — PASS:** the three safe entries are gone (two with their work trees, one branch only)

removed=3 would_remove=0 skipped=5 failed=0

**Check 11 — PASS:** the dirty, unmerged, unfinished and young entries and the operator's own branch were all kept

remaining branches: automaton/c0a10004-held-dirty automaton/c0a10005-held-unmerged automaton/c0a10006-held-unfinished automaton/c0a10007-held-young automaton/c0a10008-end-release feature/user-owned trunk 

**Check 12 — PASS:** the cleanup judged merged work against `trunk` (the repository has no `main`)

HEAD of the base repository: trunk

Audit log, `GET /api/audit?action=autonomous_worktree_remove`:
```
{"at":"2026-10-30T14:50:27.256480591-04:00","actor":"autonomous","action":"autonomous_worktree_remove","branch":"automaton/c0a10003-release-branch-only","bytes_freed":0,"base_branch":"trunk","age_seconds":259263}
{"at":"2026-10-30T14:50:27.249862079-04:00","actor":"autonomous","action":"autonomous_worktree_remove","branch":"automaton/c0a10002-release-cancelled","bytes_freed":127,"base_branch":"trunk","age_seconds":259263}
{"at":"2026-10-30T14:50:27.242190206-04:00","actor":"autonomous","action":"autonomous_worktree_remove","branch":"automaton/c0a10001-release-completed","bytes_freed":110,"base_branch":"trunk","age_seconds":259263}

```

**Check 13 — PASS:** each of the three removals is in the audit log (action `autonomous_worktree_remove`, with branch, base branch, age and bytes freed)

automaton/c0a10001-release-completed automaton/c0a10002-release-cancelled automaton/c0a10003-release-branch-only

**Check 14 — PASS:** each removal is in the Automaton's triage record

c0a10001: work branch automaton/c0a10001-release-completed and its work tree removed (110 bytes freed): no commits ahead of trunk; c0a10002: work branch automaton/c0a10002-release-cancelled and its work tree removed (127 bytes freed): no commits ahead of trunk; c0a10003: work branch automaton/c0a10003-release-branch-only and its work tree removed (0 bytes freed): no commits ahead of trunk;  (read 0 s after the branch was seen gone)

Daemon log, cleanup lines so far (the log holds 105 lines, 13 of them from the Automata engine):
```
2026/10/30 14:49:27 [autonomous] worktree-cleanup pass: mode="report" removed=0 would_remove=3 skipped=5 failed=0
2026/10/30 14:49:27 [autonomous] temp-sweep: mode="report" considered=0 removed=0 would_remove=0 skipped=0 failed=0 bytes_freed=0
2026/10/30 14:50:27 [autonomous] worktree-cleanup pass: mode="on" removed=3 would_remove=0 skipped=5 failed=0
2026/10/30 14:50:27 [autonomous] temp-sweep: mode="on" considered=0 removed=0 would_remove=0 skipped=0 failed=0 bytes_freed=0

```

Daemon log, every line from the Automata engine ([autonomous]):
```
[autonomous] boot-resume: scanning prd=c0a10007 status=completed
[autonomous] boot-resume: scanning prd=c0a10006 status=paused
[autonomous] boot-resume: scanning prd=c0a10005 status=completed
[autonomous] boot-resume: scanning prd=c0a10008 status=paused
[autonomous] boot-resume: scanning prd=c0a10001 status=completed
[autonomous] boot-resume: scanning prd=c0a10002 status=cancelled
[autonomous] boot-resume: scanning prd=c0a10003 status=completed
[autonomous] boot-resume: scanning prd=c0a10004 status=completed
[autonomous] boot-resume: resumed 0, parked 0
2026/10/30 14:49:27 [autonomous] worktree-cleanup pass: mode="report" removed=0 would_remove=3 skipped=5 failed=0
2026/10/30 14:49:27 [autonomous] temp-sweep: mode="report" considered=0 removed=0 would_remove=0 skipped=0 failed=0 bytes_freed=0
2026/10/30 14:50:27 [autonomous] worktree-cleanup pass: mode="on" removed=3 would_remove=0 skipped=5 failed=0
2026/10/30 14:50:27 [autonomous] temp-sweep: mode="on" considered=0 removed=0 would_remove=0 skipped=0 failed=0 bytes_freed=0

```

Log files the sandbox daemon wrote:
```
daemon.log: 2978 bytes, 46 lines, 0 cleanup summary lines
data/access.log: 6255 bytes, 30 lines, 0 cleanup summary lines
data/audit.log: 3537 bytes, 13 lines, 0 cleanup summary lines
data/daemon-crash.log: 1451 bytes, 16 lines, 4 cleanup summary lines

```

**Check 15 — PASS:** the daemon log carries a summary line for the worktrees pass

2026/10/30 14:50:27 [autonomous] worktree-cleanup pass: mode="on" removed=3 would_remove=0 skipped=5 failed=0 — found in: data/daemon-crash.log 

## 5. Release at end: cancelling an Automaton calls the same release

`POST /api/autonomous/prds/c0a10008/cancel` → HTTP 200, Automaton status `cancelled`. Daemon log (release lines):
```
2026/10/30 14:50:28 [autonomous] worktree-cleanup release: mode="on" removed=1 would_remove=0 skipped=0 failed=0

```

Cancelled entry, `updated_at`, branch state and the worktrees counters before and after (read from: REST):
```
updated_at=2026-10-30T14:50:28.661293742-04:00; entry age at cancel: under 1 minute; branch exists: no
before: {"last_run":"2026-10-30T18:50:27.257634732Z","skipped":5,"owner_removed":0,"net_removed":3}
after:  {"last_run":"2026-10-30T18:50:28.661431339Z","skipped":0,"owner_removed":1,"net_removed":3}

```

Audit log, the release outcome the cancel wrote (`autonomous_worktree_cleanup_release*`):
```
{"at":"2026-10-30T14:50:28.661415705-04:00","action":"autonomous_worktree_cleanup_release","details":{"failed":0,"mode":"on","removed":1,"skipped":{},"would_remove":0}}

```

**Check 16 — PASS:** cancelling an Automaton whose work is already on the default branch released its branch and work tree at once

owner_removed=1

## 6. Daemon temp files: kept while the session is live, gone when it ends
- Two worker sessions on the `aider` backend (a stand-in that sleeps): live id `dw-e2e-test-46e9`, to-be-ended id `dw-e2e-test-d300`.

Scratch files in the daemon's temp folder after both sessions started:
```
datawatch-aider-task-cs-dw-e2e-test-46e9-719482795.txt
datawatch-aider-task-cs-dw-e2e-test-d300-1302364491.txt
datawatch-foreign-check
node-compile-cache
opencode

```

**Check 17 — PASS:** each worker session left its own prefixed scratch file in the daemon's temp folder

live: `datawatch-aider-task-cs-dw-e2e-test-46e9-719482795.txt`; to-be-ended: `datawatch-aider-task-cs-dw-e2e-test-d300-1302364491.txt`

**Check 18 — PASS:** the scratch file of the ended session is gone (session end released it)

kill HTTP 200; owner_removed on the temp pass: 1

**Check 19 — PASS:** the scratch file of the live session is still there

datawatch-aider-task-cs-dw-e2e-test-46e9-719482795.txt

Status after another pass with a live session and a foreign name present:
```
{"worktrees":{"mode":"on","removed":0,"would_remove":0,"skipped":4,"failed":0,"owner_removed":1,"net_removed":3},"temp":{"mode":"on","removed":0,"would_remove":0,"skipped":2,"failed":0,"owner_removed":1,"net_removed":0},"disk_alert":false,"disk_alert_percent":85}

```

**Check 20 — PASS:** a pass in mode on kept the live session's file and a prefixed name the daemon never created

temp: removed=0 skipped=2 failed=0

**Check 21 — PASS:** the pass counted the two names it kept (the live session's file and the foreign name) as skipped

temp skipped=2

**Check 22 — PASS:** once the live session ended, its file went too

kill HTTP 200

Not shown live: removal by the periodic temp sweep itself. It only removes scratch of a session that ended at least the retention window ago (minimum 1 hour), so a sweep removal cannot happen inside a short check. The sweep's kept cases (live session, foreign name) are shown above; its removal rule is covered by the unit tests in `internal/autonomous/temp_sweep_test.go`.

## 7. The four reads agree (on mode, after the removals)

CLI text:
```
Automaton cleanup (disk alert threshold: 85%):
  worktrees pass (mode on): last run 2026-10-30T18:51:27Z, removed 0, would-remove 0, skipped 4, failed 0, freed 0 bytes
    owner vs net: 1/3 removed, 206/237 bytes freed
  temp pass (mode on): last run 2026-10-30T18:51:27Z, removed 0, would-remove 0, skipped 2, failed 0, freed 0 bytes
    owner vs net: 2/0 removed, 0/0 bytes freed
  worktree disk: /tmp/dw-cleanup-live.tQLoME/project at 47%
  temp dir disk: /tmp/dw-cleanup-live.tQLoME/daemon-tmp at 47%
  disk alert: not active
This is a status read only: the cleanup passes do the removing, not this read.

```

Comm-channel reply:
```
[dw-e2e-test] autonomous cleanup
Automaton cleanup (disk alert threshold: 85%):
  worktrees pass (mode on): last run 2026-10-30T18:51:27Z, removed 0, would-remove 0, skipped 4, failed 0, freed 0 bytes
    owner vs net: 1/3 removed, 206/237 bytes freed
  temp pass (mode on): last run 2026-10-30T18:51:27Z, removed 0, would-remove 0, skipped 2, failed 0, freed 0 bytes
    owner vs net: 2/0 removed, 0/0 bytes freed
  worktree disk: /tmp/dw-cleanup-live.tQLoME/project at 47%
  temp dir disk: /tmp/dw-cleanup-live.tQLoME/daemon-tmp at 47%
  disk alert: not active
This is a status read only: the cleanup passes do the removing, not this read.

```

MCP result (first 600 bytes):
```
{"worktrees":{"mode":"on","last_run":"2026-10-30T18:51:27.225651132Z","removed":0,"would_remove":0,"skipped":4,"failed":0,"bytes_freed":0,"owner_removed":1,"net_removed":3,"owner_bytes_freed":206,"net_bytes_freed":237},"temp":{"mode":"on","last_run":"2026-10-30T18:51:27.225718163Z","removed":0,"would_remove":0,"skipped":2,"failed":0,"bytes_freed":0,"owner_removed":2,"net_removed":0,"owner_bytes_freed":0,"net_bytes_freed":0},"disk_alert_percent":85,"worktree_disk":{"path":"/tmp/dw-cleanup-live.tQLoME/project","percent":47,"alert":false},"temp_disk":{"path":"/tmp/dw-cleanup-live.tQLoME/daemon-tm

```

**Check 23 — PASS:** REST, MCP, CLI and comm reads agree (on mode)

REST: `GET /api/autonomous/cleanup`; CLI: `datawatch autonomous cleanup --json` and `datawatch autonomous cleanup`; MCP: `autonomous_cleanup_status` over `datawatch mcp` (stdio); comm: `POST /api/test/message {"text":"autonomous cleanup"}`. Comparison result: `OK`. Comm reply as read: `[dw-e2e-test] autonomous cleanup Automaton cleanup (disk alert threshold: 85%): `

## Cleanup

- **Sandbox daemon ended** by `td_stop_all` (recorded pid `3749822`, marker = its config path). Process gone: yes. TLS port 47445 closed: yes.
- **Private tmux server** (explicit throwaway socket) ended; a session left on it: no.
- **Leftover check** against the snapshot taken before this run: `scripts/check-no-leftover-test-daemons.sh check` exit 0 — OK: no leftover test daemons

**Check 24 — PASS:** the sandbox daemon, its sessions and its tmux server are gone and no test daemon was left behind

daemon gone: yes; port closed: yes; private tmux session left: no; leftover check exit 0
- **Scratch area** (project, remote, work trees, data, config, log, daemon records, shims) is removed by the exit handler right after this report is written.

## Spec amendments and what is not shown live

- **Retention 0 is not testable and was amended to 1 hour.** The brief asked for an on-mode pass with retention 0. The smallest retention the cleanup accepts is 1 hour (check 8 shows `0` refused with 400, and a hand-edited `0` reads as the 24-hour default), so the on-mode pass uses 1 hour and the sample Automata are seeded 72 hours old in the store.
- **Release at end ignores the retention window.** The age counts from the moment the Automaton finished, so with a window of at least an hour an end-path release that honored it could never remove anything. The owner's release at completion or cancel therefore skips only the age test (all other safety tests still apply, check 16); the periodic pass keeps the window (check 11, held `young`).
- **A temp dir of an ended session swept by the periodic sweep is not shown live.** The sweep removes only paths that the daemon's in-memory created-path record attributes to a session that ended at least the retention window (minimum 1 hour) ago, and the record cannot be seeded from outside the daemon. The sweep's primary route (session end releases its own scratch, checks 18 and 22) and its keep cases (live session, a prefixed name the daemon never created, checks 19-21) are shown live; its removal rule is covered by `internal/autonomous/temp_sweep_test.go`.
- **Generality.** The project is a throwaway repository whose default branch is `trunk` and has no `main`; the base repository and the worktree root come from the daemon configuration (`autonomous.worktree_base_repo`, `autonomous.worktree_dir`); the Automata branch namespace is the single constant `AUTOMATON_PREFIX` in this script and `automatonBranchPrefix` in the daemon. Checks 6 and 12 show the base branch being found from git, not assumed.


Result: PASS
