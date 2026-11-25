# [sync] Automaton ids are checked before they are issued, and a new startup warning alert

Draft only. No issue has been filed on any tracker.

## Affected area

The datawatch-app clients (Android and iOS).

## What changed in the daemon

- The daemon draws an id again when a record, an `automaton/` branch or a
  worktree folder already holds it. No request, response or setting changed, and
  ids keep the same eight-character form.
- At start the daemon may raise one `warn` system alert titled
  "An Automaton id was issued more than once", with one line per affected record
  (at most 20 lines; the daemon log lists all).

## What the app needs

Nothing new to build: the alert arrives through the existing alert list and
notifications. Check that a multi-line alert body of about 20 lines reads well
on a phone. A client must not assume an id was never used by an older record.
