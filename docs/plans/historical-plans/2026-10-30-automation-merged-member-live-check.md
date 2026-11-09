# Automation merged-member live check

Date: 2026-10-30. Build under test: the working tree of branch `automaton/46df0001` (daemon version 9.42.2).

This is the record of a live check of the merged-member behaviour described in [the how-to](../howto/automation-merged-member.md) and [the flow](../flow/automation-merged-member-flow.md). It ran against a sandbox daemon, never the production daemon. Every result below was read back from that daemon's REST routes, CLI and alert list. Nothing here was inferred from the unit tests.

This is the second pass of the check. The first pass left three gaps that this pass closes: the default-branch scenario needed a per-Automaton base-branch override, scenario 1 was shown only with a cherry-pick landing, and the leftover-daemon confirmation was a diff against a snapshot taken late. What could not be closed is listed under [Deviations from the task text](#deviations-from-the-task-text).

## Setup

- **Daemon.** A binary built from this tree, started on a config generated from `testdata/datawatch.yaml` with the same `sed` substitutions `scripts/run-tests.sh` applies (data directory, ports, loopback bind, test token). Optional channels (webhook, ntfy, DNS, memory, MCP, council, summarizer, plugins, web search) were turned off, and the worktree directory was pointed at the scratch directory. It was started through `td_start` in `scripts/test-daemon-lib.sh` with `env -u TMUX -u TMUX_TMPDIR`, so its PID was recorded, and it was ended by that recorded PID through `td_stop_all`. No tmux server command, no `pkill` and no kill by name was used.
- **Ports.** The sandbox ports named in `AGENT.md` (18080 HTTP, 18443 TLS) were not free: a Docker container from another worktree's test compose project publishes both. The check left it alone and used three free ports, which is what `_fresh_port` in `scripts/run-tests.sh` does for the same reason. See the deviations section.
- **Isolation.** The data directory, both git repositories, the Automaton worktrees and the helper scripts all lived in one scratch directory under the project directory, removed at the end.
- **Repositories.** Two throwaway repositories, each a clone of its own local bare "remote" whose HEAD names the default branch, with one base commit each. One default branch is `main`, the other is `trunk`. Because they are clones, `refs/remotes/origin/HEAD` exists in both, so the daemon resolves the default branch the normal way. Nothing was ever pushed after the base commit. The daemon's `autonomous.worktree_base_repo` pointed at the `main` repository for scenarios 1 to 4 and was switched to the `trunk` repository (through `PUT /api/config`) for scenario 5.
- **Members.** Each member Automaton was created through the API and decomposed with `qwen3:8b` on the local Ollama. The decompose step rejects a plan whose tasks plan files without a verification script, so the spec asked for a pure research question with no planned files; two earlier plans were rejected by that gate and thrown away. The member was then put in guided mode (its task waits for approval, so no worker starts), the plan-coverage gate was acknowledged with an actor and a note, and the member was approved. Such a member stays `running` inside its Automation until it is cancelled, which is what the scenarios need.
- **Chains.** Every Automation was a chain of two or three members joined by `on_complete`, created through `POST /api/automations` and started with `/activate`. The member's branch (`automaton/<id>`) and worktree were made by the daemon's own worktree setup, with no base-branch override on any Automaton. The "work" was one commit made in that worktree. "Landed" was done in the throwaway repository with plain git. The cancel was the daemon's own `POST /api/autonomous/prds/{id}/cancel`.
- **Policy.** `completed` (the default) for every scenario except scenario 3. It was changed and read back through `PUT /api/config` and `GET /api/config`. A bad value (`sometimes`) was refused with a message naming the three valid values.

## Scenario 1: merged then cancelled (the chain advances)

Four landings of a one-commit branch were tried, because they gave different answers. Git facts are read from the base repository right after landing.

| Landing | Git facts after landing | Observed |
|---|---|---|
| Squash merge, commit message reused from the branch (`git merge --squash`, then `git commit -C <branch>`) | 1 commit ahead, `git cherry` prints `-`, tip not an ancestor of `main` | **Advanced** |
| Cherry-pick | 1 commit ahead, `git cherry` prints `-`, tip not an ancestor of `main` | **Advanced** |
| Squash merge with an edited commit message | 1 commit ahead, `git cherry` prints `-`, subject differs | **Blocked**, nothing recorded |
| `git merge --no-ff` | 0 commits ahead, tip is an ancestor of `main` | **Blocked**, nothing recorded |

**Advance (squash with reused message, and cherry-pick).** After the member was cancelled, the Automation stayed `active`, the member became `completed` with outcome `completed` and edge `on_complete`, and the next member became `running`. The decision log read, in order: `activate`, `merged_member`, `advance`. The `merged_member` entry names the member, the branch and the proving commit, for example *member Two Plus Two: branch automaton/abcf0001 is merged on main (commit 24399a085018b5047137490281ff081e85a1e4f2); policy completed treats the cancelled member as completed*, and the `advance` entry reads *Two Plus Two -> Two Plus Two via on_complete (member completed)*. `GET /api/automations/{id}/merged-members` returned one record with the default branch (`main`), the proving commit, policy `completed` and outcome `advanced`. The CLI (`datawatch autonomous merged-members`) printed one line per decision with the same fields, and the alert list held one info alert, *Automaton advanced on a merged member*, naming the Automaton, the member, the branch and the proving commit, ending with the command to read the decisions.

**Blocked (squash with an edited message).** The patch was on `main` (`git cherry` printed `-`) but the squash commit's subject was the branch subject plus a suffix. After the cancel the Automation was `blocked` with *member cancelled outside the Automation*, no `merged_member` entry, and `count: 0` from the merged-members route. This is the subject rule in the how-to ("every commit subject must match"). See finding F2.

**Blocked (`git merge --no-ff`).** After the cancel the Automation was `blocked`, the member `blocked` / outcome `cancelled`, the next member never started, the decision log held only `activate` and `blocked`, and the merged-members route returned `count: 0`. See finding F1.

## Scenario 2: cancelled unmerged (the Automation blocks with the reason)

Observed: **the Automation blocked, with a reason, and nothing was recorded as a merge.**

- The member's branch had one commit that was never landed: 1 commit ahead of `main`, `git cherry` printed `+`.
- After the cancel the Automation was `blocked`, current member still the first one, that member `blocked` / outcome `cancelled`, and the second member never started.
- The decision log held `activate` and `blocked`; the block reason was *member cancelled outside the Automation*. A warning alert *Automation blocked* carried the same text.
- No `merged_member` decision was written, and the merged-members route returned `count: 0`.

## Scenario 3: policy manual (blocks, with the proof in the reason)

Observed: **the Automation blocked, and the merge proof was carried in the block reason.**

- Policy was set to `manual` through `PUT /api/config` and read back as `manual`. The member's work was landed by cherry-pick, so the proof held.
- After the cancel the Automation was `blocked`, the member `blocked` / outcome `cancelled`, and the second member never started.
- The decision log held `activate`, `merged_member` and `blocked`. The `merged_member` note ended *policy manual keeps the Automation blocked for the operator to resume or skip the member*. The `blocked` reason read *member cancelled outside the Automation; member Two Plus Two: branch automaton/b7180001 is merged on main (commit 75d5d2be8601961e96c69cf111d841bee8022c74)*, so the operator can see the work is already on the default branch.
- The merged-members route returned policy `manual`, outcome `blocked` and the proving commit. The alert raised was the ordinary *Automation blocked* warning carrying the proof; no extra info alert was added.
- The policy was set back to `completed` afterwards. The `block` policy (no git check at all) was not exercised in this check.

## Scenario 4: reopened member (the chain resumes)

Observed: **running the cancelled member again put the Automation back to `active` on its own, and the resumed chain went on to advance.**

- Starting state: the blocked Automation from scenario 2, blocked on its first member because that member was cancelled.
- To run the member again, a second task was added to the cancelled Automaton (structural edits are allowed in the cancelled state) and `POST /api/autonomous/prds/{id}/reset_task` with `force: true` was sent for it.
- Within 3 seconds, and unchanged 10 seconds later: the Automaton was `running`, the Automation was `active` with the first member as current member and that member `running`, and the second member had not started. The decision log gained *reopened*: *Automaton reopened: member Mathematical Calculation was run again after it was cancelled; the chain resumed*. The operator did not call resume.
- The reopen also made the daemon start a worker session for the pending task, so the member really ran again. That session was ended by the daemon on the next cancel (state `killed`).
- The branch now carried two commits: the original work and the daemon's own *snapshot before task* commit made when the task was added. Both were landed by cherry-pick (`git cherry` printed `-` for both) and the member was cancelled again. The Automation then recorded `merged_member` (proving commit `15ab70d484b84e327d7c1f813c6d007e223ac6db`) and `advance`; it stayed `active`, the first member was `completed` and the second was `running`.

## Scenario 5: default branch is not main (advances)

Observed: **the chain advanced, and the decision named `trunk`, with no manual override of the base branch.**

- The base repository's default branch is `trunk`; it has no `main` branch. `refs/remotes/origin/HEAD` points at `origin/trunk`. The Automaton's `git` block held only the branch name: no `base_branch` was set through `set_git` or anywhere else, and the daemon's own worktree setup created the member branches off `trunk` by itself.
- A first landing attempt did not prove anything. The member's single commit was cherry-picked onto an untouched `trunk`; because the parent, tree, author and timestamp were identical, git produced the same commit, so the branch tip was an ancestor of `trunk` with 0 commits ahead. That is the same shape as a fast-forward, and the Automation blocked with *member cancelled outside the Automation* and no merge record. See finding F1.
- To get a landing the proof can recognise, the member was reopened (a task was added and force-reset, as in scenario 4), an unrelated commit was added to `trunk`, and the branch's one remaining commit (the daemon's snapshot commit) was cherry-picked on top. Then: 1 commit ahead, `git cherry` printed `-`, tip not an ancestor of `trunk`. After the cancel the Automation stayed `active`, the first member was `completed` via `on_complete`, and the second was `running`. The decision log read *activate*, *blocked*, *reopened*, *merged_member*, *advance*.
- The `merged_member` entry read *member Two Plus Two: branch automaton/23920001 is merged on trunk (commit 089ac83f845fae7782e69bb862213bb1508128f8); policy completed treats the cancelled member as completed*. The merged-members route returned default branch `trunk` and outcome `advanced`. The info alert said *already merged on trunk*.

## Summary of results

| # | Scenario | Landing | Observed |
|---|---|---|---|
| 1 | merged then cancelled | squash with reused message | Advanced; decision log names member, branch and commit |
| 1 | merged then cancelled | cherry-pick | Advanced; decision log names member, branch and commit; CLI and info alert agree |
| 1 | merged then cancelled | squash with edited message | Blocked, no `merged_member` record (finding F2) |
| 1 | merged then cancelled | `git merge --no-ff` | Blocked, no `merged_member` record (finding F1) |
| 2 | cancelled unmerged | none | Blocked with the reason; nothing recorded as merged |
| 3 | manual policy | cherry-pick | Blocked; proof carried in the block reason |
| 4 | reopened member | cherry-pick of every branch commit | Resumed to `active` by itself; later advanced |
| 5 | default branch `trunk` | cherry-pick of the remaining commit | Advanced; decision names `trunk`; no base-branch override |

The behaviour matches the how-to whenever the branch is landed in a way the proof recognises. A normal merge commit and a landing that rewrites the commit subject are not recognised.

## Findings outside the five scenarios

These are observations only. Nothing was changed in the code; they are recorded for the operator to decide on.

**F1. A branch landed with a merge commit or a fast-forward is never proved merged.**
`decideMergeState` in `internal/autonomous/merge_compare.go` (line 126) returns `empty` whenever the branch has 0 commits ahead of the default branch, and `MergedMemberProof` in `internal/autonomous/automation_merged_member.go` (line 88) treats anything other than `merged` as not merged. A branch whose tip is already on the default branch always has 0 commits ahead, so it is reported `empty` and the Automation blocks as before. Only a landing that leaves the tip off the default branch (a squash, a cherry-pick or a rebase that carries the same patch and subject) is recognised. The case in `mergedProofCommit` (line 112) that returns the branch tip when "the merge base equals the tip" cannot be reached for that reason. The project's own merge helper, `scripts/merge-branches-bump-version.sh` (line 117), lands branches with `git merge --no-ff`, so branches merged by that helper will not advance an Automation under the current rule. The same shape appears when a cherry-pick reproduces the identical commit (scenario 5, first attempt). The how-to documents the "no commits ahead" rule as a deliberate safe default, so this may be intended; the live check only shows how far it reaches. The unit tests use a fake git runner, which is why they did not show it.

**F2. A landing that edits the commit subject is not recognised.**
The comparison (`CompareBranchToMain`, subject check in `internal/autonomous/merge_compare.go`) requires every branch commit subject to appear on the default branch. A squash merge whose message was edited (for example a suffix added by a hosting service) has the same patch (`git cherry` prints `-`) but a different subject, and the member stays blocked. The how-to states the subject rule, so this is expected behaviour, noted because squash is a common landing style.

**F3. Edits to a cancelled Automaton add commits to its branch.**
Adding a task to a cancelled Automaton makes the daemon commit a *snapshot before task* on the member's branch. That commit is not on the default branch until it is landed too, so a proof taken after a reopen correctly says *pending* until every commit is landed. This is expected behaviour, noted because it decides what a reopened member needs before it can advance.

**F4. Not re-run in this pass: a repository with no remote HEAD.**
`defaultBranchOf` in `internal/autonomous/worktree.go` (line 53) falls back to the literal `main` when `refs/remotes/origin/HEAD` is missing. In the first pass, a `trunk` repository with no remote could not get Automaton worktrees until each Automaton named `trunk` as its base branch (the decompose step failed with *invalid reference: main*). This pass used repositories that do have a remote HEAD, so the fallback was not exercised and the earlier observation was not reproduced. The merge proof itself does not share that fallback; it asks git for the checked-out branch.

## Deviations from the task text

- **Sandbox ports.** The task and `AGENT.md` name 18443 and 18080. Both are held by a Docker container that belongs to another worktree's test compose project and had been up for days. Stopping it would have broken someone else's work, binding to 127.0.0.1 only does not get around a wildcard listener, and `scripts/run-tests.sh` itself picks a free port when the requested one is taken (`_fresh_port`, line 629). The daemon used free ports instead; everything else in the sandbox rules was followed.
- **Leftover confirmation.** `bash scripts/check-no-leftover-test-daemons.sh` with no arguments prints its usage and exits 2: the script needs a mode and a file. It was run that way as asked, and then in its two real modes (below).
- **Scenario 1 as worded.** "Merged then cancelled advances" holds for squash and cherry-pick landings and does not hold for a `git merge --no-ff` landing; both are reported above rather than only the one that advanced.

## Cleanup

- The sandbox daemon was ended by its recorded PID through `td_stop_all`, which ends any session the daemon started first. Its PID was gone and its HTTP and TLS ports were free afterwards.
- The scratch directory (daemon data, both repositories and their bare remotes, the worktrees, the helper scripts and logs) was removed. Only this report was added to the tree.

### Leftover-daemon check

- **No arguments.** Usage text, exit 2, as described above.
- **Snapshot, then check.** After the daemon was stopped, `snapshot` recorded the 12 test daemons then alive and `check` against it printed *OK: no leftover test daemons* and exited 0.
- **Check against an empty baseline.** This exited 1 and listed those same 12. None of them belongs to this check: this check's daemon was already gone, and its configuration lived under the project directory rather than a temporary location, which the script does not classify as a test daemon in the first place. The 12 are between 2 and 7 days old and belong to other sessions or runs on this shared host, so they were left alone. They are a separate clean-up item for whoever owns them.
- **Result.** This check left no daemon, session, port or process behind. It cannot truthfully report "no test daemon is running on the host", because 12 older ones are.
