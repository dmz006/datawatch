// v7.0.0 S2 — OpenWebUI adapter (OpenAI-compatible /v1/chat/completions
// proxy). Ports askOpenWebUI logic.

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

type OpenWebUI struct{}

func (a *OpenWebUI) Kind() inference.Kind { return inference.KindOpenWebUI }

func (a *OpenWebUI) Infer(ctx context.Context, node *compute.Node, llm *inference.LLM, req inference.Request) (inference.Response, error) {
	if node == nil || strings.TrimSpace(node.Address) == "" {
		return inference.Response{}, fmt.Errorf("openwebui: ComputeNode has no address")
	}
	model := inference.ResolveModel(llm, req)
	if model == "" {
		return inference.Response{}, fmt.Errorf("openwebui: no model (set llm.model or pass model_override)")
	}
	messages := []map[string]string{}
	if sys := strings.TrimSpace(req.SystemPrompt); sys != "" {
		messages = append(messages, map[string]string{"role": "system", "content": sys})
	}
	messages = append(messages, map[string]string{"role": "user", "content": req.Prompt})
	body, _ := json.Marshal(map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   false,
	})
	url := strings.TrimRight(node.Address, "/") + "/api/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return inference.Response{}, fmt.Errorf("openwebui: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if llm.APIKeyRef != "" {
		httpReq.Header.Set("Authorization", "Bearer "+llm.APIKeyRef)
	}
	client := &http.Client{Timeout: 0}
	resp, err := client.Do(httpReq)
	if err != nil {
		return inference.Response{}, &inference.ErrTransient{Err: fmt.Errorf("openwebui: %w", err)}
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode >= 500 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return inference.Response{}, &inference.ErrTransient{Err: fmt.Errorf("openwebui HTTP %d: %s", resp.StatusCode, string(buf))}
	}
	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return inference.Response{}, fmt.Errorf("openwebui HTTP %d: %s", resp.StatusCode, string(buf))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return inference.Response{}, fmt.Errorf("openwebui decode: %w", err)
	}
	if len(out.Choices) == 0 {
		return inference.Response{}, fmt.Errorf("openwebui returned no choices")
	}
	return inference.Response{
		Text:      strings.TrimSpace(out.Choices[0].Message.Content),
		UsedModel: model,
	}, nil
}
