# Project-dir worktree integrity: how an Automaton directory becomes an empty repository

Date: 2026-10-25. Scope: findings only, no source was changed. A release that fixes this is "the next patch above main".

Symptom: an Automaton's `prd.ProjectDir` (a per-Automaton linked worktree under the PRD worktrees folder) is found to be a freshly initialised standalone repository on branch `master` or `main`. It holds only `CLAUDE.md`, an mcp file and a checkpoint file, has no `go.mod` and no source, and is not listed by `git worktree list` in the base repository.

## Root cause

Confirmed chain. Every step below was read in the code and the end state was reproduced in a scratch directory (see Reproduction).

1. **The worktree is removed while the Automaton still owns the path.**
   `Manager.handleWorktreeCompletion` (`internal/autonomous/git_completion.go`) runs `git worktree remove --force <ProjectDir>` once a PRD rolls up to completed and the PR is open. `Manager.cleanupOrphanedWorktree` (same file, called from `Reject` and `Archive` in `internal/autonomous/manager.go`) runs a plain `git worktree remove <ProjectDir>`. Both leave `prd.ProjectDir` and `prd.Git.Branch` unchanged on purpose ("historical record"). The branch itself survives in the base repository. After either call the directory is gone and nothing in the PRD records that.

2. **The next run, resume or retry skips the only function that could notice.**
   `Manager.ensureWorktreeIfConfigured` (`internal/autonomous/worktree.go`) returns immediately when `prd.ProjectDir != ""`. `Run` (`internal/autonomous/executor.go`, the call near the top of the function) and the two decompose paths all go through it. Its own comment says it self-gates on `ProjectDir` so "a later resume naturally skips this". So `EnsureWorktree` is never reached for a PRD whose worktree has since been removed. There is no other existence check: a search for `os.Stat` on `ProjectDir` or the task dir in `internal/autonomous` and `cmd/datawatch` finds none.

3. **The executor spawns the worker into the missing path.**
   The executor builds `SpawnRequest.ProjectDir` from `resolveTaskProjectDir(prd, t)` (`internal/autonomous/executor.go`, the spawn call) with `Git: prdAutoCommits(prd)` true (a branch is set) and `SkipDirtyCapture: true`. No step verifies the directory.

4. **SpawnFn is inert against a missing path, but self-heals into a fake repo on the next launch.**
   In `cmd/datawatch/main.go` the `capturePreTask` closure calls `ensureProjectDirOwnGitRepo` (`cmd/datawatch/verifier_diff.go`) and then `git -C <dir> rev-parse HEAD`. With the directory missing, `git -C` fails ("cannot change to ...") and `ensureProjectDirOwnGitRepo` returns without acting. Once the directory exists (step 5), `ensureProjectDirOwnGitRepo` behaves differently, see step 6.

5. **`session.Manager.Start` recreates the directory and writes the session files into it.**
   `internal/session/manager.go` `Start` runs `os.MkdirAll(projectDir, 0755)` unconditionally. For a PRD worker the launch then writes `CLAUDE.md` (`Tracker.WriteSessionGuardrails`, `internal/session/tracker.go`, target `<ProjectDir>/CLAUDE.md`), the per-session mcp file (`channel.WriteProjectMCPConfig`, `internal/channel/mcp_config.go`, invoked from the pre-launch hook in `cmd/datawatch/main.go`) and, once the worker starts, the worker's own `CHECKPOINT.md`. That is exactly the observed file set.

6. **Something then runs `git init` on it. Two independent code paths do, and they explain the two observed branch names.**
   - **`main`:** `Start` runs `ProjectGit.IsRepo()` (`git rev-parse --is-inside-work-tree`, `internal/session/git.go`). If no ancestor of the directory is a repository, `IsRepo` is false and `ProjectGit.Init` runs `git init -b main` (fallback plain `git init`). This block is gated on `opt.Git && effectiveOneShot`, and PRD workers satisfy both (`prdAutoCommits` is true when `Git.Branch != ""`; prompt backends are one-shot). The new `.git` then satisfies `gitguard.RefuseNonProject` (its marker list includes `.git`), so `PreSessionCommit` is allowed to run against the fake repository.
   - **`master`:** if an ancestor of the directory is the operator's home directory repository (the worktree folder lives under `~/.datawatch`), `IsRepo` is true and `Start` does not init. On the next launch of any task for the PRD, the directory exists, `git rev-parse --show-toplevel` returns exactly the home directory, and `ensureProjectDirOwnGitRepo` runs a plain `git init <projectDir>` with no `-b`. On a default git configuration that creates `master`. This is the guard added for the home-directory staging incident; it is correct for its own purpose and wrong here because it treats "missing worktree recreated as an empty dir" the same as "ordinary project dir with no repo".

7. **The fake repository then looks healthy and is reused.**
   `.git` is now a directory. `EnsureWorktree`'s reuse check is `os.Stat(path/.git)` (`internal/autonomous/worktree.go`), which succeeds for a `.git` directory as well as for the `.git` file of a real linked worktree. Any later call that does reach it (for example a path that clears `ProjectDir`, or a future caller) silently reuses the fake. The base repository's `git worktree list` never contained it, which is why it is "not listed".

Evidence for: every step is in the code; both branch names are produced by distinct, named code; the reproduction yields the exact end state (empty standalone repo, `.git` directory, only the three session files, absent from `git worktree list`) for both `main` and `master`. `git worktree remove` is only ever called from the two functions in step 1 (repo-wide search of `scripts`, `internal`, `cmd` and CI files finds no other `worktree remove` or `worktree prune`).

Evidence against / not confirmed: the task limited reads to the repository, so the live incident directory, the operator's home directory and the daemon log were not inspected. It is therefore not confirmed which of the two `git init` paths (or which of the two removal functions) fired in the incident. The discriminators are cheap to check by whoever owns the incident: the stray repo's branch (`main` means `Start`'s init, `master` means `ensureProjectDirOwnGitRepo`), whether `git -C <dir> log` is empty or contains only a session snapshot commit, whether the base repo still has the `automaton/<id>-...` branch, and the daemon log lines `orphaned worktree removed` / `worktree remove failed` and `[warn] git init`. If the operator's home is not a repository, only the `main` path applies.

Ruled out as causes for a worktree under the PRD worktrees folder:
- `os.RemoveAll` in `internal/session/manager.go`: `DeleteSession` (reap of an ephemeral workspace) is guarded by `filepath.Rel` against `<dataDir>/workspaces` and by `EphemeralWorkspace`; the `deleteData` removal deletes the session tracking dir; `ReapOrphanWorkspaces` only sweeps direct children of `<dataDir>/workspaces`.
- `os.RemoveAll` in `internal/session/oneshot_reap.go`: `PruneSessionFolders` removes only `<dataDir>/sessions/<id>`.
- Caveat: if an operator configures `autonomous.worktree_dir` inside `<dataDir>/workspaces`, `ReapOrphanWorkspaces` could delete a worktree directory that no stored session references, and the chain above would then apply unchanged. The default location is not under that root.
- `scripts/merge-branches-bump-version.sh`: it only changes to the repo root, requires a clean tree and merges; it contains no `worktree`, `branch -d` or `rm` operation, so a merge cannot remove a worktree. A merge can still happen while the Automaton has queued, held or retried tasks, but it does not touch the worktree directory. Removal while work remains comes only from the PRD lifecycle in step 1: completion roll-up followed by a later reset, resume or retry of a task, or `Reject`/`Archive` followed by a re-run of the same PRD.

## Reproduction

The reproduction is a shell sequence run entirely under `mktemp -d` in the scratchpad, with `HOME` and git config isolated (`GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_SYSTEM=/dev/null`, fixed author identity). It touches no real worktree or PRD directory. It was run to completion and produced the output summarised below. No Go test was added, because that would mean editing Go source, which this task forbids; the sequence mirrors the exact git commands the code runs.

Variant A (home is not a repository; ends on `main`):

```bash
T=$(mktemp -d); export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t
BASE=$T/a/base; WT=$T/a/prd-worktrees/5f440001
mkdir -p $BASE $T/a/prd-worktrees
git -C $BASE init -q -b main; echo hi > $BASE/go.mod
git -C $BASE add go.mod; git -C $BASE commit -q -m init
# EnsureWorktree
git -C $BASE worktree add -q -b automaton/5f440001-demo $WT main
ls -ld $WT/.git                         # a FILE: "gitdir: .../base/.git/worktrees/5f440001"
# handleWorktreeCompletion / cleanupOrphanedWorktree
git -C $WT worktree remove --force $WT
test -e $WT || echo "dir gone"          # branch automaton/5f440001-demo still in $BASE
# SpawnFn capturePreTask against the missing path
git -C $WT rev-parse --show-toplevel    # fatal: cannot change to '...': No such file or directory
# session.Manager.Start: MkdirAll, then session files
mkdir -p $WT
echo "# Session Guardrails" > $WT/CLAUDE.md
echo '{"mcpServers":{}}'   > $WT/.mcp.json
echo checkpoint            > $WT/CHECKPOINT.md
# ProjectGit.IsRepo -> false (no ancestor repo) -> ProjectGit.Init
git -C $WT rev-parse --is-inside-work-tree || git -C $WT init -q -b main
git -C $WT symbolic-ref --short HEAD    # main
test -d $WT/.git && echo "EnsureWorktree reuse check os.Stat(.git) would pass: .git is a DIRECTORY"
git -C $BASE worktree list              # only $BASE; the fake repo is not listed
```

Observed: branch `main`, `.git` is a directory, 0 tracked files, `git log` empty ("does not have any commits yet"), not in `git worktree list`.

Variant B (home is a repository; ends on `master`):

```bash
H=$T/b/home; BASEB=$T/b/base; WTB=$H/.datawatch/prd-worktrees/5f440001
mkdir -p $H/.datawatch/prd-worktrees $BASEB
git -C $H init -q -b main
git -C $BASEB init -q -b main; echo hi > $BASEB/go.mod
git -C $BASEB add go.mod; git -C $BASEB commit -q -m init
git -C $BASEB worktree add -q -b automaton/5f440001-demo $WTB main
git -C $WTB worktree remove --force $WTB
mkdir -p $WTB; echo g > $WTB/CLAUDE.md; echo '{}' > $WTB/.mcp.json     # first launch
git -C $WTB rev-parse --is-inside-work-tree      # true: IsRepo walks up into $H, Start does not init
[ "$(git -C $WTB rev-parse --show-toplevel)" = "$H" ] && git init -q $WTB   # next launch: ensureProjectDirOwnGitRepo
git -C $WTB symbolic-ref --short HEAD            # master
git -C $BASEB worktree list                      # only $BASEB
```

Observed: branch `master`, `.git` a directory, only the session files present, not in `git worktree list`.

Recovery check (the branch still exists, so the safe repair is possible but no code does it):

```bash
git -C $BASEB worktree add $WTB automaton/5f440001-demo
# fatal: '$WTB' already exists   <- once the fake directory is in place, even the retry in EnsureWorktree would fail
```

The reuse check, the `ensureWorktreeIfConfigured` early return, and `Start`'s `MkdirAll` and `Init` were verified by reading the code, not by executing the Go functions.

## Contributing faults

Each of these is independent: fixing one still leaves the others able to produce or hide the same end state.

1. **No launch-time verification of the project directory.** The executor spawn call and the `SpawnFn` closure in `cmd/datawatch/main.go` never check that `ProjectDir` is a live linked worktree on `prd.Git.Branch` of the expected base repository. A removed or replaced directory is launched into silently.
2. **`ensureWorktreeIfConfigured` is a one-shot gate.** It returns early whenever `prd.ProjectDir != ""`, so `EnsureWorktree`'s documented idempotent re-attach is never exercised on resume, retry or a re-run after completion, reject or archive.
3. **Teardown leaves no machine-readable trace.** `handleWorktreeCompletion` and `cleanupOrphanedWorktree` keep `ProjectDir` and `Git.Branch` and record nothing (no field, no Decision) saying the worktree was removed, so later code cannot distinguish "removed on purpose" from "never created". The `--force` removal in the completion path also discards uncommitted leftovers by design.
4. **`EnsureWorktree` reuse check accepts any `.git` entry.** `os.Stat(path/.git)` succeeds for a `.git` directory (a standalone repository, including the fake one) as well as for the `.git` file of a real linked worktree. It does not check that the entry is a file, that its `gitdir:` points under the base repository's `.git/worktrees`, that the path appears in `git worktree list`, or that HEAD is on the expected branch.
5. **`EnsureWorktree` failure signalling is weak.** When the directory exists but is not a usable worktree, `git worktree add` fails ("already exists" for a non-empty directory) and the retry on the existing branch fails the same way. The combined error string is the only signal; there is no classification, no repair and no refusal that names the cause.
6. **`Start` creates and initialises a missing directory instead of refusing.** `os.MkdirAll(projectDir, 0755)` then `ProjectGit.Init` (`git init -b main`, fallback plain `git init`) apply to a PRD-owned worktree path exactly as to an ad-hoc scratch directory. Nothing distinguishes a worktree-owned path.
7. **`ensureProjectDirOwnGitRepo` runs a plain `git init` on any directory whose repository toplevel is the home directory.** It has no notion of a worktree-owned directory, passes no `-b`, and so produces `master` on a default configuration. It runs at every launch, which is why a second task or retry turns the empty directory into a repository even when `Start` did not.
8. **`gitguard` treats any `.git` entry as a project marker.** `projectMarkers` in `internal/gitguard/projectdir.go` includes `.git`, so once a fake repository exists, `RefuseNonProject` stops refusing and `PreSessionCommit` and later automatic commits proceed against it.
9. **Both removal paths are best-effort and unordered with respect to outstanding work.** Completion roll-up removes the worktree even if the PRD can later be reset, resumed or have a held task resolved, and `Reject`/`Archive` clean up even though the same PRD can be driven again. Nothing marks the PRD as "worktree gone, must re-attach before running".

## Fix coverage

The three guards are: (G1) a launch-time worktree check, (G2) a tightened `EnsureWorktree` reuse check, and (G3) a session start refusal to re-initialise a worktree-owned directory.

| # | Fault | Guard that addresses it | How |
|---|---|---|---|
| 1 | No launch-time verification of the project directory | G1 | Before the worker is launched, for a PRD with `Git.Branch != ""` and no `ClusterProfile`, require that the task dir resolves inside a live linked worktree of the base repo (path listed in `git worktree list --porcelain`, `.git` is a file whose `gitdir:` is under the base repo, HEAD on `Git.Branch`). On failure fail the task with an explicit reason instead of spawning. |
| 2 | `ensureWorktreeIfConfigured` skips on `ProjectDir != ""` | G1 | The launch-time check covers resume, retry and re-run regardless of the early return. When the directory is absent or empty and the branch exists, G1 may re-attach with `git worktree add <path> <branch>`; a non-empty unusable directory is refused, never repaired in place. |
| 3 | Teardown leaves no trace on the PRD | G1 (detection by shape) | G1 does not depend on a flag: it detects by inspecting the directory. Recording a Decision when teardown removes the worktree is an optional follow-up that would make the refusal message more precise; it is not required for correctness. |
| 4 | Reuse check accepts a `.git` directory | G2 | Use `Lstat`; require a regular file, a `gitdir:` target under `<baseRepo>/.git/worktrees`, and membership in `git worktree list`. Anything else is not reused. |
| 5 | Weak failure signalling in `EnsureWorktree` | G2 (with G1) | Distinguish "missing", "empty", "foreign repository" and "live worktree" and return a classified error naming the path and the state, instead of the combined add/retry string. |
| 6 | `Start` `MkdirAll` + `Init` on a worktree-owned path | G3 | For a PRD-owned launch, refuse when the project dir does not exist (do not `MkdirAll`) and refuse `ProjectGit.Init` where the path belongs to a worktree-mode PRD; return a launch error. Ad-hoc sessions keep today's behaviour. |
| 7 | `ensureProjectDirOwnGitRepo` plain `git init` | G1 and G3 | G1 runs earlier in the same `SpawnFn` and refuses before this function can act on a recreated directory. G3's refusal must also be applied here (or this helper must skip worktree-owned paths), otherwise the helper remains a second route to `master`. |
| 8 | `gitguard` accepts any `.git` marker | Not covered by the three guards | G1 and G3 stop a fake repository from being created for PRD dirs, which makes this moot for the incident. Tightening the marker check (for example, requiring a repository with a commit) is a separate follow-up. |
| 9 | Removal not ordered against outstanding work | G1 | Detection at launch catches the consequence for every re-run path. A change to when completion removes the worktree (for example, not removing while the PRD can still be reset) is a design follow-up, not part of these guards. |
