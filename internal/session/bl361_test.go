package session

// BL361 — structured session filter tests.

import (
	"testing"
	"time"
)

// bl361MakeManager creates a Manager with pre-populated test sessions.
func bl361MakeManager(t *testing.T) *Manager {
	t.Helper()
	mgr, err := NewManager("testhost", t.TempDir(), "/bin/echo", 0)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	alive := true
	dead := false

	sessions := []*Session{
		{
			ID: "aa01", FullID: "testhost-aa01",
			Name: "worker-1", State: StateRunning,
			BackendFamily: "claudecode", ClaudeAlive: &alive,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
		{
			ID: "aa02", FullID: "testhost-aa02",
			Name: "worker-2", State: StateRunning,
			BackendFamily: "claudecode", ClaudeAlive: &dead,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
		{
			ID: "aa03", FullID: "testhost-aa03",
			Name: "manager", State: StateWaitingInput,
			BackendFamily: "opencode", ClaudeAlive: &alive,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
		{
			ID: "aa04", FullID: "testhost-aa04",
			Name: "agent-x", State: StateComplete,
			BackendFamily: "opencode",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
		{
			ID: "aa05", FullID: "testhost-aa05",
			Name: "worker-3", State: StateFailed,
			BackendFamily: "claudecode",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
	}

	for _, s := range sessions {
		if err := mgr.SaveSession(s); err != nil {
			t.Fatalf("SaveSession %s: %v", s.ID, err)
		}
	}
	return mgr
}

// TestBL361_FilterByName verifies glob filter: worker-* matches worker-1,
// worker-2, worker-3 but not manager or agent-x.
func TestBL361_FilterByName(t *testing.T) {
	mgr := bl361MakeManager(t)

	got := mgr.ListSessionsFiltered(SessionFilter{Name: "worker-*"})
	if len(got) != 3 {
		names := make([]string, len(got))
		for i, s := range got {
			names[i] = s.Name
		}
		t.Fatalf("want 3 sessions matching worker-*, got %d: %v", len(got), names)
	}
	for _, s := range got {
		if s.Name != "worker-1" && s.Name != "worker-2" && s.Name != "worker-3" {
			t.Errorf("unexpected session name %q in worker-* results", s.Name)
		}
	}
}

// TestBL361_FilterByState verifies that state=running returns only running sessions.
func TestBL361_FilterByState(t *testing.T) {
	mgr := bl361MakeManager(t)

	got := mgr.ListSessionsFiltered(SessionFilter{State: "running"})
	if len(got) != 2 {
		t.Fatalf("want 2 running sessions, got %d", len(got))
	}
	for _, s := range got {
		if s.State != StateRunning {
			t.Errorf("expected state=running, got %q for session %s", s.State, s.ID)
		}
	}
}

// TestBL361_FilterByBackend verifies that backend=opencode returns only opencode sessions.
func TestBL361_FilterByBackend(t *testing.T) {
	mgr := bl361MakeManager(t)

	got := mgr.ListSessionsFiltered(SessionFilter{Backend: "opencode"})
	if len(got) != 2 {
		t.Fatalf("want 2 opencode sessions, got %d", len(got))
	}
	for _, s := range got {
		if s.BackendFamily != "opencode" {
			t.Errorf("expected backend=opencode, got %q for session %s", s.BackendFamily, s.ID)
		}
	}
}

// TestBL361_FilterByAlive verifies that alive=true returns only sessions where
// ClaudeAlive is non-nil and true.
func TestBL361_FilterByAlive(t *testing.T) {
	mgr := bl361MakeManager(t)

	// alive=true: worker-1 and manager (both have ClaudeAlive=true)
	got := mgr.ListSessionsFiltered(SessionFilter{Alive: "true"})
	if len(got) != 2 {
		t.Fatalf("want 2 alive=true sessions, got %d", len(got))
	}
	for _, s := range got {
		if s.ClaudeAlive == nil || !*s.ClaudeAlive {
			t.Errorf("session %s should have ClaudeAlive=true", s.ID)
		}
	}

	// alive=false: only worker-2 has ClaudeAlive=false
	gotDead := mgr.ListSessionsFiltered(SessionFilter{Alive: "false"})
	if len(gotDead) != 1 {
		t.Fatalf("want 1 alive=false session, got %d", len(gotDead))
	}
	if gotDead[0].ClaudeAlive == nil || *gotDead[0].ClaudeAlive {
		t.Errorf("session %s should have ClaudeAlive=false", gotDead[0].ID)
	}
}

// TestBL361_FilterCombined verifies AND logic: backend=claudecode + state=running
// should return only the two running claudecode sessions (worker-1, worker-2).
func TestBL361_FilterCombined(t *testing.T) {
	mgr := bl361MakeManager(t)

	got := mgr.ListSessionsFiltered(SessionFilter{
		Backend: "claudecode",
		State:   "running",
	})
	if len(got) != 2 {
		names := make([]string, len(got))
		for i, s := range got {
			names[i] = s.Name
		}
		t.Fatalf("want 2 claudecode+running sessions, got %d: %v", len(got), names)
	}
	for _, s := range got {
		if s.BackendFamily != "claudecode" || s.State != StateRunning {
			t.Errorf("session %s has backend=%q state=%q; expected claudecode+running",
				s.ID, s.BackendFamily, s.State)
		}
	}

	// Adding alive=true should narrow to only worker-1
	gotAlive := mgr.ListSessionsFiltered(SessionFilter{
		Backend: "claudecode",
		State:   "running",
		Alive:   "true",
	})
	if len(gotAlive) != 1 {
		t.Fatalf("want 1 claudecode+running+alive session, got %d", len(gotAlive))
	}
	if gotAlive[0].Name != "worker-1" {
		t.Errorf("expected worker-1, got %q", gotAlive[0].Name)
	}
}

// TestBL361_NoFilter verifies that an empty filter returns all sessions.
func TestBL361_NoFilter(t *testing.T) {
	mgr := bl361MakeManager(t)

	all := mgr.ListSessionsFiltered(SessionFilter{})
	if len(all) != 5 {
		t.Fatalf("want 5 sessions with empty filter, got %d", len(all))
	}
}
