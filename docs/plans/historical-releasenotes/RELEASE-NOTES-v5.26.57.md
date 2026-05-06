# datawatch v5.26.57 — release notes

**Date:** 2026-04-28
**Spans:** v5.26.56 → v5.26.57
**Patch release** (no binaries — operator directive).
**Closed:** §7k claude skip_permissions config round-trip + targetable smoke (`SMOKE_ONLY=`) + smoke-frequency rule revised.

## What's new

### 1. §7k — claude `skip_permissions` config round-trip

Operator-asked: *"What does Claude skip permissions do? Is the recent autonomous skip permissions the same thing, or should already have been there right? Have we smoke tested it?"*

Yes, it's been wired since BL3 — `cfg.Session.ClaudeSkipPermissions` controls whether the daemon launches claude-code with `--dangerously-skip-permissions` (per-tool-call approvals auto-yes). Autonomous PRD spawns inherit this; there's no separate "autonomous skip permissions" knob (the TUI status `⏵⏵ bypass permissions on` operators see is claude's display of the flag we passed).

Smoke didn't have a regression probe for the config knob until now. §7k:

```
== 7k. Claude skip_permissions config round-trip ==
  PASS  GET /api/config exposes session.skip_permissions=true
  PASS  PUT /api/config flipped session.skip_permissions to false
```

Toggle, verify, restore — never leaks state across runs. Maps to `cfg.Session.ClaudeSkipPermissions` internally; dotted-key shape is `session.skip_permissions` (a regression in the dotted-key handler can no longer silently disable the flag).

The actual *behavior* (claude bypassing prompts) needs a live claude session to verify — out of scope for the current smoke, tracked.

### 2. Targeted smoke via `SMOKE_ONLY`

Operator-asked: *"can't targeted smoke tests run instead of them all if needed."*

`scripts/release-smoke.sh` now accepts `SMOKE_ONLY=<comma-separated section numbers/prefixes>`:

```bash
SMOKE_ONLY=1,7k    bash scripts/release-smoke.sh   # health + skip_permissions only
SMOKE_ONLY=7d,7e,7f,7g,7h,7i,7j,7k bash scripts/release-smoke.sh   # service-function audit only
SMOKE_ONLY=8       bash scripts/release-smoke.sh   # observer peer only
```

Sections that don't match the filter print `(skipped — not in SMOKE_ONLY=...)` instead of running their assertions. Counters (`PASS` / `FAIL` / `SKIP`) stay accurate against the filtered subset. Empty `SMOKE_ONLY` (the default) runs every section as before.

Implementation note: the no-op-counters approach short-circuits assertion logging but still executes some setup boilerplate inside skipped sections. A full early-exit pass (wrapping each section body in `if [[ "$SECTION_SKIP" != 1 ]]; then ... fi`) would be faster but is a much larger surgery; tracked.

### 3. Smoke-frequency rule revised

Operator directive 2026-04-28: *"smoke tests should only be run on minor and major releases unless otherwise specified or a new feature initial testing"*

`AGENT.md` § "Release testing" updated. New rule:

| Release shape | Smoke requirement |
|------|------|
| Major / minor | Full smoke required |
| Patch introducing a new feature | Full smoke required for the first patch of that feature |
| Patch (bug fix / refactor / doc) | Optional; targeted via `SMOKE_ONLY` is appropriate |

Memory file `feedback_per_release_smoke.md` updated to match. The 2026-04-27 "every release" rule was an overcorrection — the v3.10.0 bug it was reacting to would have been caught at the v3.11.0 minor boundary, not the patch boundary, so the cost of full-smoke-every-patch wasn't earning the coverage.

When in doubt, run smoke. Cost is low; coverage is the point.

## Configuration parity

No new config knob.

## Tests

Smoke against the dev daemon: 58 pass / 0 fail / 1 skip (was 56/0/1; +2 from §7k). Go test suite unaffected (465 passing).

## Known follow-ups

- **Wake-up stack L0–L5 smoke probes (#39).** Builds on the F10 fixture from v5.26.55.
- **Targeted-smoke speed.** Skipped sections still execute setup; full early-exit gating is a follow-up.

## Upgrade path

```bash
git pull
# No daemon restart needed — script + AGENT.md change. Future
# patches without new features can skip the full smoke.
```
