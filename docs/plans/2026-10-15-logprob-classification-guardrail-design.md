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

---

## SCOPE item 3 — Machinery beyond the single happy-path call

The Technique section above describes one forward pass with one question. That is enough to show the signal exists, but not enough to trust it as a verdict. This section specifies what sits around the single call: position-bias cancellation, probability renormalization and aggregation, a confidence score, routing into pass/warn/block, calibration, and the failure modes that must fail closed. Everything here is a proposal; no code is written by this pass.

### 3.1 Letter-position bias and cyclic option rotation

Instruction-tuned models have a letter-position prior: with options `A) … B) …` the model systematically favours one letter independent of content. A single call therefore measures "content + letter prior". The fix is to ask the same question several times with the option *semantics* rotated across the letter positions, then cancel the prior by averaging.

- **Binary (K = 2): 2 rotations.** Rotation 0 presents `A) pass  B) fail`; rotation 1 swaps them to `A) fail  B) pass`. Each semantic option occupies each letter once, so a constant letter preference contributes equally to both semantic options and cancels in the average.
- **K-way: K rotations.** Rotation `r` (r = 0 … K−1) places semantic option `S[(i + r) mod K]` at letter `i`. Every semantic option appears in every letter position exactly once across the K rotations, which is the cyclic Latin-square condition that makes the average prior-free to first order. Full permutations (K!) are not needed for this; K rotations are the proposed default, with the full set reserved for K ≤ 3 if spread checks (below) fail.
- **Cost.** Each rotation is one forward pass with `max_tokens: 1`, so the cost is K prompt-processing passes per verdict. For binary guardrails that is 2×; for K = 4 it is 4×. The per-rotation prompt bodies are identical except for the option block, so prompt-prefix caching on the llama-server side should cover most of the spec and diff text; this is a P6 measurement, not an assumption.

### 3.2 Renormalization and aggregation

For rotation `r`, the classifier reads the raw probabilities of the K option tokens (plus their alias sets, see Failure modes) from `top_logprobs` at position 0. Two steps:

1. **Renormalize over the option tokens.** Let `q_i` be the summed alias-mass for letter `i`, and `R` the residual mass outside all options (the abstain signal). Then `p_i = q_i / Σ_j q_j`. The residual is kept, not discarded: `R = 1 − Σ_j q_j` is recorded and gates the abstain route.
2. **Map back to semantics and average.** Letter `i` in rotation `r` corresponds to semantic option `S[(i + r) mod K]`, so each rotation yields a semantic vector `p^(r)`. The aggregate is the arithmetic mean of semantic probabilities across rotations: `P(S_k) = (1/K) Σ_r p^(r)(S_k)`. The mean is taken in probability space, not log-odds space, so a single near-zero log-probability cannot dominate the average.

The verdict is the argmax of `P`. The per-rotation disagreement is kept as a separate signal: `spread(S_k) = max_r p^(r)(S_k) − min_r p^(r)(S_k)`. A large spread means the answer depends on letter position, which is exactly the bias the rotations were meant to expose.

**Worked example, K = 2 (binary pass/fail).**

| Rotation | Letter A | Letter B | Raw option mass | Residual R | Renormalized | Semantic `pass` |
|---|---|---|---|---|---|---|
| r0 (`A=pass`, `B=fail`) | 0.62 | 0.31 | 0.93 | 0.07 | pA = 0.62/0.93 = 0.667, pB = 0.333 | 0.667 |
| r1 (`A=fail`, `B=pass`) | 0.20 | 0.75 | 0.95 | 0.05 | pA = 0.20/0.95 = 0.211, pB = 0.789 | 0.789 |

- `P(pass) = (0.667 + 0.789) / 2 = 0.728`; `P(fail) = 0.272`.
- Verdict = `pass`. Spread on `pass` = 0.789 − 0.667 = 0.122.
- Confidence (below) = (2 × 0.728 − 1) / 1 = **0.456**.

**Worked example, K = 4.** Options S1…S4 are the four verdict categories in the guardrail question (`A) fully meets spec  B) partially meets  C) misses spec  D) contradicts spec`); the classifier maps them onto pass/warn/block as in 3.4. Rotation `r` puts `S[(i + r) mod 4]` at letter `i`. Renormalized letter probabilities per rotation:

| Rotation | A | B | C | D | Semantic values (S1, S2, S3, S4) |
|---|---|---|---|---|---|
| r0 (S1,S2,S3,S4) | 0.55 | 0.25 | 0.12 | 0.08 | 0.55, 0.25, 0.12, 0.08 |
| r1 (S2,S3,S4,S1) | 0.30 | 0.14 | 0.09 | 0.47 | 0.47, 0.30, 0.14, 0.09 |
| r2 (S3,S4,S1,S2) | 0.10 | 0.07 | 0.61 | 0.22 | 0.61, 0.22, 0.10, 0.07 |
| r3 (S4,S1,S2,S3) | 0.06 | 0.58 | 0.27 | 0.09 | 0.58, 0.27, 0.09, 0.06 |

Each rotation's letters sum to 1.00. Summing each semantic option across its four appearances and dividing by K = 4:

- `P(S1) = (0.55 + 0.47 + 0.61 + 0.58) / 4 = 0.553`
- `P(S2) = (0.25 + 0.30 + 0.22 + 0.27) / 4 = 0.260`
- `P(S3) = (0.12 + 0.14 + 0.10 + 0.09) / 4 = 0.113`
- `P(S4) = (0.08 + 0.09 + 0.07 + 0.06) / 4 = 0.075`

These sum to 1.000. Verdict = S1. Spread on S1 = 0.61 − 0.47 = 0.14. Confidence = (4 × 0.553 − 1) / 3 = **0.403**. Note that the raw letter-A preference in r0 (0.55) is not what decides the answer: S1 is the argmax only after its appearances in the other letter positions are counted.

### 3.3 Confidence score

Confidence is the rescaled winning probability:

`c = (K·p − 1) / (K − 1)`

where `p = P(winner)` after aggregation. This maps the uniform prior `p = 1/K` to `c = 0` and a certain answer `p = 1` to `c = 1`, so the score reads the same for binary and K-way guardrails. If `p < 1/K` (possible only with a degenerate aggregate) it is clamped to 0 and routed to human review.

Worked values:

- K = 2: p = 0.90 → c = 0.80; p = 0.728 → c = 0.456 (example above); p = 0.60 → c = 0.20; p = 0.50 → c = 0.00.
- K = 4: p = 0.70 → c = 0.60; p = 0.553 → c = 0.403 (example above); p = 0.40 → c = 0.20; p = 0.25 → c = 0.00.

The score is a monotone function of `p`, so it does not add information on its own. Its value is that thresholds written as `c ≥ 0.4` mean "meaningfully above chance" for both K = 2 and K = 4. Whether `c` is well calibrated against correctness is an empirical question answered in 3.5, not something the formula guarantees.

### 3.4 Routing thresholds and mapping onto `GuardrailVerdict`

Routing is decided from three signals per verdict: the aggregate confidence `c`, the spread `s = max spread over the winning option`, and the abstain residual `R̄` (mean residual across rotations). The thresholds `T_accept`, `T_review`, `S_max`, `S_hard`, `R_max` are placeholders until fitted on dev data (3.5); the names are what the implementation will expose.

| Route | Condition (all listed clauses must hold) | Semantic result | `GuardrailVerdict.Outcome` |
|---|---|---|---|
| **Auto-accept** | `c ≥ T_accept` and `s ≤ S_max` and `R̄ ≤ R_max` | winning label | `pass` if winner is pass; `warn` if winner is warn; `block` if winner is block |
| **Accept-but-mark** | `T_review ≤ c < T_accept`, or `S_max < s ≤ S_hard`, and `R̄ ≤ R_max` | winning label, flagged low-confidence | winner pass → `warn` (marked); winner warn → `warn`; winner block → `block` (never downgraded) |
| **Human-review** (fail closed) | `c < T_review`, or `s > S_hard`, or `R̄ > R_max`, or any rotation abstains, or logprobs missing | no automated judgment | `block`, with `Category` set so the reason is visible (see below) |

Mapping notes against the existing types in `internal/autonomous/models.go`:

- `GuardrailVerdict.Outcome` is already the string `pass | warn | block` (models.go:711). The classifier does not add a fourth value. Routing into the human-review lane therefore reuses `block`, which is the existing operator-gated path: a block sets the story or task to `blocked` and the PRD to `PRDBlocked` ("awaits operator action", models.go:32), and story/task verdict handling says "Block on any block outcome" (models.go:481, :658). Only `block` halts execution; `warn` does not. That is why accept-but-mark is `warn` for a pass and never a silent pass.
- **Never downgrade a block.** A low-confidence `block` stays a `block`. Lowering it to `warn` would let a low-confidence "this defect exists" pass through unseen, which is the fail-open failure the design rules out. Human-review is the only lane that can resolve it.
- **`Category` is the carrier for *why*.** The BL410 taxonomy in models.go (`infra` vs `content`, lines 716–724) already distinguishes "the judging mechanism failed" from "a real judgment found an issue". Map as follows: missing logprobs, transport error, unsupported node capability, or abstain → `Category: infra`; low confidence or spread/residual gate on a real judgment → `Category: content` with `Summary` stating "classifier low confidence, operator review" and `Issues` carrying `c`, `s`, `R̄`, and the per-rotation vector. This keeps the BL410 invariant that "infra" means the verdict never reached a real judgment.
- **Open check before implementation.** Whether a `block` with `Category: infra` is retried automatically or routed to the operator depends on the BL410 retry path in `executor.go`, which this pass has not traced. P4 must confirm the classifier's infra-blocks take the same path as today's infra-blocks, and that a repeated classifier infra-failure cannot loop silently.
- **Audit fields.** `VerificationResult` already stamps `VerifierBackend/Model/Effort/Source` (models.go:744). The mode proposal in "How it plugs in" adds `VerifierMode`, the aggregated score vector, `c`, `s`, `R̄`, and the route taken, so every verdict can be reproduced from the record without re-running the model.

### 3.5 Calibration

The thresholds are fitted, not chosen by intuition, and they are fitted per model + prompt + quantization combination (3.6 says what invalidates a fit).

**Dev set (held out from the thresholds' own evaluation).**

- Split into a *fit* set (≈ 60%) used to choose thresholds and a *test* set (≈ 40%) used once to report metrics. The test set is never used to tune anything.
- **Ground truth must not come from the classifier.** Labels come from objective sources:
  - *BL422 / model-tiering fixtures* under `testdata/eval/model-tiering/`: `review/` contains `candidate-pass.diff` (the real accepted fix, expected `pass`) and `candidate-fail.diff` (the same fix with one deliberate spec-violating defect, expected `fail`), keyed by `review/expected.json`. Only one pass/fail pair exists today, so it is a seed, not a dev set.
  - *Per-cell verifiers* `verify/{analysis,scripting,docs,review}.sh`: each exits 0 only if the cell meets its spec. Running the verifier over worker outputs produces objective pass/fail labels independent of any judge.
  - *Decompose rubric* (`decompose/rubric.md`): objective, script-checkable scores, usable as labels for decompose-stage guardrails.
  - *BL449 matrix cell verdicts*: **not usable as labels yet.** The `model_result` field is the verifier pass/fail, but the r8 run is recorded as invalid for scoring (16/16 cells suspect, `model_result` null), so no valid scored run exists. This source becomes usable only after a valid BL449 run; until then it contributes nothing to the dev set.
- **Required composition** (proposed minimums, to be revised after P0):
  - Each label (pass / warn / block) represented, with at least 30 items per label in the *fit* set and 15 in the *test* set. `warn` is the hardest class and is over-sampled.
  - Each task type covered: analysis, scripting, docs, review, plus decompose.
  - Positives and negatives from both real accepted work and deliberately injected defects (mutants), so the dev set is not only "obviously good" vs "obviously broken".
  - Hard negatives: diffs that look like they meet the spec but fail the verifier, since these are where a classifier is most likely to be confidently wrong.
  - Each item evaluated under all K rotations, so calibration sees the same aggregation the production path uses.

**Metrics.**

- **Reliability curve and ECE.** Bin the aggregate confidence into 10 equal-width bins. For each bin `b`, compare mean confidence to empirical accuracy. Expected Calibration Error: `ECE = Σ_b (n_b / N) · |acc_b − conf_b|`. The reliability diagram is required output, not just the scalar, because ECE can hide a miscalibrated tail.
- **Accuracy-vs-coverage.** Sort test items by `c` descending; at each coverage level `x` (fraction of items auto-accepted), report accuracy and *block-recall* of the auto-accepted set. The operating point is chosen from this curve, not from ECE alone.
- **Asymmetric error costs.** A false `pass` (a defect let through) is costlier than a false `block` (an operator reviews a fine diff). The fit therefore targets a minimum precision on the `pass` and `block` routes separately (e.g. auto-accept `pass` precision ≥ a set target), and accepts a lower auto-accept coverage in return. The concrete targets are a decision for the operator after P2 numbers exist; this pass only fixes the form.
- **Route rates.** Report the share of items in each route. A classifier that sends everything to human review is "calibrated" trivially and useless; coverage must be reported alongside accuracy.

### 3.6 Re-calibration triggers

Thresholds are invalid for any change that moves the probability distribution. Recalibrate, and do not reuse the old thresholds, when any of the following changes:

- **The model file.** Any change to the GGUF SHA-256 (SCOPE item 4): a new revision, a re-quantization, or a different quant level.
- **The inference stack.** `llama-server` version tag, the chat template, or the handling of `enable_thinking`. A template change can move mass into `<think>` tokens and change residual `R`.
- **The prompt.** Option wording, option ordering scheme, the system prompt, or the spec/diff framing. Any wording change changes the letter-to-semantic mapping and the prior.
- **The sampling/request parameters.** `top_logprobs`, `temperature`, `max_tokens`, alias sets.
- **The node.** Moving the same model to a different host or GPU/CPU backend, since numeric precision can shift the logprobs slightly.
- **Production drift.** The human-review share, or the rate of `warn` at the auto-accept route, moves outside the band observed at fit time for a sustained window. This catches drift without a deliberate change.
- **A periodic re-fit** on the current dev set, even when nothing above changed, at an interval set by the operator.

Each recalibration writes a new calibration record (model SHA, prompt hash, thresholds, reliability curve, coverage table) and the verdict audit fields from 3.4 cite that record, so an old verdict can always be traced to the thresholds that produced it.

### 3.7 Failure modes

Each of these must fail closed to human review (`block`, `Category: infra`), never to `pass` and never to a silent generative fallback. The design's "no fail-open" rule applies to all of them.

1. **Tokenization of option letters.** `A`, ` A` (leading space), and `a` are different tokens, and in some tokenizers a letter is merged with adjacent text or is more than one token. If an option letter is not a single token in the target model's tokenizer, its mass cannot be read from position 0 and the question is invalid for that model. Mitigation: per-model alias sets are built and verified at bind time (P2), and a model whose option tokens do not resolve cleanly is marked `unsupported` for classification.
2. **`top_logprobs` does not cover all options.** The response returns only the top N tokens. If an option token falls outside the top N, the response simply omits it. An omitted option is not the same as zero mass, and treating it as zero would silently distort the renormalization. Rule: if any option (or any alias) is absent from the returned top list, the rotation is treated as **abstain**, not as `p = 0`. `top_logprobs` must be at least the number of option tokens plus their aliases, with headroom (the proposal uses 5 for K ≤ 3).
3. **Server does not return logprobs.** This is the Ollama case already established above: a 200 response with no `logprobs` object. The capability probe (P1) marks the node `unsupported`, and any dispatch of a classifier verdict to an `unsupported` or `unknown` node fails loudly. If logprobs are missing at runtime despite a passing probe (for example a server restart onto a different backend), the verdict is `block` with `Category: infra`. No text fallback is attempted.
4. **Thinking mode not suppressed.** If the template ignores `enable_thinking`, the first token is `<think>` and the option mass is small, so residual `R` becomes large. The abstain gate catches this automatically, but it must be tested explicitly in P6, because a high residual with a plausible-looking argmax is the dangerous case.
5. **Position bias not cancelled.** A large per-semantic-option `spread` (3.2) means letter position still matters after rotation. The spread gate (`S_max`, `S_hard`) routes these to accept-but-mark or human review. A model that fails the spread gate on most items is not suitable as a classifier for that task type.
6. **Injection through the reviewed content.** The spec and diff under review are untrusted text placed inside the classifier prompt. A diff can contain text that asks the judge to answer `A`. Mitigations are framing (the content is delimited and labelled as data), a dev-set item for injected instructions, and the spread/residual gates, which catch many forced answers. This is a mitigation, not a guarantee, and it is listed so the limit is explicit.
7. **Quantization-dependent calibration.** Two quantizations of the same model can calibrate differently. Thresholds are fitted per quant and recorded in the calibration record (3.6); a quant change is a recalibration trigger, not a silent swap.
8. **Logprob numeric precision across backends.** Small differences in logprobs between backends can move borderline items across a threshold. Items near thresholds are the ones to re-check on any node change; P6 should record this as a measured effect rather than assume it is negligible.

---

## SCOPE item 4 — Model sourcing discipline

The classifier only works if the weights are the ones we think they are, and the weights are wired into an agent with file-write and git-commit access. A model that is subtly wrong, or deliberately altered, is a supply-chain input to the guardrail, and a compromised guardrail is worse than no guardrail: it produces confident `pass` verdicts for bad work. This section sets the sourcing rule and the controls that enforce it.

### 4.1 The rule

- **Only the model's own verified publisher namespace on the model hub.** For Qwen3, that is the Qwen organization's repository, and the GGUF used must come from that organization's repo, not a re-upload, a quantization made by someone else, or a merge.
- **No third-party re-uploads, quants, merges, or "fine-tunes" for guardrail use.** Even a faithful-looking re-quantization is a different artifact with a different hash and an unknown build process. If an official quant does not exist at the needed size, the answer is to choose a different size of the official model or to quantize from the official full-precision weights ourselves and record the result, not to take someone else's file.
- **Uncensored or abliterated variants are excluded from the judge role.** These are derivative models whose safety behaviour was deliberately removed. Excluding them is a sourcing rule, not a content judgment: a model whose refusal behaviour was stripped is also the wrong kind of artifact to trust as an independent judge.

### 4.2 The finding (2026-10-15)

On 2026-10-15 a review of the model hub found **more than 100 unverified repositories squatting on the name of another candidate model**, many labelled "uncensored" or "abliterated". This is an operator-reported finding; the underlying hub listing is not reproduced in this repository, so the count and labels are recorded here as reported, not independently verified in-repo. The consequence is the one this section exists to prevent: a model name in a config file or a pull command does not identify the publisher. Anyone can publish under a confusable or identical-looking name, and a plausible name is not evidence of provenance.

### 4.3 Why this is a supply-chain risk here, specifically

Three properties of this deployment make ordinary "it's just weights" thinking insufficient:

1. **Weights are parsed by native code.** The GGUF file is read by `llama-server`, a native C++ program that parses binary metadata and tensors. A malformed or hostile file is a parser-attack surface, so the loader is part of the trust boundary, not only the numbers inside the file.
2. **The chat template travels inside the GGUF.** The template embedded in the file's metadata is rendered by the loader's template engine on every request. It is therefore part of what gets trusted when a file is accepted, and a file from an unknown publisher can change how prompts are rendered.
3. **The model's output drives an agent with write access.** The guardrail verdict gates whether an automated agent's work is accepted, and the same agent family has file-write and git-commit access. A model that is backdoored, or simply altered to answer `pass` on particular inputs, turns the guardrail into a path that approves bad code and commits it. The integrity of the judge is therefore a precondition for the integrity of the repository.

### 4.4 Concrete controls

Each control below is a requirement for P1 or P5 unless marked otherwise. The config keys named here are proposals.

1. **Publisher allowlist.** Each model entry in config carries a `publisher` (the verified organization name, e.g. the Qwen organization for Qwen3). Only allowlisted publishers may be added. The allowlist is owned by config and is not supplied by the model file or by the hub listing. A repository whose owner is not on the list is refused at add time, regardless of how closely its name matches an allowed model.
2. **Pinned revision and file hash.** The entry records the repository's full commit hash (`revision`) and the **SHA-256 of the GGUF file** (`sha256`) and its filename. A floating tag such as `latest` or a branch name is not accepted as a pin.
3. **Verify on load.** Before `llama-server` is started with a model file, the daemon computes the SHA-256 of that file and compares it to the recorded value. A mismatch refuses to start the model, marks the node's classifier capability as not available, and any guardrail routed to it fails closed to human review (`block`, `Category: infra`). There is no fallback model.
4. **`llama-server` from the official source tag only.** The inference binary is built from the official llama.cpp source at a named release tag, with the tag and source commit hash recorded alongside the model entry. It is built locally on the target host. No prebuilt binaries from third parties, and no binaries shipped inside a model repository, are used.
5. **No third-party binaries anywhere in the chain.** This includes helper executables, converters, and tokenizer tools pulled from model repositories. Any tool that must run is built from source under the same tag discipline.
6. **Audit-log entry on model add.** Adding a model writes an entry to the existing audit log with: publisher, repository, `revision`, GGUF filename, `sha256`, declared license, the actor that performed the add, and a timestamp. The audit log is the same facility that backs the existing audit query tools (`internal/mcp/sx_parity.go`, `internal/router/sx2_parity.go`), so the record is queryable alongside other administrative actions. A later load-time hash mismatch is also audit-logged.
7. **Pull paths are validated, not trusted by name.** The Ollama pull path (`runOllamaPull`, `internal/server/marketplace.go` and the compute-node pull handler) passes a model name straight to the Ollama API. Ollama accepts hub references in an `hf.co/<org>/<repo>` form, which lets a name resolve to any repository on the hub. Proposal: the pull path rejects any reference whose publisher is not on the allowlist, and rejects hub-style references unless the publisher is allowlisted.
8. **Re-verification on revision change.** A new `revision` or new `sha256` is a new artifact. It goes through the add flow again (allowlist check, audit entry) and triggers recalibration (3.6). Nothing is updated in place.
9. **Revocation.** Removing a publisher from the allowlist, or an entry's `sha256` being found to be wrong or compromised, blocks load immediately on the next start. The audit log records the revocation and the reason.

### 4.5 Relation to existing conventions

- **The marketplace catalog is name-based today.** `internal/server/marketplace.go` defines `embeddedOllamaCatalog`, whose entries carry `Name`, `Description`, `License`, and a `URL` of the form `ollama.com/library/<name>` (`MarketplaceCatalogEntry`, lines 30–36). Tags carry size and RAM estimates but no publisher, revision, or hash. So the catalog is a discovery list, not a provenance record, and does not satisfy the controls above. Proposal: add `publisher`, `revision`, and `sha256` fields to `MarketplaceCatalogTag`, populate them for any entry used by a guardrail, and have the catalog refuse to offer a guardrail-eligible entry that lacks them. The Ollama library namespace is the catalog's source today; the allowlist is evaluated against the publisher of the specific artifact, not against the catalog listing.
- **CVE and supply-chain conventions in `AGENT.md`.** The existing supply-chain checks are dependency scanners (`gosec`, `govulncheck`, Trivy-based CVE checks, gitleaks) run in CI, with a documented rule for suppressing no-fix CVEs in `.trivyignore`, and a security-failure exception for CVEs older than 72 hours (AGENT.md around lines 886–893). None of these scan model weights. Model artifacts are therefore a *new dependency class* that the current scanners do not cover, and the controls in 4.4 are the only check on them. The pinned `sha256` plays the role that a pinned, checksummed Go module plays for code: an artifact is only as trusted as the digest it is pinned to. Model-artifact triage should follow the same discipline as CVE triage: the operator decides on a recorded basis, not by silent deletion.
- **Operator-gated changes.** Adding a model to a guardrail is a change to the judge, which is the same class of change the design already gates behind operator review (the human-review lane in 3.4). The model-add flow therefore needs an explicit operator confirmation in addition to the allowlist check, rather than running automatically from a catalog pick.

### 4.6 What this section does not decide

- The exact allowlist membership. Publishers are named in config by the operator; this document only fixes that the list exists and who owns it.
- Whether a particular official model is adequate as a classifier. That is a P2 calibration question (3.5), not a sourcing question. A verified model can still fail calibration, and a calibrated model still needs the controls above.
- Whether the operator wants a separate hub-verification step beyond publisher namespace (for example, reviewing the publisher's model card). Possible, but not required by this pass.
