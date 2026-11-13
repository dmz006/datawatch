---
docs:
  index: true
  topics: [autonomous, automation, supervisor loop, escalation, jobs, runs, settings, eval, models]
exec_steps:
  - tool: supervisor_loop_jobs
    description: Read the supervisor loop's jobs, with each model job's style, backend and model and each job's last outcome
    args: {}
    read_only: true
  - tool: supervisor_loop_runs
    description: Read the newest run records, optionally for one job or one outcome
    args: {}
    read_only: true
---

# Supervisor loop

The **supervisor loop** is the daemon's standing loop that keeps every Automaton
moving, so you do not have to type the next command. Each pass runs a set of
mechanical steps, which make no model call, and three model jobs, which ask a
model one narrow question and check the answer. Every run leaves a run record you
can read. The [Supervisor loop entry](../datawatch-definitions.md#supervisor-loop)
defines each setting.

## What it does

### The mechanical steps (no model)

A pass runs each enabled step once, in this order. A step that fails is recorded
and is not tried again in the same pass.

| Step | What it does |
|---|---|
| `close_finished_records` | Closes the records of Automata that have finished: completed ones have no grace and are archived on the next idle pass, but only once their work has landed on the base branch — a completed one whose branch still holds commits that are not on the base branch is kept until that work is merged (a rebased or squashed landing counts), its branch is deleted, or you archive it yourself; rejected ones are archived on the next idle pass; a cancelled one is archived once the `archive_cancelled_after_minutes` grace has passed since its most recent cancel (0 archives it at once); and a cancelled one whose branch still holds commits that are not on the base branch is kept until you archive it, its work is merged, or its branch is deleted. A branch whose state cannot be determined is kept — never archived early. The loop's archives are recorded with the actor `supervisor-loop`; your own archive is unaffected and is recorded with the actor `operator`. |
| `deploy_after_ship` | Runs your deploy step once for each newly shipped release. With no deploy step configured it records "not configured" and does nothing. |
| `retry_failed_infra` | Retries tasks that were held after the infrastructure retries ran out. A task that is held again stays for you. |
| `cleanup_sweep` | Runs the cleanup sweep for leftover worktrees and scratch files, and raises the disk alert when it is due. |
| `apply_answers` | Applies the answers you already gave to the held questions that are waiting on them. An answer is never applied twice. |
| `lane_count` | Moves the lane count toward the wanted count, never outside your minimum and maximum. With the maximum at 0 it moves nothing. |
| `start_next_chain_step` | Starts the next Automaton in a chain when the one before it is done. |
| `run_merge_until_conflict` | Runs the merge pipeline until a merge conflicts, and keeps the newest conflict for you to see. |

### The model jobs

Each model job asks a model for one thing and checks the answer before using it.

| Job | What it asks for | The check |
|---|---|---|
| `draft_spec` | A draft specification, as stories and tasks | The answer must be valid JSON in the decompose shape, every task needs a spec and a files list, and no name may contain a tracker identifier. |
| `classify_ci_failure` | The class of a failed CI run | The answer must be exactly one of `infra`, `flaky_test`, `test_failure`, `build_error`, `lint`, `dependency` or `unknown`. |
| `pick_backend` | Which backend should take a task | The answer must be exactly one of the candidate backends it was given. |

A job runs in one of two **styles**: `oneshot` (a single call, the default) or
`session` (a full session). If you set no backend and model for a job it uses the
decision assistant's configured model. If there is none, the job **escalates to
you at once**; it never falls back to a hosted model on its own.

## Settings and defaults

In the config file the keys sit in the `autonomous.supervisor_loop` block, and each
key is a flat dotted name that keeps the full `supervisor_loop.` prefix, for example
`"supervisor_loop.lane_count.max": 4`. A key without the prefix, such as
`lane_count.max`, is rejected as an unknown setting and the default is used.
Everywhere else they are written `autonomous.supervisor_loop.<key>`. Each one has an entry in
the [definitions](../datawatch-definitions.md#supervisor-loop).

| Key | Default | Meaning |
|---|---|---|
| `enabled` | `true` | Master switch for the whole loop. |
| `<step>.enabled` (for each of the eight steps) | `true` | Turns one mechanical step on or off. |
| `<job>.style` (for each model job) | `oneshot` | `oneshot` or `session`. |
| `<job>.backend` | empty | The backend for the job. Empty uses the decision assistant's. |
| `<job>.model` | empty | The model for the job. Empty uses the decision assistant's. |
| `validator_retries` | `2` | Extra attempts after an answer fails its check. |
| `escalate_after_failures` | `2` | Failed attempts in one run before you are asked. |
| `run_retention` | `500` | How many recent run records are kept. |
| `archive_cancelled_after_minutes` | `60` | The grace, in minutes, before a cancelled Automaton is archived, measured from its most recent cancel. `0` archives it at once. |
| `lane_count.min` | `0` | The lowest lane count the loop may set. |
| `lane_count.max` | `0` | The highest lane count the loop may set. `0` means no bounds, so the lane count step moves nothing. |
| `wake.enabled` | `false` | Turns the event wake on: a listed event starts a pass at once instead of waiting for the scheduled check, which stays as the fallback. Off by default, so nothing changes until you turn it on. |
| `wake.kinds` | the default list | The event kinds that wake the supervisor: a blocked Automaton, a task needing review, a failed task or final gate, every judge verdict, all tasks done or cancelled, an Automaton stuck running, a long capacity wait and a burn guard rung change. Each must be a kind a subscription can filter on; written comma-separated over the CLI and the comm channel. `datawatch autonomous event-kinds` lists every valid kind and marks the ones in the default list. |
| `wake.coalesce_seconds` | `30` | The quiet window after a wake: events arriving within it fold into the same pass. `0` to `3600`; `0` wakes on every event. |
| `wake.fallback_interval_minutes` | `20` | Minutes between scheduled passes while the wake is on, `1` to `1440`. Events run passes in between; with the wake off the scheduled check runs on every idle tick. Read live. |

## Event wake

By default the supervisor loop runs only on its schedule. Turn on
`wake.enabled` and a listed lifecycle event starts a pass at once instead of
waiting for the next scheduled check. The schedule stays as the fallback, run
every `wake.fallback_interval_minutes`, so a missed event is still caught.

How it behaves:

- Enabling it registers a built-in subscription set named `supervisor-wake`, one
  subscription per kind in `wake.kinds`. Nothing is registered while the wake is
  off, and none of your own stored subscriptions are touched.
- Events arriving within `wake.coalesce_seconds` of each other are folded into
  one pass. Only one pass runs at a time; events that arrive during a pass are
  queued and handled by the next one.
- Each wake runs the loop's mechanical steps at once and, if a supervisor session
  is running (tagged `role:supervisor`, running or waiting for input), types a line
  such as `supervisor wake: task.failed automaton=<id> task=<id>` into it. With no
  session running the pass still runs and only the line is skipped.
- The registration follows the setting live: switching `wake.enabled` or changing
  the kinds or quiet window takes effect within a few seconds, with no restart.
- The scheduled check is the fallback: while the wake is on it runs a pass every
  `wake.fallback_interval_minutes`, and each scheduled pass is counted in the
  status.
- The wake status shows when the supervisor last woke for an event and when it
  last ran a scheduled check, with a count of each. If the event path has been
  silent for more than an hour, the status says so, which usually means the
  kinds list is too narrow or nothing you subscribed to has happened.

For the whole path (choosing events, subscribing to a peer, extension
registration, MCP resource subscriptions and what a peer forwards), see
[The event-driven supervisor](automaton-event-driven-supervisor.md).

Review before relying on it: the wake is off by default and nothing turns it on
for you. Check that the kinds list matches the states you want a pass for.
After switching it on, cause one of those events and confirm the status shows an
event wake.

Change the settings and read the status on any surface:

| Surface | Settings and status |
|---|---|
| Web UI | **Settings → Automata → Supervisor wake**; the status line sits in the same group. |
| CLI | `datawatch supervisor-loop wake` shows the status; add `--enabled=true`, `--kinds A,B`, `--coalesce-seconds N` or `--fallback-interval-minutes N` to set. |
| REST API | `GET /api/supervisor-loop/wake` for the status; write the `autonomous.supervisor_loop.wake.*` keys with `PUT /api/config`. |
| MCP | `supervisor_loop_stats` returns a `wake` object; write the keys with `config_set`. |
| Comm channel | `supervisor wake` or `supervisor wake status`; `supervisor wake set key=value ...` to change. |
| YAML | The `"supervisor_loop.wake.*"` keys in the `autonomous.supervisor_loop` block; see `docs/config-reference.yaml`. |

## Access methods

| Method | How |
|---|---|
| YAML | Edit the `autonomous.supervisor_loop` block in your config file; see `docs/config-reference.yaml`. Example: `"supervisor_loop.lane_count.max": 4` (quote the key and keep the `supervisor_loop.` prefix). |
| CLI | `datawatch config set autonomous.supervisor_loop.enabled false`. Read with `datawatch supervisor-loop jobs` and `datawatch supervisor-loop runs`; run one job with `datawatch supervisor-loop run <job>`; show or set the event wake with `datawatch supervisor-loop wake`. |
| Web UI | **Settings → Automata → Supervisor loop** to change settings. The **Supervisor loop** card on the **Setup** sub-tab of the **Automation** view shows the jobs and runs. |
| REST API | `PUT /api/config` with the `autonomous.supervisor_loop.*` key. `GET /api/supervisor-loop/jobs`, `GET /api/supervisor-loop/runs`, `GET /api/supervisor-loop/wake`, `POST /api/supervisor-loop/jobs/<job>/run`, `POST /api/supervisor-loop/eval`. |
| Comm channel | `configure autonomous.supervisor_loop.enabled=false` to change a setting. `supervisor jobs`, `supervisor runs`, `supervisor stats`, `supervisor wake` and `supervisor run job=<job>` to read and run. |

MCP clients use `config_set` for settings and the five `supervisor_loop_*` tools
listed in the [MCP reference](../mcp.md).

## Where to see

| Surface | Where |
|---|---|
| Web UI card | The **Supervisor loop** card on the **Setup** sub-tab of the **Automation** view: jobs (name, style, backend, model, last outcome, escalations) and recent runs. |
| REST runs route | `GET /api/supervisor-loop/runs` (filters `job`, `outcome`, `limit`). The jobs are at `GET /api/supervisor-loop/jobs`. |
| CLI runs command | `datawatch supervisor-loop runs [--job NAME] [--outcome RESULT] [--limit N] [--json]` |
| MCP tool | `supervisor_loop_runs` (and `supervisor_loop_jobs`) |
| Comm verb | `supervisor runs [job=NAME] [outcome=RESULT] [limit=N]` |
| Monitor numbers | The **Supervisor loop** card on the **Monitor** tab; `GET /api/supervisor-loop/stats`; `datawatch supervisor-loop stats`; the MCP tool `supervisor_loop_stats`; `supervisor stats` on the comm channel. Prometheus: `datawatch_supervisor_loop_runs{outcome}`, `datawatch_supervisor_loop_jobs_enabled`, `datawatch_supervisor_loop_last_run_age_seconds`. |

## Reading a run record

A run record shows the job, its style, backend and model, and then:

- **Outcome.** `ok` means the answer passed its check. `validator_failed` means
  every attempt failed its check and the retries ran out. `escalated` means the
  failed attempts reached `escalate_after_failures` and you were asked. `error`
  means the call itself failed, for example no model was reachable or none was set.
- **Validator failures.** The reason each rejected answer failed, such as "tracker
  ID in task name" or "class is not one of ...". The same reason repeating tells
  you what the model keeps getting wrong.
- **Retries.** How many extra attempts the job needed. A job that passes on the
  first try shows none.
- **Escalations.** How many times the job was handed to you.
- **Duration.** How long the run took, in milliseconds. A slow model shows here.

## Tuning

- **Change a job's model.** Set `<job>.backend` and `<job>.model`, for example
  `datawatch config set autonomous.supervisor_loop.classify_ci_failure.model <model>`.
  The next run uses it.
- **Change a job's style.** Set `<job>.style` to `session` when a one-shot call is
  too thin for the job, or back to `oneshot` for speed.
- **Change the retry count.** Raise `validator_retries` when a good model fails the
  check now and then, or lower it to stop sooner. Keep `escalate_after_failures`
  at or below the number of attempts (`validator_retries` plus one), or a job will
  run out of retries before it ever asks you.
- **Read the signs.** Many validator failures with one model: try a stronger model
  or the `session` style. Escalations right away with an empty reason: no model is
  set for that job. Long durations with passing runs: try a smaller model.

## Running the eval

The eval compares models on a model job using stored cases, each marked as one
the model should pass or one it should fail. It reports, per model, the cases,
how many passed, the validator failures, retries, median and maximum duration and
escalations. It tells you which model to give each job.

```
datawatch supervisor-loop eval --job classify_ci_failure --model <model-a> --model <model-b>
```

The job is `draft_spec`, `classify_ci_failure` or `pick_backend`. Use `--cases
<dir>` to point at your own cases (a relative directory holding `<job>/*.json`)
and `--format json` for the raw result. The same eval is `POST
/api/supervisor-loop/eval` and the MCP tool `supervisor_loop_eval`. If none of the
models you list can be reached, it fails and names each one; it never invents a
result.
