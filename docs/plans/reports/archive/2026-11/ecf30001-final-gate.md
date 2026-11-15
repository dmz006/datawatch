# Compute placement (ecf30001) — final gate

**Date:** 2026-11-08
**Scope:** final gate for the placement feature; read-only, one report file, no daemon started, no gh, no version variable changed.

## Checks

| # | Check | Result |
|---|---|---|
| 1 | Every acceptance point of the feature has a test | pass |
| 2 | The placement settings round trip through PUT and GET of the config API | pass |
| 3 | The placement settings round trip through the CLI | pass |
| 4 | The placement settings round trip through MCP | pass |
| 5 | The placement settings round trip through the comm channel | pass |
| 6 | The PWA help link and the definitions entries exist | pass |
| 7 | The how-to exists | pass |
| 8 | The datawatch-app parity note is in the merge plan | pass |
| 9 | The full go test suite passes | pass |

**Count: 9 pass, 0 fail.**

## Evidence (counts only)

1. Placement tests: planner 15, ask-store 4, model pull/load 12, config policy 5, admission smoke 11 subtests (report in this directory), CLI placements 12, comm verbs 4, MCP tools 7, PWA card 9, plus the pin and routing tests of the existing model routing.
2. Config API round trip: 6 tests (defaults, per-key round trip, combined patch, save-and-load, invalid values, whole-patch rejection).
3. CLI: 12 tests covering the settings read, full and partial writes, escaping and guard rails.
4. MCP: 7 tests covering the two placement tools, the settings key forwarded by the generic config tool, and the unconfigured loopback.
5. Comm channel: 4 tests covering list, approve, deny and the settings key forwarded by the configure verb.
6. Five help-link map entries point at the how-to; four definitions entries exist; all five locale bundles carry the card strings.
7. The how-to exists with the five-access-method table.
8. The PRD's merge-time plan (spec and the operator-surfaces story description) both carry the note that the datawatch-app parity issue is filed at merge time and the version is set at merge time.
9. 87 packages ok, 12 packages with no test files. One 30-second timeout flake in an unrelated daemon-cleanup test under full-suite load passed on immediate rerun in isolation and at package scope.
