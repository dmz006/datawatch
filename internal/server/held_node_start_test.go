package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
)

// heldNodeServer wires a server with two Ollama-kind compute nodes (alpha
// first, beta second) and a pi-prompt inference entry "pi-gpu" that lists both.
func heldNodeServer(t *testing.T) *Server {
	t.Helper()
	srv := newLLMServer(t)
	// In-memory tmux fake: handleStartSession never touches a real multiplexer.
	srv.manager.WithFakeTmux()

	reg, err := compute.NewRegistry(filepath.Join(t.TempDir(), "nodes.json"))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	for _, n := range []*compute.Node{
		{Name: "alpha", Kind: compute.KindOllama, Address: "10.0.0.1:11434"},
		{Name: "beta", Kind: compute.KindOllama, Address: "10.0.0.2:11434"},
	} {
		if err := reg.Add(n); err != nil {
			t.Fatalf("Add %s: %v", n.Name, err)
		}
	}
	srv.computeReg = reg
	if err := srv.inferenceReg.Add(&inference.LLM{
		Name:         "pi-gpu",
		Kind:         inference.KindPiPrompt,
		ComputeNodes: []string{"alpha", "beta"},
	}); err != nil {
		t.Fatalf("Add pi-gpu: %v", err)
	}
	return srv
}

// startHeldNodeSession posts body to handleStartSession, requires a 200, and
// returns the started session's record.
func startHeldNodeSession(t *testing.T, srv *Server, body map[string]any) (*httptest.ResponseRecorder, string) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(raw))
	w := httptest.NewRecorder()
	srv.handleStartSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("handleStartSession: status %d body: %s", w.Code, w.Body.String())
	}
	cleanupStartedSession(t, srv, w)
	var resp struct {
		FullID string `json:"full_id"`
		ID     string `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	id := resp.FullID
	if id == "" {
		id = resp.ID
	}
	return w, id
}

// TestStartResolvesHeldNodeAddress verifies a start that names a compute_node
// with a bare registry-backed backend (no llm) runs on that node, not the
// entry's first node, and the node's address is stored on the session record.
func TestStartResolvesHeldNodeAddress(t *testing.T) {
	srv := heldNodeServer(t)
	_, id := startHeldNodeSession(t, srv, map[string]any{
		"task":         "hello",
		"backend":      "pi-gpu",
		"model":        "qwen3.8:27b",
		"compute_node": "beta",
		"one_shot":     true,
		"project_dir":  t.TempDir(),
	})
	sess, ok := srv.manager.GetSession(id)
	if !ok || sess == nil {
		t.Fatalf("session %q not found", id)
	}
	if sess.ComputeNodeRef != "beta" {
		t.Fatalf("ComputeNodeRef = %q, want beta", sess.ComputeNodeRef)
	}
	if sess.NodeAddress != "http://10.0.0.2:11434" {
		t.Fatalf("NodeAddress = %q, want http://10.0.0.2:11434", sess.NodeAddress)
	}
	if sess.LLMRef != "pi-gpu" {
		t.Fatalf("LLMRef = %q, want pi-gpu", sess.LLMRef)
	}
}

// TestInteractiveStartUsesHeldNode verifies an interactive start (no
// compute_node named) runs on the node capacity admission granted, even when
// that is not the entry's first node.
func TestInteractiveStartUsesHeldNode(t *testing.T) {
	srv := heldNodeServer(t)
	var admittedBackend string
	srv.SetCapacityAdmit(func(_ context.Context, backend, _, _ string, _ int) (string, error) {
		admittedBackend = backend
		return "beta", nil
	})
	_, id := startHeldNodeSession(t, srv, map[string]any{
		"task":        "hello",
		"backend":     "pi-gpu",
		"model":       "qwen3.8:27b",
		"project_dir": t.TempDir(),
	})
	if admittedBackend != "pi-gpu" {
		t.Fatalf("admitted backend = %q, want pi-gpu", admittedBackend)
	}
	sess, ok := srv.manager.GetSession(id)
	if !ok || sess == nil {
		t.Fatalf("session %q not found", id)
	}
	if sess.ComputeNodeRef != "beta" {
		t.Fatalf("ComputeNodeRef = %q, want the granted node beta", sess.ComputeNodeRef)
	}
	if sess.NodeAddress != "http://10.0.0.2:11434" {
		t.Fatalf("NodeAddress = %q, want http://10.0.0.2:11434", sess.NodeAddress)
	}
}

// TestStartWithoutHeldNodeUnchanged verifies a start with no held node behaves
// as before: the entry's first compute node is used, the admission returns no
// node, and a compute_node with no llm and no registry backend is still a 400.
func TestStartWithoutHeldNodeUnchanged(t *testing.T) {
	t.Run("first entry node", func(t *testing.T) {
		srv := heldNodeServer(t)
		srv.SetCapacityAdmit(func(_ context.Context, _, _, _ string, _ int) (string, error) {
			return "", nil
		})
		_, id := startHeldNodeSession(t, srv, map[string]any{
			"task":        "hello",
			"backend":     "pi-gpu",
			"model":       "qwen3.8:27b",
			"project_dir": t.TempDir(),
		})
		sess, ok := srv.manager.GetSession(id)
		if !ok || sess == nil {
			t.Fatalf("session %q not found", id)
		}
		if sess.ComputeNodeRef != "alpha" {
			t.Fatalf("ComputeNodeRef = %q, want the first entry node alpha", sess.ComputeNodeRef)
		}
		if sess.NodeAddress != "http://10.0.0.1:11434" {
			t.Fatalf("NodeAddress = %q, want http://10.0.0.1:11434", sess.NodeAddress)
		}
		if sess.OllamaURL != "" {
			t.Fatalf("OllamaURL = %q, want empty for a pi-prompt session", sess.OllamaURL)
		}
	})

	t.Run("compute_node without llm or registry backend", func(t *testing.T) {
		srv := heldNodeServer(t)
		raw, _ := json.Marshal(map[string]any{
			"task":         "hello",
			"backend":      "claude-code",
			"compute_node": "alpha",
			"project_dir":  t.TempDir(),
		})
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(raw))
		w := httptest.NewRecorder()
		srv.handleStartSession(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status %d, want 400; body: %s", w.Code, w.Body.String())
		}
	})
}
