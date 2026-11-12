# [draft] Migrate every workflow "Install Trivy" step to scripts/ci-install-trivy.sh

> **Status update (2026-11-12): resolved in the repository.** All eight Trivy steps now call
> `scripts/ci-install-trivy.sh` with the tag read from `scripts/trivy-version.txt`, and the kind
> smoke workflow installs helm through `scripts/ci-install-helm.sh` with the tag in
> `scripts/helm-version.txt`. The text below is the original 2026-11-09 draft and describes the
> state before that change. See `docs/howto/container-image-refresh.md`.


Draft 2026-11-09 for a datawatch repository CI issue. This work (the hardened
Trivy installer `scripts/ci-install-trivy.sh`, its bash test
`scripts/tests/ci-install-trivy_test.sh`, and the docs set in
`docs/howto/container-image-refresh.md`, `docs/operations.md`, and
`docs/plans/2026-11-01-upstream-monitoring-maintainer-guide.md`) ships the
repaired install step without shipping the workflows that use it, so this
draft records the parity gap it deliberately leaves open. It is a file
artifact only: nothing here creates an issue on any tracker or host.

## Affected area

The eight inline "Install Trivy" steps across the three container-scan
workflows: two in `.github/workflows/release.yaml`, two in
`.github/workflows/containers.yaml`, and four in
`.github/workflows/image-refresh.yaml` (one each in its plan, base,
recheck-ignored-cves, and agents jobs). Each of them gates a Trivy scan of a released or rebuilt
container image, so the install step is the load-bearing first step of the
whole image-scan pipeline. The gap was found in production on 2026-11-09: a
release run's install step completed green while leaving the worker with no
Trivy binary at all. A closely related but out-of-scope instance of the same
pattern also exists in `.github/workflows/kind-smoke.yaml`, which installs
helm by piping `curl …/helm/main/scripts/get-helm-3 | bash` — unpinned to a
release, no retry, failure visibility left to luck.

## Current behavior

Every one of the eight steps runs the same inline form:
`curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b /usr/local/bin <version>`
(release names it `v0.73.0`, containers `v0.70.0`, image-refresh the
workflow's `$TRIVY_VERSION`). Three properties of that form cause the
failure mode: the script is fetched from the moving `main` branch rather
than the pinned release tag, so what executes is whatever upstream decided
to ship, and a fetch that races an upstream rewrite can end with a different
or partial script; the download is piped straight into a shell, so a
truncated or failed transfer can finish the step without surfacing as an
error, leaving an install directory that is empty or half-populated; and a
download that fails mid-stream gets exactly one attempt, with no retry. The
observable result is that the install step reports success and the next step
dies with `trivy: command not found`, or — worse, and the reason this was
only found by reading a run log — a non-blocking scan step with a trailing
`|| true` silently skips the scan entirely, so a release can publish with no
image scan performed and no red anywhere in the run. The pipeline is only as
loud as its quietest step, and today its first is one of them.

Meanwhile this work adds the repaired shape as a first-class script:
`scripts/ci-install-trivy.sh` fails loudly (`set -euo pipefail`, curl's exit
status and the failing URL on stderr, install directory left empty), retries
before giving up (five attempts ten seconds apart on any error, with a
connect timeout and a total cap), fetches the install script for the pinned
version tag itself rather than `main`, and downloads to a repository-tree
temp file before executing, so `sh` never reads a pipe as its script source.
Its behavior is pinned by the test harness that fakes `curl` and asserts the
attempt count, the exit code, the error text, and the state left behind.
The docs set documents the new guarantee and explicitly flags the old steps
as "still pending review." The parity gap is therefore exactly this: the
strong form exists, is tested, and is documented, but none of the eight
production install steps invokes it, and the three workflow files still
carry two different pinned versions plus a workflow variable, so the
guarantee does not hold anywhere it is paid for.

## Expected behavior

Each workflow's "Install Trivy" step invokes the hardened installer instead
of the inline pipe —
`bash scripts/ci-install-trivy.sh "<trivy-version-tag>" /usr/local/bin` —
so all eight steps inherit the same four guarantees: no transient network
blip becomes a silent missing binary; a genuine outage fails the step with
the URL and curl's exit status visible in the log; the script that executes
belongs to the exact version tag being installed; and the shell never
executes a download it did not fully verify. After the migration there is
one way to install Trivy in CI, the inline `curl | sh` form is gone from the
repository, and the pinned tag is stated once per workflow in the form the
script expects.

## Suggested next step

Open a CI issue in the datawatch repository to migrate the eight inline
steps to `scripts/ci-install-trivy.sh`, keeping each workflow's existing
pinned version (release `v0.73.0`, containers `v0.70.0`, image-refresh
`$TRIVY_VERSION`) as the script's first argument and `/usr/local/bin` as the
install directory, and verify with one forced image-refresh run and one
release run that a deliberately flaky install now fails the step with the
expected error text rather than producing `trivy: command not found` two
steps later. While touching those steps, decide whether the three workflows
should converge on a single Trivy tag — they currently disagree, and the
script makes the disagreement explicit rather than hidden — and file the
kind-smoke helm install as a separate parity issue since it shares the
pattern but not the script. Finally, when the migration lands, drop the
"Still pending review" paragraph from
`docs/howto/container-image-refresh.md` so the howto stops describing a gap
that no longer exists.

## Parity surface

This is CI and shell only. No daemon, API, config or UI code changes, so every
product surface is excluded.

| Surface | Decision | Why |
|---------|----------|-----|
| REST | excluded | No endpoint is added or changed; the installer runs in GitHub Actions. |
| MCP | excluded | No MCP tool is added or changed. |
| CLI | excluded | `datawatch` gains no command; `scripts/ci-install-trivy.sh` is a repository script, not a CLI subcommand. |
| comm channel | excluded | No channel command or message changes. |
| YAML/config | excluded | No daemon config key is added. The Trivy pins stay in the workflow files. |
| Web UI (PWA) | excluded | No UI change. |
| Android app | excluded | No app change. |
| iOS app | excluded | No app change. |
