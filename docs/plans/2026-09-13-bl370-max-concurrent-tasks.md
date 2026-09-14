# Plan: Per-PRD max_concurrent_tasks — Semaphore-Bounded Parallel Task Executor (v8.26.0)

**Status:** Completed 2026-09-13
**Release:** v8.26.0

## Problem

The autonomous executor ran tasks strictly sequentially — one task at a time per PRD, regardless
of how many independent tasks (no unmet dependencies) were ready. For PRDs with large numbers
of parallel stories or independent tasks this is inefficient: the GPU and CPU sit idle while a
single task session is in flight.

## Approach

Added a `max_concurrent_tasks` field at both the global config level
(`autonomous.max_concurrent_tasks`) and the per-PRD level (`prd.max_concurrent_tasks`).
Resolution: per-PRD overrides global; 0 on either means "use the next level"; effective ≤ 1
means sequential (existing behaviour, unchanged path).

When concurrency > 1, the `Run` function in `executor.go` switches to a goroutine pool:

- A `taskResult` channel collects completions from worker goroutines.
- `isReady(tid)` checks the `completedIDs` map for each task's `DependsOn` list before
  launching. Dependency ordering is preserved without any changes to the topo-sort.
- `launch()` iterates the topo-sorted order, skipping in-flight and already-completed tasks,
  starting goroutines until `concurrency` slots are full.
- The main coordinator goroutine `select`s on `results` and `ctx.Done`. On each completion
  it updates `completedIDs`, handles blocked/failed PRD rollup, and calls `launch()` again.
- Each goroutine captures `capturedPRD := prd` at launch time; no shared writes between
  concurrent goroutines.
- Error handling mirrors the sequential path: story rollup, blocked check, context cancellation
  drain all go through the same helpers.

The sequential path (concurrency ≤ 1) is entirely unchanged — the same loop body, same helpers.

A new `set_concurrency` API action (`POST /api/autonomous/prds/{id}/set_concurrency` with
`{"max_concurrent_tasks": N}`) allows runtime changes without restarting the daemon. The PWA
PRD Settings modal exposes a number field (0 = global default, 2+ = fan out).

## Files Changed

- `internal/autonomous/models.go` — added `MaxConcurrentTasks int` to `PRD` struct
- `internal/autonomous/manager.go` — added `MaxConcurrentTasks int` to `Config`; added
  `SetPRDConcurrency` manager method
- `internal/autonomous/executor.go` — concurrent execution path added to `Run`; sequential
  path untouched
- `internal/autonomous/api.go` — added `SetPRDConcurrency` API method + `EmitPRDUpdate` call
- `internal/server/api.go` — added `SetPRDConcurrency` to `AutonomousAPI` interface
- `internal/server/autonomous.go` — added `case "set_concurrency"` handler
- `internal/server/web/app.js` — PRD Settings modal: concurrency field, save logic, overview
  meta display (`Concurrency: N tasks` chip when > 1)
- `internal/server/orchestrator_enrich_test.go` — added `SetPRDConcurrency` stub to
  `fakeOrchAutonomous` (all four test mock types embed this)

## Testing

- 134 autonomous package tests pass (pre-existing suite covers executor, manager, store).
- 2517 total tests pass across all packages after mock fix.
- Sequential path verified: default config (MaxConcurrentTasks = 0) unchanged behaviour.
