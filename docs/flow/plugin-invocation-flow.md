# Plugin invocation flow

How a hook firing inside the daemon dispatches to a registered
subprocess plugin and merges the result back into the originating
operation.

```
   ┌─── daemon hook site ─────────────────────────────────────────────┐
   │                                                                  │
   │  e.g. session.Manager → onSessionStart                           │
   │       e.g. messaging  → onMessageSend                            │
   │       e.g. autonomous → onTaskComplete                           │
   │                                                                  │
   │  registry.Dispatch(ctx, hook, payload)                           │
   │                                                                  │
   └────────────────────────────┬─────────────────────────────────────┘
                                │
                                ▼
   ┌─── internal/plugins.Registry ────────────────────────────────────┐
   │                                                                  │
   │  503-ish behaviour:                                              │
   │      cfg.Plugins.Enabled = false ─→ skip silently                │
   │                                                                  │
   │  fsnotify watcher (v4.0.1) keeps the registry hot:               │
   │      <data_dir>/plugins/<name>/manifest.yaml change              │
   │          ─→ debounced 500 ms ─→ rescan + reregister              │
   │                                                                  │
   │  for each plugin where manifest.hooks contains <hook>:           │
   │      │                                                           │
   │      ▼                                                           │
   │  exec.CommandContext(timeout, plugin.entrypoint)                 │
   │      stdin  ← JSON {hook, payload, session_ctx, env}             │
   │      stderr → captured into plugin.last_error                    │
   │      stdout → JSON {result, exit_action, modifications}          │
   │                                                                  │
   │  invoke_count++                                                  │
   │  success ─→ result merged into the call site                     │
   │  exit!=0 ─→ last_error set; hook continues with next plugin      │
   │                                                                  │
   └──────────────────────────────────────────────────────────────────┘
```

## Native plugins

Built-in subsystems (observer today, future native bridges) appear in
`/api/plugins` alongside subprocess plugins via
`Server.RegisterNativePlugin()`. They do **not** go through the
subprocess Dispatch path — their status is computed live from the
subsystem's own state. The PWA renders them with a `native` tag:

```
   POST /api/plugins  (rare — usually GET only)
   GET  /api/plugins
       └─→ {plugins: [subprocess], native: [observer, …]}
```

## Operator surfaces

| Surface | Endpoint / control |
|---|---|
| PWA Settings → Plugins | live status table (subprocess + native) |
| REST | `/api/plugins`, `/api/plugins/{name}/{enable,disable,test}` |
| CLI | `datawatch plugins {list,reload,enable,disable,test}` |
| MCP | `plugins_list`, `plugins_test` |
| File | drop a manifest under `<data_dir>/plugins/<name>/` |

## Failure modes

| Symptom | Likely cause | Fix |
|---|---|---|
| Plugin doesn't fire | `enabled: false` in manifest, or hook name mismatch | check `/api/plugins` `enabled` + `hooks` fields |
| `last_error` set | Subprocess crashed or returned non-JSON | check stderr in the plugin status detail |
| Hot-reload not working | fsnotify backend not available (e.g. NFS) | restart the daemon to force a rescan |
| Slow hook | Plugin synchronous timeout | bump `plugins.timeout_ms`, or move to async via outbox |

## Related

- Operator doc: [`docs/api/plugins.md`](../api/plugins.md)
- Design doc: [`docs/plans/2026-04-20-bl33-plugin-framework.md`](../plans/2026-04-20-bl33-plugin-framework.md)
- Implementation: `internal/plugins/`
- REST handlers: `internal/server/plugins.go`
