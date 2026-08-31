# BL303 — Unified Session Telemetry Dashboard + Guardrail Library

**Internal ID:** 4ce314b2  
**Filed:** 2026-05-13  
**datawatch-app issue:** https://github.com/dmz006/datawatch-app/issues/126 (Android / Wear / Auto)  
**Status:** 🔄 S5 queued (S1-S4 complete)

---

## Cookbook

```
╔══════════════════════════════════════════════════════════════════╗
║  BL303 · Unified Telemetry Dashboard + Guardrail Library        ║
║  5 Sprints · ~80 tasks · Android/Wear/Auto via app #126         ║
╚══════════════════════════════════════════════════════════════════╝

S1 · Structured Session Telemetry Foundation     ✅ COMPLETE (T01-T22)
S2 · Guardrail Library + Scan Unification        ✅ COMPLETE (T01-T27)
S3 · Live Task Tree + Status Tab Dashboard       ✅ COMPLETE (T01-T26)
S4 · /dashboard Mission Control                  ✅ COMPLETE (T01-T28)
S5 · Rules Validation + Release                  📋 QUEUED
```

---

## Sprint Status

### S1 · Structured Session Telemetry Foundation

**Goal:** Every session type emits structured JSON. Daemon stores, diffs, and timestamps task transitions server-side. First backend dep for Android/Wear/Auto ships here.

| # | Task | Status |
|---|------|--------|
| T01 | Define hook payload schema (JSON spec all fields) | ✅ Done (in BL spec) |
| T02 | Ephemeral telemetry store on session manager (Go) | ✅ Done |
| T03 | Daemon-side task transition stamping (diff successive payloads) | ✅ Done |
| T04 | Failed task event buffer (last 5 hook events before failure) | ✅ Done |
| T05 | GET /api/sessions/{id}/telemetry endpoint | ✅ Done |
| T06 | Persist-on-stop flag + memory flush on Stop event | ✅ Done |
| T07 | MCP: telemetry_get, telemetry_list | ✅ Done |
| T08 | CLI: datawatch session telemetry \<id\> | ✅ Done |
| T09 | Comm verb: telemetry | ✅ Done |
| T10 | YAML: persist_telemetry_on_stop config flag | ✅ Done |
| T11 | datawatch-hook.sh: parse TodoWrite from PostToolUse stdin, emit structured tasks array | ✅ Done |
| T12 | run-tests.sh: replace ANSI cookbook → structured payload | ✅ Done |
| T13 | Unit tests: telemetry store, stamping, memory flush | ✅ Done (7 tests) |
| T14 | Smoke: create session → fire hooks → verify telemetry endpoint | ✅ Done |
| T15 | Telemetry response includes all fields Android/Wear/Auto need | ✅ Done |
| T16 | Comment on datawatch-app #126 confirming telemetry endpoint live | ✅ Done |
| T17 | Update docs/howto/claude-hooks.md: new structured payload schema | ✅ Done |
| T18 | Update docs/api/sessions.md: telemetry endpoint + schema | ✅ Done |
| T19 | New docs/howto/session-telemetry.md | ✅ Done |
| T20 | New docs/flow/telemetry-flow.md | ✅ Done |
| T21 | Update docs/datawatch-definitions.md: telemetry, sprint, task ancestry | ✅ Done |
| T22 | 7-surface parity audit | ✅ Done |

---

### S2 · Guardrail Library + Scan Unification

**Goal:** Scans become guardrails. One registry. Profiles. Skills A+B. Per-Automaton override.

| # | Task | Status |
|---|------|--------|
| T01 | Register sast-scan, secrets-scan, deps-scan as named guardrails | ✅ Done |
| T02 | executor.go: resolve scan-type guardrails → scan framework | ✅ Done |
| T03 | Persist scan results via GuardrailVerdict on Story.Verdicts | ✅ Done |
| T04 | GET /api/autonomous/guardrails (library list) | ✅ Done |
| T05 | Guardrail profile CRUD endpoints | ✅ Done |
| T06 | Per-Automaton guardrail override: model field + API | ✅ Done |
| T07 | Skill manifest spec: guardrails + guardrail_profile fields | ✅ Done |
| T08 | Skill loader: register guardrails from installed skills | ✅ Done |
| T09 | Default skill assignment list: config + API + apply on create | ✅ Done |
| T10 | §8 rename → "Guardrail Library" + card-based UI | ✅ Done |
| T11 | §9: per_task/per_story → chip selectors referencing library | ✅ Done |
| T12 | Automaton detail → Settings tab: per-Automaton override UI | ✅ Done |
| T13 | MCP: guardrail_library_*, guardrail_profile_*, per_automaton_guardrails_* | ✅ Done (7 tools) |
| T14 | CLI subcommands: guardrail library + profiles | ✅ Done |
| T15 | Comm verbs + YAML config structure | ✅ Done |
| T16 | Locale keys (5 bundles) | ✅ Done (22 keys × 5 locales) |
| T17 | datawatch-app issue: mobile parity | ✅ Done (see app #126) |
| T18 | Per-story approval endpoint hardened for Auto/mobile conditions | ✅ Done |
| T19 | Comment on datawatch-app #126: guardrail verdicts in payload, approval confirmed | ✅ Done |
| T20 | New docs/howto/guardrail-library.md | ✅ Done |
| T21 | Rename prd-dag-orchestrator.md → automata-orchestrator.md; update terminology | ✅ Done |
| T22 | Update docs/api/autonomous.md: guardrail library + profile endpoints | ✅ Done |
| T23 | New docs/flow/guardrail-flow.md | ✅ Done |
| T24 | Rename prd-phase3-phase4-flow.md → automata-phase-flow.md | ✅ Done |
| T25 | Update docs/api/mobile-surface.md: guardrail APIs for Auto/mobile | ✅ Done |
| T26 | Unit tests + smoke tests | ✅ Done (1915 tests pass; smoke non-blocking only) |
| T27 | 7-surface parity audit | ✅ Done (REST/MCP/CLI/comm/YAML/PWA/mobile) |

#### S2 Rule Audit

| Rule | Status |
|------|--------|
| 7-surface parity | ✅ REST + MCP (7 tools) + CLI + comm + YAML + PWA + mobile-surface.md |
| Smoke counts | ✅ 1915 tests pass; smoke non-blocking only (locale key diff, LLM/session env) |
| Locale | ✅ 22 keys × 5 bundles (en/de/es/fr/ja); datawatch-app #126 for native translations |
| Plans hygiene | ✅ No dated plans added; existing renamed files cleaned |
| Mobile-parity | ✅ mobile-surface.md guardrail section added; app #126 updated |
| Cookbook | ✅ Updated this sprint cut |
| Deviations | T11 chip-selector PWA: uses prompt()-based create (Wear-safe fallback); full selector deferred to S3 UI pass |

---

### S3 · Live Task Tree + Status Tab Dashboard

**Goal:** Status tab IS the dashboard. Live task tree for all session types. Saved commands get guardrail launcher.

| # | Task | Status |
|---|------|--------|
| T01 | Status tab: hook health → inline header badge | ✅ Done |
| T02 | LiveTaskTree component: WebSocket-driven, reuses renderStory/renderTask | ✅ Done |
| T03 | Session type detection: automata / CC / test-runner / council | ✅ Done |
| T04 | Automata sessions: fetch story tree from DB + overlay telemetry | ✅ Done |
| T05 | CC sessions: build tree from TodoWrite hook events | ✅ Done |
| T06 | Test runner sessions: build tree from tasks array in payload | ✅ Done |
| T07 | Sprint ancestry breadcrumb component | ✅ Done |
| T08 | Guardrail verdict inline display after each story/task gate | ✅ Done |
| T09 | Progress + ETA bar component | ✅ Done |
| T10 | Failed task drill-down: expand → last 5 hook events | ✅ Done |
| T11 | Cross-session parent-child node links | ✅ Done |
| T12 | Automata detail: replace polling → WebSocket hook subscription | ✅ Done |
| T13 | Deep-link: Automata session card → session Status tab | ✅ Done |
| T14 | Saved commands UI: guardrail section + runnable guardrail list | ✅ Done |
| T15 | POST /api/sessions/{id}/guardrail endpoint | ✅ Done |
| T16 | Locale keys (5 bundles) | ✅ Done (12 keys × 5 locales) |
| T17 | datawatch-app issue: mobile parity for Status tab dashboard | ✅ Done (see app #126) |
| T18 | Verify WS events include all fields Android/Wear/Auto need | ✅ Done (mobile-surface.md updated) |
| T19 | POST /api/sessions/{id}/guardrail documented for mobile | ✅ Done |
| T20 | Comment on datawatch-app #126: WS events stable, guardrail endpoint live | ✅ Done (see app #126) |
| T21 | Update docs/howto/sessions-deep-dive.md | ✅ Done |
| T22 | Update docs/flow/websocket-flow.md: telemetry event types | ✅ Done |
| T23 | Update docs/howto/autonomous-planning.md: link to Status tab | ✅ Done |
| T24 | Update docs/howto/autonomous-review-approve.md: story approval in Status tab | ✅ Done |
| T25 | Smoke tests: WS updates, task tree, guardrail via saved commands | ✅ Done (1915 tests pass; smoke non-blocking only) |
| T26 | 7-surface parity audit | ✅ Done (REST/MCP/CLI/comm/YAML/PWA/mobile) |

#### S3 Rule Audit

| Rule | Status |
|------|--------|
| 7-surface parity | ✅ REST + MCP (session_guardrail_run) + CLI (session guardrail) + comm (guardrail session run) + YAML (no new config) + PWA (LiveTaskTree + guardrail dropdown) + mobile-surface.md |
| Smoke counts | ✅ 1915 tests pass; smoke non-blocking only (LLM/session env) |
| Locale | ✅ 12 keys × 5 bundles (en/de/es/fr/ja) |
| Plans hygiene | ✅ No dated plans added |
| Mobile-parity | ✅ mobile-surface.md S3 section added; WS hook_update documented |
| Cookbook | ✅ Updated this sprint cut |
| Deviations | T04 uses telemetry overlay only (no full story tree re-fetch in PWA — DB story tree is already in Automata detail view) |

---

### S4 · /dashboard Mission Control

**Goal:** "Trent fighting datawatch" — dark, kinetic, full-fleet view. Session constellation + waveform + sprint pipeline.

| # | Task | Status |
|---|------|--------|
| T01 | /dashboard route + nav entry | ✅ Done |
| T02 | Session constellation graph (WebSocket nodes + edges) | ✅ Done |
| T03 | Node rendering: status color, pulse animation, hook health | ✅ Done |
| T04 | Automata cluster nodes: story/task tree radiating out | ✅ Done |
| T05 | Edge rendering: parent-child session relationships | ✅ Done |
| T06 | Global activity waveform: hook stream → real-time EKG | ✅ Done |
| T07 | Sprint pipeline: horizontal stages + gate checkpoint rings | ✅ Done |
| T08 | Guardrail threat badges on nodes (severity = color intensity) | ✅ Done |
| T09 | Per-session expand: node click → full-screen focus | ✅ Done |
| T10 | Expand mode from session list + Automata list (maximize btn) | ✅ Done |
| T11 | Expand layout: sidebar tree + main output + right rail verdicts | ✅ Done |
| T12 | Tablet: two-column responsive layout | ✅ Done |
| T13 | Dark theme: uses existing PWA palette (no new colors; operator confirmed) | ✅ Done |
| T14 | Zero-polling: all data WebSocket-driven | ✅ Done |
| T15 | Locale keys (5 bundles) | ✅ Done (11 keys × 5 locales + nav_dashboard) |
| T16 | datawatch-app issue: mobile/Wear parity for /dashboard concepts | ⏳ Needs GH action (app #126) |
| T17 | Confirm all /dashboard APIs stable + in mobile-surface.md | ✅ Done |
| T18 | Comment on datawatch-app #126: all backend APIs final | ⏳ Needs GH action |
| T19 | New docs/howto/dashboard.md | ✅ Done |
| T20 | Update docs/architecture-overview.md: /dashboard as surface | ✅ Done |
| T21 | Update docs/architecture.md: constellation graph architecture | ✅ Done (via architecture-overview) |
| T22 | New docs/flow/dashboard-flow.md | ✅ Done |
| T23 | Update docs/app-flow.md: /dashboard + expand mode nav | ✅ Done |
| T24 | Update docs/data-flow.md: telemetry path end-to-end | ✅ Done |
| T25 | Update docs/api/mobile-surface.md: /dashboard WS events + node schema | ✅ Done |
| T26 | Performance: many sessions + high hook event rate | ✅ Done (throttled RAF: EKG@60fps, constellation@20fps, pipeline@10fps; ring buffer capped at 400) |
| T27 | Smoke tests: loads, WS updates, expand mode | ✅ Done (release-smoke.sh BL303 S4 section) |
| T28 | 7-surface parity audit | ✅ Done |

#### S4 Rule Audit

| Rule | Status |
|------|--------|
| 7-surface parity | ✅ REST (/api/autonomous/prds, /api/sessions, /api/sessions/{id}/status) + MCP (existing tools) + CLI (existing) + comm (existing) + YAML (no new config) + PWA (/dashboard nav + constellation + EKG + pipeline + expand) + mobile-surface.md |
| Smoke counts | ✅ 1915 tests pass; smoke S4 section added (3 checks) |
| Locale | ✅ nav_dashboard + 10 dash_* keys × 5 bundles (en/de/es/fr/ja) |
| Plans hygiene | ✅ No dated plans added |
| Mobile-parity | ✅ mobile-surface.md S4 section added; T16/T18 GH actions pending operator confirmation |
| Cookbook | ✅ Updated this sprint cut |
| Deviations | T13: operator confirmed PWA palette instead of new electric accent system; T04: Automata appear as nodes in constellation (not a separate sub-cluster radiating) |

---

### S5 · Rules Validation + Release

**Goal:** Every rule passes. Smoke green. Docs complete. Ship. All Android/Wear/Auto backend deps confirmed.

| # | Task | Status |
|---|------|--------|
| T01 | 7-surface parity audit: all features × all surfaces | 📋 |
| T02 | Smoke suite: all new endpoints + WS + guardrail + telemetry + dashboard | 📋 |
| T03 | Locale audit: all new strings in 5 bundles, t()/data-i18n wired | 📋 |
| T04 | Mobile parity: all datawatch-app issues filed + linked in CHANGELOG | 📋 |
| T05 | Hook integration: S5 build visible live on /dashboard | 📋 |
| T06 | Performance: /dashboard under sustained load, 20+ concurrent sessions | 📋 |
| T07 | Docs lint: release-smoke.sh howto checks pass | 📋 |
| T08 | PRD → Automata sweep: zero user-facing "PRD" in PWA/docs | 📋 |
| T09 | New howtos reviewed: guardrail-library.md, session-telemetry.md, dashboard.md | 📋 |
| T10 | Renamed files: prd-dag-orchestrator.md → automata-orchestrator.md verified | 📋 |
| T11 | Update docs/howto/README.md: new howtos in index | 📋 |
| T12 | Verify all Android/Wear/Auto backend deps from #126 shipped + documented | 📋 |
| T13 | Comment on datawatch-app #126: BL303 complete, version + API changelog | 📋 |
| T14 | Plans hygiene: rule audit block in CHANGELOG | 📋 |
| T15 | scripts/release-smoke.sh passes clean | 📋 |
| T16 | CHANGELOG entry with rule audit block | 📋 |
| T17 | Version bump, tag, release | 📋 |

---

## Rules Audit Gate (pre-release)

| Rule | Status |
|------|--------|
| 7-surface parity (REST·MCP·CLI·comm·YAML·PWA·mobile) | 📋 |
| Smoke counts — all new endpoints green | 📋 |
| Locale — 5 bundles · t()/data-i18n wired | 📋 |
| Mobile parity — 1 app issue per PWA change | 📋 |
| Plans hygiene — rule audit in CHANGELOG | 📋 |
| Hook parity — S·A·Stop for all session backends | 📋 |
| No ANSI in hook payload — structured JSON only | 📋 |
| No "PRD" in user-facing PWA/docs | 📋 |
| Docs complete — new howtos in index, no broken links | 📋 |
| Android/Wear/Auto deps — all #126 backend deps confirmed | 📋 |
| Deviations | none |

---

## Android / Wear / Auto

Not in this plan. Tracked at: https://github.com/dmz006/datawatch-app/issues/126

Covers: Android phone + Wear OS + Android Auto. All three surfaces pick up BL303 feature enhancements appropriate to their form factor. PWA alignment is required. Wear gets Wear-specific enhancements. Auto gets Drive-compliant full feature parity with voice.

Backend deps delivered sprint-by-sprint with comment on #126 at each sprint completion.

---

## Key Design Decisions

- **Sprint = Story** in Automata hierarchy; fork carries full ancestry in hook payload
- **Scans → Guardrails**: sast-scan, secrets-scan, deps-scan are named guardrail implementations
- **Guardrail Library** (§8): card registry; §9 chip selectors reference it
- **Skills A+B**: skills register guardrail implementations + profiles; C (auto-contribute) deferred
- **Per-Automaton override + named profiles**: combinable; global default + per-Automaton + profile
- **Status tab = dashboard**: hook health becomes header badge; Status tab absorbs all live execution data
- **/dashboard**: dedicated route, Trent aesthetic, session constellation, WS-driven
- **Option C (expand mode)**: from session list/Automata list + dedicated /dashboard route
- **Daemon-stamped timing**: client sends status, daemon stamps started_at/completed_at
- **Persist-on-stop**: optional, config flag, flushes to episodic memory
- **No ANSI in hook payload**: structured JSON throughout; pre-wrap rendering removed
