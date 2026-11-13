# Parity: finished Automata shown as Completed and the completed migration (datawatch v9.170.0)

Parent release: datawatch v9.170.0

## What changed in datawatch

An Automaton whose work was merged now displays as Completed everywhere, in counts, list and detail views, the chip, the legend and the filter badges, and in the CLI and chat text output, instead of a separate Shipped status word. A record that carries a shipped record counts under Completed together with the Automata that completed normally, and never under Cancelled. The merge commit, release and merge time stay available as detail fields, and `status=shipped` is still accepted on the list as a way to select records that carry a shipped record. A new built-in post-ship step, `mark_completed`, sets the Automaton to Completed after its merge instead of cancelling it. A migration restores Automata (and Automations) that were cancelled only because their work had merged, and leaves an Automaton cancelled without merged work alone.

## New route

`POST /api/autonomous/completed/migrate` with `dry_run`, `limit` and `id` taken from the query or a JSON body (the query wins). Also `datawatch autonomous completed-migrate [--dry-run] [--limit N] [--id ID]`, the `autonomous_completed_migrate` MCP tool and a chat verb.

## What the mobile app needs

- Show an Automaton with a shipped record as Completed in the status chip, counts, legend and filters; never as Cancelled or a separate Shipped state. Keep the merge commit, release and merge time in the detail.
- Keep accepting `status=shipped` on the list filter as a selector for records that carry a shipped record.
- Show the built-in post-ship step `mark_completed` in the post-ship step list, with its new label.
- One new web string, `post_ship_builtin_mark_completed` ("Mark the Automaton Completed (mark_completed)"), in all five locales.
