# datawatch v5.26.59 — release notes

**Date:** 2026-04-28
**Spans:** v5.26.58 → v5.26.59
**Patch release** (no binaries — operator directive).
**Closed:** ZAP customized per interface (PWA + API + diagrams).

## What's new

Operator-asked: *"Are zap audits customized to api, pwa and other interfaces?"*

Honest pre-v5.26.59 answer: no. The v5.26.58 ZAP workflow used a single passive baseline scan against the daemon's HTTP root, which sweeps the PWA + API + diagrams together but doesn't tune for any specifically.

v5.26.59 splits the workflow into three passes against a single port-forwarded daemon:

| Pass | Scan type | Target | Action | Mode |
|------|------|------|------|------|
| 1 | Baseline (passive) | `http://localhost:18080/` (PWA root) | `zaproxy/action-baseline@v0.15.0` | fail-on-find |
| 2 | API scan (schema-driven) | `http://localhost:18080/api/` | `zaproxy/action-api-scan@v0.10.0` with `docs/api/openapi.yaml` | fail-on-find |
| 3 | Baseline (passive) | `http://localhost:18080/diagrams.html` | `zaproxy/action-baseline@v0.15.0` | advisory (warn-only) |

### Why three passes

- **PWA baseline (1)** spiders the JS-driven SPA — picks up CSP / X-Frame-Options / cookie scope issues on the PWA's own pages.
- **API scan (2)** consumes the daemon's existing OpenAPI spec at `docs/api/openapi.yaml` and exercises every documented endpoint with shape-aware probes (parameter fuzzing in passive mode, missing-auth detection, response-contract checks). This is dramatically richer than what a generic spider produces against the same surface.
- **diagrams baseline (3)** runs separately because the diagrams viewer is markdown rendered through marked.js — its findings (CSP for embedded mermaid, anchor reflection in URL fragments) shouldn't drown in the PWA's bigger result set. Marked **advisory** because the surface is mostly static and the failure modes there don't gate releases.

### Tear-up cost

A single `kind` cluster is shared by all three passes — total runtime ~25–35 minutes (kind setup + chart deploy + 3 ZAP scans). The previous single-baseline workflow ran ~12 minutes, so the new split adds ~15 minutes for substantially better coverage.

### Operator-runnable

Triggered via `workflow_dispatch`. Inputs:

- `target_host` — defaults to `localhost:18080` (the kind port-forward target). Override to point at a non-CI daemon (e.g. a dedicated auditing instance).

The workflow creates GitHub issues for findings above the configured ZAP rule thresholds; titles are prefixed with which pass found them (`OWASP ZAP — PWA baseline`, `OWASP ZAP — API scan`, `OWASP ZAP — diagrams.html baseline`).

## Configuration parity

No new config knob. The OpenAPI spec was already shipped at `docs/api/openapi.yaml` (and embedded at `internal/server/web/openapi.yaml`).

## Tests

CI-only change. Smoke unaffected (58/0/1). Go test suite unaffected (465 passing).

## Known follow-ups

- Authenticated ZAP scans — current passes are unauthenticated. Endpoints that require a bearer token are skipped (or 401-fingerprinted). Adding auth needs a `secrets.ZAP_TOKEN` and per-action `auth.*` config; tracked.
- ZAP **full scan** (active attacks) — out of scope for this patch. The full scan can DoS the daemon under test, so it needs an isolated runner config and operator approval per run.

## Upgrade path

```bash
git pull
# No daemon restart needed — workflow-only change. Trigger via
# Actions → owasp-zap → Run workflow.
```
