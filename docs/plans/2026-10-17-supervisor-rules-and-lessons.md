# Supervisor rules and lessons learned

**Date:** 2026-10-17
**Version:** v9.4.0
**Status:** Active. Rules in force today for the human-driven supervisor loop, and the
specification the Automation coordinator (BL431) is built to replace that loop with.
**Scope:** how a supervisor (a person or an AI session) runs PRD building, validates
decompose output and results, decides whether to fix datawatch itself, and hands off.
The values below are defaults. Any datawatch user should be able to change them in
config; nothing here is specific to this repository.

Operating rules for code changes live in [`AGENT.md`](../../AGENT.md). This document
holds the supervisor's rules and the lessons that produced them. The Automation
coordinator ([automation orchestration design](2026-10-14-automation-orchestration-design.md),
open questions 17 to 20) is where these rules become mechanical.

---

## 1. Who decides what

- **The operator makes every design decision.** That means what the product does, any
  behaviour change a user will see, defaults that affect users, the intent behind the
  roadmap's order, escalation channels and thresholds, and anything that is a choice
  between valid designs. The supervisor asks, with a recommendation, and waits.
- **The supervisor decides and does the build and bug-fix work** up to the point where the
  Automation coordinator exists, then hands off to it. That includes: fixing datawatch bugs
  through qwen fix PRDs; approving a decompose that passes the section 4 checklist; running,
  resetting and cancelling PRDs; recovering stuck tasks; repository hygiene (removing
  tracked junk, refreshing roadmap statuses, release housekeeping); filing the parity
  issues that the rules require; and committing work it has validated. It does not ask
  permission for these.
- **Escalate only for a design decision, or when a loop will not break.** A loop means the
  same failure twice, then two sub-agent investigation passes that did not resolve it.
  Escalate with what failed, what was tried and what is still broken. Do not ask about
  work that is inside the supervisor's authority.
- **Stop-and-report rule.** When something unexpected turns up outside the task at hand,
  report it and carry on with the task. Fix it only if it is inside the supervisor's
  authority and does not change the plan.
- **Never touch the operator's own sessions or jobs.** Other sessions and unrelated jobs
  (for example a mail-rules job) are not PRD work. If datawatch itself misbehaves around
  them, that is a product bug to fix in datawatch, not a reason to change their setup.
- **Commits.** The supervisor commits validated work it owns, staging an explicit list of
  files. An automatic snapshot or task commit is not approval of its contents.
- **Idle lanes are the supervisor's defect, not an operator question (2026-10-21).** The operator should never have to ask why a lane is idle. Check 4 of the tick (section 11) is mandatory and fills every idle lane in the same tick. Ask the operator only for design decisions.
- **Merge, push, tag and release are the supervisor's calls (2026-10-21).** The supervisor is
  release manager. A completed, validated PRD is merged, pushed (fast-forward, Section A
  checks pass) and tagged (patch or minor) without asking; only the 10.0 tag is the
  operator's. Never end a tick on "do you want me to merge/push/tag?". If a residual risk
  remains (for example other flaky tests), ship the fix that is ready, file a follow-up fix
  PRD for the rest, and report both. A question to the operator is allowed only for a
  design choice or after the loop rule; a build or release question is neither.
- **Resets.** Reset a blocked task once after its cause is fixed, then run. If it fails
  again with the same error, that is the loop rule above.

## 2. The supervisor's role

The supervisor orchestrates and validates. It does not hand-fix code.

- It operates datawatch through datawatch's own CLI, MCP and REST tools, not raw tmux
  or curl.
- When it finds a real datawatch bug it files a small fix PRD, has it decomposed
  atomically, runs it on the open-model backend, and validates the decompose and the
  result. A one-shot pi session is allowed for a fix, and it is subject to the
  validation gate in section 8.
- Deep investigation goes to a sub-agent so the supervisor's own loop stays small. The
  sub-agent gets at most two investigation or fix passes. If the problem is still
  unresolved, the supervisor escalates to the operator with what failed, what was
  tried, and what is still broken.
- The loop never lapses. It re-arms every cycle and ends only when the Automation
  coordinator takes over.

## 3. Backends, models and compute

- **Lane model (operator, 2026-10-20; supersedes the pi-only rule of 2026-10-17).** The weekly
  Claude ceiling was exhausted on 2026-10-17 and reset on Sunday 2026-10-18 08:00. Claude is
  tracked, not capped, and the reset day can differ by provider, so the reset date is read from
  `docs/plans/2026-10-14-concurrency-and-budget-design.md`, not assumed.
  - **GPU lanes (two):** pi-prompt with `qwen3.8-64k:27b`, one engine per node.
  - **Claude lanes (three, interchangeable):** each runs one PRD and one decompose thread at a
    time. A worker is Claude Haiku (`claude-haiku-5-5`); the verifier is Claude Sonnet for a PRD
    on a Claude lane. Any Claude lane takes a decompose, a worker or a verify as the queue needs.
  - **Budget pace (operator):** Claude work beyond the lanes is allowed only while
    `weekly % <= days elapsed since reset / 7 x 100`. Read the weekly % each tick and record it
    in the budget doc. Above pace, Claude takes no extra work; the GPU lanes carry the queue.
  - **P0 and overflow (operator):** a P0 takes a free Claude lane. If every lane is full and the
    pace rule allows, the supervisor opens a temporary Claude lane for it. Otherwise the P0 waits
    for the next free GPU or Claude lane.
  - **One-off decompose:** a decompose may run as a one-off Claude session when there is pace
    budget. Otherwise it waits for any lane to free.
  - **Compare, don't assume:** record Claude vs qwen time and accuracy per PRD (the
    `time`/`accuracy` columns in the budget doc). If qwen matches Claude, the operator may move
    work back to the GPU lanes.
  - The supervisor enforces the lane count; the daemon does not. Do not exceed two GPU engines
    or three Claude lanes.
  - **Accepted ideas (operator, 2026-10-20):** (1) when both GPU lanes are full and pace
    allows, the next approved PRD runs on a Claude lane as a Haiku worker; (2) run the time and
    accuracy log for about a week before moving work between qwen and Claude. **Not adopted:** keeping a Claude lane idle
    for P0. P0 is infrequent, so the three Claude lanes stay interchangeable and P0 takes a free
    one, or a temporary lane under the P0 rule.
- Two GPU nodes are two lanes. Keep both busy, and run at most one engine per node. If
  two land on one node, cancel the newer one.
- **No experiments on a busy GPU.** Never run a test, benchmark or trial of a model setting
  on a node while a PRD engine uses it, and never take a lane for it. An idea about open
  models (thinking levels, prompts, settings) is written into the plan docs as a tracker
  item and tested later, in the open-model matrix after v10.0.0 or when the operator
  frees a node. The supervisor only observes (logs, process lists, small reads).
- **Host settings.** Model hosts run with `OLLAMA_CONTEXT_LENGTH=65536` and
  `OLLAMA_KEEP_ALIVE=-1`, one large model per node; see
  [model host requirements](2026-10-18-model-host-requirements.md).
- **Pin the model.** The 64k-context variant (`qwen3.8-64k:27b`) is pinned resident
  with `keep_alive: -1`. The 32k default caused eviction and context overflow
  (`stopReason=length`). Check residency every tick and re-pin when it lapses.
- **A second model evicts the pin.** A one-shot pi session launched with no model fell
  back to pi's own default (`qwen3.8:27b`), loaded it, and pushed the 64k model out.
  Pi-family sessions now take the configured model (`c0c26f59`). Treat any unpinned
  model loading on a pinned node as a fault.
- **Config drift is silent.** Global `planning_backend`, `planning_model`,
  `verification_backend` and `verification_model` can drift to claude-code or opencode,
  and a PRD's decomposition profile can pin a stale backend that auto-retry keeps
  using. Check both every tick. A non-pi autonomous session means cancel its PRD,
  because killing the session does not stop the retry loop.
- A model change for the supervisor session is an operator decision. Haiku was not
  adequate as supervisor, and the session moved to Sonnet on 2026-10-17.

## 4. Validating a decompose

Do not rubber-stamp a plan. Before approving, check every item:

- Tests are part of the plan (2026-10-20 rule). For each feature, bug fix or refactor the plan
  has unit-test tasks, a smoke or e2e task for every web, API or CLI surface it changes, and a
  verification script that runs them. A plan without them is revised, not approved.
- Every story and task has a title and a spec.- Every story and task has a title and a spec. The planning parser once accepted blank
  titles and specs (`dbf62787`); a plan with blanks is rejected.
- Each task is one tool-call chain with one checkable outcome. A task that reads like
  two or more deliverables is split into atomic tasks with `depends_on` chains. Two
  bundled tasks failed on the BL465 pilot and both succeeded once split.
- Every task that changes files has a real verification script or a clear verification
  path. A plan with no verification script is rejected.
- `depends_on` is set wherever a task needs another task's output.
- No tracker ID (BL, B, GH) appears in any file, function, test or type name the plan
  specifies. Tracker IDs belong in `docs/plans/` and commit messages only.
- The PRD is pinned to the open-model backend and model before it is approved or run.
- Release tasks never contain a fixed version number. The release version is taken
  at merge time as the next patch above main: task titles, specs and descriptions
  say 'next patch above main' instead of a number, and no number is repeated even
  if a revision note or earlier plan used one.
- Before decomposing, append the atomic-sizing guidance block to the spec while the PRD
  is still a draft.
- The atomic-sizing guidance block appended before decomposing must also carry this
  release-version rule, so the draft spec and the planner prompts state it the same
  way.

- Feature plans carry the parity and config checks from `DATAWATCH-CONTEXT.md` (2026-10-20): a task that adds or changes an endpoint records the `docs/testing-tracker.md` section (B1); a new user-facing PWA string adds the five locale keys and the `TestLocales_CommonNavKeysPresent` entry (B2, B3); a new config field covers all six registration points (B6: `config.go`, `api.go`, `app.js`, `config-reference.yaml`, `implementation.md`, MCP); a new operator-facing endpoint adds a smoke section (B12); a change to `SignalBackend` or `LLMBackend` is flagged for operator sign-off (interface stability).
- Design-document tasks carry the two checks decided under DIP on 2026-10-20 (AGENT.md, "Design-document verification"): every heading the spec names exists and is non-empty, and each section meets the minimum length and cites a file or PRD ID.
- An open design decision with no rule in AGENT.md is not decided by the supervisor. It goes to the operator through the DIP (AGENT.md, Decision Making), and the plan waits.

If the plan fails, either request a revision with specific feedback or fix a small
mechanical gap directly through the edit, add and remove task commands. If a decompose
ends with no plan, read the newest pi log for `stopReason=length` and report it.

## 5. Triage when a task fails or blocks

Pull the task's own failure reason and, while it exists, the session output. Never
trust the stored status string alone. Classify with the existing failure taxonomy, then:

- **Infrastructure** (the mechanism did not finish): retry within the existing budget.
- **Content, matching a signature an open fix PRD already tracks:** do not retry
  blindly. Leave it blocked, record it as evidence on the tracking PRD, and resume it
  when that fix ships.
- **Content, a new failure shape:** surface it immediately and hold for the operator or
  a higher-tier model. A small model cannot diagnose its own failed attempt.
- **Scope** (the spec itself is wrong, or asks for too much in one task): never retry
  the identical spec. Request a revision, or split the task.
- **Same failure twice:** stop that task and investigate (section 2), then fix and
  reinstall by atomic rename (copy to a temporary name, then `mv -f` over the binary),
  then restart the daemon through systemd.

Recovery moves that have been needed in practice:

- A task showing a stale "dependency failed" after the dependency succeeded: reset the
  dependent task too.
- A task stuck in verifying with a dead session: cancel the task, force-reset it, run.
- A task frozen with an unchanged session while its log shows it finished (a zombie
  executor goroutine): cancel at the PRD level, force-reset the task, run. Cancelling
  only the task does not clear the goroutine.
- Use the CLI to read PRDs. The MCP `prd_get` and `reset_task` replies are about
  450,000 characters, unreadable in a session. Use MCP only to reset and run, and
  confirm the result with the CLI afterwards.

## 6. Verify the verifier

- "Verification failed after retries" with a worker whose code builds and passes tests
  means the verifier may be wrong. Run the task's verification script by hand before
  blaming the worker. PRD `29bd62a1` task T2 failed twice because its script used
  `grep -v '--- PASS'`, which grep reads as an option. The one-line fix was
  `grep -v -e '--- PASS'`.
- A retry whose diff is empty because the work already landed must be judged on the
  earlier attempt's committed work, not reported as "no output". The fallback exists
  (`4c02bc31`, `a27cdfe2`), and a review found it has no retry check yet, so a first
  attempt that did nothing could pass if its script already passes. That is an open
  defect.
- The stale-worker watchdog does not apply to pi-prompt. One-shot `pi --print` writes
  nothing while it works, so an idle pane is not a stall.
- Prompt delivery to a TUI is wrong for one-shot backends. The send-keys gate once
  typed pi-prompt tasks into bash and killed decompose sessions in seconds.

## 7. Git and working-tree hygiene

- **The shared tree is the biggest source of breakage.** Verification was polluted
  three times by unrelated changes landing on the same checkout while a PRD ran. A PRD
  task is also blocked by `foreign_file_drift` when any file outside its scope is
  modified or newly committed during the run.
- Do not edit repo files, including plan docs, in a tree a PRD is running on. Commit,
  or work in a separate worktree, first.
- **The pre-session snapshot uses `git add -A`.** It swept an unrelated session's files
  and test debris into history (`286499ce`, including a file named `etc/shadow` that is
  still tracked). A snapshot or task commit is not a review. After any snapshot,
  audit what it staged.
- PRD commits stage only the task's declared `write_dirs`. A worktree-mode PRD scopes
  to its own worktree. A direct-mode PRD with no declared `write_dirs` does not
  auto-commit (BL470).
- **Git is opt-in.** Not everything datawatch runs is development work. A script or scheduled
  job (a mail-rules job, a report) must never get a `git init`, a snapshot or a commit. Only
  PRD and development sessions, or a session that asks for it, use git (BL476). Since v9.2.0
  every one-shot got git involvement and the opt-out was retired, which reached a script
  running in the operator's home directory.
- **Worktree mode is the default for every PRD.** Direct tree mode stays as an explicit
  opt-in, and only one running PRD may hold a direct tree. A recursive child PRD is
  exempt from its parent's hold (BL469).
- Worktree pruning, archive versus purge, is deferred until after the 10.0.0
  architecture, project and release work. It belongs to automation planning (BL471).

## 8. One-shot sessions

- **Validation gate.** An ad-hoc one-shot (not owned by a PRD) that changes code
  declares its validation commands. It commits only after they pass, and an unvalidated
  or failing one never commits. See the example rule in the
  [automation orchestration design](2026-10-14-automation-orchestration-design.md).
- A one-shot whose command has ended but whose session never reached complete is a
  zombie. The reaper closes it through the normal completion path, removes its scratch
  file, and closes its pane. Session folders are kept for a retention window, 30 days
  by default and configurable.

## 9. After-done checklist before advancing past a completed PRD

Run this before marking a PRD done and moving on. Anything missing is a content gap to
route back as corrective work, not something to skip:

- The tests the change needs (unit, smoke, and e2e where a surface changed) are in the same commit, cover the new behaviour, and pass on the merged main. A bug fix has a regression test that fails before the fix.
- Version bumped in `cmd/datawatch/main.go` and `internal/server/api.go` together.
- A CHANGELOG entry in plain operator language with no tracker IDs.
- Real tests for new behaviour, not only a passing existing suite.
- The full parity surface for anything touching comms, config, MCP, API or PWA: REST,
  MCP, CLI, comm channel and PWA, plus a `datawatch-app` issue and locale strings.
- New config keys in `docs/config-reference.yaml`; docs, testing tracker and
  cross-references updated.
- The PRD's roadmap entry closed, and the README status matching the PRD registry.
- Pushed, not only committed. Done means the work is on the remote.
- Every minor version is tagged for release. A minor version is a complete feature.
  The tag push runs the CI release pipeline, so releases are never built or uploaded by
  hand.
- CI is checked after every tag, on every tick, until the run is green. Anything CI finds
  goes to a fix PRD (never a hand fix) and the check repeats until it is clear.

Version and release kinds (from AGENT.md, with the operator's tag rule):

- **Patch** (`X.Y.Z`): bug fixes, docs, refactors and config changes. Tagged for release,
  and CI builds it. A patch needs a released base, so it is not cut on top of unreleased
  work.
- **Minor** (`X.Y.0`): a completed feature, meaning every checklist item above is done.
  Never a slice of a feature, and never a scaffolding change.
- **Major** (`X.0.0`): breaking changes, only when the operator explicitly asks.

These release rules are the operator's rules for this project. Other projects will have
their own, so the Automation coordinator must treat release rules (what counts as a patch,
a minor or a major, what gets tagged and when, whether a push needs approval) as
per-project configuration, not built-in behaviour. The first complete release after the
unreleased 9.1 to 9.5 work is numbered 9.6.0; the gap is left in place.

Bumping the version in the code for every behaviour change, as happened from 9.0.87 to
9.5.0 without a single tag, produces numbers that are development counters, not releases.
Do not release those; fold them into the first complete release.

The standing permission to push and tag applies only to a completed feature that has
passed this checklist. Any other push still needs the operator's explicit go-ahead.

The interim loop did not run this checklist, which is why it is a coordinator
requirement (open question 19).

## 10. Roadmap truthfulness

- The README order is authoritative, and its statuses go stale. An audit on
  2026-10-17 found nine or more rows that disagreed with the PRD registry. Re-check the
  registry at least hourly and correct the README.
- Do not build out of order silently. If an out-of-order item is worth building, record
  why in the README.
- Done is defined by the checklist in section 9, not by the executor reporting
  complete.

## 11. Supervisor loop mechanics

- A recurring wakeup runs the tick every 20 minutes (cron `7,27,47 * * * *`), and an hourly
  backstop (cron `17 * * * *`) confirms the tick chain is alive. Never a 5-minute tick.
- **A tick must not strand the work it interrupts.** Each tick does its checks, then
  tells the supervisor to resume what it was doing. Every tick prompt names the work in
  progress.
- Report state changes only. A tick with nothing new says so briefly.
- Long-lived facts about datawatch go in `docs/plans/` or datawatch's own memory, not
  in an assistant's private memory.
- **Defect intake.** Every datawatch defect seen during supervision, with or without a
  workaround, gets a tracker row and a draft fix PRD in the same tick. Writing "fix it
  later" in a tick prompt does not count. Every tick ends with a defect sweep: list
  anything odd seen (failed task causes, stuck sessions, wrong models, silent drops,
  oversized replies, side effects of a restart) and confirm each has a row and a PRD.
- **Lane fill (lesson, 2026-10-18).** Every tick lists each lane (the two GPU lanes and the
  Claude lane) and what it is running. An idle lane with eligible queued work is a defect
  of the tick, not a quiet state: start the next item on it in the same tick, or write down
  why not (for example a hold the operator asked for). The qwen lanes sat idle for about an
  hour because the tick only tracked the PRDs it already knew about and the whole fix queue
  had been assigned to the Claude lane; nothing re-assigned work when the two qwen PRDs
  finished. The queue is split by lane up front (small independent fixes to the qwen lanes,
  work needing the Claude builder to the Claude lane), planning for the next item runs while
  the current one builds, and the report names each lane and its occupant.
- **Tmux-loss check (lesson, 2026-10-18).** Every tick runs
  `systemctl --user show datawatch-tmux -p NRestarts,ActiveEnterTimestamp`. A higher
  NRestarts or a newer ActiveEnterTimestamp than the last tick means the tmux server was
  lost and every session died. Then list sessions, audit every non-terminal PRD for killed
  worker, verify and guardrail sessions, restart the supervisor session if it was killed
  (`datawatch session restart <id> --task "<short note>"`; a task over about 8 KB cannot be
  resumed, BL503), recreate the session-only crons, and find the cause before moving on:
  read what ran in the last 60 seconds in `~/.pi/agent/sessions/*/` and
  `~/.claude/projects/*/` transcripts (UTC) and `journalctl --user -u datawatch-tmux`.
- **Dangerous-script review (lesson, 2026-10-18).** A task that writes or runs a script
  touching tmux, `pkill`, `killall`, `systemctl`, daemon start or stop, or a sandbox or
  cleanup helper is read line by line before its verification runs. Require an explicit
  `-S <socket>` or `env -u TMUX`, never `TMUX_TMPDIR` alone (tmux prefers `$TMUX`, which
  inside a worker pane points at the production server), and never `kill-server`,
  `kill-session -a` or a name-pattern kill. A verification script that exercises such a
  helper must run with `env -u TMUX -u TMUX_TMPDIR` and throwaway directories only.
- **Second tmux loss, 2026-10-18 22:55:29.** The dedicated tmux server exited and systemd restarted it at 22:55:31; every session died. Cause not established: no worker command in the minute before touched tmux, the 22:50 and 22:51 kills targeted PIDs that were already gone, and the production server (PID 2185604) was not among them. Lesson: duplicated tmux helpers written by workers are the risk; test cleanup must use the existing scripts only.
- **Mass-kill log.** 2026-10-18 20:18:09: a worker's sandbox helper ran
  `TMUX_TMPDIR=$dir tmux kill-server` inside a production pane, which killed the production
  tmux server and every session. Found in the worker's pi transcript, reproduced with
  throwaway servers. Recovered: sessions relaunched by the daemon, the supervisor restarted,
  the helper fixed in the PRD worktree. Fix PRD: BL504.
- **Lane fill, every tick (lesson, 2026-10-19).** Lanes went idle while eligible work was
  waiting. The tick checked which PRD occupied each lane but never checked what was queued
  behind it. Every tick now lists the four lanes (datawatch GPU, local GPU, Claude 1, Claude 2)
  with the occupant of each. An idle lane starts the next eligible PRD in the same tick, and a
  lane with fewer than two queued items behind it gets the next draft decomposed on that
  lane's backend. Eligible means approved and not marked HOLD in its spec, or a pinned draft
  with a decompose guidance block. A spec that says HOLD, do-not-run or operator-decision
  pending is never run. A PRD whose project directory is the main checkout is recreated with
  no project directory first. A lane may sit idle only with a named blocker in the report.
  Recreating PRDs: a draft created from the command line gets the working directory as its
  project directory, so create drafts through MCP with the project directory left empty.
- **Lane idle diagnosis corrected (2026-10-19).** A first diagnosis said the local GPU lane
  was idle because the LLM registry did not bind the 64k model to its node. That was wrong.
  Both machines have the 64k model, and both hold it resident (verified 2026-10-19:
  `ollama ps` on johnnyjohnny shows qwen3.8-64k:27b at a 65536 context, 100% GPU; the remote
  box at 100.64.0.20 also shows it resident, about 18 GB VRAM). The remote box has done PRD
  work, per the operator. Which Ollama serves a given pi PRD is NOT yet verified: the
  pi sessions and daemon were seen connected to the local Ollama on one check, and no
  connection to the remote one was seen on that check. The capacity pool labels do not show
  where traffic goes. Do not call either GPU idle or busy without a live check (`ss -tnp |
  grep 11434` on each side, `ollama ps` on each machine, the remote box's own logs). Each
  tick does this before judging a lane.
  Lanes, defined: GPU lane A = this machine (johnnyjohnny), qwen3.8-64k:27b;
  GPU lane B = remote datawatch (100.64.0.20), qwen3.8-64k:27b; traffic routing unverified;
  Claude 1 and Claude 2 = claude-code, two lanes only.
- **Registry gap (2026-10-19).** Neither ollama backend (`ollama-datawatch`,
  `ollama-johnnyjohnny`) lists `qwen3.8-64k:27b` in its model list; each lists only
  `qwen3.8:27b`. The PRD and config settings use `ollama/qwen3.8-64k:27b` anyway. Track this
  in the automation BL; do not change the registry without a decision.
- **Plan check against the original spec.** A rewritten or shortened spec can drop test
  requirements, so compare the decomposed plan with the original request and add missing
  tests or surfaces with a task edit.

## 12. What the Automation coordinator must absorb

The coordinator replaces the human loop, so these become mechanical:

- Plan validation (section 4) as an approval gate with a pass or reject verdict.
- Failure triage (section 5), including the same-failure-twice stop and escalation.
- Verifier self-check (section 6): re-run a failing verifier outside the worker.
- Tree admission and scoped commits (section 7), already started as BL469 and BL470.
- The one-shot validation gate and reaper (section 8), shipped as part of this work.
- The after-done checklist and a registry-versus-README consistency check
  (sections 9 and 10) before it advances, including push, tag and watching the CI run
  for the tag until it is green, with CI failures routed to fix PRDs.
- A minimum model context (64k), the pinned model per node and the model-swap rule as
  policy settings, and accounting for every other consumer of the tracked models.
- Lane occupancy: each tick (or event) compares every configured lane with the queue, starts
  eligible work on an idle lane, plans the next item while the current one runs, and alerts
  when a lane has been idle with eligible work for longer than a configured limit, because a
  lane that nobody mentions is easy to miss (qwen lanes idle for an hour on 2026-10-18).
- Alerting when a bad state sits silent. In one incident two tasks sat blocked for about
  fifty minutes before a person noticed.

- Release rules (patch, minor, major, tagging, push approval) read from per-project
  configuration, since they differ between projects.
- Every other option for how automation runs is configurable too, with the settings proven
  in our tests as the shipped defaults (a per-project automation policy, BL483): cadence,
  lane and concurrency limits, which backend and model plans, executes and verifies, retry
  and escalation limits, plan checks, override-lock states, the git default, timeouts,
  defect-intake behaviour and the escalation channel. Other users and their agents build a
  plan that matches their own rules.
- Defect intake (section 11): when it triages a failure as a product defect, it files the
  tracker row and a draft fix PRD itself.
- Completed-but-unmerged detection: a completed PRD whose commits are not on main (compared by
  content, not by commit count) is shown as merge pending in the Automation view, and raises
  an alert when it sits longer than a configurable time. Never silent (da660001, 2026-10-24).
- CI for every workflow, the security-failure exception, failed and blocked PRD triage each poll, a Claude lane whose PRDs all wait on verifiers counted as idle, and detectors that match the tool's own error line: see the Automation design doc section 'Supervisor duties the Automation coordinator must absorb' (2026-10-24), which is the requirement list for the Automation PRD.
- Search before create: creating a PRD warns when its title or spec overlaps an existing PRD of
  any status or an unmerged branch, and names it. Warn, never block.

Design choices left to the operator: the escalation channel and thresholds, which
surfaces map to which regression suites, and how an urgent bug may jump the queue.

- **Local merges are authorized; ask only before a push (lesson, 2026-10-20).** Rule 6 says to trial-merge a completed branch, renumber it to the next patch above main, build and test it, and then merge. Do the local merge without asking. Only a push needs operator approval. The "do not run git add or git commit" line in `/home/dmz/CLAUDE.md` belongs to another session's guardrail (the imap-mcp run-rules session). It does not govern datawatch supervisor work, and the project rules (`CLAUDE.md` in the datawatch folder and `/home/dmz/workspace/CLAUDE.md`) allow commits. Holding merges for a go-ahead stalled every lane for several ticks (the BL516 and BL519 fixes sat unmerged while PRDs stayed blocked). Stage explicit paths only, and never include unrelated working-tree files such as `.oss-scanner/threat_model.md`. Recorded in `docs/plans/README.md` row 95 (BL514) and in memory as supervisor-local-merge-authorized.

- **Supervisor pushes (rule 6b, decided 2026-10-20).** The supervisor decides pushes, not the operator. The operator makes design decisions only. A push is allowed when all of these hold: it is a fast-forward of `origin/main` (never force-push, never rewrite history); the touched packages build and their tests pass on the local `main`; no HOLD PRD is in the pushed range; and the push is recorded in the automation BL (`docs/plans/README.md`, row 95) with the commit range and the remote URL. Anything that is not a fast-forward, or that fails build or tests, is held and reported with the reason, not pushed. Release tags are not pushed by the supervisor; the release CI runs on tags, and the release decision stays with the operator.

- **Correction to rule 6b (2026-10-20): AGENT.md governs every merge and push.** Rule 6b as first written said build and the touched package's tests were enough. That was wrong. Before any push or merge the supervisor must follow `AGENT.md`: feature branches never change the version (the merge script does it); merge with `scripts/check-merge-version.sh`, and bump with `scripts/merge-branches-bump-version.sh` (one bump commit per run, next patch above main) and `scripts/set-release-version.sh`, never by hand; run the Section A checks before each push (full `go test ./...`, `make lint`, `make build`, `node --check`, CHANGELOG entry, README current-release line, backlog refactor, ID and leak checks), and check CI after the push. Two merges on 2026-10-20 (`06ae0001` as 9.5.27 and `be670001` as 9.5.28) were bumped by hand in separate commits, and were pushed before the Section A checks had run. That is recorded as a process deviation in `docs/plans/README.md` row 95 (BL514).

- **[SUPERSEDED 2026-10-21 by the 'Merge, push, tag and release' bullet in section 1: the supervisor pushes and tags.] Earlier text: pushes need operator approval.** The tick instruction is explicit: no pushes without operator approval. Rule 6b, as written earlier the same day, let the supervisor push. That conflicts with the tick and is withdrawn. The supervisor merges completed branches into local `main` (rule 6, per AGENT.md: merge script, one bump per run, Section A checks), and records each merge in the BL. Pushing local `main` to origin is an operator decision, and the supervisor asks for it with the commit range and the Section A results. The push of `c1922994` on 2026-10-20 was made before this rule was settled and is recorded as a deviation in row 95 (BL514).

- **Pushes are the supervisor's decision when tests pass (decided by the operator, 2026-10-20).** This supersedes the 2026-10-20 line above that required operator approval. Push is not a design decision. The supervisor pushes local `main` to origin when all of these hold: fast-forward only, never force; `go test ./...` passes; `make build` and `node --check` pass; `make lint` is clean (AGENT.md Section A12 requires this, so lint is part of "tests pass"); no HOLD PRD is in the range; the push is recorded in the BL with the commit range and the Section A results; and CI is checked after the push. If any condition fails, the supervisor does not push and records the reason. Release tags stay with the operator. Deviation on 2026-10-20: `c1922994` was pushed while `make lint` had 16 findings (10 errcheck, 6 staticcheck); the lint cleanup is BL522.

- **Claude decompose is one thread per Claude lane (operator decision, 2026-10-20).** Earlier wording said "one decompose at a time, never two", and the supervisor read that as one decompose for all of Claude. That was wrong. Each Claude lane runs one decompose thread at a time, and the two lanes may decompose in parallel. An agent within one decompose is not limited. An idle Claude lane with decompose backlog starts the next draft in the same tick.

- **Three Claude lanes (operator decision, 2026-10-20).** The lane model is now two GPU channels (one pool) and three Claude lanes, each running one PRD, plus one decompose thread per lane. This supersedes "two Claude channels" in the earlier lane model and in the tick prompts. The daemon has no Claude lane count (the capacity pools cover nodes and pi-prompt only), so the limit is enforced by the supervisor. The operator tracks usage: 16% weekly at 2026-10-20 04:31 (calculator reading), recorded in `docs/plans/2026-10-14-concurrency-and-budget-design.md`.

- **Record before you push (lesson, 2026-10-20).** On 2026-10-20 the BL row edit failed on an assertion, but the shell command continued and pushed `c1922994..69b0db61` before the BL record existed. Order is fixed: write the BL record, check that it succeeded, and only then push. Run the push as its own step and stop the chain if the record step fails.


- **Release manager and kill validation (operator decisions, 2026-10-20).** The supervisor is release manager: it does patch and minor version bumps and releases through the automation scripts (AGENT.md scripts, one bump commit per run). Only a major release (10.0) is the operator's. The "no version bump without ask" gotcha in `DATAWATCH-CONTEXT.md` predates automation and is superseded by this line; the context file is updated to match. A session is killed only when all of these hold: its pi process has exited (checked by process, not by pane silence); the session log or task output shows no completion record and nothing written after the exit; and the reason is written to the BL. A session that is still running and quiet is waiting, not wedged. The earlier kills on 2026-10-19 and 2026-10-20 did not check the log before killing; that gap is recorded as a deviation.

### 11a. Schedule and procedures (current, 2026-10-26; supersedes the tick prompt below)

Why this exists: the cron prompt had grown to about 1,900 words by accretion (every incident added a
clause) and was re-sent twice an hour, while a tick that found nothing still ran a dozen state commands.
The operator asked for a cheap check often and the full procedure hourly (2026-10-26: "I'm happy with the
hourly change but want something small every 20"). The cron texts are now short pointers; this section is
the single source for what each run does.

**Watch, every 20 minutes** (cron `3,23,43 * * * *`). Run `python3 scripts/supervisor-watch.py`. It prints
`OK <lane summary>` or `ATTENTION` plus one line per finding. On OK, reply with that one line and stop. On
ATTENTION, handle each finding: a restarted tmux server (section 11, tmux-loss check), a down daemon or a
missing model, a newly failed or review-needed task (section 5), a finished PRD to merge (section 7), an
idle lane (fill it, GPU rule below), a broken or silent worktree (see "worktree check" below), a dirty main
checkout (commit the tracked changes first, a ship aborts otherwise), red or stuck CI (section 8b below).
The watch does not run merges beyond a MERGE READY finding, the registry audit, the v10 table or the
budget estimate. Deliberately held PRDs are listed in `IGNORE` in the script with the reason in the registry.

**Full tick, hourly** (cron `17 * * * *`; the old separate backstop is folded in). In order:
0. CronList: both jobs present (watch `3,23,43 * * * *`, full tick `17 * * * *`); recreate from this section if missing.
1. Run the watch script and read its findings first.
2. Budget: estimate Claude use against the pace line before filling any Claude lane. Pace line = days since the
   Sunday 08:00 reset / 7 x 100. Last operator reading is the newest entry in
   `docs/plans/2026-10-14-concurrency-and-budget-design.md`. Lead the report with an over-pace warning when the
   estimate or the reading is above the line; ask for a fresh reading. The lane count is the operator's decision.
3. Every non-terminal PRD including blocked and failed ones. Held `review_needed` task: retry with
   `datawatch autonomous resolve-review <prd> --task T --action retry --backend claude-code --model sonnet
   --effort high --note ...` after reading its attempt summary; a completed task's spec cannot be edited, so fold a
   correction into the next held task or add a task. After 3 failures of one task stop and bring it to the operator
   with an attempt summary (a stronger worker first, never the default Haiku). Idle Claude worker over 30 minutes
   with no file or commit activity: read its pane and nudge with `datawatch session send`.
4. Worktree check: each running PRD's directory under `~/.datawatch/prd-worktrees/<id>` has `go.mod`, is a
   registered worktree, and its `.datawatch/verify/*.sh` scripts are executable (a missing exec bit failed a final
   gate three times on 2026-10-26; a rebuilt worktree must carry the verify folder over).
   Claude lanes: the standing cap is 1. Operator, 2026-10-26: "Stay at 1 but use 2nd for special". Raise the cap to 2
   (`datawatch llm update claude-code --max-inflight 2`) only for a special item (a defect that blocks other work or
   throughput, a security finding that needs triage, a merge that cannot wait), return it to 1 when that item's Claude
   work is done, and state the use, its reason and open and close times in the next report.
5. Lane fill (GPU rule: GPU lanes cost no Claude usage, so Claude limits never hold GPU work). For every idle lane:
   a. decomposed PRDs: read the spec first (never run HOLD or sequenced-after-unshipped work), 4a grep main for what
      it asks for (a stale PRD is dropped and reported), validate (section 4, read tasks touching tmux, pkill,
      killall or systemctl line by line, no forbidden literals or release numbers in specs), ack coverage false
      positives, approve, run. Mechanical work goes to pi-prompt, design-heavy and debugging-heavy work to Claude.
   b. finished PRDs: merge (below). c. drafts: decompose. d. new work only from decided designs, after searching the
   registry, `prd-list` and `scripts/branch-audit.py`. An implementation PRD for an open design point is gated on a
   `Confirmed: yes` line in the proposal file.
6. Merge procedure: `/tmp/sv/trial-merge.sh <id>` in a throwaway worktree; resolve conflicts (a helper subagent for
   semantic ones); changelog body = lines added by the branch against its merge-base; `BUMP=minor` for a feature,
   patch for a fix or refactor; `/tmp/sv/finish-merge.sh` (lint plus full `go test` with `env -u TMUX -u TMUX_TMPDIR`);
   ship only on go exit 0 and only with a clean main checkout; `/tmp/sv/ship-merge.sh`; cancel the PRD record only
   after the ship succeeded; update the registry row; redeploy when a release or setting needs it and no worker is
   on a final task (record in-flight sessions; resume PRDs the restart parked). After each redeploy run
   `scripts/check-waiter-nodes.sh` with the daemon address and token (`DATAWATCH_URL`, `DATAWATCH_TOKEN`): exit
   status 2, or any over-limit node it reports, is a defect to triage before the next tick — the pool may briefly
   read over limit at boot while resumed workers drain, so re-run after a minute before filing.
7. Registry audit (`docs/plans/README.md` against reality; `scripts/branch-audit.py`; a pending branch is merged
   or recorded as dropped; every defect has a row and a draft PRD) and CI for all workflows on main and the latest
   tags (`gh run list`; read failures; security patches are consumed as soon as available, an unfixed CVE stays
   suppressed in `security/accepted-risks.yml`; never rerun a security workflow; leave external dependabot and
   `auto/cve-cleanup` PRs alone).
9. Dogfood (operator, 2026-10-26: "remember we should be using DataWatch automation and debugging as we do so, where
   possible"). When a datawatch built-in replaces one of these checks (the BL609 follow-on steps 9 to 11: daemon-side
   watch, lane fill switched on, merge mechanics), run it in parallel with the manual check, compare the outputs, file
   every difference as a tracker row and a draft PRD, and retire the manual check only after a full day of agreement.
8. Report: one line per lane, then state changes, then the audit result, then `python3 scripts/v10-status.py`.
   Questions go as plain text at the end, one at a time. Never use the blocking question tool. Record every
   operator answer in the design or decisions doc with the words quoted.

### Tick prompt (superseded 2026-10-26 by 11a; kept for history, 2026-10-20)

The tick runs these checks in order and reports only state changes and idle lanes with blockers:

0. tmux: `systemctl --user show datawatch-tmux -p NRestarts,ActiveEnterTimestamp`; baseline NRestarts 3 and 18:46:38 EDT (third tmux death, 2026-10-20 18:46:35). Never kill the tmux server.
1. Infrastructure: qwen3.8-64k:27b resident locally (`ollama ps`) and on 100.64.0.20.
2. Every non-terminal PRD (`prd-list`, `prd-get`). A session silent for 15 minutes after a read with no reply is wedged: kill that one session by ID.
3. Model and routing drift: restate backend and decomposition profile together in `prd-set-llm`.
4. **Lane fill is mandatory (operator, 2026-10-21: "I shouldn't have to keep asking about idle lanes").** After checks 0 to 3, look at GPU 1, GPU 2 and Claude 1 to 3. Every lane that is not running a PRD MUST be given work in this same tick, taking the first source that has something:
   a. Already-decomposed PRDs (`prd-list` approved, then needs_review): read the spec first (a spec that says HOLD or "sequenced last" is never run; BL424 was started by mistake on 2026-10-21), validate against section 4, approve, run.
   b. PRDs whose tasks are all done but whose status is blocked or failed: rejudge or merge (section 7).
   c. Draft PRDs: set backend and decomposition profile together, decompose, validate, run.
   d. File a new PRD from the roadmap: the lowest-numbered not-started row in `docs/plans/README.md` whose dependencies are done (rows 10, 11, 12, 13 first, then open defects), with the design document's phase section as the spec, and decompose it.
   GPU lanes take mechanical PRDs on pi-prompt (tests, docs, ports, small fixes, CRUD); Claude decomposes everything and takes the hard debugging and design-heavy PRDs. Keep decompose-ahead: at all times at least one validated, approved PRD waits per lane type so a freed lane starts without a tick. The report line for an idle lane must name a blocker with an identifier (for example "GPU 2: projected weekly 47% vs 46.7% allowed") or the lane gets work. "Nothing eligible" is never a valid report; a lane left idle without a named blocker is a supervisor defect, fixed in that tick and recorded in the BL514 row.
   Pace: use the last operator-reported weekly % and project it forward (assume +4 points per 24 hours with no new reading, and +2 points for each Claude PRD started since the reading). Ask the operator for a reading at most once per day and never wait for the answer. If the projection exceeds the allowed pace, move the work to the GPU lanes rather than idling. A one-off Claude decompose is allowed inside the pace; P0 per the P0 rule.
5. Validate every decompose against section 4, including the tests rule.
6. Merges per AGENT.md; the supervisor then pushes (fast-forward, Section A green) and tags patch and minor releases itself, watches CI, and reruns a failed Release. Do not ask the operator; if a risk remains, ship what is ready and file a follow-up PRD.
   **A judge timeout never justifies merging around the judge (operator, 2026-10-27, after asking why a large change could not be judged).** Verified: with `judge_policy` empty the judges follow `verifier_policy.backends` = pi-prompt then claude-code, one attempt each, 30 minutes per run, and a PRD whose judge times out on every backend is held in needs_review (`judge_timeout_hold`). Hold it and report it. Do not snapshot or commit the worktree and merge, as was done for BL613. Open improvement (design): size-aware routing so a large diff goes straight to Claude instead of spending 30 minutes on the local model first.
   **Parity issue at every merge (operator, 2026-10-27: "Parity is required, no design there, it is a core rule").** Before the ship step, check `git diff <merge-base> <branch> -- internal/server/web/app.js internal/server/web/index.html internal/server/web/locales`. If the PWA changed in an operator-visible way (new strings, layout, behaviour, API shape, settings, affordances added or removed), file a datawatch-app issue for the release titled `feat(i18n): add <keys> for <feature> parity (datawatch vX.Y.Z)` (or `[sync] ...` when there are no new strings) with the keys and EN values, the screen, the API contract change and the parent release, as the Mobile-Parity Rule in AGENT.md says, and name it in the release report. It is not a design decision and it needs no operator word. The audit of 2026-10-27 found 58 of 68 PWA-changing releases since v9.0.0 with no datawatch-app issue naming the version; the backfill is tracked as its own item.

**File the next step at the merge of the previous one (added 2026-10-29).** When a merge ships step N of a multi-step plan, in the same pass either file step N+1 as a draft PRD (when the plan already carries its design and the operator's confirmation covers it) or put one plain question in that report asking for the go-ahead (when the confirmation covered only the shipped step, or the plan says the step needs re-confirmation). A step is never parked as "design-gated" when its design is written and only a filing or a go-ahead is missing. BL610 Step 2 sat unfiled for two days after Step 1 shipped for exactly this reason.

**New PRDs run inside an Automation (added 2026-10-29).** Operator, quoted: "shouldn't prd be in automation, how else are we going to test and debug". Approve the PRD, create or extend an Automation with it as a member, activate the Automation and let the coordinator start and advance it; do not run a PRD on its own except to rescue a held one. Log every defect, surprise or missing feature in `docs/plans/2026-10-29-automation-dogfood-findings.md` and file a tracker row for each. A PRD that is already running is not adopted until a cheap PRD has tested adoption.
7. **Registry audit is mandatory every tick (operator, 2026-10-21: "you should audit the BL backlog for complete stuff, on ticks").** `docs/plans/README.md` is the backlog; it must never say draft, running, approved, blocked or not started for work that has shipped. Each tick: (a) list rows whose status cell is not done and look up each named PRD (`prd-list`) and its branch (`git rev-list --count main..automaton/<id>`); (b) a PRD completed with 0 commits ahead is shipped: mark the row **done** with the release it shipped in (find it in `CHANGELOG.md` or `git log`); (c) a PRD completed with commits ahead is a pending merge: mark the row "completed, merge pending" and use it as lane-fill source (b) in check 4; (d) a PRD blocked, failed or cancelled while its work is merged gets its record cancelled (never left blocked); (e) every new defect gets a row and a draft PRD the moment it is found; (f) after any merge, update the row in the same commit; (g) run `python3 scripts/branch-audit.py` every tick: it compares branch commits to main by subject, because the commit count ahead of main is wrong for any merge that rewrites commits and made every branch look like stale noise; a PENDING line (PRD completed, work not on main) must be merged or recorded as dropped with a reason in the registry the same tick, and a `landed` branch is deleted; (h) before filing or creating any PRD from the roadmap, search for the capability first: the registry, `prd-list` including completed and cancelled PRDs, and the branch audit; if work for it exists, stop and report what exists instead of filing. The audit output (rows changed) goes in the tick report.
8b. **CI is part of every tick, all workflows, not only Release (operator, 2026-10-24: "is the tick still monitoring and fixing the ci").** Each tick run `gh run list --limit 20` and for the head of main and every tag pushed since the last tick list the result of every workflow (Release, security-scan, docs-sync, secret-scan, CI summary). Any `failure` or a run stuck in progress for over 90 minutes is read with `gh run view <id> --log-failed` in the same tick and either fixed (a registry, docs or test fix goes straight to main; a code fix goes through a PRD) or filed as a defect row with a draft PRD and named in the report. A hung step (the stats-cluster tarball) is rerun with `gh run rerun <id> --failed`. A red workflow is never reported as "CI running". AGENT.md governs the rest, and the tick follows it: after a fix is confirmed green, delete the failed runs (`gh run delete <id>`) so the next failure stands out; the security-failure rule in AGENT.md applies (operator, 2026-10-25: the 72-hour window is withdrawn and security patches are consumed as soon as they are available): a dependency or CVE failure with a fixed version available is upgraded now, one with no fix stays suppressed in the registry with a real trace, and the operator is told only for a reachable high or critical finding or a suppression renewed past its first expiry; A suppression is added to `security/accepted-risks.yml` and generated, never to `.trivyignore`, with the real first_added date (it is immutable), the real trace, and a package version read from the scan or the image, not guessed.
9. **v10.0.0 status block in every report (operator, 2026-10-21).** Run `python3 scripts/v10-status.py` and paste its table: PRDs split into features and bugs (total, done, running, waiting, blocked), tasks for each (total, done, left), worker time, average time per PRD and per task, what is left in the registry for 10.0.0 (open rows, with phase rows expanded), and two time-to-completion estimates (measured throughput, and lane math from the average PRD time over five lanes). The two estimates are a range, not a promise. Fix a misclassified PRD in the script's keyword list, not by hand.
8. Report: one line per lane (occupant, or the named blocker), then state changes only. Never end a report with a question about idle lanes.

### Lesson: third tmux server death, 2026-10-20 18:46:35 (cause found)

- **Time:** the dedicated tmux server (`datawatch-tmux.service`) died at 18:46:35 EDT and every session died with it, including the supervisor. systemd restarted it at 18:46:38 (NRestarts 3). Earlier deaths: 2026-10-18 20:18:09 (found: a pi worker's sandbox helper) and 2026-10-18 22:55:31.
- **Cause:** at 22:46:32.852Z (18:46:32 EDT) the Claude Haiku worker of PRD `148b0001` (live smoke cleanup of the retried-task story) ran `TMUX_TMPDIR=$S/tmux tmux kill-server`. tmux gives the `$TMUX` variable priority over `TMUX_TMPDIR`, and every worker runs inside a pane where `$TMUX` points at the production socket, so the "sandbox" kill hit the production server. It is the same mechanism as the 2026-10-18 20:18 death. Found in `~/.claude/projects/-home-dmz--datawatch-prd-worktrees-148b0001/*.jsonl` by searching the minute before the death.
- **Why it recurred:** the fix PRDs for this class (BL504 `c7f10001`, then `e2120001`) were cancelled on 2026-10-19 as duplicated by the scoped cleanup in `scripts/run-tests.sh` and `scripts/release-smoke.sh`. Those scripts are scoped, but a worker writes and runs its own cleanup, so the worker environment itself must make the production socket unreachable.
- **Recovered:** no task was left running or verifying. After the restart the daemon resumed the workers, and `148b0001`, `17880001`, `677d0001` and `51670001` completed. `cece0001` is blocked with all five tasks completed (verdict defect). Merges for six completed PRDs are pending.

- **Hold exception (operator, 2026-10-20 23:00):** the hold on test-heavy PRDs until the production tmux guard (`df790001`) ships is lifted for the two release blockers only: the Go version bump (`9db60001`) and the flaky autonomous tests (`4cbc0001`). Each is validated first, runs with Sonnet or Haiku workers on a Claude lane, and its tests run with `env -u TMUX -u TMUX_TMPDIR`. Everything else stays held.

- **Hold lifted (2026-10-20 23:39):** the production tmux guard shipped as 9.5.34 and the daemon runs it. PRD-owned worker, verifier and guardrail sessions start without `TMUX` and `TMUX_TMPDIR` (verified on a new worker's process). Operator-started sessions are unchanged. Keep running tests with `env -u TMUX -u TMUX_TMPDIR`, and keep reading any task that touches tmux, pkill, killall or systemctl before its verification runs.
- **Restart revives stale smoke PRDs (BL500, 2026-10-20 23:38):** `3b518952` and `69e8421b` came back as running after every daemon restart, even after a cancel (the cancel leaves them `failed`). Cancel them after each restart until BL500 ships, or ask the operator to delete them.

- **Set the Sonnet verifier before approving a Claude-lane PRD (lesson, 2026-10-20 23:57).** `prd-set-verification-llm` and `prd-set-llm` are accepted only while the PRD is `needs_review`, `revisions_asked` or `cancelled`; on an approved or running PRD the call fails (HTTP 400) and my commands hid the error. So the Claude-lane PRDs `148b0001`, `51670001`, `677d0001`, `df790001`, `9db60001`, `4cbc0001` and `60270001` were all verified by qwen on the GPU pool (`verifier_source: global`), not by Sonnet, and each such verify takes a GPU engine, which can push the pool past two engines. Order for a Claude-lane PRD: validate the plan, then `prd-set-llm` (worker) and `prd-set-verification-llm --backend claude-code --model sonnet`, read the result, then approve and run. Never send these calls to /dev/null.

## Lesson: merge helpers must refuse leftover conflict markers (2026-10-22)

The BL398 merge (v9.5.76) shipped `docs/testing-tracker.md` with unresolved conflict markers: the trial merge listed it as unresolved, only the changelog was resolved by hand, and `finish-merge.sh` committed everything with `git add -A`. The build, lint and tests do not read that file, so nothing failed. It was found on the next merge and fixed on main without a release. `finish-merge.sh` now aborts with `CONFLICT_MARKERS_REMAIN` when the staged diff adds a marker line. Before running it, list `git diff --name-only --diff-filter=U` and resolve every file it names, not only the one you expect. Earlier the helper script's `--ours` shortcut for `api.go` and `main.go` silently dropped branch changes; it now resolves only the version-variable hunks.

## Lesson: the planner copies absolute paths and process words into task specs (2026-10-22)

Task specs that contain an absolute home or temp path, or the words kill and tmux next to a session, fail the scope lint and block the PRD. Add this to every new PRD spec: "Wording rule for every task text and script: never use absolute paths, never use the words kill, killed or tmux, and never name process-ending commands; say session end path and terminal multiplexer." A blocked PRD accepts task edits; replace the text and reset the task with force.

## Lesson: run go mod tidy before shipping (2026-10-22)

The v9.5.90 release (BL428 Phase 5) failed the pipeline's "verify go.mod is tidy" step: the worker's code imported a module directly that `go.mod` still listed as indirect. Build, lint and tests do not catch it. `finish-merge.sh` now runs `go mod tidy` and stages `go.mod` and `go.sum` before committing, so the merge commit carries the fix. Also check the Release workflow's goreleaser job for every tag, not only the go test job.

## Rule: release numbering, features are minor and fixes are patch (operator 2026-10-23)

Each feature is a minor version bump (x.y.z becomes x.(y+1).0). Defects, fixes, flaky-test repairs and pure documentation or test changes that ship with a fix are patch bumps (x.y.z becomes x.y.(z+1)). The supervisor decides which a merge is by reading the PRD: if it adds operator-visible behaviour or a setting, it is a feature. Only the 10.0 tag belongs to the operator. Until 2026-10-23 every release was a patch bump, including features (9.5.100 to 9.5.125); those stay as released. The merge helper takes `BUMP=minor` for a feature and defaults to a patch. Features waiting at that date: the progress dashboard, the Automation sub-tab and the failure escalation policy; fixes: the Automata list paging, the external pull request review, the commit scope check and the restart matrix. **Which changes skip the bump (corrected 2026-10-28):** only files under `docs/plans/` and test-only changes skip it. An edit to any other doc (for example `docs/operations.md`), to a workflow, to a script or to `README.md` needs a patch bump with a CHANGELOG entry and the README release line; the CI version-bump check fails the push otherwise (BL622 was pushed without one and needed v9.30.2).

## Lesson: the GPU lanes sat idle for hours and the reports blamed the review backlog (2026-10-24)

From about 00:26 on 2026-10-24 until 06:46, with one 50 minute exception, both GPU lanes were idle while eight decomposed PRDs waited for review. The operator had to ask why.

What went wrong, in order of weight:

1. **I rewrote the 30 minute tick prompt and dropped the rule that sends mechanical PRDs to the GPU lanes** ("GPU lanes take mechanical PRDs on pi-prompt; Claude decomposes everything and takes hard work; keep decompose-ahead; if over pace move work to the GPU lanes"). The idle-lane rule near line 44 of this document says an idle lane is the supervisor's defect, but the shortened prompt no longer carried it.
2. **I treated my own review backlog as a blocker.** The reports said "idle, blocker: no approved mechanical PRD, PRD X is decomposed and waits for my review". That names an identifier but the blocker was me, which the rule forbids.
3. **I held approvals to protect the Claude budget** and applied that to every lane. The GPU lanes cost no Claude usage, so the hold had no reason to cover them. Every PRD had also been set to a Claude worker at filing time, so nothing was queued for the GPU lanes.

Rules from now on, also written into the tick prompt:

- Claude limits never hold work for the GPU lanes. If a GPU lane is idle and any PRD is needs_review, approved, draft or decomposable, review it, move its worker to pi-prompt with `prd-set-llm`, approve it and run it in the same tick.
- Mechanical patterns (config surfaces and parity, tests, docs, ports, small fixes, CRUD) go to the GPU lanes; design-heavy, debugging-heavy and UI-layout work waits for Claude.
- The supervisor's own review backlog is never a valid blocker. A lane is reported idle only for an external blocker with an identifier, such as an operator decision.
- Keep at least two mechanical PRDs queued for the GPU lanes at all times (decompose ahead).
- When shortening or rewriting the tick prompt, diff it against this document first.


## Lesson: a finished PRD was never merged and the same feature was built again (2026-10-24)

PRD `da660001` (automatic lane fill, 12 commits, completed 2026-10-19) never reached main, and on 2026-10-24 the supervisor filed BL514 and started building the same capability again. Two process gaps caused it. First, the registry audit counted commits ahead of main per branch; merges here rewrite commits, so 18 finished branches looked "ahead" forever and the whole list was read as stale noise, hiding the one completed PRD that was truly unmerged. Second, filing a PRD from the roadmap had no search-first step: row 95 described the problem and nobody looked for a completed PRD, a branch or a closeout for it. Fixes, all in check 7 above: the content-based `scripts/branch-audit.py` every tick, a PENDING branch must be merged or recorded as dropped in the same tick, and a search of the registry, all PRDs and the branches before any new PRD is filed. The Automation coordinator needs both as built-in behaviour (section 12).


## Lesson: security-scan was red for a day and the ticks only watched the Release workflow (2026-10-24)

Two earlier commits (the libbrotli1 and openjdk suppressions) edited `.trivyignore` by hand. The security-scan job regenerates `.trivyignore` from `security/accepted-risks.yml` and fails on any difference, so every push to main since then showed a red security-scan. The ticks listed only the Release workflow and called it healthy. Fixed on 2026-10-24 by moving the three CVEs into the registry (traces kept) and regenerating. Rules: check 8b above (all workflows every tick), and a suppression is only ever added to the registry, then generated, never to `.trivyignore` directly.

## Lesson: a blocking question stopped the ticks and the lanes sat idle for 16 hours (2026-10-24)

The ticks are session crons and fire only while the session is idle. At 15:10 on 2026-10-24 the supervisor asked a blocking multiple-choice question (BL607 decisions) and the operator answered at 07:21 the next day, 16 hours later. No tick ran in between, so the lanes sat idle, 4 PRDs finished unmerged, 3 more failed or blocked and nothing was triaged. Rule: before asking any blocking question, first put every lane on work that does not depend on the answer, merge what is finished, and start the merges and deploys that take time; ask the question only when the next tick is not due within the next hour, and prefer a plain-text question at the end of the report (which does not block the crons) for anything that can wait. Do not use the blocking question tool when the operator may be away.


## Lesson: a stale draft PRD was run and duplicated shipped work (2026-10-25)

PRD 85d00001 (BL431 Phase 3) was a draft from 2026-10-22 that asked for pause, resume, cancel, skip, insert, remove and reorder on an Automation, on REST, MCP, CLI and the comm channel. Phases 2 and 3a had already shipped all of it. The supervisor decomposed and approved it on 2026-10-25 without grepping main, and it ran 8 tasks (about 4,500 lines) before the merge attempt showed 22 conflicts in files that already contained the same functions. Rule: before running any decomposed or draft PRD, grep main for the names it asks for (check 4a), and when the PRD is older than the last release, read the registry rows of the work it depends on.

### Everything that waits on the operator is a question in the queue (2026-10-30)

Operator, 2026-10-30, quoted: "Should blocked or held waiting on me also be questions?" Yes. With the question channel on (`autonomous.questions.enabled`), anything that waits on the operator (a design proposal, a held or blocked PRD, a policy choice, a deletion I am not authorised to make) is raised with `datawatch question raise`: options, my recommendation with its reason, and a forced default (`hold` when nothing should happen without an answer). The tick report lists the count of open questions and the oldest, not the text of each; it reads the queue at the start of every tick and acts on answers (an answer's quoted words go into the design or decisions doc as before). A question is raised once; do not raise duplicates. The first 15 were raised on 2026-10-30: node placement for evals, the eight pre-10.0.0 proposals, hung request options, the chain advance rule, model routing (BL517), push on complete (BL486), BL644 pending items and BL433.
