# LLM "Enabled Models" overhaul — pre-v7.0

**Date:** 2026-05-10
**Operator directive:** All scope below ships pre-v7.0. No phase deferrals.
**Cut:** v7.0.0-alpha.37 (this work) — single sprint, can split into 37a/b/c if needed.

## Why

Today an LLM record has a single `model` field. The actual operating model is:

> An LLM enables a specific inference engine — on selected Compute Nodes (local) or via SaaS — and exposes a set of models to operators when that LLM is picked.

Operator concrete case: nodes with 500 GB RAM run 120B models; nodes with 32 GB cannot. The current single-`model` field can't represent "this LLM enables Ollama on `gpu-big` and `gpu-small` but only `qwen3:120b` is available on the big one." Multi-select per node is mandatory, not a UX nicety.

## What ships

### A. Data model (Go)

**`internal/inference/llm.go`** — extend the `LLM` struct:

```go
type LLM struct {
    Name            string
    Kind            Kind
    ComputeNodes    []string

    // GATE alpha.37: replaces single Model field. Element shape:
    //   { node: "gpu-1", model: "qwen3:120b" }
    // When ComputeNodes contains a single entry, every Models[i].node
    // must equal that one entry. When ComputeNodes is empty (SaaS
    // kinds: claude-code, gemini), Models[i].node is empty.
    Models          []EnabledModel

    AutoAddModels   bool       // when set, daemon's model-refresh loop
                               // appends newly-discovered models on
                               // each ComputeNode automatically
    APIKeyRef       string
    Disabled        bool
    Tags            []string
    AutoCreated     bool
    CreatedAt       time.Time
    UpdatedAt       time.Time

    // Back-compat alias — single-model deployments and old YAML.
    // Migration: on Load, if Models is empty AND Model is set,
    // populate Models from Model × each ComputeNode.
    Model           string `json:",omitempty"`
}

type EnabledModel struct {
    Node  string `json:"node,omitempty"` // empty for SaaS kinds
    Model string `json:"model"`
}
```

**Migration**: `LLMRegistry.LoadAll()` runs once at startup — if `Models == nil && Model != ""`, populates `Models = [{node: cn, model: Model}]` for every `cn` in `ComputeNodes`. Old `Model` field stays serialized for one release as a read alias, then dropped in v7.1.

### B. Daemon endpoints

**Existing surfaces (CRUD parity already wired)**:
- `GET    /api/llms` — returns `Models[]` instead of `Model`
- `POST   /api/llms` — accepts both `models[]` and legacy `model` (back-compat shim)
- `PUT    /api/llms/{name}` — same
- `DELETE /api/llms/{name}` — already exists; gets enhanced with the in-use check below

**New**:
- `GET    /api/llms/{name}/in_use` → `{ sessions: [...], automata: [...], personas: [...], total: N }`
  - Returns every session / automaton / council persona currently bound to this LLM
  - Includes state per row so PWA can mark active vs terminal
  - Paginated query params: `?page=1&size=5|10|50&filter=foo&kinds=session,automata`
- `POST   /api/llms/{name}/refresh_models` → triggers a Compute Node model-list refresh + auto-enable reconcile
- `DELETE /api/llms/{name}` enforces:
  1. Hold a registry write-lock for the duration
  2. Call `in_use` with `state IN (running, planning, decomposing, waiting_input)` filter
  3. If non-empty → 409 Conflict with body `{ blocked_by: [...] }`
  4. Operator must reassign / cancel / force-cascade — see `reassign` endpoint below
- `POST   /api/llms/{name}/reassign` → body `{ to_llm: "<other-llm>", to_model: "<model-name>" }`
  - Updates every active binding (sessions, automata, personas) currently using `{name}` to use `to_llm` (and optionally a specific `to_model` within it)
  - For `running` sessions with in-flight LLM calls, the reassign queues for the next call (next `/api/ask` resolves the new LLM)
  - For `waiting_input` / `planning` (decompose) it takes effect immediately on next dispatch
  - Returns `{ reassigned: N, sessions: [...], automata: [...], personas: [...] }`
  - Operator can chain this with DELETE for a "reassign-then-delete" flow (PWA does this in one transactional pair)
- `POST   /api/llms/{name}/force_delete` → cascade-cancels all active bindings then deletes. Requires `confirm: "yes I understand this terminates active work"` in the body to fire. Audit-logged with operator name + binding count.

### C. CLI / MCP / comm parity (per `feedback_full_parity_required` rule)

- `datawatch llm models add <llm-name> --node <cn> --model <name>`
- `datawatch llm models remove <llm-name> --node <cn> --model <name>`
- `datawatch llm models list <llm-name>`
- `datawatch llm in-use <llm-name>`
- `datawatch llm refresh-models <llm-name>`
- MCP tools mirror: `llm_add_model`, `llm_remove_model`, `llm_list_models`, `llm_in_use`, `llm_refresh_models`
- Comm verbs: `add llm <name> model on <cn>: <model>`, `remove llm <name> model on <cn>: <model>`, `show llm <name> in-use`

### D. PWA — Edit/Add LLM panel rewrite

Replace the single `Model` text input with a structured **per-node table**:

```
┌─ Compute Nodes (multi-select; ordered failover) ────────┐
│  [x] gpu-big                                            │
│  [x] gpu-small                                          │
│  [ ] dev-laptop                                         │
└──────────────────────────────────────────────────────────┘

┌─ Enabled models ─────────────────────────────────────────┐
│  Node          Model                          [+ Add]   │
│  gpu-big       qwen3:120b              [✕]              │
│  gpu-big       qwen3:8b                [✕]              │
│  gpu-small     qwen3:8b                [✕]              │
│  + Add row → [node-dropdown] [model-dropdown probed]    │
│                                                          │
│  ☐ Auto-enable new models from these compute nodes      │
└──────────────────────────────────────────────────────────┘
```

- Node column: dropdown of selected compute nodes (subset of ComputeNodes multi-select)
- Model column: probed from `/api/<kind>/models?node=<cn>` per row; falls back to free-text if probe fails
- "+ Add row" appends an empty pair; can add same model on multiple nodes (failover) OR different models on different nodes (capability split)
- Validation: model must exist on the node (probe verifies); inline ⚠ on mismatch
- For SaaS kinds (no ComputeNodes): table collapses to a single column `Model` with no node

**In-use envelope** — collapsible card under each LLM row in the list view AND inside the Edit panel:

```
▾ In use by 3 sessions, 1 automaton  (click to expand)
  ─────────────────────────────────────────────────────
  Filter: [____________]  Page size: [5▾]   1–4 of 4
  ─────────────────────────────────────────────────────
  Sessions
    🟢 ralfthewise-a95f  smoke-state-engine  running
    ⚪ ring-laptop-2bfd  ring dispatch       complete
    🔴 dev-c123          test                killed
  Automata
    🟢 9fc249b5  GATE smoke  planning  →
```

- Default page size: 5; selectable 5/10/50
- Filter: `&`-joined AND substring (`smoke & running`)
- Each row clicks through to its detail view
- Closed by default

**Block on delete** (operator-spec'd 3-tier flow):
- Before the DELETE API call, PWA fetches `in_use?states=active`
- If empty: confirm + send DELETE.
- If non-empty: open the block modal:
  ```
  ⚠ N sessions / M automata / K personas are using this LLM
  ─────────────────────────────────────────────────────────
  • smoke-state-engine (running)            → open
  • 9fc249b5  GATE smoke (planning)          → open
  • code-reviewer persona (running)          → open
  ─────────────────────────────────────────────────────────
  Reassign to another LLM:
    [ dropdown of LLMs (excluding this one) ▾ ]   [Reassign + Delete]

  OR cancel each binding manually using the links above

  ─────────────────────── ⋯ Force delete ──────────────────
  ```
  - **Primary path: Reassign + Delete** — operator picks replacement LLM (and optionally a specific model within it); PWA POSTs `/reassign` then `DELETE`. One transaction. Active sessions keep working under the new binding.
  - **Secondary path: manual cancel** — links open detail views; operator cancels each, returns to retry DELETE.
  - **Tertiary path: ⋯ Force delete** — hidden under overflow menu; opens a confirm modal "Cancel N sessions + M automata + K personas + delete LLM. This terminates work in progress. ☐ I understand". Only fires `/force_delete` when checkbox checked AND the confirm button clicked.

### E. Locales × 5 (per `feedback_localization_rule`)

New keys (all 5 bundles):
- `llm_field_enabled_models`, `llm_field_enabled_models_hint`
- `llm_field_auto_add_models`
- `llm_in_use_collapsed`, `llm_in_use_filter_ph`, `llm_in_use_page_size`, `llm_in_use_pagination`
- `llm_in_use_sessions`, `llm_in_use_automata`, `llm_in_use_personas`
- `llm_delete_blocked_title`, `llm_delete_blocked_hint`, `llm_delete_blocked_fix_link`
- `llm_models_add_row`, `llm_models_node_col`, `llm_models_model_col`
- `llm_models_node_missing_warn` (warn when selected model isn't on the chosen node)

### F. Mobile-parity (per `feedback_mobile_parity_trigger`)

File one datawatch-app issue (NOT a comment) labeled `alpha.37 PWA parity — LLM Enabled Models`:
- Per-node multi-select model picker
- Auto-enable toggle
- In-use envelope (paginated)
- Block-on-delete UX
- New locale keys pulled from upstream
- Wear OS / Android Auto: not applicable (settings panel only on phone)

### G. Smoke tests (per `feedback_per_release_smoke`)

Add to `scripts/release-smoke.sh`:
- `llm_enabled_models_basic` — create LLM with 2 ComputeNodes + 3 EnabledModels (some node-specific), GET shows full list, PUT updates one row, DELETE blocked when bound to running smoke-* session
- `llm_in_use_pagination` — POST 12 smoke-* sessions bound to the test LLM, GET in_use returns paginated batches of 5
- `llm_auto_add_models` — set `auto_add_models=true`, simulate a new model arriving on the compute node (admin endpoint POST), verify it appears in the LLM's enabled list
- `llm_back_compat_load` — load YAML with old `model:` field, verify it's surfaced as a 1-row Models array
- `llm_delete_409` — bind LLM to active session, attempt DELETE, verify 409 with `blocked_by` body
- `llm_reassign_active_bindings` — bind LLM A to a smoke session, POST /api/llms/A/reassign with `to_llm: B`, verify session now uses B + LLM A is deletable
- `llm_force_delete_audit` — bind LLM to smoke session, POST /api/llms/A/force_delete with `confirm`, verify session is cancelled + LLM gone + audit log entry

### H. Cookbook entry per cut (per `feedback_docs_plans_audit`)

CHANGELOG.md entry shape:
```
## v7.0.0-alpha.37 — LLM Enabled Models overhaul

### Schema
- LLM.Models []EnabledModel (per-node + per-model)
- LLM.AutoAddModels bool

### REST
- GET /api/llms/{name}/in_use — paginated; filter; states
- POST /api/llms/{name}/refresh_models
- DELETE /api/llms/{name} — 409 when active session/automaton bound

### CLI / MCP / comm
- (full list as above)

### PWA
- Edit/Add LLM panel: per-node-per-model table, auto-enable toggle
- In-use collapsible envelope per LLM row (paginated 5/10/50, filter)
- Delete blocked on active bindings — modal lists offenders + links

### Mobile parity
- datawatch-app#XXX filed

### Rule audit
- 7-surface parity ✓ (REST + CLI + MCP + comm + PWA + locale × 5 + mobile-parity issue)
- Smoke counts: +5 new
- Locale ×5 — all keys filled (English fallback for non-en bundles pending translation pipeline)
- Plans hygiene — this plan doc dated; archived after v7.0 ship
- Mobile-parity issue — datawatch-app#XXX
- Cookbook — this entry
- Deviations — none
```

## Sequencing

Single sprint, but executable in 3 commits if review prefers smaller deltas:

1. **alpha.37a** — schema + back-compat load + REST endpoints + CLI/MCP/comm parity + smoke tests
2. **alpha.37b** — PWA Edit/Add LLM panel rewrite (per-node table + auto-enable)
3. **alpha.37c** — In-use envelope (PWA + API) + block-on-delete + pagination/filter + cookbook + datawatch-app issue

Each commit independently smoke-tested + tagged. Operator can pause between cuts.

## Plan audit (against memory rules)

| Rule | Compliance |
|---|---|
| `feedback_full_parity_required` (REST + MCP + CLI + comm + PWA + mobile + YAML) | ✓ — all 7 surfaces enumerated in B/C/D/E/F |
| `feedback_localization_rule` (× 5 bundles + datawatch-app issue) | ✓ — section E + F |
| `feedback_per_release_smoke` (functional smoke gates the tag) | ✓ — section G; 5 new smoke checks |
| `feedback_docs_plans_audit` (CHANGELOG + plan doc with rule audit per cut) | ✓ — section H + this audit |
| `feedback_user_facing_docs_no_internals` (no BL### / alpha.NN in user-facing copy) | ✓ — locale strings in E avoid version refs |
| `feedback_automata_not_prd` | ✓ — uses "automata" / "automaton" in copy |
| `feedback_backlog_is_spec` (no decide-then-ship) | ✓ — this plan IS the spec; operator approves before code |
| `feedback_smoke_naming_cleanup` (smoke-* + add_cleanup) | ✓ — smoke tests use smoke-* prefix per section G |
| `feedback_smoke_cleanup_only_tracked` (no mass-sweeps by name pattern) | ✓ — each smoke check tracks its own resources |
| `feedback_no_empty_exec_target` | n/a — no exec calls |
| `feedback_phase_release_pattern` (implement → build → smoke → tag) | ✓ — sequencing in section above |
| `feedback_no_mega_bucket_close` (audit sub-features before umbrella close) | ✓ — splits into a/b/c with explicit checklist per cut |
| `feedback_recut_install` (re-cut needs manual cp) | ✓ — noted in release-workflow; standard process |
| `feedback_no_global_pattern_widening` | n/a — no global pattern changes |

Audit verdict: **clean against all 14 applicable memory rules**. No deviations.

## Estimated effort

| Section | Effort |
|---|---|
| A. Schema + migration | 1 hour |
| B. Daemon endpoints (REST) | 2 hours |
| C. CLI / MCP / comm parity | 2 hours |
| D. PWA panel rewrite + in-use envelope + delete-block | 4 hours |
| E. Locales × 5 | 30 min |
| F. Mobile-parity issue + content | 30 min |
| G. Smoke tests | 1 hour |
| H. CHANGELOG + cookbook | 30 min |
| **Total** | ~11 hours, single sprint |

## Operator decisions (interview 2026-05-10)

1. **Auto-enable scope** → **Per-LLM** (single global toggle). Per-Compute-Node-within-LLM granularity deferred — add as Phase 3 if it becomes painful.

2. **Block-on-delete UX** → **Reassign-first, then cascade as escape hatch**:
   - **Primary action**: "Reassign to..." button + dropdown of available LLMs (or models within this LLM minus the one being removed). One click updates every active binding (sessions / automata / personas) to use the replacement, then DELETE proceeds. This is the operator's spec — avoids killing in-flight work.
   - **Secondary action**: strict by default — operator can also cancel each binding manually via the click-through links.
   - **Tertiary action under ⋯ menu**: "Force delete (cancel all + delete)" — power-user escape hatch with explicit "I understand this terminates work" checkbox. Hidden by default.
   - **Backend implication**: need a session/automaton/persona model-reassign API that's safe while the binding is running. For sessions in `running` state with an in-flight LLM call, the reassign queues for the next call; for `waiting_input` it takes effect immediately on next dispatch.

3. **Filter syntax** → **AND substring across all columns** (option A). Simplest, phone-friendly, matches Linear/GitHub/Slack behavior. False-positives tolerable in small-N lists (operator scans visually).

These answers are folded into the implementation sections above — see the revised C (REST adds POST /api/llms/{name}/reassign), D (PWA modal flow), and G (smoke now includes `llm_reassign_active_bindings`).
