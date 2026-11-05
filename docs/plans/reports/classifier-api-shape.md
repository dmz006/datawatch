# Classifier API shape, capability probe and load-limit decision

**Scope:** `internal/classifier`. Design source:
`docs/plans/2026-10-15-logprob-classification-guardrail-design.md`.

## Confirmation status

The official Ollama documentation was **not re-read** for this change. The shapes below
are reconstructed from the design document's Backend note (one live call that returned,
per named question, the chosen option and a probability per option) and from the
OpenAI chat-completions logprobs format. **The operator must confirm every field name and
the request shape against the official docs before any non-fixture use.** This answers
design open question 4 only provisionally. The minimum Ollama release is the one named in
the Backend note of the design document; it is not restated here.

All fixtures under `internal/classifier/testdata/` are hand-authored, labelled
`SYNTHETIC`, and were not recorded from a live endpoint.

## Native decision endpoint: `POST {base}/v1/systemone`

```
request:  {"model": "<model>", "prompt": "<text ending in the option set>",
           "questions": [{"name": "<id>", "options": ["A", "B"]}]}
response: {"model": "<model>",
           "questions": {"<id>": {"choice": "A",
                                  "probabilities": {"A": 0.93, "B": 0.07},
                                  "score": 0.93}},
           "usage": {"input_tokens": N, "output_tokens": 0}}
```

`score` is optional. `output_tokens` is 0. The Go types are in `shape.go`.

## Generic logprobs path: `POST {base}/v1/chat/completions`

Send `logprobs: true, top_logprobs: N, max_tokens: 1` (and `enable_thinking: false` for
Qwen3-style templates). The verdict is `choices[0].logprobs.content[0].top_logprobs`.

## Fail-closed rules

Missing scores, timeouts, transport errors and abstains are a block with category
`infra` (`classifier.FailureCategory`). A block is never downgraded. There is no silent
fallback to generate-and-parse.

## Capability probe

State is tri-state `unknown | supported | unsupported`, probed per **(node, model, path)**
for `native` and `logprobs`, through an injectable HTTP client.

| Probe result | State |
|---|---|
| 2xx with the pinned shape | supported |
| 404, 405, 501, 400, or 2xx without the needed fields | unsupported |
| transport error, timeout, other non-2xx, node unresolvable | unknown (carries its cause) |

`Require` returns a typed `*CapabilityError` unless the state is supported. Supported and
unsupported results are cached for `classifier.probe_ttl` (default 10m); **unknown is never
cached**, so the next call probes again. `Invalidate(node)` drops a node's entries.
Probe timeout is `classifier.probe_timeout` (default 5s).

## Existing parser behaviour (correction to the task premise)

The task asked for a replay test showing an unparseable verifier reply is classified as an
**infra** failure by the existing behaviour. That is not what the code does.
`internal/autonomous/failure_reason.go` only defines the `FailureReason` struct and its
category documentation; the parse lives in `cmd/datawatch` (`extractJSON` plus
`json.Unmarshal`). A reply that was **written but cannot be parsed** is deliberately the
terminal category `invalid-response`, not `infra`: an infra block re-runs the whole judge
and re-delivers the same consumed output (see `cmd/datawatch/bl410_failure_classification_test.go`).
`infra` is reserved for no-output failures. `TestReplayUnparseableReplyExistingBehaviour`
therefore pins the real behaviour (strict parse fails, `ClassifyInfraText` leaves it
unclassified, `main.go` still stamps `invalid-response`). The classifier's own failures
are `infra`. Whether the V Method path should stamp a still-unparseable legacy reply as
`infra` is a decision for the operator and is left unchanged.

## Decision: classify calls share the `node:<name>` inference-load limit

**Yes, they share it.** `internal/inference/budget.go` is a weekly budget window and
`dispatcher.go` holds no per-node limit; the node limit is the `node:<name>` pool in
`internal/capacity` (`Request.Pools`, `withNode`). A classify call is one short forward
pass, but it occupies the same device as generate work and can force a model load. Exempting
it would let verdict reads starve or thrash the work they judge, and would make the
"lane" accounting wrong. So a classify call takes one slot of `node:<name>` for its
duration.

Implemented as `classifier.Gate` (acquire a slot of pool `node:<node>`, return a release)
and `classifier.WithGate`. A nil gate, a refusal, or an unreachable pool is a typed
`*GateError`, which is an `infra` block; a classify call is never run outside the limit.
Wiring `Gate` to the capacity ledger is not part of this change.

## Model sourcing

The judge-model publisher allowlist, the pinned entries (model, publisher, revision,
sha256) and the operator confirmation with an audit entry are provided by the model-pull
consent work (the requirements Automaton), not by this Automaton.

The classifier here takes the allowlist as an interface: a function that answers whether a
model is allowed. It ships with no allowlist content: no model, publisher or pin is named
or defaulted in this change.
