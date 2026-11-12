# Automation merged-member flow

How the coordinator decides what to do when a member Automaton of an
Automation ends cancelled or completed: it asks read-only git whether the
member's branch is already merged on the project's own default branch, and the
`autonomous.automation.merged_member_policy` key then decides whether the
Automation moves on or stays blocked. Nothing in this flow merges, deletes,
pushes or checks out a branch.

## The member-end decision

The check runs when a member ends. A member that ends any other way (failed,
for example) is not looked at, and a fork branch carries no outcome edge, so
only a non-branch member can be blocked by this path.

```mermaid
flowchart TD
    E[member Automaton ends cancelled or completed] --> P{merged_member_policy}
    P -- block --> OLD[old behaviour: no git runs,<br/>cancelled member blocks the Automation]
    P -- completed or manual --> B{member has a work branch?}
    B -- no --> OLD
    B -- yes --> DB[resolve the default branch for this project:<br/>origin/HEAD symbolic ref,<br/>else the base repository's checked-out branch]
    DB --> CMP[compare branch with the default branch:<br/>git cherry plus commit subjects]
    CMP --> M{merged, and a proving commit found?}
    M -- "no: not merged, empty or unknown" --> OLD
    M -- yes --> REC[record one merged_member decision<br/>and one audit entry]
    REC --> C{member status}
    C -- completed --> ADV0[outcome advanced: follow on_complete<br/>no alert]
    C -- cancelled --> PL{policy}
    PL -- completed --> ADV[outcome advanced: treat as completed,<br/>follow on_complete, one info alert]
    PL -- manual --> BLK[outcome blocked: Automation stays blocked,<br/>block reason carries the proof,<br/>usual blocked alert]
```

## What the proof is

```mermaid
flowchart LR
    R[base repository] --> S[symbolic-ref origin/HEAD]
    S -- no answer --> H[rev-parse --abbrev-ref HEAD]
    S --> D[default branch]
    H --> D
    D --> V[rev-parse --verify the member branch]
    V --> K[CompareBranchToMain]
    K --> T{merge-base equals the branch tip?}
    T -- yes --> TIP[proving commit is the branch tip]
    T -- no --> PM[find the default-branch commit<br/>whose patch equals the tip patch]
    PM --> PC[proving commit is that commit]
```

Every git command in the proof is a read (`symbolic-ref`, `rev-parse`,
`rev-list`, `cherry`, `log`, `diff-tree`, `merge-base`). Any git error, a
missing repository or branch, or a default branch git cannot name gives "not
merged" with the reason kept, so the Automation keeps its blocked outcome.

## The policy at a glance

| Policy | Branch not merged or unknown | Branch proved merged, member cancelled | Branch proved merged, member completed |
|---|---|---|---|
| `completed` (default) | Blocked | Advanced, one info alert | Advanced, no alert |
| `manual` | Blocked | Blocked, block reason carries the proof | Advanced, no alert |
| `block` | Blocked, no git run | Blocked, no git run | Follows the member's normal outcome, no git run |

## Reopening a member

```mermaid
flowchart LR
    X[member Automaton goes back to running] --> Q{Automation blocked on this member<br/>because it was cancelled?}
    Q -- no --> N[nothing changes]
    Q -- yes --> U[member running, Automation active,<br/>reopened decision recorded]
```

A reopened member never revives an Automation that is cancelled, completed,
paused or blocked for another reason, and a repeated running event does
nothing a second time.

## Reading the result

| Surface | How |
|---|---|
| REST | `GET /api/automations/merged-members`, `GET /api/automations/{id}/merged-members` |
| MCP | `automation_merged_members` |
| CLI | `datawatch autonomous merged-members [automation-id]` |
| Comm channel | `autonomous merged-members [automation-id]` |
| PWA | **Automaton merged-member decisions** block on the Automation detail view, and the decision log line |

For the operator guide see
[howto/automation-merged-member.md](../howto/automation-merged-member.md); for
the comparison this flow reuses see
[automation-merge-pending-flow.md](automation-merge-pending-flow.md).
