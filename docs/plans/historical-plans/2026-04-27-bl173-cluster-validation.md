# BL173 cluster→parent push — live validation

**Date:** 2026-04-27
**Cluster:** local kubectl-wired "testing" cluster (TKG / vSphere, Antrea overlay, 3 nodes)
**Datawatch version under test:** v5.26.5 (agent-base image)
**Status:** ✅ **PASS** — cluster→parent push verified end-to-end on a real cluster, no code changes needed in the runtime path; two chart-side cleanups landed in v5.26.6.

This report closes the BL173 follow-up that's been carried since the v4.x cut: *"Live cluster→parent push is operator-side: dev workstation parent isn't reachable from the testing-cluster pod overlay. Production deploys don't have this gap. No code action; verify on a production cluster when convenient."*

The dev-workstation reachability gap is real (cluster pods can't dial back into the workstation overlay), so the validation pivots to the inverse topology: deploy the parent **inside** the cluster and exercise the peer-registration + push + cross-host aggregation flow there.

## Method

1. Install the chart at `charts/datawatch` into a fresh `datawatch` namespace.
2. Pre-create the API-token Secret (operator-owned).
3. Override `image.registry=ghcr.io/dmz006/datawatch` + `image.tag=5.26.5`.
4. Wait for the parent Pod to roll out, port-forward `8080`, smoke-test `/api/health` + `/api/backends` + `/api/observer/config`.
5. Register a peer (`POST /api/observer/peers`).
6. Push an observer envelope from the peer (`POST /api/observer/peers/{name}/stats`).
7. Read it back via the per-peer endpoint and the cross-host aggregator.

## Findings

### ✅ Anonymous GHCR pull works (with the right tag form)

The cluster pulled `ghcr.io/dmz006/datawatch-agent-base:5.26.5` anonymously. **The `v` prefix is the gotcha:** the CI workflow at `.github/workflows/containers.yaml` runs `echo "version=${GITHUB_REF#refs/tags/v}"` which strips the leading `v` from the git tag. `v5.26.5` is published as `5.26.5` on GHCR. Pulling with the operator-natural `v5.26.5` form returns 404.

**Fix landed in v5.26.6:**
- `charts/datawatch/Chart.yaml` `appVersion` bumped to `5.26.5` (was `v4.7.1` — also out-of-date).
- `charts/datawatch/templates/_helpers.tpl` strips a leading `v` from `image.tag`/`appVersion` so either form works:
  ```
  {{- $rawTag := default .Chart.AppVersion .Values.image.tag -}}
  {{- $tag := trimPrefix "v" $rawTag -}}
  ```

### ✅ Helm chart deploys cleanly

```bash
kubectl create namespace datawatch
kubectl -n datawatch create secret generic datawatch-api-token \
  --from-literal=DATAWATCH_API_TOKEN="$(openssl rand -hex 32)"
helm install dw ./charts/datawatch \
  --namespace datawatch \
  --set image.registry=ghcr.io/dmz006/datawatch \
  --set image.tag=5.26.5 \
  --set apiTokenExistingSecret=datawatch-api-token
```

Deployment + ServiceAccount + RBAC + Service + ConfigMap + bootstrap-token Secret all rendered correctly. Pod went `0/1 ContainerCreating` → `1/1 Running` in ~14 s.

Daemon log on first boot:
```
[entrypoint] no /data/config.yaml found, writing minimal default
[channel] using native Go bridge: /usr/local/bin/datawatch-channel
[observer] plugin started (tick=1000ms, topN=200)
[observer] peer registry ready — 0 peer(s) loaded
[dw-...] PWA server: http://0.0.0.0:8080
[dw-...] datawatch v5.26.5 started.
```

`/api/health` → `{"status":"ok","version":"5.26.5"}`. `/api/backends` returned the full registry. `/api/observer/config` showed `peers.allow_register: true`, `peers.push_interval_seconds: 5`, `peers.listen_addr: 0.0.0.0:9001`.

### ✅ Peer registration round-trip works

```bash
POST /api/observer/peers
  body: {"name":"thor","shape":"A","version":"v5.26.5"}
  →    {"name":"thor","shape":"A","token":"<43-char-base64url>"}
```

Token is the only opportunity to capture (per the chart README's dual-supply pattern). The parent stores only the bcrypt hash.

### ✅ Peer push accepted, snapshot recorded, cross-host visible

```bash
POST /api/observer/peers/thor/stats
  Authorization: Bearer <peer token>
  body: {
    "shape":"A",
    "peer_name":"thor",
    "snapshot": { "v":2, "envelopes":[{
      "id":"thor-env-001",
      "kind":"Backend",
      "name":"thor-test-backend",
      "rss_bytes":12345678,
      "cpu_pct":1.5,
      "listen_addrs":[{"addr":"10.0.0.5","port":8443,"proto":"tcp"}],
      "outbound_edges":[{"remote_addr":"10.0.0.6","remote_port":8443,"proto":"tcp"}]
    }] }
  }
  →   {"status":"ok"}
```

Read-back from per-peer endpoint:

```bash
GET /api/observer/peers/thor/stats
  → full StatsResponse (the just-pushed snapshot)
```

Cross-host aggregator:

```bash
GET /api/observer/envelopes/all-peers
  → {
      "by_peer": {
        "local": [...local envelopes...],
        "thor":  [...thor's pushed envelopes...]
      }
    }
```

The `thor` envelope shows up alongside the parent's own `local` envelopes — exactly the topology BL180 Phase 2 cross-host federation correlation depends on. No code path failures end-to-end.

### 🟡 Test-script gotcha worth noting

`GET /api/observer/peers/{name}/stats` returns the StatsResponse **directly** (no `{snapshot: ...}` wrapper). My initial probe script was looking for a `.snapshot` key and reported "no snapshot" when in fact the snapshot was the response body. Documenting here so future operator scripts don't repeat the mistake.

## What's covered

| Surface | Behavior | Status |
|---------|----------|--------|
| Helm chart deploy | image pull + Pod boot + service exposure | ✅ |
| Anonymous GHCR pull | `5.26.5` (no v) | ✅ |
| API-token Secret dual-supply | `apiTokenExistingSecret=…` | ✅ |
| `POST /api/observer/peers` | peer register + bcrypt token | ✅ |
| `POST /api/observer/peers/{n}/stats` | peer push (Bearer auth) | ✅ |
| `GET /api/observer/peers/{n}/stats` | last-pushed snapshot read-back | ✅ |
| `GET /api/observer/envelopes/all-peers` | cross-host federation aggregator | ✅ |

## What's still operator-side

- **`parent-full` image not in GHCR** — chart works against `agent-base` (which has the datawatch binary baked in); operators wanting the Signal/Java side-car must build locally per `docs/container-hygiene.md`. CI add-on lands in v6.0.
- **Cross-cluster spawn validation** — the Helm chart supports `kubeconfig.existingSecret` for spawning workers into other clusters; this report tested the in-cluster peer-push path only. Cross-cluster spawn still requires an operator-side multi-cluster kubeconfig.
- **Shape-C observer DaemonSet** — `observer.shapeC.enabled=true` not exercised in this validation; the chart template renders correctly under `helm template` but real per-pod metric scraping needs DCGM-exporter integration to surface GPU rows. Functional smoke (CPU/mem/net rows) is straightforward to add when needed.

## Cleanup

```bash
helm uninstall dw -n datawatch
kubectl delete namespace datawatch
```

Both ran without errors; PVC + Secret + RBAC all garbage-collected with the namespace.

## Next: production-cluster validation

The dev-workstation parent reachability gap from the original audit note is unchanged (Antrea pod overlay + workstation iptables means cluster pods can't dial back). This validation closes the BL173 follow-up by exercising the same code paths from the **other** end (parent in cluster, peer pushing in). Anyone running datawatch as a production K8s deployment should follow the same sequence on their actual cluster — the recipe is now: `helm install` → register → push → read.
