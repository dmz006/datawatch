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

- While the Claude weekly ceiling is exhausted, every PRD decompose, run, verify and
  smoke session uses the pi or pi-prompt backend with a local qwen model. No
  claude-code and no opencode. The window and its history are in
  [concurrency and budget, section 4b](2026-10-14-concurrency-and-budget-design.md).
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

- Every story and task has a title and a spec. The planning parser once accepted blank
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
- Before decomposing, append the atomic-sizing guidance block to the spec while the PRD
  is still a draft.

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

- A recurring wakeup runs the tick every five minutes, and an hourly backstop confirms
  the tick chain is alive.
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
- **Local GPU lane idle, root cause (2026-10-19).** The local node johnnyjohnny ran nothing
  for most of a day while its 64k model was resident and the lane was defined. The LLM
  registry lists qwen3.8-64k:27b only on the datawatch node, so every local-model PRD
  routed to node datawatch and queued behind it. The lane was defined on paper but had no
  route. Lesson: a lane is defined by its node and its model, and both must be bound in the
  registry. Each tick must check that every defined lane has a route: the model appears under
  its node in `datawatch llm list`, and a freshly pinned PRD lands on that node in
  `datawatch capacity`. A lane with no route is a blocker to name, not an idle state to report.
  Lanes, defined: datawatch GPU = node datawatch, ollama-datawatch/qwen3.8-64k:27b;
  local GPU = node johnnyjohnny, ollama-johnnyjohnny/qwen3.8-64k:27b (binding pending);
  Claude 1 and Claude 2 = claude-code, two lanes only.
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

Design choices left to the operator: the escalation channel and thresholds, which
surfaces map to which regression suites, and how an urgent bug may jump the queue.
