package autonomous

import (
	"context"
	"testing"
)

// TestSpawnRequestCarriesHeldNode verifies the worker SpawnRequest carries the
// compute node its capacity lease was granted on, read after admit succeeds.
// With the pooled fixture the ledger places the single task on "fast" (the
// higher-priority candidate), so the spawn must see ComputeNode "fast". With
// capacity off there is no lease, and the request carries no node.
func TestSpawnRequestCarriesHeldNode(t *testing.T) {
	run := func(t *testing.T, m *Manager) []string {
		t.Helper()
		prd := newPRDWithTasks(t, m, 1)
		cur, _ := m.Store().GetPRD(prd.ID)
		cur.Status = PRDApproved
		_ = m.Store().SavePRD(cur)

		var nodes []string
		spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
			nodes = append(nodes, req.ComputeNode)
			return SpawnResult{SessionID: "s-" + req.TaskID}, nil
		}
		verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
			return VerificationResult{OK: true, Summary: "ok"}, nil
		}
		if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
			t.Fatalf("Run: %v", err)
		}
		return nodes
	}

	t.Run("pooled node held", func(t *testing.T) {
		m, _ := poolFixture(t, true)
		if nodes := run(t, m); len(nodes) != 1 || nodes[0] != "fast" {
			t.Fatalf("spawn ComputeNode = %v, want [fast]", nodes)
		}
	})

	t.Run("capacity off", func(t *testing.T) {
		m, _, _, _ := apiFixture(t)
		if nodes := run(t, m); len(nodes) != 1 || nodes[0] != "" {
			t.Fatalf("spawn ComputeNode = %v, want [\"\"] with no lease", nodes)
		}
	})
}
