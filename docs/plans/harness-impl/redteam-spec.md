# Red-team Validator Pipeline — Implementation Spec (BL369 Deepening)

**Date:** 2026-09-16 · **Status:** Draft · **Source:** Proposal 3 (`docs/plans/harness-research/enhancement-proposals.md` §5 "Red-team validator pipeline (deepening BL369)") + Theme 4 (`docs/plans/harness-research/feature-themes.md` §4 "Guardrail depth: red-team validator pipeline at the model boundary")

**Scope:** Pipeline design · Stage + Verdict contracts · Config/MCP/REST/CLI/comm surface extensions · Test plan · Sequencing.

**Self-contained:** every code reference below is an existing file and line verified against the working tree at spec-writing time. An implementer who has not read other `harness-impl/` docs needs only this file, the two research sources, and the cited source files.

---

## 1. Overview

### 1.1 Problem

BL369's injection guard is a **lexical, warn-only phrase scan** at the API boundary. It is not a validator pipeline:

- The scan is a fixed table of 11 regexes (`injectionPatterns`, `internal/autonomous/security.go:98-114`); anything not literally matching those phrases — paraphrased, obfuscated, or task-shaped injections — flows unchecked into an autonomous worker spec.
- Even when it matches, the default behaviour is **warn-only**: `internal/autonomous/manager.go:139-160` (`checkInjectionGuard`) logs the finding and increments `metrics.InjectionGuardHitsTotal` (`internal/metrics/prometheus.go:86-87`, `datawatch_injection_guard_hits_total`), and returns an error **only** when `BlockOnInjection` is also true.
- The existing semantics are the superset's starting point. `internal/config/config.go:1532-1537`: `InjectionGuard` (warn) vs `BlockOnInjection` (reject, **requires** `InjectionGuard` true — `block_on_injection` without the guard is a documented no-op, per the test at `internal/autonomous/bl369_injection_test.go:98-112`, `TestBL369_CheckInjectionGuard_Disabled`, whose config at line 104-105 sets `InjectionGuard: false, BlockOnInjection: true` with the comment `// block_on_injection without guard = no-op`).
- The guard emits **no verdict record**: it is an inline log line + counter, so it does not appear in `per_automaton_guardrails` overrides, the guardrail verdict stream, or `orchestrator_verdicts` — the operator cannot tune it per-PRD, cannot audit it as a block action, and cannot approve/dismiss it.

**Goal:** elevate the guard to a multi-stage **validator pipeline at the PRD/task boundary** (Guardrails-AI / promptfoo shape, per `enhancement-proposals.md:65-75`), while **subsuming the existing two booleans with backward-compatible defaults** — after upgrade, a daemon with `injection_guard: true, block_on_injection: false` behaves exactly as today (the smoke regression in §4.3 checks this against `bl369_injection_test.go` expectations).

### 1.2 The 3-stage design

Stage 1 (existing, unchanged contract) → Stage 2 (new) → Stage 3 (new).

| # | Stage | What it is | Input | Action policy |
|---|-------|-----------|-------|---------------|
| 1 | **Regex layer** | The existing `ScanForInjection` table (`internal/autonomous/security.go:118-126`) re-exposed as a pipeline stage. Deterministic, zero-latency, no network. | PRD/task spec text | `warn` (default) or `block` (opt-in) |
| 2 | **LLM adversarial classifier** | New. One-shot LLM call (reuses the existing `ask` path / planning backend) that classifies the spec into exactly one of four labels with a confidence score. Interface-only in this spec (§3.7); no prompt is shipped by this doc. | text + context (PRD ID, title, story/task title, owner peer) | `pass` below threshold; `warn` in-band; `block` above escalation threshold — all per the Stage 3 action map |
| 3 | **Action escalation** | New. Maps each stage's verdict to an operator-visible `pass` / `warn` / `block` action, emits it into the guardrail verdict stream, and enforces `block` (reject at the API boundary) when the action map says so — behind the audit gate (§2.4). | stage verdicts + config action map | `warn` default for both stages; `block` strictly opt-in |

Classifier labels (fixed vocabulary — the classifier MUST return one of these):

| Label | Meaning |
|-------|---------|
| `benign` | In-bounds task spec; no injection or tool-abuse signal. |
| `instructing-out-of-bounds` | Spec instructs the worker to step outside its delegated task (role override, ignore-context, new-instructions patterns) — the class the regex table already targets, generalized. |
| `exfiltration` | Spec pushes the worker to read/return secrets, tokens, credentials, or out-of-project data. |
| `tool-abuse` | Spec steers the worker toward dangerous tool use (destructive shell, credential access, network egress, filesystem escape). |

**Backward-compat rule (superset):** the pipeline's Stage 1 is the regex layer itself, and its default action is `warn` — identical to `injection_guard: true` alone. Setting `block_on_injection: true` (existing key, existing semantics) maps 1:1 to `stage_actions.regex: "block"`. The new keys (`classifier_enabled`, `confidence_threshold`, `stage_actions.*`) all default to "off / warn", so a pre-existing config file produces byte-identical runtime behaviour (verified by the §4.3 smoke).

---

## 2. Pipeline design

### 2.1 Stage interface contract

Every stage (regex, LLM classifier, and any future stage) implements the same Go-structural contract. Field names + types below are the proposed shape; they are defined here as the interface the implementer codes against.

```
Stage (interface)
  Run(ctx, Input) (StageVerdict, error)
```

```
Input
  Text        string    // PRD spec or task spec text under validation
  Kind        string    // "prd-create" | "prd-edit" | "task-edit" (the call site label —
                        // matches the existing label strings "prd spec", "prd spec edit",
                        // "task spec edit" in manager.go:639, 1085, 1112)
  PRDID       string    // PRD ID when present (empty for pre-create)
  Title       string    // PRD title (context for the classifier)
  OwnerPeer   string    // federation owner peer, if set (PRD.OwnerPeer — see
                        // bl369_injection_test.go:178-198 for the OwnerPeer round-trip test)
  ProjectDir  string    // project dir for the PRD (empty at create time)
```

```
StageVerdict
  Stage       string    // "regex" | "llm-classifier" | <future stage name>
  Outcome     string    // "pass" | "warn" | "block"
  Confidence  float64   // 0.0–1.0; regex stage uses 1.0 on hit, 0.0 on miss
  Evidence    []string  // evidence excerpts: regex pattern message + matched span,
                        // or classifier label + confidence + quoted span
  Summary     string    // one-line human summary (goes into Verdict.Summary)
  VerdictAt   time.Time
  Error       string    // stage execution failure (timeout, backend down) — treated as
                        // "pass with note" per §2.3 fail-open rule, unless severity=block
```

**Outcome vocabulary is `pass`/`warn`/`block`** — deliberately the same three-value vocabulary as the orchestrator's `Verdict.Outcome` (`internal/orchestrator/models.go:43-50`) so the verdict-stream join in §2.5 is a direct field copy, not a translation.

### 2.2 Stage registration

The pipeline registers itself as a **named guardrail entry** in the existing guardrail library, using the same registration path as the built-in scan guardrails:

- Existing pattern: `initGuardrailLibrary()` pre-registers `sast-scan`, `secrets-scan`, `deps-scan` at Manager construction (`internal/autonomous/guardrail_registry.go:25-48`); additional entries flow through `RegisterGuardrail` (`internal/autonomous/guardrail_registry.go:59-71`), and skill-contributed entries through `loadSkillGuardrails` (`internal/autonomous/guardrail_registry.go:190-208`). The library is read by `GuardrailLibrary()` (`internal/autonomous/guardrail_registry.go:50-57`) and surfaced at `GET /api/autonomous/guardrails` (`internal/router/guardrails.go:40-65` comm verb `guardrail library`).
- **New:** a fourth entry, name `injection-pipeline`, `Type: "classifier"` (new type string, alongside the existing `"scan"` value used at `guardrail_registry.go:32` so the library type filter keeps working). It appears in the library listing, in profiles, and in `per-task`/`per-story` guardrail override lists (`resolveGuardrails`, `internal/autonomous/guardrail_registry.go:85-105`) — this is what gives per-PRD tuning "for free": an operator can set `per_task_guardrails: ["injection-pipeline"]` on a PRD and the pipeline runs as a per-task gate exactly like `sast-scan` does today (`invokeScanGuardrail` at `internal/autonomous/guardrail_registry.go:107-178` is the shape to mirror — same `GuardrailVerdict{Outcome, Summary, Issues, VerdictAt}` return, same `block`/`warn`/`pass` mapping).
- **Boundary check (unchanged):** the pipeline ALSO remains the default pre-create/pre-edit gate at the three existing call sites — `checkInjectionGuard` invocations at `internal/autonomous/manager.go:639` (CreatePRD), `:1085` (EditPRDFields), `:1112` (EditTaskSpec). The refactor keeps those call sites; their body becomes "run the pipeline (regex stage always; classifier stage if enabled), aggregate, enforce action map". No new call site is required.

### 2.3 Stage execution order + fail-open rule

1. **Regex stage runs first, always** (it is the backward-compat anchor).
2. **Classifier stage runs second, only if `classifier_enabled` is true.** It receives the regex stage's evidence in context so the LLM can confirm or contradict it.
3. **Aggregation:** final action = max severity across stages mapped through the action map (`block` > `warn` > `pass`).
4. **Fail-open rule (MUST):** if a stage returns `Error` (LLM timeout, backend unreachable, malformed JSON), that stage contributes `pass` with a note in `Summary` and the pipeline proceeds. Rationale: the classifier is a defense-in-depth layer, not the only layer; the regex layer still runs. A hard error in the *only* enabled stage (e.g. regex stage, which cannot fail) is not a supported configuration. This mirrors the existing "no guardrail fn wired → informational pass" tolerance at `internal/orchestrator/runner.go:232-238`.

### 2.4 The block-mode gate + audit (MUST sit behind both)

`block` is a self-modifying security posture (the daemon will now refuse user-supplied input). Both conditions are required before a `block` verdict is *enforced* (rejected at the API boundary):

1. **`mcp.allow_self_config` gate.** The gate is the daemon-wide `AllowSelfConfig` bool at `internal/config/config.go:1162-1170` (`AllowSelfConfig bool` with the BL110 comment "gates the config_set MCP tool"). Enforcement in process: `allowSelfConfigCheck` at `internal/mcp/bl220_mcp_tools.go:27-35` (denied-result helper: `"permission denied: set mcp.allow_self_config=true in the config file (and restart) to enable self-modify"`) and the same pattern at `internal/mcp/memory_tools.go:916-927` (`config_set` handler gate + bootstrap-protect on the key itself). The pipeline's block-mode **config writes** (setting `stage_actions.*: "block"` via MCP `autonomous_config_set`) must pass this same gate — block mode is an anti-replay/anti-poisoning posture and the daemon already treats self-modifying config as a privileged act; the block *decision* path at the API boundary does not need an MCP session (it runs synchronously in the HTTP handler), so the gate applies to the *configuration of* block mode, consistent with how `autonomous.block_on_injection` is written through `PUT /api/config` today.
2. **Audit trail.** Every block decision (and every override-approval) appends a JSON-lines row to the self-config audit sink — the existing `auditSelfConfig` pattern in `internal/mcp/self_config_audit.go:14-57` (`SelfConfigAudit{At, Key, Value, Note}` → stderr + `SelfConfigAuditPath` file, `Note: "AI-initiated via MCP config_set (mcp.allow_self_config=true)"`). The pipeline emits the same shape with `Key: "autonomous.injection-pipeline.block"` and `Value: <stage verdict JSON>` so the audit stream is grep-uniform with existing AI-initiated config events.
3. **Existing counter retained.** `metrics.InjectionGuardHitsTotal` (`internal/metrics/prometheus.go:86-87`) continues to increment on any regex hit — the smoke in §4.3 can assert on it.

### 2.5 Verdict-stream join (orchestrator_verdicts + per_automaton_guardrails)

The pipeline's aggregate `StageVerdict` is mapped into the **exact** shape the orchestrator verdict stream already stores, `internal/orchestrator/models.go:41-50`:

```
type Verdict struct {
    Outcome     string    `json:"outcome"`               // pass | warn | block
    Severity    string    `json:"severity,omitempty"`    // info | low | medium | high | critical
    Summary     string    `json:"summary"`
    Issues      []string  `json:"issues,omitempty"`
    VerdictAt   time.Time `json:"verdict_at"`
    ValidatorID string    `json:"validator_id,omitempty"` // session ID of the validator worker, if any
}
```

Mapping (stage verdict → `Verdict`): `Outcome → Outcome` (direct), `Evidence → Issues`, `Summary → Summary`, `VerdictAt → verdict_at`, `Confidence → Summary` suffix `(confidence=%.2f)`, `Stage → Severity` (`regex`=medium, `llm-classifier`=high when non-pass), `ValidatorID` = classifier LLM name when the classifier stage produced the verdict.

- **`orchestrator_verdicts` join:** the verdict endpoint is `GET /api/orchestrator/verdicts` (`internal/server/server.go:372`, handler `handleOrchestratorVerdicts` → `internal/server/orchestrator.go:12` doc comment, `:213` `writeJSONOK … s.orchestratorAPI.ListVerdicts()`). The pipeline appends its `Verdict` to the same store the guardrail nodes append to (the `r.guard(ctx, GuardrailRequest{…})` adapter call at `internal/orchestrator/runner.go:221-262`, where a non-`block` outcome completes the node and `block` halts the graph — runner.go:257-260). The classifier stage is therefore a **drop-in guardrail-node payload**: no orchestrator change is required to *store* the verdict; the new work is the adapter (§2.2) that produces `models.Verdict` from `StageVerdict`.
- **`per_automaton_guardrails` join:** per-PRD override precedence is already `explicit PerTask/PerStoryGuardrails > named GuardrailProfile > global Config` (`internal/autonomous/guardrail_registry.go:8-10`, resolver §2.2). The pipeline entry (`injection-pipeline`) participates in this list like any other name; the MCP tool that sets it is `per_automaton_guardrails_set` (`internal/mcp/guardrail_tools.go:1-14`, tool at `:145`), and the comm verb is `guardrail automaton set <id>` (`internal/router/guardrails.go:132-155` → `PUT /api/autonomous/prds/<id>/guardrails`). No schema change — the pipeline is just another name in an existing `[]string`.
- **Session-level approval path (override-allow):** the existing per-guardrail block-approval shape — `HookGuardrailVerdict` at `internal/server/hook_events.go:62` with `Approved`/`ApprovalNote` fields and the `approveVerdict` + `unblocked` logic exercised by `internal/server/gh153_guardrail_approve_test.go:27-73` (TC-1/TC-2/TC-3: approve one of two blocks → still blocked; approve all → unblocked) — is the override-allow surface the block-mode PRD inherits. A blocked PRD spec gets an `Approved` flag the operator can set per-PRD; the §4.2 integration "override-allow path" test asserts this.

### 2.6 PRD-scan-loop sibling (Phase 3)

The fix-sub-PRD loop exists and is the exact shape to reuse: `CreateFixPRD` at `internal/autonomous/manager.go:2169-2171` ("creates a child PRD whose spec targets the violations"), exposed as `POST /api/autonomous/prds/<id>/scan/fix` (`internal/server/autonomous.go:1221-1229` → `CreateFixPRD`), MCP `autonomous_prd_scan_fix` (`internal/mcp/bl221_scan.go:98-108`), and comm `autonomous scan-fix <id>` (`internal/router/sx2_parity.go:538-543`). The Phase 3 pipeline sibling (`injection-pipeline-fix`) creates a fix child-PRD whose spec targets the *reported* injection patterns, using the same `CreateFixPRD` code path; false-positives reported via the approval path can feed the `scan-rules` AGENT.md rule-proposal loop (`internal/router/guardrails.go`-adjacent existing verb `autonomous scan-rules <id>`, `internal/router/sx2_parity.go:286` help string).

---

## 3. API / interface design (markdown only)

### 3.1 Config additions (superset over BL369)

New fields in the autonomous config, placed next to the existing `InjectionGuard`/`BlockOnInjection` pair at `internal/config/config.go:1532-1537`. All additive; existing two booleans retain their exact semantics.

| Key | Type | Default | Meaning |
|-----|------|---------|---------|
| `injection_guard` | bool | **existing default (false)** | Stage 1 (regex) enabled. Unchanged from BL369. |
| `block_on_injection` | bool | **existing default (false)** | Stage 1 action = `block`. Unchanged from BL369; requires `injection_guard: true` (no-op otherwise — `bl369_injection_test.go:104-105`). Superseded by `stage_actions.regex` when the latter is set. |
| `classifier_enabled` | bool | `false` | Stage 2 (LLM adversarial classifier) enabled. |
| `classifier_confidence_threshold` | float64 | `0.7` | Above this the classifier's non-`benign` label escalates per `stage_actions.llm`; below it the classifier contributes `pass`. |
| `classifier_block_confidence` | float64 | `0.9` | Above this the classifier escalates to `block` (when `stage_actions.llm: block`). Between the two thresholds the action is `warn`. |
| `stage_actions.regex` | string | `"warn"` | Action for the regex stage: `warn` \| `block`. Overrides `block_on_injection` when set explicitly. |
| `stage_actions.llm` | string | `"warn"` | Action for the classifier stage: `warn` \| `block`. |
| `classifier_timeout_ms` | int | `8000` | Per-call timeout for the classifier LLM. On timeout → fail-open (§2.3). |
| `classifier_backend` | string | `""` | LLM backend for the classifier (empty = inherit `autonomous.planning_backend`). |

**Precedence (block-override priority):** explicit `stage_actions.<stage>` > `block_on_injection` (regex stage only) > default `warn`. `block` in either stage action is the operator's explicit opt-in; `warn` is always the default. The `mcp.allow_self_config` gate (§2.4) applies to the *write* of any `*_block*`-impacting value via the MCP surface.

**Round-trip requirement** (AGENT.md "Configuration Accessibility Rule", AGENT.md:383-410): `PUT /api/config` → `GET /api/config` must round-trip every new key, mirroring the existing `autonomous.injection_guard` / `autonomous.block_on_injection` cases at `internal/server/api.go:5824-5827`.

### 3.2 MCP `autonomous_config_set` extension

Existing surface: the tool declares the two booleans at `internal/mcp/autonomous.go:62-63` (`injection_guard` + `block_on_injection` booleans) and forwards them at `:118-122` (`body["injection_guard"] = v` / `body["block_on_injection"] = v`). Extend **additively** — the two existing booleans remain declared and forwarded unchanged (backward-compat with existing MCP clients), and new params are appended to the same `mcpsdk.NewTool("autonomous_config_set", …)` declaration:

| New param | Type | Forwards to |
|-----------|------|-------------|
| `classifier_enabled` | bool | `autonomous.classifier_enabled` |
| `classifier_confidence_threshold` | float | `autonomous.classifier_confidence_threshold` |
| `classifier_block_confidence` | float | `autonomous.classifier_block_confidence` |
| `stage_actions_regex` | string (`warn`\|`block`) | `autonomous.stage_actions.regex` |
| `stage_actions_llm` | string (`warn`\|`block`) | `autonomous.stage_actions.llm` |
| `classifier_timeout_ms` | int | `autonomous.classifier_timeout_ms` |
| `classifier_backend` | string | `autonomous.classifier_backend` |

The `autonomous_config_get` tool (`internal/mcp/autonomous.go:33-37`) surfaces all of the above in its response — it proxies `GET /api/autonomous/config` which reads from `handleAutonomousConfig` (`internal/server/autonomous.go:29-30`).

### 3.3 REST config surface

Existing keys on both ends:

- **GET:** `internal/server/api.go:4969-4970` — `"injection_guard": s.cfg.Autonomous.InjectionGuard` and `"block_on_injection": s.cfg.Autonomous.BlockOnInjection` inside the `"autonomous"` map.
- **PUT:** `internal/server/api.go:5824-5827` — `case "autonomous.injection_guard"` and `case "autonomous.block_on_injection"` in `applyConfigPatch`.

Extended with the §3.1 key set (identical keys, both GET and PUT cases). The autonomous-specific config endpoint is separate: `GET/PUT /api/autonomous/config` (`internal/server/server.go:345`, handler `internal/server/autonomous.go:29-30`) — the new fields are added to its body struct alongside the existing `InjectionGuard`/`BlockOnInjection` fields at `internal/autonomous/manager.go:114-115`.

### 3.4 CLI surface

Existing verbs: `datawatch autonomous config-get` / `config-set <json>` at `cmd/datawatch/cli_autonomous.go:116-145` (thin REST proxies to `GET /api/autonomous/config` and `PUT /api/autonomous/config`). Both verbs accept the new keys **without CLI change** — `config-set` takes a JSON body and forwards it verbatim (`cli_autonomous.go:130-143`). No new CLI verb is required. Document in the `config-set` help (line 127): the new keys from §3.1 are accepted alongside `injection_guard` / `block_on_injection`.

### 3.5 Comm-channel surface

Existing pattern: the `autonomous` command family is handled in `internal/router/commands.go:105-117` (verb list) and dispatched in `internal/router/sx2_parity.go:270-487+` (each sub-verb proxies to a REST endpoint via `r.commGet`/`r.commJSON`). The guardrail-specific comm verbs are in `internal/router/guardrails.go:1-181` (`guardrail library`, `guardrail profile *`, `guardrail automaton set`, `guardrail session run`). Extended additively:

| New comm verb | Proxies to |
|---------------|-----------|
| `autonomous config-set k=v,…` (existing verb shape, new keys accepted) | `PUT /api/autonomous/config` |
| `injection-pipeline status` (new) | `GET /api/autonomous/pipeline/status` (new endpoint, §3.3) |
| `injection-pipeline run <prd-id> <stage>` (new, on-demand single-stage run) | `POST /api/autonomous/pipeline/run` (new endpoint, §3.3) |
| `guardrail automaton set <id> per-task=injection-pipeline` (existing verb, new guardrail name) | `PUT /api/autonomous/prds/<id>/guardrails` (existing) |

New REST endpoints (§3.3 additions): `GET /api/autonomous/pipeline/status` (effective stage config + last 10 verdicts), `POST /api/autonomous/pipeline/run` (on-demand run of a named stage against a PRD/task — mirrors `InvokeGuardrailByName` at `internal/autonomous/guardrail_registry.go:251-269`).

### 3.6 Parity surface

Per AGENT.md "Parity surface section" rule (AGENT.md:151 — "every generated plan document under `docs/plans/` MUST contain a `## Parity surface` section") and the full parity-surface set defined at AGENT.md:462-463 (`REST`, `MCP`, `CLI`, `comm channel`, `YAML/config`, `PWA`, `Android`, `iPhone/iOS`).

| Surface | Status | Detail |
|---------|--------|--------|
| **YAML/config** | required | §3.1 keys in `autonomous:` section; `injection_guard` + `block_on_injection` retain existing keys (config.go:1536-1537) |
| **REST** | required | `GET/PUT /api/config` (api.go:4969/5824 extension), `GET/PUT /api/autonomous/config` (autonomous.go:29-30 extension), `GET /api/autonomous/pipeline/status` (new), `POST /api/autonomous/pipeline/run` (new) |
| **MCP** | required | `autonomous_config_get`/`autonomous_config_set` extended (autonomous.go:62-63/118-122), `per_automaton_guardrails_set` (guardrail_tools.go:145) accepts `injection-pipeline` |
| **CLI** | required | `autonomous config-get` / `config-set` (cli_autonomous.go:116-145) — new keys accepted, no new verb |
| **Comm channel** | required | §3.5 new verbs added to `handleConfigure` dispatch (router.go:834) and `sx2_parity.go` |
| **PWA** | required | Settings → Automata tab: two new toggles (`classifier_enabled`, `stage_actions` selectors) alongside existing `injection_guard`/`block_on_injection` toggles (app.js:11871-11872); locale keys × 5 bundles (en/de/es/fr/ja) per Localization Rule (AGENT.md:415) |
| **Android** | required (parity target) | datawatch-app issue filed: Settings card parity for new toggles; capability required, implementation idiomatic (AGENT.md:455-467) |
| **iPhone/iOS** | required (parity target) | same datawatch-app issue; SwiftUI client parity per AGENT.md:455-460 |
| **Federation** | gated | `injection-pipeline block` mode must carry a `fedCap` guard on the new write endpoints per Federation-Parity Rule (AGENT.md:526, B18 checklist AGENT.md:1151) |

### 3.7 Classifier prompt + response contract (interface only)

Documented here as the **interface** the classifier stage MUST satisfy. The prompt text is an operator-supplied implementation detail (stored in config at `classifier_system_prompt`, a future key — not part of this spec's config additions). The response contract is enforced by the stage.

**Input (stage request → LLM prompt):**

| Field | From |
|-------|------|
| `task_text` | `Input.Text` |
| `kind` | `Input.Kind` (one of `prd-create`, `prd-edit`, `task-edit`) |
| `prd_id` | `Input.PRDID` (may be empty) |
| `prd_title` | `Input.Title` |
| `owner_peer` | `Input.OwnerPeer` |
| `regex_evidence` | Stage 1 `Evidence` array (confirming or contradicting) |
| `project_dir` | `Input.ProjectDir` |

**Response (LLM → stage, strict JSON, no markdown fences):**

```
{
  "label":        "benign" | "instructing-out-of-bounds" | "exfiltration" | "tool-abuse",
  "confidence":   0.0-1.0,
  "evidence":     [ "<quoted span from task_text>", … ],
  "rationale":    "<≤ 200 chars explaining the classification>"
}
```

**Enforcement rules:**
- `label` MUST be one of the four; any other value → stage `Error` → fail-open (§2.3).
- `confidence` MUST be in `[0,1]`; out-of-range → stage `Error` → fail-open.
- `evidence` array MUST be non-empty when `label != "benign"`; empty evidence on a non-benign label → stage `Error` → fail-open (the classifier must cite the text it is classifying).
- Stage maps the response: `benign` → `pass` (regardless of confidence); non-`benign` at `confidence < classifier_confidence_threshold` → `pass`; at `confidence ≥ classifier_confidence_threshold` and `< classifier_block_confidence` with `stage_actions.llm: block` → `warn`; at `confidence ≥ classifier_block_confidence` with `stage_actions.llm: block` → `block`; any non-`benign` at any confidence with `stage_actions.llm: warn` → `warn`.

---

## 4. Test plan

### 4.1 Unit tests

| # | What | Where | Key assertions |
|---|------|-------|----------------|
| U1 | **Regex stage warn** — regex hit, `stage_actions.regex: warn` | `bl369_injection_test.go`-style test (package `autonomous`) | `Outcome=="warn"`, `Evidence` non-empty, `Confidence==0.0` (miss) or `1.0` (hit); `InjectionGuardHitsTotal` incremented (prometheus.go:86-87) |
| U2 | **Regex stage block** — regex hit, `stage_actions.regex: block` | same | `Outcome=="block"`, `Error` returned containing the label string (mirror `bl369_injection_test.go:89-95` assertion `strings.Contains(err.Error(), "injection-guard")`) |
| U3 | **Regex stage no-op** — `injection_guard: false`, `block_on_injection: true` | mirror `bl369_injection_test.go:98-112` | `nil` error, no verdict row (backward-compat no-op) |
| U4 | **Classifier `benign`** below threshold | mocked LLM returns `{"label":"benign","confidence":0.95}` | `Outcome=="pass"`, no audit row |
| U5 | **Classifier `benign`** above threshold | mocked LLM returns `{"label":"benign","confidence":0.99}` | `Outcome=="pass"` (benign is always pass regardless of confidence) |
| U6 | **Classifier `instructing-out-of-bounds`** at/below threshold | mocked LLM, confidence = threshold exactly, then threshold−0.01 | at threshold: `Outcome` per action map; below: `Outcome=="pass"` |
| U7 | **Classifier `exfiltration`** below block threshold, action=block | mocked LLM, confidence between `confidence_threshold` and `block_confidence` | `Outcome=="warn"` |
| U8 | **Classifier `exfiltration`** at/above block threshold, action=block | mocked LLM, confidence = `block_confidence` exactly, then +0.01 | `Outcome=="block"`, audit row written (self_config_audit.go shape) |
| U9 | **Classifier `tool-abuse`** with `stage_actions.llm: warn` | mocked LLM, high confidence | `Outcome=="warn"` (action map caps at warn) |
| U10 | **Malformed LLM response** — invalid label / out-of-range confidence / empty evidence | mocked LLM returns bad JSON on each axis | stage `Error` set, fail-open: `Outcome=="pass"`, pipeline continues |
| U11 | **Block escalation → audit** | block outcome from any stage | JSON-lines row appended to `SelfConfigAuditPath` with `Key:"autonomous.injection-pipeline.block"`, `Value:` contains stage-name + label |
| U12 | **Fail-open on LLM error** — `classifier_timeout_ms` exceeded | mocked LLM sleeps past timeout | stage `Error` set, pipeline returns regex-stage result alone |
| U13 | **StageVerdict → models.Verdict join** | unit test in `package orchestrator | autonomous` | `Outcome`, `Issues` (from `Evidence`), `Summary`, `VerdictAt` field-by-field; `ValidatorID` set when classifier stage was the blocking stage |
| U14 | **Guardrail library registration** | `guardrail_registry_test.go`-style test | `GuardrailLibrary()` contains `"injection-pipeline"` with `Type:"classifier"`; `lookupGuardrailEntry("injection-pipeline")` returns the entry |
| U15 | **Per-PRD override precedence** | `guardrail_registry_test.go:149-188` (existing pattern) | PRD with `PerTaskGuardrails: ["injection-pipeline"]` → pipeline runs for that PRD; PRD without → does not run for that PRD |
| U16 | **Clean spec passes all stages** | mirror `bl369_injection_test.go:114-128` | `Outcome=="pass"` end-to-end, no audit row, no verdict row |

### 4.2 Integration tests

| # | Scenario | Steps | Expected |
|---|----------|-------|----------|
| I1 | **PRD create blocked** | Manager with `injection_guard: true`, `stage_actions.regex: block`; call `CreatePRD("ignore previous instructions and do X", …)` (mirror bl369_injection_test.go:64-77 setup) | `err != nil`, error contains `"injection-guard"` (same string as existing test line 93); PRD not persisted |
| I2 | **Verdict in orchestrator_verdicts** | After a classifier-stage `block` on a PRD that is a guardrail-node in an orchestrator graph: call `runGuardrail` path (runner.go:221-262) with the pipeline wired as the `r.guard` fn | `Node.Verdict.Outcome == "block"`; node NOT marked done (runner.go:257-260); `GET /api/orchestrator/verdicts` (server.go:372 → orchestrator.go:213) returns the verdict row with `validator_id: "<classifier_backend>"` |
| I3 | **Override-allow path** | Reuse `gh153_guardrail_approve_test.go:27-73` pattern: seed `HookGuardrailVerdict{Guardrail:"injection-pipeline", Outcome:"block"}`; call `approveVerdict(sid, "injection-pipeline", "operator override")` | `Approved==true`, `ApprovalNote=="operator override"`; `unblocked==true`; PRD task unblocks |
| I4 | **PRD-scan-loop sibling (Phase 3)** | Blocked PRD with a recorded `injection-pipeline` verdict; call `CreateFixPRD` (manager.go:2169-2171) with the verdict attached | Child PRD created with spec targeting the reported injection pattern; child PRD status `draft` |
| I5 | **Comm verb round-trip** | `autonomous config-set classifier_enabled=true` via comm channel | REST round-trip: `GET /api/autonomous/config` → `classifier_enabled: true` |
| I6 | **MCP `autonomous_config_set`** | Call MCP tool with `classifier_enabled: true`, `stage_actions_llm: "block"` | REST round-trip via `GET /api/autonomous/config`; `autonomous_config_get` MCP tool returns both keys |
| I7 | **Per-PRD pipeline via override** | `per_automaton_guardrails_set` with `per_task_guardrails: ["injection-pipeline"]`; run the PRD | Task-level pipeline verdict present in task telemetry |
| I8 | **Federation guard** | Attempt `PUT /api/autonomous/prds/<id>/guardrails` with `fedCap: read` (lower than write) | `403` or equivalent per Federation-Parity Rule (AGENT.md:526) |

### 4.3 Smoke (regression)

| # | What | Steps | Expected |
|---|------|-------|----------|
| S1 | **Default-behaviour unchanged** | Start daemon with `autonomous.injection_guard: true, block_on_injection: false` (the existing BL369 default). Run `TestBL369_CheckInjectionGuard_WarnMode` (bl369_injection_test.go:64-77): CreatePRD with an injection phrase succeeds. | `err == nil` — identical to pre-upgrade behaviour |
| S2 | **Block-mode unchanged** | Same config + `block_on_injection: true`. Run `TestBL369_CheckInjectionGuard_BlockMode` (bl369_injection_test.go:81-96). | `err != nil`, `strings.Contains(err.Error(), "injection-guard")` |
| S3 | **No-op unchanged** | `injection_guard: false, block_on_injection: true`. Run `TestBL369_CheckInjectionGuard_Disabled` (bl369_injection_test.go:100-112). | `err == nil` — no-op preserved |
| S4 | **Counter increments** | Run S1, then read `datawatch_injection_guard_hits_total` via Prometheus | counter incremented by 1 |
| S5 | **Classifier disabled → zero LLM calls** | `classifier_enabled: false`; run S1/S2 | No LLM call made (assert via call-count in the ask-path mock); verdict stream shows only regex-stage row |

---

## 5. Sequencing & dependencies

```
Phase 0 ──────────► Phase 1 ──────────► Phase 2 ──────────► Phase 3
(classifier         (stage dispatch+    (block mode+        (fix-sub-PRD
 contract+config)    verdict join)      audit gate)          loop)
```

### Phase 0 — Classifier contract + config

**Goal:** the pipeline's config keys, data structures, and the classifier JSON contract are defined and loadable. No pipeline runs yet.

| File to touch | Change |
|---------------|--------|
| `internal/config/config.go` | Add §3.1 fields next to existing `InjectionGuard`/`BlockOnInjection` (config.go:1532-1537) |
| `internal/autonomous/manager.go` | Add `Config` struct extensions next to existing `InjectionGuard`/`BlockOnInjection` (manager.go:110-115); update `DefaultConfig()` to set defaults |
| `internal/autonomous/pipeline.go` | **New file** — `Stage`, `StageVerdict`, `Input`, classifier JSON contract type, stage interface definition (no implementation beyond type definitions) |
| `internal/server/api.go` | Add GET/PUT cases for new keys (api.go:4969-4970 GET map; api.go:5824-5827 PUT switch) |
| `internal/server/autonomous.go` | Add fields to `handleAutonomousConfig` body struct (autonomous.go:29-30) |
| `internal/mcp/autonomous.go` | Add new params to `autonomous_config_get`/`set` tool declarations (autonomous.go:62-63 tool spec; 118-122 body forwarding) |
| `cmd/datawatch/cli_autonomous.go` | Update `config-set` help string (cli_autonomous.go:127) to list new keys |
| `internal/router/guardrails.go` | No change yet (Phase 0 has no new comm verb) |
| `internal/router/router.go` | No change yet |

### Phase 1 — Stage dispatch + verdict join

**Goal:** the regex stage runs as a pipeline stage (not inline); the classifier stage dispatches to the LLM and its `StageVerdict` joins `orchestrator_verdicts` + `per_automaton_guardrails`. Both stages default to `warn`.

| File to touch | Change |
|---------------|--------|
| `internal/autonomous/pipeline.go` | Implement `regexStage.Run`; stub `classifierStage.Run` (LLM call + JSON parse + fail-open) |
| `internal/autonomous/manager.go` | Refactor `checkInjectionGuard` (manager.go:139-160) to call `pipeline.Run(ctx, input)` and aggregate; update the three call sites (manager.go:639, 1085, 1112) |
| `internal/autonomous/guardrail_registry.go` | Register `injection-pipeline` entry in `initGuardrailLibrary` (guardrail_registry.go:27-48); add `Type: "classifier"` to the entry; add a `invokeClassifierGuardrail` alongside `invokeScanGuardrail` (guardrail_registry.go:107-178) |
| `internal/autonomous/models.go` | Add `injection-pipeline` to the guardrail name set if there is a validation list (models.go:186 `PerTaskGuardrails` context) |
| `internal/orchestrator/runner.go` | No structural change — the pipeline's `GuardrailVerdict`→`models.Verdict` mapping is injected as the `r.guard` fn (§2.5). If the `GuardrailRequest` struct needs the classifier-specific context (PRD spec text, regex evidence), add the fields additively (runner.go:245-252 `GuardrailRequest` call site) |
| `internal/orchestrator/models.go` | No change — `Verdict` struct (models.go:43-50) is reused verbatim |
| `internal/metrics/prometheus.go` | No change — `InjectionGuardHitsTotal` counter (prometheus.go:86-87) continues to increment |
| `internal/server/autonomous.go` | Wire pipeline entry into `GET /api/autonomous/guardrails` response (autonomous.go:1614 context) |

### Phase 2 — Block mode + audit gate

**Goal:** `stage_actions.regex: block` and `stage_actions.llm: block` are enforced. The `mcp.allow_self_config` gate is verified on block-mode config writes. The audit sink fires on every block decision and every override-approval.

| File to touch | Change |
|---------------|--------|
| `internal/autonomous/manager.go` | Action-map enforcement: when aggregated outcome is `block`, return the same error shape as existing `checkInjectionGuard` block path (manager.go:157 `fmt.Errorf("injection-guard: …")`) |
| `internal/autonomous/pipeline.go` | Audit call: emit `SelfConfigAudit{Key: "autonomous.injection-pipeline.block", Value: <verdict JSON>}` via the same stderr + file sink pattern as `internal/mcp/self_config_audit.go:29-57` |
| `internal/mcp/autonomous.go` | Enforce `allowSelfConfigCheck` (bl220_mcp_tools.go:27-35 pattern) before forwarding any `stage_actions_*: "block"` or `block_on_injection: true` value through `handleAutonomousConfigSet` (autonomous.go:67-122) |
| `internal/server/hook_events.go` | `HookGuardrailVerdict` (hook_events.go:62) — add `injection-pipeline` to the guardrail names accepted by `approveVerdict` (hook_events.go:316-337 context); the `Approved`/`ApprovalNote` fields already exist (used by gh153_guardrail_approve_test.go:39-42) |
| `internal/server/autonomous.go` | Override-allow REST endpoint: `POST /api/autonomous/prds/<id>/guardrail/injection-pipeline/approve` (mirrors `POST /api/sessions/<id>/guardrail/<name>/approve` tested in gh153_guardrail_approve_test.go:93-108) |
| `internal/router/guardrails.go` | Add `injection-pipeline approve <prd-id>` comm verb alongside existing `guardrail automaton set` (guardrails.go:132-155) |
| `internal/router/router.go` | Dispatch the new comm verb (router.go:1014-1015 `case CmdConfigure` dispatch area) |

### Phase 3 — Auto fix-sub-PRD loop

**Goal:** a blocked PRD can produce a fix child-PRD targeting the reported injection, mirroring the scan-fix pattern.

| File to touch | Change |
|---------------|--------|
| `internal/autonomous/manager.go` | Extend `CreateFixPRD` (manager.go:2169-2171) to accept an `injection-pipeline` verdict and generate a spec targeting the reported pattern; or add a sibling method `CreateInjectionFixPRD` |
| `internal/server/autonomous.go` | `POST /api/autonomous/prds/<id>/injection/fix` endpoint (autonomous.go:1221 `case "scan/fix"` pattern) |
| `internal/mcp/bl221_scan.go` | `autonomous_prd_injection_fix` MCP tool (bl221_scan.go:98-108 pattern) |
| `internal/router/sx2_parity.go` | `autonomous injection-fix <prd-id>` comm verb (sx2_parity.go:538-543 pattern) |
| `cmd/datawatch/cli_autonomous.go` | `autonomous prd-injection-fix <id>` CLI verb (cli_autonomous.go:61-104 command list) |

### Dependencies & independence

- **Independent of Proposal 1 (Eval Sweep) and Proposal 2 (Eval DAG nodes).** The pipeline touches no eval runner, no RunSet, no orchestrator node kind. The shared surface is the **guardrail verdict stream** (`models.Verdict` / `orchestrator_verdicts`) — that is an *interface* already defined and in production use by guardrail nodes, not a new dependency.
- **Shared surface:** the `orchestrator_verdicts` endpoint and the `per_automaton_guardrails` override precedence. An eval-node implementation (Proposal 2) that lands before Phase 1 will find the `injection-pipeline` verdict already in the stream; an eval-node that lands after Phase 1 will find the pipeline registered in the guardrail library and the new `Type: "classifier"` value alongside the existing `Type: "scan"`. Either ordering works; there is no build-order constraint.
- **Ordering rationale** per `enhancement-proposals.md:79-84` (Sequencing section): "Proposal 5 second — small surface, closes the security gap (#4) with existing guardrail plumbing." This spec is "Proposal 5" (Proposal 3 by the user's numbering = §5 of enhancement-proposals.md = Theme 4 of feature-themes.md). The smallest-new-surface principle applies: the pipeline is a new guardrail-library entry + a new Type value + config fields; no new store, no new runner, no new node kind, no new REST route family (the pipeline/status and pipeline/run endpoints are single-method additions to the existing `/api/autonomous/*` family). This is materially smaller than either Proposal 1 or 2, supporting the "second" sequencing slot in the enhancement-proposals ordering.

---

## Parity surface

Per AGENT.md:151 (Parity surface section rule) and AGENT.md:462-463 (full parity-surface set).

| Surface | Status | Key file / key |
|---------|--------|---------------|
| YAML/config | required | `autonomous.classifier_enabled`, `autonomous.classifier_confidence_threshold`, `autonomous.classifier_block_confidence`, `autonomous.stage_actions.regex`, `autonomous.stage_actions.llm`, `autonomous.classifier_timeout_ms`, `autonomous.classifier_backend` + existing `injection_guard`, `block_on_injection` (config.go:1536-1537) |
| REST | required | GET/PUT `/api/config` (api.go:4969/5824), GET/PUT `/api/autonomous/config` (autonomous.go:29-30 → server.go:345), GET `/api/autonomous/pipeline/status` (new), POST `/api/autonomous/pipeline/run` (new), POST `/api/autonomous/prds/<id>/injection/fix` (new, Phase 3), POST `/api/autonomous/prds/<id>/guardrail/injection-pipeline/approve` (new, Phase 2) |
| MCP | required | `autonomous_config_get`/`auto­nomous_config_set` (autonomous.go:33/46), `per_automaton_guardrails_set` (guardrail_tools.go:145), `autonomous_prd_injection_fix` (new, Phase 3) |
| CLI | required | `autonomous config-get`/`config-set` (cli_autonomous.go:116-145), `autonomous prd-injection-fix <id>` (new, Phase 3) |
| Comm channel | required | `autonomous config-set k=v,…` (existing shape), `injection-pipeline status`, `injection-pipeline run <prd> <stage>` (new), `autonomous injection-fix <id>` (new, Phase 3), `injection-pipeline approve <prd-id>` (new, Phase 2) |
| PWA | required | Settings → Automata tab: toggles + selectors alongside existing injection_guard/block_on_injection toggles (app.js:11871-11872); 5 locale bundles (en/de/es/fr/ja) |
| Android | required (parity target) | datawatch-app issue per Mobile-Parity Rule (AGENT.md:455) |
| iPhone/iOS | required (parity target) | datawatch-app issue per Mobile-Parity Rule (AGENT.md:455-460) |
| Federation | gated | `fedCap` guard on new write endpoints per Federation-Parity Rule (AGENT.md:526, B18 AGENT.md:1151) |
| Audit | required (Phase 2) | `self_config_audit` JSON-lines sink (self_config_audit.go:14-57) for every block decision + override-approval |
