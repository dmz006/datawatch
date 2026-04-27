# datawatch v5.26.4 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.3 → v5.26.4
**Patch release** (no binaries / containers — operator directive: every release until v6.0 is a patch).
**Closed:** Doc-alignment sweep — MCP tool reference, CLI command reference, README interface tables, channel API doc, testing-tracker headline number, design/architecture/architecture-overview v5 appendix

## What's new

Pure docs. No code changes outside the version bumps.

### `docs/mcp.md` — full v5.x tool catalog

The "Available Tools" section had been stuck at "100+ tools" with a `Tool families (high-level)` table that hadn't been refreshed since v5.9. v5.26.4:

- Bumps the headline to "**132 tools**" (the actual count from the v5.26.3 binary).
- Refreshes the family table to include every tool currently registered (added: `session_bind_agent`, `session_import`, `session_reconcile`, `session_rollback`, `sessions_stale`, `stop_all_sessions` to the Sessions row).
- Adds a "Tools added in v5.9 → v5.26" callout so operators upgrading across the patch series can see what's new at a glance: `autonomous_prd_children` (Q4 spawn), `autonomous_prd_edit_task`, `autonomous_prd_set_llm` + `autonomous_prd_set_task_llm` (BL203), `autonomous_prd_instantiate`, `observer_envelopes_all_peers` (BL180 cross-host), the six new session tools, and `reload`.

### `docs/commands.md` — v5.x CLI reference

The "CLI Commands" section listed about 20 commands and stopped at v3.x. The actual binary has 134 cobra factories. v5.26.4 adds inline reference for every v4 → v5 addition with parameter tables where they take args:

`datawatch reload` (v5.7.0), `ask`, `assist`, `autonomous <subcommand>` (status / config / learnings / prd CRUD / approve+reject+revise / children / set-llm / set-task-llm), `observer <subcommand>` (stats / envelope / envelopes / envelopes-all-peers / agent / peer / config), `orchestrator <subcommand>` (graph CRUD + verdicts + config), `agent <subcommand>` (spawn / list / show / logs / kill), `profile <subcommand>` (project / cluster), `projects <subcommand>` (summary / upsert), `cooldown`, `device-alias`, `routing-rules`, `template`, `plugins` + `plugin`, `cost`, `audit query`, `diagnose`, `alerts`, `health`, `link`, `logs`, `rollback`, `stale`, `export`, `config <subcommand>` (init / generate / show / get / set / edit), `test [--pr]`, `session schedule`.

### README interface tables — refreshed

- **MCP Tools** section: bumped from "60+ tools" → "**132 tools**"; refreshed the Areas table to include every current family (Sessions / Memory+KG / Cost+audit / Operations / Autonomous / Observer / Agents / Plugins / Orchestrator / Profiles+projects / Templates+scheduling+cooldown+devices+routing / Pipelines+saved+ask).
- **REST API** section: replaced the 13-row table with a comprehensive 30+-row table of every high-traffic endpoint, grouped by area: sessions CRUD, config + reload + restart + update, observer + peers + federation, autonomous PRD CRUD + actions + children, orchestrator graphs + verdicts, agents + bootstrap, profiles + projects, memory, channel (including the new `GET /api/channel/history` from v5.26.1), voice, devices, proxy, test-message, diagnose, health.

### `docs/api/channel.md` — new

Channel API was undocumented. v5.26.4 adds a full reference covering `POST /api/channel/{reply,notify,send,ready}` plus the new `GET /api/channel/history?session_id=…` from v5.26.1. WS message types, direction values, and per-channel reachability table included. Wired into `/diagrams.html` API group.

### `docs/testing.md` — headline test count refreshed

The "Unit Test Summary (v2.4.1)" section claimed 228 tests across 40 packages (12.6% coverage). The current binary ships **1395 tests across 58 packages** — 6× growth. v5.26.4 adds a new "Unit Test Summary (v5.26.3)" header with the live number plus a list of major test additions since v2.4.1 (server httptest battery, autonomous CRUD/recursion/guardrails, observer cross-peer + eBPF, agents pinned-mTLS, CLI smoke). The v2.4.1 section is preserved as historical interest.

### `docs/architecture-overview.md` — v5.x deltas appendix

The Mermaid one-screen diagram remains the canonical view but hasn't been redrawn since the v3.x cut. Rather than re-render every node, v5.26.4 adds a **v5.x deltas** table at the bottom enumerating every subsystem added since the original cut (BL191 review/approve, BL203 LLM overrides, BL17 reload CLI, BL201 voice inheritance, BL191 Q4 child PRDs, BL191 Q5/Q6 guardrails, BL180 cross-host federation, eBPF kprobes, BL190 howto-shoot pipeline, autonomous CRUD, MCP channel redirect-bypass, BL202 learnings, observer/whisper config-parity, autonomous WS auto-refresh, settings docs chips, diagrams-page restructure, retention rule, configured-only backends, channel history, howto README links, helm/k8s setup, long-press refresh, button-revival escHtml fix, security review). Each row points at the howto / API doc / source file.

### `docs/architecture.md` — package list expanded

The package table had `config`, `messaging`, `signal`, `llm`, `session`, `router`, `mcp`, `server`, `proxy`, `transcribe`, `metrics`, `rtk`, `tlsutil`. v5.26.4 adds: `autonomous`, `orchestrator`, `observer`, `observerpeer`, `plugins`, `agents`, `profile`, `channel`, `audit`, `alerts`, `devices`, `cost`, `pipeline`, `kg`+`memory`. Each row carries its BL ID and one-line role.

### `docs/design.md` — v4 → v5 design evolution appendix

The original v3.x design rationale stays as-is. New "v4 → v5 design evolution (2026-04-27 audit)" section covers each major subsystem and the key design decisions, organized as:

5.1 Autonomous PRD substrate (BL24+BL25 → BL191 → BL202)
5.2 PRD-DAG orchestrator (BL117)
5.3 Observer + federation (BL171/BL172/BL180)
5.4 Plugin framework (BL33)
5.5 Ephemeral worker agents (F10)
5.6 Federated observer (S14a)
5.7 Helm chart (charts/datawatch)
5.8 Channel transport (claude MCP / opencode-acp)
5.9 Configuration parity backbone

Each section names the decisions, references the relevant PRD / config knobs, and points at the implementation package.

## Configuration parity

No new config knob.

## Tests

1395 still passing. No code changes outside version bumps.

## Known follow-ups

- Container hygiene patch (next — v5.26.5): parent-full retag, GHCR cleanup script (operator-side action with `read:packages + delete:packages` token), datawatch-app#10 catch-up issue, container audit doc.
- v6.0 cumulative release notes — to be drafted before the v6.0 cut. Operator manually treats everything before then.

## Upgrade path

```bash
git pull          # patch series — no binary update path
# Browse the refreshed docs at /diagrams.html → How-tos / Subsystems / API / Architecture
# Or read directly: docs/mcp.md, docs/commands.md, docs/api/channel.md,
#                   docs/architecture-overview.md (v5.x deltas appendix),
#                   docs/architecture.md (package list),
#                   docs/design.md (v4 → v5 design evolution)
```
