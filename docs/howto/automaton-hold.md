---
docs:
  index: true
  topics: [autonomous, automaton, hold, held, operator hold, release, extend, lane fill, housekeeping]
---

# Put an Automaton on hold

An operator can put an Automaton on hold on purpose, for example until after a
release. A held Automaton **waits by decision**: it is not blocked, not failed
and not waiting for review, and nothing starts it. Its tasks and stories are
never touched.

## What a hold records

| Field | Meaning |
|---|---|
| Reason | Required, at most 500 characters after trimming. Say why it waits. |
| Condition | Optional free text shown to the operator ("after v10.0 ships"). It is never evaluated. |
| Look-again time | Optional (`until`). Must be in the future. |
| Held from | The status the Automaton had. A release restores it. For a blocked Automaton the blocked reason is kept and comes back too. |
| Held at, held by | The time and the actor. |

## Operations

- **Hold** is allowed from `draft`, `needs_review`, `approved`, `revisions_asked`,
  `paused`, `blocked` and `failed`. An Automaton that is running, planning,
  completed, cancelled, archived or already held is refused with a message
  naming the allowed statuses.
- **Extend** changes the hold of a held Automaton: a field you give replaces the
  stored one, a field you leave out is kept, and the expiry mark is cleared. The
  status stays `held`.
- **Release** ends the hold and restores the status the Automaton had. It does
  not start anything; the normal paths pick it up from there.

Each operation writes a decision entry (`hold`, `hold_extend`, `hold_release`)
and emits the `prd.held` status event, so lifecycle subscriptions and webhooks
can follow it (see [lifecycle subscriptions](automaton-lifecycle-subscriptions.md)).

## When the look-again time passes

The status does not change. The Automaton stays held, lane fill keeps refusing
it (`operator_hold`) and nothing starts it. Expiry never releases a hold. The
daemon's periodic tick does three things, **once** per hold:

- marks the hold expired (the mark is stored, so a second pass, a daemon restart
  or a reload adds nothing);
- records one `hold_expired` decision entry;
- raises one **warn** alert, "Automaton hold expired", that names the title and
  the hold reason and says to release the hold or give it a new look-again time.
  It is not counted as a blocked or review alert.

The same expiry shows as one warn row in the [Events tab](automata-events.md)
(summary "Hold expired: <title> is ready to review", next action "Release the
hold, or set a new hold-until time"). **Extend** with a new look-again time
clears the mark, so a later expiry raises a new alert; **Release** removes the
row. A hold with no look-again time never expires.

## Where a held Automaton is shown

A held Automaton is its own group, never counted as failed, blocked or waiting.
Expired holds are listed first, then the soonest look-again time, then the oldest
hold. Each line carries the title, the reason, the optional condition and a
look-again line: "looked at again on <date>", "no date set" or "hold expired,
ready to review".

- **Triage** and the **supervisor watch** status show a "Held by the operator"
  group beside the failed and blocked ones. Triage ignores a held Automaton's
  old failure record; it shows again if the hold is released.
- **Progress** counts held Automata in a `held` column, shown only while at least
  one is held. They are not counted as running, waiting or blocked, and the
  "left" line reports them separately. See [Progress](automata-progress.md).
- **Dashboard console**: the *Held & blocked* section does not count held
  Automata as stuck; its summary adds ", N held (not counted as stuck)" so they
  are not forgotten. See [Operator console](dashboard-operator-console.md).
- **Status** reports the count as `held_prds`, never inside `blocked_prds`.
- **Attention lists and the dependency-update report** skip a held Automaton or
  report it as held; it is never listed as waiting for review.

## Moving look-alike Automata onto a hold

The hold migration is for blocked or failed Automata that are really waiting on
purpose. It lists candidates by matching stored text (title, spec, tags, blocked
reason and the newest decision note) against three rules, and runs nothing:

| Rule | Looks like |
|---|---|
| `eval_measurement` | An evaluation, tiering, matrix or benchmark run with a failed task, so the run's own measurement failed. |
| `design_parked` | Blocked design, proposal or mockup work waiting on an operator decision or a `HOLD` line in the spec. |
| `operator_hold_note` | A blocked reason or newest decision note that says hold, parked, until after, or after the release. |

Ordinary failed implementation work matches no rule and is not listed. You
choose the ids (they need not be candidates) and **you supply the reason**; it is
never generated or defaulted. Condition and look-again time are optional and
apply to every chosen id. A dry run, or a run with no ids, only lists. A real
run refuses while a final gate is running, and skips, with a reason, an id that
is missing, already held, or not blocked or failed. Only an operator command
runs it: no loop, housekeeping step or startup path does.

## What leaves a held Automaton alone

- **Every start path** refuses it with an error that names the hold reason:
  approve, run, decompose, reset and requeue. Release first.
- **Lane fill** never starts it. Its eligibility reason is `operator_hold`,
  separate from the older `held` reason (a `HOLD` line in the spec). The detail
  reads "on hold by the operator: <reason>; look again at <time>". See
  [Lane fill](automation-lane-fill.md#why-lane-fill-skips-an-automaton).
- **The executor** stops walking tasks if a hold lands while a task runs: nothing
  new starts, and the status stays `held`.
- **Automation chains** do not start a held member and do not block the
  Automation for it. A release lets a later pass start it.
- **Housekeeping repairs, the supervisor loop, the watchdogs, boot reconcile,
  retries, merge passes and migrations** (other than the operator-run hold
  migration above) never change a held Automaton or its
  tasks. See [housekeeping repairs](automaton-housekeeping-repairs.md#what-the-daemon-never-does).
- **Answering a question** does not resume a held Automaton's tasks.

## Review before relying on it

- The hold migration and the held-group read exist as Go API methods
  (`HoldMigrationCandidates`, `MigrateToHold`) and reads. No REST route, CLI
  command, MCP tool or comm verb for them was confirmed in this change.

- The hold, extend and release operations exist in the Automata engine and the
  Go API. This change set adds **no REST route, CLI command, MCP tool,
  comm verb or settings entry** for them, and the PWA only knows `prd.held` as a
  notify event kind. Until those surfaces ship, a hold cannot be placed from an
  operator surface. Parity for REST, CLI, MCP, comm and the PWA (and the mobile
  app) is still to be planned and filed.
- Whether a held Automaton should appear under its own filter in the Automata
  list, and where the hold reason shows, is a placement decision for the operator.
