# datawatch v5.26.26 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.25 → v5.26.26
**Patch release** (no binaries — operator directive).
**Closed:** Per-session workspace reaper for daemon-cloned project_profile workspaces.

## What's new

### Daemon-owned workspaces are reaped on session delete

v5.26.21 introduced `project_profile`-driven cloning in `handleStartSession`. The clone target is `<data_dir>/workspaces/<profile>-<8hex>/`, owned by the daemon. Every PRD spawn that referenced a project_profile produced a new clone, but nothing ever removed them — the workspaces directory grew unbounded across PRD lifecycles.

v5.26.26 closes the loop. `Session` gained an `EphemeralWorkspace bool` field (persisted) and `StartOptions` gained a matching field. `handleStartSession` sets it to `true` when it actually creates the clone — never for operator-supplied `project_dir`. On `Manager.Delete`, the reaper runs:

```
if sess.EphemeralWorkspace && sess.ProjectDir != "" {
    // Defense-in-depth: refuse to remove anything outside <data_dir>/workspaces/.
    if filepath.Rel(<data_dir>/workspaces, sess.ProjectDir) is a valid relative path:
        os.RemoveAll(sess.ProjectDir)
}
```

The path safety guard means even a corrupted state file (or an attacker-set `ProjectDir`) can't trick the reaper into deleting an arbitrary directory — the path *must* resolve under `<data_dir>/workspaces/`.

The reaper runs **regardless of `deleteData`**, since the workspace is ephemeral by definition (the operator opted in by using `project_profile` instead of `project_dir`). Operator-supplied `project_dir` is never touched — `EphemeralWorkspace` stays `false`.

### Why Delete instead of Kill

`Manager.Kill` marks a session as killed but keeps it on disk so the operator can inspect logs / `git diff` results. The session — and its workspace — only goes away when the operator explicitly deletes it (or when the autonomous executor cascades a PRD delete through the session list). v5.26.13's REST cascade-delete already walked `SessionIDsForPRD` and called the session-delete path, so PRD lifecycle now reaps clones automatically too.

## Configuration parity

No new config knob — the reaper is automatic when `EphemeralWorkspace` is set, which only happens at the clone site.

## Tests

3 new tests in `internal/session/ephemeral_workspace_test.go`:

| Test | Verifies |
|------|----------|
| `TestDelete_ReapsEphemeralWorkspace` | Clone-managed workspace gets removed on Delete |
| `TestDelete_LeavesOperatorProjectDirAlone` | Operator-supplied `project_dir` is never reaped |
| `TestDelete_RefusesReapOutsideWorkspaceRoot` | Defense-in-depth: path safety guard rejects out-of-bounds reap even when `EphemeralWorkspace=true` |

Total: 458 tests passing across `internal/session`, `internal/server`, `internal/autonomous` (was 455 in v5.26.25; +3 reaper tests).

## Known follow-ups

- **Startup orphan reaper** — if the daemon crashes mid-session, the workspace persists with no Session record to bind it. Adding a startup pass that walks `<data_dir>/workspaces/` and removes directories not referenced by any active session's `ProjectDir` would close the last leak. Tracked in `docs/plans/2026-04-27-v6-prep-backlog.md`.
- agent-goose Dockerfile + CI publish.
- Pre-release security scan automation.
- Kind-cluster smoke workflow.

## Upgrade path

```bash
git pull
datawatch restart
# Existing daemon-cloned workspaces stay until you delete the
# session that owns them. New deletes (REST or PRD-cascade) start
# reaping immediately.
```
