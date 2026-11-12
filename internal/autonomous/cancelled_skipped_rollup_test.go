// Cancelled tasks are skipped, not blockers: the completion path runs the
// judge when every task is completed or cancelled and at least one completed.
package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skippedRollupFixture builds a running Automaton with one story per entry of
// stories (each a list of task statuses), a rules file and a counting pass
// judge.
func skippedRollupFixture(t *testing.T, stories ...[]TaskStatus) (*Manager, *PRD, *int) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.GuardrailModel = "ollama/qwen3.8:27b"
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", dir, "claude", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	var ss []Story
	for si, sts := range stories {
		var tasks []Task
		for ti, st := range sts {
			tasks = append(tasks, Task{ID: "t-" + string(rune('a'+si)) + string(rune('1'+ti)), Title: "Task " + string(rune('A'+si)) + string(rune('1'+ti)), Status: st})
		}
		ss = append(ss, Story{ID: "s-" + string(rune('a'+si)), Title: "Story", Tasks: tasks})
	}
	if err := m.Store().SetStories(prd.ID, ss); err != nil {
		t.Fatal(err)
	}
	calls := new(int)
	m.SetGuardrail(func(_ context.Context, inv GuardrailInvocation) (GuardrailVerdict, error) {
		if inv.Level == "prd" {
			*calls++
		}
		return GuardrailVerdict{Outcome: "pass", Summary: "clean"}, nil
	})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.PerPRDGuardrails = []string{"rules-review"}
	prd.Status = PRDRunning
	for si := range prd.Story {
		prd.Story[si].Status = StoryInProgress
		for ti := range prd.Story[si].Tasks {
			prd.Story[si].Tasks[ti].Status = stories[si][ti]
		}
	}
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, prd, calls
}

func skippedDecisions(p *PRD) []Decision {
	var out []Decision
	for _, d := range p.Decisions {
		if d.Kind == "cancelled_tasks_skipped" {
			out = append(out, d)
		}
	}
	return out
}

func TestCancelledSkipped_AdvancesToJudgeAndNamesTask(t *testing.T) {
	m, prd, calls := skippedRollupFixture(t, []TaskStatus{TaskCompleted, TaskCancelled, TaskCompleted})
	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if *calls == 0 {
		t.Fatal("the judge never ran")
	}
	if got.Status != PRDCompleted {
		t.Fatalf("status = %q, want completed", got.Status)
	}
	ds := skippedDecisions(got)
	if len(ds) != 1 {
		t.Fatalf("cancelled_tasks_skipped decisions = %d, want 1", len(ds))
	}
	n := ds[0].Note
	if !strings.Contains(n, "1 cancelled task") || !strings.Contains(n, "Task A2") {
		t.Fatalf("note %q does not name the skipped task", n)
	}
	if strings.Contains(strings.ToLower(n), "prd") {
		t.Fatalf("note %q uses internal wording", n)
	}
}

func TestCancelledSkipped_RecordedOncePerPass(t *testing.T) {
	m, prd, _ := skippedRollupFixture(t, []TaskStatus{TaskCompleted, TaskCancelled})
	if !m.recordCancelledTasksSkipped(prd) {
		t.Fatal("first pass recorded nothing")
	}
	if m.recordCancelledTasksSkipped(prd) {
		t.Fatal("re-fire recorded the decision again")
	}
	if n := len(skippedDecisions(prd)); n != 1 {
		t.Fatalf("decisions = %d, want 1", n)
	}
}

func TestCancelledSkipped_StoryOnlyCancelledIsSkippedNotFailed(t *testing.T) {
	s := &Story{Tasks: []Task{{Status: TaskCancelled}, {Status: TaskCancelled}}}
	if got := storyRollupStatus(s); got != StoryCancelled {
		t.Fatalf("status = %q, want cancelled (skipped)", got)
	}
}

func TestCancelledSkipped_StoryWithCompletedIsCompleted(t *testing.T) {
	s := &Story{Tasks: []Task{{Status: TaskCompleted}, {Status: TaskCancelled}}}
	if got := storyRollupStatus(s); got != StoryCompleted {
		t.Fatalf("status = %q, want completed", got)
	}
	s = &Story{Tasks: []Task{{Status: TaskCompleted}, {Status: TaskFailed}, {Status: TaskCancelled}}}
	if got := storyRollupStatus(s); got != StoryFailed {
		t.Fatalf("status = %q, want failed", got)
	}
}

func TestCancelledSkipped_AllCancelledDoesNotAdvance(t *testing.T) {
	m, prd, calls := skippedRollupFixture(t, []TaskStatus{TaskCancelled, TaskCancelled})
	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if *calls != 0 {
		t.Fatalf("judge ran %d times with nothing completed", *calls)
	}
	if got.Status == PRDCompleted {
		t.Fatal("all-cancelled Automaton completed")
	}
	if len(skippedDecisions(got)) != 0 {
		t.Fatal("skipped decision recorded without a judge run")
	}
}

func TestCancelledSkipped_FailedTaskStillFails(t *testing.T) {
	m, prd, calls := skippedRollupFixture(t, []TaskStatus{TaskCompleted, TaskCancelled, TaskFailed})
	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if *calls != 0 {
		t.Fatal("judge ran for a failed Automaton")
	}
}

func TestCancelledSkipped_RejudgeAllowsCancelledWithCompleted(t *testing.T) {
	m, prd, _ := newRejudgeBlockedPRD(t, TaskCompleted, TaskCancelled)
	j := newRejudgePassJudge(t)
	m.SetGuardrail(j.fn)
	if _, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", ""); err != nil {
		t.Fatalf("rejudge refused with a cancelled task: %v", err)
	}
	if j.calls == 0 {
		t.Fatal("judge not asked")
	}
}

func TestCancelledSkipped_RejudgeRefusesPendingAndAllCancelled(t *testing.T) {
	for _, sts := range [][]TaskStatus{{TaskCompleted, TaskPending}, {TaskCompleted, TaskFailed}, {TaskCancelled, TaskCancelled}} {
		m, prd, _ := newRejudgeBlockedPRD(t, sts...)
		if _, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", ""); err == nil {
			t.Fatalf("rejudge accepted %v", sts)
		}
	}
}
