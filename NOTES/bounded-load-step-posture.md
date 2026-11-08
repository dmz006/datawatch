# Bounded-load-step posture (template:bounded_load_step)

**Recorded for:** "Fix the built-in task templates so a decomposed Automaton's final gate and version note tasks pass the verifier instead"
**Date:** 2026-11-08
**Type:** Posture note — a rule to apply to any planned task, not a code change. This note does not alter any behavior of the product.

## The rule

Any CPU-load or stress step a task asks for — a busy loop, a stress test, a load or
soak test, or any other command that pins the CPU — **must be bounded before it
runs**:

1. **Yield to the system.** Run it niced — `nice -n 19` — so it yields to the
   system's other work.
2. **Cap the width.** Use at most a quarter of the cores: cap the worker count
   at `nproc/4`, minimum one.
3. **Hard timeout.** Give it its own hard timeout — a bounded
   `timeout <duration> ...` wrapper — so it always dies on its own, even if
   nothing else kills it.
4. **Own its cleanup.** Clean up its children with a `trap` on `EXIT INT TERM`
   so no orphaned load survives. This is consistent with AGENT.md's
   test-daemon cleanup rules: every job starts a process it must end on every
   exit path, by the PID it recorded when it started, never by name pattern.
5. **Lifetime ≤ owning task.** The load must never outlive its owning task —
   when the task ends, the load ends with it.

## Rationale

Automaton plans tasks to leave the host as it found it. An unbounded load step
is a defect in the plan, not a runtime accident to be tolerated.

## Reference shape

```sh
# workers capped at a quarter of the cores, minimum one
WORKERS=$(( $(nproc) / 4 )); [ "$WORKERS" -lt 1 ] && WORKERS=1

trap 'kill 0' EXIT INT TERM        # own every child on every exit path
nice -n 19 timeout 300 ./load_driver -workers "$WORKERS" ...
```

- `nice -n 19`: lowest scheduling priority — the load yields to everything else.
- `timeout 300` (duration per the task's need): hard death even on hang.
- `trap ... EXIT INT TERM`: no orphaned children on normal exit, failure, or interrupt.
- Task-end ⇒ load-end: the step's lifetime is bounded by its owning task's by
  construction, since its children are trapped to the step's own exit.

This is a note, not a change: it writes no product files, touches no code, and
changes no settings.
