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
- **MCP:** `compute_node_update` (`session_slots`, `max_concurrent_sessions`),
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

**Which node is picked.** The ledger takes the node with the highest
scheduling priority that has a free slot. A node's priority is its
`scheduling_priority` (0 to 100, default 50). Nodes of equal priority are tried
in the order the list gives them. A node with no free slot is passed over, and
the next one is tried.

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
on a node in the same way. A verifier keeps the node its task's worker holds
when it can run on the same pools. Otherwise the worker's slot is released and
the verifier acquires its own. A guardrail judge on an entry with one node
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

## Reactive usage backoff

When a backend runs out of usage (a rate limit or a weekly cap), starting
more autonomous work on it only burns retries. A usage back-off flag holds
autonomous dispatch to that one backend until the limit lifts. The flag is
in memory and per backend: it is lost on a daemon restart, and backing off
`claude-code` never affects `pi` or `ollama`. The implementation lives in
`internal/autonomous/backoff.go`; the design is in
`docs/plans/2026-10-14-concurrency-and-budget-design.md`.

The flag only pauses autonomous dispatch. It never blocks an interactive
session start, so you can still open a session on a backed-off backend by
hand. While a flag is active, tasks and decompose runs that would use that
backend wait with a visible wait reason (like any capacity wait) instead of
failing or burning their retry budget, and they resume when it clears.

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
  `usage_backoff set <backend> <minutes> [reason]`, `usage_backoff clear`.

Status lists each active flag (backend, until, reason, and whether it was
set by a rate limit or by an operator) together with the effective mode and
maximum wait. The capacity status (`datawatch capacity`, the PWA Capacity
card) shows the same flags as the wait reason on blocked tasks.

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
| Budget window full | `budget window full: claude-code spent $50.12 of $50.00 ceiling; window rolls over at 2026-10-26T09:00:00-04:00 (in 86h20m)` | The LLM's dollar window is full. With a peer's spend counted, the text splits it: `spent $52.40 (local $50.12, peers $2.28) of $50.00 ceiling`. If the window frees up only as spend ages out, it ends `window rolls over at when peer spend ages out`. |
| Global cooldown | `global cooldown until 14:30 UTC (in 12m)` | The daemon-wide rate-limit cooldown is active. When its end is not known the text is `global cooldown active`. |
| Awaiting external | `await_external: CI run on the branch` | The worker parked itself on an outside condition (see "Park on an external condition"). A bare `await_external` means it gave no condition. A long condition is cut at 400 characters. |
| Tree lock | `tree lock: tree:/home/me/project held by task t-7c1d44 (PRD 4e2a9b10) since 2026-10-22T18:10:00Z` | Another task holds the write lock on the same project tree. If the lock record cannot be read: `tree lock: <key> held by unknown holder (record unreadable)`. |

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

### Where the reason appears

The same text, from the same source, on every surface:

- **`autonomous_status`** (MCP; REST `GET /api/autonomous/status`): `queue`
  lists each queued or waiting task with `reason`, `cause`, `since`,
  `resets_at` (when known) and `holder`; `holdings` lists each running task
  with the pools, node and session it holds; `usage_backoff` and `budgets`
  carry the flags and the dollar windows.
- **`GET /api/capacity`** (MCP `capacity_status`): the same task views under
  `tasks.waiting` and `tasks.running`, next to `pools`, `leases`, `waiting`
  (the raw ledger queue), `backoff` (each flag with `until`, `reason` and
  `reset_known`) and `budgets`. Add `?prd_id=<id>` to scope it to one
  Automaton.
- **Automata task detail (PWA):** a waiting task shows the cause chip, the
  reason text and, when known, `(Resets at <time>)`; a running task shows a
  `Holds:` line with its pools and node.
- **Status tab (PWA, session view):** the same wait row, labelled
  *Waiting on*, and the same `Holds:` line, for a session that belongs to an
  Automaton task.
- **`datawatch capacity`** (add `--json` for the raw body): prints
  `Queued tasks (n):` with each reason and how long it has waited,
  `Running tasks (n):` with what each holds, `Back-off:` and `Budgets:`.
- **`capacity`** (chat command): the same text as the CLI.

A task parked on an external condition also appears under running tasks with
a note: either the inference slot is released and the tree lock is kept, or
both are released.

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
  maximum back-off, and the reason underneath.
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
| `autonomous_capacity_admitted` | `autonomous` | the task id | `waited_seconds`, `pools` |
| `autonomous_usage_backoff_set` | `autonomous` for a rate limit, `operator` (or the caller's own actor) for a flag set by hand | the backend | `backend`, `until` (RFC 3339), `source` (`rate_limit` or `operator`) |
| `autonomous_usage_backoff_cleared` | `operator` (or the caller's own actor) | the backend, or `all` | `backend` |
| `autonomous_usage_backoff_auto_cleared` | `autonomous` | the backend | `backend`, `until` (RFC 3339) |
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
