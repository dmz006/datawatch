# Handover plan: from supervisor-run to daemon-run Automation

**Date:** 2026-11-03
**Version:** written at 9.71.0
**Status:** draft for the operator; each phase starts only on the operator's word
**Scope:** move the mechanical parts of running Automata (planning review, stage mechanics, lane fill, merge, ship, deploy, watch and tick) from the supervisor session into the daemon, in shadow mode first, with the supervisor staying as the monitor of exceptions, bugs and improvements. Written so another operator can follow the same pattern with their own settings.

## Where we are

Automated today: task execution, verification scripts, judge reviews, infrastructure retries, plan coverage, scope lint, the burn guard, capacity accounting, CI.

Done by hand by the supervisor in one working day (2026-11-02 to 11-03): eight releases (trial merge, conflict resolution, review fixes, changelog, README line, version, ship, parity issue, deploy), about twenty-five decompose reviews with rewrites in most, approvals and backend settings, four rebuilds of one stage, re-queues, one dead worker session, CI run cleanups, registry rows and lessons, lane filling.

## Principles

1. **Shadow first.** Every handed-over step runs in shadow (records what it would do, does nothing) and is compared with the supervisor's real action for a fixed number of cases before it is switched on.
2. **One switch per step.** Each step has its own mode (off, shadow, on) and a kill switch; turning it off returns the step to the supervisor with no data loss.
3. **Evidence to move on.** A phase ends with counts-only evidence against its exit criteria, not an opinion.
4. **Rules stay with people.** The operator keeps: the 10.0.0 tag, design decisions, configuration authority (LLM entries, defaults, repository settings), and anything that spends money outside the plan. The daemon never edits the project rules files.
5. **Everything is configuration.** Nothing in the handover is tied to this installation: modes, thresholds, lists and names are settings with documented defaults, so another operator can run the same sequence.
6. **The supervisor does not disappear.** It reviews exceptions, reads the shadow comparisons, files defects and improvements the same day, and tunes rules and templates from the findings.

## Phase 0: measure the baseline (before anything changes)

Record, counts only, per day and per release:

| Measure | Source |
|---|---|
| Decomposes approved without an edit / with edits / rewritten | decompose lint scorecard and the supervisor ledger |
| Merge fixes made by hand (docs, tests, lint, conflicts) | merge log and the registry row |
| Minutes a lane sat idle with eligible work | lane utilization view |
| Manual re-queues, session kills, stage rebuilds | supervisor ledger |
| Red CI runs needing action | CI log |
| Releases shipped and releases deployed by hand | changelog and deploy log |

The ledger is a counts-only file the supervisor updates at each tick. These numbers are the "before".

Dependencies: decompose lint (running), lane utilization view (queued). Until they exist the ledger is kept by hand.

## Phase 1: plans that need no review

**Moves:** the supervisor's decompose review.
**How:** the decompose lint rules and task templates run on every decompose in shadow, then enforce with the retry ladder (touch-up, one re-plan with feedback, then the stronger planner), routed by the planning routing rules.
**Exit:** ten consecutive decomposes approved with no supervisor edit, across at least three kinds of work (UI, config and surfaces, defect with tests); the lint scorecard shows the false-positive rate under the agreed threshold.
**Rollback:** lint mode back to shadow; planner back to the global setting.
**Needs:** decompose lint (running), planning routing (approved), priority by kind (approved).

## Phase 2: stages that run themselves

**Moves:** building, starting and rebuilding stages.
**How:** an Automation can start from a fork or a nested Automation; a blocked Automation that never started returns to draft; queued planning runs and queued tasks can be re-routed to another pool; a node can be drained for maintenance; the release arc activates and advances stage by stage.
**Exit:** two consecutive stages start and finish with no rebuild; one node upgrade done by drain with no failed task.
**Needs:** activation fix (running), planner and worker re-queue (approved), node drain (planned), automatic grouping tags, the Gantt and lane utilization views for observation.

## Phase 3: lanes that fill themselves

**Moves:** the supervisor's lane filling.
**How:** lane fill goes shadow to on in two steps. Local-model lanes first (mechanical work, no spend), then the cloud lane under the burn guard and the automatic lane count. Proposal-file and eligibility rules are settled first (drafts filed from chat have no proposal file today).
**Exit:** three days where idle minutes with eligible work stay under the threshold; the shadow log and the real fills agree on at least ninety percent of decisions.
**Rollback:** lane fill mode back to shadow.
**Needs:** the lane fill eligibility fix, verifier slots with the headroom check (so verification does not starve), the auto lane count.

## Phase 4: review and merge by the daemon

**Moves:** the trial merge, mechanical conflict resolution, review fixes, changelog and README entries, version, parity issue, registry row.
**How, in order:**
1. The judge separates blocking findings from documentation findings. Documentation and process findings become automatic corrective tasks instead of a block; a real code or security finding still blocks.
2. The merge pipeline runs in shadow: it builds the trial merge, resolves what is mechanical (version files, changelog and README lines, locale JSON), runs lint and tests, and writes the merge plan it would execute; the supervisor compares it with the manual merge.
3. For patch-level changes with a clean verdict the pipeline goes on; minor releases still need an explicit confirmation until the comparison is clean; the 10.0.0 tag stays with the operator.
4. The datawatch-app parity issue is filed by the merge step after the check that the PWA changed.
**Exit:** ten merges where the shadow plan equals the manual merge (or is better), including at least two with conflicts and two with PWA changes.
**Needs:** the checks (shipped), classification verdicts (approved), review-timeout state (approved), spec-read truncation fix (approved), the read-only report path (approved), protect-the-rules-files (blocked, to triage).

## Phase 5: ship and deploy

**Moves:** push, tag, CI watch, deploy, restart.
**How:** ship steps shadow to on (already built, default on once a release is finished; confirm the secret scan before push is in place first); deploy after ship drains the node, installs, restarts, health-checks, resumes in-flight planning, and rolls back on a failed check.
**Exit:** five releases pushed, tagged, watched and deployed with no manual step and no lost planning run.
**Needs:** secret scan before push (approved), post-ship hooks and deploy (approved), re-queue after restart.

## Phase 6: the watch and the tick in the daemon

**Moves:** the supervisor's twenty-minute watch and hourly tick.
**How:** the daemon supervisor loop runs the mechanical checks (idle lane, stuck session, red CI, merge ready, budget position, registry audit) as configured jobs with selectable model and style, and raises a plain question or alert only for exceptions. The supervisor reads the exception stream, not every tick.
**Exit:** one week where the loop's exception list matches what the supervisor would have raised, with no missed incident.
**Needs:** supervisor loop (approved), the stats pull for federated peers, the daemon-side CI watch.

## Phase 7: made usable by others

The onboarding guide and skills (final stage) turn this sequence into a procedure another operator can run on a new repository: inspect, propose settings, run in shadow, compare, switch on. The release audit and full validation placeholder checks the rules, the test matrix and the methodology before any 10.0.0 candidate.

## Metrics of success

| Measure | Baseline (Phase 0) | Target |
|---|---|---|
| Supervisor edits per decompose | most decomposes | under 0.1 |
| Manual steps per release | about 12 | 0 for patches, 1 confirmation for minors |
| Idle lane minutes with eligible work | hours per day | under 30 per day |
| Stage rebuilds | 4 in one day | 0 |
| Releases not deployed within an hour | several | 0 |
| Supervisor time on routine ticks | continuous | exceptions only |

## Open decisions for the operator

Each is asked separately, with a recommendation, when its phase is about to start:

1. Should minor releases need a confirmation from the operator at first? Recommendation: yes for the first ten, then automatic when the shadow comparison is clean.
2. Should the daemon push and tag, or only prepare them? Recommendation: push and tag, with the secret scan and the CI watch in place, because the supervisor already holds that authority.
3. Which planner handles security-sensitive work? Recommendation: start on the stronger planner and move to a local model when the scorecard supports it.
4. How long is the shadow period per phase? Recommendation: the exit criteria above, not a fixed time.

## Parity surface

| Surface | Touched by the handover | Note |
|---|---|---|
| REST | yes | each step's mode, status and comparison report |
| MCP | yes | the same reads and mode setting |
| CLI | yes | status, compare and mode commands per step |
| Comm channel | yes | summaries and exception alerts |
| YAML/config | yes | one mode and threshold block per step |
| PWA | yes | the Setup and policy, Resources and Inbox sub-tabs, the arc view and the Gantt |
| Android | yes | each operator-visible change gets a datawatch-app issue at its merge |
| iPhone | yes | same issues |

This plan document itself changes no code and no surface.

## Status

- Generated wrapping (change record and the five drafted artefacts: gate report, changelog draft, parity draft, tracker rows, registry row): built, unit-tested and documented in [howto/generated-artefacts.md](../howto/generated-artefacts.md); not yet validated on a live daemon. The datawatch-app parity issue is drafted in `docs/plans/parity-issues/generated-artefacts.md` and is filed by the supervisor at merge. Tracked in the [plans index](README.md).

## Operator decisions, 2026-11-07 evening (quoted)

- Finishing review and merge by the daemon (Phase 4) now, after the supervisor traced that the daemon writes its own generated documents and then holds the Automaton for them as uncommitted work. The supervisor offered three options: only exclude the generated files from the finish-time check (A), finish the intent now (B), or A now plus Phase 4 written down (C). Operator, quoted: "b finsih now we need to be using it to debug it and monitor its right. and you've been doign minor automated bu tit should be configurable, same for major there is a block on me aproving, but it could be configurable. ask the questions needed for it with these quetsions".
- Reading of that answer: the release approval for each size of release (patch, minor, major) is a setting with the modes automatic, ask and manual, not a convention. Today there is no such setting: the merge pipeline configuration has a conflict policy, a conflict model, version files and post-ship hooks, and the supervisor decided minor releases by hand and left a major release to the operator by rule only. The daemon runs the merge in shadow first, the supervisor compares its plan with the manual merge, and differences are recorded as defects.
- Earlier the same evening, quoted: "a, discard them - but isn't the intent of them so that the automation can do the files at merge/clsoe time vs you? isn't the goal for the automation to automate itself? you should just be validating it did it right or a validator check." The six uncommitted-work questions about the generated draft files were answered discard.
- Release approval policy (Phase 4 question 1 of 5), operator, quoted: "a that is correct, but you need to tightly monitor and debug/tune it and make sure it does what you would have done based on the events it runs into". Decision: a setting with a mode for each size of release (patch, minor, major), each automatic, ask or manual, per project overridable, on all six surfaces; defaults patch automatic, minor automatic, major ask (today's behaviour). The daemon's merge runs in shadow beside the supervisor's manual merge; the supervisor compares each plan with what it did, records every difference as a defect, and tunes until the daemon does what the supervisor would have done for the events it meets.
- Release size (Phase 4 question 2 of 5), operator, quoted: "a seems right a feature is a minor, a bug gives a patch. automatoin does seem right place to track and at project level that will probably define a major change across all minor updates. but i expect others may do this differently so needs to be flexable, do you suggest anything?" and then: "your suggestions are great and projects tend to start with a root idea that turns into the major feature that is developed over time as features are rolled into it and it's expaned and rounded out as it's tested and used untl the full major is ready for release as a working featureset. and there is overlap with other plans make sure you udpate them as needed and keep them in mind for this."

### Phase 4 design as decided (2026-11-07)

- **Approval modes.** One setting with a mode for each release size (patch, minor, major): `auto`, `ask` or `manual`, overridable per project, on all six surfaces, with help entries. Defaults keep today's behaviour: patch `auto`, minor `auto`, major `ask`.
- **Release size resolves in layers, most specific first.** (1) An explicit `release_size` on the Automaton (patch, minor, major or none). (2) The Automation (arc) it belongs to: an arc can be marked a milestone. (3) The project: a map from kind to size (default feature = minor, bug = patch, overridable) and the rule that triggers a major (`manual`, `milestone` or `never`). (4) The built-in default feature = minor, bug = patch. A major is never inferred from a diff or a kind.
- **The arc is the major feature being grown.** A project starts with a root idea; that becomes an Automation (arc) whose member Automata ship as minors and patches while it is tested, used and rounded out, and when the arc is complete it raises the major release step under the major mode. The 10.0.0 release is the first such arc.
- **A breaking-change flag** on an Automaton raises its release to the major approval mode, so a breaking change is never merged quietly.
- **Flexibility for other projects.** A release cadence per project (`per_merge` today, then `batch` by count or time window, or `manual`) and a version scheme per project (semantic versioning on the pipeline's version files by default; other schemes through the pipeline's custom step). Version 1 covers the kind map with the explicit override, the milestone, the breaking flag, per-merge cadence and semantic versioning; batch cadence and other schemes are the next step and the setting names leave room for them.
- **Order and version safety.** One merge at a time per repository (a merge lock); rebase onto the latest main inside the lock; compute the version after that rebase from main's version and the release size; fast-forward push only, redo from the rebase if main moved; a validator before push (both version variables equal, the top changelog heading equals the version, exactly one step above the last tag, the tag does not exist, the README line matches); every step safe to run twice.
- **Monitoring and tuning.** The daemon's merge runs in shadow beside the supervisor's manual merge; every merge is compared (version, heading, README line, registry row, parity issue, conflict resolutions, gate results); every difference is a defect row; the daemon is tuned until it does what the supervisor would have done for the events it meets. The first tests replay the events of 2026-11-07: a task branch never merged back, a stray tracked file at the repository root, a test that fails only under load, lint that differs between local and CI, a broken plan link and a stale settings-count test, a version bump that is not safe to run twice, a PWA suite that hangs, and a review that passed on a branch missing its fix.
- **First step, filed 2026-11-07:** Automaton `3e690001` makes the finish-time uncommitted-work check ignore the daemon's own unedited generated documents (the daemon wrote them and then held every Automaton for them). Open questions of the five still to ask: who pushes and tags, conflict handling in the shadow period, and the switch from shadow to on.

### Overlap with other plans (kept in step)

- [Automata pages redesign](2026-11-06-automata-pages-redesign.md): the project, arc and Automaton model; the arc as milestone is added there.
- [Supervisor duties, generic and project split](2026-10-30-supervisor-duties-generic-and-project-split.md): the merge pipeline as per-project configuration (registry row 268); the approval modes and release size rules are per-project settings in that block.
- The 10.0.0 requirements and the major release checklist: the first milestone arc.
- Counts by project and the Projects tab: release size and arcs are what the project page shows.
- The weekly dependency Automation (`4ce60001`): its release is a patch or minor under these rules.
- Who pushes and tags (Phase 4 question 3 of 5), operator, quoted: "a - the daemon, but again it should be configurable, and we should support making sure we document gh, do we provide an api token (should we?) do we have user login to gh or gitlab (should support all) anything else?" Decision: the daemon pushes and tags under the release approval modes, as a setting (push and tag on, or prepare only) per project. The supervisor found that today the daemon has a git provider layer (`internal/git`): GitHub works by running the `gh` command with the host's login, GitLab is a stub that answers not implemented, and no other host exists; the daemon holds no push token of its own (it uses the host's credential helper, SSH agent or `gh auth`, a token in the `DATAWATCH_GIT_TOKEN` environment variable for clones in a cluster, or the short-lived token broker for workers); the pre-push secret scan exists and defaults to shadow; and no user login flow to a git host exists. The credential and host model is asked next.
- Git host credentials (Phase 4 question 3, follow-up), operator, quoted: "we're usign gh command and i am currently logged in, should we support gh api token or need to help with gh login or keep doing what we do and recommend that to users?" and then: "a - host login we hae with fallback, and i like your other suggestions of what you'd add". Decision: the host's own login stays the default and is the recommended way (today `gh auth login` as `dmz006` with the scopes repo, workflow, read:org and gist, stored in the gh config file, no token in the daemon's service environment); a token in the secrets vault, referenced by name and passed to the host command as an environment variable, is the optional fallback for servers and clusters; datawatch holds no token of its own by default and does not build a login screen. Added to the plan: a Git hosts settings card (host, kind, base URL for self-hosted hosts, credential source) with a readiness check that shows the account and whether the scopes include repo and workflow and the exact fix command when not; a Test connection button; a release path setting (direct push, or open a pull request and merge when checks pass) for protected branches; GitLab implemented the same way through its own command line tool, other hosts through plain git over SSH or HTTPS; provider-neutral issue and release steps; the CI watch per host; optional signed tags; every push or tag audit-logged with the credential source and never the token; the how-to "Connect datawatch to your git host" with one table per host. The pre-push secret scan must be in enforce mode before the daemon pushes main on its own.
- Merge conflicts during shadow (Phase 4 question 4 of 5), operator, quoted: "a - looks most complete". Decision: conflicts are resolved by file class. Mechanical files (changelog, version lines, README line, locale JSON, generated indexes) are always resolved automatically. For code and documents the model proposes a resolution in a throwaway worktree; the build, lint and full tests must pass or the merge stops and asks; and the first N model-resolved code conflicts after the switch to on need the operator's confirmation (a new setting, default 5, 0 turns the confirmation off). In shadow the model's resolution is computed and compared with the supervisor's, never applied. The existing `conflict_policy` and `conflict_model` settings stay.
- From shadow to on (Phase 4 question 5 of 5), operator, quoted: "a - but you not me will be doing the validatoin and checks and as it gets better work to get to auto". Decision: the evidence bar stays (ten merges where the daemon's plan equals the manual merge or is better, at least two with conflicts and two with PWA changes, no blocking comparison defect), but the supervisor, not the operator, does the validation and the checks, reports the comparison, and moves the release modes toward `auto` as the daemon improves; each mode change is recorded as a decision with its evidence. Delegation as read by the supervisor: patch and minor modes are the supervisor's to move; the major mode stays `ask` (the operator's) unless the operator changes it. After a switch the daemon falls back to `ask` for that size on a failed validator, a red release CI within the window or a merge that needed a manual fix, raises an alert and files a defect; the supervisor keeps comparing a sample of merges.

### Phase 4 Automata filed 2026-11-08 (drafts, decomposed after the weekly budget reset)

In the order the plan builds them: `3e690001` the finish-time check ignores the daemon's own generated documents (first step, already filed); `99be0001` merge lock, pre-push validator and idempotent steps; `253e0001` release approval modes; `603b0001` release size rules (layers, milestone, breaking flag, cadence, version scheme); `0bff0001` conflict resolution by file class with the confirmation counter; `7bd50001` Git hosts card with readiness check, vault token fallback and GitLab; `44820001` daemon push and tag with the release path and the fall-back; `d2f10001` shadow merge and comparison with the replay tests; `b3ed0001` release readiness and the audit of mode changes. All are bound by the decisions above and by the five answers quoted in this plan. They are grouped into one Automation (arc) when they are decomposed.

## 2026-11-08: answers on the ZAP bump, the reaper and the direction of handover

- ZAP bump (`q-18e5ffa41147665bb502`, the protected-path change to `.github/workflows/owasp-zap.yaml` by a dependabot-replacement task): the operator answered, quoted: "yes approve zap". Recorded as the answer `approve` on the question.
- Reaper scope: the supervisor had asked whether to scope the reaper's evidence folder to Automaton worktrees. The operator answered, quoted: "reaper go with your recommendation". The recommendation was to leave the reaper as it is, because the how-to already limits it to the session's own worktree and scratch folders, and narrowing it to worktrees only would leave processes started from scratch folders running.
- Direction, quoted: "we want to get where datawatch is doing much of what you do and you should be slowly handing stuff to it and just observing until we are confident in it's automatoin." The supervisor hands one duty at a time to the daemon, runs each in shadow beside the manual check, and observes until the operator is confident.
- Measured on 2026-11-08 for the context work that follows: a task session starts with a median of about 85,000 tokens of context and runs a median of 13 turns for about 8,000 output tokens; a bare `claude -p` start in a task-like folder carries about 64,000 tokens by default and about 28,000 with MCP servers switched off (`--strict-mcp-config`), so the MCP tool definitions are about 36,000 tokens (56%) of every session start.

## 2026-11-08: session context and cost optimization (operator answers)

Asked: which optimizations to file for cheaper sessions. The operator answered, quoted: "yes proceed - do we need to have a seperate DATAWATCH-WORKER-CONTEXT.md or other contexts or will the creation of a project/automatoin generate the needed stuff so it's dynamic for other datawtach users? remember it's not just for our dev work but for other AI infrastructure users of datawtach. and some workers may need tools/mcp depending on what they are doing so make configurable options and ways of optimizing that as well".

Decision proposed in the filed specs (the operator can correct it): no hand-written worker context file. The daemon generates each session's starting context at launch from the task spec, the project's rules and context files (the existing `autonomous.rules_context` digest) and a configurable worker session profile (mcp, tools, skills, context depth, model, effort), overridable per task, Automaton, project and globally. Three draft Automata: worker session profiles (`24230001`), script gates and daemon-generated notes (`91600001`, held until the template fix `b9ba0001` has merged), and cheap retries and escalation (`fb940001`).

## 2026-11-08: burn guard back on and the placement task retry (operator answers)

At a 6% reading (pace line 5.4%) the supervisor asked whether to turn the burn guard back on and whether to retry the held placement task. The operator answered, quoted: "burn guard should be on, we did good last week lets not run over again, normal ladder. task retry yes if it failed 3x on pi it should bump to claude". Done: the burn guard is enabled with the ladder lower_inflight, route_pi_prompt, hold_claude (it had been off since 11:08, by the operator's request, so Claude could run). The held task `72070008` of the Automaton `ecf30001` was retried on Claude Sonnet at high effort with the verifier findings as the note. The operator's rule, to be built into retries: a task that fails three times on the local lane moves up to Claude (this is the escalation the cheap retries Automaton `fb940001` implements).

## 2026-11-08 night: loosening the burn guard, more decomposes, and shipping the template fix on evidence

At an 8% reading (pace line 9.5%, 1.5 points of headroom; the usage advice projected 57% to 84% of the week while the burn guard's own projection said 173.7% and held all new Claude work) the supervisor recommended: (1) set the guard ladder to the first two rungs (lower in-flight, route to pi) until a fix makes the guard follow the readings (draft Automaton `87a60001`), keeping it as a safety net and restoring the hold rung if a reading goes over the pace line; (2) start four more decomposes and approve the two validated drafts; (3) merge the template fix `b9ba0001` on the five-outcome evidence after its task commits were recovered by hand, with the full suites run first. The operator answered, quoted: "Go with your recommendations". Done: the ladder is now lower_inflight and route_pi_prompt, the Claude back-off was cleared, `13f00001` and `a9170001` were approved and started, `7bd50001`, `d2f10001`, `5ab00001` and `91600001` started planning, and a merge helper was started for `b9ba0001`.

## 2026-11-09: standard tasks and Automaton templates as installable packs

Operator asked whether the standard tasks and Automaton templates could be modular, as an extension of community skills. Supervisor recommended: make a pack a named, versioned unit; reuse the existing skills registry rather than a second system; gate third-party packs behind operator trust and the worker profile tool allow-list; build the pack format first and sharing later; and offered to write it up as a draft Automaton spec for review. Operator answered: "Yes do that, good plan". Draft spec: the standard task packs Automaton (left in draft, not decomposed, and held until the standard task rename Automaton merges). The pack format, the trust model, the app placement and whether community packs are in the first release are open questions for the operator.

Pack question 1 of 4 (format on disk). Options: A one folder with a manifest file (recommended), B a registry entry that points at a folder elsewhere. Operator answered: "A, fits well". Recorded in the draft spec for the standard task packs Automaton.

Pack question 2 of 4 (trust model). Options: A accept once per pack version (recommended), B accept once per source. Operator answered: "A with the opt in". Recorded as: per-version acceptance showing what changed, plus an opt-in per source (off by default) that auto-accepts a new version only when it adds no new tools, scripts or file scopes. Recorded in the draft spec.

Pack question 3 of 4 (app placement). Options: A a Packs sub-tab under Templates (recommended), B inside the skills registry page. Operator answered: "I like a, but it is added to registry so may need a check in Settings to enable packs, or somehow the gate and set up will still be tied to registry right?" Supervisor confirmed: sources stay in the registry settings, the Packs sub-tab reads the registry, one off-by-default Settings gate on all surfaces. Detail question (sub-tab while packs are off): A visible with an explanation and an Enable button (recommended), B hidden. Operator answered: "A". Recorded in the draft spec.

Pack question 4 of 4 (community packs in the first release). Options: A built-in pack plus packs from a source the operator connects, B built-in pack only, C a curated community list. Operator answered: "Are we creating a bunch of packs with the stuff or automate now? If so, those should see the first community stuff and we should use the community packs from the beginning. That's the intent. We shouldn't try to phase our way into it. We should use it as it is and make sure it works. This is for 10.0.0, or is key to daemon automation and projects". Recorded: community packs from the start, no phasing, datawatch's own standard tasks and Automaton templates are the first packs and use the same path. The operator states this is for 10.0.0 and key to daemon automation and projects. Open: the default source and the offline case.

Pack question 5 (distribution of the first packs). Options: A a default registry source plus a pinned copy for offline (recommended), B the default source only. Operator answered: "Datawatch has a community register, i like the idea of pinned but then it's 2 cos to maintain, if they can download datawatch they can download community, if we need offline support we need a script to copy into an archive but I'm not worried about offline users right now, they can come repo if they need, community repo, so b". Recorded: B. The first packs are published in the dmz006/datawatch-community registry, already pre-seeded on every install, with no pinned copy; offline support is later and out of scope now.

Pack question 6 (first acceptance of the pre-seeded community registry). Options: A accept the source once when packs are turned on (recommended), B trust it automatically. Operator answered: "A". Recorded in the draft spec. All design questions for the pack Automaton are decided; decompose after the standard task rename Automaton merges.

2026-11-09 protected path approval for the Trivy install Automaton. Supervisor read the diff for the task that replaces the two Install Trivy steps in the containers workflow (curl piped to a shell becomes the pinned, retrying install script) and asked whether to approve it and the matching edits in the release and image-refresh workflows, reading each diff first. Operator answered: "Approved". Answered on the held question as the operator; each later workflow diff is still read before its hold is answered.

2026-11-09 planner backend question. Options: A keep planning on Claude for now (recommended), B route planning to pi while the burn guard is at route_pi_prompt. Operator answered: "Keep using Cloud to plan after 10.0 when we do our matrix test and we look at other models. We might be able to change things then. For now, keep it as Cloud." (the operator wrote Cloud for Claude). Recorded: planning stays on Claude; revisit after 10.0.0 with the model matrix test. No config change.

2026-11-09 worker profile launch wiring. Supervisor set out the risks (conflicts with the running executor and restart-safety Automata, and the functional risk that a too-narrow profile causes failed attempts) and the guardrails (no behaviour change by default, narrowest-risk rollout first, measure before widening, fall back to the full harness when a profile cannot be applied) and asked whether to start now. Operator: "What's this chance of it causing problems if we start it now? ... I'm leaning toward yes now", then "Proceed then it looks good". Created as a draft Automaton from the spec worker-profile-launch-wiring and sent to decompose (planning stays on Claude per the operator's earlier answer).

2026-11-09 plans folder tidy. Supervisor measured the folder (196 top-level plans, 15 MB, registry 637 lines and 452 rows, 47 plans overdue, the report and parity-issue folders untouched by the tidy) and asked whether to run the tidy now, proposing a daily daemon Automation, a monthly registry archive and tidying of the subfolders. Operator answered: "Yes, clean everything up. And this is also the reason I want to have all the automation and Autonomo and PRDs decomposed. And we need to have the plan of project stuff together. When that comes together, then we should be able to view everything. The registry is just we're using to track everything right now. Once we have full project planning and everything else, we'll have to re-examine what we do, where and how we track things. But for now, clean up the folder. Let's keep it tidy. And let's see if we can make sure everything gets done and we're ready to go." Done: the tidy ran (47 plans archived, 38 links rewritten, 0 broken). The automatic daily tidy and the subfolder and registry archive work is the Automaton 29550001. A web app registry view is not being built now; it waits for full project planning.

2026-11-09 order and priorities. Operator: "I want to make sure we get the daemon automation first because that is going to be cost savings ... Do a quick pass to make sure the order looks good to you based upon the features we have coming and what we will get from the cost savings going forward." Supervisor proposed the order (daemon automation first, then the daemon merge phases and merge-back reliability, then cost savers, then the project layer decomposed in parallel, then recoveries; holds and research stay parked). Operator: "The prioritization changes you recommended. Let's go ahead and make those." Applied: 91600001 to 90, 99be0001 to 88, d2f10001 to 85, 3b7c0001 to 84, 7bd50001 to 80, 5efe0001 to 78, the three daemon merge drafts (b3ed0001, 44820001, 0bff0001) to 75, 24400001 to 72, 87a60001 to 70, the Projects tab d3680001 to 55, the dashboard data fixes edc10001 down from 80 to 40 so the UI drafts sit below the daemon work; af1e0001 stays at 100. Decomposing the Projects tab and triage cards drafts was part of the same recommendation; the answer named only the priority changes, so the decomposes wait for a separate yes.

2026-11-09 decompose cadence. Operator: "You are going to be doing one or two or three decomposes an hour So we could stay in our budget just get everything queued up. There's no reason to do extra decomposes Let's just make sure it gets done". Standing rule from now: one to three decomposes per hour, in priority order (the daemon merge drafts first, then the burn guard calibration, then the project layer and UI chain), each validated, cleaned of standard tasks, coverage-read and approved; no extra decomposes beyond that. Started 08:31: b3ed0001 and 44820001 are planning; 29550001 (plans folder auto-tidy) was approved and started.

2026-11-09 evening decompose hold lifted. Supervisor reported 22% at 17:14 (1.6 points ahead of pace, projection 114%) and proposed holding new decomposes until the burn was back under 0.6 an hour. Operator reading at 18:00: "23%, we need decompose, are we doing claude workers and not decomposing, what can we do to get and keep work going by stay on track". Decision: the hold is lifted and decomposes resume at two an hour in priority order. Supervisor's spend analysis since 08:32 (8.4 estimated points): task worktree sessions 7.2 (workers and verifiers about 70 percent of the total, planners about 0.09 each), this supervisor session 1.3, and by model haiku 5.6 and sonnet 2.8, where the sonnet share is mostly the supervisor's merge helpers (eight of them today, about 0.3 each). The controllable items are the merge helpers (the daemon merge in Phase 4 removes them) and the supervisor session's own turns.


### Supervisor as skills (operator, 2026-11-09)

Operator: "We need to modulator even the supervisor, skills and eventually other models seem essential" (read as: make the supervisor modular; skills, and eventually other models, are essential). Scope and shape are open; first question asked of the operator is whether to draft a design Automaton or fold it into the packs work.

Operator, on how to take it forward: "A, but it should be designed to fit into the packs". Design proposal: docs/plans/2026-11-10-supervisor-as-skills-design.md (open questions listed there, asked one at a time).

### Reporting data store (operator, 2026-11-10)

Current stores: Automata are one JSON file each (fsync and rename writes, side files for large text); trace is per-session JSONL plus an index; memory, web search, council and docs-index vectors use SQLite (memory also has a Postgres store); small state is single JSON files.

Operator on how reports and dashboards read this data: "Let's go with A. I like that for initial step and if we find that we're using the database more we can look at a migration for a future version, but I like A for right now." Decision: files stay the source of truth; a derived SQLite reporting index, rebuildable from the files, serves reports and dashboards. A migration of the source of truth to a database is a possible future version, revisited if database use grows. Not yet drafted as an Automaton; it is part of the reports work and the pack run record (the trace event is extended, the index reads it).

### Plans and backlog primary in datawatch (operator, 2026-11-10)

Operator (pasted text, as heard): backlog, plans and status should run inside datawatch with an optional export to a repo, a migration plan is needed, and skills for synchronizing the files drop to a low-priority background helper until the migration is done. Design proposal and open questions: docs/plans/2026-11-10-plans-and-backlog-in-datawatch.md. Deprioritised: the plans folder auto-tidy Automaton, the daily tidy Automation, the scheduler Automaton for it, and file-sync skills.

### Standard-tasks pack authored in the community registry (operator, 2026-11-10)

The supervisor asked where the standard-tasks pack content is authored. Operator: "We said when designing that it was too go on community registry" (read as: it was decided at design time that it goes in the community registry; option A). This restates the 2026-11-09 decision: no pinned copy inside the binary or this repository. The packs Automaton's pack-folder task was rewritten to author only a test fixture under internal/packs/testdata/, and its embed task is to be rewritten to build the defaults pack in memory from the existing Go definitions (the pack active while packs are off). The real pack is authored in the community registry repository as a separate step.
Operator reading, 2026-11-10 14:17 EST: "Claude at 33%".

Operator, 2026-11-10, on the Vault unreachable alert default (120 seconds): "A" (keep 120 seconds; confirmed).
Operator, 2026-11-10, on the peer silence alert default (5 minutes): not yet asked.

### Embedded built-in pack to be removed before the packs release (operator, 2026-11-10)

Finding: before the pack tasks were rewritten, work in the packs Automaton put a built-in copy of the standard-tasks pack inside the binary (an embedded folder under internal/packs/builtinpacks with a LoadBuiltin function). That is the pinned copy ruled out on 2026-11-09. Operator choice: "A" (seed the community registry from that folder first so the content is not lost, then replace the embedded copy with an in-memory defaults pack built from the existing Go task definitions; one copy, in the community registry).
Plan: the content was copied to the supervisor scratch folder (pack-seed) for seeding. When the packs Automaton is merge-ready, the merge helper replaces LoadBuiltin and the embedded folder with DefaultsAsPack and updates the tests that used it, before shipping. Seeding the community registry repository is an outward-facing push to the operator's repository, so it waits for the operator's explicit go-ahead.

Operator, 2026-11-10, on the peer silence alert default (5 minutes): "A, but configurable" (keep 5 minutes; it is already a setting, observer.peers.silent_alert_minutes, on every surface; 0 disables).

Operator answer on the Projects tab empty lines (2026-11-10): "It should default to none showing but have a flag they can check that would show them. That way we can make sure nothing is being missed and can go browse things as we need." (option A with a show-empty control, off by default.) Drafted as the Automaton "Hide the empty lines on the Projects tab by default and add a flag that shows them" (draft, not yet decomposed). The operator also asked whether design questions are available in the datawatch questions interface so they can be answered in the UI.

Operator answers in the questions interface (2026-11-10):
- Worker profile tiers: "Yes, in two steps" with: "I want to be staging this stuff out so we can test it and iterate and make it better so that when Tendido comes out we've actually tested these some." ("Tendido" as heard, read as 10.0.) Staged rollout before 10.0. Note: the built-in defaults already put the planner on full, workers, verifiers and docs on digest, and judge and note tasks on minimal; the howto's rollout steps start with the docs kind on minimal. The profile launch wiring (released after the running daemon was built) is not active until the daemon is redeployed, so step 1 cannot be set until the deploy.
- Branch refresh: "Draft an Automaton for it" (no text). Drafted.
- Watch interval, product record: recorded in the supervisor-as-skills design and the plans proposal.

Operator, 2026-11-10, in the questions interface on the git hosts observability waiver: chose "Add observability to this change now" (no text). The git hosts Automaton gets observability tasks before it merges.

Answers to the old-item questions, in the questions interface (operator, 2026-11-10):
- MCP resource subscriptions for lifecycle events: "It overlaps with what we're trying to do. Let's roll it into that if possible and make sure that we don't break or interfere with anything happening with it." Rolled into the event-driven supervisor Automaton as requirement 9 (additive, off by default, no change to existing subscriptions). The old plan was archived.
- Register the skills catalog research: "Make sure we're not losing anything that isn't already in existing PRD or automation. This was anything dealing with the skills catalog mean to make sure we're not losing, but it can definitely be archived." The catalog research exists only as untracked files in a sibling working folder (datawatch-skills, docs/catalog; the repository there has no commits), so a copy was saved to the supervisor scratch folder (skills-catalog-backup), and a new draft Automaton "Build the first skills from the datawatch community skills catalog" carries the pending work (the version 10.0.0 checklist item). The register-only plan was then archived.
- Kept as drafts for later, each with "This is post 10.0.0 planning": per-turn chunked output capture, the Hermes Agent research, the open-model research recommendations, the openrig.dev research registration, the individual-builder harness survey registration, personal agents with personality, the open-model decompose research, and the harness planning documents.
Note: the openrig.dev and harness survey register-only items and the harness planning documents were recommended for archive and were kept by the operator.

Operator, 2026-11-10, on the skills catalog: "I totally forgot that the one I said to archive about the skills, let's make sure we don't lose that. We want to do a full skills mapping of all of the DataWatch features and services and stuff it can do and make sure there's a community skill available to be able to use DataWatch properly. We don't want to lose all of that intent." Decision: the draft Automaton for the skills catalog (now titled "Skills mapping of every datawatch feature and the community skills that cover them") is widened from building a few skills to a full map: an inventory of every feature and service derived from the tool listing, commands, routes, settings and how-tos; a coverage matrix of which community skill covers each; a coverage check in the final gate so a new feature cannot ship without a place in the map; the gaps ranked; and tranches of skills built until every area has one. The first Automaton builds at most five.

Operator, 2026-11-10, on the CI follow-ups from the Trivy install change: "A, and why is it pinned? Is there a technical reason" (option A: one small Automaton for the Trivy pin unification, the helm pin and the orphaned upstream-watch check). Drafted as "Unify the Trivy version pin in one place, install helm in the kind smoke workflow with the same pinned and retrying method, and retire the upstream-watch check that no longer matches anything" (draft, not yet decomposed).

Operator, 2026-11-10, on the Gantt charts plan (waiting since November 4): "A" (replan now, one planner run against the cost, effort and filter requirements and the product and project model). The planner run was started; the plan is read before it is approved.

Operator, 2026-11-11, quoted: "Claude at 43%, but we just got a few reset so we can reset to 0 once this week, let's go 2 claude lanes and examine maybe 3, let's get all decomposing done and let's iterate on questions for me" and, clarifying: "Got a free not few reset" (heard as: a free reset of the weekly allowance). Done: reading 43% recorded; claude-code max in flight set to 2 (three to be examined); planner runs started for the five ready drafts (CI follow-ups, event-driven supervisor, branch refresh, hide-empty Projects lines, skills mapping).

Operator, 2026-11-11, quoted answers in the questions interface: third lane "Move the limit to 3" (claude-code max in flight now 3); free reset "I will do the rest and let you know when it is done,  but we can burn to 100% today and tomorrow then I'll reset tomorrow or Friday"; MCP classification "Need them broken down by type and know what classification options are available"; held plans "Raise one question per held plan" (nine raised); Gantt cost and effort "Wait until the first Gantt Automaton has shipped". Supervisor: reproduced the blocking agent-base image scan locally, 56 HIGH findings (14 krb5 CVEs on four client libraries, no fix, server-side code absent from the image), suppressed with a traced entry in .trivyignore.
