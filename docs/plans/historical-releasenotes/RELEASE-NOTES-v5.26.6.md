# datawatch v5.26.6 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.5 → v5.26.6
**Patch release** (no binaries — operator directive: every release until v6.0 is a patch).
**Closed:** Autonomous tab final polish (cache bump + WS-aware Refresh button + verdict drill-down + child-PRD navigation) + BL173 live-cluster validation + Helm chart `v`-prefix tag fix.

## What's new

### PWA Autonomous tab — operator-reported follow-ups

Operator: *"I see refresh button on autonomous tab and if u edit or delete something it is all there until i refresh, i also can't edit not sure buttons work."*

Three issues bundled here. The buttons-don't-work + don't-auto-refresh complaints both root-caused to **stale cached app.js** in installed PWAs. The v5.26.3 escHtml fix and the v5.24.0 WS auto-refresh handler are both in current source, but PWAs that hit a transient offline window during the v5.7→v5.26 stretch ended up with the pre-fix `datawatch-v5-6-1` cache locked in.

**Fixes:**

- **SW cache name bumped** `datawatch-v5-6-1` → `datawatch-v5-26-6` (`internal/server/web/sw.js`). Same pattern as the BL187/v5.0.4 cache invalidation. Forces every installed PWA to drop the v5-6-1 cache on next `activate` and re-fetch app.js / index.html / style.css. Brings v5.26.3+ button-revival fix and v5.24.0 WS auto-refresh handler into clients that were stuck.
- **Refresh button hidden when WS connected** — the manual `↻ Refresh` on the Autonomous toolbar is replaced with a small green `● auto` badge when `state.connected` is true, signaling the panel is live-updating via the `prd_update` WS broadcast. When WS goes down (status dot turns red), the Refresh button reappears as a fallback. Updated dynamically via `updateStatusDot`.

### BL202 polish — verdict drill-down + child-PRD navigation

Operator request from the original audit; deferred as iterative cosmetic. Now closed:

- **Verdict drill-down panel** (`renderVerdicts`). Verdict badges (`pass` / `warn` / `block`) are now click-to-expand: clicking opens an inline panel below the row showing the guardrail name, outcome, severity, summary, and the full issues list. Click again to collapse. Tooltip kept for desktop hover; click pattern is the touch-device path (mobile companion / Wear OS). State stored in `state._verdictPayloads` so the same panel slot can flip between badges.
- **Child-PRD navigation** (`loadPRDChildren`). The Children disclosure on each parent PRD now renders rows with:
  - Clickable child ID — clicking scrolls the parent panel to that child PRD's own row, with a brief 2px accent-color outline highlight.
  - Stories+tasks count inline (`5s/12t` shorthand).
  - Verdict-count badge — green muted "12 verdicts" when none blocked, red "3 block" when any verdict blocks.
  - `scrollToPRD(id)` helper falls back to a toast when the child isn't currently rendered (e.g. a status filter is active).

### BL173 cluster→parent push — validated on real cluster

Full report: [`docs/plans/2026-04-27-bl173-cluster-validation.md`](2026-04-27-bl173-cluster-validation.md).

Deployed the chart at `charts/datawatch` to the local kubectl-wired testing cluster (TKG / vSphere, Antrea overlay, 3 nodes), pre-created the API-token Secret, ran `helm install dw … --set image.registry=ghcr.io/dmz006/datawatch --set image.tag=5.26.5 --set apiTokenExistingSecret=datawatch-api-token`. Pod rolled out in ~14 s. End-to-end smoke covering peer registration → push → read-back → cross-host aggregator all passed:

- `POST /api/observer/peers` minted bcrypt token.
- `POST /api/observer/peers/thor/stats` (Bearer auth) accepted snapshot.
- `GET /api/observer/peers/thor/stats` returned the just-pushed StatsResponse.
- `GET /api/observer/envelopes/all-peers` showed both `local` + `thor` envelope groups — exactly the topology BL180 Phase 2 cross-host federation correlation depends on.

Validates that the cluster→parent push code path works in a real cluster. The dev-workstation reachability gap from the original audit note is unchanged (Antrea pod overlay + workstation iptables means pods can't dial back into the workstation), but the inverse topology — parent in cluster, peer pushing in — exercises the same code and is the production-cluster pattern. Recipe is now: `helm install` → register → push → read.

### Helm chart — `v`-prefix tag fix + appVersion bump

Two operator-side gotchas surfaced during the cluster validation:

1. **GHCR tag form mismatch.** CI publishes tags WITHOUT the `v` prefix (`echo "version=${GITHUB_REF#refs/tags/v}"` in `.github/workflows/containers.yaml`), so `v5.26.5` becomes `5.26.5` on GHCR. Operators pasting a release tag with `v` got `ImagePullBackOff`.
   - `charts/datawatch/templates/_helpers.tpl` now `trimPrefix "v"` from `image.tag` / `appVersion` so either form works.
2. **`appVersion` was 21 releases stale** (`v4.7.1`). Bumped to `5.26.5` so `helm install` without `--set image.tag=…` pulls the current version. Chart `version` bumped 0.21.1 → 0.22.0.

`helm template ./charts/datawatch --set image.tag=v5.26.5` now emits `image: ghcr.io/dmz006/datawatch/agent-base:5.26.5` (correct).

## Configuration parity

No new config knob.

## Tests

1395 still passing. Cluster validation is documented as a runbook (no automated test — would require a kind cluster spin-up in CI which is v6.0 scope).

## Known follow-ups

All operator-driven items closed. Remaining for v6.0 cut as previously documented:

- v6.0 cumulative release notes
- CI: add `parent-full` + `agent-goose` to `containers.yaml`
- CI: pre-release security scan automation
- CI: kind-cluster-based BL173-style helm-install smoke test (now that the recipe is documented)
- GHCR past-minor cleanup run (operator-side action)

## Upgrade path

```bash
git pull          # patch series — no binary update path
# Operators with installed PWAs: a hard refresh (Ctrl+Shift+R) or
# closing+reopening the PWA picks up the new SW cache name and
# brings the v5.26.3 button-revival + v5.24.0 auto-refresh fixes
# into clients that were stuck on the old cache.
```
