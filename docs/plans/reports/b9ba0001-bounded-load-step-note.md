# b9ba0001 — bounded load step posture note

- Date: 2026-11-08
- Story: "Fix the built-in task templates so a decomposed Automaton's final gate and version note tasks pass the verifier instead" (PRD `b9ba0001`)
- Template: `bounded_load_step` (note task — records a posture, changes no behavior)
- Result: posture recorded; story contains no load or stress steps

## The rule (verbatim, from the single source `boundedLoadStepRule`)

Any CPU-load or stress step a task asks for — a busy loop, a stress test, a
load or soak test, or any other command that pins the CPU — must be bounded
before it runs: run it niced (`nice -n 19`) so it yields to the system's other
work; use at most a quarter of the cores (cap the worker count at `nproc/4`,
minimum one); give it its own hard timeout (a bounded
`timeout <duration> ...` wrapper) so it always dies on its own; clean up its
children with a trap on `EXIT INT TERM` so no orphaned load survives; and it
must never outlive its owning task — when the task ends, the load ends with it.
Automaton plans tasks to leave the host as it found it, so an unbounded load
step is a defect in the plan.

## Application to this story

This story only edits the built-in task templates (the final gate template and
the version note template, in `task_templates_defaults.go`) so those tasks pass
the verifier. Every task is a static template and documentation edit: no task
here performs a busy loop, a stress test, a load or soak test, or any other
command that pins the CPU. The bounded load step rule therefore applies as a
planning posture for this story — had any task asked for such a step, it would
have been a plan defect until it satisfied all five bounds above — and no load
step is executed or required by this story's work.

## Where the rule lives (single source, carried verbatim)

- Rule text: `internal/autonomous/load_step_rules.go` (`boundedLoadStepRule`)
- Built-in `bounded_load_step` task template: `internal/autonomous/task_templates_defaults.go`
- Decompose-prompt rules block: `Manager.loadStepSuffix` in `internal/autonomous/load_step_rules.go`
- Decompose-lint warn finding `unbounded_load_step`: `internal/autonomous/decompose_lint_rules.go`
- Operator docs: `docs/howto/automata-orphan-reaping.md` (§ "The bounded load step rule"), `docs/datawatch-definitions.md` (glossary entry "Bounded load step (rule)")
- Session guardrail: `templates/session-CLAUDE.md`
