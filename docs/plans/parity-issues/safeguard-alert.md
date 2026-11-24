# Safeguard alert: what the Android and iPhone apps need

**Status:** draft, not filed. File it against datawatch-app when the datawatch release that carries it ships.

## Affected area

An interim alert that tells the operator when a safeguard is lowered by a caller that is neither the web app nor a session credential. Until real caller identities exist, the local API needs no token, so a script is indistinguishable from the operator.

- Covered actions: a pause resume that takes effect (`POST /api/pause/resume`), an attempt-count force reset (`POST /api/autonomous/prds/{id}/reset_task` with `force`, made as the operator) and, once it lands, a force run of already shipped work. A session credential never raises it (it is refused or demoted as before).
- The alert is an ordinary system alert (`GET /api/alerts`, level `warn`, source `system`). The title starts "Pause resumed by a script or CLI caller" / "Attempt count force reset by a script or CLI caller" / "Shipped work force run by a script or CLI caller". The body has four lines: `Action:`, `Target:`, `Reason:` (the reason, or "no reason given") and `Caller:` (claimed actor, remote address, user agent, credential kind). The text is English; the apps show the alert as the daemon sends it.
- The web app is told apart by a request header, `X-Datawatch-Client: pwa`, sent by `apiFetch`. It is not a secret and can only hide the alert; every call that carries it is written to the audit log as `safeguard_action` with the marker, the remote address and the user agent.
- `reset_task` takes an optional `reason` string (REST body, MCP `reason`, CLI `--reason`, chat `reason <text>`).
- Setting `audit.safeguard_alert` (bool, default true), in Settings -> General -> Audit & Access Log. Locale key `settings_audit_safeguard_alert` in en, de, es, fr and ja.

## Current behavior

The apps list the alert like any other system alert, so nothing breaks. If an app has a pause or force-reset control, its calls carry no marker, so every resume or force reset from the phone raises an alert that names the phone's address and user agent.

## Wanted

- Show the new alert text in the alert list and the push notification as sent.
- If the app resumes a pause or force-resets a task, send the client marker so the operator's own action does not alert. The daemon accepts only the value `pwa` today; whether to accept `android` and `ios` is an open decision for the operator (a marker is forgeable either way, so a wider set changes nothing about what it can hide).
- Add the `audit.safeguard_alert` toggle with the five translations of `settings_audit_safeguard_alert`.
- Pass a reason on a force reset if the app has the control.

## Parity surface

REST, MCP, CLI, comm channel, YAML/config and PWA are done. Android and iPhone are this draft.
