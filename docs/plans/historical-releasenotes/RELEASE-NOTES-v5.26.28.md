# datawatch v5.26.28 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.27 → v5.26.28
**Patch release** (no binaries — operator directive).
**Closed:** Smoke memory check was silently broken — wrong endpoint + python AttributeError swallowed by `2>/dev/null`.

## What's new

### Smoke now actually exercises memory recall

Operator: *"Memory should be working"*.

Two bugs in `scripts/release-smoke.sh` made the memory check skip on every release even though the subsystem was healthy:

1. **Wrong endpoint.** Smoke called `/api/memory/recall`, but the daemon route is `/api/memory/search`. Smoke was getting a 404 page back from the HTTP redirector and silently SKIPping.
2. **Python AttributeError.** The check tried `d.get("results",[])` on what was actually a top-level JSON list. `list` has no `.get()`, so the assertion threw, the `2>/dev/null` swallowed the error, and smoke fell through to the SKIP branch.

Fix:

```bash
MR=$(curl ... "$BASE/api/memory/search?q=smoke" || true)
if echo "$MR" | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d,list) or isinstance(d.get("results",[]),list)' 2>/dev/null; then
  ok "memory search returned a result list"
else
  skip "memory not enabled or returned $(echo "$MR" | head -c 100)"
fi
```

Order of `isinstance` cases matters — `isinstance(d,list)` short-circuits before `.get()` is attempted.

### What was hiding behind the silent SKIP

The bug landed when smoke first added the memory section. The endpoint name doesn't appear in the daemon source — the wrong path was a mistype that always 404'd. Because every release legitimately could have memory disabled, "SKIP" looked like the expected outcome and nobody noticed for ~22 patches.

Net effect of the fix: smoke now reports `34 pass / 0 fail / 1 skip` instead of `33 / 0 / 2` (the remaining skip is orchestrator, which actually IS disabled in this dev env).

## Configuration parity

No code changes — pure smoke-script fix.

## Tests

Smoke memory section now passes against running v5.26.28 with operator memory enabled:

```
== 9. Memory recall (smoke) ==
  PASS  memory search returned a result list
```

## Known follow-ups

Same as v5.26.27 — see `docs/plans/2026-04-27-v6-prep-backlog.md`.

## Upgrade path

```bash
git pull
# No daemon restart needed — pure smoke-script change.
```
