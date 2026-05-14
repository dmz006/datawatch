# BL303 — Unified Session Telemetry Dashboard + Guardrail Library

**Internal ID:** 4ce314b2  
**Filed:** 2026-05-13  
**datawatch-app issue:** https://github.com/dmz006/datawatch-app/issues/126 (Android / Wear / Auto)  
**Status:** 🔄 S1 in progress

---

## Cookbook

```
╔══════════════════════════════════════════════════════════════════╗
║  BL303 · Unified Telemetry Dashboard + Guardrail Library        ║
║  5 Sprints · ~80 tasks · Android/Wear/Auto via app #126         ║
╚══════════════════════════════════════════════════════════════════╝

S1 · Structured Session Telemetry Foundation     🔄 IN PROGRESS
S2 · Guardrail Library + Scan Unification        📋 QUEUED
S3 · Live Task Tree + Status Tab Dashboard       📋 QUEUED
S4 · /dashboard Mission Control                  📋 QUEUED
S5 · Rules Validation + Release                  📋 QUEUED
```

---

## Sprint Status

### S1 · Structured Session Telemetry Foundation

**Goal:** Every session type emits structured JSON. Daemon stores, diffs, and timestamps task transitions server-side. First backend dep for Android/Wear/Auto ships here.

| # | Task | Status |
|---|------|--------|
| T01 | Define hook payload schema (JSON spec all fields) | ✅ Done (in BL spec) |
| T02 | Ephemeral telemetry store on session manager (Go) | 📋 |
| T03 | Daemon-side task transition stamping (diff successive payloads) | 📋 |
| T04 | Failed task event buffer (last 5 hook events before failure) | 📋 |
| T05 | GET /api/sessions/{id}/telemetry endpoint | 📋 |
| T06 | Persist-on-stop flag + memory flush on Stop event | 📋 |
| T07 | MCP: telemetry_get, telemetry_list | 📋 |
| T08 | CLI: datawatch session telemetry \<id\> | 📋 |
| T09 | Comm verb: telemetry | 📋 |
| T10 | YAML: persist_telemetry_on_stop config flag | 📋 |
| T11 | datawatch-hook.sh: parse TodoWrite from PostToolUse stdin, emit structured tasks array | 📋 |
| T12 | run-tests.sh: replace ANSI cookbook → structured payload | 📋 |
| T13 | Unit tests: telemetry store, stamping, memory flush | 📋 |
| T14 | Smoke: create session → fire hooks → verify telemetry endpoint | 📋 |
| T15 | Telemetry response includes all fields Android/Wear/Auto need | 📋 |
| T16 | Comment on datawatch-app #126 confirming telemetry endpoint live | 📋 |
| T17 | Update docs/howto/claude-hooks.md: new structured payload schema | 📋 |
| T18 | Update docs/api/sessions.md: telemetry endpoint + schema | 📋 |
| T19 | New docs/howto/session-telemetry.md | 📋 |
| T20 | New docs/flow/telemetry-flow.md | 📋 |
| T21 | Update docs/datawatch-definitions.md: telemetry, sprint, task ancestry | 📋 |
| T22 | 7-surface parity audit | 📋 |

---

### S2 · Guardrail Library + Scan Unification

**Goal:** Scans become guardrails. One registry. Profiles. Skills A+B. Per-Automaton override.

| # | Task | Status |
|---|------|--------|
| T01 | Register sast-scan, secrets-scan, deps-scan as named guardrails | 📋 |
| T02 | executor.go: resolve scan-type guardrails → scan framework | 📋 |
| T03 | Persist scan results via GuardrailVerdict on Story.Verdicts | 📋 |
| T04 | GET /api/autonomous/guardrails (library list) | 📋 |
| T05 | Guardrail profile CRUD endpoints | 📋 |
| T06 | Per-Automaton guardrail override: model field + API | 📋 |
| T07 | Skill manifest spec: guardrails + guardrail_profile fields | 📋 |
| T08 | Skill loader: register guardrails from installed skills | 📋 |
| T09 | Default skill assignment list: config + API + apply on create | 📋 |
| T10 | §8 rename → "Guardrail Library" + card-based UI | 📋 |
| T11 | §9: per_task/per_story → chip selectors referencing library | 📋 |
| T12 | Automaton detail → Settings tab: per-Automaton override UI | 📋 |
| T13 | MCP: guardrail_library_*, guardrail_profile_*, per_automaton_guardrails_* | 📋 |
| T14 | CLI subcommands: guardrail library + profiles | 📋 |
| T15 | Comm verbs + YAML config structure | 📋 |
| T16 | Locale keys (5 bundles) | 📋 |
| T17 | datawatch-app issue: mobile parity | 📋 |
| T18 | Per-story approval endpoint hardened for Auto/mobile conditions | 📋 |
| T19 | Comment on datawatch-app #126: guardrail verdicts in payload, approval confirmed | 📋 |
| T20 | New docs/howto/guardrail-library.md | 📋 |
| T21 | Rename prd-dag-orchestrator.md → automata-orchestrator.md; update terminology | 📋 |
| T22 | Update docs/api/autonomous.md: guardrail library + profile endpoints | 📋 |
| T23 | New docs/flow/guardrail-flow.md | 📋 |
| T24 | Rename prd-phase3-phase4-flow.md → automata-phase-flow.md | 📋 |
| T25 | Update docs/api/mobile-surface.md: guardrail APIs for Auto/mobile | 📋 |
| T26 | Unit tests + smoke tests | 📋 |
| T27 | 7-surface parity audit | 📋 |

---

### S3 · Live Task Tree + Status Tab Dashboard

**Goal:** Status tab IS the dashboard. Live task tree for all session types. Saved commands get guardrail launcher.

| # | Task | Status |
|---|------|--------|
| T01 | Status tab: hook health → inline header badge | 📋 |
| T02 | LiveTaskTree component: WebSocket-driven, reuses renderStory/renderTask | 📋 |
| T03 | Session type detection: automata / CC / test-runner / council | 📋 |
| T04 | Automata sessions: fetch story tree from DB + overlay telemetry | 📋 |
| T05 | CC sessions: build tree from TodoWrite hook events | 📋 |
| T06 | Test runner sessions: build tree from tasks array in payload | 📋 |
| T07 | Sprint ancestry breadcrumb component | 📋 |
| T08 | Guardrail verdict inline display after each story/task gate | 📋 |
| T09 | Progress + ETA bar component | 📋 |
| T10 | Failed task drill-down: expand → last 5 hook events | 📋 |
| T11 | Cross-session parent-child node links | 📋 |
| T12 | Automata detail: replace polling → WebSocket hook subscription | 📋 |
| T13 | Deep-link: Automata session card → session Status tab | 📋 |
| T14 | Saved commands UI: guardrail section + runnable guardrail list | 📋 |
| T15 | POST /api/sessions/{id}/guardrail endpoint | 📋 |
| T16 | Locale keys (5 bundles) | 📋 |
| T17 | datawatch-app issue: mobile parity for Status tab dashboard | 📋 |
| T18 | Verify WS events include all fields Android/Wear/Auto need | 📋 |
| T19 | POST /api/sessions/{id}/guardrail documented for mobile | 📋 |
| T20 | Comment on datawatch-app #126: WS events stable, guardrail endpoint live | 📋 |
| T21 | Update docs/howto/sessions-deep-dive.md | 📋 |
| T22 | Update docs/flow/websocket-flow.md: telemetry event types | 📋 |
| T23 | Update docs/howto/autonomous-planning.md: link to Status tab | 📋 |
| T24 | Update docs/howto/autonomous-review-approve.md: story approval in Status tab | 📋 |
| T25 | Smoke tests: WS updates, task tree, guardrail via saved commands | 📋 |
| T26 | 7-surface parity audit | 📋 |

---

### S4 · /dashboard Mission Control

**Goal:** "Trent fighting datawatch" — dark, kinetic, full-fleet view. Session constellation + waveform + sprint pipeline.

| # | Task | Status |
|---|------|--------|
| T01 | /dashboard route + nav entry | 📋 |
| T02 | Session constellation graph (WebSocket nodes + edges) | 📋 |
| T03 | Node rendering: status color, pulse animation, hook health | 📋 |
| T04 | Automata cluster nodes: story/task tree radiating out | 📋 |
| T05 | Edge rendering: parent-child session relationships | 📋 |
| T06 | Global activity waveform: hook stream → real-time EKG | 📋 |
| T07 | Sprint pipeline: horizontal stages + gate checkpoint rings | 📋 |
| T08 | Guardrail threat badges on nodes (severity = color intensity) | 📋 |
| T09 | Per-session expand: node click → full-screen focus | 📋 |
| T10 | Expand mode from session list + Automata list (maximize btn) | 📋 |
| T11 | Expand layout: sidebar tree + main output + right rail verdicts | 📋 |
| T12 | Tablet: two-column responsive layout | 📋 |
| T13 | Dark theme + electric accent system (cyan/amber/red/green) | 📋 |
| T14 | Zero-polling: all data WebSocket-driven | 📋 |
| T15 | Locale keys (5 bundles) | 📋 |
| T16 | datawatch-app issue: mobile/Wear parity for /dashboard concepts | 📋 |
| T17 | Confirm all /dashboard APIs stable + in mobile-surface.md | 📋 |
| T18 | Comment on datawatch-app #126: all backend APIs final | 📋 |
| T19 | New docs/howto/dashboard.md | 📋 |
| T20 | Update docs/architecture-overview.md: /dashboard as surface | 📋 |
| T21 | Update docs/architecture.md: constellation graph architecture | 📋 |
| T22 | New docs/flow/dashboard-flow.md | 📋 |
| T23 | Update docs/app-flow.md: /dashboard + expand mode nav | 📋 |
| T24 | Update docs/data-flow.md: telemetry path end-to-end | 📋 |
| T25 | Update docs/api/mobile-surface.md: /dashboard WS events + node schema | 📋 |
| T26 | Performance: many sessions + high hook event rate | 📋 |
| T27 | Smoke tests: loads, WS updates, expand mode | 📋 |
| T28 | 7-surface parity audit | 📋 |

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
