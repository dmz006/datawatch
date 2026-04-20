# BL33 — Plugin framework

**Status:** v1 design, implementation underway in v3.11.0 (Sprint S7).
Operator directive: extensibility without relinking the daemon.

---

## 1. What we're building

A plugin loader that lets operators extend datawatch with **out-of-tree
subprocess plugins** — external binaries or scripts the daemon
discovers at startup, invokes per-hook via a line-oriented JSON-RPC
protocol, and reaps when done.

End-state goal: a user drops an executable + manifest under
`<data_dir>/plugins/<name>/` and its declared hooks start firing
without restarting anything fancier than the daemon.

---

## 2. Alternatives considered (and why subprocess)

| Mechanism | Pros | Cons |
|---|---|---|
| Go `.so` (`plugin.Open`) | Fast, in-process, typed | **Brittle** — must match Go version, CGO enabled, glibc, the exact datawatch build; a recompile breaks every plugin. Not supported on Windows. |
| Embedded Lua/JS | Sandboxable, hot-reload | Adds a big runtime dependency; the plugin author must learn our embedded surface; no reuse of existing CLI tooling. |
| Native Docker/K8s workers | Strong isolation, reuses F10 | Overkill for "transform this one line of output"; latency too high for per-line filters. **Already available via F10 Project Profiles for heavy work.** |
| **Subprocess + JSON-RPC over stdio** | Language-agnostic, portable, reuse of every CLI tool, operator can debug by running the plugin standalone | Fork-per-call cost (mitigated by long-lived stdio mode), manual type discipline |

Subprocess wins on portability and operator ergonomics. It's how LSP
servers, git hooks, Claude Code MCP servers, and Vim-remote plugins
work — established pattern.

---

## 3. Plugin contract

### Discovery

At startup (and on SIGHUP / `POST /api/plugins/reload`), the daemon
scans `<data_dir>/plugins/` for directories containing a
`manifest.yaml`. The entry binary is resolved relative to the manifest
directory.

### Manifest

```yaml
name: my-filter
description: Redacts API keys from session output.
version: 0.1.0
entry: ./filter          # executable relative to manifest dir
hooks:
  - post_session_output  # per-line filter
  - on_alert             # notification hook
timeout_ms: 2000         # per-invocation timeout
mode: oneshot            # or "long-lived" for persistent stdio
```

### JSON-RPC over stdio

Requests arrive on **stdin**, one JSON object per line. Responses go
to **stdout**, one per line. Stderr is captured to the audit log.

```json
{"hook":"post_session_output","session_id":"abc","line":"sk-1234"}
{"action":"replace","line":"[redacted]"}
```

Or, for a notification hook with no return required:

```json
{"hook":"on_alert","session_id":"abc","severity":"error","text":"…"}
{"ok":true}
```

### Hooks (v1 set — kept intentionally small)

| Hook | When | Fields |
|---|---|---|
| `pre_session_start` | Before `session.Manager.Start` | `task, project_dir, backend, effort` → plugin can veto or mutate (`action: "block"` / `"mutate"` with replacement fields) |
| `post_session_output` | Each buffered output chunk | `session_id, line` → plugin can replace, drop, or pass |
| `post_session_complete` | On session completion | `session_id, status, cost` → fire-and-forget (ok=true) |
| `on_alert` | Before alert emission | `severity, channel, text` → fire-and-forget; plugin can mirror to its own sink |

Additional hooks land in v3.11.x patches as operator demand surfaces.

### Failure policy

- Timeouts → plugin returns `{"action":"pass"}` (treat as no-op).
- Non-zero exit → logged, plugin auto-disabled for the process lifetime.
- Invalid JSON → logged, response discarded.
- **Plugins never block the hot path.** `post_session_output` runs
  async off a bounded per-plugin channel.

---

## 4. Security model (document-and-disclose, not sandbox)

v1 plugins run **with the same privileges as the daemon**. Operators
must trust what they install, the same way they trust any systemd
unit. This is stated prominently in `docs/api/plugins.md`:

> ⚠️ Plugins run in-process (subprocess, not containerized). They can
> read every file the daemon can, and can make network calls. Install
> only plugins you've reviewed.

Future v3.12.x patches can add:
- Per-plugin `allowed_hooks` allowlist (already in config).
- Per-plugin `cwd_sandbox: true` to chroot the working dir.
- Cluster-mode plugins via F10 spawn + BL103 validator (heavy work).

v1 doesn't do any of that. Document clearly, don't pretend.

---

## 5. v1 scope (what ships in v3.11.0)

**In:**
- `internal/plugins/` package: `Manifest`, `Plugin`, `Registry`,
  subprocess driver, `Invoke(hook, req) -> (resp, err)`.
- Oneshot mode (fork-per-call). Long-lived stdio mode deferred.
- 4 hooks: `pre_session_start`, `post_session_output`,
  `post_session_complete`, `on_alert`.
- YAML config block `plugins:` with `enabled`, `dir`, `timeout_ms`,
  `disabled: [names]`.
- REST: `/api/plugins` list, `/api/plugins/reload` POST,
  `/api/plugins/{name}` enable/disable/test.
- Full channel parity per the rule: MCP tools + CLI `datawatch plugins`
  + comm via `rest` passthrough.
- Operator doc `docs/api/plugins.md` (how to write a plugin + example).
- Tests: manifest parse, registry discovery, subprocess invoke with a
  fake plugin using the `testdata/` pattern, hook dispatch, timeout.

**Out (deferred):**
- Long-lived stdio mode with persistent per-plugin subprocess.
- Lua/JS embedded interpreter.
- Per-plugin container sandboxing (BL117-ish territory).
- Go `.so` loading — rejected (brittle).
- Hot reload via inotify — SIGHUP + REST reload is enough for v1.
- Plugin marketplace / signature verification.

---

## 6. Configuration (full parity)

```yaml
plugins:
  enabled:     false                      # off by default
  dir:         ~/.datawatch/plugins       # discovery root
  timeout_ms:  2000                       # per-invocation
  disabled:    []                         # names to skip at discovery
```

Every knob reachable from YAML + REST + MCP + CLI + comm.

---

## 7. REST surface

```
GET    /api/plugins                    list discovered + status
POST   /api/plugins/reload             rescan dir, restart registry
GET    /api/plugins/{name}             manifest + last invocation stats
POST   /api/plugins/{name}/enable      add to enabled set (remove from disabled)
POST   /api/plugins/{name}/disable     add to disabled set
POST   /api/plugins/{name}/test        synthetic hook invocation for debug
                                        body: {hook, payload}
```

---

## 8. Non-goals (explicitly)

- Plugins are **not** a replacement for F10 workers. Heavy coding /
  multi-step work is still the F10 agent path.
- Plugins are **not** a replacement for BL103 validators. Verification
  of LLM output is still BL25 (BL103 agent or session.Start).
- Plugins do **not** get access to datawatch's internal Go types
  beyond the documented JSON-RPC surface. No tight coupling.

---

## 9. Testing strategy

- Unit: manifest parse, registry discovery (with scripted plugin
  under `testdata/`), timeout, disabled allowlist.
- Integration: full daemon run with a shell-script plugin that writes
  its `on_alert` payloads to a file — assert the file contents.
- Documented in `docs/test-coverage.md`.
