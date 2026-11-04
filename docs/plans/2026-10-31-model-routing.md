# Model routing: which node serves which model, and the node pin

**Date:** 2026-10-31
**Version:** 9.51.0 (the `var Version` in `internal/server/api.go` when this plan was written; the shipping version is the next minor above main and is set at merge time)

**Terminology.** Operator-facing text says **Automaton / Automata**, following the Terminology
Rule in `AGENT.md`. The internal Go type is `PRD` (`internal/autonomous`); this document uses
"PRD" only for code identifiers, API paths and tool names.

## Problem

An Automaton worker could be placed on a compute node that does not serve the model the task
needs, and the task then failed far from the cause. Nothing showed which models a node served,
nothing let an operator pin work to a node, and the job that verifies a task could land on a
different node from its worker.

## Design

- **Served models.** `internal/compute/served_models.go` reads each registered node's model list
  (Ollama and OpenAI-style endpoints) and caches it for `served_models_cache_seconds`. Every
  reading carries its age; an unreachable node is reported, not hidden.
- **Pin-time check.** `internal/compute/model_check.go` answers whether any registered node
  serves a model. If none does, the pin is refused with a message naming the model and the nodes
  checked, or, when `refusal_mode` is `warn`, applied with a warning. A node whose models cannot
  be read counts by `unknown_node_policy`. With `alert_on_refusal` a refusal also raises an alert.
- **Node pin.** A task and a story can each be pinned to one compute node
  (`internal/autonomous/node_pin.go`). A task's pin wins over its story's, an empty node clears
  it, a node that is not registered is refused in both modes, and placement then considers only
  the pinned node.
- **Verifier.** With `verifier_follows_worker_node` the verification job is placed on the node
  its worker ran on (`internal/autonomous/verifier_node.go`) and waits for that node rather than
  moving to another.

### Decisions

1. **Refuse is the default.** A model no node serves is refused at pin time; `warn` is the opt-in
   softer mode.
2. **Unknown nodes count as serving by default.** A node whose models cannot be read is assumed
   to serve the model (`assume_serves`), so a network blip does not block work; `assume_not` is
   the strict choice.
3. **Six keys, no more.** `autonomous.model_routing` holds exactly `refusal_mode` (`refuse`),
   `served_models_cache_seconds` (300), `verifier_follows_worker_node` (`true`),
   `unknown_node_policy` (`assume_serves`), `probe_timeout_seconds` (8) and `alert_on_refusal`
   (`true`).
4. **The verifier waits for the worker's node rather than moving.** Moving would verify on a node
   that may not serve the verifier's model or hold the worker's files; the setting turns the
   behaviour off.

## Parity surface

| Surface | Included? | Intent |
|---|---|---|
| REST | Yes | `GET /api/compute/served-models` (`node`, `model`, `backend`, `refresh`); `POST /api/autonomous/prds/{id}/set_task_node` and `set_story_node`; `GET` and `PUT /api/config` carry the six `autonomous.model_routing.*` keys, a bad value refusing the whole put; `pinned_node` on tasks and stories. |
| MCP | Yes | The read-only `compute_served_models` tool and the write tools `autonomous_prd_set_task_node` and `autonomous_prd_set_story_node`; the federation capability table lists them. |
| CLI | Yes | `datawatch compute node served-models [name]`; `datawatch autonomous prd-set-task-node` and `prd-set-story-node`; `datawatch config set autonomous.model_routing.<key>`. |
| comm channel | Yes | A served-models verb, `set-task-node` and `set-story-node`, printing the same refusal text as REST; `configure autonomous.model_routing.<key>=<value>`. |
| YAML/config | Yes | The `autonomous.model_routing` block with all six keys and their defaults in `docs/config-reference.yaml`. |
| PWA | Yes | A Served models block with age and Refresh on each compute node, a Pin to node select on the task and story editors, a Node badge on pinned rows, the six settings fields with description and docs link, and locale keys in all five bundles. |
| Android | Excluded in this work | This work does not change the apps. The mobile contract is the [sync] issue recorded in `docs/plans/parity-issues/model-routing.md`, which covers the route, the `pinned_node` field, the settings and the locale keys. |
| iPhone/iOS | Excluded in this work | Same single [sync] issue as Android: `docs/plans/parity-issues/model-routing.md`. The apps pick the change up from that issue, not from this branch. |

## Scope

- `internal/config`: the `autonomous.model_routing` block, defaults, validation, round trip.
- `internal/compute`: served-models reading and cache, the model check, node pin fields.
- `internal/autonomous`: pin-time refusal, node pin, verifier node, task and story fields.
- `internal/inference`: the dispatcher honours a pinned node.
- `internal/server`: the read route, the pin actions, the config keys, the federation guards.
- `internal/mcp`, `internal/router`, `cmd/datawatch`: the tools, verbs and commands.
- `internal/server/web`: the block, the select, the badge, the settings fields, five locale bundles.
- `docs`: how-to `docs/howto/model-routing.md`, `docs/operations.md`, `docs/config-reference.yaml`,
  `docs/datawatch-definitions.md`, `docs/testing-tracker.md`, `CHANGELOG.md`, and this plan.

Out of scope: pulling models onto a node automatically, choosing a node for the operator, and any
change to the mobile apps.

## Phases

Status as of 2026-10-31.

1. **Config keys.** Status: **Done**.
2. **Served-models read and cache.** Status: **Done**.
3. **Pin-time check and refusal.** Status: **Done**.
4. **Node pin on tasks and stories.** Status: **Done**.
5. **Verifier follows the worker's node.** Status: **Done**.
6. **REST, MCP, CLI and comm channel.** Status: **Done**.
7. **PWA and locale keys.** Status: **Done**.
8. **Mobile parity record.** Status: **Done** (`docs/plans/parity-issues/model-routing.md`).
9. **Operator documentation.** Status: **Done**.
10. **Tracker, changelog and plan.** Status: **Done**. Tested: Yes, Validated: No on every row.
11. **Sandbox live check.** Status: **Not started**. `scripts/model-routing-live-check.sh` is
    written by a later task, which will flip Validated on the rows it shows live.
