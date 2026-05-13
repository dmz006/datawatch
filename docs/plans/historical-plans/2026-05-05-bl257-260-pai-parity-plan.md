# BL257–BL260 — PAI Parity Implementation Plan

**Date:** 2026-05-05
**Source:**
- `docs/plans/2026-05-02-pai-comparison-analysis.md` — feature catalog + High/Medium/Low classification
- `docs/plans/2026-05-02-unified-ai-platform-design.md` — original architecture intent (Week-4 Identity, Week-6/7 Evals, etc.)
- [danielmiessler/Personal_AI_Infrastructure](https://github.com/danielmiessler/Personal_AI_Infrastructure) — upstream PAI for design reference + already-shipped Skill Registry default
**Priority:** mixed (BL257 High, BL258 High, BL259 Medium, BL260 Medium)
**Effort:** ~6–8 release cycles total (each BL is its own minor release; phases below are intra-BL)

---

## Why this plan exists

These four PAI features were originally claimed as `(target)` versions in `docs/plan-attribution.md` but never shipped — they were sub-features of the BL221 mega-bucket that got silently dropped at the v6.2.0 closure. The operator caught the gap on 2026-05-05 with the question "I don't see the identity robot interview, what else was missed in setting up PAI features?" and asked for them to be tracked, with implementation plan and app-parity issues. See memory `feedback_no_mega_bucket_close.md` for the umbrella-BL audit rule that was added to prevent this recurring.

Sequencing rationale (operator-validated 2026-05-05):

1. **BL257 (Identity / Telos)** first — it produces the context that BL258 Algorithm Mode's Decide phase and BL260 Council's persona prompts both consume. Builds the foundation.
2. **BL258 (Algorithm Mode)** second — extends the existing partial Guided Mode (PRD-only, 5-phase) into a generic per-session 7-phase harness. Already has scaffolding to extend.
3. **BL259 (Evals)** third — replaces the existing binary verifier with a rubric-based scorer; Algorithm Mode's Measure phase consumes eval results, so order matters but BL258 can ship without Evals if needed.
4. **BL260 (Council)** fourth, can run parallel with BL259 — orthogonal to Evals/Algorithm; only needs the orchestrator and per-persona session-spawn primitives that already exist. Filed last to give the operator a chance to refine persona definitions in actual use before locking the API.

Out-of-scope (frozen / deferred):
- **BeCreative** (PAI §5) — flagged "Low value" in the analysis; not retro-filed as BL. Will reconsider if a concrete need surfaces.
- **Daemon / public profile** (PAI §7) — operator-frozen 2026-05-05; out of scope for datawatch (technical tool, not personal-brand site).

---

## BL257 — Identity / Telos layer + interview-style init

**Estimated:** 1 minor release (~v6.8.0). Two phases.

### Phase 1 — Identity layer + 7-surface CRUD (target v6.8.0)

| Layer | Work |
|---|---|
| **Schema** | New `internal/identity/identity.go` with `Identity` struct (`role`, `north_star_goals []string`, `current_projects []string`, `values []string`, `current_focus string`, `context_notes string`, `updated_at time.Time`). Loaded from `~/.datawatch/identity.yaml`; hot-reload via fsnotify. |
| **Wake-up wiring** | Extend `internal/memory/wakeup.go` (or equivalent L0 generator) to inject identity sections into the L0 system-prompt scaffold. Snapshot at session-spawn time so changes don't perturb live sessions. |
| **REST** | `GET /api/identity` (read), `PUT /api/identity` (write whole doc), `PATCH /api/identity` (merge fields). Bearer-authenticated; audit-logged. |
| **MCP** | `get_identity`, `set_identity` (full replace), `update_identity` (merge). 3 tools. |
| **CLI** | `datawatch identity get [--field <k>]`, `datawatch identity set --field <k> --value <v>`, `datawatch identity edit` (opens in $EDITOR). |
| **Comm** | `identity get [field]`, `identity set <field> <value>`, `identity show`. Read-only by default per Comm-Channel-Read-Mostly Rule. |
| **PWA** | New card in **Settings → Agents → Identity** (just below Container Workers). Edit form with field-by-field inputs; Save / Reset. |
| **Locale** | `identity_*` keys in all 5 bundles (`identity_section_title`, `identity_field_role`, `identity_field_goals`, `identity_field_values`, `identity_field_focus`, `identity_field_notes`, `identity_btn_save`, `identity_btn_reset`, `identity_saved_toast`, `identity_loading`, `identity_load_error`, ~12 keys). |
| **YAML** | `identity:` block top-level in `datawatch.yaml` (optional; sidecar file is canonical); seed-from-config at first start. |
| **Audit** | `action=identity_set` / `identity_update` audit events. |
| **Smoke** | Add identity round-trip to `scripts/release-smoke.sh`. |
| **Tests** | Unit: identity loader + merger + wake-up injection. Integration: REST round-trip. |

### Phase 2 — Interview automaton (target v6.8.1 minor; or fold into v6.8.0 if scope allows)

| Layer | Work |
|---|---|
| **Skill manifest extension** | Extend the BL255 Skill manifest parser with a new top-level `type: interview` mode. New manifest fields: `phases:` (list of `{name, prompt, output_field}`), `output_file:` (relative path, defaults to `~/.datawatch/identity.yaml`), `update_mode: merge|replace`. Backward-compatible (Skills-Awareness Rule — unknown types ignored). |
| **Built-in `interview-identity` skill** | Ships with the PAI default registry (or as a built-in datawatch-provided skill if PAI doesn't have one — need to verify which). Phases: `role` → `north_star_goals` → `values` → `current_focus` → `context_notes`. Each phase prompts the LLM, accepts operator answer, parses + merges into the target file. |
| **PWA robot icon** | New header-icon entry point (robot emoji or icon) in the PWA top bar. Click → opens "Identity Setup" modal → looks up `interview-identity` skill in registry → spawns it as a `personal` automaton → user steps through phases inside the chat panel. |
| **CLI / comm / MCP** | `datawatch identity configure` (CLI), `identity configure` (comm), `identity_configure` (MCP) — all dispatch to the same automaton spawn so there's one canonical path. |
| **Mobile parity** | New robot-icon nav entry in the Compose Multiplatform app + interview UI mirror. |
| **Tests** | Unit: interview manifest parser, phase-state machine, merge semantics. |

**Risks / open questions:**
- Does the PAI registry already include an interview skill we can adopt, or do we ship our own? Verify during Phase-2 design.
- L0 budget: identity sections + existing top-learnings can blow past current ~600-token L0 cap. Likely need a token budget knob per section. Decide during Phase-1 design.

---

## BL258 — Algorithm Mode (general 7-phase per-session harness)

**Estimated:** 1 minor release (~v6.9.0). One phase.

| Layer | Work |
|---|---|
| **Session option** | Add `algorithm_mode bool` field on `Session` struct + `SessionRequest`. Mutually exclusive with PRD's existing `GuidedMode` (Algorithm is its strict superset). |
| **Phase state machine** | New `internal/sessions/algorithm.go` with `AlgorithmPhase` enum (`Observe`, `Orient`, `Decide`, `Act`, `Measure`, `Learn`, `Improve`). `currentPhase` field + transitions. Bridge detects phase boundary markers in LLM output (configurable regex per phase, defaults shipped). |
| **Gate behavior** | At each phase boundary, transition session to `WaitingInput`. Operator can: confirm (advance), edit (replace phase output), abort. Persisted alongside session state. |
| **Identity context injection** | If BL257 shipped, Decide phase's prompt includes `current_focus` + `north_star_goals` from identity. |
| **REST** | `POST /api/sessions {algorithm_mode:true}`. New `GET /api/sessions/{id}/algorithm-state` returning current phase + phase-output history. |
| **MCP** | `session_create algorithm_mode`, `session_algorithm_state`, `session_algorithm_advance`. |
| **CLI** | `datawatch session new --algorithm`. `datawatch session algorithm-state <id>`. `datawatch session algorithm-advance <id>`. |
| **Comm** | `session new ... algorithm`, `session algorithm-state <id>`. |
| **PWA** | (a) New-session form: "Algorithm mode" checkbox. (b) Active-session view: phase indicator strip across top showing all 7 phases with current highlighted. (c) Per-phase output panel + advance/edit buttons at gates. |
| **Locale** | `algorithm_*` keys (~15 across the 7 phase names + UI strings). |
| **Tests** | Unit: phase detector, state transitions. Integration: gate round-trip via REST. |
| **Mobile parity** | Phase strip + algorithm-mode toggle. |
| **Smoke** | Add algorithm round-trip to release smoke. |

**Risks:**
- Phase-boundary detection: hand-built regex is brittle. Alternative: structured LLM output (JSON envelope per phase). Decide during design — recommend JSON envelope (matches BL221 scan framework precedent).

---

## BL259 — Evals Framework

**Estimated:** 1 minor release (~v6.10.0). Two phases.

### Phase 1 — Grader infrastructure + 4 grader types (target v6.10.0)

| Layer | Work |
|---|---|
| **Package** | New `internal/evals/` with `Grader` interface (`Grade(ctx, input, expected, actual) (Result, error)`) and `Result` struct (`pass bool`, `score float`, `feedback string`). |
| **Graders** | 4 implementations: `string_match` (exact / contains / case-insensitive options), `regex_match`, `binary_test` (run cmd, exit code = pass), `llm_rubric` (uses configured LLM with a rubric prompt → returns score + feedback). |
| **Suite definition** | YAML at `~/.datawatch/evals/<suite>.yaml` with `mode: capability|regression`, `pass_threshold` (0.0–1.0), `cases: []`. Each case: `name`, `input`, `expected`, `grader: {type, opts}`. |
| **Runner** | `evals.Runner` loads suite, iterates cases, aggregates results → `RunResult` with `pass_rate`, per-case detail. JSON-on-disk run history in `~/.datawatch/evals/runs/`. |
| **REST** | `POST /api/evals/run {suite}`, `GET /api/evals/suites`, `GET /api/evals/runs?suite=`, `GET /api/evals/runs/{id}`. |
| **MCP** | `eval_run`, `eval_list_suites`, `eval_get_results`, `eval_get_run`. |
| **CLI** | `datawatch evals run <suite>`, `datawatch evals list`, `datawatch evals results <suite> [--limit N]`, `datawatch evals run-detail <run-id>`. |
| **Comm** | `evals run <suite>`, `evals list`, `evals results <suite>`. |
| **PWA** | Settings → Agents → Evals card: suite list, "Run" button per suite, results table (pass/fail/score), drill-down on per-case detail. |
| **Locale** | `evals_*` keys (~20). |
| **Tests** | Unit per grader; integration on a sample suite. |
| **Smoke** | Add a 1-case canary suite to release smoke. |
| **Mobile parity** | Evals card + run-trigger + results view. |

### Phase 2 — Migrate BL221 scan framework to use Evals (target v6.10.1)

| Layer | Work |
|---|---|
| Replace ad-hoc scan handlers (rules check, security scan) with `llm_rubric` + `binary_test` grader invocations. |
| Algorithm Mode's Measure phase (BL258) auto-runs eval suite tied to session type if configured. |
| Remove old binary `verifier` shim once parity is confirmed. |

**Risks:**
- LLM-rubric grader needs to be deterministic enough for regression mode (~99% pass target). Consider temperature=0 + structured-output JSON for grading prompts.

---

## BL260 — Council Mode (multi-agent debate)

**Estimated:** 1 minor release (~v6.11.0). One phase.

| Layer | Work |
|---|---|
| **Personas** | Built-in default 6: `security-skeptic`, `ux-advocate`, `perf-hawk`, `simplicity-advocate`, `ops-realist`, `contrarian`. Each persona = `{name, role, system_prompt, llm_overrides}`. Operator-extensible via `~/.datawatch/council/personas/<name>.yaml`. |
| **Orchestrator** | `internal/orchestrator/council.go` — `CouncilOrchestrator` spawns N parallel reviewer sessions (one per selected persona) with the proposal as input. Each writes a structured response. After round complete, optional next round shows previous round's summaries (`debate` mode = 3 rounds; `quick` = 1). |
| **Synthesizer** | After final round, spawns a `synthesizer` session that ingests all persona outputs + produces consensus recommendation with dissent notes. |
| **PRD pre-decompose hook** | New PRD flag `pre_decompose_council: bool` + `council_personas []string`. When true, runs council on the proposed approach before decomposition; consensus + dissent fed into decomposer system prompt. |
| **REST** | `POST /api/council/run {proposal, personas, mode, rounds}`, `GET /api/council/personas`, `GET /api/council/runs/{id}`. |
| **MCP** | `council_run`, `council_personas_list`, `council_get_run`. |
| **CLI** | `datawatch council run --proposal <file> --personas <list> --mode debate`, `datawatch council personas list`, `datawatch council run-detail <id>`. |
| **Comm** | `council run`, `council personas`. |
| **PWA** | Settings → Agents → Council card: persona list (checkbox to enable per-run), ad-hoc "Run Council" form (proposal textarea + personas + mode), past-runs table with transcript drill-down. |
| **Locale** | `council_*` keys (~25). |
| **Tests** | Unit: persona loader, round state machine, synthesizer prompt builder. Integration: 2-persona quick-mode run end-to-end. |
| **Smoke** | Add a 2-persona quick-mode round-trip to smoke. |
| **Mobile parity** | Council card + run-trigger + transcript view. |

**Risks:**
- Cost: 6-persona × 3-round debate = 18 + 1 synthesizer LLM calls per run. Add per-run cost estimator + confirmation UI before kicking off.
- Identity (BL257) feeds persona prompts? Recommended: yes — operator's `current_focus` and `values` injected into each persona's system prompt so the debate stays anchored to operator priorities.

---

## Cross-cutting requirements (apply to all four)

- **7-surface parity rule** — every BL must cover REST + MCP + CLI + comm + PWA + locale + YAML before its release tag.
- **Per-release functional smoke** — each release adds a smoke case for the new feature; existing 95-case suite must continue to pass.
- **Mobile-Parity Rule** — file a `dmz006/datawatch-app` issue per BL at release-cut time covering layout / behavior / API contract / new affordances. Filed at plan-acceptance time too (see "App parity issues" below).
- **Localization Rule** — every new user-facing string keyed in all 5 locale bundles + `data-i18n` wiring + app issue.
- **Plan attribution** — `docs/plan-attribution.md` gets a "Shipped:" timestamp + version per BL when each closes; no more `(target)` markers without backlog tracking.
- **Sub-feature audit at closure** — when each of these BLs closes, the closing release notes enumerate every sub-feature against the acceptance criteria above; if any are dropped, file the next BL.

---

## App parity issues filed at plan-acceptance

Mobile-Parity Rule covers any operator-visible PWA change. These four BLs introduce new PWA cards / entry points / forms — each gets an upstream issue at plan-filing time so the mobile pipeline can scope work alongside daemon work:

- **[datawatch-app#53](https://github.com/dmz006/datawatch-app/issues/53) (BL257)** — Settings → Agents → Identity card + interview robot-icon nav entry + interview-modal flow.
- **[datawatch-app#54](https://github.com/dmz006/datawatch-app/issues/54) (BL258)** — New-session "Algorithm mode" toggle + active-session 7-phase strip + per-phase output / advance / edit UI.
- **[datawatch-app#55](https://github.com/dmz006/datawatch-app/issues/55) (BL259)** — Settings → Agents → Evals card + suite list / run / results UI.
- **[datawatch-app#56](https://github.com/dmz006/datawatch-app/issues/56) (BL260)** — Settings → Agents → Council card + persona list + run form + transcript view.
- **[datawatch-app#57](https://github.com/dmz006/datawatch-app/issues/57) (BL261)** — Settings → Automata tab card padding fix (Pipeline / Orchestrator / Skills).

Issues filed at plan-commit time (2026-05-05).

---

## Sprint sequencing

| Release | BL | Headline | Notes |
|---|---|---|---|
| v6.7.7 (patch) | BL261 | Settings → Automata tab card padding | v6.7.6 follow-up bug; ship before BL257 work begins |
| v6.8.0 (minor) | BL257 Phase 1 | Identity layer + 7-surface CRUD | Foundation for BL258 Decide phase + BL260 persona prompts |
| v6.8.1 (minor or patch) | BL257 Phase 2 | Interview automaton + robot-icon entry | Closes BL257 |
| v6.9.0 (minor) | BL258 | Algorithm Mode (general 7-phase) | Strict superset of partial Guided Mode |
| v6.10.0 (minor) | BL259 Phase 1 | Evals framework + 4 graders + UI | Replaces binary verifier |
| v6.10.1 (patch or minor) | BL259 Phase 2 | Migrate BL221 scan framework | Removes old verifier shim |
| v6.11.0 (minor) | BL260 | Council Mode | Last; can run parallel with BL259 if reviewer bandwidth |

Rule reminders: every minor release is host-arch + cross binaries per release workflow; every release runs `scripts/release-smoke.sh` and passes before tag; per-release GH-runner status check; per-release secrets-store-rule audit for any new credential surface.
