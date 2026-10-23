package autonomous

import "testing"

// linkFixture builds a manager with one in-progress task and returns the data
// dir so a test can reload the store from disk.
func linkFixture(t *testing.T) (m *Manager, dir string, prd *PRD, task Task) {
	t.Helper()
	dir = t.TempDir()
	m, err := NewManager(dir, DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err = m.CreatePRD("spec", "/projlink", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := m.Store().SetStories(prd.ID, []Story{
		{Title: "story", Tasks: []Task{{Title: "task", Status: TaskInProgress}}},
	}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	return m, dir, prd, prd.Story[0].Tasks[0]
}

// fakeCreate mimics the session manager: create the record, fire the
// created hook, then deliver the task text.
func fakeCreate(prdID, taskID, sessionID string, created func(prdID, taskID, sessionID string), deliver func()) {
	created(prdID, taskID, sessionID)
	deliver()
}

func TestTaskSessionLinkPersistedBeforeDelivery(t *testing.T) {
	m, _, prd, task := linkFixture(t)
	const sid = "sess-new"
	var atDelivery string
	fakeCreate(prd.ID, task.ID, sid, m.LinkTaskSession, func() {
		got, _ := m.Store().GetTask(task.ID)
		atDelivery = got.SessionID
	})
	if atDelivery != sid {
		t.Fatalf("SessionID at delivery = %q, want %q", atDelivery, sid)
	}
}

func TestTaskSessionLinkSurvivesRestartBeforeFirstPrompt(t *testing.T) {
	m, dir, prd, task := linkFixture(t)
	const sid = "sess-restart"
	fakeCreate(prd.ID, task.ID, sid, m.LinkTaskSession, func() {
		// Daemon restart in the window: reload the store from disk.
		s2, err := NewStore(dir)
		if err != nil {
			t.Fatalf("reload store: %v", err)
		}
		got, ok := s2.GetTask(task.ID)
		if !ok {
			t.Fatalf("task %s missing after reload", task.ID)
		}
		if got.SessionID != sid {
			t.Fatalf("reloaded SessionID = %q, want %q", got.SessionID, sid)
		}
	})
}
