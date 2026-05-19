# Post-v7 — ComputeNode Routing dimension

**Status:** roadmap (not in v7.x).
**Owner:** TBD.
**Created:** 2026-05-09 (alpha.23 design conversation, operator).

## Context

In v7.0.0-alpha.23, `ComputeNode.Kind` was reduced to LLM-API protocol (currently `ollama` and `openai-compat`). The old enum (`local`, `remote`, `ssh`, `docker`, `k8s`, `remote-proxy`) mixed two dimensions: WHAT API the daemon talks AND HOW the daemon gets there.

The "HOW" dimension belongs to a separate field — provisionally named **Routing** — that does not exist yet. Operator confirmed Q9: "if routing field is not available yet it should not be visible until it is, just make sure it's documented so it's not forgotten in the plan."

This file is that plan.

## Proposed schema (v8.0 candidate)

Add a `Routing` field to `internal/compute/node.go` `Node`:

```go
type RoutingMode string

const (
    RoutingDirect          RoutingMode = "direct"            // daemon hits node.Address directly (default; covers v7.x "local" + "remote-host")
    RoutingK8sSidecar      RoutingMode = "k8s-sidecar"       // daemon installs a tailscale/k8s sidecar; URL routes through service mesh
    RoutingDockerNetwork   RoutingMode = "docker-network"    // daemon spins up the LLM container on a docker network; URL is container name
    RoutingDatawatchProxy  RoutingMode = "datawatch-proxy"   // points at another datawatch instance's /api/proxy/llm/<name> (federation)
)

// Node struct gains:
Routing RoutingMode `yaml:"routing,omitempty" json:"routing,omitempty"` // default: RoutingDirect
```

Empty / `direct` is the v7.x behavior (daemon does an HTTP call to `node.Address`).

## Routing-specific fields

### `k8s-sidecar`

- `routing_k8s.cluster_profile` — references a `ClusterProfile` (existing).
- `routing_k8s.namespace` — defaults to `default`.
- `routing_k8s.service_name` — k8s Service the sidecar exposes; URL becomes `<service>.<namespace>:<port>`.
- Daemon side: extends the existing tailscale sidecar code (BL243) to also install a k8s service-mesh sidecar pod when this routing is selected.

### `docker-network`

- `routing_docker.network_name` — docker bridge network.
- `routing_docker.container_name` — daemon spins up `<container_name>` running ollama (or the container the operator specifies).
- Daemon side: uses local docker socket; extends the existing docker-runner.

### `datawatch-proxy`

- `routing_proxy.peer` — datawatch peer name (registered observer).
- `routing_proxy.llm_name` — remote LLM registry name to invoke through `<peer>/api/proxy/llm/<llm_name>`.
- Daemon side: uses the existing `agents.PeerBroker` (BL104) for peer-to-peer messaging; extended for inference round-trip.

## PWA implications

- Add-form gains a `Routing` dropdown, default `direct`.
- Routing-specific fields conditional-render below the dropdown (similar to the kind-aware Hardware section pattern).
- Smoke probe: `compute.Probe()` learns to route through the selected mechanism (k8s sidecar reachable? docker network exists? peer reachable?).
- Migration: existing v7.x nodes default `Routing=direct`; no migration needed because the field is additive.

## Why deferred to v8.0

- k8s-sidecar requires extending tailscale sidecar code beyond what BL243 ships.
- docker-network requires a docker container lifecycle manager.
- datawatch-proxy requires extending PeerBroker for inference RPC.
- Each is a multi-sprint effort. Better as v8.0 than rushing in v7.x.

## Operator decision audit trail

- 2026-05-09 — operator: "i do want to be able to have a k8s or docker or remote datawatch instances even through proxy able to route to a compute node but that may be an 8.0 feature".
- 2026-05-09 — operator (Q9): routing field hidden in v7.x; documented here so it's not forgotten.

## Related

- `docs/plans/post-v7-llm-kinds.md` — the orthogonal dimension (WHAT the Node speaks).
- BL243 — Tailscale sidecar (foundation for `k8s-sidecar`).
- BL104 — PeerBroker (foundation for `datawatch-proxy`).
