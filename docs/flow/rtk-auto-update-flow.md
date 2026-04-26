# RTK auto-update flow

How datawatch keeps the bundled `rtk` binary current and surfaces the
"update available" signal to operators without auto-clobbering anything.

```
   ┌─── parent daemon startup ────────────────────────────────────────┐
   │                                                                  │
   │  internal/rtk.CheckInstalled()                                   │
   │      └─→ shells out to `rtk --version`, parses semver            │
   │                                                                  │
   │  if cfg.RTK.AutoUpdateCheck:                                     │
   │      go internal/rtk.StartUpdateChecker(interval, autoUpdate,    │
   │                                          callback)               │
   │                                                                  │
   └────────────────────────────┬─────────────────────────────────────┘
                                │
                                ▼
   ┌─── background goroutine — every cfg.RTK.CheckInterval (24h) ────┐
   │                                                                  │
   │  internal/rtk.CheckLatestVersion()                               │
   │      └─→ GET https://api.github.com/repos/rtk-ai/rtk/releases    │
   │              /latest  (no auth needed for public repos)          │
   │                                                                  │
   │      VersionStatus{ Installed, Available, UpdateAvailable }      │
   │                                                                  │
   │      ▼                                                           │
   │  callback(status)                                                │
   │      ├─→ /api/rtk/version cache write                            │
   │      ├─→ alert publish (PWA toast + Signal/etc. when configured) │
   │      └─→ if cfg.RTK.AutoUpdate and status.UpdateAvailable:       │
   │              internal/rtk.UpdateBinary()                         │
   │                                                                  │
   └─────────────────────────┬────────────────────────────────────────┘
                             │
                             ▼
   ┌─── operator-triggered update (PWA / mobile / Signal) ───────────┐
   │                                                                  │
   │  POST /api/rtk/update                                            │
   │      │                                                           │
   │      ▼                                                           │
   │  internal/rtk.UpdateBinary()                                     │
   │      • download release tarball for $TARGETARCH                  │
   │      • verify SHA-256                                            │
   │      • atomic replace via tempfile + rename                      │
   │      • chmod 755                                                 │
   │      │                                                           │
   │      ▼                                                           │
   │  return new version string                                       │
   │                                                                  │
   │  PWA Settings → Monitor card refreshes /api/rtk/version          │
   │                                                                  │
   └──────────────────────────────────────────────────────────────────┘
```

## Operator surfaces

| Surface | Endpoint / control |
|---|---|
| PWA Monitor card | live `installed` / `available` / `update_available` |
| Signal / Telegram | `rtk version` (status); upgrades use the install one-liner: `curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh \| sh` |
| CLI | `datawatch setup rtk` (install or update) |
| MCP | (none — admin-only operation) |
| Manual | `POST /api/rtk/update` |

## Failure modes

| Symptom | Likely cause | Fix |
|---|---|---|
| `update_available: false` after a known release | GitHub API rate limit | back off; check `last_check` timestamp |
| 502 on `/api/rtk/update` | Network / SHA mismatch | retry; check release artifact integrity |
| Old version still reported after update | `$PATH` cache | `hash -r` + retry; ensure `~/.local/bin` precedes any older path |
| 409 "already up to date" | Race with the background checker | benign — ignore |

## Related

- Endpoint specs: [`docs/api/openapi.yaml` → `/api/rtk/*`](../api/openapi.yaml)
- RTK integration overview: [`docs/rtk-integration.md`](../rtk-integration.md)
- Background goroutine: `internal/rtk/update.go` → `StartUpdateChecker()`
- REST handlers: `internal/server/rtk.go`
