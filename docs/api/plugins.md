# Plugin framework (v3.11.0)

**Shipped in v3.11.0.** Subprocess plugin framework — external
binaries or scripts the daemon discovers under
`<data_dir>/plugins/<name>/`, invokes per-hook via line-oriented
JSON-RPC over stdio, and reaps when done.

Disabled by default — opt in by setting `plugins.enabled: true`.

Design doc: `docs/plans/2026-04-20-bl33-plugin-framework.md`.

---

## ⚠️ Security

Plugins run **in-process as a subprocess** — same privileges as the
daemon. They can read every file the daemon can and make network
calls. **Install only plugins you've reviewed.** No sandboxing in v1;
container-based isolation is a future v3.12.x patch.

---

## Quickstart

```sh
# 1. Write the plugin
mkdir -p ~/.datawatch/plugins/my-filter
cat > ~/.datawatch/plugins/my-filter/manifest.yaml <<'YAML'
name: my-filter
description: Redact API keys from session output.
version: 0.1.0
entry: ./filter
hooks:
  - post_session_output
timeout_ms: 1500
YAML
cat > ~/.datawatch/plugins/my-filter/filter <<'SH'
#!/bin/sh
read line
echo "$line" | grep -qE 'sk-[a-zA-Z0-9]{20,}' \
  && echo '{"action":"replace","line":"[redacted]"}' \
  || echo '{"action":"pass"}'
SH
chmod +x ~/.datawatch/plugins/my-filter/filter

# 2. Enable plugins globally
datawatch plugins reload     # rescan after install

# 3. Verify
datawatch plugins list
datawatch plugins test my-filter post_session_output '{"line":"sk-abcd1234567890abcdef"}'
```

---

## Surfaces (full channel parity)

| Channel | Entry-point |
|---|---|
| YAML  | `plugins:` block in `~/.datawatch/config.yaml` |
| REST  | `/api/plugins/*` |
| MCP   | `plugins_list`, `plugins_reload`, `plugin_get/enable/disable/test` |
| CLI   | `datawatch plugins <subcmd>` |
| CLI   | `datawatch plugins install <registry> <name>` + `browse-registry <name>` (v8.1.0) |
| MCP   | `plugin_install`, `plugin_browse_registry` (v8.1.0) |
| Comm  | via the comm `rest` passthrough |

---

## Configuration

```yaml
plugins:
  enabled:     false                     # off by default
  dir:         ~/.datawatch/plugins      # discovery root
  timeout_ms:  2000                      # per-invocation budget
  disabled:    []                        # names to skip at discovery
  managed:     []                        # service plugins datawatch may supervise (v9.0.32)
```

---

## REST endpoints

```
GET    /api/plugins                     list discovered plugins
POST   /api/plugins/reload              rescan dir
GET    /api/plugins/{name}              manifest + invocation stats
POST   /api/plugins/{name}/enable       optional body: {"managed": bool}
POST   /api/plugins/{name}/disable
POST   /api/plugins/{name}/test         body: {hook, payload}
POST   /api/plugins/{name}/widgets/token        mint/rotate the widget push token (v9.0.40)
POST   /api/plugins/{name}/widgets/{widget_id}  push a widget value, plugin push token (v9.0.40)
GET    /api/plugins/{name}/widgets              declared widgets + latest values (v9.0.40)
GET    /api/plugins/{name}/widgets/{widget_id}  one widget + latest value (v9.0.40)
```

The widget endpoints are described under [Widgets](#widgets-v9040).

When `plugins.enabled` is false, every endpoint returns
`503 plugins disabled`.

### Community registry install + browse (v8.1.0)

Two additional endpoints for installing plugins from a registry and
browsing registry contents.

**POST /api/plugins/install**

Install a plugin from a named registry.

Request body:
```json
{"registry": "<registry-name>", "name": "<plugin-name>"}
```

Response:
```json
{"status": "ok", "installed": "<name>", "from_registry": "<registry>"}
```

The daemon downloads the plugin into `<data_dir>/plugins/<name>/`,
validates the manifest, and makes it available immediately. A
`plugins reload` is not required after install.

**GET /api/plugins/browse?registry=\<name\>**

List all plugins available in a named registry.

Response:
```json
{
  "registry": "<name>",
  "plugins": [
    {
      "name": "...",
      "path": "...",
      "manifest": {
        "name": "...",
        "description": "...",
        "version": "...",
        "author": "...",
        "license": "...",
        "category": "...",
        "hooks": ["..."],
        "contributor_notes": "...",
        "datawatch_min_version": "..."
      }
    }
  ]
}
```

The community registry is pre-seeded as the first registry by default
at `https://github.com/dmz006/datawatch-community`. Use
`datawatch plugins browse-registry community` to list it without any
additional setup.

```sh
export BASE=https://localhost:8443
export TOKEN=<your_bearer_token>

# Browse the default community registry.
curl -sk -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/plugins/browse?registry=community"

# Install a plugin from the community registry.
curl -sk -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"registry": "community", "name": "redact-secrets"}' \
  "$BASE/api/plugins/install"
```

---

## Plugin contract

### Manifest (`manifest.yaml`)

| Field | Required | Description |
|---|---|---|
| `name`        | yes | Unique plugin name |
| `entry`       | yes (automation) | Executable path; relative paths are resolved against the plugin dir |
| `hooks`       | yes (automation) | List of hook names this plugin registers for |
| `description` | no  | Human-readable one-liner |
| `version`     | no  | Freeform version string |
| `timeout_ms`  | no  | Per-invocation timeout (overrides global `plugins.timeout_ms`) |
| `mode`        | no  | **Deprecated, ignored** (since v9.0.31). Parsed for one more release so existing manifests still load, then removed. Use `kind` instead. |
| `kind`        | no  | `automation` (default when absent — fork-per-call hook subprocess, the v2.1 behavior) or `service` (a persistent external process datawatch talks to as a client). Case-insensitive. Manifest v3, v9.0.31. |
| `connection`  | yes when `kind: service` | How datawatch reaches a service-kind plugin. See the `connection.*` rows. |
| `connection.base_url`    | yes when `kind: service` | Absolute `http://` or `https://` URL of the running service. |
| `connection.auth.type`   | no  | Auth scheme. Only `bearer` is supported; empty means `bearer`. |
| `connection.auth.token`  | yes when `connection.auth` is set | Exactly one `${secret:NAME}` reference — never a literal credential. Only the reference shape is validated; the secret is resolved at use time. |
| `connection.health_path` | no  | Path (relative to `base_url`) for health probing in either lifecycle mode. |
| `lifecycle`   | no  | Only allowed when `kind: service` (rejected on automation manifests). Absent means attach-only. See the `lifecycle.*` rows. |
| `lifecycle.supported_modes` | yes when `lifecycle` is set | Non-empty list of `attach_only` and/or `managed`, no duplicates. Author-declared: the operator's later mode choice is limited to what is listed here. |
| `lifecycle.entry`           | yes when `managed` is listed | Executable to launch for managed mode (relative to the plugin dir, or absolute). |
| `lifecycle.args`            | no  | Arguments passed to `lifecycle.entry` (managed mode only). |
| `lifecycle.restart_policy`  | no  | `on-failure`, `always` or `never`. Empty = supervisor default. |
| `lifecycle.stop_timeout_ms` | no  | Grace period before a managed process is killed on stop. Must be `>= 0`; `0` = supervisor default. |
| `widgets`     | no  | List of dashboard/session widgets, allowed on both kinds. See [Widgets](#widgets-v9040) for the per-widget fields. v9.0.40. |

Unknown keys are ignored, so a manifest written for a newer daemon
still loads on an older one. A manifest that fails validation is
skipped at discovery (logged to stderr) and rejected by
`POST /api/plugins/install`.

### Service-kind plugins (manifest v3)

Added in v9.0.31. A `kind: service` plugin is a long-running process
or server that datawatch connects to as a client, instead of a
subprocess it forks per hook call. The plugin stays a standalone
program: it must work on its own, without datawatch. The `connection`
block is what datawatch needs to attach to it. The optional
`lifecycle` block says whether datawatch may also start and supervise
that same binary.

```yaml
name: mail-service
description: Mail triage service exposed to datawatch.
version: 1.4.0
kind: service
connection:
  base_url: http://127.0.0.1:8765
  health_path: /healthz
  auth:
    type: bearer
    token: ${secret:mail-service-token}   # secrets-store ref, never a literal
lifecycle:
  supported_modes: [attach_only, managed]
  entry: bin/mail-service                 # required because managed is listed
  args: ["--listen", "127.0.0.1:8765"]
  restart_policy: on-failure
  stop_timeout_ms: 5000
```

- **`attach_only`**: datawatch only connects to an instance that is
  already running. It never starts or stops it.
- **`managed`**: datawatch starts the same binary as a child process,
  health-checks it, restarts it per `restart_policy`, and stops it.
- The plugin author declares which modes the plugin supports. The
  operator can only choose from what the author listed.
- Service-kind plugins are **never invoked as hooks**. Any `hooks:`
  list on a service manifest is ignored, and `POST /api/plugins/{name}/test`
  returns an error for them.

**What ships when.** v9.0.31 added parsing and validation of
service-kind manifests (discovery, install,
`scripts/check-plugin-manifests.sh`, and the `kind` / `connection` /
`lifecycle` fields in `GET /api/plugins`). v9.0.32 adds the runtime
for `managed` mode, described below. `attach_only` still does nothing
at runtime: datawatch does not yet connect to or health-probe an
attach-only service.

#### Managed mode at runtime (v9.0.32)

datawatch supervises a service plugin only when **all** of these hold:

1. `kind: service` and the author lists `managed` in
   `lifecycle.supported_modes` (the ceiling: an operator cannot force
   a mode the author did not declare);
2. the operator has opted the plugin in (see below);
3. the plugin is enabled, and `plugins.enabled` is on.

Anything else (automation plugins, attach-only plugins, plugins not
opted in) is never started, stopped or probed. Nothing is spawned
until the daemon has finished starting its plugin subsystem.

**Opting in.** Persistently, in `~/.datawatch/config.yaml`:

```yaml
plugins:
  enabled: true
  managed:
    - mail-service        # names of service plugins datawatch may supervise
```

At runtime over REST (in memory, like enable/disable; add the name to
`plugins.managed` to keep it across restarts):

```bash
curl -sk -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"managed": true}' \
  "$BASE/api/plugins/mail-service/enable"
```

`{"managed": false}` opts out (and stops the service); an empty body
leaves the opt-in unchanged. The endpoint returns `400` when the
plugin does not declare `managed` support or the body is malformed,
and `404` when the plugin is unknown. Over MCP, `plugin_enable` takes
the same optional boolean: `plugin_enable(name="mail-service",
managed=true)`; the body is sent only when `managed` is given.

**Lifecycle.**

- **Start** — at daemon boot, and whenever a plugin becomes eligible
  later (enabled, opted in, or newly found by `POST /api/plugins/reload`
  or the dir watcher). `lifecycle.entry` runs with `lifecycle.args` in
  the plugin dir as a child process in its own process group. Env is
  built the same way as for hook plugins, so `${secret:NAME}` env
  values work.
- **Health** — with `connection.health_path` set, datawatch GETs
  `base_url` + `health_path` every 5s, sending the resolved
  `connection.auth` bearer token when configured. The first 2xx marks
  the service `healthy`. A health failure is either no 2xx within 30s
  of launch, or 3 consecutive failed probes after the service was
  healthy (it shows as `unhealthy` in between). On a health failure the
  child is stopped and the restart policy decides what happens next.
  Without a `health_path`, a process still running 30s after launch is
  treated as healthy.
- **Restart** — per `lifecycle.restart_policy`:

  | Policy | Non-zero exit / signal / health failure | Clean exit 0 |
  |---|---|---|
  | `on-failure` (default) | restart | stop (state `stopped`) |
  | `always` | restart | restart |
  | `never` | no restart (state `crashed`) | no restart (state `stopped`) |

  Restarts back off exponentially: 1s, 2s, 4s, … capped at 30s.
- **Give-up** — after 5 restarts in a row without a stable run (the
  6th consecutive failure), datawatch stops supervising and leaves the
  service in state `failed`. A run that stays healthy for 60s resets
  the failure count. To try again, disable and re-enable the plugin
  (or restart the daemon).
- **Stop** — disabling the plugin, opting it out, removing it from the
  plugin dir, and daemon shutdown all stop the service: SIGTERM to the
  process group, then SIGKILL if it is still running after
  `lifecycle.stop_timeout_ms` (default 10s). datawatch waits for the
  child to be reaped. On shutdown all managed services stop
  concurrently, bounded at 30s overall. A hot-reload of the plugin dir
  never orphans a running child.

**Status.** `GET /api/plugins` and `GET /api/plugins/{name}` add a
`service` object to any plugin that currently has a supervisor
(plugins without one are unchanged):

```json
"service": {
  "state": "healthy",
  "pid": 41237,
  "restarts": 1,
  "last_error": "process exited: exit status 2"
}
```

`state` is one of `stopped`, `starting`, `healthy`, `unhealthy`,
`crashed`, `restarting`, `failed`, `stopping`. `pid` and
`last_error` are omitted when empty.

**Audit events.** Every lifecycle transition is written to the audit
log with actor `daemon`, resource type `plugin` and the plugin name,
and also printed to the daemon log:

| Action | When | Details |
|---|---|---|
| `plugin_service_start` | a child process is launched (first start and each restart) | `pid`, `restarts` |
| `plugin_service_restart` | a run ended and the policy will restart it | `restarts`, `backoff_ms`, `exit_code`, `error`, `health_fail` |
| `plugin_service_health_fail` | startup deadline missed or probe-failure threshold reached | `reason` (`startup_deadline` / `probe_failures`), `error`, `url` |
| `plugin_service_stop` | requested stop, a run the policy won't restart, or give-up | `reason` (`requested` / `exited` / `max_restarts`), plus `state`, `exit_code`, `error`, `consecutive_failures` when not requested |

### Widgets (v9.0.40)

Added in v9.0.40. A plugin of either kind can declare **widgets**:
small tiles of plugin data for the dashboard or a session view. Each
widget gets its data in one of two ways. In **pull** mode the daemon
fetches the value on an interval. In **push** mode the plugin sends
the value whenever it wants. You choose the mode per widget, so one
plugin can mix both.

**What ships when.** v9.0.40 ships the data model and transport:
manifest validation, pulling, the push endpoint, push tokens, storage
and the read endpoints below. **Nothing renders widgets yet.** The PWA
renderer, and widget surfaces in the mobile apps, MCP, CLI and comm
channels, come in a later release. Until then, the read endpoints are
the only way to see a widget's value.

#### Manifest `widgets:` fields

| Field | Required | Description |
|---|---|---|
| `id`               | yes | Unique within the manifest. Used in the push and read URLs. `token` is reserved and rejected. |
| `surface`          | yes | Where the widget will render: `dashboard` or `session`. |
| `kind`             | yes | How it will render: `stat_tile`, `table`, `list`, `sparkline`, `status_badge` or `markdown`. |
| `data_mode`        | yes | `pull` (the daemon fetches the value) or `push` (the plugin posts it). |
| `pull_path`        | pull only | Required when `data_mode: pull`, and must be empty for `push`. On `kind: service` it is a path appended to `connection.base_url` and must start with `/`. On `kind: automation` it is passed to the plugin in the `status_poll` request, so a plugin with several pull widgets can tell them apart. |
| `interval_seconds` | no  | Pull interval in seconds (pull only). `0` or unset means 30s. Negative values are rejected. |

An invalid `widgets:` block fails validation like any other manifest
error: the plugin is skipped at discovery and rejected by
`POST /api/plugins/install`. Widget values are free-form JSON for now.
The shape each `kind` expects will be defined with the renderer.

#### Pull mode

The daemon runs one pull worker per pull widget of each **enabled**
plugin. Workers follow plugin reloads and enable/disable changes. The
first pull happens right away, then one per interval. Each widget is
isolated: a slow or hung pull only delays that widget. A tick that
arrives while the widget's previous pull is still running is skipped,
not queued. A pull is cut off at the widget's interval at the latest.

- **`kind: automation`**: the daemon invokes the plugin's
  `status_poll` hook through the normal hook path (same subprocess,
  env, `${secret:…}` handling and `timeout_ms` budget as every other
  hook; see below).
- **`kind: service`**: the daemon sends
  `GET <connection.base_url><pull_path>` with `Accept: application/json`.
  When `connection.auth` is set, it adds
  `Authorization: Bearer <resolved secret>`. The service must answer
  `2xx` with a JSON body of at most 64 KiB. The body is stored as the
  widget value, unchanged.

A failed pull records an `error` on the widget and **keeps the previous
value**. A failure is an error response, a non-2xx status, a timeout,
invalid JSON or a body over 64 KiB.

#### The `status_poll` hook

`status_poll` is a oneshot hook, like the other hooks. List it under
`hooks:` in an automation plugin that has pull widgets.

Request (one line on stdin):

```json
{"hook":"status_poll","payload":{"widget_id":"queue-depth","pull_path":"/queue","surface":"dashboard","kind":"stat_tile"}}
```

Response (one line on stdout):

```json
{"fields":{"data":{"value":42,"label":"queued"}}}
```

- The widget value is `fields.data`. If there is no `data` key, the
  whole `fields` object is the value. It is stored as JSON (at most
  64 KiB).
- A non-empty `error`, a missing `fields` object, a timeout or a
  non-zero exit is a failed pull: the error is recorded and the widget
  keeps its previous value.

#### Push mode and push tokens

A plugin pushes a value with
`POST /api/plugins/{name}/widgets/{widget_id}`. The request body **is**
the widget value: any JSON document, at most 64 KiB, stored unchanged.

Pushes use a **plugin-scoped push token**, not the operator token. Mint
it once with operator auth (config-write capability) and hand it to the
plugin process, for example via the secrets store:

```bash
# Mint (or rotate) the push token for plugin "build-feed". The token is
# returned once; only a bcrypt hash is kept, in
# <data_dir>/plugins/widget_tokens.json.
curl -sk -X POST \
  -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/plugins/build-feed/widgets/token"
# → {"token":"q3V…"}
```

Calling the endpoint again **rotates** the token: the old one stops
working immediately. A token only authenticates `POST` to that one
plugin's push route. It is not accepted by any other endpoint, by
another plugin's push route, or by the read endpoints. The operator
token is not accepted on the push route either.

Push responses:

| Status | When |
|---|---|
| `200` | Stored. Body: `{"updated_at":"<RFC 3339 time>"}` |
| `400` | Body is empty or not valid JSON |
| `401` | Missing bearer token, wrong token, another plugin's token, or the operator token |
| `404` | Unknown plugin, or the plugin does not declare that widget |
| `409` | The widget is `data_mode: pull`, so pushes are refused |
| `413` | Body over 64 KiB |
| `503` | Widgets unavailable: `plugins.enabled` is off |

#### Read endpoints

Both need operator auth (config-read capability).

```bash
# Every widget a plugin declares, each with its latest value.
curl -sk -H "Authorization: Bearer $TOKEN" "$BASE/api/plugins/build-feed/widgets"

# One widget.
curl -sk -H "Authorization: Bearer $TOKEN" "$BASE/api/plugins/build-feed/widgets/recent-builds"
```

```json
{
  "plugin": "build-feed",
  "widgets": [
    {
      "id": "recent-builds",
      "surface": "session",
      "kind": "list",
      "data_mode": "push",
      "value": {
        "plugin": "build-feed",
        "widget_id": "recent-builds",
        "data": {"items": ["#812 passed", "#811 failed"]},
        "updated_at": "2026-10-12T07:15:02Z",
        "source": "push"
      }
    }
  ]
}
```

The single-widget endpoint returns one entry of that `widgets` array.
`value` is `null` until data arrives. `source` is `push` or `pull`.
`error` holds the last pull error and is omitted when there is none.
Unknown plugin or undeclared widget → `404`; plugins disabled → `503`.

#### Storage

The daemon keeps one latest value per (plugin, widget) in memory. It
writes them to `<data_dir>/plugins/widgets.json` every 60s if anything
changed, and once more on shutdown. Widgets keep their last value
across a daemon restart.

#### Example: a pull widget on an automation plugin

`~/.datawatch/plugins/queue-stats/manifest.yaml`:

```yaml
name: queue-stats
description: Show the job queue depth on the dashboard.
version: 0.1.0
entry: ./poll.py
hooks: [status_poll]
timeout_ms: 1500
widgets:
  - id: queue-depth
    surface: dashboard
    kind: stat_tile
    data_mode: pull
    pull_path: /queue          # passed through to status_poll
    interval_seconds: 15
```

`~/.datawatch/plugins/queue-stats/poll.py` (make it executable):

```python
#!/usr/bin/env python3
import json, os, sys

req = json.loads(sys.stdin.readline())
if req.get("hook") == "status_poll":
    payload = req.get("payload", {})
    if payload.get("pull_path") == "/queue":
        depth = len(os.listdir("/var/spool/jobs"))   # your data source
        print(json.dumps({"fields": {"data": {"value": depth, "label": "queued jobs"}}}))
    else:
        print(json.dumps({"error": "unknown widget " + payload.get("widget_id", "")}))
    sys.exit(0)
print(json.dumps({"action": "pass"}))
```

After `datawatch plugins reload`, the daemon calls `poll.py` every 15s.
`GET /api/plugins/queue-stats/widgets/queue-depth` returns the latest
value with `"source": "pull"`.

#### Example: a push widget on a service plugin

`~/.datawatch/plugins/build-feed/manifest.yaml`:

```yaml
name: build-feed
description: Recent CI builds, pushed by the build-feed service.
version: 1.0.0
kind: service
connection:
  base_url: http://127.0.0.1:8770
  auth:
    type: bearer
    token: ${secret:build-feed-token}
widgets:
  - id: recent-builds
    surface: session
    kind: list
    data_mode: push
```

Mint the push token once (see above), give it to the service, and the
service pushes whenever its data changes:

```bash
curl -sk -X POST \
  -H "Authorization: Bearer $WIDGET_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"items":["#812 passed","#811 failed"]}' \
  "$BASE/api/plugins/build-feed/widgets/recent-builds"
# → {"updated_at":"2026-10-12T07:15:02.481Z"}
```

`connection.auth` is for calls from datawatch to the service, such as
pulls and health probes. The push token is for calls from the service
to datawatch. They are separate credentials.

### Hooks (v1 set)

| Hook | When | Input fields | Expected response |
|---|---|---|---|
| `pre_session_start`      | Before `Session.Start` | `task, project_dir, backend, effort` | `{action:"pass"\|"block"\|"mutate", fields:{...}}` |
| `post_session_output`    | Per output chunk       | `session_id, line`                   | `{action:"pass"\|"drop"\|"replace", line:"…"}` |
| `post_session_complete`  | On session end         | `session_id, status, cost`           | `{ok:true}` — fire-and-forget |
| `on_alert`               | Before alert emission  | `severity, channel, text`            | `{ok:true}` — fire-and-forget |
| `status_poll`            | On a pull widget's interval (automation plugins, v9.0.40) | `payload: {widget_id, pull_path, surface, kind}` | `{fields:{data: <widget value>}}`. See [The status_poll hook](#the-status_poll-hook) |

### JSON-RPC over stdio

- **Stdin**: one JSON object, newline-terminated.
- **Stdout**: one JSON object response, newline-terminated.
- **Stderr**: captured into the daemon audit log on non-zero exit.

### Failure policy

- **Timeout**: response treated as `{action:"pass"}`; error recorded.
- **Non-zero exit**: response treated as `{action:"pass"}`; error
  recorded; plugin's error counter increments but is not disabled.
- **Invalid JSON on stdout**: response discarded, `{action:"pass"}`
  applied.
- **Plugins never block the hot path**: `post_session_output` runs
  through a bounded per-plugin channel.

---

## Fan-out for filter hooks

When multiple plugins register for the same `post_session_output`
hook, the daemon chains them in plugin-name alphabetical order. Each
`replace` feeds into the next plugin's `line` input; the first `drop`
stops the chain. Operators who need a specific ordering should name
their plugins with a numeric prefix (e.g. `00-redact`, `10-trim`).

---

## Example plugin (Python)

```python
#!/usr/bin/env python3
import json, sys
req = json.loads(sys.stdin.readline())
if req.get("hook") == "post_session_output":
    line = req.get("line", "")
    if "PASSWORD" in line:
        print(json.dumps({"action": "replace", "line": "[redacted]"}))
        sys.exit(0)
print(json.dumps({"action": "pass"}))
```

Manifest:
```yaml
name: py-redact
entry: ./redact.py
hooks: [post_session_output]
```

---

## Not in v1

Deferred to later patches:
- Long-lived plugins — now declared with `kind: service` (manifest v3,
  v9.0.31; see [Service-kind plugins](#service-kind-plugins-manifest-v3)).
  Manifests are parsed and validated; attaching to and supervising
  the running service is still deferred to a later release.
- Per-plugin container sandboxing.
- Hot reload via filesystem watcher (SIGHUP + `POST
  /api/plugins/reload` is enough for v1).
- Plugin signature verification / code-signing (install from registry
  shipped in v8.1.0; signing/verification remains deferred).
- Go `.so` loading — intentionally rejected; Go plugins lock to exact
  toolchain + CGO + glibc versions and rebreak on every daemon rebuild.

---

<!-- BL279 see-also footer -->
## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [addons](../addons.md)
- [architecture-overview](../architecture-overview.md)
