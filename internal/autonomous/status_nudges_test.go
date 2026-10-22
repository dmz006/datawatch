// Nudge observability in LoopStatus: per-task counts and the per-backend sum.

package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestStatus_NudgeCountsPerTaskAndBackend(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ExecutionBackend = "claude-code"
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "/projnudge", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "story",
		Tasks: []Task{
			{Title: "a", Status: TaskInProgress},
			{Title: "b", Status: TaskInProgress, Backend: "pi-prompt"},
			{Title: "c", Status: TaskQueued},
		},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	a, b := prd.Story[0].Tasks[0].ID, prd.Story[0].Tasks[1].ID
	now := time.Now()
	for _, id := range []string{a, a, b} {
		if err := m.RecordTaskNudge(prd.ID, id, now); err != nil {
			t.Fatalf("RecordTaskNudge: %v", err)
		}
	}

	st := m.Status()
	if st.NudgesTotal != 3 {
		t.Fatalf("NudgesTotal = %d, want 3", st.NudgesTotal)
	}
	if len(st.NudgeTasks) != 2 {
		t.Fatalf("NudgeTasks = %+v, want 2 entries (the un-nudged task is omitted)", st.NudgeTasks)
	}
	per := map[string]int{}
	for _, n := range st.NudgeTasks {
		per[n.TaskID] = n.Count
		if n.PRDID != prd.ID {
			t.Errorf("NudgeTasks prd_id = %q, want %q", n.PRDID, prd.ID)
		}
	}
	if per[a] != 2 || per[b] != 1 {
		t.Fatalf("per-task counts = %v, want %s=2 %s=1", per, a, b)
	}
	if st.NudgesByBackend["claude-code"] != 2 || st.NudgesByBackend["pi-prompt"] != 1 {
		t.Fatalf("NudgesByBackend = %v, want claude-code=2 pi-prompt=1", st.NudgesByBackend)
	}
}

func TestStatus_NoNudgesOmitsFields(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	st := m.Status()
	if st.NudgesTotal != 0 || st.NudgeTasks != nil || st.NudgesByBackend != nil {
		t.Fatalf("expected empty nudge fields, got %+v", st)
	}
}

func TestConfigRoundTrip_WorkerIdleNudge(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	cfg := m.Config()
	if err := json.Unmarshal([]byte(`{"worker_idle_nudge_seconds":120,"worker_idle_nudge_stall_seconds":300}`), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	m.SetConfig(cfg)
	c := m.Config()
	if c.WorkerIdleNudgeSeconds != 120 || c.WorkerIdleNudgeStallSeconds != 300 {
		t.Fatalf("config = %d/%d, want 120/300", c.WorkerIdleNudgeSeconds, c.WorkerIdleNudgeStallSeconds)
	}
	b, _ := json.Marshal(c)
	if !strings.Contains(string(b), `"worker_idle_nudge_seconds":120`) || !strings.Contains(string(b), `"worker_idle_nudge_stall_seconds":300`) {
		t.Fatalf("keys dropped from JSON: %s", b)
	}
}
