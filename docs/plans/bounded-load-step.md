# Bounded load step posture

Source: operator note, 2026-11-07, Phase 4 of the autonomy handover plan
("Part of finishing review and merge by the daemon").

Any CPU-load or stress step a task asks for (a busy loop, stress test, load or
soak test, or any other command that pins the CPU) must be bounded before it runs:

1. **Niced:** run with `nice -n 19` so it yields to the host's other work.
2. **Capped workers:** use at most a quarter of the cores, `nproc/4`, with a minimum of one.
3. **Hard timeout:** wrap it in `timeout <duration> ...` so it always dies on its own.
4. **Trap cleanup:** install `trap cleanup EXIT INT TERM` that kills the load's children,
   so no orphaned load survives.
5. **Task-scoped lifetime:** the load must never outlive its owning task. When the task
   ends, the load ends with it.

Automaton plans tasks to leave the host as it found it, so an unbounded load step is a
defect in the plan.

Example shape:

```bash
WORKERS=$(( $(nproc) / 4 )); [ "$WORKERS" -lt 1 ] && WORKERS=1
cleanup() { pkill -P $$ 2>/dev/null; }   # children of this shell only
trap cleanup EXIT INT TERM
for _ in $(seq "$WORKERS"); do
  timeout 300s nice -n 19 sh -c 'while :; do :; done' &
done
wait
```

Note: the example's `pkill -P $$` only targets direct children of the script's shell.
Any load step must be verified to end on its own timeout before it is accepted.

## Parity surface

This rule changes no PWA, app or other client surface: it is a rule for how a task spec bounds a CPU-load or stress step and adds no setting, endpoint, message or screen. The decompose lint warning it adds (`unbounded_load_step`) is carried by the existing lint findings read.
