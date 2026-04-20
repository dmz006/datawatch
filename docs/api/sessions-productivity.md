# Sessions productivity API (v3.6.0 / Sprint S2)

Six new surfaces shipped in v3.6.0. They share a theme: less typing
per session start, better recovery from things going wrong.

---

## Templates — `/api/templates` (BL5)

Reusable named bundles of start parameters.

```
GET    /api/templates           list
POST   /api/templates           upsert
GET    /api/templates/{name}    fetch one
DELETE /api/templates/{name}    remove
```

Body fields (all optional except `name` on POST):
`name`, `project_dir`, `backend`, `profile`, `effort`, `env`,
`auto_git_commit`, `auto_git_init`, `description`.

Then start a session with the template:

```bash
curl -X POST http://localhost:8080/api/sessions \
  -H 'Content-Type: application/json' \
  -d '{"task":"refactor login","template":"web"}'
```

Per-request fields override template defaults.

---

## Projects — `/api/projects` (BL27)

Named project-directory aliases.

```
GET    /api/projects
POST   /api/projects            body: {name, dir, default_backend?, description?}
GET    /api/projects/{name}
DELETE /api/projects/{name}
```

`dir` MUST be absolute. Then start with `"project": "<name>"` instead
of repeating `project_dir`.

Templates and projects compose: template defaults apply first, then
the project resolves any unset `project_dir`/`backend`, then the
request body wins.

---

## Recurring schedules (BL26)

`ScheduledCommand` JSON gained two optional fields:

```json
{ "recur_every_seconds": 300, "recur_until": "2026-05-01T00:00:00Z" }
```

When set, a successful run reschedules instead of marking done. A
failure (exit non-zero, send-keys error) terminates the recurrence
to avoid runaway retries — re-create explicitly to resume.

---

## Git checkpoints + rollback (BL29)

When `session.auto_git_commit: true` (the default) **and** the
project_dir is a git repo, the daemon now also tags:

- `datawatch-pre-{session-id}` at start
- `datawatch-post-{session-id}` after a successful end

Rollback to the pre-checkpoint:

```bash
curl -X POST http://localhost:8080/api/sessions/{id}/rollback \
  -H 'Content-Type: application/json' \
  -d '{"force": false}'
```

`force: false` (default) refuses if the working tree has uncommitted
changes. `force: true` discards them.

---

## Rate-limit cooldown (BL30)

```
GET    /api/cooldown                         current state
POST   /api/cooldown                         body: {until_unix_ms, reason}
DELETE /api/cooldown                         clear immediately
```

When `session.rate_limit_global_pause: true` (config) **and** a
cooldown is active, `POST /api/sessions` returns an error so callers
can route to a different backend. Without the opt-in flag, the
cooldown is purely informational.

---

## Stale task recovery (BL40)

```
GET /api/sessions/stale[?seconds=N]
```

Returns running sessions whose `UpdatedAt` is older than `N` seconds
(or `session.stale_timeout_seconds`, default 1800). Response shape:

```json
{
  "threshold_seconds": 1800,
  "hostname":          "host",
  "count":             1,
  "sessions": [
    { "id":"aa", "full_id":"host-aa", "stale_seconds":4321, ... }
  ]
}
```

Use it from a comm bot or cron to detect daemon-restart orphans.

---

## AI / MCP integration notes

- All endpoints are tested with real `httptest` servers; contracts
  listed here match the implementation.
- Per the Configuration Accessibility rule, every config field
  introduced in v3.6.0 is settable via YAML, REST `/api/config`,
  and reload-applied via SIGHUP / `POST /api/reload`.
- For session start: the recommended path for AI agents is
  `template + project` (compact), then per-task overrides.
