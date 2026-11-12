# feat(automation): surface held pending inserts + Approve control on mobile

Parent release: datawatch 9.33.0 (the next patch above main at the time of writing).

## What changed in datawatch

An Automation whose decision assistant (or an agent) asks to insert a member that is
not yet part of the train no longer blocks the chain silently. The insert is held as
a *pending insert* on the Automation: the daemon records the member id, the PRD id
of the Automaton to insert, who asked for it and why, and the chain keeps going on
its existing members. Operators can see every held insert — member id, Automaton
PRD id, requester, reason — on the Automation's status/detail surface, and a single
Approve control inserts the held member where it is editable (Automation in one of
its editable statuses, e.g. `active`). Finished Automations show the same rows in a
read-only form: the ids stay visible but no control is offered. In parallel, the
loop-status row now words the daemon-side waiting count ("waiting: N") for the
operator instead of a bare number.

## What to add to the mobile app

| Item | Value |
|---|---|
| Where | Automation status/detail view (the same surface that shows the loop status row) |
| REST source | `pending_inserts[]` on each Automation's status entry (e.g. `GET /api/autonomous/status` `automations[]`), each element `{id, prd_id, name, actor, reason, status}` |
| Render | one row per held insert: `Member <id>` · `Automaton <prd_id>` plus the requester (if present) and the reason, both as plain escaped text |
| Approve control | a single Approve action per row, only while the Automation is editable (one of `draft`, `active`, `paused`, `blocked`); tap posts the REST control `approve_insert` (CLI equivalent: `datawatch automation approve <id> <member-id>`); hidden on finished Automations |
| Status row | the existing loop-status line already flows from the same `automations[]` entry; no change needed beyond keeping the new `waiting: N` wording |

Empty `pending_inserts` (or the key absent) means: render nothing for that block.

## New locale keys

| Key | EN | DE, ES, FR, JA (placeholder) |
|---|---|---|
| `automation_pending_inserts` | Pending inserts | Pending inserts |
| `automation_pending_insert_by` | Requested by %1$s | Requested by %1$s |
| `automation_pending_insert_ids` | Member %1$s · Automaton %2$s | Member %1$s · %2$s |
| `automation_approve_insert` | Approve insert | Approve |

The DE, ES, FR and JA bundles carry an English placeholder, as the Localization Rule
requires. The placeholder for `automation_pending_insert_ids` deliberately omits the
word "Automaton" because the Terminology Rule guard
(`TestLocales_AutomatonNeverUntranslatedOrMistranslated`) refuses that English word
in those bundles. Please send translations of all four keys for DE, ES, FR and JA
through the Compose Multiplatform pipeline, using the app's own word for "Automaton"
(De: Automat, Es: autómata, Fr: automate, Ja: オートマトン); the PWA bundles then
mirror them in place of the placeholders.

## Acceptance

- A held insert is visible on the Automation's status/detail view with its member id,
  Automaton PRD id, requester (when present) and reason, rendered as plain text (no
  HTML interpretation of any field).
- While the Automation is editable, Approve inserts the held member exactly once;
  on finished Automations the row is read-only.
- An empty `pending_inserts` list renders no block.
- Label and rows are localized in EN, DE, ES, FR and JA.

## Filing record

Filed: https://github.com/dmz006/datawatch-app/issues/300 (2026-10-29, via `gh issue create --repo dmz006/datawatch-app`).
