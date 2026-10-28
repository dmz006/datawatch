---
docs:
  index: true
  topics: [schedules, results, dashboard, integrations, subprocess]
exec_params:
  - {name: session_id, required: true, description: "Session posting the result (its full hostname-id or short ID)"}
  - {name: schedule_name, required: false, description: "Schedule name to read the retained history for (schedule_results_list only)"}
exec_steps:
  - tool: post_result
    description: Post a structured result for a session (lands in that schedule's history when the session belongs to a named schedule)
    args:
      session_id: "{{params.session_id}}"
    read_only: false
  - tool: schedule_results_list
    description: Read the per-schedule latest results, or one schedule's full retained history
    args:
      schedule_name: "{{params.schedule_name || ''}}"
    read_only: true
---
# How-to: Result panel for scheduled jobs

Scheduled jobs (shell wrappers, one-shot spawns, long-running daemons)
finish with an exit code and some stdout — and then the information is
gone until someone scrolls logs. The **result panel** gives every
scheduled job a structured place to say what it actually did: a title,
an ok/warn/error status, a summary, metrics, tables, and an optional
markdown report. The daemon keeps the last N results per job (N is
configurable), and the PWA shows them in two places: the session's
**Status tab Result panel** and the opt-in **Integration status**
dashboard card with a sparkline across recent runs.

```
  ┌──────────────┐   spawn    ┌──────────────────────────────┐
  │  operator    │───────────▶│  datawatch daemon             │
  │  (schedule)  │            │  job runs as a subprocess     │
  └──────────────┘            │  env: DATAWATCH_SESSION_ID   │
                              │      DATAWATCH_BASE_URL      │
                              │      DATAWATCH_RESULT_TOKEN  │
                              └──────────────┬───────────────┘
                                             │ POST /api/sessions/{id}/result
                                             │ (or: datawatch schedule-results post,
                                             │       MCP post_result)
                              ┌──────────────▼───────────────┐
                              │  per-schedule result store    │
                              │  (last N per schedule name)   │
                              └──────┬───────────────┬────────┘
                                     │               │
                          ┌──────────▼─────┐  ┌──────▼━━━━━━━━━━━━━━━━┐
                          │ Status tab     │  │ Integration status    │
                          │ Result panel   │  │ dashboard card        │
                          │ (PWA, per      │  │ (PWA, opt-in, spark-  │
                          │  session)      │  │ line across runs)     │
                          └────────────────┘  └───────────────────────┘
```

This is a complement to, not a replacement for, the alert system:
results answer **"what did the run produce"**, alerts answer
**"does the operator need to be paged now"**. A healthy green run
posts an `ok` result so you can see its numbers; a rule still fires
when something needs attention. See
[alerts-and-notifications](alerts-and-notifications.md) and
[alert-rules](alert-rules.md).

## What it is

- **A payload contract** (same shape over REST, CLI and MCP):
  required `title`; `status` ∈ `ok` | `warn` | `error`; optional
  `summary` (one line), `metrics` (free-form JSON object), `tables`
  (up to 20, each up to 100 rows), `markdown` (short notes).
- **A per-schedule history store**: results whose session belongs to a
  named schedule are kept in the daemon's data dir, newest first,
  bounded by `results.schedule_history_limit` (default 20, oldest
  evicted first). It survives daemon restarts and the death of the
  posting session.
- **A board slot**: every valid post — scheduled or not — is also
  written to the posting session's status-board result slot, which the
  PWA Status tab renders.
- **Read surfaces**: PWA (Status tab panel + dashboard card), CLI
  (`datawatch schedule-results list` / `history <name>`), MCP
  (`schedule_results_list`), comm channels (`schedule-results
  [name]`), and REST (`GET /api/schedule-results[/{name}]`).
- **Identity rule**: a session's spawn-scoped credential may post only
  for the session it was minted for — posting to any other session is
  rejected with 403.

## Base requirements

A running datawatch daemon (any release that includes the result
store). For jobs that post: the job must run as a **spawned subprocess
session** (`datawatch schedule spawn --shell …` or `subprocess: true`)
so it receives the injected session identity and credential — or it
must already hold a token with the `sessions:result_write` capability.
Reading results needs only the usual daemon access (any reader with
`autonomous:read` / the operator's token).

## Setup

### 1. Spawn your job as a subprocess session

```bash
datawatch schedule spawn --shell "imap-mcp run-rules --config ~/workspace/imap.yaml" \
  --path ~/workspace --schedule-name "imap-mcp run-rules" --cron "0 * * * *"
```

`--shell` sets `subprocess` mode: the daemon runs the command as a
child process (one program, no shell; no LLM, no tmux), marks the session complete when it
exits (exit 0 = complete, non-zero = failed), and injects three
variables into the child's environment:

| Variable | Value |
|---|---|
| `DATAWATCH_SESSION_ID` | this session's full ID |
| `DATAWATCH_BASE_URL` | the daemon's loopback base URL |
| `DATAWATCH_RESULT_TOKEN` | a scoped credential carrying only `sessions:result_write`, minted for exactly this session |

The token is single-purpose and single-session: it cannot read
anything, cannot post for any other session, and is swept from the
token store when the session no longer exists.

### 2. Have the job post a result on its way out

The simplest form — the CLI picks up the session ID and credential
from the environment:

A `--shell` task is **one program with arguments, with no shell**. Quoting
(`'…'`, `"…"`, `\`) and a leading `~/` work, but `|`, `&`, `;`, `<`, `>`,
`( )`, `$`, backticks and newlines outside quotes are refused, and the
session fails with a message that says so. A job with several steps goes in
a script file, and the task runs that file:

```bash
# ~/workspace/jobs/imap-run-rules.sh  (chmod +x)
#!/usr/bin/env bash
code=0
set +e
imap-mcp run-rules --config ~/workspace/imap.yaml
code=$?
set -e
datawatch schedule-results post \
  --title "imap-mcp run-rules" \
  --status ok \
  --summary "40 active rules, 2 messages actioned" \
  --metrics '{"rules": 40, "matched": 2, "errors": 0, "duration_s": 95}'
exit $code
```

```bash
datawatch schedule spawn --shell "~/workspace/jobs/imap-run-rules.sh" \
  --path ~/workspace --schedule-name "imap-mcp run-rules" --cron "0 * * * *"
```

For a one-liner that needs a pipe or `&&`, name the shell yourself, for
example `--shell "bash -c 'make test && make lint'"`. That is an explicit
choice of program; the launcher still starts no shell on its own.

`post --file payload.json` accepts a full payload file (flags override
its fields), and `--table '{"title":…,"columns":[…],"rows":[[…]]}'`
is repeatable for tabular data.

The raw REST equivalent, for languages without a CLI:

```bash
curl -sS -X POST "$DATAWATCH_BASE_URL/api/sessions/$DATAWATCH_SESSION_ID/result" \
  -H "Authorization: Bearer $DATAWATCH_RESULT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "imap-mcp run-rules",
    "status": "ok",
    "summary": "40 active rules, 2 messages actioned",
    "metrics": {"rules": 40, "matched": 2},
    "tables": [{"title": "Rules that matched",
                "columns": ["rule", "action", "matched"],
                "rows": [["newsletters", "move", 2]]}]
  }'
```

### 3. Read the results

```bash
datawatch schedule-results list              # latest per schedule
datawatch schedule-results history "imap-mcp run-rules"   # full retained history
datawatch schedule-results list --json       # raw JSON
```

MCP tools (same contract as REST):

- `post_result` — `session_id` + `title` required; `status`,
  `summary`, `metrics`, `tables`, `markdown` optional.
- `schedule_results_list` — no arg: one group per schedule (latest
  result, retained count, recent-status sequence); with
  `schedule_name`: that schedule's full retained history.

Comm channel (read-only): `schedule-results`,
`schedule-results <name>` (history, newest first, capped at 10
lines).

REST (with the operator's token):

```bash
curl -sS "$BASE/api/schedule-results" -H "Authorization: Bearer $DW_TOKEN"
curl -sS "$BASE/api/schedule-results/imap-mcp run-rules" -H "Authorization: Bearer $DW_TOKEN"
```

### 4. See it in the PWA

- **Status tab Result panel** — open any session that has posted a
  result; the panel appears under the other status-board cards with
  the status badge, summary, and rendered content. Shown only when a
  result exists.
- **Integration status dashboard card** — opt-in, off by default:
  on the `/dashboard` tab, open the *Add Card* picker; *Integration
  status* is listed first. One row per schedule name: status badge,
  title + summary, relative posted time, retained count, and a
  sparkline across the recent runs (oldest → newest). Clicking a row
  opens the session that posted it.

### 5. Tune retention

Keep the last N results per schedule name (default 20, minimum 1);
changes apply from the next posted result:

```bash
datawatch configure results.schedule_history_limit=50
```

Also settable via MCP `config_set key=results.schedule_history_limit
value=…`, `PUT /api/config`, the PWA Settings card, or YAML
(`results.schedule_history_limit`).

## Payload limits

| Field | Limit |
|---|---|
| Whole payload (after processing) | 64 KB |
| `tables` | at most 20 |
| Rows per table | at most 100 |
| `summary`, `markdown`, table titles, headers, cells | 2000 chars — longer values are cut with a `…(truncated)` marker, never silently dropped |
| `title` | required; a post without one is rejected |
| `status` | `ok`, `warn` or `error` (blank allowed = unset) |
| `metrics` | a JSON object |
| Retention | `results.schedule_history_limit` per schedule name (default 20) |

Content is rendered as escaped text in the PWA — no raw HTML;
markdown runs through the same sanitized pipeline as every other
markdown surface in the app.

## Verification

1. `datawatch schedule-results list` shows your schedule with the
   latest status.
2. Open the posting session in the PWA — the Result panel on the
   Status tab shows the same title/status/summary.
3. Add the *Integration status* card to the dashboard; the row
   appears with its sparkline after at least one run.

## Troubleshooting

- **`--session is required` / 403 on post from inside the job** — the
  job is not running as a spawned subprocess session (no injected
  `DATAWATCH_SESSION_ID` / `DATAWATCH_RESULT_TOKEN`). Use
  `datawatch schedule spawn --shell …`, or pass a token that has
  `sessions:result_write` explicitly.
- **403 posting to another session** — expected: a spawn-scoped token
  may only post for its own session. Use the operator's token only if
  that token actually carries the capability.
- **Result missing from schedule history but present in the session**
  — the session has no schedule name. Result posting always updates
  the session's own status board; only sessions spawned by a named
  schedule also land in the per-schedule history. Spawn with
  `--schedule-name …`.
- **Card not in the Add Card picker** — your PWA is older than the
  daemon serving it; reload the dashboard (the app self-heals on a
  version mismatch).

## Related

- [Dashboard](dashboard.md) — Mission Control and the card picker
- [Alerts + Notifications](alerts-and-notifications.md) — paging the operator
- [Alert rules](alert-rules.md) — automatic thresholds and actions
- [Commands](../commands.md) — `schedule spawn` and comm-channel verbs
