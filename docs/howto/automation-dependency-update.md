---
docs:
  index: true
  topics: [autonomous, automation, dependencies, dependency update, weekly, dependabot, security updates, major updates, upgrade note, protected paths, alert]
exec_steps:
  - tool: dependency_update_status
    description: Read the weekly dependency update status - the settings in effect, the next scheduled run in local time and the last run with its outcome
    args: {}
    read_only: true
  - tool: autonomous_config_get
    description: Read the autonomous settings, including the autonomous.automation.dependency_update block
    args: {}
    read_only: true
  - tool: get_config
    description: Read the full configuration to confirm the three dependency update settings
    args: {}
    read_only: true
---

# Dependency update

The **weekly dependency update** is one datawatch Automation that brings every
dependency up to date together. It replaces the stream of single-package
version-update pull requests that Dependabot used to open: each of those
rebased on every push and started the same CI jobs, so they competed for the
shared API rate limit and overlapped with each other. One weekly Automaton
makes one branch and one change for review instead.

## Base requirements

- A running daemon with the autonomous engine enabled.
- The repository added as a project, so the Automaton has a directory to work in.

## What the weekly run does and when

By default it runs **Monday at 05:00, in the daemon's local time**. Each run
creates one Automaton that:

1. **Checks three places** for outdated dependencies: the Go modules, the
   container base image tags (including tags built from build arguments), and
   the pinned GitHub Actions.
2. **Updates minor and patch versions together** in one task. Security updates
   are included and labelled as security in the change.
3. **Gives each major update its own task**, with a short upgrade note written
   from the project's release notes. At most `max_majors` majors are taken in
   one run; the rest wait for a later run.
4. Fixes any build error the updates caused, runs the repository's full checks,
   and ends as **one branch and one change for review**.

If nothing is outdated the run makes no change, writes no Events row and raises
no alert.

## How to read its change

The change description opens with a one-line count (updated, dropped, skipped,
failed its tests) and then a summary table with these columns:

| Column | Meaning |
|---|---|
| Dependency | The module, image or action name |
| Old version | The version before the run |
| New version | The version the run moved it to |
| Kind | `security`, `major`, `minor` or `patch` |
| Test result | `passed`, `failed`, `dropped` or `skipped` for that dependency |

Rows are sorted security first, then major, minor and patch.

The **Needs your attention** section lists every major update and every update
that failed or was dropped. A failed or dropped update shows the last lines of
its failing output so you can see why. A major shows its upgrade note.

A dependency is **dropped** when the full checks failed because of it and could
not be fixed: only that update is reverted, the rest of the batch continues,
and the dropped update is named in the change. One bad dependency never blocks
the whole run. A dropped update is picked up again in the next run.

The run is also reported on the Automata Events tab as one `check` row, such as
"2 updated, 1 dropped, 0 skipped". An alert is raised only when the run needs
you, once per run: a dropped or failed update, a task waiting on a protected
paths question, or a run that could not start.

## How to skip a major

Any one of these:

- **Take fewer majors.** Set `max_majors` lower (0 means no majors at all).
- **Cancel that major's task** in the Automaton before it runs.
- **Skip it when its review comes up**, if you would rather leave the
  dependency where it is.

A skipped major is listed as skipped in the change and is offered again next
week.

## How to turn it off

Set `autonomous.automation.dependency_update.enabled` to `false`, from any of:

- **Settings** in the PWA (Automata section, Dependency update toggle).
- **config.yaml**: the `autonomous.automation.dependency_update` block.
- **Comm channel**: `configure autonomous.automation.dependency_update.enabled=false`.
- **CLI**: `datawatch config set autonomous.automation.dependency_update.enabled false`.
- **API**: `PUT /api/config` with the same dotted key.

The MCP tool `config_set` also works. `dependency_update_status` shows the
setting in effect and leaves `next_run` empty while it is off.

## Settings

A bad value is refused on every write path and when the config loads. A
schedule change applies from the next scheduled run.

| Key | Default | Allowed |
|---|---|---|
| `autonomous.automation.dependency_update.enabled` | `true` | `true` or `false` |
| `autonomous.automation.dependency_update.schedule` | `monday 05:00` | `weekday HH:MM`: a weekday name (monday to sunday) and a 24-hour time, in the daemon's local time |
| `autonomous.automation.dependency_update.max_majors` | `5` | a whole number from 0 to 20; 0 means no major updates in a run |

Read the settings in effect, the next run and the last run with the MCP tool
`dependency_update_status` (read-only), or with `datawatch config get`, the
REST `GET /api/config`, or the Settings page.

## Dependabot and workflow files

**Dependabot stays on for security updates only.** `.github/dependabot.yml`
sets `open-pull-requests-limit: 0` on every ecosystem, which turns off
version-update pull requests but not security ones, so each advisory still gets
its own pull request. `make lint` keeps that limit at zero.

Updating a pinned action edits files under `.github/workflows`. Those are
protected paths: the edit **raises the protected paths question and waits for
your answer**. There is no bypass and the weekly run never authorises those
paths in advance. See [Automaton protected paths](automaton-protected-paths.md).

## Operator checklist: what only you can check

Some parts of the change live outside this repository, so nothing in the
repository can verify them. Check each once after the change lands:

1. **The hosting setting.** In the repository's hosting settings (Settings,
   Code security and analysis), **Dependabot security updates must stay
   enabled**. `.github/dependabot.yml` limits version updates to zero but does
   not turn security updates on. If that setting is off, advisories get no pull
   request at all and nothing else will tell you.
2. **The Automation is on and scheduled.** Run `dependency_update_status` (or
   read the three settings with `datawatch config get`). It should show
   `enabled` as true and a `next_run` in local time. An empty `next_run` means
   the weekly run is off.
3. **Leftover pull requests.** Version-update pull requests that Dependabot
   opened before the change are not closed by it. Review the open ones and
   close each that the weekly run now covers, so the same dependency is not
   updated twice. Open security pull requests stay: merge those as usual.
4. **The first run.** Read its change description, answer the protected paths
   question when a pinned action needs a workflow file edit, and decide each
   major from its upgrade note. A dropped update comes back next week, so a
   repeated drop of the same dependency is yours to look at.
5. **Mobile apps.** The Android and iPhone settings group, Events row and
   alert are not built yet. They are tracked in
   [the datawatch-app parity issue draft](../plans/parity-issues/weekly-dependency-update-settings.md),
   filed as https://github.com/dmz006/datawatch-app/issues/458; until it ships, change the settings from the PWA,
   chat, the CLI or the API.

## See also

- [Repo CI watch](automation-repo-ci-watch.md): the standing watch over CI that
  will show the run of the resulting change.
- [Container image refresh](container-image-refresh.md): the daily image
  rebuild, which is unrelated to dependency versions.
