package session

import (
	"path/filepath"
	"testing"
	"time"
)

func makeTestSession(id, hostname string, state State) *Session {
	return &Session{
		ID:          id,
		FullID:      hostname + "-" + id,
		Task:        "test task for " + id,
		ProjectDir:  "/tmp/test-" + id,
		TmuxSession: "cs-" + hostname + "-" + id,
		LogFile:     "/tmp/test-" + id + ".log",
		State:       state,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		Hostname:    hostname,
	}
}

func TestStore_NewEmpty(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "sessions.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if n := len(s.List()); n != 0 {
		t.Errorf("new store has %d sessions, want 0", n)
	}
}

func TestStore_NewFromMissingFile(t *testing.T) {
	dir := t.TempDir()
	// File doesn't exist — should succeed with empty store
	s, err := NewStore(filepath.Join(dir, "nonexistent.json"))
	if err != nil {
		t.Fatalf("NewStore with missing file: %v", err)
	}
	if s == nil {
		t.Fatal("store should not be nil")
	}
}

func TestStore_Save_Get(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(filepath.Join(dir, "sessions.json"))

	sess := makeTestSession("ab12", "myhost", StateRunning)
	if err := s.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, ok := s.Get("myhost-ab12")
	if !ok {
		t.Fatal("Get returned not found for saved session")
	}
	if got.ID != "ab12" {
		t.Errorf("ID = %q, want ab12", got.ID)
	}
	if got.State != StateRunning {
		t.Errorf("State = %q, want %q", got.State, StateRunning)
	}
	if got.Task != sess.Task {
		t.Errorf("Task = %q, want %q", got.Task, sess.Task)
	}
}

// F10 sprint 3.6 — Session.AgentID survives Save → disk → Load.
func TestStore_Save_Get_AgentID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")

	s, _ := NewStore(path)
	sess := makeTestSession("cd34", "myhost", StateRunning)
	sess.AgentID = "agent-abc-123"
	if err := s.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Reload from disk via a fresh Store to ensure the field made it
	// through JSON serialisation, not just the in-memory map.
	s2, _ := NewStore(path)
	got, ok := s2.Get("myhost-cd34")
	if !ok {
		t.Fatal("Get returned not found for persisted session")
	}
	if got.AgentID != "agent-abc-123" {
		t.Errorf("AgentID = %q, want agent-abc-123", got.AgentID)
	}
}

func TestStore_GetMissing(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(filepath.Join(dir, "sessions.json"))

	_, ok := s.Get("nonexistent-1234")
	if ok {
		t.Error("Get should return false for unknown fullID")
	}
}

func TestStore_GetByShortID(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(filepath.Join(dir, "sessions.json"))

	sess := makeTestSession("cd34", "myhost", StateWaitingInput)
	s.Save(sess)

	got, ok := s.GetByShortID("cd34")
	if !ok {
		t.Fatal("GetByShortID returned not found")
	}
	if got.FullID != "myhost-cd34" {
		t.Errorf("FullID = %q, want myhost-cd34", got.FullID)
	}
}

func TestStore_GetByShortID_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(filepath.Join(dir, "sessions.json"))
	s.Save(makeTestSession("ef56", "myhost", StateRunning))

	_, ok := s.GetByShortID("EF56")
	if !ok {
		t.Error("GetByShortID should be case-insensitive")
	}
	_, ok = s.GetByShortID("Ef56")
	if !ok {
		t.Error("GetByShortID should be case-insensitive (mixed case)")
	}
}

func TestStore_GetByShortID_Missing(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(filepath.Join(dir, "sessions.json"))

	_, ok := s.GetByShortID("zzzz")
	if ok {
		t.Error("GetByShortID should return false for unknown short ID")
	}
}

func TestStore_List(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(filepath.Join(dir, "sessions.json"))

	s.Save(makeTestSession("aa11", "host1", StateRunning))
	s.Save(makeTestSession("bb22", "host1", StateComplete))
	s.Save(makeTestSession("cc33", "host2", StateKilled))

	list := s.List()
	if len(list) != 3 {
		t.Errorf("List() returned %d sessions, want 3", len(list))
	}
}

func TestStore_ListEmpty(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(filepath.Join(dir, "sessions.json"))

	list := s.List()
	if list == nil {
		t.Error("List() should return empty slice, not nil")
	}
	if len(list) != 0 {
		t.Errorf("List() returned %d sessions, want 0", len(list))
	}
}

func TestStore_Update(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(filepath.Join(dir, "sessions.json"))

	sess := makeTestSession("dd44", "myhost", StateRunning)
	s.Save(sess)

	sess.State = StateComplete
	sess.UpdatedAt = time.Now()
	s.Save(sess)

	got, _ := s.Get("myhost-dd44")
	if got.State != StateComplete {
		t.Errorf("State after update = %q, want %q", got.State, StateComplete)
	}
	if len(s.List()) != 1 {
		t.Errorf("should still have 1 session after update, got %d", len(s.List()))
	}
}

func TestStore_Delete(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(filepath.Join(dir, "sessions.json"))

	s.Save(makeTestSession("ee55", "myhost", StateComplete))
	s.Save(makeTestSession("ff66", "myhost", StateRunning))

	if err := s.Delete("myhost-ee55"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, ok := s.Get("myhost-ee55"); ok {
		t.Error("deleted session should not be found")
	}
	if _, ok := s.Get("myhost-ff66"); !ok {
		t.Error("other session should still exist after delete")
	}
	if len(s.List()) != 1 {
		t.Errorf("List() = %d, want 1 after delete", len(s.List()))
	}
}

func TestStore_DeleteMissing(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(filepath.Join(dir, "sessions.json"))

	// Deleting a non-existent session should not error
	if err := s.Delete("nonexistent-1234"); err != nil {
		t.Errorf("Delete non-existent: %v", err)
	}
}

func TestStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")

	// Write two sessions
	s1, _ := NewStore(path)
	s1.Save(makeTestSession("gg77", "myhost", StateRunning))
	s1.Save(makeTestSession("hh88", "myhost", StateWaitingInput))

	// Reload from disk
	s2, err := NewStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(s2.List()) != 2 {
		t.Errorf("reloaded store has %d sessions, want 2", len(s2.List()))
	}

	got, ok := s2.Get("myhost-gg77")
	if !ok {
		t.Error("session gg77 not found after reload")
	}
	if got.State != StateRunning {
		t.Errorf("State after reload = %q, want %q", got.State, StateRunning)
	}

	got2, ok := s2.Get("myhost-hh88")
	if !ok {
		t.Error("session hh88 not found after reload")
	}
	if got2.State != StateWaitingInput {
		t.Errorf("State after reload = %q, want %q", got2.State, StateWaitingInput)
	}
}

func TestStore_PersistAfterDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")

	s1, _ := NewStore(path)
	s1.Save(makeTestSession("ii99", "myhost", StateComplete))
	s1.Save(makeTestSession("jj00", "myhost", StateKilled))
	s1.Delete("myhost-ii99")

	s2, _ := NewStore(path)
	if len(s2.List()) != 1 {
		t.Errorf("after delete+reload, got %d sessions, want 1", len(s2.List()))
	}
	if _, ok := s2.Get("myhost-ii99"); ok {
		t.Error("deleted session should not appear after reload")
	}
}

func TestStore_MultipleSavesSameID(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(filepath.Join(dir, "sessions.json"))

	sess := makeTestSession("kk11", "myhost", StateRunning)
	s.Save(sess)
	s.Save(sess)
	s.Save(sess)

	if len(s.List()) != 1 {
		t.Errorf("multiple saves of same session: got %d, want 1", len(s.List()))
	}
}

func TestStateConstants(t *testing.T) {
	states := []State{StateRunning, StateWaitingInput, StateComplete, StateFailed, StateKilled, StateRateLimited}
	seen := make(map[State]bool)
	for _, s := range states {
		if s == "" {
			t.Error("state constant should not be empty string")
		}
		if seen[s] {
			t.Errorf("duplicate state constant: %q", s)
		}
		seen[s] = true
	}
}
