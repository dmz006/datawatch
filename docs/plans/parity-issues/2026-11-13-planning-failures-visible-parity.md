# Parity: visible and self-correcting Automaton planning failures (datawatch v9.169.0)

Parent release: datawatch v9.169.0

## What changed in datawatch

An Automaton whose planning failed no longer sits as a draft with zero stories and no explanation. The failure is recorded with its reason, category, the rule that fired, the phrases involved and its retry counts and budget. It shows on the Automaton detail, as a "Planning failed" badge in the list, and through the REST, MCP, CLI and chat reads. A non-retryable failure raises one alert and a question waits with the choices "Fix the wording and plan again" and "Leave it". A spec check lints the spec text when an Automaton is created or revised and reports wording the planner may copy into task titles (fixed release numbers, tracker identifiers, private addresses, home paths); it is a warning only and never blocks the request. When a fixed release number is the only problem in a draft, the daemon rewords it and plans again.

## New config key

Returned and accepted by `GET` and `PUT /api/config` as a dotted key, and shown in Settings > Automata.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `autonomous.housekeeping_repairs.planning` | `off`, `report` or `on` | `report` | What the housekeeping repairs do with a draft Automaton whose planning failed and will not clear by retrying. |

## New response fields

- `planning_failure` on `GET /api/autonomous/prds/{id}` and on each row of `GET /api/autonomous/prds`: `reason`, `category`, `source`, `phrases`, `retryable`, `retry_counts`, `retry_budget`, `occurred_at`; absent when the last planning attempt did not fail.
- `spec_check` (always present, empty when clean) on the replies of `POST /api/autonomous/prds` and `POST /api/autonomous/prds/{id}/request_revision`; see the existing spec check parity draft for the finding fields.

## What the mobile app needs

- A Planning failed badge in the Automata list and a planning failure block on the Automaton detail (reason, category, rule, phrases, retryable or not, retry counts and budget), with the pending question and its two choices.
- The spec check findings after a create or a revise, as described in the spec check parity draft.
- A Settings field for `autonomous.housekeeping_repairs.planning` and the Planning group in the housekeeping repairs list.
- New web strings in all five locales: `planning_failure_*` (title, reason, category, source, phrases, retryable, not_retryable, retry_counts, retry_budget, badge, question_pending, question_fix, question_leave), `spec_check_*`, `settings_housekeeping_planning`, `settings_housekeeping_planning_help` and `housekeeping_repairs_group_planning`.
