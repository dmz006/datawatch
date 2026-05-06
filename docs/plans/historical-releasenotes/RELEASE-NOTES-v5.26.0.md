# datawatch v5.26.0 — release notes

**Date:** 2026-04-26
**Spans:** v5.25.0 → v5.26.0
**Closed:** Settings card-section docs chips

## What's new

### Every Settings card section gets a docs chip

Operator: *"Every card section in settings should have docs, they don't. And complex settings should have howto."*

`settingsSectionHeader(key, title, docsPath)` already supported a `docsPath` arg (added when the inline-docs-links toggle shipped) but no caller passed one — so the chip never rendered.

v5.26.0:

- Added `docs:` field to every entry in `COMMS_CONFIG_FIELDS`, `LLM_CONFIG_FIELDS`, `GENERAL_CONFIG_FIELDS`.
- Updated the three render paths (`COMMS_CONFIG_FIELDS.map`, `LLM_CONFIG_FIELDS.map`, `GENERAL_CONFIG_FIELDS.map`) to forward `sec.docs` to `settingsSectionHeader`.

For complex sections (autonomous, orchestrator, voice, pipelines, memory, sessions, RTK), the docs chip points at the relevant **howto** so operators get the walkthrough rather than just the API reference. For simpler sections (web server, MCP server, plugins, datawatch, auto-update), the chip points at the architecture-level doc.

| Section | docs chip target |
|---------|------------------|
| Comms → Web Server | `operations.md` |
| Comms → MCP Server | `mcp.md` |
| General → Datawatch | `howto/setup-and-install.md` |
| General → Auto-Update | `howto/daemon-operations.md` |
| General → Session | `howto/chat-and-llm-quickstart.md` |
| General → Pipelines | `howto/pipeline-chaining.md` |
| General → Autonomous | `howto/autonomous-planning.md` |
| General → Plugin framework | `agents.md` |
| General → PRD-DAG orchestrator | `howto/prd-dag-orchestrator.md` |
| General → Voice Input (Whisper) | `howto/voice-input.md` |
| LLM → Episodic Memory | `howto/cross-agent-memory.md` |
| LLM → RTK | `rtk-integration.md` |

The `Show inline doc links` toggle (already in Settings → General) hides every chip when off — operator-controlled per-browser preference.

## Configuration parity

No new config knob (the chip system was already in place; v5.26.0 just populates it).

## Tests

1390 still passing. Pure PWA-config-array changes.

## Known follow-ups

- Design doc audit / refresh — `docs/design.md` + `docs/architecture.md` + `docs/architecture-overview.md` need a sweep
- datawatch-app#10 catch-up issue
- Container parent-full retag
- GHCR container image cleanup
- gosec HIGH-severity review

## Upgrade path

```bash
datawatch update                        # check + install
datawatch restart                       # apply
# Open Settings — every section header now has a "docs" chip on the right.
# Click any chip to open the linked howto/architecture doc in /diagrams.html.
```
