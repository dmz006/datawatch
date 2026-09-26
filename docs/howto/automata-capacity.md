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
- **GPU backpressure (optional).** With `autonomous.capacity_gpu_util_pct`
  set, new tasks are held while a node's busiest GPU is at or above that
  percent.

## Base requirements

- datawatch running with autonomous enabled.
- For node and LLM limits: at least one compute node / LLM configured
  (Settings > Compute).

## Setup

Limits default to: host from `session.max_sessions`, nodes and LLMs
unlimited. Turn on the limits that match your hardware. Example: one
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
