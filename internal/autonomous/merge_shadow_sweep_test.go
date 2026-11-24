package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The daemon-start sweep removes a leftover trial under the manager's data
// directory and records it.
func TestSweepStaleMergeTrialsRemovesAndAudits(t *testing.T) {
	data := t.TempDir()
	m, err := NewManager(data, DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var audits []map[string]any
	m.SetAuditFn(func(action string, d map[string]any) {
		if action == "autonomous_merge_trial_sweep" {
			audits = append(audits, d)
		}
	})
	if n := m.SweepStaleMergeTrials(context.Background()); n != 0 || len(audits) != 0 {
		t.Fatalf("empty data dir: removed %d, audits %v", n, audits)
	}
	holder := filepath.Join(data, "merge-shadow-scratch", "trial-merge-1")
	if err := os.MkdirAll(holder, 0o750); err != nil {
		t.Fatal(err)
	}
	if n := m.SweepStaleMergeTrials(context.Background()); n != 1 {
		t.Fatalf("removed %d, want 1", n)
	}
	if _, err := os.Stat(holder); !os.IsNotExist(err) {
		t.Fatal("holder still present")
	}
	if len(audits) != 1 {
		t.Fatalf("audits = %v", audits)
	}
	if got, _ := audits[0]["removed"].([]string); len(got) != 1 || got[0] != holder {
		t.Fatalf("audit removed = %v", audits[0]["removed"])
	}
}
