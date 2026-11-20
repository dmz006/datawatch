# 9c8f0001 final gate (guard and smoke script)

- **Date**: 2026-11-20
- **Branch**: `automaton/9c8f0001--tasks-docs-set-for-the-branch-of-automaton-e19-54b90005-tpl-docs-set`
- **PRD**: "The branch of Automaton e19b0001 (daemon-side priority stall wake) changes the PWA although its own spec forbade any PWA"
- **Gate kind**: end-to-end check that the no-PWA guard and the priority-stalls smoke script both pass, and that this Automaton's own commits leave `internal/server/web/app.js` and `AGENT.md` untouched.

## What the gate checks

1. The guard script `scripts/check-no-pwa-when-spec-forbids.sh` and its test `tests/test-no-pwa-when-spec-forbids.sh` exist, parse, are wired into the Makefile test target, and all four self-test cases pass.
2. The smoke script `scripts/release-smoke-priority-stalls.sh` and `scripts/release-smoke.sh` parse; the smoke script probes all three surfaces; `release-smoke.sh` calls the smoke script and has a "Priority stalls" section.
3. `git diff` between the merge base and HEAD for `internal/server/web/app.js` and `AGENT.md` is empty (the PWA line that triggered this PRD is on branch `automaton/e19b0001`, not on this branch).

## Commands run

```
bash .datawatch/verify/final-gate.sh
# which runs, in order:
bash .datawatch/verify/no-pwa-guard.sh            # PASS x4 lines, then the two static checks print "ok"
bash .datawatch/verify/priority-stalls-smoke.sh
git diff --quiet "$(git merge-base HEAD main)" HEAD -- internal/server/web/app.js AGENT.md
echo ok
```

Run from the PRD task worktree (the Automaton task worktree), where `.datawatch/verify/` lives; the guard, its self-test, the smoke static check and the untouched-file check were independently re-run on this branch's worktree with the same result.

## Guard self-test (4 cases)

```
PASS case 1: spec forbids the PWA and app.js changed -> exit 1 with the exact line
PASS case 2: spec forbids the PWA but app.js untouched -> exit 0
PASS case 3: spec does not forbid the PWA -> exit 0 even with app.js changed
PASS case 4: missing spec file -> exit 2
```

All 4 PASS. Exit 0.

## Priority-stalls smoke script check

```
ok
```

Exit 0.

## PWA / AGENT.md untouched check

```
$ git diff --quiet "$(git merge-base HEAD main)" HEAD -- internal/server/web/app.js AGENT.md
# silent, exit 0 — the diff is empty
ok
```

The last `ok` is `final-gate.sh` printing it after the silent, successful diff check.

## Live smoke demo (main build, route absent)

```
SKIP: REST — GET /api/autonomous/priority-stalls: HTTP 404 (endpoint absent or disabled); the 405, MCP and CLI probes skip with it
SKIP: MCP — the daemon has no priority-stalls route (HTTP 404), so the tool-list probe (same build) skips
SKIP: CLI — the REST route is absent/disabled (HTTP 404), so the CLI probe (same daemon route) skips
SKIP: priority-stalls smoke — some probes skipped (see above), none failed
rc=2   (exit 2 = skip, none failed)
```

Expected on a `main` build: the priority-stalls route ships with branch `automaton/e19b0001` (PRD `e19b0001`, "Daemon-side priority stall wake with blocker and action"), which is not in `main`. The section and the standalone script both skip cleanly with `rc=2` and `none failed`, which is the designed behaviour for a build that does not serve the route.

## Live guard demo (e19b0001 branch — the actual violation)

```
$ cd <worktree of e19b0001>   # branch automaton/e19b0001
$ bash <docs-branch>/scripts/check-no-pwa-when-spec-forbids.sh <e19b-spec.md>
forbidden PWA change: internal/server/web/app.js
rc=1   (exit 1 = violation detected)
```

The e19b0001 spec constraints say `no PWA change`, and the branch changes `internal/server/web/app.js` (the `'priority_stall'` line added to `_LIFECYCLE_SUB_KIND_GROUPS`). The guard catches exactly this case — it is the guard that would have stopped this line reaching the Events lifecycle filter.

## Verdict

**PASS (exit 0)** — all three gate checks pass; the guard catches the known violation; the smoke script skips cleanly on a build that does not serve the route; the PWA source file and `AGENT.md` are untouched by this PRD's commits.

## Operator review flags

- The one `app.js` line on branch `automaton/e19b0001` (`'priority_stall'` in `_LIFECYCLE_SUB_KIND_GROUPS`) is the operator's decision at merge. This PRD does not remove it and does not merge that branch.
- The guard is not in `make lint`; it is run as a standalone script. Whether to wire it into `make lint` or `scripts/check-howto-coverage.sh` is an operator decision.
- The priority-stalls smoke section will report `skip` on any build derived from `main` until `automaton/e19b0001` is merged. That is the designed skip path, not a failure.
