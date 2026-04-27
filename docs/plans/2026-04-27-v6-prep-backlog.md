# v6.0 prep — open backlog

**Date:** 2026-04-27
**Status:** pre-v6.0 cut (v5.x patch window per operator directive)

These items roll up across v5.26.x patch releases; they don't gate v6.0 unless flagged. Each release notes file has its own "Known follow-ups" section — this document is the consolidated view.

## Blocking v6.0 cut

_(none — operator-driven cut: v6.0 ships when operator declares ready. Currently all v5.26.x patch work is additive.)_

## Open

### Mempalace alignment audit + spatial memory expansion plan
**Added:** 2026-04-27 (operator clarification)
**Files:** `internal/memory/`, `docs/plans/`, target plan doc TBD

Operator clarification: when asking about spatial memory layers, the comparison was against **mempalace**, not just internal plumbing. Current state per `docs/plan-attribution.md`:

- We adopted mempalace's L0–L3 wake-up stack (BL96 added L4/L5 for F10 multi-agent — *our* extension, not mempalace's).
- We adopted mempalace's wing/hall/room schema in pg_store (originally a nightwire concept; `docs/plan-attribution.md` line 87 credits mempalace for the structured layering and BL55 v1.5.0 lands the wing/room/hall columns).
- Three mempalace ports landed: BL97 agent diaries (per-agent wing), BL98 KG contradiction detection (`fact_checker` port), BL99 closets/drawers (verbatim → summary chain).

**What this audit needs to produce:**

1. **Latest mempalace feature inventory.** Pull current main; diff against the BL97/98/99 baseline; enumerate everything we haven't ported.
2. **Full spatial-memory parity assessment.** Mempalace has additional spatial constructs beyond wing/hall/room (corridors, suites, archives, etc., depending on current upstream). Map each to either an existing datawatch concept, a planned BL, or a gap.
3. **Plan doc with prioritised proposals.** For each gap: what it does, what it costs to port, whether it composes with our F10/cluster work or only makes sense single-host. Output: `docs/plans/2026-04-XX-mempalace-alignment-audit.md` matching the shape of `docs/plans/2026-04-25-bl174-go-mcp-channel-and-slim-container.md`.
4. **Quick-win shortlist.** 1–3 features that are <1 day of work each so v6.1 can ship them without blocking v6.0.

Out of scope for the audit itself: implementation. Audit produces the plan doc; subsequent BLs implement.

### Service-function audit + smoke completeness + howto coverage
**Added:** 2026-04-27 (operator directive after v5.26.29)
**Files:** `scripts/release-smoke.sh`, `internal/**`, `docs/howto/`

Operator directive: every service-level function should be exercised by smoke as completely as possible, AND anything complex enough to need smoke coverage should also have a corresponding howto doc. Concrete inventory of what smoke currently misses vs. what the daemon actually exposes:

- **Spatial memory layers (L0–L5)** — smoke calls `/api/memory/search` only. No wake-up stack exercise (`L0`/`L0ForAgent`/`L1`/`L2`/`L3`/`L4`/`L5`/`WakeUpContext`/`WakeUpContextForAgent`), no spatial-dimension filtering (wing/hall/room), no agent diary writes, no KG contradiction detection, no closets/drawers chains. Memory is one of the most-extended subsystems and the least-covered by smoke.
- **F10 ephemeral agents** — smoke covers PRD spawn round-trip but not agent-lifecycle health (mint/revoke broker token, cluster spawn, sibling visibility, parent-namespace import).
- **Channel backends** — smoke checks `/api/channel/history` shape only. No actual signal/telegram/slack send round-trip even when wired.
- **Voice transcription** — smoke checks endpoint reachability, not a real audio→transcript round-trip.
- **Orchestrator** — entire section is `SKIP` when disabled. Need a "minimum config bring-up" smoke that actually enables it for the duration of the test.
- **MCP tools** — no smoke coverage at all. `memory_recall` / `memory_remember` / `kg_query` / `kg_add` / `research_sessions` / `copy_response` / `get_prompt` are operator-facing tools and should each have a smoke probe.
- **Schedule store, alert store, filter store, project/cluster profiles** — partial coverage. Each store has REST CRUD that should round-trip in smoke.

Howto coverage parity rule: if a subsystem is complex enough to need smoke, it deserves a `docs/howto/<subsystem>.md` aimed at operators. Audit existing `docs/howto/` for gaps in the smoke-required list above.

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
