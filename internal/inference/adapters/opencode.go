// v7.0.0 S2 — opencode adapter. opencode is a wrapper around Ollama's
// /api/generate protocol — same wire as the Ollama adapter, just with
// a different label so operators can distinguish "this LLM is served
// by opencode (with its tooling stack)" from "raw ollama".
//
// Operator-decided 2026-05-08 (BL295 design Q21): every currently-
// supported LLM kind must be in v7.0.0. opencode is currently used
// via AgentSettings.OpenCodeOllamaURL + OpenCodeModel.

package adapters

import (
	"context"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
)

type OpenCode struct {
	// Embeds Ollama because the wire protocol is identical.
	inner Ollama
}

func (a *OpenCode) Kind() inference.Kind { return inference.KindOpenCode }

func (a *OpenCode) Infer(ctx context.Context, node *compute.Node, llm *inference.LLM, req inference.Request) (inference.Response, error) {
	resp, err := a.inner.Infer(ctx, node, llm, req)
	if err == nil {
		// Tag the response with the right Backend label.
		resp.Backend = inference.KindOpenCode
	}
	return resp, err
}
