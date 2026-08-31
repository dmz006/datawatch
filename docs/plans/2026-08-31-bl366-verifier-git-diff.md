# BL366 — Autonomous Verifier: Git-Diff Grounding

**Date:** 2026-08-31
**Version at planning:** v8.15.0
**Shipped:** v8.16.0

## Problem

The BL25 independent verifier prompt only receives `task.Spec` (what the task was
supposed to do). It cannot see the actual code change produced by the worker session.
A task that produces no output, or wrong output, passes verification as long as the
spec sounds complete.

## Proposed Fix

1. Capture `git rev-parse HEAD` in the project directory **before** the worker session
   starts. Store the SHA in `SpawnResult.PreTaskSHA`.
2. After the worker session completes, run `git diff <pre_task_sha>..HEAD` in the
   project directory to produce a diff of all changes the worker made.
3. Inject the diff into the verifier prompt (capped at `autonomous.verifier_diff_max_bytes`,
   default 8192). Truncation is noted in the prompt.
4. If `ProjectDir` is empty, not a git repo, or the worker made no git commits
   (SHA unchanged), verification falls back to spec-only mode.

## Scope

| File | Change |
|------|--------|
| `internal/autonomous/executor.go` | Add `PreTaskSHA string` to `SpawnResult`; store on `t.PreTaskSHA` after spawn |
| `internal/autonomous/models.go` | Add `PreTaskSHA string` to `Task` |
| `internal/config/config.go` | Add `VerifierDiffMaxBytes int` to `AutonomousConfig` |
| `cmd/datawatch/main.go` | Capture SHA in `autonomousSpawn`; inject diff in `autonomousVerify` |
| `internal/server/api.go` | Wire `autonomous.verifier_diff_max_bytes` in GET/PUT config |
| `docs/config-reference.yaml` | New field |
| `docs/implementation.md` | New entry |
| `CHANGELOG.md` | [Unreleased] entry |
| `docs/plans/README.md` | BL366 → active → closed; BL368 closed; current state → v8.15.0 |
| `README.md` | Update current release line |
| `internal/autonomous/executor_test.go` | Unit tests for PreTaskSHA threading |

## Security note

Diff content comes from the local git repository — not user input — so it does not need
`<user_data>` wrapping. Only `task.Spec` (user-supplied planning content) keeps its
data-boundary tag per BL369.

## Phases

### Phase 1 — Core implementation ✅ SHIPPED v8.16.0

- `SpawnResult.PreTaskSHA` + `Task.PreTaskSHA` added
- SHA capture in `autonomousSpawn` (graceful fallback when no git repo or cluster dispatch)
- Diff injection in `autonomousVerify` with configurable truncation
- `autonomous.verifier_diff_max_bytes` config field (all 6 surfaces)
- Unit tests

### Live validation pending (next major release gate)

The following are Validated=No in `docs/testing-tracker.md` and must be confirmed at the v9.0.0 release checkpoint per AGENT.md §Release testing:

| Test | How to run |
|------|-----------|
| Verifier receives real git diff in prompt | Submit a PRD against a local git project; inspect the verifier's LLM input via daemon logs or a debug session — confirm `<diff>` block is present and references actual changed files |
| `autonomous.verifier_diff_max_bytes=512` truncation | Set the field low, submit a PRD with a large task; confirm the diff in the verifier prompt is capped and annotated "(truncated to 512 bytes)" |
| Cluster-dispatch graceful no-diff | With a cluster worker, run a task; verify `Task.PreTaskSHA == ""` and verifier falls back to spec-only without error |
| Config round-trip via comm channel | `configure autonomous.verifier_diff_max_bytes=4096` via Signal/Telegram; `GET /api/config` and confirm new value |

### Phase 2 — BL366 verifier synergy with BL368 vision (deferred)

If a PRD has `quality_gates.screenshot_path_glob`, collect matching screenshots after
each task and pass them to the vision service for description before sending to the
verifier. Deferred — requires vision + verifier integration surface not yet designed.
