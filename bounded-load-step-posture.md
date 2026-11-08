# Bounded Load-Step Posture (record 2026-11-08)

This note records the `bounded_load_step` posture for the PRD task
"Show each system tag once in the Sessions tag menu" (found 2026-11-08
from the operator's phone screenshot: the Tags menu). It is a record,
not a change — no code, configuration or other file is modified.

## The rule

Any CPU-load or stress step a task asks for — a busy loop, a stress
test, a load or soak test, or any other command that pins the CPU —
must be bounded before it runs:

- **Niced** — run it with `nice -n 19` so it yields to the system's
  other work.
- **Capped** — use at most a quarter of the cores (cap the worker
  count at `nproc/4`, minimum one).
- **Timed** — give it its own hard timeout (a bounded
  `timeout <duration> ...` wrapper) so it always dies on its own.
- **Self-cleaning** — clean up its children with a trap on
  `EXIT INT TERM` so no orphaned load survives.
- **Bounded by its owner** — it must never outlive its owning task;
  when the task ends, the load ends with it.

Automaton plans tasks to leave the host as it found it, so an
unbounded load step is a defect in the plan.

## Where the rule already lives

- `templates/session-CLAUDE.md` — session-level rule for every
  claude-code session.
- `docs/howto/automata-orphan-reaping.md` — operator howto, "The
  bounded load step rule" section.
- `internal/autonomous/task_templates_defaults.go` — built-in
  `bounded_load_step` task template.
- `internal/autonomous/decompose_lint_rules.go` — the
  `unbounded_load_step` decompose-lint warning.
- `docs/plans/README.md` row 431 — shipped 2026-11-08 in v9.112.0.

The same posture was additionally recorded to datawatch memory
(this session's project scope) on 2026-11-08.
