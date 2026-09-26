# Container image refresh (daily rebuild pipeline)

Released container images are built once, at release time. Operating-system
and base-image fixes published afterwards would otherwise never reach them. The
`image-refresh` GitHub Actions workflow keeps every published image current.

## What it does

Every day at 05:30 UTC it:

1. **Plans.** Finds the latest published release and scans its published images
   (agent-base, validator, stats-cluster, agent-claude, agent-opencode,
   agent-aider, agent-gemini, agent-goose, parent-full) with the same blocking
   Trivy policy the release uses (`HIGH,CRITICAL`, exceptions in
   `.trivyignore`). It rebuilds when an image fails that scan or is missing,
   every Monday as a weekly baseline, or when a run is forced.
2. **Rebuilds** the base images, then the agent images, with no cache and
   freshly pulled bases. The datawatch source inside agent-base is always the
   release tag's. The build recipes (`docker/dockerfiles`) and `.trivyignore`
   come from `main`, so a recipe or policy fix reaches already-released
   versions without a new release.
3. **Scans before publishing.** The rebuilt image is pushed under a staging tag
   `refresh-YYYYMMDD`, scanned, and only if the scan is clean is it promoted to
   the release tag (for example `:8.34.1`) and `:latest`. A failing scan never
   replaces a published image. The run fails instead, which emails the
   repository owner.

Staging tags older than three days are removed by the weekly `ghcr-cleanup`
workflow (see below).

## Running it by hand

GitHub > Actions > `image-refresh` > Run workflow. Options:

- `force`: rebuild even if the published images scan clean.
- `version`: refresh a specific release, for example `8.34.1`, instead of the
  latest.

CLI: `gh workflow run image-refresh.yaml -f force=true`.

## When a refresh fails

The run log shows the Trivy table for the failing image. Options, in order:

1. The finding has a fixed version: bump the package in the relevant
   `docker/dockerfiles/Dockerfile.*` on `main` (for example an added
   `pip install "package>=X"` pin) and re-run. No release is needed.
2. No fix exists in the base distribution and the package is not reachable in
   the containers: add the CVE to `.trivyignore` with a comment and add a row
   to the exceptions table in `docs/security-review.md`. Re-review each
   release.

## Registry housekeeping

`ghcr-cleanup` runs weekly, once per image package. It always deletes stale
`refresh-*` staging tags. It also lists old patch versions of closed minor
lines, but that part is a dry run unless started manually with
`dry_run=false`.

## See also

- [`../security-review.md`](../security-review.md#container-image-scanning--v800)
- [Container workers](container-workers.md)
