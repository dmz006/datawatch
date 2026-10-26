package server

import (
	"context"
	"testing"

	"github.com/dmz006/datawatch/internal/inference"
)

// addSoloPiEntry registers a pi-prompt entry that lists only alpha.
func addSoloPiEntry(t *testing.T, srv *Server) {
	t.Helper()
	srv.SetCapacityAdmit(func(_ context.Context, _, _, _ string, _ int) (string, error) {
		return "", nil
	})
	if err := srv.inferenceReg.Add(&inference.LLM{
		Name:         "pi-solo",
		Kind:         inference.KindPiPrompt,
		ComputeNodes: []string{"alpha"},
	}); err != nil {
		t.Fatalf("Add pi-solo: %v", err)
	}
}

// TestOneShotStartWithLeasedNodeUsesNodeAddress verifies an autonomous spawn
// (one-shot) that carries the node its lease was granted on runs against that
// node's address with the provider-qualified model, even when the entry lists
// a single node.
func TestOneShotStartWithLeasedNodeUsesNodeAddress(t *testing.T) {
	srv := heldNodeServer(t)
	addSoloPiEntry(t, srv)
	_, id := startHeldNodeSession(t, srv, map[string]any{
		"task":         "hello",
		"backend":      "pi-solo",
		"model":        "ollama/qwen3.8:27b",
		"compute_node": "alpha",
		"one_shot":     true,
		"project_dir":  t.TempDir(),
	})
	sess, ok := srv.manager.GetSession(id)
	if !ok || sess == nil {
		t.Fatalf("session %q not found", id)
	}
	if sess.ComputeNodeRef != "alpha" {
		t.Fatalf("ComputeNodeRef = %q, want alpha", sess.ComputeNodeRef)
	}
	if sess.NodeAddress != "http://10.0.0.1:11434" {
		t.Fatalf("NodeAddress = %q, want the leased node's address http://10.0.0.1:11434", sess.NodeAddress)
	}
	if sess.Model != "ollama/qwen3.8:27b" {
		t.Fatalf("Model = %q, want the provider-qualified ollama/qwen3.8:27b", sess.Model)
	}
}

// TestStartWithNoNodeNamedKeepsEntryRules verifies the entry-size rules still
// apply to a start that names no node: a multi-node entry binds its first node
// and a one-node entry keeps pi's own provider table, one-shot or not.
func TestStartWithNoNodeNamedKeepsEntryRules(t *testing.T) {
	t.Run("multi-node entry binds its first node", func(t *testing.T) {
		srv := heldNodeServer(t)
		srv.SetCapacityAdmit(func(_ context.Context, _, _, _ string, _ int) (string, error) {
			return "", nil
		})
		_, id := startHeldNodeSession(t, srv, map[string]any{
			"task":        "hello",
			"backend":     "pi-gpu",
			"model":       "ollama/qwen3.8:27b",
			"one_shot":    true,
			"project_dir": t.TempDir(),
		})
		sess, ok := srv.manager.GetSession(id)
		if !ok || sess == nil {
			t.Fatalf("session %q not found", id)
		}
		if sess.ComputeNodeRef != "alpha" || sess.NodeAddress != "http://10.0.0.1:11434" {
			t.Fatalf("node = %q at %q, want alpha at http://10.0.0.1:11434", sess.ComputeNodeRef, sess.NodeAddress)
		}
	})

	t.Run("one-node entry keeps pi's own provider table", func(t *testing.T) {
		srv := heldNodeServer(t)
		addSoloPiEntry(t, srv)
		_, id := startHeldNodeSession(t, srv, map[string]any{
			"task":        "hello",
			"backend":     "pi-solo",
			"model":       "ollama/qwen3.8:27b",
			"one_shot":    true,
			"project_dir": t.TempDir(),
		})
		sess, ok := srv.manager.GetSession(id)
		if !ok || sess == nil {
			t.Fatalf("session %q not found", id)
		}
		if sess.NodeAddress != "" {
			t.Fatalf("NodeAddress = %q, want empty when no node is named", sess.NodeAddress)
		}
	})
}
