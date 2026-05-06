# datawatch v5.15.0 — release notes

**Date:** 2026-04-26
**Spans:** v5.14.0 → v5.15.0
**Closed:** BL190 density expansion (first cut)

## What's new

### BL190 density — recipe map 11 → 19; 22 PNGs total

v5.14.0 shipped structural completeness (every howto has at least
one PNG). v5.15.0 fills in the per-howto density per the original
plan's target.

#### New recipes

| Recipe | What |
|--------|------|
| `sessions-mobile` | Sessions tab in 412×850 portrait viewport |
| `autonomous-mobile` | Autonomous tab in portrait — every status pill visible |
| `autonomous-prd-expanded` | "Stories & tasks" toggle opened on the seeded `fixrich` PRD — shows actual story + 3 tasks |
| `autonomous-prd-decisions` | Same expansion with all nested `<details>` open — Decisions log surfaced |
| `session-detail-mobile` | Live tmux pane in portrait |
| `settings-general-autonomous` | Settings → General scrolled to the Autonomous block |
| `settings-general-auto-update` | Settings → General scrolled to the Auto-update card |
| `settings-comms-signal` | Settings → Comms scrolled to the Signal section |
| `settings-llm-ollama` | Settings → LLM scrolled to the Ollama block |
| `settings-llm-memory` | Settings → LLM scrolled to the Episodic Memory config |
| `settings-monitor-mobile` | Settings → Monitor in portrait |
| `header-search` | Sessions tab with the header search affordance toggled — backend filter chips visible |
| `diagrams-flow` | `/diagrams.html` scrolled to a flowchart |

The puppeteer driver (`scripts/howto-shoot.mjs`) gained per-recipe
`viewport` overrides so the same script handles desktop + portrait
captures without two passes.

#### Seed-fixtures enrichment

`scripts/howto-seed-fixtures.sh` now writes a `fixrich` PRD with
real content:

```jsonl
{"id":"fixrich", "status":"running",
 "stories":[{"title":"Wire the CACHE column …",
   "tasks":[
     {"id":"t1","title":"Add CacheHitPct to StatsResponse","status":"completed", …},
     {"id":"t2","title":"Populate from collector","status":"in_progress"},
     {"id":"t3","title":"Render the chip","status":"pending","depends_on":["t2"]}
   ],
   "verdicts":[{"guardrail":"rules","outcome":"pass","summary":"adheres to BL10 …"}]
 }],
 "decisions":[{"kind":"decompose"}, {"kind":"approve"}, {"kind":"run"}], "fixture":true}
```

The `autonomous-prd-expanded` recipe targets this row by name so the
screenshot has substance instead of "no stories yet".

The shell counter at the end of the seed script also got fixed — it
was matching `fixture:` (any context) instead of `"fixture":true`.

#### Inline coverage extended

Multi-shot sequences land in 8 howtos:

| Howto | Total PNGs |
|-------|------------|
| `chat-and-llm-quickstart.md` | 6 (settings-llm + settings-llm-ollama + sessions-landing + session-detail + sessions-mobile + settings-comms) |
| `daemon-operations.md` | 6 (settings-monitor + alerts-tab + settings-about + header-search + settings-general-auto-update + session-detail-mobile) |
| `autonomous-planning.md` | 3 (autonomous-landing + autonomous-prd-expanded + autonomous-mobile) |
| `autonomous-review-approve.md` | 4 (autonomous-landing + autonomous-new-prd-modal + autonomous-prd-expanded + settings-general-autonomous) |
| `comm-channels.md` | 2 (settings-comms + settings-comms-signal) |
| `cross-agent-memory.md` | 2 (settings-monitor + settings-llm-memory) |
| `federated-observer.md` | 2 (settings-monitor + settings-monitor-mobile) |
| `prd-dag-orchestrator.md` | 2 (autonomous-landing + autonomous-prd-expanded) |
| `setup-and-install.md` | 2 (settings-monitor + settings-about) |
| `mcp-tools.md` | 1 (diagrams-landing) |
| `voice-input.md` | 1 (settings-voice) |
| `pipeline-chaining.md` | 1 (session-detail) |
| `container-workers.md` | 1 (settings-monitor) |

22 PNGs total — about double the v5.14.0 set. The Makefile
`sync-docs` rsync still carries them into the embedded PWA docs
viewer.

## Known follow-ups (still open)

- **BL190 deeper density** — per-state captures (failure-path yellow
  popup, verdict drill-down panel, mid-run progress bar) need
  scripted stateful interaction beyond what the current recipe
  shape handles. Next cycle if the operator wants the original
  15-20-per-howto density.

## Upgrade path

```bash
datawatch update                                # check + install
datawatch restart                               # apply the new binary
```

To regenerate screenshots locally:

```bash
bash scripts/howto-seed-fixtures.sh             # idempotent re-seed
datawatch reload
node scripts/howto-shoot.mjs all --out=docs/howto/screenshots
make sync-docs
```
