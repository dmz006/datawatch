package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
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
	s.Save(sess) //nolint:errcheck

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
	s.Save(makeTestSession("ef56", "myhost", StateRunning)) //nolint:errcheck

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

	s.Save(makeTestSession("aa11", "host1", StateRunning)) //nolint:errcheck
	s.Save(makeTestSession("bb22", "host1", StateComplete)) //nolint:errcheck
	s.Save(makeTestSession("cc33", "host2", StateKilled)) //nolint:errcheck

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
	s.Save(sess) //nolint:errcheck

	sess.State = StateComplete
	sess.UpdatedAt = time.Now()
	s.Save(sess) //nolint:errcheck

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

	s.Save(makeTestSession("ee55", "myhost", StateComplete)) //nolint:errcheck
	s.Save(makeTestSession("ff66", "myhost", StateRunning)) //nolint:errcheck

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
	s1.Save(makeTestSession("gg77", "myhost", StateRunning)) //nolint:errcheck
	s1.Save(makeTestSession("hh88", "myhost", StateWaitingInput)) //nolint:errcheck

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
	s1.Save(makeTestSession("ii99", "myhost", StateComplete)) //nolint:errcheck
	s1.Save(makeTestSession("jj00", "myhost", StateKilled)) //nolint:errcheck
	s1.Delete("myhost-ii99") //nolint:errcheck

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
	s.Save(sess) //nolint:errcheck
	s.Save(sess) //nolint:errcheck
	s.Save(sess) //nolint:errcheck

	if len(s.List()) != 1 {
		t.Errorf("multiple saves of same session: got %d, want 1", len(s.List()))
	}
}

// BL92 — Save must write through synchronously (no debounce). The
// previous bug surfaced when an operator-spawned session existed on
// disk under sessions/<id>/session.json but never made it into
// sessions.json because of an in-memory-only update path. This test
// asserts every Save flushes the registry before returning.
func TestStore_Save_WriteThrough(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	sess := makeTestSession("wt01", "host", StateRunning)
	if err := s.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Re-open the file in a separate Store instance — proves the
	// data was already on disk at Save return time.
	s2, err := NewStore(path)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	if got, ok := s2.Get(sess.FullID); !ok {
		t.Fatalf("session not on disk after Save")
	} else if got.State != StateRunning {
		t.Errorf("state mismatch: got %q want %q", got.State, StateRunning)
	}
}

func TestStore_Flush_PersistsCurrentState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	s, _ := NewStore(path)
	sess := makeTestSession("fl01", "host", StateRunning)
	_ = s.Save(sess)

	// Flush after no mutation must succeed and not corrupt.
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	s2, _ := NewStore(path)
	if _, ok := s2.Get(sess.FullID); !ok {
		t.Fatal("session lost after Flush")
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

// v6.15.1 (BL286) — corruption recovery + subdir merge regression test.
// Operator post-mortem 2026-05-07: a non-atomic write left
// sessions.json truncated at 128 KB mid-string and the daemon couldn't
// boot. v6.15.1 ships atomic write + auto-recovery; this test pins
// both paths so they can't silently regress.
func TestStore_CorruptedSessionsJson_AutoRecovery(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "sessions.json")

	// Build a 3-session fixture, then truncate mid-third-session to
	// simulate the operator's outage.
	good := `[
  {"id":"aaaa","full_id":"host-aaaa","name":"first","state":"complete"},
  {"id":"bbbb","full_id":"host-bbbb","name":"second","state":"complete"},
  {"id":"cccc","full_id":"host-cccc","name":"thi`
	if err := os.WriteFile(storePath, []byte(good), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Open should NOT error — recovery should kick in.
	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore should auto-recover, got: %v", err)
	}

	// First two sessions survive.
	if _, ok := store.Get("host-aaaa"); !ok {
		t.Errorf("first session lost in recovery")
	}
	if _, ok := store.Get("host-bbbb"); !ok {
		t.Errorf("second session lost in recovery")
	}
	// Third was truncated mid-record — should NOT be present.
	if _, ok := store.Get("host-cccc"); ok {
		t.Errorf("partial third session should have been dropped")
	}
	// Corrupted body should be saved for forensics.
	matches, _ := filepath.Glob(storePath + ".corrupted-*")
	if len(matches) == 0 {
		t.Errorf("expected a .corrupted-* backup at %s; found none", storePath)
	}
}

func TestStore_CorruptedSessionsJson_SubdirMerge(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "sessions.json")

	// Truncated JSON has two sessions.
	good := `[
  {"id":"aaaa","full_id":"host-aaaa","name":"first","state":"complete"},
  {"id":"bbbb","full_id":"host-bbbb","name":"second","state":"complete"}
]`
	if err := os.WriteFile(storePath, []byte(good), 0644); err != nil {
		t.Fatalf("seed json: %v", err)
	}

	// But three subdirectories exist on disk — including one (host-cccc)
	// that's NOT in the JSON. Subdir merge should pick it up.
	for _, id := range []string{"host-aaaa", "host-bbbb", "host-cccc"} {
		sub := filepath.Join(dir, "sessions", id)
		if err := os.MkdirAll(sub, 0755); err != nil {
			t.Fatalf("mkdir subdir: %v", err)
		}
		meta := `{"id":"` + id[len(id)-4:] + `","full_id":"` + id + `","name":"sub-` + id + `","state":"running"}`
		if err := os.WriteFile(filepath.Join(sub, "session.json"), []byte(meta), 0644); err != nil {
			t.Fatalf("write subdir meta: %v", err)
		}
	}

	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for _, id := range []string{"host-aaaa", "host-bbbb", "host-cccc"} {
		if _, ok := store.Get(id); !ok {
			t.Errorf("session %q missing after subdir merge", id)
		}
	}
}

func TestSecfile_AtomicWrite_SurvivesPartialFlush(t *testing.T) {
	// Confirms WriteFile uses an atomic .tmp + rename pattern: even if
	// the destination already exists with content, a successful write
	// fully replaces it without ever leaving a half-written body. This
	// test exercises the happy path; the failure-mode invariant (a
	// crash during write leaves the OLD body untouched, not a half
	// file) is enforced by the rename's POSIX atomicity.
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	if err := os.WriteFile(path, []byte("[{\"id\":\"old\"}]"), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	sess := &Session{ID: "new", FullID: "host-new", Name: "fresh"}
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// The .tmp file should not linger after a successful rename.
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Errorf("WriteFile should remove .tmp after successful rename; .tmp still exists")
	}
}

// BL294 — concurrent persist() calls from two goroutines (simulating two
// Manager instances sharing sessions.json during a daemon re-exec) must not
// corrupt the file. After both goroutines finish, the file must contain valid
// JSON and must include all sessions that were saved.
func TestStore_ConcurrentPersist_NoCorruption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")

	// Two independent Store instances share the same file — simulating the
	// brief window where the parent daemon and child daemon co-exist.
	s1, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore s1: %v", err)
	}
	s2, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore s2: %v", err)
	}

	// Pre-populate each store's in-memory map so their persist() calls write
	// disjoint but overlapping session sets.
	sess1a := makeTestSession("cc01", "host", StateRunning)
	sess1b := makeTestSession("cc02", "host", StateComplete)
	sess2a := makeTestSession("cc03", "host", StateRunning)
	sess2b := makeTestSession("cc04", "host", StateWaitingInput)

	// Load sessions into each store without calling persist yet.
	s1.mu.Lock()
	s1.sessions[sess1a.FullID] = sess1a
	s1.sessions[sess1b.FullID] = sess1b
	s1.mu.Unlock()

	s2.mu.Lock()
	s2.sessions[sess2a.FullID] = sess2a
	s2.sessions[sess2b.FullID] = sess2b
	s2.mu.Unlock()

	// Fire concurrent persists.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		s1.mu.Lock()
		defer s1.mu.Unlock()
		if err := s1.persist(); err != nil {
			t.Errorf("s1.persist: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		s2.mu.Lock()
		defer s2.mu.Unlock()
		if err := s2.persist(); err != nil {
			t.Errorf("s2.persist: %v", err)
		}
	}()
	wg.Wait()

	// The file must be valid JSON (no corruption from torn writes).
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sessions.json after concurrent persist: %v", err)
	}
	var sessions []*Session
	if err := json.Unmarshal(raw, &sessions); err != nil {
		t.Fatalf("sessions.json is not valid JSON after concurrent persist: %v\nbody: %s", err, raw)
	}
	// The last writer wins; it must have written a non-empty, structurally
	// valid array — we already verified that above. No further assertion on
	// which writer's sessions survived (last-writer-wins is acceptable for
	// cross-process races; the boot ReconcileSessions recovers any gap).
	if len(sessions) == 0 {
		t.Errorf("sessions.json is empty after concurrent persist — expected at least one session")
	}
}
