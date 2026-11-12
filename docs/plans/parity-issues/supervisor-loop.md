# feat(automation): Supervisor loop card, Settings group and run record on Android and iPhone

Draft for the datawatch-app repository. It is filed at merge time, not by the task that wrote it.

## What changed in datawatch

The daemon now runs a supervisor loop that keeps every Automaton moving. Each pass runs
mechanical steps (no model) and three model jobs (`draft_spec`, `classify_ci_failure`,
`pick_backend`), and keeps a run record for each. The web app gained a **Supervisor loop**
card in the Automation view and a **Supervisor loop** group in Settings → Automata. The
how-to is `docs/howto/supervisor-loop.md`; each setting has a help link into
`docs/datawatch-definitions.md#supervisor-loop`.

## What to add to the mobile app

### 1. The card (Automation view)

| Item | Value |
|---|---|
| Where | Automation view, a card alongside the other Automation cards |
| Jobs source | `GET /api/supervisor-loop/jobs` → `{enabled, jobs[]}`; each job `{name, kind, style, backend, model, enabled, last_outcome, last_run}` |
| Runs source | `GET /api/supervisor-loop/runs?limit=N` (filters `job`, `outcome`) → `{runs[]}` |
| Render | Jobs list: name, style, backend, model, last outcome, escalation count. Recent runs underneath, newest first. A link to the how-to. |
| Off state | When `enabled` is false show "The supervisor loop is switched off." |
| Manual run | Optional: `POST /api/supervisor-loop/jobs/<name>/run` runs one job now; an unknown name is a 404 |

### 2. The Settings group (Settings → Automata → Supervisor loop)

Every key is `autonomous.supervisor_loop.<key>`, read from `GET /api/config` and written with
`PUT /api/config`. Each field has a help link to its entry in the definitions page.

| Control | Key | Default |
|---|---|---|
| Toggle | `enabled` | on |
| Toggle ×8 | `<step>.enabled` for `close_finished_records`, `deploy_after_ship`, `retry_failed_infra`, `cleanup_sweep`, `apply_answers`, `lane_count`, `start_next_chain_step`, `run_merge_until_conflict` | on |
| Select ×3 | `<job>.style` (`oneshot`, `session`) for the three model jobs | `oneshot` |
| Text ×6 | `<job>.backend` and `<job>.model` | empty (uses the decision assistant's) |
| Number | `validator_retries` (0 or more) | 2 |
| Number | `escalate_after_failures` (1 or more) | 2 |
| Number | `run_retention` (1 or more) | 500 |
| Number | `lane_count.min` (0 or more) | 0 |
| Number | `lane_count.max` (0 = no bounds, otherwise at least min) | 0 |

### 3. The run record

Each run shows: job, style, backend, model, outcome (`ok`, `validator_failed`, `escalated`,
`error`), result text, validator failures (a list of plain-language reasons), retries,
escalations, duration in milliseconds and the start time. Render the failure reasons as plain
escaped text. Show retries and escalations only when above zero.

### 4. The Monitor card (Monitor view)

| Item | Value |
|---|---|
| Where | Monitor view, a stat card beside the Supervisor watch and Burn guard cards |
| Source | the `supervisor_loop_*` fields of the stats snapshot (WebSocket stats broadcast), or `GET /api/supervisor-loop/stats` → `{enabled, jobs, jobs_enabled, runs, runs_ok, runs_validator_failed, runs_escalated, runs_error, retries, last_run_age_seconds}` |
| Render | State (on or off), jobs enabled as `n / total`, runs stored, the four outcome counts, retries used, last run age in seconds (a dash when -1). Escalated and error counts above zero use the warning colour. |
| Hidden | Left out until the snapshot reports a job count |
| Strings | the twelve `stats_supervisor_loop*` keys in all five locale bundles |

## New locale keys

Use the `supervisor_loop_*` keys already in the web app's `en.json` (group title and hint, card
title, how-to link, off state, the Jobs and Recent runs headings, the column names, and one label
per setting). The DE, ES, FR and JA bundles carry translations; reuse them.

## Acceptance

- The card shows the jobs and recent runs and refreshes while the Automation view is open.
- The Monitor card shows the loop's numbers and drops out when the daemon reports none.
- Every setting reads and writes through the config routes and survives an app restart.
- Android and iPhone/iOS behave the same.
