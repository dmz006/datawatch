# Bounded load step posture — Part of finishing review and merge by the daemon (Phase 4 of the autonomy handover plan)

- Record type: **operator note** (template `bounded_load_step`)
- Recorded: 2026-11-08
- Scope: "Part of finishing review and merge by the daemon (Phase 4 of the autonomy handover plan). Operator, 2026-11-07, quoted:"

## The posture (quoted)

> Any CPU-load or stress step a task asks for — a busy loop, a stress test, a load or
> soak test, or any other command that pins the CPU — must be bounded before it runs:
> run it niced (`nice -n 19`) so it yields to the system's other work; use at most a
> quarter of the cores (cap the worker count at `nproc/4`, minimum one); give it its
> own hard timeout (a bounded `"timeout <duration> ..."` wrapper) so it always dies on
> its own; clean up its children with a trap on `EXIT INT TERM` so no orphaned load
> survives; and it must never outlive its owning task — when the task ends, the load
> ends with it. Automaton plans tasks to leave the host as it found it, so an unbounded
> load step is a defect in the plan.

## Where this posture is already enforced in-tree

The quoted text is the shipped rule, verbatim:

- `internal/autonomous/load_step_rules.go` — `boundedLoadStepRule` constant; injected
  into the task prompt context and matched by `internal/autonomous/load_step_rules_test.go`.
- `internal/session/tracker.go` — the same rule text is part of the session prompt
  discipline, covered by `internal/session/tracker_test.go`.
- `internal/autonomous/decompose_lint_rules.go` — the decompose lint rejects a spec that
  requests a CPU-load or stress step without the bounding requirements (nice, quarter of
  the cores, hard timeout, trap cleanup, cannot outlive the owning task).

Any task in this PRD that needs a load or stress step is therefore already held to the
posture above; this note records that posture for the Phase 4 scope.

## Verification of "note, not edit"

- `git status` → clean working tree: the only file produced by this task is this note;
  no code, config, version field, CHANGELOG heading, or other file was modified.
- The quoted text above matches `boundedLoadStepRule` in
  `internal/autonomous/load_step_rules.go` word for word.
