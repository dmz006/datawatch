# Trace retention follows session — counts only

Date: 2026-11-13. Counts and pass/fail facts only, counted from the git diff against the merge base with main. No transcripts, no prose analysis.

## Go test cases (added or changed)

| Area | File area | Added | Changed |
|---|---|---|---|
| Store | internal/trace (config, store) | 4 | 5 |
| Session delete | internal/session (delete path) | 4 | 0 |
| Surfaces | internal/config + internal/server | 2 | 1 |
| **Go total** | | **10** | **6** |

## JS test cases (added or changed)

| Area | Added | Changed |
|---|---|---|
| Web app (locale + field-wiring test) | 3 | 0 |

## Surfaces and texts

| Item | Count |
|---|---|
| Locale bundles updated (de, en, es, fr, ja) | 5 of 5 |
| Docs files updated (config reference, definitions, agent tracing how-to, implementation, operations, parity entry, testing tracker) | 7 |
| Retention_days default is 0 (keep forever) | PASS |

## Deletion paths checked

| Path | Result |
|---|---|
| Store: delete removes file, index and seen-set for exactly the deleted id | PASS |
| Store: unknown id is a no-op | PASS |
| Store: invalid ids refused without touching the store | PASS |
| Session delete: removes the trace of the exact id only | PASS |
| Session delete: ended and archived sessions keep their trace | PASS |
| Session delete: a trace-deleter error does not fail the delete | PASS |
| Session delete via Automaton cleanup: removes the trace | PASS |

Deletion paths checked: 7 of 7 PASS.

## Test runs (all with `env -u TMUX -u TMUX_TMPDIR`)

| Run | Pass | Fail |
|---|---|---|
| Go build (whole module) | 1 | 0 |
| Go tests: internal/trace package | PASS | 0 |
| Go tests: internal/config package | PASS | 0 |
| Go tests: 4 new session-delete tests | 4 | 0 |
| Go tests: 2 new server validation tests | 2 | 0 |
| JS tests: retention hint + field wiring | 3 | 0 |

Total: 0 fails across all runs.

## Gate facts

- Fixtures, fakes, fixed clocks and temp directories only.
- No daemon started.
- No operator configuration read.
- No version bump.
- No hosting CLI invoked.
- User-visible text uses Automaton and Automata.

## Flagged for operator review

- `CHANGELOG.md` `[Unreleased]` carries no entry for the trace-retention change
  (default 0 = keep while the session exists, session delete removes the trace).
  The story's docs scope did not include `CHANGELOG.md`; the release commit
  should carry it.
- `tracing.retention_days: 0` now means traces are kept **forever** by default
  (previously the 14-day default applied). An operator who wants bounded disk
  growth should set a positive `tracing.retention_days` explicitly.
- Gated verifications still pending a live environment are unchanged: the
  testing-tracker rows for the trace feature remain `Validated = No` (Go unit
  level only), per `docs/testing-tracker.md`.
