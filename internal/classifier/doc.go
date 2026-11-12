// Package classifier reads a guardrail verdict as a closed multiple-choice
// classification instead of generating text and parsing it.
//
// Two read paths exist: the native decision endpoint (POST /v1/systemone) and
// generic OpenAI-compatible logprobs. This package holds the pinned wire
// shapes (shape.go), the replayable evidence fixtures (fixtures.go), the
// per-model, per-node capability probe (capability.go) and the node load gate
// (gate.go). It never falls back silently: an unknown or unsupported
// capability, a missing score, a timeout, a transport error or an abstain is a
// typed error the caller turns into a block with the "infra" category.
//
// Design: docs/plans/2026-10-15-logprob-classification-guardrail-design.md.
// Wire shapes and decisions: docs/plans/reports/classifier-api-shape.md.
package classifier
