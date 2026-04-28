# datawatch v6.0.0 — release notes (DRAFT)

**Status:** DRAFT — operator finalizes at cut time.
**Date:** TBD (operator decides cut moment).
**Spans:** v5.0.0 → v6.0.0 (cumulative).

This draft consolidates the v5.0.x → v5.26.68 patch + minor accumulation into a single operator-facing release narrative. Every line is grounded in `CHANGELOG.md` entries; operator can prune / reorder / re-tone before publishing.

---

## Headline changes since v5.0.0

### PRD-flow rework — Phases 1 through 6

Operator-driven multi-phase rework of the autonomous PRD flow. Each phase shipped incrementally across v5.26.30–v5.26.67:

- **Phase 1 — unified Profile dropdown (v5.26.30, v5.26.34, v5.26.46).** The New PRD modal now collapses three previously-separate fields (`project_profile`, `cluster_profile`, `project_dir`) into one Profile dropdown. First option `__dir__` (project directory + LLM backend + session profile); subsequent options every configured F10 project profile by name. Cluster dropdown's first option is "Local service instance" (daemon-side clone + local tmux). Same flow now mirrored to the New Session modal (v5.26.63).
- **Phase 2 — story review + edit (v5.26.32).** Story rows in the PRD detail view render `description` inline + an ✎ button while the PRD is in `needs_review` / `revisions_asked`. `POST /api/autonomous/prds/{id}/edit_story`.
- **Phase 3 — per-story execution profile + per-story approval gate (v5.26.60–62).** New `PRD.DecompositionProfile` (re-purposes `ProjectProfile` as default execution); `Story.ExecutionProfile` per-story override; `Story.Approved` + `StoryStatus="awaiting_approval"`. New endpoints `set_story_profile` / `approve_story` / `reject_story`. New config knob `autonomous.per_story_approval` (default off) gates the awaiting-approval transition. PWA story rows show profile + Approve/Reject pills.
- **Phase 4 — file association (v5.26.64–67).** `Story.FilesPlanned`, `Task.FilesPlanned`, `Task.FilesTouched` (50-cap each). LLM populates planned files at decompose time; daemon populates touched files post-spawn from `git diff --name-only`. PWA renders `📝` (planned) + `✅` (touched) pills with conflict (`⚠`) markers when two pending stories plan the same file. Operator-facing edit modals.
- **Phase 5 — persistent test fixtures (v5.26.33).** `smoke-testing` cluster + `datawatch-smoke` project profile created idempotently by smoke; reused across runs.
- **Phase 6 — howtos / screenshots / diagrams (v5.26.39, v5.26.54, v5.26.69).** `docs/howto/autonomous-planning.md` + `docs/howto/profiles.md` refreshed; autonomous screenshots recaptured via the BL190 puppeteer pipeline; new `docs/flow/prd-phase3-phase4-flow.md` mermaid sequence + state-machine diagrams.

### Container Workers (F10) — fully wired

`agents.image_prefix`, `agents.image_tag`, `docker_bin`, `kubectl_bin`, `callback_url`, `bootstrap_token_ttl_seconds`, `worker_bootstrap_deadline_seconds` now have full configuration parity (YAML + REST + MCP + CLI + comm + Web UI section under Settings → General → Container Workers, v5.26.56). BL113 token broker wired into both cluster spawn (existing) and daemon-side `project_profile` clone (v5.26.24). Per-spawn ephemeral tokens replace long-lived `DATAWATCH_GIT_TOKEN` env in production. v5.26.42 published `agent-goose` to GHCR. Per-session workspace reaper (v5.26.26) + startup orphan reaper (v5.26.27) prevent clone-leak across crashes.

### Memory + Knowledge Graph

- L0–L5 wake-up stack (BL96 added L4/L5 for F10 multi-agent, our extension over mempalace's L0–L3).
- BL97 agent diaries, BL98 KG contradiction detection, BL99 closets/drawers verbatim → summary chain.
- v5.26.47 + v5.26.51 added smoke probes for save/list/search round-trip + KG stats.
- v5.26.65 added wake-up stack L0–L3 surface smoke; full L0–L5 covered by 7 unit tests.
- Mempalace alignment audit landed v5.26.69 with module-by-module gap table + 5-item quick-win shortlist.

### Operator UX — cumulative

- **PWA New PRD** — Floating Action Button at bottom-right (v5.26.36/37), filter toggle in header bar matching sessions (v5.26.46), unified Profile dropdown (Phase 1), story-edit + approval widgets (Phases 2/3), file pills + conflict markers + edit modals (Phase 4).
- **Session view** — `⬡ worker` badge for container-worker sessions (v5.26.58), tmux toolbar + screen format survive daemon restart (v5.26.35 + v5.26.45), yellow Input Required banner refresh on bulk WS pushes (v5.26.49) + dismiss-refits-xterm (v5.26.44).
- **Diagrams viewer** — howto README is the default page (v5.26.50), anchor-fragment links work (v5.26.50), `<img>` + heading slug ids + scroll-to-anchor (v5.26.51).
- **Settings** — Container Workers (F10) section (v5.26.56), Per-story approval gate toggle (v5.26.62), interactive Whisper test mic dialog (v5.26.56).
- **Directory picker** — `+ New folder` while browsing (v5.26.46) on both New Session and New PRD modals.

### CI hardening — full v5.26.25 audit closed

- gh-actions audit fixes (v5.26.25): eBPF drift loud-fail, parent-full publish, concurrency guard.
- Pre-release security scan automation (v5.26.29) — gosec + govulncheck.
- Pinned action SHAs across all workflows (v5.26.38).
- gosec baseline-diff blocking gate (v5.26.40) — `.gosec-baseline.json`.
- agent-goose Dockerfile + CI publish (v5.26.42).
- Kind-cluster smoke workflow (v5.26.43).
- secret-scan + dependency-review + OWASP ZAP per-interface (v5.26.58 / v5.26.59).

### Smoke coverage

From 33/0/2 at v5.26.x start to **66/0/3+** at v5.26.68. New sections §7d–§7s cover: persistent fixtures, filter CRUD, memory + KG, MCP surface, schedule CRUD, channel send (`/api/test/message`), F10 agent lifecycle, skip_permissions config round-trip, Phase 3 lifecycle, wake-up surface, KG add+query, spatial-filter, entity detection, per-backend send, stdio MCP, wake-up L4/L5 prerequisite. Targeted runs via `SMOKE_ONLY=`. Encryption-mode smoke runs via `release-smoke-secure.sh`.

### Smoke frequency rule

Operator-revised 2026-04-28: full smoke required on minor/major releases plus the first patch of any new feature. Subsequent patches use targeted runs. AGENT.md + memory feedback reflect.

## Configuration parity rule — enforced

Every config knob added in the v5.x window is reachable through YAML + Web UI + REST + MCP + CLI + comm channels. Per-feature parity checks in CI catch REST-only landings.

## Mobile companion — datawatch-app

7 child issues filed under [datawatch-app#10](https://github.com/dmz006/datawatch-app/issues/10) for the v5.26.27 → v5.26.67 PWA changes. Each scoped per-feature so the Android / Wear OS / Android Auto client can pick them up independently.

## Migration notes (operator)

- **No breaking REST changes.** All Phase-3 + Phase-4 endpoints are additive.
- **`autonomous.per_story_approval` defaults to false** — existing PRDs continue to auto-approve every story on PRD approval. Toggle via Settings → General → Autonomous to opt in.
- **JSON tag rename** in v5.26.67: `Story.FilesPlanned` / `Task.FilesPlanned` JSON tag is `files` (was `files_planned` for the v5.26.64–66 window). Stored PRDs from that 3-patch window need a one-time `jq` fix-up if they touched the field — extremely unlikely in production since the field was operator-edit-only and the window was 3 days. Re-emitting the PRD via `POST .../edit_story` migrates implicitly.
- **`agents.image_prefix` + `agents.image_tag`** must be set for cluster spawns to resolve worker images from your registry. v5.26.56 added the Web UI section.

## Known follow-ups (v6.1+)

- 5-item mempalace quick-win shortlist from the audit doc.
- Phase 4 file-conflict pre-merge (current detection is display-only; could refuse approve on conflicts).
- Stdio-mode MCP smoke wrapper.
- L4/L5 wake-up smoke with full F10 fixture (current §7s checks prerequisites only).
- Per-comm-backend outbound send smoke (gated on per-CI recipient config).
- Authenticated ZAP scans with secrets.ZAP_TOKEN.
- ZAP active scan workflow (currently baseline + API + diagrams; full active scan needs isolated runner).

## Upgrade path

Operator-driven. v5.x → v6.0 is a major cut: refresh containers, run full smoke + kind-smoke + OWASP ZAP, retag GHCR with the new major, push tag.

---

*This is a DRAFT. Operator finalizes prose / order / level-of-detail at cut time. CHANGELOG.md is the authoritative per-version record; this doc is the operator-facing narrative.*
