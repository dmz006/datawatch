# Release Notes — v6.10.0 (BL259 Phase 1 — Evals Framework)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.10.0
Smoke: 102/0/6

## Summary

BL259 Phase 1 — Evals Framework with full 7-surface parity. Replaces the legacy binary verifier (yes/no on session completion) with rubric-based grading across 4 grader types: `string_match`, `regex_match`, `llm_rubric` (stubbed), `binary_test`.

PAI source: `docs/plans/2026-05-02-pai-comparison-analysis.md` §3 + Recommendation M1.

Phase 2 (v6.10.1) will migrate BL221's scan framework (rules check + security scan) to use Evals graders, and remove the old verifier shim.

## Added

### `internal/evals` package

- Types: `Grader`, `Case`, `Suite`, `Run`, `CaseResult`.
- `Mode`: `capability` (default threshold 0.7) or `regression` (default threshold 0.99).
- `GraderType`: `string_match` (substring or strict exact), `regex_match`, `binary_test` (sh exit code), `llm_rubric` (stubbed in v6.10.0).
- `Runner`: loads suites from `~/.datawatch/evals/<name>.yaml`, executes, persists `Run` JSON to `~/.datawatch/evals/runs/<id>.json`.
- 15 unit tests pass.

### REST surface (4 endpoints)

- `GET /api/evals/suites` — list defined suites with mode + threshold + case count.
- `POST /api/evals/run?suite=<name>` — execute, return Run.
- `GET /api/evals/runs[?suite=&limit=N]` — list runs (most recent first).
- `GET /api/evals/runs/{id}` — fetch one run.
- Audit-logged on run (`action=evals_run`).

### MCP tools (4)

- `eval_list_suites`, `eval_run`, `eval_list_runs`, `eval_get_run`.

### CLI

- `datawatch evals list`
- `datawatch evals run <suite>`
- `datawatch evals runs [--suite <s>] [--limit N]`
- `datawatch evals get-run <id>`

### Comm verb

- `evals` (list)
- `evals run <suite>`
- `evals runs [<suite>]`
- `evals get-run <id>`

### PWA

- Settings → Agents → Evals card.
- Suite list with mode badge + threshold + case count + Run button per suite.
- Recent runs (last 10) with PASS/FAIL badge + pass-rate% + click-to-detail.

### Locale

- 12 new keys × 5 bundles (`evals_*`).

### Smoke

- New step "15. v6.10.0 BL259 P1 — Evals framework: list suites + grader smoke" — drops a 2-case suite into `~/.datawatch/evals/`, runs it via REST, asserts `pass:true`, cleans up.

## Backward compatibility

- Existing binary verifier untouched. Phase 2 (v6.10.1) will swap BL221 scan handlers to use Evals graders.
- No new go-mod dependencies.
- New on-disk dirs (`~/.datawatch/evals/` + `~/.datawatch/evals/runs/`) created on demand.

## Sequence reminder

- BL257 ✅ closed (v6.8.0 + v6.8.1).
- BL258 ✅ closed (v6.9.0).
- BL259 P1 ✅ this release.
- Next: BL259 P2 — migrate BL221 scan to evals (v6.10.1).
- Then: BL260 — Council Mode (v6.11.0).

## Mobile parity

[`datawatch-app#55`](https://github.com/dmz006/datawatch-app/issues/55) updated with shipped scope.

## See also

- CHANGELOG.md `[6.10.0]`
- `docs/plan-attribution.md` (BL259 row updated to ✅ v6.10.0 P1)
