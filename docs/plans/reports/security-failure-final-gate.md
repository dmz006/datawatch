# Security failure report — final gate

Run on 2026-10-30, 02:44–02:48 local, from the `f1f20052` worktree (branch `automaton/f1f20052`, HEAD
`357288242`). No code was changed. Every command ran under `env -u TMUX -u TMUX_TMPDIR`, so no tmux
command could reach the production socket. The working tree was clean before and after.

## Verdict

**Every listed command passed, but the gate is recorded as a failure** because the live check this gate
must record ended in `Result: FAIL` (see "Live check"). The live check found that the feature stops
working after the daemon saves its config and restarts, which is a defect in the shipped behaviour, not
a missing fixture. The final line of this report is therefore `Result: FAIL`.

## Commands

All times are local, 2026-10-30. Logs were kept under the session scratchpad, not in the repository.

| # | Command | Start | End | Elapsed | Exit |
|---|---|---|---|---|---|
| 1 | `go build ./...` | 02:44:44 | 02:44:48 | 4 s | 0 |
| 2 | `go vet ./cmd/datawatch/ ./internal/autonomous/ ./internal/config/ ./internal/federation/ ./internal/mcp/ ./internal/router/ ./internal/server/` | 02:44:48 | 02:44:49 | 1 s | 0 |
| 3 | `go test -count=1 ./...` | 02:44:49 | 02:47:31 | 162 s | 0 |
| 4 | `make lint` | 02:47:31 | 02:48:08 | 37 s | 0 |
| 5 | `node --test internal/server/web/*.test.js` | 02:48:08 | 02:48:10 | 2 s | 0 |
| 6 | `scripts/check-no-direct-ignore-writes.sh` | 02:48:10 | 02:48:10 | 0 s | 0 |
| 7 | `scripts/check-no-internal-refs.sh` | 02:48:10 | 02:48:10 | 0 s | 0 |
| 8 | `scripts/check-howto-coverage.sh` | 02:48:10 | 02:48:10 | 0 s | 0 |

Results in detail:

* **go build:** no output, exit 0.
* **go vet:** run on the seven packages whose Go files this branch changes since its merge-base with
  `main` (`75cf04c27`): `cmd/datawatch`, `internal/autonomous`, `internal/config`, `internal/federation`,
  `internal/mcp`, `internal/router`, `internal/server`. No output, exit 0.
* **go test -count=1 ./...:** 82 packages `ok`, 12 with no test files, 0 failed, 0 other lines.
* **make lint:** runs the repository checks (tmux, test daemons, service scope, go pins, internal refs,
  security ignore writes) and then `golangci-lint run ./...`, which printed `0 issues.`
* **node --test:** 1591 tests, 1591 pass, 0 fail, 0 cancelled, 0 skipped.
* **check-no-direct-ignore-writes.sh:** `OK: generated ignore files match the registry`.
* **check-no-internal-refs.sh:** `PASS: no internal refs in user-facing surfaces.`
* **check-howto-coverage.sh:** `PASS: every howto under docs/howto/ has exec_steps or is LLM-only.`

## Parity issue

`docs/plans/parity-issues/security-failure-exception.md` records the datawatch-app parity issue as
**#306** (https://github.com/dmz006/datawatch-app/issues/306), titled "[sync] feat(automaton): security
failure report route and PWA card".

## Live check

`docs/plans/reports/security-failure-shadow-live-check.md` ends with **`Result: FAIL`**. Its summary:

* Passed: in shadow the report is stored with a `would_stop` line and nothing is stopped or alerted; in
  `on` the linked Automaton is blocked, the report is copied byte-identically into its triage record and
  exactly one alert is raised; REST, MCP, CLI, the comm verb and the PWA card agree in both modes; no
  security run on GitHub was rerun, cancelled or deleted.
* Failed: after the daemon saved its config and reloaded it, the unset `security_workflows` default had
  become an explicit empty list, so no run was classified `security` and the feature was silently off in
  every mode until the list was set by hand. The `on` phase only worked after that workaround.
* Also failed as specified: the fixtures could not be created through the API, so a throwaway repository,
  a `gh` double and patched Automaton files were used.
* Reported and not fixed: the comm verbs send no `Authorization` header, so they answer 401 on a
  token-protected daemon; a run assessed in shadow is never assessed again in `on`.

This task changes no code, so the live-check failure stays open.

## Files this branch changes

`git diff --stat main` is a two-dot diff against the current tip of `main` (`9581221c2`). This branch
forked at `75cf04c27`, is 12 commits ahead of `main` and **61 commits behind it**, so the listing below
also shows the work that landed on `main` after the fork as deletions (83 deleted files, among them the
merge, overlap and worker-question features). Those deletions are not changes made by this branch; they
disappear when the branch is rebased onto `main`. The branch's own change set, measured from its
merge-base, is `71 files changed, 6134 insertions(+), 24 deletions(-)`.

Full output of `git diff --stat main`:

```
 .trivyignore                                       |  26 -
 CHANGELOG.md                                       |  39 +-
 Makefile                                           |   7 +-
 README.md                                          |  25 +-
 cmd/datawatch/cli_autonomous.go                    |  16 +-
 cmd/datawatch/cli_merge.go                         |  82 ---
 cmd/datawatch/cli_merge_test.go                    | 100 ----
 cmd/datawatch/cli_overlap.go                       |  69 ---
 cmd/datawatch/cli_overlap_test.go                  | 156 ------
 cmd/datawatch/cli_security_failure.go              |  71 +++
 cmd/datawatch/cli_security_failure_test.go         | 149 ++++++
 cmd/datawatch/main.go                              |  79 +--
 .../question_worker_marker_config_test.go          |  47 --
 docs/README.md                                     |   6 +-
 docs/api-mcp-mapping.md                            |   2 -
 docs/api/autonomous.md                             | 275 +---------
 docs/api/mobile-surface.md                         |  75 ---
 docs/api/openapi.yaml                              | 307 +----------
 docs/commands.md                                   |   3 -
 docs/config-reference.yaml                         | 114 +---
 docs/cursor-mcp.md                                 |   4 +-
 docs/data-flow.md                                  |   1 -
 docs/datawatch-definitions.md                      |  17 +-
 docs/flow/automation-merge-pending-flow.md         | 169 ------
 docs/flow/automaton-create-overlap-flow.md         |  82 ---
 docs/howto/README.md                               |   1 -
 docs/howto/automation-repo-ci-watch.md             |   9 +
 docs/howto/automation-security-failure.md          | 231 ++++++++
 docs/howto/automation-tab.md                       |  23 -
 docs/howto/automaton-search-before-create.md       | 238 ---------
 docs/howto/operator-questions-workers.md           | 262 ---------
 docs/implementation.md                             |  83 ++-
 docs/mcp.md                                        |  86 +--
 docs/operations.md                                 | 154 ++----
 docs/parity-status.md                              |   5 +-
 ...-10-25-automation-supervisor-duties-proposal.md |   5 +-
 ...026-10-25-operator-decision-channel-proposal.md |   4 +-
 ...9-automation-merge-pending-final-gate-report.md | 167 ------
 ...26-10-29-automation-merge-pending-live-check.md | 157 ------
 ...29-operator-question-step4-final-gate-report.md | 122 -----
 ...026-10-29-operator-question-step4-live-check.md | 106 ----
 .../2026-10-29-search-before-create-live-check.md  | 100 ----
 docs/plans/2026-10-29-search-before-create.md      | 107 ----
 docs/plans/README.md                               |   4 +-
 .../2026-10-29-automation-merge-pending-parity.md  | 168 ------
 .../2026-10-29-search-before-create.md             | 117 ----
 .../operator-question-step4-worker-marker.md       | 106 ----
 .../parity-issues/security-failure-exception.md    | 114 ++++
 .../reports/security-failure-shadow-live-check.md  | 360 +++++++++++++
 docs/security-review.md                            |  23 +-
 docs/testing-tracker.md                            | 190 +++----
 docs/testing.md                                    |  38 +-
 internal/audit/cef.go                              |   2 -
 internal/audit/events.go                           |   8 -
 internal/audit/question_marker_refused_test.go     | 107 ----
 internal/autonomous/api.go                         |   9 -
 internal/autonomous/automation_coordinator.go      |  37 +-
 internal/autonomous/automation_idle.go             |  15 +-
 internal/autonomous/await_decision.go              |  93 ----
 internal/autonomous/await_decision_test.go         | 316 -----------
 internal/autonomous/await_external.go              |  72 +--
 internal/autonomous/decision_gate_executor_test.go |   3 -
 internal/autonomous/manager.go                     | 101 +---
 internal/autonomous/merge_compare.go               | 139 -----
 internal/autonomous/merge_compare_test.go          | 202 -------
 internal/autonomous/merge_pending_pass.go          | 400 --------------
 internal/autonomous/merge_pending_pass_test.go     | 590 ---------------------
 internal/autonomous/merge_safe_delete.go           | 152 ------
 internal/autonomous/merge_safe_delete_test.go      | 181 -------
 internal/autonomous/merge_shadow_record.go         | 154 ------
 internal/autonomous/merge_status.go                | 134 -----
 internal/autonomous/merge_text.go                  | 154 ------
 internal/autonomous/models.go                      |   4 -
 internal/autonomous/overlap_finder.go              | 364 -------------
 internal/autonomous/overlap_finder_test.go         | 384 --------------
 internal/autonomous/overlap_matcher.go             |  94 ----
 internal/autonomous/overlap_matcher_test.go        | 154 ------
 internal/autonomous/overlap_shadow.go              |  33 --
 internal/autonomous/overlap_shadow_test.go         | 112 ----
 internal/autonomous/overlap_sources.go             | 164 ------
 internal/autonomous/overlap_sources_test.go        | 185 -------
 internal/autonomous/overlap_text.go                |  56 --
 internal/autonomous/question_tick.go               | 347 ------------
 internal/autonomous/question_tick_test.go          | 313 -----------
 internal/autonomous/questions.go                   |  53 --
 internal/autonomous/questions_store.go             |  33 +-
 internal/autonomous/repo_ci_watch_poll.go          |   1 +
 internal/autonomous/repo_ci_watch_record.go        |   4 +
 internal/autonomous/security_failure_advisory.go   | 183 +++++++
 .../autonomous/security_failure_advisory_test.go   | 231 ++++++++
 internal/autonomous/security_failure_assess.go     | 165 ++++++
 .../autonomous/security_failure_assess_test.go     | 200 +++++++
 internal/autonomous/security_failure_config.go     |  79 +++
 .../autonomous/security_failure_config_test.go     | 111 ++++
 internal/autonomous/security_failure_decide.go     | 267 ++++++++++
 .../autonomous/security_failure_decide_test.go     | 237 +++++++++
 internal/autonomous/security_failure_guard_test.go | 174 ++++++
 internal/autonomous/security_failure_pass.go       | 359 +++++++++++++
 internal/autonomous/security_failure_pass_test.go  | 437 +++++++++++++++
 internal/autonomous/security_failure_registry.go   | 109 ++++
 .../autonomous/security_failure_registry_test.go   | 157 ++++++
 internal/autonomous/security_failure_render.go     | 153 ++++++
 internal/autonomous/task_visibility.go             |  19 +-
 internal/autonomous/task_visibility_test.go        |   4 -
 internal/autonomous/triage_pass.go                 |   5 +
 internal/autonomous/triage_record.go               |  24 +-
 internal/autonomous/triage_text.go                 |  25 +
 internal/autonomous/wait_reason.go                 |  38 +-
 internal/autonomous/wait_reason_test.go            |  28 -
 internal/autonomous/worker_question.go             | 187 -------
 .../autonomous/worker_question_branches_test.go    | 417 ---------------
 internal/autonomous/worker_question_stats.go       |  69 ---
 internal/autonomous/worker_question_test.go        | 443 ----------------
 internal/autonomous/worktree.go                    |  19 +-
 internal/config/config.go                          | 207 +++-----
 internal/config/merge_pending_config_test.go       | 109 ----
 internal/config/overlap_config_test.go             | 107 ----
 internal/config/questions_worker_marker_test.go    |  98 ----
 internal/config/security_failure_config_test.go    |  57 ++
 internal/federation/mcp_tool_caps.go               |   5 +-
 internal/mcp/autonomous.go                         |  21 +-
 internal/mcp/merge_pending_config_test.go          |  47 --
 internal/mcp/merge_tools.go                        |  47 --
 internal/mcp/merge_tools_test.go                   | 112 ----
 internal/mcp/overlap_tools.go                      |  48 --
 internal/mcp/overlap_tools_test.go                 |  91 ----
 internal/mcp/question_stats_tool.go                |  26 -
 internal/mcp/question_stats_tool_test.go           |  78 ---
 internal/mcp/question_worker_marker_config_test.go |  39 --
 internal/mcp/security_failure_tools.go             |  44 ++
 internal/mcp/security_failure_tools_test.go        | 174 ++++++
 internal/mcp/server.go                             |   5 +-
 internal/metrics/metrics_test.go                   |  29 -
 internal/metrics/prometheus.go                     |   8 -
 internal/router/merge_comm.go                      |  72 ---
 internal/router/merge_comm_test.go                 | 203 -------
 internal/router/overlap_comm.go                    |  57 --
 internal/router/overlap_comm_test.go               |  97 ----
 .../router/question_worker_marker_config_test.go   |  79 ---
 internal/router/router.go                          |   6 +-
 internal/router/security_failure_comm.go           |  72 +++
 internal/router/security_failure_comm_test.go      | 188 +++++++
 internal/router/sx2_parity.go                      |   8 +-
 internal/server/api.go                             | 207 ++------
 internal/server/autonomous.go                      |  41 +-
 internal/server/gh201_phase4_audit_test.go         |   5 -
 internal/server/merge.go                           | 107 ----
 internal/server/merge_pending_config_test.go       | 128 -----
 internal/server/merge_test.go                      | 287 ----------
 internal/server/overlap.go                         |  98 ----
 internal/server/overlap_config_test.go             | 137 -----
 internal/server/overlap_create.go                  | 202 -------
 internal/server/overlap_create_test.go             | 317 -----------
 internal/server/overlap_live_smoke_test.go         | 310 -----------
 internal/server/overlap_rest_test.go               | 207 --------
 internal/server/question_raise_dispatch.go         |  10 -
 internal/server/question_worker_stats.go           |  51 --
 internal/server/question_worker_stats_test.go      | 133 -----
 internal/server/questions.go                       |   1 -
 internal/server/questions_tick_list_test.go        |  68 ---
 .../server/questions_worker_marker_config_test.go  |  94 ----
 internal/server/security_failure.go                | 129 +++++
 internal/server/security_failure_config_test.go    | 100 ++++
 internal/server/security_failure_locale_test.go    | 122 +++++
 internal/server/security_failure_pwa_test.go       |  67 +++
 internal/server/security_failure_test.go           | 246 +++++++++
 internal/server/server.go                          |  15 +-
 internal/server/v5280_locales_test.go              |  47 +-
 .../server/v8_73_33_automata_terminology_test.go   |  24 +-
 .../web/app-autonomous-settings-fields.test.js     |  90 ----
 internal/server/web/app-merge-pending.test.js      | 248 ---------
 internal/server/web/app-overlap-warnings.test.js   | 160 ------
 .../server/web/app-question-decision-link.test.js  | 179 -------
 internal/server/web/app-security-failure.test.js   | 194 +++++++
 .../server/web/app-worker-question-stats.test.js   |  81 ---
 internal/server/web/app.js                         | 405 +++++---------
 internal/server/web/locales/de.json                |  70 +--
 internal/server/web/locales/en.json                |  72 +--
 internal/server/web/locales/es.json                |  70 +--
 internal/server/web/locales/fr.json                |  70 +--
 internal/server/web/locales/ja.json                |  70 +--
 internal/server/web/openapi.yaml                   | 231 +++-----
 internal/session/manager.go                        |   8 -
 internal/session/manager_worker_question_test.go   | 208 --------
 internal/session/question_marker.go                | 333 ------------
 internal/session/question_marker_test.go           | 247 ---------
 internal/session/tracker.go                        |   1 -
 internal/session/tracker_test.go                   |  60 ---
 internal/stats/collector.go                        |   9 -
 internal/stats/collector_test.go                   |  27 -
 scripts/check-no-direct-ignore-writes.sh           |  86 +++
 scripts/release-smoke.sh                           | 109 ----
 scripts/test-stories/TS-838.sh                     |  10 -
 .../test-stories/pwa/question-decision-link.mjs    | 129 -----
 security/accepted-risks.yml                        |  18 -
 templates/session-CLAUDE.md                        |   2 -
 196 files changed, 6182 insertions(+), 16503 deletions(-)
```

For reference, the branch's own change set (`git diff --stat 75cf04c27 HEAD`):

```
 CHANGELOG.md                                       |   1 +
 Makefile                                           |   7 +-
 README.md                                          |  13 +-
 cmd/datawatch/cli_autonomous.go                    |   5 +
 cmd/datawatch/cli_security_failure.go              |  71 ++++
 cmd/datawatch/cli_security_failure_test.go         | 149 +++++++
 cmd/datawatch/main.go                              |   2 +
 docs/README.md                                     |   1 +
 docs/config-reference.yaml                         |  27 ++
 docs/cursor-mcp.md                                 |   1 +
 docs/datawatch-definitions.md                      |   3 +
 docs/howto/automation-repo-ci-watch.md             |   9 +
 docs/howto/automation-security-failure.md          | 231 +++++++++++
 docs/implementation.md                             |  62 +++
 docs/mcp.md                                        |  16 +-
 docs/operations.md                                 |  31 ++
 docs/parity-status.md                              |   1 +
 ...-10-25-automation-supervisor-duties-proposal.md |   2 +-
 .../parity-issues/security-failure-exception.md    | 114 ++++++
 .../reports/security-failure-shadow-live-check.md  | 360 +++++++++++++++++
 docs/security-review.md                            |  20 +
 docs/testing-tracker.md                            |  90 +++++
 internal/autonomous/automation_idle.go             |   3 +-
 internal/autonomous/manager.go                     |  27 ++
 internal/autonomous/repo_ci_watch_poll.go          |   1 +
 internal/autonomous/repo_ci_watch_record.go        |   4 +
 internal/autonomous/security_failure_advisory.go   | 183 +++++++++
 .../autonomous/security_failure_advisory_test.go   | 231 +++++++++++
 internal/autonomous/security_failure_assess.go     | 165 ++++++++
 .../autonomous/security_failure_assess_test.go     | 200 ++++++++++
 internal/autonomous/security_failure_config.go     |  79 ++++
 .../autonomous/security_failure_config_test.go     | 111 ++++++
 internal/autonomous/security_failure_decide.go     | 267 +++++++++++++
 .../autonomous/security_failure_decide_test.go     | 237 +++++++++++
 internal/autonomous/security_failure_guard_test.go | 174 ++++++++
 internal/autonomous/security_failure_pass.go       | 359 +++++++++++++++++
 internal/autonomous/security_failure_pass_test.go  | 437 +++++++++++++++++++++
 internal/autonomous/security_failure_registry.go   | 109 +++++
 .../autonomous/security_failure_registry_test.go   | 157 ++++++++
 internal/autonomous/security_failure_render.go     | 153 ++++++++
 internal/autonomous/triage_pass.go                 |   5 +
 internal/autonomous/triage_record.go               |  11 +
 internal/autonomous/triage_text.go                 |  25 ++
 internal/config/config.go                          |  70 ++++
 internal/config/security_failure_config_test.go    |  57 +++
 internal/federation/mcp_tool_caps.go               |   1 +
 internal/mcp/security_failure_tools.go             |  44 +++
 internal/mcp/security_failure_tools_test.go        | 174 ++++++++
 internal/mcp/server.go                             |   1 +
 internal/router/router.go                          |   6 +-
 internal/router/security_failure_comm.go           |  72 ++++
 internal/router/security_failure_comm_test.go      | 188 +++++++++
 internal/router/sx2_parity.go                      |   4 +-
 internal/server/api.go                             |  57 +++
 internal/server/security_failure.go                | 129 ++++++
 internal/server/security_failure_config_test.go    | 100 +++++
 internal/server/security_failure_locale_test.go    | 122 ++++++
 internal/server/security_failure_pwa_test.go       |  67 ++++
 internal/server/security_failure_test.go           | 246 ++++++++++++
 internal/server/server.go                          |  11 +-
 internal/server/v5280_locales_test.go              |  26 ++
 .../server/v8_73_33_automata_terminology_test.go   |   4 +
 internal/server/web/app-security-failure.test.js   | 194 +++++++++
 internal/server/web/app.js                         | 122 +++++-
 internal/server/web/locales/de.json                |  28 +-
 internal/server/web/locales/en.json                |  30 +-
 internal/server/web/locales/es.json                |  28 +-
 internal/server/web/locales/fr.json                |  28 +-
 internal/server/web/locales/ja.json                |  28 +-
 internal/server/web/openapi.yaml                   |  81 ++++
 scripts/check-no-direct-ignore-writes.sh           |  86 ++++
 71 files changed, 6134 insertions(+), 24 deletions(-)
```

Result: FAIL
