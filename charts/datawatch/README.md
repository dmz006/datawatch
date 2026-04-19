# datawatch (Helm chart)

Datawatch parent for in-cluster K8s deploys. Pairs with the F10
Sprint 4 K8s driver — once the parent is running here, you spawn
worker Pods (in this namespace by default) via the existing
`/api/agents` REST API or `datawatch agent spawn` CLI.

## Quickstart

```sh
helm install dw ./charts/datawatch -n datawatch --create-namespace \
  --set image.registry=ghcr.io/your-org/datawatch \
  --set image.tag=v2.4.5 \
  --set publicURL=http://datawatch.datawatch.svc.cluster.local:8080
```

**`image.registry` has no default** — point it at your own registry
(GHCR, Harbor, GitLab Container Registry, ECR, …) before install or
the Pod will `ImagePullBackOff`. See [docs/registry-and-secrets.md](../../docs/registry-and-secrets.md)
for the full per-channel registry/k8s/secret configuration walkthrough.

The default `service.type=ClusterIP` is intentional — datawatch is
single-tenant and not designed for direct internet exposure. Wrap
with an Ingress (with auth middleware) or expose via VPN.

## Common values

| Knob | Default | Notes |
|------|---------|-------|
| `image.registry` | `""` | **set this** — your registry + image-name prefix (`ghcr.io/your-org/datawatch`, `harbor.example.com/datawatch`, `localhost:5000/datawatch` for local dev). Set `imagePullSecret` to the matching Secret name when the registry is private. |
| `image.tag` | `""` (→ `Chart.appVersion`) | pin a specific release; `latest` not recommended |
| `replicas` | `1` | HA pinned to 1 — see Limitations |
| `publicURL` | `""` | set to the routable URL spawned workers should dial home to |
| `tls.enabled` | `false` | when `true`, supply `cert`+`key` inline OR `existingSecret` (recommended for prod) |
| `apiToken` | `""` | bearer token for `/api/*` auth; empty = no auth (dev only) |
| `persistence.enabled` | `false` | switch to `true` + set `storageClass`/`size` for sessions/memory durability |
| `postgres.url` | `""` | empty → embedded SQLite at `$DATA_DIR/memory.db` |
| `rbac.create` | `true` | grants the parent's K8sDriver pod create/delete in this namespace |

See `values.yaml` for the full list including `agents.*` knobs that
mirror the standalone config schema in `internal/config/config.go`.

## Spawning workers from inside the cluster

When `rbac.create=true` (default), the parent Pod has a ServiceAccount
with permission to create + delete Pods in its own namespace. Cluster
Profiles with `kind: k8s` and `context: ""` will use the in-cluster
ServiceAccount automatically (kubectl picks it up from
`/var/run/secrets/kubernetes.io/serviceaccount/`).

For cross-namespace or cross-cluster spawns, set `rbac.create=false`,
provision your own ClusterRole + ClusterRoleBinding, and point
ClusterProfiles at the appropriate kubectl `context` and `namespace`.

## TLS pinning

When `tls.enabled=true`, the parent computes the SHA-256 of its leaf
cert at startup and injects `DATAWATCH_PARENT_CERT_FINGERPRINT` into
every spawned worker Pod. Workers refuse to connect to any other cert
— **rotate the parent cert and the workers' env in lockstep**, or
they'll fail to bootstrap after the rotation.

The parent's cert is also served at `GET /api/agents/ca.pem` for
operator setup.

## Limitations

- Single-replica only. Multi-replica needs leader election + state
  externalisation; tracked as a future sprint.
- No Ingress template included — bring your own (the chart deliberately
  stays out of the Ingress controller business; configurations vary
  too much across nginx-ingress / traefik / cloud LBs).
- The `agents.callback_url` and `server.public_url` chain still
  applies — when running in-cluster, set `publicURL` (above) so
  spawned workers know where to call home.

## Smoke

```sh
helm install dw ./charts/datawatch -n datawatch --create-namespace -f testing-values.yaml
kubectl -n datawatch wait --for=condition=Ready pod -l app.kubernetes.io/name=datawatch --timeout=120s
kubectl -n datawatch port-forward svc/dw-datawatch 8080:8080 &
curl -sf http://localhost:8080/healthz
```

Once green, the S4.5 spawn smoke (`tests/integration/spawn_docker.sh`
adapted for k8s ClusterProfile) becomes the next acceptance gate.
