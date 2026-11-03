# Checks and ship integration test (in-process, not a live daemon)

This is an in-process test; no daemon was started. The live sandbox-daemon check is `docs/plans/2026-11-02-checks-and-ship-live-check.md` (settings, read routes, CLI, served PWA code). The ship step itself has not run on a live daemon.

In-process test `TestChecksShipIntegration` (`cmd/datawatch/checks_ship_integration_test.go`).
Setup: checks `pre_start_verifier` on, ship `push` and `tag` shadow. Real manager,
httptest server, temporary directories and a local bare remote; no daemon started.
Counts only.

| Measure | Count |
|---|---|
| Tasks created with a missing verification script | 1 |
| Tasks blocked before start (triage record status blocked) | 1 |
| Worker spawns for the blocked task | 0 |
| Triage notes recorded for the block | 1 |
| Blocked status transitions observed on the task (in_progress, blocked, failed) | 3 |
| Surfaces that returned the check finding (REST, CLI, MCP, comm) | 4 of 4 |
| Surfaces that returned the ship finding (REST, CLI, MCP, comm) | 4 of 4 |
| Ship commands recorded in shadow | 3 |
| Ship commands run in shadow | 0 |
| Ref changes on the remote or in the work repo after the shadow ship (all refs, including refs/heads/main and tags) | 0 |
| Branch pushes by push-on-complete in the shipping run (policy never) | 0 |
| Test result | 1 passed, 0 failed |

Observation: the task does reach status blocked (observed through the lifecycle
events), and the triage record keeps status blocked. The executor then settles the
task's own final status as failed (1 task); that is existing behavior, not changed here.
