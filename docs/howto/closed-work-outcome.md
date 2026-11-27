---
docs:
  index: true
  topics: [autonomous, automaton, archive, outcome, delivered, superseded, dropped, held, deploy, shipped, check, hold]
exec_params:
  - {name: id, required: true, description: "The finished Automaton to label"}
  - {name: outcome, required: true, description: "delivered, superseded, dropped or held"}
  - {name: delivered_via, required: false, description: "The twin Automaton that delivered the work, or the one that replaced it"}
  - {name: note, required: false, description: "Why, in a sentence"}
exec_steps:
  - tool: autonomous_prd_set_outcome
    description: Record how a finished Automaton ended
    args:
      id: "{{params.id}}"
      outcome: "{{params.outcome}}"
      delivered_via: "{{params.delivered_via}}"
      note: "{{params.note}}"
    read_only: false
  - tool: autonomous_outcome_check
    description: Read the standing check of closed work
    args: {}
    read_only: true
---
# How-to: Say how finished work ended

"Archived" used to mean four different things: the work was delivered, a twin
Automaton delivered it, a newer Automaton replaced it, or it was thrown away. An
Automaton now carries its own **outcome class**, and the daemon keeps every
finished Automaton labelled and every deploy on record. This guide shows the
four classes, what the daemon fills in by itself, what it refuses, and how to
read the standing check that finds what is missing.

None of this depends on a model. The daemon records and refuses by its own
rules.

## The four outcomes

| Outcome | Meaning |
|---|---|
| `delivered` | The work is on the base branch: by this Automaton's own shipped record, or by a twin named in `delivered_via` (the twin must be a stored Automaton with a shipped record) |
| `superseded` | A newer Automaton replaced it; `delivered_via` names the replacement (required) |
| `dropped` | Thrown away on purpose: a throwaway, a duplicate, work no longer wanted |
| `held` | Waiting by decision. Held work is never archived |

Archived is only a storage state that comes after delivered, superseded or
dropped.

A cancelled, rejected or archived record with outcome **delivered** reads
**Completed** in the progress numbers, the timeline, the list's Completed filter
and the web app's status chip, also with no shipped record (work without code).
Its stored status does not change.

## What the daemon fills in

Cancel, reject and archive record the outcome in the same step:

- an Automaton with a shipped record (release and merge commit) is `delivered`;
- a rejected Automaton is `dropped`;
- a cancelled Automaton whose work was left unfinished is `dropped`.

Nothing else is guessed. A completed Automaton with no shipped record, or a
cancelled one whose tasks all finished but never shipped, has no provable
outcome: you say what happened to it.

## What the archive step refuses

With `autonomous.closed_outcome_archive` on (the default), the archive step
refuses:

- a finished Automaton with no outcome that the daemon cannot prove from its
  record;
- an outcome of `held`, or a hold that is not met (a hold record not released
  and not expired by its own time);
- an Automaton whose own spec declares a hold (the capital word HOLD, "do not
  approve", "do not run", "held until", "until the operator says", "needs
  explicit operator go-ahead") and which has no recorded outcome. Recording any
  outcome resolves it: that is your decision. A shipped record resolves it too.

A separate guard also refuses to archive or cancel an Automaton whose branch
holds commits that are not on the default branch until a keep or drop decision
is recorded for its landing; see [finished-work.md](finished-work.md).

It never archives running, planning, paused, held or queued work, whoever asks.
The supervisor loop's close step counts the records it kept for want of an
outcome in its detail line (`kept N with no outcome or an unmet hold`) and does
not raise an error for them; each refusal is written once to the Automaton's
decision log as `closed_outcome_refused`.

## Closed records stay closed

A record is **closed** when it is cancelled, rejected or archived, or when it
carries any recorded outcome, whatever its status reads. The daemon's own
recovery never touches a closed record:

- the restart pass does not release its protected-paths holds, reset its tasks,
  plan, resume, recover or park it;
- the periodic passes (boot reconcile, stuck-running watchdog, merge retry,
  verifier and judge reconcile, held questions, the housekeeping repairs, gate
  repair, the supervisor loop's retry and the security failure stop) skip it;
- lane fill never lists it, starts it or plans it;
- an answer to one of its questions is not applied, and the record says so once
  (`closed_record_answer_ignored`);
- a task reset or task review by anything other than you (a repair, a loop, a
  session with its own credential, a federation peer) is refused;
- no start door runs a record that carries an outcome, whatever
  `autonomous.shipped_refusal` says.

If a record carries an outcome but reads **running**, the daemon puts its closed
status back (from the record's own cancel, reject or archive entry), ends the
worker sessions that reopening started, and records `closed_record_restored`
with the reason. It does this before boot resume and on every poll.

Only you reopen a closed record: your own task reset (web app, CLI, chat or the
MCP tool without a session credential), reset to draft, or the force past the
shipped-work rule. Each one clears the outcome and records
`closed_outcome_cleared` with what it was. There is no setting that turns this
rule off.

One exception to "never touched": the one-time unarchive repair still looks at
an older archived record that has **no** outcome, and only moves it to another
finished status.

## Record an outcome

| Surface | How |
|---|---|
| CLI | `datawatch autonomous prd-set-outcome <id> --outcome delivered --via <twin> --note "..."` (add `--archive` to archive in the same step) |
| MCP | `autonomous_prd_set_outcome` (`id`, `outcome`, `delivered_via`, `note`, `archive`) |
| Chat | `autonomous outcome <id> outcome=delivered via=<twin> archive=true note=<text, last>` |
| REST | `POST /api/autonomous/prds/{id}/outcome` with `{outcome, delivered_via, note}`; the same body on `POST .../archive` and `POST .../cancel` records it in that step |
| Web app | The outcome chip on the Automaton shows the class and, on hover, the twin and the note |

Cancel takes the same fields: `datawatch autonomous prd-cancel <id> --outcome
superseded --via <id>`, the `autonomous_prd_cancel` MCP tool, and
`autonomous cancel <id> outcome=... via=...` in chat.

## The deploy record

The deploy-after-ship step writes a **deploy record** onto every Automaton
shipped in the release it deployed: the version, the time, the host and the
health check result (`passed`, `failed`, or `unchecked` when no health check
ran). If you deploy by hand or with your own script, write it yourself:

```
datawatch autonomous deploy-record --release v1.2.0 --host build-1 --health passed
```

The MCP tool is `autonomous_deploy_record`, the chat verb is
`autonomous deploy-record release=v1.2.0 host=build-1 health=passed`, and the
REST route is `POST /api/autonomous/deploy/record`. It is idempotent for the
same deploy and never changes a status.

## The standing check

`autonomous.closed_outcome_check` sets the mode: `shadow` (the default) records
findings for the read, `on` also raises one alert per finding, `off` does
nothing. The check reads only; it never changes an Automaton. It runs live
whenever you read it, and once a day on the supervisor loop's schedule it also
re-runs the intent proof. What it flags:

| Finding | Meaning |
|---|---|
| `closed_no_outcome` | Rejected, cancelled or archived with no outcome |
| `completed_no_shipped` | Completed with no shipped record and no outcome |
| `shipped_no_deploy` | Shipped, with no deploy record (or a failed one), on a host that deploys after ship; a merge is given two hours before this counts |
| `deployed_older_than_shipped` | The deploy record's version is older than the shipped release |
| `daemon_older_than_shipped` | The running daemon is older than the newest shipped release |
| `intent_unproven` | Closed as delivered with no shipped twin, but the files and symbols its spec names are not all on the base branch, or tasks are unfinished, or the spec names nothing to look for |

The intent proof is the named-evidence check: every file path and symbol the
spec names must be on the base branch and every task complete. It looks at up
to 500 records a run.

Read it with `datawatch autonomous outcome-check` (`--run` runs it now),
the `autonomous_outcome_check` MCP tool (`run=true` to run), `autonomous
outcome-check` in chat, `GET /api/autonomous/outcome-check` (`POST` runs it), or
the **Closed work check** card on the Automata Housekeeping sub-tab.

## Settings

| Key | Values | Default |
|---|---|---|
| `autonomous.closed_outcome_archive` | `on`, `shadow` (only logs what it would refuse), `off` | `on` |
| `autonomous.closed_outcome_check` | `shadow`, `on`, `off` | `shadow` |

Both apply live. Set them with `datawatch config set`, `PUT /api/config`, the
MCP `autonomous_config_set` tool (keys `closed_outcome_archive`,
`closed_outcome_check`), the chat `configure` verb, or Settings, Automata.
