# Pi-prompt descendant-only stall bound (autonomous.descendant_stall_seconds)

**Date:** 2026-10-28
**Version:** version at merge time (next patch above main) — no version number is set in `CHANGELOG.md` itself; the entry rides the `[Unreleased]` section and is stamped at release alongside every other entry.
**Scope:** a new daemon-side stall detector for the `pi-prompt` backend family, driven by one config key. Files: `internal/autonomous/descendant_stall.go` and `_test.go`, `internal/config/descendant_stall.go` and `_test.go`, `internal/config/config.go`, `internal/server/api.go` and `descendant_stall_config_test.go`, `internal/mcp/autonomous.go`, `cmd/datawatch/main.go`, `descendant_processes.go`, `descendant_stall_wait.go`, `end_attribution.go`, their tests, `internal/server/web/app.js` plus the settings field tests and the five locale bundles (en, de, es, fr, ja). Docs: `CHANGELOG.md`, `docs/config-reference.yaml`, `docs/implementation.md`, `docs/operations.md`, `docs/howto/autonomous-recovery.md`, `docs/api/autonomous.md`, `docs/mcp.md`, `docs/testing-tracker.md`, this file.
**Status:** code Done and documented on this branch; the live fire (a real pi-prompt worker parked behind a living child being reaped) is not yet exercised against a running daemon, so `Validated=No` in the tracker section.

## What it does

A one-shot `pi --print` run writes nothing to its session log until it finishes, so the log-based
stall check excludes that family, and the exit-silence check does not cover the case where the
runner has exited but a child it started (a build, a test, a long command) is still alive. This
unit adds one bound, `autonomous.descendant_stall_seconds` (default `2700`), that fires only for
the `pi-prompt` family and only while a live work descendant is present and there is no other
evidence of life.

- **Clock:** the latest of the tracker start, the previous observation, the session-log mtime and
  the newest regular file in the worker's work directory. Any fresher evidence moves the clock;
  a zero descendant count resets it. Silence strictly longer than the bound trips the detector.
- **Identity rule:** only the PIDs this attempt recorded (union of observations, keyed by PID and
  verified by start time) may ever be signalled. Clean-up sends `SIGTERM`, waits a 5-second grace,
  escalates to `SIGKILL` only for the still-live recorded PIDs, and never touches a recycled PID.
- **Failure path:** the session ends (`descendant-only stall detector: <reason>`), a
  `Task worker descendant-only stall` alert fires, evidence (pane tail, recorded descendants,
  which ended on TERM vs KILL) lands on the task, and the `DescendantStallError` that wraps
  `ErrWorkerStalled` takes the attempt through the existing `auto_fix_retries` budget.
- **Default:** normalization makes `0` or a negative value read back as `2700`, so the detector
  can never be silently disabled.

## Phases

| # | Phase | Status |
|---|-------|--------|
| 1 | `DescendantStallTracker` + default/normalization in `internal/autonomous` and `internal/config` | Done |
| 2 | Wait-loop integration in `cmd/datawatch` (family-gated `descendantStallTick`, evidence, recorded-identity kill path) | Done |
| 3 | Surfaces: config YAML, REST `GET`/`PUT`, MCP `autonomous_config_set`, CLI `config set`, comm `configure`, PWA settings card (5 locales) | Done |
| 4 | Tests: 23 Go test functions across 5 packages + the PWA settings field tests | Done |
| 5 | Documentation: CHANGELOG, config reference, implementation, operations, howto, API docs, MCP docs, testing tracker, this plan | Done |
| 6 | Live validation on a real daemon (pi-prompt worker left with a live descendant) | Pending |

Shipped in: version at merge time (next patch above main); the entry is in `CHANGELOG.md` under `[Unreleased]` and stamped at release.

## Parity surface

| Surface | Touched | State |
|---|---|---|
| REST | Yes | `GET` exposes `descendant_stall_seconds` in the `autonomous.*` config read; `PUT` accepts `autonomous.descendant_stall_seconds` with the same non-negative integer validation as `session_idle_timeout_seconds` (negative rejected with 400). Covered by `internal/server/descendant_stall_config_test.go`. |
| MCP | Yes | `autonomous_config_set` declares the `descendant_stall_seconds` parameter (forwards it only when supplied) and `autonomous_config_get` reads it back. Documented in `docs/mcp.md`. A schema-declaration test (like the one for `gate_disagreement`) is the remaining low-cost addition — tracked in the testing tracker section. |
| CLI | Yes | Generic config-set path; document with `datawatch config set autonomous.descendant_stall_seconds 2700`. No new command or flag. |
| comm channel | Yes | `configure autonomous.descendant_stall_seconds=2700`; the router forwards it to the REST `PUT` config endpoint (same path as every other `autonomous.*` key). No new verb. |
| YAML/config | Yes | `autonomous.descendant_stall_seconds: 2700` in `config.yaml`, documented in `docs/config-reference.yaml`; read at attempt start, so a change applies to the next attempt without a daemon restart. |
| PWA | Yes | Settings card field positioned directly after `session_idle_timeout_seconds`; label `settings_descendant_stall_seconds` present in all five locale bundles (en, de, es, fr, ja); covered by `app-autonomous-settings-fields.test.js` and `app-autonomous-settings-field-values.test.js`. |
| Android | Not done — supervisor step | No contract change for the native client: the key is a daemon-side setting, and the mobile app today does not surface this key. The datawatch-app parity issue (cross-repo) is a supervisor step and is tracked separately; recorded here as pending. |
| iPhone | Not done — supervisor step | Same as Android: no client contract change; covered by the same pending datawatch-app parity step. |

Nothing is excluded without a reason: the Android and iPhone rows are untouched because this
branch changes no client-facing contract — the native client neither sends nor parses the new key,
and the setting is reachable over every surface the daemon already exposes.

## Verification

```bash
# Go (TMUX/TMUX_TMPDIR unset so the process-tree tests see a clean tree)
unset TMUX TMUX_TMPDIR
go test ./internal/autonomous/ ./internal/config/ ./internal/server/ ./cmd/datawatch/ ./internal/mcp/

# PWA settings field tests
node --test internal/server/web/app-autonomous-settings-fields.test.js internal/server/web/app-autonomous-settings-field-values.test.js

# Docs contract for this unit
.datawatch/verify/document-descendant-stall-bound.sh
```

## Completion checklist

- Functionality: done per the phase table; live fire still pending (phase 6).
- Tests: 23 Go test functions across `internal/autonomous`, `internal/config`, `internal/server`,
  `cmd/datawatch`, `internal/mcp` — all pass; the PWA settings field tests pass. Details and the
  pending items (MCP schema-declaration test, live daemon fire) are in the testing-tracker section
  "Pi-prompt descendant-only stall bound (autonomous.descendant_stall_seconds)": **Tested=Yes,
  Validated=No**.
- Documentation checklist: `CHANGELOG.md` `[Unreleased]` entry added (no version number);
  `docs/config-reference.yaml` key documented; `docs/implementation.md` "Descendant-only stall
  bound" section; `docs/operations.md` "Descendant-only stall bound" subsection;
  `docs/howto/autonomous-recovery.md` "Task failed with `descendant-only stall`" section;
  `docs/api/autonomous.md` config block; `docs/mcp.md` `autonomous_config_set` parameter entry;
  `docs/testing-tracker.md` section; no tracker IDs in operator-facing docs.
- Localization Rule: new `t('settings_descendant_stall_seconds')` key added to all five locale
  bundles (en, de, es, fr, ja); covered by the PWA field test.
- Mobile-Parity Rule: recorded as pending (Android, iPhone) — the datawatch-app parity issue is a
  supervisor step (cross-repo).
- Version bump: N/A for this unit; the release stamps the `[Unreleased]` entry and bumps both
  `Version` variables at merge.
- Commit tag: `tracker: descendant-only stall bound`.
