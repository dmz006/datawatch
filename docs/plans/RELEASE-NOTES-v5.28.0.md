# datawatch v5.28.0 — release notes

**Date:** 2026-04-30
**Minor.** BL214 (datawatch#32) — PWA i18n foundation with DE/ES/FR/JA translations sourced from the Android companion.

## What's new

### BL214 — PWA i18n foundation (DE/ES/FR/JA)

The datawatch-app Android client (Compose Multiplatform) shipped vetted DE/ES/FR/JA translations in v0.52.0 (BL15). The PWA had nothing — every string hardcoded in English. v5.28.0 lays the foundation so the PWA can match.

**Shipped:**

| Piece | Where | What |
|---|---|---|
| Locale bundles | `internal/server/web/locales/{en,de,es,fr,ja}.json` | 5 JSON files, ~240 keys each. Sourced 1:1 from `composeApp/src/androidMain/res/values{,-de,-es,-fr,-ja}/strings.xml` so a single mobile-side translation update lands here on the next bundle pull. Embedded in the binary; served as `/locales/<lang>.json` |
| i18n harness | `internal/server/web/app.js` | Zero-dep `window._i18n` + `t(key, vars)` helper supporting Android-style `%1$s` / `%1$d` / `%2$s` placeholders. `applyI18nDOM(root)` sweeps `data-i18n="<key>"` attributes (with `data-i18n-attr` / `data-i18n-html` variants) |
| Auto-detection | `detectLocale()` | `localStorage('datawatch.locale')` override → `navigator.language` strip-to-base → fallback to `en` |
| Settings picker | Settings → General → Language | Auto / English / Deutsch / Español / Français / 日本語. Persists override in localStorage; reload applies the new bundle to every rendered surface |
| Initial coverage | nav + settings tabs | `nav_sessions/autonomous/alerts/settings` (via `data-i18n` in index.html) + `settings_tab_monitor/general/comms/llm/about` (via `t()` in renderSettingsView). HTML `lang=` attribute set on document root |

**Iterative expansion path:** the rest of `app.js` (~9700 lines) is unchanged in v5.28.0. Subsequent v5.28.x patches will extend `t()` calls into modals (Save/Cancel/Delete/Confirm…), Sessions screen (filter labels, state badges, empty states), New Session form (field labels), Autonomous/PRD CRUD, etc. — using the same Android keys as the source-of-truth. The harness is in place; coverage is incremental and shouldn't risk regressions.

**Why source from Android:** the Android translations are operator-vetted and have already been through real-user UX feedback. Machine-translating 240+ strings ad hoc would produce inconsistent quality and conflict with the mobile companion's wording. Mirroring keeps both clients in sync without translator round-trips.

## Tests

```
Go build:  Success (via `make build` + `make cross`)
Go test:   1544 passed in 58 packages (+3 new for v5.28.0)
Smoke:     run after install
```

New tests in `internal/server/v5280_locales_test.go`:

- `TestLocales_AllPresent` — all 5 bundles embedded + valid JSON + non-empty
- `TestLocales_ParityWithEnglish` — each non-EN locale covers ≥90% of EN keys (catches stale translation pulls)
- `TestLocales_CommonNavKeysPresent` — explicit guard on the keys v5.28.0 actually consumes (nav + settings tabs + common actions)

## datawatch-app sync

datawatch-app#38 tracks Settings → System → MCP channel mirror (carried from v5.27.10). No new mobile parity issue for BL214 — Android already shipped the source-of-truth translations in v0.52.0; mirror direction is parent ← mobile.

## Backwards compatibility

- All additive. Older clients without the picker default to browser-language detection; older browsers without `navigator.language` fall back to English.
- Locale bundles served from `/locales/<lang>.json` — a new path, no conflict with existing routes.
- `t()` returns the key string itself when a translation is missing (intentional — makes coverage gaps visible, doesn't crash).

## Upgrade path

```bash
git pull
datawatch update && datawatch restart
# Hard-reload the PWA (cache name → datawatch-v5-28-0).
# Pick a language: Settings → General → Language.
```

No data migration. No new schema. No new server-side config keys.
