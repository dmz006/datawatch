# Release Notes — v3.3.0 (Observability group, partial)

**2026-04-19.** Observability backlog group ships **BL10**, **BL11**,
**BL12**. **BL86** (remote GPU/system stats agent — needs a separate
`datawatch-agent` binary) is deferred.

---

## Highlights

- **BL10 — Session diffing.** After `PostSessionCommit`,
  `ProjectGit.DiffStat()` runs `git diff --shortstat HEAD~1..HEAD`
  and stores a structured summary on `Session.DiffSummary`
  ("3 file(s), +47/-12"). Surfaces in the session JSON so the web UI
  badge, completion alerts, and comm-channel messages can show it.
- **BL11 — Anomaly detection helpers.** Pure-logic detectors for
  stuck-loop output, long-input-wait, and duration-outlier sessions.
  Tests cover boundary conditions; live-monitor wiring is opt-in
  (operator can plug into the existing `NeedsInputHandler` chain).
- **BL12 — Historical analytics.** `GET /api/analytics?range=Nd`
  returns day-bucketed session counts, completed/failed/killed
  splits, average duration, and overall success rate. Range values
  accept `7d`, `14d`, `30d`, `90d`, or any `Nd` from 1–365.

---

## Deferred from this release

- **BL86 — Remote GPU/system stats agent.** Implementing the
  recommended Option A (lightweight Go binary exposing
  `GET /stats`) means shipping a second binary product alongside
  `datawatch`, with its own packaging, systemd unit, and
  cross-build matrix. Worth a dedicated release rather than
  bundling.

---

## API additions

```
GET /api/analytics                    # default 7d
GET /api/analytics?range=30d          # 1d–365d, suffix "d" required
```

Response shape:

```json
{
  "range_days": 7,
  "from": "2026-04-13",
  "to":   "2026-04-19",
  "buckets": [
    {"date":"2026-04-13","session_count":2,"completed":2,"failed":0,"killed":0,"avg_duration_seconds":312.5}
  ],
  "success_rate": 0.92
}
```

---

## Schema additions

```json
// Session JSON (existing) gains:
{ "diff_summary": "3 file(s), +47/-12" }
```

Older clients ignore unknown fields.

---

## Container images

| Image | Change | Action |
|---|---|---|
| `parent-full` | Daemon embeds DiffStat capture + `/api/analytics` + anomaly helpers | **Rebuild required** |
| All other images | No change | No rebuild |

```bash
make container-parent-full PUSH=true CONTAINER_TAG=v3.3.0
```

Helm: `version: 0.5.0`, `appVersion: v3.3.0`.

---

## Testing

- **991 tests / 47 packages**, all passing (+22 vs. v3.2.0).
- `internal/session/bl10_diff_test.go` — shortstat parser cases.
- `internal/session/bl11_anomaly_test.go` — stuck-loop /
  long-input-wait / duration-outlier coverage.
- `internal/stats/bl12_history_test.go` — day-bucket aggregation.
- `internal/server/bl12_analytics_test.go` — REST contract tests.

---

## Upgrading from v3.2.0

```bash
datawatch update           # single host
helm upgrade dw datawatch/datawatch -n datawatch \
  --set image.tag=v3.3.0   # cluster
```

---

## What's next

- **v3.4.0 — Operations group**: BL17 hot config reload (SIGHUP),
  BL22 RTK auto-install, BL37 system diagnostics, BL87 `datawatch
  config edit`.
- **BL86** to follow as a dedicated release once the
  `datawatch-agent` binary scope is settled.
