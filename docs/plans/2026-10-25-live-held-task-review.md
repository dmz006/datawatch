# Live held-task review

**Date:** 2026-10-25
**Version:** taken at merge, not on this branch
**Status:** In progress. The executor change, the surface wording and the Go tests are done and pass. The docs are written in this task. No live run against a sandbox daemon has been done, so every tracker row stays Validated: No.
**Backlog:** the live held-task review entry in `docs/plans/README.md`, which links here.
**Tracker section:** "Live held-task review" in `docs/testing-tracker.md`.

---

## Problem

An operator can retry or cancel a task held for review (status `review_needed`). The held task does not stop its Automaton: tasks that do not depend on it keep running, so the Automaton's executor is still live when the operator decides. The decision path did not allow for that. Before it changed anything, `ResolveTaskReview` waited up to five seconds for the executor's run slot to be free, and answered "its executor is still finishing; try again shortly" when it was not. A live executor never frees its slot while it has other work, so the operator's retry or cancel failed with a 400 on REST, and an error on MCP and CLI, for as long as the Automaton ran other tasks.

## Root cause

Two assumptions in the decision path were wrong together.

1. **The executor exits right after a hold.** The wait was written for the case where the last task fails into review and the executor exits at once. It was never written for a hold that happens while other tasks are in flight.
2. **A run launched into an occupied slot is dropped.** The relaunch after a decision uses the same slot as the running executor, so the code waited for the slot instead of telling the running executor. With an executor that never exits early, the wait could not succeed.

The fix does not make the wait longer. It removes the wait and gives the live executor a way to hear about the decision.

## Outcome

- A retry or a cancel of a held task works while the Automaton runs other tasks, and answers at once.
- No second executor is started while one is live.
- A retried task runs, and a cancelled task fails its dependents, in both executor modes.
- An Automaton with no live executor is relaunched the way Resume relaunches it, as before.
- The five second wait and its error are gone from REST, MCP, CLI and the comm channel.

## Wake design

- **One wake channel per running Automaton.** `internal/autonomous/review_wake.go` holds `reviewWakes`: a buffered channel of capacity one per Automaton, and a pending flag per Automaton. `API.Run` arms the channel before the first `Manager.Run` and passes it to the executor in the run context. `disarm` drops it when the Automaton's run slot is released.
- **A decision signals; it never blocks and never launches.** `API.ResolveTaskReview` records the decision through the Manager, then, under the run lock, checks whether a run slot is live. If it is, it signals the wake channel. The signal sets the pending flag and does a non-blocking send, so a second decision while one is buffered is not lost: the executor re-reads the whole Automaton, not one task. If no slot is live and the Automaton is running, it calls `Run`, the same relaunch as before.
- **The pending flag covers the exit.** The run goroutine, after `Manager.Run` returns, takes the pending flag and checks the Automaton is still runnable (approved, active or running) inside the same critical section that releases the slot. A decision that landed after the pass's last look but before the slot was released therefore runs a second pass under the same slot. A decision cannot fall between the check and the release.
- **Order of record and signal.** The decision is written to the store first, then the signal is sent. An executor that wakes always finds the change. A decision made before the channel is armed is already in the store, and `Run` reads the store after the arm, so it is seen without a signal.
- **A nil channel is inert.** A `Run` with no armed channel (tests, other callers) gets a nil wake channel, which never delivers, so its behaviour is unchanged.

## Re-scan rules

The executor re-reads the Automaton from the store, rebuilds the run order from the tasks as they stand now, and applies these rules. The order built at the start of a pass leaves out held tasks, so a retried task is not in it until the order is rebuilt. A cycle found when rebuilding keeps the old order and logs a line.

### One task at a time (concurrency 1, the default)

- The walk checks the wake channel before each task and after the last one, so a decision made during the last task is not left for the next `Run`.
- On a decision it re-reads the Automaton, rebuilds the order, clears its failed and held sets, and walks the order again from the top.
- On the new walk, terminal tasks are skipped, a task still held for review stays pending and holds its dependents, and the first task that is neither runs next. A retried task is that task, so it runs right after the task now running finishes. A cancelled task is terminal, so it never starts, and its dependents fail as they do for any cancelled dependency.
- The task now running is never interrupted.

### Concurrent (concurrency above 1)

- The coordinator selects on the wake channel as well as on task results. It also checks the channel when nothing is in flight, before it would give up, so a decision that lands just before the exit is processed.
- On a decision, `reviewDecisions` re-reads the Automaton. A paused Automaton launches nothing. Otherwise it rebuilds the order and walks it. A task still held stays held. A task that is terminal (cancelled by the operator) is marked finished, and marked failed when it did not succeed, so its dependents resolve. A task that is pending again (retried) is dropped from the finished, failed and held sets so it can run.
- `remaining` is recounted from the tasks in flight plus the tasks in the order that are neither finished nor held, because a task held when the pass began never took a slot in the old count.
- It then calls `autoFailDeps` and `launch`. `launch` stays the only place a task starts, so concurrency limits, readiness and admission apply to a retried task as to any other. Tasks in flight keep running and report through the result channel.

### Both modes

- Nothing in the re-scan changes how a task fails into review or how the ladder chooses a worker.

## Test plan

Go tests (all added by earlier tasks of this item, and all pass):

- `internal/autonomous/review_live_executor_test.go`: six tests, run against a real executor in both modes where the mode matters. They cover a retry while a task runs, a task held before the pass began, a cancel with dependents, a decision during the last task, a decision pending at exit that runs in a second pass, and a retry with no live executor that relaunches.
- `internal/autonomous/review_wake_test.go`: the wake channel's signal and pending flag, the run context carrying the channel, and the API signalling a live executor on retry and on cancel instead of relaunching, and relaunching when none is live.
- `internal/autonomous/review_wake_sequential_test.go`: the one-task-at-a-time walk runs a retried task and skips a cancelled one.
- Surface tests: `TestFailureEscalationRESTResolveWhileAutomatonRunning` (`internal/server`), `TestFailureEscalationToolsResolveWhileAutomatonRunning` (`internal/mcp`), `TestFailureEscalationCLIResolveReviewWhileAutomatonRunning` (`cmd/datawatch`). These use stand-in daemons and check the contract, not the executor.

Not tested by Go tests, and owed as live runs on a sandbox daemon (18443 TLS, never the production daemon):

- A retry of a held task while another task runs, on the one-task-at-a-time executor.
- The same on the concurrent executor.
- A cancel of a held task while another task runs, with a dependent and a roll-up.

The testing tracker lists these three as Live rows with Validated: No. They must not be marked Validated: Yes from unit tests.

## Parity surface

Every channel the change touches, and what is excluded or delegated.

| Surface | Status | Reason |
|---|---|---|
| REST | **Include. Changed.** `POST /api/autonomous/prds/{id}/resolve_task_review` no longer waits and no longer answers 400 "still finishing" while the Automaton runs. | The handler and the API method it calls carry the change. The route, body and other status codes are unchanged. Doc comments, `docs/api/autonomous.md` and the openapi description say so. |
| MCP | **Include. Changed.** `autonomous_prd_resolve_task_review` has the same behaviour. | The tool calls the same API method. Its description and `docs/mcp.md` say it works while the Automaton runs. |
| CLI | **Include. Changed.** `datawatch autonomous resolve-review` has the same behaviour. | The command calls the REST route. Its help text and `docs/commands.md` say so. |
| Comm channel | **Include. Changed, no code change.** `autonomous review-task` has the same behaviour. | It posts to the same REST route, so it gets the new behaviour with no edit. It has no test of its own for this change. |
| YAML / config | **Excluded.** | No setting is added. The wake is not something an operator tunes: there is no case where an operator wants a decision to fail while the Automaton runs. A setting would only let the old failure back. |
| PWA | **Include. No code change.** The held-task buttons call the same route and now succeed while the Automaton runs. | The PWA's own tests and a browser run were not repeated for this change. Any wording in the PWA that says to wait for the Automaton to go idle is not covered by this task. |
| Android | **Include: capability parity, no new work.** The companion app uses the same REST route, so it gets the behaviour. | No new field, route or status code, so no companion-app change is needed. The companion-app issue for the failure escalation feature already covers the held-task actions. Not implemented in this repository. |
| iPhone / iOS | **Include: capability parity, no new work.** Same as Android. | Same reason. Not implemented in this repository. |

Only YAML / config is excluded, and its reason is above.

## Open items

- **Live run.** Not done. The three Live rows in the testing tracker stay Validated: No until a sandbox run passes.
- **PWA wording.** Check the PWA and its locale bundles for any text that tells the operator to wait for the Automaton to be idle before acting on a held task. None was changed here.
