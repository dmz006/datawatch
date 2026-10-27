---
docs:
  index: true
  topics: [judge, judges, size, threshold, changed lines, guardrail, plan review, verifier, routing, routing policy, time limit, timeout, needs review, hold, alerts, capacity, lease, automaton, autonomous]
exec_steps:
  - tool: autonomous_config_get
    description: Read the judge settings (judge_policy, judge_timeout_seconds, judge_size_threshold_lines) and the verifier routing policy the judges follow when judge_policy is empty
    args: {}
    read_only: true
---

# How-to: Judge routing, time limit and the review hold

An autonomous **judge** is an LLM call that rules on work the daemon could not
pass on its own: the Automaton's rules-review guardrail, a task or story
guardrail, the pre-approval plan review, and any other autonomous judge. A judge
that never finishes must not pass the work, and it must not block the work
without anyone knowing. This guide covers how a judge picks its backend, how
long it may run, what happens when it runs out of time, the alerts it raises,
and how you resolve a hold.

Judges and verifiers are different roles. A **verifier** checks one task's work
after it runs. A **judge** rules on the Automaton's work at the guardrail and
plan-review points. Both use the same routing policy and the same capacity
leases, but each has its own settings.

| | Verifier | Judge |
|---|---|---|
| Asked | After each task runs | At guardrail and plan-review points |
| Backend list | `autonomous.verifier_policy.backends` (then the verification backend pool) | `autonomous.judge_policy`; empty means the verifier policy's list |
| Mode, Claude cap, capacity lease | The verifier routing policy | The same policy, read as the judge role |
| Time limit | `autonomous.verification_timeout_seconds` | `autonomous.judge_timeout_seconds` (default 30 minutes) |

For the verifier side of the routing policy (modes, Claude session cap, the
stronger verifier), see [Verifier policy](../api/autonomous.md#verifier-policy-set_task_verifier_policy--set_verifier_policy).

## Base requirements

- At least one LLM backend configured and enabled (see [LLM registry](llm-registry.md)).
- Capacity leases on (the default). Without a capacity ledger the judge runs
  with no lease and no slot wait, so the modes below choose only by order.
- A running daemon with the autonomous subsystem enabled.

## The judge role

A judge is a distinct role in the routing policy. It is not a copy of the
verifier's settings, and its picks are kept apart from the verifier's in the
decision log and in the Claude session count.

The judge's backend list is chosen in this order:

1. **`autonomous.judge_policy`** when it is not empty. The list is ordered; the
   judge tries the first backend and moves down the list only when an attempt
   fails the way described below.
2. **The verifier routing policy's own backend list** (`autonomous.verifier_policy.backends`)
   when `judge_policy` is empty. This is the default: the judges follow the
   verifier policy, and any later change to the verifier policy reaches the
   judges without another edit.
3. **The verification backend pool** (`autonomous.verification_backends`), and
   then the single configured verification backend, when the verifier policy
   names no backends.

Everything else about the routing policy, the mode, the Claude session cap and
its window, comes from the verifier policy in every case. The judge's list
replaces only the list of backends.

Two exceptions:

- **A unit that pins its own guardrail backend** keeps that backend as the only
  entry. The selector still chooses it, waits for its slot and applies the time
  limit, but the judge never moves to a different backend.
- **Plan review with a pinned plan-review backend** (`autonomous.plan_review_backend`)
  takes that backend as its only entry, in the same way.

When the judge list is empty, the verifier policy is `fixed`, and that policy
names no backends and sets no Claude session cap, the judge has nothing to
choose from. It then uses the unit's own guardrail backend, and only on its
first attempt.

## The three modes for judges

A judge uses the verifier policy's mode. The selector runs the same rules for
judges and verifiers, and the two presets (`gpu-only`, `claude-only`) filter the
judge list the same way.

| Mode | What the judge does |
|---|---|
| `fixed` | Takes the first backend in the judge list that is eligible. If it times out, the next one is taken. |
| `prefer-free` | Takes the first backend that has a free slot right now. When none has one, it takes the first eligible backend and waits for that backend's slot. |
| `balance` | Takes the admitting backend with the lowest relative load, read from the capacity ledger. Ties go to list order. When none admits, it chooses among all eligible backends. |

A backend that is held (a usage back-off on a Claude backend holds every Claude
backend, since they share one limit) or that has reached the Claude session cap
is not eligible, whatever the mode.

Every pick is written to the Automaton's decision log with the judge role, the
backend, the model and the reason, and to the audit log. Look for a
`judge_policy` decision whose note starts with `role=judge`. The reason reads
like `prefer-free: claude-code has a free slot` or `prefer-free: all full, waiting on ollama-a`.

## Running a judge on a free Claude lane when local slots are full

Put a Claude backend after your local backends in the judge list, and use
`prefer-free`. When the local backends have no free slot, the judge is probed on
the Claude backend and runs there if it has a free slot.

```yaml
autonomous:
  verifier_policy:
    mode: prefer-free
    backends: [ollama-a, ollama-b, claude-code]
    claude_session_cap: 4
    claude_cap_window_minutes: 60
  judge_policy: [ollama-a, ollama-b, claude-code]
  judge_timeout_seconds: 1800
```

How it plays out:

1. The judge checks `ollama-a` and `ollama-b`. Both are full, so neither is
   chosen.
2. The judge checks `claude-code`. It has a free slot and the Claude session
   cap for the window is not reached, so the judge runs there. The decision log
   says `prefer-free: claude-code has a free slot`.
3. If the Claude backend is also full, the judge waits on the first eligible
   backend, `ollama-a`, for its slot. Its wait is the capacity wait
   (`autonomous.capacity_wait_timeout_seconds`), and the time limit does not
   start until a slot is granted.

Points to keep in mind:

- The Claude session cap counts judge sessions on Claude backends in their own
  window, separate from the verifier's count. A judge that takes a Claude lane
  uses one count toward the cap, and only once its lease is granted.
- A Claude backend is recognised by its name, which starts with `claude`.
- The cap and the usage back-off apply to judges as they do to verifiers. The
  stronger-verifier fields (`force_stronger_on_retry`, `final_gate_stronger`)
  do not apply to judges, because a judge is never a retry or the final gate.

## Capacity leases

A judge takes a capacity lease the way a verifier session does, under the holder
`judge:<unit id>`. The lease is taken before the judge runs and released when the
judge ends, on success, failure, cancellation and panic alike. The judge uses the
same capacity ledger and lease registry type as verifier sessions, with its own
holder name, so the capacity card shows it like any other holder.

- **The wait for a slot is not part of the time limit.** A judge waiting for a
  slot can wait up to the capacity wait. The time limit starts once the slot is
  granted.
- **No slot within the wait** means the judge never ran on that backend. The
  judge moves to the next backend with no warning. The attempt is recorded as
  "did not run" (see [The attempt summary](#the-attempt-summary)).
- **Cancelling a judge that is waiting for a slot** stops the judge at once. It
  is not a slot shortage, so no further backend is tried and nothing is held.

## The time limit

`autonomous.judge_timeout_seconds` is the time limit for one judge run.

- **Default: 1800 seconds (30 minutes).** A value of `0` or a negative value
  means the default.
- **It counts the judge run only.** The run is one judge session (the
  one-shot guardrail session) or one ask call, measured from its start. The wait
  for a capacity slot is not counted.
- **It applies to each attempt.** Each backend in the list gets its own run and
  its own limit, so the total time on a judge with three backends can be three
  limits plus the waits.
- **A cancelled judge is not a timeout.** Only the judge's own limit counts as
  a timeout.

When an attempt reaches the limit, the judge is recorded as timed out on that
backend, a warning is raised, and the judge moves to the next backend.

## One attempt per backend

A judge call tries each backend in the judge policy at most once.

- Each backend that times out is recorded against the Automaton, so a daemon
  restart in the middle of a judge does not try that backend again.
- A backend that gets no capacity slot is also passed over, so it is tried at
  most once too.
- When every backend has timed out, the attempts equal the length of the judge
  policy list. The total never goes above that length.

Example: a judge policy of `[ollama-a, ollama-b, claude-code]`, with a 30-minute
limit. If `ollama-a` times out, a warning names it and the judge moves to
`ollama-b`. If `ollama-b` also times out, a second warning names it and the
judge moves to `claude-code`. If `claude-code` times out too, there is no backend
left, the Automaton is held (see below) and an error is raised. Three attempts,
three limits, and nothing else.

## Size-aware routing

A large change is slow for a small local model to judge. On 2026-10-26 an
Automaton of about 1,000 changed lines timed out on the local judge, and the
whole 30-minute limit was spent on a model that could not finish. Size-aware
routing sends a large change to `claude-code` first so the limit is not spent
on a backend that cannot finish.

`autonomous.judge_size_threshold_lines` is the number of changed lines above
which the rule applies.

- **What is counted.** The lines added plus the lines deleted on the Automaton
  branch, measured against its merge base with the base branch. Lock files
  (`go.sum`, `package-lock.json`, `yarn.lock`, `pnpm-lock.yaml`, `Cargo.lock`
  and the other package-manager lock files) are left out. Only committed work
  counts, and a binary file counts as 0.
- **Default 400.** A change of 400 changed lines or fewer keeps the configured
  order. A change of 401 or more is a large change.
- **`0` turns it off.** Nothing is measured and the judge backends are tried in
  their usual order. A negative value is refused.
- **Above the threshold, `claude-code` is tried first.** It moves to the front of
  the judge's backend list, and the other backends follow in their usual order.
  It is still one attempt per backend (see [One attempt per backend](#one-attempt-per-backend)),
  so the number of attempts does not change. If `claude-code` is not in the
  list, the order is unchanged.
- **Balance mode still picks by load.** In `balance` mode the load decides which
  backend runs, and the size rule only breaks ties between equally loaded
  backends. The decision log says so.
- **A judge that times out on every backend still holds the Automaton.** The
  `judge_timeout_seconds` limit, the warnings and the `needs_review` hold with
  the error alert work as described in [The needs-review hold](#the-needs-review-hold).
  The hold reason is `judge_timeout_hold`. The size rule changes the order only.
- **A unit that pins its own guardrail backend is not reordered.** It keeps that
  one backend.
- **If the lines cannot be counted** (for example the Automaton has no branch
  yet), the judge keeps the usual order and the decision log says the count is
  unknown. The judge is never blocked by a failed count.

The Automaton decision log shows which rule routed the judge and the line count,
at the start of each `judge_policy` decision:

```
size rule: 1012 changed lines is above the threshold of 400, claude-code tried first; <the usual reason>
default order: 212 changed lines is at or below the threshold of 400; <the usual reason>
default order: size routing is off (threshold 0); <the usual reason>
default order: 1012 changed lines is above the threshold of 400 but claude-code is not in the chain; <the usual reason>
```

Set it with any of the five methods:

| Method | Where | How |
|---|---|---|
| YAML | `autonomous.judge_size_threshold_lines` in `config.yaml` | `autonomous:` then `judge_size_threshold_lines: 400`, and restart the daemon |
| CLI | `datawatch autonomous config-set` with the flag `--judge-size-threshold-lines` (the CLI has no `judge-policy-set` subcommand; that verb belongs to the comm channel) | `datawatch autonomous config-set --judge-size-threshold-lines 400` |
| Web UI | Settings > Automata > Autonomous Automata planning > Judge size threshold | Enter the number of lines (0 = off) and save |
| REST API | `PUT /api/config` | Send the key `autonomous.judge_size_threshold_lines`, for example `{"autonomous.judge_size_threshold_lines": 400}` |
| Comm channel | `autonomous judge-policy-set` | `autonomous judge-policy-set judge_size_threshold_lines=400` |

A change made through the CLI, REST API, comm channel or Web UI applies to the
next judge call, with no restart. A YAML edit takes effect on restart.

## Alerts

Judge alerts go to the daemon's existing alert channels, the same ones other
Automaton alerts use. Each alert names the Automaton by its id (the id the API
also calls the PRD id), the backends tried so far and the time spent.

| Level | When | Title | Body (the wording of each part) |
|---|---|---|---|
| **warning** | Each time an attempt hits the time limit | `judge timed out for Automaton <id>` | `judge for Automaton <id> timed out on backend <backend> after <time spent> (limit <limit>); backends tried so far: <list>, <total time> in total; retrying on backend <next>, the next one in the judge policy` |
| **warning** (last backend) | The attempt that timed out was the last one | same as above | same as above, with `no backend is left in the judge policy, so the Automaton needs review` in place of the retry clause |
| **error** | The Automaton is held, because every backend timed out | `Automaton <id> needs review: the judge timed out on every backend tried` | `Automaton <id> needs review: the judge timed out on every backend tried (<list>), <total time> in total. Nothing was approved or passed. Attempts: <attempt lines, joined with semicolons>` |

The alert joins the attempt lines with semicolons. The hold verdict keeps them
one per line, as in the summary below.

Two cases use different wording:

- **Some backends timed out and others got no slot.** The error title reads
  `Automaton <id> needs review: the judge did not finish on any backend tried: timed out or no capacity slot`.
  A backend that got no slot is listed as "no capacity slot within the wait, the
  judge did not run".
- **No attempt timed out, and some backends got no slot.** No alert is raised
  and no hold is set. The judge never ran, so it returns an infrastructure
  verdict, and the usual infrastructure retry runs the judge again.

Nothing is silent. A timeout is always a warning, and a hold is always an error.

## The needs-review hold

When the last backend times out, the judge does not pass and does not block
silently. The Automaton is held.

What you see:

- **The Automaton is in `needs_review`**, the status it has while it waits for
  your decision.
- **The verdict is a block with category `judge-timeout`** and severity `high`.
  Its summary names the Automaton, the reason, the backends and the total time.
  Its `issues` list has one line per attempt, and its `judge_attempts` field
  holds the same summary as structured records (backend, started, duration,
  outcome).
- **Nothing starts it on its own.** Lane fill does not start a held Automaton.
  Only your decision moves it on.
- **Plan review** that ends the same way records a held plan-review verdict,
  with one finding per attempt. The Automaton stays in `needs_review`. In
  `block` mode, approval is refused until the review is re-run. In `warn` mode the
  held verdict is shown, and approval stays your decision.

### The attempt summary

Every attempt is listed, in the order tried:

```
backend ollama-a, started 2026-10-26T09:14:02Z: timed out after 30m0s (limit 30m0s)
backend ollama-b, started 2026-10-26T09:44:03Z: timed out after 30m0s (limit 30m0s)
backend claude-code, started 2026-10-26T10:14:04Z: no capacity slot within the wait, the judge did not run
```

Read the summary in any of these places:

- `datawatch autonomous prd-get <id>`, or `GET /api/autonomous/prds/{id}`: the
  verdicts, with the hold verdict's `issues` and `judge_attempts`. For plan
  review, the `plan_review` object.
- MCP `autonomous_prd_get` with the same id.
- The Automaton's decision log: one `judge_policy` decision per backend pick.
- The alert itself, which carries the same attempt lines.

### How to resolve a hold

The hold never clears itself, and the operator decides the next step. First
fix the cause, then pick the route that matches the hold.

1. **Find the cause.** Read the attempt summary. A judge that timed out on
   every backend usually means the backends are slow, busy or unreachable. A
   judge that did not run on a backend means that backend had no free slot.
2. **Fix the settings or the capacity.** Options:
   - Raise `autonomous.judge_timeout_seconds` if the backends are slow but
     working.
   - Add a backend that is not busy to `autonomous.judge_policy`, for example a
     Claude backend after your local backends, or a second local backend.
   - Free a slot, or wait for one, if the backends are only full.
   - Check that a Claude backend is not held and is under its session cap.
3. **Re-run the judgment that was held.**
   - **Plan-review hold:** re-run the plan review. The re-run replaces the held
     verdict. A passing verdict clears the `block`-mode refusal, and you can
     approve.
     - REST: `POST /api/autonomous/prds/{id}/plan-review`
     - MCP: `autonomous_prd_plan_review`
     - CLI: `datawatch autonomous prd-plan-review <id>`
     - Comm channel: `autonomous plan-review <id>`
   - **Guardrail hold on an Automaton or on one of its tasks or stories:** the
     Automaton is in `needs_review`, and the rejudge action does not apply to
     it, because rejudge only runs on a blocked Automaton. The reset action
     `reset_task` does not accept a `needs_review` Automaton either, so cancel
     the Automaton first. Then reset the held task with `reset_task` (use
     `force` when the task shows as completed) and run the Automaton again, so
     the work reaches its guardrail check again under the settings you now
     have. The steps are in [Recover a stuck, failed, or incorrectly-terminal Automaton](autonomous-recovery.md).
4. **Or decide the work is not wanted.** Reject it, or cancel the Automaton.

## Setting the judge policy on each surface

| Surface | Read | Write |
|---|---|---|
| YAML (`~/.datawatch/config.yaml`) | `autonomous.judge_policy`, `autonomous.judge_timeout_seconds`, `autonomous.judge_size_threshold_lines` | Edit the keys and restart the daemon |
| REST | `GET /api/autonomous/config` and `GET /api/config` | `PUT /api/autonomous/config` with `{"judge_policy": ["a","b"], "judge_timeout_seconds": 1800, "judge_size_threshold_lines": 400}`; or `PUT /api/config` with the dotted keys |
| MCP | `autonomous_config_get` | `autonomous_config_set` with `judge_policy` (array), `judge_timeout_seconds` (number) and `judge_size_threshold_lines` (number) |
| CLI | `datawatch autonomous config-get` | `datawatch autonomous config-set --judge-policy ollama-a,claude-code --judge-timeout-seconds 1800 --judge-size-threshold-lines 400` |
| Comm channel | `autonomous judge-policy` | `autonomous judge-policy-set judge_policy=ollama-a,claude-code judge_timeout_seconds=1800 judge_size_threshold_lines=400`; or `configure autonomous.judge_timeout_seconds=1800` |
| PWA | Settings, autonomous card | The `judge_policy` comma list, the `judge_timeout_seconds` field and the `judge_size_threshold_lines` field (Judge size threshold) on the same card |

Notes on the write forms:

- `judge_policy` replaces the whole list. Read it first and send the full list.
- An empty `judge_policy` (`--judge-policy ""`, `judge_policy=`, or `[]`) clears
  the list, so the judges follow the verifier routing policy again.
- `--judge-timeout-seconds 0` sets the default of 1800 seconds. A negative value
  is refused.
- `judge_size_threshold_lines` is a whole number of 0 or more. `0` turns the size
  rule off, and a negative value is refused. See [Size-aware routing](#size-aware-routing).
- A change made through the API, MCP, CLI, comm channel or PWA applies to the
  next judge call, with no restart. A YAML edit takes effect on restart.

Example, by MCP:

```
autonomous_config_set(judge_policy=["ollama-a","ollama-b","claude-code"], judge_timeout_seconds=1800)
```

## Troubleshooting

- **The judge always uses the verifier's backends.** `judge_policy` is empty.
  Set it to get a separate judge list.
- **The judge never moves to a Claude backend.** The Claude backend is not in
  the judge list, or it is held, or its session cap for the window is reached.
  Check the `judge_policy` decisions for the reason.
- **The judge keeps waiting.** The mode is `prefer-free` or `balance` and every
  backend is full. The wait is the capacity wait. Check the capacity card in the
  PWA, or the `capacity_status` MCP tool.
- **A judge timed out, but the run looked fine.** The limit is per run, so a slow
  model with a long answer can hit it. Raise `judge_timeout_seconds`.
- **A hold appeared after a restart.** Timed-out backends are kept on the
  Automaton, so the restart does not retry them. Read the attempt summary: the
  hold reflects the attempts before the restart as well.

## See also

- [Verifier policy](../api/autonomous.md#verifier-policy-set_task_verifier_policy--set_verifier_policy):
  the routing policy the judges share
- [Automata capacity](automata-capacity.md): slots, leases and the wait queue
- [Recover a stuck, failed, or incorrectly-terminal Automaton](autonomous-recovery.md):
  reset, cancel and the rejudge action
- [Failure escalation](failure-escalation.md): the separate ladder for failing tasks
- [Alerts and notifications](alerts-and-notifications.md): where alerts go
- [Retry policy](retry-policy.md): infra retries, which a judge that never ran
  goes through
