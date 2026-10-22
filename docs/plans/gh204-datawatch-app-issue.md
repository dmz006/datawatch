# datawatch-app issue body — result panel parity (filed from datawatch GH#204)

**Ready to file as a new issue in `dmz006/datawatch-app`.** Title and body below.
The two PWA surfaces (Status tab Result panel, dashboard "Integration status"
card) already ship in the datawatch PWA (vanilla JS, `app.js`); this issue
tracks the native Android/iPhone equivalents per the usual mobile-parity
policy for operator-visible PWA surfaces.

---

## Title

**Mobile parity: result panel (per-session result card + "Integration status" dashboard card)**

## Body

### Background

The datawatch PWA recently gained a **result panel**: a scheduled
subprocess session can post a small, structured summary of what a run
produced (title, `ok`/`warn`/`error` status, summary text, a few metrics,
optional tables), and the PWA renders it in two places:

1. **Status tab — Result panel** on a session's status page: status badge,
   title, summary, metrics, tables, shown only when that session has
   posted a result.
2. **Dashboard — "Integration status" card** (opt-in, added from the card
   picker, not in the default layout): one row per schedule name with the
   last status, summary, timestamp, and a sparkline across the retained
   recent runs; click-through jumps to that run's session.

Both exist only in the web PWA today. The Android/iPhone apps do not
surface posted results at all — a scheduled job that degrades gradually
(is still "completing") is invisible on mobile until it fully fails.

### Data available (datawatch REST, all already released)

| Endpoint | Purpose |
|---|---|
| `POST /api/sessions/{id}/result` | agent posts its structured result for its own session (scoped credential; 403 on another session's path) |
| `GET /api/schedule-results` | grouped summary per schedule name: latest result, retained entry count, `recent_statuses` (last up to 50) for the sparkline |
| `GET /api/schedule-results/{schedule_name}` | full retained history for one schedule, oldest→newest, each entry carrying the posting session id + timestamp |

Result payload shape (all strings; the server caps individual fields at
2000 chars, ~64KB total, 20 tables × 100 rows, truncating beyond the
limits):

```json
{
  "title":    "required — what this run was",
  "status":   "ok | warn | error",
  "summary":  "short human sentence",
  "metrics":  { "checked": 40, "actioned": 2 },
  "tables":   [ { "title": "...", "columns": ["..."], "rows": [["...", "..."]] } ],
  "markdown": "optional longer notes"
}
```

History retention per schedule name is operator-configured
(`results.schedule_history_limit`, default 20).

### Proposed mobile surfaces

- **Status tab equivalent (native session detail)**: a "Result" section
  rendering the latest result for that session — status badge with the
  same ok/warn/error semantics, title, summary, metrics as key/value rows,
  tables in a simple two-column layout. Hidden when the session has no
  posted result (same "only render what's present" rule as the PWA).
- **Dashboard / home-screen card** (optional, opt-in like the PWA card):
  top-N schedules by recency, each with schedule name, latest status
  chip, one-line summary, relative time, and a small status strip /
  sparkline from `recent_statuses`. Tap → open that run's session detail.

### Acceptance criteria

- [ ] Mobile session detail shows the latest posted result (or nothing
      when none has been posted) for a subprocess session spawned from a
      schedule.
- [ ] A status strip / sparkline on the dashboard (or equivalent screen)
      reflects `recent_statuses` order and the server-truncated shape.
- [ ] ok/warn/error styling matches the PWA palette semantics
      (green / amber / red).
- [ ] Payload fields longer than the server limits render the server's
      truncation marker, not a crash / layout break.
- [ ] No new endpoints needed — everything reads from the three REST
      routes above with the operator credential already in use.

### Cross-references

- datawatch GH#204 (feature + plan: `docs/plans/2026-10-09-gh204-result-panel.md`)
- datawatch operator doc: `docs/howto/result-panel.md`
- datawatch PWA implementation: `app.js` Status tab Result panel +
  dashboard "Integration status" card (opt-in card type)

---

*File this in `dmz006/datawatch-app` and note the issue number in the
datawatch changelog/planning where B4 (mobile parity) is referenced.*
