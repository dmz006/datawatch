# Checks and ship integration test

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
| Surfaces that returned the check finding (REST, CLI, MCP, comm) | 4 of 4 |
| Surfaces that returned the ship finding (REST, CLI, MCP, comm) | 4 of 4 |
| Ship commands recorded in shadow | 3 |
| Ship commands run in shadow | 0 |
| Tags created locally or on the remote | 0 |
| Test result | 1 passed, 0 failed |

Observation: the blocked task's final status after the run is failed (1 task);
the triage record keeps status blocked.
