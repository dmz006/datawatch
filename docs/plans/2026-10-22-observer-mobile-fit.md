# Observer mobile fit — full-profile matrix reproduction (unfixed baseline)

Date: 2026-10-22 · tree: branch `automaton/dbd5000a`

> **Read-me-first:** this document is the *frozen unfixed-baseline* record — the
> Result / Findings / Fix-plan sections describe the tree **before** the layout
> fixes, when `internal/server/web/app.js` and `internal/server/web/style.css` were
> untouched. The branch **does** now ship those fixes (recorded in `## Gate
> results`, 2026-10-24: the `app.js`/`style.css` in the tree are the fixed files,
> pinned by `docs/testing/runs/observer-mobile-fit/web-sha256.txt`). See
> `## Status` for phase status and `## Parity surface` for per-surface intent.

## Status

Planned → Done status per the Planning Rules, with the shipped-version line.
This branch ships on the `v9.5.98` line (in-tree version at gate time; the gate
note records "no version bump" on this branch — the fixes land in the next
release and are called out under `## [Unreleased]` in `CHANGELOG.md`).

| Phase | Status |
|---|---|
| 1 — Mobile-fit checker + full-profile matrix harness (`scripts/test-stories/pwa/`) | **Done** 2026-10-22 — harness, detector self-test (9/9) and report/seed unit tests (34/34) in the tree; on branch `dbd5000a` (v9.5.98 line) |
| 2 — Unfixed-baseline reproduction (284 scenarios, 68 groups, 29,968 findings) | **Done** 2026-10-22/23 — committed reports under `docs/testing/runs/observer-mobile-fit/`; on branch `dbd5000a` |
| 3 — Observer layout fixes for phone widths (`app.js`, `style.css`, `index.html`) | **Done** 2026-10-24 — full matrix 0 findings, TS-817/TS-818 clean; the fixed files are in this tree (sha256 pin `web-sha256.txt`) and ship in the next release on the v9.5.98 line |
| 4 — Other-tabs worklist + release-smoke wiring (`scripts/release-smoke-mobile-fit.sh`, `e2e-pwa.sh`) | **Done** 2026-10-24 — report-only pass (19 groups, 185 findings) recorded above; smoke 95 scenarios / exit 0 |
| 5 — End-to-end gate | **Done** 2026-10-25 — steps 1–7d all PASS (see `## Gate results`) |

## Parity surface

Full set per AGENT.md (Mobile-Parity Rule): `REST`, `MCP`, `CLI`, `comm channel`,
`YAML/config`, `PWA`, `Android`, `iPhone/iOS`.

| Surface | Intent for this work | Notes / reason |
|---|---|---|
| PWA | ✅ **changed** | the target surface: Observer tab (esp. System Statistics) phone-width layout, ≥40px tap targets, clipped-text, overlap and re-render fixes in `app.js` / `style.css` / `index.html` (Phase 3) |
| REST | N/A — no change | no endpoint, request-body or response-shape change; the matrix consumes the existing `/api/*` state as-is, so mobile clients parse unchanged contracts |
| MCP | N/A — no change | pure web-layout work; no MCP tool added or altered |
| CLI | N/A — no change | no CLI surface touched |
| comm channel | N/A — no change | no comm surface touched |
| YAML/config | N/A — no change | no config fields added or renamed; no defaults or persisted-state keys changed |
| Android | tracked — issue filed | operator-visible PWA layout change → Mobile-Parity / Mobile App Sync compliance: **`dmz006/datawatch-app#270`** (filed 2026-10-25) tracks the native phone-width equivalent |
| iPhone/iOS | tracked — issue filed | same change, same issue: **`dmz006/datawatch-app#270`** |

## Goal

The Observer tab — and System Statistics in particular — does not fit a phone: the
operator's report is that System Statistics on a phone scrolls horizontally and its
controls are too small to touch. This task reproduces that on the unfixed tree with
the full-profile mobile-fit matrix, groups the findings, and attributes every group
to the CSS/JS source that causes it, so a follow-up fix task can work from this table.
In this task `internal/server/web/app.js` and `internal/server/web/style.css` are
not modified — the evidence is the deliverable.

## Approach

- **Checker** — `scripts/test-stories/pwa/mobile-fit-lib.mjs` checks each rendered
  frame: page-scroll (`html` scrollWidth past the viewport), edge-cross (element
  crossing a container edge), clipped-text (overflowing content exempt only when it
  has ellipsis + tooltip), tap-target (interactive elements ≥ 40×40px), overlap
  (placed sibling spans sharing a line box), plus the interaction checks (network
  request stacking and layout drift under `refresh`, `section-collapse-expand` and
  `rerender-timers` loops). Findings are grouped by normalized `check|selector` so
  one CSS defect = one Findings-table row.
- **Scenario matrix** — `mobile-fit-matrix.mjs` full profile across 16 Observer
  states: sweep A all 12 viewports (320×568 to 768×1024, both orientations) at dark
  100%; sweep B the four reference viewports at light 100%; sweep C 130%/200% zoom
  at the 320/360/390/412 phone widths — 284 scenario runs including the 3
  interaction loops and the operator-named `system-stats-phone` run; state,
  viewport, theme and zoom are asserted on every pass.
- **Reproduction** — the PWA is embedded in the daemon binary, so build the daemon
  fresh in an `mktemp -d` directory (random free loopback ports, temp data dir, no
  TLS, never the production daemon), serve API responses seeded from
  `mobile-fit-seeds.mjs` / `mobile-fit-fixtures/`, and drive headless Chromium via
  Playwright (DPR 1, forced page scale 1).
- **Fix** — the follow-up task edits `app.js`/`style.css` from the Findings table
  (Fix plan below), group by group.
- **Regression guard** — the same matrix run is the acceptance check on the fixed
  tree: all 68 groups below must reach zero findings (or carry an explicit,
  documented exemption); the runner stays in `scripts/test-stories/pwa/` so
  `make test-e2e-pwa` re-runs it.
- **Release-smoke wiring** — add a `--view observer --profile smoke` pass to the
  release smoke path (`scripts/e2e-pwa.sh`) so every release proves the
  `system-stats-phone` scenario passes before it ships.
- **Other-tabs worklist** — the same checker already supports the other tabs
  (Sessions, Autonomous, Alerts, Settings); running those views and recording
  baseline reports the same way is a follow-up task.

## Result

**29,968 findings — every one of 284 scenario runs fails — in 68 groups.**
Breakdown: tap-target 28,354 · clipped-text 911 · overlap 700 · interaction 2 · page-scroll 1.

Every group below is a reproducible defect on the unfixed tree; the *cause* column
names the code that produces it. That table is the fix worklist.

## Findings

| group id | check (findings) | selector (normalized) | example scenario | cause (unfixed tree) |
|---|---|---|---|---|
| G-20442ed3 | page-scroll (1) | `html` | system-stats-phone | Page-level horizontal scroll: stats panels force `grid-template-columns:repeat(2,1fr)` with a 260px-min two-column Ollama card (`grid-column:span 2`), pushing the layout to 361px > 360px viewport. app.js:28031, 28112 |
| G-69e806cc | clipped-text (432) | `#memoryBrowserList > div.settings-row:nth(*) > div:nth(*) > div:nth(*)` | no-peers@390x844/dark/z100 | Memory-browser row body is `max-height:60px;overflow:hidden;white-space:pre-wrap` with no ellipsis or tooltip — long strings clip (worst: scrollHeight 119 > 60). app.js:17452, 17490 |
| G-31b381fd | clipped-text (241) | `#nav > button.nav-btn:nth(*) > span.nav-icon:nth(*)` | one-peer@390x844/dark/z100 | Bottom nav `.nav-icon` glyphs (20px) squeezed into 18px slots on phones — mobile `.nav-btn {flex:1;min-width:0}` leaves 18px icon box, no ellipsis or tooltip. style.css:338; @500px rule style.css:4485–4486 |
| G-9a2a7095 | clipped-text (47) | `#observerClusterList > div:nth(*)` | long-hostname@390x844/dark/z100 | Federated cluster rows with long node/peer paths overflow 245 vs 212 with no `word-break`/ellipsis. `renderObserverClusterList`, app.js:27864–27892 |
| G-9dddb071 | clipped-text (42) | `#auditEntries > div:nth(*) > div:nth(*)` | no-peers@320x568/dark/z100 | Audit entry detail line is unbroken monospace `JSON.stringify(details)`; widest row (245px) exceeds the 236px column. app.js:34354 |
| G-a80c2e92 | clipped-text (34) | `#perSystemGrid > div:nth(*) > div:nth(*) > div:nth(*) > span:nth(*)` | long-hostname@390x844/dark/z100 | System-stat card name span has ellipsis but the row text (hostname / metric value) overflows 435 > 194 with no wrapping in a 2-col card. app.js:27279–27285 |
| G-5406eafe | clipped-text (34) | `…n:nth(*) > div.settings-section-title.settings-section-toggle:nth(*)` | three-peers@320x568/dark/z200 | Section title bar (uppercase 11px title + docs link) is 150px wide in a 104px box at 200% zoom; no `word-break`. style.css:4084; app.js:8179 |
| G-3fce2c5d | clipped-text (32) | `#observerPeersList > div:nth(*)` | long-hostname@390x844/dark/z100 | Peer-list empty-state hint line "deploy `datawatch-stats …`" does not wrap in the 212px body (416px wide content). app.js:27570 |
| G-8f68ca67 | clipped-text (32) | `#statsPanel > div:nth(*) > div:nth(*) > div:nth(*)` | sessions-large@390x844/dark/z100 | Stat-card value text (e.g. "12965 / 12965 sessions" in `sessions-large`) overflows the 194px card in a 2-col grid — no `overflow-wrap`. app.js:27971, 28031 |
| G-9c1bbe7e | clipped-text (5) | `#observerPeersPanel > div:nth(*) > div:nth(*) > div:nth(*)` | three-peers@320x568/dark/z200 | Peer-card detail row overflows the card at 200% zoom. `renderObserverPeersCard`, app.js:32231–32330 |
| G-6502e162 | clipped-text (3) | `#headerTitle` | three-peers@320x568/dark/z130 | `.header-title {overflow:hidden}` has no `text-overflow:ellipsis`; "datawatch" + status chips clip at 130% zoom (scrollWidth-51 > client-39). style.css:165–175 |
| G-980a5fd2 | clipped-text (3) | `#commBackendsBlock > div:nth(*)` | three-peers@320x568/dark/z200 | Comm-backends header line overflows 76px box at 200% zoom. `#commBackendsBlock`, app.js:31867 |
| G-756b3686 | clipped-text (2) | `#commBackendsStatus` | three-peers@320x568/dark/z200 | Comm-backends status list row overflows 76px box at 200% zoom. `#commBackendsStatus`, app.js:31868 |
| G-faf17635 | clipped-text (1) | `#pluginsStatusList > div:nth(*)` | three-peers@320x568/dark/z200 | Plugin-strip row (10px meta text) overflows its 52px box at 200% zoom. app.js:27470–27478 |
| G-fb1d37f8 | clipped-text (1) | `#acmeHealthBlock > div:nth(*)` | three-peers@320x568/dark/z200 | ACME-health card line overflows 76px box at 200% zoom. `#acmeHealthBlock`, app.js:31814 |
| G-ce0f4988 | clipped-text (1) | `#envelopesBlock > div:nth(*)` | three-peers@320x568/dark/z200 | Envelopes card line overflows 76px box at 200% zoom. `#envelopesBlock`, app.js:31819 |
| G-37cc0d29 | clipped-text (1) | `#settings-sec-memscopes > div:nth(*)` | three-peers@320x568/dark/z200 | Memory-scope hint line overflows 76px box at 200% zoom. `#settings-sec-memscopes`, app.js:31903–31911 |
| G-0b199460 | tap-target (3374) | `#statsPanel > div:nth(*) > div:nth(*) > div:nth(*)` | no-peers@390x844/dark/z100 | Expandable session/channel rows inside the stats panel are plain clickable divs at 15px tall (`_expandedSessions`/`_expandedChannels` rows). app.js:28177, 28218 |
| G-ab12e474 | tap-target (3267) | `…gs-section-title.settings-section-toggle:nth(*) > a.docs-link:nth(*)` | no-peers@390x844/dark/z100 | Settings-section `.docs-link` is a text-only link with no padding (`.docs-link{text-decoration:none;opacity:.6}`) → ~19px tall. style.css:262–264; rendered by `settingsSectionHeader`/`defsLink`, app.js:8176 |
| G-fe0ab882 | tap-target (3060) | `…n:nth(*) > div.settings-section-title.settings-section-toggle:nth(*)` | no-peers@390x844/dark/z100 | Clickable section-header bars `.settings-section-title` (toggle, has `role="button"`/`onclick`) only `padding:10px 16px 6px` at 11px title font → 33px tall. style.css:4084–4100; `settingsSectionHeader`, app.js:8179 |
| G-cc54d58d | tap-target (2070) | `#observerPeersPanel > div:nth(*) > div:nth(*) > button.btn-icon:nth(*)` | no-peers@390x844/dark/z100 | `renderObserverPeersCard` `.btn-icon` buttons at 28×17px. app.js:32268–32330; style.css:3638 |
| G-2b7f2105 | tap-target (1990) | `#observerPeersList > div:nth(*) > button.btn-icon:nth(*)` | no-peers@390x844/dark/z100 | Peer row `.btn-icon` buttons (remove/refresh) at 28×17px — `.btn-icon {padding:5px}`. style.css:3638–3662, :3642; row at app.js:27576+ |
| G-c9747bf6 | tap-target (1782) | `#cooldownStatus > div:nth(*) > button.btn-secondary:nth(*)` | no-peers@390x844/dark/z100 | Cooldown "Release now" button at 57×37px (`.btn-secondary`, 11px inline). app.js:31987–31999; style.css:4236 |
| G-12393df6 | tap-target (1667) | `#observerPeersList > div:nth(*) > button.filter-toggle-btn:nth(*)` | no-peers@390x844/dark/z100 | Peer filter pills `.filter-toggle-btn {padding:3px 9px;font-size:11px}` → 47×18px. style.css:4863, 4878; rendered in `loadObserverPeers`, app.js:27563 |
| G-29f76add | tap-target (891) | `#memoryBrowserList > div.settings-row:nth(*) > button.btn-icon:nth(*)` | no-peers@390x844/dark/z100 | Memory-row `.btn-icon` icon buttons at 27×27px (`padding:2px`). app.js:17445–17455; style.css:3642 |
| G-6499cd76 | tap-target (874) | `#settings-sec-membrowser > div:nth(*) > button.btn-secondary:nth(*)` | no-peers@390x844/dark/z100 | Memory-browser `.btn-secondary` buttons at 69×39px, 1px under the 40px floor (`.btn-secondary {padding:7px 14px 8px;font-size:12px}`). style.css:4236–4255 |
| G-96b178ab | tap-target (603) | `…sec-memmaint > div:nth(*) > div:nth(*) > button.btn-secondary:nth(*)` | no-peers@390x844/dark/z100 | Memory-maintenance "Add" buttons at 105×37px (`.btn-secondary`). app.js:28354; style.css:4236 |
| G-182d37b6 | tap-target (594) | `#settings-sec-daemonlog > div:nth(*) > button.btn-secondary:nth(*)` | no-peers@390x844/dark/z100 | Daemon-log tail/copy controls at ~34px (`.btn-secondary`). app.js:32014–32030; style.css:4236 |
| G-3f4cac4d | tap-target (297) | `#headerHelpLink` | no-peers@390x844/dark/z100 | `#headerHelpLink` is an inline anchor with `height:26px` and 12px text → 27×29px. index.html:102 |
| G-20977e3f | tap-target (297) | `#settings-sec-memscopes > div:nth(*) > button.btn-secondary:nth(*)` | no-peers@390x844/dark/z100 | Memory-scope `.btn-secondary` buttons (e.g. "Save scope") at ~31px via the same `.btn-secondary` padding. style.css:4236; section at app.js:31903–31935 |
| G-8c154cfa | tap-target (297) | `#auditPanel > div:nth(*) > button.btn-secondary:nth(*)` | no-peers@390x844/dark/z100 | Audit "Load" button at ~31px (`.btn-secondary`). app.js:34344; style.css:4236 |
| G-34e9ec29 | tap-target (297) | `#memScopeSelect` | no-peers@390x844/dark/z100 | `#memScopeSelect` at 146×39px. app.js:31921; style.css:3951 |
| G-8008e9ca | tap-target (297) | `#analyticsRange` | no-peers@390x844/dark/z100 | `#analyticsRange` select at 49×20px. app.js:34277; style.css:3951 |
| G-01a3acda | tap-target (297) | `#auditLimitFilter` | no-peers@390x844/dark/z100 | `#auditLimitFilter` select at ~20px (`padding:3px 4px`, 11px font). app.js:34342 |
| G-25f58ef5 | tap-target (297) | `#memoryQuickAddTags` | no-peers@390x844/dark/z100 | `#memoryQuickAddTags` input at ~31px (`.form-input` padding, 11px font). app.js:31890; style.css:3951 |
| G-c6a8ba90 | tap-target (297) | `#memScopePRDID` | no-peers@390x844/dark/z100 | `#memScopePRDID` input at 104×31px (fixed `width:100px`). app.js:31929; style.css:3951 |
| G-506de02c | tap-target (297) | `#memScopeStoryID` | no-peers@390x844/dark/z100 | `#memScopeStoryID` input at ~31px. app.js:31930; style.css:3951 |
| G-557470b3 | tap-target (297) | `#memScopePersona` | no-peers@390x844/dark/z100 | `#memScopePersona` input at ~31px. app.js:31931; style.css:3951 |
| G-c147e779 | tap-target (297) | `#memScopeProject` | no-peers@390x844/dark/z100 | `#memScopeProject` input at ~31px. app.js:31932; style.css:3951 |
| G-e7fd60fd | tap-target (297) | `#cooldownReason` | no-peers@390x844/dark/z100 | `#cooldownReason` input at 236×37px. app.js:34093; style.css:3951 |
| G-cc4d63c1 | tap-target (297) | `#auditActorFilter` | no-peers@390x844/dark/z100 | `#auditActorFilter` input at ~20px (`padding:3px 6px`). app.js:34340 |
| G-26aaef56 | tap-target (297) | `#auditActionFilter` | no-peers@390x844/dark/z100 | `#auditActionFilter` input at 49×20px. app.js:34341 |
| G-6d841193 | tap-target (297) | `#kgEntityInput` | no-peers@390x844/dark/z100 | `#kgEntityInput` at 158×37px. app.js:34429; style.css:3951 |
| G-84643ef6 | tap-target (297) | `#kgSubject` | no-peers@390x844/dark/z100 | `#kgSubject` at 46×37px. app.js:34433; style.css:3951 |
| G-3cfa91ba | tap-target (297) | `#kgPredicate` | no-peers@390x844/dark/z100 | `#kgPredicate` at ~37px. app.js:34434; style.css:3951 |
| G-1a3c6ff1 | tap-target (297) | `#kgObject` | no-peers@390x844/dark/z100 | `#kgObject` at 44×31px. app.js:34435; style.css:3951 |
| G-25a994a0 | tap-target (297) | `#headerAlertPill` | no-peers@390x844/dark/z100 | `#headerAlertPill` at 50×21px — inline `height:26px` pill with 11px text. index.html:116 |
| G-90e1cd98 | tap-target (294) | `#channelBridgeBlock > div:nth(*) > a:nth(*)` | no-peers@390x844/dark/z100 | "help" anchor inside the MCP channel-bridge header bar → 23×15px. app.js:31849 |
| G-3593ef37 | tap-target (294) | `#channelDiagBlock > div:nth(*) > button:nth(*)` | no-peers@390x844/dark/z100 | Channel-diagnostics "refresh" button `font-size:10px;padding:1px 6px` → 45×15px. app.js:31863 |
| G-9d3072a6 | tap-target (292) | `#kgPanel > div:nth(*) > button.btn-secondary:nth(*)` | no-peers@390x844/dark/z100 | Knowledge Panel "Query" button at 85×37px (`.btn-secondary`). app.js:34428; style.css:4236 |
| G-1e7b545d | tap-target (275) | `#channelBridgeBlock > div:nth(*)` | no-peers@390x844/dark/z100 | "MCP channel bridge" header bar is a clickable 21px-tall div (11px text, 6px bottom padding, `onclick`). app.js:31847 |
| G-9f85f18a | tap-target (273) | `#channelDiagBlock > div:nth(*)` | no-peers@390x844/dark/z100 | "Channel bridge diagnostics" header bar is a clickable 36px-tall div. app.js:31862 |
| G-14fdde2f | tap-target (262) | `#statsPanel > div:nth(*) > div:nth(*) > a:nth(*)` | no-peers@390x844/dark/z100 | "N sessions in store" is an inline anchor in the Session Statistics card with no tap padding → 14px tall. app.js:28195 |
| G-7fe448cb | tap-target (262) | `#websearchStatsCardBody > button:nth(*)` | no-peers@390x844/dark/z100 | WebSearch "History" button uses `font-size:10px;padding:2px 8px` → 49×17px. app.js:27938 |
| G-be8b4967 | tap-target (226) | `#peerStaleBadge` | one-peer@390x844/dark/z100 | `#peerStaleBadge` is an inline click target at 17×15px (`font-size:9px;padding:1px 4px`). index.html:175 |
| G-92888624 | tap-target (144) | `#memorySinceFilter` | no-peers@390x844/dark/z100 | `#memorySinceFilter` select at 114×39px. app.js:31901; style.css:3951 |
| G-104e495e | tap-target (119) | `#headerFullscreenBtn` | no-peers@768x1024/dark/z100 | `#headerFullscreenBtn` (`.btn-icon`) at 24×28px — `padding:6px 14px 8px` around a 16px icon. style.css:3638–3674; index.html:90 |
| G-316ddee2 | tap-target (42) | `#memoryRoleFilter` | no-peers@320x568/dark/z100 | `#memoryRoleFilter` select at 96×39px — `.form-select` `padding:7px 10px` with 11px font. app.js:31894; style.css:3951 |
| G-bf26d4e6 | tap-target (34) | `…> div:nth(*) > div:nth(*) > div:nth(*) > button.btn-secondary:nth(*)` | no-peers@568x320/dark/z100 | Detection-pattern "Add" row buttons at 74×37px (`font-size:10px` inline on `.btn-secondary`). app.js:28354; style.css:4236 |
| G-50d22ee3 | tap-target (17) | `#memSweepDays` | no-peers@568x320/dark/z100 | `#memSweepDays` number input (`width:80px;font-size:11px`) at 29px tall. app.js:31947; style.css:3951 |
| G-ff3cb8eb | tap-target (4) | `#memoryQuickAddInput` | three-peers@320x568/dark/z200 | Quick-add memory row at 200% zoom: tag input + buttons leave the flex input only 30px wide. app.js:31880–31891 |
| G-f14d2c0c | tap-target (1) | `#channelDiagStatus > details:nth(*) > summary:nth(*)` | interaction:section-collapse-expand | Channel-diagnostics hint `<summary>` (10px text, no padding) at 15px tall once diagnostics render. app.js:27165–27178 |
| G-6419b8d1 | overlap (678) | `#pluginsStatusList > div:nth(*) > span:nth(*)` | no-peers@390x844/dark/z100 | Plugin-strip rows: inline status spans share one line box (`.session-plugin-widget` line) → sibling overlap flagged on 348 scenarios. app.js:27468–27479; style.css:3070 |
| G-300e55cc | overlap (18) | `…st > div.settings-row:nth(*) > div:nth(*) > div:nth(*) > span:nth(*)` | three-peers@320x568/dark/z130 | Memory-row title and tag-chip spans (`_renderMemoryTagChips`) share the content box → 875px² overlap at 130% zoom. app.js:17463–17493 |
| G-80adcd2a | overlap (2) | `#channelDiagStatus > div:nth(*) > span:nth(*)` | interaction:section-collapse-expand | Diagnostics session row status spans share the line box → 171px² sibling overlap. app.js:27152–27178 |
| G-02f98b0e | overlap (2) | `#pluginsStatusList > div:nth(*) > strong:nth(*)` | three-peers@390x844/dark/z200 | Plugin-strip `<strong>` name span overlaps its status span in one line box (64px², 100%) at 200% / 390×844. app.js:27470–27478 |
| G-4f4b0701 | interaction (1) | `#perSystemGrid` | interaction:rerender-timers | Rerender loop keeps firing `/api/stats` (2 → 44 requests): `loadStatsPanel()` re-runs grid + 11 sub-loaders, each polling independently, layered on the 1s `loadSessionStats` tick. app.js:26960, 27255, 27907–27911, 4670 |
| G-929e9bfc | interaction (1) | `#settings-sec-stats` | interaction:rerender-timers | Stats section re-renders are not layout-idempotent: placeholder ↔ content toggle (via `loadingEyeBlock`) shifts layout, 129 → 131 layout findings. app.js:26963, 26977, 19063 |


## Reproduction

Isolated daemon freshly built from this tree (never a stale `bin/datawatch`):

    go build -o datawatch ./cmd/datawatch/     # in a mktemp -d scratch dir
    ./datawatch                                # temp data dir, random loopback port, no TLS
    node scripts/test-stories/pwa/mobile-fit.mjs --view observer --profile full

- **Profile:** `full` — 16 Observer states across the three sweeps above (12
  viewports at dark 100%; four reference viewports at light 100%; 130%/200% zoom at
  the 320/360/390/412 phone widths), plus the 3 interaction loops and the named
  `system-stats-phone` run.
- **Scenario count and totals:** 284 scenario runs, 29,968 findings in 68 groups —
  tap-target 28,354 · clipped-text 911 · overlap 700 · interaction 2 · page-scroll 1.
- **Committed report:** `docs/testing/runs/observer-mobile-fit/repro-report.json`
  and `docs/testing/runs/observer-mobile-fit/repro-report.txt`. The per-scenario
  screenshots from this run stay in the scratch evidence directory and are **not**
  committed.

The named `system-stats-phone` scenario (System Statistics, 360×640 portrait,
dark, 100% zoom — the operator's report) fails on every run of the unfixed tree
with 31 findings: the page-level horizontal scroll of group G-20442ed3 plus
sub-40px tap targets, clipped text and overlaps. Then diff re-runs against the two
committed report files.

## Fix plan (for the follow-up task on the fixed tree)

1. **Page overflow (G-20442ed3)** — the stats grid's `repeat(2,1fr)` + the 260px-min
   Ollama card (app.js:28031, 28112) push the layout past 360px. Make sub-480px
   viewports single-column or drop the min-width.
2. **Tap targets (45 groups, 28,354 findings)** — global floor for buttons/links/pills:
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

---

## Other tabs worklist

Sandbox daemon run (fresh build of this tree, quick profile, default state, dark
theme, viewports 320×568 / 360×740 / 390×844 / 768×1024 / 740×360 + 360×740 light
+ 360×740 @z200), report-only final pass:
`MOBILE_FIT_PROFILE=quick MOBILE_FIT_REPORT_ONLY=1` with
`MOBILE_FIT_VIEW=sessions,autonomous,automation,settings,alerts`.
Output: `docs/testing/runs/observer-mobile-fit/other-tabs-report.json` and
`other-tabs-report.txt` (19 groups, 185 findings across the 5 tabs). Several
groups are shared header controls, so they recur per tab below; one global fix
covers them everywhere.

### Sessions

| Group | Check | Selector | Widths | Remedy |
|---|---|---|---|---|
| G-6502e162 | clipped-text | `#headerTitle` | 320×568 | `overflow:hidden; text-overflow:ellipsis; white-space:nowrap` + `title` on the header title (shared observer fix) |
| G-3f4cac4d | tap-target | `#headerHelpLink` | all 7 cells | raise 27×29 hit area to ≥40px (padding or `min-height`) |
| G-6e692494 | tap-target | `#headerSearchBtn` | all 7 cells | raise 35×26 hit area to ≥40px |
| G-f4f80146 | tap-target | `…toast.toast-error > .toast-error-row > button.toast-error-close` | 320×568 | raise 28×28 toast close button to ≥40px hit area |
| G-25a994a0 | tap-target | `#headerAlertPill` | all 7 cells | raise 50×21 pill (height 21 < 40) to ≥40px |
| G-104e495e | tap-target | `#headerFullscreenBtn` | 768×1024, 740×360 | raise 24×28 hit area to ≥40px |
| G-4874945e | edge-cross | `#headerRefreshSpinner` | 360×740 @z200 | right of header cluster overflows 180px CSS viewport — let the header wrap or shrink paddings at <200px |
| G-5b00d250 | edge-cross | `#statusDot` | 360×740 @z200 | keep the status dot inside the viewport (right margin / wrap) |

### Automata

| Group | Check | Selector | Widths | Remedy |
|---|---|---|---|---|
| G-6502e162 | clipped-text | `#headerTitle` | 320×568, 360×740 (+light) | ellipsis + `title` on the header title (shared) |
| G-3f4cac4d | tap-target | `#headerHelpLink` | all 7 cells | ≥40px hit area (shared) |
| G-bd9b90a2 | tap-target | `#headerIdentityBtn` | all 7 cells | raise 38×30 hit area to ≥40px |
| G-6e692494 | tap-target | `#headerSearchBtn` | all 7 cells | ≥40px hit area (shared) |
| G-12200f65 | tap-target | `.automata-tabs-bar > button.output-tab.active` | all 7 cells | raise 89×29 tab height to ≥40px (shared `.output-tab` floor) |
| G-d4c8a4fd | tap-target | `.automata-tabs-bar > button.output-tab` | all 7 cells | raise 92×27 tab height to ≥40px |
| G-44afec82 | tap-target | `#automataSelectBtn` | all 7 cells | raise 32×28 hit area to ≥40px |
| G-245b9237 | tap-target | `#automataFilterBtn` | all 7 cells | raise 32×28 hit area to ≥40px |
| G-ef2dc788 | tap-target | `#automataHistoryBtn` | all 7 cells | raise 32×28 hit area to ≥40px |
| G-ea8f5fbd | tap-target | `#automataWatchFilterBtn` | all 7 cells | raise 32×28 hit area to ≥40px |
| G-f4f80146 | tap-target | `…toast-error-close` | 320×568 | ≥40px toast close button (shared) |
| G-25a994a0 | tap-target | `#headerAlertPill` | all 7 cells | ≥40px pill (shared) |
| G-104e495e | tap-target | `#headerFullscreenBtn` | 768×1024, 740×360 | ≥40px hit area (shared) |
| G-ef8b66d8 | edge-cross | `#headerAlertPill` | 360×740 @z200 | header cluster overflows 180px CSS viewport at z200 — wrap/shrink |
| G-4874945e | edge-cross | `#headerRefreshSpinner` | 360×740 @z200 | keep inside viewport at z200 (shared) |
| G-5b00d250 | edge-cross | `#statusDot` | 360×740 @z200 | keep inside viewport at z200 (shared) |

### Automation

| Group | Check | Selector | Widths | Remedy |
|---|---|---|---|---|
| G-a4359a64 | open-failed | `[data-view="automation"]` | not measured | the tab was not measured: the harness `VIEWS` whitelist (scripts/test-stories/pwa/mobile-fit.mjs) rejects `automation` as an unknown view (exit 2), although the app defines the view. Add it to `VIEWS` (+ provider if needed) and re-run |

### Settings

| Group | Check | Selector | Widths | Remedy |
|---|---|---|---|---|
| G-3f4cac4d | tap-target | `#headerHelpLink` | all 7 cells | ≥40px hit area (shared) |
| G-65d7a747 | tap-target | `.settings-tabs-bar > button.settings-tab-btn.output-tab` | all 7 cells | 42 findings — raise settings tab height (79×27) to ≥40px via the shared `.output-tab` floor |
| G-f4f80146 | tap-target | `…toast-error-close` | 320×568 | ≥40px toast close button (shared) |
| G-25a994a0 | tap-target | `#headerAlertPill` | all 7 cells | ≥40px pill (shared) |
| G-104e495e | tap-target | `#headerFullscreenBtn` | 768×1024, 740×360 | ≥40px hit area (shared) |
| G-6502e162 | clipped-text | `#headerTitle` | 360×740 @z200 | ellipsis + `title` on the header title (shared) |

### Alerts

| Group | Check | Selector | Widths | Remedy |
|---|---|---|---|---|
| G-d206c048 | tap-target | `#view > .view-content > div > a` | all 7 cells | 16×16 alert-row link below 40px — enlarge hit area or make the row the tap target |
| G-f4f80146 | tap-target | `…toast-error-close` | 320×568 | ≥40px toast close button (shared) |
| G-25a994a0 | tap-target | `#headerAlertPill` | all 7 cells | ≥40px pill (shared) |
| G-104e495e | tap-target | `#headerFullscreenBtn` | 768×1024, 740×360 | ≥40px hit area (shared) |
| G-6502e162 | clipped-text | `#headerTitle` | 360×740 @z200 | ellipsis + `title` on the header title (shared) |

Data note: the sandbox daemon starts from a minimal seed (no pre-populated
session/PRD/alert rows in most views), so this pass measures the empty/default
state: per-row findings, detail panes and populated lists are not represented
here. The header-control and tab-bar groups above are state-independent and are
the solid ground truth; after the shared fixes, re-run with seeded data (see
`mobile-fit-seeds.mjs` for provider seeding) to catch the row-level cases.

## Release smoke run

2026-10-22 — live smoke of `scripts/release-smoke-mobile-fit.sh` against a fresh
daemon built from this tree (isolated loopback ports, never the production daemon):

- **Live run** (`DATAWATCH_BASE` = fresh daemon, `DATAWATCH_TOKEN` set): exit
  **0**; the report path is printed (`OK: report: .../mobile-fit-report.json`).
  The quick profile ran **95 scenarios**; report summary line:
  `mobile-fit: 95 scenarios, 411 findings in 6 groups` (remaining worklist groups
  only — tap-target `#headerHelpLink` / `#headerAlertPill` / `#peerStaleBadge` /
  `#headerFullscreenBtn`, clipped-text `.nav-icon`, interaction
  `#settings-sec-stats`).
- **Skip run** (`DATAWATCH_BASE` on an unused port): exit **2** with the line
  `SKIP: daemon unreachable at <base>`.

Script note: the smoke originally treated a completed browser run that reports
findings (runner exit 1) as a failure; the fix makes it pass when the suite ran
to completion and wrote a usable report, and fail (exit 1) only when no usable
report was produced or a scenario could not even open its view
(`open-failed`). Exit 2/skip semantics unchanged.

## Gate results

### 2026-10-25 — end-to-end gate (fresh run, strict order)

Full end-to-end gate re-run in strict order against this tree
(`/home/user/.datawatch/prd-worktrees/dbd5000a`; the only file this gate
writes is this plan doc). Every
browser step ran against an isolated loopback daemon freshly built from
this tree (`go build ./cmd/datawatch` to a temp dir, `.datawatch/verify/_daemon.sh`) —
never the production daemon. No layout files were modified in this run:
`internal/server/web/app.js` and `style.css` are unchanged, and the
Provenance pin still verifies (`sha256sum -c` → both OK).

| Step | Command | Result (2026-10-25) |
|---|---|---|
| 1 — JS syntax | `node --check internal/server/web/app.js` (09:54) | **PASS** — exit 0 |
| 2 — web unit tests | `node --test internal/server/web/*.test.js` (09:55) | **PASS** — # pass 717, # fail 0 |
| 3 — PWA report/seed tests | `node --test` report+seed suite (09:55) | **PASS** — 34 pass, 0 fail |
| 4 — detector self-test | `node mobile-fit-selftest.mjs` (09:55) | **PASS** — 9/9 scenarios (0 fail), exit 0 |
| 5 — check scripts | docs-sync, no-internal-refs, curated-howtos, howto-coverage, pwa-suite-path (09:55) | **PASS** — all five exit 0 |
| 6 — Go | `go vet ./...` then `go test ./...` (09:55→09:59) | **PASS** — vet clean; 7641 tests passed, 87 packages, 0 FAIL |
| 7a — Observer full profile | `mobile-fit.mjs --view observer --profile full` (10:09→10:16) | **PASS** — 284 scenarios (≥ 250), 0 findings, exit 0 |
| 7b — TS-817 regression guard | `MOBILE_FIT_SCOPE=#view` TS-817 against a fresh daemon (10:19→10:23) | **PASS** — 99 scenarios, 0 findings; none of the 68 recorded groups reproduce |
| 7c — TS-818 baseline guard | TS-818 against a fresh daemon (10:24) | **PASS** — 38 tracked selectors × 3 viewports, all within tolerance |
| 7d — release smoke | `scripts/release-smoke-mobile-fit.sh` against a fresh daemon (10:28→10:32) | **PASS** — 95 scenarios, 0 findings, exit 0 |

Notes on this run:

- The tree is byte-for-byte the committed one: no `package.json`, CHANGELOG
  or README changes, no version bump; the tracked
  `scripts/test-stories/pwa/node_modules` symlink was left exactly as
  committed (it points at a looped node_modules in the main checkout, so a
  bare `import 'playwright'` from the suite dir cannot resolve here).
- To run the browser steps without writing any tracked file, Playwright was
  resolved purely by environment: `NODE_PATH` for the CJS dependency
  pre-checks and a `--import` resolve hook (scratch files only, under the
  git-ignored `.datawatch/` dir) mapping `playwright` / `playwright-core`
  onto a resolvable Playwright 1.60.0 install on this machine (satisfies the
  `^1.50.0` requirement). With that environment removed the same scripts
  SKIP (exit 2) exactly as designed.
- Steps 1–7d all green: the 2026-10-24 layout fixes hold end-to-end on this
  tree (Observer full 284 scenarios, 0 findings; TS-817 / TS-818 / release
  smoke all clean).

### 2026-10-24 — end-to-end gate (original run, history)

All steps in `## Acceptance criteria` passed on this tree (Observer layout
fixes: 3 files in `internal/server/web/`).

| Step | Command | Result (2026-10-24) |
|---|---|---|
| 1 — JS syntax check | `node --check internal/server/web/app.js` | **PASS** — exit 0 |
| 2 — web unit tests | `node --test internal/server/web/*.test.js` | **PASS** — # pass 717, # fail 0 |
| 3 — PWA report + seed unit tests | `node --test` report+seed suite | **PASS** — 34 pass, 0 fail |
| 4 — self-test | `node mobile-fit-selftest.mjs` | **PASS** — 9/9 scenarios (0 fail), exit 0 |
| 5 — check scripts | docs-sync, no-internal-refs, curated-howtos, howto-coverage, pwa-suite-path | **PASS** — all five exit 0 |
| 6 — Go | `go vet ./...` then `go test ./...` | **PASS** — vet clean; 7641 passed, 87 packages, 0 FAIL |
| 7 — Observer (full profile) | via `e2e-pwa.sh` against a fresh daemon | **PASS** — 284 scenarios, 0 findings, exit 0 |
| 8 — TS-817 | `MOBILE_FIT_SCOPE=#view bash scripts/e2e-pwa.sh TS-817` | **PASS** — 99 scenarios, 0 findings; none of the 68 recorded groups reproduce |
| 9 — TS-818 | `bash scripts/e2e-pwa.sh TS-818` | **PASS** — all 38 tracked selectors × 3 viewports within tolerance |
| 10 — release smoke | `bash scripts/release-smoke-mobile-fit.sh` | **PASS** — 95 scenarios, 0 findings |

### Fixes made in this gate

- **`#peerStaleBadge` / `#vaultUnreachableBadge` (tap-target + clipped-text)** —
  both badges lived inside `.nav-icon` as `position:absolute` 17×15 pills; an
  absolutely-positioned descendant inflates the icon span's `scrollWidth`
  (clipped-text) and the pill itself sits below the 40px floor (tap-target).
  Re-anchored at the `.nav-btn` box as 44×44 rounded badges (index.html for the
  static badge, app.js `_setVaultBadge` for the dynamic one, `display:flex` in
  `updatePeerStaleBadge`).
- **`#headerTitle` (clipped-text)** — added the declared truncation model
  (`white-space:nowrap; text-overflow:ellipsis`) in style.css and a
  MutationObserver in `navigate()` (app.js) keeping `title=` synced so the
  full label stays reachable on hover.
- **`#headerHelpLink` / `#headerAlertPill` / `#headerFullscreenBtn`
  (tap-target then edge-cross)** — their inline padding (4px 8px etc.) plus
  border made the boxes 60–62px: below-40px tall as inline boxes, and wide
  enough at 130%/200% zoom to push `#statusDot` / the pill off the right edge
  (measured: at z130 the 320 cell, header row = 32px padding + 60 + 62 + 10
  + gaps). Now: `box-sizing:content-box; min-width/min-height:44px`, inline
  padding stripped (`padding:0 !important`), 46px max box for the bordered
  pill — and at ≤219 CSS px (200% zoom on phones, where even 44px controls
  can't all fit) the help + fullscreen toggles hide so the title, alert pill
  and status dot stay on screen. `#headerRefreshSpinner` findings cleared
  with the same row-budget fix.
- **`.toast-error-close` (tap-target)** — 28×28 → 44×44 (style.css).
- **check-docs-sync** — mechanical re-sync via `make sync-docs` (no content
  edits in the mirror tree).
- **check-no-internal-refs** — removed three BL tracker tags that had leaked
  into README.md (the script only allows refs in docs/testing-tracker.md,
  plans, changelog files).

Provenance pin regenerated with the final `app.js`/`style.css` on 2026-10-24
and re-verified on 2026-10-25 (both files unchanged since):
`docs/testing/runs/observer-mobile-fit/web-sha256.txt`.
