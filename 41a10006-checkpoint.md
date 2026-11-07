# CHECKPOINT — 41a10006 wire the hourly auto-lanes apply into the loop (DONE)

## What was built

The daemon-side hourly auto-lanes wire. When `usage.calculator.auto_lanes` is
true, `cmd/datawatch` now runs a loop — once at start and once every hour —
that (1) asks the attached calculator service for the freshest advice
(`Service.Advice`, the same service REST serves), (2) counts the open
Automata whose capacity priority is at or above
`auto_lanes_special_priority` (new `Manager.CountOpenPRDsWithPriority`, read
under the Manager mutex), (3) applies the pure rule
`DecideAutoLanes(suggested, special, maximum, current)` — so the cap is at
most `auto_lanes_max` and at least 1 — and (4) when the target differs from
the current cap, sets the first enabled claude-code LLM entry's
`max_inflight` through `Registry.Update` — the exact same save as the
operator's `PUT /api/llms/{name}` — so `syncLLMLimits` (10s) picks it up in
the ledger live. Every decision, moved or not, is appended to the existing
`AutoLanesLog` in the data dir (from, to, direction, reason, policy, lanes
running now, special count). When `auto_lanes` is false the loop does
nothing. The loop reads settings live off the config on every pass, so the
operator flipping the switch takes effect at the next pass.

## Files

- `cmd/datawatch/usage_autolanes_wiring.go` (new) — `autoLanesWiring`
  (testable pass with injected advisor/target/apply/special/settings/log/
  clock), `runOnce`, `Start` (channel ticks; blocking), `StartHourly`
  (process-lifetime hour ticker, self-stopping), `startAutoLanesLoop`
  (production wiring: service, registry, Manager, config, data dir), and the
  `HttpServer`-free entry point.
- `cmd/datawatch/usage_autolanes_wiring_test.go` (new) — 9 tests, all
  `AutoLanes*`: rhythm (start + per tick), clamp at the operator maximum,
  floor at 1, unchanged case (no registry churn, decision still logged),
  off-switch does nothing, entry pick (real `inference.Registry`; disabled
  claude-code and claude-API entries untouched), advice-error skip then
  recovery, and save-error still logs the decision (log is written before
  apply). Fakes: advisor (fixed advice/error + call count), clock, special
  count. Save path is the real `Registry.Update`.
- `cmd/datawatch/main.go` — `go startAutoLanesLoop(ctx, cfg, httpServer,
  amgr, expandHome(cfg.DataDir))` right after `AttachUsageCalculator`.
- `internal/autonomous/manager.go` — `CountOpenPRDsWithPriority` (open =
  not completed/rejected/cancelled/archived/failed; special =
  priority ≥ threshold; mutex-guarded like `prdStatus`).
- `internal/server/usage_calculator_advice.go` — `HTTPServer.UsageCalculator()`
  getter over the existing `api.UsageCalculator()`.

## Gates (all green, run at the end)

- `go vet ./cmd/datawatch/` — clean
- `go build ./cmd/datawatch/` — clean
- `gofmt -l` across touched files — clean
- `go test ./cmd/datawatch/ -count=1` — 1750 passed, 0 failed
- `go test ./internal/autonomous/ ./internal/server/ -count=1` — 7682 passed,
  0 failed

## Judgment calls (for the supervisor)

- **Entry pick + save path**: "first enabled claude-code entry" = first in
  `Registry.List()` (name-sorted) with `Kind == claude-code` and
  `!Disabled`. The save is `Registry.Update(l)` with `l.MaxInflight =
  target` — byte-for-byte the path of the operator's LLM update (same
  validation, same persistence, same change broadcast); the ledger's 10s
  limit sync (`syncLLMLimits`) picks the new `max_inflight` up within ~10s,
  so no ledger call from the loop itself.
- **Special count = open Automata by capacity priority**: the config field
  is `auto_lanes_special_priority` and the PRD's `Priority` is documented as
  the capacity-queue priority — so "open special items" are open
  (draft/planning/needs_review/revisions_asked/approved/running/blocked/
  paused) PRDs with `Priority ≥ auto_lanes_special_priority`. Completed,
  rejected, cancelled, archived and failed PRDs do not count.
- **Advice lanes = `advice.Advice.LanesSuggested`** (the resolved policy's
  suggestion), consistent with `DecideAutoLanes`'s `suggestedLanes`
  parameter ("how many lanes the policy for this reading suggested").
- **Order of effects**: decision is appended to the log *before* the save,
  so a failed save still leaves the operator-visible record (and the cap
  stays unchanged).
- **Loop is process-lifetime, self-stopping**: `StartHourly` owns the
  `time.Hour` ticker and stops it when the daemon context ends (the first
  draft had a `defer t.Stop()` bug that would have killed the hour ticker
  immediately; fixed and covered by the rhythm test running through a
  channel).
- **Off switch is a silent, zero-effect skip** — no advice call, no save, no
  log entry.
- **No schema/migration surface**: nothing new is stored beyond the existing
  `AutoLanesLog` and the LLM registry entry (already persisted schema).
  `config.UsageCalculatorConfig` needed no changes — `AutoLanes`,
  `AutoLanesMax`, `AutoLanesSpecialPriority` already exist with defaults and
  live-read support.
