// BL34 — read-only `ask` endpoint.
//
// POST /api/ask body: {"question": "...", "backend": "ollama|openwebui"}
// Sends a single-shot prompt to the chosen chat-backend (Ollama or
// OpenWebUI) and returns the response inline. No tmux session, no
// persistent state — fast path for quick questions.
//
// Returns 503 when the backend isn't configured/reachable, 400 for
// invalid input, 500 on backend error.

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AskRequest is the wire form of POST /api/ask.
type AskRequest struct {
	Question string `json:"question"`
	Backend  string `json:"backend,omitempty"` // "ollama" (default) | "openwebui"
	Model    string `json:"model,omitempty"`   // optional override
}

// AskResponse is what we emit on success.
type AskResponse struct {
	Backend  string `json:"backend"`
	Model    string `json:"model,omitempty"`
	Answer   string `json:"answer"`
	DurationMs int64 `json:"duration_ms"`
}

func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req AskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" {
		http.Error(w, "question required", http.StatusBadRequest)
		return
	}
	if req.Backend == "" {
		req.Backend = "ollama"
	}

	start := time.Now()
	var (
		answer string
		err    error
	)
	switch req.Backend {
	case "ollama":
		answer, err = askOllama(s, req)
	case "openwebui":
		answer, err = askOpenWebUI(s, req)
	default:
		http.Error(w, "unsupported backend: "+req.Backend, http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(AskResponse{
		Backend:    req.Backend,
		Model:      req.Model,
		Answer:     answer,
		DurationMs: time.Since(start).Milliseconds(),
	})
}

// askOllama posts to the configured Ollama /api/generate endpoint
// with stream=false, returning the response field.
func askOllama(s *Server, req AskRequest) (string, error) {
	if s.cfg == nil || s.cfg.Ollama.Host == "" {
		return "", fmt.Errorf("ollama not configured (set ollama.host)")
	}
	model := req.Model
	if model == "" {
		model = s.cfg.Ollama.Model
	}
	if model == "" {
		return "", fmt.Errorf("no model: pass `model` or set ollama.model")
	}
	body, _ := json.Marshal(map[string]any{
		"model":  model,
		"prompt": req.Question,
		"stream": false,
	})
	url := strings.TrimRight(s.cfg.Ollama.Host, "/") + "/api/generate"
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// v5.26.9 — bumped 60s → 300s. First-token latency on a cold model
	// (especially ollama qwen3:8b+ on a busy host) regularly exceeds
	// 60s, which used to surface as "context deadline exceeded" half-
	// way through autonomous decompose. Operator-reported when
	// validating claude-code autonomous end-to-end.
	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("ollama: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama HTTP %d: %s", resp.StatusCode, string(buf))
	}
	var out struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("ollama decode: %w", err)
	}
	return strings.TrimSpace(out.Response), nil
}

// askOpenWebUI posts to the configured OpenWebUI /api/chat/completions
// endpoint (OpenAI-compatible). Returns choices[0].message.content.
func askOpenWebUI(s *Server, req AskRequest) (string, error) {
	if s.cfg == nil || s.cfg.OpenWebUI.URL == "" {
		return "", fmt.Errorf("openwebui not configured (set openwebui.url)")
	}
	model := req.Model
	if model == "" {
		model = s.cfg.OpenWebUI.Model
	}
	if model == "" {
		return "", fmt.Errorf("no model: pass `model` or set openwebui.model")
	}
	body, _ := json.Marshal(map[string]any{
		"model":    model,
		"messages": []map[string]string{{"role": "user", "content": req.Question}},
		"stream":   false,
	})
	url := strings.TrimRight(s.cfg.OpenWebUI.URL, "/") + "/api/chat/completions"
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if s.cfg.OpenWebUI.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+s.cfg.OpenWebUI.APIKey)
	}
	// v5.26.9 — bumped 60s → 300s. First-token latency on a cold model
	// (especially ollama qwen3:8b+ on a busy host) regularly exceeds
	// 60s, which used to surface as "context deadline exceeded" half-
	// way through autonomous decompose. Operator-reported when
	// validating claude-code autonomous end-to-end.
	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("openwebui: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		buf, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openwebui HTTP %d: %s", resp.StatusCode, string(buf))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("openwebui decode: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("openwebui returned no choices")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}
