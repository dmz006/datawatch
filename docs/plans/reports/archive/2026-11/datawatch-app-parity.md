# datawatch-app Parity Status

## (a) New locale keys added to `internal/server/web/locales/en.json`

`about_version_skew_note`
`settings_showing_server`

(Taken from `git diff main -- internal/server/web/locales/en.json`; the two
removals in that diff, `session_chip_stalled` and
`capacity_card_backoff_rung`, are not new keys and are excluded.)

## (b) Parity status of the peer-version comparison in the app

not verified from this worktree

(No note inside this repository verifies the separate datawatch-app client.
`docs/plans/reports/version-skew-audit.md` documents the embedded server PWA's
version-skew behavior, which is not the app client.)

## (c) Draft issue

**Title:** `feat(i18n): add version-skew keys for multi-server About parity`

**Body:**

The multi-server version-skew work in this repository shipped two new strings
that need translations. Please provide DE/ES/FR/JA translations for both keys.

### Keys and English values

- `about_version_skew_note`:
  `This server is running v%1$s, a different release than v%2$s, the one serving this page. Behavior is unchanged and no reload is needed.`
- `settings_showing_server`:
  `Showing: %1$s`

### Screens they appear on

- About — version-skew note (`about_version_skew_note`)
- Showing line (`settings_showing_server`)
- Settings — server label (`settings_showing_server`)

### Translation request

Please supply DE/ES/FR/JA translations for both keys listed above.

### Additional item

1. Please confirm that the app does **not** reload when switching servers —
   this worktree cannot verify the app-side behavior, so an explicit
   confirmation from the app side is requested.

## (d) Filing status

The issue is not filed from here, because gh is out of scope.
