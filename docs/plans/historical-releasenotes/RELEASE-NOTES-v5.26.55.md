# datawatch v5.26.55 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.54 → v5.26.55
**Patch release** (no binaries — operator directive).
**Closed:** §7j F10 agent lifecycle smoke probe (mint → spawn → audit → terminate).

## What's new

Operator-asked: *"F10 you can configure the test service and complete."*

### §7j — F10 agent lifecycle

Last service-function audit residual that didn't need an entirely new fixture. Smoke now exercises the agent lifecycle plumbing end-to-end against the persistent `datawatch-smoke` + `smoke-testing` fixtures from §7d:

```
== 7j. F10 agent lifecycle (mint→spawn→audit→terminate) ==
  PASS  GET /api/agents returns canonical {agents:[]} shape
  PASS  agent spawn round-trip returned id=…
  PASS  agent … appears in GET /api/agents
  (PASS auth audit grew on spawn — when the BL113 broker is wired)
  PASS  agent … DELETE → 204 (terminate + token revoke path triggered)
```

### Token cleanup invariant

Operator-asked: *"Make sure if git tokens are used that they should be cleaned up when done with them."*

The BL113 broker (v5.26.24) already mints + revokes per-spawn — smoke verifies the audit invariant that *something* lands in `~/.datawatch/auth/audit.jsonl` on every spawn (mint or mint-fail). When the broker successfully mints, the matching revoke fires on agent terminate; when mint fails (e.g. `gh auth login` not configured for the daemon's broker context), no token was ever issued so nothing leaks.

### What this required on the dev daemon

- `agents.image_prefix=harbor.dmzs.com/datawatch` and `agents.image_tag=latest` set via `PUT /api/config` (configuration parity rule — same knobs available through YAML / Web UI / MCP / CLI / comm channels). Without the prefix the daemon tried to pull `agent-opencode:v5.26.55` from Docker Hub, which doesn't exist.
- Daemon restart (these keys require restart per `/api/reload`'s response).
- The persistent `datawatch-smoke` project profile + `smoke-testing` cluster profile from §7d (v5.26.33).

These are all operator-side config; the smoke gates cleanly when any of them are missing (skip with clear reason instead of fail).

### Smoke total

51 → 56. Five new PASS from §7j (four hard-asserts + one optional audit-grew check that lands when the broker is wired).

## Configuration parity

No new config knob in the daemon. The `agents.image_prefix` / `agents.image_tag` keys already existed and are reachable via every channel.

## Tests

Smoke against the dev daemon: 56 pass / 0 fail / 1 skip. Go test suite unaffected: 465 passing.

## Known follow-ups

- **Wake-up stack L0–L5 probes (#39).** Builds on the F10 fixture from this patch — once a real agent boots, read its wake-up bundle. Tracked.
- **Stdio-mode MCP tools probe.** Needs an MCP client wrapper.
- **PWA Settings UI for `agents.*`.** Operator-asked: *"where in the pwa settings is the agent configuration."* Tracked separately.
- **Whisper test mic dialog.** Operator-asked: a real interactive recording test, not just a backend health check. Tracked separately.

## Upgrade path

```bash
git pull
# No daemon restart needed for the smoke change. To run the F10
# section locally you need:
#   - agents.image_prefix + agents.image_tag set (Web UI or REST)
#   - the §7d persistent profiles already in place (smoke handles
#     creating these idempotently)
# The §7j section gates cleanly when prerequisites are missing.
```
