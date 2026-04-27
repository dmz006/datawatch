# datawatch v5.26.38 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.37 → v5.26.38
**Patch release** (no binaries — operator directive).
**Closed:** Pinned action SHAs across every workflow (CI residual from v5.26.25 audit).

## What's new

### Workflow `uses:` references now pin to commit SHAs

The v5.26.25 gh-actions audit flagged "pinned action SHAs (no floating `@main` references)" as supply-chain hygiene. v5.26.38 closes that residual: every `uses:` reference across `.github/workflows/*.yaml` resolves to a 40-character commit SHA with a trailing `# vN` comment naming the major version each SHA tracks.

| Action | Pinned SHA | Tracks |
|------|------|------|
| `actions/checkout` | `34e114876b0b11c390a56381ad16ebd13914f8d5` | v4 |
| `actions/setup-go` | `40f1582b2485089dde7abd97c1529aa768e1baff` | v5 |
| `docker/setup-qemu-action` | `c7c53464625b32c7a7e944ae62b3e17d2b600130` | v3 |
| `docker/setup-buildx-action` | `8d2750c68a42422c14e847fe6c8ac0403b4cbd6f` | v3 |
| `docker/login-action` | `c94ce9fb468520275223c153574b00df6fe4bcc9` | v3 |
| `docker/build-push-action` | `ca052bb54ab0790a636c9b5f226502c73d547a25` | v5 |
| `softprops/action-gh-release` | `3bb12739c298aeb8a4eeaf626c5b8d85266b0e65` | v2 |

22 `uses:` lines across 4 workflow files (`containers.yaml`, `docs-sync.yaml`, `ebpf-gen-drift.yaml`, `security-scan.yaml`) all updated; zero floating `@vN` references remain.

### Why pin

A floating `@v4` tag is a moving target — the action maintainer can re-point it to any commit at any time, including a compromised one. Pinning to a 40-char SHA freezes exactly one tree of code; the next time the action is fetched it'll match the SHA or fail the run loudly. CI won't silently execute a different binary just because the upstream tag moved.

The cost is upkeep: dependabot or a manual cadence has to bump the SHAs to pick up real fixes. Documented in the `containers.yaml` file header:

```yaml
# v5.26.38 — every `uses:` is pinned to a commit SHA for supply-
# chain hardening. The trailing `# vN` comment names the major
# version each SHA tracks. To bump, query
# `gh api repos/<owner>/<repo>/git/refs/tags/<tag>` and replace
# both the SHA and the version comment.
```

### What didn't change

The SHAs resolved to whatever the `@vN` floating tags pointed at the moment of the v5.26.38 patch — same code as the previous CI run, just frozen in place. No behavior change in any workflow.

## Configuration parity

No new config knob.

## Tests

CI-only change. The next tag push (this one) exercises the pinned references end-to-end.

Smoke unaffected: 37 pass / 0 fail / 1 skip. Go test suite unaffected: 465 passing.

## Known follow-ups

CI residuals remaining (per `docs/plans/2026-04-27-v6-prep-backlog.md`):

- **agent-goose Dockerfile + CI publish.** Dockerfile not yet written — placeholder only.
- **Kind-cluster smoke workflow.** Spin up `kind`, deploy chart, run `release-smoke.sh`.
- **gosec baseline-diff mechanism.** Lets the gosec job become blocking instead of advisory.

## Upgrade path

```bash
git pull
# No daemon restart needed — CI-only change. Push a tag to verify
# the pinned references resolve cleanly on the next build.
```
