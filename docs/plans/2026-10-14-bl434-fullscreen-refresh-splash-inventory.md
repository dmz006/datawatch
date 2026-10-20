# BL434 — PWA Full-Screen Refresh-Splash: Inventory + Treatment Plan

- **Date:** 2026-10-14
- **Version:** v9.5.26 (datawatch version at time of planning)
- **Scope:** PWA browser-side only — `internal/server/web/app.js`, `internal/server/web/splash-art.js` (primitives reused, not re-invented), `docs/`. No Go/daemon changes, no new API, no new config.

## Scope

Operator-visible change surface: the PWA's full-screen overlays. When any
full-page transition (`location.reload()` / stale-page refresh) fires, the
operator currently sees the view unmount and a blank page for the round-trip.
This plan classifies every such site and prescribes, per site, which full-screen
`DWSplashArt` treatment runs during the reload gap, modeled on the existing
session-connect treatment (`termLoadingSplash` mount +
`state._termSplashMountedAt` + `_dismissTermLoadingSplashWithMinDwell()` —
see `app-gh192-phase1b.test.js` for the tested min-dwell contract).

Reuse requirement (hard): implementation MUST reuse the existing
`window.DWSplashArt.startEyeOnly(canvas)` and
`window.DWSplashArt.startSessionLoading(canvas)` primitives from
`splash-art.js`. Do not reinvent the eye, the Earthrise scene, or the pulsing
lightning-bolt animation — wire one of these two stop-able handles onto a full
view (`#splash` / `#splashCanvas` or a full-viewport sibling), with min-dwell
dismiss semantics copied from the session-connect path.

## Baseline treatment rule (how the table decisions were made)

- **Eye-only is the default.** The pulsing lightning bolt in
  `startSessionLoading` signals *session connect in flight*. None of these
  reload sites are session connects, so the bolt would be misleading —
  reusing `DWSplashArt.startEyeOnly` is the correct "we are alive, reloading"
  signal.
- **Full eye+bolt is earned only by a site whose reload directly continues an
  in-flight session reconnect.** Exactly one site below qualifies (the
  WS-reconnect version catch-up at app.js:481): the reload fires inside the
  WS-reopen transition to keep the session link moving, which is
  semantically a session-continuation event, not a plain page swap.
- **`window.replaySplash` may be left as-is** on the single-line reason: the
  boot `#splash` Earthrise scene immediately follows the reload (the dev
  helper only clears the 24h/version gating, and the WS-connect handler
  re-shows the real launch splash itself).

## Inventory table — every full-page transition site

Treatments chosen exactly from: **Full eye+bolt** / **Eye-only** / **Leave-as-is**.

| # | file:line | Trigger | What the operator sees today | Recommended treatment (+ one-line reason) | Intended operator-visible result |
|---|---|---|---|---|---|
| 1 | `internal/server/web/app.js:137` (`setLocaleOverride`) | Operator picks a locale in Settings → `location.reload()` | Full blank page between unmount and the locale-switched page remount | **Eye-only** — not a session connect, bolt would misreport "connecting"; eye = "app alive, switching locale" | Full-screen eye overlay covers the blank gap; page re-appears in the new locale with no visible white flash |
| 2 | `internal/server/web/app.js:481` (WS-open version catch-up) | WS (re)opens during a reconnect transition, `/api/health` probe shows daemon version changed → `location.reload()` | Full blank reload mid-reconnect, exactly when the operator believes they are still reconnecting to their session | **Full eye+bolt** — the one site whose reload directly continues an in-flight session reconnect, so the bolt truthfully signals session continuation (mirrors `termLoadingSplash`) | Same full-screen eye+bolt session-loading treatment the operator sees on session connect, then the app remounts with the new daemon's payload |
| 3 | `internal/server/web/app.js:755` (sessions-broadcast version change) | `sessions` WS broadcast carries a version different from `state._daemonVersion` → `location.reload()` | Full blank reload, often right after the operator sent a command (v6.11.8 operator report) | **Eye-only** — the trigger is a new daemon build being detected, not a session connect, so the bolt is misleading | Full-screen eye overlays the reload; operator sees the app "come back" under the new build instead of a dead page |
| 4 | `internal/server/web/app.js:17207` (`window.replaySplash`) | Dev/diagnostic "Replay splash" → clear 24h gating → `location.reload()` | Blank reload, then the boot splash plays on its own | **Leave-as-is** — the boot `#splash` Earthrise scene immediately follows the reload, so an added overlay would only cover itself | Unchanged: reload, then the real launch Earthrise splash re-appears as designed (GH#172 D59 behavior) |
| 5 | `internal/server/web/app.js:18911` (SW `controllerchange`) | `navigator.serviceWorker` `controllerchange` fires after activate → `window.location.reload()` | Full blank reload while the new service worker's assets are picked up | **Eye-only** — a PWA asset swap, not a session connect; eye = "new build taking over" | Full-screen eye covers the asset-revalidation gap; PWA remounts on the freshly-activated build |
| 6 | `internal/server/web/app.js:18932` (`window.forcePWAUpdate`) | Operator/dev escape hatch: unregister SWs + drop caches → `window.location.reload()` | Full blank reload (often deliberately "ugly" — that's its job) | **Eye-only** — cache-bust reload, not a session connect; eye keeps the gesture feeling intentional rather than broken | Operator sees a deliberate full-screen "clearing + reloading" eye gesture, then a clean remount instead of a dead white screen |
| 7 | `internal/server/web/index.html:53` (version stamp) | One-shot check: baked `window.__DW_VERSION__` ≠ `/api/health` version (stale back/forward/suspended-tab page) → `location.reload()` | Full blank reload on startup / tab restore; operator suspects the app is broken | **Eye-only** — startup self-heal, not a session connect | Full-screen eye over the one-shot self-refresh; app appears to "wake up" rather than flash blank |
| 8 | `internal/server/web/app.js:24866` (auth-required / re-auth) | No `cs_token` stored + `/api/health.auth_required` → splash DOM replaced with token-entry prompt (NOT a page navigation) | Splash swaps to an "Access token required" prompt — no full page transition exists | **Leave-as-is** — verified site: there is no `location.reload()`/navigation here; the splash is the overlay | Unchanged — the auth prompt already lives on the `#splash` surface; in-scope only to *not* disturb it |
| 9 | `internal/server/web/app.js:16671` (`selectServer` federated switch) | Operator switches federated server (or back to local/"all") → WS closed + `connect()` + view re-render (NOT a page navigation) | View goes empty briefly while sessions repopulate; no full page transition exists | **Leave-as-is** — verified site: this is a WS-level transition (in-view refresh), not a full-view page reload; covering it with a full-screen overlay would hide the honest "Connected to: <server>" toast + re-render cycle | Unchanged — the existing status toast/banner and in-view re-render remain the feedback |
| 10 | `internal/server/web/app.js:628` (hard WS disconnect) | WS `close` with backoff `scheduleReconnect()`; "Not connected" banner + re-rendered views (NOT a page navigation) | Banner + stale/empty view until reconnect lands; no full page transition exists | **Leave-as-is** — verified site: blank-ish state is the WS-down state, and the #1–#7 reloads that *do* fire in that flow already carry the overlay treatment | Unchanged — reconnect banner stays the source of truth; if the daemon comes back with a new version, row 2/row 3 treatment covers the reload |

## OUT-OF-SCOPE (explicitly untouched)

- **The ~40 inline panel spinners built from `loadingEyeBlock()`** (defined at
  `app.js:18015`, wired by `_wireLoadingEyes` at `app.js:18035`, used across
  settings panels, timeline, observer cards, etc.). These are **in-panel**
  loading glyphs, not full-screen transitions. They are left exactly as they
  are; this plan adds no changes to `loadingEyeBlock()` or
  `_wireLoadingEyes`.
- **The initial boot `#splash` Earthrise scene** — `index.html` `#splash` /
  `#splashCanvas`, driven by `DWSplashArt.startScene(canvas, {compact})`
  (wired at `app.js:10`, auto-stopping on disconnect). Boot behavior, its
  24h + version gating, and the auth-prompt swap all stay as-is.

## Config, parity, and accessibility statements

- **No new config knob.** Config parity: N/A. This change adds no config
  fields, no REST/WS fields, and no new API contract — it is a purely
  in-PWA presentation treatment on existing code paths.
  `docs/config-reference.yaml` needs no new entry.
- **Mobile parity:** `datawatch-app` (Android/iOS) already ships the
  equivalent full-screen connect/refresh overlays, so mobile parity is met by
  the PWA catching up — **no app-side change is required** for this feature.
- **`prefers-reduced-motion` already honored:** `splash-art.js` `loop()`
  draws a single static frame (t=0, matrix frozen at row 1, bolt at ~0.55
  intensity) when `window.matchMedia('(prefers-reduced-motion: reduce)')`
  matches. Because rows 1–7 (except the leave-as-is rows) merely reuse the
  `DWSplashArt` primitives, the new full-screen treatment **inherits**
  reduced-motion support with no additional work.
- **Localization:** no new operator-visible strings are required (the overlay
  is animation, not copy); existing `t()` strings are untouched. If an
  implementation adds any visible status line, it must use a locale-bundle
  key per the Localization Rule.

## Parity surface

Per the AGENT.md Parity-surface rule, every channel is enumerated — included
or N/A-with-reason:

| Channel | Status | Reason / note |
|---|---|---|
| **PWA** | ✅ Include | The feature IS the PWA — `app.js` + `splash-art.js` overlays on the 7 reload sites |
| **Android** | N/A — already parity | `datawatch-app` already ships the equivalent full-screen connect/refresh overlays; PWA catching up satisfies `PWA == Android == iOS` with no app change |
| **iOS / iPhone** | N/A — already parity | Same as Android: native client already has the connect/refresh overlay treatment; capability parity met, implementation parity not required |
| **Web** (non-PWA browser) | ✅ Include | Same `app.js`/`index.html` runs in any browser; overlay treatment is identical and no SW-gated behavior changes it |
| **Docs** | ✅ Include (this plan + CHANGELOG) | This `docs/plans/` doc is the record; a `CHANGELOG.md` [Unreleased] entry lands with the implementing story. No new operator-facing concept, so no `docs/datawatch-definitions.md`/`docs/flow/*.md` addition (N/A with reason) |
| **CLI** | N/A — unrelated | No datawatch CLI command is added or changed; refresh treatment is browser-only presentation |
| **Config (YAML)** | N/A — no new knob | Explicitly no new config field or default; `docs/config-reference.yaml` unchanged |
| **i18n / localization** | N/A — no new strings | Overlay is animation, not copy; existing `setLocaleOverride` row (row 1) reuses existing bundles. If an implementation adds a visible line, it ships in all 5 locale bundles per the Localization Rule |
| REST | N/A — no contract change | No endpoint added, renamed, or re-shape; `/api/health` is only *read*, as today |
| MCP | N/A — no contract change | The MCP surface is untouched by a presentation change |
| Comm channel | N/A — no contract change | No new session/message types; the WS `sessions`/WS-open flows are only *consumed*, as today |

## Phases

(= this PRD's stories/tasks; status: Planned / In Progress / Done)

1. **Story 1 — shared full-screen overlay helper (Planned).** Add one
   helper in `app.js` that mounts a `DWSplashArt.startEyeOnly` (or
   `startSessionLoading` for row 2) canvas onto a full-viewport overlay with
   `stop()` and min-dwell semantics modeled on
   `state._termSplashMountedAt` / `_dismissTermLoadingSplashWithMinDwell()`
   (contract per `app-gh192-phase1b.test.js`). Reuse only — no new animation
   code in `splash-art.js` required.
   - Phase checklist: Phase Completion Checklist applies (functionality,
     tests, live smoke, `go test ./...` + `node --test internal/server/web/*.test.js`,
     the 8-item general docs checklist, conditional docs N/A, Mobile-Parity
     Rule N/A — datawatch-app already ships the overlay, Localization Rule
     N/A unless a string is added, version-bump decision, status update).
2. **Story 2 — wire the 6 Eye-only sites (Planned).** Rows 1, 3, 5, 6, 7 and
   the leave-as-is guard for row 4: show the eye overlay before
   `location.reload()` at `setLocaleOverride` (app.js:137), the
   sessions-broadcast version change (app.js:755), the SW
   `controllerchange` handler (app.js:18911), `forcePWAUpdate`
   (app.js:18932), and the index.html:53 version-stamp check; leave
   `window.replaySplash` (app.js:17207) exactly as-is (boot `#splash`
   Earthrise immediately follows).
   - Phase checklist: Phase Completion Checklist applies (same items as Story 1).
3. **Story 3 — wire the 1 Full eye+bolt site (Planned).** Row 2: the
   WS-reconnect version catch-up (app.js:481) shows the
   `DWSplashArt.startSessionLoading` (eye + pulsing bolt) treatment,
   mirroring the `termLoadingSplash` connect session — the only reload that
   directly continues an in-flight session reconnect.
   - Phase checklist: Phase Completion Checklist applies (same items as Story 1).
4. **Story 4 — tests + gates + live smoke (Planned).** Node test asserting the
   overlay is shown (and auto-stopped on canvas disconnect) before each wired
   reload site and that `replaySplash` stays untouched; `node --check
   internal/server/web/app.js` green; `node --test internal/server/web/*.test.js`
   green; reduced-motion live smoke (static single frame, not animation);
   `CHANGELOG.md` [Unreleased] entry.
   - Phase checklist: Phase Completion Checklist applies (same items as Story 1).

## Status

| Story | Status | Shipped in |
|---|---|---|
| 1 — shared overlay helper | Planned | — |
| 2 — 6 eye-only sites | Planned | — |
| 3 — 1 eye+bolt site | Planned | — |
| 4 — tests + gates + smoke | Planned | — |

This document is the inventory + plan of record for BL434; it is
read-only with respect to source — no file listed in Scope has been modified.
