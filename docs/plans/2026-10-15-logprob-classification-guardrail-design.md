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
- The remote `datawatch` compute node is on Ollama 0.33.2 and must be upgraded before use. The local `johnnyjohnny` node is on 0.40.5.
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

Moving #4, #5, #7, #8, and the final gate of #11 covers the verdict bit of nearly every LLM-judged guardrail.

## Design

- **A gate, not a worker.** Workers keep full tool access. The classifier only decides verdicts.
- **Per-LLM mode.** Add `verification_mode: classify | generate` to the LLM entry (`internal/inference/llm.go`). The default is `generate`, so nothing changes for existing setups. The override chain (task, story, Automaton, global) and the `Verification*` fields stay as they are.
- **Routing.** Map the result onto the existing `GuardrailVerdict.Outcome` (`pass | warn | block`). Low confidence goes to human review, expressed as `block` with an explanatory `Category`. A block is never downgraded.
- **Fail closed.** Missing scores, timeouts, transport errors, and abstains become `block` with `Category: infra`. There is no silent fallback to generate-and-parse and no fail-open pass.
- **Audit.** Record `VerifierMode`, the score vector, and the route on `VerificationResult` (`models.go:744` area), so a verdict can be reproduced from its record.
- **Calibration.** Fit thresholds per model, prompt, and quantization. Re-calibrate when the model, prompt, chat template, or node changes.
- **Model sourcing.** Judge models come only from the model's official publisher, pinned by revision and SHA-256. Use a publisher allowlist in config. No third-party quants, re-uploads, or abliterated variants. An operator-reported hub review found over 100 squatting repositories. That is unverified in-repo, and it is the reason for the rule. Adding a judge model requires operator confirmation and an audit entry.
- **Parity.** REST, MCP, CLI, comm channel, YAML, PWA, Android, and iOS are all touched at implementation time. The detail belongs in the implementation PRD.

## Phases

- **P0 — Evidence fixtures.** Check in a replayable open-model unparseable reply, and a native-endpoint response beside a generic-path response for the same prompt.
- **P1 — Endpoint capability.** Probe for `/v1/systemone` (Ollama 0.35.0 or later) and for generic logprobs, with tri-state capability (`unknown | supported | unsupported`). Unsupported or unknown fails loudly. Pin the documented API shape first.
- **P2 — Classifier client and calibration.** Build the request, read the scores, handle token-variant aliases on the generic path, and fit thresholds on a fit/test split. Measure whether rotation is needed on the generic path.
- **P3 — Task-verdict wiring.** Wire `verification_mode` into the verifier for `ok` and `severity`. Add the optional generative explanation call.
- **P4 — Guardrail wiring.** Map per-task and per-story outcomes. Use the classifier only as the final gate for `rules-review` and `ci-watch`. Scan guardrails and scripted passes stay untouched.
- **P5 — Surfaces and docs.** Parity surfaces, `docs/config-reference.yaml`, operations docs, and a how-to without tracker IDs.
- **P6 — Live validation.** Run on a live daemon: the native path on an upgraded remote node, the generic path on `llama-server`, and a deliberate failure on a pre-0.35 node.

**Evaluation (not yet run).** Measure, per model and task type: infra-block rate, false-pass and false-block rates against the verifier scripts (`testdata/eval/model-tiering/verify/*.sh`), and p50/p95 latency on GPU and CPU nodes. Use a fit/test split. BL449 has no valid scored run yet, so no metric can be computed.

## Open questions

1. Should `verification_mode` live on the LLM (recommended) or on each override level?
2. Add a plain `openai-compat` LLM kind for the generic path, or reuse `opencode-api`?
3. Should a low-confidence result warn, or escalate to the generative verifier?
4. What is the exact `/v1/systemone` request and response shape in Ollama's official docs?
5. Is letter-position rotation needed on the generic logprob path?
