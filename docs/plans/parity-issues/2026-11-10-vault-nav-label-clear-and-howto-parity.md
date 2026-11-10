# [sync] PWA Settings nav label stays clear when the Vault is unreachable; Vault outage alert documented (datawatch next release)

Parent release: datawatch next release

## Affected area

The Settings entry point on the datawatch PWA bottom navigation, and the Vault outage alert that replaces the old badge. The mobile and companion clients (datawatch-app, Android and iOS) have the same Settings entry and the same alert surface. The PWA side is covered by the story `scripts/test-stories/pwa/vault-nav-label-clear.mjs` and by the "Vault-unreachable alert" section of `docs/howto/secrets-manager.md`.

## Current behavior

On the PWA, a Vault that is unreachable no longer draws anything on the Settings nav button. The story forces the Vault status to unreachable at a 390 px viewport and checks two things: nothing except the nav button and its label sits over the centre of the Settings label, and the retired `#vaultUnreachableBadge` element does not exist. Before this work the badge covered the label on narrow screens.

The how-to now explains the replacement. When the Vault stays unreachable for `secrets.vault.unreachable_alert_after_seconds` (default 120, `0` turns it off, negative values are rejected), one Vault unreachable error alert is raised for that outage. It carries only the Vault address and the last error, with secret values scrubbed. It appears in Alerts in the PWA and goes to messaging channels. The how-to shows the setting through YAML, REST, the CLI, MCP and chat commands, and through the PWA delay field in Settings, Secrets Store.

The companion clients have not been changed or checked. If they still draw a Vault-unreachable marker on their Settings entry, it can sit over the label the same way. They also have no field for the alert delay, so an operator on a phone cannot read or change it. There is no story that checks the app Settings entry the way the PWA story does.

## Expected behavior

Every client keeps its Settings entry clear of Vault markers at narrow widths, with the label fully readable when the Vault is down. A Vault outage reaches the operator only as the real alert, shown in the app alert list and in messaging channels. The app Secrets settings show the alert delay with the same meaning as the PWA field: whole seconds, `0` is off, negative values are refused. Each client has a check that mirrors the PWA story: force the Vault unreachable on a narrow screen and confirm nothing overlaps the Settings label.

## Suggested next step

Add this to the datawatch-app parity issue already drafted for the removed badge (`2026-11-10-pwa-vault-nav-badge-removed-parity.md`) rather than filing a second one. The extra asks are the narrow-screen label check and the alert delay field. The alert delay field is also covered by `2026-11-10-vault-unreachable-alert-threshold-parity.md`, so keep the wording the same. The operator should confirm where the delay field goes in the app Settings before the issue is filed. This draft only records the gap. Nothing was filed on any host.
