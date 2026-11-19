---
docs:
  index: true
  topics: [autonomous, automaton, completed, landing, merge, gate, no-code, completion_refusal]
---

# Why an Automaton will not read Completed until its work has landed

An Automaton only reads **Completed** when its work has landed on the default branch (`main`). If the work sits on its own `automaton/<id>` branch that was never merged, the status stays where it was and the Automaton records why.

## What counts as landed

The completion landing gate accepts a change to Completed when any one of these is true:

- The Automaton branch is merged into the default branch (ancestry, or patch equivalence for a squash merge).
- A shipped or merge record names the merge commit.
- The landing record says the branch landed, or the operator recorded it as kept or dropped.
- The record carries a **no-code reason** (work that has no branch to merge).
- The gate is switched off (`autonomous.completion_landing.enabled: false`).

A judge warning does not replace a merge: accepting a warning on a re-run still needs one of the above.

## Where the gate runs

Every path that sets Completed asks the gate first: the completion rollup after the last task, the judge re-run that accepts a warning, and the post-ship hook (the commit you supply counts as the evidence). The archive check reads the same recorded evidence, so an Automaton with an unmerged branch is not archived as if it had landed.

## What a refusal looks like

The status does not change. The Automaton gets a `completion_refusal` field holding the plain reason, and its decision log gets a `completion_refused` entry naming the path (`rollup`, `judge_warn_accepted`, `post_ship`). The reason reads "completion refused: branch … is not merged into the default branch … Merge the branch, or record a no-code reason." A later allowed change clears the field. The re-run and post-ship calls also return the reason as an error.

## What to do

1. Merge the branch into `main` (or record the merge commit with the shipped-record call).
2. If the work needs no merge, record a no-code reason on the Automaton.
3. Run the completion step again (rejudge or re-roll up).

The gate only reads git. It never merges, reverts or edits anything.

## Setting

`autonomous.completion_landing.enabled` — default `true` (an absent key reads on). Set it with `datawatch config set`, `PUT /api/config`, MCP `autonomous_config_set`, or the YAML file; see `docs/config-reference.yaml`.

## Operator review

- Automata already stored as Completed are not re-checked; use the [completed migration](automata-completed-migration.md) rules for those.
- A repo path that is unset falls back to the Automaton's project directory; confirm `worktree_base_repo` is set so the proof reads the right repository.
- No PWA, mobile or MCP-specific surface was added for `completion_refusal`; it appears in the Automaton record on the existing read surfaces. Review whether a dedicated display is wanted.

## See also

- [The shipped status](automata-shipped-status.md)
- [Show finished Automata as Completed](automata-completed-migration.md)
- [Merge-pending check](../operations.md#completion-landing-gate-autonomouscompletion_landing)
- [Definitions](../datawatch-definitions.md#completion-landing-gate)
