---
docs:
  index: true
  topics: [sessions, automata, automations, api, pwa]
---
# How-to: list endpoints, paging, and loading states

datawatch's list endpoints are deliberately small. A long-running daemon
accumulates tens of thousands of bytes of long text per record — task text,
prompts, responses, story and task data — and almost none of it is rendered
in a list. So every list endpoint sends one compact row per item, pages by
`limit`/`cursor`, and answers unchanged re-requests with **304 Not
Modified**. The full record is always one request away on the by-id route.

The difference in size, measured on the operator's daemon before and after:

| List | Before | After (raw / gzip) |
|---|---|---|
| Sessions | 32 MB | 16 KB / 2 KB |
| Automata (PRDs) | 339 MB (one record alone: 13 MB) | 57 KB / 3.5 KB |
| Automations | 1.6 GB | 9.2 KB / 1.3 KB |
| Alerts | 219 KB | 22 KB / 6.8 KB |

(On the seeded live-style data set: 3000 sessions, 300 Automata, 20
Automations, 500 alerts.)

## The sessions list

`GET /api/sessions?view=summary` returns an array of rows. Each row carries
only what a list card renders — identity, state, backend, project, activity
times, a one-line `status_line` from the latest response (capped), token and
cost totals, and the parent id. No task text, no prompts, no transcripts.

By default the response holds every non-terminal session plus the 50 most
recently updated terminal ones; when the cap drops anything you get an
`X-Datawatch-Sessions-Omitted` count in the headers.

### Paging and filters

| Param | Meaning |
|---|---|
| `view=summary` | select the row-only summary view |
| `limit` | rows per page — default 50, maximum 500; with `limit` or `cursor` the body becomes the envelope `{"sessions": [...], "next_cursor": "...", "total": N}` |
| `cursor` | opaque; pass back `next_cursor` to get the next page (newest-first, no repeats, no skips) |
| `status` | comma list of state names, or `active` for all non-terminal states |
| `active=1` | only non-terminal sessions |
| `q` | case-insensitive match on name, id or the first 256 chars of the task text |
| `since` | RFC3339 or unix seconds; only sessions updated after it |

Without `view` and without paging the legacy full array is returned
unchanged, so existing scripts keep working.

```bash
# Set DW to your daemon base URL (no trailing path):
export DW="http://<host>:<port>"

# First page: 50 rows, newest first.
curl -sS "$DW/api/sessions?view=summary&limit=50"

# Next page: use the next_cursor the first page returned.
curl -sS "$DW/api/sessions?view=summary&limit=50&cursor=$NEXT"

# Only running work, matched by name or task text.
curl -sS "$DW/api/sessions?view=summary&limit=20&active=1&q=release"
```

Headers on every summary response (both shapes):

```
ETag: "9f2c…"                     ← 32-hex sha of the body
Cache-Control: no-cache
X-Datawatch-Sessions-Total: 3654  ← filtered total, even though the body is one page
```

## The Automata list

`GET /api/autonomous/prds` (no params) returns the summary list:

```json
{ "prds": [ { "id": "…", "title": "…", "status": "running", "rev": 9,
              "story_count": 5, "task_count": 14,
              "task_status_counts": {"done": 9, "in_progress": 3} } ],
  "total": 273,
  "next_cursor": "eyJ1…" }
```

A row's size depends only on the PRD's identity fields and how many distinct
task statuses it has — never on how much story and task text it holds. Fields
include `id`, `title`, `status`, `rev`, timestamps, `decision_count`,
`story_count`, `task_count`, `task_status_counts`, cost totals, `tags`,
`type`, `backend`, `priority`, `automation_id`, `blocked_reason`,
`quality_gate_state` and `parent_prd_id`. Story-level detail (per-story
counts) is one more request away on by-id.

The same paging and filter params as the sessions list, plus:

- `status=queued,blocked` — comma list of PRD statuses,
- `tag=NAME` (or `tag_id=ID`) — only PRDs carrying that tag,
- `q=needle` — matches title and id.

One quirk worth knowing: on the **unfiltered** first page you get every
active PRD (running, queued, waiting, blocked, paused, …) plus the most
recent `limit` of the rest; the cursor then walks only the rest, so active
rows are never repeated or skipped as you page.

`?full=1` returns the legacy shape: every PRD, in full. That is the 339 MB
form — useful for export, wrong for a list UI.

```bash
curl -sS "$DW/api/autonomous/prds?limit=50&active=1"
# next page:
curl -sS "$DW/api/autonomous/prds?limit=50&cursor=$NEXT"
```

## Getting the full record

| Want | Endpoint |
|---|---|
| One session in full | `GET /api/sessions/{id}` |
| One Automaton in full | `GET /api/autonomous/prds/{id}?full=1` |
| One Automaton, c…nd | `GET /api/autonomous/prds/{id}` |
| The heavy text of an Automaton (checks, results, outputs, verifier transcript) | `GET /api/autonomous/prds/{id}/outputs` |

The capped by-id form keeps each long text field under a 4 KB cap and marks
what it cut (`truncated` lists the field names; each cut field carries a ref
to the full copy), so the body stays small even on a 13 MB Automaton.

## The Automation and alert lists

- `GET /api/automations` — the list of Automation chains. These used to
  inline every member Automaton in full (1.6 GB on the operator's daemon);
  now it is the identities, statuses and edges only (9 KB).
- `GET /api/alerts?limit=50` — the alert list, capped by `limit`, with the
  same ETag behaviour.

## Re-asking without re-downloading (ETag / 304)

Every list endpoint above sets an `ETag`. Send it back and an unchanged
list answers **304 Not Modified** with no body — nothing to parse, nearly
nothing to transfer. Browsers do this for you with `Cache-Control: no-cache`
semantics; manual clients must copy the header:

```bash
ETAG=$(curl -sSI "$DW/api/autonomous/prds?limit=50" \
          | tr -d '\r' | awk -F': ' '/^ETag:/{print $2}')
# unchanged → 304, empty body:
curl -sS -D - -o /dev/null "$DW/api/autonomous/prds?limit=50" \
     -H "If-None-Match: $ETAG" | grep -i '^HTTP\|^etag'
HTTP/1.1 304 Not Modified
```

A 304 means "your cached copy is still the latest"; the client keeps
whatever it last stored. That is what lets a tab show old data
instantly and refresh it in the background.

## Compression

JSON list responses larger than ~1 KB are served gzipped to clients that
ask (`Accept-Encoding: gzip`, so `curl --compressed` or any browser).
Small bodies, streams, WebSocket and the proxy paths are never compressed.
That is the gap between the two size columns in the table above — the
Automata list goes from 57 KB to 3.5 KB on the wire.

## How the web UI loads these lists

Every list view in the web app (Sessions, Automata, Automation, Alerts)
behaves the same way:

1. **Instant skeleton + "Loading".** You never see a blank tab; the tab
   shows a placeholder row the moment you open it.
2. **Cached first paint.** If a previous response is in the tab's local
   storage it is rendered immediately and labelled *stale* / *cached —
   N s ago*, while the fresh request runs in the background.
3. **Stale is not failed.** A slow or timed-out refresh shows the stale
   copy with a warning ("data may be stale — last load N s ago"),
   never a blank screen.
4. **Failed has a Retry.** If there is no cached copy either, the error is
   shown with a Retry button that re-issues the same request.
5. **Hidden tabs stop polling.** List tabs you are not looking at do not
   keep re-fetching; opening one refetches in the background and updates
   the *stale* badge when it lands.

So the contract a tab can rely on is: **something on screen in 100 ms,
fresh data as soon as the network allows, a visible reason and a Retry
every time the network does not.** The nine slow-network scenarios (cold
load, empty list, failed load, stale revalidation, offline, and page-reload
patterns) exercise exactly this, throttled to a slow-4G profile — run them
with `cd scripts/test-stories/pwa && node run-all.mjs --group load`, or let
release smoke §79 run them against the smoke daemon.

## Troubleshooting

- **A list is blank with an error and a Retry button** — the request failed
  (network, timeout, 5xx). Retry, or hit the endpoint with curl to see the
  real status: `curl -sS "$DW/api/autonomous/prds?limit=10"`.
- **A list looks incomplete** — the summary view caps how many terminal
  sessions (or non-active PRDs on one page) it returns. Use `cursor` to
  page, `X-Datawatch-Sessions-Omitted` tells you how many terminal sessions
  the cap dropped.
- **A 304 keeps coming back** — good: your cached copy matches. Force a
  fresh body by omitting `If-None-Match`.
- **An endpoint still looks huge** — check you are not on the legacy form:
  no `view=summary`/`limit` on `/api/sessions`, or `full=1` on the Automata
  routes is exactly that.

## Where to go next

- [automation-tab.md](automation-tab.md) — building, watching and steering
  Automation chains in the tab.
- [automata-tags.md](automata-tags.md) — tagging Automata (the tag filter
  above uses the tag name).
- [session-state-and-status.md](session-state-and-status.md) — the state and
  status words the `status` and `q` filters match.
