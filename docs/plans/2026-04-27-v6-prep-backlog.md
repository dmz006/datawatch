# v6.0 prep — open backlog

**Date:** 2026-04-27 (last refactored: 2026-04-27, end of v5.26.35 cycle)
**Status:** pre-v6.0 cut (v5.x patch window per operator directive)

These items roll up across v5.26.x patch releases; they don't gate v6.0 unless flagged. Each release notes file has its own "Known follow-ups" section — this document is the consolidated view.

## Blocking v6.0 cut

_(none — operator-driven cut: v6.0 ships when operator declares ready. Currently all v5.26.x patch work is additive.)_

## Open

### PRD-flow rework — remaining phases (3, 4, 6)
**Started:** 2026-04-27 (operator multi-part request, partially shipped across v5.26.30/32/33/34)
**Files:** `internal/autonomous/`, `internal/server/`, `internal/server/web/app.js`, `docs/howto/`, `docs/flow/`

Operator directive: substantial PRD-flow rework. Phases 1, 2, 5 shipped:

- ✅ Phase 1 — unified Profile dropdown (v5.26.30 + cluster default fix v5.26.34)
- ✅ Phase 2 — story-level review + edit (v5.26.32)
- ✅ Phase 5 — persistent smoke fixtures `smoke-testing` + `datawatch-smoke` (v5.26.33)

Remaining:

- **Phase 3 — Per-story execution profile + per-story approval gate.** PRD gets `decomposition_profile` (used to GENERATE) + default `execution_profile` (used to run); per-story override allows different profiles for different stories. Per-story approval changes the run model — `Manager.Run` currently executes all stories of an approved PRD; gating each story is non-trivial. Design first, then implement.
- **Phase 4 — File association.** PRD/story/task records track which files in the workspace they reference (so the operator can see "story X touches `internal/foo.go` + `docs/howto/foo.md`"). Design needed: where do file refs come from (LLM-extracted from spec, post-hoc from session diff, both?), where do we store them (PRD store vs. session tracking dir), how do we surface them in the PWA.
- **Phase 6 — Howtos / screenshots / diagrams refresh.** Recapture screenshots that show the New PRD modal, story-edit, profile dropdowns. Update [`docs/howto/profiles.md`](../howto/profiles.md), [`docs/howto/autonomous-planning.md`](../howto/autonomous-planning.md), [`docs/howto/container-workers.md`](../howto/container-workers.md). Refresh data-flow diagrams that show the unified profile path.

### PRD panel UX polish
**Added:** 2026-04-27 (operator request after v5.26.35)
**Files:** `internal/server/web/app.js`

Operator: *"new prd should be a FAB (+) and not the new prd button at top. There should be a filter icon like sessions list to hide/show the filter and sort options, with it hidden by default."*

- Replace top-of-panel "New PRD" button with a Floating Action Button (+) anchored bottom-right, matching the sessions tab affordance.
- Add a filter icon to the PRD panel header that toggles a filter+sort row.
- Filter+sort row hidden by default — operator opens it explicitly when needed.

### Mempalace alignment audit + spatial memory expansion plan
**Added:** 2026-04-27 (operator clarification)
**Files:** `internal/memory/`, `docs/plans/`, target plan doc TBD

Operator clarification: when asking about spatial memory layers, the comparison was against **mempalace**, not just internal plumbing. Current state per `docs/plan-attribution.md`:

- Adopted mempalace's L0–L3 wake-up stack (BL96 added L4/L5 for F10 multi-agent — *our* extension, not mempalace's).
- Adopted mempalace's wing/hall/room schema in pg_store (originally a nightwire concept; mempalace credit at `docs/plan-attribution.md` line 87, BL55 v1.5.0 lands the columns).
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

Operator directive: every service-level function should be exercised by smoke as completely as possible, AND anything complex enough to need smoke coverage should also have a corresponding howto doc. Concrete gap inventory:

- **Spatial memory layers (L0–L5)** — smoke calls `/api/memory/search` only. No wake-up stack exercise (`L0`/`L0ForAgent`/`L1`/`L2`/`L3`/`L4`/`L5`/`WakeUpContext`/`WakeUpContextForAgent`), no spatial-dimension filtering (wing/hall/room), no agent diary writes, no KG contradiction detection, no closets/drawers chains. Memory is one of the most-extended subsystems and the least-covered by smoke.
- **F10 ephemeral agents** — smoke covers PRD spawn round-trip but not agent-lifecycle health (mint/revoke broker token, cluster spawn, sibling visibility, parent-namespace import).
- **Channel backends** — smoke checks `/api/channel/history` shape only. No actual signal/telegram/slack send round-trip even when wired.
- **Voice transcription** — smoke checks endpoint reachability, not a real audio→transcript round-trip.
- **Orchestrator** — entire section is `SKIP` when disabled. Need a "minimum config bring-up" smoke that actually enables it for the duration of the test.
- **MCP tools** — no smoke coverage at all. `memory_recall` / `memory_remember` / `kg_query` / `kg_add` / `research_sessions` / `copy_response` / `get_prompt` are operator-facing tools and should each have a smoke probe.
- **Schedule store, alert store, filter store** — partial coverage. Each store has REST CRUD that should round-trip in smoke. (Project + cluster profile CRUD now covered via persistent fixtures in §7d as of v5.26.33.)

Howto coverage parity rule: if a subsystem is complex enough to need smoke, it deserves a `docs/howto/<subsystem>.md` aimed at operators. Audit existing `docs/howto/` for gaps in the smoke-required list above.

### CI follow-ups (residual after v5.26.25 + v5.26.29)
**Files:** `.github/workflows/`

Most of the gh-actions audit landed in v5.26.25 (eBPF drift loud-fail, parent-full publish, concurrency guard) and v5.26.29 (pre-release security scan automation). Residual:

- **agent-goose Dockerfile + CI publish.** `agent-goose` was placeholder-only — Dockerfile doesn't exist yet, so CI publish can't be wired. Write the Dockerfile against the goose ACP backend, then add to `containers.yaml` stage 2 alongside `agent-claude` / `agent-opencode`.
- **Pinned action SHAs (supply-chain hardening).** Workflows still use floating `@v4` / `@v3` etc. Convert to commit SHAs with comments listing the version. Mechanical.
- **Kind-cluster smoke workflow.** Spin up `kind`, deploy the chart, run `release-smoke.sh` against it. Catches chart regressions before tag. Largest of the three.
- **gosec baseline-diff mechanism.** Would let the gosec job become blocking instead of advisory. Compare current findings against the documented 55-finding baseline in `docs/security-review.md`; fail on net-new findings.

### datawatch-app PWA mirror — issue #10
**Repo:** github.com/dmz006/datawatch-app

PWA changes from v5.26.6 → v5.26.35 need mirroring to the mobile companion. Tracked in datawatch-app#10. The set has grown — every v5.26.x with a `app.js` / `sw.js` change is mirror-relevant.

### v6.0 cumulative release notes
**Files:** `docs/plans/RELEASE-NOTES-v6.0.0.md`

Operator-prepared at cut time. Will span v5.0.0 → v6.0.0 (patch + minor accumulation).

### GHCR past-minor cleanup run
**Files:** `scripts/delete-past-minor-containers.sh` (created v5.26.5)

Needs a PAT with `read:packages` + `delete:packages`. Once available, run the script to prune images from past minor versions.

## Closed in v5.26.x

Newest at top.

- **v5.26.35** — tmux toolbar + screen format survive daemon restart; WS reconnect no longer rebuilds the session-detail subtree when terminal is alive.
- **v5.26.34** — Cluster dropdown leads with "Local service instance"; v5.26.30's required-cluster check on profile selection reverted.
- **v5.26.33** — Persistent smoke fixtures (`smoke-testing` cluster + `datawatch-smoke` project profile) — PRD-flow phase 5.
- **v5.26.32** — Story-level review + edit (PRD-flow phase 2): `Manager.EditStory`, `POST /api/autonomous/prds/{id}/edit_story`, ✎ button + modal in PRD detail.
- **v5.26.31** — Response capture filter (3rd pass): `isLabeledBorder`, `hasEmbeddedStatusTimer`, `isSpinnerCounter`, extended `isPureDigitLine`, broadened noise patterns. README cleanup. New `docs/howto/profiles.md`. Mempalace alignment audit added to backlog.
- **v5.26.30** — Unified "Profile" dropdown in New PRD modal (PRD-flow phase 1).
- **v5.26.29** — Pre-release security scan automation (gosec advisory + govulncheck blocking, in `.github/workflows/security-scan.yaml`).
- **v5.26.28** — Smoke memory check fix (was always silently SKIPping due to wrong endpoint + Python AttributeError).
- **v5.26.27** — Startup orphan workspace reaper for crash-recovery (`Manager.ReapOrphanWorkspaces`).
- **v5.26.26** — Per-session workspace reaper: `Session.EphemeralWorkspace` field; `Manager.Delete` reaps daemon-cloned workspaces under `<data_dir>/workspaces/`.
- **v5.26.25** — gh-actions audit fixes: eBPF drift workflow no longer swallows generator failures; `parent-full` publishes to GHCR; per-tag concurrency guard on `containers.yaml`.
- **v5.26.24** — BL113 token broker integration for daemon-side clone (per-spawn ephemeral tokens replace long-lived `DATAWATCH_GIT_TOKEN`).
- **v5.26.23** — Response capture filter (2nd pass): `hasWord3` prose gate, `isPureBoxDrawing`, `isPureStatusTimer`, multi-spinner detection. Preserved prose framed in TUI borders.
- **v5.26.22** — Git credentials abstracted for k8s deployments (HTTPS+PAT, SSH key, broker patterns documented in `docs/howto/setup-and-install.md`).
- **v5.26.21** — Daemon-side clone for `project_profile` in `handleStartSession` (auto-clone into `<data_dir>/workspaces/`).
- **v5.26.20** — PWA New PRD profile dropdowns + `PUT /api/autonomous/prds/{id}/profiles` for post-create profile changes.
- **v5.26.19** — F10 `project_profile` + `cluster_profile` on autonomous PRDs (`PRD.ProjectProfile` / `PRD.ClusterProfile` fields, profile validation, `Manager.SetPRDProfiles`).
- **v5.26.18** — Smoke baseline-diff orphan-autonomous-session sweep.
- **v5.26.17** — Loopback URL validation (`loopbackBaseURL` + `validateLoopback`); ~14 hardcoded `127.0.0.1` sites centralised.
- **v5.26.16** — Executor goroutine cancellation via `runCancels` map; `decomposition_model` / `verification_model` / `guardrail_model` config keys.
- **v5.26.15** — First response capture filter (later iterated in v5.26.23 + v5.26.31).
- **v5.26.14** — Scroll-mode preservation (3rd iteration after v5.26.9 + v5.26.10).
- **v5.26.13** — REST cascade-delete walks `SessionIDsForPRD`; orphan autonomous session cleanup phase 1.
- **v5.26.11** — PRD effort enum mismatch fix (PRD `low/medium/high/max` → session `quick/normal/thorough`).
- **v5.26.9** — Autonomous decompose loopback fix (broken since v3.10.0 due to "prompt" vs "question" field mismatch); release-smoke.sh first version.
- **v5.26.6** — GHCR tag-prefix mismatch fix (`trimPrefix "v"` in `templates/_helpers.tpl`).
