# BL173 follow-up checks (post-v4.5.0)

**Filed:** 2026-04-25 alongside the v4.5.0 release.
**Owner:** operator (requires production-ish env unavailable on dev laptop).

The v4.5.0 release ships the complete Shape C framework but three
pieces can't be exercised on the laptop. Each item below is the minimum
verification + the patch-release the fix would land in if something
breaks.

## 1. eBPF kprobe attach

**Setup needed:** Linux ≥ 5.10, BTF, `clang` + `linux-headers` for
`make ebpf-gen`, `CAP_BPF` + `CAP_PERFMON` granted (via cluster
manifest or `datawatch setup ebpf --target stats`).

**Verify:**
```bash
make ebpf-gen                          # generates netprobe_bpfel.{go,o}
make install                           # rebuild parent with the .o linked
sudo setcap cap_bpf,cap_perfmon=ep /home/dmz/.local/bin/datawatch
datawatch start --foreground &
curl -ks https://127.0.0.1:8443/api/observer/stats | jq .host.ebpf
# expect: kprobes_loaded=true, message="kprobes attached — per-process net live"
```

**Failure modes to watch:**
- `verifier rejected program` → kernel too old or BTF missing.
  Capture `dmesg | grep BPF` and file as v4.5.x patch.
- `permission denied loading program` → setcap didn't stick. Re-run
  setcap on the actual binary path (mind symlinks).
- `Loaded()=true but Read() returns []` → maps lookup logic broken.
  Patch the loader.

## 2. DCGM scrape (per-pid GPU)

**Setup needed:** NVIDIA GPU host, NVIDIA driver + DCGM exporter
running on `:9400/metrics`.

**Verify:**
```bash
datawatch-stats --shape C --datawatch https://primary:8443 \
    --name gpu-box --insecure-tls
# wait one push interval (10s default), then:
curl -ks https://primary:8443/api/observer/peers/gpu-box/stats \
    | jq .cluster.nodes
# expect non-empty array if GPU pods are running
```

**Things that might break:**
- DCGM exporter URL doesn't match the default
  (`http://localhost:9400/metrics`) — pass `--dcgm-url`.
- DCGM `DCGM_FI_PROF_PROCESS_USAGE` not enabled in the exporter
  config. Operator-side tweak; document, don't patch.
- Exporter behind auth → not currently supported. Open a v4.5.x
  patch task if anyone hits it.

## 3. k8s metrics-server scrape

**Setup needed:** k8s cluster with metrics-server installed,
`observer.shapeC.enabled=true` in the Helm values.

**Verify:**
```bash
helm upgrade --install datawatch oci://ghcr.io/dmz006/charts/datawatch \
    --version 0.19.0 \
    --set observer.shapeC.enabled=true \
    --set observer.shapeC.parentURL=https://primary:8443
kubectl get pods -l datawatch.shape=C
# wait for ready, then:
kubectl exec -it $(kubectl get pod -l datawatch.shape=C -o name | head -1) \
    -- curl -s 127.0.0.1:9001/api/stats | jq .cluster.nodes
# expect one entry per node in the cluster
```

**Things that might break:**
- ServiceAccount RBAC missing `metrics.k8s.io` GET → 403 in pod logs.
  The Helm template grants this; verify `kubectl get clusterrolebinding`.
- metrics-server itself not installed → pod silently reports empty
  `cluster.nodes`. Pre-flight check would help — file as v4.5.x.
- distroless image refuses to start because the operator passed
  `command:` instead of `args:` → fix the manifest.

## 4. Container size measurement

Once the cluster image lands at ghcr, measure the size before/after
the bpf2go inclusion:

```bash
docker pull ghcr.io/dmz006/datawatch-stats-cluster:v4.5.0
docker images ghcr.io/dmz006/datawatch-stats-cluster
# expected: ≤ 100 MB compressed (distroless + ~10 MB Go binary)
```

Document the result in v4.5.x as a CHANGELOG note.

## 5. Long-running soak test

Leave Shape C running for 24 h on at least one node. Watch:
- `last_push_at` should never go stale (push count = 24h / 10s
  ≈ 8640 pushes per peer).
- bcrypt-hashed token storage should not regress: parent restart
  preserves the registry; peer-side reload picks up the on-disk
  token.
- eBPF maps shouldn't OOM the kernel (16k entry cap; eviction
  not yet implemented — patch if someone hits the cap on a host
  with > 16k pids).

## What NOT to verify here

- Multi-cluster federation tree — out of scope for S12; tracked as
  S13.
- ROCm / Intel level_zero GPU paths — scoped, not implemented.
- Per-pod alert routing — defer.
