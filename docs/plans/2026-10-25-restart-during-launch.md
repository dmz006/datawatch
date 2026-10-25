# Plan: restart during launch (restart-matrix state)

Status: the state is specified and wired into the restart matrix. Its live check has run and **fails** (7 pass, 3 fail). The failure is a product defect, not a harness result: the bare shell that holds a worker's launch is adopted as live at boot instead of being ended. This plan does not claim the state is validated. The fix and a passing live run are open.

## Context

A worker's launch can be held after its tmux pane has been created but before the agent starts. The pane then holds only a bare shell, with no agent in it. If the daemon restarts at that point, the boot pass has to decide what to do with that shell:

- If it adopts the shell as the task's live session, the task waits on a session that never ran.
- If it ends the shell and relaunches the worker, the relaunch runs unheld and the PRD completes unaided.

The restart-during-launch state exists to check the second behaviour. The matrix holds the first worker launch in the fake backend's launch step, restarts the sandbox daemon, and checks what became of the task.

## Intended behaviour

1. At boot, the bare shell that held the launch is ended. Its launch process is ended with it.
2. The task keeps its retry count (no retry is counted for the restart).
3. The task is relaunched once and runs unheld.
4. No duplicate session is created, no output is lost, and the PRD completes without operator action.

## Live result (2026-10-25)

Run: `scripts/restart-matrix.sh --strict --keep --only restart-during-launch`, `RM_KILL_MODE=control-group`, sandbox daemon built from this tree (v9.11.0), own config, data dir, port and socket dir. The full write-up is in `docs/plans/restart-matrix-findings.md`, section "restart-during-launch (live check, 2026-10-25)".

| Check | Result | What it shows |
|---|---|---|
| `bare_shell_not_adopted` | FAIL | Session `0134` is kept across the restart. The boot log reads `task 6b650002 kept (was verifying) on live session 0134`, and the worker is delivered into that pane. |
| `bare_shell_ended_at_boot` | FAIL | The held launch pid is still alive after the restart. |
| `held_pane_is_bare_shell` | FAIL | Harness defect: the pane lookup matches the short session id against `full_id`, so the pane is never found and the precondition is not checked. |
| `restart_exited_on_term`, `daemon_replaced`, `no_duplicate_sessions`, `no_lost_output`, `retry_count_unchanged`, `task_not_left_in_progress`, `prd_completes_unaided` | PASS | The task completes, but on the adopted pane. |

Task status as the API reports it: before the restart `verifying` on session `0134`; after the restart `completed` on session `0134`, with `adopted_session` false and retry count 0.

## Open items

1. **Product fix (open).** Stop the boot pass from adopting a session whose launch never started the agent, and end that shell. Changes belong in the boot-reconcile and launch-tracking code. Blocked on an operator decision: this reopens work recorded as earlier tasks.
2. **Harness fix (open).** `held_pane_is_bare_shell` and `bare_shell_ended_at_boot` must look the pane up by `id` as well as `full_id` (`scripts/restart-matrix/states-session.sh`, `rm_session_rows`). Until this is fixed, those two checks do not measure what they are named for.
3. **Sandbox path length (open).** The sandbox socket path must be under the tmux limit. tmux expands a relative `TMUX_TMPDIR`, and the harness length check measures the unexpanded string. The smoke wrapper cannot run from inside this worktree (its `mktemp` layer adds 15 bytes to a 43-byte project root). The state runs with an absolute sandbox base of 47 bytes or less.
4. **Live rerun (owed).** After items 1 and 2, rerun the state with `--strict` and record the result here and in the findings.

## Parity surface

This state adds no new user-facing surface. It is a verification state in the restart matrix.

- **REST, MCP, CLI, comm channel, PWA**: no new endpoint, tool, command or screen. The behaviour under test is the daemon's boot pass, which these surfaces read only through existing task and session status.
- **Task status**: the boot pass changes which session a task keeps. The status values it reports (`verifying`, `completed`) are unchanged.
- **Documentation surfaces**: `docs/operations.md` (Restart safety table) and `docs/testing-tracker.md` (daemon restart matrix section) name the state and its current failing result.
- **Harness**: `scripts/restart-matrix.sh --only restart-during-launch`. The release smoke does not run this state yet, and it should not be added to the strict list until open items 1 and 2 are closed.

## Verification

- Docs: `bash .datawatch/verify/restart-launch-docs.sh` checks the internal-reference and how-to coverage audits, the whitespace of the change, that `docs/operations.md`, `docs/testing-tracker.md` and `docs/plans/restart-matrix-findings.md` name the state, and that this plan has a Parity surface section.
- Live: `DATAWATCH_BIN=<sandbox binary> RM_TMP_BASE=<absolute base, 47 bytes or less> RM_KILL_MODE=control-group scripts/restart-matrix.sh --strict --keep --only restart-during-launch`. The result is FAIL until open items 1 and 2 are closed.
