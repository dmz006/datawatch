# CI Code Scanning upload

How the release workflow uploads gosec and Trivy scan reports to GitHub Code Scanning, and why that upload can never stop a release. This is a reference for CI maintainers; it has no MCP-callable steps.

## What the upload is for

The SARIF files from the gosec and Trivy scans in the release workflow are reporting data. They are shown under Code Scanning in the Security tab so people can browse and triage findings. They never decide whether a release ships.

## Why it must not block a release

GitHub once answered the upload with a rate limit for the installation token. That failed the gosec upload step, which cancelled the security summary and skipped goreleaser and the container jobs, so no assets were published. The reporting step therefore retries and warns instead of failing.

## Where the blocking checks live

The checks that do decide a release are separate from the upload:

- The `Run gosec + baseline-diff` step in the `gosec` job of `.github/workflows/release.yaml`.
- The `govulncheck` job in `.github/workflows/release.yaml`.
- The `Scan ... (Trivy SARIF, blocking)` steps in the `build-base` and `build-agents` jobs of `.github/workflows/release.yaml`.
- The blocking `gosec` job in `.github/workflows/security-scan.yaml`.

## Retry schedule

- Four attempts in total.
- Waits of 60, 180 and 300 seconds before attempts 2, 3 and 4.
- A warning on the final failure; the step does not fail the job.
- The SARIF is kept as the `gosec-results` and `trivy-*-sarif` artifacts for 90 days, so a skipped upload can be redone by hand.
- A run whose upload gave up has no new Code Scanning entry for that run. That is not a clean scan; look for the "failed on every attempt" warning on the upload steps, then download the artifact or re-run the workflow later.
- The attempt count and waits are fixed in the workflow file, not in a daemon setting.

## Adding a new SARIF upload

- Every `upload-sarif` step needs `continue-on-error: true` and a chain of at least three attempts.
- No job that publishes assets may depend on a job whose upload can block it.

## Checking it locally

```bash
make check-sarif-upload
bash tests/test-sarif-upload-nonblocking.sh
make lint
make test
```

## For the operator to review

- Decide whether an upload that fails all four attempts should raise an alert; today it is only a warning on the step.
- The guard requires at least three upload steps per job while the workflow uses four; confirm that slack is intended.
- The guard needs Python 3 with PyYAML wherever `make lint` runs; it stops with an error if PyYAML is missing.

## See also

- [operations.md](../operations.md#code-scanning-upload-in-the-release-workflow) for the guard rules.
- [release-push-and-tag.md](release-push-and-tag.md#when-the-code-scanning-upload-fails) for what to do during a release.
- [datawatch-definitions.md](../datawatch-definitions.md) for the "Code scanning upload" definition.
