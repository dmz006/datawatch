# datawatch v5.26.43 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.42 → v5.26.43
**Patch release** (no binaries — operator directive).
**Closed:** Kind-cluster smoke workflow — last big CI residual from v5.26.25 audit.

## What's new

### `kind-smoke` workflow — chart regressions caught pre-tag

Operator directive (v5.26.25 audit): *"Kind-cluster smoke workflow: spin up `kind`, deploy chart, run `release-smoke.sh`. Catches chart regressions before tag."* v5.26.43 ships it.

Pipeline:

```
ubuntu runner
   ├── install kind v0.24.0 + kubectl + helm
   ├── kind create cluster --name dw-smoke
   ├── docker build -f Dockerfile.agent-base -t datawatch-kind-smoke:test
   ├── kind load docker-image datawatch-kind-smoke:test
   ├── helm install ... --set image.pullPolicy=Never (use the loaded image)
   ├── kubectl wait + port-forward svc/dw-smoke-datawatch 18443:8443
   ├── poll /api/health until "status":"ok"
   ├── DW_BASE=https://localhost:18443 bash scripts/release-smoke.sh
   └── (always) kind delete cluster
```

The `release-smoke.sh` script already supports `DW_BASE` env override (added v5.26.9), so the same 40-check sweep that runs against the dev daemon now runs against the chart-deployed daemon end-to-end. Catches:

- Chart template regressions (deployment/service/configmap shape).
- Image entrypoint bugs (the smoke fails fast if the daemon never serves `/api/health`).
- Helm value regressions (image.pullPolicy / persistence / etc.).
- Anything that breaks between "Pod is Ready" and "operator can hit `/api/autonomous/prds`".

### Trigger choices

- **`pull_request`** with paths filter on `charts/**`, `Dockerfile.agent-base`, `Dockerfile.parent-full`, `entrypoint.sh`, `release-smoke.sh`, `cmd/datawatch/**`, `internal/server/**`, and the workflow itself. Anything that can plausibly break the kind run gates the PR.
- **`workflow_dispatch`** for ad-hoc spot checks.
- **NOT** on tag pushes. Kind setup adds 5–7 minutes per release; the PR run is the gate. Once the chart change has merged through a green PR, the tag push doesn't need to re-run.

### Failure visibility

If anything inside the run fails (image build, helm install timeout, smoke check), an `if: failure()` step dumps:

- `kubectl get pods -A`
- `kubectl describe pod -l app.kubernetes.io/instance=dw-smoke`
- `kubectl logs -l app.kubernetes.io/instance=dw-smoke --tail=200`

So the operator can diagnose without re-running locally. The `kind delete cluster` step has `if: always()` so a failed run still cleans up.

### Action SHAs

`actions/checkout` pinned to `34e114876b...` matching the v5.26.38 fleet-wide pinning. No third-party kind/helm action used — the install is plain curl + chmod, deterministic and auditable.

## Configuration parity

No new config knob. The workflow exercises whatever the chart's defaults produce; operators with custom values can spot-check by editing the workflow's `helm install ... --set` line.

## Tests

The workflow itself runs on the next PR that touches one of its trigger paths. Local validation: chart fullname expansion verified (`dw-smoke` + chart `datawatch` → `dw-smoke-datawatch`), `release-smoke.sh` `DW_BASE` env support confirmed.

Smoke unaffected: 40 pass / 0 fail / 1 skip on the dev daemon. Go test suite unaffected: 465 passing.

## Known follow-ups

CI residual list is now empty as far as the v5.26.25 audit is concerned. Open backlog after v5.26.43:

- **PRD-flow phase 3 + phase 4** (per-story execution profile + per-story approval; file association) — design first.
- **PRD-flow phase 6 screenshots + diagrams refresh.**
- **Service-function smoke audit residuals** — schedule CRUD, memory layers, MCP tools, F10 lifecycle, channel send.
- **Mempalace alignment audit** — produces a plan doc.
- **datawatch-app PWA mirror** (issue #10).
- **v6.0 cumulative release notes** (operator-prepared at cut).
- **GHCR past-minor cleanup** (needs PAT).

## Upgrade path

```bash
git pull
# No daemon restart needed — workflow-only change. Push a PR that
# touches charts/ or Dockerfile.agent-base to verify the kind-smoke
# job runs cleanly. workflow_dispatch from the Actions UI is the
# fastest way to spot-check without a code change.
```
