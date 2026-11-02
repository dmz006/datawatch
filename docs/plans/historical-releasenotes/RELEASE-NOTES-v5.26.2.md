# datawatch v5.26.2 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.1 → v5.26.2
**Patch release** (no binaries / containers — operator directive: every release until v6.0 is a patch).
**Closed:** Setup howto — Helm/k8s install with secrets + ready-to-code path

## What's new

### Setup howto — Helm install with secrets

Operator: *"Setup howto doesn't have helm k8s install with secrets and swing up gir and being ready to code."*

The `charts/datawatch/` chart has shipped since v4.7.x with full dual-supply secret support (inline for dev, `existingSecret:` for prod with SealedSecret/ExternalSecret/Vault), but `docs/howto/setup-and-install.md` only covered the four binary/container paths. Operators dropping onto a fresh cluster had to read the chart's own `values.yaml` to figure it out.

v5.26.2 adds **Option E — Helm on Kubernetes** to the install-path table and walks the minimum-viable install end-to-end:

1. Create the namespace.
2. Pre-create the API-token Secret (`datawatch-api-token`) — operator picks the value, chart references by name so the token never lands in `values.yaml`.
3. (optional) TLS Secret (`datawatch-tls` — `kubernetes.io/tls`).
4. (optional) git token Secret (`datawatch-git-token`) for the BL113 worker token-broker.
5. `helm install` with `image.registry`, `image.tag`, all three `existingSecret` knobs, and `persistence.enabled=true`.
6. Verify via `kubectl rollout status` + `kubectl port-forward`.

Plus three follow-on patterns:

- **NFS-backed persistence** — wire `persistence.storageClass` to an NFS-fronted StorageClass for shared / multi-Pod / off-node storage. Both common providers are walked: the **CSI driver** (recommended, dynamic provisioning) and the lighter **nfs-subdir-external-provisioner** sidecar. Includes the `accessMode=ReadWriteMany` callout for future HA scaling.
- **Cross-cluster spawns** — project a multi-cluster kubeconfig as `datawatch-kubeconfig` Secret + `helm upgrade --reuse-values --set kubeconfig.existingSecret=…`.
- **Per-cluster Shape-C observer** — the `observer.shapeC.enabled=true` DaemonSet pattern, after registering the cluster as a peer.

### Setup howto — Ready to code

Same operator note. The smoke-test session just runs `echo`, but operators want the path from "binary on disk" to "actually coding against an LLM". v5.26.2 adds a four-step **Ready to code** section:

1. **Clone the repo** — `git clone` locally, with a callout for K8s installs that need `gitToken.existingSecret` for the worker token-broker (or a mounted SSH Secret per `container-workers.md`).
2. **Wire one LLM backend** — concrete invocations for claude-code, Ollama, OpenWebUI; `datawatch setup llm` to flip the default.
3. **First real coding session** — `datawatch session start --project-dir … --llm-backend … --task …` walked through end-to-end, with what each PWA tab shows (Output / Channel / Response) and how to send follow-ups.
4. **Pre-commit / pre-push integration** — `--before-cmd` / `--after-cmd` one-liner pair that gates session completion on lint + tests, with a pointer to `pipeline-chaining` for full DAG semantics.

## Configuration parity

No new config knob. Pure docs.

## Tests

1392 still passing. No code changes outside version bumps.

## Known follow-ups

- Design doc audit / refresh — `docs/design.md` + `docs/architecture.md` + `docs/architecture-overview.md`
- datawatch-app#10 catch-up issue
- Container parent-full retag
- GHCR container image cleanup
- gosec HIGH-severity review
- v6.0 cumulative release notes (full patch series compacted into one major)

## Upgrade path

```bash
git pull          # patch series — no binary update path
# Browse the new sections at /diagrams.html → How-tos → setup-and-install
# Or read directly: docs/howto/setup-and-install.md
```
