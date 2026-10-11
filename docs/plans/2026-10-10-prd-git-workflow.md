# Plan: PRD Git Workflow — branch-per-PRD + auto-PR (BL407)

- **Date**: 2026-10-10
- **Version at planning**: v9.0.17
- **Status**: In progress — Phase 0 done (v9.0.18, fixes B114), Phase 1 done (v9.0.19), Phase 2 done (v9.0.20), Phase 3 done (v9.0.21), Phase 4 done (v9.0.22), Phase 5 done (v9.0.23), Phase 6 next

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
**Status: Done (v9.0.19, shipped 2026-10-10).**
- [x] New `PRD.Git` struct (`GitWorkflow`,
  `internal/autonomous/models.go`): `{Provider, URL, BaseBranch, Branch,
  AutoPR, PRURL}`. Only `Branch` is populated this phase (by worktree
  creation) — `Provider`/`URL` auto-detection and `AutoPR`/`PRURL` wait
  for Phase 2, where they're actually consumed.
- [x] New `autonomous.Config.WorktreeBaseRepo`/`WorktreeDir`, mirrored
  into `config.AutonomousConfig` + the `amgrCfg` startup bridge
  (`cmd/datawatch/main.go`) + a new `applyConfigPatch` case pair, same
  as BL406 Phase 0's `rules_file`/`context_file` — traced each of
  REST/MCP (`config_set`)/CLI (`config set`)/comm (`configure`) to
  confirm they all proxy through the same generic `PUT /api/config`
  dot-path mechanism rather than assuming the precedent still held.
- [x] New `internal/autonomous/worktree.go`: `EnsureWorktree(prd *PRD,
  baseRepo, worktreeDir string) (path, branch string, err error)` —
  narrower signature than planned (two strings instead of the whole
  `Config`) for testability; `git worktree add -b <branch> <path>
  <base>`, idempotent (a PRD with `Git.Branch` already set and a live
  worktree at the expected path is a no-op reuse).
- [x] Wired into `Manager.Run`'s first-run setup (`executor.go`), right
  after the `PRDRunning` transition and before `EnsureIgnoredPatterns`/
  cross-seed (both read `prd.ProjectDir`). Self-gated on
  `ProjectDir == "" && ProjectProfile == "" && ClusterProfile == ""`
  rather than `isFirstRun` — once a worktree is created `ProjectDir`
  is persisted non-empty, so a resumed run naturally skips this
  without a separate "already did this" flag.
- [x] No executor.go changes needed beyond the hook above — every task
  spawn already honors `PRD.ProjectDir`.
- [x] **Phase Completion Checklist**: 12 new tests (8 unit against a
  real scratch git repo + 3 `Manager.Run` integration tests + 1 REST
  config-patch test), full `go test ./...` green, `go vet ./...`
  clean, `gosec` zero new findings, `gofmt` clean on every new file.
  `docs/config-reference.yaml` documents the two new daemon config
  fields. No Mobile-Parity issue needed — no new PWA surface this
  phase (that's Phase 5's job, together with `PRD.Git.AutoPR`).

### Phase 2 — PRD-completion push + PR (local-worktree mode)
**Status: Done (v9.0.20, shipped 2026-10-10).**
- [x] New completion hook, `Manager.handleWorktreeCompletion`
  (`internal/autonomous/git_completion.go`), called from the
  `PRDCompleted` rollup branch in `executor.go`: when `PRD.Git.AutoPR`
  is true and the PRD ran in worktree mode (`Git.Branch != ""`), push
  the branch (`runGitIn`, the same small helper Phase 1 added — no new
  abstraction) then call `git.Provider.OpenPR` with the PRD's
  title/spec as PR title/body, reusing the existing `gitProviderFn`
  test seam from BL406 Phase 4. Persists `PRD.Git.PRURL`; records a
  `git_pr_opened` Decision on success, `git_pr_failed` on either the
  push or the PR-open failing (best-effort throughout — never fails
  `Run()` itself, same shape as `PostSessionPRHook`/
  `fireUpstreamIssueActions`).
- [x] **Deviation from plan, found via `go test -race` mid-implementation**:
  originally wired as a background goroutine (mirroring the
  neighboring memory-report hook). That introduced a real data race —
  mutating the shared, unsynchronized `*PRD` pointer after `Run()` had
  already returned to its own caller. Fixed by making the hook run
  synchronously inside `Run()` instead: git push + `gh pr create` are
  fast, bounded network calls (unlike the report hook's unbounded LLM
  generation), and `Run()` already blocks synchronously on slower work
  earlier in the same function (the quality-gate baseline test run).
- [x] Worktree cleanup per Decision 5 — `git worktree remove --force`
  (the `--force` wasn't in the original plan; added after a real
  "contains modified or untracked files" failure surfaced in testing,
  see the next item).
- [x] **Second deviation, also found via integration testing**: a
  worktree-mode PRD's task sessions now force
  `auto_git_commit: true` (new `SpawnRequest.ForceAutoGitCommit`,
  threaded into `autonomousSpawn`'s local-session body in
  `cmd/datawatch/main.go`) regardless of this daemon's
  `session.auto_git_commit` default — without it, a daemon with that
  default off (true on this deployment) would silently produce an
  empty or near-empty PR, since nothing had ever been committed onto
  the branch in the first place. This wasn't in the original plan
  bullets; found because the integration test's `git worktree remove`
  failed with real uncommitted content, tracing back to the commit
  mechanism never having fired.
- [x] Regression tests: 7 unit (`git_completion_test.go`, against a
  real bare-remote + base-repo + worktree fixture) + 3 integration
  (`git_completion_run_test.go`, through `Manager.Run` end-to-end,
  including the `ForceAutoGitCommit` gating).
- [x] **Phase Completion Checklist**: full `go test ./...` green,
  `go vet ./...` clean, `gosec` zero new findings, `gofmt` clean,
  `go test -race` run specifically on this phase's new tests to verify
  the fix actually closed the race (confirmed the same race pattern
  is pre-existing elsewhere in `executor.go`, out of scope to fix
  broadly — the project's CI doesn't gate on `-race` either).
  CHANGELOG + testing-tracker + this plan updated.

### Phase 3 — Cluster-mode git: worker-side commit/push + completion callback
**Status: Done (v9.0.21).** Depended on Phase 0.
- [x] Extended `AgentResult` (`internal/agents/spawn.go`) with
  `Branch string`/`CommitSHA string`.
- [x] Worker-side: new `agents.PushOnCompletion` (`worker_clone.go`) —
  resolves the worker's current branch + HEAD SHA, pushes using the
  same bootstrap-minted token `CloneOnBootstrap` used to clone. Called
  from `runWorkerBootstrapTask` (`cmd/datawatch/main.go`) after the
  task session reaches a terminal state, win or lose (a failed task's
  partial progress is still worth preserving for inspection).
  Commit itself reuses `session.ProjectGit.PostSessionCommit` as
  planned (Decision 4) — no new commit logic — but needed a push to
  force it on: `runWorkerBootstrapTask` now always sets
  `StartOptions.AutoGitCommit: true` for the worker's one-shot task
  session, same rationale as Phase 2's `ForceAutoGitCommit` (this
  daemon's own `session.auto_git_commit` default is `false`).
- [x] Parent-side: `autonomousVerify` (`cmd/datawatch/main.go`) records
  the reported branch (plus `Git.URL`/`Provider`/`BaseBranch`, resolved
  from the dispatching Project Profile's `GitSpec`) onto `prd.Git` as
  soon as a cluster-dispatched task reports it. New
  `Manager.handleClusterCompletion` (`git_completion.go`) fires at the
  same `PRDCompleted` rollup as Phase 2's `handleWorktreeCompletion`
  and calls `git.Provider.OpenPR` using that branch — no local push
  for this path (Decision 7), confirmed via a shared `openCompletionPR`
  helper both completion paths now call.
- [x] **Deviation from plan, found while implementing**:
  `handleWorktreeCompletion` had no guard against ever being reached
  by a cluster-dispatched PRD before this phase (its `Git.Branch` was
  never set for one). Now that `autonomousVerify` sets it for cluster
  mode too, `handleWorktreeCompletion` needed an explicit
  `prd.ClusterProfile != ""` early-return — without it, PRD completion
  would have run `git push`/`git worktree remove` against an empty
  `ProjectDir`.
- [x] **Known, accepted limitation, scoped to Phase 4**: nothing yet
  creates a dedicated branch for cluster-mode tasks (Phase 1's
  `EnsureWorktree` has no cluster-mode counterpart). Until Phase 4
  extends `CloneOnBootstrap` with a create-new-branch mode, a cluster
  worker pushes to whatever branch the dispatching Project Profile's
  `GitSpec.Branch` names (or that repo's default branch if unset) —
  documented in the CHANGELOG as an operator caveat for today.
- [x] Regression tests: 2 (`internal/agents/worker_clone_test.go`,
  `PushOnCompletion` against a real bare-remote round trip) + 6
  (`internal/autonomous/cluster_completion_test.go`,
  `handleClusterCompletion`'s guards/success/failure paths plus
  `handleWorktreeCompletion`'s new cluster-mode no-op).
- [x] **Phase Completion Checklist**: full `go test ./...` green
  (3462+ passed), `go vet ./...` clean, `gosec -severity high
  -confidence medium` zero new findings, `gofmt` clean on every new/
  touched file (one genuine misalignment of my own in
  `cmd/datawatch/main.go` fixed; all other flagged files confirmed
  pre-existing drift via `git stash` same as prior phases), `go test
  -race` on this phase's new tests. CHANGELOG + testing-tracker + this
  plan updated. **`node --test internal/server/web/*.test.js`: N/A** —
  this phase touched zero PWA/JS files (confirmed: `git show --stat`
  on the v9.0.21 commit has no `internal/server/web/` entries), so
  there's nothing for that suite to exercise. Found missing/not called
  out explicitly at the time (flagged live 2026-10-10, same day) —
  logged here per AGENT.md's "N/A with a one-line reason, never
  silently skipped" rule.

### Phase 4 — Branch creation parity for cluster mode
**Status: Done (v9.0.22).** Depended on Phase 1 (branch-naming helper)
and Phase 0 (real dispatch).
- [x] `Manager.Run` assigns `prd.Git.Branch = branchNameFor(prd)` for a
  cluster-dispatched PRD (`prd.ClusterProfile != "" && prd.Git.Branch
  == ""`), the idempotent cluster-mode counterpart right next to
  Phase 1's worktree-mode `EnsureWorktree` call.
- [x] New `SpawnRequest.Branch` (`internal/autonomous/executor.go`)
  threads `prd.Git.Branch` down to the `SpawnFn`; `autonomousSpawn`
  (`cmd/datawatch/main.go`) forwards it as `"branch"` in the
  `/api/agents` POST body — the pre-existing `agents.SpawnRequest.Branch`
  field (F10 S7.3's workspace-lock field) was the first caller to ever
  give it a real, per-PRD value.
- [x] **Found and fixed, not just planned**: `handleAgentBootstrap`
  (`internal/server/agent_api.go`) built the worker's git bundle from
  `proj.Git.Branch` directly, silently dropping the per-spawn
  `agent.Branch` override that already existed — the F10 S7.3 field
  never actually reached a real `git clone` before this phase. Fixed:
  `resp.Git.Branch` now reads `agent.Branch` (falls back to
  `proj.Git.Branch` when unset — zero behavior change for every
  pre-Phase-4 caller).
- [x] New `BootstrapGit.CreateBranch` (mirrored into
  `internal/agents/client.go` per this codebase's "mirror server.*,
  don't import" convention) — true when the branch differs from the
  profile's own static default. `CloneOnBootstrap` clones the
  default branch + `git checkout -b <branch>` instead of `--branch
  <branch>` (which would fail outright against a branch that was
  never pushed); a resumed worker checks the branch back out and
  deliberately skips `git pull` (no upstream to pull from until
  Phase 3's `PushOnCompletion` pushes it — found live while testing,
  `git pull --ff-only` hard-fails with "no tracking information"
  against a local-only branch).
- [x] **Found and fixed a real concurrency bug this phase's own design
  would otherwise have introduced**: every task on one PRD now
  requests the *same* branch (Decision 8 — one PR per PRD), but the
  executor's bounded concurrent-task pool (BL370) can dispatch a
  second cluster task before the first's agent reaches a terminal
  state — `agents.Manager`'s F10 S7.3 workspace lock (built for two
  *unrelated* colliding agents) would reject that outright as a hard
  task failure. Fixed by extending the existing capacity-wait retry
  wrapper (`capacityRetrySpawn`) with a new `IsWorkspaceLockError` —
  retried exactly like a capacity-full wait, not bypassed, since two
  containers racing pushes to the same branch is a real hazard the
  lock exists to prevent.
- [x] Regression tests: 2 new (`internal/server/agent_api_test.go`:
  explicit-branch → `CreateBranch:true`, no-explicit-branch →
  unaffected `CreateBranch:false`) + 1 new
  (`internal/agents/worker_clone_test.go`:
  `TestCloneOnBootstrap_CreateBranchFresh`, a real local-git round
  trip proving clone-default+checkout-b, then a simulated worker
  restart landing back on the same branch without erroring) + 2 new
  (`internal/autonomous/cluster_branch_test.go`: `Run()` assigns +
  threads the branch, and idempotency across a simulated resume) + 2
  new (`internal/autonomous/capacity_test.go`:
  `TestCapacity_WorkspaceLockErrorIsWaitNotFailure`,
  `TestIsWorkspaceLockError`).
- [x] **Phase Completion Checklist**: full `go test ./...` green,
  `go vet ./...` clean, `gosec -severity high -confidence medium`
  zero new findings on any touched file, `gofmt` clean on every
  new/touched file except pre-existing cascading-struct-literal drift
  confirmed via `git stash` diffing (same files flagged dirty before
  and after my edit: `client.go`, `worker_clone_test.go`,
  `agent_api.go`, `agent_api_test.go`, `executor.go`,
  `capacity_test.go`). **`node --test internal/server/web/*.test.js`:
  N/A** — zero `internal/server/web/` files touched (confirmed via
  `git status` on this phase's own diff). No new operator-facing
  REST/MCP/CLI/comm/PWA surface — no Mobile-Parity issue needed, no
  localization keys added. Version bumped to v9.0.22 in both files.
  CHANGELOG + testing-tracker + this plan updated.

### Phase 5 — Config/parity surface
**Status: Done (v9.0.23).**
- [x] New `Manager.SetPRDGit(prdID, autoPR, baseBranch)` sets both
  `PRD.Git.AutoPR`/`BaseBranch` unconditionally on every call (full
  replace, same shape as `SetPRDGuardrails` — no partial-merge
  mechanism needed). Full surface: `POST
  /api/autonomous/prds/{id}/set_git` (REST), `autonomous_prd_set_git`
  (MCP), `prd-set-git <id> on|off [--base-branch]` (CLI). Comm-channel
  parity intentionally NOT added — this whole class of single-field
  PRD setters (`set_guided_mode`, `set_type`, `set_skills`,
  `set_continue_on_story_failure`, `set_memory_seed`) has never had
  comm-channel coverage in this codebase; adding it only for `set_git`
  would be inventing new symmetry beyond what the plan actually asked
  to mirror ("mirrors how `PRD.GuidedModeSource`/`PerStoryApproval`
  etc. are already exposed"), not closing a Phase-5-introduced gap.
- [x] New `autonomous.Config.DefaultAutoPR` (+ YAML-facing
  `config.AutonomousConfig.DefaultAutoPR` mirror + daemon-startup
  bridge in `cmd/datawatch/main.go`) — applied once at `CreatePRD`
  time, not resolved at use-time the way `DefaultQualityGates`/
  `ContinueOnStoryFailure` are (see Decision below). Confirmed
  `WorktreeBaseRepo`/`WorktreeDir` (Phase 1) and
  `RulesFile`/`ContextFile`/`UpstreamRepos` (BL406 Phase 0) were
  already fully `GET/PUT /api/config` + `GET/PUT /api/autonomous/config`
  round-trippable with zero new code — not re-plumbed, just verified.
  New `autonomous_config_set` MCP param for `default_auto_pr`.
- [x] **Deviation from plan, decided while implementing**: `DefaultAutoPR`
  is applied once at PRD-creation time rather than resolved at
  use-time (the `DefaultQualityGates`/`ContinueOnStoryFailure`
  pattern). `PRD.Git.AutoPR` is a plain `bool`, not a pointer — a
  resolve-at-use-time default couldn't distinguish "operator
  explicitly set this PRD's AutoPR back to false" from "never
  touched," so it would have made explicit opt-out impossible once a
  daemon-wide default was on. A one-time default at creation avoids
  the ambiguity entirely and leaves the field as the PRD's own real,
  freely overridable value from then on.
- [x] PWA: PRD Settings card gained the `Git` section (AutoPR checkbox,
  base branch field, read-only branch/PR-link display once set).
  Mobile-Parity issue filed: `datawatch-app#249` (same shape as BL406
  Phase 5's `approve_task`, `datawatch-app#248`).
- [x] **Found and fixed, not just planned**: the new MCP tool needed
  its own `federation.MCPToolCap` entry (`internal/federation/mcp_tool_caps.go`)
  or `POST /api/mcp/call` would 404 it as "unknown tool" despite being
  registered and reachable over stdio/SSE — caught by this repo's own
  `TestEveryUnconditionallyRegisteredToolHasAnMCPToolCapEntry` guard,
  not found by inspection. The REST handler's `AutonomousAPI` interface
  needed the new method added, which in turn needed a trivial stub on
  the shared `fakeOrchAutonomous` test double (5 other test files'
  fakes embed it and inherited the method for free) — same "one stub,
  many embedders" shape BL406 Phase 5's `ApproveTask` stub hit.
- [x] **Found and fixed live, PWA/locale**: the Git section's hint text
  first said "PRD" and left "automaton" untranslated in the 4
  non-English bundles — caught by `TestLocales_PRDNeverUserFacing`/
  `TestLocales_AutomatonNeverUntranslatedOrMistranslated` before this
  shipped.
- [x] Regression tests: 4 new in new
  `internal/autonomous/git_config_surface_test.go` (`SetPRDGit` sets +
  fully replaces both fields, not-found error case, `DefaultAutoPR`
  applied at creation, no-default leaves `AutoPR` false).
- [x] **Phase Completion Checklist**: full `go test ./...` green (83
  packages), `node --test internal/server/web/*.test.js` 182/182
  green, `go vet ./...` clean, `gosec -severity high -confidence
  medium` zero new findings on any touched file, `gofmt` clean on
  every new/touched file (one genuine misalignment of my own in
  `internal/mcp/server.go`'s inline-comment column, fixed; everything
  else flagged confirmed pre-existing via `git stash` diffing).
  Version bumped to v9.0.23. CHANGELOG + `docs/config-reference.yaml`
  + testing-tracker + this plan updated.

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
- New `internal/autonomous/git_completion.go` (`handleWorktreeCompletion`,
  `repoFromGitURL`), `internal/autonomous/executor.go` (synchronous call
  site in the `PRDCompleted` rollup, `SpawnRequest.ForceAutoGitCommit`),
  `cmd/datawatch/main.go` (`auto_git_commit` threading in
  `autonomousSpawn`'s local-session body) — Phase 2.
- `internal/agents/spawn.go` (`AgentResult.Branch`/`CommitSHA`),
  `internal/agents/docker_driver.go` (Phase 0's task-invocation fix) —
  Phases 0, 3.
- `internal/agents/worker_clone.go` (`PushOnCompletion`,
  `gitOutputText`), `cmd/datawatch/main.go` (`runWorkerBootstrapTask`'s
  forced `auto_git_commit` + push call, `autonomousVerify`'s branch/URL
  capture onto `prd.Git`), new
  `internal/autonomous/cluster_completion_test.go`,
  `internal/autonomous/git_completion.go`
  (`handleClusterCompletion`, shared `openCompletionPR` helper,
  `handleWorktreeCompletion`'s new `ClusterProfile` guard),
  `internal/autonomous/executor.go` (rollup calls
  `handleClusterCompletion` alongside `handleWorktreeCompletion`) —
  Phase 3.
- `internal/autonomous/executor.go` (`SpawnRequest.Branch`,
  cluster-mode branch assignment in `Run()`), new
  `internal/autonomous/cluster_branch_test.go`,
  `internal/agents/worker_clone.go` (`CreateBranch` clone/checkout-b
  mode for `CloneOnBootstrap`), `internal/agents/client.go`
  (`BootstrapGit.CreateBranch` mirror), `internal/server/agent_api.go`
  (`BootstrapGit.CreateBranch`, `handleAgentBootstrap` reading
  `agent.Branch` instead of `proj.Git.Branch`),
  `internal/autonomous/capacity.go` (`IsWorkspaceLockError`,
  `capacityRetrySpawn` extended), `cmd/datawatch/main.go`
  (`autonomousSpawn` forwards `req.Branch`) — Phase 4.
- `cmd/datawatch/main.go` (`autonomousSpawn`'s cluster branch,
  verify-loop fix) — Phase 0.
- `internal/autonomous/manager.go` (`SetPRDGit`, `Config.DefaultAutoPR`,
  `CreatePRD`'s default application), `internal/autonomous/api.go`
  (`API.SetPRDGit`), new `internal/autonomous/git_config_surface_test.go`,
  `internal/server/autonomous.go` (`set_git` REST action),
  `internal/server/api.go` (`AutonomousAPI.SetPRDGit`, generic
  `/api/config` GET/PUT surface for `default_auto_pr`),
  `internal/server/orchestrator_enrich_test.go` (`fakeOrchAutonomous`
  stub), `internal/config/config.go` (YAML-facing
  `AutonomousConfig.DefaultAutoPR` mirror), `cmd/datawatch/main.go`
  (daemon-startup config bridge), `internal/mcp/bl221_types.go`
  (`autonomous_prd_set_git` tool), `internal/mcp/autonomous.go`
  (`autonomous_config_set`'s new `default_auto_pr` param),
  `internal/mcp/server.go` (tool registration),
  `internal/federation/mcp_tool_caps.go` (`MCPToolCap` entry),
  `cmd/datawatch/cli_autonomous.go` (`prd-set-git` CLI command),
  `internal/server/web/app.js`, `internal/server/web/locales/*.json`
  (PRD Settings Git section) — Phase 5.
- `docs/operations.md`, `docs/config-reference.yaml`,
  `docs/plans/README.md` (B114 entry) — Phase 6.
