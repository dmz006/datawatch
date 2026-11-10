# [sync] New setting autonomous.verifier_max_run_seconds: a hard maximum run time for one verifier

## Affected area

Configuration of the autonomous (Automata) verifier. This work adds one new key, `autonomous.verifier_max_run_seconds`, to the daemon config in `internal/config/config.go`, with its default and validation in `internal/config/verifier_run_cap.go`. Any client that shows or edits the autonomous settings is affected: the PWA settings page, the Android and iOS apps (datawatch-app), the CLI, MCP config tools and the comm channel config commands.

## Current behavior

The daemon now reads `autonomous.verifier_max_run_seconds` from the config file and returns it in the config object. It defaults to 2700 seconds (45 minutes). An explicit 0 is accepted, kept as written, and means no cap. A negative value is refused when the config loads, with an error that names the key. This step adds only the setting and its validation. Nothing yet enforces the cap on a running verifier, and no client surface offers the key, so the only way to change it today is to edit the config file or use the generic config set path. The mobile apps do not know the key, so they would hide it or show it as an unlabelled raw field, and they have no locale text or validation hint for it.

## Expected behavior

Every surface that exposes the autonomous settings offers the key with the same meaning. Each one shows the value in seconds, states that 0 means no cap, rejects negative numbers before sending, and surfaces the daemon's validation error if one comes back. Labels and help text exist in all five locales. Once enforcement ships, the stats and session views that report a verifier ended by the cap should be rendered by the apps too, so an operator can tell a capped verifier from one that failed.

## Suggested next step

Open a datawatch-app issue for the Android and iOS settings screens once the later entries of this change land (enforcement, settings surfaces and stats), so the issue can list the exact REST fields, the end reason text and the locale keys in one place. Until then, no app change is needed: the default applies on upgrade, nothing is required of existing configs, and unknown config fields are ignored by the apps as before. This draft is a file only; no issue has been created on any tracker.
