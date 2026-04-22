# Observer — unified stats + process-tree + sub-process monitor

**Shipped in v4.1.0 (Sprint S9).** Replaces the flat `/api/stats` scalars with a structured `StatsResponse v2` payload carrying a host envelope, CPU/mem/disk/GPU details, per-process tree rolled up into per-session and per-LLM-backend envelopes, sessions + backend health, and optional cluster / peer views.

Design doc: [`../plans/2026-04-22-bl171-datawatch-observer.md`](../plans/2026-04-22-bl171-datawatch-observer.md).

## Ways to read the data

- **REST** (v2): `GET /api/stats?v=2` or `GET /api/observer/stats`.
- **REST** (v1 compat): `GET /api/stats` keeps the old flat shape for v1 clients; all flat scalars are also present as top-level aliases inside the v2 response.
- **Live** (WS): `MsgStats` broadcast on the existing `/ws` channel — payload upgraded to StatsResponse v2 at 1 s cadence.
- **MCP tools**: `observer_stats`, `observer_envelopes`, `observer_envelope`, `observer_config_get`, `observer_config_set`.
- **CLI**: `datawatch observer stats|envelopes|envelope <id>|config-get|config-set <json>`.
- **Comm**: via the existing `rest` passthrough (`rest GET /api/observer/envelopes`).

## Envelopes — the headline feature

Each tick, every process visible under `/proc` is classified into an **envelope**:

| Kind | ID format | What lands here |
|---|---|---|
| `session` | `session:<full_id>` | tmux-pane PID subtree for every tracked session (claude + spawned shells / helpers / agents) |
| `backend` | `backend:<name>[-docker\|-k8s]` | process subtree rooted at a known LLM backend executable (ollama, aider, goose, gemini, opencode, open-webui) — including children, and any docker / k8s container wrapping it |
| `container` | `container:<short-id>` | any other containerized process we picked up via `/proc/<pid>/cgroup` parsing |
| `system` | `system` | everything that didn't match — kernel threads, root procs, daemons |

Rolled-up metrics per envelope: `cpu_pct`, `rss_bytes`, `fds`, `threads`, `net_rx_bps` + `net_tx_bps` (Shape C eBPF or Shape A/B with CAP_BPF), `gpu_pct` + `gpu_mem_bytes` (when `nvidia-smi` reports compute apps for the subtree's PIDs).

Sorted by CPU descending — the operator sees "which session / backend is eating the box right now" as the first row.

## Deployment shapes

One wire contract, three deployment options — pick what fits the host:

- **Shape A — in-process plugin (default).** Lives in `internal/observer/`. Unprivileged `/proc` walk + `nvidia-smi` scrape. Default-on; toggle with `observer.plugin_enabled`.
- **Shape B — standalone daemon (`datawatch-stats.service`, Sprint S11).** Tiny binary on a host that isn't running the session manager. Registers with the main daemon over HMAC-auth peer protocol; payload merges into the aggregator's `/api/stats`.
- **Shape C — cluster container (Sprint S12).** Privileged (`CAP_BPF` + `CAP_PERFMON` or `--privileged` + `hostPID: true`). Adds eBPF per-process net, per-cgroup CPU/mem, DCGM GPU, k8s metrics-server scrape.

### eBPF across all three shapes

All three shapes share the same eBPF object; privilege is the only difference:

| Shape | Privilege | Enabled via |
|---|---|---|
| A | unprivileged default; CAP_BPF when granted | `datawatch setup ebpf` (adds `AmbientCapabilities=CAP_BPF CAP_PERFMON` drop-in to `datawatch.service` + flips `observer.ebpf_enabled=true`) |
| B | unprivileged default; CAP_BPF when granted | `datawatch setup ebpf` against the `datawatch-stats.service` unit |
| C | **via k8s / docker manifest only** — no runtime `setup ebpf` command | Helm values `securityContext.capabilities.add: [BPF, PERFMON]` (k8s) or `cap_add: [BPF, PERFMON]` + `pid: host` (compose) |

When eBPF is unavailable, the observer silently degrades to `/proc`-only (no `net.per_process[]`, no `envelope.net_*_bps`). Everything else still renders.

### Shape C — Kubernetes snippet

```yaml
# helm values.yaml — enabling the cluster observer sidecar
observerCluster:
  enabled: true
  image: ghcr.io/dmz006/datawatch-stats-cluster:v4.1.0
  securityContext:
    capabilities:
      add: [BPF, PERFMON, SYS_RESOURCE]
  hostPID: true
```

### Shape C — docker-compose snippet

```yaml
services:
  datawatch-stats-cluster:
    image: ghcr.io/dmz006/datawatch-stats-cluster:v4.1.0
    cap_add: [BPF, PERFMON, SYS_RESOURCE]
    pid: host
    environment:
      DATAWATCH_PRIMARY: https://datawatch.example:8443
      PEER_NAME: cluster-a
```

## Configuration — `observer:` YAML block

```yaml
observer:
  plugin_enabled: true              # Shape A toggle
  tick_interval_ms: 1000            # 1 s cadence (minimum 500)
  process_tree_enabled: true
  top_n_broadcast: 200
  include_kthreads: false
  session_attribution: true
  backend_attribution: true
  docker_discovery: true
  gpu_attribution: true
  ebpf_enabled: auto                # auto | true | false — shared across shapes
```

Every knob is reachable from YAML + REST (`/api/observer/config`) + MCP (`observer_config_set`) + CLI (`datawatch observer config-set`) + comm (`rest PUT /api/observer/config {…}`) per the parity rule.

## Wire shape (v2)

See the design doc for the full JSON contract. Highlights:

- `host.shape`: `"plugin"` | `"daemon"` | `"cluster"` — tells the consumer where the data came from.
- `envelopes[]`: sorted by CPU descending, kind-tagged.
- `processes.tree`: top-N by CPU with ancestors preserved so the tree is always connected.
- `peers[]`: populated only on the aggregator (main datawatch) — lists Shape B / C peers + last-push freshness.
- Every v1 flat field (`cpu_pct`, `mem_pct`, `uptime_seconds`, `sessions_total`, `sessions_running`, `disk_pct`, `gpu_pct`) is preserved at the root so v1 clients keep working.

## Sub-process monitoring examples

**Track claude's children.** If you start a Claude session and it spawns a shell + `rg` + `grep`, the resulting `session:<full_id>` envelope counts all four processes' CPU/RSS/FD together. Drill in: `datawatch observer envelope session:<full_id>` returns the full subtree.

**Watch ollama's runners.** `ollama serve` forks `ollama-runner` children per model. The `backend:ollama` envelope sums them. If Ollama is running inside a Docker container, the envelope id becomes `backend:ollama-docker` and `envelope.container_id` + `envelope.image` are populated.

**GPU attribution.** `nvidia-smi --query-compute-apps=pid,used_memory` maps compute-using PIDs to GPUs; the observer annotates matching envelopes with `gpu_pct` + `gpu_mem_bytes`.

## Not in v4.1.0

- Standalone daemon (Shape B) — arrives in Sprint S11 (v4.2.0).
- Cluster container (Shape C) — arrives in Sprint S12 (v4.2.x).
- Per-process eBPF network — library present in Shape A/B, but the kprobes ship in Sprint S12 alongside the cluster container.
- Mobile Phase 1+2 consumption (structured cards, WS live stream) — lands in datawatch-app v0.34.x.
