# Release 10.0 remaining work, in scheduled order

Generated 2026-11-18 01:24 from the running daemon (read only). Release 10.0 is 288 of 371 Automata done (78%); this file lists the 57 that still have work to do, the 0 that merged but whose review record is still open, and the Automations on the plan.

## How to read this

- **Order** is the order the daemon would take them: running first, then planning, then waiting for review or approval, then blocked, failed and finally drafts. Inside a group, higher priority first, then the plan position. Priority orders the capacity queue; it does not start a draft, which needs a decompose, an approval and a run first.
- **Expected window** is a rough estimate, not a promise: the last days finished about 8 Automata a day, so an Automaton at position N in the list is expected about N / 8 days from now. Drafts need a decompose and an approval first, so add about half a day. Blocked and failed ones have no date until someone clears the blocker. New work filed later pushes everything down.
- Tasks are listed under each Automaton as one line each, without the standard closing tasks (changelog, parity draft, docs set, load-step rule, version note) that every Automaton carries.

## Summary

| State | Count |
|---|---|
| running | 4 |
| planning | 1 |
| needs_review | 14 |
| blocked | 10 |
| failed | 2 |
| draft | 26 |
| merged, review record open | 0 |

At about 8 a day the 45 that can run without a blocker would finish in about 6 days (around 2026-11-24) if nothing new is filed. The 12 blocked or failed need a decision first.

## In scheduled order

1. **63ac0001** Session-kind profiles: lean starts for every session kind: Apply the lean session profiles to every session kind, chosen by the kind of session and by what it is doing, not only to Automaton worker, verifier and judge launches.
   - State running, priority 100, kind feature, tasks 3 of 7 done, expected about now, today.
   - Task (completed): Extend workerprofile with session-kind defaults.
   - Task (completed): Wire a profile name through the start call for every session kind.
   - Task (completed): Fix the capacity wait failure on profile starts.
   - Task (verifying): Make the LLM-row binary honored or refused.
   - Task (queued): Wire profile settings and read-back on all six surfaces.
   - Task (queued): Document session-kind profiles.
   - Task (queued): Verify the session-kind profile work end-to-end.
2. **d1130001** Free orphaned pi-pool slots held by judges and verifiers: A pi slot is held by a judge or verifier with no node lease, so one pi lane sits wasted while a node is idle.
   - State running, priority 95, kind bug, tasks 1 of 6 done, expected about now, today.
   - Task (completed): Investigate why a judge or verifier pool slot outlives its lease.
   - Task (queued): Fix slot release paths and add the orphan-slot reconcile.
   - Task (queued): Add fake-ledger tests for the orphan-slot reconcile.
   - Task (queued): Wire the periodic slot reconcile into the capacity tick with an alert line.
   - Task (queued): Document the freed-slot reconcile for operators.
   - Task (queued): Final gate for the orphan-slot reconcile.
3. **87620001** Make the full release smoke pass on main: Fix: the full release smoke fails on main in several sections that the targeted smoke never runs.
   - State running, priority 80, kind bug, tasks 11 of 14 done, expected about now, today.
   - Task (completed): Fix the capacity-budget smoke failures.
   - Task (completed): Fix throwaway Automaton creation in the capacity admission, task routing and automation chain sections.
   - Task (completed): Fix the PWA priority-source settings row.
   - Task (completed): Fix the restart matrix sandbox daemon verification.
   - Task (completed): Document the cause of each full smoke failure.
   - Task (failed): Run the full release smoke on a clean sandbox.
   - Task (completed): Fix the six full smoke failures that remain after merging main.
   - Task (completed): Fix smoke section 79: web UI slow-network load scenarios time out.
   - Task (completed): Fix smoke section 79b: the priority_by_kind settings row does not render.
   - Task (completed): Fix smoke section 80: the restart matrix loses the worker session id and counter lines.
   - Task (completed): Fix smoke sections 82 and 85: profile tier source and the usage CLI TLS error.
   - Task (completed): Fix the Branch refresh smoke section: the live routes answer 501.
   - Task (waiting_capacity): Fix the Housekeeping repairs smoke section: the group list has changed.
   - Task (queued): Fix the Task routing smoke section: dry run, throwaway save and delete fail.
4. **d1820001** Session detail: phone-width rollup of tag chips and Scheduled list: On a phone the session detail page spends about a third of the screen above the terminal on tag chips and the Scheduled list.
   - State running, priority 20, kind feature, tasks 5 of 5 done, expected about now, today.
   - Task (completed): Implement phone width Tags and Scheduled rollup in session detail.
   - Task (completed): Add rollup strings to five locale bundles and the locale guard test.
   - Task (completed): Add node test for phone width rollup and unchanged wide layout.
   - Task (completed): Update sessions deep dive docs for phone width rollup.
   - Task (completed): Final gate for the session detail phone width rollup.
5. **88b50030** (no title): Found 2026-11-18: a decomposed Automaton at priority 100 waited for approval for hours because the supervisor watch only raises its idle-lane findings when a lane is empty, and the lanes were full.
   - State planning, priority 100, kind feature, tasks 0 of 0 done, expected about 2026-11-18.
6. **a7f20001** Fix open high and critical CodeQL alerts: Fix: eleven open high or critical CodeQL alerts on main make the CodeQL workflow's alert gate fail on every push.
   - State needs_review, priority 99, kind bug, tasks 6 of 6 done, expected about 2026-11-18.
   - Task (completed): Fix path and command injection alerts in the autonomous package.
   - Task (completed): Fix certificate check and allocation size alerts in compute and server.
   - Task (completed): Fix the app.js cross-site scripting and sanitization alerts.
   - Task (completed): Rewrite the two flagged PWA tests without the flagged patterns.
   - Task (completed): Document the hardened path, command, certificate and HTML handling.
   - Task (completed): Final gate: run all touched suites and write the alert report.
7. **0b6e0001** pace_line: the supervisor's weekly and window usage guard rules inside the burn guard: Run the supervisor's weekly and window usage guard inside datawatch.
   - State needs_review, priority 90, kind feature, tasks 0 of 1 done, expected about 2026-11-18.
   - Task (queued): Config core: pace_line settings, defaults, one shared validator.
8. **9df40001** Retire the registry: proposal: Proposal-only Automaton.
   - State needs_review, priority 85, kind unset, tasks 3 of 3 done, expected about 2026-11-18.
   - Task (completed): Read and inventory the registry, findings, history and live records.
   - Task (completed): Write the retire-the-registry proposal document.
   - Task (completed): Final gate: verify the proposal end to end.
9. **ff540001** Fix the shutdowncause child process scan flake in the full test run: Fix: TestSignalChildProcScan in internal/shutdowncause fails in the full go test run of the module but passes alone.
   - State needs_review, priority 80, kind bug, tasks 3 of 3 done, expected about 2026-11-18.
   - Task (completed): Investigate the flake and record the root cause.
   - Task (completed): Fix the flake at its cause with explicit readiness signals.
   - Task (completed): Final gate: ten full race runs of the package under suite load.
10. **2caa0001** Fix housekeeping 503 from the API wrapper, the advance_judge read/wording faults, and the how-to: Every housekeeping action answers 503 "Housekeeping actions unavailable" because the server holds the wrong type.
   - State needs_review, priority 80, kind bug, tasks 5 of 5 done, expected about 2026-11-18.
   - Task (completed): Add seven housekeeping forwarding methods to the autonomous API wrapper.
   - Task (completed): Prove the real API wrapper passes the server housekeeping gate and serves all seven routes.
   - Task (completed): Fix the advance_judge group: one read per Automaton, one consistent finding per subject.
   - Task (completed): Update the housekeeping how-to for the fixed routes, the once-per-Automaton report and the finding wording.
   - Task (completed): Final gate for the housekeeping fix.
11. **e4f30001** Make the shutdowncause signal-capture process-scan test hermetic: The signal-child process-scan test in internal/shutdowncause (TestSignalChildProcScan) is not hermetic.
   - State needs_review, priority 80, kind bug, tasks 4 of 4 done, expected about 2026-11-18.
   - Task (completed): Make the shutdowncause procscan read from an injectable, descendant-scoped process source.
   - Task (completed): Add a fake-process-table regression test in shutdowncause.
   - Task (completed): Write the hermetic process-scan rule into the testing how-to.
   - Task (completed): Gate the hermetic shutdowncause scan end-to-end.
12. **964b0001** (no title): Decompose fails with a parse error when the planner writes a task verification field as a plain string.
   - State needs_review, priority 80, kind bug, tasks 0 of 4 done, expected about 2026-11-18.
   - Task (queued): Add a tolerant string-verification decode to ParsePlanning with unit tests.
   - Task (queued): Reclassify decompose parse failures as content retries that carry the parse error back to the planner.
   - Task (queued): Document the string verification acceptance and the content-classified parse retry in the planning-failure how-to.
   - Task (queued): Run the final gate and fix anything it reports.
13. **6c000001** (no title): The CLI and MCP session start cannot name a model, so the explicit-model policy refuses every Claude Code session they start.
   - State needs_review, priority 80, kind bug, tasks 0 of 6 done, expected about 2026-11-19.
   - Task (queued): Implement the LLM-row / daemon-default model fallback for direct claude-code session starts and the full-surface refusal message.
   - Task (queued): Add --model and --effort flags to the CLI session new command.
   - Task (queued): Add model, effort and name arguments to the MCP start_session tool.
   - Task (queued): Add the model= start option to the chat new: command.
   - Task (queued): Update the explicit-model how-to, the model-defaults how-to and the MCP tool docs.
   - Task (queued): Final gate: the PRD state for session-start model naming is consistent across code, docs and PWA.
14. **96820001** Repair the schedules-flood PWA e2e: temp-folder project root and default-view session selector: The schedule-flood PWA story fails for two reasons that predate the symlink removal.
   - State needs_review, priority 80, kind bug, tasks 5 of 5 done, expected about 2026-11-19.
   - Task (completed): Extract the e2e config generation into a shared helper that allows the temp project root.
   - Task (completed): Add the test that the generated e2e config carries the temp root.
   - Task (completed): Fix the schedules-flood seeded-session selector to the default tile view.
   - Task (completed): Document the generated e2e config's project-root allowance.
   - Task (completed): Gate the PWA e2e repair end-to-end.
15. **90d20001** Force-stage gitignored listed files in task auto-commit, and carry commit failure output into the hold: The automatic task commit fails when a task lists a file under the gitignored .datawatch folder, and the task then blocks with uncommitted_work.
   - State needs_review, priority 80, kind bug, tasks 0 of 3 done, expected about 2026-11-19.
   - Task (queued): Implement force-staging of ignored listed files and commit-failure output in the hold.
   - Task (queued): Update the uncommitted-work docs for force staging and failure reporting.
   - Task (queued): Final gate.
16. **2fbf0001** Authentication and multi-user design proposal: Design the authentication and authorization layer for datawatch: multi-user, local accounts, and OAuth and OpenID Connect compatible sign-in, across the web app, REST, MCP, CLI, messaging channels and federation.
   - State needs_review, priority 20, kind feature, tasks 0 of 2 done, expected about 2026-11-19.
   - Task (queued): Write the authentication and multi-user design document.
   - Task (queued): Final gate: verify the design document end to end.
17. **2c9a27e2** Personal agents with personality and decision-making (held placeholder): BL456 — Personal agents with personality and decision-making, for a real datawatch community user.
   - State needs_review, priority 0, kind research, tasks 0 of 1 done, expected about 2026-11-19.
   - Task (queued): Write the held placeholder plan document.
18. **316c41e1** Register the individual-builder harness research and its enhancement proposals: BL457 — Register completed research: individual-builder AI harness survey and datawatch enhancement proposals (PRD 8baa807b).
   - State needs_review, priority 0, kind research, tasks 0 of 2 done, expected about 2026-11-19.
   - Task (queued): Document the harness research proposals plan and link it from both indexes.
   - Task (queued): Final gate: confirm the plan record and both index links.
19. **36729dd7** Register the openrig.dev adoption research as a held item: BL458 — Register completed research: openrig.dev adoption plan for datawatch (PRD ef9dcac1).
   - State needs_review, priority 0, kind research, tasks 0 of 1 done, expected about 2026-11-19.
   - Task (queued): Write the openrig adoption registration document and link it from the tracker row.
20. **d5060001** Make the internal/autonomous race-detector run pass: Fix: the full go test -race run of internal/autonomous fails with a data race and two failing tests.
   - State blocked, priority 80, kind bug, tasks 1 of 3 done, expected about no date, blocked.
   - Task (completed): Fix the data race and the two failing race-run tests in internal/autonomous.
   - Task (review_needed): Fix the same race and timing patterns elsewhere in internal/autonomous.
   - Task (queued): Run ten consecutive full race-detector runs of internal/autonomous.
21. **70900001** Configuration parity audit and CI gate: A configuration parity audit and a CI gate.
   - State blocked, priority 20, kind feature, tasks 7 of 10 done, expected about no date, blocked.
   - Task (completed): Write the config key lister program.
   - Task (completed): Write the surface readers module.
   - Task (completed): Write the audit script with table output and exit code.
   - Task (completed): Create the empty tracked exemptions file.
   - Task (completed): Write unit tests for the audit script.
   - Task (completed): Write the configuration parity gap report.
   - Task (completed): Add baseline exemptions for the recorded gaps.
   - Task (blocked): Wire the configuration parity audit into CI as a gate.
   - Task (queued): Document the configuration parity audit.
   - Task (queued): Run the end-to-end configuration parity gate check.
22. **71f80001** Merge scripts in the repository with shadow pipeline comparison: Move the supervisor's merge scripts into the repository and run the daemon merge pipeline next to them in shadow, so the daemon can take over merging.
   - State blocked, priority 20, kind feature, tasks 0 of 12 done, expected about no date, blocked.
   - Task (review_needed): Investigate the supervisor merge steps and conflict rules.
   - Task (queued): Add the merge step scripts under scripts/merge.
   - Task (queued): Add the chain merge script under scripts/merge.
   - Task (queued): Add fake-based tests for every merge script decision.
   - Task (queued): Extend the recorded datawatch pipeline to every script step and validate it.
   - Task (queued): Add the shadow step runner and the agreement report.
   - Task (queued): Wire the shadow step run and comparison record into the daemon merge.
   - Task (queued): Show the step comparison on the MCP, CLI, comm channel and capability surfaces.
   - Task (queued): Show the step comparison on the Merge shadow card in the PWA.
   - Task (queued): Document the merge scripts, the pipeline record and the shadow comparison.
   - Task (queued): Smoke test the shadow step run in a throwaway data directory.
   - Task (queued): Run the end-to-end merge scripts and shadow final gate.
23. **2fa30001** (no title): A symlink with an absolute home path is tracked in the repository.
   - State blocked, priority 20, kind bug, tasks 4 of 6 done, expected about no date, blocked.
   - Task (completed): Remove tracked PWA node_modules symlink.
   - Task (completed): Wire runner node_modules install fallback.
   - Task (review_needed): Verify PWA suite post-removal.
   - Task (completed): Add absolute-symlink guard check.
   - Task (completed): Update PWA test setup docs.
   - Task (queued): Final gate: PWA test setup without tracked symlink.
24. **049f4713** BL433 — PWA performance audit under load (design/audit pass): BL433 — PWA performance audit under load, companion to the mobile app audit (GH issue #243, filed 2026-10-14).
   - State blocked, priority 0, kind unset, tasks 1 of 4 done, expected about no date, blocked.
   - Task (completed): Static code inventory of PWA polling, WS handling and rendering.
   - Task (failed): Live load measurement of the PWA via read-only proxy and Chromium.
   - Task (queued): Write the seven-check verdicts and ranked findings.
   - Task (queued): Register BL433 in the plans README roadmap queue.
25. **38b69678** [eval] model-tiering 20261015-r1: Eval scaffold placeholder for the model-tiering eval driver.
   - State blocked, priority 0, kind unset, tasks 17 of 52 done, expected about no date, blocked.
   - Task (completed): [eval] analysis__opus-low__r1.
   - Task (completed): [eval] analysis__opus-medium__r1.
   - Task (completed): [eval] analysis__opus-high__r1.
   - Task (completed): [eval] analysis__sonnet-low__r1.
   - Task (completed): [eval] analysis__sonnet-medium__r1.
   - Task (completed): [eval] analysis__sonnet-high__r1.
   - Task (failed): [eval] analysis__haiku-low__r1.
   - Task (failed): [eval] analysis__haiku-medium__r1.
   - Task (failed): [eval] analysis__haiku-high__r1.
   - Task (failed): [eval] analysis__fable-low__r1.
   - Task (failed): [eval] analysis__fable-medium__r1.
   - Task (failed): [eval] analysis__fable-high__r1.
   - Task (failed): [eval] analysis__default__r1.
   - Task (failed): [eval] scripting__opus-low__r1.
   - Task (failed): [eval] scripting__opus-medium__r1.
   - Task (failed): [eval] scripting__opus-high__r1.
   - Task (failed): [eval] scripting__sonnet-low__r1.
   - Task (failed): [eval] scripting__sonnet-medium__r1.
   - Task (failed): [eval] scripting__sonnet-high__r1.
   - Task (failed): [eval] scripting__haiku-low__r1.
   - Task (failed): [eval] scripting__haiku-medium__r1.
   - Task (failed): [eval] scripting__haiku-high__r1.
   - Task (failed): [eval] scripting__fable-low__r1.
   - Task (failed): [eval] scripting__fable-medium__r1.
   - Task (failed): [eval] scripting__fable-high__r1.
   - Task (failed): [eval] scripting__default__r1.
   - Task (failed): [eval] docs__opus-low__r1.
   - Task (failed): [eval] docs__opus-medium__r1.
   - Task (failed): [eval] docs__opus-high__r1.
   - Task (failed): [eval] docs__sonnet-low__r1.
   - Task (failed): [eval] docs__sonnet-medium__r1.
   - Task (failed): [eval] docs__sonnet-high__r1.
   - Task (failed): [eval] docs__haiku-low__r1.
   - Task (failed): [eval] docs__haiku-medium__r1.
   - Task (failed): [eval] docs__haiku-high__r1.
   - Task (completed): [eval] docs__fable-low__r1.
   - Task (failed): [eval] docs__fable-medium__r1.
   - Task (completed): [eval] docs__fable-high__r1.
   - Task (completed): [eval] docs__default__r1.
   - Task (completed): [eval] review__opus-low__r1.
   - Task (completed): [eval] review__opus-medium__r1.
   - Task (completed): [eval] review__opus-high__r1.
   - Task (completed): [eval] review__sonnet-low__r1.
   - Task (failed): [eval] review__sonnet-medium__r1.
   - Task (completed): [eval] review__sonnet-high__r1.
   - Task (failed): [eval] review__haiku-low__r1.
   - Task (failed): [eval] review__haiku-medium__r1.
   - Task (failed): [eval] review__haiku-high__r1.
   - Task (completed): [eval] review__fable-low__r1.
   - Task (completed): [eval] review__fable-medium__r1.
   - Task (failed): [eval] review__fable-high__r1.
   - Task (completed): [eval] review__default__r1.
26. **3be9c0e5** [eval] open-model-matrix 20261015-r10: Eval scaffold placeholder for the open-model matrix eval driver.
   - State blocked, priority 0, kind unset, tasks 0 of 16 done, expected about no date, blocked.
   - Task (failed): [eval] cpu-analysis.
   - Task (failed): [eval] cpu-scripting.
   - Task (failed): [eval] cpu-docs.
   - Task (failed): [eval] cpu-review.
   - Task (failed): [eval] fast-analysis.
   - Task (failed): [eval] fast-scripting.
   - Task (failed): [eval] fast-docs.
   - Task (failed): [eval] fast-review.
   - Task (failed): [eval] mid-analysis.
   - Task (failed): [eval] mid-scripting.
   - Task (failed): [eval] mid-docs.
   - Task (failed): [eval] mid-review.
   - Task (failed): [eval] decompose-validate-analysis.
   - Task (failed): [eval] decompose-validate-scripting.
   - Task (blocked): [eval] decompose-validate-docs.
   - Task (queued): [eval] decompose-validate-review.
27. **9a1a8666** [eval] open-model-matrix 20261015-r7: Eval scaffold placeholder for the open-model matrix eval driver.
   - State blocked, priority 0, kind unset, tasks 0 of 16 done, expected about no date, blocked.
   - Task (failed): [eval] cpu-analysis.
   - Task (failed): [eval] cpu-scripting.
   - Task (failed): [eval] cpu-docs.
   - Task (failed): [eval] cpu-review.
   - Task (failed): [eval] fast-analysis.
   - Task (failed): [eval] fast-scripting.
   - Task (failed): [eval] fast-docs.
   - Task (failed): [eval] fast-review.
   - Task (failed): [eval] mid-analysis.
   - Task (failed): [eval] mid-scripting.
   - Task (failed): [eval] mid-docs.
   - Task (failed): [eval] mid-review.
   - Task (failed): [eval] decompose-validate-analysis.
   - Task (failed): [eval] decompose-validate-scripting.
   - Task (blocked): [eval] decompose-validate-docs.
   - Task (queued): [eval] decompose-validate-review.
28. **f6a38c29** BL422 — Claude model/effort tiering eval for dev-Automaton tasks (eval only, no policy change): BL422 — Per-task Claude model/effort tiering eval for dev-PRD cost.
   - State blocked, priority 0, kind unset, tasks 2 of 4 done, expected about no date, blocked.
   - Task (completed): Eval fixtures for four task types and three Claude tiers.
   - Task (completed): Eval driver script with offline self-test.
   - Task (failed): Execute the eval matrix on the live daemon and commit raw results.
   - Task (failed): Write the model-tiering results doc and update tracking.
29. **fea73c82** BL437 — Recurring, hardware-aware LLM model/effort evaluation (design pass): BL437 — Recurring, hardware-aware LLM model/effort evaluation to keep datawatch's model advice and defaults current (design pass only, no implementation).
   - State blocked, priority 0, kind unset, tasks 1 of 2 done, expected about no date, blocked.
   - Task (completed): Write the BL437 recurring model-eval design doc.
   - Task (failed): Register BL437 in the docs/plans/README.md roadmap.
30. **5c0c0001** Remove the CPU burner and fix intermittent tests at their root: Remove the CPU load test and fix the intermittent tests at their root (operator, 2026-11-12 23:50: "things are not flaky under load, this host has great capacity, if it failed it was not load, this is the 2nd or 3rd time you did...
   - State failed, priority 80, kind bug, tasks 8 of 9 done, expected about no date, blocked.
   - Task (completed): Replace the burner with a deterministic fake and rename the load-sensitive status to intermittent.
   - Task (completed): Create a defect record automatically for an intermittent gate and surface it in the housekeeping view and an alert.
   - Task (completed): Add a CI check that fails when busy-loop or stress patterns appear in tests.
   - Task (completed): Fix the lifecycle queue overflow test and confirm the peer subscription and observer pull collector tests hold.
   - Task (completed): Fix the daemon cleanup helper test and the signal child process scan test.
   - Task (completed): Fix the PWA notify events test.
   - Task (completed): Update the supervisor rules and merge helper prompts so a failing test is a defect.
   - Task (completed): Document the intermittent gate, the defect record and the CI check in the how-to, definitions and operations docs.
   - Task (failed): Final gate for the burner removal and the intermittent test fixes.
31. **21030001** (no title): A task pinned to a local model can be handed to Claude on an Opus model by failure escalation.
   - State failed, priority 80, kind bug, tasks 2 of 2 done, expected about no date, blocked.
   - Task (completed): Failure escalation steps: resolve unnamed claude-code model and skip backed-off backends.
   - Task (completed): Docs set: failure escalation claude-code model default and backoff skip.
32. **f31d0001** Fix the shadows: plan size and version, changelog source, stored comparison, missing-plan reasons: Fix: the shadow merge plan leaves release size, version and changelog empty and its comparison is never stored.
   - State draft, priority 90, kind bug, tasks 0 of 0 done, expected about 2026-11-20.
   - Blocker: parse: json: cannot unmarshal string into Go value of type autonomous.verificationResultWire (cleaned="{\n \"title\": \"Merge shadow: plan size/version/changelog from the Automaton; store the post-shi
33. **bcf30001** (no title): Found 2026-11-16 while moving history into datawatch: the preview of the plans migration (datawatch plans migrate) says it would create 21 plans and leave 30 conflicts.
   - State draft, priority 80, kind unset, tasks 0 of 0 done, expected about 2026-11-21.
34. **2acf0001** (no title): Three PWA end-to-end stories fail on an isolated daemon.
   - State draft, priority 80, kind bug, tasks 0 of 0 done, expected about 2026-11-21.
   - Blocker: story "The PWA e2e story documentation set reflects the repaired stories and its embedded copy is in sync", task "Update docs/test-coverage.md, docs/testing-tracker.md and the testing how-to so the PW
35. **09c30001** (no title): Found 2026-11-16: infrastructure faults in the judge and the verifier are treated as findings against the work.
   - State draft, priority 70, kind unset, tasks 0 of 0 done, expected about 2026-11-21.
36. **c8f50001** (no title): Found 2026-11-16: the pi planner's replies fail to parse about one time in three, and each failure is recorded as not retryable, so the Automaton goes back to draft and needs a manual replan that waits behind the node engines...
   - State draft, priority 70, kind unset, tasks 0 of 0 done, expected about 2026-11-21.
37. **13cf0001** (no title): Found 2026-11-16 on an Automaton that failed its final gate and needed a corrective task added.
   - State draft, priority 65, kind unset, tasks 0 of 0 done, expected about 2026-11-21.
38. **08170001** (no title): Found 2026-11-16: the first CodeQL hardening round shipped and the scanner then reported twelve alerts still open (six closed), because the guards were written in shared helpers and the scanner only recognizes a check written...
   - State draft, priority 60, kind unset, tasks 0 of 0 done, expected about 2026-11-21.
39. **32100001** (no title): Found 2026-11-16: after a fix landed, the CodeQL check stayed red for two hours with no code cause.
   - State draft, priority 60, kind unset, tasks 0 of 0 done, expected about 2026-11-21.
40. **94560001** (no title): Make the judge and verifier time limit follow the size of the review and the backend.
   - State draft, priority 60, kind feature, tasks 0 of 0 done, expected about 2026-11-21.
41. **e81e0001** (no title): Found 2026-11-16: final-gate and verification scripts written by workers fail for the same three reasons over and over, and each one costs a gate failure, a hand repair and a reset of the gate and its template tasks (five...
   - State draft, priority 55, kind unset, tasks 0 of 0 done, expected about 2026-11-22.
42. **526f0001** (no title): Found 2026-11-16: the pi pool is capped at three, but the two compute nodes serve one engine each, so only two pi workers can run at once.
   - State draft, priority 50, kind unset, tasks 0 of 0 done, expected about 2026-11-22.
43. **8a3e0001** (no title): Found 2026-11-16 while promoting shipped features: several settings that ship off or in shadow have no definitions entry, and some have no documentation anywhere, so an operator cannot learn what turning one on does without...
   - State draft, priority 40, kind unset, tasks 0 of 0 done, expected about 2026-11-22.
44. **64650001** (no title): Found 2026-11-16 while supervising through the MCP tools: some reads are unbounded.
   - State draft, priority 40, kind unset, tasks 0 of 0 done, expected about 2026-11-22.
45. **70ae0001** (no title): Make the Projects and Plans pages create through the shared floating button and a dialog, not inline forms, the way the Automata, Automation, Templates and Sessions views already do.
   - State draft, priority 20, kind feature, tasks 0 of 0 done, expected about 2026-11-22.
46. **929d0001** (no title): Found 2026-11-16 while repairing Automata whose final gate failed or whose verifier timed out.
   - State draft, priority 20, kind feature, tasks 0 of 0 done, expected about 2026-11-22.
47. **234e0001** (no title): The plan-requirement coverage gate extracts file names from task and story text without trimming sentence punctuation or handling digits, so it reports files that exist as unresolved.
   - State draft, priority 20, kind bug, tasks 0 of 0 done, expected about 2026-11-22.
   - Blocker: task in story "" (index 0) has empty title or spec
48. **10980001** The llm models add and remove commands work from the CLI: datawatch llm models add answers HTTP 405 and the models command set is out of step with the daemon.
   - State draft, priority 20, kind bug, tasks 0 of 0 done, expected about 2026-11-22.
49. **f3540001** (no title): Choose a supervisor session for a project and an optional plan when launching a session, on every surface.
   - State draft, priority 20, kind feature, tasks 0 of 0 done, expected about 2026-11-23.
50. **cfb30001** (no title): A Claude Code session started with a task never receives the task text when Claude shows its development-channels confirmation.
   - State draft, priority 20, kind bug, tasks 0 of 0 done, expected about 2026-11-23.
51. **6acd0001** (no title): datawatch schedule add (the CLI) writes schedule.json directly and the running daemon never loads it, so a schedule created from the CLI never fires.
   - State draft, priority 20, kind bug, tasks 0 of 0 done, expected about 2026-11-23.
52. **674d0001** The schedule list shows fires, last fire time and result for schedules that fire: The schedule list shows no fires for schedules that do fire.
   - State draft, priority 20, kind bug, tasks 0 of 0 done, expected about 2026-11-23.
53. **f6430001** Changelog entries cut the Automaton title off mid word: Changelog entries cut the Automaton title off mid word.
   - State draft, priority 20, kind bug, tasks 0 of 0 done, expected about 2026-11-23.
54. **3c680001** (no title): HOLD: do not decompose or run this Automaton until the fast Projects and Automata pages Automaton (the roll-up and list performance work) is merged, because the script it wires in exists only on that branch until then.
   - State draft, priority 20, kind bug, tasks 0 of 0 done, expected about 2026-11-23.
55. **0fb4e302** Harness enhancement planning documents (eval sweep, eval DAG nodes, red-team validator): Based on the completed AI harness research in docs/plans/harness-research/, create detailed implementation PLANNING DOCUMENTS for all proposed enhancement proposals identified in enhancement-proposals.md: do not use internal BL...
   - State draft, priority 0, kind research, tasks 0 of 0 done, expected about 2026-11-23.
   - Blocker: session idle without completion (session=8723, backend=opencode, model=ollama/qwen3.8:27b, idle 15m0.000521533s > limit 15m0s): worker stalled
56. **404726a8** BL460 — Implement the unaddressed recommendations from the open-LLM research (PRD a2833a5e, completed...: BL460 — Implement the unaddressed recommendations from the open-LLM research (PRD a2833a5e, completed 2026-09-30).
   - State draft, priority 0, kind software, tasks 0 of 0 done, expected about 2026-11-23.
57. **79890001** (no title): BL439 — Research/design: can datawatch serve as infrastructure FOR Hermes Agent (Nous Research), not just learn from it.
   - State draft, priority 0, kind research, tasks 0 of 0 done, expected about 2026-11-24.
   - Blocker: unresolvable depends_on entry "Research: Hermes extensibility surface and datawatch substrate facts" on task "Write: datawatch-as-Hermes-infrastructure design report (needs_review)"

## Merged, review record still open


## Automations on the plan

- **9fdef1a4** Datawatch manages merging: Outcome: the daemon, not supervisor scripts, lands finished Automata: every branch has a landing record, finished work is swept, merged branches are reaped, unlanded work is counted, and the daemon merge pipeline runs in shadow... (status draft)
- **592af832** Projects tab and its sub-tabs work as one place to set up projects: Operators find, set up and browse projects from one Projects tab whose sub-tabs, layout and settings links all work. (status draft)
- **1644c59c** Projects Gantt shows an accurate, focusable timeline: The Projects Gantt draws the real schedule and its Focus view shows the right dates. (status draft)
- **448f8825** Plans and the release registry track Automata by release: Plans are ordered, renamed and reconciled as releases, and Automata attach to them by explicit link. (status draft)
- **0e7ac123** Automata lists group, tile and filter by state and project: Operators can group, tile and filter the Automata list by state and project, with correct counts. (status draft)
- **76927e64** Automata and Projects pages fit a phone screen: The Automata, Projects and Automaton detail tab strip lay out cleanly on a phone. (status draft)
- **71c2555f** Sessions, dashboard and shared navigation fit a phone: Shared navigation, the observer, sessions, the dashboard and the alerts header all fit a phone screen. (status draft)
- **0ef9c9f3** Automaton detail shows task status and notes accurately: The Automaton detail page shows real task status, notes kept off standard output, and cancelled tasks that do not block. (status draft)
- **aa8ddeb6** Automaton reads and the Automata CLI are fast and accurate: Automaton list, detail, safe-to-delete and CLI reads return quickly and show the current version. (status draft)
- **b6f37c36** Finished, held and cancelled Automata keep a safe status: Finished Automata show as Completed or Shipped, held Automata pause safely, and cancelled ones are archived after a grace period with unmerged work guarded. (status draft)
- **e5f7ffe8** Decompose follows project rules and fails visibly: Decompose reads project rules and context, lints its output, and reports planning failures instead of hiding them. (status draft)
- **4cf0770f** Workers start with the right project context and rules files: Workers and child Automata inherit the right project context, and rules files are never written with daemon or tracker noise. (status draft)
- **df96979e** Task output is checked against scope and committed work: A task completes only when its work is committed, stays within its file scope and passes the finish checks. (status draft)
- **99ec8719** The daemon repairs stuck and failed work without a human: The daemon repairs basic stuck states, orphaned slots and failed gates on its own and records what it did. (status blocked)
- **38af4745** A daemon restart keeps planning, gates and sessions alive: Restarting the daemon no longer loses planning, verification, final gates, usage back-off or running sessions. (status draft)
- **03bd1fde** Judge and verifier verdicts are fresh, bounded and recoverable: Judge and verifier runs review the current branch head, respect run caps and routing, and recover from blocked reviews. (status draft)
- **74cfb8f2** Task delivery to every session backend is reliable: Tasks reach every session backend intact, including long scripts and one-shot sessions that hit dialogs or restarts. (status draft)
- **e572c5c8** Capacity admission sends each task to a free lane: Each task is admitted to a free lane with working fall-through, and idle Automata no longer hold lanes. (status draft)
- **6f2410ad** Named pools and federated peers place work across nodes: Work moves between named compute pools and federated peers without stale leases or accidental node creation. (status draft)
- **d2553043** Tags and tile views organise sessions and Automata: Placement and system tags appear on first load, and tag filters and tile views work for sessions and Automata. (status draft)
- **d678ef5c** Sessions list grouping, filter sheet and controls: The sessions list groups by node and project, and its filter sheet and controls work on one line. (status draft)
- **bfad0781** Header server picker and scope icons stay compact: The header server dropdown and scope icons stay compact, remember the operator's choice, and switch the page to the chosen host. (status draft)
- **4132e46b** Alerts replace nav badges with real, quiet alerts: Vault and connectivity problems raise real alerts instead of nav badges, and the alerts header is clear. (status draft)
- **2cabb8b0** Code scanning alerts are closed and the CodeQL gate is bounded: Open code scanning alerts are closed with guards, and the CodeQL gate waits and uploads without blocking releases. (status draft)
- **20620ecf** CI scanners and Trivy checks run reliably on every build: Trivy and the image scanners install and scan the same way on every build, failing loudly when they cannot. (status draft)
- **c2b9e588** Merge pipeline lands Automaton work under approval modes: Automaton branches merge through a locked pipeline with release approval modes, conflict handling and shadow comparison. (status draft)
- **0e7458e4** Release and quality gates run and report honestly: Release smoke, the pre-tag run and quality gates run in the daemon and report real results before anything is tagged. (status draft)
- **26978feb** Flaky and slow tests are made deterministic and gated: Flaky, hermetic and slow tests are made deterministic, and slow ones sit behind opt-in targets. (status draft)
- **eff5124c** Autonomous storage, paths and secrets are hardened: Stored PRD writes, path handling, git calls, secret scans and encryption are audited and hardened. (status draft)
- **45272920** Standard task pack and task-type routing for Automata: The standard task pack, its installable packs and the task-type routing table give each Automaton task the right mode. (status draft)
- **6cd9b654** Spend ledger and burn guard keep usage within budget: Spend is recorded per Automaton and per LLM, and the burn guard projects usage from the readings. (status draft)
- **d817b8f7** Model and effort tiering evals stay parked for now: Model and effort tiering evals are recorded as eval-only work and stay parked rather than changing policy. (status draft)
- **0d4828f2** Automation data model and parallel chains built from design: The Automation data model, store and parallel chains with stage gating are built from their design records. (status draft)
- **bb357dec** Session history, traces and MCP and plugin links are reliable: Per-turn session history, trace retention, MCP connections and service plugins keep working across sessions. (status draft)
- **2fcfda2b** Session ends and resource ceilings are recorded and bounded: Every session end is attributed and recorded, and worker and tracker processes stay within memory ceilings. (status draft)
- **c99d62d9** Operator replies and pasted input reach the session reliably: Quick replies answer the pending question, pasted input is confirmed, and websocket input stays bounded. (status draft)
- **89dada82** Lifecycle events and operator decisions drive the supervisor: Lifecycle events and operator decisions reach the supervisor and notification sinks as subscribable events. (status draft)
- **65b046ea** Backends stall, throttle and start up without wedging work: Backend stalls and throttle lines are matched precisely, and a startup fault cannot wedge the daemon. (status draft)
- **0b4ab14f** Open-model and Hermes research registered as records: Open-model, Hermes and decompose research is registered as tracked records with their outcomes. (status draft)
- **83882295** Harness, OpenRig and voice research registered as held items: Harness, OpenRig, personal-agent and voice research is registered as held items for later decisions. (status draft)
- **d343c0d6** PWA connections, splash and refresh stay light and steady: The PWA keeps its connection, splash and refresh behaviour steady on cellular and Tailscale with light payloads. (status draft)
- **e24ce997** Repo CI watch holds completion until the pushed commit is green: The repo CI watch and its completion guardrail check the pushed commit and the head of main before a task completes. (status draft)
- **9f003487** Live drift probe runs confirmed the file-drift checks: Live probe runs on scratch repositories confirmed the file-drift verification and its strict and warn-only modes. (status draft)
- **86e93b37** Standalone fixes and small changes (Core automation and projects): Small Core fixes that fit no larger outcome: workflow API parsing, dependency updates and the skills mapping. (status draft)
- **664ab6f3** Standalone fixes and small changes (Sessions, backends and LLMs): Small Sessions fixes that fit no larger outcome: docs claims, the agent image and the effort flag. (status draft)
- **e8e71305** Promotion findings 2026-11-16: bugs and gaps found while turning shipped features on: Bugs, gaps and missing documentation found while promoting shipped features and supervising the Automata on 2026-11-16 (operator: file them as you go). (status draft)
- **ae659210** Manage everything through datawatch constructs, retire the registry: Operator direction 2026-11-14 and 2026-11-16: project, plan, Automation, Automaton and task are the only management constructs; a backlog item is an Automation not built out yet; the README is an export; the registry and finding... (status draft)
- **9803add8** Retire the registry: implementation (confirmed 2026-11-17): Implements the confirmed registry-retirement proposal in order: stop the post-ship registry writes, organize the Projects page by plan and attach every live Automaton and Automation to a plan, retire the registry and finding... (status draft)

