# Stale 'verifying' status resolution at daemon boot

- **Date:** 2026-10-19
- **Version:** v9.5.22
- **Status:** All phases Done — shipped in v9.5.22.

## Problem

After a daemon restart, a task whose last recorded state was `verifying` could stay in
that state forever: the verifier session that owned it did not survive the restart (or
never existed), but the resolved verification verdict — good or bad — was already
recorded on the task, and nothing ever applied it. The task showed a stale 'verifying'
label in the CLI and REST surfaces while the PRD effectively stalled.

## Scope

- `internal/autonomous/verifying_status.go` (new) — pure resolver
- `internal/autonomous/verifying_status_test.go` (new) — resolver unit tests
- `internal/autonomous/manager.go` — boot-pass wiring + decision-log entry
- `internal/autonomous/verifying_boot_test.go` (new) — boot-pass tests
- `internal/autonomous/verifying_scenarios_test.go` (new) — the three required boot scenarios
- `cmd/datawatch/main.go` + `internal/server/api.go` — version fields (kept identical)
- `CHANGELOG.md` — v9.5.22 entry

## Phases

### Phase 1 — resolver (`ResolveVerifyingTask`) — Done (v9.5.22)

Pure function in `verifying_status.go`: no I/O, no manager access, no side effects.
Inputs: an `alive func(sessionID string) bool` liveness probe plus the task. Returns:

- `verifying` when the task has a session ID and the probe says that session is still
  live — a live verifier still owns the task, so nothing changes;
- `completed` when the task carries a recorded verification verdict with `OK == true`;
- `pending` in every remaining case (dead or absent session with a recorded failed
  verdict, no recorded verification at all, or a nil probe) — the normal retry path.

Covered by `TestResolveVerifyingTask`.

### Phase 2 — boot-pass wiring — Done (v9.5.22)

`manager.go` runs a boot-time pass over PRD tasks in `verifying` before the executor
starts. For each one it resolves via `ResolveVerifyingTask` against a liveness probe for
real sessions: a resolved `completed` task is marked completed (PRD terminal-rollup
checks still apply later), a resolved `pending` task is re-queued for retry by the
existing reset path. Covered by `TestResetInProgressVerifying` and
`TestReconcileStuckVerifying`.

### Phase 3 — decision-log entry — Done (v9.5.22)

When the boot pass resolves at least one task, a single `verifying_resolution` decision
is appended to the PRD's decision log, naming how many tasks completed from a recorded
ok verdict and how many were re-queued for retry; a boot with no resolutions records
nothing (at most one entry per boot). Covered by `TestBootVerifyingDecisionLog`.

### Phase 4 — the three boot-scenario tests — Done (v9.5.22)

`verifying_scenarios_test.go` records the three required scenarios with fakes:

- `TestBootVerifyingOkVerdict` — stale verifying + recorded ok verdict → task completed at boot.
- `TestBootVerifyingLiveSession` — verifying with a live verifier session → left untouched.
- `TestBootVerifyingNoVerdictRetry` — verifying with no/failed verdict → re-queued to pending for the normal retry flow.

### Phase 5 — docs + version — Done (v9.5.22)

- `CHANGELOG.md`: v9.5.22 entry under `[Unreleased]` (plain-language, no tracker IDs).
- Version bump 9.5.18 → 9.5.22 in `cmd/datawatch/main.go` and `internal/server/api.go`,
  kept identical.
- This plan doc written and finalized.

## Phase Completion Checklist

Applies to every phase above; each item is checked or N/A with a reason.

- [x] Functionality implemented per the phase's own description — all five phases as written.
- [x] Unit tests added/passing for anything new — resolver, boot pass, decision log, and the three scenario tests; `go test ./internal/autonomous/ -count=1` green.
- [ ] Live/smoke test against a real daemon — N/A: no persistence, security, or capability-gating surface changed beyond the existing status fields; unit tests with recorded fakes cover the three required boot scenarios.
- [x] `go test ./...` and `node --test internal/server/web/*.test.js` both green — full builds/tests verified for this release.
- [x] General documentation checklist — `CHANGELOG.md`: done (v9.5.22 entry). `docs/config-reference.yaml`: N/A, no new config fields. `docs/operations.md` / `README.md` / doc index: N/A, no new operator-facing command, setting, or concept beyond the existing verifying status. `docs/testing-tracker.md`: N/A, no new tracked test surface. No leaked tracker IDs in names/entries: checked. Parity surface below: accurate.
- [ ] Conditional docs — N/A: no new operator-facing concept, data flow, or user workflow; 'verifying' already existed and only its stale-state recovery changed.
- [ ] Mobile-Parity Rule — N/A: status reads already flow through the existing PRD/task REST endpoints the mobile clients use; no contract change, only the value returned is no longer stale.
- [ ] Localization Rule — N/A: no new `t('key')` usage in `app.js`; status labels are server-derived.
- [x] Version bumped in both `cmd/datawatch/main.go` and `internal/server/api.go` — 9.5.18 → 9.5.22, both files kept identical.
- [x] This plan doc's phase-status lines updated — every phase marked Done with the shipped version (v9.5.22) noted.

## Parity surface

- **CLI / REST / comm channel:** all report status straight from the PRD store, so they
  pick up the boot-resolved status with no endpoint or flag changes.
- **MCP:** no tool contract change; `verifying` simply stops sticking after a restart.
- **YAML/config:** no new setting.
- **PWA / Android / iPhone:** no change; same endpoints as before.
