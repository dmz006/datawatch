# datawatch v5.26.24 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.23 → v5.26.24
**Patch release** (no binaries — operator directive).
**Closed:** BL113 token broker integration for daemon-side `project_profile` clone.

## What's new

### Daemon-side clone now uses per-spawn ephemeral tokens

v5.26.21 added `project_profile`-driven clone in `handleStartSession` so a PRD spawn could pull the operator's repo before claude/opencode/aider/codex started. v5.26.22 abstracted the credential sourcing so it works in k8s. Both relied on a long-lived `DATAWATCH_GIT_TOKEN` env in the Pod — the same shape we'd already removed from cluster-spawn workers in F10 S5.1+S5.3 by routing through the BL113 token broker.

v5.26.24 closes the parity gap. The broker constructed during agent-manager auth setup is now also wired into `HTTPServer`, so the daemon-side clone path mints a 5-minute scoped token, uses it, and revokes immediately:

```
operator POST /api/sessions/start  { project_profile: "webapp", ... }
  ├── resolve project profile
  ├── if cfg.AuthBroker.Enabled and HTTPS clone URL:
  │     broker.MintForWorker(ctx, "clone:<sess>", "owner/repo", 5m)
  │     inject into clone URL  →  git clone
  │     defer broker.RevokeForWorker(...)
  └── else: fall back to DATAWATCH_GIT_TOKEN env (legacy / dev)
```

Net effect for k8s: you can now drop `gitToken.existingSecret` from the chart and let the broker provider (currently github via `gitpkg.Resolve`) handle credential exchange. SSH URLs still bypass the rewrite — they always use the mounted SSH key. Local dev with no broker is unchanged: `DATAWATCH_GIT_TOKEN` (or no token at all + git's local credential helper) still works.

### Wire-up

The broker is constructed early in `main.go` (around the agent-manager setup) but `HTTPServer` is constructed much later in startup. v5.26.24 adds a `pendingGitMinter` capture variable so the broker adapter survives the gap, and a `httpServer.SetGitTokenMinter(pendingGitMinter)` call right after `server.New(...)`.

```go
// early — broker built for agents.Manager.GitTokenMinter
agentMgr.GitTokenMinter = brokerAdapter{broker}
pendingGitMinter = brokerAdapter{broker}   // ← new

// later — same adapter wired into the daemon-side clone path
httpServer = server.New(...)
if pendingGitMinter != nil {
    httpServer.SetGitTokenMinter(pendingGitMinter)
}
```

`brokerAdapter` already satisfies both `agents.GitTokenMinter` and the new `server.GitTokenMinter` (same two-method shape — `MintForWorker` / `RevokeForWorker`).

## Configuration parity

No new config knob — the broker either exists or doesn't, and its presence is determined by whether the token store opens cleanly during startup (same gate as the existing cluster-spawn flow).

For k8s deployments wanting per-spawn tokens, the chart already exposes:

| Value | Meaning |
|------|---------|
| `gitToken.existingSecret` | (legacy) long-lived PAT mounted as `DATAWATCH_GIT_TOKEN` env. Still works. |
| (no value, broker enabled) | daemon mints/revokes scoped tokens via broker provider. |

Documented in `docs/howto/setup-and-install.md` (v5.26.22).

## Tests

- 9 of 9 `git_auth_test.go` tests still passing (token-injection + redaction).
- 455 tests passing across `internal/server`, `internal/autonomous`, `internal/session`.
- Smoke unaffected — broker integration is opaque to the existing PRD-spawn path.

## Known follow-ups

- Per-session workspace reaper (clones persist in `<data_dir>/workspaces/` after session ends).
- v6.0 packaging items unchanged.

## Upgrade path

```bash
git pull
datawatch restart
# In k8s: drop gitToken.existingSecret from values.yaml on next chart upgrade
# if you want broker-only ephemeral tokens. Existing long-lived env still works.
```
