# datawatch v5.25.0 — release notes

**Date:** 2026-04-26
**Spans:** v5.24.0 → v5.25.0
**Closed:** Diagrams page restructure + asset retention rule refinement

## What's new

### Diagrams page (`/diagrams.html`) restructure

Operator: *"Diagrams page should not have plans, but should have app docs and howtos."*

- Dropped the `Plans` group from the sidebar — operator-internal docs are gitignored from the embedded viewer per `docs/_embed_skip.txt` since v5.3.0 anyway, so this just removes the broken/empty group.
- Added a top-level `How-tos` group with all 13 walkthroughs from `docs/howto/` (setup, chat-llm-quickstart, comm-channels, voice-input, mcp-tools, autonomous-planning, autonomous-review-approve, prd-dag-orchestrator, container-workers, pipeline-chaining, cross-agent-memory, federated-observer, daemon-operations).
- Extended the existing groups:
  - **Subsystems** gained `mcp.md` + `cursor-mcp.md` so operators can find the MCP setup docs.
  - **API** gained `observer.md`, `memory.md`, `sessions.md`, `devices.md`, `voice.md` (these existed under `docs/api/` but weren't reachable from the sidebar).

The `app-docs` (datawatch-app mobile companion) live in the datawatch-app repo and aren't in scope for the embedded viewer; a future cut may proxy them in via `/api/datawatch-app/docs`.

### Retention rule refined

Operator: *"New retention rule, save binaries and containers for every major, the latest minor and the latest patch on the latest minor."*

`AGENT.md § Release-discipline rules` updated. Keep-set is now:

1. Every **major** (X.0.0) — kept indefinitely
2. The **latest minor** (highest X.Y.0 where Y >= 1) — kept until superseded
3. The **latest patch on the latest minor** (highest X.Y.Z, Z > 0, X.Y matches the latest minor) — kept until superseded

Everything else: pruned. Release notes themselves stay forever.

`scripts/delete-past-minor-assets.sh` rewritten to implement the new keep-set logic. Idempotent — re-run on every release.

## Configuration parity

No new config. Pure docs / PWA / release-script work.

## Tests

1390 still passing. No code changes that affect Go tests.

## Known follow-ups

Per the audit doc:

- Design doc audit / refresh — `docs/design.md` + `docs/architecture.md` + `docs/architecture-overview.md` need a sweep
- Every Settings card section gets a docs chip; complex ones get a howto link
- datawatch-app#10 catch-up issue
- Container parent-full retag
- GHCR container image cleanup (needs `read:packages + delete:packages` token)
- gosec HIGH-severity review

## Upgrade path

```bash
datawatch update                            # check + install
datawatch restart                           # apply
# Open https://localhost:8443/diagrams.html — sidebar now starts with "How-tos"
```
