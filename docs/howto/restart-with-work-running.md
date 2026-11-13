# Restart with work running

When the daemon restarts, two settings decide what happens to Automata:

- `autonomous.restart_policy` covers Automata with tasks running.
- `autonomous.planning_restart_policy` covers an Automaton that is still in
  planning, meaning its decompose is still running.

Beyond those two, there is no setting to choose: a task that was **verifying**,
the guardrail **judgment** on a finished Automaton, and the **verification or
final gate script** are all picked back up by the restarted daemon instead of
being redone. See [Verifying, judging, and verification scripts](#verifying-judging-and-verification-scripts)
below.

This page covers the planning setting. For running tasks, see
[operations.md](../operations.md) (restart policy values).

## Current status

The setting is stored, validated and reported by `GET /api/config`, and the
daemon's boot path reads it. At boot the daemon decides for each planning
Automaton from the setting, the planner session it recorded when it spawned the
planner, and what that session left behind:

- the planner session is still alive → a reader is attached to it and its
  output file, and when the planner writes its plan the decompose completes as
  if the restart had not happened;
- the planner ended while the daemon was down and left a complete plan → that
  plan is ingested at boot and the Automaton moves on without a new planner
  run;
- nothing usable survives → planning is restarted once, with a decision
  recording it; a further restart with nothing usable left sends the Automaton
  back to draft.

That is the `reattach` (default) value. `restart` skips straight to the one
automatic planning restart at boot, and `draft` resets the Automaton to draft,
as before the setting existed. Every outcome lands in the boot-resume report
(see [Reading the recorded decision](#reading-the-recorded-decision)) and in
the daemon log, and live planner sessions that no reader owns are ended.

The planning boot path, the verifier/judge re-attach and the detached
verification-script pickup (below) are all in this tree, and the force-flag
guard that refuses a non-forced restart while any of that work is in flight is
unit-tested against fakes on every surface. But the restart matrix cannot be
run against a build of this tree for most states right now: the sandbox
config it generates does not set `server.allowed_project_roots` (only the
compute-node variant, used by `worker-node-placement`, does), so every state
that does not set `RM_PI_NODES` and creates an Automaton fails at creation
with `project_dir refused: project directory is outside the allowed project
roots`. Reported from a run on 2026-11-10 on a build of this tree (v9.118.3):
the enforced `worker-running` state failed at `state_reached`, and the same PRD
creation succeeded once the sandbox dir was added to `allowed_project_roots`
(one line in `scripts/restart-matrix/lib.sh`). That live result was not
re-run when these docs were revised; the static cause (no
`allowed_project_roots` in the default `rm_write_config`) was re-checked
against the tree. Until that harness fix lands,
treat every "verified on a sandbox daemon" claim in this area as stale — the
`planning` row and its release-smoke run included — and no live
deploy-restart has confirmed any of it: confirm on the build in use that a
restarted daemon behaves as above before relying on `reattach` or on a
verifying task, a judgment or a script surviving a restart, and that each
surface refuses a non-forced restart as above
(the *Still to review (operator)* notes in
[operations.md](../operations.md)).

## The three values

| Value | What the daemon does on restart |
|-------|-----------------------------------------------------------|
| `reattach` (default) | Keeps planning running. The decompose is re-attached to the restarted daemon. |
| `restart` | Restarts planning once after the restart and records a decision saying so. |
| `draft` | Resets the Automaton to draft, as the daemon did before the setting existed. |

An unset or unrecognised value reads as `reattach`. The write paths (REST, CLI,
MCP, comm channel and the PWA Settings select) refuse any value other than the
three above, and name the valid values in the refusal.

## How to set it

| Method | How |
|--------|-----|
| **YAML** | In `~/.datawatch/config.yaml`, under `autonomous:`, set `planning_restart_policy: reattach` (or `restart` or `draft`). |
| **CLI** | `datawatch autonomous config-set '{"planning_restart_policy":"restart"}'` |
| **Web UI** | PWA Settings, **Automata** tab, *Autonomous Automata planning* card — the **Planning restart policy** select with the three values `reattach`, `restart`, `draft`. Its localized help text (the `settings_planning_restart_policy*` keys in `internal/server/web/locales/*.json`) describes the values and links here. |
| **REST API** | `PUT /api/config` with `{"autonomous.planning_restart_policy": "draft"}` |
| **MCP** | `autonomous_config_set` with `planning_restart_policy` set to `reattach`, `restart` or `draft`. |
| **Comm channel** | `configure autonomous.planning_restart_policy=restart` |

The value is saved at once; the boot path reads it at the next restart, as
described under [Current status](#current-status).

## Choosing a value

- Use `reattach` when planning is healthy and you expect the restart to be quick.
  The intent is that nothing is lost and no planning work is repeated.
- Use `restart` when you suspect the planning run in the old process is stuck. The
  intent is that planning starts again once, and the boot-resume report shows it
  (see [Reading the recorded decision](#reading-the-recorded-decision)).
- Use `draft` when you want to review the Automaton before any planning runs again.
  The Automaton returns to draft and you request planning yourself.

## Reading the recorded decision

Every planning outcome the daemon takes at boot — the automatic planning
restart included — is one entry in the loop status' `boot_resume` array, in
the order the boot-resume scan took them:

- `prd_id` — the Automaton's id;
- `outcome` — `planning_restarted` for the automatic restart,
  `planning_ingested` for a plan recovered from the planner's output file,
  `planning_reset_to_draft` for the reset, `backoff_restored` for a usage
  back-off flag read back from disk (no `prd_id`; see
  [Across a daemon restart](automata-capacity.md#across-a-daemon-restart));
- `reason` — the policy in force, what the planner session left behind,
  whether the one automatic restart was already used, and how many live
  planner sessions with no reader were ended;
- `at` — when the daemon took it.

When an Automaton's backend is backed off at boot, its entry's `reason` ends
with a sentence saying new session starts for that backend wait in the capacity
queue and live sessions are left alone.

The array is empty until the daemon has restarted with a planning Automaton
in flight, and it lives for the life of the daemon process that booted; the
durable copy is the daemon log line
(`[autonomous] boot-resume: prd=<id> was planning on restart: <reason>
(action=<action>)`).

| Method | How |
|--------|-----|
| **REST API** | `GET /api/autonomous/status` — the `boot_resume` array in the body. |
| **MCP** | `autonomous_loop_status` — the `boot_resume` array. |
| **CLI** | `datawatch autonomous status` — JSON, the `boot_resume` array. |

An entry for the automatic restart looks like:

```json
{
  "prd_id": "a1b2c3d4",
  "outcome": "planning_restarted",
  "age_seconds": 0,
  "reason": "reattach: the planner session ended and left no usable output (partial); planning is restarted once automatically; policy=reattach; planning is started again once and planning_restarted_once is now set, so a further restart falls back to draft. No live planner session was left without a reader.",
  "at": "2026-11-09T06:12:41Z"
}
```

## Verifying, judging, and verification scripts

These three cases have no setting: the restarted daemon always picks them back
up, one step at a time.

**A task still verifying on a live verifier session.** The task keeps its
worker session and its worker result. If the verifier session itself survived
the restart, the daemon re-attaches to it — a new waiter reads the session and
its verdict file, and the verdict that lands completes or fails the task. The
verifier is not started a second time, the worker is not reset, and no retry
is counted. If the verifier session ended while the daemon was down but left
a verdict in its verdict file, that verdict is used as the outcome: a pass
completes the task, a fail goes through the normal retry path — again with no
second verifier and no worker re-run.

Only when nothing usable survives (no live session, no verdict on disk) does
the daemon fall back to the old behaviour: a finished worker is re-verified
once, and a task with no worker result is reset to `pending`.

**A guardrail judgment in flight** — for example the completion guardrail
judging a finished Automaton — resumes the same way: re-attach to the live
judgment session, or read the verdict it left, or re-judge once. A judgment
is never run twice for the same decision.

**The verification or final gate script.** It is no longer a child of the
daemon process: it runs detached, and its run (process, start time, output,
exit code, whether the result was delivered) is kept in a durable run record.
At boot the daemon does not re-run it:

- the recorded process is still running → the daemon waits on it and takes
  its result when it ends;
- the process ended while the daemon was down and its exit code is in the
  record, not yet delivered → the result is used once, as the script result;
- the record is missing or stale (or an earlier attempt of the same script
  left an undelivered run) → the script runs from the start, exactly once;
  an undelivered, unfinished run left behind is retired first, so its
  verdict is never consumed.

The script's timeout is enforced from its recorded start time, so a script
that outlives the daemon still ends on time even across the restart.

**A final gate survives a stop of the daemon.** The final gate (the script or
command that runs the whole verification at the end of an Automaton) keeps its
own run record, one per check and project directory, in the same run root as
the verification scripts. A final gate task that was running when the daemon
went down is not failed and does not use a retry: at boot it is put back to
`pending` with no failure, and its gate run re-attaches to the record — it waits
on a live gate, or takes a finished result once. A daemon that is only stopping
never turns a gate into a failure: the gate keeps running and the next attempt
picks it up.

The shipped systemd unit (`install/systemd/datawatch.service`) sets
`KillMode=process`, so a `systemctl stop` or `restart` ends only the daemon and
leaves the gate (and any verifier) running in the unit's control group. If you
run your own unit file, add the same line, or the service manager ends the gate
with the daemon.

**One retry for a gate killed by a signal after a restart.** If a final gate is
still killed by a signal and the daemon started less than five minutes before
the kill, the Automaton reruns the gate once. This retry is outside the task's
retry budget and counts no attempt; the task records that it was used
(`gate_restart_retry_used`), the decision log names the daemon restart as the
cause, and the gate report notes the retry. A second signal kill, a kill more
than five minutes after the daemon started, a timeout and a nonzero exit code
are real failures and go through the normal failure path.

A run is only waited on while its process is genuinely the script: the daemon
checks the process table for the same start time as the record. If that start
time cannot be read, or is implausibly large, the process is not trusted and
the run is handled as one whose process is gone (never as a live one), so a
recycled process id is not mistaken for the script.

What changed versus the recorded audit: the restart-matrix findings
([`restart-matrix-findings.md`](../plans/restart-matrix-findings.md)) report
verification scripts and final-gate scripts as "daemon children, rerun from
scratch, OPERATOR PICK" and the live verifier session as "not adopted; the
verifier runs once more". That report measures the daemon as it was at the
time. The daemon in this tree no longer behaves that way; the matrix states
and the release-smoke skip text still describe the old behaviour (the smoke
list still names those five states and their old skips), so re-run the matrix
and review before relying on them (the *Still to review (operator)* notes in
[operations.md](../operations.md)).

The read-only report is `GET /api/autonomous/restart-interruptions` (also the `restart_interruptions` MCP tool, `datawatch autonomous restart-interruptions`, and the chat verb `autonomous restart-interruptions`): the same lists it names in the refusal, plus `needs_force`, which is set while any of them is in flight. A non-forced restart is refused while `needs_force` is set, and every refusal below reuses the report as is.

## Forcing a restart past running work

A restart that would interrupt running Automata work is refused unless it is
forced. The refusal names what would be interrupted: the planning Automata,
the verifying tasks, the running judges, the running tasks and the live
sessions, with their ids and the restart policy in force. Nothing is stopped.

| Surface | Refused without force | Force |
|---------|-----------------------|-------|
| CLI | `datawatch restart` prints the report and exits non-zero | `datawatch restart --force` |
| REST | `POST /api/restart` answers 409 with the report | `POST /api/restart?force=true` or `{"force": true}` |
| MCP | `restart_daemon` returns an error result carrying the report | `restart_daemon` with `force=true` |
| Messaging channels | `restart` replies with the report | `restart force` |
| Web UI | The Restart button (Settings → About) sends `POST /api/restart` without force, so it reports the refusal as a failed restart | No force option; restart from a surface that can force instead |

When nothing that would be interrupted is running, every surface restarts as it
did before, with no force needed. The restart policy (`autonomous.restart_policy`)
still applies to a forced restart.

## Checking the value

`GET /api/config` returns the stored value as `planning_restart_policy` in the
`autonomous` block. The `datawatch autonomous config-get` command reads the
autonomous manager config and does not show this key.
