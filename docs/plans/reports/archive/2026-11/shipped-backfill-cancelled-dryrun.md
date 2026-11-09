# Shipped backfill (merged-then-cancelled) — dry-run gate report

Date: 2026-11-02

## Method

- A temporary copy of the repository history was made for this check:
  `git clone --no-hardlinks . <temp dir>`; all counts below were computed
  from that temporary copy, and the copy was deleted afterwards.
- In the copy, the named suite was run exactly as:
  `env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous/ -run 'TestShippedBackfill' -count=1 -v` (7s overall, suite PASS).
- The dry-run/real-run numbers are copied verbatim from the `-v` test log
  of the `TestShippedBackfillDryRunCountsWritesNothing` fixture test
  (printed by a `t.Logf` report line in that test).

## Test results (the four new tests)

| Test | Result |
|---|---|
| TestShippedBackfillMergedThenCancelledIsMarked | PASS (0.06s) |
| TestShippedBackfillCancelledWithoutMergeUntouched | PASS (0.05s) |
| TestShippedBackfillDryRunCountsWritesNothing | PASS (0.06s) |
| TestShippedBackfillSecondRunMarksNothing | PASS (0.05s) |

(All tests matched by the `-run` pattern passed, suite exit 0.)

## Fixture counts: dry run vs real run

| Field | Dry run | Real run |
|---|---|---|
| prds_scanned | 4 | 4 |
| prds_marked | 3 | 3 |
| prds_would_mark_cancelled | 1 | 1 |
| prds_already_marked | 0 | 0 |
| automations_marked | 0 | 1 |
| left_cancelled | 1 | 1 |

The dry run accounts for exactly what the real run then marks (the dry-run
line also reports 1 unmatched merge id; it is a count only and is not
reproduced in this report). No PRD or automation carries shipped state
after the dry run; the real run marks the same records it predicted.

## Merged-id total (from the temporary copy)

Merge commits on `main` whose subject names an `automaton/<8-hex>` branch:
**246** (extracted with `git log main --merges`, ids deduplicated).
Counts only — no branch ids, hosts, addresses or paths are recorded here.
