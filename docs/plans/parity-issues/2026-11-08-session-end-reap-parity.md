# [sync] Session-end orphan reaping, host load alert and bounded load steps — defect fix parity (datawatch app)

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI and creates
nothing on any host.

**Defect this work addresses** (found 2026-11-04): after a worker task was
cancelled, the processes the worker had started kept running — test runs,
servers, helper scripts outlived the session that launched them and piled up
on the host.

## Affected area

The defect sits in the lifecycle boundary between an Automaton worker session
and the host: a task finishing, failing, or being cancelled used to record the
session as ended and leave whatever the session had spawned behind. The fix
acts at three moments and this work lands at all of them. First, **session-end
reaping**, which is automatic and always on: whenever a session ends for any
reason — finished, cancelled, failed, or the daemon stopping — the daemon ends
the processes that session left behind. A process is attributed to a session
only by evidence read from `/proc` — the executable, a directly run script, or
the working directory inside the session's worktree or scratch folder, an
ancestry chain that reaches one of the session's root pids, or the session id
in the process's environment — and it is ended by exact pid, never by command
name, with the evidence and start time re-verified just before each signal so
a reused pid can never be misattributed. A reap that ends anything writes one
`session_end_reap` audit entry (how many were ended, how many survived, the
program names of the ones ended) and, when the session was bound to a task,
one `session_end_reap` decision on that task; a reap that finds nothing writes
nothing, and survivors are counted, not re-signalled. Second, the **host load
check** under `autonomous.host_load_alert`: a once-per-minute watch on the
load average whose `reap` mode ends the leftover processes of ended sessions,
as part of a sustained breach, using the same evidence rule — a backstop for
the session-end pass when it could not catch everything. Third, the **bounded
load step rule**: a task that asks to pin the CPU must bound it (niced, capped
to a quarter of the cores, hard-timed, self-cleaning, dead when its task is
dead), stated one way in three surfaces — the built-in `bounded_load_step` task
template, the rules block appended to the decompose prompt the planner reads,
and the warn-severity decompose-lint finding `unbounded_load_step`.

The operator guidance for all of it now lives in
[docs/howto/automata-orphan-reaping.md](../../howto/automata-orphan-reaping.md),
[docs/howto/host-load-check.md](../../howto/host-load-check.md), the
*Leftover processes (session-end reap)* and *Host load check* entries in
[docs/datawatch-definitions.md](../../datawatch-definitions.md), and the
*Processes a session leaves behind* section of
[docs/operations.md](../../operations.md).

## Current behavior (what this work delivers on the server and PWA)

The defect is closed on the datawatch side: ending a worker session in any way
no longer leaves its processes running, so a cancelled task stops the work it
started. Every reap and every load-check episode is visible on the existing
surfaces — the audit log (action `session_end_reap`), the task decision
history, the alert feed (one "Host load high: Automata may be slowed" warning
per sustained-breach episode), and the decompose-lint findings panel
(`unbounded_load_step`). The host load check's configuration
(`autonomous.host_load_alert.mode` = `off` | `alert` (default) | `reap`,
`multiple` default 1.5, `minutes` default 10) round-trips through YAML, the
PWA General settings, REST, the comm channel, the CLI
(`datawatch autonomous config-set`) and MCP, and reads live, so a change
needs no restart. The session-end reap itself is not configurable — it is
always on by design, because the failure it prevents is a host you own.

The gap this leaves is entirely on the operator-facing mobile clients. As the
app stands, none of the new records, record types, alerts, or settings have a
home there: a client that lists or filters its audit entries will meet an
action (`session_end_reap`) it has no knowledge of, an Automaton detail view
will show a task decision it does not render or summarize, the alert feed will
carry a new warning with a shape (busiest process groups, running-vs-ended
session attribution) the client has never seen, and the settings surfaces that
mirror the server's configuration have no counterpart for the host load check
fields. The built-in rule surface also changed underneath the settings: the
effective decompose-lint pack now carries the `unbounded_load_step` rule and
the built-in task templates now carry `bounded_load_step`, so any client that
displays the active rule set, the templates, or findings renders a slightly
different world than before this release.

## Expected behavior

Once parity is met, a client on any platform can do what the PWA can: see the
`session_end_reap` audit entries and the `session_end_reap` task decision as
first-class results of a session or task end (including the ended count and
the survivor count, with the existing guidance that survivors are verified by
hand), display the host load high alert with its busiest-groups detail the way
any other alert is displayed, and set and read the
`autonomous.host_load_alert` fields — mode, load-to-cores multiple, breach
minutes — from the same configuration surface the rest of the operator settings
use, with the change taking effect live with no restart. The client renders
the `unbounded_load_step` finding like any other decompose-lint finding and
shows the `bounded_load_step` template in the active template list, so the
settings a client displays matches the rules a decomposition is actually linted
against. In the absence of all of that, a client must at minimum not
misrender or drop the new audit action, decision, alert, or config block —
unknown-but-valid values should read as their payload says, not as errors.

## Suggested next step

File one `feat(autonomous):`-prefixed issue in datawatch-app covering the four
surfaces above — the `session_end_reap` audit action and task decision, the
host load high alert, the `autonomous.host_load_alert` configuration fields,
and the two new built-in load-step entries in the decompose-lint/template
surfaces — referencing this draft and the operator howtos as the source of
truth for wording and shape. Where the app already renders audit entries, task
decisions, alerts, or settings generically from their payloads, the acceptance
criteria are a pass-through test: a seeded `session_end_reap` entry and
decision, one host load alert, one `unbounded_load_step` finding, and a
round-trip of each `autonomous.host_load_alert` field must display correctly on
both Android and iPhone. If any of those four is structurally new to a client,
that client gets the dedicated handling described in Expected behavior, with
the same capability parity on both platforms and no implementation parity
required. Nothing here changes any REST or MCP contract the clients already
parse — the work is rendering, mirroring, and the config round-trip — so the
issue is a display-and-settings story, not an integration story.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included | The check's keys round-trip via `GET/PUT /api/config` (the autonomous config endpoints accept the same keys), and its read-only state rides the existing `GET /api/stats` and `GET /api/autonomous/status`. The reap's outcomes are observable on the existing audit-log and task-decision endpoints — no new endpoint. |
| MCP | included | `autonomous_config_get`/`autonomous_config_set` carry the host load alert keys, and the `autonomous_status` tool returns the REST status body verbatim, which now carries the `host_load` snapshot. |
| CLI | included | `datawatch autonomous config-set` writes `autonomous.host_load_alert.*` and the operator reads the effective values from the CLI. (The deliberately non-applicable monitoring surface is a host-load *status printing* CLI command — a new surface, out of scope per `docs/plans/host-load-status-surfaces.md`.) |
| comm channel | included | `configure autonomous.host_load_alert.<mode\|multiple\|minutes>` writes the setting and `autonomous host-load` reads the effective values. |
| YAML/config | included | The `autonomous.host_load_alert` block (`mode`, `multiple`, `minutes`) in `config.yaml`, read live on every check tick so a change needs no restart. |
| PWA | included | Settings → General → Autonomous gained the three fields (new labels in all five locales); the alert, audit action, task decision and rule/template entries render on existing PWA surfaces. |
| Android | excluded | No app code lives in this repo. Capability parity is required and is tracked by this draft: config round-trip, alert delivery, `session_end_reap` audit/decision rendering and the new lint-rule and template entries — implementation parity is not. |
| iPhone/iOS | excluded | Same as Android: no code in this repo, and the same four items are tracked here for the native client. |
