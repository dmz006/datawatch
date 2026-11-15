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

Any task in this Automaton that needs a load or stress step is therefore already held to
the posture above; this note records that posture for the Phase 4 scope.

## Verification of "note, not edit"

- `git status` → clean working tree: the only file produced by this task is this note;
  no code, config, version field, CHANGELOG heading, or other file was modified.
- The quoted text above matches `boundedLoadStepRule` in
  `internal/autonomous/load_step_rules.go` word for word.

## Attempt 2 re-verification (2026-11-08)

A retry of this task recorded the posture in project memory only and was rejected by
the change-detection gate ("no changes detected, task produced no output"). The
durable record is therefore this in-tree note plus the committed rule, and this pass
re-verified the chain and closed one wording defect:

- `boundedLoadStepRule` in `internal/autonomous/load_step_rules.go` is the single
  source of the quoted posture; the injected prompt context, the decompose lint
  message in `internal/autonomous/decompose_lint_rules.go`, and the session-prompt
discipline in `internal/session/tracker.go` all carry the same bounds
  (nice -n 19; at most nproc/4 cores, minimum one; hard `timeout` wrapper; trap on
  EXIT INT TERM; never outlives the owning task).
- `go test ./internal/autonomous/ ./internal/session/` → green
  (8132 tests passed in 2 packages, 2026-11-08).
- Terminology fix in this file: "This PRD" → "This Automaton" (AGENT.md Terminology
  Rule — user-facing wording must say Automaton, never PRD).
- No code, config, or other doc file was modified by this task.

## Parity surface

This note changes no PWA, app or other client surface: it records the bounded-load-step posture for task specs and adds no setting, endpoint, message or screen. A spec that asks for an unbounded load step shows the `unbounded_load_step` decompose lint warning through the existing lint findings read, which already reaches every surface.
