# Harness Implementation — Roadmap (Phases 0–4)

**Date:** 2026-09-16 · **Status:** Draft · **Companions:** `eval-sweep-technical-spec.md`, `eval-sweep-test-plan.md`, `eval-dag-spec.md`, `redteam-spec.md` (detailed source of truth for their respective phases)

**Sources:** `docs/plans/harness-research/enhancement-proposals.md` §79–84 (Sequencing, per `synthesis.md` recommendation), `docs/plans/harness-research/feature-themes.md` (theme numbering), story 0 of `.decompose-output.json` ("AGENT.md & DATAWATCH-CONTEXT.md parity and release-checklist audit") and its applied AGENT.md/DATAWATCH-CONTEXT.md/`docs/parity-status.md` changes.

This roadmap is the cross-phase ordering document. It is **not** a code plan — no `.go` files are created or modified by this document. Every detailed contract (contracts, file maps, test tables) lives in the owning spec doc named per phase below; this doc only orders the work, states the gates, and consolidates the parity-surface view.

---

## 1. Phase table

| Phase | Name | Owning spec doc (this dir) | Upstream dependency | Ordering rationale (per `enhancement-proposals.md:79-84`) |
|---|---|---|---|---|
| **0** | Parity-audit rule hardening (story 0) | AGENT.md / DATAWATCH-CONTEXT.md / `docs/parity-status.md` (story 0 deliverables — no `harness-impl` spec; the spec is the rule set itself) | — | Prerequisite, not a proposal: without the hardening, Phases 1–3 plans ship without a `Parity surface` section (the exact regression audit story 0 exists to prevent, cf. BL382/B88). |
| **1** | Eval Sweep core (Proposal 1) | `eval-sweep-technical-spec.md` (superseding contract; test contract in `eval-sweep-test-plan.md`) | Phase 0 | "Proposal 1 + 2 first — both reuse BL259/BL117 with minimal new machinery; together they close the largest gap (#1)." |
| **2** | Eval-DAG eval node kind (Proposal 2) | `eval-dag-spec.md` | Phase 0; Phase 1 (for sweep-param reuse, spec §6 Phase B) | Same "1 + 2 first" clause. Phase A is viable on `eval_run` alone; Phase B (`evals[].backends`/`models` on a gate) wires sweep P1. |
| **3** | Red-team validator pipeline (Proposal 5 / theme 4) | `redteam-spec.md` | Phase 0 + Phase 2 | "Proposal 5 second — small surface, closes the security gap (#4) with existing guardrail plumbing." Slotted after Phase 2: shared guardrail verdict stream and the new parity rule must already be in place. |
| **4** | Follow-ons (OUT OF SCOPE for this PRD) | none — deferred to future PRDs | — | Drift/dashboards (`feature-themes.md` theme 2), provenance/OTel spike (theme 3 = `enhancement-proposals.md` §3), grounding (theme 5 = `enhancement-proposals.md` §4). "Proposal 3 as a design spike… Proposal 4 last" per `synthesis.md` — neither is scoped into this PRD. |

---

## 2. Per-phase detail

### Phase 0 — Parity-audit rule hardening (story 0)

**Owning spec:** the story 0 deliverables themselves — the AGENT.md rule set (working-tree changes), DATAWATCH-CONTEXT.md "Parity enforcement" note, `docs/parity-status.md` plan/proposal parity gate. There is no `harness-impl/` spec file for this phase by design: it is a rules pass, and its exit criterion is a grep-able rule, not a test.

**Repo files touched** (story 0 file list):

| File | Change (per story 0) |
|---|---|
| `AGENT.md` | (a) `General documentation checklist` item 8 **Parity surface section** rule — every plan doc under `docs/plans/` MUST carry a `Parity surface` section enumerating the full set; (b) Configuration Accessibility Rule gains the Android/iPhone config-parity note (AGENT.md:407–413); (c) Mobile-Parity Rule gains the **Full parity-surface set** clause, AGENT.md:462–466 (`REST`, `MCP`, `CLI`, `comm channel`, `YAML/config`, `PWA`, `Android`, `iPhone/iOS`); (d) Federation-Parity Rule gains Per-parity-surface enumeration for `fedCap`-guarded endpoints (AGENT.md:556–562); (e) Decomposer Scope-Drift Rule gains the **Parity inheritance** clause — decomposed stories inherit the parent plan's `Parity surface` list (AGENT.md:592–599); (f) Release workflow checklist gains the **parity audit passed** item (AGENT.md:656) |
| `DATAWATCH-CONTEXT.md` | "Parity enforcement" note: any API-touching feature ships across all enumerated surfaces or carries an explicit reason-logged exclusion |
| `docs/parity-status.md` | "Plan/Proposal parity gate" section — the three planned harness features with target surfaces marked *planned* |
| `docs/plans/README.md` | Release-notes note pointing at the harness-impl planning docs (no invented version) |

**Entry criteria:** story 0 audit task complete (gap table of rule name / current surface list / missing surfaces / proposed text, keyed to AGENT.md section + line).

**Exit criteria (gate for all later phases):**

1. `grep -c "Parity surface" AGENT.md` returns ≥ 5 — the rule is in the checklist, the config rule, the Mobile-Parity set, the Federation rule, and the release checklist.
2. Every doc under `docs/plans/harness-impl/` contains a `## Parity surface` (or `### … Parity surface`) section with per-surface include/exclude-with-reason rows — the AGENT.md:656 release-checklist item ("parity audit passed") is manually verifiable as green.
3. Zero `.go` files modified (story 0 constraint).

---

### Phase 1 — Eval Sweep core (Proposal 1)

**Owning spec:** `eval-sweep-technical-spec.md` §7 (implementation sequencing). Test contract: `eval-sweep-test-plan.md` §3 (Smoke S3.1–S3.5).

**Repo files touched** (from `eval-sweep-technical-spec.md` §7 phase table, P0–P3; boot wiring per §7):

| Sweep phase | Files touched (actual paths per spec §7) |
|---|---|
| **P0 — token plumbing** | `internal/inference/dispatcher.go`; `internal/inference/adapters/*` (ollama / openwebui / opencode-api / claude; gemini-api when its adapter lands) |
| **P1 — sweep core** | `internal/evals/` (new `SweepRequest`/`RunSet`/`Cell`/`Delta`/`MatrixSpec` types; `ParentRunID` on `Run`; `SweepRunner`); `internal/server/evals.go` (`POST /api/evals/sweep`, `GET /api/evals/runsets`, `GET /api/evals/runsets/{id}`); `internal/mcp/evals.go` (`eval_sweep`, `eval_list_runsets`, `eval_get_runset`); `cmd/datawatch/` CLI + `internal/router/evals.go` comm verbs; `cmd/datawatch/main.go` boot wiring alongside `evals.NewRunner` |
| **P2 — cost + config** | `internal/config/` (new `evals` section: `max_parallel`, `max_cells`, `default_backends`, `cost_rates`, `rubric_backend`); `internal/inference/llm.go` (`eval_*` LLM-row fields); `internal/compute/node.go` (`Consumer:"eval"` / `"eval_rubric"` RBAC); `internal/evals/` (delta + best settlement) |
| **P3 — llm_rubric** | `internal/evals/` (un-stub `llm_rubric` via `Dispatcher.Call`); suite YAML fixtures under `~/.datawatch/evals/` (data, not repo code) |

**Entry criteria:**

- Phase 0 exit criteria green (this plan inherits the `Parity surface` section — see §4 below).
- BL259 `eval_run` landed (v6.10.x, in tree).
- Internal ordering is strictly P0 → P1 → P2 → P3: P1 needs P0 (tokens non-zero), P2 needs P1 (runner exists), P3 needs P2 (cost already wired) — per spec §7 "Gates the next" column.

**Exit criteria (gate for Phase 2's Phase B):**

- `eval-sweep-test-plan.md` §3 smoke, all five: **S3.1** single-backend sweep renders (CLI/PWA + REST `GET /api/evals/runsets/<id>` field-for-field identical), **S3.2** two-backend-with-one-unreachable settles within `ResolveTimeout` with `incomplete=true`, **S3.3** PWA/comm read after sweep, **S3.4** existing surfaces unregressed (byte-identical standalone `Run` JSON — additive-only gate, spec §9), **S3.5** 503 surface consistent.
- Additive-only gate re-asserted: pre-existing `internal/evals/evals_test.go` and `internal/session/bl6_cost_test.go` green unchanged (test-plan §5 gate rule).

---

### Phase 2 — Eval-DAG eval node kind (Proposal 2)

**Owning spec:** `eval-dag-spec.md` §6 (Sequencing & dependencies). Test contract: `eval-dag-spec.md` §5 (Test plan; §5.3 Smoke).

**Repo files touched** (from `eval-dag-spec.md` §6.2 phase → repo-file map):

| Dag phase | Files touched (actual paths per spec §6.2) |
|---|---|
| **A1 — node contract** | `internal/orchestrator/models.go` (`NodeKindEval` const; `EvalSuite`/`EvalBackends` on `Node`) |
| **A2 — dispatch + fn** | `internal/orchestrator/runner.go` (`EvalRequest`/`EvalFn`, `Runner.eval`, `runEval`); `cmd/datawatch/main.go` (call-site only); `internal/config/` (four `orchestrator.eval_*` keys) |
| **A3 — planner gates** | `internal/server/orchestrator.go` (handler bodies + request struct); `internal/server/web/openapi.yaml`; `internal/server/api.go` (config-patch cases) |
| **A4 — parity surfaces** | `internal/mcp/orchestrator.go`; `cmd/datawatch/cli_orchestrator.go`; `internal/router/bl220_comm_commands.go` + `internal/router/bl220_comm_commands_test.go` |
| **A5 — tests** | `internal/orchestrator/orchestrator_test.go`; `internal/server/orchestrator_enrich_test.go`-style handler tests |
| **B1 — sweep wiring** *(gated on Phase 1 P1)* | `internal/orchestrator/runner.go` (sweep path behind `EvalBackends`); `internal/server/evals.go` (`Sweep` interface extension per `eval-sweep-technical-spec.md` §3.6) |
| **C1 — optional polish** | `internal/server/web/app.js`, locale bundles — **only if** a distinct eval badge ships; otherwise closed with no client change |

`store.go`, `api.go`, and the existing `Run`/`topoSort`/`depBlocked`/`saveNode` spans in `runner.go` are **not in any phase's change set** — they compile untouched (spec §6.2 acceptance proof of E4/E6).

**Entry criteria:**

- Phase 0 exit green (Parity inheritance rule applies to this plan — `eval-dag-spec.md` §4.7 already carries the section; the release-checklist item at AGENT.md:656 is its gate).
- Phase A (single-backend gate): no Phase 1 dependency — `EvalBackends` empty → `eval_run` semantics only.
- Phase B (sweep-backed gate): Phase 1 P1 landed (`SweepRunner` + `POST /api/evals/sweep` + `Sweep` method). Until then, non-empty `evals[].backends` is a planner `400` ("sweep not available on this daemon (evals P1 not landed)") — spec §6.3.

**Exit criteria:**

- `eval-dag-spec.md` §5.3 Smoke pass: one-PRD graph with a single eval node → create → `graph-run` → poll `graph-get` → the verdict object appears **identically placed to a guardrail verdict** in CLI `datawatch orchestrator verdicts`, MCP `orchestrator_verdicts`, comm `orchestrator verdicts`, and the PWA orchestrator card; graph settles `blocked`/`completed`.
- §5.2 item 4 REST/MCP/CLI smoke green.
- Whole-spec acceptance (eval-dag-spec.md §5.3): all five existing `orchestrator_test.go` tests green unmodified; §5.1 table green; one integration order test green; smoke pass.

---

### Phase 3 — Red-team validator pipeline (Proposal 5 / theme 4)

**Owning spec:** `redteam-spec.md` §5 (Sequencing & dependencies). Test contract: `redteam-spec.md` §4 (Test plan; §4.3 Smoke S1–S5).

**Repo files touched** (from `redteam-spec.md` §5 phase tables):

| Pipeline phase | Files touched (actual paths per spec §5) |
|---|---|
| **Phase 0 — classifier contract + config** | `internal/config/config.go`; `internal/autonomous/manager.go`; `internal/autonomous/pipeline.go` (**new file** — stage/verdict types); `internal/server/api.go`; `internal/server/autonomous.go`; `internal/mcp/autonomous.go`; `cmd/datawatch/cli_autonomous.go` |
| **Phase 1 — stage dispatch + verdict join** | `internal/autonomous/pipeline.go`; `internal/autonomous/manager.go`; `internal/autonomous/guardrail_registry.go`; `internal/autonomous/models.go`; `internal/orchestrator/runner.go` (additive `GuardrailRequest` fields only); `internal/server/autonomous.go` |
| **Phase 2 — block mode + audit gate** | `internal/autonomous/manager.go`; `internal/autonomous/pipeline.go`; `internal/mcp/autonomous.go`; `internal/server/hook_events.go`; `internal/server/autonomous.go`; `internal/router/guardrails.go`; `internal/router/router.go` |
| **Phase 3 — auto fix-sub-PRD loop** | `internal/autonomous/manager.go`; `internal/server/autonomous.go`; `internal/mcp/bl221_scan.go`; `internal/router/sx2_parity.go`; `cmd/datawatch/cli_autonomous.go` |

**Entry criteria:**

- Phase 0 (rule hardening) green — this spec's own `## Parity surface` section (redteam-spec.md §372+) is its compliance artifact, and block-mode config writes ride the same `self_config_audit` sink the hardened rules enumerate.
- Phase 2 (Eval-DAG) landed — per the roadmap ordering `eval-dag + parity-audit -> redteam`: the pipeline's verdicts join the *same* guardrail verdict stream (`models.Verdict` / `orchestrator_verdicts`) and `per_automaton_guardrails` override precedence that the eval-node consumes (redteam-spec.md §5 "Shared surface"). The spec notes either order is build-order-safe, but *this roadmap* slots it after Phase 2 per the proposal sequencing.
- Backward-compat precondition: a daemon with `injection_guard: true, block_on_injection: false` behaves byte-identically post-upgrade (redteam-spec.md §1.1 / §4.3 S1).

**Exit criteria:**

- `redteam-spec.md` §4.3 Smoke S1–S5 all pass: S1 default-behaviour unchanged, S2 block-mode unchanged, S3 no-op unchanged, S4 counter increments (`datawatch_injection_guard_hits_total`), S5 classifier-disabled → zero LLM calls with regex-stage-only verdict row.
- Per spec §5 Phase 1 goal: both stages default to `warn`; Phase 2 goal: `mcp.allow_self_config` gate enforced on block-mode writes; audit sink fires on every block decision and every override-approval.

---

### Phase 4 — Follow-ons (explicitly OUT OF SCOPE for this PRD)

None of Phase 4 is a phase in this PRD. It is recorded here so the roadmap is complete against `enhancement-proposals.md` and no one "borrows" it into scope later:

| Follow-on | Source | Why deferred (cite the rationale) |
|---|---|---|
| **Drift detection + AI feedback loops** (trend dashboards, regression alerts on routine sweeps) | `feature-themes.md` theme 2 | "mostly read-side trend queries over already-persisted runs once theme 1 lands" — depends on Phase 1's `RunSet` history and Phase 3's fix-sub-PRD loop being the feedback shape to generalize; no sequencing slot in `enhancement-proposals.md` (§79–84) |
| **Provenance tracing + OpenInference/OTel interop** | `feature-themes.md` theme 3 = `enhancement-proposals.md` §3 (Proposal 3) | "Proposal 3 as a design spike — scope the span model before any export work (synthesis recommendation)" — `synthesis.md` Recommendation: "then #2 as a design spike, scoping an OpenInference-shaped span model over existing session/telemetry data rather than re-collecting." Not a build phase; a *spike* |
| **RAG / grounding metrics for docs_search / docs_read** | `feature-themes.md` theme 5 = `enhancement-proposals.md` §4 (Proposal 4) | "Proposal 4 last — depends on the embedder being stable and the BL274 plan-then-execute gate (Sprint 3) landing" — neither precondition is met at roadmap-writing time |

(For reference: `feature-themes.md` theme 6 — quality- and cost-aware routing — composes Phase 1/2 measurement output and is likewise a future-PRD candidate, not a phase here.)

---

## 3. Dependency DAG

```
                       ┌────────────────────────┐
                       │  Phase 0              │
                       │  parity-audit rule    │
                       │  hardening (story 0)  │
                       └───────────┬────────────┘
        ┌──────────────────────────┼──────────────────────────┐
        │ Parity-surface section   │ same                     │ same
        │ + Parity inheritance     │ + new parity rule        │
        ▼                          ▼                          ▼
┌───────────────────┐   ┌────────────────────────┐   ┌─────────────────────────┐
│  Phase 1          │   │  (rule applies to      │   │  (rule applies to       │
│  Eval Sweep core  │   │   Phases 2 & 3 plans   │   │   Phases 2 & 3 plans)   │
│  P0→P1→P2→P3      │   │                        │   │                         │
└────────┬──────────┘   └────────────────────────┘   └─────────────────────────┘
         │ SweepRunner + /api/evals/sweep + Sweep interface          │
         │ (eval-dag-spec §6.3: Phase B gated on sweep P1)           │
         ▼                                                           │
┌─────────────────────────────────┐                                  │
│  Phase 2  Eval-DAG eval node    │──────────────────────────────────┘
│  A1→A2→A3→A4→A5  +  B1(sweep)  │  shared guardrail verdict stream
└────────────────┬────────────────┘  (models.Verdict / orchestrator_verdicts)
                 │                   + per_automaton_guardrails precedence
                 ▼                   (redteam-spec.md §5 "Shared surface")
┌─────────────────────────────────┐
│  Phase 3  Red-team pipeline     │
│  P0→P1→P2→P3 (in spec §5)      │
└────────────────┬────────────────┘
                 │
                 ▼
        Phase 4 — follow-ons (OUT OF SCOPE: theme 2 / theme 3 spike /
        theme 5 — see §1 row and §5 below)
```

**Edge justifications (per the specs):**

- `parity-audit -> all phases`: story 0 installs the AGENT.md:656 release-checklist item and the Parity inheritance clause; every subsequent plan *inherits* the `Parity surface` section (eval-dag-spec.md §4.7 and redteam-spec.md §372+ are written to that rule).
- `sweep-core (Phase 1 P1) -> eval-dag (Phase 2 B1)`: `eval-dag-spec.md` §6.3 — "Phase B is gated on sweep P1 (`eval-sweep-technical-spec.md` §7: P1 lands `SweepRunner` + `POST /api/evals/sweep` + the `evalsRunner.Sweep` interface)."
- `eval-dag + parity-audit -> redteam`: `redteam-spec.md` §5 — stages join `orchestrator_verdicts` + `per_automaton_guardrails` (the stream the eval-node lands in first); the parity rule governs the pipeline's new config keys and block-mode `self_config_audit` entries. The spec itself notes either order is build-safe ("there is no build-order constraint"); the *ordering* here follows `enhancement-proposals.md:79-84` ("1 + 2 first… 5 second").

---

## 4. Parity surface — master table

Per the **Mobile-Parity Rule** (AGENT.md:455: "**Parity standard: PWA == Android == iOS.**") and its **Full parity-surface set** clause (AGENT.md:462–466): `REST`, `MCP`, `CLI`, `comm channel`, `YAML/config`, `PWA`, `Android`, `iPhone/iOS` — "capability parity is required; implementation parity is not — each platform uses idiomatic delivery." Per-surface detail is normative in each owning spec (`eval-sweep-test-plan.md` §4, `eval-dag-spec.md` §4.7, `redteam-spec.md` §372+); this table consolidates and flags status. **All three rows are PLANNED at document-writing time** — no `.go` file has been created or modified for any of these features.

| Surface | Proposal 1 — Eval Sweep | Proposal 2 — Eval-DAG nodes | Proposal 5 — Red-team pipeline |
|---|---|---|---|
| **REST endpoints** | `POST /api/evals/sweep`, `GET /api/evals/runsets`, `GET /api/evals/runsets/{id}` (planned; spec `eval-sweep-technical-spec.md` §5.1) | `evals` field additively added to `POST /api/orchestrator/graphs` + `.../plan` (planned; OpenAPI `internal/server/web/openapi.yaml`); verdicts routes unchanged (`eval-dag-spec.md` §4.7) | `GET/PUT /api/config` (new keys), `GET/PUT /api/autonomous/config`, + new: `GET /api/autonomous/pipeline/status`, `POST /api/autonomous/pipeline/run`, `POST /api/autonomous/prds/<id>/guardrail/injection-pipeline/approve` (Phase 2), `POST /api/autonomous/prds/<id>/injection/fix` (Phase 3) (planned; `redteam-spec.md` §5 + §372+) |
| **MCP tools** | `eval_run` gains `backends`/`models`; + `eval_list_runsets`, `eval_get_runset` (planned) | `orchestrator_graph_create`/`orchestrator_graph_plan` gain `evals` string param; `orchestrator_verdicts` description update; no new tools (planned; `eval-dag-spec.md` §4.7) | `autonomous_config_get`/`_set` new params; `per_automaton_guardrails_set` accepts `injection-pipeline`; + `autonomous_prd_injection_fix` (planned; `redteam-spec.md` §5 Phase 3) |
| **CLI verbs** | `evals sweep`, `evals runsets`, `evals get-runset` (planned) | optional 4th arg on `graph-create`, optional 2nd arg on `graph-plan`; `graph-run`/`verdicts` unchanged (planned; `eval-dag-spec.md` §4.5) | `autonomous config-get`/`config-set` (new keys); + `autonomous prd-injection-fix <id>` (planned; `redteam-spec.md` §5 Phase 3) |
| **Comm triggers** | `evals sweep [backends]`, `evals runsets`, `evals get-runset` (planned) | `create`/`plan` gain `evals_json`; `run`/`get`/`verdicts` unchanged (planned; `eval-dag-spec.md` §4.6) | `autonomous config-set k=v,…`; + `injection-pipeline status`, `injection-pipeline run <prd> <stage>`, `injection-pipeline approve <prd-id>` (Phase 2), `autonomous injection-fix <id>` (Phase 3) (planned) |
| **Config keys** | new `evals` section: `max_parallel`, `max_cells`, `default_backends`, `cost_rates`, `rubric_backend` + per-LLM `eval_*` fields (planned; spec §4.7) | `orchestrator.eval_enabled`, `orchestrator.eval_block_on_fail`, `orchestrator.eval_timeout_ms`, `orchestrator.eval_backend` (planned; `eval-dag-spec.md` §4.7) | `autonomous.classifier_enabled`, `classifier_confidence_threshold`, `classifier_block_confidence`, `stage_actions.regex`, `stage_actions.llm`, `classifier_timeout_ms`, `classifier_backend` (+ existing `injection_guard`, `block_on_injection`) (planned; `redteam-spec.md` §5 Phase 0) |
| **PWA views** | RunSet list + detail render (smoke S3.1/S3.3); launch parity required where single-run launch exists today (planned; test-plan §4) | **no change required (capability parity)** — generic node/verdict render; optional node-kind chip polish via datawatch-app issue only (planned/conditional; `eval-dag-spec.md` §4.7) | Settings → Automata tab: toggles + selectors alongside existing `injection_guard`/`block_on_injection` (app.js:11871–11872); 5 locale bundles (planned; `redteam-spec.md` §372+) |
| **Android surface** | ⏸ pending — verify comm-client reply rendering; add one smoke line or record exclusion reason before GA (planned; Mobile-Parity Rule, AGENT.md:455–472) | **excluded in v1** — full REST reachable in-app; no new screens; tracked via the PWA-polish datawatch-app issue (reason logged per `eval-dag-spec.md` §4.7) | required (parity target) — datawatch-app issue per Mobile-Parity Rule (AGENT.md:455) (planned; `redteam-spec.md` §372+) |
| **iPhone/iOS surface** | ⏸ pending — same check as Android (planned) | **excluded in v1** — same reason as Android (reason logged per `eval-dag-spec.md` §4.7) | required (parity target) — datawatch-app issue per Mobile-Parity Rule (AGENT.md:455–460) (planned; `redteam-spec.md` §372+) |

**Status legend:** *planned* = specified and sequenced, zero code landed at this writing; *pending* = surface-verification step before GA; *excluded* = reason logged in the owning spec per AGENT.md:151 (a plan that excludes a surface must carry a reason).

---

## 5. What is explicitly out of scope

From `enhancement-proposals.md` §39–63 (Proposal 3 "Unified lineage query + OpenInference span export" and Proposal 4 "Grounding metrics for docs_search / docs_read"), deferred with the `synthesis.md` rationale:

| Deferred item | Proposal / theme | Synthesis rationale (verbatim where quoted) | Unblocking precondition |
|---|---|---|---|
| **Provenance tracing + OpenInference/OTel interop** | Proposal 3 (`enhancement-proposals.md` §37–49) = `feature-themes.md` theme 3 | `synthesis.md` Recommendation: "Adopt #1 (eval orchestration) first — it reuses BL259 and BL117 machinery almost entirely — **then #2 as a design spike, scoping an OpenInference-shaped span model over existing session/telemetry data rather than re-collecting**." `enhancement-proposals.md:82`: "Proposal 3 as a design spike — scope the span model before any export work." | A span-model design spike over existing memory breadcrumbs (BL347/BL351 lineage, per-task telemetry, memory WAL); read-side assembly first, exporter second. |
| **RAG / grounding metrics for docs_search / docs_read** | Proposal 4 (`enhancement-proposals.md` §51–63) = `feature-themes.md` theme 5 | `enhancement-proposals.md:83`: "Proposal 4 last — **depends on the embedder being stable and the BL274 plan-then-execute gate (Sprint 3) landing**." `synthesis.md` §2.3: "RAG/grounding metrics… docs_search (BL274) has no measured grounding score for cited excerpts" (gap identified; sequencing left to Proposal 4-last). | Embedder stability; BL274 plan-then-execute gate (Sprint 3) shipped. |
| Drift detection + AI feedback loops | `feature-themes.md` theme 2 (no enhancement-proposals.md proposal number — it is the generalization of Phases 1–3 outcomes) | `synthesis.md` §1.2: "The PRD-scan → fix-sub-PRD → AGENT.md rule-proposal loop (SAST/secrets/deps) is the exact feedback-loop shape to generalize: eval-failure → auto-fix task → rule/prompt correction, all reusing the guardrail verdict stream and autonomous loop." | Phases 1–3 landed (persisted `RunSet` history + pipeline fix-loop as the seed). |
| Quality- and cost-aware routing | `feature-themes.md` theme 6 | `synthesis.md` §1.6 (per `feature-themes.md`): "composes the measurement layer from themes 1–2; no new machinery." | Phases 1–2 measurement output (suite × backend runs + cost). |

Nothing in this section is scope-creep-able into Phases 0–3: each has an unmet precondition that holds at roadmap-writing time.

---

## 6. Constraints honored (this document)

- **Markdown only; zero `.go` files created or modified** by this roadmap (and by story 0, Phase 0's constraint of record).
- **Every spec filename cited above exists under `docs/plans/harness-impl/`** — verified by `ls` at doc-writing time: `eval-backend-integration.md`, `eval-dag-spec.md`, `eval-sweep-api.md`, `eval-sweep-outline.md`, `eval-sweep-spec.md`, `eval-sweep-technical-spec.md`, `eval-sweep-test-plan.md`, `redteam-spec.md`. (Companions `eval-sweep-api.md` / `eval-sweep-outline.md` / `eval-backend-integration.md` / `eval-sweep-spec.md` are superseded where they conflict, per `eval-sweep-technical-spec.md` §0 and the final line of that spec — they are cited here for completeness, not as gate sources.)
- **Self-contained**: this file + the four owning docs + the two research sources (`enhancement-proposals.md`, `synthesis.md`/`feature-themes.md`) are the complete read set; no other `harness-impl` doc is a prerequisite.
- **Source-of-truth rule**: where this roadmap summarizes and a spec doc is more detailed, the spec wins — amend this roadmap, do not the reverse (mirrors the test-plan §0 rule and the eval-dag/spec §6.3 rule).

*End of roadmap.*
