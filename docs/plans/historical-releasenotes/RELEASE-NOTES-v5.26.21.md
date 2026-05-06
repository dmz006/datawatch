# datawatch v5.26.21 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.20 → v5.26.21
**Patch release** (no binaries — operator directive).
**Closed:** Daemon-side clone for `project_profile` + local session.

## What's new

### `POST /api/sessions/start` honors `project_profile`

This closes the v5.26.19 follow-up. Previously when an autonomous PRD had `project_profile` set without `cluster_profile`, the autonomous executor passed `project_profile` to the local-session spawn but the handler ignored it — operators needed to pre-clone manually.

v5.26.21: when `project_profile` is provided and `project_dir` is empty, the handler:

1. Resolves the named profile via `s.projectStore.Get`.
2. Generates a per-spawn workspace path: `<data_dir>/workspaces/<profile_name>-<8hex>/`. The 8-hex random suffix prevents collision when the same profile is reused across simultaneous PRD spawns (autonomous + multi-task).
3. Shells out to `git clone --depth 1 [--branch <branch>] <profile.Git.URL> <workspace>` with a 5-minute timeout context tied to the request.
4. Sets `req.ProjectDir = workspace` so the rest of the handler proceeds as if the operator supplied the directory directly.

### Error semantics

| Failure | HTTP |
|---|---|
| Profile subsystem not wired | 400 |
| Profile name not found | 400 |
| Profile has no `git.url` | 400 |
| Git clone failure (auth, network, branch missing) | 502 with stderr in body |
| Workspace dir create failure | 500 |

The autonomous executor's spawn callback sees the error verbatim — task gets `TaskFailed` with the clone error in the audit trail rather than running against the wrong directory.

### Auth model

Cloning uses whatever auth the daemon's user has locally:

- SSH URLs (`git@host:owner/repo.git`) — uses the daemon user's SSH agent or `~/.ssh/`.
- HTTPS public — works without auth.
- HTTPS with embedded token (`https://x-token:GH_TOKEN@github.com/...`) — works.
- HTTPS with git credential helper — works if the credential is in scope for the daemon's user.

F10 BL113 token-broker integration (which mints short-lived per-spawn tokens) lands as the v5.26.22 follow-up. For now operators with private repos on tokens-only providers should embed the token in the profile's git URL.

### Workspace lifecycle

Workspaces persist after the session ends so the operator can inspect the worker's edits. Cleanup is operator-driven (`rm -rf ~/.datawatch/workspaces/<dir>`) until a per-session reaper lands. This matches the existing F10 cluster-spawn semantics where worker Pods leave artifacts after termination.

## Configuration parity

No new config knob. The clone-on-spawn is wholly derived from the existing project profile fields.

## Tests

- 1404 + 6 (v5.26.19) Go unit tests passing.
- Smoke unaffected: 32 PASS / 0 FAIL / 3 SKIP.

## Known follow-ups

- **F10 BL113 token-broker integration** (v5.26.22) — mint short-lived per-spawn git tokens via the existing F10 broker so operators don't have to embed tokens in profile URLs.
- **Per-session workspace reaper** — drop the workspace directory when the session is hard-killed or completes; currently operator-driven.
- v6.0 packaging items unchanged.

## Upgrade path

```bash
git pull
datawatch restart
# Create a project profile pointing at a public or token-embedded git URL:
#   curl -X POST -H 'Content-Type: application/json' \
#     -d '{"name":"myproj","git":{"url":"https://github.com/foo/bar","branch":"main"},"image_pair":{"agent":"agent-claude"}}' \
#     https://localhost:8443/api/profiles/projects
# New PRD via PWA: pick "myproj" in the project profile dropdown,
# leave cluster profile as "(local)" and project_dir empty.
# When the executor spawns, the daemon clones the repo into
# ~/.datawatch/workspaces/myproj-XXXXXXXX/ and the worker runs there.
```
