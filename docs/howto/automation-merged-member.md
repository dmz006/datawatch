---
docs:
  index: true
  topics: [automation, automaton, merged member, cancelled member, merge proof, default branch, policy, blocked, reopen]
exec_params:
  - {name: automation_id, required: false, description: "Automation ID to read the merged-member decisions of (leave out for every Automation)"}
  - {name: policy, required: true, description: "Merged-member policy to set: completed, manual or block"}
exec_steps:
  - tool: autonomous_config_get
    description: Read the autonomous settings, including the merged-member policy in force
    args: {}
    read_only: true
  - tool: autonomous_config_set
    description: Set the merged-member policy (completed, manual or block); it applies to the next member that ends
    args:
      automation_merged_member_policy: "{{params.policy}}"
    read_only: false
  - tool: automation_merged_members
    description: Read the merged-member decisions, oldest first (one Automation when an ID is given, else every Automation)
    args: {id: "{{params.automation_id}}"}
    read_only: true
  - tool: automation_get
    description: Read one Automation (status, members, edges, decision log)
    args: {id: "{{params.automation_id}}"}
    read_only: true
---

# How-to: A member Automaton cancelled after its work shipped

## 1. The problem this solves

An **Automation** runs a chain of **Automata**, one member at a time. When a
member ends cancelled, the Automation has no `on_complete` edge to follow, so
it becomes `blocked` and waits for you.

That is the right answer when the member's work never landed. It is the wrong
answer when the work did land: the member's branch (`automaton/<id>`) was
already merged into the project's default branch and only the Automaton
itself was cancelled afterwards, for example by hand or by a clean-up. The
Automation then sits blocked on work that is finished.

The daemon now checks for that case. When a member ends **cancelled or
completed**, it asks git whether the member's branch is already merged on the
project's default branch. If it is, one setting decides what the Automation
does. If it is not, or if git cannot tell, nothing changes: the Automation
blocks exactly as it always did.

Base requirements: a daemon with `autonomous.enabled: true`, an Automation
that is active (see [automation-chains.md](automation-chains.md)), and
`worktree_base_repo` set to the repository the member branches live in.
Without that repository the proof cannot be made and every cancelled member
keeps the blocked outcome.

## 2. The policy

The setting is `autonomous.automation.merged_member_policy`:

| Value | What happens when a member's branch is proved merged |
|---|---|
| `completed` (default) | The member is treated as completed and the Automation follows its `on_complete` edge. One info alert is raised |
| `manual` | The Automation stays blocked, as before, but the block reason now carries the merge proof, so you can see that the work is on the default branch and choose to resume or skip the member |
| `block` | The old behaviour, exactly: the Automation stays blocked and no git check runs at all |

A key that is absent reads as `completed`. A change applies to the next
member that ends; there is no restart and a member that already ended is not
looked at again.

```bash
datawatch config set autonomous.automation.merged_member_policy manual
datawatch config get autonomous.automation.merged_member_policy
```

MCP: `autonomous_config_set` with `automation_merged_member_policy`. REST:
`PUT /api/config` with the key `autonomous.automation.merged_member_policy`;
`GET /api/config` shows the effective value. Chat: `configure
autonomous.automation.merged_member_policy=manual`. The PWA has the same
select under Settings, General, Automate (**Automaton merged-member policy**).

Anything other than the three words is refused, and so is an empty value
sent over a write surface (HTTP 400, nothing is written).

## 3. How the merge is proved

The check only reads; it never merges, deletes, pushes or checks out a
branch.

1. **The default branch is the project's own.** It is not assumed to be
   `main`. The daemon reads the symbolic ref of the base repository's remote
   `HEAD` (`refs/remotes/origin/HEAD`). If that does not answer, it uses the
   branch the base repository currently has checked out. A project whose
   default branch is `trunk` or `release/1.0` is proved against that branch.
   If neither answers, the proof is unknown and the Automation blocks.
2. **The branch is compared with it.** The comparison is the one the
   merge-pending check uses: every patch on the branch must already be on the
   default branch, and every commit subject must match. See
   [the merge-pending flow](../flow/automation-merge-pending-flow.md).
3. **A proving commit is named.** That is the branch tip when the tip is on
   the default branch, otherwise the default-branch commit that carries the
   same patch as the tip. If no such commit can be found, the member is not
   called merged.

Three results keep the blocked outcome whatever the policy says: the branch is
**not merged**, its state is **unknown** (no repository, no branch, a git
error), or it has **no commits ahead** of the default branch. An empty branch
cannot be told apart from one that never had any work, so it is not taken as
proof. When in doubt the daemon blocks and asks you.

## 4. What is recorded

Every time the proof took part in a member's end, the Automation's decision
log gets one `merged_member` entry and the audit trail gets one
`automation_merged_member` entry. Each names:

- the member and its work record,
- the branch,
- the default branch it was proved merged into,
- the proving commit,
- the policy in force,
- the outcome: **advanced** (the Automation moved on past the member) or
  **blocked** (it stayed blocked for you).

Under `completed`, a member that was cancelled and proved merged gives
`advanced` and raises one info alert, **Automaton advanced on a merged
member**, naming the Automaton, the member, the branch and the proving commit.
Under `manual` the outcome is `blocked`, and the usual blocked alert is raised
as for any blocked Automation; no extra alert is added. A member that simply
completed is recorded with its proof too, but raises no alert, because
nothing unusual happened.

## 5. Where the decision shows

| Surface | How to read it |
|---|---|
| PWA | The Automation detail view has a block titled **Automaton merged-member decisions**, newest first: member, branch, the first 12 characters of the proving commit, policy, outcome and time. With no decisions the block is not drawn. The decision log shows each entry as one line: *Automaton member `x` on `main`, proved merged at `abc123def456`: advanced* |
| CLI | `datawatch autonomous merged-members [automation-id]` prints one line per decision, newest first. Add `--json` for the raw body |
| Chat | `autonomous merged-members [automation-id]`, newest first, at most ten lines |
| MCP | `automation_merged_members` with an optional `id` |
| REST | `GET /api/automations/merged-members` for every Automation, `GET /api/automations/{id}/merged-members` for one (oldest first). An unknown ID is a 404; an Automation with no decision returns an empty list |
| Alert | The info alert above, through the usual alert sink |

All of them are read-only. A read changes nothing, merges nothing and resumes
nothing.

```bash
datawatch autonomous merged-members                 # every Automation
datawatch autonomous merged-members <automation-id> # one Automation
```

## 6. Reopen a member

If you cancelled a member and then decide it should run after all, run the
Automaton again. When its status goes back to `running` and the Automation is
blocked on exactly that member because it was cancelled, the Automation
resumes by itself: the member goes back to `running`, the Automation goes
back to `active`, and a `reopened` entry is written to the decision log. You do not
need to resume the Automation by hand.

Nothing is revived that should stay as it is. A reopened member does not
restart an Automation that is cancelled, completed or paused, or one that is
blocked for any other reason (a failure, for example). The same run-again does
nothing a second time if the event repeats.

Under `manual`, two choices are open to you from the blocked Automation:
resume it, or skip the member and name the edge to follow (see
[automation-chains.md](automation-chains.md), section 6). Skipping with
`on_complete` is the way to say "this member's work is done, carry on".

```bash
datawatch automation resume <automation-id>
datawatch automation skip <automation-id> <member> --edge on_complete --reason "work already on the default branch"
```

## 7. Choosing a policy

- **`completed`** suits a daemon where the member branches are merged by a
  person or a release step and a cancelled member nearly always means "shipped
  and cleaned up". It stops a finished chain from stalling.
- **`manual`** suits a cautious start: you still decide, but the block reason
  already tells you the work is on the default branch.
- **`block`** suits a project that wants every cancellation to stop the chain
  and does not want git run for it.

To see how it behaves on your own branches, set `manual` first, let a few
members end, and read the decisions with `datawatch autonomous merged-members`
before moving to `completed`.

## Related

- [automation-chains.md](automation-chains.md): edges, control operations and the Automation tab
- [automation-queue-advance.md](automation-queue-advance.md): how the hand-off and restart recovery work
- [../flow/automation-merged-member-flow.md](../flow/automation-merged-member-flow.md): the decision as a diagram
- [../flow/automation-merge-pending-flow.md](../flow/automation-merge-pending-flow.md): the branch comparison this check reuses
