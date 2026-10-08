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

## Stale-suppression auto-removal (`recheck-ignored-cves`)

A second daily job rebuilds every real shipped image fresh and scans it
**without** `.trivyignore` applied, to find `security/accepted-risks.yml`
entries that are safe to delete because a fix is now actually installed. If
it finds any, it opens a PR against the registry (and regenerates
`.trivyignore`/`docs/security-review.md`); it always posts/updates a daily
watch-report tracking issue either way.

**How staleness is decided (since v8.73.36):** a suppressed entry is only
called stale when the package it names is no longer installed at the exact
suppressed `version` in any image it applies to. This is checked against
Trivy's full package inventory (`--list-all-pkgs`), not against whether the
suppressed CVE ID shows up in the fresh scan — see
`scripts/compute_stale_risks.py` for the implementation.

That distinction matters because it wasn't always true. Before v8.73.36 the
job used ID presence alone: "the suppressed CVE ID isn't in today's scan" ⇒
"a fix must be available now." That's false whenever Trivy's vulnerability
database — which rebuilds itself several times a day — relabels a CVE for
a package whose installed version never moved. Confirmed in production on
2026-10-08: `zlib1g` at `1:1.2.13.dfsg-1` scanned as `CVE-2023-45853` in one
run and as `CVE-2026-27171`/`CVE-2026-85091` a few hours later, `Status:
affected` both times, no `FixedVersion` — same unfixed package, two
different ID-based "disappearances." The old logic auto-opened two PRs
(#199, #200) deleting still-valid suppressions on that basis, including an
entry (`CVE-2026-19445`) that had been traced and confirmed as a real,
currently-applicable finding on the same unchanged package mere hours
before each PR tried to remove it. Both were closed without merging.

**What to do if you see an "ID churn" note in the watch report:** nothing,
by design — the entry stayed suppressed on purpose. It means the package +
version still match the registry exactly, but the registered CVE ID didn't
show up in that day's scan; the database relabeled it, not a real fix. The
practical consequence: if a *different*, unsuppressed CVE ID for the same
package later blocks a `refresh / <image>` run, check whether it's just
this same long-suppressed issue wearing a new name before treating it as a
brand-new finding — update the registry entry's `id` field rather than
re-doing the whole impact analysis from scratch.

**Reviewing an auto-opened removal PR:** the bar for "stale" is now the
installed version, so a correctly-opened PR means a real upstream fix
landed — safe to merge after a skim. Still worth a glance at which images
and packages are listed; a version-anchored false positive would require
Trivy's own package inventory to be wrong, not just its vulnerability
labeling, which is a much rarer failure mode than ID churn.

## Registry housekeeping

`ghcr-cleanup` runs weekly, once per image package. It always deletes stale
`refresh-*` staging tags. It also lists old patch versions of closed minor
lines, but that part is a dry run unless started manually with
`dry_run=false`.

## See also

- [`../security-review.md`](../security-review.md#container-image-scanning--v800)
- [Container workers](container-workers.md)
