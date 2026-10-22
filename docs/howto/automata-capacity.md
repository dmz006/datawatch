---
docs:
  index: true
  topics: [prd, autonomous, capacity, queue, compute, sessions, priority]
exec_steps:
  - tool: capacity_status
    description: Show capacity pools, limits, holders and the wait queue
    args: {}
    read_only: true
---

# Automata capacity: waiting instead of failing

Several Automata can run at once, next to your own interactive sessions,
on a limited host and a limited model server. datawatch keeps a small
ledger of **capacity pools** so a task that cannot start yet **waits**
(status `waiting_capacity`) instead of failing, and so concurrent
Automata share the hardware fairly.

## What it is

- **Pools.** `host` (session slots, from `session.max_sessions` minus
  the slots reserved for you), `node:<name>` (autonomous sessions in
  flight on a compute node), `llm:<name>` (autonomous sessions in
  flight on one LLM). A task needs a free slot in every pool it touches.
- **Waiting is not failing.** A waiting task shows why (which pool, who
  holds it), does not use an auto-fix retry, and starts when a slot
  frees. It fails only if it waits longer than
  `autonomous.capacity_wait_timeout_seconds` (default 4 hours), and the
  error names the pool that never freed.
- **Fair queue.** Waiting tasks are served by priority, then round-robin
  across Automata (one busy Automaton cannot starve another), then in
  order within an Automaton. A task that has waited over 10 minutes goes
  ahead of model preference.
- **Model affinity.** Among equals, a task that wants the model already
  loaded on a node goes first, avoiding a slow model reload.
- **Your sessions come first.** `session.reserved_interactive` slots
  (default 1 when `max_sessions` is 3 or more) are never used by
  Automata, and sessions you start are never queued.
- **The verifier does not add a second slot.** When a task moves from
  worker to verification, it keeps the one slot it already holds (the
  lease is re-keyed to the verifier); a second, separate lease is only
  taken if the verifier runs on a different pool set.
- **Planning waits too.** A decompose run occupies the same `llm:` / `node:`
  pools a task would use (its planner lease excludes the `host` pool, since
  the planning session itself already counts against it); a saturated pool
  parks the PRD in `planning` with a visible wait reason instead of failing
  the attempt.
- **Waiting on the outside releases the slot.** A worker parked on an
  external condition (long CI run, deploy, upstream review) returns its
  capacity slot for the wait and gets it back through the normal queue when
  it resumes — the wait is visible, not a stall.
- **GPU backpressure (optional).** With `autonomous.capacity_gpu_util_pct`
  set, new tasks are held while a node's busiest GPU is at or above that
  percent.

## Base requirements

- datawatch running with autonomous enabled.
- For node and LLM limits: at least one compute node / LLM configured
  (Settings > Compute).

## Setup

Limits default to: host from `session.max_sessions`, node and LLM limits
unlimited (0) — except the per-LLM plan-tier rules in the section below.
Turn on the limits that match your hardware. Example: one
large model on one GPU node, so only one autonomous session at a time:

```bash
# REST: set the node limit (0 = unlimited)
curl -X PUT https://localhost:8443/api/compute/nodes/gpu-1 \
  -H 'Content-Type: application/json' -d '{"name":"gpu-1", "max_concurrent_sessions": 1, "...": "other fields unchanged"}'
```

Other channels:

- **PWA:** Settings > Compute > node edit > *Max concurrent sessions*;
  LLM edit > *Max in flight*; Settings > General > Automata > capacity
  fields; Automata > Capacity card shows pools and the queue.
- **MCP:** `compute_node_update` (`max_concurrent_sessions`),
  `llm_update` (`max_inflight`), `config_set` for
  `autonomous.capacity_*` and `session.reserved_interactive`,
  `capacity_status` to view, `autonomous_prd_set_priority`.
- **CLI:** `datawatch capacity` (add `--json`), `datawatch config set
  autonomous.capacity_wait_timeout_seconds 7200`.
- **Chat channels:** `capacity`, and `configure
  session.reserved_interactive=2`.
- **YAML:**

```yaml
session:
  max_sessions: 6
  reserved_interactive: 1
autonomous:
  capacity_enabled: true
  capacity_wait_timeout_seconds: 14400
  capacity_gpu_util_pct: 0      # 0 = off; e.g. 90 to hold tasks while a GPU is 90% busy
```

  plan_tier_max_inflight: {pro: 1, max_5x: 2, max_20x: 4, api: 0}
```

## Per-LLM caps: explicit and plan tier

Each LLM registry entry can carry an explicit `max_inflight` (0 =
unlimited). When it does not, datawatch derives the cap from the entry's
optional `plan_tier` field — the operator's subscription tier for that
LLM (`pro`, `max_5x`, `max_20x`, `api`) — via the
`autonomous.plan_tier_max_inflight` table. Precedence, always in this
order:

1. **`max_inflight` > 0** on the LLM entry — an explicit operator
   override always wins;
2. **`plan_tier` + the table** — the tier's mapped value;
3. **conservative kind default** — 1 in-flight for subscription kinds
   (claude-code and the other non-local, non-API kinds), unlimited for
   local Ollama/OpenWebUI kinds (those stay governed by the `node:` pools
   and `max_engines_per_node`) and for dollar-metered API kinds (the
   dollar budget is a later phase).

The default table `pro: 1, max_5x: 2, max_20x: 4, api: 0` is **operator
policy, not a technical limit** — change it in one place to match what
your plans actually allow. Every other channel: PWA Settings > Compute >
LLM edit (the *Plan tier* and *Max in flight* fields), REST
`PUT /api/llms/{name}` (`plan_tier`, `max_inflight`), MCP `llm_update`,
and `datawatch llm update`. The `GET /api/llms` and `GET /api/capacity`
responses carry the resulting effective limit plus its source (`explicit`
/ `plan_tier` / `kind`), so you can always see which rule applied. The
caps count simultaneous autonomous inflight sessions per LLM; they are
not a rate limit.

## One slot from spawn through verification

A task holds exactly one capacity slot through its whole life: the
worker's lease is handed to the verifier (holder `verify:<task>`) the
moment verification starts, not released and re-acquired — there is no
window in which a sibling task can steal the slot, and the task is never
counted twice. If the verifier's backend/model resolves to a different
pool set than the worker's, the hand-off is refused and the verifier is
admitted separately with its own lease; a second slot is never stacked on
top of the first.

## Decompose waits for capacity

The planning (decompose) call is capacity-gated the same way task spawns
are: before it runs, it takes a planner lease on the same `llm:` / `node:`
pools the PRD's tasks would use (excluding the `host` pool — the planning
one-shot already counts against it directly). On a saturated pool the PRD
stays in `planning` with a visible wait reason
(e.g. `capacity: planner waiting (n planner / m worker active …)`) instead
of failing the attempt; the PRD starts as soon as a slot frees. The wait
is bounded by `autonomous.capacity_wait_timeout_seconds`, and cancelling
or rejecting the PRD drops the planner from the queue immediately —
reported as "decompose cancelled", and not consuming the decompose retry
budget. The planner ranks after workers and verifiers, so planning never
starves running work.

## Park on an external condition

A task blocked on something outside the daemon — a long CI run, a deploy
finishing, an upstream review — parks its slot instead of pinning it.
The worker emits one marker line and stops:

```
DATAWATCH_AWAIT_EXTERNAL: CI run on the branch
```

datawatch then, within seconds:

1. parks the task in a visible wait state — the `await_external`
   sub-reason (with the condition) in the task's `wait_reason` on every
   status surface;
2. releases the task's capacity lease, so the slot is immediately
   available to queued tasks;
3. releases the project-tree write lock *unless the tree has uncommitted
   changes or commits not reachable from a remote-tracking ref* —
   unpushed work keeps the tree locked;
4. re-admits the task through the normal bounded capacity wait, then
   prompts the parked session to re-check the condition and continue — it
   resumes on the same session with its full context. No retry or failure
   is counted, and the session's stall and idle detectors are suspended
   while it is parked.

If re-admission times out or is cancelled, the task is left lease-free in
its wait state and the normal retry machinery takes over. Each cycle is
audited (`autonomous_idle_wait_release`, naming the pools and tree lock
released), and a daemon restart re-adopts a parked task whose session
survived (or re-queues it if the session is dead), so the park survives
restarts.

## Use it

1. Start two Automata that use the same limited node. The second one's
   tasks show **waiting for capacity** with the reason, for example
   `pool node:gpu-1 full (1/1, held by <task>)`.
2. Watch the queue: `datawatch capacity`, `GET /api/capacity`, or the
   PWA Capacity card. Prometheus exposes `datawatch_capacity_pool_used`,
   `datawatch_capacity_pool_limit` and `datawatch_capacity_waiting`.
3. Give an important Automaton a higher priority so its tasks go first:
   `POST /api/autonomous/prds/{id}/set_priority {"priority": 10}` (PWA:
   Automaton settings > Priority).
4. Cancel or reset a waiting task like any other; cancelling removes it
   from the queue.
5. After a daemon restart, waiting tasks are put back in the queue.

An alert is raised when a task has waited 30 minutes.

## Common pitfalls

- **Everything queues behind one big model.** That is the node limit
  doing its job. Raise `max_concurrent_sessions` if the server handles
  more than one at a time.
- **Tasks wait although nothing seems to run.** Your own interactive
  sessions count against the `host` pool. Check `datawatch capacity` for
  the holders, or lower `session.reserved_interactive`.
- **Wait timeout errors.** Raise `capacity_wait_timeout_seconds`, add
  capacity, or lower the number of Automata running at once.
- **Turning it off.** Set `autonomous.capacity_enabled: false`; tasks
  then start immediately, and a full `session.max_sessions` still makes
  them wait and retry rather than fail.

## Diagram

See [`../flow/automata-capacity-flow.md`](../flow/automata-capacity-flow.md).

## See also

- [Automata planning](autonomous-planning.md)
- [Automata DAG orchestrator](automata-orchestrator.md)
