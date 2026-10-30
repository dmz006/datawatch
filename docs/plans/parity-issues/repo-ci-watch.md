**Title:** [sync] feat(automaton): repo CI watch status and records
**Issue:** https://github.com/dmz006/datawatch-app/issues/303
**Number:** 303

## Issue text
The datawatch daemon has a new read-only REST route, `GET /api/autonomous/repo-ci-watch`, which the PWA renders as a read-only status line on the Automaton view. The Android and iPhone clients must read the same route and render the same state. Capability parity is required; implementation parity is not — each platform uses idiomatic delivery.

## The route

`GET /api/autonomous/repo-ci-watch` — read-only, GET only (any other method is 405; a 400 names the allowed status values for a bad `status` filter). Requires the autonomous read capability (`federation.CapAutonomousRead`) for federated peers. When autonomous is disabled the endpoint answers 503 (`autonomous disabled`) — hide the UI in that case, never show a stale state.

Optional query parameters (both are server-side filters on `records` only; a read that matches nothing is 200 with an empty list, never a 404):

- `repo` — narrow `records` to one repository path
- `status` — narrow `records` to one watch status: one of `green`, `failing`, `pending`, `hung`, `rerunning`, `superseded`

### Body shape

```json
{
  "mode": "off | shadow | on",
  "alert_minutes": 0,
  "repos": ["/absolute/repo/path"],
  "last_poll_at": "2026-01-01T00:00:00Z",
  "records": [ ... ]
}
```

| Field | Meaning |
|---|---|
| `mode` | The watch mode in effect: `off` (default), `shadow` or `on` |
| `alert_minutes` | The alert threshold in effect in whole minutes; `0` while the watch is off |
| `repos` | The configured repository paths; empty means nothing is watched |
| `last_poll_at` | The instant the coordinator last polled; the zero time when it never has |
| `records` | The stored watch records, newest first (last-updated first regardless of the store's order) |

### `records[]` fields

| Field | Meaning |
|---|---|
| `repo` | The repository path (e.g. `dmz006/datawatch`) |
| `ref_kind` | Kind of ref watched: `main` or `tag` |
| `ref_name` | The branch name or tag |
| `sha` | The commit the run executed against |
| `workflow` | The workflow file name the run belongs to |
| `run_id` | The CI run ID (the record's key) |
| `run_url` | The run's URL (omitempty) |
| `status` | The run's watch state: one of `green`, `failing`, `pending`, `hung`, `rerunning`, `superseded` |
| `classification` | The failure's resolved category (omitempty): one of `code_or_test`, `infra`, `hung`, `security`, `policy`, `judging`, `none` |
| `first_seen_at` | When the run was first observed under watch |
| `updated_at` | Last update instant |
| `alerted_at` | Set once a later step marks the record alerted (omitempty) |
| `rerun_log` | One entry per automatic cancel or rerun (omitempty) |
| `would_do` | Shadow-mode log of what the watch would have done (omitempty) |
| `superseded_run_ids` | The run's superseded predecessors, each with its run URL (omitempty) |
| `notes` | Append-only annotation list (omitempty) |
| `judge_verdict` | The judge's verdict cached for the run (omitempty) |

## Config keys (for reference)

Under `autonomous.automation.repo_ci_watch.*` (read from the daemon config; the PWA settings screen lists them, and the Android/iPhone clients should mirror them if they expose settings for this feature):

| Key | Meaning |
|---|---|
| `.mode` | `off` (default), `shadow` or `on` |
| `.repos` | Absolute repository paths to watch (comma-separated); empty = nothing watched |
| `.poll_seconds` | Poll interval in seconds (min 60, default 300) |
| `.alert_minutes` | Alert threshold in minutes (0 = off, default 30) |
| `.tag_count` | Newest-release-tags watched (0 = none, default 5) |
| `.hung_after_minutes` | Hung-run threshold (min 1, default 60) |
| `.security_workflows` | Workflow names watched for security signals (empty list = none) |
| `.delete_superseded` | `report` (default) or `delete` |

## PWA status line + alert (the contract to match)

- Status line (element `#repoCIWatchStatusLine`, `role="status"`) reads `GET /api/autonomous/repo-ci-watch` and renders a single line combining: the mode (`off` / `shadow` / `on`), the count of currently `failing` records and the count currently `pending` records (or `no failing or pending runs` when both are 0), and a relative `last poll <age>` (never / Ns / Nmin / Nh / Nd). The line is **hidden** when the endpoint is unavailable — never a stale state. Poll cadence: 30 000 ms (same as the Automaton list loader).
- Alert: one alert per non-green run once it has been non-green for longer than `alert_minutes` (the effective threshold; while `mode` is `off` the threshold reads `0` and no alert is raised). The alert is raised by the coordinator and stamped on the record via `alerted_at`.

## What to do in the Android and iPhone clients

Mirror the PWA status line: read `GET /api/autonomous/repo-ci-watch` on the same surface where the PWA shows it (the Automaton view), render the same mode + failing/pending counts + last-poll line, hide it when the endpoint is unavailable (503 / capability missing), and surface the same alert. Use each platform's idiomatic delivery (e.g. APNs for the alert on iOS, matching the existing device-token registration). Capability parity is required; implementation parity is not.

## Acceptance

- Both the Android and iPhone clients read `GET /api/autonomous/repo-ci-watch` and render the mode + failing/pending + last-poll status line, matching the PWA.
- When the endpoint is unavailable or the caller lacks `CapAutonomousRead`, the status line/section is hidden (no stale state shown).
- The `repo` and `status` query filters are honored (or exposed as such) and a no-match read shows an empty state, not an error.
- The alert (one per non-green run past `alert_minutes`) is received and displayed on both clients; `alerted_at` is respected so it fires once.

Parent: datawatch `feat(automaton): repo CI watch status and records` (the `GET /api/autonomous/repo-ci-watch` route).
