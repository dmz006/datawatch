package server

// The conflict_confirmation block of GET /api/autonomous/merge-pipeline and the
// conflict_mode and conflict_confirm_first_n keys, global and per project, on
// PUT /api/config.

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/mergepipeline"
)

// mpConflictCounterIncrement records n more model-resolved conflicts for demo
// in the counter the server reads.
func mpConflictCounterIncrement(t *testing.T, s *Server, n int) {
	t.Helper()
	c, err := mergepipeline.NewConflictCounter(s.statePath())
	if err != nil {
		t.Fatalf("NewConflictCounter: %v", err)
	}
	for i := 0; i < n; i++ {
		if _, err := c.Increment("demo"); err != nil {
			t.Fatalf("Increment: %v", err)
		}
	}
}

// TestMergePipelineConflictCounter checks the GET block: the defaults, the
// count and remaining from the counter, a PUT of conflict_mode on, and a PUT of
// conflict_confirm_first_n 0 followed by a GET that shows enabled false. It
// also checks the per-project keys and a counter that cannot be read.
func TestMergePipelineConflictCounter(t *testing.T) {
	s, _ := newMergePipelineServer(t)

	// Defaults: shadow mode, N 5, nothing counted yet, so the gate is off.
	cc := mpGet(t, s, "demo").ConflictConfirmation
	if cc.Mode != mergepipeline.ConflictModeShadow || cc.ConfirmFirstN != 5 || cc.Enabled {
		t.Fatalf("default block = %+v, want shadow, N 5, enabled false", cc)
	}
	if cc.Counted == nil || *cc.Counted != 0 || cc.Remaining == nil || *cc.Remaining != 5 || cc.Error != "" {
		t.Fatalf("default counts = counted %v remaining %v error %q, want 0, 5, none", cc.Counted, cc.Remaining, cc.Error)
	}

	// Two conflicts counted leave three to confirm.
	mpConflictCounterIncrement(t, s, 2)
	cc = mpGet(t, s, "demo").ConflictConfirmation
	if cc.Counted == nil || *cc.Counted != 2 || cc.Remaining == nil || *cc.Remaining != 3 {
		t.Fatalf("after 2 conflicts counts = %v/%v, want 2/3", cc.Counted, cc.Remaining)
	}

	// Mode on with N above 0 turns the gate on.
	if rec := putMergePipelineConfig(s, map[string]any{"autonomous.merge_pipeline.conflict_mode": "on"}); rec.Code != http.StatusOK {
		t.Fatalf("PUT conflict_mode on: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if cc = mpGet(t, s, "demo").ConflictConfirmation; cc.Mode != "on" || !cc.Enabled {
		t.Fatalf("after mode on = %+v, want mode on, enabled true", cc)
	}

	// N set to 0 turns confirmation off: enabled false, nothing remaining.
	if rec := putMergePipelineConfig(s, map[string]any{"autonomous.merge_pipeline.conflict_confirm_first_n": 0}); rec.Code != http.StatusOK {
		t.Fatalf("PUT confirm_first_n 0: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	cc = mpGet(t, s, "demo").ConflictConfirmation
	if cc.Enabled || cc.ConfirmFirstN != 0 || cc.Remaining == nil || *cc.Remaining != 0 {
		t.Fatalf("after confirm_first_n 0 = %+v, want enabled false, N 0, remaining 0", cc)
	}

	// A per-project N wins for demo; the global value is left alone.
	if rec := putMergePipelineConfig(s, map[string]any{"autonomous.merge_pipeline.projects.demo.conflict_confirm_first_n": 4}); rec.Code != http.StatusOK {
		t.Fatalf("PUT project confirm_first_n: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	cc = mpGet(t, s, "demo").ConflictConfirmation
	if !cc.Enabled || cc.ConfirmFirstN != 4 || cc.Remaining == nil || *cc.Remaining != 2 {
		t.Fatalf("project override block = %+v, want enabled true, N 4, remaining 2", cc)
	}
	if n := s.cfg.Autonomous.MergePipeline.EffectiveConflictConfirmFirstN(""); n != 0 {
		t.Errorf("global confirm_first_n = %d, want 0 (untouched)", n)
	}

	// Values the keys refuse leave the block unchanged.
	for _, patch := range []map[string]any{
		{"autonomous.merge_pipeline.conflict_mode": "maybe"},
		{"autonomous.merge_pipeline.conflict_confirm_first_n": -1},
		{"autonomous.merge_pipeline.conflict_confirm_first_n": 1.5},
	} {
		if rec := putMergePipelineConfig(s, patch); rec.Code == http.StatusOK {
			t.Errorf("PUT %v: want refusal, got 200", patch)
		}
	}
	if cc = mpGet(t, s, "demo").ConflictConfirmation; cc.ConfirmFirstN != 4 || cc.Mode != "on" {
		t.Errorf("block after refused PUTs = %+v, want N 4 and mode on", cc)
	}
}

// TestMergePipelineConflictCounterUnreadable checks a counter file that cannot
// be read: the block reports it in error with null counts, never a zero count.
func TestMergePipelineConflictCounterUnreadable(t *testing.T) {
	s, _ := newMergePipelineServer(t)
	dir := filepath.Join(s.statePath(), "merge-pipeline")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "conflict-counter.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	cc := mpGet(t, s, "demo").ConflictConfirmation
	if cc.Error == "" || cc.Counted != nil || cc.Remaining != nil {
		t.Fatalf("unreadable counter block = %+v, want error with null counted and remaining", cc)
	}
}
