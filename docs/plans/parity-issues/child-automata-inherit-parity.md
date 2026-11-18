Parent release: datawatch v9.161.0

## What changed in datawatch

A child Automaton now starts from its parent's setup. Where its creation gave no value of its own, it copies the parent's project link and repository directory, the parent's Automation (joining right after the parent), backend and model as a pair, verification backend and model, tags and priority. An explicit value wins, and a child of a Sonnet or Haiku parent is never put on an Opus or Fable model by inheritance. Every creation surface takes an optional parent Automaton id (REST `parent_prd_id`, MCP, CLI `--parent`, comm `--parent`).

## New config key

Returned and accepted by `GET` and `PUT /api/config` as a dotted key, and shown in Settings > Automata.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `autonomous.automation.child_inherit` | boolean | `true` | Whether a child Automaton inherits from its parent when it is created. An unset key reads true; `false` restores the earlier behavior. Any other value is rejected with a 400 that names the key. |

## What the mobile app needs

- A Settings toggle for `autonomous.automation.child_inherit` (label "Child Automata inherit from their parent (default on)" plus help text), in all five locales.
- An optional *Parent Automaton* picker on the new Automaton form that sends `parent_prd_id` (strings `prd_new_parent_label` and `prd_new_parent_help`, five locales).
- Show the `child_inherit` decision entry in the child's decisions list.
