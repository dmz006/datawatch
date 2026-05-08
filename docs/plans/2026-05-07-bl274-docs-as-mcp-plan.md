# BL274 — Docs-as-MCP-Interface — Implementation Plan

**Filed:** 2026-05-07 after operator interview (11 design questions answered).
**Owner:** datawatch-as-claude session.
**Living document:** updated at the end of every sprint as part of the quality gate.

## Design decisions (interview-locked)

| # | Question | Decision |
|---|----------|----------|
| Q1 | Index topology | (c) hybrid — core + skills unified index, plugins isolated per-plugin |
| Q2 | Indexing strategy | (c) both — vector (via existing embedder) primary, BM25 fallback |
| Q3 | docs_apply autonomy | (c) plan-then-execute default + (d) per-step risk gate opt-in |
| Q4 | exec_steps mechanism | (d)+(a) — front-matter exec_steps for ~22 critical howtos + LLM-translation fallback for rest |
| Q5 | MCP tool surface | 4 tools confirmed (docs_search, docs_read, docs_list_howtos, docs_apply) |
| Q6 | Trust tier defaults | (d) all opt-in — core trusted; skills + plugins per-source explicit opt-in |
| Q7 | Trust UX | (c) config seeds + runtime overrides, no auto-writeback; pending-trust queue with PWA breadcrumb + bulk select; comm/CLI notices include accept commands |
| Q8 | Indexing trigger | (c) fsnotify primary + explicit hooks belt-and-suspenders |
| Q9 | Plugin manifest docs | (d) `files:` required + optional `howtos:` metadata |
| Q10 | Skill SKILL.md | (c)+(b) — auto-index SKILL.md by default, `docs:` block extension when author wants more |
| Q11 | Curated scope | 22 howtos hand-authored exec_steps + 1.5 LLM-only (channel-state-engine + IDE-wiring half of mcp-tools) |

## Curated howtos (22 hand-authored exec_steps)

Setup / onboarding (5): `setup-and-install`, `identity-and-telos`, `profiles`, `comm-channels`, `secrets-manager`
Automation (4): `autonomous-planning`, `autonomous-review-approve`, `algorithm-mode`, `council-mode`
Infrastructure (3): `container-workers`, `tailscale-mesh`, `skills-sync`
Ops (2): `daemon-operations`, `federated-observer`
Sessions / chat / memory (5): `sessions-deep-dive`, `cross-agent-memory`, `chat-and-llm-quickstart`, `voice-input`, `mcp-tools` (operator-side wiring half only)
Automation graph (3): `pipeline-chaining`, `prd-dag-orchestrator`, `evals`

LLM-translation only: `channel-state-engine.md`, IDE-wiring half of `mcp-tools.md`.

## Shipping model

- **Core docs** — embedded via `//go:embed web` (already; no change).
- **Pre-built BM25 index** — `make docs-index` generates `internal/server/web/assets/docs-bm25-index.json`; embedded into binary; loaded into memory at first boot. Day 0 search works with zero embedder dependency.
- **Vector index** — built at first boot by background goroutine using operator's configured embedder (Ollama/OpenAI/etc.); persisted to `~/.datawatch/docs-index/core/vectors.sqlite`. While building, search falls back to BM25.
- **Skills + plugins** — ship docs alongside their bundles; indexed at sync/install once trusted.

## End-of-Sprint Quality Gate (mandatory before tagging)

Every sprint must complete every line below before `gh release create`:

1. **Functional tests** — `rtk go test ./...` zero failures, count delta logged here.
2. **Smoke test** — `bash scripts/release-smoke.sh` exits 0.
3. **Rule audit** — every AGENT.md rule (≈30) gets a Pass / N/A / Fix-needed line in this doc's per-sprint section. Sprint cannot ship with any Fix-needed.
4. **Mobile-parity issue** filed at `dmz006/datawatch-app` per the Mobile-Parity Rule (no exceptions).
5. **Plan-doc update** — this file gets the per-sprint section filled in (test counts, audit results, deviations from plan, follow-ups discovered).
6. **Memory update** — sprint outcome recorded to `~/.claude/projects/.../memory/`.

## Sprints

### Sprint 1 → v6.16.0 — Foundation

**Status:** 🟡 in progress (started 2026-05-07).

**Scope:**
- New `internal/docsindex/` package: chunker, BM25, indexer, search, trust.
- Build-time `make docs-index` target → embedded BM25 index in binary.
- 4 MCP tools (`docs_search`, `docs_read`, `docs_list_howtos`, `docs_apply` plan-only).
- 4 REST endpoints + 4 CLI subcommands + 4 comm verbs (7-surface parity).
- Trust system: config seed + `~/.datawatch/docs-trust.json` runtime + `~/.datawatch/docs-trust-pending.json` queue.
- Trust commands across all 7 surfaces.
- 5 curated howtos with `exec_steps` front-matter: `setup-and-install`, `identity-and-telos`, `secrets-manager`, `council-mode`, `daemon-operations`.
- PWA: Settings → General → Docs Search card with trust list + pending-trust badge.
- Locale × 5 bundles for new strings.
- Tests: chunker, BM25 ranking, trust load/save/merge, MCP tool round-trips, exec_steps frontmatter parser, 5 howto exec_steps validate against live MCP registry.

**Quality gate:** *(populated at sprint end)*

### Sprint 2 → v6.17.0 — Vector index + 8 more curated howtos

**Status:** 📋 planned.

**Scope:**
- `internal/docsindex/vector.go` integrating with existing `internal/memory` embedder interface.
- First-boot vector index build (background goroutine).
- Hybrid search: vector first, BM25 fallback; `index_kind` reports which.
- 8 more curated howtos: `profiles`, `comm-channels`, `autonomous-planning`, `autonomous-review-approve`, `algorithm-mode`, `container-workers`, `tailscale-mesh`, `skills-sync`.

**Quality gate:** *(populated at sprint end)*

### Sprint 3 → v6.18.0 — `docs_apply` execute mode + LLM fallback + 6 more howtos

**Status:** 📋 planned.

**Scope:**
- `docs_apply mode=execute` with approval token validation (Q3c).
- Per-step risk gate opt-in (Q3d).
- LLM-translation fallback for non-curated howtos.
- `provenance: authored | llm_translated` per step in every plan.
- 6 more curated howtos: `federated-observer`, `sessions-deep-dive`, `cross-agent-memory`, `chat-and-llm-quickstart`, `voice-input`, `mcp-tools` (operator-wiring half).

**Quality gate:** *(populated at sprint end)*

### Sprint 4 → v6.19.0 — fsnotify + skills/plugins + final 3 howtos + pending-trust UX

**Status:** 📋 planned.

**Scope:**
- fsnotify watchers on `~/.datawatch/skills/` and `~/.datawatch/plugins/`.
- Skill `SKILL.md` auto-indexing once trusted; optional `docs:` block extension.
- Plugin manifest `docs:` block parser + per-plugin isolated indexes.
- Pending-trust queue UX completion (PWA bulk select).
- Final 3 curated howtos: `pipeline-chaining`, `prd-dag-orchestrator`, `evals`. **22/22 ✓**

**Quality gate:** *(populated at sprint end)*

### Sprint 5 → v6.20.0 — Rules + CI lint + final polish

**Status:** 📋 planned.

**Scope:**
- AGENT.md adds: Docs-as-MCP Currency, Howto-Coverage, Plugin-Manifest Validation rules.
- CI lint scripts: `check-curated-howtos.sh`, `check-howto-coverage.sh`, `check-plugin-manifests.sh`.
- New `docs/howto/docs-as-mcp.md` with its own exec_steps.
- `docs/datawatch-definitions.md` Docs-as-MCP section + See-also footers.
- BL274 marked closed.

**Quality gate:** *(populated at sprint end)*

## Living-doc rule

This file is updated at the end of every sprint with:
- Functional test result + count delta.
- Smoke test result.
- Rule-audit table (every AGENT.md rule with Pass / N/A / Fix-needed).
- Deviations from plan (what changed mid-sprint and why).
- Follow-up issues / frozen BLs surfaced during the sprint.
- Mobile-parity issue link.
- Memory file path for the sprint record.

When this plan-doc is fully filled in across all 5 sprints, BL274 is closed.
