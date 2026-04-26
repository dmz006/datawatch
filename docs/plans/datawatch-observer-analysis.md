# Datawatch‑Observer Plugin – Analysis & Recommendations

## 1. What the observer is
* The **observer** is the core stats collector introduced in BL171 (v4.1.0).
* It lives under `internal/observer/` and provides the `/api/stats` endpoint, a WebSocket stream, and a set of MCP tools.
* The collector is **registered as an in‑process plugin** via `observer.RegisterInProcessCollector` (see `internal/server/observer.go`).
* It implements the **collector** interface defined in `internal/observer/plugin.go` (native registration, not a subprocess).

## 2. How the observer is wired into the daemon
1. **Configuration flag** – `observer.plugin_enabled` (default `true`).
2. **Server start‑up** – `cmd/datawatch/main.go` loads the global config, builds a `plugins.Registry` based on the `plugins:` block, then calls:
   ```go
   observer.RegisterInProcessCollector(registry, cfg.Observer)
   ```
   This registers a *native* plugin named `observer.default` directly into the registry.
3. **Run‑time** – When a hook `observer_collect` is triggered (internal tick), the registry fan‑outs to the native collector exactly like any subprocess plugin, but because it is a native implementation it never needs the generic `plugins.enabled` flag.

## 3. Why the generic **plugins** block is disabled yet the observer works
* The generic plugin framework (`plugins.enabled`) controls **subprocess** plugins discovered under `<data_dir>/plugins/`.
* The observer is **not a subprocess plugin**; it is compiled into the daemon and registered programmatically.
* Consequently, even when `plugins.enabled: false` (the default), the observer still runs because its activation is gated by the separate `observer.plugin_enabled` flag, which defaults to `true`.

## 4. Why the observer does not appear in `datawatch plugins list`
* `datawatch plugins list` (`/api/plugins`) enumerates the contents of the **plugins.Registry** that were discovered from the filesystem (`plugins.NewRegistry`).
* Native collectors are added via `registry.RegisterNative` (see `observer/plugin.go`). Those entries are **not part of the filesystem discovery list**, and the `/api/plugins` handler only returns `registry.List()` – which excludes native entries.
* Therefore the observer is operational but invisible to the generic plugin listing. It is reachable through its own MCP tools (`observer_stats`, `observer_envelope_list`, etc.) and the `/api/stats` endpoint.

## 5. Configuration recommendations
| Setting | Current default | Recommendation |
|---------|----------------|----------------|
| `plugins.enabled` | `false` | Keep disabled unless you need external subprocess plugins.
| `observer.plugin_enabled` | `true` | Leave `true` – this is the flag that actually governs the observer.
| `observer.tick_interval_ms` | `1000` | Adjust only if you need a different sampling rate.
| `observer.process_tree.enabled` | `true` | Enable for full stats; disable only to save CPU on very low‑power boxes.

### When to enable the generic plugin framework
* If an operator wants to replace/augment the observer with a custom subprocess plugin (e.g., a Prometheus exporter), set `plugins.enabled: true` and drop a manifest under `~/.datawatch/plugins/`.
* The custom plugin can hook `observer_collect` to merge its own metrics with the built‑in collector.

## 6. Open questions / next steps (no code change required)
1. **Documentation clarity** – ensure the operator guide (`docs/api/observer.md`) mentions that the observer runs even when `plugins.enabled` is false.
2. **Expose the observer in the generic plugin UI** (optional) – could add an endpoint `/api/observer/status` that mirrors the native registration, but this is not required for correct operation.
3. **Validate config defaults** – run a quick sanity check (`datawatch observer stats`) on a fresh install to confirm the observer reports data with the default config.

---
*Analysis performed without modifying any source files. The observer works as designed; no changes to code or config are required beyond the optional recommendations above.*