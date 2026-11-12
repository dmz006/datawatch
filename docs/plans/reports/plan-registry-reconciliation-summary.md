# Plan registry reconciliation — summary

Summary of the six batch reports against the list
([plan-registry-reconciliation-list.md](plan-registry-reconciliation-list.md),
count 118) and the registry in `docs/plans/README.md`. Class `a`: the
file's subject already has a registry row. Class `b`: a real plan or
shipped feature with no registry row (a proposed row is given). Class
`c`: the file is not a plan or decision. Class `d`: unclear.

## Counts per class

- class a: 111
- class b: 3
- class c: 4
- class d: 0
- total: 118 (equal to the list count of 118)

## Class (b) proposed rows

In registry order (ascending, the order in which they would be added to
the `docs/plans/README.md` table), one per line, ready to paste (the pipe
is escaped as the registry does):

CI-watch completion guardrail: an Automaton's completion is gated on the CI result of its own commit (how-to `docs/howto/ci-watch-guardrail.md`, live-verified 2026-10-13) \| done, shipped in v9.0.63
Per-task, per-story and per-Automaton verification LLM override: the verifier is chosen per task, story or Automaton instead of a single global setting (live end-to-end proof 2026-10-14) \| done, shipped in v9.0.73
Autonomous triage records: the triage of failed and blocked Automata is readable through the daemon's surfaces (REST GET /api/autonomous/triage, MCP, CLI, comm channel, the PWA card in the Automaton detail view, five locale bundles), with the Android / iPhone surface owed to datawatch-app issue 284 \| done, shipped in 9.17.1

Source files, in the same order: `2026-10-13-ci-watch-live-verification.md`,
`2026-10-14-verification-llm-override-live.md`,
`2026-10-26-autonomous-triage-mobile-parity-note.md`.

## Class (d) questions

None. No batch (01 to 06) classified any of the 118 listed files as
class `d`.

## Plan files naming uncovered work

None. Every file that names work is covered: the 111 class `a` files by
their registry row in `docs/plans/README.md`; the 3 class `b` files by
the Automata that carried the work (probe Automata and their shipped
releases v9.0.63, v9.0.73 and 9.17.1) — they owe only registry rows,
which the "Class (b) proposed rows" section supplies. Note that two of
the three are already summarised as shipped work in `HISTORY.md` (BL423,
v9.0.63; BL430, v9.0.73), which the registry rules name as the summary
for shipped work, so those two rows are optional. Only the autonomous-
triage record feature (9.17.1) has no entry in either the registry or
HISTORY.md. The 4 class `c` files are investigations, references or a
passing gate's counts and commit to no work.

## Open items for operator review

- Deciding whether to add the three proposed class `b` rows to
  `docs/plans/README.md`. Two of the three features (BL423, v9.0.63;
  BL430, v9.0.73) are already summarised in `HISTORY.md`, which the
  registry rules use for shipped work, so the rows are optional there;
  the triage-record row (9.17.1) is the only one missing from both.
- Confirming the datawatch-app issue number in the triage row before it
  is added: registry row 193 cites "datawatch-app parity issue 284" for
  the Step 1 CI-watch parity (merged 2026-10-27), and the triage parity
  note files `dmz006/datawatch-app` #284 for the triage card (2026-10-27).
  Two different parity debts sharing one number should be checked against
  the datawatch-app repository so the row cites the right one.
- Nothing else is outstanding: all 118 files in the coverage table exist
  under `docs/plans/`, the coverage table and the
  [full list](plan-registry-reconciliation-list.md) agree file-for-file,
  and every path referenced by the batches and this summary resolves.

## Coverage table

One row per listed file, in list order (batches 01 to 06):

| file | class |
|---|---|
| 2026-10-13-ci-watch-live-verification.md | b |
| 2026-10-14-cli-self-update-dialog-recovery.md | c |
| 2026-10-14-verification-llm-override-live.md | b |
| 2026-10-20-bl463-mcp-drop-findings.md | a |
| 2026-10-20-oneshot-startup-dialog-dismiss.md | a |
| 2026-10-20-plan-coverage-gate.md | a |
| 2026-10-20-tag-browsing-tile-design.md | a |
| 2026-10-20-voice-long-recording-rootcause.md | a |
| 2026-10-21-datawatch-app-lifecycle-subscriptions-note.md | a |
| 2026-10-21-pwa-connection-stability.md | a |
| 2026-10-22-observer-mobile-fit.md | a |
| 2026-10-23-datawatch-app-gate-scope-note.md | a |
| 2026-10-23-datawatch-app-notify-events-note.md | a |
| 2026-10-23-datawatch-app-tags-note.md | a |
| 2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal.md | a |
| 2026-10-23-quality-gate-scope.md | a |
| 2026-10-23-session-store-write-amplification.md | a |
| 2026-10-24-datawatch-app-capacity-node-usage-note.md | a |
| 2026-10-25-live-held-task-review.md | a |
| 2026-10-25-restart-during-launch.md | a |
| 2026-10-25-shared-nav-mobile-fit.md | a |
| 2026-10-25-tag-browsing-decisions.md | a |
| 2026-10-25-usage-calculator-mobile-contract.md | a |
| 2026-10-26-autonomous-triage-mobile-parity-note.md | b |
| 2026-10-26-autonomous-triage-read-route.md | a |
| 2026-10-28-ci-watch-flake.md | a |
| 2026-10-28-codeql-last-three-alerts.md | a |
| 2026-10-28-pi-descendant-only-stall.md | a |
| 2026-10-28-ten-oh-arc-automation-draft.md | a |
| 2026-10-29-automation-cli-status-gaps-final-gate-report.md | a |
| 2026-10-29-automation-merge-pending-final-gate-report.md | a |
| 2026-10-29-automation-merge-pending-live-check.md | a |
| 2026-10-29-bl412-self-config-gate-proposal.md | a |
| 2026-10-29-bl414-cost-rollup-proposal.md | a |
| 2026-10-29-bl422-tier-mismatch-event-proposal.md | a |
| 2026-10-29-bl473-automation-coordinator-proposal.md | a |
| 2026-10-29-bl556-parallel-tasks-proposal.md | a |
| 2026-10-29-bl620-protect-rules-files-proposal.md | a |
| 2026-10-29-decision-gate-final-gate-report.md | a |
| 2026-10-29-decision-gate-live-check.md | a |
| 2026-10-29-operator-question-step2-gate-report.md | a |
| 2026-10-29-operator-question-step3-gate-report.md | a |
| 2026-10-29-search-before-create-live-check.md | a |
| 2026-10-29-upstream-monitoring-proposal.md | a |
| 2026-10-29-verifier-wait-lane-live-check.md | a |
| 2026-10-29-verifier-wait-lane-occupancy-final-gate-report.md | a |
| 2026-10-30-automation-cleanup-final-gate-report.md | a |
| 2026-10-30-automation-cleanup-live-check.md | a |
| 2026-10-30-automation-merged-member-final-gate-report.md | a |
| 2026-10-30-automation-merged-member-live-check.md | a |
| 2026-10-30-automation-uncommitted-work-gate-report.md | a |
| 2026-10-30-automation-uncommitted-work-live-check.md | a |
| 2026-10-30-held-state-questions-final-gate-report.md | a |
| 2026-10-30-held-state-questions-live-check.md | a |
| 2026-10-30-infra-retry-final-gate-report.md | a |
| 2026-10-30-infra-retry-live-check.md | a |
| 2026-10-30-infra-retry.md | a |
| 2026-10-30-operator-question-clarity-final-gate-report.md | a |
| 2026-10-30-operator-question-clarity-live-check.md | a |
| 2026-10-30-test-tmp-cleanliness-report.md | a |
| 2026-10-30-test-tmp-leak-survey.md | a |
| 2026-10-30-usage-lanes-final-gate-report.md | a |
| 2026-10-30-usage-lanes-live-check.md | a |
| 2026-10-31-automaton-protected-paths-gate-report.md | a |
| 2026-10-31-automaton-protected-paths-live-check.md | a |
| 2026-10-31-decision-records-plan.md | a |
| 2026-10-31-lane-fill-eligibility-final-gate-report.md | a |
| 2026-10-31-lane-fill-eligibility-live-check.md | a |
| 2026-10-31-model-request-progress-final-gate-report.md | a |
| 2026-10-31-model-request-progress-live-check.md | a |
| 2026-10-31-model-request-progress.md | a |
| 2026-10-31-model-routing-final-gate-report.md | a |
| 2026-10-31-model-routing-live-check.md | a |
| 2026-10-31-model-routing.md | a |
| 2026-11-01-merge-pipeline-and-onboarding-report.md | a |
| 2026-11-01-parallel-worktree-live-smoke-report.md | a |
| 2026-11-01-shipped-status-parity-issue.md | a |
| 2026-11-01-shipped-status.md | a |
| 2026-11-02-checks-and-ship-live-check.md | a |
| 2026-11-02-checks-and-ship.md | a |
| 2026-11-02-cost-report-parity-issue.md | a |
| 2026-11-02-dashboard-card-audit.md | a |
| 2026-11-02-dashboard-console-app-parity-issue.md | a |
| 2026-11-02-post-ship-hooks.md | a |
| 2026-11-03-automaton-safe-delete-fast-report-gate.md | a |
| 2026-11-03-autonomous-priority-by-kind.md | a |
| 2026-11-03-busy-state-gate-report.md | a |
| 2026-11-03-decompose-lint-final-gate-report.md | a |
| 2026-11-03-placement-auto-tags-parity.md | a |
| 2026-11-03-planning-pilot.md | a |
| 2026-11-03-secret-scan-push-gate.md | a |
| 2026-11-03-server-select-parity-issue.md | a |
| 2026-11-03-spec-truncation-parity-issue.md | a |
| 2026-11-03-truncation-marker-refusal-rule-review-fixes.md | a |
| 2026-11-04-completion-review-autofix.md | a |
| 2026-11-06-dashboard-chart-examples.md | c |
| 2026-11-06-imap-mcp-service-tracking.md | a |
| 2026-11-06-supervisor-loop-archive-grace-live-check-report.md | a |
| 2026-11-06-supervisor-loop-archive-grace.md | a |
| 2026-11-07-agent-mail-design-pass.md | c |
| 2026-11-07-daemon-sections-protected-paths.md | a |
| alert-fix-verification-report.md | a |
| ancestor-claude-md-findings.md | a |
| bl462-restart-audit-findings.md | a |
| codeql-alert-gate-smoke-result.md | a |
| codeql-analyze-upload-failure-findings.md | a |
| git-exec-argument-handling.md | a |
| identity-backup-smoke-result.md | a |
| identity-loss-root-cause.md | a |
| path-injection-alert-decisions.md | a |
| pretasksha-retry-smoke.md | a |
| restart-matrix-findings.md | a |
| silent-first-tool-call-findings.md | a |
| silent-first-tool-call-matrix.md | a |
| smoke-check-gate-report.md | c |
| task-delivery-early-exit-root-cause.md | a |
| usage-calculator-zone-parity-issue.md | a |
| web-list-fetch-audit.md | a |
