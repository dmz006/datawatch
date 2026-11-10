# [sync] PWA Settings nav no longer shows the Vault-unreachable badge; Vault status row shows last error and last success (datawatch next release)

Parent release: datawatch next release

## Affected area

The datawatch PWA Settings navigation button and the Vault status row in the secrets settings. The mobile and companion clients (datawatch-app, Android and iOS) have the same Settings entry point and the same Vault status display. Files changed on the PWA side: `internal/server/web/app.js`, the tests `app-vault-nav-badge-removed.test.js` and `app-vault-status-row.test.js`, and the locale files `en.json`, `de.json`, `es.json`, `fr.json` and `ja.json`.

## Current behavior

The PWA no longer draws a "Vault unreachable" badge on the Settings nav button. That badge was a passive marker that only showed while the Vault was down, and it was easy to miss or to misread as a generic Settings warning. The nav button is now plain.

The Vault status row in Settings now shows two extra lines: the last error (or "none") and the last success (or "never"). These use new locale keys `vault_last_error_label`, `vault_last_error_none`, `vault_last_success_label` and `vault_last_success_never`, translated in all five PWA locales.

The companion clients have not been changed. If they still show a Vault-unreachable badge on their Settings entry, they keep a marker the PWA has dropped. They also do not show the last error or last success on the Vault status row, so an operator on a phone sees less detail than on the PWA.

## Expected behavior

All clients treat Vault trouble the same way. The Settings entry carries no Vault badge. A Vault outage is raised as a real alert through the normal alert path, so every surface (PWA, mobile, messaging channels) sees it. The Vault status row on every client shows reachable or unreachable plus the last error and last success, and the four new strings are translated in each client's supported locales.

## Suggested next step

File one datawatch-app parity issue. It should ask the app team to remove any Vault-unreachable badge from the Settings entry, to add the last error and last success lines to the Vault status row using the same wording as the four locale keys above, and to confirm that Vault outages reach the app as alerts. Check the alert path first: if the real Vault alert is not yet raised by the daemon, that is a separate gap to record before the app work starts. The operator should confirm the placement of the new lines on the app before the issue is filed.
