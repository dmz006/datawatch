# Upstream monitoring proposal

**Date:** 2026-10-29
**Version:** none (planning document; plan-document commits are exempt from the version bump)
**Tracker:** registry row 198

Confirmed: no — no option is chosen. This file records the problem and the options; nothing is decided and no implementation PRD is filed until a `Confirmed: yes` line replaces this one.

## Problem

On 2026-10-25 the operator withdrew the 72-hour wait rule for security patches: consume them as soon as they are published, until a better upstream monitor exists. Today that is a manual practice ("the supervisor and the release path consume patches as they find them"). The design is open on four points: sources, cadence, which updates may auto-merge, and how a malicious release is handled now that the waiting period is gone.

## What exists today (verified)

| Piece | State | Proof |
|---|---|---|
| Go vulnerability gate on every push and PR to main | EXISTS, blocking (`govulncheck`), plus an advisory run against the go.mod Go version | `.github/workflows/security-scan.yaml` |
| Go vulnerability gate before release | EXISTS, blocking | `.github/workflows/release.yaml` |
| gosec with baseline diff | EXISTS | `security-scan.yaml` |
| PR gate on new or upgraded dependencies (high severity, license deny list) | EXISTS | `.github/workflows/dependency-review.yaml` |
| Daily rebuild-and-scan of shipped images (05:30 UTC) with Trivy, rebuilds when a published image no longer scans clean | EXISTS | `image-refresh.yaml` (`plan` and refresh jobs) |
| Fresh scan of every shipped image without the suppression file, diffed to find stale suppressions | EXISTS | `image-refresh.yaml` job `recheck-ignored-cves` |
| Auto PR on a stale suppression: branch `auto/cve-cleanup-*`, patch bump, changelog entry, never pushed to main; merge by a human auto-tags a release | EXISTS | `image-refresh.yaml`, `cve-patch-release.yaml` (gated on merged and the branch prefix) |
| Accepted-risk registry with schema check, generated suppression file, daily watch report (expiry, re-trace, stable fixes) and a dismissed-alert check | EXISTS | `security/accepted-risks.yml`, `scripts/check_accepted_risks.py`, `accepted_risks_daily_watch.py`, `dismissed_alerts_watch.py` |
| Weekly CodeQL and secret scan | EXISTS | `codeql.yaml`, `secret-scan.yaml` (cron Mondays) |
| Actions pinned by commit SHA | EXISTS | every `uses:` in the workflows |
| Dependabot or Renovate version-update configuration | MISSING. No `.github/dependabot.yml`, no Renovate config. Whether repository-level Dependabot security updates are switched on in GitHub settings cannot be seen from the repo (unverified) | file search |
| Go module updates opened as PRs on a security advisory | MISSING. `govulncheck` fails the build but nothing opens the upgrade; the manual audit is a pre-release step in `AGENT.md` | `AGENT.md` dependency audit |
| Base image digest pinning and bump automation | MISSING. Dockerfiles use tags (`bitnami/minideb:bookworm`, `golang:<version>-bookworm`, `gcr.io/distroless/cc-debian12:nonroot`) and pinned tool ARGs (Go, gh, RTK versions) that no job bumps | `docker/dockerfiles/Dockerfile.agent-base`, `Dockerfile.stats-cluster` |
| Upstream tool releases (Go toolchain, GitHub CLI, RTK, Trivy installer fetched from its main branch) watched | MISSING. The Trivy install pulls a script from a moving branch in two workflows | `image-refresh.yaml` |
| Any malicious-release control (cooldown, provenance or signature check on dependencies, review of the diff of a bump) | MISSING by decision. The 72-hour cooldown was the only one and is withdrawn | `AGENT.md` dependency rule |
| Auto-merge of any dependency PR | MISSING. The one automated PR kind (cve-cleanup) is merged by a human | `cve-patch-release.yaml` |

## Options

**A. Dependabot only (security updates plus daily version updates for Go modules, Docker and Actions).** Builds: one config file, grouped PRs, existing CI as the gate. Cost: 1 PRD, about 3 tasks (config, doc, a test that the config parses). Risk: Dependabot sees Go modules, Docker base tags and Actions but not the tool ARGs or the Trivy installer; no advisory-triggered rebuild beyond what image-refresh does.

**B. Self-built monitor inside the existing daily job.** Extend `image-refresh.yaml` or add a scheduled workflow that reads the advisory feeds (Go vulnerability database, GitHub advisories, Trivy database), compares to go.mod, Dockerfile ARGs and base tags, and opens `auto/upstream-*` PRs. Cost: 2 PRDs, about 8 to 10 tasks (source adapters, version resolver, PR opener, tests, docs). Risk: the project owns the maintenance; more code in a security path.

**C. Hybrid: Dependabot for modules, Docker and Actions (A), plus a small scheduled job for what Dependabot cannot see (tool ARGs, Trivy installer, advisory-only feeds).** Cost: 1 PRD for A plus 1 PRD of about 4 tasks. Risk: two systems to explain, but each is small.

## Auto-merge and malicious-release handling (apply to any option)

- **Auto-merge tiers.** Tier 1, security fix with all required checks green: PR opened immediately, merge by a human (as today) or auto-merge if the operator opts in. Tier 2, non-security update: weekly grouped PR, human merge. Tier 3, major version or toolchain change: human only.
- **Malicious release without a cooldown.** The wait rule is gone, so the controls must be other things: require green CI and `dependency-review` on the PR, show the module diff and release age in the PR body, verify checksums through the Go sum database (already used by `go mod`), and keep Actions pinned by SHA. A hold of an advisory-less update is still allowed; only security patches skip waiting.
- **Blast radius.** A merged bump becomes a release through the existing pipeline, so auto-merge tier 1 also auto-releases unless the operator says otherwise.

## Recommendation

Option C. Dependabot covers the three ecosystems with the least code and the least ownership, and the small scheduled job covers the pinned tool ARGs and the moving Trivy installer that Dependabot cannot see. Start with human merge for every tier and revisit auto-merge once the PR stream has run for a few weeks.

## Open questions for the operator

1. Which option: A, B or C?
2. May a tier 1 security PR auto-merge when all checks are green, yes or no (default proposal: no, human merge)?
3. If auto-merge is yes: may the merge also auto-release, yes or no?
4. Is a non-security update still subject to a cooldown (the 72-hour rule kept for non-security only), yes or no?
5. Which advisory feeds count as a trigger: Go vulnerability database and GitHub advisories only, or also the Trivy database for base-image packages (Debian)?

## Dependencies and order

Needs: the repository settings (Dependabot alerts and security updates on, which cannot be checked from the repo), the existing `dependency-review` and `security-scan` jobs as the gate, and the release pipeline for the auto-tag step. Order: A first (small, immediate), then the scheduled job for the gaps. Registry row 198 is linked from the 10.0 arc draft as having no PRD today. It also interacts with the accepted-risk registry: a fix that removes a suppressed finding should delete the entry, which `recheck-ignored-cves` already does.

## Parity surface

| Surface | Change a build would make |
|---|---|
| Config | repository files only (workflow, Dependabot config); a daemon option only if monitoring is surfaced in the product |
| REST | none, unless a status endpoint is wanted |
| MCP | none |
| CLI | none |
| Comm channel | optional alert when an upstream security PR opens |
| PWA | optional card listing open upstream PRs and age |
| Mobile | none unless the PWA card is built |
