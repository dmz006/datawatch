// v7.0.0 S2 — Ollama adapter. Ports the existing askOllama logic
// (internal/server/ask.go) into the dispatcher contract.
//
// Endpoint: POST <node.Address>/api/generate with stream=false.
// Network errors → ErrTransient (failover).
// HTTP 5xx     → ErrTransient.
// HTTP 4xx     → final error (config / prompt issue).

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

type Ollama struct {
	HTTPClientFactory func() *http.Client // nil = default 300s
}

func (a *Ollama) Kind() inference.Kind { return inference.KindOllama }

func (a *Ollama) Infer(ctx context.Context, node *compute.Node, llm *inference.LLM, req inference.Request) (inference.Response, error) {
	if node == nil || strings.TrimSpace(node.Address) == "" {
		return inference.Response{}, fmt.Errorf("ollama: ComputeNode has no address")
	}
	model := inference.ResolveModel(llm, req)
	if model == "" {
		return inference.Response{}, fmt.Errorf("ollama: no model (set llm.model or pass model_override)")
	}
	body, _ := json.Marshal(map[string]any{
		"model":  model,
		"prompt": inference.FormatChatPrompt(req),
		"stream": false,
	})
	url := strings.TrimRight(node.Address, "/") + "/api/generate"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return inference.Response{}, fmt.Errorf("ollama: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	client := a.client()
	resp, err := client.Do(httpReq)
	if err != nil {
		return inference.Response{}, &inference.ErrTransient{Err: fmt.Errorf("ollama: %w", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return inference.Response{}, &inference.ErrTransient{Err: fmt.Errorf("ollama HTTP %d: %s", resp.StatusCode, string(buf))}
	}
	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return inference.Response{}, fmt.Errorf("ollama HTTP %d: %s", resp.StatusCode, string(buf))
	}
	var out struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return inference.Response{}, fmt.Errorf("ollama decode: %w", err)
	}
	return inference.Response{
		Text:      strings.TrimSpace(out.Response),
		UsedModel: model,
	}, nil
}

func (a *Ollama) client() *http.Client {
	if a.HTTPClientFactory != nil {
		return a.HTTPClientFactory()
	}
	return &http.Client{Timeout: 0} // ctx carries the deadline
}
