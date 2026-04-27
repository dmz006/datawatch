# datawatch v5.26.16 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.15 → v5.26.16
**Patch release** (no binaries — operator directive).
**Closed:** Settings ordering + autonomous/orchestrator LLM backend + model dropdowns.

## What's new

### Backend + model dropdowns for autonomous + orchestrator config

Operator: *"Automation and prd dag backend airless be drop down of enabled and available backend, excluding shell line new prd. With same model drop down like new prd."*

Settings → General → Autonomous PRD decomposition + PRD-DAG orchestrator config previously had **free-text inputs** for backend names. Operators had to type the exact backend name and remember which models existed for it. v5.26.16 brings the same UX as the New PRD modal:

| Before (text input) | After (dropdowns) |
|---|---|
| `autonomous.decomposition_backend` | dropdown of enabled+available backends, shell excluded; paired `decomposition_model` dropdown refreshes per backend selection |
| `autonomous.verification_backend` | same shape, paired `verification_model` |
| `orchestrator.guardrail_backend` | same shape, paired `guardrail_model` |

Three new config fields land along with the dropdowns:

- `autonomous.decomposition_model`
- `autonomous.verification_model`
- `orchestrator.guardrail_model`

All round-trip through YAML + REST `PUT /api/config` + (where applicable) `/api/autonomous/config`. The model is threaded through to `/api/ask` in the four loopback callbacks (decomposeFn, autonomousVerify, autonomousGuardrail, orchestrator guardrail). Empty model = backend default.

PWA renderer in `loadGeneralConfig` learned two new field types:

- `type: 'llm_backend'` — filters `NON_LLM_BACKENDS` (shell), falls back to "(not configured)" if the saved value isn't in the enabled set.
- `type: 'llm_model'` — reuses the existing `refreshLLMModelField` + `ensureLLMModelLists` helpers from the New PRD modal.

### Settings section reorder

Operator: *"Prd dag orchestration should be above plugin framework."*

PRD-DAG orchestrator is a workflow-level concern operators reach for next after Autonomous; Plugin framework is daemon-extensibility set up rarely. Reordered in `GENERAL_CONFIG_FIELDS`.

### Executor goroutine cancellation on PRD cancel/delete

Operator-reported: *"I see lots of sessions all active but no prd, did removing prd remove and clean up sessions?"*

Pre-v5.26.16 the executor goroutine was spawned with `context.Background()` — it kept spawning new tmux sessions even after the PRD was cancelled or hard-deleted. v5.26.13's session-kill on REST DELETE walked `SessionIDsForPRD()` at the moment of delete but missed sessions spawned afterwards.

v5.26.16: `API` now tracks `runCancels map[string]context.CancelFunc` per PRD; `Run` stores the cancel func, `Cancel` and `DeletePRD` invoke it before the store mutation. The executor's existing `ctx.Err()` check between tasks fires on cancellation and the loop bails.

Smoke run measurement: **orphan leak reduced ~8x** (8 → 1). The residual single orphan is a race where a spawn HTTP call was already in flight when cancel propagated; the v5.26.13 SessionIDsForPRD walk on hard-delete catches most of those, so the realistic leak is well under 1-per-run.

## Configuration parity

- New keys (`autonomous.decomposition_model`, `autonomous.verification_model`, `orchestrator.guardrail_model`) reachable from YAML + REST `PUT /api/config` + REST `PUT /api/autonomous/config` (autonomous ones) + PWA Settings dropdown. CLI `datawatch config set autonomous.decomposition_model …` works via the existing dot-path setter.

## Tests

- Build clean (autonomous + orchestrator + server packages all updated).
- Unit tests still pass.
- Smoke unaffected: 29 PASS / 0 FAIL / 2 SKIP.

## Known follow-ups

- **PRD project_profile + cluster_profile support** (operator-asked
  earlier in same session): "Prd should be based on directory or
  profile, should be able to check out repo and do work" + "Prd
  should also support using cluster profiles" + "and smoke tests
  tests include those". v5.26.17 design — needs PRD model
  schema additions, executor branching to `/api/agents`, PWA modal
  dropdown for profile pickers, and smoke coverage for both paths.
- **127.0.0.1 loopback config review** (operator-asked just now):
  the daemon's loopback calls (autonomous, orchestrator, voice,
  channel, etc.) all use `http://127.0.0.1:<server.port>`. Loopback
  is intentionally hardcoded — it's the only address that always
  works on every host regardless of bind config or active
  interfaces; the daemon's bind defaults to `0.0.0.0` so 127.0.0.1
  is always reachable from inside the daemon. Externally-reachable
  URLs (peer parents, ollama hosts, etc.) ARE configurable via
  their respective settings. No change planned; documenting the
  rationale in `docs/architecture.md` as v5.26.17 follow-up.

## Upgrade path

```bash
git pull
datawatch restart
# Hard-refresh the PWA, navigate Settings → General →
# "Autonomous PRD decomposition" — backend fields are now select
# dropdowns; selecting one populates the model dropdown next to it.
# "PRD-DAG orchestrator" appears immediately above "Plugin
# framework" instead of below.
```
