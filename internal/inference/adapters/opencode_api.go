// BL321 — OpenCodeAPI adapter: opencode HTTP inference API.
//
// opencode in API mode exposes an OpenAI-compatible /v1/chat/completions
// endpoint. Identical wire protocol to OpenWebUI but with its own Kind.

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

// OpenCodeAPI implements inference.Adapter for opencode's HTTP inference API.
type OpenCodeAPI struct{}

func (a *OpenCodeAPI) Kind() inference.Kind { return inference.KindOpenCodeAPI }

func (a *OpenCodeAPI) Infer(ctx context.Context, node *compute.Node, llm *inference.LLM, req inference.Request) (inference.Response, error) {
	if node == nil || strings.TrimSpace(node.Address) == "" {
		return inference.Response{}, fmt.Errorf("opencode-api: ComputeNode has no address")
	}
	model := inference.ResolveModel(llm, req)
	if model == "" {
		return inference.Response{}, fmt.Errorf("opencode-api: no model (set llm.model or pass model_override)")
	}
	messages := []map[string]string{}
	if sys := strings.TrimSpace(req.SystemPrompt); sys != "" {
		messages = append(messages, map[string]string{"role": "system", "content": sys})
	}
	messages = append(messages, map[string]string{"role": "user", "content": req.Prompt})
	body, _ := json.Marshal(map[string]any{"model": model, "messages": messages, "stream": false})
	url := strings.TrimRight(node.Address, "/") + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return inference.Response{}, fmt.Errorf("opencode-api: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if llm.APIKeyRef != "" {
		httpReq.Header.Set("Authorization", "Bearer "+llm.APIKeyRef)
	}
	client := &http.Client{Timeout: 0}
	resp, err := client.Do(httpReq)
	if err != nil {
		return inference.Response{}, &inference.ErrTransient{Err: fmt.Errorf("opencode-api: %w", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return inference.Response{}, &inference.ErrTransient{Err: fmt.Errorf("opencode-api HTTP %d: %s", resp.StatusCode, string(buf))}
	}
	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return inference.Response{}, fmt.Errorf("opencode-api HTTP %d: %s", resp.StatusCode, string(buf))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return inference.Response{}, fmt.Errorf("opencode-api: decode: %w", err)
	}
	if len(out.Choices) == 0 {
		return inference.Response{}, fmt.Errorf("opencode-api: empty response")
	}
	return inference.Response{
		Text:      strings.TrimSpace(out.Choices[0].Message.Content),
		UsedModel: model,
		Backend:   inference.KindOpenCodeAPI,
	}, nil
}
