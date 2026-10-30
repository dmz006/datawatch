# Automation cleanup — live check on a sandbox daemon

**Date:** 2026-10-30 (run started 14:31:04, local time)
**Version under test:** datawatch v9.42.1 (binary built from this worktree)
**Script:** `scripts/cleanup-live-check.sh` (run as `env -u TMUX -u TMUX_TMPDIR bash scripts/cleanup-live-check.sh`)
**Scope:** `GET /api/autonomous/cleanup`, the worktrees pass in `report` and then `on` mode, the release when an Automaton is cancelled, the daemon temp-file cleanup of worker-session scratch, the triage record and audit log entries for each removal, and the MCP, CLI and comm-channel reads, against a throwaway project repository whose default branch is `trunk`.

## Summary

| # | Result | Check |
|---|---|---|
| 1 | FAIL | `GET /api/autonomous/cleanup` answers 200 with both passes |
| 2 | PASS | the unset mode runs as report |
| 3 | PASS | the three removable Automaton branches are counted as would-remove and none is removed |
| 4 | PASS | the five held entries (dirty, unmerged, two unfinished, young) are counted as skipped |
| 5 | PASS | report mode removed no branch and no work tree |
| 6 | FAIL | REST, MCP, CLI and comm reads agree (report mode) |
| 7 | PASS | retention 0, mode `sometimes` and disk percent 101 are refused with 400 and nothing is saved |
| 8 | PASS | the pass picked up mode on without a restart |
| 9 | PASS | the three safe entries are gone (two with their work trees, one branch only) |
| 10 | PASS | the dirty, unmerged, unfinished and young entries and the operator's own branch were all kept |
| 11 | PASS | the cleanup judged merged work against `trunk` (the repository has no `main`) |
| 12 | PASS | each of the three removals is in the audit log (action `autonomous_worktree_remove`, with branch, base branch, age and bytes freed) |
| 13 | PASS | each removal is in the Automaton's triage record |
| 14 | PASS | the daemon log carries a summary line for the worktrees pass |
| 15 | FAIL | cancelling an Automaton whose work is already on the default branch released its branch and work tree at once |
| 16 | PASS | each worker session left its own prefixed scratch file in the daemon's temp folder |
| 17 | PASS | the scratch file of the ended session is gone (session end released it) |
| 18 | PASS | the scratch file of the live session is still there |
| 19 | PASS | a pass in mode on kept the live session's file and a prefixed name the daemon never created |
| 20 | PASS | the pass counted the two names it kept (the live session's file and the foreign name) as skipped |
| 21 | PASS | once the live session ended, its file went too |
| 22 | FAIL | REST, MCP, CLI and comm reads agree (on mode) |
| 23 | PASS | the sandbox daemon, its sessions and its tmux server are gone and no test daemon was left behind |

**Checks:** 19 passed, 4 failed.

## Setup

- **Sandbox daemon only.** Started with `td_start` from `scripts/test-daemon-lib.sh` from a config generated from `testdata/datawatch.yaml` by the substitutions the test runner uses (data folder, ports, loopback host, test token). Added: `autonomous.worktree_base_repo` and `autonomous.worktree_dir` (the throwaway project and its worktree root), `server.allowed_project_roots` (one folder for the two worker sessions) and an `aider` backend entry that points at a stand-in worker. The production daemon, its service and its data were not touched, and nothing was ended by name.
- **Ports.** HTTP 34067, TLS 53135, MCP 58039, DNS 50391, webhook 42547, notify 57447 — each probed free just before use.
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
automaton/c0a10001-release-completed fbeebc6
automaton/c0a10002-release-cancelled 4e76cc3
automaton/c0a10003-release-branch-only 2540da4
automaton/c0a10004-held-dirty 096b13d
automaton/c0a10005-held-unmerged 7614571
automaton/c0a10006-held-unfinished 86e0669
automaton/c0a10007-held-young 188ea6a
automaton/c0a10008-end-release 1c86163
feature/user-owned 57e1087
trunk a02b71b
---
/tmp/dw-cleanup-live.Fktz3E/project                              a02b71b [trunk]
/tmp/dw-cleanup-live.Fktz3E/worktrees/c0a10001-release-completed fbeebc6 [automaton/c0a10001-release-completed]
/tmp/dw-cleanup-live.Fktz3E/worktrees/c0a10002-release-cancelled 4e76cc3 [automaton/c0a10002-release-cancelled]
/tmp/dw-cleanup-live.Fktz3E/worktrees/c0a10004-held-dirty        096b13d [automaton/c0a10004-held-dirty]
/tmp/dw-cleanup-live.Fktz3E/worktrees/c0a10005-held-unmerged     7614571 [automaton/c0a10005-held-unmerged]
/tmp/dw-cleanup-live.Fktz3E/worktrees/c0a10006-held-unfinished   86e0669 [automaton/c0a10006-held-unfinished]
/tmp/dw-cleanup-live.Fktz3E/worktrees/c0a10007-held-young        188ea6a [automaton/c0a10007-held-young]
/tmp/dw-cleanup-live.Fktz3E/worktrees/c0a10008-end-release       1c86163 [automaton/c0a10008-end-release]
/tmp/dw-cleanup-live.Fktz3E/worktrees/user-owned                 57e1087 [feature/user-owned]

```

Sandbox daemon pid `3387444` (recorded by the lifecycle helper) was healthy at 14:31:08.

## 1. Report mode: would-remove is counted, nothing is removed

**Check 1 — FAIL:** `GET /api/autonomous/cleanup` answers 200 with both passes

HTTP 503, body: `Automaton cleanup status unavailable`. The checks below read the daemon's persisted copy of the same status instead (the file the daemon writes beside its merge record), so the passes themselves can still be judged.

Static diagnosis of the unavailable route (what the server can call on the object it holds):
```
14:58-	cr, ok := s.autonomousMgr.(cleanupStatusReader)
16:60-		http.Error(w, "Automaton cleanup status unavailable", http.StatusServiceUnavailable)
--- the server holds *autonomous.API (SetAutonomousAPI); its pass-through methods for the merge reads and for the cleanup read:
1751:func (a *API) MergeStatus() MergeStatus { return a.M.MergeStatus() }
1752:func (a *API) MergeSafeToDelete(ctx context.Context) (SafeDeleteReport, error) {
--- the method the route needs:
377:func (m *Manager) CleanupStatus() CleanupStatus {

```

Cleanup status (read from: the persisted status file, REST unavailable), mode left at its default:
```
{
  "worktrees": {
    "mode": "report",
    "last_run": "2026-10-30T18:31:07.458111176Z",
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
    "last_run": "2026-10-30T18:31:07.458204937Z",
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
    "path": "/tmp/dw-cleanup-live.Fktz3E/project",
    "percent": 47,
    "alert": false
  },
  "temp_disk": {
    "path": "/tmp/dw-cleanup-live.Fktz3E/daemon-tmp",
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
2026/10/30 14:31:07 [autonomous] worktree-cleanup pass: mode="report" removed=0 would_remove=3 skipped=5 failed=0
2026/10/30 14:31:07 [autonomous] temp-sweep: mode="report" considered=0 removed=0 would_remove=0 skipped=0 failed=0 bytes_freed=0

```

`GET /api/autonomous/safe-to-delete` on the same project (for comparison with the cleanup's own count):
```
{"safe":[],"held":[{"branch":"automaton/c0a10001-release-completed","worktree":"/tmp/dw-cleanup-live.Fktz3E/worktrees/c0a10001-release-completed","state":"unknown","note":"Automaton merge compare: rev-list: exit status 128: fatal: ambiguous argument 'main..automaton/c0a10001-release-completed': unknown revision or path not in the working tree.\nUse '--' to separate paths from revisions, like this:\n'git <command> [<revision>...] -- [<file>...]'"},{"branch":"automaton/c0a10002-release-cancelled","worktree":"/tmp/dw-cleanup-live.Fktz3E/worktrees/c0a10002-release-cancelled","state":"unknown","note":"Automaton merge compare: rev-list: exit status 128: fatal: ambiguous argument 'main..automaton/c0a10002-release-cancelled': unknown revision or path not in the working tree.\nUse '--' to separate paths from revisions, like this:\n'git <command> [<revision>...] -- [<file>...]'"},{"branch":"automaton/c0a10003-release-branch-only","worktree":"","state":"unknown","note":"Automaton merge compare: rev-list: exit status 128: fatal: ambiguous argument 'main..automaton/c0a10003-release-branch-only': unknown revision or path not in the working tree.\nUse '--' to separate paths from revisions, like this:\n'git <command> [<revision>...] -- [<file>...]'"},{"branch":"automaton/c0a10004-held-dirty","worktree":"/tmp/dw-cleanup-live.Fktz3E/worktrees/c0a10004-held-dirty","state":"unknown","note":"Automaton merge compare: rev-list: exit status 128: fatal: ambiguous argument 'main..automaton/c0a10004-held-dirty': unknown revision or path not in the working tree.\nUse '--' to separate paths from revisions, like this:\n'git <command> [<revision>...] -- [<file>...]'"},{"branch":"automaton/c0a10005-held-unmerged","worktree":"/tmp/dw-cleanup-live.Fktz3E/worktrees/c0a10005-held-unmerged","state":"unknown","note":"Automaton merge compare: rev-list: exit status 128: fatal: ambiguous argument 'main..automaton/c0a10005-held-unmerged': unknown revision or path not in the working tree.\nUse '--' to separate paths from revisions, like this:\n'git <command> [<revision>...] -- [<file>...]'"},{"branch":"automaton/c0a10006-held-unfinished","worktree":"/tmp/dw-cleanup-live.Fktz3E/worktrees/c0a10006-held-unfinished","state":"unknown","note":"Automaton merge compare: rev-list: exit status 128: fatal: ambiguous argument 'main..automaton/c0a10006-held-unfinished': unknown revision or path not in the working tree.\nUse '--' to separate paths from revisions, like this:\n'git <command> [<revision>...] -- [<file>...]'"},{"branch":"automaton/c0a10007-held-young","worktree":"/tmp/dw-cleanup-live.Fktz3E/worktrees/c0a10007-held-young","state":"unknown","note":"Automaton merge compare: rev-list: exit status 128: fatal: ambiguous argument 'main..automaton/c0a10007-held-young': unknown revision or path not in the working tree.\nUse '--' to separate paths from revisions, like this:\n'git <command> [<revision>...] -- [<file>...]'"},{"branch":"automaton/c0a10008-end-release","worktree":"/tmp/dw-cleanup-live.Fktz3E/worktrees/c0a10008-end-release","state":"unknown","note":"Automaton merge compare: rev-list: exit status 128: fatal: ambiguous argument 'main..automaton/c0a10008-end-release': unknown revision or path not in the working tree.\nUse '--' to separate paths from revisions, like this:\n'git <command> [<revision>...] -- [<file>...]'"}],"held_dirty":0}

```

## 2. The four reads agree (report mode)

CLI text:
```


```

Comm-channel reply:
```
[dw-e2e-test] autonomous cleanup failed
HTTP 503: Automaton cleanup status unavailable

```

MCP result (first 600 bytes):
```
autonomous_cleanup_status: HTTP 503: Automaton cleanup status unavailable

```

**Check 6 — FAIL:** REST, MCP, CLI and comm reads agree (report mode)

REST: `GET /api/autonomous/cleanup`; CLI: `datawatch autonomous cleanup --json` and `datawatch autonomous cleanup`; MCP: `autonomous_cleanup_status` over `datawatch mcp` (stdio); comm: `POST /api/test/message {"text":"autonomous cleanup"}`. Comparison result: `a read did not return the status: REST said 'Automaton cleanup status unavailable', MCP said 'autonomous_cleanup_status: HTTP 503: Automaton cleanup status unavailable'`; CLI stderr: Error: HTTP 503: Automaton cleanup status unavailable

Error: HTTP 503: Automaton cleanup status unavailable

## 3. A setting that is not allowed is refused and saves nothing

**Check 7 — PASS:** retention 0, mode `sometimes` and disk percent 101 are refused with 400 and nothing is saved

HTTP 400 / 400 / 400; automation settings identical before and after

Note: the brief asked for "retention 0". The smallest retention the cleanup accepts is 1 hour (0 is refused above, and an unset or hand-edited 0 reads as the 24-hour default), so the on-mode step below uses 1 hour and the sample Automata are 72 hours old.

## 4. On mode: only the safe entries are removed

`PUT /api/config` (retention 1 hour, mode on):
```
{"status":"ok"}

```

Cleanup status after the pass (read from: the persisted status file, REST unavailable):
```
{
  "worktrees": {
    "mode": "on",
    "last_run": "2026-10-30T18:32:07.466924505Z",
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
    "last_run": "2026-10-30T18:32:07.466981761Z",
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
    "path": "/tmp/dw-cleanup-live.Fktz3E/project",
    "percent": 47,
    "alert": false
  },
  "temp_disk": {
    "path": "/tmp/dw-cleanup-live.Fktz3E/daemon-tmp",
    "percent": 47,
    "alert": false
  },
  "disk_alert": false
}

```

**Check 8 — PASS:** the pass picked up mode on without a restart

mode=on

**Check 9 — PASS:** the three safe entries are gone (two with their work trees, one branch only)

removed=3 would_remove=0 skipped=5 failed=0

**Check 10 — PASS:** the dirty, unmerged, unfinished and young entries and the operator's own branch were all kept

remaining branches: automaton/c0a10004-held-dirty automaton/c0a10005-held-unmerged automaton/c0a10006-held-unfinished automaton/c0a10007-held-young automaton/c0a10008-end-release feature/user-owned trunk 

**Check 11 — PASS:** the cleanup judged merged work against `trunk` (the repository has no `main`)

HEAD of the base repository: trunk

Audit log, `GET /api/audit?action=autonomous_worktree_remove`:
```
{"at":"2026-10-30T14:32:07.465503945-04:00","actor":"autonomous","action":"autonomous_worktree_remove","branch":"automaton/c0a10003-release-branch-only","bytes_freed":0,"base_branch":"trunk","age_seconds":259263}
{"at":"2026-10-30T14:32:07.458073974-04:00","actor":"autonomous","action":"autonomous_worktree_remove","branch":"automaton/c0a10002-release-cancelled","bytes_freed":127,"base_branch":"trunk","age_seconds":259263}
{"at":"2026-10-30T14:32:07.450442802-04:00","actor":"autonomous","action":"autonomous_worktree_remove","branch":"automaton/c0a10001-release-completed","bytes_freed":110,"base_branch":"trunk","age_seconds":259263}

```

**Check 12 — PASS:** each of the three removals is in the audit log (action `autonomous_worktree_remove`, with branch, base branch, age and bytes freed)

automaton/c0a10001-release-completed automaton/c0a10002-release-cancelled automaton/c0a10003-release-branch-only

**Check 13 — PASS:** each removal is in the Automaton's triage record

c0a10001: work branch automaton/c0a10001-release-completed and its work tree removed (110 bytes freed): no commits ahead of main; c0a10002: work branch automaton/c0a10002-release-cancelled and its work tree removed (127 bytes freed): no commits ahead of main; c0a10003: work branch automaton/c0a10003-release-branch-only and its work tree removed (0 bytes freed): no commits ahead of main;  (read 0 s after the branch was seen gone)

Daemon log, cleanup lines so far (the log holds 118 lines, 13 of them from the Automata engine):
```
2026/10/30 14:31:07 [autonomous] worktree-cleanup pass: mode="report" removed=0 would_remove=3 skipped=5 failed=0
2026/10/30 14:31:07 [autonomous] temp-sweep: mode="report" considered=0 removed=0 would_remove=0 skipped=0 failed=0 bytes_freed=0
2026/10/30 14:32:07 [autonomous] worktree-cleanup pass: mode="on" removed=3 would_remove=0 skipped=5 failed=0
2026/10/30 14:32:07 [autonomous] temp-sweep: mode="on" considered=0 removed=0 would_remove=0 skipped=0 failed=0 bytes_freed=0

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
2026/10/30 14:31:07 [autonomous] worktree-cleanup pass: mode="report" removed=0 would_remove=3 skipped=5 failed=0
2026/10/30 14:31:07 [autonomous] temp-sweep: mode="report" considered=0 removed=0 would_remove=0 skipped=0 failed=0 bytes_freed=0
2026/10/30 14:32:07 [autonomous] worktree-cleanup pass: mode="on" removed=3 would_remove=0 skipped=5 failed=0
2026/10/30 14:32:07 [autonomous] temp-sweep: mode="on" considered=0 removed=0 would_remove=0 skipped=0 failed=0 bytes_freed=0

```

Log files the sandbox daemon wrote:
```
daemon.log: 3033 bytes, 47 lines, 0 cleanup summary lines
data/access.log: 8794 bytes, 42 lines, 0 cleanup summary lines
data/audit.log: 3533 bytes, 13 lines, 0 cleanup summary lines
data/daemon-crash.log: 1451 bytes, 16 lines, 4 cleanup summary lines

```

**Check 14 — PASS:** the daemon log carries a summary line for the worktrees pass

2026/10/30 14:32:07 [autonomous] worktree-cleanup pass: mode="on" removed=3 would_remove=0 skipped=5 failed=0 — found in: data/daemon-crash.log 

## 5. Release at end: cancelling an Automaton calls the same release

`POST /api/autonomous/prds/c0a10008/cancel` → HTTP 200, Automaton status `cancelled`. Daemon log (release lines):
```
2026/10/30 14:32:07 [autonomous] worktree-cleanup release: mode="on" removed=0 would_remove=0 skipped=1 failed=0

```

Cancelled entry, `updated_at`, branch state and the worktrees counters before and after (read from: the persisted status file, REST unavailable):
```
updated_at=2026-10-30T14:32:07.768581678-04:00; entry age at cancel: under 1 minute; branch exists: yes
before: {"last_run":"2026-10-30T18:32:07.466924505Z","skipped":5,"owner_removed":0,"net_removed":3}
after:  {"last_run":"2026-10-30T18:32:07.793826931Z","skipped":1,"owner_removed":0,"net_removed":3}

```

Audit log, the release outcome the cancel wrote (`autonomous_worktree_cleanup_release*`):
```
{"at":"2026-10-30T14:32:07.793810436-04:00","action":"autonomous_worktree_cleanup_release","details":{"failed":0,"mode":"on","removed":0,"skipped":{"retained":1},"would_remove":0}}

```

**Check 15 — FAIL:** cancelling an Automaton whose work is already on the default branch released its branch and work tree at once

cancel HTTP 200, status cancelled, branch still exists: yes; release log: 2026/10/30 14:32:07 [autonomous] worktree-cleanup release: mode="on" removed=0 would_remove=0 skipped=1 failed=0

## 6. Daemon temp files: kept while the session is live, gone when it ends
- Two worker sessions on the `aider` backend (a stand-in that sleeps): live id `dw-e2e-test-d5fb`, to-be-ended id `dw-e2e-test-7a32`.

Scratch files in the daemon's temp folder after both sessions started:
```
datawatch-aider-task-cs-dw-e2e-test-7a32-289665444.txt
datawatch-aider-task-cs-dw-e2e-test-d5fb-3261849974.txt
datawatch-foreign-check
node-compile-cache
opencode

```

**Check 16 — PASS:** each worker session left its own prefixed scratch file in the daemon's temp folder

live: `datawatch-aider-task-cs-dw-e2e-test-d5fb-3261849974.txt`; to-be-ended: `datawatch-aider-task-cs-dw-e2e-test-7a32-289665444.txt`

**Check 17 — PASS:** the scratch file of the ended session is gone (session end released it)

kill HTTP 200; owner_removed on the temp pass: 1

**Check 18 — PASS:** the scratch file of the live session is still there

datawatch-aider-task-cs-dw-e2e-test-d5fb-3261849974.txt

Status after another pass with a live session and a foreign name present:
```
{"worktrees":{"mode":"on","removed":0,"would_remove":0,"skipped":5,"failed":0,"owner_removed":0,"net_removed":3},"temp":{"mode":"on","removed":0,"would_remove":0,"skipped":2,"failed":0,"owner_removed":1,"net_removed":0},"disk_alert":false,"disk_alert_percent":85}

```

**Check 19 — PASS:** a pass in mode on kept the live session's file and a prefixed name the daemon never created

temp: removed=0 skipped=2 failed=0

**Check 20 — PASS:** the pass counted the two names it kept (the live session's file and the foreign name) as skipped

temp skipped=2

**Check 21 — PASS:** once the live session ended, its file went too

kill HTTP 200

Not shown live: removal by the periodic temp sweep itself. It only removes scratch of a session that ended at least the retention window ago (minimum 1 hour), so a sweep removal cannot happen inside a short check. The sweep's kept cases (live session, foreign name) are shown above; its removal rule is covered by the unit tests in `internal/autonomous/temp_sweep_test.go`.

## 7. The four reads agree (on mode, after the removals)

CLI text:
```


```

Comm-channel reply:
```
[dw-e2e-test] autonomous cleanup failed
HTTP 503: Automaton cleanup status unavailable

```

MCP result (first 600 bytes):
```
autonomous_cleanup_status: HTTP 503: Automaton cleanup status unavailable

```

**Check 22 — FAIL:** REST, MCP, CLI and comm reads agree (on mode)

REST: `GET /api/autonomous/cleanup`; CLI: `datawatch autonomous cleanup --json` and `datawatch autonomous cleanup`; MCP: `autonomous_cleanup_status` over `datawatch mcp` (stdio); comm: `POST /api/test/message {"text":"autonomous cleanup"}`. Comparison result: `a read did not return the status: REST said 'Automaton cleanup status unavailable', MCP said 'autonomous_cleanup_status: HTTP 503: Automaton cleanup status unavailable'`; CLI stderr: Error: HTTP 503: Automaton cleanup status unavailable

Error: HTTP 503: Automaton cleanup status unavailable

## Cleanup

- **Sandbox daemon ended** by `td_stop_all` (recorded pid `3387444`, marker = its config path). Process gone: yes. TLS port 53135 closed: yes.
- **Private tmux server** (explicit throwaway socket) ended; a session left on it: no.
- **Leftover check** against the snapshot taken before this run: `scripts/check-no-leftover-test-daemons.sh check` exit 0 — OK: no leftover test daemons

**Check 23 — PASS:** the sandbox daemon, its sessions and its tmux server are gone and no test daemon was left behind

daemon gone: yes; port closed: yes; private tmux session left: no; leftover check exit 0
- **Scratch area** (project, remote, work trees, data, config, log, daemon records, shims) is removed by the exit handler right after this report is written.

## Findings and mismatches

Written by hand after the final run. Checks 1, 6 and 22 fail for one reason (finding 1) and check 15 for another (finding 2); every other check passed. Nothing here was fixed: this check only reads, by design.

1. **The status read is unavailable on a real daemon (checks 1, 6, 22).** `GET /api/autonomous/cleanup`, the `autonomous_cleanup_status` MCP tool, `datawatch autonomous cleanup` and the `autonomous cleanup` comm verb all answer `HTTP 503: Automaton cleanup status unavailable`. Cause, confirmed by the static diagnosis block in step 1: the route asserts that the object the server holds (`*autonomous.API`, wired by `SetAutonomousAPI`) has a `CleanupStatus()` method (`internal/server/cleanup.go`, `cleanupStatusReader`). `*autonomous.API` has pass-through methods for the merge reads (`MergeStatus`, `MergeSafeToDelete`, `internal/autonomous/api.go:1751-1752`) but none for the cleanup, so the assertion fails and the route answers 503 for every caller. `Manager.CleanupStatus()` exists and works: the daemon's persisted copy of the same status (the file it writes beside its merge record) was read instead and gave the numbers used in the other checks. The unit tests use a fake server object that has the method, which is why they could not see this. The expected fix is one pass-through method on `*autonomous.API`, next to `MergeStatus`; until then none of the four reads can be compared, so checks 6 and 22 cannot pass.
2. **Release at end never removes anything (check 15).** Cancelling `c0a10008` (finished by cancel, work already on `trunk`, clean work tree) called the release at once: the daemon log shows `worktree-cleanup release: mode="on" removed=0 would_remove=0 skipped=1` and the audit entry `autonomous_worktree_cleanup_release` carries `"skipped":{"retained":1}`. The release applies the retention window (minimum 1 hour, default 24) to the entry's age, and the age is the time since the entry was last updated, which the cancel (or completion) has just set; so at the end of an Automaton the age is about zero and the branch is always held. The same function is called from the completion path and the merge pass, so the same result is expected there (not exercised live: no route completes an Automaton without running sessions). Consequence: `owner_removed` stays 0 for the worktrees pass and every removal waits for the periodic safety net, one retention window later. This contradicts the how-to ("removed when the Automaton completes or is cancelled"). The hold is recorded correctly and the next pass would remove the branch once the entry is old enough (the same branch shape was removed by the net in step 4), so nothing is lost; whether the release should skip the retention window is the owner's decision.
3. **The merge and safe-to-delete reads assume the default branch is `main` (outside the cleanup, found because the project's default is `trunk`).** `mergePendingBase = "main"` (`internal/autonomous/merge_pending_pass.go:33`). In the throwaway project `GET /api/autonomous/safe-to-delete` returned an empty safe list and every branch as `unknown` (`ambiguous argument 'main..automaton/...'`, shown in step 1), while the cleanup passes, which find the default branch from git (`refs/remotes/origin/HEAD`), judged all of it correctly and removed exactly the safe entries. The triage notes the cleanup wrote also say "no commits ahead of main" (check 13) for a repository whose default is `trunk`, because the reason text comes from the same report. The operator's generality rule ("not tied to this repository") is not met by those reads or by that wording.
4. **"Retention 0" is not a valid setting.** The brief asked for the on-mode step with retention 0. The smallest accepted value is 1 hour (0 is refused with 400, check 7; a hand-edited 0 reads as the 24-hour default). Step 4 therefore uses 1 hour with Automata whose entries are 72 hours old (written into the sandbox's own Automaton store before its first start, because no route creates a finished Automaton with a work branch; the entry age is the stored `updated_at`).
5. **"Lists the merged ones as would-remove".** The status holds counts, not names (`would_remove: 3`). Which three is shown by what step 4 then removes (and by the audit entries), not by the report-mode read. Naming the entries in the status, or giving the safe-to-delete read the default branch (finding 3), would let an operator see them before switching to `on`.
6. **Temp sweep removal is not shown live.** The sweep removes scratch only for a session that ended at least the retention window ago (at least 1 hour), which cannot happen inside a short check, and its record of what the daemon created lives in memory. What the check does show with a real session: the session's task file is in the daemon's temp folder while it runs (a stand-in `aider` worker that sleeps, on the `aider` backend); ending the session removes the file at once (the session-end owner path, `temp.owner_removed` 1); a pass in `on` mode keeps a live session's file and a `datawatch-`-prefixed folder the daemon never created, and counts both as skipped (`temp.skipped` 2); ending the second session removes its file too. The sweep's removal rule is covered by `internal/autonomous/temp_sweep_test.go`.
7. **Where the summary lines go.** The daemon was started in the foreground; its standard log output landed in `data/daemon-crash.log` (the "Log files the sandbox daemon wrote" block in step 4), not in `data/daemon.log`, which `GET /api/logs` reads. The `worktree-cleanup` and `temp-sweep` summary lines are there (check 14). Not investigated further.
8. **Not exercised:** mode `off`, the per-pass removal limit, a live session or lease holding a work tree, the disk alert, and a cleanup of a repository with a `master` default. The held cases that were exercised are dirty work tree, unmerged branch, unfinished (paused) Automaton, entry inside the retention window, and a branch outside the `automaton/` namespace; each was left exactly as it was (branch, work tree and the uncommitted file).

## End state

The Cleanup section above records the end state of the last run. After the final run: no `dw-cleanup-live.*` folder remains under `/tmp`, the sandbox daemon pid was gone and its ports closed, the private tmux socket was gone, and the leftover check against the pre-run snapshot reported nothing new. The production daemon and the operator's tmux server were not touched at any point.

Result: FAIL
