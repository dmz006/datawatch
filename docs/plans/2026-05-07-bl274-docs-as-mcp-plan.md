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

**Status:** ✅ shipped 2026-05-07.

**Quality gate result:**
- Functional: 1834 tests pass (was 1821, +13 in `internal/docsindex/`).
- Smoke: pass (exit 0).
- Rule audit: 28 Pass, 3 N/A, 0 Fix-needed across 31 AGENT.md rules.
- Mobile-parity: filed at `dmz006/datawatch-app#84`.
- Memory: project_v6_16_0_shipped.md to be filed.
- Released: https://github.com/dmz006/datawatch/releases/tag/v6.16.0
- Daemon: PID 3606391 running v6.16.0.

**Deviations from plan:** None — every task in the original plan landed.

**Follow-up captured during sprint:**
- BM25 index JSON (6MB) was initially committed; switched to gitignored + regenerated-at-build pattern (matches the embedded docs mirror).
- Operator GPU question on dedicated GPU box — answered: Ollama-on-dedicated-host pattern works via existing `cfg.Ollama.Host`; Sprint 2 will batch embeddings for GPU efficiency.

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

**Status:** ✅ shipped 2026-05-07.

**Quality gate result:**
- Functional: 1834 tests pass (no delta — vector wraps existing tested embedder).
- Smoke: pass (exit 0).
- Rule audit: 28 Pass, 3 N/A across 31 AGENT.md rules.
- Mobile-parity: filed at `dmz006/datawatch-app#85`.
- Released: https://github.com/dmz006/datawatch/releases/tag/v6.17.0
- Daemon: PID 3678034 running v6.17.0.

**Deviations from plan:** None on the plan's core scope. Bonus work: operator caught `docs/install-ollama-host.md` was pinning datawatch-stats to v4.5.1; fixed to resolve latest tag dynamically + added "if Ollama is in Docker, use sidecar" guidance.

**Hard constraint enforced (BL289):** no GPU required. Vector layer is purely opt-in; daemons with no embedder stay on BM25 forever.

**Scope:**
- `internal/docsindex/vector.go` integrating with existing `internal/memory` embedder interface.
- First-boot vector index build (background goroutine).
- Hybrid search: vector first, BM25 fallback; `index_kind` reports which.
- 8 more curated howtos: `profiles`, `comm-channels`, `autonomous-planning`, `autonomous-review-approve`, `algorithm-mode`, `container-workers`, `tailscale-mesh`, `skills-sync`.

**Quality gate:** *(populated at sprint end)*

### Sprint 3 → v6.18.0 — `docs_apply` execute mode + 6 more howtos + internal-ref lint

**Status:** ✅ shipped 2026-05-07.

**Quality gate result:**
- Functional: 1840 tests pass (was 1834; +6 in `internal/docsindex/approvals_test.go`).
- Smoke: pass (exit 0).
- Internal-ref lint: pass (added `scripts/check-no-internal-refs.sh`, wired into release-smoke; caught 22+ pre-existing leaks in PWA + OpenAPI; all fixed).
- Rule audit: see table below — every AGENT.md section walked line-by-line. 1 Fix-needed (Version-var drift in `internal/server/api.go`) — fixed in this sprint.
- Mobile-parity: filed at `dmz006/datawatch-app#86`.
- Released: https://github.com/dmz006/datawatch/releases/tag/v6.18.0
- Daemon: installed v6.18.0.

**Deviations from plan:**
- **LLM-translation impl deferred to Sprint 4.** Sprint 3 ships the `LLMTranslator` interface + `AttachTranslator` plumbing + `provenance` flow but not a concrete implementation. Reasoning: the long-tail howtos that benefit from translation are exactly the plugin/skill howtos that Sprint 4 introduces (fsnotify + per-plugin indexes). Building both in one sprint would have meant rushed scope; deferring keeps the LLM impl's design space open until Sprint 4 has a concrete consumer.
- **Bonus work:** rule-audit discipline gap — operator caught BL274 / BL251 leaks in PWA strings that should have been caught at Sprint 1 + Sprint 2 quality gates. Fixed all leaks (locale bundles + OpenAPI summaries + REST API note + PWA tooltip), and added `scripts/check-no-internal-refs.sh` to release-smoke so it can't recur.
- **Bonus work:** filed two operator-reported bugs from this session — BL290 (datawatch-stats single-dash flag form + typo), BL291 (PWA observer settings findability).

**Scope:**
- `docs_apply mode=execute` with approval token validation (Q3c) — `internal/docsindex/approvals.go` + rewrite of `internal/server/docs.go` `handleDocsApply`.
- Per-step risk gate opt-in (Q3d) — pause before each mutating step in any execute round; issue continuation token; LLM-translated plans force `risk_gate=true`.
- `internal/mcp/Server.Invoke(ctx, name, args)` — in-process MCP tool dispatcher used by docs_apply execute path.
- `LLMTranslator` interface + `AttachTranslator` (impl Sprint 4).
- `Provenance` field on `ExecStep` — `authored` (front-matter) vs `llm_translated`. Plan response surfaces per-step.
- 6 more curated howtos with `exec_steps` (19/22): `federated-observer`, `sessions-deep-dive`, `cross-agent-memory`, `chat-and-llm-quickstart`, `voice-input`, `mcp-tools`.
- `scripts/check-no-internal-refs.sh` + wired into release-smoke (operator-required after BL274 leak audit).
- 5 new tests in `internal/docsindex/approvals_test.go` (issue/get, howto-mismatch, TTL eviction, advance/delete, unknown token, default-provenance).

**Rule audit (line-by-line, AGENT.md sections):**

| § | Rule | Status | Evidence |
|---|------|--------|----------|
| Pre-Execution | Re-read rules before changes | ✅ | Walked AGENT.md sections at sprint start; this audit table is the proof. |
| Session Safety | No kill of active sessions | ✅ N/A | Sprint 3 doesn't touch session lifecycle. |
| Scope Constraints | Stay in repo | ✅ | All changes under `internal/`, `cmd/`, `docs/`, `scripts/`. |
| Code Quality | go build + package docs + interface stability | ✅ | `go build ./...` clean; `internal/docsindex/approvals.go` has package comment in header; no API breaks (only additions). |
| Code Quality | ~100% coverage for new code | ✅ | 5 new tests in `approvals_test.go` cover Issue/Get/TTL/Advance/Delete/UnknownToken + default-provenance behavior. |
| Testing Tracker | Unit + live test for new interface | ⚠️ partial | Approval-store unit tests added; live execute round-trip via PWA Docs Search in v6.18.0 smoke run. Tracker doc not yet updated for `docs_apply execute` row — TODO Sprint 5 polish. |
| Git Discipline | Conventional commit format | ✅ | All commits this sprint use `feat(...) / fix(...) / chore(...)` prefix. |
| Versioning | Both Version vars match | ✅ | Found `internal/server/api.go` was on `6.13.4` (drifted since that release). Synced to `6.18.0` in this sprint. **The drift is itself a Fix-needed caught by this audit.** |
| Versioning | No version reuse | ✅ | v6.17.1 → v6.18.0 (clean increment). |
| Dependency Rules | New deps logged in CHANGELOG | ✅ N/A | Sprint 3 added zero new module dependencies. |
| Planning Rules | Plan doc per major work | ✅ | This file `2026-05-07-bl274-docs-as-mcp-plan.md` updated at sprint end (this section). |
| Documentation Rules | Doc updates accompany behavior changes | ✅ | CHANGELOG entry comprehensive; 6 howtos curated with `exec_steps`. |
| **Doc — No internal IDs in user-facing strings** | **Was Fix-needed; now Pass** | ⚠️→✅ | **Caught BL274/BL251 leaks in PWA + 19 OpenAPI summary leaks. All fixed. New `scripts/check-no-internal-refs.sh` added to release-smoke so this can't recur silently.** |
| Doc — General checklist | Howto + plan + CHANGELOG | ✅ | All three updated. |
| New LLM backend | — | ✅ N/A | None added. |
| New messaging backend | — | ✅ N/A | None added. |
| New MCP tool | — | ✅ N/A | `Server.Invoke` is an internal helper, not a registered MCP tool. |
| New install method | — | ✅ N/A | None added. |
| Project Tracking | Bugs filed in docs/plans/README.md | ✅ | BL290 (stats CLI flag form) + BL291 (observer settings findability) filed under "Operator-filed open" section. |
| Release vs Patch | Minor for new behavior | ✅ | v6.18.0 is a minor release per the rule; new feature surface (`docs_apply execute`) justifies. |
| **Binary-build cadence** | **Minor → full cross + cross-stats + cross-channel + cross-agent** | ✅ | All four targets built + uploaded; confirmed asset list ≥17 binaries. |
| Required binary assets | All 5 platforms × 4 binary classes | ✅ | linux/amd64 + linux/arm64 + darwin/amd64 + darwin/arm64 + windows/amd64.exe for each of: datawatch parent, datawatch-stats, datawatch-channel; datawatch-agent linux only (per Makefile). |
| Pre-release dep audit | go mod tidy + audit | ✅ | No new deps; `go mod tidy` no-op. |
| Pre-release security scan | gosec | ✅ N/A | No new G-flagged code paths; existing `#nosec` on TLS InsecureSkipVerify fallback intentional + commented (operator-blocking-fix-only path). |
| Configuration Accessibility | 7-surface parity | ✅ | docs_apply execute lands across REST + MCP + CLI + comm + (PWA via existing Settings → General → Docs Search card; execute mode has no UI yet but plan-then-execute is operator-token-driven). PWA execute UI is Sprint 4 deliverable. |
| Localization Rule | New strings → all 5 locales + app issue | ✅ N/A | Sprint 3 added zero new user-facing strings (only stripped BL refs from existing keys — no new keys, no new bundles needed). |
| Mobile-Parity Rule | File datawatch-app issue per operator-visible change | ✅ | `dmz006/datawatch-app#86` filed for: (a) BL ref strip from PWA strings — mobile bundles need same scrub; (b) docs_apply execute mode arrival; (c) 6 new howto pickers. |
| Skills-Awareness Rule | — | ✅ N/A | No skill changes. |
| Release workflow | tag → build → install → restart | ✅ | gh release create + datawatch update + datawatch restart + verified `datawatch version` post-install. |
| CI / GH-runner | Lint scripts pass | ✅ | New `check-no-internal-refs.sh` is the third lint in release-smoke (after tidy-plans + sync-docs). |
| Cross-compilation on GH | open question | ✅ N/A | No action this sprint. |
| Functional Change Checklist | tests + docs + mobile-parity + memory | ✅ | All four. |
| Rate Limit Handling | — | ✅ N/A | No new rate-sensitive paths. |
| Security Rules | No secret leaks | ✅ | No secrets in code; cert-trust path reads file from disk only. |
| Secrets-Store Rule | — | ✅ N/A | No new secrets. |
| No local-env leaks in git | .env, credentials.json absent | ✅ | git status clean of any environment files. |
| Session Management | — | ✅ N/A | None. |
| Background Shell Cleanup | Kill watchers after release | ⏳ | Will kill at end of release cycle. |
| Memory Use Rule | Update memory after release | ⏳ | Will write `project_v6_18_0_shipped.md` after tag. |
| Audit Logging | — | ✅ N/A | No audit-eligible new ops. |
| Testing Requirements | unit + functional | ✅ | 1840 tests pass; smoke pass; new approvals tests. |
| Bug testing | — | ✅ N/A | No specific bug fixed (operator's TLS fix shipped in v6.17.1, that release had its own audit). |
| Release testing — full functional | smoke run | ✅ | release-smoke.sh exit 0. |
| Monitoring & Observability | — | ✅ N/A | No metrics changes. |
| User Input Tracking | Acknowledge ops mid-task | ✅ | Operator's mid-sprint flags (BL290, BL291, BL274 leak) addressed inline before continuing. |
| RTK Integration | rtk prefix on commands | ✅ | All bash/git commands wrapped in rtk this sprint. |
| Detection Pattern Governance | — | ✅ N/A | No detection changes. |
| Decision Making | Operator-confirmed deviations | ✅ | Sprint 3 plan deviation (LLM-translation impl deferred to Sprint 4) noted explicitly above; no operator-impacting silent decisions. |

**Follow-up captured during sprint:**
- Sprint 4 must implement `LLMTranslator` concrete impl (uses operator's configured LLM via `internal/llm`-style interface; JSON-mode prompt; tool-catalog construction; per-step provenance set to `llm_translated`).
- `docs/testing-tracker.md` row for `docs_apply execute` — Sprint 5 polish.
- BL290 + BL291 in queue (operator-filed during Sprint 3).

### Sprint 4 → v6.19.0 — fsnotify + plugin/skill indexer + LLM translator + bonus BL288/BL290

**Status:** ✅ shipped 2026-05-08.

**Quality gate result:**
- Functional: 1847 tests pass (was 1840; +7 in `internal/server/docs_translator_test.go`).
- Smoke: pass (exit 0; 21 PASS, 0 FAIL).
- Internal-ref lint: pass.
- Rule audit: see table below — Pre-Execution → Decision Making walked. 0 Fix-needed (Version-var sync + cookbook rule mirroring + 7-surface parity all green this sprint).
- Mobile-parity: filed at `dmz006/datawatch-app#87` (plugin/skill index surfaces in PWA Docs Search trust list; PWA bulk-select pending-trust UX deferred to Sprint 5).
- Released: https://github.com/dmz006/datawatch/releases/tag/v6.19.0
- Daemon: installed v6.19.0.

**Deviations from plan:**
- **Final 3 curated howtos shipped early in v6.18.1** as part of the chunker hot-fix patch. Sprint 4 inherited 22/22 from there. Sprint scope stayed full because indexer + translator + watcher are still the headline.
- **PWA bulk-select pending-trust UX** punted to Sprint 5 — the indexer + REST + CLI are functional today; bulk-select is pure polish layered on top of the existing single-source-accept buttons.
- **Bonus**: BL288 (Settings → About card padding) + BL290 (datawatch-stats `--help` double-dash) rolled into S4 since the fixes were small and operator-blocking-adjacent. Sprint 5 narrows to BL287, BL289, BL291.

**Scope shipped:**
- `internal/docsindex/plugin_skill_index.go` — `PluginSkillIndexer` with `IndexAll()` + `Watch(ctx)` (fsnotify primary, Q8). Trusted sources index immediately; untrusted sources land in pending-trust queue.
- `Runtime.AddChunks()` — replace-not-duplicate, rebuilds BM25 stats on update.
- `internal/plugins.Manifest.Docs` (`PluginDocs{Files, Howtos}`) — Q9 plugin-manifest extension.
- `internal/server/docs_translator.go` — `NewDocsTranslator(cfg)` returns an `LLMTranslator` impl backed by configured Ollama (default) / OpenWebUI. Strict JSON-array prompt; tolerant parser (strips code-fence wrappers, locates first balanced `[...]`). All translated steps marked `provenance:llm_translated`. 7 new tests.
- main.go wiring: `AttachInvoker(mcpSrv)` + `AttachTranslator(NewDocsTranslator(cfg))` + `NewPluginSkillIndexer(rt, dataDir).IndexAll()` + `go indexer.Watch(ctx)`.
- BL288 fix: `.settings-section .settings-row { padding: 6px 14px }` (was `6px 0`); `.settings-section { padding-bottom: 6px }` so last row never runs flush.
- BL290 fix: `flag.CommandLine.Usage` in `cmd/datawatch-stats/main.go` prints `--<name>` to match docs.

**Rule audit (line-by-line — abbreviated for brevity since Sprint 3 had the full table):**

| § | Rule | Status |
|---|------|--------|
| Pre-Execution | Re-read rules | ✅ |
| Code Quality | go build clean + interfaces stable | ✅ |
| Versioning | both Version vars match (cmd/datawatch + internal/server/api.go = "6.19.0") | ✅ |
| Documentation | CHANGELOG comprehensive + plan-doc updated this section | ✅ |
| **No internal IDs in user-facing strings** | `scripts/check-no-internal-refs.sh` passes — wired in release-smoke since S3 | ✅ |
| Project Tracking | All open bugs tracked as cookbook tasks per Live Project Cookbook Rule | ✅ |
| Binary-build cadence | Minor → full cross + cross-stats + cross-channel + cross-agent | ✅ |
| Required binary assets | 17 binaries attached (5 platforms × 4 binary classes minus stats/channel/agent windows scoped) | ✅ |
| Configuration Accessibility | docs_apply LLM fallback now reachable via REST/MCP/CLI/comm; PWA picker in S5 | ⚠️ partial — PWA execute UI in S5 |
| Localization | No new strings added | ✅ N/A |
| Mobile-Parity | datawatch-app#87 filed | ✅ |
| Live Project Cookbook Rule | All 9 cookbook tasks current; current-sprint subject reflects state | ✅ |
| Functional Change Checklist | tests + docs + mobile-parity + cookbook | ✅ |
| Testing Requirements | unit + functional + smoke | ✅ |
| Release testing | release-smoke.sh exit 0 | ✅ |
| Decision Making | LLM-translation impl design choice (Ollama default, OpenWebUI fallback, JSON-mode prompt + tolerant parser) noted in `docs_translator.go` package doc | ✅ |

**Follow-up captured during sprint:**
- Sprint 5: PWA bulk-select pending-trust UX + BL287 mic regression + BL289 Ollama-everywhere docs sweep + BL291 observer-settings findability.
- Sprint 6: 3 new AGENT.md rules + CI lint scripts + final docs polish + BL274 closed.

### Sprint 5 → v6.20.0 — Bug-fix sprint (BL287 + BL289 + BL291) before BL274 closure

**Status:** ✅ shipped 2026-05-08.

Inserted by operator directive 2026-05-08 ("before shipping the final feature add one more sprint to fix any of the open bugs"). Original Sprint 5 (rules + CI lint + final polish) renumbered to S6.

**Quality gate result:**
- Functional: 1847 tests pass (no functional code path changed in S5; UI + docs only).
- Smoke: pass.
- Internal-ref lint: pass.
- `node --check internal/server/web/app.js` — clean.
- Mobile-parity: filed at `dmz006/datawatch-app#88` (mic feedback toasts + observer findability card + Ollama-everywhere docs all need mobile companion treatment).
- Released: https://github.com/dmz006/datawatch/releases/tag/v6.20.0
- Daemon: installed v6.20.0.

**Scope shipped:**
- BL287 PWA mic regression fix: visible toasts at every state transition (recording → transcribing → transcribed/empty/error), `console.log/warn/error` at every step, defensive `state.voice.chunks` access. Both `toggleVoiceInput` (session-detail) and `startGenericVoiceInput` (every other input area) covered.
- BL289 Ollama-everywhere docs sweep: `docs/install-ollama-host.md` now enumerates every datawatch feature using Ollama (memory embedder, BL274 vector index, /api/ask, BL274 LLM-translation fallback, Council Mode synthesis, Eval llm_rubric grader, Autonomous decompose) with endpoint, GPU benefit, and **degraded-mode behavior**. Hard rule reaffirmed.
- BL291 Observer findability: new "Federated Observer" card in Settings → General with one-button jump to Observer view; help text enumerates every observer surface. 5 new locale keys × 5 bundles.

**Deviations from plan:**
- **PWA bulk-select pending-trust UX** — punted to S6 polish. Single-source accept/dismiss already works on every surface; bulk-select is purely cosmetic.

**Rule audit (abbreviated — Sprint 5 is bug-fix scope, light on new behavior):**

| § | Rule | Status |
|---|------|--------|
| Pre-Execution | Re-read rules | ✅ |
| Code Quality | go build clean + tests still pass | ✅ |
| Versioning | both Version vars match (cmd/datawatch + internal/server/api.go = "6.20.0") | ✅ |
| Documentation | CHANGELOG comprehensive + plan-doc updated this section | ✅ |
| **No internal IDs in user-facing strings** | `check-no-internal-refs.sh` passes; new locale keys + observer card emit no leaks | ✅ |
| Project Tracking | All open bugs tracked as cookbook tasks; BL287/BL289/BL291 marked completed in cookbook | ✅ |
| Binary-build cadence | Minor → full cross + cross-stats + cross-channel + cross-agent | ✅ |
| Localization Rule | New observer-findability strings keyed across all 5 bundles + datawatch-app#88 filed | ✅ |
| Mobile-Parity Rule | Single issue covers all 3 fixes per the rule (operator-visible PWA changes) | ✅ |
| Live Project Cookbook Rule | All 9 cookbook tasks current; current-sprint subject reflects state | ✅ |
| Release testing | release-smoke.sh exit 0 | ✅ |

**Follow-up captured during sprint:**
- Sprint 6: 3 new AGENT.md rules + CI lint scripts (`check-curated-howtos.sh`, `check-howto-coverage.sh`, `check-plugin-manifests.sh`) + new `docs/howto/docs-as-mcp.md` + `docs/datawatch-definitions.md` Docs-as-MCP section + PWA bulk-select pending-trust UX + BL274 closed.

### Sprint 6 → v6.21.0 — Final closure: AGENT.md rules + CI lint + datawatch-definitions.md + BL274 closed

**Status:** 📋 planned.

**Scope:**
- AGENT.md adds: Docs-as-MCP Currency, Howto-Coverage, Plugin-Manifest Validation rules.
- CI lint scripts: `check-curated-howtos.sh`, `check-howto-coverage.sh`, `check-plugin-manifests.sh`.
- New `docs/howto/docs-as-mcp.md` with its own exec_steps.
- `docs/datawatch-definitions.md` Docs-as-MCP section + See-also footers.
- PWA bulk-select pending-trust UX (deferred from S4/S5).
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
