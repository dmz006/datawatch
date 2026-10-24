# PRD Commit Scope — Verification Report

Read-only verification of three operator-confirmed requirements about
automatic PRD commits. No Go code was changed in this task; all cited tests
pre-exist and were run:

```
env -u TMUX -u TMUX_TMPDIR go test ./internal/session/ ./internal/gitguard/ -count=1 -run 'Scoped|Scope|Keep'
# ok  github.com/dmz006/datawatch/internal/session   0.755s
# ok  github.com/dmz006/datawatch/internal/gitguard  0.003s
```

Cited tests in other packages were additionally run individually and pass:
`TestRun_CreatesWorktreeWhenConfigured`, `TestSpawnRequest_GitFalse_WhenPRDHasNoGitSettings`
(internal/autonomous), and every git_scope test named below.

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

**Verdict** — covered: staging semantics are proven, but the
executor→session handoff of the PRD's dirs is *not* separately tested (see Gaps).

## 2. Worktree mode: scope equals the PRD's worktree

**Requirement** — A worktree-mode PRD scopes its automatic commit to its own worktree.

**Enforcing code path** — `internal/autonomous/worktree.go`:
`Manager.ensureWorktreeIfConfigured` sets `prd.ProjectDir` to the worktree
path (and `prd.Git.Branch`). `internal/autonomous/scope.go`:
`EffectiveTaskWriteDirs` → `EffectiveWriteDirs` falls back to
`prd.ProjectDir`, so with no declared WriteDirs the session's `WriteScope`
is the worktree path; with declared WriteDirs those win instead.

**Proving test** — not covered. `TestRun_CreatesWorktreeWhenConfigured`
(`internal/autonomous/worktree_run_test.go`) proves `SpawnRequest.ProjectDir`
is the worktree path, which is a prerequisite; but no test anywhere asserts
`SpawnRequest.WriteScope` equals the worktree path (grep of `*_test.go`
for `WriteScope`/`write_scope` matches nothing).

**Verdict** — not covered

## 3. Direct mode with no declared write dirs: no auto-commit

**Requirement** — A direct-mode PRD (no worktree) with no declared write dirs does not auto-commit.

**Enforcing code path** — partially. When `prdHasGit`
(`internal/autonomous/executor.go`) is false the session has `Git=false` and
`commitOnCompletion` (`internal/session/manager.go`) returns before any
staging — that half is enforced. But when git settings *are* set and no
WriteDirs are declared, `EffectiveTaskWriteDirs` silently widens to the
PRD's whole ProjectDir, so the auto-commit stages and commits everything
changed under the entire project directory — this **violates** the
requirement, it is not merely untested. Planning/judgment sessions use
`SkipSnapshot` (manager.go `StartOptions.SkipSnapshot`) and never stage;
that is a separate, tested safety net, not this requirement.

**Proving test** — `TestSpawnRequest_GitFalse_WhenPRDHasNoGitSettings`
(`internal/autonomous/executor_git_test.go`) and
`TestGitlessOneShotInRepoMakesNoCommit` (`internal/session/oneshot_gitless_test.go`)
cover the `Git=false` half. The git-set / no-write-dirs half: not covered.

**Verdict** — not covered, and the code path as written does not satisfy the
requirement in the git-set case.

## Gaps to close

1. Handoff test: an executor test asserting `SpawnRequest.WriteScope ==
   EffectiveTaskWriteDirs(prd, t)` for (a) declared PRD WriteDirs and
   (b) an undeclared-dirs PRD, proving the PRD's dirs actually reach the
   session body (no test references `WriteScope` today).
2. Worktree test: in `TestRun_CreatesWorktreeWhenConfigured` (or a sibling),
   assert `req.WriteScope == [worktree path]` when no WriteDirs are declared,
   and `== declared dirs` when they are.
3. Requirement 3 needs a decision plus code or a documenting negative test:
   either make `commitOnCompletion`/executor refuse to auto-commit when a
   direct-mode PRD has git settings but no declared WriteDirs (smallest
   change: gate on empty `WriteScope` for PRD task sessions), or add a test
   pinning the current whole-project-dir behavior if the operator accepts
   it. As written, the git-set case violates the requirement.
4. Optional: a session-level test that `commitOnCompletion` in PRD mode with
   an empty `WriteScope` and a dirty tree does not commit the out-of-run
   files (guards the `unscopedChangedFiles` fallback under PRD ownership).
