# Bounded load step record — Fix the built-in task templates so a decomposed Automaton's final gate and version note tasks pass the verifier instead

**Recorded:** 2026-11-08 · **Kind:** operator record — a note, not a change · **Carries no version number of its own**

## The note

For the operator's records: the bounded-load-step posture that applies to
"Fix the built-in task templates so a decomposed Automaton's final gate and
version note tasks pass the verifier instead" (and to every task it
decomposes):

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

## Where the rule already lives (not restated in code by this record)

The wording above is the single source
(`internal/autonomous/load_step_rules.go`, `boundedLoadStepRule`), carried
into the three operator-visible surfaces that all say the same thing:

| Surface | File |
|---------|------|
| built-in `bounded_load_step` task template — the posture note a fresh story starts with | `internal/autonomous/task_templates_defaults.go` |
| the rules block appended to the decompose prompt, so the planner is told the rule before it writes the JSON plan | `internal/autonomous/load_step_rules.go` (`Manager.loadStepSuffix`), wired at both decompose-prompt sites in `internal/autonomous/manager.go` |
| the warn-severity `unbounded_load_step` decompose-lint finding, so the lint hook flags specs the decomposer should bound | `internal/autonomous/decompose_lint_rules.go` |

The rule shipped with the orphan reaping and host load work
(`docs/plans/2026-11-08-orphan-reaping-and-host-load.md`, phase 4), and
`internal/autonomous/load_step_rules_test.go` covers the template, the
suffix, and the lint finding.

## State of the files (left exactly as they are)

| File | Change made by this note |
|------|--------------------------|
| `internal/autonomous/load_step_rules.go` | none — rule text untouched |
| `internal/autonomous/task_templates_defaults.go` | none — template untouched |
| `internal/autonomous/decompose_lint_rules.go` | none — lint rule untouched |
| `cmd/datawatch/main.go`, `internal/server/api.go` | none — version fields untouched |
| `CHANGELOG.md` | no `## vX.Y.Z` heading added by this note |

This record deliberately carries no version number of its own and changes no
rule text: the posture it records is the one the engine already enforces at
planning time. The release version of the story it belongs to is the next
patch above main, resolved at merge time — see
`docs/plans/2026-11-08-built-in-task-templates-release-version-note.md`.

## Cross-references (same branch, same story)

- `docs/plans/2026-11-08-built-in-task-templates-release-version-note.md`
  (the story's version note, the sibling record of this one).
- `docs/plans/2026-11-08-orphan-reaping-and-host-load.md` (phase 4: the
  bounded load step rule this note records).
- `docs/plans/parity-issues/2026-11-08-built-in-task-templates-verification.md`
  (the story's parity-issue draft).
