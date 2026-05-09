// v7.0.0 S2 — REST surface for the LLM-inference registry.
//
//	GET    /api/llms              list all LLMs
//	POST   /api/llms              create
//	GET    /api/llms/{name}       fetch one
//	PUT    /api/llms/{name}       replace
//	DELETE /api/llms/{name}       delete
//	POST   /api/llms/{name}/test  one-shot inference test against this LLM
//
// Returns 503 when no inference dispatcher is wired.

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/inference"
)

// SetInference wires the runtime registry + dispatcher into the server.
func (s *Server) SetInference(reg *inference.Registry, disp *inference.Dispatcher) {
	s.inferenceReg = reg
	s.inferenceDisp = disp
}

// Dispatcher returns the wired dispatcher (nil if none). Used by
// other server surfaces (ask, council_drafts) to route through the
// registry instead of calling askOllama / askOpenWebUI directly.
func (s *Server) Dispatcher() *inference.Dispatcher { return s.inferenceDisp }

// LLMRegistry returns the wired registry (nil if none).
func (s *Server) LLMRegistry() *inference.Registry { return s.inferenceReg }

func (s *Server) handleLLMs(w http.ResponseWriter, r *http.Request) {
	if s.inferenceReg == nil {
		http.Error(w, "llm registry disabled", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/llms")
	rest = strings.TrimPrefix(rest, "/")

	switch {
	case rest == "" && r.Method == http.MethodGet:
		writeJSONOK(w, map[string]any{"llms": s.inferenceReg.List()})

	case rest == "" && r.Method == http.MethodPost:
		var l inference.LLM
		if err := json.NewDecoder(r.Body).Decode(&l); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.inferenceReg.Add(&l); err != nil {
			code := http.StatusBadRequest
			if err == inference.ErrConflict {
				code = http.StatusConflict
			}
			http.Error(w, err.Error(), code)
			return
		}
		s.auditLLM(l.Name, "llm_add")
		writeJSONOK(w, map[string]any{"name": l.Name, "ok": true})

	case strings.HasSuffix(rest, "/test") && r.Method == http.MethodPost:
		s.handleLLMTest(w, r, strings.TrimSuffix(rest, "/test"))

	case rest != "" && r.Method == http.MethodGet:
		l, err := s.inferenceReg.Get(rest)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSONOK(w, l)

	case rest != "" && r.Method == http.MethodPut:
		var l inference.LLM
		if err := json.NewDecoder(r.Body).Decode(&l); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if l.Name == "" {
			l.Name = rest
		}
		if l.Name != rest {
			http.Error(w, "name mismatch (path vs body)", http.StatusBadRequest)
			return
		}
		if err := s.inferenceReg.Update(&l); err != nil {
			code := http.StatusBadRequest
			if err == inference.ErrNotFound {
				code = http.StatusNotFound
			}
			http.Error(w, err.Error(), code)
			return
		}
		s.auditLLM(l.Name, "llm_update")
		writeJSONOK(w, map[string]any{"name": l.Name, "ok": true})

	case rest != "" && r.Method == http.MethodDelete:
		if err := s.inferenceReg.Delete(rest); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		s.auditLLM(rest, "llm_delete")
		writeJSONOK(w, map[string]any{"name": rest, "ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleLLMTest exercises one inference call to verify the LLM
// definition is valid + adapter reachable. Body: {prompt: "..."}.
func (s *Server) handleLLMTest(w http.ResponseWriter, r *http.Request, name string) {
	if s.inferenceDisp == nil {
		http.Error(w, "inference dispatcher disabled", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		body.Prompt = "Reply with the single word OK so we can verify reachability."
	}
	llm, err := s.inferenceReg.Get(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	timeout := inference.ResolveTimeout(llm)
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	resp, err := s.inferenceDisp.Call(ctx, name, inference.Request{Prompt: body.Prompt, Consumer: "test"})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.auditLLM(name, "llm_test")
	writeJSONOK(w, map[string]any{
		"text":         resp.Text,
		"used_node":    resp.UsedNode,
		"used_model":   resp.UsedModel,
		"backend":      resp.Backend,
		"duration_ms":  resp.DurationMs,
	})
}

func (s *Server) auditLLM(name, action string) {
	if s.auditLog == nil {
		return
	}
	_ = s.auditLog.Write(audit.Entry{
		Actor:  "operator",
		Action: action,
		Details: map[string]any{
			"resource_type": "llm",
			"resource_id":   name,
		},
	})
}

// silence unused
var _ = time.Now
