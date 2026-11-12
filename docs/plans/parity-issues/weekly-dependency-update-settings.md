# The weekly dependency update settings, Events row and alert are missing from the Android and iPhone apps

**Filed:** https://github.com/dmz006/datawatch-app/issues/458 at merge (datawatch v9.151.0). The operator checklist in
`docs/howto/automation-dependency-update.md` lists the mobile apps as open until the
issue ships. Filing record below.

## Affected area

The three settings of the weekly dependency update Automation, all under
`autonomous.automation.dependency_update`:

- `autonomous.automation.dependency_update.enabled` — switch for the weekly
  run (unset reads on).
- `autonomous.automation.dependency_update.schedule` — `weekday HH:MM` in the
  daemon's local time (unset reads `monday 05:00`).
- `autonomous.automation.dependency_update.max_majors` — how many major
  updates one run may include, 0 to 20 (unset reads 5, 0 means none).

Where they live:

- Config block and validation: `DependencyUpdateConfig` in
  internal/config/dependency_update.go (a bad schedule or an out-of-range
  `max_majors` is refused on every write and on config load).
- Locale keys: `settings_dependency_update_enabled`,
  `settings_dependency_update_schedule`, `settings_dependency_update_max_majors`
  and the matching `settings_dependency_update_*_help` keys, in every file
  under internal/server/web/locales.
- Settings page group: the "Dependency update" rows in the autonomous
  automation settings list in internal/server/web/app.js (toggle, text field
  with the placeholder `monday 05:00`, number field 0 to 20), each linking the
  how-to `howto/automation-dependency-update.md`.
- Read and write: `GET /api/config` and `PUT /api/config` with the dotted keys
  above.
- MCP status tool: `dependency_update_status` in
  internal/mcp/dependency_update_tools.go, read-only; it returns the three
  settings as in effect, the next scheduled run and the last run (when, which
  Automaton, and the outcome: updated, nothing to update, or needs attention).
- Events row: a finished run is one `check` row on the Automata Events tab,
  such as "2 updated, 1 dropped, 0 skipped"; a run with nothing outdated writes
  no row.
- Alert: raised only when the run needs the operator, once per run: a dropped
  or failed update, a task waiting on a protected paths question, or a run that
  could not start.

## Current behavior

The daemon and the PWA carry all of it. The Android and iPhone apps have no
Dependency update group in Settings, so the operator cannot switch the weekly
run off, move its day and time, or cap its majors from a phone. The apps' Events
list has no weekly run row type that they know how to show, and the alert is
not recognised as one that needs attention.

## Expected behavior

Capability parity with the PWA; each app uses its own idiomatic controls.

- Settings shows a Dependency update group with three controls:
  - an on/off switch for `enabled` (default on), with the help text from
    `settings_dependency_update_enabled_help`;
  - a text field for `schedule` (placeholder `monday 05:00`, default
    `monday 05:00`), with the help text from
    `settings_dependency_update_schedule_help`, which says a change applies from
    the next scheduled run;
  - a number field for `max_majors` (whole number 0 to 20, default 5, 0 means
    no majors), with the help text from
    `settings_dependency_update_max_majors_help`.
- Each control shows its label and help text from the same locale keys the PWA
  uses, in every language the apps ship, and offers the link to the
  "Dependency update" how-to.
- A save sends the dotted key to `PUT /api/config` and shows the daemon's 400
  message unchanged when it refuses a bad schedule or an out-of-range
  `max_majors`.
- The Events view shows the weekly run as a `check` row with its summary line,
  and shows nothing when a run had nothing to update.
- The alert from a run that needs the operator appears in the apps' alerts list
  like any other alert and opens the Automaton it names.

## Acceptance

- [ ] Settings has a Dependency update group with the three settings, labels and
      help text from the `settings_dependency_update_*` keys in every shipped
      language.
- [ ] `enabled` reads and writes through `GET` and `PUT /api/config`; an unset
      value shows as on.
- [ ] `schedule` reads and writes through the same calls; an unset value shows
      the `monday 05:00` default; the form of `weekday HH:MM` is explained in
      the help text.
- [ ] `max_majors` accepts only whole numbers 0 to 20, shows 5 when unset, and
      accepts 0.
- [ ] A refused value shows the daemon's 400 text unchanged and leaves the
      stored value as it was.
- [ ] A save of one of the three settings does not change the other two or any
      other key.
- [ ] The Events view lists the weekly run's `check` row and no row for a run
      with nothing outdated.
- [ ] The alert for a run that needs the operator is shown once per run.
- [ ] Android and iPhone show the same labels, defaults and ranges as the PWA.

## Filing record

Filed at merge as https://github.com/dmz006/datawatch-app/issues/458 (datawatch v9.151.0) against the mobile app repository;
the issue number is recorded here. Drafted 2026-11-11 from the
current tree: internal/config/dependency_update.go, the settings rows in
internal/server/web/app.js, the locale keys, internal/mcp/dependency_update_tools.go
and docs/howto/automation-dependency-update.md.
