# v6.0 prep — open backlog

**Date:** 2026-04-27
**Status:** pre-v6.0 cut (v5.x patch window per operator directive)

These items roll up across v5.26.x patch releases; they don't gate v6.0 unless flagged. Each release notes file has its own "Known follow-ups" section — this document is the consolidated view.

## Blocking v6.0 cut

_(none — operator-driven cut: v6.0 ships when operator declares ready. Currently all v5.26.x patch work is additive.)_

## Open

### CI: audit + fix gh actions
**Added:** 2026-04-27 (operator directive after v5.26.24)
**Files:** `.github/workflows/{containers,docs-sync,ebpf-gen-drift}.yaml`

Audit every workflow before v6.0 cut. Recent v5.26.x runs all show `success`, but we haven't done a structural review. Things to check:

- **`containers.yaml`** — two-stage build (parent + agents) introduced in fc4c554. Does it still publish all expected images? Are any tags inconsistent? Add `agent-goose` and `parent-full` per existing v6.0 follow-up.
- **`docs-sync.yaml`** — false-positive on fresh checkout fixed in fc4c554. Re-verify on a clean clone simulation.
- **`ebpf-gen-drift.yaml`** — drift check still running on every push? Are skipped diffs actually meaningful?
- **Cross-cutting** — pinned action SHAs (no floating `@main` references), secrets scoped minimally, concurrency guards on long-running jobs, cancel-in-progress on PR runs.
- **Pre-release security scan automation** (existing follow-up) — wire `gosec` + `govulncheck` into a workflow gated on tag pushes. Currently manual.
- **Kind-cluster smoke** (existing follow-up) — add a workflow that spins up `kind`, deploys the chart, runs `release-smoke.sh` against it. Catches chart regressions before tag.

### Per-session workspace reaper
**Files:** `internal/server/api.go` (handleStartSession clone path), session lifecycle hooks

Cloned workspaces in `<data_dir>/workspaces/<sess>/` persist after the session ends. Add a cleanup hook on session-delete + a periodic reaper for orphans.

### datawatch-app PWA mirror — issue #10
**Repo:** github.com/dmz006/datawatch-app

PWA changes from v5.26.6 → v5.26.24 need mirroring to the mobile companion. Tracked in datawatch-app#10.

### v6.0 cumulative release notes
**Files:** `docs/plans/RELEASE-NOTES-v6.0.0.md`

Operator-prepared at cut time. Will span v5.0.0 → v6.0.0 (patch + minor accumulation).

### CI: parent-full + agent-goose containers
**Files:** `.github/workflows/containers.yaml` + `containers/{parent-full,agent-goose}/Dockerfile`

Dockerfiles exist; CI workflow doesn't publish them yet. Roll into the gh-actions audit above.

### GHCR past-minor cleanup run
**Files:** `scripts/delete-past-minor-containers.sh` (created v5.26.5)

Needs a PAT with `read:packages` + `delete:packages`. Once available, run the script to prune images from past minor versions.

## Closed in v5.26.x

- v5.26.6: GHCR tag-prefix mismatch fix
- v5.26.9: autonomous decompose loopback (broken since v3.10.0)
- v5.26.11: PRD effort enum mismatch
- v5.26.13/16/18: orphan autonomous session cleanup (REST cascade + executor cancel + smoke baseline-diff)
- v5.26.14: scroll-mode preservation
- v5.26.15+23: response capture filter (prose-in-borders preserved)
- v5.26.17: loopback URL validation (no more hardcoded 127.0.0.1)
- v5.26.19/20/21: F10 project_profile + cluster_profile on autonomous PRDs
- v5.26.22: git credentials abstracted for k8s
- v5.26.24: BL113 token broker for daemon-side clone
