package autonomous

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
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

// unnodedLeaseRig wires a manager whose lease names no node although the
// resolver lists the LLM entry's compute nodes: the keys carry no node, and
// the candidate list holds one node, which the ledger does not pick from.
func unnodedLeaseRig(t *testing.T, listed []capacity.NodeChoice) (*Manager, *capacity.Ledger) {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		return []string{"host", "llm:pool"}, ""
	})
	m.SetCapacityChoices(func(backend, model string) []capacity.NodeChoice { return listed })
	return m, led
}

// runUnnoded runs a one-task PRD on backend and model and returns how many
// workers were spawned and the task as stored afterwards.
func runUnnoded(t *testing.T, m *Manager, backend, model string) (int, Task) {
	t.Helper()
	prd, err := m.CreatePRD("s", "/w/proj", backend, model, EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T0", Spec: "write docs/x.md"}}}})
	cur, _ := m.Store().GetPRD(prd.ID)
	cur.Status = PRDApproved
	_ = m.Store().SavePRD(cur)

	spawns := 0
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		spawns++
		return SpawnResult{SessionID: "s-" + req.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
	_ = m.Run(context.Background(), prd.ID, spawn, verify)
	got, _ := m.Store().GetPRD(prd.ID)
	return spawns, got.Story[0].Tasks[0]
}

// TestPiFamilyWorkerWithoutLeasedNodeDoesNotSpawn verifies a pi-family worker
// that holds capacity but was leased no node is not started when its entry
// lists compute nodes: it would run on pi's own default endpoint. The failure
// names the backend and the model and says no node was leased.
func TestPiFamilyWorkerWithoutLeasedNodeDoesNotSpawn(t *testing.T) {
	listed := []capacity.NodeChoice{{Name: "gpu-a", Priority: 50}}
	for _, backend := range []string{"pi-prompt", "pi"} {
		t.Run(backend, func(t *testing.T) {
			m, _ := unnodedLeaseRig(t, listed)
			spawns, task := runUnnoded(t, m, backend, "ollama/qwen3.8:27b")
			if spawns != 0 {
				t.Fatalf("worker spawned %d times with no leased node, want 0", spawns)
			}
			if task.Status != TaskFailed {
				t.Fatalf("task status = %q, want %q", task.Status, TaskFailed)
			}
			for _, want := range []string{backend, "ollama/qwen3.8:27b", "no compute node was leased"} {
				if !strings.Contains(task.Error, want) {
					t.Fatalf("task error %q does not contain %q", task.Error, want)
				}
			}
		})
	}
}

// TestWorkerWithoutLeasedNodeUnchangedElsewhere verifies the cases that must
// keep spawning: an entry that lists no compute nodes, a hosted backend, and
// capacity switched off.
func TestWorkerWithoutLeasedNodeUnchangedElsewhere(t *testing.T) {
	t.Run("entry lists no compute nodes", func(t *testing.T) {
		m, _ := unnodedLeaseRig(t, nil)
		if spawns, task := runUnnoded(t, m, "pi-prompt", "ollama/qwen3.8:27b"); spawns != 1 || task.Status == TaskFailed {
			t.Fatalf("spawns = %d, status = %q, want one spawn", spawns, task.Status)
		}
	})
	t.Run("hosted backend", func(t *testing.T) {
		m, _ := unnodedLeaseRig(t, []capacity.NodeChoice{{Name: "gpu-a", Priority: 50}})
		if spawns, task := runUnnoded(t, m, "claude-code", "claude-opus-5-5"); spawns != 1 || task.Status == TaskFailed {
			t.Fatalf("spawns = %d, status = %q, want one spawn", spawns, task.Status)
		}
	})
	t.Run("capacity off", func(t *testing.T) {
		m, _, _, _ := apiFixture(t)
		m.SetCapacityChoices(func(backend, model string) []capacity.NodeChoice {
			return []capacity.NodeChoice{{Name: "gpu-a", Priority: 50}}
		})
		if spawns, task := runUnnoded(t, m, "pi-prompt", "ollama/qwen3.8:27b"); spawns != 1 || task.Status == TaskFailed {
			t.Fatalf("spawns = %d, status = %q, want one spawn", spawns, task.Status)
		}
	})
}
