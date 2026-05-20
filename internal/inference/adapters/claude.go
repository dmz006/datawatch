// v7.0.0 S2 — Anthropic Claude adapter. Calls Anthropic's Messages API
// directly (cloud kind; ComputeNode is informational only — endpoint
// is api.anthropic.com unless llm.ComputeNodes contains an enterprise-
// proxy entry that overrides it via Address).
//
// Operator-decided 2026-05-08 (BL295 design Q21): every currently-
// supported LLM kind must be in v7.0.0. claude-code agents inject
// ANTHROPIC_API_KEY today; the LLM registry replaces that with a
// declarative `llm.api_key_ref` (literal string OR ${secret:name}
// reference resolved at call time).
//
// Endpoint: POST <node.Address-or-default>/v1/messages with
//   x-api-key: <api_key>     (required)
//   anthropic-version: 2023-06-01

package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
)

const defaultAnthropicEndpoint = "https://api.anthropic.com"

type Claude struct{}

func (a *Claude) Kind() inference.Kind { return inference.KindClaude }

func (a *Claude) Infer(ctx context.Context, node *compute.Node, llm *inference.LLM, req inference.Request) (inference.Response, error) {
	apiKey := strings.TrimSpace(llm.APIKeyRef)
	if apiKey == "" {
		return inference.Response{}, fmt.Errorf("claude: api_key_ref required (set llm.api_key_ref to literal key or ${secret:name})")
	}
	model := inference.ResolveModel(llm, req)
	if model == "" {
		return inference.Response{}, fmt.Errorf("claude: no model (set llm.model or pass model_override)")
	}
	endpoint := defaultAnthropicEndpoint
	if node != nil && strings.TrimSpace(node.Address) != "" {
		endpoint = strings.TrimRight(node.Address, "/")
	}

	body := map[string]any{
		"model":      model,
		"max_tokens": 4096,
		"messages":   []map[string]string{{"role": "user", "content": req.Prompt}},
	}
	if sys := strings.TrimSpace(req.SystemPrompt); sys != "" {
		body["system"] = sys
	}
	bodyJSON, _ := json.Marshal(body)
	url := endpoint + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyJSON))
	if err != nil {
		return inference.Response{}, fmt.Errorf("claude: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	client := &http.Client{Timeout: 0}
	resp, err := client.Do(httpReq)
	if err != nil {
		return inference.Response{}, &inference.ErrTransient{Err: fmt.Errorf("claude: %w", err)}
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return inference.Response{}, &inference.ErrTransient{Err: fmt.Errorf("claude HTTP %d: %s", resp.StatusCode, string(buf))}
	}
	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return inference.Response{}, fmt.Errorf("claude HTTP %d: %s", resp.StatusCode, string(buf))
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return inference.Response{}, fmt.Errorf("claude decode: %w", err)
	}
	var text strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	return inference.Response{
		Text:      strings.TrimSpace(text.String()),
		UsedModel: model,
	}, nil
}
