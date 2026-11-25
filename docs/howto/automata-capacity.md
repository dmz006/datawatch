---
docs:
  index: true
  topics: [prd, autonomous, capacity, queue, compute, sessions, priority]
exec_steps:
  - tool: capacity_status
    description: Show capacity pools, limits, holders, the wait queue and per-node usage (loaded models with memory size and expiry, GPU figures)
    args: {}
    read_only: true
  - tool: autonomous_status
    description: Show the queued tasks with their wait reasons, what running tasks hold, back-off flags and budgets
    args: {}
    read_only: true
  - tool: audit_query
    description: List every budget audit event (the trailing * makes the action a prefix match)
    args: {action: "autonomous_budget_*", limit: 20}
    read_only: true
  - tool: capacity_admission
    description: Ask whether a start of a backend would be admitted now, queued or moved elsewhere (changes nothing)
    args: {backend: "ollama"}
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
  An LLM entry that lists several compute nodes is a pool of nodes; the
  ledger picks one of them for each worker. See
  [Compute-node pools and slots](#compute-node-pools-and-slots).
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
- **A verifying task holds no worker slot.** When the worker session ends and
  the task moves to verification, its worker slot is released at once. The
  verifier then waits for a slot of its own: on the node's verifier pool where
  the node has one, otherwise on the pools its backend uses. While it waits,
  the freed worker slot goes to the next waiting task, so a slow verification
  no longer holds up the lane. See
  [The worker slot at verification](#the-worker-slot-at-verification).
- **Planning waits too.** A decompose run occupies the same `llm:` / `node:`
  pools a task would use (its planner lease excludes the `host` pool, since
  the planning session itself already counts against it); a saturated pool
  parks the PRD in `planning` with a visible wait reason instead of failing
  the attempt. Changing the Automaton's decomposition profile or model while
  the run is queued re-points that queue entry at the new pool without
  starting a fresh attempt — see "Retargeting while queued" below.
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

A node that hosts a local engine takes its worker count from its slot count:
the operator's `session_slots` when it is set, otherwise a count derived from
the node's observer data. A node that hosts no local engine keeps its
`max_concurrent_sessions` limit (0 = unlimited) and ignores `session_slots`.
The rules are in [Compute-node pools and slots](#compute-node-pools-and-slots).

Other channels:

- **PWA:** Settings > Compute > node edit > *Max concurrent sessions*;
  LLM edit > *Max in flight*; Settings > General > Automata > capacity
  fields; the Capacity card on the Automata tab's Automation sub-tab shows the
  machine-wide pools and the queue.
- **MCP:** `compute_node_update` (`max_concurrent_sessions`),
- **PWA:** Settings > Compute > node edit > *Max concurrent sessions* and
  *Session slots*; LLM edit > *Max in flight*; Settings > General > Automata >
  capacity fields; Automata > Capacity card shows pools and the queue.
- **MCP:** `compute_node_update` (`session_slots`, `max_concurrent_sessions`, `hard_limit`),
  `llm_update` (`max_inflight`), `config_set` for
  `autonomous.capacity_*` and `session.reserved_interactive`,
  `capacity_status` to view, `autonomous_prd_set_priority`.
- **CLI:** `datawatch compute node update <name> --session-slots N`,
  `datawatch capacity` (add `--json`), `datawatch config set
  autonomous.capacity_wait_timeout_seconds 7200`.
- **Chat channels:** `capacity`, and `configure
  session.reserved_interactive=2`, and `compute node update <name> session_slots=N`
  (0 = derive from observer data; a node update replaces the whole node, so repeat
  every field you want to keep).
- **YAML:**

```yaml
session:
  max_sessions: 6
  reserved_interactive: 1
autonomous:
  capacity_enabled: true
  capacity_wait_timeout_seconds: 14400
  capacity_gpu_util_pct: 0      # 0 = off; e.g. 90 to hold tasks while a GPU is 90% busy
  plan_tier_max_inflight: {pro: 1, max_5x: 2, max_20x: 4, api: 0}
compute_nodes:
  - name: gpu-1
    kind: ollama
    address: http://gpu-1:11434
    session_slots: 2            # 0 = derive from observer data; a negative value is refused
```

## Compute-node pools and slots

An LLM entry can list more than one compute node in its `compute_nodes` list.
That list is the entry's **pool** of nodes, not a failover order. Each worker
the entry starts is placed on one node of the pool, and that node's
`node:<name>` pool carries the worker's lease. An entry with one node behaves
exactly as before: that node takes the lease and nothing is chosen.

**Which node is picked.** A `pi` or `pi-prompt` backend takes its candidate
nodes from its own LLM entry's `compute_nodes` list. The ledger orders them with
load first: the node holding the fewest sessions relative to its limit goes
first, and `scheduling_priority` (0 to 100, default 50, higher first) only
breaks a tie, then list order. A node with no free slot is passed over, and the
next one is tried. The provider-kind lookup (nodes found by the model's
provider, such as `ollama`) applies only to backends that have no node list of
their own. See [How a pi-family worker picks its
node](compute-nodes.md#how-a-pi-family-worker-picks-its-node).

**Checking where waiters sit.** `scripts/check-waiter-nodes.sh` reads
`GET /api/capacity` (through `datawatch capacity --json` when the CLI is
installed and `DATAWATCH_URL` is unset, else curl with `DATAWATCH_URL` and
`DATAWATCH_TOKEN`). It prints one line per waiting request: holder, backend,
model, bound node and candidate nodes, then a count of waiters per node. A
`node=-` line is not yet bound: the daemon picks among several candidates only
at dispatch, and the line lists them. The script exits `2` when, for a
multi-node backend, every waiter is bound to one node while another candidate
node has free capacity; that is the symptom to investigate. Independent of any
waiter, it also prints an `OVERLIMIT` line for each node that holds more engines
than its limit (`OVERLIMIT: node:<name> holds <n> of <limit> engines`), followed
by one indented line per lease on that node with the holder, session, model and
node. A node at or under its limit, or with no limit, prints nothing. The script
exits `2` for an `OVERLIMIT` node too, even when nothing is waiting. It exits `1`
when the daemon cannot be read, and `0` otherwise.

**A relaunched task.** A task that is relaunched, meaning its earlier session is
replaced (including one that was verifying), gives up the lease of the previous
attempt and is admitted through the same placement as a new request: the least
loaded of the backend's own compute nodes, one engine per node. The node the
previous attempt used is only a tie breaker, chosen when it is free and equally
loaded; it never outranks a less loaded node. If no node is free, the relaunch
waits like any other request. A worker that survives a daemon restart keeps the
node recorded for it. A respawned verifier and a task returning from an external
wait are placed the same way. No cap or setting changed.

**Slots per node.** A node that hosts a local engine (`pi`, `pi-prompt`,
`ollama`, `opencode`, `opencode-acp`) has a slot count: how many workers it
runs at once. The count is the first of these that applies:

1. `session_slots` set by the operator, when it is above `0`.
2. Derived from GPU data, when the observer reports GPUs. Each GPU takes at most
   one engine. The free GPU memory is each GPU's total minus its used memory,
   summed; the count is how many whole copies of the served model fit in it,
   capped at the number of GPUs. Memory held by any other program lowers it.
3. Derived from system memory, when the observer reports no GPU data. The free
   system memory divided by the size of the served model, at least one. The
   count does not use CPU cores.
4. `autonomous.max_engines_per_node`, when the observer gives neither. Its
   default is 1.

When `max_concurrent_sessions` is set on the node and smaller, it caps the
count. A derived count is read again every 10 seconds, so a change takes effect
without a restart. A derived count never drops below the workers already running
on the node, but that floor is capped at the GPU count. Running workers are not
stopped. A node that hosts no local engine takes no slot count at all; it keeps
its `max_concurrent_sessions` limit.

### Soft node slots and hard_limit

Node slots are not a physical limit by default. A local engine can often run
several models at once, only slower, and a machine with a lot of memory may run
many; how many is up to the user and the hardware. So an operator start (a
session you start yourself) over the `node:<name>` or `node-verify:<name>` pool
of a node is admitted like it would be on any other pool, and its lease records
the overage: the pool, its limit and how far over. It shows on the pool line of
`datawatch capacity`, in the chat `capacity` verb, in the Capacity card, and in
`GET /api/capacity`, and raises an alert when
`autonomous.capacity_operator_overage_alert` is on.

Nothing else changes. Workers still wait for a free slot on a node pool, the
supervisor class does not use node pools, and the node a worker is placed on is
picked by the same rules as before (least loaded first, then priority); an
operator's overage lease counts as load on that node, so a worker goes to a node
with room.

If you know a node's engine truly cannot take more, turn `hard_limit` on for that
node (default off). With it on, that node's pools keep the old behaviour for every
class: an operator start waits for a free engine and is refused with the usual
capacity message if none frees up in time. The setting is per node and read live;
a peer's node setting is its own, because each daemon reads only its own nodes.

- **PWA:** Settings > Compute > node edit > *Hard limit*.
- **CLI:** `datawatch compute node update <name> --hard-limit` (update replaces
  the whole node, so pass it again to keep it).
- **MCP:** `compute_node_update` / `compute_node_add` with `hard_limit`.
- **Chat channels:** `compute node update <name> hard_limit=true`.
- **REST:** `PUT /api/compute/nodes/{name}` with `"hard_limit": true`.
- **YAML:** `hard_limit: true` on a `compute_nodes` entry.

### Verifier slots

Verification (the model that judges a worker's output) can run on the same node
as the workers it checks. The verifier uses its own pool on the node, so on a
node that hosts a local engine a verifier no longer draws on the executor's
slots above; the two pools coexist and are limited independently:

- **How many.** `verifier_slots` on the node, unset by default. Unset means one
  verifier slot on any node that hosts a local engine and none on a node that
  does not. Set it to a positive number to allow more concurrent verifiers; set
  it to `0` explicitly to stop verifier runs landing on that node at all (a
  verifier for that node then holds the executor pool, as before). The verifier
  pool never raises the worker pool — the two coexist and are limited
  independently.
- **Memory floor.** `verifier_min_free_mem_mb` on the node, default 1024 MB. A
  verifier is placed on a node only if the node still has at least that much GPU
  memory free, so a verifier does not squeeze a worker's model out of the GPU.
  A node the observer can't report GPU figures for is not gated.
- **Advice figure.** `verifier_context_mb` on the node, default 0, meaning
  unknown. When set, the node's detail reports an advice figure — the node's
  free GPU memory divided by the per-verifier context size — for how many
  verifiers the node's current free memory would hold. It is only a figure; it
  is never applied and never changes any limit.

The three keys are per-node and set through any surface: the PWA node form, the
CLI (`datawatch compute node add|update <name> --verifier-slots N
--verifier-min-free-mem-mb N --verifier-context-mb N`), REST
`POST /api/compute/nodes` / `PUT /api/compute/nodes/{name}`, the MCP tools
`compute_node_add` / `compute_node_update`, and the comm channel's compute node
add/update kv pairs.

**Sizing the pool.** Start at the default and read the counters — wait time and
run time per verifier run, in the capacity status and the Prometheus verifier
counters (see the operations section *Verifier throughput*). When wait is a
large part of total verifier time and the node's free GPU memory comfortably
holds the extra slot (the advice figure, if you set `verifier_context_mb`, shows
how many it would hold), raise `verifier_slots` and watch again. If infra
failures or watchdogs dominate, more slots will not help — fix the failures
first. The operations side of this, including the decision rule, is in the
*Verifier node limit* section of the operations manual.

**Loading the model.** A worker that lands on a node of a multi-node pool gets
its model made resident on that node before it starts. The daemon checks the
node's running models. If the model is absent, it asks Ollama to load it and
waits up to 10 minutes for the model to show as loaded, checking every 2
seconds. A node with a pinned model loads its pin. A request for another model
is refused unless `allow_model_swap` is set, and that refusal counts as a load
failure. A single-node entry has no load step.

When a load fails, because the node cannot be reached, the load request is
refused, or the model is not loaded in time, the daemon raises an error alert
naming the node and the model. It releases the lease and places the worker
again on another node of the pool, with the failed node left out for that
attempt. When no other node is left, the start fails with the load error.

**Judges hold node leases.** The verifier and the guardrail judge take a lease
on a node in the same way. A verifier prefers the node its task's worker holds
and takes one of that node's verifier slots (see [Verifier slots](#verifier-slots)
above), so it runs alongside the workers there. A node below its free-memory
floor for verifiers is skipped for the verifier, the way a full node is skipped
for a worker. A guardrail judge on an entry with one node
takes no lease, as before.

**What you see.** When the holders of a pool span two or more nodes, the
capacity card and `datawatch capacity` print the node name beside each holder.
A pool whose holders are all on one node is shown as before. The admission
query names the node a start would be placed on. The
[capacity API](../api/autonomous.md#holder-nodes) gives the same figures.

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
   and `max_engines_per_node`) and for dollar-metered API kinds (cap those
   with a per-LLM dollar budget, below).

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

## The worker slot at verification

A task holds at most one capacity lease at a time. While its worker runs, that
is the worker's lease. When the worker session ends and the task enters
`verifying`, the worker's lease is released at once, so its slot is free for
other work while the verification runs or waits. The verifier then takes its
own lease, holder `verify:<task>`, on the pools it needs: the node's verifier
pool where the node has one, otherwise the pools its backend and model map to.
The verifier never stacks on a worker lease that is still held.

The worker's lease is not handed over to the verifier. A task in `verifying`
holds no worker slot, so a verifier waiting for its slot does not keep a lane
shut. The verifier still follows the worker's node when
`autonomous.model_routing.verifier_follows_worker_node` applies, because the
task keeps the node its worker ran on.

The guardrail judge works the same way. Its lease is `judge:<task>`. A worker
lease still present when the judge starts is released, not handed over, and the
judge then waits for its own slot like the verifier does.

## Verifying tasks in the capacity view

A task whose worker session has ended and is verifying keeps its place in the
Automaton but not its worker lane. Its verification takes a **verifier slot**
(on the node's verifier slots, or from the verifier pool). If no verifier slot
is free, the task waits for one without holding a worker slot, so the lane goes
on to the next task.

A retry after a failed verification is a new start: it goes back through
admission like any other task and may wait in `waiting_capacity` until a worker
slot is free.

The capacity view lists every verifying task and says whether it is running its
verifier or waiting for a slot:

- REST: `tasks.verifying` in `GET /api/capacity`.
- CLI: `datawatch capacity` (the `Verifying (n):` block).
- MCP: the `capacity_status` output.
- PWA: the capacity card under Settings.

**Throughput and sizing.** A lane is now busy only for worker time, so the work
a lane gets through is bounded by how long workers run, not by how long
verification takes. Verification load moves to the verifier slots instead. Size
a node's `verifier_slots` to the number of verifications you expect to overlap,
roughly the node's worker lanes times the share of a task's time spent
verifying, rounded up, and keep it within what the node's free memory can hold.
If the capacity view often shows tasks waiting for a slot, raise it; if it
never does, lower it. See [Verifier slots](#verifier-slots).

## An idle verifier is the verifier's own incident

When a task moves to verification and the verifier session goes idle — no
output, no pane activity, a dead runner — datawatch does not treat it as a
worker failure. It re-runs the verification on a **fresh verifier session**,
up to a small fixed number of times, and each idle is reported as a
**verifier incident**: an event on the task (which verifier backend and model
went idle and for how long), an entry on the Automaton's decision log, and an
`autonomous.verifier_incident` audit event you can list like any other
(`datawatch audit --action autonomous.verifier_incident`).

None of this touches the worker's attempt budget, its attempt count or its
escalation failures — the worker's result was never judged, so there is
nothing to fail. If the verifier keeps going idle until the small cap is used
up, the task is parked for operator review (`held for operator review: the
verifier went idle N times`, in the infra category). Still no worker failure
is counted, and the worker's result is left standing for you to judge.

## Retries with an explicit backend and model

When you retry a task and pick a backend and model yourself (a review
retry, a reset with a chosen LLM), capacity admission uses exactly that
pair. The task takes its slot in **that backend's pool** (`llm:<name>`,
plus the node pool the model lives on), not in the pool of the Automaton's
default worker. The pair it was admitted with is kept on the task, so
re-admission after a restart and lease adoption place it on the same pool
again instead of working the worker out a second time. A worker chosen
only for one attempt by escalation or routing is not written back to the
task's own backend and model.

The wait reason names the pool the task asked for. A retry pinned to
`claude-code` while that pool is full reads
`pool llm:claude-code full (2/2, held by t-3f9a1c, t-8b20de)`, never the
name of some other pool.

The wait timeout (`autonomous.capacity_wait_timeout_seconds`) counts from
the first admissible request, that is from the moment the task asks the
ledger for its pool. Time spent before that (waiting on dependencies, an
unstarted or paused Automaton, a tree lock) does not use up the timeout.

### Pool fall-through

> **Operator review:** fall-through is off by default. Before turning it on, review which LLM entries you mark as **Allowed fall-through target**, since a waiting worker may then run on any of them. The glossary entries are in [glossary.md](../glossary.md#pool-fall-through).

By default a task always waits for its own pool, explicit backend or not.
Turn on `autonomous.capacity_fallthrough` (default off) and a worker task
whose requested pool cannot be granted right now is admitted on another
allowed backend and model that has room, instead of waiting. The first
alternate the ledger would grant wins, in preference order. If the
requested pool has room, or no alternate does, nothing changes and the task
waits as usual. Worker tasks and planners fall through; verifiers and
judges do not. A planner is judged on its own pools (the host pool is left out,
as when it is admitted), only moves to an alternate that can name its model,
looks again on every wait pass like a task, runs on the alternate for that
attempt only (the Automaton keeps its own planning backend), and the Automaton
records a `planner_fallthrough` decision naming both backends.

The requested pool is the one the task's **effective backend** maps to. The
backend is resolved in this order: task, story, PRD, Automaton, the
Automaton's lane preference (the Claude or GPU lane backend), then the
daemon default (`execution_backend`). A task that falls back to its lane
preference is judged against that lane's pool, not the daemon default's.

The alternates for a task requested on a backend and model, in order:

1. **The lane fill pair.** The other of the Claude lane backend and the GPU
   lane backend from `autonomous.lane_fill` (`claude_backend`,
   `gpu_backend`), on that backend's default model. For the GPU lanes that
   is `autonomous.lane_fill.gpu_model` when set, otherwise the entry's own
   model. A task on neither lane backend has no lane fill pair.
2. **Entries you marked.** Every LLM entry with **Allowed fall-through
   target** switched on, in name order. The task keeps its model when the
   entry serves it; otherwise it runs on the entry's default model. An entry
   is never a target until you mark it.

The ledger is asked about the alternates in that order and the first one
it would grant wins, so a lane fill pair with room is always preferred to a
marked entry. An alternate is skipped when:

- its backend is **disabled**,
- it is already at its **in-flight cap**, or
- it **cannot serve the model** it would run.

The requested backend and model are never their own alternate. Only a worker
task falls through: a **verifier never falls through** (nor does a judge or
a planner), because it must run on the backend it was resolved to. A worker
pinned to a compute node does not fall through either, since the alternate
would be placed without the pin.

Mark an entry as a target on any surface:

| Method | How |
|--------|-----|
| **YAML** | `fallthrough_target: true` on the entry in the `llms:` list |
| **CLI** | `datawatch llm update <name> --fallthrough-target` (`--fallthrough-target=false` clears it; leaving the flag off keeps the stored value) |
| **Web UI** | Settings, LLM, edit an entry, **Allowed fall-through target** |
| **REST API** | `PUT /api/llms/<name>` with `{"fallthrough_target": true}` (omit to keep the stored value) |
| **MCP** | `llm_update` or `llm_add` with `fallthrough_target=true` |
| **Comm channel** | `llm update <name> fallthrough_target=true` |

#### Tasks that are already waiting

A task that is already waiting looks again on every wait pass (about once a
second), reading the setting from the live config each time. Turning
`autonomous.capacity_fallthrough` on therefore admits the tasks that are
waiting now, with no restart and no re-queue, as soon as an alternate has
room; the task keeps its place and its wait timeout. Marking an LLM entry as
a target, or changing the lane fill backends, is picked up the same way on
the next pass. Turning the setting off stops further fall-throughs; a task
already admitted on an alternate keeps it.

#### Explicit backends

A backend set explicitly on the task, story, PRD or Automaton is not
protected from fall-through. It is held to the same rule as every other
worker: it waits for its own pool while the setting is off, and falls
through only while the setting is on. Check the audit note to see whether it
happened: an explicit backend that fell through has a note ending
`; explicit backend; fall-through setting on`. A task whose backend came
from the Automaton's lane preference or the daemon default is not explicit,
and its note has no such ending.

#### Audit fields

A task that fell through leaves an `autonomous_capacity_admitted` audit
entry for the task, and the daemon log carries the same note on one line.
The entry has these fields:

| Field | Meaning |
|---|---|
| `fall_through` | `true`; this is what tells a fall-through entry from a plain admission |
| `from_pools` | the pools the task asked for (sorted), for example `llm:claude-code` |
| `to_pools` | the pools the task was admitted on (sorted), for example `llm:ollama`, `node:gpu-1` |
| `note` | what happened in one sentence, for example `fell through from claude-code/sonnet to ollama/qwen: requested pool full, alternate idle` |

The entry also carries `pools` (the same as `to_pools`), `task_id`,
`task_title`, `prd_id` and `prd_title`. A task that had waited first also
gets the usual plain `autonomous_capacity_admitted` entry with
`waited_seconds` beside it. List the fall-throughs with
`audit_query` for `autonomous_capacity_admitted` and look for
`fall_through`.

The Automaton's decision log gets a `capacity_fallthrough` entry too. It names
the backend and model the task was admitted on, the pool it now holds, the
backend, model and pool it asked for, and the reason (`requested pool full,
alternate pool free`). The lane is chosen at the moment of each admission from
the task's effective backend (task, then story, then Automaton), so a relaunch
is judged afresh. A task that stays on its own pool, or keeps waiting, writes
no entry. The capacity page lists the pool the task actually holds, so a task
that fell through shows the alternate's pool.

Set it on any surface:

```bash
# CLI
datawatch autonomous config-set '{"capacity_fallthrough":true}'
# REST
curl -X PUT /api/config -d '{"autonomous.capacity_fallthrough": true}'
# MCP: autonomous_config_set with capacity_fallthrough=true
# Chat channels
configure autonomous.capacity_fallthrough=true
# PWA: Settings, Automata capacity, "Pool fall-through"
# YAML: autonomous.capacity_fallthrough: true (under autonomous:)
```

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
starves running work. A planner that has waited ten minutes ranks as a
worker, so the Automaton's priority decides between it and the workers
instead of workers always going first; this keeps planning, which feeds the
lanes, from starving. Verifiers still go first.

### A plan you start, and a planner you end

A decompose **you** start (web app, CLI, chat, MCP or REST) skips the blocks
that apply only to automatic work: a [usage back-off](#reactive-usage-backoff),
the burn guard and a usage hold, as a session you start yourself does. Pool
limits still apply, so on a full pool it queues like any other planner. A plan
the engine starts (lane fill, the restart after a daemon restart, the re-plan
after a reworded spec, a child or scan-fix task) is still held by them. The
daemon cannot tell who is behind a REST or MCP call, so a decompose asked for
that way counts as yours.

If you end a planner session yourself (`datawatch session kill`, the web app,
chat, MCP or REST), datawatch does not start it again. The Automaton goes to
`failed` with "the planner session was ended by the operator"; reset it to
draft when you want it planned. A planner that ends for any other reason (the
daemon's own watchdogs, a shutdown, a vanished process) is retried as before.

### Retargeting while queued

Changing the Automaton's decomposition pair (profile or model) with
`set_llm` while its planning run is still in the queue retargets that run's
single queue entry in place to the pool the new pair resolves to (outcome
`requeued`): the old queue slot is cancelled, the run is re-queued on the new
pool, and the wait reason names the pool the run is now waiting on. The wait
budget is not reset — the run keeps the time bound it was admitted with — and
no new attempt starts, so the decompose retry budget is untouched. A run that
already started (holds a live lease) is left alone and the new pair applies
from the next planning run (`deferred_to_next_run`). Re-targeting an entry
that already sits on the target pool is a no-op, so changing the pair twice
leaves exactly one queue entry and no leaked capacity.

Workers get the same move: when a task that is `waiting_capacity` is
re-pointed with `set_task_llm` / `set_story_llm` (or an Automaton-level
`set_llm`) while `autonomous.override_queued_tasks` is on, its queued
request is retargeted to the new pool and reported with
`capacity_moved: true` (see `docs/operations.md`, the LLM override
section, and [`autonomous-planning.md`](autonomous-planning.md) for the
queued-versus-started behaviour and the changed / left-alone report).

## Changing a waiting task's worker

A per-task worker override — the backend and model written onto the task
itself with `set_task_llm` (or the task's story with `set_story_llm`, which
each task inherits when it has no override of its own) — is honoured at
admission: the task's next start resolves that pair and takes its slot in
that backend's pools, exactly like the explicit pair of a retry above. What
a task is admitted with is carried on the task, so a re-admission after a
daemon restart places it on the same pool again.

That works on a task that is **waiting for capacity** too, while the
Automaton is still running, as long as `autonomous.override_queued_tasks` is
on (the default — unset reads as on). The call touches only unstarted
tasks, and a waiting task gets more than the write: its single queued
capacity request is **re-targeted in place** to the pool the new pair
resolves to and re-queued there immediately, so its re-admission happens
against the pool you actually want, and its wait reason names that pool
from the next read on. Nothing is duplicated — moving the same queue entry
to the pool it already carries is a no-op — and a task that already holds a
live lease is left entirely alone, along with every running or finished
task, each reported as *left alone* with its reason. The response names
each affected task as *changed* or *left alone* and says, for the ones that
moved, that their capacity request was retargeted (`capacity_moved`).

The move does not depend on the task's stored backend changing. A task whose
stored pair already equals the one you set can still be queued on a different
pool, because it inherited its worker from its story or Automaton, or because
an earlier fall-through placed it elsewhere. So a waiting task is always
checked against the pool its resolved pair names, and moved if it is queued
somewhere else. When the request already sits on that pool nothing changes.
Whenever the request moves, or the task is reset, the `capacity_fallthrough`
note recorded for its old request is dropped, since it no longer says where the
task waits; the next admission records its own.

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
4. Reset a waiting task to re-admit it (it has not started, so no
   force is needed — the reset keeps its place in the capacity queue,
   retargeting its target pool if the worker resolution changed, with
   a fresh wait budget); cancelling removes it from the queue outright.
5. After a daemon restart, waiting tasks are put back in the queue.

An alert is raised when a task has waited 30 minutes.

## Reactive usage backoff

When a backend runs out of usage (a rate limit or a weekly cap), starting
more autonomous worker work on it only burns retries. A usage back-off flag
holds new **worker** sessions on that one backend until the limit lifts. The
flag is per backend, and it survives a daemon restart (see
[Across a daemon restart](#across-a-daemon-restart)); backing off
`claude-code` never affects `pi` or `ollama`. The implementation lives in
`internal/autonomous/backoff.go`; the design is in
`docs/plans/2026-10-14-concurrency-and-budget-design.md`.

The flag only pauses autonomous worker dispatch. It never blocks an
interactive session start, so you can still open a session on a backed-off
backend by hand. While a flag is active, new worker tasks that would use that
backend wait with a visible wait reason (like any capacity wait) instead of
failing or burning their retry budget, and they resume when it clears. The
review roles — guardrail judges, verifiers and decompose/planning sessions —
are a separate matter: they may still be admitted on that backend through the
[review reserve](#the-review-reserve), unless the reserve is full.

### Automatic set and clear

When a session on a backend reaches the rate-limited state
(`StateRateLimited`, detected by `session.rate_limit_patterns` in
`internal/session/manager.go`), datawatch sets the flag for that backend
automatically. It clears itself at the reset time parsed from the backend's
message. When no reset time can be parsed, it falls back to a maximum
back-off of 60 minutes, set with `autonomous.backoff_max_wait_minutes`.

The mode switch is `autonomous.usage_backoff`: `pause_backend` (the default)
sets flags automatically, and `off` disables only the automatic path. A
manual set still works in `off` mode, so you always keep an escape hatch.
The global cooldown (`rate_limit_global_pause`) is separate: a spawn refused
by it also waits rather than fails.

### Max back-off with no reset time

Some backends say only "usage limit reached" without telling you when it
lifts. Then there is no reset time to parse, and datawatch holds the backend
for `autonomous.backoff_max_wait_minutes` (default 60, whole minutes; a
value below 1 is treated as 60) and then lets one autonomous attempt through.

If that attempt is rate-limited again, the flag is set again for another
full window, so a long outage costs one probe per window rather than a
retry storm. If you know the real reset time, set the flag yourself with
`until` or an exact number of minutes (below) instead of waiting for the
maximum to expire.

### The review reserve

A back-off is a worker-side hold, not a total blackout. While the flag is up,
the review roles — **guardrail judges, verifiers, and decompose/planning
sessions** — may still be admitted on the held backend through its
**review reserve**: a bounded number of live session slots, plus optionally a
rolling one-hour token budget, decided at admission time by
`internal/autonomous/backoff_reserve.go` (installed on the capacity ledger as
the role-aware back-pressure gate). Two keys bound it:

- `autonomous.backoff_reserve_for_review` — the reserve's live slots. Default
  `1`. `0` disables the reserve: every new session on the held backend, review
  roles included, is refused (the pre-reserve behavior).
- `autonomous.backoff_reserve_tokens_per_hour` — a rolling one-hour token
  budget for reserve admissions on that backend. Default `0` (no token
  budget); enforced only when token usage is reported.

A live verifier or planner session on the backend already holds one of the
reserve's slots. A review request that cannot get a slot waits with
`pool llm:<backend> backpressure: back-off, review reserve in use` —
classified as a back-off wait, so it shows the flag's reset time — and starts
when the flag clears. Every admission and refusal is audited as
`autonomous_usage_backoff_reserve_admitted` /
`autonomous_usage_backoff_reserve_refused` (see [Audit trail](#audit-trail)).
Both keys are read live at decision time, so a change needs no restart, and
they are settable from every config surface (YAML, REST `PUT /api/config` with
the dotted keys, MCP `autonomous_config_set`, CLI
`datawatch config set autonomous.backoff_reserve_for_review=…`, comm
`configure autonomous.backoff_reserve_for_review=…`, and the two *Usage
back-off review reserve* fields in PWA Settings) — see [Usage
back-off](usage-backoff.md#what-a-flag-does-not-hold-the-review-reserve).

### Manual set, clear and status

The same four operations exist on every surface, with the same names and
arguments:

- REST: `GET /api/usage-backoff` (status), `POST /api/usage-backoff` with
  `{"backend": "claude-code", "minutes": 45, "reason": "weekly cap"}`
  (`until`, an RFC3339 time, replaces `minutes`), and
  `DELETE /api/usage-backoff` with `{"backend": "claude-code"}` (omit the
  backend, or pass `all`, to clear every flag).
- MCP: `usage_backoff_status`, `usage_backoff_set`, `usage_backoff_clear`.
- CLI: `datawatch usage-backoff status`,
  `datawatch usage-backoff set <backend> <minutes|until> [--reason text]`,
  `datawatch usage-backoff clear [backend|all]`.
- Comm channels: `usage_backoff status`,
  `usage_backoff set <backend> <minutes|until> [reason]`, `usage_backoff clear`.

A set and a clear can carry an optional `models` list (an array on MCP and
REST, `--models a,b` on the CLI, `models=a,b` on the comm channel): a set with
it holds only those models of the backend, and a clear with it removes only
those models from the flag. A clear-all is never model-scoped. See
[Usage back-off](usage-backoff.md) (`### Holding only certain models`).

Status lists each active flag (backend, until, reason, and whether it was
set by a rate limit or by an operator) together with the effective mode and
maximum wait. The capacity status (`datawatch capacity`, the PWA Capacity
card) shows the same flags as the wait reason on blocked tasks.

### Across a daemon restart

A flag is written to disk every time it is set, cleared or expires, in
`backoff.json` under the `autonomous` folder of the data directory (mode 0600),
and written once more when the daemon shuts down or restarts. When the daemon
starts it reads the file back and reinstalls every flag whose time has not
passed, before the first Automaton dispatch, so a restart no longer makes
work run straight into a limit that is still in force. Flags that expired
while the daemon was down are dropped. A missing file is normal; an
unreadable one is logged and the registry starts empty.

Each restored flag is recorded three ways so you can see why work is waiting:

- a boot-resume entry with outcome `backoff_restored` (read it as described in
  [Reading the recorded decision](restart-with-work-running.md#reading-the-recorded-decision));
- an alert titled **Back-off restored after restart** naming the backend and
  the time it lifts;
- an `autonomous_usage_backoff_restored` audit event (see
  [Audit trail](#audit-trail)).

New worker work for a restored backend waits in the capacity queue with the
usual `backoff: ...` wait reason and starts by itself when the flag clears.
A boot-resume decision for an Automaton on that backend carries a sentence
saying so. Review roles keep their [review reserve](#the-review-reserve)
path while the restored flag is up; the reserve's own state (the rolling
token stamps) is in memory and does not survive the restart. Sessions
already running on the backend are adopted as always; restoring a flag never
ends, restarts or signals one. Because the flag lives in the registry,
clearing it by hand (any surface above) also removes it from the file, so a
cleared flag does not come back on the next restart.

### Pattern staleness and manual recheck

Automatic back-off only works while `session.rate_limit_patterns` still
matches what each backend actually prints. Backends reword their limit
messages, so the status output carries `patterns_version` (a fingerprint of
the effective patterns), `patterns_stale`, which turns true whenever the
patterns change and have not been re-verified, and `claude_cli_version` —
the claude tool's own version text, which the status guard also
fingerprints. The daemon reads it once at daemon start by running the
tool's version command with a short timeout, and again on any status
request when the cached read is older than 10 minutes. If the tool is
absent or the command fails the value reports `unknown`, and that alone
never raises `patterns_stale`; a *known* version that changes does.

Re-verify whenever the backend or its CLI version changes, and whenever you
edit `session.rate_limit_patterns`: provoke or replay a fresh rate-limit
response, confirm a session reaches the rate-limited state, then record it
with `datawatch usage-backoff recheck` (REST `POST /api/usage-backoff/recheck`,
MCP `usage_backoff_recheck`, comm `usage_backoff recheck`). The recheck
stores both the pattern fingerprint and the current `claude_cli_version`
as rechecked, and clears `patterns_stale` until the patterns or the
version text change again. The same three fields (`patterns_version`,
`patterns_stale`, `claude_cli_version`) appear on the API status and
recheck responses, the capacity status, the autonomous status, the MCP
tools and the CLI status output.

**Screen canary for Claude Code.** The version text above also drives a second
check that needs no operator action. When the daemon starts, and again every
half hour, it reads the installed Claude Code version; if the text is new
(or none was recorded) it replays the real screen captures the daemon ships
(an idle box, a working agent, an open dialog, an unsent paste) through the
prompt-ready and task-delivery checks. If any capture no longer reads the way
it was recorded, the daemon raises **one** warning alert naming the version and
the captures, once per version change. The run only reads: it does not update
Claude Code and does not touch a session. The last checked version is kept in
the data directory (`claude_cli_canary_version`); there is no setting.

## Per-LLM dollar budget

The in-flight caps limit how many sessions run at once. A dollar budget
limits how much an LLM may spend inside a window. Give an LLM a ceiling and
a window and datawatch stops starting autonomous work on it once the window
is full, then resumes by itself when it rolls over. An LLM without a budget
is never limited by dollars.

### Set it

Choose one window style: a rolling window of the last N hours (1 to 720), or
a weekly window that resets on a weekday at a clock time in a zone.

```bash
# Rolling: $50 over any 168 hours
datawatch llm update claude-code --budget-ceiling-usd 50 --budget-hours 168

# Weekly: $80 per week, resetting Monday 09:00 New York time
datawatch llm update claude-code --budget-ceiling-usd 80 \
  --budget-weekday monday --budget-time 09:00 --budget-zone America/New_York

# Remove it
datawatch llm update claude-code --clear-budget
```

The same object is available everywhere: the PWA (Settings > Compute > LLM
edit), REST `PUT /api/llms/{name}` with a `budget` object, and MCP
`llm_add` / `llm_update` with `budget`. The object is replaced as a whole;
leave it out to keep the stored one, send `null` or `{}` to clear it.

### Read it

```bash
datawatch budget status              # every budgeted LLM
datawatch budget status --llm claude-code
```

REST `GET /api/budget` (or `?llm=<name>`), MCP `budget_status`, and the comm
command `budget [status] [<llm>]` return the same data; `GET /api/capacity`,
`GET /api/autonomous/status`, MCP `capacity_status` and the PWA Capacity
card carry the same `budgets` list. Each entry shows spend, ceiling, remaining, the window, when
it rolls over and whether it is full.

### The billing mode decides whether a budget is read

The budget reads the LLM entry's
[billing mode](llm-registry.md#set-the-billing-mode-of-an-llm):

- `api`: the budget compares **dollars**, as described above.
- `allowance`: the budget behaves as it always has.
- `local`: **no budget applies.** Local inference has no per-call charge, so a
  ceiling on a `local` entry is never read: admission never waits on it, the
  entry is left out of `datawatch budget status` and the `budgets` list of
  `GET /api/capacity`, and `GET /api/budget?llm=<name>` answers 404 for it
  (`no budget for llm <name>`). The stored ceiling is kept, so changing the entry
  back to `api` or `allowance` makes it count again.

### What "budget window full" means

When spend reaches the ceiling, any autonomous task that needs that LLM
shows `waiting_capacity` with a reason that starts `budget window full:` and
names the spend, the ceiling and the rollover time. It is a wait, not a
failure: no retry is used and nothing is cancelled. Your own interactive
sessions on that LLM still start, and other LLMs are unaffected.

### How it resumes

You do nothing. Admission re-checks on the wait loop's next poll. A weekly
window frees up at its next anchor; a rolling window frees up as old spend
ages out of the trailing hours. The next poll after that admits the task and
it starts. An audit event is recorded when the window fills and again when
it frees up.

The check is made when work is admitted. Work that was already running when
the ceiling was crossed finishes, so spend can end a little above the
ceiling. Leave some headroom.

### The estimate

Setting `allocation_pct` on the budget adds an estimated share-of-allocation
percentage to the status. It divides your spend by a dollar figure you
declare per plan tier in `autonomous.plan_tier_allocation_usd`, so it is
only as good as that figure. It is labelled estimated and never blocks
anything; only the ceiling does.

### Opt a peer in

If a second datawatch spends on the same account, add its name to the
budget's `peer_window` (`--budget-peer-window peerA`). Only the peers you
list are counted; the default is none. Each listed peer reports just its own
spend, so nothing is counted twice. A peer that cannot be reached is skipped
and the local spend alone decides, so a peer outage never stalls your work.
Status shows each peer, its spend and whether it is counted.

## Automation spending limit

A chain of Automata (an Automation) can spend a lot before you notice. Give
the Automation a spending limit and datawatch stops starting its members when
the limit is reached, tells you, and carries on by itself when it can. The
limit covers the whole chain, whichever LLMs it uses, and works next to the
per-LLM budget above: both apply and the tighter one decides. An Automation
without a limit is never stopped by dollars.

### Set it

```bash
# $40 per week, spread evenly, resetting Monday 09:00 New York time
datawatch automation set-budget <id> --ceiling-type dollar --ceiling-value 40 \
  --window week --pacing even \
  --reset-weekday monday --reset-time 09:00 --reset-zone America/New_York

# A tighter limit on one member, and what you expect it to cost
datawatch automation set-budget <id> --member <member-id> \
  --ceiling-type dollar --ceiling-value 10 --window automation
datawatch automation set-budget <id> --member <member-id> --estimated-cost-usd 6

# Remove it
datawatch automation set-budget <id> --clear
```

From a chat channel: `automation set-budget <id> ceiling_type=dollar
ceiling_value=40 window=week pacing=even`, or `automation set-budget <id>
clear=true`. Over REST it is `POST /api/automations/{id}/set_budget` and over
MCP `automation_set_budget`, with the same fields. Each call replaces the whole
limit. It does not start, pause or resume anything by itself.

The window is `day`, `week` or `month` (datawatch works out the reset itself,
by default at local midnight, on Monday, or on the first of the month) or
`automation` (the whole life of the Automation, which never resets). Set the
reset anchors if you want the window to line up with a provider's reset.

### How the spend is added up

The limit counts every member Automaton of the Automation: the sessions that
did the work, the planning sessions and the verification sessions, including
members of any nested Automation. The numbers come from the same spend ledger
as the per-LLM budget. A member's own limit counts only that member's spend.
Set `backend` to count only one LLM.

### Read it

```bash
datawatch automation get <id>
```

In chat, `automation status <id>`. The `budget` block shows spend against the
ceiling, the window, when it resets (or that the reset time is unknown), what a
breach will do, and any pacing hold. `datawatch autonomous status` and the
`automations` rows of `GET /api/autonomous/status` carry the same block with a
short figure such as `$12.40 of $40.00 (week)`.

### Pacing

`--pacing even` spreads the ceiling across the window. If spend is already ahead
of an even spread, the next member waits until the window has caught up; the
Automation stays active and checks again by itself. `--pacing none` (the default)
spends up to the ceiling at any speed. A member with a declared expected cost
also waits when starting it would take spend past the ceiling. Pacing does
nothing on the `automation` window, which never ends.

### When the ceiling is reached

The ceiling is checked before each member starts (on activation, on moving to
the next member, and before a whole fork) and before a member's task starts a
new session. Work that is already running is not interrupted, so spend can end
a little above the ceiling; leave some headroom.

- **Reset time known** (a `day`, `week` or `month` window, or a reset time read
  from a provider's rate-limit message): the Automation pauses and resumes by
  itself when the window resets. You do nothing. If the ceiling is still
  reached at that moment, it is blocked instead, so a pause never loops.
- **Reset time unknown** (the `automation` window), or `--on-breach block`: the
  Automation goes to blocked and waits for you. Raise the ceiling or wait for
  your own budget, then resume it. Resume is refused, with the reason, while
  the ceiling is still reached.
- **Either way** you get an alert once per breach, the breach is audited, and
  the `state` of the budget block says what it is doing and when it will
  resume.

### The estimate

Add `--allocation-estimate` for an estimated share-of-allocation percentage.
It uses a dollar figure you declare per plan tier in
`autonomous.plan_tier_allocation_usd`, so it is only as good as that figure.
It is labelled estimated, it is off by default, and it never pauses or blocks
anything. A limit of type `allocation_pct` only shows this estimate; only a
`dollar` ceiling stops work.

### Count another daemon's usage

If a second datawatch spends on the same account, name it with `--peer-window
peerA` (and give the limit a `--backend`, because a peer reports usage per
LLM). Only the peers you name are counted; the default is none. datawatch does
not confirm automatically that a named peer shares the account, because the
check that would do that does not exist yet, so the list you write is the
confirmation, and the status says so next to each counted peer. A peer that
cannot be reached counts as zero and never stalls your work.

## Rate ceilings

A pool can also carry a **rate ceiling**: a cap on how many requests or
tokens it may use in a rolling window, on top of the concurrency limit. It
is for plans that meter usage per hour. With no ceiling set, nothing
changes.

### Set it

Three `autonomous` keys control it. Each applies to every `llm:<name>` pool
and takes effect within about ten seconds, with no restart:

| Key | Meaning |
|---|---|
| `capacity_rate_requests` | lease grants allowed per window; `0` = no requests ceiling |
| `capacity_rate_tokens` | tokens allowed per window; `0` = no tokens ceiling |
| `capacity_rate_window_seconds` | window length; `0` = one hour |

```bash
# CLI: 60 requests and 2,000,000 tokens per 30 minutes
datawatch autonomous config-set --capacity-rate-requests 60 \
  --capacity-rate-tokens 2000000 --capacity-rate-window-seconds 1800

# Remove the ceilings
datawatch autonomous config-set --capacity-rate-requests 0 --capacity-rate-tokens 0

# REST
curl -sk -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"capacity_rate_requests":60}' https://localhost:8443/api/autonomous/config
```

The same keys are available from the MCP tool `autonomous_config_set`, the
chat command `configure autonomous.capacity_rate_requests=60` and the
Settings → Autonomous card. Negative values are refused.

### What a wait looks like

A task that hits the ceiling waits instead of failing, the same as any full
pool. The reason names the pool, the dimension, the usage and the reset time:

```
pool llm:ollama rate ceiling: requests 60/60 in 30m0s, resets at 2026-10-23T11:00:00Z
```

The readable form says `resets in 5m`. In `datawatch capacity` the pool
row shows the usage in the window, and the wait row shows the cause
`rate_ceiling` and the reset time. The task starts by itself when the
window frees enough usage; the reset time is when the oldest counted usage
leaves the window. If the daemon cannot tell when that is, no reset time is
shown.

### Would this start be admitted?

Ask before you start anything. The query takes no lease, adds nothing to
the queue and changes no state. The answer is **admit**, **queued** (with
your position, the reason and, for a rate ceiling, the reset time) or
**alternate_available** (another pool of the same kind has room).

```bash
# CLI
datawatch capacity admission --backend ollama --node gpu1 --model qwen3.8:27b
datawatch capacity admission --backend ollama --json

# REST
curl -sk -H "Authorization: Bearer $TOKEN" \
  'https://localhost:8443/api/capacity/admission?backend=ollama&role=worker&priority=5'

# MCP: capacity_admission with the same arguments (backend, node, model, role, priority, prd_id)

# Chat channels
capacity admission ollama gpu1 qwen3.8:27b
```

Text answers read `would be admitted now`, or `would be queued at position
2: <reason>; resets at <time>` followed by `alternate available: <pool>`
when there is one. All four surfaces give the same answer for the same
state.

## Why a task is waiting

A waiting task always says why. The text below is what the product shows,
word for word; only the names, numbers and times change. Each reason also
carries a cause label (shown as a small chip in the PWA) and, when the end
of the wait is known, the time it ends.

| Cause label | Exact text shown | Meaning |
|---|---|---|
| Pool full | `pool llm:claude-code full (2/2, held by t-3f9a1c, t-8b20de)` | A pool the task needs is at its limit. The holders are the sessions using the slots. With no holder to name, it reads `held by other sessions`. |
| Node busy | `waiting for node gpu-1: 1/1 engines in use, held by t-3f9a1c` | Every engine on the compute node is in use. |
| Back-off, reset time known | `backend claude-code backed off until 14:30 UTC (resets in 42m)` | The backend ran out of usage and told datawatch when it lifts. |
| Back-off, reset time unknown | `backend claude-code backed off until 15:12 UTC (reset time unknown; next attempt allowed then, in 1h)` | The backend gave no reset time, so datawatch holds it for the maximum back-off and then lets one attempt through. |
| Back-off, review reserve in use | `pool llm:claude-code backpressure: back-off, review reserve in use` | The backend is backed off and a review role (judge, verifier or planning session) asked for its review reserve: the live slot is held by another review session, the one-hour token budget is spent, or `autonomous.backoff_reserve_for_review` is `0`. New worker sessions on that backend wait on the back-off itself. |
| Last slot held for queued worker(s) | `pool llm:claude-code: last slot held for queued worker(s) t-3f9a1c` | A pool's last free slot is reserved for a worker queued on the same pool, so a verifier asking for that pool must let the worker go first. The verifier is admitted when the worker drains, or via its own `node-verify:<node>` pool or the review reserve. |
| Budget window full | `budget window full: claude-code spent $50.12 of $50.00 ceiling; window rolls over at 2026-10-26T09:00:00-04:00 (in 86h20m)` | The LLM's dollar window is full. With a peer's spend counted, the text splits it: `spent $52.40 (local $50.12, peers $2.28) of $50.00 ceiling`. If the window frees up only as spend ages out, it ends `window rolls over at when peer spend ages out`. |
| Global cooldown | `global cooldown until 14:30 UTC (in 12m)` | The daemon-wide rate-limit cooldown is active. When its end is not known the text is `global cooldown active`. |
| Awaiting external | `await_external: CI run on the branch` | The worker parked itself on an outside condition (see "Park on an external condition"). A bare `await_external` means it gave no condition. A long condition is cut at 400 characters. |
| Tree lock | `tree lock: tree:/home/<user>/project held by task t-7c1d44 (PRD 4e2a9b10) since 2026-10-22T18:10:00Z` | Another task holds the write lock on the same project tree. If the lock record cannot be read: `tree lock: <key> held by unknown holder (record unreadable)`. |

Two more reasons are not capacity at all, but they show up in the same
queue: a task whose dependencies are unfinished (`waiting for unfinished
dependencies: <task titles>`), and a task that is ready but is simply next
in line (`next in line to start`, or `in line to start, 2 ready task(s)
ahead in this Automaton`). A task in an approved or paused Automaton says
so: `its Automaton is approved but has not been started`, or `its Automaton
is paused, so no new tasks start`. When GPU backpressure holds a task, the
reason reads `pool <name> backpressure: <why>` and is labelled by the
underlying cause.

Times are shown with their zone (`14:30 UTC`), and "in 42m" or "in 2h05m"
counts down from the moment you read it. A wait that has no reason recorded
yet reads `waiting for a capacity slot`.

The reason is live, not a snapshot from when the task entered the queue: the ledger
recomputes it on every read and on every dispatch pass, so the queue always shows
why the task is waiting *now*. A temporary hold shows only while it lasts — a task
whose session survived a daemon restart briefly shows the restart adoption hold, and
as soon as the adoption finishes the queue shows the reason actually blocking it
(the node-engine reason, the same one the admission answer gives). And a task on a
backend that takes no node (claude-code) holds no node engine at all: if it moves to
such a backend, or is adopted on one after a restart, its old `node:<name>` lease is
released instead of keeping a node slot busy.

### Where the reason appears

The same text, from the same source, on every surface:

- **`autonomous_status`** (MCP; REST `GET /api/autonomous/status`): `queue`
  lists each queued or waiting task with `reason`, `cause`, `since`,
  `resets_at` (when known) and `holder`; `holdings` lists each running task
  with the pools, node and session it holds; `usage_backoff` and `budgets`
  carry the flags and the dollar windows.
- **`GET /api/capacity`** (MCP `capacity_status`): the same task views under
  `tasks.waiting` and `tasks.running`, and `tasks.verifying` for tasks in
  verification, next to `pools`, `leases`, `waiting` (the raw ledger queue),
  `backoff` (each flag with `until`, `reason` and `reset_known`) and
  `budgets`. `tasks.verifying` is always an array. Each entry has `state`:
  `verifying` when its verifier holds a slot, or `waiting_for_verifier` when
  it is queued for one, and then `wait_reason` says why. Add `?prd_id=<id>`
  to scope it to one Automaton.
- **Automata task detail (PWA):** a waiting task shows the cause chip, the
  reason text and, when known, `(Resets at <time>)`; a running task shows a
  `Holds:` line with its pools and node.
- **Status tab (PWA, session view):** the same wait row, labelled
  *Waiting on*, and the same `Holds:` line, for a session that belongs to an
  Automaton task.
- **Capacity card (PWA):** a *Verifying* list under the queue, one row per
  task in verification, marked `verifying` or `waiting for a verifier slot`.
  The Automata waiting for a worker slot stay in the queue list.
- **`datawatch capacity`** (add `--json` for the raw body): prints
  `Queued tasks (n):` with each reason and how long it has waited,
  `Running tasks (n):` with what each holds, `Verifying (n):` for tasks in
  verification (each marked `verifying`, or with its verifier-slot wait
  reason), `Back-off:` and `Budgets:`.
- **`capacity`** (chat command): the same text as the CLI.

A task parked on an external condition also appears under running tasks with
a note: either the inference slot is released and the tree lock is kept, or
both are released.

## Leases after a daemon restart

After a daemon restart, each resumed Automaton task is leased from the
backend its session actually runs, read from the session record, not from the
backend currently configured for the task. A task whose session is a pi
session takes a pi lease even if its configured backend has since changed to
claude-code, and the other way round.

If a lease the task already holds disagrees with its session's backend, the
lease is released and the task is leased again from the session's backend. An
operator alert reading `Resumed session leased from a different backend` is
raised for this, once per task and session, so a task that is reconciled again
does not repeat the alert.

When you see that alert:

1. Check which lane the task ran on: open the task and note the backend of
   its session, which is the lane that now holds its lease.
2. Check whether the configured backend for the task is what you intend. If
   the session's backend is the one you want, change the task's configuration
   to match so later retries land there. If the configured backend is the one
   you want, the task needs to run again on it instead of continuing on the
   resumed session.

## Orphan judge and verifier slots

A judge or verifier can end up holding a slot in the pi-prompt pool (`llm:pi-prompt`)
with no live session and no node lease. The lane then sits wasted while a node
is idle. The common leak is already closed at the source — when the daemon
frees a task's capacity, it releases the task's verifier lease along with it —
and whatever still slips through, the daemon frees on every capacity tick (the
same tick that reaps leaked leases):

- A `judge:` or `verify:` holder with **no live session and no node lease** is freed.
- A judge or verifier whose **Automaton is finished** (merged, cancelled or
  failed) and that has no live session is freed.
- A holder with a **live session is never freed**, whatever its lease state. The
  reconcile only releases ledger slots; it never ends or kills a session.
- A lease younger than the **slot reconcile grace** counts as having a node
  lease, so a holder that was just admitted is left alone. The grace is set by
  `autonomous.capacity_slot_reconcile_grace_seconds` (default 60; minimum 10;
  `0` reads the default). Values 1 to 9 and negative values are refused on every
  surface.
- Running it again frees nothing more.

**What you see.** One journal line per freed slot
(`[capacity] reaped leaked lease holder=... pool=...: <reason>`), one alert
titled `Capacity slot freed` whose body reads `Freed a pi-prompt worker
(llm:pi-prompt) slot held by <holder>: <reason>` with the holder in plain
words and the short id second, and one count on the reaped-leases metric. The
reason is `no live session and no node lease` or `Automaton finished`.

**Operator review.** The alert is informational. If it repeats for the same
Automaton, a judge or verifier is being started without a node lease; check the
node pools and the holder in [Readable holder names](#readable-holder-names).

**Setting.** `autonomous.capacity_slot_reconcile_grace_seconds` (default 60,
minimum 10, `0` = 60) is the only knob. It is read live, so no restart is
needed. Set it with the Web settings field *Slot reconcile grace*,
`datawatch config set autonomous.capacity_slot_reconcile_grace_seconds <n>`,
`PUT /api/config`, the comm `configure` verb or the MCP `autonomous_config_set`
tool. Raise it only if the alert fires for holders that were admitted just
before the check; a longer grace delays the release of a truly leaked slot by
the same amount. Nothing is turned on or off by it, and it never ends a
session.

### Confirming a freed slot

A judge or verifier can keep holding a pool slot after its session ended or
its Automaton merged. Every ten seconds the daemon checks the pi-prompt pool
and frees a `judge:` or `verify:` slot that is past the slot reconcile grace
and has no live session and no node lease, or whose Automaton is finished. Each freed
slot raises an alert that says which slot was freed and why. A slot whose
session is still running is never freed, and the reconcile never ends a
session.

To confirm:

1. Open the alert list and find the `Capacity slot freed` alert. Read the
   slot it names and the reason (`no live session and no node lease`, or
   `Automaton finished`).
2. Open the capacity view (`datawatch capacity`, or the Capacity and budget
   card) and check that the pool's held count dropped by one and that the
   holder is no longer listed.
3. If the holder is still listed, its session is still running, so it was
   correctly left alone.

## Readable holder names

You don't need to keep a task-id diary. Wherever the capacity surfaces used
to show a raw id — a pool's holders, a lease, a waiting row, a task line —
they now show you **who** holds it: the Automaton's title and the task's
title, with the familiar short id in a smaller secondary label right after,
so nothing is hidden and nothing is lost. What you see:

- **Capacity card pools.** A node pool's `Holders:` line lists each holder as
  *PRD title / task title (short id)* instead of a bare short id. A long
  title is cut in the middle ("Fix the login flow and…") and the full text is
  in the tooltip — the card never overflows. The same name shows in each
  pool's tooltip.
- **Waiting and running task lines.** A queued task reads as *PRD title /
  task title (short id)* first, then its reason; a running task shows its
  `Holds:` line with the same readable holders.
- **Wait reasons.** When a reason names holders — a full pool or a busy node
  — you see their titles, e.g. `pool llm:claude-code full (2/2, held by
  "Ship the beta" / "Write the migration" (t-3f9a1c))`. Reasons that name no
  holders (back-off, budget) read exactly as before. The session status tab
  shows the same readable sentence next to the raw one.
- **One that has no title.** A holder the daemon resolved but that has no
  PRD or task title is shown as *`(untitled t-3f9a1c)`* instead of an empty
  cell — and the raw id is still in the API body for scripts.
- **The API body is additive.** `holder_names` (pools), `holder_name`
  (leases and waiting), and `readable`/`short_id` (tasks) sit **beside** the
  unchanged `holder`, `prd_id`, `task_id` and `reason` fields; scripts that
  read the ids are unaffected. `full` on a name is the never-truncated
  sentence and only appears with `?long=1` (the PWA tooltip uses it). The
  readable text is HTML-escaped before it reaches the page.

## Capacity and budget card

The PWA shows one Capacity card, built from `GET /api/capacity` and refreshed
every five seconds. On the Automation sub-tab it is the machine-wide view; inside
an Automaton it is scoped to that Automaton's own backends (the `host` pool is
always included). The card is left out when there is nothing to show.

- **Pools.** One bar per pool that has a limit, as used / limit, green below
  75%, amber from 75% and red when full. Hover a pool for what it means; an
  LLM pool also shows its effective limit and where that came from
  (`explicit`, `plan_tier` or `kind`). Each pool lists its **holders** (short
  ids of the sessions using it) and, for a compute node, that tasks are
  waiting for the node. When a pool's holders span two or more nodes, the
  node name follows each holder's short id in a small muted label; a pool
  whose holders are all on one node is unchanged. A pool with no limit appears only while something
  holds it, marked *no limit*. A registered compute node with no cap and no
  activity is listed as *no cap configured*, so real hardware is not hidden.
- **Usage back-off.** One row per active flag: the backend, `Resets: <time>`
  when the reset time is known or *reset time unknown* when it is only the
  maximum back-off, the held and open models when the flag is limited to some
  models (see [Holding only certain models](usage-backoff.md#holding-only-certain-models)),
  and the reason underneath.
- **LLM budgets.** One block per LLM that has a budget: a spend bar
  (`$spend of $ceiling`, with peer spend added), a *budget window full* badge
  when it is full, when the window ends, and when it rolls over. The bar is
  green, amber from 75% and red when full.
- **Allocation share.** When a budget has `allocation_pct` set, the block
  adds `Allocation: n% (estimated)`. It is spend divided by the dollar figure
  you declared for the LLM's plan tier, so it is an estimate and never blocks
  anything; only the ceiling does.
- **Waiting.** The queue of capacity waits, each with the holder, the
  Automaton, how long it has waited and the reason.

## Compute node usage

Next to each node's held slots and limit, the card shows what the node is
doing right now, one row per compute node:

- **Model.** The models loaded on the node's model server: the name, how
  much memory they take (`5.0 GiB in memory`) and how long they are meant
  to stay (`expires in …`). A model that stays resident shows *stays
  loaded*, a node with none loaded shows *No model loaded*. Only `ollama`
  nodes report models (an OpenAI-compatible endpoint has no list of
  loaded models to show).
- **Slots.** What was already on the card: held slots of the configured
  limit for the node.
- **GPU.** For each GPU, the utilization percent and the memory used of
  the total (`GPU 12.5%`, `GPU memory 10.0 of 40.0 GiB`). These figures
  come from the node's observer: the node must have an observer peer set
  on it (Settings > Compute, the node's *observer peer* field) — datawatch
  does not guess which peer, if any, is the observer. A node without that
  binding shows *No GPU data from the observer*.

Two states besides the figures:

- ***Unreachable*** — the model server or observer answered neither within
  the short probe timeout (2 s); the reason is in the chip's tooltip. The
  node row still shows its slots, and GPU may still be available from the
  observer when only the model server is down.
- ***Checking...*** — the first fetch for the node is still in flight; the
  row shows the checking line and nothing fabricated.

How the figures get there:

- **Refreshed about every 15 seconds, from a cache.** The daemon caches
  each node's figures and serves them immediately, refreshing in the
  background at most once per node at a time. The capacity page never waits
  on a node: a slow or hung model server cannot hold up the card.
- **No history.** The row shows only the current figures; there is no
  history graph and no time series for a node.

The same figures are on every surface: `datawatch capacity` prints a
`model:` line and a `GPU` line under each node, the comm command
`capacity` replies with the same text, and the `capacity_status` tool
returns the body with each `node:<name>` pool carrying the `usage` object
(`state`, `checked_at`, `models`, `gpu`) of `GET /api/capacity`.

## Audit trail

Waits, admissions, back-off changes and budget transitions are written to the
audit log, so you can see afterwards what held a task up and for how long.
Each event is written once per change, never once per poll: a task that waits
on the same cause for an hour produces one wait event, and a second only if
the cause changes.

| Action | Actor | Subject | Values |
|---|---|---|---|
| `autonomous_capacity_wait` | `autonomous` | the task id | `prd_id`, `cause`, `reason` (the full wait text), `pools` (sorted) |
| `autonomous_capacity_admitted` | `autonomous` | the task id | `waited_seconds`, `pools`; for a fall-through also `fall_through`, `from_pools`, `to_pools`, `note` (see [Pool fall-through](#pool-fall-through)) |
| `autonomous_usage_backoff_set` | `autonomous` for a rate limit, `operator` (or the caller's own actor) for a flag set by hand | the backend | `backend`, `until` (RFC 3339), `source` (`rate_limit` or `operator`) |
| `autonomous_usage_backoff_cleared` | `operator` (or the caller's own actor) | the backend, or `all` | `backend` |
| `autonomous_usage_backoff_auto_cleared` | `autonomous` | the backend | `backend`, `until` (RFC 3339) |
| `autonomous_usage_backoff_restored` | `autonomous` | the backend | `backend`, `until` (RFC 3339), `source` (`rate_limit` or `operator`); written at daemon start for each flag read back from disk |
| `autonomous_usage_backoff_reserve_admitted` / `autonomous_usage_backoff_reserve_refused` | `autonomous` | the backend | one entry per review-reserve verdict (admission or refusal); `backend`, `role` (`verifier` or `planner`), `slots`, `used_slots`, `tokens_used`, `token_cap`, `at` (RFC 3339) |
| `autonomous_budget_ceiling_wait` | `autonomous` | the LLM | `llm`, `spend_usd`, `ceiling_usd`, `rolls_over_at` (RFC 3339, only when known) |
| `autonomous_budget_window_resumed` | `autonomous` | the LLM | `llm`, `spend_usd`, `ceiling_usd` |
| `autonomous_budget_window_rollover` | `autonomous` | the LLM | `previous_spend_usd`, `ceiling_usd`, `new_window_start` (RFC 3339) |

Notes on reading them:

- `spend_usd` includes the spend of any peers you opted in.
- `autonomous_budget_window_rollover` is recorded for weekly windows, whose
  boundary passes at a fixed time; a rolling window slides continuously and
  has no boundary. `previous_spend_usd` is the spend seen at the last check
  before the boundary.
- `autonomous_budget_window_resumed` with only `llm` means the LLM's budget
  was removed while its window was full.
- A manual back-off set or clear is attributed to whoever made the call:
  `operator`, or a more specific actor such as `session:<id>` or
  `peer:<name>` when the call came from a session or a federated peer.
- `autonomous_usage_backoff_patterns_rechecked` (details: `patterns_version`)
  is recorded when you record a pattern recheck; it belongs to the same
  family and shows in an `autonomous_usage_backoff_*` query.

### The same query on all four surfaces

Every surface takes the same seven filters: `actor`, `action`, `subject`,
`session_id`, `since`, `until` and `limit` (default 100). Results are
newest first. `since` and `until` must be RFC 3339 (for example
`2026-10-22T15:04:05Z`); anything else is rejected rather than ignored.
`subject` is an exact match on what the event is about: a task id, a backend
or an LLM name.

`action` is an exact match, or a **prefix match when it ends in `*`**:

- `action=autonomous_budget_*` lists the whole budget family (wait, resumed
  and rollover);
- `action=autonomous_usage_backoff_*` lists every back-off change;
- `action=autonomous_*` lists every capacity, back-off and budget event.

```bash
# REST
curl -sk "https://localhost:8443/api/audit?action=autonomous_budget_*&subject=claude-code&since=2026-10-20T00:00:00Z&limit=20"

# CLI (the session filter is --session-id)
datawatch audit --action 'autonomous_budget_*' --subject claude-code \
  --since 2026-10-20T00:00:00Z --limit 20
```

```text
# MCP: audit_query with the same arguments
{"action": "autonomous_budget_*", "subject": "claude-code", "since": "2026-10-20T00:00:00Z", "limit": 20}

# Chat channels: key=value words, no spaces around the =
audit action=autonomous_budget_* subject=claude-code since=2026-10-20T00:00:00Z limit=20
```

To follow one task, filter on its id: `datawatch audit --subject t-3f9a1c`
shows when it started waiting, why, and when it was admitted. To see who set
or cleared a back-off by hand: `datawatch audit --action
'autonomous_usage_backoff_*' --actor operator`.

## Who is admitted: worker, supervisor and operator starts

Every session start takes a lease on the capacity pools before it launches, and the daemon decides how by the way it launched the session. A field in the request never decides it: `one_shot` in a start request is accepted and ignored for admission.

| Class | Which starts | At a full pool |
|---|---|---|
| Worker | An Automaton's own launches: task sessions, decompose, verifier and judge sessions; and the restart of a worker session (rate-limit fallback, exit hook) | Waits in the queue |
| Supervisor | Supervisor loop jobs and schedules whose launch role is supervisor | Uses the buffer above the limit; never queued behind workers |
| Operator | Every other start: the app, the CLI, chat, MCP, the API, restarts, and any outside client | Admitted over the limit; the overage is recorded |

Workers carry a marker only the daemon holds (a per-process secret), so no client can claim the worker class. Every start path is admitted at the session manager, and the test `TestSessionStartPathsAreAudited` lists them: a new start path fails the test until it is on the audited list.

### Restarted workers

When a worker session is restarted (a rate-limit fallback to another profile, or an exit hook), the new session keeps the worker class and re-enters the queue at the head: it is granted before every other waiting worker, whatever their priority, so interrupted work resumes first. It still respects the cap: with a full pool it waits for a slot, for as long as any worker waits (`autonomous.capacity_wait_timeout_seconds`, default 4 hours). It does not take the operator class and gets no supervisor buffer. A verifier still ranks ahead of it, and a supervisor request is granted before it. The restart of a session that is not a worker's stays an operator start.

### Operator overage

An operator start is never refused. When `llm:claude-code` is full, the session still starts, and its lease records the overage: the pool, who, how far over. The overage counts in the pool's used figure and shows as `N over the limit: operator` on the pool line of `datawatch capacity`, in the chat `capacity` verb, in the Capacity card of the web app, and as `over` on the pool and `class` and `overage` on the lease in `GET /api/capacity`. Autonomous work waits until the pool is back under its limit. Turn on `autonomous.capacity_operator_overage_alert` to get an alert each time. A compute node's engine slots are soft in the same way: an operator start over the slots of a node is admitted and the overage is recorded (pool `node:<name>`, its limit, how far over). Set `hard_limit` on a node whose engine cannot take more; see [Soft node slots and hard_limit](#soft-node-slots-and-hard_limit).

### The operator allowance above the session cap

The host session cap (`session.max_sessions`) is a hard stop for workers. An operator start gets an allowance above it: `autonomous.capacity_operator_reserve_host` (default 5 slots; `0` = operators stop at the cap too). When an operator start finds the cap reached it still starts, into the allowance: an alert says so, and the session records how far over the cap it is (`over_session_cap` on the session; `session_cap` with `max`, `active`, `operator_slots`, `supervisor_slots` and `over` on `GET /api/capacity`; a `Host session cap: ... over` line in `datawatch capacity`). When the allowance is used up the start is refused with `max sessions (N) reached and the operator allowance of M above it is used up; raise autonomous.capacity_operator_reserve_host to allow more operator sessions`. A worker never uses the allowance, and supervisors keep their own host slots (below). The cap value itself is not changed by this setting.

### The supervisor buffer

Supervisor sessions use slots above a pool's limit that workers cannot take: `autonomous.capacity_supervisor_reserve_claude` (default 1), `autonomous.capacity_supervisor_reserve_pi` (default 1) and `autonomous.capacity_supervisor_reserve_host` (default 4, above `session.max_sessions`). They are counted in the pool's used figure, and a usage back-off or burn hold does not block them. A pool line shows `+1 reserved for supervisors`.

### The supervisor credential

An external supervisor (the detector timer) reaches the daemon over REST, which makes its starts operator starts. To give it the supervisor class without trusting a request field, mint a credential as the operator:

```
datawatch capacity supervisor-credential mint --name detector   # token shown once
datawatch capacity supervisor-credential list                   # names only
datawatch capacity supervisor-credential revoke --name detector # stops working at once
```

Send the token as the bearer token of the `POST /api/sessions/start` call only (it carries just `sessions:write` and `capacity:supervisor-start`); keep using the normal token for everything else. A start with it is supervisor class: it may use the reserved supervisor buffer on each pool and never goes past it (with the buffer in use it waits for its wait time and is then refused with 503, and starts nothing), and each one is audited as `supervisor_credential_start` with the credential's name as actor. A session credential or a federation peer cannot mint, list or revoke it, and a session token that only lists the capability is an operator start. `autonomous.capacity_supervisor_credential_enabled` false makes every credential do nothing. It is a stopgap that is removed together with the capability when the daemon runs the detector natively. The credential is managed on the CLI and REST only: the token is a secret that must not pass through chat, MCP or the web app.

### The Claude account pool

Every claude-code entry also draws on the pool `account:claude`, limited by `autonomous.capacity_account_claude_limit` (default 2: claude-code 1 plus claude-lean 1; `0` = unlimited), so the per-entry limits cannot add up past the account. A subscription backend a start names that matches no LLM entry and no compute node is listed under `unmapped` on `GET /api/capacity` (and as `No LLM entry for backend ...` in `datawatch capacity`) and held to one session until an entry is added.

**Federation.** Each host has its own local caps. A peer's local model pools (`llm:<entry>` and `node:<name>` on that peer) are its own: they add to the federated capacity, and one host's cap never limits another host's local models. `GET /api/capacity`, `datawatch capacity`, the chat `capacity` verb and the web Capacity card show a **cluster view**: this host first, then every peer that is opted in for fingerprint sync, named in a `peer_window` or matched, each at the pools and limits that peer reports, with each host's llm and node sums and the cluster total. Nothing assumes this host's sizes. The Claude account is the one pool that can span hosts. A peer's leases count against this daemon's `account:claude` limit only when (1) a **confirmed** `billing_group` match says its LLM entry bills to the same account as a local entry (the peer's own leases on that matched entry count, see [federation-fingerprint.md](federation-fingerprint.md)), or (2) you named the peer in the `peer_window` of a claude-code entry's budget (the peer's whole `account:claude` pool counts; a peer that is both is counted once). A **pending** match counts nothing and is listed as `possible shared account, awaiting your confirmation`; a rejected one counts nothing; a peer with a separate Claude account adds its own limit and counts nothing here. Peers are read on the ten-second tick, an unreachable peer counts 0 and shows as not reachable, and a peer's own external figure is never counted, so two peers that name each other do not double count.

Ask the daemon what a start would do with `datawatch capacity admission --backend claude-code --role interactive` (also `--role supervisor`); it changes nothing.

### After a restart

If the work re-adopted at a restart leaves a pool above its limit, the daemon raises one alert per pool. The sessions keep running; new work waits until the pool is back under its limit. A cancelled task never takes a lease, runs no placement and loads no model on a node.

The capacity read tells you which workers are over the limit and why. Each adopted worker lease that sits in a pool or node holding more than its limit carries `over_limit: true`, `over_limit_code: adopted_over_limit` and an `over_limit_reason` that names the pool or node, what it holds and its limit (`GET /api/capacity`, the `capacity` MCP tool, `datawatch capacity`). The text view adds a `Worker <id> over the limit: ...` line for each, and the web Capacity card marks the holder **over the limit** with the reason as a tooltip. A worker admitted through the queue is never flagged, and an operator session that was started over a limit keeps its own overage line instead. The flag clears when the pool or node is back at its limit.

## Common pitfalls

- **Everything queues behind one big model.** That is the node limit
  doing its job. Raise `max_concurrent_sessions` if the server handles
  more than one at a time.
- **A node has one slot although its GPU has room.** A node with one GPU
  always takes one slot, and so does a node whose observer reports no GPU or
  system memory data and gives no model size, which falls back to
  `autonomous.max_engines_per_node`. A set `session_slots` replaces the derived
  count, and `max_concurrent_sessions` still caps it. See
  [Compute-node pools and slots](#compute-node-pools-and-slots).
- **A worker starts on a node you did not expect.** The ledger picks the node
  with the highest `scheduling_priority` that has a free slot. Check the
  priorities in `datawatch compute node get <name>` and the holders in
  `datawatch capacity`. A pi worker that reaches the wrong host is covered in
  [LLM backends](../llm-backends.md#which-node-a-pi-worker-uses).
- **Another program on the GPU lowers the slots.** Derived slots count
  free memory, so memory held by another program reduces them. The count
  never drops below the workers already running on the node, but that
  floor is capped at the GPU count: a node with more workers than GPUs
  reads a limit below its held count, and new work waits until the held
  count falls below it. Running workers are not stopped.
- **Tasks wait although nothing seems to run.** Your own interactive
  sessions count against the `host` pool. Check `datawatch capacity` for
  the holders, or lower `session.reserved_interactive`.
- **A retry on a chosen backend never moves.** With
  `capacity_fallthrough` off (the default) it waits for that backend's
  pool. Turn the setting on if you would rather it run on another idle
  allowed pool.
- **Wait timeout errors.** Raise `capacity_wait_timeout_seconds`, add
  capacity, or lower the number of Automata running at once.
- **Turning it off.** Set `autonomous.capacity_enabled: false`; tasks
  then start immediately, and a full `session.max_sessions` still makes
  them wait and retry rather than fail.

## Diagram

See [`../flow/automata-capacity-flow.md`](../flow/automata-capacity-flow.md).

## See also

- [Automata planning](autonomous-planning.md)
- [Usage calculator](usage-calculator.md): how many lanes the weekly allowance can carry
- [Automata DAG orchestrator](automata-orchestrator.md)
