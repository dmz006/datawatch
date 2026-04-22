# BL171 — datawatch-observer: unified stats / process / sub-system monitor

**Status:** v1 design. Execution across Sprints S9 → S13 (v4.1.0 → v4.2.x).
**Sources:** GitHub issue [dmz006/datawatch#20](https://github.com/dmz006/datawatch/issues/20),
mobile-side spec [datawatch-app `docs/plans/2026-04-22-unified-monitoring.md`](https://github.com/dmz006/datawatch-app/blob/main/docs/plans/2026-04-22-unified-monitoring.md),
operator directive 2026-04-22.
**Supersedes:** the ad-hoc `/api/stats` scalars in `internal/stats/collector.go`.

---

## 1. What we're building

A **unified monitoring data plane** — one wire contract, one code path, three deployment shapes — that replaces today's flat `StatsDto` with a structured, streamable, tree-aware payload. The collector becomes an explicit *subsystem* (`internal/observer/`) that every datawatch consumer (PWA, mobile, CLI, MCP, comm, remote aggregator) talks to through the same endpoints.

Key new capabilities over today's `collector.go`:

- **Structured payload** — top-level sub-objects `host / cpu / mem / disk / gpu / net / sessions / backends / cluster / processes / envelopes`.
- **Live streaming** — 1 s WS cadence from the in-process plugin; 5 s push from remote peers into the aggregator.
- **Full sub-process monitoring** — walk `/proc` once per tick, group processes into *envelopes* keyed by session id and LLM backend (claude, claude-spawned shells, ollama + ollama children, openwebui, aider, etc.). Roll up CPU / mem / fds / net per envelope.
- **Per-envelope attribution** — every session gets a CPU / mem / net bill. Every LLM backend on the host gets a CPU / mem / net bill. Answers the operator question "which session / backend is eating the box right now?"
- **Pluggable backends** — the in-process plugin is the default; a standalone daemon and a cluster container share the same contract and come online later (Shape B / C). All three can register with one main datawatch and be aggregated behind `/api/stats`.
- **Plugin framework reuse** — the in-process collector ships under the existing `plugins.*` framework (BL33), so operators can override, disable, or replace it without a datawatch rebuild.
- **Token-auth federation** — every remote shape uses a self-generated, per-instance HMAC token that the main datawatch mints + accepts. Tokens are rotatable from REST / CLI.
- **Backward compatible** — today's flat `cpu_pct / mem_pct / disk_pct / gpu_pct / sessions_*` fields become top-level aliases inside each sub-object so v1 clients keep working.

---

## 2. Glossary (carries through the whole doc)

| Term | Meaning |
|---|---|
| **observer** | The stats subsystem as a whole. Ships three ways, same contract. |
| **collector** | One concrete implementation that produces a StatsResponse. Plugin, daemon, or container. |
| **aggregator** | The main datawatch daemon that collects its own stats AND federates peer collectors. |
| **envelope** | A logical grouping of processes — one per session, one per LLM backend, one per container. Has rolled-up CPU / mem / fd / net counters. |
| **shape** | A deployment mode (A = in-process plugin, B = standalone daemon, C = cluster container). |

---

## 3. Wire contract — `StatsResponse v2`

Lives at `GET /api/stats`. Live-streamed via the existing `MsgStats` WebSocket broadcast at 1 s cadence. Extends today's flat fields additively — v1 clients parsing `cpu_pct` still work because we keep the flat fields as aliases inside each sub-object.

```json
{
  "v": 2,
  "host": {
    "name": "ring",
    "uptime_seconds": 12345,
    "os": "linux",
    "kernel": "6.8.0",
    "arch": "x86_64",
    "shape": "plugin"                // "plugin" | "daemon" | "cluster"
  },

  "cpu": {
    "pct": 14.2,
    "cores": 16,
    "load1": 0.42,
    "load5": 0.38,
    "per_core_pct": [12.1, 18.3, ...]
  },

  "mem": {
    "pct": 41.0,
    "used_bytes": 6.8e9,
    "total_bytes": 16.5e9,
    "swap_used_bytes": 0,
    "swap_total_bytes": 8.5e9
  },

  "disk": [
    { "mount": "/",         "pct": 62.0, "used_bytes": ..., "total_bytes": ... },
    { "mount": "/mnt/data", "pct": 18.0, "used_bytes": ..., "total_bytes": ... }
  ],

  "gpu": [
    { "name": "NVIDIA RTX 4090", "vendor": "nvidia", "util_pct": 78.5,
      "mem_used_bytes": ..., "mem_total_bytes": ..., "power_w": 310,
      "temp_c": 68, "proc_pids": [3401, 4112] }
  ],

  "net": {
    "rx_bytes_per_sec": 125000,
    "tx_bytes_per_sec": 78000,
    "per_process": [                 // populated by Shape C (eBPF) only
      { "pid": 3401, "comm": "ollama", "rx_bps": 120000, "tx_bps": 75000 }
    ]
  },

  "sessions": {
    "total": 5,
    "running": 2,
    "waiting": 1,
    "rate_limited": 0,
    "per_backend": { "claude-code": 3, "openwebui": 1 }
  },

  "backends": [
    { "name": "ollama", "reachable": true, "last_ok_unix_ms": ..., "latency_ms": 12 }
  ],

  "processes": {                     // FULL sub-process tree rooted at datawatch + known LLMs
    "sampled_at_unix_ms": 1713801234567,
    "total_tracked": 47,
    "tree": [
      {
        "pid": 3100, "ppid": 1, "comm": "datawatch",
        "cmdline": "datawatch start --foreground",
        "cpu_pct": 0.8, "rss_bytes": 85000000, "fds": 42, "threads": 9,
        "cgroup": "", "container_id": "",
        "children": [ /* recursive */ ]
      },
      {
        "pid": 3401, "ppid": 1, "comm": "ollama",
        "cmdline": "ollama serve",
        "cpu_pct": 21.4, "rss_bytes": 2.1e9, "fds": 118, "threads": 44,
        "gpu_pct": 78.5, "gpu_mem_bytes": 6.4e9,
        "children": [
          { "pid": 3512, "ppid": 3401, "comm": "ollama-runner", ... }
        ]
      }
    ]
  },

  "envelopes": [                     // ROLLED-UP per session + per backend
    {
      "id": "session:ralfthewise-787e",
      "kind": "session",
      "label": "Claude-dev",
      "root_pid": 4089,
      "pids": [4089, 4100, 4109, 5002],
      "cpu_pct": 14.6,
      "rss_bytes": 640000000,
      "fds": 63,
      "net_rx_bps": 0,
      "net_tx_bps": 0,
      "gpu_pct": 0,
      "last_activity_unix_ms": 1713801233000
    },
    {
      "id": "backend:ollama",
      "kind": "backend",
      "label": "ollama",
      "root_pid": 3401,
      "pids": [3401, 3512, 3513, 3514],
      "cpu_pct": 21.4,
      "rss_bytes": 2100000000,
      "fds": 118,
      "net_rx_bps": 120000,
      "net_tx_bps": 75000,
      "gpu_pct": 78.5,
      "gpu_mem_bytes": 6400000000
    },
    {
      "id": "backend:ollama-docker",
      "kind": "backend",
      "label": "ollama (docker: ollama)",
      "container_id": "6a4b...",
      "image": "ollama/ollama:latest",
      "pids": [ /* container-namespaced PIDs */ ],
      "cpu_pct": 9.2, "rss_bytes": 1.1e9, "gpu_pct": 0
    }
  ],

  "cluster": {                       // Shape C only
    "nodes": [
      { "name": "worker-1", "ready": true, "cpu_pct": 45, "mem_pct": 32,
        "pod_count": 8, "pressure": [] }
    ]
  },

  "peers": [                         // Main aggregator view only
    { "name": "ollama-box", "shape": "daemon", "reachable": true, "last_push_unix_ms": ... },
    { "name": "cluster-a",  "shape": "cluster", "reachable": true, "last_push_unix_ms": ... }
  ]
}
```

### Back-compat aliases

For every existing flat field on `/api/stats`, we expose both the v2 nested form and the v1 alias at the top level. v1 clients keep working unchanged. v2 clients read the structured form.

| v1 field | v2 location |
|---|---|
| `cpu_pct` | `cpu.pct` |
| `mem_pct` | `mem.pct` |
| `disk_pct` | `disk[0].pct` (root) |
| `gpu_pct` | `gpu[0].util_pct` |
| `sessions_total` | `sessions.total` |
| `sessions_running` | `sessions.running` |
| `uptime_seconds` | `host.uptime_seconds` |

---

## 4. Full sub-process monitoring — how the tree + envelopes are built

This is the most operator-visible new capability. It runs on every tick, on Linux only (Shapes A + B + C). macOS / Windows get a trimmed subset (host + cpu + mem + disk + sessions; no process tree).

### 4.1 Tick pipeline (1 s for plugin, 5 s for remote push)

```
Tick
 ├─ scan /proc/[pid]/{stat, status, cmdline, fd, io}
 │    → raw per-process records
 ├─ build parent/child graph from stat.ppid
 ├─ classify each process into an envelope:
 │    session:<full_id>       — root is the tmux pane's shell PID bound to that session
 │    backend:<name>          — root matches a configured backend executable (ollama, aider, claude, openwebui-runner, …)
 │    backend:<name>-docker   — backend running inside a docker container we can see
 │    backend:<name>-k8s      — ditto for k8s pods (Shape C only)
 │    container:<short-id>    — any other container whose root process we picked up
 │    system                  — everything else
 ├─ roll up CPU / rss / fds / threads / net / gpu per envelope
 ├─ sort envelopes by CPU desc (truncate at top-N for WS payload)
 ├─ emit StatsResponse v2 with processes.tree (N = 200 top by activity) + envelopes[]
 └─ broadcast MsgStats
```

### 4.2 Session-envelope attribution

Each `session.Session` already carries a `TmuxSession` name and the tmux pane's PID is discoverable via `tmux list-panes -F '#{pane_pid}'`. The observer caches `session_id → pane_pid` on session start. The envelope is the transitive child-process closure of that root PID.

### 4.3 Backend-envelope attribution — local processes

Configurable signatures in `observer.backend_signatures`:

```yaml
observer:
  backend_signatures:
    claude:    { exec: ["claude", "claude-code"], track: true }
    ollama:    { exec: ["ollama"],                 track: true }
    openwebui: { exec: ["open-webui"],             track: true }
    aider:     { exec: ["aider"],                   track: true }
    goose:     { exec: ["goose"],                   track: true }
    shell:     { exec: ["bash", "zsh", "fish"],     track: false }   # default off
```

Walk the tree, match by `comm` first then `cmdline[0]` basename. The root is the shallowest matching process; everything under it joins that envelope.

### 4.4 Backend-envelope attribution — docker / podman

Two discovery paths, tried in order:

1. **Socket-reachable Docker** — `unix:///var/run/docker.sock` accessible, engine responds → query `/containers/json` and `/containers/{id}/stats?stream=false`. Correlate container PIDs via `/containers/{id}/top`. Classify containers by image-name substring (`ollama`, `open-webui`, …) or explicit `observer.container_labels` allowlist.
2. **`/proc/<pid>/cgroup` scan** — for each process, parse the `0::/docker/<id>` or `0::/kubepods/.../..._<id>` path to find the container / pod id even when the Docker socket isn't reachable (unprivileged host). Correlate id → image / pod via a small on-disk cache populated when the socket path succeeded (if it ever did) or via the `containerd` snapshotter metadata when available.

Either path produces `envelope.container_id`, `envelope.image`, and, where possible, `envelope.pod` / `envelope.namespace`.

### 4.5 GPU attribution

Per-process GPU utilisation via `nvidia-smi --query-compute-apps=pid,used_memory --format=csv,noheader`. Intel / AMD: deferred (Shape C cluster container will add `level_zero` + DCGM scrape).

### 4.6 eBPF per-process network (Shape C only)

Runs inside the cluster container (privileged). Attaches `kprobe/tcp_sendmsg` + `kprobe/tcp_recvmsg` / `udp_*` probes and aggregates by pid. The rest of the wire shape is unchanged — only `net.per_process` and `envelope.net_{rx,tx}_bps` go non-zero.

---

## 5. Three deployment shapes

### Shape A — in-process plugin (default, ships first)

- Lives in `internal/observer/`.
- Registers with the daemon as a `plugin.go` of the BL33 framework — but since the plugin is in-tree Go code (not a subprocess manifest) we register it via a new `observer.RegisterInProcessCollector(r *plugins.Registry)` call from `main.go`. Out-of-tree subprocess plugins can still replace or extend it by registering an `observer_collect` hook (see §7).
- Unprivileged. Reads `/proc`, runs `nvidia-smi` if present, reads `/sys/class/powercap` for RAPL. No eBPF.
- Default-on; disable with `observer.plugin_enabled: false`.
- Publishes via the existing `/api/stats` REST endpoint + `MsgStats` WS broadcast.

### Shape B — standalone daemon (`datawatch-stats.service`)

- New binary at `cmd/datawatch-stats/`. Tiny (no session manager, no tmux, no router).
- Systemd unit file at `deploy/systemd/datawatch-stats.service`.
- Listens on `:9001` by default (configurable), serves the same `GET /api/stats` and `MsgStats` WS.
- Registers with the main datawatch via `POST /api/observer/peers` on startup. Main datawatch mints a per-peer HMAC token on first contact; peer stores it to disk (`~/.datawatch-stats/peer.token`) and uses it for subsequent 5 s push of `StatsResponse v2` to `POST /api/observer/peers/{name}/stats`.
- Same `/proc` + `nvidia-smi` coverage as Shape A. No eBPF.

### Shape C — cluster container

- OCI image at `ghcr.io/dmz006/datawatch-stats-cluster:vX.Y.Z`.
- Runs with `CAP_BPF` + `CAP_SYS_RESOURCE` + `hostPID: true` (k8s) or `--privileged --pid=host` (docker).
- Adds: eBPF per-process net (`bpf/net.c`), per-cgroup CPU / mem, `nvidia-smi` + DCGM scrape, optional k8s metrics-server scrape.
- Same federation path as Shape B — registers with the main datawatch, pushes the full v2 payload including `processes.tree.*.net_*` + `cluster.nodes[]`.

---

## 6. Auth / token model (shared by B + C)

Per-peer HMAC tokens, self-generated on first contact:

```
peer boot:                 datawatch-stats --datawatch https://primary:8443 --name ollama-box
     POST /api/observer/peers/register { name, shape, host_fingerprint }
     → primary mints token_secret (32 bytes), stores it keyed by peer name
     → returns { token: "<peer-token>", peer_id: "..." }
peer writes token to ~/.datawatch-stats/peer.token (0600)

per-push (every 5 s):      POST /api/observer/peers/ollama-box/stats
     Header: Authorization: Bearer <peer-token>
     Body:   StatsResponse v2
     → primary verifies HMAC, stores latest payload + last_push_unix_ms

rotation:                  datawatch observer peer rotate ollama-box
     → primary mints a new token_secret; peer picks it up on next registered-401
     → old token accepted for 60 s grace to avoid dropped pushes
```

Token storage on the primary: same secret-backend abstraction as the existing bootstrap-token broker (`internal/auth`). No new storage subsystem.

CLI + REST + MCP + comm parity from day one — peer registration, rotation, and listing are all reachable from every channel.

---

## 7. Plugin integration points

### In-tree (Shape A)

`internal/observer/plugin.go` exposes:

```go
// RegisterInProcessCollector wires the default collector into the
// daemon's plugin registry. Always called from main.go when
// observer.plugin_enabled is true.
func RegisterInProcessCollector(reg *plugins.Registry, cfg Config) {
    reg.RegisterNative("observer.default", &defaultCollector{cfg: cfg})
}
```

The in-process collector implements a small interface:

```go
type Collector interface {
    Collect(ctx context.Context) (*StatsResponse, error)
    Name() string
}
```

### Out-of-tree (BL33 subprocess plugins)

Adds two new hook names to the BL33 plugin manifest:

| Hook | Input | Output | Use case |
|---|---|---|---|
| `observer_collect` | `{}` | `StatsResponse` fragment | Replace or augment the default collector (e.g. a site-specific Prometheus-scrape plugin) |
| `observer_envelope_classify` | `{pid, comm, cmdline, ppid}` | `{envelope_id, label, kind}` | Classify processes the default rules miss |

Fan-out rule: multiple plugins can register `observer_collect`; their outputs are **deep-merged** (later wins on scalar conflicts, arrays concatenated). `observer_envelope_classify` is first-match.

---

## 8. Consumer integration

Everything below is in scope for Sprint S9 (v4.1.0).

### 8.1 REST

- `GET /api/stats` → `StatsResponse v2` (with v1 aliases).
- `GET /api/stats/processes?envelope=session:<id>` → process sub-tree for one envelope (drill-down, avoids the 1 s broadcast carrying unbounded trees).
- `GET /api/observer/peers` → list registered Shape B / C peers + last-push timestamps.
- `POST /api/observer/peers/register` → (peer-side) auto-registration.
- `POST /api/observer/peers/{name}/stats` → (peer-side) push.
- `DELETE /api/observer/peers/{name}` → remove a peer (token revoked).
- `POST /api/observer/peers/{name}/rotate` → rotate a peer's token.

### 8.2 MCP

New tools:

| Tool | Purpose |
|---|---|
| `observer_stats` | Returns the current `StatsResponse v2`. |
| `observer_envelope_list` | Lists envelopes (sorted by CPU desc, with label + kind + top-N pids). |
| `observer_envelope_get` | Drill-down for one envelope — full process tree + rolled-up metrics. |
| `observer_peers_list` | Registered peers + reachability. |
| `observer_peer_rotate` | Rotate a peer's token. |

### 8.3 CLI

```
datawatch observer stats                     # snapshot, pretty-printed
datawatch observer envelopes                 # table: kind, label, cpu, mem, net
datawatch observer envelope <id>             # drill-down
datawatch observer peers                     # list
datawatch observer peer rotate <name>        # token reset
datawatch observer watch                     # live 1 s tail (TTY)
```

### 8.4 Comm (via `rest` passthrough)

```
rest GET /api/stats
rest GET /api/observer/peers
rest POST /api/observer/peers/<name>/rotate
```

### 8.5 PWA

Monitor tab rework (new cards, existing scalars retained):

- **Host header** — name, uptime, OS, kernel, shape badge.
- **CPU / mem / disk / GPU tiles** — threshold-colored, live via WS.
- **Per-core CPU strip** — horizontal 4-wide wrap, click → full stat.
- **Envelopes table** — sortable by CPU / mem / net / GPU. Click row → envelope-drill-down modal with the tree, cmdlines, container / cgroup info.
- **Backends row** — green / red dots + last-ok ms.
- **Peers row** — Shape B / C peers with push-freshness indicator; click → that peer's stats.
- Sidebar "Kill orphans" card (already surfaces from `/api/stats/kill-orphans`) moves *under* the Host header per the open-features request.

Federated peers show on `/settings/comms` too — satisfies the open-features "settings/comms is missing federated peers" item.

### 8.6 Mobile (datawatch-app)

Consumes the same v2 payload. Mobile has its own 4-phase plan in `docs/plans/2026-04-22-unified-monitoring.md` (in the mobile repo). Landing gated on **S9** shipping here. **Mobile v1.0 finish is ahead of Shape B / C work** per operator direction — S9 unblocks mobile Phase 1 + 2; mobile completes before we start S11.

### 8.7 Backward compat

- `/api/stats` keeps its v1 flat fields as top-level aliases — every v1 client keeps working unchanged during the rollout.
- `MsgStats` WS payload grows but stays JSON-compatible.
- Remove the v1 aliases in v5.0 (far future).

---

## 9. Sprints

### Sprint S9 — v4.1.0 (this sprint): observer core + plugin + PWA consumer

Substrate. Everything below lands in one release.

| BL | Item |
|----|------|
| BL171.1 | `internal/observer/` package — `StatsResponse v2` types, process-tree walker, envelope classifier, 1 s ticker. |
| BL171.2 | In-tree plugin wiring (`observer.RegisterInProcessCollector`). Replaces `internal/stats/collector.go` as the source of truth for `/api/stats`; keep a thin back-compat shim so the old `SystemStats` shape is still computed for any unexpected consumer. |
| BL171.3 | Docker + cgroup discovery — socket-path first, `/proc/<pid>/cgroup` fallback. Container-id + image correlation. |
| BL171.4 | GPU per-process via `nvidia-smi`. Populate `gpu[].proc_pids` + `envelope.gpu_pct`. |
| BL171.5 | REST: `/api/stats` serves v2 with aliases; `/api/stats/processes` drill-down. |
| BL171.6 | WS: `MsgStats` payload upgraded to v2. 1 s cadence. |
| BL171.7 | `observer:` YAML block; full channel parity (CLI / MCP / comm / web). |
| BL171.8 | PWA Monitor tab rework — envelopes table, per-core CPU strip, drill-down modal. |
| BL171.9 | `docs/api/observer.md` + `docs/flow/bl171-observer-flow.md` + architecture-overview / data-flow updates. |
| BL171.10 | OpenAPI entries for the new endpoints (both copies). MCP tool mapping. |
| BL171.11 | `kill orphans` surfaced on the PWA Monitor tab under Host header (open-features item). |
| BL171.12 | Federated-peers stub on Settings → Comms (list-only, peers from `/api/observer/peers`). Write path lands in S11 with Shape B. |
| BL171.13 | Tests — unit (classifier, tree walker, alias compat), live smoke against the operator's daemon. |

**Shipping gate:** operator can open the Monitor tab, see CPU 100 % on an ollama run localised to `backend:ollama`, drill into the `session:<id>` envelope for a running claude session and see the shell + claude + any subprocess children.

### Sprint S10 — v4.1.1–v4.1.x (mobile phases 1 + 2)

No server-side work, but the server team's responsibility is to keep the v2 contract stable and fix any mobile-raised bugs inside this window. Mobile ships: StatsDto v2 parsing, structured card layout, WS live stream, fallback polling. Aligns to mobile's `docs/plans/2026-04-22-unified-monitoring.md` Phase 1 + 2.

### Sprint S11 — v4.2.0: Shape B (standalone daemon)

| BL | Item |
|----|------|
| BL172.1 | `cmd/datawatch-stats/` binary — reuses `internal/observer/` collector. |
| BL172.2 | Federation: `POST /api/observer/peers/register`, token mint + grace-window rotation, secure per-peer HMAC via `internal/auth`. |
| BL172.3 | Main daemon aggregation — merge local + peer payloads, expose `peers[]` in the root response. |
| BL172.4 | `deploy/systemd/datawatch-stats.service`, `deploy/homebrew/datawatch-stats.rb`. |
| BL172.5 | Operator doc + flow diagram for the peer-registration handshake. |
| BL172.6 | PWA Settings → Comms "federated peers" card gains full CRUD. |

### Sprint S12 — v4.2.x: Shape C (cluster container)

| BL | Item |
|----|------|
| BL173.1 | `docker/dockerfiles/Dockerfile.stats-cluster` — distroless + `bpf2go`-compiled eBPF. |
| BL173.2 | eBPF `kprobe/tcp_*` + `kprobe/udp_*` per-process net aggregation. |
| BL173.3 | DCGM scrape for GPU; ROCm / level_zero deferred but scoped. |
| BL173.4 | k8s metrics-server scrape → `cluster.nodes[]`. |
| BL173.5 | Helm values snippet for the cluster sidecar; compose snippet for single-host docker deploys. |
| BL173.6 | PWA Monitor tab gains the `cluster.nodes` tab when non-empty. |

### Sprint S13 — v4.3.0+: Agent / recursive / worker drill-downs

Merges the observer surface with the F10 ephemeral-agent + BL105 orchestrator trees. Out of scope until mobile v1.0 lands AND Shape C is running in production. Skeleton reserved: every F10 worker spawns its own Shape A plugin and publishes peer stats back to the parent over the already-authenticated bootstrap channel.

---

## 10. Org structure — what lives where

```
internal/observer/
  types.go          — StatsResponse v2 + subobjects
  collector.go      — default in-process collector (Shape A)
  procfs_linux.go   — /proc walk; fast path
  procfs_other.go   — trimmed build for non-linux
  envelopes.go      — envelope classifier + roll-up
  docker.go         — docker socket + cgroup parse
  gpu.go            — nvidia-smi driver; iface for DCGM / ROCm / level_zero
  api.go            — adapter for server.ObserverAPI
  ws.go             — MsgStats payload builder
  peers.go          — federation server + auth
  plugin_hooks.go   — observer_collect / observer_envelope_classify fan-out
  collector_test.go
  envelopes_test.go
  procfs_linux_test.go
  peers_test.go

internal/server/
  observer.go       — REST handlers for /api/stats, /api/stats/processes,
                      /api/observer/peers[,/{name}[,/stats|/rotate]]

cmd/datawatch-stats/   (S11)
  main.go           — standalone binary reusing internal/observer
  config.go         — minimal config (listen addr, primary url, peer name)

docker/dockerfiles/
  Dockerfile.stats-cluster   (S12)

deploy/systemd/
  datawatch-stats.service    (S11)

docs/
  api/observer.md            (S9) — operator + AI-ready
  flow/bl171-observer-flow.md (S9) — tick pipeline + envelope classification
  flow/bl172-peer-federation-flow.md (S11)
  flow/bl173-cluster-monitor-flow.md (S12)
  plans/2026-04-22-bl171-datawatch-observer.md   (this doc)
```

`internal/stats/` sticks around as a thin compat shim for v4.1.x, then marked deprecated in v4.2 and removed in v5.0.

---

## 11. Docs update requirements

Every sprint's close-out checklist must verify:

- [ ] Operator doc under `docs/api/` (created in S9, updated per-sprint as new shapes arrive).
- [ ] `docs/api/openapi.yaml` — every new endpoint documented with request / response schemas.
- [ ] `internal/server/web/openapi.yaml` resynced.
- [ ] `docs/api-mcp-mapping.md` — REST → MCP row for every new endpoint.
- [ ] `docs/architecture-overview.md` — subsystem ownership-map row for `internal/observer`.
- [ ] `docs/data-flow.md` — entry pointing to the observer flow diagram.
- [ ] `docs/flow/bl171-observer-flow.md` (S9), and additional flow docs for S11 / S12.
- [ ] `docs/config-reference.yaml` — `observer:` block with every tunable commented.
- [ ] `docs/test-coverage.md` — new tests per sprint.
- [ ] `CHANGELOG.md` — per-version detail.
- [ ] `docs/plans/README.md` — move BL171 from Pending to Shipped once S9 lands; same for BL172 / BL173.
- [ ] **No BL/F/B numbers in any user-visible string** (existing rule — doubled down here because the observer adds many new Settings fields and tooltip strings).

---

## 12. Configuration — `observer.*` YAML block

```yaml
observer:
  # Plugin / in-process collector (Shape A)
  plugin_enabled:   true
  tick_interval_ms: 1000
  process_tree:
    enabled:          true
    top_n_broadcast:  200    # per-tick, largest-CPU first
    include_kthreads: false
  envelopes:
    session_attribution: true       # group per session's pane PID subtree
    backend_attribution: true       # group per LLM backend root + children
    docker_discovery:    true
    gpu_attribution:     true
    backend_signatures:
      claude:    { exec: [claude, claude-code] }
      ollama:    { exec: [ollama] }
      openwebui: { exec: [open-webui] }
      aider:     { exec: [aider] }
      goose:     { exec: [goose] }

  # Federation (S11+)
  peers:
    allow_register: true             # main daemon accepts new peer registrations
    token_ttl_rotation_grace_s: 60
    push_interval_seconds:      5    # peer → primary
    listen_addr:                "0.0.0.0:9001"  # Shape B only

  # Cluster (S12+)
  cluster:
    ebpf_enabled:     false          # Shape C only; requires CAP_BPF
    k8s_metrics_scrape: false
    dcgm_endpoint:    ""             # optional
```

Every key reachable from YAML + REST + MCP + CLI + comm + web UI per the parity rule.

---

## 13. Test + release posture

- **S9 target:** +40 tests (envelope classifier × 10, procfs parse × 8, docker discovery × 6, GPU attribution × 4, REST shape × 6, WS broadcast × 3, config parity × 3). Full suite budget: 1170 → ~1210.
- **Live smoke gate (S9):** operator runs `datawatch observer watch` against the live daemon with an active Claude session + an `ollama run` process, observes the `session:<id>` envelope CPU rise during Claude activity and `backend:ollama` envelope GPU rise during inference.
- **Container maintenance:** S9 requires a `parent-full` rebuild (daemon binary). S11 adds `datawatch-stats` build targets. S12 adds the `Dockerfile.stats-cluster` image. All three helm-chart bumps.

---

## 14. Risk notes

1. **`/proc` walk cost.** 1 s tick + 500-process host = noticeable. Mitigation: cache per-pid static fields (cmdline, comm) between ticks; only re-read `stat` / `io` each tick. Benchmark gate in CI.
2. **Docker socket availability.** Most hosts run datawatch as the operator's user; the docker socket is `docker` group-gated. Mitigation: socket probe fails soft → fall back to `/proc/<pid>/cgroup` parse; operator gets a one-line "docker socket not reachable; using cgroup heuristic" on start.
3. **Token-store compatibility.** Peer tokens reuse `internal/auth` but that package is currently scoped to F10 bootstrap tokens. Add a `scope: "observer-peer"` discriminator so token lookups don't collide.
4. **Envelope label stability.** Session names can change (operator rename). Envelope id is the session's `FullID`, not the name — stable.
5. **Mobile gating.** Mobile team can't start Phase 1 until the v2 wire contract ships. Lock the contract in S9 and freeze it for the mobile window; additive-only changes after.

---

## 15. Explicit non-goals (for this doc)

- **Prometheus endpoint** — later, as a format option, not load-bearing.
- **Long-term TSDB storage** — operators that need it run Prometheus or VictoriaMetrics externally.
- **Alerting on thresholds** — the existing `detection.*` framework handles this path.
- **Windows / macOS process-tree** — trimmed subset only; Linux is the production target.
- **Agent + recursive stats (S13)** — separately scoped; merges into observer once Shape C is live and mobile v1.0 is out.
