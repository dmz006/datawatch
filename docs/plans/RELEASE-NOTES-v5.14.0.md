# datawatch v5.14.0 — release notes

**Date:** 2026-04-26
**Spans:** v5.13.0 → v5.14.0
**Closed:** BL190 expand-and-fill (full howto coverage)

## What's new

### BL190 expand-and-fill — every howto has at least one screenshot

v5.11.0 shipped the puppeteer-core capture pipeline + 6 screenshots
inlined into 4 howtos. v5.14.0 extends the recipe map and pushes
coverage to all 13 howtos.

**Recipe map: 6 → 11**

New recipes in `scripts/howto-shoot.mjs`:

| Recipe | What |
|--------|------|
| `settings-monitor` | Settings → Monitor (CPU/mem/disk/GPU + daemon RSS + RTK savings + episodic memory + ollama runtime tap) |
| `settings-about` | Settings → About (version, GitHub link, orphan-tmux maintenance) |
| `alerts-tab` | Alerts tab (incoming-event queue) |
| `autonomous-new-prd-modal` | Autonomous tab → "+ New PRD" modal with backend / effort / model dropdowns |
| `session-detail` | Sessions tab → click a card → live tmux pane + saved-commands + response viewer |

**Inline coverage: 4 → 13 howtos**

Every howto now carries at least one inline PNG:

| Howto | Screenshots |
|-------|-------------|
| `setup-and-install.md` | settings-monitor, settings-about |
| `chat-and-llm-quickstart.md` | settings-llm, sessions-landing, session-detail, settings-comms |
| `comm-channels.md` | settings-comms |
| `mcp-tools.md` | diagrams-landing |
| `voice-input.md` | settings-voice |
| `autonomous-planning.md` | autonomous-landing |
| `autonomous-review-approve.md` | autonomous-landing, autonomous-new-prd-modal |
| `prd-dag-orchestrator.md` | autonomous-landing |
| `container-workers.md` | settings-monitor |
| `pipeline-chaining.md` | session-detail |
| `cross-agent-memory.md` | settings-monitor |
| `federated-observer.md` | settings-monitor |
| `daemon-operations.md` | settings-monitor, alerts-tab, settings-about |

**Why 1-3 per howto and not the 15-20 from the original plan**

The original BL190 plan targeted ~15-20 PNGs per howto for a
full visual walkthrough — about 200 PNGs total. This release
delivers the structural completeness (every howto reachable, every
section has at least one PNG) without locking in the deeper density.
Per-howto expansion stays a mechanical follow-up: each new shot
needs a recipe, a fixture row, and a markdown insertion, but the
load-bearing pipeline is now stable and reused across captures.

## Known follow-ups (still open)

- **BL190 density** — extend each howto from 1-3 PNGs to the original
  15-20-per-howto target. Mechanical; per-howto cuts. No new
  infrastructure needed; the recipe template + seed-fixtures pattern
  is reusable.

## Upgrade path

```bash
datawatch update                      # check + install
datawatch restart                     # apply the new binary

# Re-capture screenshots locally:
bash scripts/howto-seed-fixtures.sh
datawatch reload
node scripts/howto-shoot.mjs all --out=docs/howto/screenshots
make sync-docs                        # mirror into the embedded PWA viewer
```
