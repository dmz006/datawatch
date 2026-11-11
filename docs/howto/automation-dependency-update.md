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
4. **Confirm the schedule and the model** the Automation runs with; both are
   operator choices.

## See also

- [Container image refresh](container-image-refresh.md): the daily image
  rebuild, which is unrelated to dependency versions.
- [Operations: Dependency updates](../operations.md#dependency-updates-security-only-dependabot-and-the-weekly-automation)
- Upstream monitoring maintainer guide:
  [2026-11-01-upstream-monitoring-maintainer-guide.md](../plans/historical-plans/2026-11-01-upstream-monitoring-maintainer-guide.md)
