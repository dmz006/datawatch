# [sync] Vault-unreachable alert threshold setting (secrets.vault.unreachable_alert_after_seconds) in PWA Settings (datawatch next release)

**Not filed yet.** This is the tracked draft. Nothing has been created on any tracker. The operator files the issue when ready.

Parent release: datawatch next release, the one that removes the PWA Vault-unreachable badge from the Settings nav button and replaces it with a real alert.

## Affected area

The Vault-unreachable alert and its configurable threshold, `secrets.vault.unreachable_alert_after_seconds`. The daemon side covers the config field in `internal/config/config.go`, the REST config surface in `internal/server/api.go`, the Vault health monitor in `internal/server/vault_health.go`, the MCP secrets tool in `internal/mcp/secrets.go`, the startup wiring in `cmd/datawatch/main.go`, and the entry in `docs/config-reference.yaml`. The PWA side is the "Vault unreachable alert after (seconds)" number field in the Settings secrets card (`internal/server/web/app.js`) with its label and help strings in the `en`, `de`, `es`, `fr` and `ja` locale files. The companion clients (datawatch-app, Android and iOS) share the same Settings secrets card and the same alert feed.

## Current behavior

The daemon now watches the active Vault backend. When it stays unreachable for the threshold, it raises one system alert through the normal alert store. It raises one info alert when the Vault recovers. The threshold defaults to 120 seconds, 0 turns the alert off, and a negative value is rejected. It can be set by config file, by the REST config API and by the MCP tools, and it takes effect live.

The PWA exposes the threshold as a number field in Settings, translated in five locales. Because the Vault problem is now a real alert, the PWA no longer shows a passive badge on the Settings nav button.

The companion clients have not been changed. They have no field for the threshold, so an operator on a phone cannot see or change it. If they still draw a Vault-unreachable badge on their Settings entry, they keep a marker the PWA has dropped. Whether the new alerts reach them depends on how each client reads the alert feed, which has not been checked.

## Expected behavior

Every client treats Vault trouble the same way. The Settings entry carries no Vault badge. A Vault outage appears as an alert on every surface (PWA, mobile, messaging channels), and so does the recovery notice. Each client's secrets settings has the threshold field, with the same default of 120, the same meaning of 0 as off, and the same rejection of negative values. The label and help text are translated in each client's supported locales, using the wording of the five PWA locale strings.

## Suggested next step

File one datawatch-app parity issue. It should ask the app team to remove any Vault-unreachable badge from the Settings entry, to confirm that the Vault outage and recovery alerts show in the app's alert list, and to add the threshold field to the secrets settings, reading and writing the key above through the config API. Before filing, the operator should confirm where the field sits on the app. The app team should reuse the PWA label and help text so the wording matches across clients. If the alerts do not reach the app, record that as a separate gap before the app work starts.
