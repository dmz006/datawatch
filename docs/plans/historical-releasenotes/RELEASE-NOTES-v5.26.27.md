# datawatch v5.26.27 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.26 → v5.26.27
**Patch release** (no binaries — operator directive).
**Closed:** Startup orphan-workspace reaper for crash-recovery.

## What's new

### Daemon startup now sweeps orphan project_profile clones

v5.26.26 added per-session reaping in `Manager.Delete` — the happy path is solved. But if the daemon dies mid-session (SIGKILL, OOM, host crash) or someone removes a session record by hand, the cloned workspace under `<data_dir>/workspaces/<profile>-<8hex>/` survives with no live `Session.ProjectDir` pointing to it. Across many crashes those orphans accumulate.

v5.26.27 closes the gap. `Manager.ReapOrphanWorkspaces()` is a new method called once during daemon startup (alongside `ResumeMonitors` and `StartReconciler`):

```
list <data_dir>/workspaces/*
  ↓
build set of every Session.ProjectDir across the store
  ↓
for each direct child not in the set: os.RemoveAll
```

Logs a single line summarizing how many orphans were removed:

```
[session] reaped 3 orphan workspace(s) from previous run
```

### Why match against ALL ProjectDirs, not just EphemeralWorkspace=true

If an operator deliberately set `project_dir` to a path under `<data_dir>/workspaces/` (unusual but possible — e.g. they pre-cloned manually and pointed at it), we shouldn't reap that path just because `EphemeralWorkspace` is `false`. The simple rule "keep what any session points at, remove the rest" handles both cases without requiring the operator to know about the flag.

### Conservative scope

The reaper only sweeps direct children of `<data_dir>/workspaces/`. It doesn't recurse into per-profile subtrees, doesn't touch `<data_dir>/sessions/`, doesn't follow symlinks. Failure modes (permission errors, racing rmdir) log warnings but never block startup.

## Configuration parity

No new config knob — runs unconditionally at startup. Cheap (single readdir + map build).

## Tests

2 new tests in `internal/session/ephemeral_workspace_test.go` (now 5 total in that file):

| Test | Verifies |
|------|----------|
| `TestReapOrphanWorkspaces_RemovesUnreferencedDirs` | 3 dirs in workspaces/, only 1 referenced — reaper removes the other 2 |
| `TestReapOrphanWorkspaces_NoRootDirIsHarmless` | Fresh install (workspaces/ doesn't exist) returns cleanly without error |

Total: 460 tests passing across `internal/session`, `internal/server`, `internal/autonomous` (was 458 in v5.26.26).

## Known follow-ups

Rolled into `docs/plans/2026-04-27-v6-prep-backlog.md`:

- agent-goose Dockerfile + CI publish
- Pre-release security scan automation (`gosec` + `govulncheck`)
- Kind-cluster smoke workflow
- Pinned action SHAs (supply-chain hardening)
- datawatch-app PWA mirror (issue #10)
- v6.0 cumulative release notes
- GHCR past-minor cleanup run

## Upgrade path

```bash
git pull
datawatch restart
# On first restart after v5.26.27, expect a one-line log message
# summarizing any pre-existing orphan workspaces being reaped.
```
