# datawatch v5.26.18 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.17 → v5.26.18
**Patch release** (no binaries — operator directive).
**Closed:** Loopback hardcode sweep finished + smoke kills race-condition orphan sessions.

## What's new

### Smoke kills race-condition orphan sessions

Operator: *"Another orphaned prd test session."*

v5.26.16 added executor goroutine cancellation on PRD cancel/delete (8 → 1 orphan per smoke run). v5.26.18 closes the residual leak via a baseline-diff sweep in the smoke's `cleanup_all` trap:

1. Before §6/§7/§7b runs, capture a snapshot of `autonomous:*` running session IDs (the baseline).
2. In `cleanup_all` on EXIT, list `autonomous:*` running sessions again and compare to baseline.
3. Kill any session that wasn't in the baseline (i.e. spawned during smoke).

Real operator-initiated autonomous runs that pre-existed when smoke started are NOT touched — they're in the baseline. Only smoke-spawned orphans get killed.

Per-run orphan count: 8 (pre-v5.26.16) → 1 (v5.26.16) → **0** (v5.26.18).

### Remaining loopback hardcode sweep

v5.26.17 replaced 6 highest-priority hardcoded `http://127.0.0.1:port` sites in autonomous + orchestrator callbacks. v5.26.18 finishes the sweep — 8 more sites in `cmd/datawatch/main.go` switched to `loopbackBaseURL(cfg)`:

- `channel.js` subprocess `DATAWATCH_API_URL` env (2 places)
- `/api/test/message` invoke
- `/api/config` PUT/GET in CLI helpers (4 places)
- `/api/stats` + `/api/alerts` in CLI commands (2 places)

The only remaining `127.0.0.1` literal is the return value inside `loopbackBaseURL` itself, which is correct (resolves to 127.0.0.1 when bind is `0.0.0.0` / `""`).

Combined with v5.26.17, **every** daemon-internal loopback HTTP call now respects `cfg.Server.Host`. Operators with non-default binds (`192.168.1.5`, `::`, etc.) get autonomous + orchestrator + voice + channel + test-message + stats/alerts/config CLI working out of the box.

## Configuration parity

No new config knob.

## Tests

- 1404 Go unit tests passing (no new tests this release; smoke covers the loopback paths via the autonomous flow).
- Functional smoke: 29 PASS / 0 FAIL / 2 SKIP, **0 orphan sessions left after run**.

## Known follow-ups

- **PRD project_profile + cluster_profile support** still queued from earlier in this session — needs schema additions, executor branching to `/api/agents`, PWA modal dropdowns, smoke coverage. v5.26.19 design.
- v6.0 packaging items unchanged.

## Upgrade path

```bash
git pull
datawatch restart
# Run a smoke pass: ./scripts/release-smoke.sh
# Cleanup section now ends with 0 orphans (or only kills race-survivors,
# explicitly logged as "killed orphan-autonomous-session ... (race-survivor)").
```
