Parent release: datawatch v9.162.0

## What changed in datawatch

Registry rows and findings are now records kept by the daemon. A plan record has a title unique inside its project, a short id, a permanent uid, a state, notes, links and the Automaton that shipped or raised it. They are read and changed on REST (`/api/plan-records/...`), six MCP tools, the `datawatch plan-record` CLI and the `autonomous plan-records` chat verbs. An import reads the old registry table and findings files into records. Automata and Automations carry a read-only `project` field derived from the project link. The web app gained a **Plan records** section on the Projects page, Overview sub-tab.

## New config keys

Returned and accepted by `GET` and `PUT /api/config` as dotted keys, and shown in Settings > Automata.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `autonomous.plan_records.enabled` | boolean | `true` | Master switch. While false every plan-record route answers 404 and the section hides. |
| `autonomous.plan_records.write_through` | boolean | `false` | When on, the post-ship step records the shipped registry row as a plan record. |
| `autonomous.plan_records.import_conflict_policy` | `suffix`, `rename` or `skip` | `suffix` | What an import does with a conflicting title when the call names no policy. Any other value is rejected with a 400 that names the key. |

## What the mobile app needs

- A Plan records section on the project page: Registry rows and Findings views, a state filter, search, a detail panel to change title, state, notes and the linked Automaton, New record, and Import archive.
- Settings fields for the three keys above, in all five locales (the web app added the section and settings strings in all five).
- Show the read-only `project` field in the Automaton and Automation detail.
