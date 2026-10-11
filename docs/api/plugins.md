# Plugin framework (v3.11.0 — BL33)

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
```

---

## REST endpoints

```
GET    /api/plugins                     list discovered plugins
POST   /api/plugins/reload              rescan dir
GET    /api/plugins/{name}              manifest + invocation stats
POST   /api/plugins/{name}/enable
POST   /api/plugins/{name}/disable
POST   /api/plugins/{name}/test         body: {hook, payload}
```

When `plugins.enabled` is false, every endpoint returns
`503 plugins disabled`.

### BL325 — Community registry install + browse (v8.1.0)

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

**What ships in v9.0.31 (and what does not).** datawatch parses and
validates service-kind manifests: discovery, install, and
`scripts/check-plugin-manifests.sh` all apply the rules in the table
above, and `GET /api/plugins` / `GET /api/plugins/{name}` return the
`kind`, `connection` and `lifecycle` fields. datawatch does **not**
yet attach to, start, supervise or health-probe service plugins.
Neither `attach_only` nor `managed` does anything at runtime yet; that
support arrives in a later release.

### Hooks (v1 set)

| Hook | When | Input fields | Expected response |
|---|---|---|---|
| `pre_session_start`      | Before `Session.Start` | `task, project_dir, backend, effort` | `{action:"pass"\|"block"\|"mutate", fields:{...}}` |
| `post_session_output`    | Per output chunk       | `session_id, line`                   | `{action:"pass"\|"drop"\|"replace", line:"…"}` |
| `post_session_complete`  | On session end         | `session_id, status, cost`           | `{ok:true}` — fire-and-forget |
| `on_alert`               | Before alert emission  | `severity, channel, text`            | `{ok:true}` — fire-and-forget |

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
- Per-plugin container sandboxing (BL117 territory).
- Hot reload via filesystem watcher (SIGHUP + `POST
  /api/plugins/reload` is enough for v1).
- Plugin signature verification / code-signing (install from registry
  shipped in v8.1.0 — BL325; signing/verification remains deferred).
- Go `.so` loading — intentionally rejected; Go plugins lock to exact
  toolchain + CGO + glibc versions and rebreak on every daemon rebuild.

---

<!-- BL279 see-also footer -->
## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [addons](../addons.md)
- [architecture-overview](../architecture-overview.md)
