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
