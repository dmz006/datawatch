# Full smoke failures on main — causes and guards

**Date:** 2026-11-14 (written 2026-11-15)

**Recovered:** 2026-11-26, from a branch that was never merged. The fixes reached main later in a
separate port; the details below were rechecked against main on that day by reading the code. This
is not a record that the full smoke passes.

**Scope:** the four failures the full release smoke (`scripts/release-smoke.sh`) reported on main, and the fix for each one. Each note names the cause found and the check that now guards it.

| Failure | Smoke section | Cause in one line |
|---|---|---|
| Capacity budget | 77 | The test LLM had no billing mode set, so the daemon stored its budget but never reported it |
| Throwaway Automaton creation | 77, 78, Task routing | The scratch daemon refused `/tmp` as a project dir, and profile saves checked default rows for backends this daemon lacks |
| PWA priority-source row | 79b | The row rendered, but the story looked for it with a stale selector and sent a nested config body that the daemon ignores |
| Restart matrix pid verification | 80 | The sandbox config allowed its own dir as a project root only for the pi-node states |

## 1. Capacity budget

**Symptom.** In section 77 ("budget wait and rollover"), `GET /api/budget?llm=<throwaway>` returned 404. The throwaway LLM was also missing from the `budgets` array of `GET /api/budget` and `GET /api/capacity`.

**Cause.** The smoke created its throwaway LLM as kind `ollama` with no `billing_mode`. Kind `ollama` defaults to the `local` billing mode, and the daemon only reports budgets for `api` and `allowance` entries. The budget was saved but never shown, which is how the daemon is meant to treat local entries. The defect was in the test, not the daemon. The budget PUT updates the stored entry, so the mode set at create is kept.

**Fix.** The smoke now creates the throwaway LLM with `"billing_mode": "api"`.

**Guard.** Section 77 checks both sides of the rule. The api-mode entry must appear on `GET /api/budget` and `GET /api/capacity`. A second throwaway LLM in local mode, with a budget set, must get 404 from `GET /api/budget?llm=`. If either side changes, the section fails. See [`scripts/release-smoke.sh`](../../../scripts/release-smoke.sh).

## 2. Throwaway Automaton creation

**Symptom.** In section 77 (capacity admission wait and release), the smoke could not create its two throwaway Automata. Section 78 (Automation chain, fork and join) could not create its draft Automata either. In the Task routing section, saving a throwaway routing profile failed with HTTP 400. The smoke logged only "could not create". Before the fix, section 77 reported the create failure as a skip, blamed on a possibly disabled autonomous subsystem.

**Cause.** There were three causes:

- The scratch daemon's config set no `server.allowed_project_roots`. The throwaway Automata use `/tmp` and a `mktemp` dir as their project dir, so the allowed project roots check refused them with HTTP 400.
- Saving, switching or deleting a task routing profile checked every row against the LLM registry, including the shipped default rows. Those rows name backends that a fresh daemon, such as the smoke's scratch daemon, does not have, so every save failed with HTTP 400.
- The dry run for a draft Automaton with no tasks returned `"entries": null` instead of `[]`. An empty typed slice inside an interface is not `nil`, so the empty-list fallback never applied.

The Automation audit entries also carry the Automation id in `details.resource_id`, not in `subject`, so the section 78 audit lookup missed them.

**Fix.** The scratch daemon config now lists `/tmp` and `${TMPDIR:-/tmp}` under `allowed_project_roots`. Routing validation now takes the table already in force as a baseline. A row identical to one already in force is not looked up again. New and changed rows are still checked, and an unknown backend is still refused. The per-Automaton routing read and the dry run now return `[]` for an Automaton with no tasks. The audit lookup now also matches `details.resource_id`.

**Guard.**
- When the smoke cannot create a throwaway Automaton, it now reports a failure with the HTTP code and response body. Only HTTP 503 (autonomous manager disabled) is still a skip. A refused routing profile save also prints the daemon's response. See [`scripts/release-smoke.sh`](../../../scripts/release-smoke.sh).
- In [`internal/server/task_routing_test.go`](../../../internal/server/task_routing_test.go):
  - `TestTaskRoutingRESTProfilesWorkWhenDefaultBackendsUnknown` saves, activates and deletes profiles on a daemon that knows none of the default backends. It also checks that a new row naming an unknown backend is refused and leaves the stored table unchanged.
  - `TestTaskRoutingRESTDraftWithNoTasksAnswersEmptyList` checks that both the dry run and the routing read return `[]`.

## 3. PWA priority-source row

**Symptom.** Section 79b runs the browser story for the "Capacity priority by kind" and "Defect intake Automation" settings rows. It reported that the rows were missing, and the round trip of the table did not read back.

**Cause.** The rows did render. The story was wrong in two ways:

- It looked for the inputs with selectors (a class name, or a `value` equal to the config key) that the settings card had never produced.
- It wrote to `PUT /api/config` with a nested `{"autonomous": {...}}` body. The daemon applies that endpoint as a flat map of dotted keys, the same form the PWA sends, so it ignored the nested writes.

**Fix.** The other General card text inputs already had their config key in a `data-cfg-key` attribute. The priority-by-kind input and the Comms card text inputs now have it too. The story finds the inputs by that attribute. It now types the table into the real field and fires its change event, so the save goes through the PWA's own `PUT /api/config` call. Every direct write in the story now uses dotted keys.

**Guard.** [`internal/server/web/app-priority-source.test.js`](../../../internal/server/web/app-priority-source.test.js) renders the autonomous settings card and checks that both inputs exist with their `data-cfg-key`, current value, save handler and localized label. It also checks that the table row still renders, with its placeholder, when the config has no table. Section 79b still runs the browser story, under [`scripts/test-stories/pwa/`](../../../scripts/test-stories/pwa/), against a live daemon.

## 4. Restart matrix pid verification

**Symptom.** Section 80 runs `scripts/restart-matrix.sh --strict` one state at a time. It failed for `worker-running`, `waiting-capacity`, `waiting-external`, `git-operation` and `automation-branches`. These states never started their work. The harness reported only "could not create", and a start failure ended with a bare "does not verify as this sandbox's daemon".

**Cause.** The sandbox config added `server.allowed_project_roots` with the sandbox dir only for the states that set `RM_PI_NODES`. Once project roots were enforced, the daemon refused the project dir for every other state that creates an Automaton. Once that was fixed, `worker-running` exposed a daemon defect. A verifier session also carries the task id, so the session-created hook replaced the task's worker session id with the verifier's session id. That broke the restart matrix's check that the session id stays the same across a restart.

**Fix.** [`scripts/restart-matrix/lib.sh`](../../../scripts/restart-matrix/lib.sh) now always adds the sandbox dir as an allowed project root in the server block. When a create is refused, the harness logs the daemon's response. When pid verification fails, the error now says whether the pid is alive, gives its command line, and names the expected config and the daemon log. The pid check itself is as strict as before. In the daemon, `Manager.LinkCreatedSession` links only the worker session to the task. Verifier, guardrail, planner and other sessions for the same task are ignored.

**Guard.**
- [`tests/test-restart-matrix-sandbox-config.sh`](../../../tests/test-restart-matrix-sandbox-config.sh) runs in `make test`. It checks that the plain and pi-node sandbox configs each list their own dir as an allowed project root exactly once, inside the server block. It checks that `rm_verify_sandbox_pid` still refuses a foreign pid, an empty pid, init, garbage, and a process named datawatch that uses another config. When a binary is available, it starts a sandbox daemon and checks that the daemon accepts an Automaton in the sandbox project.
- `TestTaskSessionCreatedHookLinksOnlyTheWorkerSession` in [`cmd/datawatch/task_session_hook_test.go`](../../../cmd/datawatch/task_session_hook_test.go) checks that the task keeps its worker session id when other role sessions of the same task are created.
- The live strict re-run of the matrix noted on the original branch was not carried over; [`restart-matrix-findings.md`](../restart-matrix-findings.md) holds the findings kept on main.
