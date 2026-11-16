# feat(i18n): add the Automaton landing record keys for parity (datawatch v9.179.0)

Parent release: datawatch v9.179.0

## What changed

Every Automaton branch now has a landing record: pending, landed, kept or dropped. An operator can keep a branch unmerged or drop its work, each with a required reason, and the record shows the state, the commit, who decided and when.

## Settings to add to the mobile app

None.

## REST contract

- `POST /api/autonomous/prds/{id}/landing/keep` and `POST /api/autonomous/prds/{id}/landing/drop`, each taking a required reason.
- The landing record (state, commit, actor, time, reason) is the `landing` field of the Automaton read.

## New locale keys with English values

| Key | English |
|---|---|
| `automata_detail_landing` | Landing |
| `automata_detail_landing_actor` | Actor |
| `automata_detail_landing_at` | Time |
| `automata_detail_landing_commit` | Commit |
| `automata_detail_landing_drop_btn` | Drop (work not merged) |
| `automata_detail_landing_drop_prompt` | Reason the work will not be merged (required) |
| `automata_detail_landing_keep_btn` | Keep (branch not merged) |
| `automata_detail_landing_keep_prompt` | Reason for keeping this branch unmerged (required) |
| `automata_detail_landing_none` | No landing record yet — the Automaton has no work branch to keep or drop. |
| `automata_detail_landing_reason` | Reason |
| `automata_detail_landing_state_dropped` | dropped |
| `automata_detail_landing_state_kept` | kept |
| `automata_detail_landing_state_landed` | landed |
| `automata_detail_landing_state_pending` | pending |

## Screens

- Automata detail: a Landing section showing the state, commit, actor, time and reason, with Keep and Drop actions (a reason is required for each) and a "no landing record yet" state for an Automaton with no work branch.
