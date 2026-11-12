# Parity issue draft — usage calculator: server zone default (datawatch-app)

File: **datawatch-app** — the issue is already filed as **datawatch-app#297**
(open); update it with the surfaces below instead of filing a second one.

## Changed UI surfaces

The usage calculator settings surface gains the time-zone row, and the daemon's
advice and config responses gain new fields for it. datawatch-app must mirror
all of it, not just the two locale strings:

- `datawatch-app/src/app/screens/SettingsScreen.tsx` — General tab, "Usage
  calculator" section: the time-zone input (label `settings_usage_calculator_reset_zone`,
  hint `settings_usage_calculator_reset_zone_hint`, placeholder
  `America/New_York` when the daemon shows the zone as a suggestion).
- `datawatch-app/src/i18n/`: the three new keys `en.json`, `de.json`, `es.json`,
  `fr.json`, `ja.json` (the label, the hint and the server-zone note).

### New locale keys to add (three)

```
"settings_usage_calculator_reset_zone": "Usage calculator reset time zone (IANA name; empty follows the server's zone)"
"settings_usage_calculator_reset_zone_hint": "An IANA zone name such as America/New_York. Empty uses the zone the daemon runs in, falling back to UTC if that cannot be found. A fixed offset such as +02:00 is not accepted."
"settings_usage_calculator_reset_zone_server_note": "server zone — empty in the config; the reset follows this daemon's local time zone"
```

### API contract surfaces (the new fields)

The daemon changed two responses; the native client reads them, so they are
parity surfaces too (contract:
`docs/plans/2026-10-25-usage-calculator-mobile-contract.md`):

- **GET /api/config** — `usage.calculator.reset_zone` default changed from
  `UTC` to **empty** (empty = the zone the daemon runs in, falling back to
  UTC). Two display fields appear beside it: `usage.calculator.reset_zone_source`
  (`"configured"` when the operator set a zone, `"server"` when the value
  follows the daemon's local zone) and `usage.calculator.reset_zone_fell_back`
  (bool; true only when no zone could be resolved and UTC stood in). Saving
  empty (`PUT /api/config` with `""`) restores the server default — the same
  rule as the PWA: the input saves the value the operator typed, never a
  rewritten default.
- **GET /api/usage/advice** — the advice object reports the zone it ran in:
  `zone` (string; the effective IANA zone, the operator's set zone or the
  daemon's local zone) and `server_zone` (bool; true when the operator left
  the zone unset, so the reset follows the daemon's local time zone).

## Parity surface

The reset-zone row on the settings surface (label, hint, placeholder and the
"server zone" note shown exactly when `reset_zone_source` is `server`), plus
the two API contract surfaces above, must behave identically in
`datawatch-app` and in the PWA (this repo, `internal/server/web`) and
`datawatch-desktop` (`app/renderer/src`):

```
PWA:                internal/server/web (usage calculator section, Settings > General)
datawatch-app:      src/app/screens/SettingsScreen.tsx
datawatch-desktop:  app/renderer/src  (usage calculator settings card)
```

Verify by reading the current source of each (the PWA implementation in this
branch is the reference) before filing or updating the issue; the rule: what
the user sees and saves on one surface, the other surfaces show and save
identically.

## How to verify

1. `en.json` carries `settings_usage_calculator_reset_zone`,
   `settings_usage_calculator_reset_zone_hint` and
   `settings_usage_calculator_reset_zone_server_note`.
2. `SettingsScreen.tsx` renders the field bound to `usage.calculator.reset_zone`
   with the label and hint keys, and shows the server-zone note under the
   input when `reset_zone_source === "server"` (no note when a zone is
   configured).
3. Saving an empty value sends `""` for `usage.calculator.reset_zone` and does
   not fill in the effective zone; the daemon then reports the server zone
   again with `reset_zone_source: "server"`.
4. The advice screen renders `zone` (and that it is the server's, when
   `server_zone` is true) from `GET /api/usage/advice`.

## Context / links

- daemon-side code in this repo: `internal/config` (usage calculator config),
  `internal/server` (advice + config API), `internal/server/web` (PWA
  settings card), `internal/usagecalc` (the reset-zone math).
- PWA tests that pin the behaviour:
  `internal/server/web/app-usage-calculator-settings.test.js` (field list,
  placeholder/note rendering, empty-save semantics).
- Mobile contract (updated): `docs/plans/2026-10-25-usage-calculator-mobile-contract.md`.
- datawatch-app#297 — this surface set (open; update the issue body with the
  third key and the API contract section above).
