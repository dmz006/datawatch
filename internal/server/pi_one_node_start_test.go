package server

import (
	"context"
	"testing"

	"github.com/dmz006/datawatch/internal/inference"
)

// TestOneNodePiEntryKeepsOwnProviderTable: a pi-prompt entry that lists a single
// compute node starts its session without a node address, so pi reads its own
// provider table as it did before node binding. The node is still recorded on
// the session. A two-node entry keeps binding, as TestStartWithoutHeldNodeUnchanged
// checks.
func TestOneNodePiEntryKeepsOwnProviderTable(t *testing.T) {
	srv := heldNodeServer(t)
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
	_, id := startHeldNodeSession(t, srv, map[string]any{
		"task":        "hello",
		"backend":     "pi-solo",
		"model":       "qwen3.8:27b",
		"project_dir": t.TempDir(),
	})
	sess, ok := srv.manager.GetSession(id)
	if !ok || sess == nil {
		t.Fatalf("session %q not found", id)
	}
	if sess.ComputeNodeRef != "alpha" {
		t.Fatalf("ComputeNodeRef = %q, want the only entry node alpha", sess.ComputeNodeRef)
	}
	if sess.NodeAddress != "" {
		t.Fatalf("NodeAddress = %q, want empty for a one-node pi entry", sess.NodeAddress)
	}
}
