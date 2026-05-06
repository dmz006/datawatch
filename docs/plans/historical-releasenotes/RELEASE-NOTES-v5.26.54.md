# datawatch v5.26.54 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.53 → v5.26.54
**Patch release** (no binaries — operator directive).
**Closed:** Phase 6 screenshots refresh — autonomous howto screenshots recaptured against current PWA shape.

## What's new

Operator-asked: *"I thought you could do screenshots without the browser, check memory and rules."* The repo already ships `scripts/howto-shoot.mjs` (BL190, v5.11.0+) which drives Chrome over the CDP via puppeteer-core — no GUI browser session needed. v5.26.54 uses that pipeline to recapture the autonomous howto shots so they reflect the v5.26.30/32/36/37/46 UX.

### Screenshots recaptured

```
docs/howto/screenshots/autonomous-landing.png       (90 KB — fresh)
docs/howto/screenshots/autonomous-prd-expanded.png  (106 KB — fresh)
docs/howto/screenshots/autonomous-mobile.png        (196 KB — fresh)
docs/howto/screenshots/autonomous-new-prd-modal.png (51 KB — fresh; recipe fixed)
```

The `autonomous-new-prd-modal` recipe was stale — it looked for a button containing "New PRD" text, which v5.26.36 replaced with a Floating Action Button (`+`, no "New PRD" text). v5.26.54 updates the recipe to find the FAB by `id="prdNewFab"` first, with text/aria-label fallbacks for older PWA builds.

```js
// Recipe shape after v5.26.54
const fab = document.getElementById('prdNewFab');
if (fab) { fab.click(); return; }
const aria = Array.from(document.querySelectorAll('button')).find(b =>
  (b.getAttribute('aria-label') || '').toLowerCase() === 'new prd');
if (aria) { aria.click(); return; }
const btn = Array.from(document.querySelectorAll('button')).find(b => /new prd/i.test(b.textContent));
if (btn) btn.click();
```

### What the screenshots now show

- **`autonomous-landing.png`** — PRDs label + magnifying-glass filter toggle in the top header bar (v5.26.46), bottom-right `+` FAB matching sessions FAB size (v5.26.37), 7 seeded PRDs in the list.
- **`autonomous-prd-expanded.png`** — expanded PRD card with story description visible (v5.26.32) + ✎ edit affordances on stories/tasks.
- **`autonomous-mobile.png`** — same layout at mobile viewport (412×850).
- **`autonomous-new-prd-modal.png`** — unified Profile dropdown (v5.26.30/34) — first option `__dir__`, configured profiles below; cluster row appears only when a profile is picked.

### Pipeline reuse

The shoot script's recipe map covers many more shots that are unchanged but still capture-able. Running the full recipe set is an operator decision (some recipes need fresh fixtures or specific config); the v5.26.54 pass focused on the autonomous recipes since the v5.26.30+ UX rework concentrated there.

To run the full set yourself:

```bash
bash scripts/howto-seed-fixtures.sh    # seed PRD / graph / pipeline fixtures
datawatch restart                       # daemon picks up the JSONL stores
for s in autonomous-landing autonomous-prd-expanded autonomous-mobile autonomous-new-prd-modal \
         settings-llm settings-comms settings-voice settings-monitor sessions-landing \
         session-detail diagrams-landing; do
  PUPPET_DIR=/tmp/puppet node scripts/howto-shoot.mjs "$s" --out=docs/howto/screenshots --base=https://localhost:8443
done
```

Recipes are added/edited inside `scripts/howto-shoot.mjs` — the `RECIPES` map at the top of the file documents each shot and its setup steps.

## Configuration parity

No new config knob — pipeline already exists.

## Tests

Smoke unaffected (51/0/1). Go test suite unaffected (465 passing). Screenshot byte-size deltas confirm fresh capture.

## Known follow-ups

- F10 ephemeral agent lifecycle smoke probe — Docker socket + agent images verified available on this dev workstation; the smoke probe could land next.
- Wake-up stack L0–L5 probes — builds on F10 fixture.
- Stdio-mode MCP tools probe — needs an MCP client wrapper.

Other backlog unchanged — see `docs/plans/2026-04-27-v6-prep-backlog.md`.

## Upgrade path

```bash
git pull
# No daemon restart needed — docs + scripts only.
```
