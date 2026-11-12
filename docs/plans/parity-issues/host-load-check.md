# Parity issue — host load check (`autonomous.host_load_alert`, datawatch-app)

**Parent release:** the next patch above main at merge time (session-end reap of a worker's leftover processes plus the host load check that backstops it)

## Affected area

Two related daemon-side changes close around the defect "after a worker task was cancelled, processes the worker had started kept running and slowed the host". First, every session end (cancel, kill, failure) now reaps the processes the session left behind using a `/proc` evidence rule — no name matching, exact-pid signals (`internal/autonomous/orphan_reaper.go`, wired in `cmd/datawatch`). Second, a new host load check (`internal/autonomous/host_load.go`) reads the 1-minute load average once a minute, and once it has stayed above `multiple` (default 1.5) times the core count for `minutes` (default 10) consecutive minutes, raises one warning alert titled "Host load high: Automata may be slowed" naming the busiest process groups (5 by default) and whether each belongs to the worktree or scratch folder of a running or an ended Automaton session, proven by the same folder-evidence rule, never by name. The check's mode is `off`, `alert` (default; report only) or `reap` (same alert, plus ending the leftover processes of **ended** sessions the evidence rule proves; a running session is never touched, and `reap` acts only at the moment the alert fires).

The setting `autonomous.host_load_alert` with its three keys `mode`, `multiple` and `minutes` is exposed on every datawatch surface: YAML config, `GET/PUT /api/config` (and `PUT /api/autonomous/config`), the MCP `autonomous_config_get`/`autonomous_config_set` tools (`host_load_alert_mode`, `host_load_alert_multiple`, `host_load_alert_minutes`), the CLI (`datawatch autonomous config-set --host-load-alert-mode reap ...`), the comm channel (`configure autonomous.host_load_alert.mode=reap`; read with `autonomous host-load`), and the PWA Settings → General → Autonomous form, which added three new locale keys — `settings_host_load_alert_mode`, `settings_host_load_alert_multiple`, `settings_host_load_alert_minutes` — in all five locales. `0` for a number reads as the default; negative values and unknown modes are refused on every write surface with an error naming the key, and a change applies on the next minute with no restart.

## Current behavior

On the daemon and the PWA, everything above already works: a cancelled or failed task no longer leaves its processes running (the session-end reap), and the daemon now notices it when a host is nonetheless overloaded, alerts once per breach episode, and — in `reap` mode — cleans up proven leftovers of ended sessions. The PWA operator can read and write all three settings, and the effective settings are readable in plain text from the comm channel. There is no separate status route for the check; the app-visible state is the three config keys plus the alert itself.

## Expected behavior

For the Android and iPhone clients, the parity this work leaves open is narrow, because the check itself is internal and the new surface is only a setting and an alert:

1. **Settings parity.** If the app has an Autonomous settings screen (as the PWA Settings → General → Autonomous does), it should be able to read and write `autonomous.host_load_alert.mode` (`off` / `alert` / `reap`), `.multiple` (number ≥ 0, `0` means default 1.5) and `.minutes` (whole ≥ 0, `0` means default 10) via the existing `GET/PUT /api/config` keys — same defaults, same refusals (the daemon answers 400 naming the key for bad values). If the app does not expose autonomous settings at all, reading the keys is sufficient and writing is optional.
2. **Alert parity.** The new daemon alert "Host load high: Automata may be slowed" must be deliverable on the app's alert channels like any other daemon alert; the body already names each busy group and whether it belongs to a running or an ended session folder, so no app-side enrichment is needed.
3. **No misleading UI.** Nothing in the app should suggest that a cancelled task can keep processes running, and the app must not present the host load check as a per-session control — it is a host-level check with one action (`reap` acts only at an alert, on ended sessions only).

## Suggested next step

File one `[sync]` issue on datawatch-app covering the three points above: read the three `autonomous.host_load_alert` keys from `GET /api/config`, expose them on the Autonomous settings surface if one exists (write via `PUT /api/config`, surfacing the daemon's 400 text on refusal), and deliver the "Host load high: Automata may be slowed" alert. Capability parity is required, implementation parity is not. Three locale keys (`settings_host_load_alert_mode`, `settings_host_load_alert_multiple`, `settings_host_load_alert_minutes`) were added in five locale files, with English placeholders in four (de, es, fr, ja) pending the Android app's translations; the operators of record read the effective values from the comm channel (`autonomous host-load`). Separately, worth confirming on the app: the `session_end_reap` audit action from the reaper fix should render with its raw name or a generic label rather than dropped, since the app may key off a fixed audit-action set.

## Filing record

Not filed. This is a draft only; no issue was created on any tracker and no hosting CLI was run.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included | `autonomous.host_load_alert.mode/.multiple/.minutes` round-trip via `GET/PUT /api/config` (and the autonomous config endpoints), with the same defaults and the same 400 refusals naming the key. The snapshot rides the existing `GET /api/stats` and `GET /api/autonomous/status`. |
| MCP | included | `autonomous_config_get`/`autonomous_config_set` expose the three keys, and `autonomous_status` returns the status body verbatim, which now carries the `host_load` object. |
| CLI | included | `datawatch autonomous config-set --host-load-alert-*` writes the setting; a separate host-load status-printing CLI surface was deliberately not added (`docs/plans/host-load-status-surfaces.md`, non-applicable row). |
| comm channel | included | `configure autonomous.host_load_alert.mode=reap` writes; `autonomous host-load` reads the effective values in plain text. |
| YAML/config | included | The `autonomous.host_load_alert` block in `config.yaml`; the check reads it live each minute, so no restart. |
| PWA | included | Settings → General → Autonomous form with the three new locale keys in all five bundles; the "Host load high: Automata may be slowed" alert rides the existing alert feed. |
| Android | excluded | No app code in this repo; capability parity (read/write the three keys, deliver the alert, not present the check as a per-session control) is required and tracked by this draft — implementation parity is not. |
| iPhone/iOS | excluded | Same as Android — tracked by this draft, no code to write here. |
