# Bounded load step posture — recorded 2026-11-08

**Date:** 2026-11-08
**Recorded against:** "Stop a slow image description from costing the operator
an extra Enter. Found 2026-11-08 right after the vision describe"
**Record kind:** note only — it changes nothing; every rule below already
shipped with orphan reaping, the host load alert and the bounded load step
rule (v9.112.0, BL431).

## The posture

Any CPU-load or stress step a task asks for — a busy loop, a stress test, a
load or soak test, or any other command that pins the CPU — must be bounded
before it runs:

- run it niced (`nice -n 19`) so it yields to the system's other work;
- use at most a quarter of the cores (cap the worker count at `nproc/4`,
  minimum one);
- give it its own hard timeout (a bounded `timeout <duration> ...` wrapper)
  so it always dies on its own;
- clean up its children with a trap on `EXIT INT TERM` so no orphaned load
  survives; and
- it must never outlive its owning task — when the task ends, the load ends
  with it.

Automaton plans tasks to leave the host as it found it, so an unbounded load
step is a defect in the plan.

## Where this wording already lives (single source)

The rule text is the `boundedLoadStepRule` const in
`internal/autonomous/load_step_rules.go`, carried into three
operator-visible surfaces that all say the same thing:

1. the built-in `bounded_load_step` task template
   (`internal/autonomous/task_templates_defaults.go`,
   `TemplateBoundedLoadStep`) — the posture note a fresh story starts with;
2. the rules block appended to the decompose prompt
   (`Manager.loadStepSuffix`, both decompose-prompt sites) so the planner
   reads the rule before it writes the plan;
3. the warn-severity `unbounded_load_step` finding in the decompose lint
   (`internal/autonomous/decompose_lint_rules.go`) — flags a spec that asks
   for an unbounded load step.

Reading it from the surfaces is covered by
[docs/howto/automata-orphan-reaping.md](../howto/automata-orphan-reaping.md)
(the *bounded load step rule* section) and the *Bounded load step (rule)*
entry in [docs/datawatch-definitions.md](../datawatch-definitions.md).

## What this record does and does not do

Records the posture for the task above as-of 2026-11-08. It makes no code,
config or documentation change beyond this note file itself, and touches
nothing else.
