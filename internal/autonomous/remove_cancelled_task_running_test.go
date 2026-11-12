package autonomous

import (
	"strings"
	"testing"
)

// seedRunningPRD builds a running Automaton with one story holding a
// cancelled task, a dependant, and extra tasks in the given statuses.
func seedRunningPRD(t *testing.T, extra TaskStatus) (*Manager, *PRD) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", t.TempDir(), "ollama", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	story, err := m.Store().AddStory(prd.ID, "s", "d")
	if err != nil {
		t.Fatalf("AddStory: %v", err)
	}
	for _, id := range []string{"t-cancel", "t-dep", "t-extra"} {
		if _, err := m.Store().AddTask(prd.ID, story.ID, id, "spec "+id, nil); err != nil {
			t.Fatalf("AddTask: %v", err)
		}
	}
	p, _ := m.Store().GetPRD(prd.ID)
	m.Store().WithLock(func() {
		p.Status = PRDRunning
		for i := range p.Story[0].Tasks {
			tk := &p.Story[0].Tasks[i]
			switch tk.Title {
			case "t-cancel":
				tk.Status = TaskCancelled
			case "t-dep":
				tk.Status = TaskPending
			case "t-extra":
				tk.Status = extra
			}
		}
	})
	return m, p
}

func taskIDByTitle(p *PRD, title string) string {
	for _, tk := range p.Story[0].Tasks {
		if tk.Title == title {
			return tk.ID
		}
	}
	return ""
}

func TestRemoveCancelledTaskWhileRunning(t *testing.T) {
	m, p := seedRunningPRD(t, TaskCompleted)
	cancelID := taskIDByTitle(p, "t-cancel")
	m.Store().WithLock(func() {
		for i := range p.Story[0].Tasks {
			if p.Story[0].Tasks[i].Title == "t-dep" {
				p.Story[0].Tasks[i].DependsOn = []string{cancelID}
			}
		}
	})
	out, err := m.RemoveTask(p.ID, p.Story[0].ID, cancelID, "operator")
	if err != nil {
		t.Fatalf("RemoveTask: %v", err)
	}
	if taskIDByTitle(out, "t-cancel") != "" {
		t.Fatal("task still present")
	}
	for _, tk := range out.Story[0].Tasks {
		for _, d := range tk.DependsOn {
			if d == cancelID {
				t.Fatalf("task %s still depends on removed task", tk.Title)
			}
		}
	}
	last := out.Decisions[len(out.Decisions)-1]
	if last.Kind != "remove_task" {
		t.Fatalf("last decision = %q", last.Kind)
	}
}

func TestRemoveLiveTaskWhileRunningRefused(t *testing.T) {
	for _, st := range []TaskStatus{TaskInProgress, TaskQueued, TaskWaitingCapacity} {
		m, p := seedRunningPRD(t, st)
		id := taskIDByTitle(p, "t-extra")
		if _, err := m.RemoveTask(p.ID, p.Story[0].ID, id, "operator"); err == nil {
			t.Fatalf("status %s: removal should be refused", st)
		} else if got := err.Error(); !strings.Contains(got, string(st)) {
			t.Fatalf("status %s: error does not name the state: %v", st, err)
		}
		if after, _ := m.Store().GetPRD(p.ID); taskIDByTitle(after, "t-extra") == "" {
			t.Fatalf("status %s: task was removed", st)
		}
	}
}

func TestOtherStructuralEditsStillRefusedWhileRunning(t *testing.T) {
	m, p := seedRunningPRD(t, TaskCompleted)
	sid := p.Story[0].ID
	if _, err := m.AddStory(p.ID, "new", "d", "op"); err == nil {
		t.Error("AddStory should be refused")
	}
	if _, err := m.EditStory(p.ID, sid, "x", "y", "op"); err == nil {
		t.Error("EditStory should be refused")
	}
	if _, err := m.RemoveStory(p.ID, sid, "op"); err == nil {
		t.Error("RemoveStory should be refused")
	}
}

func TestAddTaskAndLiveRemoveRefusedWhileRunning(t *testing.T) {
	m, p := seedRunningPRD(t, TaskCompleted)
	if _, err := m.AddTask(p.ID, p.Story[0].ID, "new", "spec", "op", nil); err == nil {
		t.Error("AddTask should be refused while running")
	}
	for _, st := range []TaskStatus{TaskInProgress, TaskQueued, TaskWaitingCapacity} {
		m2, p2 := seedRunningPRD(t, st)
		if _, err := m2.RemoveTask(p2.ID, p2.Story[0].ID, taskIDByTitle(p2, "t-extra"), "op"); err == nil {
			t.Errorf("status %s: RemoveTask should be refused", st)
		}
	}
}

func TestRemovePendingTaskWhileRunning(t *testing.T) {
	m, p := seedRunningPRD(t, TaskCompleted)
	if _, err := m.RemoveTask(p.ID, p.Story[0].ID, taskIDByTitle(p, "t-dep"), "op"); err != nil {
		t.Fatalf("never-started pending task should be removable: %v", err)
	}
	// A pending task that already holds a session is live and is refused.
	m2, p2 := seedRunningPRD(t, TaskCompleted)
	id := taskIDByTitle(p2, "t-dep")
	m2.Store().WithLock(func() {
		for i := range p2.Story[0].Tasks {
			if p2.Story[0].Tasks[i].ID == id {
				p2.Story[0].Tasks[i].SessionID = "live"
			}
		}
	})
	if _, err := m2.RemoveTask(p2.ID, p2.Story[0].ID, id, "op"); err == nil {
		t.Fatal("pending task with a session should be refused")
	}
}
