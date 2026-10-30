**Title:** [sync] feat(automaton): security failure report route and PWA card
**Issue:** https://github.com/dmz006/datawatch-app/issues/306
**Number:** 306

## Issue text
The datawatch daemon has a new read-only REST route, `GET /api/autonomous/security-failures`, which the PWA renders as a read-only security failure card in the Automation view, beside the triage record card. The Android and iPhone clients must read the same route and render the same card. Capability parity is required; implementation parity is not — each platform uses idiomatic delivery.

## The route

`GET /api/autonomous/security-failures` — read-only, GET only (any other method is 405; a 400 names the allowed status values for a bad `status` filter). Requires the autonomous read capability (`federation.CapAutonomousRead`) for federated peers. When autonomous is disabled the endpoint answers 503 (`autonomous disabled`) — hide the card in that case, never show a stale state.

Optional query parameters (both are server-side filters; a read that matches nothing is 200 with an empty list, never a 404):

- `repo` — narrow the list to one repository path
- `status` — narrow the list to one watch status: one of `green`, `failing`, `pending`, `hung`, `rerunning`, `superseded`

### Body shape

A JSON list, newest first (last-updated first). Each entry is one repo CI watch run that carries a stored security failure report:

| Field | Meaning |
|---|---|
| `repo` | The repository path (e.g. `dmz006/datawatch`) |
| `ref_kind` | Kind of ref watched: `main` or `tag` |
| `ref_name` | The branch name or tag |
| `workflow` | The workflow file name the run belongs to |
| `run_id` | The CI run ID |
| `run_url` | The run's URL |
| `status` | The run's watch state: `green`, `failing`, `pending`, `hung`, `rerunning` or `superseded` |
| `report` | The stored security failure report (below) |

### `report` fields

| Field | Meaning |
|---|---|
| `dependency` | The vulnerable dependency |
| `advisory_ids` | The advisory identifiers (list of strings) |
| `workflow`, `run_id`, `run_url`, `sha` | The run the report was made for |
| `published_at` | The advisory publication date; empty when unknown |
| `fixed_version` | The fixed version when one is known; empty otherwise |
| `registry_found` | `true` when the advisory has an entry in the risk registry |
| `registry_id` | The registry entry identifier |
| `registry_first_added` | The registry entry's `first_added` date |
| `registry_added` | The registry entry's `added` date |
| `registry_expires` | The registry entry's `expires` date |
| `registry_error` | Why the registry could not be read; empty when it was read |
| `lookup_error` | Why the advisory lookup failed; empty when it did not |
| `recommendation` | One of `upgrade`, `keep`, `keep_no_entry`, `unknown` |
| `note` | A free-text note; empty when there is none |
| `mode` | The security failure mode in effect: `off`, `shadow` or `on` |
| `action` | `stopped` (the Automaton line was stopped), `would_stop` (shadow mode: it would have been stopped) or `report_only` |
| `assessed_at` | When the report was made |

## The card (the contract to match)

The PWA card is read-only: it has no control that acts. One card per list entry, showing:

- the run identity line: repository, ref and workflow, with the run linked to `run_url` when it is an http(s) URL
- the dependency
- the advisory identifier(s), comma-separated
- the publication date, or `unknown` when `published_at` is empty
- the fixed version, only when one is known
- the registry entry: its id with the `first_added`, `added` and `expires` dates (each `unknown` when empty), or `not in the registry` when `registry_found` is false (with the reason appended when `registry_error` is set)
- the advisory lookup failure, only when `lookup_error` is set
- the recommendation
- the mode
- the action taken: `Action taken: stopped the Automaton line`, `Would have taken (shadow mode): stop the Automaton line` for `would_stop`, or `Action taken: none, report only`
- the note, only when one is set

An empty list shows an empty state; a failed read shows a retry banner. The PWA polls every 5 s with the same loader as the triage card.

## English keys

Every string is a locale key; the English values are the placeholders until translations arrive (the same keys exist in the de, es, fr and ja bundles of the daemon with the English value):

| Key | English value |
|---|---|
| `automation_security_failure_title` | Security failure |
| `automation_security_failure_empty` | No security failure reports. |
| `automation_security_failure_failed` | Could not load the security failure reports: %1$s |
| `automation_security_failure_report_empty` | This run carries no security failure report. |
| `automation_security_failure_unknown` | unknown |
| `automation_security_failure_run` | Run %1$s |
| `automation_security_failure_dependency` | Dependency: %1$s |
| `automation_security_failure_advisory` | Advisory: %1$s |
| `automation_security_failure_published` | Published: %1$s |
| `automation_security_failure_fixed_version` | Fixed version: %1$s |
| `automation_security_failure_registry` | Registry entry: %1$s (first added %2$s, added %3$s, expires %4$s) |
| `automation_security_failure_registry_none` | Registry entry: not in the registry |
| `automation_security_failure_registry_unreadable` | Registry entry: not in the registry (the registry could not be read: %1$s) |
| `automation_security_failure_lookup_failed` | Advisory lookup failed: %1$s |
| `automation_security_failure_recommendation` | Recommendation: %1$s |
| `automation_security_failure_recommend_upgrade` | upgrade to the fixed version |
| `automation_security_failure_recommend_keep` | keep, the registry holds an entry |
| `automation_security_failure_recommend_keep_no_entry` | keep, but add a registry entry |
| `automation_security_failure_recommend_unknown` | unknown |
| `automation_security_failure_mode` | Mode: %1$s |
| `automation_security_failure_action_stopped` | Action taken: stopped the Automaton line |
| `automation_security_failure_action_would_stop` | Would have taken (shadow mode): stop the Automaton line |
| `automation_security_failure_action_report_only` | Action taken: none, report only |
| `automation_security_failure_action_other` | Action taken: %1$s |
| `automation_security_failure_note` | Note: %1$s |

## Acceptance criteria for mobile

- Both the Android and iPhone clients read `GET /api/autonomous/security-failures` and show one read-only card per entry on the screen where the triage record card appears, matching the PWA card contents above.
- The card has no control that acts: no stop, retry, dismiss or registry edit.
- The publication date reads `unknown` when absent; the registry line reads `not in the registry` when `registry_found` is false and shows the three dates (each `unknown` when absent) when it is true.
- Shadow mode (`action: would_stop`) shows the would-have-taken line, and never the action-taken line.
- When the endpoint answers 503 or the caller lacks `CapAutonomousRead`, the card is hidden and no stale state is shown.
- The `repo` and `status` filters are honored (or exposed as such) and a no-match read shows the empty state, not an error.
- Every string uses the keys above, with no hard-coded English; the English values stand in until the translations arrive.

Parent: datawatch `feat(automaton): security failure report card` (the `GET /api/autonomous/security-failures` route and the Automation view card).
