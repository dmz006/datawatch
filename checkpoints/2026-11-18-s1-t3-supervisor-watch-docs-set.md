# s1-t3 supervisor-watch docs set — 2026-11-18 (session 24d79b7c7)

## Scope
Docs-only pass for the `priority_stall` + `backlog` findings, the two new
settings (`stall_threshold_minutes`, `priority_floor`) and the per-band wait
stats, so the whole doc set matches the code in `internal/autonomous/`, the
MCP tools, the app settings and the stats broadcast.

## Verified against code (before writing)
- `supervisor_watch_config.go`: ten keys; `stall_threshold_minutes` unset reads
  30 (0 legal, negative refused, age must be strictly past it); `priority_floor`
  unset reads 80 (0 legal, negative refused). `Effective*` accessors only.
- `supervisor_watch_status.go` / `supervisor_watch_render.go`: bands are fixed —
  high ≥ 80, medium 50–79, low < 50; a band with no waiting is omitted; render
  prints one line per band that waits something.
- `supervisor_watch_work.go:340` `CheckPriorityStall`, `CheckBacklog` called
  from `supervisor_watch_pass.go:146-147`; kinds in
  `supervisor_watch_record.go:79`; backlog caps at 16 named + `+k more`,
  highest priority first then oldest, subject `backlog`.
- `scripts/supervisor-watch.py`: `BACKLOG_CAP = 16`, `PRIORITY_FLOOR = 80`;
  the script also prints `PRIORITY STALL` / `BACKLOG` now.
- `internal/server/web/app.js` (26192-26193 + card render) and
  `internal/server/web/locales/en.json` (10 setting labels) carry the two new
  fields with the same defaults.
- `internal/stats/collector.go:189` `SupervisorWatchPriorityBands` in the
  stats broadcast; `cmd/datawatch/main.go:9412-9415` CLI.

## Files changed in this pass
- `docs/config-reference.yaml` — 16 checks (kinds incl. `priority_stall`,
  `backlog`), per-band wait stats note, two new key descriptions,
  `priority_bands` on the read-surface bodies, two new lines in the YAML.
- `docs/implementation.md` — Eight→Ten keys with the two new keys and their
  accessors; the queue checks named in `supervisor_watch_work.go`; render band
  lines; `priority_bands` on the monitoring read + stats broadcast field.
- `docs/operations.md` — REST/CLI/PWA rows carry `priority_bands` / band lines;
  new "Per-band wait stats" paragraph under the read surfaces; monitoring
  table: `priority_bands`, `supervisor_watch_priority_bands`, Monitor card
  fields filled in.
- `docs/datawatch-definitions.md` — **Supervisor watch** entry gained the
  per-band wait stats definition (fixed bands, row shape, omission rule) and
  the two new settings with their defaults (80 / 30) and the report-only
  scope of both findings.
- `docs/mcp.md` — `supervisor_watch_stats` and `supervisor_watch_list` both
  list `priority_bands` (the band definition inline).
- `docs/testing-tracker.md` — sixteen checks (12 script-shared + the two new
  shared + 2 watch-only), kinds list, per-band stats noted.
- `docs/parity-status.md` — REST/CLI/PWA/Monitor/settings rows now say stall
  threshold + priority floor, `priority_bands`, ten keys / ten fields.

## Already in final shape (verified, untouched)
- `docs/howto/automation-supervisor-watch.md` — findings table rows 77-78,
  "Per-band wait stats" section, both settings rows, all surfaces.
- Tracked `internal/server/web/assets/docs-bm25-index.json` — regeneration is
  byte-identical to HEAD; it already carried the new vocabulary, search
  surfaces it. `internal/docsindex/assets` copy regenerated in parity.

## Gates (all green, 2026-11-18)
- `go build ./...` — ok
- `go test ./internal/autonomous/ -run SupervisorWatch -count=1` — 199 passed
- `node --test app-supervisor-watch*.test.js` — 15 passed, 0 failed
- New diff contains no tracker IDs (BL/GH-) in operator-facing files
- All anchor links in the touched docs resolve (checked against headings)

No code changes. Nothing needs operator review beyond reading the how-to.
