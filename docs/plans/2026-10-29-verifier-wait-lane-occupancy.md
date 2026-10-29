# Lane fill: a lane waiting on a verifier counts as available

**Date:** 2026-10-29
**Version:** next minor above main (set at merge time)

**Terminology.** Operator-facing text says **Automaton / Automata**, following the Terminology
Rule in `AGENT.md`. The internal Go type is `PRD` (`internal/autonomous`); this document uses
"PRD" only for code identifiers, API paths and tool names. Lane fill is a duty of the
coordinator and is not an Automation (capital A).

This is **Step 3 (duty 6)** of
`docs/plans/2026-10-25-automation-supervisor-duties-proposal.md`:

> **Step 3.** Verifier-wait lane occupancy (duty 6). Adds a wait-on-verifier flag to each
> occupant (the wait reason is already captured by `setWaiting`), so `PlanLaneFill` counts
> such a Claude lane as available under the Claude lane count and the usage policy.

It builds on the shipped lane fill in `docs/plans/2026-10-24-lane-fill-shadow-mode.md` and the
lane model there (2 GPU pool lanes, a configured number of Claude channels, one Automaton per
Claude channel). The row for this work in `docs/plans/README.md` is row 252 (BL650).

## Problem

A Claude lane whose Automaton has finished its work-for-now and is only waiting on verifier
results counts as occupied in `PlanLaneFill`. The lane is in fact idle for planning purposes:
no worker runs on it, no Claude execution happens on it, and the queued work behind it could
start there today. Until this step, the planner waits for the verifier to return and for the
lane to roll up before it will start queued work, and the operator sees the lane as occupied
while watching an empty one.

## Design

### Occupant wait flag

Each lane occupant in the planner's snapshot gains a `waiting_verifier` flag. The wait reason
is already captured by `setWaiting` (the task moves to `waiting_capacity` with the reason), so
the flag is derived from the occupant's live tasks: a Claude lane counts as waiting on a
verifier when the Automaton on it has no running worker and all of its remaining work is in
the verifier-wait state.

`PlanLaneFill` treats such a lane as **available** for the next start, under the existing
Claude lane count (`autonomous.lane_fill.claude_lanes`) and the usage policy. A lane that is
waiting on a verifier but still has running work anywhere is counted as occupied, as before.
Nothing changes for the GPU pool: pool idleness is still judged from live evidence, never from
a flag.

### Setting

`autonomous.lane_fill.count_verifier_wait` (boolean, default `true`) on every config surface.
When `false`, the lane fill planner behaves exactly as it does today: such a lane counts as
occupied. The setting is inert with `autonomous.lane_fill.mode: off`, and lane fill keeps its
existing default, `mode: shadow`: the planner records what it would start on the now-available
lane and starts nothing.

### Lane state on the surfaces

Each lane entry in the lane fill status gains two fields:

- `wait_reason` — the string `"verifier"` when the occupant's work is all waiting on a
  verifier; absent otherwise. It is a read-only state field, not something a client sets.
- `available` — boolean, the planner's view of the lane under the current settings
  (`count_verifier_wait` and `mode`). An `available: true` lane with an occupant is the
  verifier-wait case.

REST, MCP, CLI, the comm channel and the PWA lane fill card show the state the same way. The
idle-lane alert, when it fires for a lane counted available with eligible work, names the lane,
the reason (verified waiting, not a gap) and the first eligible item.

### One alert per episode

An alert episode is one continuous period during which a lane counts available while eligible
work is queued. The alert fires once for the episode — at the episode's first poll past
`autonomous.lane_fill.idle_alert_minutes` — and not again until the episode ends (the lane
stops counting available, or no eligible work is queued) and a new episode starts. This
replaces the per-poll firing the duty 6 step calls out as wrong.

## Parity surface

| Surface | Included? | Intent |
|---|---|---|
| REST | Yes | `GET /api/autonomous/lane-fill` lane entries gain `wait_reason` and `available`; config get and put carry `autonomous.lane_fill.count_verifier_wait`; documented in `openapi.yaml`. |
| MCP | Yes | `autonomous_lane_fill_status` returns the same lane state; capability table unchanged (no new tool); the setting travels through the existing config tools. |
| CLI | Yes | `datawatch autonomous lane-fill` shows the wait state and availability per lane; `datawatch config set autonomous.lane_fill.count_verifier_wait` covers the key. |
| comm channel | Yes | `autonomous lane-fill` compact status carries the per-lane state; `configure autonomous.lane_fill.count_verifier_wait=<bool>` sets the key; the once-per-episode alert carries the reason. |
| YAML/config | Yes | `autonomous.lane_fill.count_verifier_wait`, default `true`, in `docs/config-reference.yaml`. |
| PWA | Yes | The lane fill card on the Automation sub-tab shows each lane's occupant, the verifier-wait state and availability; the Automata settings block carries the new field; locale keys in all five bundles. |
| Android | Excluded in this work | This work does not change the apps. The single mobile parity issue is drafted in `docs/plans/parity-issues/verifier-wait-lane-occupancy.md`; no gh command is run by the implementing session. |
| iPhone | Excluded in this work | Same single draft issue as Android: `docs/plans/parity-issues/verifier-wait-lane-occupancy.md`. The supervisor files it at merge. |

**Mobile-Parity.** Mobile clients consume one changed REST contract: the lane fill status.
`GET /api/autonomous/lane-fill` lane entries gain `wait_reason` (string, `"verifier"` when the
occupant's work is waiting on a verifier, absent otherwise) and `available` (boolean, the
planner's view of the lane under the current settings). The existing `alerts[]` gains no new
shape — an alerted lane simply appears once per episode instead of once per poll.
`autonomous.lane_fill.count_verifier_wait` travels through the existing config get and put. The
ready-to-file issue text, setting table and the three new locale keys with their placeholders
are in `docs/plans/parity-issues/verifier-wait-lane-occupancy.md`.

## Scope

Affected packages and trees:

- `internal/autonomous`: the occupant wait flag in the planner snapshot, the
  `PlanLaneFill` availability rule, the `count_verifier_wait` read in the config block, the
  `wait_reason`/`available` fields in the lane fill status, and the once-per-episode alert.
- `internal/config`: the `autonomous.lane_fill.count_verifier_wait` key, default and
  validation.
- `internal/server`: the status handler's new fields, config get and put, the alert path,
  `openapi.yaml`.
- `internal/mcp`: the status tool's new fields; the setting through the existing config tools.
- `internal/router`: the comm channel `autonomous lane-fill` line and the `configure` verb.
- `cmd/datawatch`: the `datawatch autonomous lane-fill` output and the config wiring; the
  parity test.
- `internal/server/web`: the lane fill card state, the settings field, locale keys in all five
  bundles.
- `docs`: `docs/config-reference.yaml`, `docs/howto` (the automata how-to lane fill section),
  `docs/testing-tracker.md`, `docs/parity-status.md`, `CHANGELOG.md`, and this plan's status
  line.

Out of scope: starting or approving anything in shadow mode (that is the existing `mode: on`
path and its own rules), changing the verifier policy, the GPU pool idleness evidence, and any
change to the mobile apps.

## Phases

Status as of 2026-10-29: all four stories are **Planned**; none is built. Each story's own
checklist includes the Phase Completion Checklist from `AGENT.md` (functionality, unit tests,
live smoke against a real daemon, `env -u TMUX -u TMUX_TMPDIR go test ./...` and
`node --test internal/server/web/*.test.js`, the general documentation checklist, the
Mobile-Parity Rule, the Localization Rule, and the plan status line).

1. **Story 1: the occupant wait flag and the planner.** Status: **Planned**.
   - `waiting_verifier` on the occupant snapshot, derived from `setWaiting`'s captured wait
     reason: no running worker and all remaining work in the verifier-wait state
     (`internal/autonomous/capacity.go` state, read by the planner in
     `internal/autonomous/lane_fill.go`).
   - `PlanLaneFill` counts such a Claude lane as available under `autonomous.lane_fill.claude_lanes`
     and the usage policy; a mixed lane (verifier-wait plus running) stays occupied; the GPU
     pool is untouched.
   - `TestVerifierWaitPlanner*` table-driven: the 2026-10-29 shape (one Claude lane whose
     Automaton waits on a verifier, one queued decomposed Automaton), the mixed-lane case, and
     the flag-off case.
   - Unit tests only in this story; no surfaces, no config key yet (the planner reads a
     `count_verifier_wait` parameter defaulting to true).
2. **Story 2: the `autonomous.lane_fill.count_verifier_wait` setting.** Status: **Planned**.
   - The key in `internal/config` (boolean, default `true`), wired into the planner call.
   - Every config surface: REST get and put, MCP config tools, CLI `datawatch config
     set`, comm `configure`, the PWA Automata settings block (field, label, hint),
     `docs/config-reference.yaml`, locale keys in all five bundles.
   - `TestVerifierWaitSetting*`: round-trip through every surface, default when absent,
     switching `false` restores today's occupancy rule.
3. **Story 3: the lane state on REST, MCP, CLI, comm and PWA.** Status: **Planned**.
   - `wait_reason` and `available` on every lane entry of `GET /api/autonomous/lane-fill`, the
     MCP `autonomous_lane_fill_status` tool, the CLI `datawatch autonomous lane-fill` output,
     the comm `autonomous lane-fill` line, and the PWA lane fill card (state chip plus the
     availability line).
   - `openapi.yaml`, locale keys in all five bundles, the federation capability check.
   - `TestVerifierWaitParity` in `cmd/datawatch/lane_fill_parity_test.go` (the same
     four-surface shape as `TestTriageParity`): the wait state and the availability read the
     same on REST, MCP, CLI and comm for the same daemon state.
4. **Story 4: one alert per episode, docs and final gate.** Status: **Planned**.
   - The idle-lane alert fires once per episode for a lane counted available with eligible
     work, names the verified-wait reason and the first eligible item, and does not re-fire
     until the episode ends.
   - `TestVerifierWaitAlert*`: the once-per-episode shape — re-polls during the same episode
     add no alert; a new episode alerts again at most once.
   - `CHANGELOG.md` (Unreleased, plain language), the how-to lane fill section,
     `docs/config-reference.yaml`, the `docs/testing-tracker.md` section (all live rows
     Validated: No until the sandbox check), `docs/parity-status.md`, the mobile parity issue
     draft in `docs/plans/parity-issues/verifier-wait-lane-occupancy.md`, and this plan's
     status line.
   - Final gate: `env -u TMUX -u TMUX_TMPDIR go build ./...`, `go vet ./...`,
     `make lint`, `go test ./...`, `node --test internal/server/web/*.test.js`, the
     documentation checklist scripts, and a sandbox-daemon smoke of the shadow mode with one
     Claude lane waiting on a verifier — the state visible on all five surfaces and exactly
     one alert for the episode.

## Operator confirmation

Decisions for the operator before `autonomous.lane_fill.mode` is switched to `on` with this
step in place:

- [ ] The default of `autonomous.lane_fill.count_verifier_wait` (this plan ships `true`;
      confirm or set `false` in config).
- [ ] Whether a Claude lane waiting on a verifier may receive a **second** Automaton in `on`
      mode (start on the same lane while the first waits for its verifier), or whether the lane
      only counts available for the **planner's order and budget math**, never for a concurrent
      start. The stories above build the second reading — available for ordering and the lane
      count, and the existing `on`-mode admission path still applies one Automaton per lane —
      and this plan does not change start seriality per lane.
- [ ] The alert wording for the verified-wait episode (the names-the-lane-and-reason line in
      Story 4).

Shipped in: not yet released (the merge helper sets the version at merge; this document does
not name a version on purpose).
