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

- **The operator makes every design decision.** The supervisor stops and asks when a
  choice is a design choice, when guidance is unclear, or when a finding is outside the
  task. Unexpected findings are reported, not chased.
- **The supervisor makes build and progression decisions** up to the point where the
  Automation coordinator exists, then hands off to it. That covers what to run next
  (the order in `docs/plans/README.md`), whether a decompose is good enough to approve,
  whether a finished PRD is ready to advance past, and how to recover a stuck task.
- **Commits.** The supervisor commits only validated work the operator has asked for.
  An automatic snapshot or task commit is not approval of its contents.
- **Resets.** A blocked task may be reset once after its cause is fixed, then run. A
  second reset needs the operator's approval.

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

## 12. What the Automation coordinator must absorb

The coordinator replaces the human loop, so these become mechanical:

- Plan validation (section 4) as an approval gate with a pass or reject verdict.
- Failure triage (section 5), including the same-failure-twice stop and escalation.
- Verifier self-check (section 6): re-run a failing verifier outside the worker.
- Tree admission and scoped commits (section 7), already started as BL469 and BL470.
- The one-shot validation gate and reaper (section 8), shipped as part of this work.
- The after-done checklist and a registry-versus-README consistency check
  (sections 9 and 10) before it advances.
- Alerting when a bad state sits silent. In one incident two tasks sat blocked for about
  fifty minutes before a person noticed.

Design choices left to the operator: the escalation channel and thresholds, which
surfaces map to which regression suites, and how an urgent bug may jump the queue.
