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

Nothing changes. The Automaton stays held and is shown as hold expired until you
extend or release it. Expiry never releases a hold.

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
  retries, merge passes and migrations** never change a held Automaton or its
  tasks. See [housekeeping repairs](automaton-housekeeping-repairs.md#what-the-daemon-never-does).
- **Answering a question** does not resume a held Automaton's tasks.

## Review before relying on it

- The hold, extend and release operations exist in the Automata engine and the
  Go API. This change set adds **no REST route, CLI command, MCP tool,
  comm verb or settings entry** for them, and the PWA only knows `prd.held` as a
  notify event kind. Until those surfaces ship, a hold cannot be placed from an
  operator surface. Parity for REST, CLI, MCP, comm and the PWA (and the mobile
  app) is still to be planned and filed.
- Whether a held Automaton should appear under its own filter in the Automata
  list, and where the hold reason shows, is a placement decision for the operator.
