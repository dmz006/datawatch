// BL367 — unit tests for per-PRD quality gate config.
// Covers: SetPRDQualityGates persistence, per-PRD override vs manager default,
// no-gate passthrough, and QualityGateResult stored on task after run.

package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/pipeline"
)

// TestBL367_SetPRDQualityGates_Persisted verifies that SetPRDQualityGates
// stores the config on the PRD and it survives a store round-trip.
func TestBL367_SetPRDQualityGates_Persisted(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir, Config{Enabled: true, AutoFixRetries: 0}, nil)
	prd, _ := mgr.Store().CreatePRD("test prd", "/proj", "claude-code", EffortNormal)

	updated, err := mgr.SetPRDQualityGates(prd.ID, true, "go test ./...", 60, true)
	if err != nil {
		t.Fatalf("SetPRDQualityGates: %v", err)
	}
	qg := updated.QualityGates
	if qg == nil {
		t.Fatal("expected QualityGates to be non-nil")
	}
	if !qg.Enabled {
		t.Error("expected Enabled=true")
	}
	if qg.TestCommand != "go test ./..." {
		t.Errorf("TestCommand: got %q want %q", qg.TestCommand, "go test ./...")
	}
	if qg.Timeout != 60 {
		t.Errorf("Timeout: got %d want 60", qg.Timeout)
	}
	if !qg.BlockOnRegression {
		t.Error("expected BlockOnRegression=true")
	}

	// Verify it round-trips through the store.
	stored, ok := mgr.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD not found after SetPRDQualityGates")
	}
	if stored.QualityGates == nil || stored.QualityGates.TestCommand != "go test ./..." {
		t.Error("quality gate not persisted to store")
	}
}

// TestBL367_SetPRDQualityGates_NotFound verifies that an unknown PRD ID
// returns a descriptive error and does not panic.
func TestBL367_SetPRDQualityGates_NotFound(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir, Config{Enabled: true, AutoFixRetries: 0}, nil)

	_, err := mgr.SetPRDQualityGates("no-such-prd", true, "go test ./...", 0, false)
	if err == nil {
		t.Fatal("expected error for unknown PRD ID")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

// TestBL367_ResolveQualityGates_PerPRDOverridesDefault verifies that a PRD
// with its own QualityGates takes precedence over the manager default.
func TestBL367_ResolveQualityGates_PerPRDOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir, Config{
		Enabled:        true,
		AutoFixRetries: 0,
		DefaultQualityGates: pipeline.QualityGateConfig{
			Enabled:     true,
			TestCommand: "make test",
		},
	}, nil)

	// Create PRD via the manager's own store so it's visible in-memory.
	prd, _ := mgr.Store().CreatePRD("test prd", "/proj", "claude-code", EffortNormal)
	_, err := mgr.SetPRDQualityGates(prd.ID, true, "cargo test", 30, false)
	if err != nil {
		t.Fatalf("SetPRDQualityGates: %v", err)
	}

	p, ok := mgr.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD not found after SetPRDQualityGates")
	}
	resolved := mgr.resolveQualityGates(p)
	if resolved.TestCommand != "cargo test" {
		t.Errorf("expected per-PRD override 'cargo test', got %q", resolved.TestCommand)
	}
}

// TestBL367_ResolveQualityGates_FallsBackToDefault verifies that a PRD
// without its own QualityGates uses the manager-level default.
func TestBL367_ResolveQualityGates_FallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	mgr, _ := NewManager(dir, Config{
		Enabled:        true,
		AutoFixRetries: 0,
		DefaultQualityGates: pipeline.QualityGateConfig{
			Enabled:     true,
			TestCommand: "make test",
		},
	}, nil)

	// Create PRD with no per-PRD quality gates.
	prd, _ := mgr.Store().CreatePRD("test prd", "/proj", "claude-code", EffortNormal)
	p, ok := mgr.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD not found")
	}
	resolved := mgr.resolveQualityGates(p)
	if resolved.TestCommand != "make test" {
		t.Errorf("expected default 'make test', got %q", resolved.TestCommand)
	}
}

// TestBL367_QualityGateResult_StoredOnTask verifies that after a task runs
// with quality gates enabled, Task.QualityGateResult is populated.
func TestBL367_QualityGateResult_StoredOnTask(t *testing.T) {
	projDir := t.TempDir()
	// Create a minimal Go module so `go build .` succeeds.
	if err := os.WriteFile(filepath.Join(projDir, "go.mod"), []byte("module testmod\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	dir := t.TempDir()
	st, _ := NewStore(dir)
	prd, _ := st.CreatePRD("test prd", projDir, "claude-code", EffortNormal)
	_ = st.SetStories(prd.ID, []Story{{
		Title: "S1",
		Tasks: []Task{{Title: "T1", Spec: "build it"}},
	}})
	prd, _ = st.GetPRD(prd.ID) // re-fetch so task IDs are populated
	prd.Status = PRDApproved
	_ = st.SavePRD(prd)

	var taskIDs []string
	for _, s := range prd.Story {
		for _, tk := range s.Tasks {
			taskIDs = append(taskIDs, tk.ID)
		}
	}

	mgr, _ := NewManager(dir, Config{
		Enabled:        true,
		AutoFixRetries: 0,
		DefaultQualityGates: pipeline.QualityGateConfig{
			Enabled:           true,
			TestCommand:       "go build .",
			Timeout:           30,
			BlockOnRegression: false,
		},
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	bl367Spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		return SpawnResult{SessionID: "sess-" + req.Title}, nil
	}
	bl367Verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok", VerifiedAt: time.Now()}, nil
	}
	err := mgr.Run(ctx, prd.ID, bl367Spawn, bl367Verify)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, id := range taskIDs {
		task, ok := mgr.Store().GetTask(id)
		if !ok {
			t.Errorf("task %s not found in manager store", id)
			continue
		}
		if task.QualityGateResult == nil {
			t.Errorf("task %s: expected QualityGateResult to be populated", id)
		}
	}
}
