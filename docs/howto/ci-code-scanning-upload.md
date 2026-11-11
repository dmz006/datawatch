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
