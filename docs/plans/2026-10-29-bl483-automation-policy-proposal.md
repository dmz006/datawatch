# BL483 Automation policy configurable per project: design proposal

**Date:** 2026-10-29
**Version:** none (planning document; plan-document commits are exempt from the version bump)
**Tracker:** BL483

Confirmed: no — no option is chosen. This file records the problem and the options; nothing is decided and no implementation PRD is filed until a `Confirmed: yes` line replaces this one.

## Problem

Registry row 64 asks that every option for how automation runs be configurable per project, with the behaviour we have tested as the shipped default. Today most options are daemon-global keys under `autonomous.*`; per-project scope exists only as per-Automaton overrides. Several options in the row have no key at all, and the supervisor holds them in its head or in the rules doc. BL473's coordinator reads this policy, so gaps here become manual work there.

## What exists today (verified)

Scope facts: `AutonomousConfig` (`internal/config/config.go`, about line 2134) is one daemon-wide block. No project-level policy type exists (search for a project policy found nothing; `internal/profile/project.go` holds git, image, memory and hooks, not automation policy). Per-Automaton overrides exist on the PRD record (`internal/autonomous/models.go`): git, verification backend and model, failure escalation, retry policy, quality gates, `max_concurrent_tasks`, `refuse_dirty_tree_start`. Config keys reach REST (`/api/config`), the CLI config set, MCP `config_set` and the PWA Settings fields generically; the PWA lists most keys explicitly (`app.js`, autonomous settings fields).

| Policy option in row 64 | Configurable today | Where (key) | Per project |
|---|---|---|---|
| Release rules (minor for features, patch for fixes, version in two files) | no | rules doc only; `scripts/check-merge-version.sh` | no |
| Cadence (poll, watch, tick) | yes | `autonomous.poll_interval_seconds`, `lane_fill.poll_seconds`, `automation.idle_alert_minutes`, `triage_alert_minutes` | global |
| Lane limits | yes | `lane_fill.gpu_lanes`, `claude_lanes`; `max_concurrent_tasks`; `plan_tier_max_inflight` | global (task limit per Automaton) |
| Backends and models | yes | `planning_*`, `verification_*`, `execution_backend`, `lane_fill.gpu_backend/gpu_model/claude_backend`, `guardrail_model` | global; per Automaton and per task |
| Retry and escalation limits | yes | `retry_policy`, `retry_ceiling_total`, `auto_fix_retries`, `infra_retry_escalate_after`, `failure_escalation.{enabled,max_failures,steps}` | global; per Automaton |
| Plan checks | yes | `plan_coverage_check`, `plan_coverage_min_ratio`, `plan_review_mode`, `scope_lint` | global |
| Override-lock states | yes | `task_override_statuses` | global |
| Git default | yes | `default_auto_pr`; per-Automaton git workflow | global; per Automaton |
| Timeouts | yes | `planning_timeout_seconds`, `verification_timeout_seconds`, `judge_timeout_seconds`, `task_max_attempt_seconds`, `task_no_progress_seconds`, `ci_watch_timeout_seconds`, `stuck_running_seconds` | global |
| Defect intake | no | no key; defects are filed by hand as registry rows and draft PRDs | no |
| Escalation channel | partly | `autonomous.notify.*` (events, webhook, forwarding); no key picks the channel for operator questions | global |
| Minimum model context (64k) | no | not enforced; only in `2026-10-18-model-host-requirements.md` | no |
| Pinned model per node | partly | pinned-model enforcement for pi-family sessions (`internal/autonomous/pinned_model.go`) and the compute node pin (`internal/compute/node.go`) | per node, not per project |
| Model-swap rule | no | referenced as BL494 in the host doc; no BL494 row or key found | no |

Gaps: five options have no key (release rules, defect intake, min context, model-swap rule, escalation channel selection) and no option is project-scoped except through per-Automaton overrides.

## Options

**A. Add the missing keys, keep scope global.** New `autonomous.policy.*` keys for the five gaps; defaults equal tested behaviour. Cost: 1 PRD, about 10 to 14 tasks. Risk: "per project" in row 64 stays unmet; other users running datawatch with several projects share one policy.

**B. A project policy layer over the global block.** A `policy` object stored on the project record (project alias), merged over the global defaults when an Automaton resolves its project, with an effective-policy read. Includes the five new keys. Cost: 2 PRDs (resolution layer and surfaces; the five keys), about 20 to 28 tasks. Risk: precedence confusion with per-Automaton overrides; the merge must be explicit and shown (project over global, Automaton over project).

**C. B limited to the keys BL473 reads first.** Project scope only for plan checks, retry and escalation, git default, lane limits and the new keys; the rest stays global until asked. Cost: 2 PRDs but about 14 to 20 tasks. Risk: a later widening needs another pass.

## Recommendation

Option C. It meets "per project" where the coordinator needs it, keeps the shipped defaults identical, and avoids moving every timeout into two scopes at once. Precedence stays Automaton over project over global.

## Open questions for the operator

1. A, B or C?
2. Is the project record (project alias) the right owner of a policy, or a separate named policy that projects point at (yes: project record; no: named policy)?
3. Is the minimum model context enforced (refuse to start on a smaller context) or only warned (yes: enforce)?
4. What does the model-swap rule say: swap never allowed, allowed only when the node is idle, or allowed with operator approval (answer N, I or A)? BL494 has no row, so should it be filed?
5. Defect intake: should a failed or blocked Automaton auto-file a draft defect row and PRD, or only alert (yes: auto-file)?

## Dependencies and order

Goes before BL473 option B (its checks read the mode keys). Depends on nothing shipped. The escalation-channel key should wait for BL610 (operator decision channel) Step 2, which owns the return path. The model-swap key overlaps BL472 and BL491 scheduling in the host doc.

## Parity surface

| Surface | Touched by B or C |
|---|---|
| Config | yes, `autonomous.policy.*` plus the project block, `docs/config-reference.yaml` |
| REST | yes, project policy read and write, effective-policy read |
| MCP | yes, project upsert gains policy, effective read |
| CLI | yes, project policy set and show |
| Comm channel | yes, show effective policy |
| PWA | yes, Settings fields and a project policy panel; five locale keys per string |
| Mobile | yes, one parity issue for the project REST contract |
