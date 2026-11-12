# PRD Commit Scope — Verification Report

Verification of three operator-confirmed requirements about automatic
PRD commits. The initial pass was read-only (the worktree and direct-mode
requirements were previously not covered, and gap 3 had no fix); follow-up
tasks then added the four tests named below and one small code change
(`prdAutoCommits` in `internal/autonomous/executor.go`, see the
`Change made` section). Every cited test exists and passes:

```
env -u TMUX -u TMUX_TMPDIR go test ./internal/session/ ./internal/gitguard/ -count=1 -run 'Scoped|Scope|Keep'
# ok  github.com/dmz006/datawatch/internal/session   0.755s
# ok  github.com/dmz006/datawatch/internal/gitguard  0.003s
```

The previously cited
`TestRun_CreatesWorktreeWhenConfigured` and
`TestSpawnRequest_GitFalse_WhenPRDHasNoGitSettings` (internal/autonomous)
and every git_scope test named below were additionally run individually and
pass, as do the new tests above.
## 1. Stages only files under the task's declared write dirs

**Requirement** — A PRD commit stages only the files under the task's declared write dirs.

**Enforcing code path** — `internal/session/git.go`:
`ProjectGit.stageChanges(scope)` is the single staging entry for
`PreSessionCommit`, `CommitSessionWork` and `CommitTaskWork`. With a
non-empty scope it filters tracked-diff, staged and untracked candidates
(`scopedCandidates` + `underAnyDir`), passes them through the staging guard
(`stageCandidates` → `internal/gitguard/policy.go` `Keep`/`RefuseReason`),
skips scope dirs outside the repo with a warning, and stages nothing if no
scope dir is inside the repo. `commitOnCompletion`
(`internal/session/manager.go`) calls `CommitTaskWork(.../current.WriteScope)`.
The scope arrives via `internal/autonomous/executor.go`
(`SpawnRequest{WriteScope: EffectiveTaskWriteDirs(prd, t)}`, `scope.go`)
forwarded by `buildAutonomousSessionBody` (`cmd/datawatch/autonomous_effort.go`)
into `session.StartOptions.WriteScope` (`internal/server/api.go`,
`internal/session/manager.go`).

**Proving test** — `TestCommitSessionWork_ScopedStagesOnlyInScopeDir`,
`TestCommitSessionWork_ScopedOutOfScopeDirtyTreeStillCommitsOnlyScope`,
`TestPreSessionCommit_ScopedLeavesOutOfScopeUnstaged`,
`TestStageChanges_ScopeDirOutsideRepoDoesNotLeakOrError`,
`TestPostSessionCommit_Scoped` (all in `internal/session/git_scope_test.go`);
guard: `TestKeepSizeCap`, `TestRefuseReason` (`internal/gitguard/policy_test.go`).

Handoff (the PRD's dirs actually reach the session body that starts the
spawned task session), previously not covered, now proven by
`TestWriteScopeForwardedFromDeclaredWriteDirs`
(`internal/autonomous/executor_write_scope_test.go`) — the spawn request's
`WriteScope` is exactly the declared PRD write dirs (and a task ProjectDir
override does not replace them) — and
`TestBuildAutonomousSessionBodyCarriesWriteScope`
(`cmd/datawatch/autonomous_effort_test.go`) — the session-start body built
from the spawn request carries `write_scope` through to
`session.StartOptions.WriteScope`.

**Verdict** — covered: the stage-only-in-scope semantics are proven by the
git_scope tests above, the staging guard is proven by the gitguard tests
above, and the executor→session handoff of the PRD's dirs has been proven
by the two handoff tests just named (that handoff was previously not
covered).

## 2. Worktree mode: scope equals the PRD's worktree

**Requirement** — A worktree-mode PRD scopes its automatic commit to its own worktree.

**Enforcing code path** — `internal/autonomous/worktree.go`:
`Manager.ensureWorktreeIfConfigured` sets `prd.ProjectDir` to the worktree
path (and `prd.Git.Branch`). `internal/autonomous/scope.go`:
`EffectiveTaskWriteDirs` → `EffectiveWriteDirs` falls back to
`prd.ProjectDir`, so with no declared WriteDirs the session's `WriteScope`
is the worktree path; with declared WriteDirs those win instead.

**Proving test** — `TestWriteScopeWorktreeModeIsOwnWorktree`
(`internal/autonomous/executor_write_scope_test.go`): a real throwaway
worktree is created by the same `ensureWorktreeIfConfigured` gate `Run()`
uses (scratch git repo as base, worktree under a temp dir, no WriteDirs
declared), and the spawn request's `WriteScope` is asserted to be exactly
the PRD's own worktree path. `TestRun_CreatesWorktreeWhenConfigured`
(`internal/autonomous/worktree_run_test.go`) proves the prerequisite that
`SpawnRequest.ProjectDir` is the worktree path; the direct-mode test
`TestDirectModeWithoutWriteDirsDoesNotAutoCommit` also pins the
worktree-mode row's `WriteScope` (the PRD project dir / worktree).

**Verdict** — covered (previously not covered: no test previously asserted
`SpawnRequest.WriteScope` equals the worktree path)

## 3. Direct mode with no declared write dirs: no auto-commit

**Requirement** — A direct-mode PRD (no worktree) with no declared write dirs does not auto-commit.

**Enforcing code path** — `internal/autonomous/executor.go` now builds the
spawn request with `Git: prdAutoCommits(prd)` (previously
`Git: prdHasGit(prd)`). `prdAutoCommits` is false unless the PRD has git
settings **and** either runs in worktree mode
(`prd.Git.Branch != ""`) or declares write dirs
(`len(prd.WriteDirs) > 0`), so a direct-mode PRD with no declared write
dirs gets `StartOptions.Git=false`; `commitOnCompletion`
(`internal/session/manager.go`) then returns before any staging. A
direct-mode PRD that *does* declare write dirs still auto-commits, scoped
to those dirs via `WriteScope: EffectiveTaskWriteDirs(prd, t)`. Planning
and judgment sessions use `SkipSnapshot` (manager.go
`StartOptions.SkipSnapshot`) and never stage; that is a separate, tested
safety net, not this requirement.

**Change made** — the direct-mode task did change code:
`internal/autonomous/executor.go` gained the helper `prdAutoCommits` (next
to `prdHasGit`), and the spawn request now uses it for `Git` while other
callsites keep `prdHasGit` where "has git settings" is the right question.
The rule in one line: a PRD's task sessions get git activity (and therefore
an auto-commit) only when the PRD has git settings **and** is in worktree
mode or declares write dirs — a direct-mode PRD with no declared write
dirs never auto-commits. No other production code was changed. (This case
was previously not covered, and the path as previously written violated
the requirement in the git-set case: the whole project directory would
have been staged and committed.)

**Proving test** — `TestDirectModeWithoutWriteDirsDoesNotAutoCommit`
(`internal/autonomous/executor_direct_mode_commit_test.go`): direct mode
with git url set and no write dirs → `Git=false`; direct mode with a
project profile and no write dirs → `Git=false`; no git settings →
`Git=false` (unchanged); direct mode with write dirs declared → `Git=true`
and `WriteScope` exactly the declared dirs; worktree mode with no write
dirs → `Git=true` and `WriteScope` the worktree path (unchanged). Plus the
existing `TestSpawnRequest_GitFalse_WhenPRDHasNoGitSettings`
(`internal/autonomous/executor_git_test.go`, also updated for the new
gate) and `TestGitlessOneShotInRepoMakesNoCommit`
(`internal/session/oneshot_gitless_test.go`), which prove the session
side: a git-less session makes no commit even inside a repo.

**Verdict** — covered (previously not covered: the git-set /
no-write-dirs half was previously not covered, and is now fixed and pinned
by the tests above).

## Gaps (all closed)

1. Handoff test — closed: `TestWriteScopeForwardedFromDeclaredWriteDirs`
   (executor) and `TestBuildAutonomousSessionBodyCarriesWriteScope`
   (cmd) prove the PRD's dirs reach the session body (before these tasks
   no test referenced `WriteScope`).
2. Worktree test — closed: `TestWriteScopeWorktreeModeIsOwnWorktree`
   asserts the spawn request's `WriteScope` is the worktree path when no
   WriteDirs are declared; the declared-dirs case is covered by
   `TestWriteScopeForwardedFromDeclaredWriteDirs`.
3. Requirement 3 — closed with the smallest code change: the executor now
   gates `Git` on `prdAutoCommits` (see `Change made` above), and
   `TestDirectModeWithoutWriteDirsDoesNotAutoCommit` pins all five cases,
   including the unchanged worktree-mode and declared-dirs behaviors.
4. Optional session-level guard — folded into gap 3: the executor now sets
   `Git=false` for the direct-mode no-write-dirs case, so
   `commitOnCompletion` no longer runs for those sessions; the git-less
   session path is proven by `TestGitlessOneShotInRepoMakesNoCommit`.
