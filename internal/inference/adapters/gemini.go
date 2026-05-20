// BL321 — Gemini adapter: Google Generative Language v1beta API.
//
// POST https://generativelanguage.googleapis.com/v1beta/models/<model>:generateContent?key=<api_key>
// Body: {"contents":[{"parts":[{"text":"prompt"}]}],"systemInstruction":{"parts":[{"text":"sys"}]}}
// Response: .candidates[0].content.parts[0].text

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

// Gemini implements the inference.Adapter for Google Generative Language v1beta.
type Gemini struct{}

func (a *Gemini) Kind() inference.Kind { return inference.KindGeminiAPI }

func (a *Gemini) Infer(ctx context.Context, node *compute.Node, llm *inference.LLM, req inference.Request) (inference.Response, error) {
	apiKey := strings.TrimSpace(llm.APIKeyRef)
	if apiKey == "" {
		return inference.Response{}, fmt.Errorf("gemini: api_key_ref required")
	}
	model := inference.ResolveModel(llm, req)
	if model == "" {
		model = "gemini-1.5-flash"
	}
	endpoint := "https://generativelanguage.googleapis.com"
	if node != nil && strings.TrimSpace(node.Address) != "" {
		endpoint = strings.TrimRight(node.Address, "/")
	}

	contents := []map[string]any{
		{"parts": []map[string]string{{"text": req.Prompt}}},
	}
	body := map[string]any{"contents": contents}
	if sys := strings.TrimSpace(req.SystemPrompt); sys != "" {
		body["systemInstruction"] = map[string]any{
			"parts": []map[string]string{{"text": sys}},
		}
	}
	bodyJSON, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", endpoint, model, apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyJSON))
	if err != nil {
		return inference.Response{}, fmt.Errorf("gemini: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 0}
	resp, err := client.Do(httpReq)
	if err != nil {
		return inference.Response{}, &inference.ErrTransient{Err: fmt.Errorf("gemini: %w", err)}
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return inference.Response{}, &inference.ErrTransient{Err: fmt.Errorf("gemini HTTP %d: %s", resp.StatusCode, string(buf))}
	}
	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return inference.Response{}, fmt.Errorf("gemini HTTP %d: %s", resp.StatusCode, string(buf))
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return inference.Response{}, fmt.Errorf("gemini: decode: %w", err)
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return inference.Response{}, fmt.Errorf("gemini: empty response")
	}
	return inference.Response{
		Text:      strings.TrimSpace(out.Candidates[0].Content.Parts[0].Text),
		UsedModel: model,
		Backend:   inference.KindGeminiAPI,
	}, nil
}
