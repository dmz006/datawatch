# Release Notes — v6.10.1 (BL259 Phase 2 — Algorithm → Evals bridge)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.10.1
Smoke: 98/0/7

## Summary

BL259 Phase 2 — closes BL259 by wiring the Algorithm Mode Measure-phase boundary to the Evals framework shipped in v6.10.0.

## Added

- **REST** — `POST /api/algorithm/{id}/measure?suite=<name>` — runs eval suite, summarizes pass/fail, advances the phase with `"evals[suite/mode]: PASS/FAIL — pass_rate=NN% (threshold=NN%)"` as the captured Measure output. Returns both the eval Run and new algorithm state.
- **MCP** — `algorithm_measure` tool.
- **CLI** — `datawatch algorithm measure <session-id> --suite <name>`.
- **Comm** — `algorithm measure <session-id> <suite>`.

## Fixed

- Smoke step 15 (BL259 P1) variable name collision — local `PASS=$(...)` overwrote the global PASS counter, triggering `set -u` failure on later steps. Renamed to `EV_PASS`.

## What didn't change

- The legacy BL221 scan framework (rules check + security scan) is unchanged. P2's intent was the eval-bridge wiring; a destructive removal of the binary verifier shim was scoped out to avoid regressions in scan paths.
- No new go-mod dependencies.

## Sequence reminder

- BL257 ✅ closed (v6.8.0 + v6.8.1)
- BL258 ✅ closed (v6.9.0)
- BL259 ✅ closed (v6.10.0 P1 + v6.10.1 P2 — this release)
- Next: BL260 — Council Mode (v6.11.0)

## See also

- CHANGELOG.md `[6.10.1]`
- `docs/plan-attribution.md` (BL259 row updated to ✅ v6.10.0–v6.10.1 closed)
