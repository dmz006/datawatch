// Resetting a task ends every live session of the task or its worktree before
// a replacement launches, and records them.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -run 'TestResetEnds|TestResetRecords|TestOneSessionTotal'
package autonomous

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// liveFake is a session table whose kill flips Alive, like a real manager.
type liveFake struct {
	mu       sync.Mutex
	sessions []SessionInfo
	kills    []string
}

func (f *liveFake) list() []SessionInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]SessionInfo(nil), f.sessions...)
}

func (f *liveFake) kill(id, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kills = append(f.kills, id)
	for i := range f.sessions {
		if f.sessions[i].ID == id {
			f.sessions[i].Alive = false
		}
	}
	return nil
}

func (f *liveFake) liveIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, s := range f.sessions {
		if s.Alive {
			out = append(out, s.ID)
		}
	}
	return out
}

func (f *liveFake) wire(m *Manager) {
	m.SetSessionAliveFn(func(string) bool { return false })
	m.SetSessionListerFn(f.list)
	m.SetSessionKillerFn(f.kill)
}

func TestResetEndsEveryLiveSessionOfTaskWorktree(t *testing.T) {
	m, prd, dir := adoptReconcileSetup(t, "sess-dead")
	task := prd.Story[0].Tasks[0]
	f := &liveFake{sessions: []SessionInfo{
		{ID: "by-task", TaskID: task.ID, PRDID: prd.ID, Alive: true},
		{ID: "by-dir", PRDID: prd.ID, ProjectDir: dir + "/", Task: "no id here", Alive: true},
		{ID: "other-prd-dir", PRDID: "someone", ProjectDir: dir, Alive: true},
		{ID: "other-task", TaskID: "someone-else", PRDID: prd.ID, ProjectDir: dir, Alive: true},
	}}
	f.wire(m)
	got := m.endLiveSessionsOfTask(prd, &prd.Story[0].Tasks[0], "", "autonomous.test", "test reason")
	if len(got) != 2 {
		t.Fatalf("ended %v, want by-task and by-dir only", got)
	}
	live := strings.Join(f.liveIDs(), ",")
	if live != "other-prd-dir,other-task" {
		t.Fatalf("live after end = %q", live)
	}
	// a just-adopted session is never ended
	f2 := &liveFake{sessions: []SessionInfo{{ID: "adopted", TaskID: task.ID, PRDID: prd.ID, Alive: true}}}
	f2.wire(m)
	if got := m.endLiveSessionsOfTask(prd, &prd.Story[0].Tasks[0], "adopted", "autonomous.test", "test reason"); len(got) != 0 || len(f2.kills) != 0 {
		t.Fatalf("adopted session ended: %v", got)
	}
}

func TestResetRecordsEndedSessionsInTaskHistory(t *testing.T) {
	m, prd, dir := adoptReconcileSetup(t, "sess-dead")
	task := prd.Story[0].Tasks[0]
	f := &liveFake{sessions: []SessionInfo{
		{ID: "old-a", PRDID: prd.ID, ProjectDir: dir, Task: "unrelated", CreatedAt: time.Now(), Alive: true},
	}}
	f.wire(m)
	m.reconcileStuckTasks()

	got, _ := m.Store().GetPRD(prd.ID)
	tk := got.Story[0].Tasks[0]
	if tk.Status != TaskPending || tk.SessionID != "" {
		t.Fatalf("status=%s session=%q, want reset to pending", tk.Status, tk.SessionID)
	}
	if len(tk.History) != 1 || !strings.Contains(tk.History[0].Note, "old-a") {
		t.Fatalf("task history = %+v, want a note naming old-a", tk.History)
	}
	found := false
	for _, d := range got.Decisions {
		if d.Kind == "ended_sessions" && strings.Contains(d.Note, "old-a") && strings.Contains(d.Note, task.ID) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no ended_sessions decision naming old-a: %+v", got.Decisions)
	}
}

func TestOneSessionTotalAfterUnadoptableReset(t *testing.T) {
	m, prd, dir := adoptReconcileSetup(t, "")
	f := &liveFake{sessions: []SessionInfo{
		// same worktree and PRD but its task text does not name the task: not adoptable
		{ID: "old-unadoptable", PRDID: prd.ID, ProjectDir: dir, Task: "something else", Alive: true},
	}}
	f.wire(m)
	m.reconcileStuckTasks()

	got, _ := m.Store().GetPRD(prd.ID)
	tk := got.Story[0].Tasks[0]
	if tk.Status != TaskPending {
		t.Fatalf("status=%s, want pending (not adopted)", tk.Status)
	}
	if live := f.liveIDs(); len(live) != 0 {
		t.Fatalf("live after reset = %v, want none before launch", live)
	}
	// the replacement launch: the executor ends leftovers again (none), then spawns one
	if again := m.endLiveSessionsOfTask(got, &got.Story[0].Tasks[0], "", "autonomous.test", "test reason"); len(again) != 0 {
		t.Fatalf("second end pass found %v", again)
	}
	f.mu.Lock()
	f.sessions = append(f.sessions, SessionInfo{ID: "new", TaskID: tk.ID, PRDID: prd.ID, ProjectDir: dir, Alive: true})
	f.mu.Unlock()
	if live := f.liveIDs(); len(live) != 1 || live[0] != "new" {
		t.Fatalf("live = %v, want exactly [new]", live)
	}
}

// shortIDAlive reports a session alive when its short id is shortID, the way
// the daemon's liveness hook resolves either id form.
func shortIDAlive(shortID string) func(string) bool {
	return func(id string) bool { return sessionShortID(id) == shortID }
}

func TestResetKeepsBoundWorkerWhenStoredIDIsShort(t *testing.T) {
	m, prd, dir := adoptReconcileSetup(t, "a1b2")
	task := prd.Story[0].Tasks[0]
	f := &liveFake{sessions: []SessionInfo{
		{ID: "datawatch-box-a1b2", TaskID: task.ID, PRDID: prd.ID, ProjectDir: dir, Alive: true},
	}}
	f.wire(m)
	m.SetSessionAliveFn(shortIDAlive("a1b2"))
	// The task stores the short id; the caller keeps the bound worker by that id.
	got := m.endLiveSessionsOfTask(prd, &task, task.SessionID, "autonomous.test", "test reason")
	if len(got) != 0 || len(f.kills) != 0 {
		t.Fatalf("bound worker ended: ended=%v kills=%v", got, f.kills)
	}
	if live := f.liveIDs(); len(live) != 1 || live[0] != "datawatch-box-a1b2" {
		t.Fatalf("live = %v, want the bound worker", live)
	}
}

func TestResetDoesNotEndWorkerClaimedByAnotherTask(t *testing.T) {
	m, prd, dir := claimedPairSetup(t, "a1b2")
	unclaimed := prd.Story[0].Tasks[0]
	f := &liveFake{sessions: []SessionInfo{
		// bound to task b under its short id; unlinked, same worktree, so only the claim keeps it
		{ID: "datawatch-box-a1b2", PRDID: prd.ID, ProjectDir: dir, Alive: true},
		// same worktree and unclaimed by any task: the control that proves the match runs
		{ID: "datawatch-box-c3d4", PRDID: prd.ID, ProjectDir: dir, Alive: true},
	}}
	f.wire(m)
	m.SetSessionAliveFn(shortIDAlive("a1b2"))
	got := m.endLiveSessionsOfTask(prd, &unclaimed, "", "autonomous.test", "test reason")
	if len(got) != 1 || got[0] != "datawatch-box-c3d4" {
		t.Fatalf("ended %v, want only the unclaimed worker", got)
	}
	if live := strings.Join(f.liveIDs(), ","); live != "datawatch-box-a1b2" {
		t.Fatalf("live after reset = %q, want the worker claimed by task b", live)
	}
}
