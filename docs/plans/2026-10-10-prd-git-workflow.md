# Plan: PRD Git Workflow — branch-per-PRD + auto-PR (BL407)

- **Date**: 2026-10-10
- **Version at planning**: v9.0.17
- **Status**: In progress — Phase 0 done (v9.0.18, fixes B114), Phase 1 next

## Context

BL406 (Configurable Project-Rules Enforcement) shipped completely as of
v9.0.17. Per the locked v10.0.0 Stage 1 sequencing
(`docs/plans/README.md`'s Roadmap section), **BL407 is next** — it's the
second half of the "traditional build" exercise before Stage 2's
PRD-driven shakedown run. The need was first raised while scoping
BL406 Phase 2: every PRD this session has driven (including BL406's own
implementation) ran directly inside `/home/dmz/workspace/datawatch` —
this repo's own live checkout — with no branch, no isolation, and no
reviewable diff before it landed. That's fine for a human operator
typing commands, but it's the wrong default for an autonomous PRD loop
that's about to run unattended against a real backlog.

Two rounds of live investigation (this session, 2026-10-10) found the
problem is bigger than "add git wiring":

1. **No branch-creation logic exists anywhere.** `internal/session`'s
   `ProjectGit.PreSessionCommit`/`PostSessionCommit`
   (`internal/session/git.go:40-59`) commit on whatever branch is
   already checked out — they never create or switch branches.
   `internal/agents/post_session.go`'s `PostSessionPRHook` pushes a
   branch and opens a PR, but only one that's *already* checked out
   (`cfg.Pusher.CurrentBranch(dir)` — read, not create). `PRD` has zero
   git-related fields; `autonomous.Config` has zero git-related knobs.
   The only place in the whole repo that actually creates a branch +
   commits + pushes + opens a PR end-to-end is
   `cmd/datawatch/main.go:14806`'s `openTestingTrackerPR` (the
   `datawatch test --pr` CLI feature) — unrelated to PRDs, but a
   working, provable pattern to mirror (`runGitCmd`/`findGitRoot`
   helpers, `git checkout -b` → add → commit → push → `gh pr create`).

2. **`PRD.ClusterProfile` dispatch is not functionally wired today,
   independent of git.** `cmd/datawatch/main.go:4452-4480` (the
   `autonomousSpawn` closure's cluster-dispatch branch) returns
   `SpawnResult{SessionID: "agent:" + out.ID}` — a synthetic string
   never registered in `session.Manager`. The executor's own verify
   loop then polls `mgr.GetSession(task.SessionID)` for that ID, which
   can never resolve — it spins until context cancellation. Separately,
   `docker_driver.go:178-179` has a literal comment ("Task, for now, is
   visible to the worker via env... Actually invoking the task lives in
   the session start flow (Sprint 6+)") confirming task execution
   *inside* the container isn't wired yet either. `PostSessionPRHook`
   has never been exercised end-to-end against a real docker/k8s spawn
   in production (only against test mocks in
   `post_session_test.go`) — the session-binding mechanism it depends
   on (`SetAgentBinding`) is a manual operator action on a
   *pre-existing* local session, never something the `/api/agents`
   spawn path or the autonomous executor does automatically.

   Operator decision (this session): fix cluster-dispatch for real
   first (treat it as BL407's own Phase 0, not a separate deferred bug),
   then build git plumbing for both the local-worktree and
   cluster/container paths in the same arc, rather than shipping
   worktree-mode now and leaving cluster-mode broken.

3. **Design notes already captured** (`docs/plans/README.md`, filed
   2026-10-10 while scoping BL406 Phase 2): local git worktree
   (`~/.datawatch/prd-worktrees/<prd-id>/`, shares this repo's `.git`
   object store) for the common case; container dispatch reusing the
   already-isolated `PRD.ClusterProfile` path for stronger isolation
   needs. Centralization happens via git push + PR on completion, not a
   shared directory — this resolves cleanly for container mode
   specifically, since the working tree disappears with the container
   but nothing is lost once the push has happened. PRD/session
   *metadata* (title, status, decisions, telemetry) is already
   centralized via the autonomous `Store` regardless of mode — not
   something this plan needs to solve.

4. **Cross-reference, not in scope**: interim session output/telemetry
   during a run is pull-only for container-dispatched sessions
   (`forwardSessionToAgent`, `internal/server/api.go:1733` live-proxies
   every output request — nothing durable accumulates on the
   orchestrating daemon). `docs/plans/2026-10-04-f2-session-worker-isolation.md`
   §11 already names this as a separate, undesigned F-2/container-
   hardening concern (operator wants push-primary + pull-fallback,
   mirroring the session-list SSE+polling pattern) — BL407 covers the
   *final* diff (branch push + PR at PRD completion), not interim
   visibility during the run. This plan cross-references §11 but does
   not build it.

## Confirmed current-state findings (live-verified 2026-10-10)

- `ProjectGit.PreSessionCommit`/`PostSessionCommit`
  (`internal/session/git.go:40-59`): whole-tree `git add -A` + commit,
  no-ops if clean, errors logged but never fatal to the session. Wired
  via `session.Manager.SetAutoGit`/`StartOptions.AutoGitCommit` — this
  is the mechanism every session already uses; BL407 reuses it as-is
  for per-task commits, just pointed at a PRD's worktree directory
  instead of whatever `ProjectDir` happened to be set to.
- `git.Provider` interface (`internal/git/provider.go:65-90`):
  `OpenPR(ctx, PROptions) (string, error)` already exists, already
  shells to `gh pr create --head <branch> [--base <base>]` via
  `GitHub.OpenPR` (`internal/git/github.go:84-113`), already parses the
  PR URL out of `gh`'s output. BL407 reuses this directly — no new
  provider interface method needed. (Note: `openTestingTrackerPR` shells
  `gh pr create` directly rather than going through `git.Provider` —
  inconsistent with the rest of the codebase, but pre-existing and out
  of scope here; BL407's new code goes through `git.Provider.OpenPR`.)
- `profile.GitSpec` (`internal/profile/project.go:177-182`) is the
  existing shape for "a repo's git identity": `Provider`, `URL`,
  `Branch` (singular, static, used as *base*), `AutoPR`. `PRD` needs its
  own, differently-shaped `Git` config — a PRD needs a *generated*
  branch name (one per PRD), not a shared static one.
- `PRD.ProjectDir` (`internal/autonomous/models.go:118`) is honored
  whenever `ProjectProfile`/`ClusterProfile` are empty — this is the
  existing seam BL407's worktree mode plugs into: set `ProjectDir` once
  to the worktree path at PRD-run time, and every existing task-spawn
  code path (local `/api/sessions/start`) already does the right thing
  with zero executor changes.
- `agents.CloneOnBootstrap` (`internal/agents/worker_clone.go:30-72`):
  worker-side `git clone [--branch <existing-branch>]` — the pattern
  BL407's Phase 3 extends to `git clone && git checkout -b <new-branch>`
  for a PRD-owned branch instead of an existing static one.
- `AgentResult` (`internal/agents/spawn.go:143-148`): `{Status,
  Summary, Artifacts, ReportedAt}` — no branch/commit field. BL407 Phase
  3 adds one.

## Decisions

1. **Branch naming**: `automaton/<prd-id>-<slug>` — new
   `autonomous.branchNameFor(prd *PRD) string` helper, lowercased,
   non-alnum collapsed to `-`, title truncated to keep the whole branch
   name under ~60 chars. Computed once at PRD-run time and persisted on
   `PRD.Git.Branch` (idempotent — a daemon restart mid-run doesn't
   generate a second branch for the same PRD).
2. **Worktree mode is daemon-config-gated, not per-PRD-flagged.** New
   `autonomous.Config.WorktreeBaseRepo string` (path to the repo
   worktrees are created from, e.g. this checkout for self-build work;
   empty = feature off, fully backward compatible) and
   `WorktreeDir string` (default `~/.datawatch/prd-worktrees`). When a
   PRD has no `ProjectDir`/`ProjectProfile`/`ClusterProfile` set **and**
   `WorktreeBaseRepo` is configured, the executor creates the worktree
   automatically at PRD-run time and persists the resulting path onto
   `PRD.ProjectDir` — no new per-PRD opt-in needed; an operator who sets
   the daemon-wide config gets the safety default (never run bare in
   the base repo) for every future PRD without having to remember to
   ask for it each time. A PRD that already sets an explicit
   `ProjectDir` is left alone (explicit wins, same convention as today).
3. **`PRD.Git.AutoPR` defaults to false** (explicit per-PRD opt-in),
   consistent with `profile.GitSpec.AutoPR`'s own default and with
   `docs/operations.md`'s existing caution about unconfirmed GitHub
   actions. A daemon-wide `autonomous.Config.DefaultAutoPR bool` mirrors
   the existing `default_quality_gates` pattern for operators who want
   it on by default.
4. **Commit granularity**: reuse `session.ProjectGit` as-is, one commit
   per task (matches today's per-session granularity exactly — no new
   commit-batching logic).
5. **Worktree cleanup**: delete (`git worktree remove`) on a PRD that
   successfully pushes + opens a PR; keep on `blocked`/`failed`/
   `cancelled` for operator inspection. Cleanup is attempted but
   non-fatal (a `git worktree remove` failure logs and leaves the
   directory, it never blocks marking the PRD completed).
6. **Base branch**: `PRD.Git.BaseBranch` optional override; default is
   the worktree base repo's current default branch (reuse
   `findGitRoot`-adjacent helpers, resolve via `git symbolic-ref
   refs/remotes/origin/HEAD`, fall back to `main`).
7. **Cluster-mode PR trigger is callback-driven, not local-push-driven.**
   Once Phase 0 makes task execution inside the container real, the
   worker commits + pushes *itself* (it has the only real clone) using
   its bootstrap-minted token, then reports the branch name via an
   extended `AgentResult`. The parent only calls `git.Provider.OpenPR`
   once it has that branch name — it never attempts
   `Pusher.PushBranch` against a local directory for container-
   dispatched work (that directory doesn't contain the container's
   commits, confirmed this session).
8. **One PR per PRD, not per task.** Both modes trigger push+PR at
   PRD-completion (`PRDCompleted`/`PRDFailed`/`PRDBlocked` transition in
   `internal/autonomous/manager.go`), never at individual
   session/task-end — this is the key difference from
   `PostSessionPRHook`'s existing per-session trigger, which would open
   one PR per task if naively reused at PRD scope.

## Phases

### Phase 0 — Make `PRD.ClusterProfile` dispatch actually work (no git yet)
**Status: Done (v9.0.18, shipped 2026-10-10).**
- [x] Replaced the synthetic `SpawnResult{SessionID: "agent:"+out.ID}`
  with a real virtual `session.Session`, registered by
  `autonomousSpawn`'s cluster branch via `mgr.SaveSession` and a new
  `agents.VirtualSessionFullID(hostname, agentID)` helper (deterministic
  so the result-report handler can recompute it without a reverse
  index) — mirrors the existing council-virtual-session pattern
  (`cmd/datawatch/main.go`'s `councilOrch.SessionFn`). Chose the
  session-registration route over polling `/api/agents/{id}` directly,
  as planned: the verify loop's existing `GetSession`/`Kill` code path
  needed zero changes (`Kill` already guards the no-tmux case for
  virtual sessions).
- [x] Wired actual task execution inside the container — but this
  needed one more piece than the plan anticipated: `/api/agents/{id}/
  result` (the "existing" completion-report endpoint) turned out to be
  registered on the authenticated API router with no credential a
  worker actually holds (its bootstrap token is single-use, already
  burned). New `agents.Manager` mints a per-agent **result-report
  token** at `Spawn` (same pattern as the existing secrets token);
  new pre-auth `POST /api/agents/report` (registered alongside
  bootstrap/secrets on `mux`, not `apiMux`) authenticates with it
  instead. `Backend`/`Effort`/`Model`/`PermissionMode` added to
  `SpawnRequest`/`Agent`/`BootstrapResponse` so the worker knows what
  to run `Task` with (previously dropped entirely on the cluster
  path). The worker — a full datawatch daemon itself — now starts
  `Task` as a local one-shot session (`mgr.Start`, `OneShot: true`)
  once its own daemon finishes booting after the clone, waits for a
  terminal state, and calls the new `agents.ReportResult` (mirrors
  `CallBootstrap`'s TLS-pinning shape). On receipt,
  `handleAgentReport` flips the virtual session directly — no polling
  bridge was needed in the end, since the report is itself the event.
- [x] Regression tests: 11 new (`internal/agents/spawn_test.go`:
  token mint/lookup/revoke-on-Terminate, task-settings copied onto
  `Agent`, `VirtualSessionFullID` determinism; new
  `internal/server/agent_result_report_test.go`: success/failure
  report flips the virtual session to the right terminal state, wrong/
  missing token rejected, bootstrap response carries the result token
  + task settings).
- [x] **Phase Completion Checklist**: full `go test ./...` green (0
  failures), `go vet ./...` clean, `gosec -severity high -confidence
  medium` shows zero new findings on any touched file, `gofmt` clean.
  No new operator-facing REST/MCP/CLI/comm/PWA surface — the new route
  and token are worker↔parent plumbing only — so no Mobile-Parity
  issue needed. CHANGELOG + `docs/testing-tracker.md` + this plan +
  `docs/plans/README.md`'s B114 entry updated; version bumped to
  v9.0.18. **Known, accepted limitation** (documented in code and
  CHANGELOG, not silently dropped): a worker task that genuinely hangs
  isn't caught by the parent's existing stall-detection (keyed off
  fields a virtual session doesn't have) — the existing agent
  idle-timeout reaper is the backstop. Out of scope for "make it
  resolve at all."

### Phase 1 — `PRD.Git` config + local worktree isolation
**Status: Not started.**
- [ ] New `PRD.Git` struct (`internal/autonomous/models.go`):
  `{Provider, URL, BaseBranch, Branch, AutoPR, PRURL}` — `Provider`/`URL`
  resolved from `PRD.ProjectProfile`'s `GitSpec` when set, else
  auto-detected from the worktree's own `origin` remote.
- [ ] New `autonomous.Config.WorktreeBaseRepo`/`WorktreeDir` (Decision 2).
- [ ] New `internal/autonomous/worktree.go`: `EnsureWorktree(prd *PRD, cfg
  Config) (path string, err error)` — `git worktree add -b <branch>
  <path> <base>` under `WorktreeDir`, idempotent (if the worktree
  already exists for this PRD, reuse it rather than erroring).
- [ ] Wire into the PRD run-start transition (`Manager.Run` or wherever a
  PRD moves to `PRDRunning`): when the gating in Decision 2 applies,
  call `EnsureWorktree`, persist the path onto `PRD.ProjectDir` and the
  branch onto `PRD.Git.Branch`.
- [ ] No executor.go changes needed beyond this — every task spawn already
  honors `PRD.ProjectDir`.
- [ ] **Phase Completion Checklist**.

### Phase 2 — PRD-completion push + PR (local-worktree mode)
**Status: Not started.**
- [ ] New completion hook in `internal/autonomous` (manager.go, wherever a
  PRD lands in `PRDCompleted`/`PRDFailed`/`PRDBlocked`): when
  `PRD.Git.AutoPR` is true and the PRD ran in worktree mode, push the
  branch (new small helper mirroring `openTestingTrackerPR`'s
  `runGitCmd(dir, "push", "-u", "origin", branch)`, not a new
  abstraction) then call `git.Provider.OpenPR` with the PRD's
  title/spec as PR title/body. Persist the returned URL onto
  `PRD.Git.PRURL`; record a `Decision` (same shape as every other
  PRD-lifecycle event).
- [ ] Worktree cleanup per Decision 5.
- [ ] Regression test: a completed PRD with `AutoPR:true` in worktree mode
  produces a real local branch + a `git.Provider.OpenPR` call (fake
  provider in tests, mirroring `internal/git/provider_test.go`'s
  existing fake-`gh`-failure-path shape) with the right head/base/title.
- [ ] **Phase Completion Checklist**.

### Phase 3 — Cluster-mode git: worker-side commit/push + completion callback
**Status: Not started.** Depends on Phase 0.
- [ ] Extend `AgentResult` (`internal/agents/spawn.go:143-148`) with
  `Branch string`/`CommitSHA string`.
- [ ] Worker-side (wherever Phase 0 wires task invocation): after the task
  completes, `git add -A && git commit` (same message shape as
  `ProjectGit.PostSessionCommit`) then `git push` using the
  bootstrap-minted token already available to the worker, then include
  the branch/SHA in its result report.
- [ ] Parent-side: on `PRDCompleted` for a cluster-dispatched PRD, call
  `git.Provider.OpenPR` using the reported branch — no local
  `PushBranch` call for this path (Decision 7).
- [ ] **Phase Completion Checklist**.

### Phase 4 — Branch creation parity for cluster mode
**Status: Not started.** Depends on Phase 1 (branch-naming helper) and
Phase 0 (real dispatch).
- [ ] Extend `CloneOnBootstrap` (`internal/agents/worker_clone.go`) with a
  "create new branch" mode: `git clone && git checkout -b <branch>`
  instead of `--branch <existing>`, using the same `branchNameFor`
  helper from Phase 1 so local and cluster modes never diverge on
  naming. The bootstrap response needs to carry the new branch name
  (already has a `Git` sub-struct per the existing `BootstrapResponse`
  shape — add the field, don't restructure it).
- [ ] **Phase Completion Checklist**.

### Phase 5 — Config/parity surface
**Status: Not started.**
- [ ] `PRD.Git.AutoPR`/`BaseBranch` settable via existing structural-edit
  patterns (`autonomous_prd_edit_*` MCP tools, REST, CLI, comm, PWA) —
  mirrors how `PRD.GuidedModeSource`/`PerStoryApproval` etc. are already
  exposed, not a new mechanism.
- [ ] `autonomous.Config.WorktreeBaseRepo`/`WorktreeDir`/`DefaultAutoPR` via
  the existing `GET/PUT /api/config` + `autonomous_config_set` pattern
  (same as BL406 Phase 0's `rules_file`/`context_file`).
- [ ] PWA: PRD Settings card gains the `Git` section (AutoPR checkbox, base
  branch field, read-only branch/PR-URL display once set) — Mobile-
  Parity issue filed per AGENT.md's rule, same as BL406 Phase 5's
  `approve_task` (`datawatch-app#248`).
- [ ] **Phase Completion Checklist**.

### Phase 6 — Docs + closure
**Status: Not started.**
- [ ] `docs/operations.md`: new section alongside "Autonomous External
  GitHub Actions" (same caution framing — this is a third unconfirmed-
  GitHub-action mechanism, document it next to the other two, not as an
  unrelated new heading).
- [ ] `docs/config-reference.yaml`, `docs/testing-tracker.md`, CHANGELOG,
  version bump.
- [ ] `docs/plans/README.md`: close out the BL407 entry; file the Phase 0
  fix as its own named bug (next number after B113, i.e. **B114** —
  "`PRD.ClusterProfile` dispatch never resolves: synthetic session ID +
  unwired container task execution") so it's traceable independent of
  this plan's git-workflow framing, same way B111/B112 were tracked
  alongside BL406 Phase 5.
- [ ] Cross-reference `docs/plans/2026-10-04-f2-session-worker-isolation.md`
  §11 from both this plan and the operations.md section (Context
  item 4) — explicitly note interim telemetry remains unsolved, not
  silently implied as covered by the completion-time push.

## Parity surface

| Capability | REST | MCP | CLI | Comm | YAML | PWA | Android/iOS |
|---|---|---|---|---|---|---|---|
| Cluster-dispatch fix (Phase 0) | n/a (internal correctness fix) | n/a | n/a | n/a | n/a | n/a | n/a |
| `PRD.Git` config (Phase 1/5) | existing structural-edit endpoints, extended | existing `autonomous_prd_edit_*` tools, extended | existing CLI edit commands, extended | existing comm edit verbs, extended | `autonomous.worktree_base_repo`/`worktree_dir`/`default_auto_pr` | new Git section on PRD Settings card | reporting-only |
| Push+PR at completion (Phase 2-4) | reporting via PRD `Decisions`/`Git.PRURL` | same | same | same | n/a (runtime PRD state) | PR URL link on PRD detail | reporting-only, parity issue filed |

## Reuse-and-Expand audit

- Per-task commits → existing `session.ProjectGit`, zero new commit
  logic (Decision 4).
- PR opening → existing `git.Provider.OpenPR`/`PROptions`, zero new
  interface method.
- Worktree/push helpers → mirror `openTestingTrackerPR`'s proven
  `runGitCmd`/`findGitRoot` shape (`cmd/datawatch/main.go:14806`), not a
  new git-shelling abstraction.
- Config gating → mirrors `default_quality_gates`'s existing
  daemon-wide-default-plus-per-PRD-override shape.
- Structural-edit/parity wiring → mirrors BL406 Phase 5's
  `approve_task` precedent exactly (same REST/MCP/CLI/comm/PWA checklist,
  same Mobile-Parity issue-filing step).

## Out of scope / deferred

- Interim session output/telemetry durability during a run (F-2 §11) —
  cross-referenced, not built here.
- Fixing `Story.ExecutionProfile` being dead code (never consulted by
  the executor when building a `SpawnRequest`) — a separate,
  pre-existing gap found during this session's investigation, unrelated
  to branch-per-PRD git workflow. Worth its own backlog entry, not
  rolled into BL407.
- Auto-merging an opened PR — no mechanism anywhere in this codebase
  opens AND merges a PR; BL407 keeps the existing "open for operator
  review" behavior.
- Container/k8s hardening itself (network/filesystem namespace
  scoping, credential scoping) — that's F-2/BL395's job; BL407 only
  makes the *existing* `ClusterProfile` dispatch path functionally
  correct and gives it git plumbing, it doesn't change its isolation
  properties.

## Testing / verification

- Phase 0: regression test proving a cluster-dispatched task reaches a
  terminal state instead of polling forever (test-doubled driver).
- Phase 1: `EnsureWorktree` unit tests (fresh create, idempotent re-run,
  bad base-repo-path error case) against a real scratch git repo in
  `t.TempDir()`.
- Phase 2: fake `git.Provider`-driven test proving one `OpenPR` call per
  PRD completion with the right head/base/title, plus a real `git` CLI
  smoke (branch created, commits present, pushed to a local bare-repo
  test remote — same "use a real local remote, not just mocks" rigor
  BL406 Phase 4's upstream-issue tests used).
- Phase 3/4: worker-side commit/push tested against the same kind of
  local bare-repo test remote; completion-callback payload shape
  tested like every other `AgentResult`-consuming test.
- Mobile-Parity Rule audit for Phase 5's new PWA Git section.
- Full `go test ./...` + `node --test` green before closing any phase,
  per AGENT.md's Phase Completion Checklist.

## Files (representative, not exhaustive)

- `internal/autonomous/models.go` (`PRD.Git`), new
  `internal/autonomous/worktree.go` — Phase 1.
- `internal/autonomous/manager.go` (completion hook) — Phase 2.
- `internal/agents/spawn.go` (`AgentResult.Branch`/`CommitSHA`),
  `internal/agents/worker_clone.go` (branch-create mode),
  `internal/agents/docker_driver.go` (Phase 0's task-invocation fix) —
  Phases 0, 3, 4.
- `cmd/datawatch/main.go` (`autonomousSpawn`'s cluster branch,
  verify-loop fix) — Phase 0.
- `internal/server/web/app.js`, `internal/server/web/locales/*.json` —
  Phase 5.
- `docs/operations.md`, `docs/config-reference.yaml`,
  `docs/plans/README.md` (B114 entry) — Phase 6.
