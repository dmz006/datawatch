# Automata Events

The **Events** tab on the Automata page is one stream of what your Automata
and Automations are doing: watch notices, supervisor decisions, infrastructure
retries, checks, cleanup, alerts and errors.

## Reading the stream

- Each row shows the time, the event kind, the project, and the owner: an
  **Automation** (the parent) and/or an **Automaton** (the child).
- Click a row to expand its detail, reason and next action.
- A **quiet** entry is a routine notice shown as one muted line with its reason.
  It needs no action.
- An **error** entry is shown as an alert with what happened, the reason, and a
  **Next action** button that opens the place where you fix it.

## Filtering

- **Kind** chips toggle one or more event kinds on and off.
- **Project** narrows the list to one project; *All projects* clears it.
- **Search events** matches the text of the summary and reason.
- **Load more** fetches older events.

## Moved cards

These cards now live in the Events stream instead of their own panels:

- Supervisor watch and recent decisions
- Infrastructure retries and Automata checks
- Safe to delete and cleanup status

Each card's **?** link opens this page.
