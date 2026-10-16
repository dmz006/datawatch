# BL452 — Logprob-classification guardrail backend ("The V Method") — design pass

**Status:** needs_review · **Date:** 2026-10-15 · **Tracker:** BL452
**datawatch version at planning:** v9.1.8 (`internal/server/api.go:192`)
**Scope of this doc:** design only. No Go or Python code, no config writes, no default changes, no node or model registration, no pulls. Everything under "Phases" is a proposal.

**Scope (files read for this pass, none modified):** `internal/autonomous/guardrail_registry.go`, `internal/autonomous/rules_review.go`, `internal/autonomous/ci_watch.go`, `internal/autonomous/models.go`, `internal/autonomous/executor.go`, `internal/autonomous/verification_llm.go`, `cmd/datawatch/main.go` (verifier prompt), `internal/compute/node.go`, `internal/compute/probe.go`, `internal/compute/registry.go`, `internal/llm/registry.go`, `internal/inference/llm.go`. Likely implementation footprint is listed in the Phases.

---

## Context and evidence

### The problem: judges that finish but do not report

The BL449 open-model matrix ([2026-10-15-bl449-open-model-matrix.md](2026-10-15-bl449-open-model-matrix.md)) is the evidence base. What that document actually records:

- r8 (Automaton `d07570dd`): the fast/mid workers died 3–7 s after spawn without writing `MODEL-IDENTITY.txt`; the `cpu` cell ran but wrote placeholder identities; `decompose-validate` ran and self-reported correctly, but no cell passed its verifier. 16/16 cells were suspect.
- The wider pattern in the same file and in `docs/plans/README.md` Stage A is **work happens, the durable artifact does not**: BL455 ("a retry can record the wrong pre-task SHA, producing a false 'no output' failure on real, correctly-committed work") and the BL431 Phase 1 tasks that failed with the same "no output" symptom.

The reported BL449 observation for this design is that **open-model verifiers (for example `qwen3:8b`) completed but never wrote parseable structured output**, the verifier-side twin of workers that do work and never commit. The verifier contract today is a free-text reply that must be strict JSON (`cmd/datawatch/main.go:4914-4915`: `{"ok": <bool>, "severity": ..., "summary": ..., "issues": [...]}`). A small model that answers in prose, wraps the JSON in commentary, or runs out of budget while "thinking" produces an unparseable reply, and the codebase classifies that as an **infra** failure, not a judgment (`internal/autonomous/failure_reason.go:38-41`: "unparseable response").

> **Evidence gap, stated plainly.** The committed BL449 document does not contain a per-verifier transcript for `qwen3:8b`. The verifier-specific claim comes from the BL452 task brief and the operator's BL449 runs; it is not independently reproducible from the repo. The first Phase below (P0) is to capture it as a checked-in fixture so this design stops resting on recollection.

### The technique

Replace "ask the model to produce a well-formed document" with "ask the model to **choose**":

1. **Closed multiple-choice question.** The prompt ends with a fixed option set, for example `A) the diff implements the spec  B) it does not`, and instructs the model to answer with only the letter.
2. **First-token logprob classification.** Do not parse text. Request `logprobs: true, top_logprobs: N` with `max_tokens: 1` and read the log-probability of each option token at position 0. The verdict is the argmax; the margin between options is a free confidence score.
3. **Reasoning disabled.** For Qwen3-family models, pass `chat_template_kwargs: {"enable_thinking": false}` so the first generated token is the answer and not the start of a `<think>` block.
4. **One forward pass.** Prompt in, one token out. No retry loop for malformed output, because malformed output is impossible: the result is a probability vector.

Properties that matter here: no JSON parsing, so the "unparseable response → infra" failure class cannot occur for the verdict bit; deterministic cost and latency (prefill dominated); and a calibrated-ish score that can drive a `warn` band instead of forcing a binary.

### Live validation (2026-10-15)

Reported validation run for this design (operator-run; no raw transcript is committed — see P0):

- `llama-server` from llama.cpp, **built from the official source**, CPU-only.
- Official Qwen3 GGUF.
- `POST /v1/chat/completions` with `logprobs` and `top_logprobs` **returns** `logprobs` / `top_logprobs` populated for the first token, with `chat_template_kwargs.enable_thinking=false` honoured.

### Confirmed blocker: Ollama cannot do this

The same request shape sent to Ollama's `/api/generate` and `/v1/chat/completions` is **accepted and silently returns no logprobs**. The parameters are not rejected, so a client cannot tell from the status code. Consequences:

- **Ollama is ruled out as a backend for this technique.** Every datawatch compute node of `kind: ollama` today, and any `openai-compat` node that is actually Ollama or OpenWebUI-in-front-of-Ollama, will pass a reachability probe and then return nothing.
- The failure mode is **silent**. A guardrail that treats "no logprobs" as "no signal" could fail open. The design must make "logprobs unavailable" a hard, loud error (see Phase 3 and the capability flag below).
- This is a different constraint from BL452's earlier lead (a possible Ollama native decision-model endpoint). That lead is separate and untested; nothing here depends on it.

---

## SCOPE item 1 — Inventory of today's guardrail and verifier decisions

Classification key: **closed-form** = the decision is a fixed small label set that a first-token classifier can emit directly. **hybrid** = a generative step must gather evidence first, then a closed-form step decides. **generative** = open-ended judgment or text whose value is the prose itself; must stay generative.

`GuardrailVerdict.Outcome` is `pass | warn | block` (`internal/autonomous/models.go:711`), with an orthogonal `Category` taxonomy (`:726`, doc'd at `failure_reason.go:36-48`: `infra | content | scope | policy`).

| # | Decision | Where | Today's mechanism | Class | Rationale |
|---|---|---|---|---|---|
| 1 | SAST / secrets / deps scan outcome | `guardrail_registry.go:30-46`, outcome mapping `:195-214` | Deterministic scanners; `block` if any finding ≥ `FailOnSeverity`, else `warn`, else `pass` | **Not an LLM decision** | Already deterministic and cheaper than any model. Out of scope; do not route through the classifier. |
| 2 | `project-rules-scan` | `guardrail_registry.go:53-57`, `:181-190` | Deterministic regex/presence/parity rules filtered by granularity | **Not an LLM decision** | Same as #1. |
| 3 | Scripted pass short-circuit | `executor.go:1520-1529`, `:1614-1622` (`scriptPassVerdict`) | A clean `VerificationScript` pass skips the LLM entirely | **Not an LLM decision** | Already the best case: a deterministic check beats any classifier. The classifier is for units that have no script. |
| 4 | Task verification `ok` | `cmd/datawatch/main.go:4914-4915`; call site `executor.go:1029-1030`; model resolution `verification_llm.go:53` | Free-text reply that must be strict JSON; `ok` is a bool | **closed-form** | "Does the diff plausibly implement the spec": yes/no. This is the exact decision whose parse failures BL449 saw. Primary target. |
| 5 | Task verification `severity` | same prompt, `severity: info\|low\|medium\|high\|critical` | Same JSON | **closed-form** | Five fixed labels; one forward pass with five-option choice, or derive from the yes/no margin. Secondary. |
| 6 | Task verification `summary` / `issues[]` | same prompt | Free text in JSON | **generative** | The prose is the product (it becomes the retry hint, `contentFixHint`, `executor.go:1575`). Keep generative, and make it **optional** and best-effort once the verdict no longer depends on parsing it. |
| 7 | Per-task guardrail outcome (`rules`, `security`, `release-readiness`, `docs-diagrams-architecture`, …) | `executor.go:1500-1580`; names listed in the `GuardrailVerdict.Guardrail` comment `models.go:710` | LLM judge returns a verdict via `runGuardrailWithInfraRetry`; `pass \| warn \| block` | **closed-form** (outcome) + **generative** (`Summary`, `Issues`) | Three-way outcome is a fixed label set. `warn` maps naturally to a low-margin band. |
| 8 | Per-story guardrail outcome | `executor.go:1592-1660` | Same shape at story level | **closed-form** + generative | Same as #7. |
| 9 | Verdict `Category` (`infra\|content\|scope\|policy`) | `models.go:726`; taxonomy `failure_reason.go:36-48` | Set by the judging mechanism | **closed-form** | Four labels. Caveat: `infra` means the mechanism failed, so it cannot be classified by the mechanism that failed. Keep `infra` as a harness-assigned category (transport error, missing logprobs, timeout), not a model output. |
| 10 | PRD-completion `rules-review` | `guardrail_registry.go:67-70`; `rules_review.go:125-186`; prompt `:58-74` | Tool-capable session reads the rules file and the real `git diff`, then reports "every rule you find violated or skipped, not just the first" | **generative** (finding) → optional **hybrid** (gate) | The value is the list of violated rules, which needs tool use and prose. It must stay generative. A classifier can only sit **after** it as the pass/warn/block gate over the generated findings, never replace it. Do not route the discovery step. |
| 11 | CI-failure triage `ci-watch` | `guardrail_registry.go:80-83`; `ci_watch.go:535-597` (`ciWatchJudge`) | `classifyCIRuns` is already deterministic (`ci_watch.go:249-262`); only the *failed-run judgment* uses an LLM, reading logs against the rules file | **hybrid** | Reading CI logs is evidence gathering (generative or tool-driven). The final "acceptable? / policy / content" call is closed-form over that evidence. |
| 12 | Content-autofix eligibility | `rules_review.go:178`, `ci_watch.go:~567` (`v.Category == ""` + budget) | Deterministic branch on the verdict's `Category` | **Not an LLM decision** | Consumes #9; unchanged. |
| 13 | Decompose output, planning, worker code | `autonomous` decompose path | Generative | **generative** | Not a judgment at all. Out of scope. |

**Takeaway.** The decisions worth moving are #4, #5, #7, #8 (and #11's final gate). Together they are the *verdict bit* of nearly every LLM-judged guardrail. The prose (#6, the `Issues` text, and all of #10's discovery) stays generative. The design therefore **splits every judged verdict into two independent calls**: a cheap, parse-proof classification that fixes `Outcome`, and an optional generative call that explains it. A failed or garbled explanation can no longer erase a valid verdict.

---

## SCOPE item 2 — ComputeNode / LLM-registry shape

### What exists

**ComputeNode (`internal/compute`)**

- `NodeKind` is documented as "strictly: what API protocol does the LLM endpoint at `<address>` speak?" (`node.go` NodeKind comment, `:~113-127`). Supported kinds are `ollama` and `openai-compat` (`node.go:129-130`; `SupportedKinds` at `:143`). The `KindOpenAICompat` comment **already lists llama.cpp server** alongside OpenWebUI, vLLM and LMStudio (`:130`). Deprecated kinds still parse (`validKind`, `:473`).
- `Node` carries `DeclaredCapacity` and `Hardware` (`node.go:~161-176`, `~312`) but **no capability or feature set**. Nothing on a node says what the endpoint can do beyond "speaks protocol X".
- `Probe` (`probe.go:41-80`) sends both `ollama` and `openai-compat` to the same `probeHTTP` (`probe.go:61-62`), which is a **HEAD request**; any HTTP response counts as healthy (`probe.go:202-231`). It cannot see a missing feature.
- `Registry` (`registry.go:31`) is name-keyed CRUD with persistence; it has no per-node capability state.

**LLM registry**

- `internal/llm/registry.go` (32 lines) is only the **`Backend` registry**: `Register`/`Get`/`Names` over a name→`Backend` map, populated from `init()` in `internal/llm/backends/*`.
- The operator-facing **LLM definitions** live in `internal/inference/llm.go`: `LLM` (`:91`) with `ComputeNodes` failover list and per-node `Models []EnabledModel` (`:~100-120`), `Kind` from `AllKinds` (`:76-80`: `ollama`, `openwebui`, `opencode`, `claude`, session kinds, `gemini-api`, `opencode-api`). Adapters are in `internal/inference/adapters/` (`ollama.go`, `openwebui.go`, `opencode_api.go`, …). **There is no generic `openai-compat` LLM kind**; the closest are `openwebui` and `opencode-api` (OpenAI-compatible, `llm.go:~68`).
- Verification LLM resolution is separate and already layered: `ResolveVerificationLLM` resolves **task → story → Automaton → global** (`verification_llm.go:53`) from `VerificationBackend/Model/Effort` fields on `Task` (`models.go:~596`), `Story` (`:~511`) and `PRD` (`:~162`). Guardrails have their own chain, `resolveGuardrailBackendModel` (`executor.go:1817`). The two chains are intentionally independent.

### Option A — reuse `openai-compat` (add a capability, not a kind)

A llama.cpp `llama-server` is registered as a `ComputeNode{kind: openai-compat, address: …}`, which the code and comment already anticipate. Add an optional, probed **capability set** to the node, starting with one capability, `logprobs`.

- **Pros**
  - Honors the documented meaning of `Kind` (wire protocol). `llama-server` *is* OpenAI-compatible; nothing about the protocol differs.
  - Touches no `validKind` / `AllKinds` / `SupportedKinds` / PWA dropdown / `UIRoutingModes`-style enumerations, and no parity-surface enum.
  - Models the real axis of variation. Logprobs support differs **within** the protocol (llama-server: yes; vLLM: yes; Ollama and OpenWebUI-in-front-of-Ollama: silently no). A new Kind would still need a capability flag for the next server that speaks the same protocol, so the flag is required either way.
  - Also covers a hosted OpenAI-compatible endpoint with logprobs if one is ever wanted.
- **Cons**
  - Needs a new field and a deeper probe than today's HEAD.
  - A misconfigured `openai-compat` node pointing at an Ollama gateway fails at capability-probe time, not at add time, unless the probe runs on save.

### Option B — new kind `llama-cpp`

- **Pros**
  - Explicit, discoverable in the node dropdown; could carry server-specific probes (`/health`, `/props`, slot count) and model-load management.
  - "This is a llama-server" is unambiguous to an operator.
- **Cons**
  - Violates the Kind-means-protocol rule that alpha.23 deliberately fixed (the same refactor that removed the deployment-shaped kinds).
  - Touches `NodeKind`, `SupportedKinds`, `validKind`, `Probe`'s switch, the PWA dropdown, REST/MCP/CLI/comm parity enums, config reference, and the datawatch-app mirrors, for no wire-level difference.
  - Still does not remove the need for a logprobs capability flag.
  - A second `llama-cpp`-only mechanism would have to be kept consistent with `openai-compat` request shaping forever.

### Recommendation: Option A, with a probed `logprobs` capability

Reasoning in one line: **the thing that varies is a feature, not a protocol.** Use `kind: openai-compat` for `llama-server` and add a capability the daemon *measures*. Revisit Option B only if a llama.cpp-specific control surface (slot management, model hot-swap, `/props`) is wanted later; that would be a separate decision with its own justification.

#### Capability flag

- Add `Capabilities` to `Node`: a small set (initially `logprobs`) where each entry is tri-state **`unknown | supported | unsupported`** plus `probed_at` and the probe's error text. Tri-state matters: `unknown` (never probed) must not be treated as `supported`.
- Operator may declare it, but the **probed value wins** and a declared `supported` that probes `unsupported` is surfaced as a warning on the node (the same declared-vs-measured pattern the BL437 hardware work uses).
- Persisted with the node; shown in `compute_node_get` / detail and in the PWA node card (parity surface, below).

#### Health probe

- Keep `probeHTTP` for reachability. Add a separate **capability probe** that runs on demand and when a node is bound as a verification LLM, not on every save (save-time probes must stay fast).
- Probe request: a canned two-option question with `max_tokens=1`, `logprobs=true`, `top_logprobs=5`, `chat_template_kwargs={"enable_thinking": false}`.
- **Pass criterion:** `choices[0].logprobs.content[0].top_logprobs` is present, non-empty, and contains both option tokens. Anything else is `unsupported`, including a 200 response with no `logprobs` object (the exact Ollama behaviour).
- Because the failure is silent, the probe must **inspect the body, not the status code**.

#### Request parameters

| Param | Value | Why |
|---|---|---|
| `logprobs` | `true` | Required to get per-token log-probabilities. |
| `top_logprobs` | small, ≥ option count (e.g. 5) | Reads all option tokens even when the model prefers a third token. |
| `max_tokens` | `1` | One forward pass; the answer is the first token. |
| `chat_template_kwargs` | `{"enable_thinking": false}` | Keeps Qwen3-style models from opening a `<think>` block. Passed through for templates that honour it; harmless otherwise. |
| `temperature` | `0` | Determinism; the logprobs are what we read, not a sample. |
| `messages` | system + user with explicit option letters and "answer with one letter" | Option tokens must be single tokens in the target tokenizer; verified per model in P2. |

The classifier must also handle **token-variant leakage**: `A`, ` A`, `a` can be different tokens. Normalize by summing probability mass over a small, per-model alias set for each option, and treat residual mass outside all options above a threshold as `abstain`.

#### How it plugs in as a verification-LLM override

The override chain already exists and needs no new precedence rules: **task → story → Automaton → global** (`ResolveVerificationLLM`, `verification_llm.go:53`). The proposal is to let a verification LLM entry *declare* that it is a classifier:

- A new per-LLM attribute on the verification target, e.g. `verification_mode: classify | generate` (default `generate`, so nothing changes for anyone). Placed on the LLM definition (`internal/inference/llm.go`) rather than on every `Task/Story/PRD`, so the existing six `Verification*` fields and the MCP tools `autonomous_prd_set_{,story_,task_}verification_llm` keep working unchanged: an operator points a task, story or Automaton at an LLM, and the LLM's mode decides how it is called.
- The LLM must resolve to a node whose `logprobs` capability is `supported`. At bind time (`set_*_verification_llm`) and at dispatch, the daemon checks the capability and **fails loudly** if it is `unsupported` or `unknown`: no silent fallback to generative parsing and no fail-open pass.
- Because `LLM` is `inference`-package, a thin adapter is needed for the OpenAI-compatible transport. Today there is no generic OpenAI-compat inference kind (only `openwebui`, `opencode-api`). Either reuse `opencode-api` with a documented `base_url`, or add a plain `openai-compat` LLM `Kind` mapped to the existing `openai-compat` node kind. The second is cleaner, small, and consistent with Option A; it is the one inference-side `Kind` addition this design assumes. Call it out for review.
- Resolution record: `VerificationResult` already stamps `VerifierBackend/Model/Effort/Source` (`models.go:744`). Add `VerifierMode` and the **score vector + margin** so a verdict is auditable and a `warn` band can be tuned from data.
- Guardrails (distinct chain, `resolveGuardrailBackendModel`, `executor.go:1817`) get the same `mode` through their LLM entry; no change to how `GuardrailBackend/Model` resolve.

---

## Phases

Each phase carries the Phase Completion Checklist from `AGENT.md` (Planning Rules item 5). Phase items below list only what is specific to the phase; the standard checklist items still apply and must be checked or marked N/A with a reason.

| # | Phase | Status |
|---|---|---|
| P0 | Evidence fixture | Planned |
| P1 | Node capability + probe | Planned |
| P2 | Classifier client and per-model alias calibration | Planned |
| P3 | Verification-mode wiring (task verdict bit) | Planned |
| P4 | Guardrail wiring and hybrid gates | Planned |
| P5 | Surfaces, docs, parity | Planned |
| P6 | Live validation against a real `llama-server` | Planned |

**P0 — Evidence fixture. Status: Planned.**
Capture the two claims this design depends on as checked-in, replayable artifacts: (a) an open-model verifier reply that is not parseable as the strict JSON contract (`main.go:4914`), and (b) the Ollama no-logprobs response beside a `llama-server` logprobs response for the same prompt. Resolves the evidence gap above. Acceptance: fixtures under `testdata/`, a table test showing today's parser rejects (a) and the capability probe's criterion distinguishes (b).

**P1 — Node capability + probe. Status: Planned.**
`Capabilities` on `Node` (tri-state, `probed_at`, error text); an on-demand capability probe in `internal/compute`; persistence; no change to `Kind`. Acceptance: unit tests for supported / unsupported / unknown; a 200-without-`logprobs` body is `unsupported`; existing nodes load unchanged.

**P2 — Classifier client and calibration. Status: Planned.**
A small client that builds the closed-question request, reads `top_logprobs`, sums alias mass per option, and returns `{label, scores, margin, abstain}`. Per-model alias sets and a margin threshold for the `warn` band. Acceptance: table tests on recorded responses; abstain when residual mass is high; token-variant cases covered.

**P3 — Verification-mode wiring. Status: Planned.**
`verification_mode` on the LLM; the verify path (`main.go` verifier closure, `executor.go:1029`) calls the classifier for `ok`/`severity` and, optionally, a separate generative call for `summary`/`issues`. `infra` is assigned only by the harness (transport error, missing logprobs, abstain). Acceptance: with a classifier LLM, no parse step exists on the verdict path; a garbled optional explanation does not change `ok`.

**P4 — Guardrail wiring and hybrid gates. Status: Planned.**
Map the three-way `Outcome` onto the margin bands for per-task and per-story guardrails (`executor.go:1500`, `:1592`). For `rules-review` and `ci-watch`, keep the generative discovery step and add the classifier only as the final gate over its findings. Acceptance: `rules-review` still emits its full findings list; scan guardrails and scripted passes are untouched.

**P5 — Surfaces, docs, parity. Status: Planned.**
See Parity surface. CHANGELOG, `docs/config-reference.yaml`, `docs/operations.md`, README and docs index, `docs/testing-tracker.md`, a how-to (with `exec_steps` or `LLM_ONLY`, version strings only, no tracker IDs), and datawatch-app issues.

**P6 — Live validation. Status: Planned.**
Against a real `llama-server` (CPU first, then the GPU hosts): classification accuracy against the BL422/BL449 fixtures' known-good and known-bad diffs, latency per verdict, and a deliberate Ollama-backed node to prove the loud-failure path. This phase must run against a live daemon; a mocked HTTP server would hide the silent-no-logprobs bug that motivates the capability flag.

---

## Parity surface

| Surface | Decision | Reason |
|---|---|---|
| REST | Include | Node capability read/probe endpoint; `verification_mode` on the LLM; verdict score fields. |
| MCP | Include | `compute_node_get/detail/health` expose capabilities; LLM tools accept the mode; `autonomous_prd_set_*verification_llm` unchanged but validate capability. |
| CLI | Include | Same data via the existing node/LLM subcommands. |
| comm channel | Include (read-only) | Capability and verdict score in node/verdict replies. |
| YAML/config | Include | `verification_mode` and node capability declaration in `docs/config-reference.yaml`. |
| PWA | Include | Capability badge on the node card; mode selector on the LLM editor; margin shown in verdict detail. |
| Android | Proposal only | File a datawatch-app issue per the Mobile-Parity rule; no mobile change here. |
| iPhone/iOS | Proposal only | Same. |

## Open questions

1. **Where does `verification_mode` live** — on the `LLM` (recommended, keeps the six existing override fields untouched) or on each override level?
2. **Plain `openai-compat` LLM `Kind`** vs. reusing `opencode-api`: confirm the inference-side addition.
3. **Margin → `warn` mapping.** Is a low-margin band `warn`, or should low confidence instead escalate to the generative path? Needs P6 data.
4. **Abstain policy.** Residual mass outside the option set: fail the task as `infra`, retry with a rephrased prompt, or fall back to a generative verifier? Recommended default: `infra`, loud, no silent fallback.
5. **Should `rules-review` and `ci-watch` gate through the classifier at all** in v1, or stay fully generative until P3/P4 prove out on task-level verdicts?
6. **Tokenizer coverage.** Per-model alias sets must be built and checked; which models are in the first supported set?
7. **Option B trigger.** What concrete llama.cpp-only capability would justify a dedicated kind later?
8. **Evidence provenance.** P0 should replace the unrecorded BL449 `qwen3:8b` and live-validation claims with fixtures; confirm who captures them.

## Checklist

- [x] Header lines: Status needs_review, Date 2026-10-15, Tracker BL452, datawatch version
- [x] Scope, numbered Phases with Status, Parity surface
- [x] Inventory table with file:line references (SCOPE item 1)
- [x] Option A vs B with a recommendation (SCOPE item 2)
- [x] No code, no config writes, no version bump, no pulls
- [ ] Operator review (needs_review)
