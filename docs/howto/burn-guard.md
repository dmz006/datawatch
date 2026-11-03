---
docs:
  index: true
  topics: [usage, burn, guard, ladder, effort, retry, capacity, alerts]
exec_steps:
  - tool: burn_guard_status
    description: Read the guard first. It shows whether it is on, the rung in force, the projected weekly percent at the reset against the pace line and margin, and the last decision
    args: {}
    read_only: true
  - tool: burn_guard_config_get
    description: Read the guard's settings (usage.burn_guard) before changing any of them
    args: {}
    read_only: true
  - tool: burn_guard_run
    description: Evaluate the guard now instead of waiting for the next interval. It may climb or step back one rung. It never ends a running session
    args: {}
  - tool: burn_guard_config_set
    description: Worked example, a state-changing call. Widen the margin to 3 points so the guard acts later. One invalid value rejects the whole call and stores nothing
    args: {margin_points: 3}
  - tool: burn_guard_config_set
    description: Worked example, a state-changing call. Allow 2 retries of one task on Claude before the task is held for you (0 to 20)
    args: {retry_limit_claude: 2}
  - tool: burn_guard_config_set
    description: Worked example, a state-changing call. Turn on ask mode so the guard also raises a question per change alongside the alert, keeping at most one open
    args: {ask_on_change: true}
---

# Burn guard

The burn guard watches how fast the Claude sessions the daemon runs are using
your weekly allowance. When the burn projects past the weekly pace line before
the reset, it steps down a **ladder** of changes to what new work is admitted.
When the burn is back under the line it steps back up.

**The guard changes admission only. It never ends, kills or restarts a running
session.** A session that is already running always finishes.

It builds on the [usage calculator](usage-calculator.md): it uses the readings
you type there for the pace line and the ceiling, so a recent reading is the
one thing it needs from you.

## What it does each interval

1. Reads the session transcripts and counts input, output, cache-write and
   cache-read tokens per hour, split between worker and verifier sessions and
   everything else.
2. Turns tokens into points of the weekly allowance with two prices, points per
   million cache-write tokens and points per million cache-read tokens. When
   `refit_on_reading` is on, each new usage reading refits both prices against
   the points used between readings, so the model follows what your provider
   actually charges. A poor fit shows in the status as a fit error.
3. Projects the points to the weekly reset and compares the result with the
   pace line of your newest reading.
4. Moves **at most one rung** up or down the ladder.

With no usable reading yet, the guard holds and the status says what it needs.

## The ladder

The ladder is an ordered list of rungs. Each rung is a change to admission.

| Rung | What it does |
|---|---|
| `lower_inflight` | Lowers the cap on how many Claude sessions may run at once. The original cap is remembered and restored when the rung is released |
| `route_pi_prompt` | Sends new Claude work to the `pi-prompt` backend instead |
| `hold_claude` | Holds new Claude work. Work already running continues; held work starts again when the rung is released |

The default ladder is `lower_inflight`, `route_pi_prompt`, `hold_claude`, in
that order. You can reorder it or leave a rung out (for example
`lower_inflight`, `hold_claude`). An unknown or repeated rung is dropped, and an
empty list means the default.

**Climbing.** When the projection is more than `margin_points` above the target,
the guard applies the next rung. **Holding.** Inside the margin it changes
nothing. **Stepping back.** When the projection is more than `margin_points`
below the target and `step_back` is on, it releases the last rung it applied,
one per interval, in reverse order. With `step_back` off, rungs stay in force
until you release them (turn the guard off, which releases everything at once).

Turning the guard off (`enabled: false`) releases every rung at once, so
admission is never left restricted by a guard that is off.

## Settings

All settings are under `usage.burn_guard`. Each is applied to the next
evaluation with no restart, and one invalid value rejects the whole change.

| Setting | Meaning | Default |
|---|---|---|
| `enabled` | Guard on or off | `true` |
| `interval_minutes` | Minutes between evaluations, 1 to 1440 | `60` |
| `margin_points` | How far the projection may run over the pace line before the guard acts, 0 to 50 | `2.0` |
| `ladder` | Ordered rungs applied while over the line | `lower_inflight, route_pi_prompt, hold_claude` |
| `step_back` | Release rungs in reverse once back under the line | `true` |
| `ask_on_change` | When false (default) one alert per change and no question; when true the per-change alert still goes out and at most one open question per change is kept | `false` |
| `cache_write_points_per_million` | Weekly points per million cache-write tokens, above 0 and at most 1000 | `0.373` |
| `cache_read_points_per_million` | Weekly points per million cache-read tokens, above 0 and at most 1000 | `0.0069` |
| `refit_on_reading` | Refit the two prices when a new reading arrives | `true` |
| `effort_ceiling` | Task kind to ceiling (see below) | `docs`, `parity`, `live_check`, `final_gate` all `low` |
| `retry_limit_claude` | Most retries of one task on Claude, 0 to 20 | `2` |

An invalid value in a saved config falls back to its default with a warning in
the daemon log; the daemon still starts.

`ask_on_change` picks how the guard reaches out about a change. When false
(the default) it sends one alert per change and no question. When true it
keeps at most one open question per change until you answer it, alongside
the per-change alert.

### The question on top of the alert (`ask_on_change`)

An alert goes out every time the guard applies or releases a rung; this
setting decides whether a question then asks you to keep or release the
change.

- **`false` (the default) — one alert per change, no question.** The guard
  applies or releases the rung, warns you with the numbers behind it, and
  asks nothing of you. Each such alert also carries the one-line suggestion
  *Set `usage.burn_guard.ask_on_change` to true to get a question per
  change*, so you can turn the questions on directly from the alert.
- **`true` — the guard asks.** For every step that actually changed a setting
  it holds **at most one open question**, worded like *"The weekly burn
  projects to 104.2% at the reset against a target of 90.0%. I did this:
  lowered the claude-code inflight cap from 4 to 3. Release it, or leave
  it?"* with the answers **Leave it as is** and **Release**. Answering
  *Release* records your decision; the guard owns the rung and releases it
  itself when it steps back. If a new rung change arrives while a question
  is still open, the guard withdraws that open question first — the
  withdrawal reason says it was *superseded by a newer burn guard rung
  change* — and raises the question for the new step, so a staircase of
  changes leaves you with one question, not a pile of them.

A step raises a question only when it changed a setting. A hold (changed
nothing), a `lower_inflight` rung whose cap was already at one, a release that
would restore a cap that was never lowered, a step that only releases the
hold (a `hold_claude` release), and turning the guard off all alert but ask
nothing. Two more withdrawals keep you from ever answering
a question that no longer stands: if you turn `ask_on_change` off again, the
next change withdraws the last open question (reason: *ask_on_change is off,
so the burn guard alerts only*), and when the daemon starts, any guard
question a previous run left open that the current ladder state no longer
supports is withdrawn as stale (reason: *at start the burn guard no longer
applies the rung this question refers to*). You can set `ask_on_change` from any row of the *Where to change them* table
below, without a restart — the guard reads the current value every interval.

### Where to change them

| Surface | How |
|---|---|
| YAML (config file) | `usage.burn_guard.ask_on_change: true` under the `usage.burn_guard` key; see the [configuration reference](../config-reference.yaml) |
| Web UI | Usage calculator view, **Burn guard** card: the settings under the status (**Ask instead of alerting** is `ask_on_change`), **Run now** to evaluate once; Settings > Usage burn guard for the same keys; the **Burn guard** stat card in the Monitor panel |
| CLI | `datawatch config set usage.burn_guard.ask_on_change true` (switch it back with `false`); `datawatch usage burn-guard status`, `run`, `config [key=value ...]`; `datawatch automation set-effort-ceiling <id> docs=low ...` (or `--clear`) for the effort ceiling |
| MCP | `config_set` with `key: usage.burn_guard.ask_on_change`, `value: true`; `burn_guard_status`, `burn_guard_run`, `burn_guard_config_get`, `burn_guard_config_set` |
| Chat | `configure usage.burn_guard.ask_on_change=true` |
| REST | `PUT /api/config` with `{"usage.burn_guard.ask_on_change": true}`, or `PUT /api/usage/burn-guard/config` with `{"ask_on_change": true}`; `GET /api/usage/burn-guard`, `POST /api/usage/burn-guard/run`, `GET`/`PUT /api/usage/burn-guard/config`; `GET /api/config` returns the block under `usage.burn_guard`; `POST /api/automations/{id}/set_effort_ceiling` for the effort ceiling |
| Comm channel | `usage burn-guard` for the status; settings through `configure usage.burn_guard.<key>=<value>` |

## Reading an alert

Each time a rung is applied or released, the guard raises one warning alert
titled `Burn guard: climb <rung>` or `Burn guard: step_back <rung>`. With
`ask_on_change` on, it additionally raises the keeping/releasing question
described above; off, the alert is the whole notice. The alert text states
what was done and the numbers behind it, and (with `ask_on_change` off)
carries the one-line suggestion to set `usage.burn_guard.ask_on_change` to
`true` to get a question per change:

> Burn guard stepped up to rung 1 of 3 (lower_inflight): lowered the claude-code
> inflight cap from 3 to 2. (projected 31.4% at the reset, target 28.0%, burning
> 0.95 points/hour against 0.80 allowed) 2 running session(s) were left alone.

Read it from left to right:

- **Rung n of m** is how far down the ladder you are. Rung 3 of 3 means the
  last rung is in force and the guard has nothing further to do.
- **What was done** is the admission change in plain words.
- **Projected** is the weekly percent the burn reaches at the reset. **Target**
  is the ceiling less the safety margin of the usage calculator.
- **Burning ... against ... allowed** compares points per hour now with the
  points per hour that would land exactly on the line.
- **Running sessions were left alone** confirms nothing running was touched.

The question asks whether to release the change or leave it. It does not block
anything. If a step cannot raise its question, the status shows the error and
the change stays in force.

## Stepping back

To step back yourself:

- **Wait.** With `step_back` on, the guard releases one rung per interval once the
  projection is under the line by more than the margin.
- **Run now.** `burn_guard_run` (or **Run now** in the card) evaluates at once; it
  still moves at most one rung.
- **Raise the margin or reorder the ladder** if the guard acts too early or on a
  rung you do not want. Take `hold_claude` out of the ladder to keep it from
  ever pausing new Claude work.
- **Turn it off.** `enabled: false` releases every rung immediately.
- **Type a fresh reading** in the usage calculator. The pace line and the fit
  both move with it, and a reading that shows the week is lower than projected
  lets the guard step back.

### Where the rung and its reason show

While a rung holds a backend, the daemon lists it as an active back-off flag
next to the pools and budgets. You see the rung name next to its reason in
three places:

- **`datawatch capacity`** prints each active back-off flag; a flag the guard
  set reads `claude-code: resets <time> (burn guard rung <rung>: <reason>)`,
  and a flag without a rung (a rate limit or an operator set) keeps the
  plain `(<reason>)` form. The reset time is shown in the zone the daemon
  sent it in.
- **The comm channel** `capacity` verb answers with the same text.
- **The web UI** capacity card shows the rung as a small label next to the
  backend name, with the reason on its own line below.

The hold ends on its own when the guard steps back, at the next reading, or
at the weekly reset. Until then the flag stays in the list — its presence is
the clearest sign that something the guard did is still restricting
admission.

## The effort ceiling

The effort ceiling caps how much effort a kind of task may use, so routine
tasks do not spend heavy-model effort. It only ever lowers effort or moves the
task to the cheaper backend; it never raises anything, and an effort already at
or under the ceiling is kept.

Ceilings are `low`, `medium`, `high` and `cheaper_backend`. The default caps the
kinds `docs`, `parity`, `live_check` and `final_gate` at `low`. A task with no
effort set takes its ceiling. Most specific wins: the task's own ceiling, then
the Automaton's per-kind map, then the daemon-wide map under
`usage.burn_guard.effort_ceiling`. With the guard disabled every ceiling is a
no-op.

The ceiling has its own tools rather than the generic settings call:

- `automation_set_effort_ceiling` sets an Automaton's per-kind map (an empty map
  clears it), for example `{"docs":"low","final_gate":"cheaper_backend"}`.
- `autonomous_prd_set_task_effort_ceiling` sets one task's ceiling (empty clears
  it).
- The comm channel verb is `automation set-effort-ceiling <id> docs=low ...` (or `clear=true`), the CLI (`datawatch automation set-effort-ceiling <id> docs=low`, `--clear` to remove) and REST (`POST /api/automations/{id}/set_effort_ceiling`) carry the same call, and the PWA card shows the
  daemon-wide ceilings read-only.

## The Claude retry limit

`retry_limit_claude` is the most times one task is retried on Claude (default
2, 0 to 20). A task that has used its retries is **held for your review**, not
failed, and, with the operator question channel on, you get one question naming
the task with two answers:

- **Retry the task now** runs it again. Raise `retry_limit_claude` first, or it is
  held again at the same limit.
- **Cancel the task** cancels it; tasks that depend on it do not run. This is the
  default.

This stops a task that keeps failing from spending the week's allowance on
repeat attempts.

## What the guard does not do

- It does not end, kill or restart a running session.
- It does not change a lane count you set; the lane count stays yours (see the
  [usage calculator](usage-calculator.md)).
- It does not run without readings; type one to give it a pace line.

## See also

- [Usage calculator](usage-calculator.md)
- [Automata capacity](automata-capacity.md)
- [Configuration reference](../config-reference.yaml) (`usage.burn_guard`)
- [Parity status](../parity-status.md)
