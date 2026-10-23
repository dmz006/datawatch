# Observer mobile fit — full-profile matrix reproduction (unfixed baseline)

Date: 2026-10-22 · tree: branch `automaton/dbd5000a` (unfixed — `internal/server/web/app.js` and `internal/server/web/style.css` were **not** modified)

## Goal

Reproduce the Observer-view mobile-fit matrix failures on the unfixed tree using the full-profile matrix (16 Observer states × 12 viewports × 2 themes × 3 zoom levels, plus 3 interaction loops), group the findings, and attribute every group to the CSS/JS source that causes it, so a follow-up fix task can work from this table.

## How

1. Build the daemon, start it on a random loopback port (temporary data dir, no TLS).
2. In headless Chromium (Playwright, DPR 1, forced scale factor 1) run:
   `node scripts/test-stories/pwa/mobile-fit.mjs --view observer --profile full`
   (state, viewport, theme and zoom asserted on every pass — see `mobile-fit-matrix.mjs`).
3. The library (`mobile-fit-lib.mjs`) checks per rendered frame:
   page-scroll, edge-cross, clipped-text (ellipsis + tooltip required to be exempt),
   tap-target (interactive elements ≥ 40×40px), overlap (placed sibling spans),
   and interaction loops (network request stacking + layout drift under refresh /
   section-collapse-expand / rerender-timers).
4. Findings are grouped by normalized `check|selector` so one CSS defect = one row.
   Evidence: `docs/testing/runs/observer-mobile-fit/repro-report.{json,txt}` and the
   per-scenario screenshots; raw run in the evidence directory.

## Result

**29,968 findings — every one of 284 scenario runs fails — in 68 groups.**
Breakdown: tap-target 28,354 · clipped-text 911 · overlap 700 · interaction 2 · page-scroll 1.

Every group below is a reproducible defect on the unfixed tree; the *cause* column
names the code that produces it. That table is the fix worklist.

## Findings

| group | check (findings) | selector (normalized) | extent | cause (unfixed tree) |
|---|---|---|---|---|
| G-20442ed3 | page-scroll (1) | `html` | scrollWidth 360 > viewport 360 (element inside scope crosses right edge); widest: #statsPanel > div:nth-child(1) > div.stat-card:nth-child(10) (361px wide), #statsPanel > div:nth-child(1) > div.stat-card:nth-child(10) > div.stat-label:nth-child(1) (339px wide), #websearchStatsCardBody (339px wide), #websearchStatsCardBody > div:nth-child(1) (339px wide), #websearchStatsCardBody > div:nth-child(2) (339px wide) | Page-level horizontal scroll: stats panels force `grid-template-columns:repeat(2,1fr)` with a 260px-min two-column Ollama card (`grid-column:span 2`), pushing the layout to 361px > 360px viewport. app.js:28031, 28112 |
| G-69e806cc | clipped-text (432) | `#memoryBrowserList > div.settings-row:nth(*) > div:nth(*) > div:nth(*)` | Δ688px overflow | Memory-browser row body is `max-height:60px;overflow:hidden;white-space:pre-wrap` with no ellipsis or tooltip — long strings clip (worst: scrollHeight 119 > 60). app.js:17452, 17490 |
| G-31b381fd | clipped-text (241) | `#nav > button.nav-btn:nth(*) > span.nav-icon:nth(*)` | Δ8px overflow | Bottom nav `.nav-icon` glyphs (20px) squeezed into 18px slots on phones — mobile `.nav-btn {flex:1;min-width:0}` leaves 18px icon box, no ellipsis or tooltip. style.css:338; @500px rule style.css:4485–4486 |
| G-9a2a7095 | clipped-text (47) | `#observerClusterList > div:nth(*)` | Δ379px overflow | Federated cluster rows with long node/peer paths overflow 245 vs 212 with no `word-break`/ellipsis. `renderObserverClusterList`, app.js:27864–27892 |
| G-9dddb071 | clipped-text (42) | `#auditEntries > div:nth(*) > div:nth(*)` | Δ169px overflow | Audit entry detail line is unbroken monospace `JSON.stringify(details)`; widest row (245px) exceeds the 236px column. app.js:34354 |
| G-a80c2e92 | clipped-text (34) | `#perSystemGrid > div:nth(*) > div:nth(*) > div:nth(*) > span:nth(*)` | Δ377px overflow | System-stat card name span has ellipsis but the row text (hostname / metric value) overflows 435 > 194 with no wrapping in a 2-col card. app.js:27279–27285 |
| G-5406eafe | clipped-text (34) | `…n:nth(*) > div.settings-section-title.settings-section-toggle:nth(*)` | Δ69px overflow | Section title bar (uppercase 11px title + docs link) is 150px wide in a 104px box at 200% zoom; no `word-break`. style.css:4084; app.js:8179 |
| G-3fce2c5d | clipped-text (32) | `#observerPeersList > div:nth(*)` | Δ379px overflow | Peer-list empty-state hint line "deploy `datawatch-stats …`" does not wrap in the 212px body (416px wide content). app.js:27570 |
| G-8f68ca67 | clipped-text (32) | `#statsPanel > div:nth(*) > div:nth(*) > div:nth(*)` | Δ15px overflow | Stat-card value text (e.g. "12965 / 12965 sessions" in `sessions-large`) overflows the 194px card in a 2-col grid — no `overflow-wrap`. app.js:27971, 28031 |
| G-9c1bbe7e | clipped-text (5) | `#observerPeersPanel > div:nth(*) > div:nth(*) > div:nth(*)` | Δ22px overflow | Peer-card detail row overflows the card at 200% zoom. `renderObserverPeersCard`, app.js:32231–32330 |
| G-6502e162 | clipped-text (3) | `#headerTitle` | Δ75px overflow | `.header-title {overflow:hidden}` has no `text-overflow:ellipsis`; "datawatch" + status chips clip at 130% zoom (scrollWidth-51 > client-39). style.css:165–175 |
| G-980a5fd2 | clipped-text (3) | `#commBackendsBlock > div:nth(*)` | Δ43px overflow | Comm-backends header line overflows 76px box at 200% zoom. `#commBackendsBlock`, app.js:31867 |
| G-756b3686 | clipped-text (2) | `#commBackendsStatus` | Δ24px overflow | Comm-backends status list row overflows 76px box at 200% zoom. `#commBackendsStatus`, app.js:31868 |
| G-faf17635 | clipped-text (1) | `#pluginsStatusList > div:nth(*)` | Δ16px overflow | Plugin-strip row (10px meta text) overflows its 52px box at 200% zoom. app.js:27470–27478 |
| G-fb1d37f8 | clipped-text (1) | `#acmeHealthBlock > div:nth(*)` | Δ16px overflow | ACME-health card line overflows 76px box at 200% zoom. `#acmeHealthBlock`, app.js:31814 |
| G-ce0f4988 | clipped-text (1) | `#envelopesBlock > div:nth(*)` | Δ3px overflow | Envelopes card line overflows 76px box at 200% zoom. `#envelopesBlock`, app.js:31819 |
| G-37cc0d29 | clipped-text (1) | `#settings-sec-memscopes > div:nth(*)` | Δ19px overflow | Memory-scope hint line overflows 76px box at 200% zoom. `#settings-sec-memscopes`, app.js:31903–31911 |
| G-0b199460 | tap-target (3374) | `#statsPanel > div:nth(*) > div:nth(*) > div:nth(*)` | 60×15px | Expandable session/channel rows inside the stats panel are plain clickable divs at 15px tall (`_expandedSessions`/`_expandedChannels` rows). app.js:28177, 28218 |
| G-ab12e474 | tap-target (3267) | `…gs-section-title.settings-section-toggle:nth(*) > a.docs-link:nth(*)` | 38×19px | Settings-section `.docs-link` is a text-only link with no padding (`.docs-link{text-decoration:none;opacity:.6}`) → ~19px tall. style.css:262–264; rendered by `settingsSectionHeader`/`defsLink`, app.js:8176 |
| G-fe0ab882 | tap-target (3060) | `…n:nth(*) > div.settings-section-title.settings-section-toggle:nth(*)` | 190×35px | Clickable section-header bars `.settings-section-title` (toggle, has `role="button"`/`onclick`) only `padding:10px 16px 6px` at 11px title font → 33px tall. style.css:4084–4100; `settingsSectionHeader`, app.js:8179 |
| G-cc54d58d | tap-target (2070) | `#observerPeersPanel > div:nth(*) > div:nth(*) > button.btn-icon:nth(*)` | 20×16px | `renderObserverPeersCard` `.btn-icon` buttons at 28×17px. app.js:32268–32330; style.css:3638 |
| G-2b7f2105 | tap-target (1990) | `#observerPeersList > div:nth(*) > button.btn-icon:nth(*)` | 20×16px | Peer row `.btn-icon` buttons (remove/refresh) at 28×17px — `.btn-icon {padding:5px}`. style.css:3638–3662, :3642; row at app.js:27576+ |
| G-c9747bf6 | tap-target (1782) | `#cooldownStatus > div:nth(*) > button.btn-secondary:nth(*)` | 47×37px | Cooldown "Release now" button at 57×37px (`.btn-secondary`, 11px inline). app.js:31987–31999; style.css:4236 |
| G-12393df6 | tap-target (1667) | `#observerPeersList > div:nth(*) > button.filter-toggle-btn:nth(*)` | 47×18px | Peer filter pills `.filter-toggle-btn {padding:3px 9px;font-size:11px}` → 47×18px. style.css:4863, 4878; rendered in `loadObserverPeers`, app.js:27563 |
| G-29f76add | tap-target (891) | `#memoryBrowserList > div.settings-row:nth(*) > button.btn-icon:nth(*)` | 27×27px | Memory-row `.btn-icon` icon buttons at 27×27px (`padding:2px`). app.js:17445–17455; style.css:3642 |
| G-6499cd76 | tap-target (874) | `#settings-sec-membrowser > div:nth(*) > button.btn-secondary:nth(*)` | 52×37px | Memory-browser `.btn-secondary` buttons at 69×39px, 1px under the 40px floor (`.btn-secondary {padding:7px 14px 8px;font-size:12px}`). style.css:4236–4255 |
| G-96b178ab | tap-target (603) | `…sec-memmaint > div:nth(*) > div:nth(*) > button.btn-secondary:nth(*)` | 105×37px | Memory-maintenance "Add" buttons at 105×37px (`.btn-secondary`). app.js:28354; style.css:4236 |
| G-182d37b6 | tap-target (594) | `#settings-sec-daemonlog > div:nth(*) > button.btn-secondary:nth(*)` | 63×37px | Daemon-log tail/copy controls at ~34px (`.btn-secondary`). app.js:32014–32030; style.css:4236 |
| G-3f4cac4d | tap-target (297) | `#headerHelpLink` | 27×29px | `#headerHelpLink` is an inline anchor with `height:26px` and 12px text → 27×29px. index.html:102 |
| G-20977e3f | tap-target (297) | `#settings-sec-memscopes > div:nth(*) > button.btn-secondary:nth(*)` | 64×37px | Memory-scope `.btn-secondary` buttons (e.g. "Save scope") at ~31px via the same `.btn-secondary` padding. style.css:4236; section at app.js:31903–31935 |
| G-8c154cfa | tap-target (297) | `#auditPanel > div:nth(*) > button.btn-secondary:nth(*)` | 59×37px | Audit "Load" button at ~31px (`.btn-secondary`). app.js:34344; style.css:4236 |
| G-34e9ec29 | tap-target (297) | `#memScopeSelect` | 146×39px | `#memScopeSelect` at 146×39px. app.js:31921; style.css:3951 |
| G-8008e9ca | tap-target (297) | `#analyticsRange` | 49×20px | `#analyticsRange` select at 49×20px. app.js:34277; style.css:3951 |
| G-01a3acda | tap-target (297) | `#auditLimitFilter` | 49×22px | `#auditLimitFilter` select at ~20px (`padding:3px 4px`, 11px font). app.js:34342 |
| G-25f58ef5 | tap-target (297) | `#memoryQuickAddTags` | 52×37px | `#memoryQuickAddTags` input at ~31px (`.form-input` padding, 11px font). app.js:31890; style.css:3951 |
| G-c6a8ba90 | tap-target (297) | `#memScopePRDID` | 100×37px | `#memScopePRDID` input at 104×31px (fixed `width:100px`). app.js:31929; style.css:3951 |
| G-506de02c | tap-target (297) | `#memScopeStoryID` | 100×37px | `#memScopeStoryID` input at ~31px. app.js:31930; style.css:3951 |
| G-557470b3 | tap-target (297) | `#memScopePersona` | 110×37px | `#memScopePersona` input at ~31px. app.js:31931; style.css:3951 |
| G-c147e779 | tap-target (297) | `#memScopeProject` | 120×37px | `#memScopeProject` input at ~31px. app.js:31932; style.css:3951 |
| G-e7fd60fd | tap-target (297) | `#cooldownReason` | 76×37px | `#cooldownReason` input at 236×37px. app.js:34093; style.css:3951 |
| G-cc4d63c1 | tap-target (297) | `#auditActorFilter` | 76×20px | `#auditActorFilter` input at ~20px (`padding:3px 6px`). app.js:34340 |
| G-26aaef56 | tap-target (297) | `#auditActionFilter` | 76×20px | `#auditActionFilter` input at 49×20px. app.js:34341 |
| G-6d841193 | tap-target (297) | `#kgEntityInput` | 30×37px | `#kgEntityInput` at 158×37px. app.js:34429; style.css:3951 |
| G-84643ef6 | tap-target (297) | `#kgSubject` | 30×37px | `#kgSubject` at 46×37px. app.js:34433; style.css:3951 |
| G-3cfa91ba | tap-target (297) | `#kgPredicate` | 30×37px | `#kgPredicate` at ~37px. app.js:34434; style.css:3951 |
| G-1a3c6ff1 | tap-target (297) | `#kgObject` | 30×37px | `#kgObject` at 44×31px. app.js:34435; style.css:3951 |
| G-25a994a0 | tap-target (297) | `#headerAlertPill` | 50×21px | `#headerAlertPill` at 50×21px — inline `height:26px` pill with 11px text. index.html:116 |
| G-90e1cd98 | tap-target (294) | `#channelBridgeBlock > div:nth(*) > a:nth(*)` | 23×15px | "help" anchor inside the MCP channel-bridge header bar → 23×15px. app.js:31849 |
| G-3593ef37 | tap-target (294) | `#channelDiagBlock > div:nth(*) > button:nth(*)` | 45×15px | Channel-diagnostics "refresh" button `font-size:10px;padding:1px 6px` → 45×15px. app.js:31863 |
| G-9d3072a6 | tap-target (292) | `#kgPanel > div:nth(*) > button.btn-secondary:nth(*)` | 85×37px | Knowledge Panel "Query" button at 85×37px (`.btn-secondary`). app.js:34428; style.css:4236 |
| G-1e7b545d | tap-target (275) | `#channelBridgeBlock > div:nth(*)` | 216×21px | "MCP channel bridge" header bar is a clickable 21px-tall div (11px text, 6px bottom padding, `onclick`). app.js:31847 |
| G-9f85f18a | tap-target (273) | `#channelDiagBlock > div:nth(*)` | 291×21px | "Channel bridge diagnostics" header bar is a clickable 36px-tall div. app.js:31862 |
| G-14fdde2f | tap-target (262) | `#statsPanel > div:nth(*) > div:nth(*) > a:nth(*)` | 86×14px | "N sessions in store" is an inline anchor in the Session Statistics card with no tap padding → 14px tall. app.js:28195 |
| G-7fe448cb | tap-target (262) | `#websearchStatsCardBody > button:nth(*)` | 49×17px | WebSearch "History" button uses `font-size:10px;padding:2px 8px` → 49×17px. app.js:27938 |
| G-be8b4967 | tap-target (226) | `#peerStaleBadge` | 13×15px | `#peerStaleBadge` is an inline click target at 17×15px (`font-size:9px;padding:1px 4px`). index.html:175 |
| G-92888624 | tap-target (144) | `#memorySinceFilter` | 114×39px | `#memorySinceFilter` select at 114×39px. app.js:31901; style.css:3951 |
| G-104e495e | tap-target (119) | `#headerFullscreenBtn` | 24×28px | `#headerFullscreenBtn` (`.btn-icon`) at 24×28px — `padding:6px 14px 8px` around a 16px icon. style.css:3638–3674; index.html:90 |
| G-316ddee2 | tap-target (42) | `#memoryRoleFilter` | 96×39px | `#memoryRoleFilter` select at 96×39px — `.form-select` `padding:7px 10px` with 11px font. app.js:31894; style.css:3951 |
| G-bf26d4e6 | tap-target (34) | `…> div:nth(*) > div:nth(*) > div:nth(*) > button.btn-secondary:nth(*)` | 63×37px | Detection-pattern "Add" row buttons at 74×37px (`font-size:10px` inline on `.btn-secondary`). app.js:28354; style.css:4236 |
| G-50d22ee3 | tap-target (17) | `#memSweepDays` | 80×37px | `#memSweepDays` number input (`width:80px;font-size:11px`) at 29px tall. app.js:31947; style.css:3951 |
| G-ff3cb8eb | tap-target (4) | `#memoryQuickAddInput` | 30×43px | Quick-add memory row at 200% zoom: tag input + buttons leave the flex input only 30px wide. app.js:31880–31891 |
| G-f14d2c0c | tap-target (1) | `#channelDiagStatus > details:nth(*) > summary:nth(*)` | 212×15px | Channel-diagnostics hint `<summary>` (10px text, no padding) at 15px tall once diagnostics render. app.js:27165–27178 |
| G-6419b8d1 | overlap (678) | `#pluginsStatusList > div:nth(*) > span:nth(*)` | sibling overlap with #pluginsStatusList > div shared | Plugin-strip rows: inline status spans share one line box (`.session-plugin-widget` line) → sibling overlap flagged on 348 scenarios. app.js:27468–27479; style.css:3070 |
| G-300e55cc | overlap (18) | `…st > div.settings-row:nth(*) > div:nth(*) > div:nth(*) > span:nth(*)` | sibling overlap with #memoryBrowserList > div.settings-row shared | Memory-row title and tag-chip spans (`_renderMemoryTagChips`) share the content box → 875px² overlap at 130% zoom. app.js:17463–17493 |
| G-80adcd2a | overlap (2) | `#channelDiagStatus > div:nth(*) > span:nth(*)` | sibling overlap with #channelDiagStatus > div shared | Diagnostics session row status spans share the line box → 171px² sibling overlap. app.js:27152–27178 |
| G-02f98b0e | overlap (2) | `#pluginsStatusList > div:nth(*) > strong:nth(*)` | sibling overlap with #pluginsStatusList > div shared | Plugin-strip `<strong>` name span overlaps its status span in one line box (64px², 100%) at 200% / 390×844. app.js:27470–27478 |
| G-4f4b0701 | interaction (1) | `#perSystemGrid` | auto-refresh stacking: /api/stats requests grew from 2 (befo | Rerender loop keeps firing `/api/stats` (2 → 44 requests): `loadStatsPanel()` re-runs grid + 11 sub-loaders, each polling independently, layered on the 1s `loadSessionStats` tick. app.js:26960, 27255, 27907–27911, 4670 |
| G-929e9bfc | interaction (1) | `#settings-sec-stats` | layout drifted after the rerender loop: 129 layout finding(s | Stats section re-renders are not layout-idempotent: placeholder ↔ content toggle (via `loadingEyeBlock`) shifts layout, 129 → 131 layout findings. app.js:26963, 26977, 19063 |


## Reproduction

    go build -o datawatch ./cmd/datawatch/
    ./datawatch    # temp data dir, random loopback port, no TLS
    node scripts/test-stories/pwa/mobile-fit.mjs --view observer --profile full

Then diff re-runs against the two report files referenced above. The named
`system-stats-phone` scenario (360×640 portrait, dark, z100) always fails on the
unfixed tree with the page-scroll + 5 tap-target findings in group G-20442ed3 and
friends.

## Fix plan (for the follow-up task on the fixed tree)

1. **Page overflow (G-20442ed3)** — the stats grid's `repeat(2,1fr)` + the 260px-min
   Ollama card (app.js:28031, 28112) push the layout past 360px. Make sub-480px
   viewports single-column or drop the min-width.
2. **Tap targets (41 groups, 28,354 findings)** — global floor for buttons/links/pills:
   raise `.btn-secondary`/`.btn-icon`/`.filter-toggle-btn`/form-controls and the inline
   header controls to ≥ 40px hit area (padding or `min-height`), without changing visual
   size.
3. **Clipped text (16 groups)** — add `overflow:hidden; text-overflow:ellipsis` + a
   `title`/tool-tip (or `word-break:break-word` where wrapping is intended) to the
   named lines (memory rows, audit details, peer/cluster hints, stat values, header
   title, section titles, card lines).
4. **Overlap (4 groups)** — inline spans in plugin rows, diagnostics rows and memory
   rows share a line box; give them `white-space:nowrap` + a z-context or wrap in
   blocks.
5. **Interaction (2 groups)** — the 1s `loadSessionStats` + per-section load loop
   stacks requests (2→44) and leaves the stats section at 129→131 layout findings;
   coalesce the sub-loaders onto one tick and keep re-renders layout-idempotent.

Acceptance: after the fix, re-run the same matrix on the fixed tree; every group in
this table must go to zero findings (or an explicit, documented exemption).
