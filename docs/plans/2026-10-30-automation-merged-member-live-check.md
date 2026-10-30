# Automation merged-member live check

Date: 2026-10-30. Build under test: the working tree of branch `automaton/46df0001` (daemon version 9.42.2).

This is the record of a live check of the merged-member behaviour described in [the how-to](../howto/automation-merged-member.md) and [the flow](../flow/automation-merged-member-flow.md). It ran against a sandbox daemon, never the production daemon. Every result below was read back from that daemon's REST routes, CLI and alert list. Nothing here was inferred from the unit tests.

## Setup

- **Daemon.** A freshly built binary of this tree, started on a throwaway config generated from `testdata/datawatch.yaml` (the config the sandbox rules in `AGENT.md` name). It was started with `env -u TMUX -u TMUX_TMPDIR`, recorded through `scripts/test-daemon-lib.sh`, and ended by its exact PID through the same library. No tmux server command, no kill by name, and no `pkill` was used.
- **Ports.** The sandbox ports in `AGENT.md` (18443 TLS, 18080 HTTP) were already held by another process that is not part of this check, so the daemon used free ports instead. That other process was left alone.
- **Isolation.** The data directory, the git repositories and the Automaton worktrees all lived in one scratch directory under the project directory. The LLM, compute node and embedder settings were pointed at the local Ollama only. Webhook, ntfy, DNS, memory and the other optional channels were turned off.
- **Repositories.** Two throwaway git repositories with one base commit each: one whose default branch is `main`, and one whose default branch is `trunk`. Neither has a remote. The daemon's `autonomous.worktree_base_repo` pointed at one of them at a time.
- **Members.** Each member Automaton was created through the API, decomposed with `qwen3:8b`, given a single placeholder story, put in guided mode so its task waits for approval and no worker is started, then approved. The plan-coverage gate needed an explicit acknowledgement for the placeholder plan, which the API supports with an actor and a note. Running such a member leaves it `running` until it is cancelled, which is what the scenarios need.
- **Chains.** Each scenario used a two-member Automation (`m1` leads to `m2` on `on_complete`). The first member's branch (`automaton/<id>`) was created by the daemon's own worktree setup. The "work" was one commit made in that worktree, and "merged" was done in the throwaway repository with plain git. The cancel was the daemon's own `POST /api/autonomous/prds/{id}/cancel`.
- **Policy.** `completed` (the default) for every scenario except scenario 3. It was changed and read back through `PUT /api/config` and `GET /api/config`. A bad value (`sometimes`) was refused with a message naming the three valid values and left the policy as it was.

## Scenario 1: merged then cancelled (the chain advances)

Two landings were tried, because they gave different answers.

**1a. Landed with `git merge --no-ff` (the style `scripts/merge-branches-bump-version.sh` uses).** Observed: **the Automation blocked.**

- The member's branch had one commit and the base repository then merged it with `git merge --no-ff`.
- Git facts afterwards: `main..branch` has 0 commits, `git cherry` prints nothing, and the merge base of `main` and the branch equals the branch tip.
- After the member was cancelled the Automation was `blocked`, with member `a1` `blocked` / outcome `cancelled`, and `a2` never started (its Automaton stayed `approved`).
- The decision log holds only `activate` and `blocked` ("member cancelled outside the Automation"). There is **no** `merged_member` entry, and `GET /api/automations/{id}/merged-members` returned `count: 0`.
- This is the behaviour the how-to describes for a branch with no commits ahead of the default branch, but it means a branch landed by a normal merge commit is never recognised. See finding F1.

**1b. Landed by cherry-pick (same patch and subject on `main`, branch tip not an ancestor of `main`).** Observed: **the chain advanced.**

- Git facts before the cancel: `main..branch` has 1 commit, `git cherry` prints `-` for it (patch already on `main`), and the branch tip is not an ancestor of `main`.
- After the member was cancelled the Automation stayed `active`, member `e1` became `completed` with outcome `completed` and edge `on_complete`, member `e2` became `running`, and the Automation's current member was `e2`.
- The decision log read, in order: `activate`, `merged_member`, `advance`. The `merged_member` entry names the member, the branch and the proving commit: *member Live-Check Member E1 Open: branch automaton/9d700001 is merged on main (commit 19ccaa9d6aab09e1e084ec560ae863e8fb0560f5); policy completed treats the cancelled member as completed*. The `advance` entry reads *E1 -> E2 via on_complete (member completed)*.
- `GET /api/automations/{id}/merged-members` returned one record: default branch `main`, proving commit `19ccaa9d…560ae863e8fb0560f5`, policy `completed`, outcome `advanced`.
- The CLI (`datawatch autonomous merged-members`) printed the same decision, one line per Automation, and the alert list held one info alert, *Automaton advanced on a merged member*, naming the Automaton, the member, the branch and the proving commit.

The same advance was seen three more times in the other scenarios (scenario 2's follow-up, scenario 5), always with a cherry-picked landing.

## Scenario 2: cancelled unmerged (the Automation blocks with the reason)

Observed: **the Automation blocked, with a reason, and nothing was recorded as a merge.**

- The member's branch had one commit that was never landed: `main..branch` 1, `git cherry` printed `+` for it.
- After the cancel the Automation was `blocked`, current member still `b1`, member `b1` `blocked` / outcome `cancelled`, and `b2` stayed `approved` and unstarted.
- The decision log held `activate` and `blocked`; the block reason was *member cancelled outside the Automation*. An alert *Automation blocked* carrying the same text was raised.
- No `merged_member` decision was written, and the merged-members route returned `count: 0`.

## Scenario 3: policy manual (blocks, with the proof in the reason)

Observed: **the Automation blocked, and the merge proof was carried in the block reason.**

- Policy was set to `manual` through `PUT /api/config` and read back as `manual`. The member's work was landed by cherry-pick, so the proof held.
- After the cancel the Automation was `blocked`, current member `c1`, member `c1` `blocked` / outcome `cancelled`, and `c2` stayed `approved`.
- The decision log held `activate`, `merged_member` and `blocked`. The `merged_member` note ended *policy manual keeps the Automation blocked for the operator to resume or skip the member*. The `blocked` reason read *member cancelled outside the Automation; member Live-Check Member C1 Maintenance: branch automaton/5e250001 is merged on main (commit 5581168f3e552880b382b410a9a981c53ac126ea)*, so the operator can see the work is already on the default branch.
- The merged-members route returned policy `manual`, outcome `blocked`, proving commit `5581168f…ac126ea`. The alert raised was the ordinary *Automation blocked* warning; no extra info alert was added.
- The policy was set back to `completed` afterwards. The `block` policy (no git check at all) was not exercised in this check.

## Scenario 4: reopened member (the chain resumes)

Observed: **running the cancelled member again put the Automation back to `active` on its own, and the resumed chain went on to advance.**

- Starting state: the blocked Automation from scenario 2, blocked on `b1` because it was cancelled.
- To run the member again, a second task was added to the cancelled Automaton (allowed in the cancelled state), and `POST /api/autonomous/prds/{id}/reset_task` with `force: true` was sent for it. This sets the Automaton's status back to `running`.
- Within 2 seconds, and unchanged 10 seconds later: the Automaton was `running`, the Automation was `active` with current member `b1`, member `b1` was `running`, and `b2` was still `approved`. The decision log gained *reopened*: *Automaton reopened: member Live-Check Member B1 Accessibility was run again after it was cancelled; the chain resumed*. The operator did not call resume.
- Follow-up 1: the member's branch now carried an extra commit, the daemon's own *snapshot before task* commit made when the task was added. With only the first commit landed the member was cancelled and the Automation blocked again with no merge record, which is correct because that snapshot commit was not on `main`. This was caused by the test setup, not by a fault.
- Follow-up 2: the member was reopened a second time (a second *reopened* entry), every remaining commit was landed by cherry-pick, and the member was cancelled again. The Automation then recorded `merged_member` (proving commit `d7a0a9db…678795d6dbab7248`), then `advance` to `b2`. The Automation stayed `active`, `b1` was `completed`, `b2` was `running`.
- Each reopen also made the daemon start a worker session for the reopened member's pending task, so the member really ran again. Those sessions were ended by the daemon on the next cancel.

## Scenario 5: default branch is not main (advances)

Observed: **the chain advanced, and the decision named `trunk`.**

- The base repository's default branch is `trunk`; it has no `main` branch and no remote, so `refs/remotes/origin/HEAD` does not exist. The proof therefore used the "branch currently checked out" fallback.
- The daemon's own worktree setup could not create the member branches for this repository until each Automaton named `trunk` as its base branch (`POST /api/autonomous/prds/{id}/set_git` with `base_branch`). See finding F2.
- The member's work was landed on `trunk` by cherry-pick (`git cherry` printed `-`, branch tip not an ancestor of `trunk`). After the cancel the Automation stayed `active`, `d1` was `completed` via `on_complete`, and `d2` was `running`.
- The decision log read *member Live-Check Member D1 Open: branch automaton/f06b0001 is merged on trunk (commit 71297c6c911e1dde80912650f118e3c86d254bdb); policy completed treats the cancelled member as completed*. The merged-members route returned default branch `trunk`, outcome `advanced`. The info alert text said *already merged on trunk*.

## Summary of results

| # | Scenario | Landing | Observed |
|---|---|---|---|
| 1a | merged then cancelled | `git merge --no-ff` | Blocked, no `merged_member` record (finding F1) |
| 1b | merged then cancelled | cherry-pick | Advanced to the next member; decision log names member, branch and commit |
| 2 | cancelled unmerged | none | Blocked with the reason; nothing recorded as merged |
| 3 | policy manual | cherry-pick | Blocked; proof carried in the block reason |
| 4 | reopened member | n/a, then cherry-pick | Resumed to `active` by itself; later advanced |
| 5 | default branch `trunk` | cherry-pick | Advanced; decision names `trunk` |

All five behaviours match the how-to when the branch is landed in a way the proof recognises. The one gap is a normal merge commit (F1).

## Findings outside the five scenarios

These are observations only. Nothing was changed in the code; they are recorded for the operator to decide on.

**F1. A branch landed with a merge commit or a fast-forward is never proved merged.**
`decideMergeState` in `internal/autonomous/merge_compare.go` (line 128) returns `empty` whenever the branch has 0 commits ahead of the default branch, and `MergedMemberProof` in `internal/autonomous/automation_merged_member.go` (line 88) treats anything other than `merged` as not merged. A branch whose tip is already on the default branch always has 0 commits ahead, so it is reported `empty` and the Automation blocks as before. Only a landing that leaves the tip off the default branch (a cherry-pick or rebase carrying the same patch and subject) is recognised. The case in `mergedProofCommit` that returns the branch tip when "the merge base equals the tip" (line 117) cannot be reached for that reason. The project's own merge helper, `scripts/merge-branches-bump-version.sh` (line 117), lands branches with `git merge --no-ff`, so branches merged by that helper will not advance an Automation under the current rule. The how-to documents the "no commits ahead" rule as a deliberate safe default, so this may be intended; the live check only shows how far it reaches. The unit tests use a fake git runner, which is why they did not show it.

**F2. A remote-less repository whose default branch is not `main` cannot get Automaton worktrees without a per-Automaton base branch.**
`defaultBranchOf` in `internal/autonomous/worktree.go` (line 62) falls back to the literal `main` when `refs/remotes/origin/HEAD` is missing, and worktree creation (line 102) uses that unless the Automaton's own `git.base_branch` is set. With no `main` branch the decompose step failed with *invalid reference: main*. Setting `base_branch` through `set_git` avoided it. The merge proof itself does not share this fallback; it asks git for the checked-out branch.

**F3. Edits to a cancelled Automaton add commits to its branch.**
Adding a task to a cancelled Automaton made the daemon commit a *snapshot before task* on the member's branch. A branch with such a commit is not merged until that commit is landed too, so a proof taken after the edit correctly said *pending*. This is expected behaviour, noted because it made the first reopen follow-up block again.

## Cleanup

- The sandbox daemon was ended by its recorded PID through `scripts/test-daemon-lib.sh`, which ended any session it had started first.
- The scratch directory (daemon data, the two repositories, the worktrees, the helper scripts and logs) was removed. Only this report was added to the tree.
- `bash scripts/check-no-leftover-test-daemons.sh` was run last to confirm that no test daemon was left running. Its result is recorded below.

### Leftover-daemon check

- **Raw run.** `bash scripts/check-no-leftover-test-daemons.sh check <empty baseline>` exited 1 and listed 12 live test daemons. None of them is this check's: the two daemons this check started (the first was restarted once to fix a config mistake) were both gone, their PIDs are not in the list, and their config was not under a temporary location. The 12 are between 2 and 7 days old and belong to other sessions, so they were left alone. The script says as much in its own note about an empty baseline on a shared host.
- **Diff run.** A snapshot of those 12 was taken (neither of this check's PIDs is in it), and `check` against it printed *OK: no leftover test daemons* and exited 0. This check left no daemon, session, port or process behind.
- **Operator note.** The 12 older test daemons are a separate clean-up item for whoever owns those sessions; this check did not touch them.
