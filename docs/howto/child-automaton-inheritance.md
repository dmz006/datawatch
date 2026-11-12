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

# How-to: What a child Automaton inherits from its parent

A child Automaton is one that a running Automaton spawns for a task, a fix Automaton filed from a scan, or a defect filed against a parent. With `autonomous.automation.child_inherit` on (the default), a child starts from its parent's setup when it is created, so you do not set it all again by hand.

## 1. What a child inherits

| Item | What the child gets |
|---|---|
| Project and repository directory | The parent's project link and the project's repository directory (for example `<project-dir>`). It is never a worktree path, even when the parent runs in one. If the repository cannot be resolved, the directory is skipped and the decision log says so. |
| Automation membership and position | The child joins the parent's Automation right after the parent, through the normal member insert. A held insert and `agent_insert_policy` apply as for any insert. It is skipped when the parent has no Automation or the Automation is completed or cancelled. |
| Backend and model | Copied as a pair. A model never travels without its backend. |
| Verification LLM | The verification backend and model, copied as a pair the same way. |
| Tags | The parent's tags are added to the child's own tags. |
| Priority | Copied together with where the priority came from. |

## 2. An explicit value wins

Each item is copied only where the creation gave no value of its own. A project directory, backend, model, verification LLM, tags or priority you set on the child is never replaced by the parent's value. A model given without a backend keeps its own pairing.

## 3. The Sonnet/Haiku to Opus/Fable clamp

A child of a Sonnet or Haiku parent never ends up on an Opus or Fable model through inheritance. The inherited pair is clamped to the parent's own entry. A value you gave explicitly is never clamped. The clamp is noted in the child's decision log entry.

## 4. Where the decision log shows it

Inheritance runs once per child and never fails a creation. It writes one `child_inherit` entry to the child's decision log. The entry names the parent, what was inherited, what was clamped and what was skipped. Open the child Automaton and read its decisions (REST `GET /api/autonomous/prds/<id>`, MCP `autonomous_prd_get`, or the decisions list in the PWA detail view). A second pass changes nothing. With the setting off, nothing is copied and no entry is written.

## 5. Turn it off

`autonomous.automation.child_inherit` is `true` or `false`. An unset key reads `true`. Any other value is refused with 400.

```bash
datawatch config set autonomous.automation.child_inherit false
```

`PUT /api/config` and comm `configure autonomous.automation.child_inherit=false` write the same key. In the PWA, use Settings, Automata, *Child Automata inherit from their parent*. The key is read when each child is created, so a change applies to the next child with no restart and does not touch existing children.

## 6. Give the parent when you create a child

Every create surface takes an optional parent Automaton id. A parent id stands in for the project directory, and an unknown parent is refused before anything is created.

| Surface | How to give the parent |
|---|---|
| REST | `POST /api/autonomous/prds` with `parent_prd_id` in the body; an unknown parent is a 404 |
| MCP | `autonomous_prd_create` with the optional `parent_prd_id` argument |
| CLI | `datawatch autonomous prd-create --parent <id> "<spec>"` |
| Comm channel | `autonomous create <spec> --parent <id>` |
| PWA | The *Parent Automaton (optional)* field on the new Automaton form |

## 7. Operator review

- The PWA help text under the *Child Automata inherit from their parent* setting names only the project, priority, tags and Automation membership. The daemon also copies backend, model and verification LLM, so the help text is shorter than the behavior described here.
- A defect filed with a parent is not offered to the `defect_intake_automation` Automation when it already joined its parent's Automation.
- This page and [automaton-child-inheritance.md](automaton-child-inheritance.md) overlap on purpose: this one is the task-oriented reference, the other the background. Decide whether to merge them.

See also [automaton-child-inheritance.md](automaton-child-inheritance.md), [automation-chains.md](automation-chains.md) and the *Child inheritance* section of [../operations.md](../operations.md).
