# Orphan sweep compares session ids in short form

Date: 2026-10-24
Version: 9.8.1 (the version at planning time; the fix ships in the next patch above main)
Status: code and tests done; documentation in this change; live validation not performed
Tracker row: row 184 in `docs/plans/README.md` (BL601)

## Problem

The autonomous orphan sweep ends live task worker sessions that no non-terminal task is bound to, after a grace period. It compared the session lister's full session id (host name, a hyphen, then a 4-character short id) with the short id a task stores at spawn. The two never matched, so every live Claude worker counted as unbound and was ended about five minutes after its task was delivered. The session end reasons added in 9.8.1 made the cause visible (`by=autonomous.orphan-sweep ... after 5m30s unbound`).

## Approach

Reduce either form to the short id (the text after the last hyphen, since the short id never contains a hyphen) and compare short ids on both sides. The same helper is applied everywhere a task's stored session id is compared with a live session. A once-per-process self-check logs one line when the two id forms differ, so a future mismatch is visible in the daemon log. The default grace period (300 seconds) is unchanged, and no config field is added.

## Scope

Files changed by this work:

- `internal/autonomous/session_id.go`: the short id helper and the same-session comparison.
- `internal/autonomous/orphan_sweep.go`: short-id comparison for bound and orphan sessions, and the id form self-check.
- `internal/autonomous/orphan_sweep_test.go`: regression tests for bound workers, true orphans, the self-check and the audit entries.
- `internal/autonomous/session_id_test.go`: tests for the short id and the comparison.
- `internal/autonomous/manager.go`: the task-to-session lookup uses the helper.
- `internal/autonomous/reset_ends_sessions.go`: a reset compares session ids in short form.
- `internal/autonomous/reset_ends_sessions_test.go`: reset regression tests.
- `internal/autonomous/boot_reconcile_adopt.go`: boot adoption compares session ids in short form.
- `internal/autonomous/boot_reconcile_adopt_test.go`: boot adoption regression tests.
- `internal/autonomous/find_task_by_session_test.go`: the task lookup accepts the full id.
- `CHANGELOG.md`, `docs/testing-tracker.md`, `docs/operations.md`, `docs/howto/autonomous-recovery.md`, `docs/plans/README.md`: documentation for this change.
- `docs/plans/2026-10-24-orphan-sweep-session-ids.md`: this plan.

No change to `docs/config-reference.yaml`: its comment on `autonomous.orphan_grace_seconds` was checked against `NormalizeOrphanGraceSeconds` and is accurate.

## Phases

1. **Short id helper and orphan sweep.** Status: done (commit a8c3c4ca). Adds `sessionShortID` and `sameSessionID`, compares bound and orphan sessions by short id, and adds the once-per-process form self-check.
2. **Remaining comparisons.** Status: done (commit ce80847f). Applies the helper to reset, boot adoption and the task lookup by session id.
3. **Documentation.** Status: done in this change. CHANGELOG entry under Unreleased, a testing-tracker section, an operator note in the restart section, one sentence in the recovery how-to, and the plans index row updated.
4. **Live validation.** Status: not performed. Still needed: a live check on a daemon, with a real Claude worker bound to a task, that the sweep leaves it running past the grace period, and that an unbound worker is ended after the grace period. This is the check that closes the defect for operators.

## Test conditions

Unit tests only, run with Go 1.27.2 (linux/amd64) using `go test ./internal/autonomous/ -count=1`, under `env -u TMUX -u TMUX_TMPDIR`. They use fakes and fixed clocks and do not touch a live daemon or a production store.

## Parity surface

| Surface | Included or excluded | Reason |
|---|---|---|
| REST | Excluded | No endpoint, request or response field changes. The sweep is internal to the autonomous manager. |
| MCP | Excluded | No tool or argument changes. |
| CLI | Excluded | No command or flag changes. |
| Comm channel | Excluded | No command or reply changes. |
| YAML / config | Excluded | No field is added or changed. `autonomous.orphan_grace_seconds` keeps its default of 300 and its meaning; the comment in the config reference is already accurate. |
| PWA | Excluded | No screen or API call changes. |
| Android | Excluded | No client change; the behaviour is server-side. |
| iPhone | Excluded | No client change; the behaviour is server-side. |

The only operator-visible change is the once-per-process form self-check line in the daemon log, which is documented in `docs/operations.md` under *Daemon restart and in-flight Automata*.
