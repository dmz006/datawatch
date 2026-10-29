# Pre-10.0.0 design proposals: index and the questions in one list

**Date:** 2026-10-29
**Version:** none (planning document; plan-document commits are exempt from the version bump)
**Tracker:** rows 244 and 245 in `docs/plans/README.md`

Operator, 2026-10-29, quoted: "Yes, draft the design proposals", in answer to the supervisor's offer to draft one short design proposal for each item below so the operator can answer in one pass. Every proposal says `Confirmed: no`; nothing is decided and no implementation PRD is filed until the operator answers and a `Confirmed: yes` line replaces it.

## The eight proposals

| Item | File | Recommended | What it unlocks |
|---|---|---|---|
| BL473 Automation coordinator | `2026-10-29-bl473-automation-coordinator-proposal.md` | B: finish the BL609 steps plus one pre-flight and post-flight checks PRD | the supervisor's manual checks move into the daemon |
| BL483 Automation policy | `2026-10-29-bl483-automation-policy-proposal.md` | C: project scope for the options the coordinator reads, plus five missing keys | per-project automation settings |
| BL556 parallel tasks in one PRD | `2026-10-29-bl556-parallel-tasks-proposal.md` | A: a read-only task class that skips the tree write lock | up to about 1.7x faster PRDs |
| BL412 self-config gate | `2026-10-29-bl412-self-config-gate-proposal.md` | C: fold the boolean flag into the capability system | one mechanism, one audit point |
| BL414 cost rollup | `2026-10-29-bl414-cost-rollup-proposal.md` | A: one cost report over existing data, no new storage | per Automaton, Automation, backend and week reports |
| BL422 follow-up (tier-mismatch event) | `2026-10-29-bl422-tier-mismatch-event-proposal.md` | C: resume a narrowed BL422 eval with a usage ceiling, then define the event | unblocks the BL422 eval records |
| Upstream monitoring | `2026-10-29-upstream-monitoring-proposal.md` | C: Dependabot plus a small scheduled job for pinned tools, human merge first | security patches consumed when published |
| BL620 protect the rules files | `2026-10-29-bl620-protect-rules-files-proposal.md` | B first: judge reads rules from the base commit plus a protected-paths gate | workers cannot weaken the rules |

## The questions, in one list

Answer by number with a letter or yes or no; "recommended" for any item means the recommended option in the table and the default answers each file proposes.

- **BL473:** (1) A, B or C? (2) may the verifier pre-run block a task start when the script is missing (yes) or only alert (no)? (3) is the registry-versus-README check report-only (yes) or may it rewrite a row (no)? (4) may the daemon push and tag a completed minor version without asking (only matters for C)? (5) is the rules section 4 plan list the full set of plan checks?
- **BL483:** (1) A, B or C? (2) is the project record the owner of a policy (yes) or a separate named policy (no)? (3) is the minimum model context enforced (yes) or only warned (no)? (4) the model-swap rule: never, only when the node is idle, or with operator approval (N, I or A); and should BL494 get a registry row? (5) should a failed or blocked Automaton auto-file a draft defect row and PRD (yes) or only alert (no)?
- **BL556:** (1) A, B, C or D? (2) may decompose set the read-only flag itself, or must the operator or supervisor confirm each (decompose or confirm)? (3) keep the daemon default of `max_concurrent_tasks` at 1 until the build ships (yes or no)? (4) if B, a merge conflict between two tasks blocks the Automaton or goes to triage (block or triage)? (5) run a measurement of the read-only share first (yes or no)?
- **BL412:** (1) A, B, C or D? (2) new sessions get config writes by default (allowed) or denied until granted (denied)? (3) if allowed, covering secrets, LLM registry and federation too (all) or only non-secret settings? (4) audit every session configuration change on every surface (yes or no)? (5) file the audit of shell and token reach as its own item ahead of the build (yes or no)?
- **BL414:** (1) A, B, C or D? (2) "per week" means the calendar week or the subscription week the calculator anchors to (calendar or subscription)? (3) show dollars for subscription backends as labelled estimates at API rates (yes or no)? (4) include opt-in peer spend (yes or no)? (5) is 31 days of history enough (enough) or must it be longer, which forces option B?
- **BL422 follow-up:** (1) A, B, C or D? (2) paid-usage ceiling for the eval run: yes to the proposed 25 dollars, or give a number? (3) narrowed grid: all 4 task types, the 3 Claude models in use, effort low and high only (yes) or change it? (4) classify a task by operator tag, a decompose-time label, or inferred by the daemon? (5) verification tier in scope, which needs the guarded global config flip (yes or no)?
- **Upstream monitoring:** (1) A, B or C? (2) may a security update PR auto-merge when all checks are green (default no)? (3) if yes, may the merge also auto-release (yes or no)? (4) keep the 72-hour cooldown for non-security updates only (yes or no)? (5) advisory feeds that trigger: Go vulnerability database and GitHub advisories only, or also the Trivy database for base-image packages?
- **BL620:** (1) A, B, C or D (or A now and the rest later)? (2) protected list exactly AGENT.md, the supervisor rules, CODEOWNERS and the workflows (yes) or name additions? (3) a task that touches a protected path is held for the operator (hold) or only warned (warn)? (4) may the operator authorise a path per Automaton (yes or no)? (5) stop the daemon writing the cross-session block into AGENT.md (yes or no)?

## Items the supervisor holds outside the proposals

- **BL644** (Automation members for work with no PRD): option C was chosen; question 2 (an unattached pending item holds the Automation, or is skippable on a timer; the draft assumes it holds) and question 3 (registry rows come from parsing `docs/plans/README.md` or from a typed title and reference) are still open.
- **BL517** (model-to-node routing): the model pin covers most of it; cancel it and file a small PRD for the two pieces that are not on main (refuse a model no node serves at pin time, show each node's served models), cancel only, or leave parked.
- **BL486** (push on complete, PRD `92fe7faf`): an operator design decision: should the harness push finished work, and to which remote.
- **BL645** (hung model request, PRD `95950001`): investigate first, then choose among the options it writes up.
- **BL433** (PWA performance audit): needs Claude measurement tooling; proposed after the Sunday reset.
- **Security observation from the BL412 draft (unverified in a live run):** `POST /api/cost/usage` requires only the read capability `analytics:read`; whether a write there matters is part of BL412 question 5.

## Notes on verification

The drafting agents flagged claims they could not fully verify in code, listed at the end of each file's report: several "nothing exists" statements rest on searches of `internal/` only (the verifier pre-run, the registry-versus-README check, the merge-pending suffix writer, tag use), task and PRD counts are rough estimates, and whether branch protection and Dependabot security updates are enabled in the GitHub settings is not visible from the repository.
