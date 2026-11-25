package autonomous

import "testing"

// A task stores the short session id and the lister reports the full one. A
// restart that adopts that same live worker must leave the recorded id as it
// is: the session is the one the task started, not a new one.
func TestAdoptLiveWorktree_SameSessionKeepsRecordedID(t *testing.T) {
	m := capFixtureManager(t)
	fs := newFakeSessions("host-a1b2")
	fs.wire(m)
	cur, dir := pendingTaskWithDir(t, m)
	tk := &cur.Story[0].Tasks[0]
	tk.SessionID = "a1b2"
	tk.ComputeNode = "node-a"
	m.SetSessionListerFn(listerFor(fs, SessionInfo{ID: "host-a1b2", TaskID: tk.ID, PRDID: cur.ID, ProjectDir: dir, TmuxSession: "tmux-a1b2"}))
	if id := m.adoptLiveWorkerBeforeSpawn(cur, tk, 0, dir); id != "host-a1b2" {
		t.Fatalf("adopted %q, want host-a1b2", id)
	}
	if tk.SessionID != "a1b2" {
		t.Fatalf("recorded session id = %q after adopting the same worker, want a1b2", tk.SessionID)
	}
	if !tk.AdoptedSession || tk.ComputeNode != "node-a" {
		t.Fatalf("adopted=%v node=%q, want adopted on node-a", tk.AdoptedSession, tk.ComputeNode)
	}
}

// Adopting a different session still records its id and drops the old node.
func TestAdoptLiveWorktree_OtherSessionRecordsItsID(t *testing.T) {
	m := capFixtureManager(t)
	fs := newFakeSessions("host-c3d4")
	fs.wire(m)
	cur, dir := pendingTaskWithDir(t, m)
	tk := &cur.Story[0].Tasks[0]
	tk.SessionID = "a1b2"
	tk.ComputeNode = "node-a"
	m.SetSessionListerFn(listerFor(fs, SessionInfo{ID: "host-c3d4", TaskID: tk.ID, PRDID: cur.ID, ProjectDir: dir, TmuxSession: "tmux-c3d4"}))
	if id := m.adoptLiveWorkerBeforeSpawn(cur, tk, 0, dir); id != "host-c3d4" {
		t.Fatalf("adopted %q, want host-c3d4", id)
	}
	if tk.SessionID != "host-c3d4" || tk.ComputeNode != "" {
		t.Fatalf("session=%q node=%q, want host-c3d4 and no node", tk.SessionID, tk.ComputeNode)
	}
}
