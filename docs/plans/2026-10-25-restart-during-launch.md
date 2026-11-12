# Plan: restart during launch

Date: 2026-10-25. Version: the next patch above main (the merge helper sets the number; this document does not bump a version).

Status: the fix is in code on this branch, and its unit tests pass. The restart-matrix state `restart-during-launch` was run live twice: once before the fix (FAIL, 7 pass, 3 fail), and once after it (10 of 10 checks pass, one run, measure mode). A `--strict` rerun is still owed, so the state is not validated and is not in the release smoke's strict list.

## Context

A worker's launch can be held after its tmux pane has been created but before the agent starts. The pane then holds only a bare shell, with no agent in it. If the daemon restarts at that point, the boot pass has to decide what to do with that shell:

- If it adopts the shell as the task's live session, the task waits on a session that never ran.
- If it ends the shell and relaunches the worker, the relaunch runs unheld and the PRD completes unaided.

The restart-during-launch state checks the second behaviour. The matrix holds the first worker launch in the fake backend's launch step, restarts the sandbox daemon, and checks what became of the task.

## Scope

In scope:

- Boot adoption of a live session only once its agent has started, and ending a live bare shell at boot (including a task that was `verifying` on one).
- The start-confirm bound (`autonomous.session_start_confirm_seconds`, default 120 s) applied to an adopted claude-code session.
- Tracking launches in flight and draining them on shutdown with a fixed 15 s grace.
- The restart-matrix state, its held-pane lookup, and the unit tests for all of the above.
- Operator documentation: `docs/operations.md` (Restart safety), `docs/howto/autonomous-recovery.md`, `docs/testing-tracker.md`, `docs/plans/restart-matrix-findings.md`.

Out of scope, unchanged, and still open in the findings document:

- Planning PRDs on boot (an OPERATOR PICK).
- Where a daemon-run verification script runs (an OPERATOR PICK).
- The sandbox socket path limit: the state runs only with an absolute sandbox base of 47 bytes or less.

## Intended behaviour

1. At boot, a live session is adopted only once its agent has started. A live bare shell is ended, and its launch process with it.
2. The task keeps its retry count. A bare shell found at boot is requeued with the reason `session never started`, and no retry is counted.
3. The task is relaunched once and runs unheld.
4. An adopted claude-code session that never reaches its first assistant turn is ended after the start-confirm bound and counted against the start-failure budget.
5. No duplicate session is created, no output is lost, and the PRD completes without operator action.

## Phases

| Phase | What | Commits | Status |
|---|---|---|---|
| 1. Agent-started predicate | `Manager.AgentStarted`: the agent process in the pane, channel-ready, a delivered task, or a first assistant turn; a short session id resolves | cba24a1d | Done. Unit tests `TestAgentStarted*` pass. |
| 2. Boot adoption gate | Live sessions are adopted only once the agent has started; a live bare shell is ended and the task reset with the start-failure reason; the check is sampled three times | d7cb4f96, 379f896c | Done. Unit tests `TestBootAdoptionGate*` pass. |
| 3. Start-confirm bound for adopted sessions | An adopted claude-code session in `waiting_input` with no assistant turn past the limit is ended and counted as a start failure | cb9c2c71 | Done. Unit tests `TestAdoptedSession*` and `TestAdoptedStartFailure*` pass. |
| 4. Launch registry and shutdown drain | Launches in flight are tracked; shutdown refuses new launches and rolls back those still running after 15 s | 27892a93, 06262306 | Done. Unit tests `TestLaunchRegistry*` and `TestShutdownLaunch*` pass. |
| 5. Restart-matrix state and fake-backed cases | The `restart-during-launch` state with a launch-hold control; five fake-backed cases | aa644777, 3aedeb57 | Done. `TestRestartLaunchCases` passes. |
| 6. Live check, before the fix | `--strict --keep --only restart-during-launch`, sandbox daemon v9.11.0, 2026-10-25 10:48 | bb1f0bf6 | Done as a record: FAIL, 7 pass, 3 fail. The held shell was kept as live. |
| 7. Fix for a verifying task on a bare shell, and harness lookup | A verifying task on a bare shell is ended and requeued; the held-pane lookup matches the short id and reads the pane with `list-panes` | 379f896c | Done in code. |
| 8. Live check, after the fix | Same harness, 2026-10-25 13:59, measure mode, `restart-during-launch` and `worker-running` only | none (record under `.rm/`, git-ignored) | Done as a record: 10 of 10 pass for `restart-during-launch`. One run. |
| 9. Strict live rerun and smoke decision | Run `--strict` on the state, record the result in the findings and the tracker, then decide whether to add it to the release smoke's strict list | none | Owed. |
| 10. Documentation | Findings, operations, tracker, how-to, this plan, CHANGELOG | this change | Done in this change. |

## Live results

Recorded run before the fix (`scripts/restart-matrix.sh --strict --keep --only restart-during-launch`, `RM_KILL_MODE=control-group`, sandbox daemon v9.11.0, own config, port and socket dir): **FAIL, 7 pass, 3 fail**.

- `bare_shell_not_adopted` FAIL: session `0134` was kept as live at boot, and the worker was delivered into the held pane.
- `bare_shell_ended_at_boot` FAIL: the held launch pid was still alive after the restart.
- `held_pane_is_bare_shell` FAIL: a harness defect. The pane lookup matched the short id against `full_id`.
- The other seven checks passed, including no duplicate sessions, no lost output and retry count unchanged. The task completed on the adopted pane.

Run after the fix (sandbox daemon, same harness, 2026-10-25 13:59, measure mode, `restart-during-launch` and `worker-running` only): **restart-during-launch 10 of 10 checks pass**. The held launch pid was gone and its pane was absent before the new session began. The task completed on a new session (`7c5b` → `2c00`), with retry count 0 → 0, no duplicate sessions and no lost output. `worker-running` also passed 10 of 10.

Limits: one run after the fix, on one machine, with a fake backend and no real terminal UI. The record is under `.rm/`, which is git-ignored, so it is not in the commit. The run was not `--strict`, so it does not satisfy phase 9.

## Parity surface

This change adds no new user-facing surface. No PWA or REST contract changed.

Checked against the branch diff (`git diff main...HEAD`):

- The diff touches only `internal/autonomous`, `internal/session`, `cmd/datawatch`, `scripts` and `docs`. Nothing under `internal/server`, `internal/mcp`, `internal/router` or the web assets changed.
- No route, no MCP tool, no CLI command and no comm-channel command was added or changed.
- No JSON or YAML struct tag was added or changed.
- The config key `autonomous.session_start_confirm_seconds` already existed. This change extends its bound to adopted sessions. No new config key was added on any surface.
- Task status values are unchanged (`pending`, `verifying`, `completed`, `failed`). A task reset at boot reuses the existing `session never started` error text.

What an operator sees differently is behaviour, not contract: after a restart, a held launch is relaunched instead of adopted, and the shutdown log may contain `[session] rolled back launch <id>: still running at shutdown`.

Documentation surfaces updated: `docs/operations.md` (Restart safety), `docs/testing-tracker.md` (restart matrix and unit rows), `docs/plans/restart-matrix-findings.md` (result and matrix tables, fix section) and `docs/howto/autonomous-recovery.md`.

Harness: `scripts/restart-matrix.sh --only restart-during-launch`. The release smoke does not run this state. It stays out of the strict list until phase 9 is recorded.

## Verification

- Unit tests, run from the worktree with `env -u TMUX -u TMUX_TMPDIR go test ./internal/session/ ./internal/autonomous/ ./cmd/datawatch/ -count=1 -run '^(TestAgentStarted|TestBootAdoptionGate|TestAdoptedSession|TestAdoptedStartFailure|TestLaunchRegistry|TestShutdownLaunch|TestRestartLaunchCases)'`: all pass: 15 test functions in `internal/session` (11 `TestAgentStarted*`, 4 `TestLaunchRegistry*`), 12 in `internal/autonomous` (7 `TestAdoptedSession*` and `TestAdoptedStartFailure*`, 4 `TestBootAdoptionGate*`, `TestRestartLaunchCases` with five subtests) and 3 `TestShutdownLaunch*` in `cmd/datawatch`.
- Docs: `bash .datawatch/verify/restart-launch-docs.sh` checks the internal-reference and how-to coverage audits, the whitespace of the change, that the operations, tracker and findings documents name the state, and that this plan has a Parity surface section.
- Live, still owed: `DATAWATCH_BIN=<sandbox binary> RM_TMP_BASE=<absolute base, 47 bytes or less> RM_KILL_MODE=control-group scripts/restart-matrix.sh --strict --keep --only restart-during-launch`, recorded in the findings and the tracker.
