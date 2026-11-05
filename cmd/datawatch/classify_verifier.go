package main

import (
	"context"
	"fmt"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// classifyRuntimeProvider builds the classify-mode runtime (classifier client,
// calibration store, route, fingerprint and optional explanation call) for the
// node/backend/model that serves one verification. The daemon installs it when
// a classifier surface is wired; while it is nil a classify-mode verification
// fails closed with a setup verdict. There is no fallback to generate mode.
var classifyRuntimeProvider func(node, backend, model string) (autonomouspkg.ClassifyRuntime, error)

// runClassifyVerification runs one verification in classify mode and returns
// its verdict. Every failure is a block.
func runClassifyVerification(ctx context.Context, node, backend, model, prompt string, recordScores bool) autonomouspkg.VerificationResult {
	if classifyRuntimeProvider == nil {
		return autonomouspkg.ClassifyVerify(ctx, autonomouspkg.ClassifyRuntime{}, prompt, time.Now())
	}
	rt, err := classifyRuntimeProvider(node, backend, model)
	if err != nil {
		return autonomouspkg.VerificationResult{
			OK: false, Severity: "medium", Category: autonomouspkg.ClassifyCategoryInfra,
			Summary:      fmt.Sprintf("verifier: classify runtime for %q/%q unavailable: %v", backend, model, err),
			VerifiedAt:   time.Now(),
			VerifierMode: autonomouspkg.VerifierModeClassify,
		}
	}
	rt.RecordScores = recordScores
	return autonomouspkg.ClassifyVerify(ctx, rt, prompt, time.Now())
}

// runScopeBreadth runs the scope-breadth read through the same runtime provider
// as classify-mode verification, with the scope question's own fingerprint. A
// missing provider or runtime is an infra note, never a block.
func runScopeBreadth(ctx context.Context, text string) autonomouspkg.ScopeBreadthResult {
	if classifyRuntimeProvider == nil {
		return autonomouspkg.ScopeBreadthResult{InfraNote: "scope-breadth: no classifier runtime"}
	}
	rt, err := classifyRuntimeProvider("", "", "")
	if err != nil {
		return autonomouspkg.ScopeBreadthResult{InfraNote: fmt.Sprintf("scope-breadth: classifier runtime unavailable: %v", err)}
	}
	rt.Fingerprint = autonomouspkg.ScopeBreadthFingerprint(rt.Fingerprint)
	return autonomouspkg.ClassifyScopeBreadth(ctx, rt, text)
}
