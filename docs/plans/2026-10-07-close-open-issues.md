# Close out all open GitHub issues (#191, #190, #189, #182, #172, #107, #4)

- **Date**: 2026-10-07
- **Version at planning**: v8.63.15
- **Status**: Complete — all 6 actionable issues closed (#107, #172, #182, #189, #190, #191), v8.66.0. #4 deliberately left open (perpetual tracking umbrella, status comment posted instead).

## Context

Operator asked to finish every open issue in `dmz006/datawatch`, closing
each as it's done, following AGENT.md/DATAWATCH-CONTEXT.md rules. Full
survey of all 7 done first (issue bodies + comments read in full, plus
the companion decisions in `dmz006/datawatch-app`'s `docs/parity/`
sections and `DocsLinks.kt` for the two items that needed real specifics
pulled from the mobile repo rather than guessed).

Two genuine product decisions were resolved with the operator before
starting:
- **#172 D65** (three-finger swipe-up gesture): confirmed via
  `docs/parity/sections/02-sessions-list.md` row 91 that the real spec is
  "64dp, 500ms debounce → opens the server picker" (not undefined as the
  issue thread implied) and confirmed via the Touch Events API that a web
  page CAN detect 3 simultaneous touch points — operator decision: build
  it.
- **#172 D67** ("other Android session-detail extras"): confirmed via
  `03-session-detail.md` rows 34/46/66 the three concrete items — hooks-
  installed toast, rate-limit inline notice, Terminal/Chat mode
  persistence — operator decision: build them (the audit already showed
  what to do; not a "full parity, nothing to do" case).
- **#182 deep link**: operator picked `protocol_handlers` + `web+datawatch://`
  scheme over a bare hash-route.

## Scope per issue

### #190 — stale .trivyignore suppressions (chore)
Bot-filed; body already says "no stale suppressions found... 9/9 real
images scanned". Re-verify is still current (today's date), comment, close.
No code change expected.

### #107 — PWA==Android==iOS parity standard
All 4 asks done (parity-status.md iOS column, APNs shipped as BL335/v8.62,
no iOS-only paths, AGENT.md rule updated). Comment summarizing, close.
No code change.

### #172 — 2026-10-04 parity decisions (25 D-items)
23 of 25 already shipped. This plan ships the last 2:
- **D65**: hand-rolled 3-touch swipe-up detector (`touchstart`/`touchmove`/
  `touchend`, net upward delta ≥64px-equivalent, 500ms debounce) wired to
  the existing server/profile picker open function.
- **D67**: three sub-items — hooks-installed one-time toast (claude-code
  sessions), rate-limit inline notice (dismissible, amber, retry-at) in
  session detail, Terminal/Chat mode preference persisted to localStorage
  instead of resetting on every render.

### #182 — operator decisions 2026-10-05 (8 items)
7 of 8 already shipped. Last item: alert deep link via Web App Manifest
`protocol_handlers` (`web+datawatch` scheme → `/?alert=<id>` → PWA reads
`alert` query param on load and opens that alert).

### #189 — help-link anchor mismatch + 3 missing pages + 2 doc asks
- Port `DocsLinks.kt`'s `byKey`/`channelTypes`/`llmBackends` maps + its
  `forKey` prefix-fallback logic into `app.js` verbatim (same keys the PWA
  already passes into `settingsSectionHeader(key, title, docsPath)` today
  — confirmed via grep that every `key` argument already matches a
  `DocsLinks.byKey` key). Rewrite `defsLink` to look up by key first,
  falling back to the old title-slug-against-the-manual behavior only for
  a key with no table entry (never a regression).
- Write the 3 missing howto pages the table's own comments flag as
  server-side gaps: `howto/new-session.md`, `howto/automata-wizard.md`,
  `howto/prd-dag-orchestrator.md` — wait, the ported table actually
  points PAST these three (it already redirects to pages that DO exist:
  `howto/sessions-deep-dive.md#4b-happy-path-pwa`,
  `datawatch-definitions.md#launch-automation-form`,
  `howto/automata-orchestrator.md`). Porting the table itself closes the
  "wrong anchor" bug for all three without needing new pages — but the
  issue explicitly lists the 3 filenames as things "the server doesn't
  ship", so write them anyway as real pages (not just redirects) since
  they're referenced by name in the issue and may be linked to elsewhere.
- Add the two operator-requested doc sections: a short "App-only
  settings" section (Face ID/biometric lock, Theme, Language) in
  `datawatch-definitions.md`'s Settings → About area, and a new Automata
  Type Registry subsection under Settings → Automata.

### #191 — Community Plugins card (new UI)
New Settings → Plugins card, wired to the existing `GET /api/plugins/browse`
/ `POST /api/plugins/install` (BL325). Registry picker (not hard-coded
"community") per the issue's own note that Android will follow whatever
the web UI does. Not-connected state offers a Connect action via the
existing skill-registry connect endpoint. Loading eye (GH#186 primitive).

### #4 — meta parity-tracking umbrella
**Not closed.** Its own body defines it as a perpetual process doc ("when
you add a PWA feature... note it... file a linked issue... when the
mobile client needs a PWA capability, file it here") — closing it would
contradict its stated purpose and break the cross-repo tracking workflow.
Comment with a status note instead.

## Parity surface
All of #172/#182/#191's items are PWA-only (Android/iOS already have
them, or #191's case: apps already have the feature, PWA catching up).
#189 is docs-only. No REST/MCP/CLI/config contract changes anywhere in
this batch.

## Verification
- `node --test internal/server/web/*.test.js` after each app.js change.
- Manual review of the full ported `DocsLinks` table against the Kotlin
  source (diff-by-eye, not just "it compiles") since a silent wrong
  mapping is worse than the original bug (leads confidently to the wrong
  page instead of obviously to the top of the manual).
- Each shippable unit gets its own version bump + CHANGELOG entry per
  AGENT.md's per-commit discipline, not one giant patch.
