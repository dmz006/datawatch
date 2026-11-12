# Claude Code interstitial prompt dismissal: release smoke result

Section 92 of `scripts/release-smoke.sh` was run on the script's own isolated
sandbox daemon (temporary data directory, random ports, no auth token), never on
the production daemon. Command, from the repository root:

```
env -u TMUX -u TMUX_TMPDIR DW_SKIP_TIDY_CHECK=1 SMOKE_ONLY=92 bash scripts/release-smoke.sh
```

`DW_SKIP_TIDY_CHECK=1` skips the unrelated plan-tidiness pre-gate, which
otherwise exits 1 over stale files in `docs/plans` before any section runs.
The binary was built with `go build -o datawatch ./cmd/datawatch/`.

## Run 1: before the fix (FAILED)

```
== 92. Claude Code interstitial prompt dismissal — session.ui_prompt* config keys + stats ui_prompts ==
  FAIL  defaults: a session.ui_prompt* key is missing or not at its default
  SKIP  changed-value round-trip: GET /api/config could not resolve all four keys; skipping the write steps
  FAIL  stats: ui_prompts present but malformed or the stats snapshot is broken: 

== Summary ==
  (skipped — not in SMOKE_ONLY=92)
  Pass:  0
  Fail:  2
  Skip:  1

FAIL: 2 functional check(s) failed; release should NOT proceed.

```

Exit status 1. Both failures were an unreachable daemon, not a wrong value.

### Cause

The checks before section 92 were skipped by `SMOKE_ONLY`, but section 81
still ran `scripts/release-smoke-mobile-fit.sh`. That child sources the
daemon-lifecycle library, whose EXIT trap ends every process recorded in the
`TD_STATE_DIR` it inherits. The inherited directory held the smoke script's own
daemon, so the daemon was ended the moment the child exited, even on a clean
skip (Playwright not installed). Every later request got "connection refused".
A finer-grained trace showed the daemon's PID vanishing about 0.12 s after the
child started.

## Fix

One change in `scripts/release-smoke.sh`: the mobile-fit runner is started with
`env -u TD_STATE_DIR -u TD_OWNER`, so it records and ends only its own
processes. Nothing else was changed.

## Run 2: after the fix (PASSED)

```
== 92. Claude Code interstitial prompt dismissal — session.ui_prompt* config keys + stats ui_prompts ==
  PASS  defaults: dismiss_ui_prompts=true, dismiss_delay_seconds=5, clear_bound_seconds=30, max_attempts=2
  PASS  changed values PUT (false/3/45/3) answered 200 and read back applied
  PASS  max_attempts=0 (below the minimum of 1) refused with 400
  PASS  negative delay (-8) refused with 400
  PASS  non-integer clear bound (1.5) refused with 400
  PASS  the refused bad-value PUTs changed nothing (values are still false/3/45/3)
  PASS  all four keys restored to their original values (true/5/30/2)
  PASS  stats: ui_prompts object absent (nothing dismissed yet) or well-shaped (dismissed/not_cleared/refused counts + by_kind map)

== Summary ==
  (skipped — not in SMOKE_ONLY=92)
  Pass:  8
  Fail:  0
  Skip:  0

OK: all functional checks passed (skips are fine — gated on whether the subsystem is configured).

```

Exit status 0.

## Leftover daemons

`bash scripts/check-no-leftover-test-daemons.sh check <baseline>`, with the
baseline taken before run 1, reported `OK: no leftover test daemons` after the
runs (it only reports and never ends anything).
