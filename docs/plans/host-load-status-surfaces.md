# Host load check state in monitoring/observability surfaces

Scope: make the existing host load check (`autonomous.host_load_alert`,
implemented in `internal/autonomous/host_load.go`) readable from the monitoring
surfaces that already exist, without adding any new subsystem. The check itself
was not changed behaviorally; it now keeps a read-only snapshot that one
provider call exposes everywhere.

## Data flow

```
HostLoadCheck.Check()          internal/autonomous/host_load.go
  └─ records each successful reading + reap counts (last N per check)
HostLoadCheck.Status()         same file — one snapshot read, no new state
  ├─ Manager.SetHostLoadProvider(fn)     internal/autonomous/manager.go
  │    └─ Manager.Status()
  │         └─ LoopStatus.HostLoad       internal/autonomous/models.go
  │              ├─ Server handleAutonomousStatus → GET /api/autonomous/status
  │              └─ autonomous_status MCP tool (returns the REST body verbatim,
  │                 internal/mcp/autonomous.go) — no MCP change needed
  ├─ cmd/datawatch/main.go stats wiring (SetAutonomousStatsFunc callback)
  │    └─ SystemStats.HostLoad* fields → GET /api/stats (and its WS broadcast,
  │       internal/stats/collector.go)
  └─ cmd/datawatch/main.go Prometheus wiring (SetOnCollect callback)
       └─ datawatch_host_load_* gauges (internal/metrics/prometheus.go)
```

All snapshot state lives inside `HostLoadCheck` itself (guarded by its mutex);
there is no separate shared state object and no daemon-global besides the
existing `hostLoadAlert` sink. The three exposure paths read the same
`Status()` call.

## Surfaces

| # | Surface | Status | Detail |
|---|---------|--------|--------|
| 1 | `GET /api/stats` (SystemStats) | **Done** | New fields (internal/stats/collector.go): `host_load_mode` (off\|alert\|reap, omitted when empty), `host_load_last`, `host_load_limit`, `host_load_breached` (always present, default false), `host_load_last_check_at` (omitted before first reading), `host_load_orphans_reaped`. Populated in cmd/datawatch/main.go from `Manager.Status().HostLoad` in the existing SetAutonomousStatsFunc callback; also rides the existing /api/stats WS broadcast (no new push path). |
| 2 | `autonomous_status` MCP tool | **Done** | No MCP change: `handleAutonomousStatus` internal/mcp/autonomous.go already returns the GET /api/autonomous/status body verbatim. The body now carries `host_load` (a `HostLoadStatus` object, omitted when no provider is wired) because `Manager.Status()` includes it via `LoopStatus.HostLoad` internal/autonomous/models.go. |
| 3 | `GET /api/autonomous/status` | **Done** (enabler for 2) | `handleAutonomousStatus` internal/server/autonomous.go unchanged; reads `Manager.Status()` which now carries `st.HostLoad` when `SetHostLoadProvider` (manager.go) was called — done by main.go at startup. |
| 4 | Prometheus | **Done** | Four gauges in internal/metrics/prometheus.go (registered in existing Register): `datawatch_host_load_last_reading`, `datawatch_host_load_limit`, `datawatch_host_load_breached` (1/0), `datawatch_host_load_orphans_reaped_total`. Updated in the existing SetOnCollect callback in cmd/datawatch/main.go, guarded so they are not touched unless a reading exists. |
| 5 | CLI | Non-applicable | No existing `datawatch <noun> status`-style CLI surface prints host load; adding a new CLI command would be a new surface, out of scope here. The alert the check already raises (hostLoadAlertSink → alerts store, warning level) is unchanged and remains the one proactive notification. |
| 6 | Web UI (non-API) | Non-applicable | The web UI already renders /api/stats and /api/autonomous/status; the new JSON fields are additive and show up wherever those endpoints are displayed. No template or JS change was required or made. |
| 7 | Session alerts / chat broadcast | Non-applicable (existing alert unchanged) | The check's alert path (alert mode text, reap mode reap) is untouched. No new per-session alert or WS frame was added — the /api/stats WS broadcast already ships SystemStats, which now includes the fields. |

## Snapshot semantics (what the surfaces show)

- **mode** — effective mode (off/alert/reap) at read time; "off" yields an empty
  last reading, so the numeric fields read zero.
- **last reading** — 1-minute load average (`last_load`) and its threshold
  `multiple × cores` (`last_limit`), with `last_check_at`. The check records a
  reading on every tick where it could read load, whether or not breached.
- **breached** — true when the last reading is over the limit; i.e. the breach
  streak the check counts minutes on is still open. It is not the same as
  "an alert fired": an alert only fires once the streak has lasted the
  configured minutes, and at most once per episode.
- **orphans_reaped** — cumulative count of leftover processes the reaper ended
  for ended sessions (reap mode; always 0 in alert/off mode). The alert fires
  on the same tick as the reap.

## Tests

- `internal/autonomous/host_load_test.go` — `TestHostLoadStatusTracksFakeLoad`
  (snapshot tracks a fake load reading over then under the limit),
  `TestHostLoadStatusCountsReapedOrphans` (reap mode: reap count in status),
  `TestHostLoadStatusOffModeIsEmpty`, `TestManagerStatusCarriesHostLoad`
  (nil without provider, snapshot verbatim with one).
- `internal/server/autonomous_status_host_load_test.go` —
  `TestAutonomousStatusIncludesHostLoad`: the `/api/autonomous/status` body
  omits `host_load` with no provider and carries the snapshot verbatim with one.
- `internal/mcp/autonomous_status_host_load_test.go` —
  `TestAutonomousStatusToolIncludesHostLoad`: the `autonomous_status` tool
  text (the REST body verbatim) carries the `host_load` object.

Gate: `env -u TMUX -u TMUX_TMPDIR go test -count=1 ./internal/autonomous/ ./internal/server/ ./internal/mcp/` — all pass.

## What was deliberately not done

- No new CLI command, new WS channel, new per-session alert, or new config key.
- No change to the check's breach/alert/reap behavior, thresholds, or cadence.
- No change to the version in `cmd/datawatch/main.go` or
  `internal/server/api.go`.
