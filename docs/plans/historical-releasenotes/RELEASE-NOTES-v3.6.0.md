# Release Notes — v3.6.0 (Sprint S2 — Sessions Productivity)

**2026-04-19.** Six items shipped: BL5 templates, BL26 recurring
schedules, BL27 projects, BL29 git checkpoints + rollback, BL30
rate-limit cooldown, BL40 stale task recovery.

---

## Highlights

- **BL5 — Session templates.** Operator-defined named bundles of
  `start_session` parameters (project_dir, backend, profile, effort,
  env, auto-git toggles). REST CRUD at `/api/templates` and
  `template` field on the start payload.
- **BL26 — Recurring schedules.** New `recur_every_seconds` (+
  optional `recur_until`) on `ScheduledCommand`. The scheduler
  reschedules the next run instead of marking done after a
  successful fire. Failures still terminate the recurrence (no
  thundering retries).
- **BL27 — Project management.** `/api/projects` CRUD plus a
  `project` field on the start payload that resolves to a
  registered name.
- **BL29 — Git checkpoints + rollback.** Pre-/post-session annotated
  tags (`datawatch-pre-{id}`, `datawatch-post-{id}`).
  `POST /api/sessions/{id}/rollback` does `git reset --hard
  datawatch-pre-{id}` (refuses dirty trees unless `force=true`).
- **BL30 — Global rate-limit cooldown.** Manager-level pause that
  refuses new starts when `session.rate_limit_global_pause: true`.
  REST surface: `GET / POST / DELETE /api/cooldown`.
- **BL40 — Stale task recovery.** `GET /api/sessions/stale[?seconds=N]`
  returns running sessions whose `UpdatedAt` is older than the
  configured threshold (`session.stale_timeout_seconds`, default
  1800).

---

## API additions

```
POST   /api/sessions               request body now accepts:
                                     "template": "<name>"   (BL5)
                                     "project":  "<name>"   (BL27)

GET    /api/templates              # BL5 list
POST   /api/templates              # BL5 upsert
GET    /api/templates/{name}       # BL5 fetch
DELETE /api/templates/{name}       # BL5 remove

GET    /api/projects               # BL27 list
POST   /api/projects               # BL27 upsert (dir must be absolute)
GET    /api/projects/{name}        # BL27 fetch
DELETE /api/projects/{name}        # BL27 remove

POST   /api/sessions/{id}/rollback # BL29  body: {"force": bool}

GET    /api/cooldown               # BL30 status
POST   /api/cooldown               # BL30 set {until_unix_ms, reason}
DELETE /api/cooldown               # BL30 clear

GET    /api/sessions/stale[?seconds=N] # BL40 list stale running sessions
```

`ScheduledCommand` JSON gained:
```json
{
  "recur_every_seconds": 300,            // BL26
  "recur_until": "2026-05-01T00:00:00Z"  // BL26 — optional deadline
}
```

---

## Configuration changes

```yaml
session:
  default_effort:           "normal"     # BL41 (carried from v3.5.0)
  stale_timeout_seconds:    1800         # BL40 — running > Ns flagged stale
  rate_limit_global_pause:  false        # BL30 — when true, starts refuse during cooldown

projects:                                # BL27
  fooapp:
    dir: "/home/op/fooapp"
    default_backend: "claude-code"
    description: "main project"

templates:                               # BL5
  web:
    backend: "claude-code"
    effort: "thorough"
    auto_git_commit: true
    description: "frontend dev"
```

All session-level fields default-fill to the historic behaviour
when omitted, and are hot-reloadable via SIGHUP / `POST /api/reload`.

---

## Container images

| Image | Change | Action |
|---|---|---|
| `parent-full` | Daemon adds new endpoints + checkpoint tags + cooldown | **Rebuild required** |
| All other images | No change | No rebuild |

```bash
make container-parent-full PUSH=true CONTAINER_TAG=v3.6.0
```

Helm: `version: 0.8.0`, `appVersion: v3.6.0`.

---

## Testing

- **1056 tests / 47 packages**, all passing (+34 vs. v3.5.0).
- New test files:
  - `internal/session/bl26_recur_test.go` (3 tests)
  - `internal/session/bl29_checkpoint_test.go` (5 tests, real git repo)
  - `internal/session/bl30_cooldown_test.go` (6 tests)
  - `internal/session/bl40_stale_test.go` (6 tests)
  - `internal/server/bl5_templates_test.go` (3 tests)
  - `internal/server/bl27_projects_test.go` (4 tests)
  - `internal/server/bl30_cooldown_test.go` (3 tests)
  - `internal/server/bl40_stale_test.go` (2 tests)

---

## Upgrading from v3.5.0

```bash
datawatch update                                            # single host
helm upgrade dw datawatch/datawatch -n datawatch \
  --set image.tag=v3.6.0                                    # cluster
```

To opt into rate-limit global pause:

```yaml
session:
  rate_limit_global_pause: true
```

then SIGHUP the daemon.

---

## What's next

**Sprint S3 (v3.7.0) — Cost + observability tail**: BL6 cost tracking
(per-session token + dollar accounting), BL86 remote GPU/system
stats agent (introduces a new `datawatch-agent` binary), BL9 audit
log. ~1 week effort.
