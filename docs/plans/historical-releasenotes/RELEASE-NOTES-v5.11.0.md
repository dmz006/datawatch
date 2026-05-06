# datawatch v5.11.0 — release notes

**Date:** 2026-04-26
**Spans:** v5.10.0 → v5.11.0
**Closed:** BL190 howto screenshot capture pipeline (first cut)

## What's new

### BL190 — Howto screenshot capture pipeline

Operator removed the chrome MCP plugin (suspected memory leak); the
new capture path goes through **puppeteer-core in `/tmp/puppet`
driving `/usr/bin/google-chrome` headless** — entirely outside the
chrome MCP. Two scripts ship:

#### `scripts/howto-shoot.mjs`

Recipe-driven puppeteer driver. Each recipe pre-seeds the PWA's
`localStorage` (`cs_active_view`, `cs_splash_time`,
`cs_splash_version`) so the splash never blocks captures, runs any
recipe-specific setup (e.g. `window.switchSettingsTab('comms')`),
and writes one PNG per shot label into
`docs/howto/screenshots/<recipe>.png`.

Six recipes ship:

- `sessions-landing` — Sessions tab with running session row
- `autonomous-landing` — Autonomous tab with PRD list (status pills)
- `settings-llm` — Settings → LLM sub-tab
- `settings-comms` — Settings → Comms sub-tab
- `settings-voice` — Settings → General with Voice Input section
- `diagrams-landing` — `/diagrams.html` page header (API spec + MCP tools links)

```bash
PUPPET_DIR=/tmp/puppet \
  node scripts/howto-shoot.mjs sessions-landing --out=docs/howto/screenshots
```

The `puppeteer-core` install lives at `/tmp/puppet` (not committed;
survives between runs). Re-installing is one `pnpm install` away;
the `package.json` is intentionally tiny.

#### `scripts/howto-seed-fixtures.sh`

Idempotent JSONL seeder. Wipes anything tagged `"fixture":true` and
re-seeds:

- One PRD per status pill (`draft`, `decomposing`, `needs_review`,
  `approved`, `running`, `completed`) so the autonomous tab
  screenshot covers every badge.
- One orchestrator graph with two PRD nodes + a guardrail node.
- One pipeline with before/after gates.

```bash
bash scripts/howto-seed-fixtures.sh        # default: ~/.datawatch
datawatch reload                            # pick up the new JSONL
```

#### Six screenshots inlined into four howtos

| Howto | Screenshots added |
|-------|-------------------|
| `chat-and-llm-quickstart.md` | `settings-llm.png` (LLM sub-tab), `sessions-landing.png` (running session row), `settings-comms.png` (Comms sub-tab) |
| `autonomous-planning.md` | `autonomous-landing.png` (PRD list with status pills + LLM button) |
| `voice-input.md` | `settings-voice.png` (Settings → General with Voice Input section) |
| `mcp-tools.md` | `diagrams-landing.png` (`/diagrams.html` header with API spec + MCP tools links) |

The `Makefile sync-docs` rsync include list (already extended in
v5.0.5 for `*.png`) carries the screenshots into the embedded PWA
docs viewer, so the same screenshots ship inside the daemon binary
for offline rendering.

## Why a "first cut" and not the full ~200 PNGs

Each new shot needs the recipe in `howto-shoot.mjs`, the seeded
fixture rows it depends on, and the markdown insertion in the right
howto. Adding ~15-20 shots × 13 howtos all at once is a lot of
plate-spinning that's better done iteratively per-howto so each
walkthrough lands as a coherent set rather than a screenshot
avalanche. The capture pipeline is the load-bearing piece — once
that's stable, expanding the recipe map is mechanical.

## Known follow-ups (still open)

- **BL190 expand-and-fill** — extend the `howto-shoot.mjs` recipe map
  per-howto and inline more screenshots into the remaining 9 howtos
  (`setup-and-install`, `comm-channels`, `federated-observer`,
  `autonomous-review-approve`, `prd-dag-orchestrator`,
  `container-workers`, `pipeline-chaining`, `cross-agent-memory`,
  `daemon-operations`). Mechanical work; iterative.
- **BL180 Phase 2** — eBPF kprobes (resume) + cross-host federation
  correlation. The eBPF work was backed out cleanly mid-edit in
  v5.6.0 (never compiled successfully); will resume with
  `BPF_MAP_TYPE_LRU_HASH` + userspace TTL pruner. Cross-host needs
  multi-peer infrastructure that isn't reachable from the dev
  workstation overlay.

## Upgrade path

```bash
datawatch update                          # check + install
datawatch restart                         # apply the new binary

# Optional — re-capture the bundled six screenshots locally:
bash scripts/howto-seed-fixtures.sh       # one-shot seed
datawatch reload
node scripts/howto-shoot.mjs all --out=docs/howto/screenshots
```
