---
docs:
  index: true
  topics: [autonomous, automata, automation, events, event log, supervisor watch, lane fill, infrastructure retries, checks, cleanup, safe to delete, repo ci watch, error, next action, quiet]
---

# Automata Events

The **Events** tab is the third sub-tab of the Automata view (after
Automation and Automata). It is one newest-first stream of what the daemon
watches and does for you: supervisor watch findings, lane fill decisions,
infrastructure retries, Automata check findings, the safe-to-delete and
cleanup state, the repo CI watch, and the errors the daemon recorded along
the way. The rows reflect the state the daemon holds when you read it —
the findings that are still open, the task that is still held, the latest
cleanup report — not a replay of past alerts.

## Event kinds

| Kind | What you see | Severity |
|------|-------------|----------|
| **watch** | One row per supervisor watch finding (open findings; a resolved finding is a quiet row) and the repo CI watch: a quiet "Repo CI watch is off" row that says how to turn it on, an "has not polled yet" row, or one warn row per failing or pending CI run with its run link | info; **warn** when an alert was raised or a CI run is failing/pending |
| **decision** | The recent lane fill planner decisions: start Automaton, decompose, decompose ahead, report merge, blocked, idle. A blocked decision is warn; an idle lane is a quiet row; a decision that ran with an error is an error row with the error as its detail | info; **warn** blocked; **error** on a decision error |
| **retry** | One row per task that was retried or is held after an infrastructure failure. A retried row says which failure class, attempt *n* of *m*, and when the next retry is due; a held row says how many retries were spent and what to do | info; **warn** when the task is held |
| **check** | The findings the Automata checks recorded, newest first; the reason names the check that wrote it | info |
| **cleanup** | One row per branch in the safe-to-delete report (safe rows; held rows, warn when the worktree has uncommitted changes) plus one cleanup status row covering both cleanup passes and the disk readings | info; **warn** held-for-dirty or disk alert; **error** when a pass recorded a failure |
| **alert** | Part of the kind vocabulary, reserved for daemon alerts; today the feeds report an alert as the **warn** severity of their own row rather than a separate alert row | — |
| **error** | The raw errors the daemon recorded — from its error records and from the failure reasons on failed tasks — classified into what happened, why it matters and what to do next. A reviewed outcome ("content" failure) is a judgment, not an error, and does not appear here | **error**, or quiet **info** when harmless |

## Reading the stream

- Each row shows the time, the event kind, the project, and the owner: an
  **Automation** (the parent) and/or an **Automaton** (the child).
- Click a row to expand its detail, reason and next action.
- A **quiet** entry is a routine notice shown as one muted line with its
  reason. It needs no action.
- An **error** entry is shown as an alert with what happened, the reason,
  and a **Next action** button that opens the tab where you fix it
  (the Automaton's tab, or the Automation's tab when the event belongs to a
  chain).

### What a quiet entry means

A quiet row is background state, reported so you can see the daemon is
watching — not a problem. The quiet rows you will meet:

- a supervisor watch finding that has **resolved**,
- a lane that went **idle**,
- the **repo CI watch turned off** (the row also says how to turn it on),
- branches that are **safe to delete**,
- **harmless permission errors** — for example the daemon statting a worktree
  that was already removed, or a path the daemon does not need.

### What an error entry means

Each recorded raw error is classified into one of three cases:

| Case | Row | Next action |
|------|-----|-------------|
| **Harmless permission error** — a path the daemon does not need (a removed worktree, a directory it has no business reading) | quiet, info | none — the reason says why it is ignorable |
| **Permission denied** on a path the daemon does need (`permission denied`, `EACCES`, `EPERM`) | error | grant the daemon user access to the path in the error, then retry the step |
| **Any other failure** — including a source that reported failure with no message | error | open the daemon log for the full error, fix its cause, then retry the step |

The one-line summary is capped at 160 characters; the full raw text is in
the daemon log, and the row's reason carries the raw error alongside the
classification. The **Next action** button on an error row jumps to the
tab that owns the event, so you can look at the Automaton or Automation
directly.

## Filtering, searching and paging

**In the PWA**

- **Kind** chips toggle one or more event kinds on and off.
- **Project** narrows the list to one project; *All projects* clears it.
- **Search events** matches text in the summary, detail, project and the
  Automaton/Automation names, case-insensitively.
- **Load more** fetches the next page of older events.

**On every other surface**

| Surface | Read | Parameters |
|---------|------|-----------|
| REST | `GET /api/autonomous/event-log` | `kind=` comma list of kinds (an unknown kind is a 400), `project=` exact project, `q=` case-insensitive text, `limit=` 1–200 (default 50), `before=` the previous page's `next_cursor` (a bad cursor is a 400) |
| CLI | `datawatch autonomous event-log` | `--kind` (repeatable or comma list), `--project`, `--q`, `--limit`, `--before`, `--json` for the raw body |
| Comm | `autonomous event-log` | `kind=watch,error`, `project=…`, `q=…`, `limit=` 1–10 (default 10), `before=` the cursor from the previous reply |

The REST body is `{"events": [...], "next_cursor": "...", "total": n}` —
`total` counts every event that matches the filters across all pages, and
`next_cursor` is empty on the last page. Pass that cursor back as `before`
to read the next page.

```
GET /api/autonomous/event-log?kind=error&limit=20
GET /api/autonomous/event-log?kind=watch,retry&project=datawatch&q=lane
datawatch autonomous event-log --kind retry --limit 10
```

## The cards that moved here

These cards now live in the Events stream instead of their own panels on the
Automation tab:

- **Supervisor watch** → `watch` rows
- **Recent decisions** (lane fill) → `decision` rows
- **Infrastructure retries** → `retry` rows
- **Automata checks** → `check` rows
- **Safe to delete** → `cleanup` rows
- **Cleanup status** → the one `cleanup` status row

The repo CI watch's CI line is a `watch` row now too. Each moved card's
**?** link opens this page.

## Status cards on the Events tab

The Events tab also carries the status that used to sit on the removed
**Setup and policy** sub-tab. Each card stays hidden while it is off, empty or
has never run, so a quiet system shows only the stream:

- **Onboarding report**
- **Supervisor loop**: the recent runs and their escalation counts. It polls
  only while the Events tab is open, and stays hidden until the loop has run.
- **Merge shadow**
- **Release readiness**
- **Model request watch** (see [model-request-progress.md](model-request-progress.md))

A link under the cards opens Settings, where all Automata configuration now
lives: the merge pipeline, the decision records folder, and the usage reading
and extra credit forms (**Settings → General → Automata**).

## Related

- [Supervisor watch](automation-supervisor-watch.md) — the findings behind the `watch` rows
- [Lane fill](automation-lane-fill.md) — the decisions behind the `decision` rows
- [Infrastructure retry](infrastructure-retry.md) — the holds behind the `retry` rows
- [Automata checks and ship](automata-checks-and-ship.md) — the checks behind the `check` rows
- [Worktree cleanup](automaton-worktree-cleanup.md) — the passes behind the `cleanup` rows
- [Automation tab](automation-tab.md) — where the moved cards used to live
