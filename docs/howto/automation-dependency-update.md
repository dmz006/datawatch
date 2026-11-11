---
docs:
  index: true
  topics: [dependencies, dependabot, security, automation, ci, maintenance]
exec_params: []
exec_steps: []
---
# Dependency updates: security-only Dependabot plus a weekly Automation

Dependency updates reach this repository by two separate routes. Dependabot
handles **security** updates only. A **weekly Automation** handles every
**version** update, so the whole set moves together.

## What changed

Before, Dependabot opened one version-update pull request per package and per
ecosystem every day. Those pull requests each rebased on every push to main and
each started the same CI jobs. Now:

| Kind of update | Who does it | Result |
|---|---|---|
| Security update (an advisory exists) | Dependabot, security updates | One reviewable pull request per advisory |
| Version update (Go modules, Docker base images, GitHub Actions) | The weekly dependency update Automation | One Automaton, one branch, all ecosystems together |

## How Dependabot is held to security-only

`.github/dependabot.yml` keeps its three ecosystems (`gomod`, `docker`,
`github-actions`) and sets `open-pull-requests-limit: 0` on every one. That
limit turns off version-update pull requests. It does not limit security
updates, which are governed by the repository's security settings instead.

The file also keeps these rules, each explained in its header:

- No auto-merge. A human merges every Dependabot pull request.
- No custom labels, no cooldown and no `ignore` entries (an ignore rule could
  hide a security update).

### The lint check

`scripts/check-dependabot-security-only.sh [path]` fails (exit 1) when:

- any `package-ecosystem` entry lacks `open-pull-requests-limit: 0`;
- `gomod`, `docker` or `github-actions` is missing;
- the file has an uncommented `ignore:` key.

It exits 2 on a usage error (unreadable file, more than one argument).
`scripts/check-dependabot-security-only_test.sh` exercises those cases, and
`make check-dependabot-security-only` runs both. The target is part of
`make lint`, so a change that re-enables version updates fails lint.

## What the weekly Automation covers

The Automation updates everything Dependabot no longer opens version pull
requests for, plus what Dependabot could never see: Docker `FROM` lines built
from build arguments (for example `golang:${GO_VERSION}-...` and
`${REGISTRY}/agent-base:${BASE_TAG}`). Do not try to pin those tags in
`dependabot.yml`.

Review its pull request like any other: a human merges first, and breaking
changes in a major bump are fixed in the same Automaton's work.

## Where a run is reported

Each run is reported on the Automata Events tab (see
[Automata Events](automata-events.md#the-weekly-dependency-update)):

- A run that updated something, or needs attention, writes one `check` row with
  a one-line summary such as "2 updated, 1 dropped, 0 skipped".
- A run with nothing to update writes no row and raises no alert.
- An alert is raised only when the run needs you, once per run: a dropped or
  failed update, a major waiting on a protected paths decision, or a run that
  could not start. An updated run alerts only while its Automaton waits for you.

## Settings

Three keys under `autonomous.automation.dependency_update` control the weekly
Automation. Defaults apply when a key is unset; a bad value is refused on every
write path and when the config loads.

| Key | Default | Allowed |
|---|---|---|
| `enabled` | `true` | `true` or `false` |
| `schedule` | `monday 05:00` | `weekday HH:MM`: a weekday name (monday..sunday) and a 24-hour time, in the daemon's local time |
| `max_majors` | `5` | a whole number from 0 to 20; 0 means no major updates in a run |

Change them from any surface:

- **PWA:** Settings, Automata section: "Dependency update" toggle, schedule and
  major-limit fields (translated in en, de, es, fr and ja).
- **REST / config:** `PUT /api/config` with the dotted key, or edit the YAML
  block (see `docs/config-reference.yaml`).
- **CLI:** `datawatch config set autonomous.automation.dependency_update.schedule "friday 03:00"`.
- **Chat:** `configure autonomous.automation.dependency_update.max_majors=3`.
- **MCP:** `config_set` changes the keys; the read-only `dependency_update_status`
  tool reports the settings in effect, the next scheduled run in local time
  (empty while the Automation is off), and the last run: when, the Automaton
  title and the outcome (`updated`, `nothing to update` or `needs attention`).
  The last-run fields are empty until a run has happened.

A schedule change applies to the next scheduled run.

## Operator checklist (needs a human)

These cannot be verified from the repository files:

1. **Dependabot security updates must be enabled** in the hosting settings
   (Settings, then Code security and analysis). `dependabot.yml` alone does not
   turn them on; if the setting is off, no security pull request will appear.
2. **Confirm the weekly Automation exists and is active.** It was filed as a
   draft; until it is approved and activated, nothing opens version updates.
   Check it on the Automation tab or with `automation_list`.
3. **Close the version-update pull requests that are still open.** Dependabot
   does not close existing ones when the limit drops to zero.
4. **Confirm the schedule, major limit and the model** the Automation runs with;
   all are operator choices. The settings above only have defaults; nothing
   verifies that they suit this repository.
5. **Watch the Events tab and Alerts after the first run.** The row summary and
   the alert rules above are new; confirm they tell you what you need.
6. **Check the parity follow-up:** the mobile apps (datawatch-app) do not yet
   show these settings; that is tracked outside this repository.

## See also

- [Container image refresh](container-image-refresh.md): the daily image
  rebuild, which is unrelated to dependency versions.
- [Operations: Dependency updates](../operations.md#dependency-updates-security-only-dependabot-and-the-weekly-automation)
- Upstream monitoring maintainer guide:
  [2026-11-01-upstream-monitoring-maintainer-guide.md](../plans/historical-plans/2026-11-01-upstream-monitoring-maintainer-guide.md)
