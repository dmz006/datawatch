# Parity issue — splash dwell settings (datawatch-app)

**Parent release:** datawatch 9.71.0 (unreleased) — splash dwell settings on the PWA
**Plan doc:** `docs/plans/2026-11-03-splash-dwell.md`

## What changed on the PWA

The PWA launch splash (logo screen shown on first visit, when the daemon's version
changed, or after 24+ hours in the same browser) now honours two dwell settings,
dismisses early on input once connected, and shows the previous version in the
version line when it is being shown for a new version.

| Setting key | Type | Default | Range | `0` means |
|---|---|---|---|---|
| `ui.splash_min_seconds` | integer seconds | 6 | 0–60 | keep the legacy timing (splash goes at 3000 ms from page start) |
| `ui.splash_min_seconds_new_version` | integer seconds | 9 | 0–60 | keep the legacy timing |

Sources of truth (datawatch repo):

- `internal/server/api.go` — `GET/PUT /api/config` serves and writes both keys under `ui`
- `internal/config/config.go` — defaults (6 / 9), range 0–60, validation naming the key
- `internal/server/web/app.js` — `splashDwellRemainingMs`, `_scheduleSplashDismiss`, the early-dismiss listeners, the version line
- `internal/server/web/locales/*.json` — `splash_version_previous`, `settings_splash_min_seconds*`

## What the datawatch-app should add / change

Mirror the PWA behaviour on Android and iPhone:

1. **Fetch the two settings** from `GET /api/config` (keys `ui.splash_min_seconds`
   and `ui.splash_min_seconds_new_version`) before deciding the dwell, and use them.
2. **Dwell timing:** the splash stays for the configured whole seconds —
   `splash_min_seconds_new_version` when the splash is shown because the daemon
   version changed, otherwise `splash_min_seconds` — measured from the moment the
   gating allowed the splash, then fades out.
3. **Early dismiss:** once the app is connected to the daemon, a tap, click, or keypress
   dismisses the splash immediately (with the fade) instead of waiting out the dwell.
4. **`0` = legacy:** a configured `0` keeps the pre-setting behaviour (3000 ms from the
   splash start) rather than dismissing instantly.
5. **Version line:** when the splash is shown for a new version, the version line shows
   the previous version (PWA locale key `splash_version_previous`) instead of only the
   current one.

## Not in scope for this issue

- The 24-hour / first-visit / version-change gating itself is unchanged on both sides.
- No new daemon endpoints: the app reads the existing `GET /api/config` keys.
