**Date:** 2026-11-02
**Version:** next minor release above main (taken at merge time)

# Supervisor loop

## What it is

The supervisor loop is a daemon-side job that does the routine supervisor duties without a live model session holding the context. Mechanical steps (closing finished records, deploying after a ship, retrying infrastructure failures, sweeping leftovers, applying operator answers, filling lanes, starting the next step of a chain and running a merge until it conflicts) run as plain daemon code. Three narrow jobs need judgment (drafting a spec, classifying a CI failure, picking a backend). Each runs on a selectable model and style behind a validator, and escalates to the operator after repeated failure. Every run is recorded so the operator can see how the loop is performing and what needs tuning.

## Design record

Operator answer of 2026-11-01 (source: `docs/plans/2026-10-29-pre-10-0-design-proposals-index.md`, section "Answers given in the question queue"):

> The model and style,  long running or not,  should be selectable,  and results and documentation observable to see how it is performing and needs tuning

Consequences: each model job has its own `style` (`oneshot` or a long-running session), `backend` and `model`; every step and job writes an observable run record; and an operator how-to documents the loop and its tuning.

## Scope

- `internal/supervisorloop`: the loop, the step runners, the model jobs, the validators and the run store.
- `internal/autonomous`: the hooks the steps read and act on (records, lanes, merge, chains).
- `internal/server`: REST endpoints for status, runs and config.
- `internal/mcp`: matching MCP tools.
- `cmd/datawatch`: the CLI subcommands and daemon wiring.
- `internal/router`: comm channel commands.
- `internal/config`: the `supervisor_loop` config block and its validation.
- `internal/server/web`: the PWA status and settings card.

## Phases

1. Phase 1: loop core, config block, run store and retention.
2. Phase 2: mechanical steps (close_finished_records, deploy_after_ship, retry_failed_infra, cleanup_sweep, apply_answers).
3. Phase 3: lane and chain steps (lane_count within the operator's bounds, start_next_chain_step, run_merge_until_conflict).
4. Phase 4: model jobs (draft_spec, classify_ci_failure, pick_backend) with selectable style, backend and model, validators, retries and escalation.
5. Phase 5: observability, covering REST, MCP, CLI and comm channel status and run history, plus the stats snapshot, the Monitor card and the Prometheus gauges.
6. Phase 6: PWA card, the operator how-to, and the filed parity issue for Android and iPhone/iOS.

## Status

| Phase | Status |
|---|---|
| Phase 1 | Done (implemented on branch `automaton/dfea0001`; not yet released, the version is taken at merge time) |
| Phase 2 | Done (implemented on branch `automaton/dfea0001`; not yet released, the version is taken at merge time) |
| Phase 3 | Done (implemented on branch `automaton/dfea0001`; not yet released, the version is taken at merge time) |
| Phase 4 | Done (implemented on branch `automaton/dfea0001`; not yet released, the version is taken at merge time) |
| Phase 5 | Done (implemented on branch `automaton/dfea0001`; not yet released, the version is taken at merge time) |
| Phase 6 | Done (implemented on branch `automaton/dfea0001`; not yet released, the version is taken at merge time) |

## Decision notes

| Setting | Default | Note |
|---|---|---|
| `supervisor_loop.enabled` | true | Master switch. |
| `supervisor_loop.close_finished_records.enabled` | true | Mechanical step. |
| `supervisor_loop.deploy_after_ship.enabled` | true | Mechanical step. |
| `supervisor_loop.retry_failed_infra.enabled` | true | Mechanical step. |
| `supervisor_loop.cleanup_sweep.enabled` | true | Mechanical step. |
| `supervisor_loop.apply_answers.enabled` | true | Mechanical step. |
| `supervisor_loop.lane_count.enabled` | true | May move only inside the operator's configured lane bounds. |
| `supervisor_loop.start_next_chain_step.enabled` | true | Mechanical step. |
| `supervisor_loop.run_merge_until_conflict.enabled` | true | Mechanical step. |
| `supervisor_loop.draft_spec.style` / `.backend` / `.model` | `oneshot` / empty / empty | Empty means use the decision assistant's configured model. If none is configured the job escalates and never silently falls back to Claude. |
| `supervisor_loop.classify_ci_failure.style` / `.backend` / `.model` | `oneshot` / empty / empty | Same empty-value rule. |
| `supervisor_loop.pick_backend.style` / `.backend` / `.model` | `oneshot` / empty / empty | Same empty-value rule. |
| `supervisor_loop.validator_retries` | 2 | Retries after a validator rejects a model job's output. |
| `supervisor_loop.escalate_after_failures` | 2 | Consecutive failures before the operator is asked. |
| `supervisor_loop.run_retention` | 500 | Run records kept. |

## Parity surface

| Surface | Included | Intent |
|---|---|---|
| REST | Included | Status, run history and config read and write endpoints. |
| MCP | Included | Tools matching each REST endpoint. |
| CLI | Included | Subcommands for status, runs, config and a manual tick. |
| Comm channel | Included | Status and run summary commands; escalations arrive as questions. |
| YAML/config | Included | The `supervisor_loop` block with every default above, documented in `docs/implementation.md`. |
| PWA | Included | A status and settings card with model and style selectors and run history. |
| Android | Included | Same card and settings; see `docs/plans/parity-issues/supervisor-loop.md`, a draft filed at merge. |
| iPhone/iOS | Included | Same card and settings; see `docs/plans/parity-issues/supervisor-loop.md`, a draft filed at merge. |
