# BL452 — "The V Method": classification-based guardrail verdicts

**Status:** needs_review · **Date:** 2026-10-15 (revised 2026-10-16) · **Tracker:** BL452
**Scope:** design only. No code, config, defaults, model pulls, or node registration. Phases are proposals.
**Line references** were checked against the tree on 2026-10-16.

---

## The technique

Do not ask a model to write a structured answer and then parse it. Pose the decision as a closed multiple-choice question and read the answer as a classification.

- The prompt ends with a fixed option set, for example `A) the diff implements the spec  B) it does not`.
- The verdict is read in one forward pass, in one of two ways:
  1. **Native decision endpoint (preferred).** Ollama's `POST /v1/systemone` returns, per named question, the chosen option, a probability for each option, and optionally a score. There is no generated text to parse, and `output_tokens` is 0.
  2. **Generic logprobs.** Any OpenAI-compatible server that returns logprobs, such as llama.cpp `llama-server`. Send `logprobs: true, top_logprobs: N, max_tokens: 1` and read the first-token probabilities of the option tokens. For Qwen3-style templates, set `enable_thinking: false`.
- The verdict is the argmax. The probability vector is the confidence.

## Why it is useful here

The verifier contract is free text that must parse as strict JSON (`cmd/datawatch/main.go:4910`: `{"ok": <bool>, "severity": ..., ...}`). A model that answers in prose, wraps the JSON in commentary, or spends its budget thinking produces an unparseable reply. The codebase classifies that as `infra` (`internal/autonomous/failure_reason.go`, "unparseable response"). BL449 open-model verifiers hit this failure class.

The V Method removes it for the verdict bit. The output is a probability vector, so there is nothing to parse. Prose explanations stay generative and optional, and a garbled explanation cannot erase a valid verdict.

> The BL449 `qwen3:8b` observation comes from operator runs and the BL452 brief. No per-verifier transcript is checked in. P0 captures one.

## Backend note: Ollama

The earlier version of this doc ruled Ollama out. That was correct for Ollama's generic `/api/generate` and `/v1/chat/completions` endpoints, which accept logprobs parameters and silently return none. It does not hold for the native decision endpoint:

- **`/v1/systemone` shipped in Ollama 0.35.0.** The technique requires Ollama 0.35.0 or later on every node that runs it. Earlier versions do not have the route.
- **Confirmed live on 2026-10-15** against a local Ollama 0.40.5 node using the official `nimble` model. A yes/no guardrail question and an A/B/C quality question in one call returned `correct: {noul: 0.9896}` and `quality: {A: 0.517, B: 0.474, C: 0.0087}`. These matched ground truth.
- Secondary sources agree on the endpoint and the 0.35.0 version. The official Ollama API docs were not re-read for this revision. P1 must pin the documented request and response shape before any code is written.
- The remote `datawatch` compute node is on Ollama 0.33.2 and must be upgraded before use. The local `local-node` node is on 0.40.5.
- Ollaya is a separate third-party project that serves a compatible `/v1/systemone` format. It is not Ollama, and results should not be attributed to Ollama.

**Letter-position bias.** The earlier design spent most of its length on cancelling letter-position bias through rotated options and averaging. That bias is a property of a generic chat model emitting a letter token. A native decision head returns per-option scores, so for `/v1/systemone` the rotation machinery is likely unnecessary. This doc drops it. For the generic logprob path, rotation stays a candidate mitigation. P2 measures whether it is needed before anyone adopts it.

## Where it applies in datawatch

| # | Decision | Where | Fit |
|---|---|---|---|
| 1 | SAST / secrets / deps scan | `internal/autonomous/guardrail_registry.go:30` | Not an LLM decision. Keep deterministic. |
| 2 | `project-rules-scan` | `guardrail_registry.go:53` | Deterministic. Keep. |
| 3 | Scripted pass short-circuit | `executor.go:1520` | Deterministic. Keep. |
| 4 | Task verification `ok` | `cmd/datawatch/main.go:4910`; call site `executor.go:1029`; model resolution `verification_llm.go:53` | **Primary target.** Closed-form. |
| 5 | Task verification `severity` | same prompt | Closed-form, five labels. Secondary target. |
| 6 | Task verification `summary` / `issues` | same prompt | Generative. Keep as an optional separate call. It feeds the retry hint (`executor.go:1575`). |
| 7 | Per-task guardrail outcome | `runPerTaskGuardrails`, `executor.go:1500`; labels `models.go:710-711` | Closed-form `pass / warn / block`. Prose stays generative. |
| 8 | Per-story guardrail outcome | `runPerStoryGuardrails`, `executor.go:1592` | Same as 7. |
| 9 | Verdict `Category` | `models.go:726`; taxonomy `failure_reason.go:36` | Closed-form, but `infra` is assigned by the harness (transport error, missing scores, abstain), never by the model. |
| 10 | `rules-review` | `guardrail_registry.go:67`; `rules_review.go:125` | Discovery stays generative (it needs tools and prose). The classifier may act only as the final gate over its findings. |
| 11 | `ci-watch` | `guardrail_registry.go:80`; `ci_watch.go:535` | Hybrid. Log reading stays generative. The final acceptability call is closed-form. |
| 12 | Content-autofix eligibility | `rules_review.go:178` | Not an LLM decision. Consumes #9. Unchanged. |
| 13 | Decompose, planning, worker code | `autonomous` decompose path | Generative. Out of scope. |
| 14 | Task scope-breadth classification (operator-raised 2026-10-16, see BL431's Open Question 19 extension in its own doc) | A new call site, not yet built: fired on a `content`-category task-verification failure, before deciding whether to retry | Closed-form, two options: "A) one coherent change  B) bundles multiple separable units". Feeds a new `scope_too_broad` signal on `FailureReason` (BL410), which the Automation orchestrator's failure-triage tree (BL431) branches on to auto-split the task via a narrow re-decompose, instead of blind-retrying or escalating. Live motivation: two of BL465's own pilot tasks hit exactly this shape in one night. |

Moving #4, #5, #7, #8, #14, and the final gate of #11 covers the verdict bit of nearly every LLM-judged guardrail plus the one retry-routing decision identified so far outside a guardrail.

## Design

- **A gate, not a worker.** Workers keep full tool access. The classifier only decides verdicts.
- **Per-LLM mode.** Add `verification_mode: classify | generate` to the LLM entry (`internal/inference/llm.go`). The default is `generate`, so nothing changes for existing setups. The override chain (task, story, Automaton, global) and the `Verification*` fields stay as they are.
- **Routing.** Map the result onto the existing `GuardrailVerdict.Outcome` (`pass | warn | block`). Low confidence goes to human review, expressed as `block` with an explanatory `Category`. A block is never downgraded.
- **Fail closed.** Missing scores, timeouts, transport errors, and abstains become `block` with `Category: infra`. There is no silent fallback to generate-and-parse and no fail-open pass.
- **Audit.** Record `VerifierMode`, the score vector, and the route on `VerificationResult` (`models.go:744` area), so a verdict can be reproduced from its record.
- **Calibration.** Fit thresholds per model, prompt, and quantization. Re-calibrate when the model, prompt, chat template, or node changes.
- **Model sourcing.** Judge models come only from the model's official publisher, pinned by revision and SHA-256. Use a publisher allowlist in config. No third-party quants, re-uploads, or abliterated variants. An operator-reported hub review found over 100 squatting repositories. That is unverified in-repo, and it is the reason for the rule. Adding a judge model requires operator confirmation and an audit entry.
- **Parity.** The per-surface decisions are in the Parity surface section below. Five operator-interface surfaces (REST, MCP, CLI, comm channel, YAML/config) are touched in a future implementation. The three client surfaces are decided there on their merits, not assumed.

## Phases

- **P0 — Evidence fixtures.** Check in a replayable open-model unparseable reply, and a native-endpoint response beside a generic-path response for the same prompt.
- **P1 — Endpoint capability.** Probe for `/v1/systemone` (Ollama 0.35.0 or later) and for generic logprobs, with tri-state capability (`unknown | supported | unsupported`). Unsupported or unknown fails loudly. Pin the documented API shape first.
- **P2 — Classifier client and calibration.** Build the request, read the scores, handle token-variant aliases on the generic path, and fit thresholds on a fit/test split. Measure whether rotation is needed on the generic path.
- **P3 — Task-verdict wiring.** Wire `verification_mode` into the verifier for `ok` and `severity`. Add the optional generative explanation call.
- **P4 — Guardrail wiring.** Map per-task and per-story outcomes. Use the classifier only as the final gate for `rules-review` and `ci-watch`. Scan guardrails and scripted passes stay untouched.
- **P5 — Surfaces and docs.** Parity surfaces, `docs/config-reference.yaml`, operations docs, and a how-to without tracker IDs.
- **P6 — Live validation.** Run on a live daemon: the native path on an upgraded remote node, the generic path on `llama-server`, and a deliberate failure on a pre-0.35 node.
- **P7 — Scope-breadth classifier (table row #14, added 2026-10-16).** Depends on P2 (classifier client) existing. Wire the "one unit / bundles multiple units" question into the `content`-category failure path, writing `FailureReason.ScopeTooBroad` (BL410, additive field — no code changes to BL410's existing categories). Does not depend on P3/P4 (task-verdict/guardrail wiring) — this is a separate call site, not a guardrail. The consumer (auto-split on this signal) lives in BL431's orchestrator doc, not here.

## Evaluation plan

This section says how a future implementation would be judged. It does not run anything. The comparison baseline is the current generate-and-parse verifier on the same models, same prompts, and same hardware. Classify mode must beat or match that baseline to justify the feature.

**Status of the data.** The open-model matrix ([BL449](./2026-10-15-bl449-open-model-matrix.md)) has no valid scored run yet. Every run so far was invalid for scoring (see its "Status and history" table). So no metric can be quoted here, and none is quoted.

**Inputs.**

- Models and tasks: the open-model matrix (4 roles by 4 task types), driven by `scripts/eval_open_model_matrix.py`, which reuses the HTTP client and PRD helpers from `scripts/eval_model_tiering.py`.
- Ground truth: the scripted verifiers in `testdata/eval/model-tiering/verify/` (`scripting.sh`, `docs.sh`, `analysis.sh`, `review.sh`, and the `hidden` checks). Their pass/fail verdict is the label. Grading stays deterministic, as in the recurring model eval design ([Test matrix and Grading](./2026-10-14-recurring-model-eval-design.md)).
- The `review` task's fixed `candidate-pass.diff` and `candidate-fail.diff` pair gives labelled inputs for the verdict bit.

**The four measurements.**

(a) **Completion reliability.** For each model and task type, count the infra-block rate: replies that end as `Category: infra` because they are unparseable, empty, or timed out. Run classify mode and generate mode on identical prompts, so the only difference is how the verdict is read. Report both rates side by side.

(b) **Agreement with ground truth.** Compare each mode's verdict to the scripted verdict. Report the false-pass rate (mode passes, script fails) and the false-block rate (mode blocks, script passes). Use a fit/test split. Calibration thresholds are fitted on the fit set only and scored on the test set only. A threshold is never scored on the set that fitted it.

(c) **Latency and cost per decision.** Report p50 and p95 wall time per decision, and tokens per decision. The native decision endpoint reports zero output tokens, because it returns scores and not text. Generate-and-parse spends output tokens on every verdict. The comparison states both numbers plainly, and notes that the optional generative explanation call adds its own cost back.

(d) **CPU-only feasibility.** Can the classify path run on the CPU-only node class (H4, `cpu-only-24c`, the `local-node-cpu` node in the recurring model eval design)? At what p50 and p95 latency per decision? Only models in the CPU budget are in scope. Models of 32B and up are excluded, as in that doc.

**Phases.**

| Item | What it measures | Depends on | Status |
|---|---|---|---|
| E0 | Fixtures and the scoring harness: labelled inputs from the verify scripts, the fit/test split, and a paired classify/generate runner | A valid matrix run to supply real replies; P0 evidence fixtures | Planned |
| E1 | Completion reliability: infra-block and unparseable-reply rate per model and task type, classify vs generate | E0; P1 and P2 for the classify side | Planned |
| E2 | Ground-truth agreement: false-pass and false-block rates on the test split | E0, E1; P2 calibration | Planned |
| E3 | Latency and cost: p50/p95 and tokens per decision, native endpoint vs generate-and-parse | E0; P1 | Planned |
| E4 | CPU-only feasibility: classify latency and agreement on the H4 node class | E3; the minimum Ollama release named in the Backend note on the CPU node | Planned |

## Out of scope

- This pass is design and evaluation only. Implementing the backend is out of scope: the classifier client, the `verification_mode` wiring, endpoint probing, config keys, and surfaces. It is design-only in the same way as the recurring model eval design pass ([BL437](./2026-10-14-recurring-model-eval-design.md)).
- Replacing the generative worker path is out of scope. The worker is unaffected by this feature and still needs full codebase tool access.
- Choosing or pulling judge models.
- Registering or upgrading compute nodes.
- Changing any default.

## Open questions and risks

1. Should `verification_mode` live on the LLM (recommended) or on each override level?
2. Add a plain `openai-compat` LLM kind for the generic path, or reuse `opencode-api`?
3. Should a low-confidence result warn, or escalate to the generative verifier?
4. What is the exact `/v1/systemone` request and response shape in Ollama's official docs?
5. Is letter-position rotation needed on the generic logprob path?
6. How should classify-mode calls count against the inference-load cap and the usage budget? A native-endpoint call spends no output tokens, but it still occupies a node slot. The cap and budget rules are in the [concurrency and budget design](./2026-10-14-concurrency-and-budget-design.md) (inference-load cap, usage-budget throttling). Decide in P1 whether classify calls share the existing `node:<name>` limit or get their own.
7. Should the verdict record (`VerifierMode`, score vector, route) be returned in full by default, or only on request? The score vector is small, but it appears on every surface that shows a verdict. Decide in P5.
8. What happens when a node reports the capability but the model has no decision head (a plain chat model behind the same route)? P1's tri-state capability probe should be per model, not only per node. Decide in P1.
9. Which publishers go on the initial allowlist, and who confirms each addition? This is the operator's call (see the model-sourcing risk below).

### Risks

- **Calibration drift.** Thresholds fitted for one model, prompt, quantization, and node can be wrong after any of them changes. Mitigation: per-combination calibration with a fit/test split (P2), and a re-calibration trigger on any change (Design, Calibration). E2 measures agreement on the held-out set.
- **Minimum Ollama release and lagging nodes.** The route exists only on the minimum Ollama release named in the Backend note, and a remote node can be behind it. Mitigation: tri-state capability probing that fails loudly and never falls back silently (P1), and a deliberate pre-floor failure test on a live daemon (P6).
- **Confident false-pass.** A model that is wrong with high confidence yields a clean-looking pass, which is worse than a garbled reply that fails closed. Mitigation: classify only as the final gate where a deterministic check also exists (P4), never downgrade a block, and track the false-pass rate separately from the false-block rate (E2).
- **Small evaluation set, no valid scored run.** The matrix has produced no valid scored run yet, so no agreement or reliability number can be quoted. Mitigation: no adoption decision until E1 and E2 run on a valid matrix run (Evaluation plan, E0). This doc quotes no metric.
- **Cost bounds.** Classify calls are cheap in tokens but still count toward node concurrency and, on a shared account, toward the usage window. Mitigation: route them through the existing capacity admission rules in the [concurrency and budget design](./2026-10-14-concurrency-and-budget-design.md), and measure latency and tokens per decision (E3). Owner: P1 for the counting rule, E3 for the numbers.
- **Scope-breadth classifier dependency.** The table row 14 classifier (P7) uses the same client, so any client defect or calibration drift also affects the split signal. Mitigation: P7 starts only after P2, has its own calibration set, and writes an additive field only.
- **Model sourcing.** Choosing the publisher allowlist and the pinned revision is an operator decision, and a bad judge model undermines every verdict. Mitigation: publisher allowlist, pinned revision and SHA-256, and operator confirmation plus an audit entry for each addition (Design, Model sourcing). Owner: the operator, with P6 as the live check.

## Parity surface

Per AGENT.md rule 8, this section lists every surface in the full parity-surface set and decides each one. This pass is design-only and implements nothing. These decisions apply to the future implementation PRD, and that PRD must not narrow or drop a surface without a stated reason. The Mobile-Parity Rule applies to the client rows: capability parity is required, implementation parity is not.

| Surface | Decision | Reason |
|---|---|---|
| REST | touched in a future implementation | `verification_mode` is read and set through the LLM endpoints, and the verdict record (`VerifierMode`, score vector, route) is returned on the verification result. |
| MCP | touched in a future implementation | The same setting and verdict record must be reachable through the matching LLM and Automaton MCP tools. No new tool is assumed. |
| CLI | touched in a future implementation | The CLI exposes the setting and prints the verdict record, since it is an operator-facing configuration surface. |
| comm channel | touched in a future implementation | The setting and the verdict record are reachable by chat command, and a low-confidence block has to be reviewable there. |
| YAML/config | touched in a future implementation | `verification_mode` is a per-LLM config field, plus the publisher allowlist and thresholds. It goes in `docs/config-reference.yaml`. |
| PWA | touched in a future implementation | The PWA already shows LLM settings and verdicts. It gains a mode selector and shows the route and score vector, so an operator can see why a task was blocked. |
| Android | touched in a future implementation | Capability parity: the app shows the mode and the verdict record from the same REST contract. Delivery is idiomatic, and a `datawatch-app` issue is filed per the Mobile-Parity Rule. |
| iPhone | touched in a future implementation | Same capability parity as Android, using the same REST contract. Setting the mode on a phone is not required, but reading the verdict record is. |

**Reconciling the Design line.** The Design section's Parity bullet points here. Each surface above is a decision with a reason, not an assumption, so the two statements agree. If the implementation PRD finds a client surface has no operator-visible change, it records that row as `intentionally excluded` with a reason, as the BL449 matrix doc does.

## Cross-references

- [open-model matrix](./2026-10-15-bl449-open-model-matrix.md): supplies the models, tasks, and scripted ground truth the Evaluation plan reuses. It is also where the lack of a valid scored run is recorded.
- [recurring model eval design](./2026-10-14-recurring-model-eval-design.md): defines the deterministic grading and the CPU-only node class that measurement (d) uses.
- [concurrency and budget design](./2026-10-14-concurrency-and-budget-design.md): the inference-load cap and usage-budget rules. This is where cost bounds apply to classify-mode calls.

Same documents by file name, for tools that match links without the `./` prefix: [matrix](2026-10-15-bl449-open-model-matrix.md), [eval design](2026-10-14-recurring-model-eval-design.md), [budget design](2026-10-14-concurrency-and-budget-design.md).
