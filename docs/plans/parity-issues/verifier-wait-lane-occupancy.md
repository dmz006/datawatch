# feat(automation): verifier-wait lane state in the lane fill status view

Parent release: the next minor above main at the time of writing (set at merge time).
Plan: [`docs/plans/2026-10-29-verifier-wait-lane-occupancy.md`](../2026-10-29-verifier-wait-lane-occupancy.md).

## What changed in datawatch

A Claude lane whose Automaton is only waiting for verifier results now counts as available in
the lane fill planner's count, so queued work can start on that lane instead of sitting behind
it (lane fill stays in shadow mode by default, so the planner records what it would start).
Every lane view carries the state: `GET /api/autonomous/lane-fill` lane entries gain a
`wait_reason` field (`"verifier"` when the occupant's work is waiting on a verifier, absent
otherwise) and an `available` flag (whether the planner counts the lane as available for the
next start). The idle-lane alert for a lane counted available this way fires once per idle
episode, not once per poll. A new setting, `autonomous.lane_fill.count_verifier_wait`
(default `true`), turns the counting off again.

## REST contract (what the app already consumes plus the new fields)

| Item | Value |
|---|---|
| Endpoint | `GET /api/autonomous/lane-fill` (existing) |
| New lane fields | `wait_reason`: string, present and `"verifier"` when the occupant's work is all waiting on a verifier; absent otherwise. `available`: boolean, the planner's view of the lane under the current settings. |
| Alert behaviour | the existing `alerts[]` entry for an idle-with-work lane is emitted once per episode, not per poll; the episode ends when the lane stops counting available |

## Setting to add to the mobile app

| Item | Value |
|---|---|
| Config key | `autonomous.lane_fill.count_verifier_wait` |
| REST field | `count_verifier_wait` under `autonomous.lane_fill` in `GET`/`PUT /api/config` |
| Label | Count lanes waiting for a verifier as available |
| Type | boolean |
| Default | `true` (default; flip to `false` to make such lanes count as busy again) |
| Placement | the lane fill settings fields, next to `mode`, `claude_lanes` and `idle_alert_minutes` |

## New locale keys

| Key | EN | DE, ES, FR, JA (placeholder) |
|---|---|---|
| `lane_fill_waiting_verifier` | Waiting on verifier | Waiting on verifier |
| `lane_fill_lane_available` | Available to the planner | Available |
| `settings_count_verifier_wait` | Count lanes waiting for a verifier as available | Count verifier-wait lanes as free |

The DE, ES, FR and JA bundles carry an English placeholder, as the Localization Rule
requires. The placeholder avoids the word "Automaton" because the Terminology Rule guard
(`TestLocales_AutomatonNeverUntranslatedOrMistranslated`) refuses that English word in those
bundles. Please send translations of all three keys for DE, ES, FR and JA through the
Compose Multiplatform pipeline, using the app's own word for "Automaton"; the PWA bundles
will then mirror them in place of the placeholders.

## Acceptance

- The lane fill status view shows, per lane, the occupant, the `wait_reason` label when the
  lane is waiting on a verifier, and the `available` flag read from `GET /api/autonomous/lane-fill`.
- The `count_verifier_wait` field appears in the app's lane fill settings, reads the current
  value from the daemon config and writes it back through the config endpoint.
- The idle-lane alert renders once per episode (one notification per idle period, matching the
  PWA alert list).
- Label and help text are localized in EN, DE, ES, FR and JA.
- Unknown fields in the lane fill status response are ignored (forward compatibility), as for
  the existing lane fill view.

## Filing record

Draft — not filed. The supervisor files the single combined datawatch-app issue for Android
and iPhone at merge, from this text.
