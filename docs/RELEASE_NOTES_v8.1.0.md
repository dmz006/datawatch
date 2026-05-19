# datawatch v8.1.0 — Release Notes

**Released:** 2026-05-19  
**Previous release:** v8.0.0 (2026-05-19)  
**Commits since v8.0.0:** BL324–BL326 + S14b

---

## What v8.1.0 Is

v8.1.0 is the community and observability release. Where v8.0 extended datawatch outward across hosts and trust boundaries, v8.1 opens the ecosystem in two directions simultaneously: community-contributed extensions are now first-class citizens — seeded before the built-in PAI registry, installable with one command — and production-grade alerting on observer metrics closes the loop between telemetry and automated response.

This release is smaller in scope than v8.0, but targeted: every change is directly operator-useful and ships with full 7-surface parity across the areas where it applies.

---

## Highlights

- **Community registry first-class** — the `dmz006/datawatch-community` registry seeds before the PAI registry on every new installation. Browse categorized, community-contributed skills and plugins; install a plugin with a single `datawatch plugins install community <name>` command.
- **Alert rules** — named threshold rules evaluated every 30 s against observer envelopes. Fire system alerts or trigger autoscaling stubs when `cpu_pct`, `mem_pct`, `gpu_pct`, `rss_bytes`, `net_rx_bps`, or `net_tx_bps` cross a threshold. 8 REST endpoints, 8 MCP tools, full CLI, 100-entry firings ring buffer.
- **Mic recording UX** — PWA voice-input now shows an animated recording overlay with a 5-bar waveform, a duration counter, and explicit Cancel / Send buttons replacing the previous inline toast.

---

## Major Features

### Community Skills + Plugins Registry (BL324)

Every new datawatch installation now seeds `dmz006/datawatch-community` as its first registry — before the built-in PAI registry. This gives operators immediate access to community-contributed extensions without any manual configuration.

Community registry entries carry three mandatory fields absent from the PAI registry: `author`, `contributor_notes`, and `license`. These fields are validated at connect-time and displayed in the browse output so operators always know who wrote a plugin and under what terms before installing it.

**Getting started:**

```bash
# Browse the community skills catalog
datawatch skills registry list

# Browse community plugins
datawatch plugins browse-registry community

# Connect a community fork or mirror
datawatch skills registry connect community
```

The community registry is at **https://github.com/dmz006/datawatch-community**. PRs for new skills and plugins are welcome; see the repo's `CONTRIBUTING.md` for the manifest schema and review checklist.

---

### Plugin Install from Registry (BL325)

Operators can now copy a plugin from any connected registry clone into the local plugins directory and hot-reload it in one step:

```bash
datawatch plugins install community my-plugin-name
```

The install path resolves `<registry_clone_dir>/plugins/<name>/` → `<data_dir>/plugins/<name>/`, validates the `manifest.yaml`, copies atomically, and calls the existing reload pipeline. No daemon restart required.

**All surfaces:**

| Surface | How |
|---|---|
| CLI | `datawatch plugins install <registry> <name>` |
| REST | `POST /api/plugins/install` |
| MCP | `plugin_install` tool |
| Comm | via `rest POST /api/plugins/install` passthrough |
| YAML | community registry pre-seeded in `plugins_registries` on first run |
| PWA | browse/install UI pending v8.2 |

---

### Per-Pod Alert Rules (S14b)

Alert rules are named observer-metric threshold checks that run every 30 seconds against all observer envelopes. When a rule fires, it can:

- Emit a **system alert** visible in the alerts dock and deliverable via any configured comm channel
- Dispatch a **scale_up** or **scale_down** stub for future autoscaling integration

Rules are persisted in `<data_dir>/alert-rules.yaml` and managed across all six surfaces:

**Example rule:**

```yaml
id: high-cpu-worker-1
name: "Worker 1 CPU spike"
filter:
  pod: worker-1
condition:
  metric: cpu_pct
  operator: ">"
  threshold: 85
action: alert
cooldown_seconds: 300
enabled: true
```

**Supported metrics:** `cpu_pct` · `mem_pct` · `gpu_pct` · `rss_bytes` · `net_rx_bps` · `net_tx_bps`

**Supported operators:** `>` · `>=` · `<` · `<=` · `==`

**Actions:** `alert` · `scale_up` · `scale_down`

The evaluator enforces a per-rule cooldown so a sustained threshold crossing does not storm the alert dock. The last 100 firings per rule are kept in a ring buffer accessible via `GET /api/alert-rules/firings` or `datawatch alert-rules firings`.

**All surfaces:**

| Surface | How |
|---|---|
| CLI | `datawatch alert-rules {list,get,add,update,delete,enable,disable,firings}` |
| REST | 8 endpoints at `/api/alert-rules` |
| MCP | 8 tools: `alert_rule_{list,get,add,update,delete,enable,disable,firings}` |
| YAML | `<data_dir>/alert-rules.yaml` |
| Comm | inherits system alerts; no dedicated `alert-rules` comm command (v8.2) |
| PWA | management card pending v8.2 |

See [`docs/howto/alert-rules.md`](howto/alert-rules.md) for the full walkthrough, YAML schema, and example rules.

---

### Mic Recording Overlay (BL326)

The PWA voice-input flow received a complete UX overhaul. The previous inline toast that appeared while recording has been replaced with a full-screen animated overlay:

- **5-bar waveform** driven by real AudioContext analyser data — bar heights animate in real time to the microphone input level
- **Recording duration counter** showing elapsed seconds
- **Cancel** button — discards the recording immediately
- **Send** button — submits the recording for transcription and routes the result to the active session input

This is a PWA-only change. The mobile companion (datawatch-app) uses the native OS audio recorder, which already provides equivalent UX on Android and iOS.

---

## Operator Notes

### Community registry

The community registry is seeded at index 0 in `skills_registries` and `plugins_registries` on first run of v8.1.0. Existing installations that upgrade from v8.0.0 will see the community registry added automatically the next time the daemon starts.

To opt out of community registry seeding, remove the `community` entry from `skills_registries` in `datawatch.yaml` after the first start.

The community registry URL is: **https://github.com/dmz006/datawatch-community**

### Alert rules evaluator

The alert rules evaluator starts automatically when the daemon starts. It polls observer envelopes every 30 s. Rules with `enabled: false` are skipped without evaluation cost. The evaluator is safe to run with an empty `alert-rules.yaml` — it simply idles.

Per-rule cooldowns are tracked in memory and reset on daemon restart. If you need persistent cooldown state across restarts, note this behavior when writing rules with very long cooldown windows.

### Version check

```
datawatch version
# → v8.1.0
```

---

## Commit Log (feat/fix)

```
[BL324]  feat(registry): seed community registry first in AddBuiltinDefaults
[BL324]  feat(registry): browse-registry CLI + REST GET /api/plugins/browse + MCP plugin_browse_registry
[BL325]  feat(plugins): plugin install from registry — CLI/REST/MCP + atomic copy + reload
[BL326]  feat(pwa): mic recording overlay — animated waveform, cancel/send, duration counter
[S14b]   feat(alertrules): types + YAML store + evaluator (cpu/mem/gpu/rss/net metrics, 30s tick)
[S14b]   feat(alertrules): 8 REST endpoints at /api/alert-rules
[S14b]   feat(alertrules): 8 MCP tools alert_rule_{list,get,add,update,delete,enable,disable,firings}
[S14b]   feat(alertrules): CLI datawatch alert-rules {list,get,add,update,delete,enable,disable,firings}
[S14b]   docs: howto/alert-rules.md
        chore: version bump 8.0.0 → 8.1.0
```

---

*datawatch v8.1.0 — open to the community, wired to the metrics.*
