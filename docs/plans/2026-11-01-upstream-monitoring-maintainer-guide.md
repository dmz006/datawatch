# Upstream monitoring — maintainer guide

**Date:** 2026-11-01
**Version:** none (plan document; plan-doc and CI changes are exempt from the version bump, and the merge helper assigns the release number at merge time)
**Tracker:** registry row 198
**PRD:** fb0a0001

## What this is

Maintainer-only tooling for the datawatch repository: it makes sure upstream
security patches are seen and consumed quickly, and that the pinned tools the
image and CI depend on don't silently age. It is built around two small pieces
of repository infrastructure — a Dependabot configuration and one scheduled
workflow — plus this guide and the proposal it closes.

It is **not a daemon feature**. It adds no `datawatch` configuration option,
no REST or MCP surface, no CLI command, no comm-channel message, no PWA card
and no mobile surface. It exists to maintain datawatch's code, not to be part
of what users of datawatch need — datawatch's users are not maintaining
datawatch's code. Nothing here should ever be documented in user-facing howtos
or operations docs as a product capability. See the `## Parity surface`
section at the end for the explicit exclusion.

## What covers what

| Upstream | Watched by | Where |
|---|---|---|
| Go modules (`go.mod` / `go.sum`) | Dependabot | `.github/dependabot.yml` — `gomod`, daily, minor and patch updates grouped into one PR, security updates ungrouped |
| Docker image tags (literal `FROM` lines in `docker/dockerfiles/*` and `Dockerfile.dev`) | Dependabot | `.github/dependabot.yml` — `docker` ecosystem |
| GitHub Actions used in `.github/workflows` | Dependabot | `.github/dependabot.yml` — `github-actions`; every `uses:` is pinned by commit SHA with a trailing `# vN` comment, and Dependabot moves both together |
| Pinned tool versions (`ARG NAME=value` in `docker/dockerfiles/*` and `Dockerfile.dev`, e.g. Go, gh, rtk, kubectl, helm) | upstream-watch job | `.github/workflows/upstream-watch.yaml` running `scripts/upstream_watch.py` — these `FROM` lines are built from ARGs, which Dependabot cannot see |
| The Trivy scanner pin (`TRIVY_VERSION` in the workflows) vs the latest `aquasecurity/trivy` release | upstream-watch job | same workflow and script |
| The Trivy installer fetched from the moving `main` branch | upstream-watch job (monitoring only — never edits the workflow) | same workflow and script; reported as a standing note, plus a finding when the installer text at `main` has drifted from the one at the pinned `v${TRIVY_VERSION}` tag |
| Advisory-only feeds (OSV queries for every pin whose package can be named) | upstream-watch job | same script; tools OSV cannot name are listed under "Not checked" |
| Go vulnerabilities in our code (blocking gate on every push/PR and before release) | existing job — left as is | `govulncheck` in `.github/workflows/security-scan.yaml` and `.github/workflows/release.yaml` |
| Gate on new or upgraded dependency PRs (high severity, license deny list) | existing job — left as is | `.github/workflows/dependency-review.yaml` |
| Base-image rebuild, Trivy rescan of shipped images, stale-suppression detection | existing job — left as is | `.github/workflows/image-refresh.yaml` (daily 05:30 UTC) |
| Accepted-risk registry, daily watch report, dismissed-alert check | existing tooling — left as is | `security/accepted-risks.yml`, `scripts/accepted_risks_daily_watch.py`, `scripts/dismissed_alerts_watch.py` |

**Trivy install step (updated 2026-11-09):** the installer row above
describes the workflows as they were written — an inline
`curl … /trivy/main/contrib/install.sh | sh` fetch from the moving `main`
branch that can fail silently and leave the worker with no scanner. The
repo now carries a hardened installer, `scripts/ci-install-trivy.sh
<tag> <dir>`, which pulls the install script **for the pinned version
tag** (not `main`), into a temp file (no pipe-to-shell), with retries and
a loud failure on a persistent download error. As of 2026-11-09 all eight
"Install Trivy" steps in `release.yaml`, `containers.yaml` and
`image-refresh.yaml` are migrated to it, so the workflows no longer fetch the
installer from `main`. The watcher itself is unchanged, and this guide does not
verify whether its standing-note row now reports no workflow lines; check the
next upstream-watch run before relying on that.

Rule of thumb: Dependabot opens pull requests for the three ecosystems it can
see; the upstream-watch job reports (it never opens a PR, pushes, or edits a
file) what Dependabot cannot see, because the edits for those pins are coupled
and a human makes them.

## Repository settings the maintainer must confirm in GitHub

The following are GitHub-side switches; **none of them can be checked from the
repository** and nothing in this PRD turns them on:

- **Security updates** (Dependabot security updates) — Settings → Code security
  and analysis. Without it enabled, the security-update PRs described below
  never appear; `.github/dependabot.yml` alone does not switch it on.
- **Dependabot alerts** — the advisory feed that tells you about vulnerable
  dependencies even where Dependabot has not (or cannot) open a PR.
- **Dependency graph** — needed for Dependabot to work at all and for
  Security updates to have a list of dependencies to check.

Confirm all three are enabled in this repository's settings. The `upstream-watch`
label used by the tracking issue does **not** need to be created: the workflow
creates it itself (`gh label create ... || true`) before it looks the issue up
by label, and finds the issue by label, never by number.

## Handling a Dependabot pull request

**Security update vs daily version update — how to tell them apart:**

- A security update arrives as its own PR (`.github/dependabot.yml` keeps
  `gomod` security updates ungrouped on purpose). Its body references the
  advisory — a `GHSA-...` / CVE id — and explains what is being fixed.
- A daily version update arrives as the grouped "version updates" PR (minor
  and patch only, one PR per ecosystem), with no advisory reference and a
  `chore(deps)` / `chore(ci)` commit prefix. There is no `cooldown` set in
  `.github/dependabot.yml`.

**Every one is merged by a human at first.** There is no auto-merge anywhere
in the configuration and none is wired in the workflows. The merge policy is:

1. Required CI checks must be green on the PR — build, tests, lint — and the
   `dependency-review` job (`.github/workflows/dependency-review.yaml`) is the
   dependency gate in particular.
2. Read the diff: for a Go module bump that is `go.mod` / `go.sum`; for a
   Docker tag bump, the tag line; for an action, the SHA plus its `# vN`
   comment.
3. For a **non-security** routine update, the AGENT.md 72-hour wait rule still
   applies, applied by hand: if the release is less than 72 hours old, hold the
   merge until it is older (that is the malicious-release control the cooldown
   used to be). For a **security fix**, consume it as soon as it is available —
   no waiting, per the operator change of 2026-10-25.
4. After merging, the usual release path applies: a merged dependency bump
   becomes a release through the normal pipeline.

**Version-bump exemption.** AGENT.md's Versioning rule explicitly exempts
"Dependabot-authored dependency-bump PRs" (they are dependency-only changes —
`go.mod`, `go.sum`, Dockerfile tags, action SHAs — with no behavior change of
our own). `scripts/check-version-bump.sh` implements that: a range in which
every non-merge commit is authored by the exact git author name
`dependabot[bot]` passes without a version bump, in both check modes; merge
commits are ignored, and a range that mixes a Dependabot commit with any other
author, or whose name merely *contains* `dependabot[bot]`, falls back to the
normal bump rules.

One caveat to know about: **git authorship is not authenticated** — `%an` is a
self-declared string anyone can set. The exemption is therefore a *bump
exemption only*; the real gates for these PRs remain the required CI checks and
the `dependency-review` job. Review the diff regardless of who claims
authorship.

**What moved in the latest round of major bumps.** Three groups of upstream
majors were reviewed, and each review is kept under `docs/plans/reports/`. The
mcp-go Go module moved from v0.46.0 to v1.5.0: the one change the code needed
was that both server constructors now pass the legacy server-initiated-requests
option, because v1 otherwise refuses sampling and elicitation requests for
clients on the 2026-07-28 protocol; the tool-result size limit stays off by
default, the registry replacement is harmless because resources and prompts are
added only at startup, and the existing test suites passed unchanged despite the
empty-array and integer-request-ID changes. The channel bridge packages moved
the MCP SDK from 1.31.0 to 1.32.0 (with its transitive packages such as zod
4.3.6 to 4.6.7 and qs 6.16.0 to 6.17.0), and the only change that touches the
bridge code is that a tool call may now arrive without an `arguments` field, so
the handlers that destructure it need a `?? {}` guard; the other packages are
not called directly. The workflow actions moved upload-artifact from v5 to
v7.0.2, setup-qemu from v3 to v4.4.0, golangci-lint-action from v8 to v9, and
the ZAP baseline and full-scan actions from v0.9.0 to v0.15.0 and v0.13.0, with
no workflow input needing to change (the Node 24 runtime matters only on
self-hosted runners); the only edits required are to correct the now-stale
version comments on the two ZAP pins and to decide whether the API-scan action,
still on v0.9.0, should move with them.

## Handling the upstream-watch issue

The workflow `.github/workflows/upstream-watch.yaml` (daily 06:15 UTC, plus
`workflow_dispatch`) runs `scripts/upstream_watch.py` and, when there are
findings, opens or edits one tracking issue titled
`chore: upstream watch - pinned tools and advisories`, labelled
`upstream-watch`. Finding something is not a failure — the job fails only if
the script crashes.

- **What a stale-pin entry means:** the pinned `ARG` value (or `TRIVY_VERSION`)
  is older than the upstream feed's current release, for the named tool. It is
  a report, not a pull request: the workflow's permissions are `contents: read`
  and `issues: write`, and by design the job never edits a file — the edits
  are coupled, so a human makes them.
- **Coupled edits a bump requires** (named in the report body, not just
  "bump it"):
  - `GH_VERSION`: `GH_COMMIT` in the same Dockerfile must move to the new
    tag's commit in the same change — the build verifies the clone HEAD equals
    it, so a version/commit mismatch breaks the image build.
  - Go pins (`GO_VERSION`, `GH_GO_VERSION`): the new value must keep
    `scripts/check-go-toolchain-pins.sh` passing — an exact `X.Y.Z` at or
    above the floor there (currently `1.26.10`), no floating pins like `1.26`
    or `1.27.x`, and not `1.27.0`–`1.27.2` (CVE-2026-111943) — and it must keep
    `scripts/check-dockerfile-go-versions.sh` passing. The `golang:<ver>-bookworm`
    builder tag has to exist for the chosen version.
- **Major bumps and toolchain (Go) changes are human-only.** The report marks
  them and policy treats them as such — no tier of this tooling proposes them
  and nothing auto-merges them.
- **Standing notes** (monitoring-only items, e.g. the Trivy installer fetched
  from `main`) are printed in the report body on every run, but they do **not**
  open an issue or keep one open by themselves — only findings do
  (`findings_count > 0` is the gate for create/edit).
- **How to close or silence:** fix a stale pin, and it stops being a finding;
  the next clean run comments on the labelled issue (naming that run) and closes
  it automatically, re-opening it on the next run with findings. You can also
  close the issue by hand — it will reappear if there are still findings at the
  next run. A standing note cannot be "closed": it is informational and stays
  in the body until the underlying situation changes (for the Trivy
  `main`-branch installer, that means the separate decision to pin the
  installer — see Known gaps).

## Running it locally

The script is stdlib-only Python, needs no daemon, and prints one summary line
to stdout. Exit status is `0` whether or not findings exist (the caller decides
what to do with the report) and `2` on bad arguments or an internal error:

```bash
# Live run (reads the pinned versions from the tree, queries the upstream feeds):
python3 scripts/upstream_watch.py --repo-root . --report-md /tmp/upstream-watch.md

# Offline run against fixtures — the fixtures file is a JSON object mapping
# URL -> response body text; a URL missing from it counts as a feed failure
# and is recorded under "Not checked" rather than aborting the run:
python3 scripts/upstream_watch.py --repo-root . --report-md /tmp/upstream.md \
  --report-json /tmp/upstream.json --offline-fixtures fixtures.json
```

The unit tests are offline-only (they substitute the feed fetcher with a fake)
and stdlib `unittest`:

```bash
python3 -I scripts/tests/test_upstream_watch.py
# or
python3 -I -m unittest discover -s scripts/tests -p "test_upstream_watch.py"
```

## Deferred decisions

The operator answered the proposal's first question on 2026-11-01 — option C,
maintainer-only, human merge first for everything — and built it (this PRD).
Open questions 2 to 5 of the proposal are **unanswered** and stay deferred:

1. Question 2 — may a tier 1 security PR auto-merge when all required checks
   are green? (default proposal was: no, human merge — still the case)
2. Question 3 — if auto-merge is yes: may the merge also auto-release?
3. Question 4 — is a non-security update still subject to a cooldown (the
   72-hour rule kept for non-security only, applied by hand as described
   above)?
4. Question 5 — does the Trivy database for base-image packages (Debian) count
   as an advisory trigger, alongside the Go vulnerability database / OSV?

The proposal's recommendation was to start with human merge for every tier and
revisit auto-merge once the PR stream has run for a few weeks. Nothing in this
PRD prepares for auto-merge; that is a later decision.

## Known gaps

- **The workflows' Trivy install steps were moved off the moving `main` branch**
  on 2026-11-09. All eight steps in `.github/workflows/containers.yaml`,
  `.github/workflows/image-refresh.yaml` and `.github/workflows/release.yaml`
  now run `scripts/ci-install-trivy.sh` with the pinned tag. The upstream-watch
  job's standing note about `main` was written before that change. Whether it
  still reports is not verified here. Its finding for `main` drift (sha256) from
  the pinned `v${TRIVY_VERSION}` tag is unchanged, and **changing the watcher
  itself is a separate decision**, not part of option C as built. `scripts/upstream_watch.py` is monitoring-only
  and never edits those workflows.
- **AGENT.md still says "until upstream monitoring exists"** in its dependency
  rule (the operator change of 2026-10-25). The wording is now stale, but
  editing AGENT.md is outside this PRD's scope — flag it separately if the
  wording should be updated.
- **Repository settings are unverified from the repo:** Dependabot alerts,
  Security updates and the dependency graph can only be confirmed in GitHub's
  settings UI (see above).

## Parity surface

All eight product surfaces are **intentionally excluded** from this work, each
for the same root reason: this is maintainer tooling for the datawatch
repository, not a feature of the datawatch daemon, and users of datawatch are
not maintaining datawatch's code.

| Surface | Change a build would make | Decision |
|---|---|---|
| REST | none | intentionally excluded — maintainer tooling only; no daemon involvement at all |
| MCP | none | intentionally excluded — maintainer tooling only; nothing for MCP to expose |
| CLI | none | intentionally excluded — maintainer tooling only; there is no `datawatch` command for this |
| Comm channel | none | intentionally excluded — maintainer tooling only; no alert or message is delivered through the daemon |
| YAML/config | none | intentionally excluded — maintainer tooling only; no daemon configuration option exists or is added |
| PWA | none | intentionally excluded — maintainer tooling only; no status card is shown in the PWA |
| Android | none | intentionally excluded — maintainer tooling only; nothing to mirror on Android |
| iPhone | none | intentionally excluded — maintainer tooling only; nothing to mirror on iPhone |
