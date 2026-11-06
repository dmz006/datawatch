package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func newMergeBlockTask(t *testing.T) (*Manager, *Task) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", t.TempDir(), "ollama", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	task := &Task{ID: "mb-task-1", PRDID: prd.ID}
	if err := m.store.SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	return m, task
}

func TestMergeBlockSetKeepsSinceAndCountsAttempts(t *testing.T) {
	m, task := newMergeBlockTask(t)
	if err := m.setMergeBlock(task, MergeBlock{Kind: MergeBlockKindDirtyWorktree, Reason: "dirty", Files: []string{"a.go"}}); err != nil {
		t.Fatal(err)
	}
	first := *task.MergeBlock
	if first.Attempts != 1 || first.Since.IsZero() {
		t.Fatalf("first block: attempts=%d since=%v", first.Attempts, first.Since)
	}
	time.Sleep(2 * time.Millisecond)
	if err := m.setMergeBlock(task, MergeBlock{Kind: MergeBlockKindDirtyWorktree, Reason: "still dirty", LastRetryAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got := task.MergeBlock
	if !got.Since.Equal(first.Since) {
		t.Errorf("Since changed: %v -> %v", first.Since, got.Since)
	}
	if got.Attempts != 2 {
		t.Errorf("Attempts = %d, want 2", got.Attempts)
	}
	if got.Reason != "still dirty" {
		t.Errorf("Reason = %q", got.Reason)
	}
	// A different kind starts over.
	if err := m.setMergeBlock(task, MergeBlock{Kind: "other"}); err != nil {
		t.Fatal(err)
	}
	if task.MergeBlock.Attempts != 1 || task.MergeBlock.Since.Equal(first.Since) {
		t.Errorf("different kind should reset: %+v", task.MergeBlock)
	}
	stored, _ := m.store.GetTask(task.ID)
	if stored.MergeBlock == nil || stored.MergeBlock.Kind != "other" {
		t.Errorf("not saved: %+v", stored.MergeBlock)
	}
}

func TestMergeBlockClearRemovesRecord(t *testing.T) {
	m, task := newMergeBlockTask(t)
	if err := m.setMergeBlock(task, MergeBlock{Kind: MergeBlockKindDirtyWorktree}); err != nil {
		t.Fatal(err)
	}
	if err := m.clearMergeBlock(task); err != nil {
		t.Fatal(err)
	}
	if task.MergeBlock != nil {
		t.Fatal("MergeBlock not cleared")
	}
	b, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "merge_block") {
		t.Errorf("cleared task still serialises merge_block: %s", b)
	}
	// Clearing again is a no-op.
	if err := m.clearMergeBlock(task); err != nil {
		t.Fatal(err)
	}
}

// newStoredBlockTask returns a Manager and a task that lives inside its
// Automaton's first story, as a real task does.
func newStoredBlockTask(t *testing.T) (*Manager, *PRD, *Task) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	prd, err := m.CreatePRD("spec", t.TempDir(), "ollama", "", "")
	if err != nil {
		t.Fatal(err)
	}
	var task *Task
	m.store.WithLock(func() {
		prd.Story = append(prd.Story, Story{ID: "s1", Tasks: []Task{{ID: "mbt-1", PRDID: prd.ID, Title: "Do it"}}})
		task = &prd.Story[len(prd.Story)-1].Tasks[0]
		m.store.tasks[task.ID] = task
	})
	return m, prd, task
}

func TestMergeBlockTriageRecordLifecycle(t *testing.T) {
	m, prd, task := newStoredBlockTask(t)
	if err := m.setMergeBlock(task, MergeBlock{Kind: MergeBlockKindDirtyWorktree, Reason: "dirty", Action: "commit it"}); err != nil {
		t.Fatal(err)
	}
	snap, _ := m.store.SnapshotPRD(prd.ID)
	if snap.Triage == nil || snap.Triage.Status != TriageStatusMergeBlocked || len(snap.Triage.MergeBlocks) != 1 {
		t.Fatalf("triage = %+v", snap.Triage)
	}
	if b := snap.Triage.MergeBlocks[0]; b.TaskID != "mbt-1" || b.Reason != "dirty" || b.Action != "commit it" || b.Attempts != 1 {
		t.Errorf("block = %+v", b)
	}
	if raw, _ := json.Marshal(snap.Story[0].Tasks[0]); !strings.Contains(string(raw), `"merge_block"`) {
		t.Errorf("task JSON lacks merge_block: %s", raw)
	}
	if err := m.clearMergeBlock(task); err != nil {
		t.Fatal(err)
	}
	if snap, _ = m.store.SnapshotPRD(prd.ID); snap.Triage != nil {
		t.Errorf("triage not removed: %+v", snap.Triage)
	}
}

func TestMergeBlockTriageKeepsFailedRecordFields(t *testing.T) {
	m, prd, task := newStoredBlockTask(t)
	if err := m.SaveTriageRecord(prd.ID, &TriageRecord{Status: "failed", Error: "boom", TaskID: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := m.setMergeBlock(task, MergeBlock{Kind: MergeBlockKindDirtyWorktree}); err != nil {
		t.Fatal(err)
	}
	snap, _ := m.store.SnapshotPRD(prd.ID)
	if tr := snap.Triage; tr.Status != "failed" || tr.Error != "boom" || len(tr.MergeBlocks) != 1 {
		t.Fatalf("triage = %+v", tr)
	}
	if err := m.clearMergeBlock(task); err != nil {
		t.Fatal(err)
	}
	snap, _ = m.store.SnapshotPRD(prd.ID)
	if tr := snap.Triage; tr == nil || tr.Status != "failed" || tr.Error != "boom" || len(tr.MergeBlocks) != 0 {
		t.Fatalf("triage after clear = %+v", tr)
	}
}
