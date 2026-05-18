// BL320 — /api/proxy/llm/<name> inference proxy endpoint.
//
// Allows a federation peer (or admin token) to invoke an LLM on this
// datawatch instance by name. Used by ProxyRouter in the datawatch-proxy
// routing mode so federated instances can delegate inference to peers.
//
//	POST /api/proxy/llm/{name}
//	Body: {"prompt":"...","system_prompt":"...","model":"..."}
//	Response: {"text":"...","used_model":"...","duration_ms":N}

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/inference"
)

// handleProxyLLM serves POST /api/proxy/llm/<name>.
// Requires sessions:input or full-control capability.
func (s *Server) handleProxyLLM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Require sessions:input capability (allows federation peers with that cap
	// to drive inference remotely; admin token always passes).
	if !s.fedCap(w, r, federation.CapSessionsInput) {
		return
	}
	if s.inferenceDisp == nil {
		http.Error(w, "inference dispatcher disabled", http.StatusServiceUnavailable)
		return
	}

	// Parse the LLM name from the path: /api/proxy/llm/<name>
	name := strings.TrimPrefix(r.URL.Path, "/api/proxy/llm/")
	name = strings.TrimSuffix(name, "/")
	if name == "" {
		http.Error(w, "llm name required in path", http.StatusBadRequest)
		return
	}

	var body struct {
		Prompt       string `json:"prompt"`
		SystemPrompt string `json:"system_prompt"`
		Model        string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}

	llm, err := s.inferenceReg.Get(name)
	if err != nil {
		http.Error(w, "llm not found: "+name, http.StatusNotFound)
		return
	}

	timeout := inference.ResolveTimeout(llm)
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	resp, err := s.inferenceDisp.Call(ctx, name, inference.Request{
		Prompt:        body.Prompt,
		SystemPrompt:  body.SystemPrompt,
		ModelOverride: body.Model,
		Consumer:      "proxy",
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"text":        resp.Text,
		"used_model":  resp.UsedModel,
		"duration_ms": resp.DurationMs,
	})
}
