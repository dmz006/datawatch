# Release Notes — v3.5.0 (Sprint S1 — Quick Wins + UI Diff)

**2026-04-19.** First release of the post-refactor sprint plan. Five
items shipped: BL1 (IPv6), BL34 (read-only ask), BL35 (project
summary), BL41 (effort levels), F14 (live cell DOM diffing).

---

## Highlights

- **BL1 — IPv6 listener support.** Every server bind site now uses
  `net.JoinHostPort` (`internal/server/listen_addr.go`) so IPv6
  literals are correctly bracketed. Configure `server.host: "::"`
  for dual-stack listening, or `[::1]` for loopback-only.
- **BL34 — Read-only `POST /api/ask`.** One-shot LLM ask without
  spawning a session. Routes to Ollama or OpenWebUI; no tmux, no
  state. Docs: `docs/api/ask.md`.
- **BL35 — `GET /api/project/summary`.** Structured per-directory
  snapshot: git status + recent commits + per-project session
  history + aggregate stats. Docs: `docs/api/project-summary.md`.
- **BL41 — Effort levels per task.** New `Session.Effort` field
  (`quick` / `normal` / `thorough`); supplied via REST start, falls
  back to `session.default_effort` config (default `normal`).
  Hot-reloadable via SIGHUP / `POST /api/reload`.
- **F14 — Live cell DOM diffing.** Web UI's session list now
  attempts a per-card diff before falling back to full re-render.
  Eliminates flicker + scroll-reset on every WS state push for the
  common case (state badge / timestamp / prompt updates).

---

## API additions

```
POST /api/ask                       # BL34 — see docs/api/ask.md
GET  /api/project/summary?dir=<abs> # BL35 — see docs/api/project-summary.md
```

### REST schema additions

- `POST /api/sessions` request body gained an optional `effort`
  field (`quick` / `normal` / `thorough`).
- `Session` JSON now includes optional `effort`.

---

## Configuration changes

```yaml
session:
  # BL41 — applied when the start request doesn't pass an effort.
  default_effort: "normal"   # or "quick" / "thorough"
```

Existing configs migrate transparently — empty value defaults to
`normal`.

Hot-reloadable: `POST /api/reload` (or SIGHUP) applies a new
`session.default_effort` to the live manager without restart.

---

## Container images

Per the container-maintenance rule:

| Image | Change | Action |
|---|---|---|
| `parent-full` | Daemon adds new endpoints + IPv6-safe binds + Effort field | **Rebuild required** |
| All other images | No change | No rebuild |

```bash
make container-parent-full PUSH=true CONTAINER_TAG=v3.5.0
```

Helm: `version: 0.7.0`, `appVersion: v3.5.0`.

---

## Testing

- **1022 tests / 47 packages**, all passing (+21 vs. v3.4.1).
- New test files:
  - `internal/session/bl41_effort_test.go` — IsValidEffort,
    Manager.DefaultEffort + resolveEffort.
  - `internal/server/bl1_listen_test.go` — joinHostPort across
    IPv4 / IPv6 / dual-stack / hostname inputs.
  - `internal/server/bl34_ask_test.go` — method + empty-question +
    unsupported backend + not-configured + happy-path against a
    fake Ollama httptest server.
  - `internal/server/bl35_summary_test.go` — method + missing dir +
    relative dir + no-git + with sessions + real git repo.

---

## Upgrading from v3.4.x

```bash
datawatch update                                            # single host
helm upgrade dw datawatch/datawatch -n datawatch \
  --set image.tag=v3.5.0                                    # cluster
```

To enable IPv6 dual-stack in an existing install, set:

```yaml
server:
  host: "::"   # bracketed in URLs as [::]
```

then reload (no restart needed):

```bash
kill -HUP $(pgrep datawatch)   # or POST /api/reload
```

---

## What's next

**Sprint S2 (v3.6.0) — Sessions productivity**: BL5 templates, BL26
cron schedules, BL27 project mgmt, BL29 git checkpoints, BL30
rate-limit cooldown, BL40 stale recovery. ~1 week effort.
