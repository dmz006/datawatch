# Pre-10.0.0 design proposals: index and the questions in one list

**Date:** 2026-10-29
**Version:** none (planning document; plan-document commits are exempt from the version bump)
**Tracker:** rows 244 and 245 in `docs/plans/README.md`

Operator, 2026-10-29, quoted: "Yes, draft the design proposals", in answer to the supervisor's offer to draft one short design proposal for each item below so the operator can answer in one pass. Every proposal says `Confirmed: no`; nothing is decided and no implementation PRD is filed until the operator answers and a `Confirmed: yes` line replaces it.

Confirmed: yes. The operator answered every proposal in the queue between 2026-10-30 and 2026-11-01; the answers, with the operator's words, are recorded in the two tables near the end of this file, and every behaviour is a setting with those answers as the default. One item is parked by the operator's answer and stays unbuilt: the Claude model tiering eval and the tier-mismatch event, until after 10.0. This line was missing until 2026-11-02, which kept the implementation drafts out of the lane fill; the supervisor should have added it when the answers were read.

## The eight proposals

| Item | File | Recommended | What it unlocks |
|---|---|---|---|
| BL473 Automation coordinator | `historical-plans/2026-10-29-bl473-automation-coordinator-proposal.md` | B: finish the BL609 steps plus one pre-flight and post-flight checks PRD | the supervisor's manual checks move into the daemon |
| BL483 Automation policy | `2026-10-29-bl483-automation-policy-proposal.md` | C: project scope for the options the coordinator reads, plus five missing keys | per-project automation settings |
| BL556 parallel tasks in one PRD | `historical-plans/2026-10-29-bl556-parallel-tasks-proposal.md` | A: a read-only task class that skips the tree write lock | up to about 1.7x faster PRDs |
| BL412 self-config gate | `historical-plans/2026-10-29-bl412-self-config-gate-proposal.md` | C: fold the boolean flag into the capability system | one mechanism, one audit point |
| BL414 cost rollup | `historical-plans/2026-10-29-bl414-cost-rollup-proposal.md` | A: one cost report over existing data, no new storage | per Automaton, Automation, backend and week reports |
| BL422 follow-up (tier-mismatch event) | `historical-plans/2026-10-29-bl422-tier-mismatch-event-proposal.md` | C: resume a narrowed BL422 eval with a usage ceiling, then define the event | unblocks the BL422 eval records |
| Upstream monitoring | `historical-plans/2026-10-29-upstream-monitoring-proposal.md` | C: Dependabot plus a small scheduled job for pinned tools, human merge first | security patches consumed when published |
| BL620 protect the rules files | `historical-plans/2026-10-29-bl620-protect-rules-files-proposal.md` | B first: judge reads rules from the base commit plus a protected-paths gate | workers cannot weaken the rules |

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

## Answers given in the question queue, 2026-10-30

Answered in the PWA (surface pwa) unless noted; the quotes are the operator's words exactly as stored with the answer.

| Time (EDT) | Question | Answer | Operator's words |
|---|---|---|---|
| 06:55 | BL433 audit | Yes, start after the Sunday reset | (none) |
| 13:33 | Claude lanes to Sunday | 2 lanes (via the CLI, relayed from chat) | "2 lanes, go ahead" |
| 13:36 | BL644 pending items | Hold the Automation until the item is attached; parse the README rows | "Other users of datawatch may not use the same readme numbering sequence,  so need to track internally but support file registry,  but really this world be an internal datawatch function" |
| 13:38 | push on complete (BL486) | Yes: push the finished work branch to origin (never force, visible not-pushed warning) | (none) |
| 13:38 | model routing (BL517) | Cancel the old rows and file one small PRD for the two missing pieces | (none) |
| 13:40 | chain advance rule | Yes: a merged member counts as completed, so chains advance | "These merge questions should be configurable,  other datawatch users may want their own settings, my selection is default" |
| 13:41 | hung request (BL645) | Option 1: observe request progress (no remediation yet) | (none) |
| 13:42 | BL620 rules files | Confirm the recommended option (B first) and its default answers | "These questions should be configurable,  other datawatch users may want their own settings, my selection is default" |

Standing design rule from these answers (operator, 2026-10-30, quoted: "need to remember most options need to be configurable since others may use datawatch in their own way, my answers are default and our use case"): every behaviour choice a PRD builds is a configuration key whose default is the operator's answer, because other datawatch users will run it their own way. Consequences recorded: the BL644 answer says the registry the coordinator tracks should be an internal datawatch function that also supports a file registry (a README table) as one source, not the README numbering as the source of truth; the chain advance rule, the push on complete behaviour and the BL620 protected paths are all configurable with these answers as defaults.


## Answers given in the question queue, 2026-10-31 to 2026-11-01

All thirteen open questions were answered while Claude was at its limit and the supervisor could not read them; they were read on 2026-11-01 14:30. Each answer is immutable in the queue; the quotes below are verbatim. Every behaviour stays a setting with these answers as the default, so other users can choose their own.

| Question | Answer | Operator's words | Follow-up |
|---|---|---|---|
| Five open code scanning alerts, all in test code | Fix the test code so the alerts disappear | (no words) | PRD filed: fix the five alerts in test code |
| What the dashboard is for | Console first, boards later | "We already have the observer,  we don't need to replicate what is there in boards,  but boards for a dashboard are different and higher level,  some observer cards may be good dashboard cards" | PRD filed: dashboard card audit and the operator console; boards follow |
| What the daemon does alone at a merge conflict | Additive conflicts mechanically; for the rest a strong model proposes a resolution that must pass the tests, otherwise ask | "This should be an option for others and be able to select model of they code what i did" | in the pipeline PRD: conflict policy and model are settings |
| Who runs the supervisor loop for other users | Plain daemon code for the mechanical steps, a small local model for narrow jobs behind validators, escalate after two failures | "The model and style,  long running or not,  should be selectable,  and results and documentation observable to see how it is performing and needs tuning" | PRD filed: the supervisor loop in the daemon |
| Where a project's merge pipeline is defined | Both, the file winning over the record | "When we make tutorial and onboarding stuff this should be part of that,  set up a report for datawatch automation,  help users use datawatch" | pipeline PRD re-filed with the onboarding report |
| Where evals run: placing models on nodes | Pin a model to a node, and load the model when a node has the resources | "It can be pinned but should also ask and add model of enough resources,  no reason to fail when we can load a model " | PRD filed: load a missing model on a node with resources |
| Keeping dependencies and images patched | Dependabot plus a small scheduled job for what it cannot see | "And this is just for us in maintaining datawatch,  not part of what users of datawatch need as they are not maintaining datawatch code" | PRD filed: maintainer-only upstream monitoring |
| Claude model tiering eval and the mismatch event | Park until after 10.0 | (no words) | none, parked |
| What things cost | One cost report on the existing data | "Add per call costs when ready" | PRD filed: cost report; per-call costs later |
| Who may change settings from a session | Fold the switch into the capability system as a built-in group | (no words) | PRD filed |
| Per-project automation settings | A project policy for the settings the coordinator reads first | (no words) | PRD filed |
| Automation coordinator and the manual checks | The checks feature plus the daemon pushes and tags each finished release and watches its CI | "But again should be configurable,  other users may want to adjust and use differently,  be flexable and creative" | PRD filed |
| Parallel tasks in one Automaton | Each task in its own worktree, merged back in order | "A and b, read only should be able to run parallel,  work tree is an option users can use and should be default" | PRD filed: read-only tasks in parallel and a worktree per task as the default |

## Answer given in chat, 2026-11-01

| Question | Answer | Operator's words | Follow-up |
|---|---|---|---|
| How a shipped Automaton or Automation is shown | Both: completed plus merged fields, and a shipped filter | "both, and backfill the old ones" | PRD `89560001` unblocked: merged fields, the Shipped chip and filter, and a backfill of the 207 mislabelled records from git history |
