# BL173 — Sprint S12: Shape C cluster observer container

**Status:** Design — implementation pending
**Filed:** 2026-04-25
**Predecessors:**
  [BL171 / S9 v4.1.0 — observer substrate + Shape A](2026-04-22-bl171-datawatch-observer.md) ·
  [BL172 / S11 v4.4.0 — Shape B standalone daemon](2026-04-25-bl172-shape-b-standalone-daemon.md)
**Target release:** v4.5.0
**GitHub:** [#20](https://github.com/dmz006/datawatch/issues/20)

## Context

S9 (v4.1.0) shipped the observer substrate + Shape A in-process plugin
with optional eBPF on the parent's binary. S11 (v4.4.0) shipped Shape B
— the standalone `datawatch-stats` daemon that pushes to a primary
parent over HTTPS.

Shape C is the third deployment mode: a **privileged cluster container**
that runs as a sidecar in Kubernetes / Docker Compose, owns the full
eBPF + DCGM + cgroup + k8s-metrics-scrape responsibilities, and pushes
to the same `/api/observer/peers/*` surface Shape B uses.

What separates Shape C from Shape B isn't the wire contract — it's
**privilege + deployment**. Shape B is a single binary on a host; Shape
C is an OCI image deployed via Helm with `securityContext.capabilities:
add: [BPF, PERFMON, SYS_RESOURCE]` and a hostPath mount of `/sys`.

## Scope

In scope:
- New OCI image `ghcr.io/dmz006/datawatch-stats-cluster:vX.Y.Z` built
  via `docker/dockerfiles/Dockerfile.stats-cluster` (multi-stage,
  distroless runtime).
- Real eBPF kprobes (`kprobe/tcp_sendmsg`, `kprobe/tcp_recvmsg`,
  `kprobe/udp_sendmsg`, `kprobe/udp_recvmsg`) compiled via `bpf2go`,
  populating `net.per_process` + `envelope.net_{rx,tx}_bps`.
- DCGM scrape for NVIDIA GPU per-process telemetry (replaces
  `nvidia-smi` shell-out on Shape C — DCGM is the right path inside
  containers).
- k8s metrics-server scrape → `cluster.nodes[]` so Shape C can report
  per-node CPU/mem alongside per-process envelopes.
- Helm chart snippet under `charts/datawatch/templates/observer-cluster.yaml`
  + values entries.
- docker-compose snippet for single-host Docker deployments.
- PWA: when `/api/observer/stats` returns non-empty `cluster.nodes`,
  render a new "Cluster nodes" subsection above the existing envelopes.

Out of scope:
- Multi-cluster federation tree (defer to S13+).
- ROCm / Intel level_zero GPU paths (scoped, not implemented).
- Per-pod alert routing (defer).

## Wire contract

Reuses `StatsResponse v2` unchanged. Shape C populates two fields the
other shapes leave empty:

```json
{
  "v": 2,
  "shape": "C",
  "host": { … },
  "envelopes": [ … ],
  "net": {
    "per_process": [ {"pid": 1234, "rx_bps": 12345, "tx_bps": 6789}, … ]
  },
  "cluster": {
    "nodes": [ {"name": "node-1", "cpu_pct": 45.2, "mem_used_mb": 8192, …} ]
  }
}
```

Push uses the same `POST /api/observer/peers/{name}/stats` endpoint
as Shape B with the same bearer token flow.

## Deliverables

### 1. eBPF programs (BL173.2)

Build with `cilium/ebpf`'s `bpf2go` codegen so the resulting binary
embeds the compiled BPF object — no runtime `clang` required.

```
internal/observer/ebpf/
  netprobe.bpf.c       — kprobe/tcp_*, kprobe/udp_* per-pid byte counters
  netprobe_bpfel.go    — bpf2go output (linux/amd64)
  netprobe_bpfeb.go    — bpf2go output (linux/arm64 BE — unused but generated)
  loader.go            — load program + create PerProcessNetReader{ Read() … }
  loader_test.go       — happy + missing-CAP_BPF + verifier-rejection paths
```

Loader is invoked from `internal/observer.Collector` when
`cfg.EBPFEnabled == "true"` (Shape C requires; Shapes A/B opt in).
On `kprobe/tcp_sendmsg`: increment `tx_bytes[pid] += sk->sk_wmem_queued`
delta. On `kprobe/tcp_recvmsg`: increment `rx_bytes[pid] += <return val>`.

### 2. DCGM GPU scrape (BL173.3)

`internal/observer/gpu_dcgm.go` — pulls from a sidecar DCGM exporter
(NVIDIA's official Prometheus exporter at `localhost:9400/metrics`).
Parses `DCGM_FI_DEV_GPU_UTIL` / `DCGM_FI_DEV_FB_USED` per-PID via the
DCGM_FI_PROF_PROCESS_USAGE family. Falls back to nothing if DCGM isn't
exposed — a Shape C container without GPUs reports zero per-process
GPU metrics rather than erroring.

### 3. k8s metrics-server scrape (BL173.4)

`internal/observer/cluster_k8s.go` — when `KUBERNETES_SERVICE_HOST` env
is set + `cfg.Cluster.K8sMetricsScrape == true`, scrape
`/apis/metrics.k8s.io/v1beta1/nodes` using the in-pod service-account
token. Populates `cluster.nodes[]`. Refresh once per minute (heavier
than the per-second collector tick).

### 4. Distribution

```
docker/dockerfiles/Dockerfile.stats-cluster
  - Stage 1: golang builder with bpf2go-compiled object
  - Stage 2: gcr.io/distroless/cc-debian12 (no shell, no apt) +
    /usr/local/bin/datawatch-stats-cluster
  - HEALTHCHECK uses /healthz on the standalone listener

charts/datawatch/templates/observer-cluster.yaml
  - DaemonSet so each k8s node gets one Shape C pod
  - hostPID: true, securityContext.capabilities: [BPF, PERFMON,
    SYS_RESOURCE], hostPath mounts of /sys + /proc + /var/run/docker.sock
  - Reads token from a Secret (mounted at /etc/datawatch-stats/peer.token)

docker-compose.shape-c.yml
  - Single-host equivalent for Docker users without Kubernetes
  - cap_add: [BPF, PERFMON], pid: host, volumes for /sys + /proc

Makefile target:
  cluster-image:
    docker buildx build --platform linux/amd64,linux/arm64 \
        -f docker/dockerfiles/Dockerfile.stats-cluster \
        -t ghcr.io/dmz006/datawatch-stats-cluster:$(VERSION) --push .
```

### 5. PWA cluster nodes subsection

When `/api/observer/stats` returns non-empty `cluster.nodes`:
- New collapsible "Cluster nodes" subsection above envelopes.
- One row per node: name + CPU/mem bar + pod count.
- Click → drill-down by-pod (uses the existing envelope panel scoped
  to `kind: pod`).

## Open questions

1. **Distroless vs alpine** — Distroless wins on size + attack
   surface but operators can't `kubectl exec` into it for debugging.
   Compromise: ship distroless as default, document an `alpine`
   variant (`Dockerfile.stats-cluster-debug`) for emergency triage.
2. **eBPF kernel-version matrix** — `bpf2go` outputs work on Linux
   ≥ 4.18 with BTF, but tcp_sendmsg signature changed across LTS
   kernels. Pick the modern signature and document the minimum
   (5.10 LTS) — operators on 4.x get a clean degrade-to-/proc-only
   message rather than a verifier failure.
3. **DCGM dependency** — Do we ship the DCGM exporter as a sub-
   container in the same DaemonSet pod, or require operators to
   install it separately? Sub-container = simpler ops, larger
   manifest. Default to sub-container, document opt-out.
4. **Push interval on Shape C** — Cluster-scale pushes might overwhelm
   a small parent. Default 10 s instead of 5 s; operator override
   via `--push-interval`.
5. **K8s RBAC** — DaemonSet needs ServiceAccount + RoleBinding for
   metrics-server. Helm chart includes both with sane defaults.

## Sprint plan (6 tasks, ~5 days)

| # | Task | Notes |
|---|---|---|
| 1 | `internal/observer/ebpf/` package + bpf2go integration | netprobe.bpf.c + loader; tests cover load + degrade-on-missing-CAP |
| 2 | DCGM scrape + k8s metrics-server scrape | independent collector goroutines feeding the StatsResponse builder |
| 3 | `cmd/datawatch-stats-cluster/` binary (or repurpose datawatch-stats with `--shape C`) | decision in task 3 — `--shape` flag is leaner |
| 4 | `Dockerfile.stats-cluster` + `cluster-image` Makefile target + multi-arch buildx | sub-container DCGM exporter + distroless runtime |
| 5 | Helm chart DaemonSet + values + RBAC | charts/datawatch/templates/observer-cluster.yaml |
| 6 | PWA cluster.nodes subsection | conditional render when payload non-empty |

## Acceptance criteria

- [ ] `helm upgrade --install datawatch oci://… --set observer.shapeC.enabled=true`
      deploys the DaemonSet and the new pods register as peers within
      30 s of node ready.
- [ ] On a host with NVIDIA GPU, `cluster.nodes[]` and
      `net.per_process[]` are populated within 60 s of pod start.
- [ ] On a host without GPU or with eBPF unavailable, the pod still
      pushes a useful `StatsResponse v2` (cpu/mem from /proc) instead
      of crashlooping.
- [ ] PWA shows the "Cluster nodes" subsection on a multi-node cluster
      and hides it on single-node deployments.
- [ ] `kubectl logs <pod>` is empty of stack traces under steady-state
      operation.

## Dependencies

- BL172 (S11) — must be released and stable. ✅ shipped v4.4.0.
- F10 Helm chart infrastructure for the templates dir layout.
- DCGM exporter availability on the target cluster (operator-provided
  or sub-container).

## What can land independently of full S12

- Task 1 (eBPF loader) lands first — Shapes A + B inherit it
  immediately so operators with `CAP_BPF` get per-process net even
  without deploying Shape C.
- Task 2 DCGM piece is independent of the cluster shape; Shape A/B
  benefit if the operator runs DCGM exporter on the host.

These two can ship as v4.4.x patches. The full Shape C image lands
when the manifest + buildx pipeline are wired (tasks 3-5).
