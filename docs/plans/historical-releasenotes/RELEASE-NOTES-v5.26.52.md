# datawatch v5.26.52 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.51 → v5.26.52
**Patch release** (no binaries — operator directive).
**Closed:** Schedule store CRUD smoke probe (§7h) + channel send round-trip smoke probe (§7i).

## What's new

### §7h — Schedule store CRUD

Service-function audit residual. `/api/schedules` supports both `command` (against a live session) and `new_session` (deferred session spawn) types. Smoke uses `new_session` with a far-future `run_at` (1 hour out) plus immediate cancel — schedule never fires during the test. Three new PASS:

```
== 7h. Schedule store CRUD ==
  PASS  schedule created: 71bf13e2 (name=smoke-sched-…, run_at=…)
  PASS  schedule 71bf13e2 round-trips through GET /api/schedules
  PASS  schedule 71bf13e2 cancelled
```

### §7i — Channel send round-trip

`/api/test/message` simulates an inbound channel command (signal/telegram/slack/etc) without needing a live messaging backend. Smoke verifies the canonical response shape on `help` and `list` commands. Two new PASS:

```
== 7i. Channel send round-trip (test/message) ==
  PASS  /api/test/message help round-trip returns canonical command list
  PASS  /api/test/message list returns canonical {count, responses} shape
```

### Smoke total

46 → 51. Three schedule passes + two channel passes net +5.

## Configuration parity

No new config knob — script-only additions.

## Tests

Smoke: 51 pass / 0 fail / 1 skip. Go test suite unaffected (465 passing).

## Known follow-ups

Service-function smoke residuals after v5.26.52:

- F10 ephemeral agent lifecycle (needs `agents.enabled=true` fixture).
- Wake-up stack layer probes L0–L5 (need spawned-agent fixture).
- Stdio-mode MCP tools (need MCP client wrapper).

PRD-flow phases 3 + 4 design, mempalace alignment audit, screenshots refresh, datawatch-app PWA mirror — all unchanged.

## Upgrade path

```bash
git pull
# No daemon restart needed — script-only change.
```
