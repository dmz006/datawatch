# BL190 follow-up — How-to docs + PWA screenshot rebuild

**Status:** plan only — paused. Order tightened by operator 2026-04-26:
the screenshot rebuild runs **last**, after BL180 Phase 2 (#274), BL191
(#275), and the howto-coverage expansion (#276). The rebuild stays in
this doc; the coverage expansion has its own task and is summarized in
the new section below.

## Howto coverage expansion (prereq — task #276)

Per operator directive 2026-04-26, before any new screenshots are
captured the howto suite gets expanded so every semi-complex feature
has a walkthrough an operator can follow:

1. **Refresh the existing six** against every change shipped since the
   howto first landed. Anything stale gets rewritten, not patched.
   Cross-check against `docs/plans/README.md` Recently-closed for
   features that landed after the howto was written, and against
   AGENT.md for any rule changes that affect the workflow.
2. **Add a "Setup + install" howto** — first-time install end-to-end:
   binary install, daemon start, config file location, smoke-test
   ping, where logs land, how to verify the PWA is reachable.
3. **Add a "Configure the most common chat + LLM" howto** — the one or
   two most-used LLM backends (claude + ollama) and chat channels
   (signal + telegram) with copy-paste config snippets, the order they
   should be set up, and the verification step for each.
4. **Sweep every feature ever shipped** (AGENT.md + plans/README.md
   Closed table + `datawatch --help` long-help + REST OpenAPI + MCP
   tool list + chat verbs + PWA Settings sub-tabs). Anything
   semi-complex that lacks a howto gets one — target ~12-15 howto
   docs total when done (current 6 + at least 6-9 new).

When the expansion is done, the screenshot rebuild below picks up the
finished howto set and captures one full visual walkthrough per doc.

**Goal:** turn the current six how-to docs from skim-friendly outlines
into walkthrough-grade documentation an operator can follow start-to-finish
without leaving the page. Each step that has a UI surface gets a real PNG
captured from the live PWA.

---

## Scope

- 6 how-to docs under `docs/howto/`:
  `autonomous-planning`, `prd-dag-orchestrator`, `container-workers`,
  `pipeline-chaining`, `cross-agent-memory`, `daemon-operations`.
- Target: **~15 – 20 PWA screenshots per how-to** (~100 total).
- **CLI + comm-channel samples stay as code blocks** (already grep-able and
  render in the diagrams viewer). PNGs are reserved for PWA states.
- Each PNG is paired with prose explaining what the operator should see and
  what to do next.

## Per-how-to shot list (target)

Ten to twelve states per how-to, plus 3 – 5 step-detail close-ups:

1. **Setup landing**: the relevant Settings panel before any change.
2. **Setup mid-state**: the toggle / dropdown / input being changed.
3. **Setup verified**: the post-save state that confirms persistence.
4. **Pre-walkthrough state**: empty list or "no items yet" baseline.
5. **First object created**: PRD draft / graph draft / pipeline created /
   memory wing created / etc.
6. **Decompose / plan / configure**: the explosion-into-stories /
   guardrail-nodes / before-after-gates / room-layout state.
7. **Run started**: progress bar present, queued tasks visible.
8. **Mid-run drill-down**: per-task / per-node detail pane open.
9. **Worker session list**: the autonomous-spawned sessions appearing in
   the sessions tab.
10. **Worker session detail**: tmux output, channel pane, status badges.
11. **Verdict / attestation**: pass / warn / block badges with a
    drill-down panel open.
12. **Failure path**: the "input required" yellow popup or `block` halt
    state, with the operator-action affordance visible.
13. **Done state**: completed PRD / graph / pipeline summary.
14. **Inspector / log**: REST verdicts JSON in the response viewer, or the
    docs-link chip click landing in `/diagrams.html`.

## Fixture seeding (no real LLM runs)

Real runs would (a) burn LLM credits, (b) take minutes per shot, (c)
clutter the operator store. Instead, seed the JSONL store directly so the
PWA renders populated views:

- `~/.datawatch/autonomous/prds.jsonl` — append PRDs in `draft`,
  `decomposed`, `running`, `done` states with hand-built `Story` +
  `Task` + `VerificationResult` entries that exercise every badge.
- `~/.datawatch/orchestrator/graphs.jsonl` — same idea for graphs +
  guardrail nodes.
- `~/.datawatch/pipeline/jobs.jsonl` — chained tasks with
  before / after gates.
- Memory + KG: use the public `memory_remember` + `kg_add` MCP tools so
  the views show real records.
- Sessions: spawn one or two short-lived shell sessions just before
  capture so the sessions list isn't empty.

After seeding, restart the daemon once so it reloads the JSONL stores.

The seed script lives at `scripts/howto-seed-fixtures.sh`. It is
idempotent: each run wipes the screenshot fixtures (anything tagged
`fixture: true`) and re-seeds, leaving non-fixture data alone.

## Capture pipeline

Already validated end-to-end on this branch:

- **`/tmp/puppet/shoot.mjs`** — puppeteer-core driving system Chrome
  (`/usr/bin/google-chrome`).
- Pre-seeds `localStorage`:
  `cs_active_view`, `cs_active_session`, `cs_splash_time`,
  `cs_splash_version` so the splash never blocks captures.
- Waits on real selectors (`.session-card`, `.settings-tab-btn`,
  `.diagram-shell`) before screenshotting.
- For Settings sub-tabs: calls `window.switchSettingsTab(name)` directly
  (the SPA does not URL-hash route sub-tabs).
- For scroll targeting: uses `document.getElementById('view').scrollTo(...)`
  — the `.view` element is the scroll container, not `window`. Will be
  switched to `element.scrollIntoView({block:'start'})` keyed on text label
  so card-specific shots stop depending on pixel offsets.
- Output → `/tmp/puppet/out/howto-*.png` → `cp` into
  `docs/howto/screenshots/` → `make sync-docs` mirrors into the embedded
  PWA tree.
- ImageMagick available for compositing arrows / callouts on top of the
  raw screenshots when an annotated callout is clearer than prose.

The puppeteer-core install at `/tmp/puppet` survives between runs; it is
not committed. The driver script itself will be moved to
`scripts/howto-shoot.mjs` and committed when the rebuild ships.

## Embedded-asset sync (already shipped)

- `Makefile sync-docs` rsync include list extended to `*.png`, `*.svg`,
  `*.jpg`, `*.gif`.
- `scripts/check-docs-sync.sh` mirrors the same include list so the
  pre-commit hook + CI guard accept image assets.

## Per-channel matrix

Every how-to keeps the existing reachability matrix at the bottom (CLI,
REST, MCP, chat, PWA). The PNG additions only enrich the PWA column.

## Sequencing

1. **Now (paused):** finish backlog — BL198 reopen, plan-doc refactor,
   recently-closed audit.
2. **Resume:** stage one how-to at a time. Each stage = 1 patch
   release with the rebuilt how-to + its screenshot set + any fixture
   seeding the captures depend on.
3. After all six are done, retire this plan doc.

## Questions deferred to operator

None. Approach confirmed 2026-04-26: seeded fixtures (no real runs),
code blocks for CLI / comms, PNGs for PWA only, ~15 – 20 per how-to.
