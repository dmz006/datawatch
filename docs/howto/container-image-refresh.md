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

## How CI installs Trivy

Found in a 2026-11-09 release run: the inline
`curl -sfL …/trivy/main/contrib/install.sh | sh` form of an "Install Trivy"
step can **fail silently**. A flaky or partial download finishes the step
without an error, and the worker ends up with no Trivy. A blocking scan step
then dies with `trivy: command not found`, and a non-blocking one (a trailing
`|| true`) skips the scan without saying so.

`scripts/ci-install-trivy.sh` is the hardened installer; an install step
invokes it as:

```bash
bash scripts/ci-install-trivy.sh "<trivy-version-tag>" <install-dir>
```

It guarantees, in order:

- **Fails loudly.** `set -euo pipefail` throughout. A download that fails
  exits non-zero with curl's status and the failing URL named on stderr
  (for example `error: failed to download Trivy install script (curl exit
  status 22): https://raw.githubusercontent.com/aquasecurity/trivy/v0.73.0/contrib/install.sh`) and the install
  directory is left empty. Step fails, run red, nothing hides.
- **Retries before giving up.** The download gets 5 attempts, 10 s apart, on
  any error (`--retry-all-errors`), with a 20 s connect timeout and a 120 s
  total cap. A transient blip is absorbed; an outage is not.
- **Pinned, not moving.** The install script is fetched for the **pinned
  version tag** (`aquasecurity/trivy/<tag>/contrib/install.sh`), not from
  the moving `main` branch — the script that runs is the one that belongs to
  the version being installed.
- **No pipe-to-shell.** The script downloads first into a temp file
  (created with `mktemp`), then feeds that file to
  `sh -s -- -b <install-dir> <tag>` — `sh` never reads a pipe, so a
  truncated download can never run as partial code, and the temp file is
  removed on exit.

Run its coverage with
`bash scripts/tests/ci-install-trivy_test.sh` — the test fakes `curl` on the
PATH and exercises the success, the persistent-failure, and the
missing-argument paths, asserting the error text, the exit code and the state
left behind.

**One pin, one place.** The Trivy version is pinned in a single file,
`scripts/trivy-version.txt`, which holds one `vX.Y.Z` tag (currently
`v0.73.0`). Every workflow that installs Trivy reads that file and passes the
tag to the installer:

```bash
bash scripts/ci-install-trivy.sh "$(cat scripts/trivy-version.txt)" /usr/local/bin
```

No workflow carries a `TRIVY_VERSION:` value or a literal tag of its own, so
`release.yaml`, `containers.yaml` and `image-refresh.yaml` can no longer drift
apart (they used to pin `v0.73.0`, `v0.70.0` and `v0.73.0`). The kind smoke
workflow installs helm the same way: pinned, with a retrying download (see
[Helm in the kind smoke workflow](#helm-in-the-kind-smoke-workflow)).

**To bump Trivy,** edit `scripts/trivy-version.txt` only. A scan that begins
failing or passing after the bump is the new Trivy's doing, not drift.

**The guard.** `bash scripts/check-trivy-version-single.sh` fails when the file
is missing or does not hold exactly one `vX.Y.Z` line, when a workflow carries
a `TRIVY_VERSION:` value or passes a literal tag to `ci-install-trivy.sh`, or
when a workflow calls the installer without reading the version file. Its
coverage is `bash scripts/tests/check-trivy-version-single_test.sh`, which runs
it against fixture trees.

**Operator review.**

- `containers.yaml` moves from `v0.70.0` to `v0.73.0`. Its scans now run with
  the newer Trivy and its database rules; confirm the next container build is
  not newly red.
- Confirm the guard is wired into `make lint` and `ci.yaml` in the merged
  change; check by running `make lint` and reading `ci.yaml`.
- The helm pin in the kind smoke workflow is its own version, in
  `scripts/helm-version.txt`; the guard above covers only the Trivy pin.
  See the next section.

## Helm in the kind smoke workflow

The "Install kind + kubectl + helm" step of `.github/workflows/kind-smoke.yaml`
used to pipe the moving `get-helm-3` script into `bash`. It now follows the
Trivy pattern:

```bash
sudo bash scripts/ci-install-helm.sh "$(cat scripts/helm-version.txt)" /usr/local/bin
```

- **One pin.** The Helm version is the single `vX.Y.Z` tag in
  `scripts/helm-version.txt` (currently `v3.17.3`). To bump Helm, edit that
  file only; the workflow holds no literal tag.
- **Pinned download.** `scripts/ci-install-helm.sh <tag> <install-dir>` fetches
  `https://get.helm.sh/helm-<tag>-linux-amd64.tar.gz` for that exact tag.
- **Retries.** 5 attempts, 10 s apart, on any error, with a 20 s connect and a
  120 s total timeout.
- **No pipe-to-shell.** The archive goes to a temp file first, then only
  `linux-amd64/helm` is unpacked and installed into the install directory
  (mode 0755). Temp files are removed on exit.
- **Fails loudly.** A persistent download failure exits non-zero with curl's
  status and the URL on stderr (`error: failed to download helm archive (curl
  exit status N): <url>`), so the step goes red. Missing arguments exit 2 with
  a usage line.

Coverage: `bash scripts/tests/ci-install-helm_test.sh` (fake curl, no network).

**Operator review.**

- The helm script does not verify a checksum of the archive, and no guard
  script checks the helm pin the way `check-trivy-version-single.sh` checks the
  Trivy one. Decide whether either is wanted.
- The kind smoke workflow has not been run since this change; confirm the next
  run's install step is green.
- The kind and kubectl downloads in the same step are unchanged (kind is
  pinned to `v0.24.0`; kubectl follows `stable.txt`).

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

## Throwaway recheck image cleanup

Because the recheck job scans fresh rebuilds before anything else, it
pushes, for each shipped image on each run, a throwaway image tagged
`cve-recheck-RUN_ID` (where `RUN_ID` is the GitHub Actions run id, `github.run_id`).
The cleanup step deletes those throwaway tags again once the run is over.

To do that it lists, for each shipped image package, the versions that
carry the recheck tag. The listing is paged: `gh api --paginate` fetches
all pages (one JSON array per page), the pages are folded with `jq -s`
and flattened into a single list, first trying the user endpoint and
falling back to the organization endpoint. It then deletes the
throwaway versions it finds.

A listing or delete failure never fails the run: a warning names the package
that could not be handled, and cleanup moves on to the next package or the
next version. At the end, the step writes a counts-only report to the job
summary: the number of packages checked, listing failures, throwaway
versions found, versions deleted, and delete failures. The report contains
no version IDs and no tag names.

## Registry housekeeping

`ghcr-cleanup` runs weekly, once per image package. It always deletes stale
`refresh-*` staging tags. It also lists old patch versions of closed minor
lines, but that part is a dry run unless started manually with
`dry_run=false`. Its version listing is paged too: it fetches the listing
in pages and flattens them the same way the recheck cleanup does.

## See also

- [`../security-review.md`](../security-review.md#container-image-scanning--v800)
- [Container workers](container-workers.md)
