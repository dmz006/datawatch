---
docs:
  index: true
  topics: [automaton, automation, child, parent, inherit, inheritance, project, repository, model, backend, verification, tags, priority, clamp, child_inherit]
exec_steps:
  - tool: autonomous_config_get
    description: Read the autonomous settings, including automation.child_inherit (default on)
    args: {}
    read_only: true
---

# How-to: A child Automaton starts from its parent's setup

## 1. The problem this solves

An Automaton can have a parent: a child Automaton that a running Automaton spawns for a task, a fix Automaton filed from a scan, or a defect filed against a parent. A child used to start from nothing. It had no project link, no Automation, no backend or model and no tags, so the operator had to set all of that by hand, and the child could run outside the chain its parent belonged to.

With `autonomous.automation.child_inherit` on, a child starts from the parent's setup at the moment it is created.

## 2. What a child inherits

Each item is copied only where the caller of the creation gave no value of its own. An explicit value always wins.

| Item | What happens |
|---|---|
| Project link and project directory | Copied. The directory is the project's repository, never a git worktree path, even when the parent runs in a worktree. If the repository cannot be resolved the directory is skipped and the child's record says so. |
| The parent's Automation | The child joins it as a member right after the parent, through the normal member insert. A held insert and `agent_insert_policy` apply as for any insert. It is skipped quietly when the parent has no Automation, or the Automation is completed or cancelled. |
| Backend and model | Copied as a pair. A model never travels without its backend, so a child with its own backend keeps its own model choice. |
| Verification backend and model | Copied the same way as the backend and model pair. |
| Tags | The parent's tags are added to the child's own tags. |
| Priority | Copied together with where the priority came from. |

**Model tier rule.** A child of a Sonnet or Haiku parent never ends up on an Opus or Fable model through inheritance. The pair is clamped to the parent's entry, and the clamp is noted in the child's record. A value the caller gave explicitly is never changed.

## 3. Once per child, and never a failure

Inheritance runs once, when the child is created. The child gets one `child_inherit` entry in its decisions: the parent, what was inherited, what was clamped and what was skipped. A second pass (for example defect intake after creation) changes nothing. Inheritance never fails a creation; whatever could not be inherited is listed as skipped.

A defect filed with a parent is not offered to the `defect_intake_automation` Automation when it already joined its parent's Automation.

## Creating a child from any surface

Every creation surface takes an optional parent Automaton id and the same inheritance rule applies. An explicit project directory, backend, model or tags always win over the inherited values. A parent id stands in for the project directory, and an unknown parent is refused before anything is created.

| Surface | How to give the parent |
|---|---|
| REST | `POST /api/autonomous/prds` with `parent_prd_id` in the body; an unknown parent is a 404 |
| MCP | `autonomous_prd_create` with the optional `parent_prd_id` argument |
| CLI | `datawatch autonomous prd-create --parent <id> "<spec>"` |
| Comm channel | `autonomous create <spec> --parent <id>` |
| PWA | The *Parent Automaton (optional)* field on the new Automaton form |

## 4. The setting

`autonomous.automation.child_inherit` is `true` or `false` and is **on by default**. An unset key reads `true`, so an existing config file gets the new behavior. `false` restores the earlier behavior: nothing is copied and nothing is recorded. Any other value is refused with 400.

```bash
datawatch config set autonomous.automation.child_inherit false
```

`PUT /api/config` with the key, and comm `configure autonomous.automation.child_inherit=false`, write the same key. In the PWA, use Settings, Automata, *Child Automata inherit from their parent*. It is read at the time each child is created, so a change applies to the next child with no restart. Children that already exist are not changed.

## 5. Check what a child inherited

Read the child Automaton and look for the `child_inherit` decision: REST `GET /api/autonomous/prds/<id>`, MCP `autonomous_prd_get`, or the decisions list in the PWA detail view. `GET /api/config` shows the value in effect under `autonomous.automation.child_inherit`. With the setting off, no entry is written.

See also [automation-chains.md](automation-chains.md), [automaton-pending-items.md](automaton-pending-items.md) and, for the held-insert policy, [automation-queue-advance.md](automation-queue-advance.md).
