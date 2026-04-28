# datawatch v5.26.58 — release notes

**Date:** 2026-04-28
**Spans:** v5.26.57 → v5.26.58
**Patch release** (no binaries — operator directive).
**Closed:** Session worker badge + 3 new security workflows (gitleaks, dependency-review, OWASP ZAP baseline).

## What's new

Operator-asked: *"Are there any badges to show a session is in docker or k8s or cf and how does it indicate recursive path."* and *"do we have gh runners for security like datawatch-app does with secrets, depends, owasp."*

### 1. Session-card "⬡ worker" badge

When `sess.agent_id` is non-empty (the session lives inside a parent-spawned container worker), the session card now shows a small purple `⬡ worker` pill next to the backend badge. Hover for the agent ID. PRD recursion-depth + parent badges already exist (BL191 Q4, v5.16.0) — confirmed live in `renderPRDRow`.

Future enrichment: full driver-kind (docker/k8s/cf) + cluster-profile name need an agent-record fetch on render — tracked.

### 2. Three new security workflows

| Workflow | Trigger | Action |
|------|------|------|
| `secret-scan.yaml` | PR + push to main + weekly cron + manual | gitleaks-action@v2 — fails on net-new committed secrets |
| `dependency-review.yaml` | PR | dependency-review-action@v3 — fails on HIGH-severity vuln in new/upgraded deps OR copyleft license (`GPL-3.0-only`, `AGPL-3.0-only` denied) |
| `owasp-zap.yaml` | manual dispatch | spins up kind, deploys chart, port-forwards, runs ZAP baseline (passive scan) — operator-triggered before minor/major cuts |

All three use SHA-pinned actions per the v5.26.38 fleet-wide convention.

Combined with the existing `security-scan.yaml` (gosec baseline-diff + govulncheck blocking, v5.26.40), the daemon now has comparable security-CI coverage to `datawatch-app`:

| Concern | Workflow | Mode |
|------|------|------|
| Reachable Go vulns | security-scan.yaml | govulncheck blocking |
| Static-analysis findings | security-scan.yaml | gosec baseline-diff blocking |
| Committed secrets | secret-scan.yaml | gitleaks fail-on-find |
| Dependency CVEs at PR | dependency-review.yaml | fail-on-high-severity |
| License compliance | dependency-review.yaml | deny-list |
| Web-surface vulns | owasp-zap.yaml | manual ZAP baseline |

## PRD plan status (operator-asked twice this turn)

Honest assessment:

| Phase | Status |
|------|------|
| Phase 1 — unified Profile dropdown | ✅ shipped v5.26.30 + .34 |
| Phase 2 — story review/edit | ✅ shipped v5.26.32 |
| Phase 3 — per-story execution profile + approval | 🟡 design at `docs/plans/2026-04-27-prd-phase3-per-story-execution.md`. **NOT YET IMPLEMENTED.** Tracked as task #44. |
| Phase 4 — file association | 🟡 design at `docs/plans/2026-04-27-prd-phase4-file-association.md`. **NOT YET IMPLEMENTED.** Tracked as task #45. |
| Phase 5 — persistent smoke fixtures | ✅ shipped v5.26.33 |
| Phase 6 — howtos + screenshots + diagrams | 🟡 howto text v5.26.39, screenshots v5.26.54. Diagram updates pending. |

Phase 3 implementation starts next.

## Configuration parity

No new config knob in this patch.

## Tests

Smoke unaffected (58/0/1). Go test suite unaffected (465 passing).

## Known follow-ups

Active task list shown in operator-visible bottom strip:

- #39 — Wake-up stack L0–L5 smoke probes
- #41 — docs/testing.md ↔ smoke coverage audit
- #44 — **Phase 3 implementation** (next)
- #45 — Phase 4 implementation

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload the PWA — sessions running inside a container worker
# now show "⬡ worker" badge. Three new security workflows take
# effect on the next PR / push to main.
```
