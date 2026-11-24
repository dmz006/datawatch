# Closed Automata stay closed: what the mobile apps may see

**Not filed.** Draft only.

## Affected area

No new setting, endpoint, field or screen. The daemon now refuses recovery on a
closed Automaton (cancelled, rejected or archived, or with a recorded closed
outcome). The Android and iPhone apps may see:

- three new decision-log kinds on an Automaton: `closed_record_restored`,
  `closed_outcome_cleared` and `closed_record_answer_ignored`;
- an Automaton moving from running back to cancelled, rejected, archived or
  completed after a daemon restart;
- the operator's task reset or reset to draft clearing `closed_outcome`,
  `delivered_via`, `closed_outcome_note` and `closed_outcome_at`.

## What to check

- The decision log shows an unknown kind as plain text with its note.
- A task retry or reset sent by the app carries the actor `operator` (or none).
  Any other actor label is refused on a closed Automaton.
- The outcome badge disappears after a reopen.
