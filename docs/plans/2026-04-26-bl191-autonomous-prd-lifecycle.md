# BL191 — Autonomous PRD lifecycle: design questions

**Status:** Operator-driven — questions ready for design conversation. **Do not implement until aligned.**
**Filed:** 2026-04-25
**Pre-requisite reads:**
- [`docs/api/autonomous.md`](../api/autonomous.md) — the current operator surface
- [`docs/howto/autonomous-planning.md`](../howto/autonomous-planning.md) — current end-to-end walkthrough
- [`docs/plans/2026-04-20-bl24-autonomous-decomposition.md`](2026-04-20-bl24-autonomous-decomposition.md) — original BL24+BL25 design
- BL197 (closed v4.9.2) — chat-channel parity audit; PWA UI portion folded into this doc

---

## Why this is a conversation, not a spec

The operator explicitly asked for joint design: *"do not decide on
your own; work with me for the full plan on how automation, auto
prd, recursive building and how this all comes together."* So the
contents below are **questions + options + my recommendation per
question**, not a fait-accompli plan. Each section is sized to be
read out loud during the call.

---

## Today's pipeline (verified inventory)

```
operator
   │
   ▼
 POST /api/autonomous/prds {spec}        ← 9 REST endpoints
   │
   ▼
 store: <data_dir>/autonomous/prds.json  ← flat file via Manager.Store
   │
   ▼
 POST /api/autonomous/prds/{id}/decompose
   │
   ▼
 DecomposeFn  (cmd/datawatch/main.go:2006)
   ├── REST loopback to /api/ask
   ├── backend = autonomous.decomposition_backend  (falls back to session.llm_backend)
   └── LLM returns JSON → parsed into Story[].Tasks[]
   │
   ▼
 POST /api/autonomous/prds/{id}/run
   │
   ▼
 Manager.Run  (internal/autonomous/manager.go)
   ├── topo-sort tasks
   ├── for each task: spawn ephemeral worker via session.Manager.Start
   ├── verify via autonomous.verification_backend
   └── retry up to autonomous.auto_fix_retries
```

**Surfaces** (per BL197 audit):
- CLI ✓ (`datawatch autonomous {status, config, prd*}`)
- REST ✓ (9 endpoints)
- MCP ✓ (9 tools mirroring REST 1:1)
- Chat ✓ (added v4.9.2 — `autonomous` + `prd` aliases for status/list/get/decompose/run/cancel/learnings/create)
- PWA ✗ — only the config card under Settings → General → Autonomous; **no PRD lifecycle UI**

---

## Questions

### Q1 — Review-and-edit gate before Run

Today: `Decompose` persists the LLM's stories+tasks JSON; `Run` is
a separate call. There's no prescribed *human gate* — the moment
the operator calls `Run`, the worker fan-out starts.

The operator stated the gap explicitly: *"there should be a way to
save, review, update, decide on prd once they are done"*.

**Options:**
- **(a)** Add an explicit `status: needs_review` after Decompose; `Run` refuses unless status is `approved`. Operator transitions status via `POST /api/autonomous/prds/{id}/approve` (or `reject` / `request_revision`).
- **(b)** Keep state implicit (no transition), but require `--approved` on `Run` and surface a "needs review" badge in the PWA.
- **(c)** Status machine with explicit transitions AND a per-task edit surface (operator can rewrite an LLM-decomposed task's `spec` before running).

**Recommendation: (c).** The cost of the extra states is small; the value of letting an operator tighten an LLM's loose decomposition before spawning real workers is large. Concrete shape:

```
draft → decomposing → decomposed
                         │
                         ▼
                    needs_review ── operator edits per-task ──┐
                         │                                    │
                         ▼                                    │
               approved ────────────► running ──► complete    │
                         │                       │            │
                         └──── revisions  ◄──────┘            │
                         │                                    │
                         └◄────────────────────────────────────┘
```

### Q2 — PRD library / templates

Operator gap: *"there should be a way to save"* (saved-PRD library
for recurring patterns).

**Options:**
- **(a)** Templates are first-class records in `<data_dir>/autonomous/templates.json`. CRUD via REST. `POST /api/autonomous/prds {template_id, vars}` instantiates with substitutions.
- **(b)** Reuse the existing `session.CmdLibrary` model — store template-shaped commands that produce a PRD spec when executed.
- **(c)** Hybrid: templates are PRDs with `is_template: true` + `template_vars: [{name, default, required}]`. Listing PRDs filters by the flag. Cheaper to build (one schema, two views).

**Recommendation: (c).** Same data model, two views. No new file; one boolean and a substitution pass.

### Q3 — Decisions log

Operator gap: *"decisions log (which LLM produced this decomposition, prompt, cost)"*.

Today: the LLM call hits `/api/ask` which uses the session's billing path. Cost rolls up at session level but isn't attributed to "the decomposition for PRD prd_a3f9".

**Options:**
- **(a)** Add a `Decisions[]` slice to `PRD`: each Decompose / Run / Verify call records `{at, kind, backend, model, prompt_chars, response_chars, cost_usd, verdict_outcome}`. Surfaced via `GET /api/autonomous/prds/{id}/decisions`.
- **(b)** Extend the audit log (`internal/audit`) with a `prd_id` field; query the existing audit endpoint with the filter.
- **(c)** Both: (a) for the operator-facing per-PRD timeline, (b) for the security/compliance audit trail.

**Recommendation: (c).** The two audiences are different (operator wants timeline; compliance wants append-only audit log) and the data write is cheap.

### Q4 — Recursive build / child PRDs

Operator gap: *"recursive building"* — a PRD whose tasks spawn child PRDs.

Today: `Manager.Run` uses `pipeline.Task` per task. There's no "this task is itself a PRD" concept.

**Options:**
- **(a)** Add `Task.SpawnPRD bool`. When true, the task body is a *spec* and the worker calls `POST /api/autonomous/prds` to create a child PRD, decompose it, run it, then mark the parent task complete. Cycle-safe via depth limit + child PRD's `parent_id` field.
- **(b)** Build it on top of the BL117 PRD-DAG orchestrator: a child PRD is just a node in a graph, the parent task is a `prd:` node depending on it.
- **(c)** Defer until the operator hits a real need.

**Recommendation: (b)** for the structural layer + a small `Task.SpawnPRD` (a) shortcut for the common case. Reusing the orchestrator means we get guardrails on child PRDs for free; the shortcut keeps the simple single-PRD case readable without forcing the operator into a full graph definition.

### Q5 — PWA lifecycle UI

The audit (BL197) found that the PWA only exposes the *config*
card. No PRD list / create / approve / inspect / verdicts UI.

**Options:**
- **(a)** Phase-1 PWA: read-only PRD list + per-PRD detail page (stories, tasks, verdicts, decisions log if Q3 lands). No mutations from PWA — operators use CLI / chat to act.
- **(b)** Full CRUD: list / create / decompose / approve / edit-task-spec / run / cancel from PWA, mirror of CLI / chat verbs.
- **(c)** Phase-1 read-only first (a), Phase-2 mutations second once the CLI / chat patterns are stable.

**Recommendation: (c).** Read-only ships fast and validates the data shape; mutation surfaces follow once Q1 / Q2 / Q3 / Q4 are settled (otherwise PWA forms have to be reworked).

### Q6 — How does this overlap with BL117 PRD-DAG orchestrator?

The orchestrator already wraps PRDs in a DAG with guardrails. Q4
above proposed building recursion on top of it. Two follow-on
questions:

- **(6a)** Should the autonomous Manager call into the orchestrator
  for *any* PRD with more than one story? Or only opt-in?
- **(6b)** Where do guardrails belong — at the PRD level
  (autonomous responsibility) or at the orchestrator-graph level?

**Recommendation:** keep autonomous PRDs single-graph by default
(the operator's mental model: "the PRD is the unit"); the
orchestrator is the *meta-DAG* across PRDs. Guardrails stay at the
orchestrator layer (where they already are) and gain a "per-PRD"
mode that fires after the autonomous Run completes — aligns with
where verdicts already live.

---

## Recommended implementation order (after we align)

1. **Q3** decisions log + audit extension — no behaviour change, pure observability. Unblocks operator confidence in the rest.
2. **Q1** review-and-edit gate — cheapest behaviour change; immediate operator value.
3. **Q5 Phase-1** PWA read-only PRD list / detail — needs Q1 / Q3 to be useful.
4. **Q2** template flag — small schema add, big workflow win for recurring PRDs.
5. **Q4** recursion via the orchestrator — last because it's the most architecturally invasive; benefits from Q1 + Q3 being live so child PRDs are inspectable.
6. **Q5 Phase-2** PWA mutations — after the data shape stops moving.

---

## Out of scope for this conversation

- Switching the decomposition LLM provider (orthogonal — operator already controls via `autonomous.decomposition_backend`).
- Cost-policy gates (would belong in the orchestrator's guardrail layer, not the autonomous Manager).
- Multi-operator / RBAC on PRDs (BL7 territory — frozen).

---

## Decision log (filled during the conversation)

_(Empty — populate during the call.)_
