# v6.0 prep — open backlog

**Date:** 2026-04-27 (last refactored: 2026-04-27, end of v5.26.53 cycle)
**Status:** pre-v6.0 cut (v5.x patch window per operator directive). **Ready for final tests** as of v5.26.53 — every backlog item except v6.0 cumulative release notes is either closed, has a design doc, or has a clear deferred-with-blocker reason.

These items roll up across v5.26.x patch releases; they don't gate v6.0 unless flagged. Each release notes file has its own "Known follow-ups" section — this document is the consolidated view.

## Blocking v6.0 cut

_(none — operator-driven cut: v6.0 ships when operator declares ready. Currently all v5.26.x patch work is additive.)_

## Open

### PRD-flow rework — remaining phases (3, 4, 6)
**Started:** 2026-04-27 (operator multi-part request)
**Files:** `internal/autonomous/`, `internal/server/`, `internal/server/web/app.js`, `docs/howto/`, `docs/flow/`

Phases 1, 2, 5 shipped + designs landed for 3 + 4:

- ✅ Phase 1 — unified Profile dropdown (v5.26.30 + cluster default fix v5.26.34)
- ✅ Phase 2 — story-level review + edit (v5.26.32)
- 🟡 Phase 3 — design landed v5.26.53 at [`docs/plans/2026-04-27-prd-phase3-per-story-execution.md`](2026-04-27-prd-phase3-per-story-execution.md). Implementation pending operator review.
- 🟡 Phase 4 — design landed v5.26.53 at [`docs/plans/2026-04-27-prd-phase4-file-association.md`](2026-04-27-prd-phase4-file-association.md). Implementation pending operator review.
- ✅ Phase 5 — persistent smoke fixtures `smoke-testing` + `datawatch-smoke` (v5.26.33)
- 🟡 Phase 6 — howto text refreshed (v5.26.39); diagrams viewer fixes (v5.26.50/51); screenshots recapture still pending. Needs browser-automation tooling for the screenshot pass.

### Mempalace alignment audit + spatial memory expansion plan
**Status:** 🟡 Audit-frame doc landed v5.26.53 at [`docs/plans/2026-04-27-mempalace-alignment-audit.md`](2026-04-27-mempalace-alignment-audit.md). The doc establishes current state matrix + three-step procedure (pull upstream / enumerate / fill gap) + provisional quick-win shortlist. The audit itself runs against current upstream and produces a follow-up doc with the actual gap table. Implementation BLs follow from there.

**Operator clarification carried over:** when asking about spatial memory layers, the comparison was against **mempalace**, not just internal plumbing. Current state per `docs/plan-attribution.md`:

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
**Status:** 🟡 partial — five new sections shipped this cycle, three still need agent fixtures.

Smoke is **51 pass / 0 fail / 1 skip** as of v5.26.53 (was 33/0/2 at the start of the audit). Closed:

- ✅ §7d Persistent test profiles (v5.26.33)
- ✅ §7e Filter store CRUD (v5.26.41)
- ✅ §7f Memory + KG round-trip (v5.26.47, fixed v5.26.51)
- ✅ §7g MCP tool surface (v5.26.48)
- ✅ §7h Schedule store CRUD (v5.26.52)
- ✅ §7i Channel send round-trip via `/api/test/message` (v5.26.52)

Still open — all blocked on `agents.enabled=true` fixture availability:

- **F10 ephemeral agent lifecycle.** Mint/revoke broker token, cluster spawn, sibling visibility, parent-namespace import. The kind-smoke workflow (v5.26.43) deploys the chart but doesn't yet enable agents in the test config. Track as a follow-up: enable agents + Docker socket in kind-smoke values + add §7j.
- **Wake-up stack L0–L5 probes.** Builds on F10 fixture — needs a spawned agent to read its wake-up bundle from. The composers don't have direct REST endpoints; they fire at agent-bootstrap time.
- **Stdio-mode MCP tools.** `memory_recall` / `kg_query` / `research_sessions` / `copy_response` / `get_prompt` (mentioned in CLAUDE.md but not in `/api/mcp/docs`) live behind the stdio MCP server-mode. Smoke would need an MCP client wrapper.

Howto coverage parity rule (still active): every smoke-required subsystem should have a `docs/howto/<subsystem>.md` for operators.

### CI follow-ups (residual after v5.26.25 + v5.26.29 + v5.26.38 + v5.26.40 + v5.26.42 + v5.26.43)
**Files:** `.github/workflows/`

All four CI residuals from the v5.26.25 audit are now closed:

- ✅ agent-goose Dockerfile + CI publish (v5.26.42)
- ✅ Pinned action SHAs (v5.26.38)
- ✅ Kind-cluster smoke workflow (v5.26.43)
- ✅ gosec baseline-diff blocking gate (v5.26.40)

CI residual list is empty. Future CI items would be net-new asks, not v5.26.25 carry-overs.

### datawatch-app PWA mirror — issue #10 + child issues #11–#17
**Repo:** github.com/dmz006/datawatch-app

PWA changes from v5.26.6 → v5.26.53 need mirroring to the mobile companion. Tracked in [datawatch-app#10](https://github.com/dmz006/datawatch-app/issues/10) (umbrella) plus 7 scoped child issues filed v5.26.53:

- [#11](https://github.com/dmz006/datawatch-app/issues/11) — Unified Profile dropdown
- [#12](https://github.com/dmz006/datawatch-app/issues/12) — Story-level review + edit
- [#13](https://github.com/dmz006/datawatch-app/issues/13) — PRD panel FAB + filter toggle
- [#14](https://github.com/dmz006/datawatch-app/issues/14) — Directory picker + mkdir-while-browsing
- [#15](https://github.com/dmz006/datawatch-app/issues/15) — Response capture filter
- [#16](https://github.com/dmz006/datawatch-app/issues/16) — Input Required banner refresh
- [#17](https://github.com/dmz006/datawatch-app/issues/17) — `/diagrams.html` viewer fixes

Each is independently shippable on the mobile side; #10 is the umbrella tracker.

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
