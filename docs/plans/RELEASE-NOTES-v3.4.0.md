# Release Notes — v3.4.0 (Operations group complete)

**2026-04-19.** Operations backlog group ships **all four items**:
**BL17** (hot config reload), **BL22** (RTK auto-install),
**BL37** (system diagnostics), **BL87** (`datawatch config edit`).

---

## Highlights

- **BL17 — Hot config reload.** `POST /api/reload` and `SIGHUP`
  re-read `config.yaml` and re-apply the hot-reloadable subset
  without daemon restart. Currently re-applies
  `session.schedule_settle_ms` and `session.mcp_max_retries` on
  the live `Manager`. Config struct is swapped wholesale so
  GET-style handlers see new values immediately. Fields requiring
  restart (`server.host/port`, `mcp.sse_*`, `signal.*`,
  `agents.*`, database) are listed in the response under
  `requires_restart`.
- **BL22 — RTK auto-install.** `datawatch setup rtk` now downloads
  the platform-matched `rtk-<os>-<arch>` binary from
  `github.com/rtk-ai/rtk` releases into `~/.local/bin/rtk` when
  RTK isn't already on PATH. Linux + Darwin supported; Windows
  falls back to manual install instructions.
- **BL37 — System diagnostics.** `GET /api/diagnose` returns a
  composite health snapshot: tmux availability, session manager
  status, config-file readability, data-dir writability, free
  disk space (warns under 1 GB), and Go goroutine count
  (warns over 5 000). Composite `ok` is true only when every
  check passes.
- **BL87 — `datawatch config edit`.** Visudo-style safe editor:
  opens `~/.datawatch/config.yaml` in `$EDITOR` (or `$VISUAL`,
  `vim`, `vi`, `nano`), validates the YAML on save, and offers to
  re-edit on failure. Encrypted (`.enc`) configs are intentionally
  refused for now — operator decrypts manually first; secure
  `/dev/shm` tempfile flow is a future addition.

---

## API additions

```
POST /api/reload             # BL17 — re-read config and re-apply hot-reloadable subset
GET  /api/diagnose           # BL37 — health snapshot
```

Signals:
```
kill -HUP <datawatch-daemon-pid>   # BL17 — equivalent to POST /api/reload
```

CLI additions:
```
datawatch config edit         # BL87 — visudo-style editor
datawatch setup rtk           # BL22 — now auto-installs if RTK absent
```

---

## Container images

| Image | Change | Action |
|---|---|---|
| `parent-full` | Daemon embeds reload + diagnose endpoints + SIGHUP handler | **Rebuild required** |
| All other images | No change | No rebuild |

```bash
make container-parent-full PUSH=true CONTAINER_TAG=v3.4.0
```

Helm: `version: 0.6.0`, `appVersion: v3.4.0`.

---

## Testing

- **1001 tests / 47 packages**, all passing (+10 vs. v3.3.0).
- `internal/server/bl17_reload_test.go` — reload happy-path,
  no-config-path, HTTP method check, missing-file tolerance.
- `internal/server/bl37_diagnose_test.go` — composite OK
  contract, JSON shape, method check, allOK helper.

---

## Upgrading from v3.3.0

```bash
datawatch update           # single host
helm upgrade dw datawatch/datawatch -n datawatch \
  --set image.tag=v3.4.0   # cluster
```

Once running, `kill -HUP <pid>` is the new lightweight way to
re-apply config edits without a full restart for the supported
field subset.

---

## What's next

- **BL86** — Remote GPU/system stats agent (Observability tail).
- **BL24 + BL25** — Autonomous task decomposition + independent
  verification (Intelligence tail; planned together).
- **BL117** — PRD-driven DAG orchestrator with guardrail
  sub-agents (post-F10 future feature).

5 active backlog items remain (Sessions/Memory/Collab/Messaging
spread). See `docs/plans/README.md`.
