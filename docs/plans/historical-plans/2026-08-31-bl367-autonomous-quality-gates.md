# BL367 — Autonomous PRD Quality Gates

**Status:** Implemented (v8.17.0)
**Date:** 2026-08-31

## Problem

The autonomous executor verified tasks with an LLM verifier (BL25) and git-diff grounding (BL366), but had no mechanism to check whether a task's changes introduced test regressions. Operators could only detect breakage after the full PRD run, not per-task.

## Solution

Mirror the BL28 pipeline quality gate pattern inside the autonomous executor:
1. Capture a test baseline (`pipeline.RunTests`) before the first task starts.
2. After each task's verifier passes, re-run the test suite and compare with `pipeline.CompareResults`.
3. If `block_on_regression: true` and tests regressed, fail the task and enter the auto-fix retry cycle with the regression summary injected as context.
4. `Task.quality_gate_result` carries pass/fail counts and the regression flag for every completed task, regardless of blocking.

## Config

| Key | Type | Default | Description |
|---|---|---|---|
| `autonomous.default_quality_gates.enabled` | bool | `false` | Master toggle |
| `autonomous.default_quality_gates.test_command` | string | `""` | Shell command run from `project_dir` |
| `autonomous.default_quality_gates.timeout` | int | `0` | Seconds per run (0 = no cap) |
| `autonomous.default_quality_gates.block_on_regression` | bool | `false` | Fail task on regression |

Per-PRD override: send `quality_gates` object in `POST /api/autonomous/prds` body.

## Implementation

- `internal/autonomous/models.go` — `PRD.QualityGates *pipeline.QualityGateConfig`, `Task.QualityGateResult *pipeline.QualityGateResult`
- `internal/autonomous/manager.go` — `resolveQualityGates`, `SetPRDQualityGates`, `Config.DefaultQualityGates`
- `internal/autonomous/executor.go` — baseline capture in `Run()`, gate check in `executeOne()`
- `internal/config/config.go` — `AutonomousConfig.DefaultQualityGates` using existing `QualityGateConfig` type
- `cmd/datawatch/main.go` — bridge `config.QualityGateConfig` → `pipeline.QualityGateConfig` in `amgrCfg`
- `internal/server/api.go` — `SetPRDQualityGates` on `AutonomousAPI` interface; `autonomous.default_quality_gates.*` PUT cases
- `internal/server/autonomous.go` — `quality_gates` in `POST /api/autonomous/prds` request body
- `internal/server/web/app.js` — four settings fields in Automate card

## Reuse

Uses existing `pipeline.QualityGateConfig`, `pipeline.RunTests`, `pipeline.CompareResults` from BL28 unchanged. Import-cycle workaround: `config.QualityGateConfig` (same fields, already existed) used in `config` package; bridged to `pipelinePkg.QualityGateConfig` in `main.go`.

## Tests

`internal/autonomous/bl367_quality_gates_test.go` — 5 unit tests:
- `SetPRDQualityGates_Persisted` — store round-trip
- `SetPRDQualityGates_NotFound` — unknown PRD error
- `ResolveQualityGates_PerPRDOverridesDefault` — per-PRD precedence
- `ResolveQualityGates_FallsBackToDefault` — default fallback
- `QualityGateResult_StoredOnTask` — end-to-end with real `go build` gate

## Live validation pending (v9.0.0 gate)

| Item | Status |
|---|---|
| Run a PRD with `test_command: "go test ./..."` on a real project | Not yet |
| Verify `Task.quality_gate_result` in GET /api/autonomous/prds/{id} | Not yet |
| Confirm regression blocking fails the task and auto-fix retries with hint | Not yet |
| Confirm `block_on_regression: false` records result but continues | Not yet |
