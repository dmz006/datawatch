# [sync] Silent observer peer alert and silent_alert_minutes setting; PWA peer-stale badge removed (datawatch next release after v9.90.2)

**Not filed yet.** This is the tracked draft. The operator opens the issue on the
datawatch-app repo when they choose to. Writing this file does not file anything and
changes nothing outside this repo.

Parent work: PRD `20070001`, operator request "Remove the PWA peer-stale badge from the
Settings nav button and replace it with a real connectivity alert".

## Affected area

- The Settings nav button in the web app (the red stale-peer count badge, now removed).
- The Settings screen, Observer section: a new field for the silent peer threshold.
- The Alerts list: two new system alerts about observer peers.
- The config surfaces: REST config, CLI `config set`, MCP `config_set`, and the chat `configure` command.
- The five PWA locale bundles (en, de, es, fr, ja).

## Current behavior

The daemon now runs a peer silence watcher (`internal/observer/peer_silence.go`), started
in `cmd/datawatch/main.go` only when the observer peer registry is created, which happens
when `observer.peers.allow_register` is true. With registration off, no watcher runs. Every minute it checks each registered
observer peer, skipping the daemon's own row. If a peer has not pushed stats for longer
than `observer.peers.silent_alert_minutes`, the watcher raises one warning system alert,
"Observer peer silent: <name>", through the existing alerts store. A peer that never
pushed is measured from its registration time. It raises one alert per outage, not one
per check. When the peer pushes again, it raises one info alert, "Observer peer
recovered: <name>". A deleted peer's outage state is dropped without an alert.

The setting `observer.peers.silent_alert_minutes` is a whole number of minutes. The
default is 5, and 0 turns the watcher off. It is read and written through REST config
(`internal/server/api.go`), `config set` on the CLI, the MCP config tool, and the chat
`configure` command, whose help text lists the key. Each surface has a test. The PWA
Settings screen has a number field for it, labelled "Silent peer alert after (minutes;
0 = disabled)", with a hint, translated in all five locales. The PWA Settings nav badge
and its 30-second poll of the peers endpoint are gone. The alert replaces them, so a
silent peer now reaches the operator through the Alerts list and any channel that
forwards alerts. It is no longer a count on one nav button.

## Expected behavior

The mobile app should reach the same behavior:

- Show the two new system alerts like any other alert. No special handling is needed if
  the app already renders system alerts generically. It should not rely on the badge.
- Remove the Settings-nav stale-peer count badge and its separate peers poll, if the app
  built the earlier breadcrumb, because the PWA has no counterpart any more.
- Add a "Silent peer alert after (minutes)" number field to the app's Observer settings.
  The minimum is 0, 0 means disabled, and it reads and writes
  `observer.peers.silent_alert_minutes`. Use the same label and hint wording in the
  app's locale bundles.
- Do not copy the watcher logic into the app. It runs only in the daemon.

## Open question for the operator

The existing draft `2026-11-10-pwa-peer-stale-badge-removal-parity.md` describes a
per-row "Stale / Never pushed" note in the Observer peer list as the replacement. The
code on this branch implements the alert-based replacement instead. I found no
`peerStaleBadge` references left in `app.js`, but I did not verify that the per-row
note exists. That draft should be checked against the branch and corrected or merged
into this one before either is filed.

## Suggested next step

Operator decision: file one Mobile-Parity issue on datawatch-app covering the Settings
field, the removal of the badge, and the alert display, using this file as the body after
the draft conflict above is settled. Link PRD `20070001`.

## Files this work touched

`internal/observer/peer_silence.go` (watcher), `internal/config/config.go` (config field
and default), `internal/server/api.go` (REST config read and write),
`internal/mcp/observer.go` and `internal/router/router.go` (MCP and chat surfaces),
`cmd/datawatch/main.go` (startup wiring and CLI help), `internal/server/web/app.js` and
`internal/server/web/locales/{en,de,es,fr,ja}.json` (Settings field and labels),
`internal/server/v5280_locales_test.go` (locale guard), plus the matching
`peer_silence*_test.go` files in the observer, config, server, cmd and mcp packages, plus the router's
`peer_silence_configure_test.go` (the chat configure surface).
