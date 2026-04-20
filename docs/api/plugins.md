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

---

## Plugin contract

### Manifest (`manifest.yaml`)

| Field | Required | Description |
|---|---|---|
| `name`        | yes | Unique plugin name |
| `entry`       | yes | Executable path; relative paths are resolved against the plugin dir |
| `hooks`       | yes | List of hook names this plugin registers for |
| `description` | no  | Human-readable one-liner |
| `version`     | no  | Freeform version string |
| `timeout_ms`  | no  | Per-invocation timeout (overrides global `plugins.timeout_ms`) |
| `mode`        | no  | `oneshot` (default) — fork-per-call |

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
- Long-lived stdio mode with persistent per-plugin subprocess.
- Per-plugin container sandboxing (BL117 territory).
- Hot reload via filesystem watcher (SIGHUP + `POST
  /api/plugins/reload` is enough for v1).
- Plugin marketplace / signature verification.
- Go `.so` loading — intentionally rejected; Go plugins lock to exact
  toolchain + CGO + glibc versions and rebreak on every daemon rebuild.
