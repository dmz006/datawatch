# [draft] Parity — Trivy install steps that retry and fail loudly: what the work closes and what it leaves

Draft 2026-11-09 for a datawatch repository CI issue, following the work
"Make the Trivy install step in the workflows retry and fail loudly instead of
silently" (found 2026-11-09 in a release run whose "Install Trivy" step
finished green while leaving the worker with no Trivy binary at all). That
work shipped the hardened installer `scripts/ci-install-trivy.sh`, its bash
test `scripts/tests/ci-install-trivy_test.sh`, the docs set in
`docs/howto/container-image-refresh.md` and `docs/operations.md`, and the
migration of all eight "Install Trivy" steps in
`.github/workflows/release.yaml` (two), `.github/workflows/containers.yaml`
(two) and `.github/workflows/image-refresh.yaml` (four). This draft records
the parity gap the work closes and the parity gaps it deliberately leaves
open. It is a file artifact only: nothing here runs a hosting CLI, creates an
issue on any tracker, or writes to any host.

## Affected area

The Trivy install steps in the three container-scan workflows —
`release.yaml`, `containers.yaml` and `image-refresh.yaml` — each of which
gates a Trivy scan of a released or rebuilt container image, plus the three
operator-facing documents that describe those steps, the one adjacent
workflow that shares the same install pattern (`kind-smoke.yaml`), and the
Trivy version pins themselves, which the three workflows state independently.

## Current behavior

The closed gap is the failure mode found in the 2026-11-09 release run. Every
"Install Trivy" step ran the inline form
`curl -sfL https://raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh | sh -s -- -b /usr/local/bin <version>`,
with three properties that made it silent: the install script was fetched
from the moving `main` branch rather than the pinned release tag; the
download was piped straight into a shell, so a truncated or failed transfer
could finish the step without surfacing as an error; and a failed download
got exactly one attempt, with no retry. The observable result was an install
step reporting success with no Trivy on the worker, a blocking scan dying two
steps later with `trivy: command not found`, or — the worst case — a
non-blocking scan step with a trailing `|| true` silently skipping the scan
entirely, so a release could publish with no image scan performed and no red
anywhere in the run. The work closes this on the install side: all eight
steps — `release.yaml` lines 278–279 and 370–371,
`containers.yaml` lines 91–92 and 166–167, and `image-refresh.yaml`
lines 84–85, 160–161, 229–230 and 612–613 — now invoke
`bash scripts/ci-install-trivy.sh <tag> /usr/local/bin`
(release and image-refresh with `v0.73.0`, containers with `v0.70.0`,
image-refresh via its `TRIVY_VERSION` variable at the top of the file), and
the installer guarantees, in order, that a failed download exits non-zero with curl's
status and the failing URL on stderr and the install directory left empty;
that the download is retried five times ten seconds apart on any error, with
a twenty-second connect timeout and a one-hundred-twenty-second total cap;
that the install script is fetched for the pinned version tag itself, never
from `main`; and that `sh` runs over a downloaded temp file instead of a
pipe, so a truncated transfer can never execute as partial code. A missing
binary therefore can no longer reach a scan step: the step fails loud
before the scan starts, which also defuses the silent `|| true` skip for the
table-format scan steps in `release.yaml` and the fresh-scan steps in
`image-refresh.yaml`.

The work leaves four gaps open, each visible in the current tree. First, the
documentation no longer matches the workflows it describes: the "Still
pending review" paragraphs in `docs/howto/container-image-refresh.md`
(§ "How CI installs Trivy") and `docs/operations.md`, and the matching
"still pending review as of this note" sentence in
`docs/plans/2026-11-01-upstream-monitoring-maintainer-guide.md`, all state
that the eight steps still use the inline `curl … | sh` form fetching the
installer from `main` — a description of the pre-migration state, now stale
in the same tree that carries the migration. The earlier draft
`2026-11-09-trivy-install-workflow-migration.md` makes the same claim ("none
of the eight production install steps invokes it") and is superseded by the
merged migration. A maintainer following the docs would "migrate" steps that
are already migrated. Second, `.github/workflows/kind-smoke.yaml` still
installs helm with
`curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash`
— unpinned to the moving `main` branch, no retry, and a pipe-to-shell whose
failure visibility is left to luck: the same silent-failure family this work
closes for Trivy, one workflow out of scope of it. Third, the three workflows
still pin three different Trivy versions — `containers.yaml` at `v0.70.0`
and `release.yaml` plus `image-refresh.yaml` at `v0.73.0` — a disagreement
the inline form hid and the script now states explicitly in each step, so a
pin change that looks like it updates the scan tooling would silently leave
one workflow behind. Fourth, the closed-gap guarantee is not yet
demonstrated in production: the behavior is covered by the fake-`curl` test
harness, but no real release or image-refresh run has exercised the loud
failure path, so the guarantee holds by construction, not by observation.

## Expected behavior

Every "Install Trivy" step in the repository installs Trivy through
`scripts/ci-install-trivy.sh`, so a transient network blip is absorbed by the
retry, a genuine outage fails the step with the failing URL and curl's exit
status in the log and an empty install directory, the script that executes
belongs to the exact version tag being installed, and the shell never
executes a download it did not fully save first. The three operator-facing
documents describe exactly that state — no "pending review" wording left in
them — and state one Trivy pin for the whole repository, or explicitly say
why the workflows differ. No workflow in the repository installs a tool by
piping an unpinned `main`-branch download straight into a shell. And at
least one real run has shown the loud failure path working, so the guarantee
is observed, not just constructed.

## Suggested next step

Open (or re-open, superseding
`2026-11-09-trivy-install-workflow-migration.md`) a CI issue in the datawatch
repository with two parts. The first part closes the docs-parity gap the
migration itself opened: replace the three "Still pending review" passages in
`docs/howto/container-image-refresh.md`, `docs/operations.md` and
`docs/plans/2026-11-01-upstream-monitoring-maintainer-guide.md` with a
statement of the migrated state — all eight steps invoke
`scripts/ci-install-trivy.sh`, the installer is no longer fetched from
`main` by any workflow — and mark the earlier draft file superseded so the
repository's written record agrees with its tree. The second part closes the
two open behavior gaps: decide on a single Trivy pin for all three workflows
(keeping the current tags if they are to stay split, but saying so in one
place) and migrate the `kind-smoke.yaml` helm install out of the unpinned
pipe-to-shell form — either to the same download-to-file-then-run discipline
`ci-install-trivy.sh` uses, or to a pinned release download, whichever fits
helm's distribution — as a small follow-up, since it shares the pattern but
not the script. Finish by verifying the guarantee in production: force one
image-refresh run and one release run and confirm the scan steps see a
present binary, and exercise the failure path once deliberately (a run where
the download cannot succeed) to confirm the step goes red with the expected
error text and empty install directory rather than a quiet missing scanner
two steps later.

Verified against the tree the draft describes: the eight steps above all
invoke `scripts/ci-install-trivy.sh` exactly as stated (no workflow still
fetches the Trivy installer off `main`); `scripts/ci-install-trivy.sh` and
`scripts/tests/ci-install-trivy_test.sh` are present on `main`-equivalent;
the three "Still pending review" passages and the superseded draft
`2026-11-09-trivy-install-workflow-migration.md` still read as of the draft
date; and `kind-smoke.yaml` is the only remaining unpinned `main`-branch
pipe-to-shell tool install in the workflow set.

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
