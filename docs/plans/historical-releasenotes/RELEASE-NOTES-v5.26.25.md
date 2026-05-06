# datawatch v5.26.25 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.24 → v5.26.25
**Patch release** (no binaries — operator directive).
**Closed:** GitHub Actions audit — silent generator failures, missing parent-full publish, retag race window.

## What's new

### CI: gh-actions audit fixes

Operator directive after v5.26.24: audit every workflow before v6.0 cut. Three concrete issues found and fixed:

#### 1. eBPF drift workflow no longer swallows generator failures

`ebpf-gen-drift.yaml` ran:

```yaml
go generate ./... || true
```

The `|| true` was protective (an old kernel-headers package mismatch could fail without meaning drift), but it had a much worse failure mode: a clang error in `netprobe.bpf.c` produces no diff — the artifacts simply aren't regenerated. The next step ran `git diff --quiet` against unchanged files and passed green, so a syntax error in the eBPF source could land on `main` and CI would never notice.

v5.26.25 removes the `|| true`. A generator failure now fails the job loudly, which is what the workflow was meant to do all along. If the runner's kernel-headers package legitimately can't be installed, the fix is to pin the install step, not to mask the symptom.

#### 2. `parent-full` image now publishes to GHCR

`docker/dockerfiles/Dockerfile.parent-full` has existed for a while but was missing from `containers.yaml`'s build matrix, so it never landed on GHCR. v5.26.25 adds it to stage 2 alongside `agent-claude` / `agent-opencode` / `agent-aider` / `agent-gemini` (it's a `FROM agent-base` image so the dependency ordering is the same).

After the next tag push, `ghcr.io/dmz006/datawatch-parent-full:<version>` will be published.

#### 3. Containers workflow now serializes per-tag

Two tag pushes in close succession (rare, but possible: tag-fix + retag, or accidental double-push) would race the same GHCR upload path. v5.26.25 adds a `concurrency:` block keyed on `github.ref` so a second run for the same tag waits for the first to finish:

```yaml
concurrency:
  group: containers-${{ github.ref }}
  cancel-in-progress: false
```

`cancel-in-progress: false` is intentional — release artifact uploads should never be cancelled mid-push (cancelling could leave GHCR in a half-tagged state).

## Configuration parity

No new config knob — pure CI changes.

## Tests

Workflow YAML changes ride on the next tag push. Local Go test suite unaffected.

## Known follow-ups

Rolled into `docs/plans/2026-04-27-v6-prep-backlog.md`:

- agent-goose Dockerfile + CI publish (placeholder only, Dockerfile not yet written).
- Pre-release security scan automation (`gosec` + `govulncheck`).
- Kind-cluster smoke workflow (run `release-smoke.sh` against a `kind`-deployed chart).
- Pinned action SHAs (currently floating `@v4` etc. — supply-chain hardening).
- Per-session workspace reaper.
- datawatch-app PWA mirror — issue #10.

## Upgrade path

```bash
git pull
datawatch restart
# First tag push after v5.26.25 will publish parent-full to GHCR for
# the first time. No operator action needed.
```
