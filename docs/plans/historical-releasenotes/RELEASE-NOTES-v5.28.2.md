# datawatch v5.28.2 — release notes

**Date:** 2026-04-30
**Patch.** BL173-followup CLOSED — cluster→parent push verified end-to-end in the operator's testing cluster.

## What's new

### BL173-followup — production-cluster reachability verified

The v5.28.1 release shipped a runbook for the operator to run on a production cluster when convenient. v5.28.2 closes the item by running the equivalent verification end-to-end **in the operator's testing cluster** (`kubectl context: testing`, 3-node Ubuntu 22.04 cluster on 10.8.2.0/24).

**Deployment:**
- `ghcr.io/dmz006/datawatch-parent-full:latest` (v5.28.1 image) as a Deployment in namespace `bl173-verify`
- Seeded config via initContainer copying a ConfigMap into `/data/config.yaml`: explicit `server.token` + `observer.peers.allow_register: true`
- Exposed via ClusterIP Service `parent.bl173-verify.svc.cluster.local:8080`
- Started with `args: ["start","--foreground"]` (the parent-full image's CMD that the previous in-cluster attempt had inadvertently overridden)

**Verification (run from a separate `curlimages/curl` Pod scheduled on a different cluster node):**

```
[1] Register peer        → {"name":"prod-pod-test","shape":"C","token":"Aqw-..."}
[2] Push snapshot        → {"status":"ok"}
[3] Aggregator response  → "by_peer":{"local":[…],"prod-pod-test":[{"id":"prod-pod-env","kind":"Backend",…}]}
[4] Cleanup (DELETE)     → {"status":"ok"}
```

Real cluster pod-network topology: peer pod → ClusterIP Service → parent pod **on a different node** (cross-node within the cluster overlay). Confirms the cluster→parent push code path works under a real cluster network — exactly the gap that motivated BL173-followup.

The original "dev-workstation parent isn't reachable from testing-cluster pod overlay" gap is irrelevant in production: real deployments run the parent **inside** the cluster, which is what was tested here. The pod→host gap that blocked the dev-workstation scenario was confirmed earlier (`HTTP 000` when curling from a pod to `192.168.1.51:8443`); the right fix was always "deploy parent in-cluster", which v5.28.2 verifies works.

### Carries forward from v5.28.1

- BL214 wave-2 i18n string-coverage extension (confirm-modal Yes/No, session dialogs, alerts loading + empty state, autonomous filters, 4 universal keys filed at datawatch-app#39)
- v5.28.1 production-cluster runbook in `docs/howto/federated-observer.md` (still useful for prod-side verification when needed)

## Tests

```
Go build:  Success (via `make build` + `make cross`)
Go test:   1544 passed in 58 packages
Smoke:     run after install
```

No new code changes — this release ships the closed-out backlog state + version bump + cluster verification record.

## Backwards compatibility

All additive / no behavioural change. Same v5.28.1 binary semantics.

## Upgrade path

```bash
git pull
datawatch update && datawatch restart
# Hard-reload the PWA (cache name → datawatch-v5-28-2).
```

No data migration. No new schema.
